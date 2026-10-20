# Malina user guide

Malina is a device for monitoring and controlling power equipment: the MAP, KES, battery packs with BMS, and grid-tie inverters. It is managed through a web admin panel accessible from a phone, tablet, or computer over the network.

The documentation is organized by menu pages. If you do not know where to start, open the section “Where to start”.

![Main page: connected device cards](img/main/home.png)

## Where to start

- [Where to start](start.md) — first power-on, connection, login, and the first mandatory settings.
- [Administrator manual](admin.md) — SSH, access recovery, services, logs, network modes.

## Devices

- [MAP](pages/map.md) — current state, nameplate, and MAP control.
- [MAP settings](pages/map-settings.md) — enabling polling and the log level.
- [KES](pages/kes.md) — panel output, battery charging, relays.
- [KES settings](pages/kes-settings.md) — enabling polling and the log level.
- [BMS](pages/bms.md) — battery state, cells, temperatures, alarms.
- [BMS settings](pages/bms-settings.md) — gateways, BMS receiver, and BMS proxy.
- [Grid-tie inverters](pages/grid.md) — Deye and Sofar: output, faults, charts.
- [Grid-tie inverter settings](pages/grid-settings.md) — the list of inverters, placements, polling period.

## Network and data

- [Network settings](pages/network.md) — access point or connection to an external Wi-Fi, addresses, DHCP, Internet for clients.
- [Statistics: charts](pages/stats-charts.md) — parameter dynamics, comparison, real-time mode.
- [Statistics: tables](pages/stats-tables.md) — exact values and time, paged history browsing.

## System

- [System information](pages/info.md) — device health, load, disks, network, connected devices, and processes.
- [System settings](pages/system.md) — name, timezone, logs, MAP time, statistics, monitoring, password, power.
- [Services](pages/services.md) — service state, start and stop, reboot.
- [Logs](pages/logs.md) — viewing system logs.
- [Email](pages/email.md) — sending notifications by e-mail.
- [Events](pages/events.md) — “condition → action” rules.

## Other

- [Master node](pages/master.md) — overview of several Malina devices.
- [Master node settings](pages/master-settings.md) — the list of nodes.
- [Sign in](pages/login.md) — signing in to the web admin and choosing a language.

## API for developers

- [Native API: lists, raw](api-native.md) — tokens, device lists, raw contracts, writing, and statistics.
- [Legacy API](api-legacy.md) — the whole old API: `read_json.php`, `read_memory.php`, `check_daemons.php`, `read_bms.php`, `read_sec.php`, writing via `write_*`.
- [Backup settings](pages/stats-settings.md)
