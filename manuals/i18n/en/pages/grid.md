# Grid-tie inverters

![Grid-tie inverter page](../img/grid/screen.png)

## When to come here

- You need to see a grid-tie inverter's output: solar panels, grid, phases.
- You need to check for faults.
- You need to view the inverter's parameter history.

The “Grid” section works with Deye and Sofar inverters connected over the local network. The list of inverters and their pages appear in the menu, and tiles on the main page.

## What you will see

### Tiles on the main page

The tiles are grouped by location. On each one — name, type, and serial number, with the active power and total panel power shown large. Below: panel voltages and currents, reactive power, frequency, phase voltages and currents, temperatures, output today and in total.

If an inverter has a fault, an exclamation mark appears in the top left. If the inverter is unreachable, the tile is dimmed and shows the time of the last successful poll.

### Inverter page

It opens by clicking a tile or from the menu. Data refreshes once a second. There is a “Disable update” checkbox.

Sections:

- **PV** — panel voltage, current, and power.
- **A.C.** — active and reactive power, frequency, phase voltages and currents.
- **Temperatures** — two sensors. If a sensor is not connected, a dash is shown.
- **Condition and accidents** — the inverter status, alarm words, and the table of active faults with decoding.
- **Energy** — output today and in total.
- **Charts** — voltages, currents, powers, frequency, and temperatures.

Inverter states: “standby”, “self-check”, “normal”, “fault”, “permanent fault”.

## How to view history

1. Scroll the page to the charts.
2. Choose a period.
3. Press “Show” or “Refresh”.

History appears only after successful polls. There are no charts for energy, statuses, or faults.

## How to check it worked

- The tile on the main page shows fresh values, the connection status is “OK”, the inverter operating state is “normal”.
- On the inverter page the data refreshes and the last success time is fresh.
- There are no unexpected codes in the faults table.

## If it did not work

- **The inverter is not in the menu or on the main page** — check the settings: whether polling is enabled, whether the inverter is added, and whether the logger SN is filled in.
- **No data, status “not polled”** — after the first full poll the factory serial number needs to be read. Wait for a poll cycle.
- **“No connection”** — the inverter is off, asleep at night, unreachable over the network, or its IP has changed. Check the power and the address stability.
- **“Bad response”** — the inverter answered with an incomplete frame; the poll retries on its own. If it keeps repeating, check the inverter type and network.
- **“Ban 1/5/10 min”** — a temporary restriction after a series of failures. Usually no action is needed; polling will resume on its own.
- **A dash instead of temperature** — this model has no such sensor; this is not an error.
- **No charts** — there have been no successful polls for the inverter in the selected period yet.

## Related

- [Grid-tie inverter settings](grid-settings.md)
- [Statistics: charts](stats-charts.md)
- [Statistics: tables](stats-tables.md)