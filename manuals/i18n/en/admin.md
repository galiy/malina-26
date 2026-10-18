# Administrator manual

This page is for whoever maintains the device: SSH access, access recovery, service and log management. It is not needed for everyday work with the devices.

## SSH access

Connect to the device network (access point or shared network) and run:

```
ssh malina@<device-address>
```

- On the access point network the address is `10.10.100.254`.
- On a shared network — the address the device received via DHCP or set manually.

By default the password is `malina-26`. It also applies to the `root` user.

The password is changed on the “System parameters → Change password” page and immediately applies to the web sign-in and SSH.

## If the password is lost

- If SSH access is still available, sign in and change the password via the “System parameters” web page.
- If both the web password and the SSH password are lost, there is no regular way to restore access without reinstalling the device. Contact whoever is responsible for the device.

## Managing services

Open “System → Services”.

- The **“Malina”** tab — subsystem services. These can be started and stopped.
- The **“System”** tab — operating system services, view only.

Before stopping a service, read the comment in its row. Some services must not be stopped:

- the network service — the admin panel and access point will disappear;
- the web admin itself — the interface will close;
- critical OS services — can disrupt the device;
- timers and services managed by automation do not need to be started manually.

If access is lost after stopping the network service, reboot the device.

## Reboot and shutdown

- “System parameters → Power management” — reboot and clean shutdown.
- “Services → Reboot Malina” — the same.

After a reboot a waiting page opens. It checks availability for up to two minutes. If it takes longer — refresh the page manually.

On shutdown, wait for complete stop and only then disconnect power.

## Network: modes and access recovery

- **Mode 1 “Access point”** — the device creates a Wi-Fi network. The admin panel is available at the device address on this network (`10.10.100.254` by default). The wired port connects to the shared network.
- **Mode 2 “Wi-Fi client”** — the device connects to an external Wi-Fi network. The admin panel is available at the address the device received on this network. The local network for wired clients remains on `eth0`.

When the mode changes, the access address changes. If access is lost after saving, look for the device at the new address on the router. If the device fails to connect to the external network, it temporarily brings up the access point by itself.

If a foreign DHCP server is detected on the wired segment, the device pauses its own address distribution but stays reachable at its own address. When the foreign server disappears, distribution resumes automatically.

## Logs

“System → System information → Logs” — viewing system logs. The file list is fixed; logs are read-only.

To forward logs to an external server, specify the address in “System parameters → Logging”. If the field is empty, the address from the network settings is used.

## Time

- The device timezone is changed in “System parameters → Host”.
- The MAP time is synchronized with the device every 10 minutes if the difference is greater than a minute. It is disabled in the same place, under “MAP time sync”.

## Monitoring

“System parameters → Monitoring” enables periodic notification sending to an external receiver. You need to specify the receiver IP and the period. If monitoring is not needed, leave it disabled.

## API

- [Native API: lists, raw](api-native.md) — tokens, lists, raw contracts, writing, and statistics.
- [Legacy API](api-legacy.md) — the whole old API and “Master Node” compatibility.
- The Native API uses a Bearer token or a cookie. Legacy reading is open without authorization; legacy writing uses HTTP Basic (`malina` + password).

## What to do in case of failures

1. Open “System information”: check temperature, power, disks, and failed services.
2. Open “Services”: look at the service states.
3. Open “Logs”: find error messages.
4. If the device is hung — reboot it.
5. If the problem persists — save the description and time, contact the developer.