#!/usr/bin/env python3
"""Генератор статики для вкладки «Управление» (control-data.js).

Источники:
  - ~/src/mapsettings/catalog.json — названия/единицы/min/max/scale/offset/enum/desc;
  - daemons/map-reader/cells.go    — адрес → имя поля контракта (для чтения значения);
  - легаси-набор (load_menu.php)   — какие группы/параметры показываем и их типы.

Запуск:  python3 web/tools/gen_control.py [-c catalog.json] [-o control-data.js]
"""
import argparse, json, os, re, sys

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

# Легаси-набор: (группа, адрес, легаси-тип, доп-ячейка). Группы: 0 Генерация,
# 1 Сеть1, 2 Сеть2, 3 АКБ, 4 Реле, 5 Прочее. Реле3 (0x120..0x123) — только Титан.
GROUPS = ["Генерация", "Сеть 1", "Сеть 2", "АКБ", "Реле", "Прочее"]
SPEC = [
    (0, 0x138, "list", None), (0, 0x139, "list", None), (0, 0x13A, "ac", None),
    (0, 0x13B, "list", None), (0, 0x13C, "list", None),     (0, 0x13D, "dc_dop", 0x102),
    (0, 0x13E, "list", None),
    (1, 0x150, "list", None), (1, 0x151, "list", None), (1, 0x152, "power_dop", 0x107),
    (1, 0x153, "dc", None), (1, 0x155, "number", None), (1, 0x156, "list", None),
    (1, 0x157, "number", None),
    (2, 0x168, "power_dop", 0x107), (2, 0x169, "ac", None), (2, 0x16A, "ac", None),
    (2, 0x16B, "list", None), (2, 0x16C, "dc_dop", 0x103), (2, 0x16D, "time", None),
    (2, 0x16E, "time", None), (2, 0x16F, "number", None),
    (3, 0x180, "list", None), (3, 0x181, "capacity", None), (3, 0x182, "current_c", None),
    (3, 0x183, "current_c", None), (3, 0x184, "list", None), (3, 0x185, "dc_dop", 0x104),
    (3, 0x186, "dc_dop", 0x105), (3, 0x187, "dc_dop", 0x106),
    (4, 0x198, "relay_funk", None), (4, 0x199, "relay", None), (4, 0x19A, "relay", None),
    (4, 0x19B, "relay", None), (4, 0x19C, "relay_funk", None), (4, 0x19D, "relay", None),
    (4, 0x19E, "relay", None), (4, 0x19F, "relay", None),
    # Реле3 — только Титанатор
    (4, 0x120, "relay_funk", None, "T"), (4, 0x121, "relay", None, "T"),
    (4, 0x122, "relay", None, "T"), (4, 0x123, "relay", None, "T"),
    (5, 0x1B2, "list", None), (5, 0x1B3, "list", None), (5, 0x1B4, "list", None), (5, 0x1B5, "list", None),
    (5, 0x1B6, "time", None), (5, 0x1B7, "charge_time", None),
]


# Enum с полным набором значений (в каталоге бывает только часть).
ENUM_EXTRA = {
    0x13E: {0: "нет включения генерации по нагрузке, МАП всегда производит генерацию от АКБ.",
            1: "генерация включается только при наличии нагрузки (при отсутствии нагрузки генерация выключена)."},
}

# Курированные короткие подписи (<=30 символов) для enum; ключ — адрес.
SHORT = {
    0x13E: {0: "Выключено", 1: "Включено"},
    0x138: {0: "Чистый Sin, THD≤5%", 1: "Оптимальный Sin, THD≤5%", 2: "Pmax, THD≤20%", 3: "Прецизионный Sin, THD≤1%"},
    0x139: {0: "Ведущий, однофазный", 1: "Ведущий / фаза 1", 2: "Фаза 2", 3: "Фаза 3", 4: "Ведомый (параллель)", 255: "Однофазная (нет платы)"},
    0x13B: {0: "Выключена", 1: "Включена (по Pmax)"},
    0x13C: {0: "Нет подкачки (генерация)", 1: "Подкачка от АКБ", 2: "Продажа в сеть", 255: "Недоступно (нет ЭКО)"},
    0x150: {0: "Промышленная сеть", 1: "Топливный генератор", 2: "Пром. топл. генератор"},
    0x151: {0: "Выключен", 1: "Промышленная сеть", 2: "Топливный генератор", 3: "Пром. топл. генератор", 4: "Дублёр входа 1"},
    0x156: {0: "Выключено", 1: "BMS (C)mART", 2: "MPPT (C)mART", 3: "BMS + MPPT mART"},
    0x16B: {0: "Трансляция + заряд", 1: "Только трансляция", 2: "Принуд. генерация ЭКО", 3: "Тарифная сеть"},
    0x180: {0: "Кислотный (авто)", 1: "Гелевый / AGM", 2: "AGM Shoto", 3: "Li 15S (3,4 В/банка)",
            4: "Li 16S (3,4 В/банка)", 5: "LiFePO4 16S (3,6 В)", 6: "LTO 24S (2,7 В)"},
    0x184: {0: "2 ступени, 2 тока", 1: "3 ступени + дозаряд", 2: "3 ступени + буферный", 3: "4 ступени + дозаряд + буфер"},
    0x1B2: {1: "По ситуации"},
    0x1B3: {1: "Только проблемы"},
    0x1B5: {5: "САП-заряд", 6: "Питание Wi-Fi адаптера"},
}

# Функция реле (по доку; 8 — внешнее управление, нужно для доступности реле).
RELAY_FUNK = {
    0: "Выключена", 1: "Пуск дизель-генератора", 2: "По напряжению АКБ", 3: "По температуре АКБ",
    4: "По времени", 5: "Событие сети 220В", 6: "Трансляция сети", 7: "В режиме заряда",
    8: "Внешнее управление", 9: "Нет 220 на выходе", 10: "Избыток мощности MPPT",
    11: "Избыток мощности сети", 12: "По цифровому входу", 13: "По мощности выхода",
}
RELAY_FUNK_SHORT = {
    0: "Выключена", 1: "Пуск ДГУ", 2: "По напр. АКБ", 3: "По темп. АКБ", 4: "По времени",
    5: "Событие сети", 6: "Трансляция сети", 7: "Режим заряда", 8: "Внешнее управление",
    9: "Нет 220 на выходе", 10: "Избыток MPPT", 11: "Избыток сети", 12: "Цифровой вход", 13: "По мощности",
}
RELAY_FUNK_ADDRS = {0x198, 0x19C, 0x120}

# Имена для ячеек, отсутствующих в каталоге.
NAME_EXTRA = {0x1B6: "Текущее время"}


def cells_addr2name():
    path = os.path.join(ROOT, "daemons", "map-reader", "cells.go")
    txt = open(path, encoding="utf-8").read()
    m = {}
    for name, addr in re.findall(r'\{"(\w+)",\s*(0x[0-9A-Fa-f]+),', txt):
        m[int(addr, 16)] = name
    return m


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("-c", "--catalog", default=os.path.expanduser("~/src/mapsettings/catalog.json"))
    ap.add_argument("-o", "--out", default=os.path.join(ROOT, "web", "assets", "static", "control-data.js"))
    a = ap.parse_args()

    cat = json.load(open(a.catalog, encoding="utf-8"))["params"]
    byaddr = {}
    for p in cat:
        byaddr.setdefault(p["addr"], p)
    fid = cells_addr2name()

    groups = {}
    missing = []
    for row in SPEC:
        grp, addr, typ = row[0], row[1], row[2]
        dop = row[3] if len(row) > 3 else None
        modes = row[4] if len(row) > 4 else ""
        c = byaddr.get(addr)
        if c is None:
            missing.append(hex(addr))
        e = {
            "addr": "%X" % addr,
            "fid": fid.get(addr, ""),
            "type": typ,
            "name": (c or {}).get("name", "") or ("0x%X" % addr),
            "unit": (c or {}).get("unit", ""),
            "min": (c or {}).get("min"),
            "max": (c or {}).get("max"),
            "scale": (c or {}).get("scale", 1.0),
            "offset": (c or {}).get("offset", 0.0),
            "enum": (c or {}).get("enum") or None,
            "desc": (c or {}).get("desc", ""),
        }
        if dop is not None:
            e["dop"] = "%X" % dop
        if modes:
            e["modes"] = modes
        if addr in NAME_EXTRA:
            e["name"] = NAME_EXTRA[addr]
        if addr in ENUM_EXTRA:
            e["enum"] = {str(k): v for k, v in ENUM_EXTRA[addr].items()}
        if addr in RELAY_FUNK_ADDRS:
            e["enum"] = {str(k): v for k, v in RELAY_FUNK.items()}
            e["short"] = {str(k): v for k, v in RELAY_FUNK_SHORT.items()}
        if addr in SHORT:
            e["short"] = {str(k): v for k, v in SHORT[addr].items()}
        groups.setdefault(grp, []).append(e)

    out = {
        "groups": [{"name": GROUPS[g], "params": groups[g]} for g in sorted(groups)],
        "addr2fid": {("%X" % a): n for a, n in fid.items()},
        "commands": {
            "on": {"code": "02"}, "off": {"code": "01"}, "reset": {"code": "06"},
            "charge_on": {"code": "05"}, "charge_off": {"code": "04"},
            "relay_cell": "586",
        },
    }
    if missing:
        sys.stderr.write("НЕТ в каталоге: %s\n" % ", ".join(missing))
    js = "window.MAP_CONTROL = " + json.dumps(out, ensure_ascii=False, separators=(",", ":")) + ";\n"
    open(a.out, "w", encoding="utf-8").write(js)
    n = sum(len(g["params"]) for g in out["groups"])
    print("control-data.js: групп=%d, параметров=%d, нет в каталоге=%d" % (len(out["groups"]), n, len(missing)))


if __name__ == "__main__":
    main()
