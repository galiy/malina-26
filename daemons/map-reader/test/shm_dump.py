#!/usr/bin/env python3
"""Контроль map-reader через shm (без правок демона).

Читает дамп /dev/shm/malina-map-state и печатает значения ячеек по адресам.
Порядок ячеек берётся из cells.go (тот же, что в контракте: id = порядок таблицы).

Использование:
    shm_dump.py [dump.bin] [addr ...]
    # dump.bin по умолчанию /dev/shm/malina-map-state (нужен доступ на чтение)
    # без addr — печатает все ненулевые ячейки

Пример:
    sudo cat /dev/shm/malina-map-state > /tmp/state.bin
    python3 test/shm_dump.py /tmp/state.bin 0x400 0x403 0x1B6 0x44B
"""
import os
import re
import struct
import sys

HEADER = 64
LIST_ITEM = 72
SLOTS = 9
SLOT_HDR = 32
SLOT_SIZE = 32 + 16384
CELLS = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "cells.go")


def load_cells(path):
    txt = open(path, encoding="utf-8").read()
    cells = []
    for m in re.finditer(r'\{"(\w+)",\s*(0x[0-9A-Fa-f]+),\s*(\d+)\}', txt):
        cells.append((m.group(1), int(m.group(2), 16), int(m.group(3))))
    return cells


def u16(b, o):
    return struct.unpack_from("<H", b, o)[0]


def u32(b, o):
    return struct.unpack_from("<I", b, o)[0]


def u64(b, o):
    return struct.unpack_from("<Q", b, o)[0]


def main():
    path = sys.argv[1] if len(sys.argv) > 1 and sys.argv[1].endswith(".bin") else "/dev/shm/malina-map-state"
    args = sys.argv[2:] if path == sys.argv[1] else sys.argv[1:]
    want = [int(a, 0) for a in args]
    dump = open(path, "rb").read()
    cells = load_cells(CELLS)
    addr2idx = {c[1]: i for i, c in enumerate(cells)}

    base_slots = HEADER + SLOTS * LIST_ITEM
    for i in range(SLOTS):
        lo = HEADER + i * LIST_ITEM
        if u32(dump, lo) != 1:
            continue
        port = dump[lo + 32:lo + 64].split(b"\0")[0].decode()
        model = chr(dump[lo + 4])
        gen = u64(dump, lo + 16)
        pl = base_slots + i * SLOT_SIZE + SLOT_HDR
        nfields = u16(dump, pl + 6)
        presence = dump[pl + HEADER: pl + HEADER + (nfields + 7) // 8]
        voff = pl + HEADER + (nfields + 7) // 8

        def present(idx):
            return presence[idx // 8] & (1 << (idx % 8)) != 0

        print(f"slot={i} port={port} model={model} gen={gen} nfields={nfields}")
        if want:
            for a in want:
                idx = addr2idx.get(a)
                if idx is None:
                    print(f"  0x{a:03X} ? (нет в таблице)")
                    continue
                tag, ad, size = cells[idx]
                if present(idx):
                    v = u32(dump, voff + idx * 4)
                    print(f"  0x{ad:03X} {tag} size={size} = {v} (0x{v:0{size * 2}X})")
                else:
                    print(f"  0x{ad:03X} {tag} (нет данных)")
        else:
            for idx, (tag, ad, size) in enumerate(cells):
                if present(idx) and u32(dump, voff + idx * 4):
                    print(f"  0x{ad:03X} {tag} = {u32(dump, voff + idx * 4)}")


if __name__ == "__main__":
    main()
