#!/usr/bin/env python3
import sqlite3
import time

utxof = open("/home/honey/.bitcoin/utxo.dat", "rb")
blockhash = utxof.read(32)[::-1].hex()
coinscount = int.from_bytes(utxof.read(8), 'little')
print(f"UTXO Snapshot at block {blockhash}, contains {coinscount} coins")

def read_var_int(f):
    n = 0
    while True:
        dat = f.read(1)[0]
        n = (n << 7) | (dat & 0x7f)
        if dat & 0x80:
            n += 1
        else:
            return n

def log(s):
    #print(s)
    pass

t = time.time()

for coin_idx in range(coinscount):
    log(f"Coin {coin_idx+1}/{coinscount}:")
    # read key (COutPoint)
    outpoint = utxof.read(36)
    log(f"\tprevout.hash = {outpoint[:32][::-1].hex()}")
    log(f"\tprevout.n = {int.from_bytes(outpoint[32:], 'little')}")

    # read value (Coin)
    code = read_var_int(utxof)
    log(f"\theight = {code >> 1}, coinbase = {'y' if code & 1 else 'n'}")
    amount = read_var_int(utxof)
    # TODO: decompress amount!
    #log(f"\tamount = {amount} sats")
    log(f"\tamount = ??? sats")
    spk_size = read_var_int(utxof)
    log(f"\tspk_size = {spk_size}")
    if spk_size == 0:
        pkh = utxof.read(20)
        log(f"\t\t pkhash = {pkh.hex()}")
    elif spk_size == 1:
        utxof.read(20)
    elif spk_size == 2:
        utxof.read(32)
    elif spk_size == 3:
        utxof.read(32)
    elif spk_size == 4:
        utxof.read(32)
    elif spk_size == 5:
        utxof.read(32)
    else:
        actual_size = spk_size - 6
        if actual_size > 10000:
            log("TOO LONG SCRIPT!")
        utxof.read(actual_size)

    if coin_idx % 1000000 == 0:
        print(f"{coin_idx} coins read, {time.time()-t} passed since start")

# TODO: validate that we have reached exactly the end of the file
utxof.close()
