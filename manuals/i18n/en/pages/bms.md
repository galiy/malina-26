# BMS

![BMS page: cells and temperatures](../img/bms/screen.png)

## When to come here

- You need to see the battery state: charge, voltage, current, temperatures, cells.
- You need to check for warnings and protections.
- You need to view the battery parameter history.

The “BMS” section is a permanent menu item. Devices appear in it automatically when a BMS is detected and polling is enabled.

## What you will see

### Tiles on the main page

On the main page — battery tiles: type, mode, charge (SOC), voltage, current, power, and pack temperature, and the “reachable / unreachable” state. Clicking a tile opens that battery's page (address `/bms`).

### Device page

At the top — the device ID and the “Disable update” checkbox. Data refreshes roughly once a second.

On the panel — key indicators (charge, voltage, current, power, cell spread), highlighted cells, temperatures, power switches, and alarms. The maximum cell is marked in red, the minimum in blue. If there are no alarms, it says so. Below — history charts for the selected period.

For some BMS types, per-cell warnings and protections are not transmitted by the protocol — then only balancing and the minimum/maximum cells are visible.

## How to view history

1. Scroll the page to the charts.
2. Choose a period: “24 hours”, “Today”, “Yesterday”, “7 days”, “Month”, “Latest”, or a custom interval.
3. Press “Show” or “Refresh”.

The history may be empty until data accumulates.

## How to check it worked

- The data on the page keeps refreshing.
- The device status is “reachable”, the last success time is fresh.
- There are no unexpected protections among the alarms.

## If it did not work

- **Only “Settings” is in the menu** — no BMS devices detected. Check whether polling is enabled, whether the adapter is connected, and whether the port is not busy with another device. Some BMS devices need a working stock display.
- **“No data / polling disabled”** — enable polling in “BMS settings”.
- **The device is in the status but shows “No connection”** — check the cable, power, and gateway reachability.
- **The page does not refresh or shows an error banner** — check the status on the settings page; if the “Disable update” checkbox is checked, uncheck it.
- **Two batteries look the same** — their IDs match. They can be told apart by status; if necessary, change the settings so the IDs differ.
- **The charts are empty** — history has not accumulated yet or an empty period is selected.

## Related

- [BMS settings](bms-settings.md)
- [Statistics: charts](stats-charts.md)
- [Statistics: tables](stats-tables.md)