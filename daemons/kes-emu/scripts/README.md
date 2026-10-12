# scripts — постоянный запуск эмулятора КЭС

Артефакты для запуска `kes-emu` службой systemd.

> **Не для общего деплоя.** Ставится **вручную на конкретный хост** (dev/test).
> В целевой образ, `deploy/`, `stage2.sh` и `deploy/manifest.tsv` не добавляется.

## Состав

| Файл | Назначение |
|------|-----------|
| `kes-emu.service.in` | шаблон unit-файла (`@BINDIR@`, `@ARGS@`, `@PREFIX@`) |
| `install.sh` | сборка пути, установка бинарника и службы, `enable`/`restart` |
| `uninstall.sh` | остановка и удаление службы (`-p` — с бинарником и картой) |

## Размещение

Выбирается автоматически:

| Хост | Каталог |
|------|---------|
| устройство `.60` (новый стек, есть `/malina`) | `/malina` |
| устройство `.68` (legacy, есть `/settings`) | `/settings` |
| остальные | `/opt/kes-emu` |

Туда кладётся бинарник `kes-emu` и карта памяти `kes-emu.json` (`-store <каталог>`).
Переопределение: `install.sh -d <каталог>`.

## Параметры службы

По умолчанию: **3 КЭС, скорость 19200 бит/с, устройства `/dev/ttyUSB9..11`** (везде единообразно):

```
ExecStart=<каталог>/kes-emu -n 3 -baud 19200 -dir /dev -prefix ttyUSB -start 9 -store <каталог>
```

- `-n 3` — число виртуальных КЭС;
- `-baud 19200` — скорость порта;
- `-store <каталог>` — где лежит карта памяти;
- `-dir/-prefix/-start` — имена симлинков устройств.

Имя `ttyUSB*` выбрано как у реального USB-адаптера — эмулятор работает как дроп-ин устройства,
и legacy `tty_scan.sh` (фильтр `/dev/ttyUSB*`) находит его: `usbscanner` пишет тип `D5`
(КЭС) в `devices_found`, затем стартует `mpptd`. Старт с индекса `9` не занимает реальный
`/dev/ttyUSB0`.

Полный набор параметров можно задать через `KES_EMU_ARGS` или дописать после `--`
(эти параметры отменяют host-правило):

```sh
sudo ./install.sh -d /settings -- -n 1 -start 9
```

## Использование

```sh
# собрать бинарник (на armv7 — arm)
make -C .. arm        # daemons/kes-emu/dist/kes-emu-armv7

sudo ./scripts/install.sh
systemctl status kes-emu
journalctl -u kes-emu -f

sudo ./scripts/uninstall.sh          # снять службу
sudo ./scripts/uninstall.sh -p       # + удалить бинарник, карту и /opt/kes-emu
```

`Restart=always` — служба поднимается сама после падения/перезагрузки.

`ExecStartPost` — на legacy `.68` после старта эмулятора пересканировать порты
(`tty_scan.sh` → `usbscanner` → `D5` → `mpptd`), иначе `add_tty` может отработать раньше
появления `/dev/ttyUSB*`. Вызов выполняется только если есть И юнит `add_tty.service`, И `/usr/sbin/tty_scan.sh`
(на `.60` их нет — шаг пропускается, служба не падает и не шумит в journal).
Требуется `root` (создание симлинков в `/dev`, работа с PTY).
