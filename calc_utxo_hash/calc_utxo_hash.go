package main

import (
    "crypto/sha256"
    "database/sql"
    "encoding/binary"
    "encoding/hex"
    "flag"
    "fmt"
    _ "github.com/mattn/go-sqlite3"
    "golang.org/x/crypto/chacha20"
    "math/big"
    "os"
    "time"
)

var verbose bool = false
var num3072_prime = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 3072), big.NewInt(1103717))

func swapBytes(bytes []byte) {
    for i, j := 0, len(bytes)-1; i < j; i, j = i+1, j-1 {
        bytes[i], bytes[j] = bytes[j], bytes[i]
    }
}

func hashToStr(bytes [32]byte) (string) {
    swapBytes(bytes[:])
    return fmt.Sprintf("%x", bytes)
}

func serializeTransaction(txid []byte, vout uint32,
                          value uint64, coinbase uint32, height uint32,
                          scriptpubkey []byte) []byte {
    ser := make([]byte, 0, 128)
    var tmp [8]byte

    ser = append(ser, txid...)
    binary.LittleEndian.PutUint32(tmp[:4], vout)
    ser = append(ser, tmp[:4]...)
    binary.LittleEndian.PutUint32(tmp[:4], 2*height + coinbase)
    ser = append(ser, tmp[:4]...)
    binary.LittleEndian.PutUint64(tmp[:8], value)
    ser = append(ser, tmp[:8]...)

    if len(scriptpubkey) < 253 {
        ser = append(ser, byte(len(scriptpubkey)))
    } else if len(scriptpubkey) <= 10000 {
        binary.LittleEndian.PutUint16(tmp[:2], uint16(len(scriptpubkey)))
        ser = append(ser, 253)
        ser = append(ser, tmp[:2]...)
    } else {
        panic(fmt.Sprintf("scriptPubKey too long (%d > 10000)!", len(scriptpubkey)))
    }
    ser = append(ser, scriptpubkey...)

    return ser
}

func main() {
    flag.BoolVar(&verbose, "v", false, "show verbose output for each UTXO")
    flag.Usage = func() {
        w := flag.CommandLine.Output()
        fmt.Fprintf(w, "Usage: go run calc_utxo_hash.go [-v] UTXOFILE\n\n")
        fmt.Fprintf(w, "Calculate MuHash for a UTXOFILE in SQLite3 format\n\n")
        fmt.Fprintf(w, "\t")
        flag.PrintDefaults()
    }
    flag.Parse()
    if len(flag.Args()) != 1 {
        flag.Usage()
        os.Exit(1)
    }
    inputFilename := flag.Args()[0]

    db, err := sql.Open("sqlite3", "file:" + inputFilename)
    if err != nil { panic(err) }
    defer db.Close()

    // TODO: read metadata
    //fmt.Printf("UTXO Snapshot at block %s, contains %d coins\n",
    //           hashToStr(blockHash), numUTXOs)

    rows, err := db.Query("SELECT * FROM utxos")
    if err != nil { panic(err) }
    defer rows.Close()

    t := time.Now()

    num3072 := big.NewInt(1)
    coin_idx := uint64(0)

    for rows.Next() {
        var txid_hex string
        var vout uint32
        var value uint64
        var coinbase uint32
        var height uint32
        var scriptpubkey_hex string

        err = rows.Scan(&txid_hex, &vout, &value, &coinbase, &height, &scriptpubkey_hex)
        if err != nil { panic(err) }
        coin_idx++

        txid, err := hex.DecodeString(txid_hex)
        if err != nil { panic(err) }
        swapBytes(txid)
        scriptpubkey, err := hex.DecodeString(scriptpubkey_hex)
        if err != nil { panic(err) }

        if verbose {
            fmt.Printf("\ttxid = %x\n", txid)
            fmt.Printf("\tvout = %d\n", vout)
            fmt.Printf("\tvalue = %d sats\n", value)
            fmt.Printf("\tcoinbase = %d\n", coinbase)
            fmt.Printf("\theight = %d\n", height)
            fmt.Printf("\tscriptPubKey = %x\n", scriptpubkey)
            fmt.Printf("\n")
        }

        txser := serializeTransaction(txid, vout, value, coinbase, height, scriptpubkey)
        txser_hash := sha256.Sum256(txser)
        cc20, err := chacha20.NewUnauthenticatedCipher(txser_hash[:], make([]byte, 12))
        if err != nil { panic(err) }
        var num3072_raw [384]byte
        cc20.XORKeyStream(num3072_raw[:], num3072_raw[:])

        swapBytes(num3072_raw[:])
        num3072_insert := new(big.Int).SetBytes(num3072_raw[:])
        num3072.Mul(num3072, num3072_insert)
        num3072.Mod(num3072, num3072_prime)

        if coin_idx % (512*1024) == 0 {
            elapsed := time.Since(t)
            fmt.Printf("%d coins read, %s passed since start\n", coin_idx, elapsed)
        }
    }

    // Finalize MuHash
    var result [384]byte
    num3072.FillBytes(result[:])
    swapBytes(result[:])
    muhash_final := sha256.Sum256(result[:])
    fmt.Printf("MuHash: %s\n", hashToStr(muhash_final))
}
