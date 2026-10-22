# BMS settings

![BMS settings](../img/bms-settings/screen.png)

## When to come here

- You need to enable collecting data from the battery packs.
- You need to add a BMS connected through a network gateway.
- You need to set up transferring battery data to the inverter.
- You need to figure out why the batteries do not appear.

This page has three parts: the gateways table, the BMS receiver settings, and the BMS Proxy settings. At the top — the status card of all BMS devices.

## What the status card shows

The list of batteries: ID, base, source, type, state, format, generation, and last success time. For everyday work the important ones are “Type”, “State”, and “Last success”. It refreshes once a second.

## BMS gateways

This is the list of network gateways through which the device collects data.

1. Press “+” to add a row.
2. Specify the host or IP, port, and mode.
3. Press “Save”.

Modes:

- **modbus** — a MODBUS-TCP gateway;
- **none** — a transparent gateway (e.g., RS485↔TCP) or a native protocol over TCP.

You can delete a row with the “✕” button. COM devices do not need to be added: they are detected automatically.

## BMS Receiver

- **Enabled** — battery polling. Disabled by default.
- **Log level** — “Debug”, “Info”, “Warning”, “Error”.

While the receiver is disabled, the service runs but no batteries are polled and the list is empty.

## BMS Proxy

The proxy transfers battery data to the inverter through a serial port.

- **Enabled** — enable the proxy. Disabled by default.
- **BMS protocol** — `Vektor(Growatt)` (default) or `Seplos(Pylon)`. Vektor replies to the inverter with a status block (SOC, voltage, current, temperature, limits) and 16 cell voltages from active batteries (1-device mode); Seplos — PACE/ASCII.
- **Port speed** — 2400, 9600, 19200, 57600, 115200, or 230400. 9600 is the default.
- **Log level.**
- **Inverter addressing** — `1–16` (default) or `0–15`. Depends on how the inverter numbers the batteries.
- **UDP listener (gateway)** — an optional second channel if the inverter is connected through a transparent UDP↔RS485 gateway. It is enabled with a separate checkbox; specify the listen address, e.g. `0.0.0.0:502`. Without an address the listener stays inactive.

The proxy does not poll batteries on its own: it takes data from the receiver. If the receiver is disabled or does not see the batteries, the proxy has nothing to answer with.

## How to save

The page has a single “Save” button — it writes all three parts at once. The settings apply without restarting the service.

## How to check it worked

- After saving a success message appears.
- In the BMS status card — “reachable”, “Last success” is updating.
- The proxy, if enabled, answers the inverter: it sees the batteries.

## If it did not work

- **The batteries do not appear** — check whether the receiver is enabled, whether the adapter or gateway is connected, and whether the port is not busy with another device. Detection can take up to a few tens of seconds.
- **Status “No data / polling disabled”** — enable the receiver and save.
- **It is in the status but shows “No connection”** — check the cable, power, and gateway reachability; look at “Last success”.
- **The inverter does not see the batteries through the proxy** — check whether the proxy is enabled, whether the port speed matches, whether the addressing is correct, and whether the batteries are alive. When using UDP, check the checkbox, the address, and the gateway setting.
- **The port does not save** — enter an integer from 1 to 65535.
- **No per-cell warnings** — some BMS protocols do not transmit them.

## Related

- [BMS](bms.md)
- [Services](services.md)