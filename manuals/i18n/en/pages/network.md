# Network settings

![Network settings: modes and parameters](../img/network/screen.png)

## When to come here

- You need to choose how the device works on the network: it creates its own Wi-Fi network or connects to an existing one.
- You need to change the access point name or password.
- You need access point clients to reach the Internet.
- You need to change addresses or disable DHCP distribution on the wired port.

## Two modes

The device works in one of two modes. Switching — choosing a tab and pressing “Save”. Both sets of settings are stored separately and are not lost when switching.

### Access point

The device creates a Wi-Fi network, distributes addresses, and can let clients reach the Internet through the wired port.

Fields:

- **SSID** — the network name.
- **Password** — a WPA2 password, minimum 8 characters.
- **Channel** — `auto` or 1–11.
- **Region** — the Wi-Fi region.
- **Disable Wi-Fi** — turn off the access point while Ethernet is connected and bring it back up after the cable is unplugged.
- **Our network / DHCP** — the device IP address, mask, and the start and end of the address pool for clients.
- **Ethernet** — `DHCP` or `static`; for static mode — IP, mask, gateway, DNS.
- **Internet transit (NAT via Ethernet)** — allow clients to reach the Internet through Ethernet. Enabled by default.
- **Captive portal** — offer clients a sign-in page when connecting. Enabled by default.

### Wi-Fi client

The device connects to an external Wi-Fi network, and the wired port becomes a local network for clients.

Fields:

- **SSID** and **Scan** — choose the external network.
- **Password** of the external network.
- **STA IP** — `DHCP` or `static`; for static mode — IP, mask, gateway, DNS.
- **Our network / DHCP (eth0)** — the device IP address, mask, address pool.
- **Serve DHCP on eth0** — whether to issue addresses to wired clients. Enabled by default.

## How to change the mode

1. Open “System → Network”.
2. Choose the tab of the needed mode.
3. Fill in the fields.
4. Press “Save”.

The network will restart and the current connection may drop. After saving, connect again:

- in access point mode — to the device Wi-Fi, the default device address is `10.10.100.254`;
- in client mode — to the external Wi-Fi network and find the device's new address in the router's client list.

The “Restore” button returns the fields to the last saved values, not to the factory ones.

## How to change the access point password

1. Open the “Access point” tab.
2. Change the password (minimum 8 characters).
3. Save and connect to the network again.

## How to check

- “System information → Network state” shows the persistent and current mode.
- Clients receive addresses if distribution is enabled.
- With NAT enabled, access point clients reach the Internet through Ethernet.

## If it did not work

- **Access lost after saving** — the address or mode has changed. Find the device at the new address or connect to the access point.
- **Cannot connect to an external Wi-Fi** — check the SSID, password, and network range. After a failure the device brings up the access point by itself so you can fix the settings.
- **No Internet for access point clients** — a connected Ethernet with Internet and NAT enabled are required.
- **Wired clients do not receive addresses** — check “Serve DHCP on eth0” and the “DHCP monitor” state on “System information”. When a foreign DHCP is detected, distribution is intentionally paused.
- **The access point disappeared when the cable was connected** — this is how the “Disable Wi-Fi” option works. Uncheck it or unplug the cable.
- **The access point is not visible** — wait up to two minutes, check the mode and the “Disable Wi-Fi” option. If that does not help — reboot the device.

## Related

- [Where to start](../start.md)
- [System information](info.md)
- [Administrator manual](../admin.md)