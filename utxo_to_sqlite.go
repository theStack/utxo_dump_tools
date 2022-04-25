package main

import (
    "bufio"
    "database/sql"
    "encoding/binary"
    _ "github.com/mattn/go-sqlite3"
    "fmt"
    "io"
    "os"
    "time"
)

func log(str string) {
    // fmt.Println(str)
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
        // TODO: convert compressed to uncompressed pubkey (needs secp library :/), put in buf[1:66]
        buf[66] = 0xac
        return false, nil // report UTXO as invalid for now
    default: // others (bare multisig, segwit etc.)
        readSize := spkSize - 6
        if readSize > 10000 {
            panic(fmt.Sprintf("too long script with size %d\n", readSize))
        }
        buf := make([]byte, readSize)
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
    if err != nil {
        panic(err)
    }
    utxof := bufio.NewReader(f)

    // read metadata
    var blockHash [32]byte
    var numUTXOs uint64
    readIntoSlice(utxof, blockHash[:])
    readUInt64(utxof, &numUTXOs)
    fmt.Printf("UTXO Snapshot at block %s, contains %d coins\n",
               hashToStr(blockHash), numUTXOs)

    db, err := sql.Open("sqlite3", "file:utxos.sqlite3?_journal_mode=memory&_cache_size=-128000")
    if err != nil { panic(err) }
    defer db.Close()

    execStmt(db, "DROP TABLE IF EXISTS utxos")
    execStmt(db, "CREATE TABLE utxos (prevoutHash BLOB, prevoutIndex INT, scriptPubKey BLOB, amount INT)")
    addUTXOStmt, err := db.Prepare("INSERT INTO utxos (prevoutHash, prevoutIndex, scriptPubKey, amount) VALUES (?, ?, ?, ?)")
    if err != nil { panic(err) }
    defer addUTXOStmt.Close()
    tx, err := db.Begin()
    if err != nil { panic(err) }

    t := time.Now()

    coins_skipped := uint64(0)
    // read in coins
    for coin_idx := uint64(1); coin_idx <= numUTXOs; coin_idx++ {
        //log(fmt.Sprintf("Coin %d/%d:", coin_idx, numUTXOs))

        // read key (COutPoint)
        var prevoutHash [32]byte
        var prevoutIndex uint32
        readIntoSlice(utxof, prevoutHash[:])
        readUInt32(utxof, &prevoutIndex)
        //log(fmt.Sprintf("\tprevout.hash = %s", hashToStr(prevoutHash)))
        //log(fmt.Sprintf("\tprevout.n = %d", prevoutIndex))

        // read value (Coin)
        code := readVARINT(utxof)
        //log(fmt.Sprintf("\theight = %d, coinbase = %d",
        //    code >> 1, code & 1))
        _ = code
        amount := decompressAmount(readVARINT(utxof))
        //log(fmt.Sprintf("\tamount = %d sats", amount))
        spkSize := readVARINT(utxof)
        //log(fmt.Sprintf("\tspk_size = %d", spkSize))
        success, scriptPubKey := readCompressedScript(spkSize, utxof)
        if success {
            _, err = tx.Stmt(addUTXOStmt).Exec(prevoutHash[:], prevoutIndex, scriptPubKey, amount)
            if err != nil { panic(err) }
        } else {
            coins_skipped++
        }

        if coin_idx % (1024*1024) == 0 {
            elapsed := time.Since(t)
            fmt.Printf("%d coins read, %d coins skipped, %s passed since start\n",
                coin_idx, coins_skipped, elapsed)
            tx.Commit()
            tx, err = db.Begin()
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
