# MAP settings

![MAP settings: status and form](../img/map-settings/screen.png)

## When to come here

- The MAP is not displayed or data is not refreshing — you need to check whether polling is enabled.
- You need to temporarily disable MAP polling.
- You need to raise the log verbosity for diagnostics.

## What you will see

At the top — the “Device status” card: all MAPs the device sees. For each one, the ID, serial number, port, model, protocol, speed, state, and time of the last successful poll are shown. The “OK” state means data has been arriving recently; “No connection” means the device is silent. The card refreshes once a second.

Below — the settings form.

## How to enable or disable polling

1. Check or uncheck “Enabled”.
2. Press “Save”.

When polling is off, the service keeps running but no MAPs are polled: no new data arrives, and the status card may keep the last state.

## How to change the log level

Choose a level and save:

- **Debug** — the most detailed, for diagnostics.
- **Info** — normal mode.
- **Warning** — warnings only.
- **Error** — errors only.

“Info” is used by default. It is worth enabling a detailed level temporarily: it fills the log faster.

## How to check it applied

- After saving a success message appears.
- The settings apply immediately, without restarting the service.
- In the status card the MAP state should remain “OK” and the last success time should keep updating.

## If it did not work

- **Polling did not change after saving** — repeat the save. If that does not help, check the service state on the “Services” page.
- **The card shows “No data / polling disabled”** — enable polling and save.
- **The MAP shows “No connection”** — check the cable, power, and port; if necessary, contact the administrator.
- **“Invalid log level”** — choose a value from the list.

## Related

- [MAP](map.md)
- [System information](info.md)
- [Services](services.md)