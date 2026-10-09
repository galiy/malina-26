#!/usr/bin/env python3
"""Эмулятор МАП (Микроарт) на PTY для smoketest демона map-reader.

Отдаёт синтетическую память 0x000..0x7FF: Титанатор, serial 51510, любую
запрошенную страницу. Только чтение.
"""
import os, select, sys, time, errno

LINK = "/tmp/kilo/fakemap"


def build_mem():
    m = bytearray(0x800)
    m[0x00] = 0x03        # _Device
    m[0x01] = 0x82        # _VerPow (гибрид)
    m[0x02] = 0x84        # _VerPO
    m[0x05] = 0x09        # _POW
    m[0x06] = 0x02        # _UACC
    m[0x07] = 0x03        # _DevOpt = 3 (Титанатор)
    m[0x18] = 0x36
    m[0x19] = 0xC9        # serial 0xC936 = 51510
    m[0x23] = ord('a')
    # немного «телеметрии»
    m[0x405] = 0x0E
    m[0x406] = 0x1A        # UAcc ~ ... (сырое)
    m[0x42A] = 0x00
    m[0x718] = 0x37
    return m


MEM = build_mem()


def esc(b):
    if b == 0x0A:
        return bytes([0xDB, 0xDC])
    if b == 0xDB:
        return bytes([0xDB, 0xDD])
    return bytes([b])


def response(addr, length):
    data = MEM[addr:addr + length]
    out = bytearray(b'o')
    s = 0x6F
    for b in data:
        e = esc(b)
        out += e
        s = (s + sum(e)) & 0xFF
    ck = (0x100 - s) & 0xFF
    out.append(ck)
    if ck != 0x0A:
        out.append(0x0A)
    return bytes(out)


def main():
    master, slave = os.openpty()
    sname = os.ttyname(slave)
    os.close(slave)
    try:
        os.unlink(LINK)
    except FileNotFoundError:
        pass
    os.symlink(sname, LINK)
    sys.stderr.write("fakemap at %s -> %s\n" % (LINK, sname))
    sys.stderr.flush()
    req = bytearray()
    while True:
        r, _, _ = select.select([master], [], [], 1.0)
        if not r:
            continue
        try:
            b = os.read(master, 1)
        except OSError as e:
            if e.errno in (errno.EIO, errno.EAGAIN):
                req = bytearray()
                time.sleep(0.01)
                continue
            break
        if not b:
            continue
        os.write(master, b)  # эхо
        req += b
        if req and req[0] == ord('r') and req[-1] == 0x0A:
            page = req[1]
            addr = (req[2] << 8) | req[3]
            resp = response(addr, page + 1)
            os.write(master, resp)
            req = bytearray()
            # вычитать ровно эхо ответа (демон возвращает каждый байт),
            # не съедая следующий запрос
            got = 0
            t = time.time()
            while got < len(resp) and time.time() - t < 0.5:
                r, _, _ = select.select([master], [], [], 0.02)
                if r:
                    try:
                        got += len(os.read(master, len(resp) - got))
                    except OSError:
                        break


if __name__ == "__main__":
    main()
