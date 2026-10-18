#!/usr/bin/env python3
"""Генерация grid-reader.json из конфига sunReceiver.

Приватные данные (IP, logger_sn) остаются в файле назначения; в git его не класть.
По умолчанию размещения: Deye Left/Deye Right -> «Гараж», остальные -> «Дом».
"""
import argparse, json, sys

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("source", help="sunReceiver.json")
    ap.add_argument("out", help="grid-reader.json")
    ap.add_argument("--enabled", action="store_true", help="включить опрос")
    ap.add_argument("--period", type=int, default=10, choices=[10, 20, 30])
    a = ap.parse_args()
    d = json.load(open(a.source, encoding="utf-8"))
    invs = []
    for x in d.get("invertors", []):
        if x.get("disabled"):
            continue
        name = x.get("name") or x.get("ip")
        place = "Гараж" if name in ("Deye Left", "Deye Right") else "Дом"
        invs.append({
            "name": name,
            "ip": x["ip"],
            "port": 8899,
            "type": x["type"],
            "placement": place,
            "logger_sn": int(x["logger_sn"]),
        })
    cfg = {
        "enabled": bool(a.enabled),
        "log_level": "info",
        "poll_period": a.period,
        "placements": ["Дом", "Гараж"],
        "inverters": invs,
    }
    with open(a.out, "w", encoding="utf-8") as f:
        json.dump(cfg, f, ensure_ascii=False, indent=2)
        f.write("\n")
    print(f"{a.out}: {len(invs)} инверторов")

if __name__ == "__main__":
    main()
