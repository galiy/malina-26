# KES

![KES page: output and battery](../img/kes/screen.png)

## When to come here

- You need to see how much the solar panels produce and how the battery is charging.
- You need to check temperatures, current sensors, errors, and the relay state.
- You need to switch a relay.

The page opens for a specific KES from the “KES” menu.

## What you will see

At the top — the “Disable update” checkbox. It pauses the automatic refresh.

On the panel:

- the output indicator (“sun”);
- three relays — you can click on them;
- the charge mode and the last snapshot time;
- panel power and the MPPT ID;
- blocks: panels (voltage, current, power), battery (voltage, charge current, output power), current sensors, internal temperature, daily energy, battery temperature, state and errors, wind generator speed (if any);
- at the bottom — the model and firmware version.

If there are no errors, the state shows “n/a”. When there is an error, its description appears; an I²C sensor error is also shown as separate text.

## How to switch a relay

1. Click on the icon of the needed relay.
2. Wait for the state to refresh.

Only the selected relay is switched, the others keep their state. No confirmation is requested.

There are no “turn KES on/off”, “charge”, and “reset” commands for the KES — control is limited to the relays.

## How to check it worked

- The relay icon on the panel changed state.
- The output and charge data keep refreshing.
- There are no new errors in the state.

## If it did not work

- **Empty page or “No data / polling disabled”** — KES polling is disabled by default. Enable it in “KES settings”.
- **“Connection lost”** — data is not arriving. Check the connection and polling settings; the page keeps retrying.
- **Relay control error** — retry. If it repeats, check the connection.
- **The status shows “n/a”** — there are no errors, this is normal.
- **The page “disappeared” after reconnection** — wait up to a minute: it will find the device by serial number on its own.

## Related

- [KES settings](kes-settings.md)
- [Statistics: charts](stats-charts.md)
- [Statistics: tables](stats-tables.md)