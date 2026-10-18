# KES settings

![KES settings](../img/kes-settings/screen.png)

## When to come here

- The KES is not displayed — most likely polling is disabled.
- You need to temporarily disable polling.
- You need to raise the log verbosity for diagnostics.

## What you will see

At the top — the “Device status” card: all KES devices the device sees. The ID, port, speed, serial number, state, and last success time are shown. “OK” — data has been arriving recently; “No connection” — the device is silent. The card refreshes once a second.

Below — the settings form.

## How to enable polling

1. Check “Enabled”.
2. Press “Save”.

KES polling is disabled by default. While it is off, no new data arrives, and the status card may keep the last state.

## How to change the log level

Choose “Debug”, “Info”, “Warning”, or “Error” and save. “Info” is the default. Enable a detailed level temporarily: it fills the log faster.

## How to check it applied

- After saving a success message appears.
- The settings apply immediately, without restarting the service.
- In the status card the KES state should be “OK”.

## If it did not work

- **“No data / polling disabled”** — enable polling and save.
- **The KES shows “No connection”** — check the cable, power, and port; if necessary, contact the administrator.
- **Polling did not change after saving** — repeat the save; check the service state on the “Services” page.
- **“Invalid log level”** — choose a value from the list.

## Related

- [KES](kes.md)
- [Services](services.md)