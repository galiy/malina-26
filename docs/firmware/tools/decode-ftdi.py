#!/usr/bin/env python3
"""Разбор USB-дампа (usbmon) обмена с FTDI FT232R → артефакты этапа.

Вход: каталог с usb.pcapng и meta.json.
Выход в том же каталоге:
  timeline.md   — протокол по меткам времени (MSK): CTRL-запросы + пачки TX/RX
  serial-tx.bin — восстановленный поток host→МАП
  serial-rx.bin — восстановленный поток МАП→host
  serial-tx.hex / serial-rx.hex — hex-дампы
  summary.md    — сводка (объёмы, длительность, паузы, CTRL-события)

Принцип: ничего не открывает и не пишет в порт; только читает pcapng.
"""
import json
import os
import re
import subprocess
import sys
import datetime

MSK = datetime.timezone(datetime.timedelta(hours=3))

FIELDS = [
    "frame.number",
    "frame.time_epoch",
    "usb.device_address",
    "usb.endpoint_address",
    "usb.transfer_type",
    "usb.urb_type",
    "usb.urb_status",
    "usb.bmRequestType",
    "usb.setup.bRequest",
    "ftdi-ft.bRequest",
    "ftdi-ft.lValue",
    "ftdi-ft.hValue",
    "ftdi-ft.lIndex",
    "ftdi-ft.hIndex",
    "ftdi-ft.wLength",
    "usb.setup.wValue",
    "usb.setup.wIndex",
    "usb.setup.wLength",
    "ftdi-ft.if_a_tx_payload",
    "ftdi-ft.if_a_rx_payload",
    "ftdi-ft.latency_time",
    "usb.capdata",
    "usb.data_fragment",
]

REQ = {
    0x00: "RESET",
    0x01: "MODEM_CTRL",
    0x02: "SET_FLOW_CTRL",
    0x03: "SET_BAUD",
    0x04: "SET_DATA",
    0x05: "GET_MODEM_STATUS",
    0x06: "SET_CHARS",
    0x07: "SET_BITMODE",
    0x08: "GET_PINS",
    0x09: "SET_LATENCY",
    0x0A: "GET_LATENCY",
    0x0B: "SET_BITMODE_FT232R",
}


def hexbytes(h, sep=""):
    return bytes.fromhex(sep.join(re.findall(r"[0-9A-Fa-f]{2}", h or "")))


def ascii_of(b):
    return "".join(chr(c) if 32 <= c < 127 else "." for c in b)


def fmt_data(b, limit=96):
    if not b:
        return ""
    head = b[:limit]
    s = " ".join(f"{x:02X}" for x in head)
    if len(b) > limit:
        s += f" … (+{len(b) - limit} Б)"
    asc = ascii_of(head)
    return f"`{s}`<br>`{asc}`"


def decode_ctrl(req, wvalue, windex, wlength, latency):
    if req is None:
        return ""
    name = REQ.get(req, f"REQ_0x{req:02X}")
    if req == 0x03:  # SET_BAUD: FT232R — целая часть в wValue[13:0],
        # дробная (3 бита) в wValue[15:14], старшие биты — в wIndex.
        v = wvalue or 0
        # FT232R: 14-битная целая часть + 3 дробных бита (шаг 1/8); третий
        # дробный бит лежит в бите 0 wIndex. Старшие биты wIndex — не делитель.
        frac = ((v >> 14) & 0x3) | ((windex or 0) & 0x1) << 2
        div = (v & 0x3FFF) + frac / 8
        baud = round(3_000_000 / div) if div else 0
        note = ""
        if abs(div - 4.75) < 1e-9:
            note = "; номинал 625000 (вендор: _RSBAUD_Rasb_Load=0x64)"
        idx = f", wIndex=0x{windex:04X}" if windex else ""
        return f"{name}: ~{baud} бод (divisor={div:g}, wValue=0x{v:04X}{idx}){note}"
    if req == 0x09:  # SET_LATENCY
        return f"{name}: latency_timer={wvalue} мс"
    if req == 0x04:  # SET_DATA
        v = wvalue or 0
        bits = v & 0xFF
        par = (v >> 8) & 0x7
        stop = (v >> 11) & 0x1
        brk = (v >> 14) & 0x1
        parn = {0: "none", 1: "odd", 2: "even", 3: "mark", 4: "space"}.get(par, str(par))
        return (f"{name}: {bits} бит, parity={parn}, stop={2 if stop else 1}, break={brk}"
                f" (wValue=0x{v:04X})")
    if req == 0x02:
        return f"{name}: wValue=0x{(wvalue or 0):04X} wIndex=0x{(windex or 0):04X}"
    if req == 0x01:
        return f"{name}: wValue=0x{(wvalue or 0):04X} (DTR/RTS/break)"
    if req in (0x05, 0x08, 0x0A):
        return f"{name} → {wlength} Б"
    if latency is not None:
        return f"{name}: latency={latency} мс"
    return f"{name}: wValue=0x{(wvalue or 0):04X} wIndex=0x{(windex or 0):04X} len={wlength}"


def run_tshark(pcap):
    cmd = ["tshark", "-r", pcap, "-n", "-T", "fields",
           "-E", "separator=\t", "-E", "occurrence=a"] 
    for f in FIELDS:
        cmd += ["-e", f]
    out = subprocess.run(cmd, capture_output=True)
    if out.returncode != 0:
        sys.stderr.write(out.stderr.decode("utf-8", "replace"))
        raise SystemExit("tshark не смог разобрать pcapng")
    return out.stdout.decode("utf-8", "replace").splitlines()


def main():
    d = sys.argv[1] if len(sys.argv) > 1 else "."
    meta_path = os.path.join(d, "meta.json")
    meta = json.load(open(meta_path)) if os.path.exists(meta_path) else {}
    bus = meta.get("usb", {}).get("bus")
    dev = meta.get("usb", {}).get("devnum")
    pcap = os.path.join(d, "usb.pcapng")
    if not os.path.exists(pcap):
        raise SystemExit(f"нет {pcap}")

    events = []  # (epoch, kind, direction, payload, note)
    seen_addrs = set()
    tx = bytearray()
    rx = bytearray()

    for line in run_tshark(pcap):
        f = line.split("\t")
        f += [""] * (len(FIELDS) - len(f))

        def get(name):
            return f[FIELDS.index(name)] or None

        try:
            addr = int(get("usb.device_address") or -1)
        except ValueError:
            addr = -1
        try:
            epoch = float(get("frame.time_epoch") or 0)
        except ValueError:
            epoch = 0.0
        ep = get("usb.endpoint_address")
        try:
            epn = int(ep, 16) if ep else None
        except ValueError:
            epn = None
        ttype = get("usb.transfer_type")
        def num(name, base=0):
            v = get(name)
            try:
                return int(v, base) if v else 0
            except ValueError:
                return 0

        # для vendor-запросов FTDI стандартные поля setup не заполняются:
        # значения берём из ftdi-ft (l/h — байты wValue, lIndex/hIndex — wIndex)
        req_s = get("usb.setup.bRequest") or get("ftdi-ft.bRequest")
        req = int(req_s) if req_s and req_s.isdigit() else None
        bm_s = get("usb.bmRequestType")
        bm = int(bm_s, 0) if bm_s else None
        wvalue = num("usb.setup.wValue") or (num("ftdi-ft.lValue") | (num("ftdi-ft.hValue") << 8))
        windex = num("usb.setup.wIndex") or (num("ftdi-ft.lIndex") | (num("ftdi-ft.hIndex") << 8))
        wlength = num("usb.setup.wLength") or num("ftdi-ft.wLength")
        lat_s = get("ftdi-ft.latency_time")
        lat = int(lat_s) if lat_s and lat_s.isdigit() else None

        p_tx = "".join(f[FIELDS.index("ftdi-ft.if_a_tx_payload")].split("\t")) if get("ftdi-ft.if_a_tx_payload") else ""
        p_rx = "".join(f[FIELDS.index("ftdi-ft.if_a_rx_payload")].split("\t")) if get("ftdi-ft.if_a_rx_payload") else ""
        cap = get("usb.capdata") or get("usb.data_fragment")

        # CTRL-запрос (setup) — только один раз на транзакцию (S-пакет)
        vendor = bm is not None and ((bm >> 5) & 0x3) == 0x2
        # FTDI-запрос опознаём по диссектору (ftdi-ft.bRequest) или по адресу
        # устройства на момент старта; адрес может смениться при ре-энумерации.
        is_ftdi = bool(get("ftdi-ft.bRequest")) or dev is None or addr in (-1, int(dev))
        if ttype == "0x02" and req is not None and vendor and is_ftdi:
            cdir = "←" if (bm is not None and (bm & 0x80)) else "→"
            events.append((epoch, "ctrl", cdir, b"", decode_ctrl(req, wvalue, windex, wlength, lat)))
            continue

        payload = b""
        direction = None
        note = ""
        if p_tx:
            payload, direction, note = hexbytes(p_tx), "TX", ""
        elif p_rx:
            payload, direction, note = hexbytes(p_rx), "RX", ""
        elif ttype == "0x03" and cap and epn is not None:
            raw = hexbytes(cap)
            if epn & 0x80:  # IN: 2 байта modem/line status в конце пакета FTDI
                if len(raw) > 2:
                    payload, note = raw[:-2], "(raw IN, status срезан)"
                else:
                    continue
                direction = "RX"
            else:
                payload, direction, note = raw, "TX", "(raw)"
        if not payload:
            continue

        seen_addrs.add(addr)
        if direction == "TX":
            tx += payload
        else:
            rx += payload
        events.append((epoch, "bulk", "→" if direction == "TX" else "←", payload, note))

    # --- артефакты -------------------------------------------------------------
    open(os.path.join(d, "serial-tx.bin"), "wb").write(tx)
    open(os.path.join(d, "serial-rx.bin"), "wb").write(rx)
    for name, data in (("serial-tx", tx), ("serial-rx", rx)):
        with open(os.path.join(d, f"{name}.hex"), "w") as fh:
            for i in range(0, len(data), 16):
                chunk = data[i:i + 16]
                hx = " ".join(f"{c:02x}" for c in chunk)
                fh.write(f"{i:08x}  {hx:<47}  {ascii_of(chunk)}\n")

    t0 = events[0][0] if events else 0.0
    with open(os.path.join(d, "timeline.md"), "w") as fh:
        fh.write(f"# Протокол этапа: {meta.get('stage', '?')}\n\n")
        if meta.get("note"):
            fh.write(f"Примечание оператора: {meta['note']}\n\n")
        fh.write(f"- Старт (MSK): {meta.get('started_msk', '?')}\n")
        fh.write(f"- USB: bus {bus}, dev {dev}, serial {meta.get('usb', {}).get('serial', '?')}\n")
        fw = meta.get("firmware", {})
        fh.write(f"- Прошивка: `{os.path.basename(fw.get('path', '?'))}`, SHA-256 `{fw.get('sha256', '')}`\n\n")
        fh.write("| Время MSK | Δ, мс | Напр. | Тип | Длина | Данные / событие |\n")
        fh.write("|---|---|---|---|---|---|\n")
        for epoch, kind, dirn, payload, note in events:
            ts = datetime.datetime.fromtimestamp(epoch, MSK).strftime("%H:%M:%S.%f")[:-3]
            dt = (epoch - t0) * 1000 if t0 else 0
            if kind == "ctrl":
                fh.write(f"| {ts} | {dt:.3f} | {dirn} | CTRL | – | {note} |\n")
            else:
                data = fmt_data(payload)
                if note:
                    data += f" {note}"
                fh.write(f"| {ts} | {dt:.3f} | {dirn} | BULK | {len(payload)} | {data} |\n")

    # паузы > 50 мс
    gaps = []
    for (e1, *_), (e2, *_) in zip(events, events[1:]):
        if e2 - e1 >= 0.05:
            gaps.append((e1 - t0, (e2 - e1) * 1000))
    with open(os.path.join(d, "summary.md"), "w") as fh:
        fh.write(f"# Сводка: {meta.get('stage', '?')}\n\n")
        fh.write(f"- Старт: {meta.get('started_local', '?')}\n- Конец: {meta.get('stopped_local', '?')}\n")
        fh.write(f"- Событий: {len(events)}\n")
        fh.write(f"- TX (host→МАП): {len(tx)} Б\n- RX (МАП→host): {len(rx)} Б\n")
        if events:
            fh.write(f"- Длительность по событиям: {events[-1][0] - t0:.3f} с\n")
        ctrl = [e for e in events if e[1] == "ctrl"]
        if ctrl:
            fh.write("\n## CTRL-события\n\n| Время MSK | Событие |\n|---|---|\n")
            for epoch, _, _, _, note in ctrl:
                ts = datetime.datetime.fromtimestamp(epoch, MSK).strftime("%H:%M:%S.%f")[:-3]
                fh.write(f"| {ts} | {note} |\n")
        if gaps:
            fh.write(f"\n## Паузы ≥ 50 мс ({len(gaps)})\n\n| После, с | Пауза, мс |\n|---|---|\n")
            for at, ms in gaps[:200]:
                fh.write(f"| {at:.3f} | {ms:.1f} |\n")
        fh.write("\n## Начало TX (первые 40 байт)\n\n")
        fh.write(fmt_data(tx[:40]) + "\n")

    print(f"TX {len(tx)} Б, RX {len(rx)} Б, событий {len(events)}")
    if seen_addrs:
        print(f"адреса устройств с данными: {sorted(seen_addrs)}")
    print(f"артефакты: {d}")


if __name__ == "__main__":
    main()
