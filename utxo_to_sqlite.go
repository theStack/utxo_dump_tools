package main

import (
    "bufio"
    "database/sql"
    "encoding/binary"
    "fmt"
    _ "github.com/mattn/go-sqlite3"
    "io"
    "math/big"
    "os"
    "time"
)

const verbose bool = false;
const write_data_as_hex_text bool = true;
var prime, _ = new(big.Int).SetString( // prime used for secp256k1
    "fffffffffffffffffffffffffffffffffffffffffffffffffffffffefffffc2f", 16)

// Decompress pubkey by calculating y = sqrt(x^3 + 7) % p
// and negating the result if necessary
func decompressPubkey(pubkey_in []byte, pubkey_out []byte) bool {
    if len(pubkey_in) != 33 {
        panic("compressed pubkey must be 33 bytes long!")
    }
    if pubkey_in[0] != 0x02 && pubkey_in[0] != 0x03 {
        panic("compressed pubkey must have even/odd tag of 0x02 or 0x03!")
    }
    if len(pubkey_out) != 65 {
        panic("storage for uncompressed pubkey must be 65 bytes long!")
    }

    x := new(big.Int).SetBytes(pubkey_in[1:])
    x2 := new(big.Int).Mul(x, x)
    x2.Mod(x2, prime) // x^2 = (x * x) % p
    x3 := new(big.Int).Mul(x2, x)
    x3.Mod(x3, prime) // x^3 = (x^2 * x) % p
    rhs := new(big.Int).Add(x3, big.NewInt(7))
    rhs.Mod(rhs, prime) // rhs = (x^3 + 7) % p
    y := new(big.Int).ModSqrt(rhs, prime) // y = sqrt(x^3 + 7) % p
    if y == nil {
        fmt.Printf("WARNING: Couldn't find modular square root!\n")
        return false
    }

    tag_is_odd := pubkey_in[0] == 3
    y_is_odd := y.Bit(0) == 1
    if tag_is_odd != y_is_odd {
        neg_y := new(big.Int).Sub(big.NewInt(0), y)
        y.Mod(neg_y, prime)
    }

    pubkey_out[0] = 0x04
    x.FillBytes(pubkey_out[1:33])
    y.FillBytes(pubkey_out[33:65])
    return true
}

func readIntoSlice(r *bufio.Reader, buf []byte) {
    _, err := io.ReadFull(r, buf)
    if err != nil { panic(err) }
}

func readUInt32(r *bufio.Reader, target *uint32) {
    err := binary.Read(r, binary.LittleEndian, target)
    if err != nil { panic(err) }
}

func readUInt64(r *bufio.Reader, target *uint64) {
    err := binary.Read(r, binary.LittleEndian, target)
    if err != nil { panic(err) }
}

func readCompressedScript(spkSize uint64, r *bufio.Reader) (bool, []byte) {
    buf := make([]byte, 0, 67)  // enough capacity for special types (0-5)
    switch spkSize {
    case 0: // P2PKH
        buf = buf[:25]
        buf[0], buf[1], buf[2] = 0x76, 0xa9, 20
        readIntoSlice(r, buf[3:23])
        buf[23], buf[24] = 0x88, 0xac
    case 1: // P2SH
        buf = buf[:23]
        buf[0], buf[1] = 0xa9, 20
        readIntoSlice(r, buf[2:22])
        buf[22] = 0x87
    case 2, 3: // P2PK (compressed)
        buf = buf[:35]
        buf[0], buf[1] = 33, byte(spkSize)
        readIntoSlice(r, buf[2:34])
        buf[34] = 0xac
    case 4, 5: // P2PK (uncompressed)
        buf = buf[:67]
        var compressed_pubkey [33]byte
        compressed_pubkey[0] = byte(spkSize) - 2
        readIntoSlice(r, compressed_pubkey[1:])
        buf[0] = 65
        success := decompressPubkey(compressed_pubkey[:], buf[1:66])
        if !success {
            return false, nil
        }
        buf[66] = 0xac
    default: // others (bare multisig, segwit etc.)
        readSize := spkSize - 6
        if readSize > 10000 {
            panic(fmt.Sprintf("too long script with size %d\n", readSize))
        }
        buf = make([]byte, readSize)
        readIntoSlice(r, buf[:])
    }

    return true, buf
}

func readVARINT(r *bufio.Reader) (uint64) {
    n := uint64(0)
    for {
        dat, _ := r.ReadByte()
        n = (n << 7) | uint64(dat & 0x7f)
        if (dat & 0x80) > 0 {
            n++
        } else {
            return n
        }
    }
}

func decompressAmount(x uint64) (uint64) {
    if x == 0 {
        return 0
    }
    x--
    e := x % 10
    x /= 10
    n := uint64(0)
    if e < 9 {
        d := (x % 9) + 1
        x /= 9
        n = x*10 + d
    } else {
        n = x+1
    }
    for e > 0 {
        n *= 10
        e--
    }
    return n
}

func hashToStr(bytes [32]byte) (string) {
    for i, j := 0, 31; i < j; i, j = i+1, j-1 {
        bytes[i], bytes[j] = bytes[j], bytes[i]
    }
    return fmt.Sprintf("%x", bytes)
}

func execStmt(db *sql.DB, stmt string) {
    _, err := db.Exec(stmt)
    if err != nil { panic(err) }
}

func main() {
    homeDir, err := os.UserHomeDir()
    f, err := os.OpenFile(homeDir + "/.bitcoin/utxo.dat", os.O_RDONLY, 0600)
    if err != nil { panic(err) }
    utxof := bufio.NewReader(f)

    // read metadata
    var blockHash [32]byte
    var numUTXOs uint64
    readIntoSlice(utxof, blockHash[:])
    readUInt64(utxof, &numUTXOs)
    fmt.Printf("UTXO Snapshot at block %s, contains %d coins\n",
               hashToStr(blockHash), numUTXOs)

    db, err := sql.Open("sqlite3", "file:utxos.sqlite3?_journal_mode=off")
    if err != nil { panic(err) }
    defer db.Close()

    execStmt(db, "DROP TABLE IF EXISTS utxos")
    if write_data_as_hex_text {
        execStmt(db, "CREATE TABLE utxos(txid TEXT, vout INT, value INT, coinbase INT, height INT, scriptpubkey TEXT)")
    } else {
        execStmt(db, "CREATE TABLE utxos(txid BLOB, vout INT, value INT, coinbase INT, height INT, scriptpubkey BLOB)")
    }
    addUTXOStmt, err := db.Prepare("INSERT INTO utxos VALUES (?, ?, ?, ?, ?, ?)")
    if err != nil { panic(err) }
    defer addUTXOStmt.Close()
    tx, err := db.Begin()
    if err != nil { panic(err) }

    t := time.Now()

    coins_skipped := uint64(0)
    for coin_idx := uint64(1); coin_idx <= numUTXOs; coin_idx++ {
        // read key (COutPoint)
        var prevoutHash [32]byte
        var prevoutIndex uint32
        readIntoSlice(utxof, prevoutHash[:])
        readUInt32(utxof, &prevoutIndex)

        // read value (Coin)
        code := readVARINT(utxof)
        height := code >> 1
        isCoinbase := code & 1
        amount := decompressAmount(readVARINT(utxof))
        spkSize := readVARINT(utxof)
        success, scriptPubKey := readCompressedScript(spkSize, utxof)

        // write to database
        if success {
            if write_data_as_hex_text {
                _, err = tx.Stmt(addUTXOStmt).Exec(hashToStr(prevoutHash), prevoutIndex, amount, isCoinbase, height, fmt.Sprintf("%x", scriptPubKey))
            } else {
                _, err = tx.Stmt(addUTXOStmt).Exec(prevoutHash[:], prevoutIndex, amount, isCoinbase, height, scriptPubKey)
            }
            if err != nil { panic(err) }
        } else {
            coins_skipped++
        }

        if verbose {
            fmt.Printf("Coin %d/%d:\n", coin_idx, numUTXOs)
            fmt.Printf("\tprevout.hash = %s\n", hashToStr(prevoutHash))
            fmt.Printf("\tprevout.n = %d\n", prevoutIndex)
            fmt.Printf("\theight = %d, is_coinbase = %d\n", height, isCoinbase)
            fmt.Printf("\tamount = %d sats\n", amount)
            fmt.Printf("\tscriptPubKey = %x\n", scriptPubKey)
        }

        if coin_idx % (1024*1024) == 0 {
            elapsed := time.Since(t)
            fmt.Printf("%d coins read [%.2f%%], %d coins skipped, %s passed since start\n",
                coin_idx, (float32(coin_idx)/float32(numUTXOs))*100, coins_skipped, elapsed)
            tx.Commit()
            tx, err = db.Begin()
            if err != nil { panic(err) }
        }
    }
    tx.Commit()

    // check for EOF (read must fail)
    _, err = utxof.ReadByte()
    if err != nil {
        fmt.Println("EOF reached.")
    } else {
        fmt.Println("WARNING: File is not at EOF yet!")
    }
    fmt.Printf("TOTAL: %d coins read, %d coins skiped => %d coins written.\n",
        numUTXOs, coins_skipped, numUTXOs - coins_skipped)
}
