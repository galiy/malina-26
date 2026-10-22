# MAP

![MAP page: panel](../img/map/screen.png)

## When to come here

- You need to see what is happening with the MAP right now: mode, grid, load, battery, temperatures.
- You need to change parameters or issue a command: turn the MAP on/off, charge, relay, perform a reset.
- You need to check the device nameplate and versions.

The page opens for a specific MAP from the “MAP” menu. If there are several MAPs, all devices will be in the menu.

## What you will see

At the top — a header with the model and serial number and the “Disable update” checkbox. It pauses the automatic data refresh. The last opened tab is remembered.

The page is split into three tabs.

### Panel

A live schematic: voltages, currents, grid, load, and battery powers, mode, energies, temperatures, relay state, MAP time, and last snapshot time. If a BMS or parallel MPPTs are connected, their tables appear below.

### Information

Two sub-tabs:

- **Main** — the device nameplate: board and firmware version, type, power, operating voltage, charge and grid parameters, relays, BMS/MPPT.
- **System** — tables of all parameters by group. Names are labeled, values are recalculated into human units. A dash means the value is unavailable for this model or firmware.

### Control

At the top — commands, below — parameters by group: “Generation”, “Net 1”, “Net 2”, “Battery”, “External devices”, “Relay”, “Other”.

The “External devices” group configures the RS3 port and external BMS/MPPT. RS3 is available on Titanator only; on Dominator only BMS/MPPT are shown.

## How to issue a command

1. Open the “Control” tab.
2. Press the needed button.
3. If a confirmation appears — confirm the action.

Commands:

- **Turn MAP on/off.** Turning off requires a confirmation.
- **Start/Stop charge.**
- **Relay 1..N** — only if the relays are configured for external control. The Titanator has three relays, the Dominator has two. The R1–R3 indicators on the panel are always shown and display the line state, even if the relays are not available for control.
- **Reset** — reboots the controller. Requires three confirmations in a row; you can cancel at any step.

## How to change a parameter

1. Find the row in the needed group.
2. Change the value in the “Value” column.
3. Press “Save” in the row — or “Save all” to write all changes at once.

Rules:

- a changed row is highlighted;
- an invalid value is highlighted in red and cannot be saved;
- “Cancel” in the row restores the current value;
- “Cancel all” cancels all unsaved edits;
- after a successful write the values are re-read from the MAP;
- if you have started editing a field, auto-refresh does not overwrite your input.

The “?” icon next to a parameter shows an explanation.

## How to check it worked

- The command has been executed: the state on the panel has changed.
- The parameter has been saved: after “Save” the change highlight cleared and the value was re-read from the MAP.
- There are no write errors on the page.

## If it did not work

- **“Connection lost”** — data is not arriving. Check the connection and polling settings; the page keeps retrying.
- **The value is not saved** — check the range. The “?” tooltip explains the parameter; red highlighting indicates an error.
- **Saving returned an error** — retry. If the error repeats, check the connection and polling state; if necessary, contact the administrator.
- **No relay buttons** — relays are not provided for this model or have not been switched to external control mode.
- **The page “disappeared” after reconnection** — wait up to a minute: the page will find the device by serial number on its own.
- **Many dashes** — the values are not published by this model or firmware. This is not necessarily a fault.

## Related

- [MAP settings](map-settings.md)
- [Statistics: charts](stats-charts.md)
- [Statistics: tables](stats-tables.md)