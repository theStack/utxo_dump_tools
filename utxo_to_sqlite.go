package main

import (
    "bufio"
    "encoding/binary"
    "fmt"
    "io"
    "os"
    "time"
)

func log(str string) {
    // fmt.Println(str)
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

func main() {
    utxof2, err := os.OpenFile("/home/honey/.bitcoin/utxo.dat", os.O_RDONLY, 0600)
    if err != nil {
        return
    }
    utxof := bufio.NewReader(utxof2)

    // read metadata
    var blockHash [32]byte
    var numUTXOs uint64
    _, err = io.ReadFull(utxof, blockHash[:])
    err = binary.Read(utxof, binary.LittleEndian, &numUTXOs)
    fmt.Printf("UTXO Snapshot at block %s, contains %d coins\n",
               hashToStr(blockHash), numUTXOs)

    t := time.Now()

    // read in coins
    for coin_idx := uint64(1); coin_idx <= numUTXOs; coin_idx++ {
        //log(fmt.Sprintf("Coin %d/%d:", coin_idx, numUTXOs))

        // read key (COutPoint)
        var prevoutHash [32]byte
        var prevoutIndex uint32
        _, err = io.ReadFull(utxof, prevoutHash[:])
        err = binary.Read(utxof, binary.LittleEndian, &prevoutIndex)
        //log(fmt.Sprintf("\tprevout.hash = %s", hashToStr(prevoutHash)))
        //log(fmt.Sprintf("\tprevout.n = %d", prevoutIndex))

        // read value (Coin)
        code := readVARINT(utxof)
        //log(fmt.Sprintf("\theight = %d, coinbase = %d",
        //    code >> 1, code & 1))
        _ = code
        amount := decompressAmount(readVARINT(utxof))
        //log(fmt.Sprintf("\tamount = %d sats", amount))
        _ = amount
        spkSize := readVARINT(utxof)
        //log(fmt.Sprintf("\tspk_size = %d", spkSize))
        var actualSize uint64
        switch spkSize {
        case 0, 1:
            actualSize = 20
        case 2, 3, 4, 5:
            actualSize = 32                 
        default:
            actualSize = spkSize - 6       
            if actualSize > 10000 {
                panic(fmt.Sprintf("too long script with size %d\n", actualSize))
            }
        }
        buf := make([]byte, actualSize)
        _, err = io.ReadFull(utxof, buf[:])

        if coin_idx % 1000000 == 0 {
            elapsed := time.Since(t)
            fmt.Printf("%d coins read, %s passed since start\n",
                coin_idx, elapsed)
        }
    }

    // check for EOF (read must fail)
    _, err = utxof.ReadByte()
    if err != nil {
        fmt.Println("EOF reached.")
    } else {
        fmt.Println("WARNING: File is not at EOF yet!")
    }
}
