# Grid-tie inverter settings

![Grid-tie inverter settings](../img/grid-settings/screen.png)

## When to come here

- You need to add a Deye or Sofar grid-tie inverter.
- You need to change a location, the poll period, or the list of locations.
- You need to auto-detect the logger SN.

## What you will see

At the top — the status table of all inverters from the settings, including not-yet-polled ones: #, name, serial, address, type, status, and last success time. Until an inverter has been polled, its serial and address may be empty. It refreshes once a second.

Below — an important reminder: **the logger IP address must be stable.** Set a static address on the logger itself or reserve an address for it on the DHCP server. If the address changes, polling will stop until you specify a new one.

## How to enable polling

1. Check “Enable polling”.
2. Press “Save”.

Polling is disabled by default. While it is off, the section on the main page is hidden.

## How to set common parameters

- **Log level** — “Debug”, “Info”, “Warning”, “Error”. “Info” is the default.
- **Poll period** — 10, 20, or 30 seconds. 10 is the default.
- **Locations** — the list of names for grouping tiles on the main page. Add and delete rows. A location used by an inverter cannot be deleted.

## How to add an inverter

1. Press “Add inverter”.
2. Fill in the name (can be left empty — the IP will be substituted), IP, port, type, location, and logger SN.
3. If necessary, press “Detect” next to the SN — the device will try to get the logger serial over the network. The button works for local addresses.
4. Press “Save”.

Up to 16 inverters are allowed. The default port is 8899.

Distinguish between the two serial numbers:

- **Logger SN** — you set it manually or detect it with the button; it is needed for polling.
- **Factory serial number of the inverter** — read from the inverter automatically; it identifies the device in the history.

Until the factory serial number is read, there will be no data.

## How to check

- After saving the “Settings saved” message appears.
- In the status table the inverter transitions to “OK” after a successful poll.
- Tiles appear on the main page, and inverter pages in the menu.

## If it did not work

- **“SN not received”** — check the inverter's network reachability, IP, port, and type. For Sofar automatic detection may not work — enter the SN manually.
- **Another inverter cannot be added** — the limit of 16 has been reached.
- **A location cannot be deleted** — it is used by an inverter. First assign the inverters a different location.
- **“The service did not respond” error** — repeat the save.
- **The inverter shows “not polled”** — check that polling is enabled and the SN is filled in.
- **No data after the inverter IP changed** — specify the new address in the settings.

## Related

- [Grid-tie inverters](grid.md)
- [Network settings](network.md)