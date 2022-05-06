package main

import (
    "crypto/sha256"
    "database/sql"
    "encoding/binary"
    "encoding/hex"
    "fmt"
    _ "github.com/mattn/go-sqlite3"
    "golang.org/x/crypto/chacha20"
    "math/big"
    "time"
)

const verbose bool = false;
var num3072_prime = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 3072), big.NewInt(1103717))

func hash256SwapEndianness(hash256 []byte) {
    for i, j := 0, 31; i < j; i, j = i+1, j-1 {
        hash256[i], hash256[j] = hash256[j], hash256[i]
    }
}

func num3072SwapEndianness(num3072 []byte) {
    for i, j := 0, 383; i < j; i, j = i+1, j-1 {
        num3072[i], num3072[j] = num3072[j], num3072[i]
    }
}

func hashToStr(bytes [32]byte) (string) {
    hash256SwapEndianness(bytes[:])
    return fmt.Sprintf("%x", bytes)
}

func serializeTransaction(txid []byte, vout uint32,
                          value uint64, coinbase uint32, height uint32,
                          scriptpubkey []byte) []byte {
    ser := make([]byte, 0, 128)
    tmp4 := make([]byte, 4)
    tmp8 := make([]byte, 8)

    ser = append(ser, txid...)
    binary.LittleEndian.PutUint32(tmp4, vout)
    ser = append(ser, tmp4...)
    binary.LittleEndian.PutUint32(tmp4, 2*height + coinbase)
    ser = append(ser, tmp4...)
    binary.LittleEndian.PutUint64(tmp8, value)
    ser = append(ser, tmp8...)

    if len(scriptpubkey) < 253 {
        ser = append(ser, byte(len(scriptpubkey)))
    } else if len(scriptpubkey) <= 10000 {
        tmp2 := make([]byte, 2)
        binary.LittleEndian.PutUint16(tmp2, uint16(len(scriptpubkey)))
        ser = append(ser, 253)
        ser = append(ser, tmp2...)
    } else {
        panic(fmt.Sprintf("scriptPubKey too long (%d > 10000)!", len(scriptpubkey)))
    }

    ser = append(ser, scriptpubkey...)

    return ser
}

func main() {
    db, err := sql.Open("sqlite3", "file:/home/honeybadger/.bitcoin/utxo.sqlite")
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
        hash256SwapEndianness(txid)
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
        //fmt.Printf("SHA256 of the serialized UTXO: %x\n", txser_hash)
        cc20, err := chacha20.NewUnauthenticatedCipher(txser_hash[:], make([]byte, 12))
        if err != nil { panic(err) }
        var num3072_raw [384]byte
        cc20.XORKeyStream(num3072_raw[:], num3072_raw[:])
        //fmt.Printf("Chacha20 of SHA256 of the serialized UTXO: %x\n", num3072_raw)

        num3072SwapEndianness(num3072_raw[:])
        num3072_insert := new(big.Int).SetBytes(num3072_raw[:])
        num3072_insert.Mod(num3072_insert, num3072_prime)

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
    num3072SwapEndianness(result[:])
    muhash_final := sha256.Sum256(result[:])
    fmt.Printf("MuHash: %s\n", hashToStr(muhash_final))
}
