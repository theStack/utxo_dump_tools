# `utxo_to_sqlite`

`utxo_to_sqlite` is a simple tool for converting a compact-serialized UTXO set generated
by Bitcoin Core (via the `dumptxoutset` RPC) to a SQLite database.

Run via:
```
$ git clone https://github.com/theStack/utxo_to_sqlite.git
$ cd utxo_to_sqlite
$ go run utxo_to_sqlite.go
```

Note that the first run likely takes longer, as golang has to fetch and build the SQLite library
(https://github.com/mattn/go-sqlite3) first.

## TODOs
- support specifying input filename as parameter (right now it is fixed to `~/.bitcoin/utxo.dat`)
- support specifying output filename as parameter (right now it is fixed to `./utxos.sqlite3`)
- support decompressing and writing P2PK outputs with uncompressed pubkeys (right now they are skipped)
- ...
