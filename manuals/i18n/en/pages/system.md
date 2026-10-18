# System parameters

![System parameters](../img/system/screen.png)

The page opens from the “System → System parameters” menu. The page title itself is “System settings”.

## When to come here

- You need to change the web admin and SSH password.
- You need to rename the device or change the timezone.
- You need to set up log forwarding, MAP time sync, statistics accumulation, or monitoring.
- You need to reboot or shut down the device.

## What you will see

Cards with settings: hostname and timezone, log forwarding, MAP time sync, statistics accumulation, monitoring, change password, and power management. Changes are saved separately by each card.

## How to change the name and timezone

- **Hostname** — the device's network name. Latin letters, digits, and hyphen are allowed; the name must not start or end with a hyphen. Applied immediately.
- **Time zone** — the device's local time. Affects the time on the pages and in the history.

The address by the name `malina.local` is not guaranteed — use the IP.

## How to set up log forwarding

Specify a **remote syslog host** — a host name or IP. The port is not specified. An empty field means the address is taken automatically from the network (DHCP).

1. Enter the address.
2. Press “Save”.

## How to enable MAP time sync

The switch enables comparing the MAP clock with the device. The comparison runs every 10 minutes; the time is corrected only if the difference is greater than a minute.

1. Check “Enable MAP time synchronization”.
2. Press “Save”.

## How to set up statistics accumulation

- **Enable averaging** — write an averaged history.
- **Averaging period (min)** — 1, 2, 3, 4, 5, 6, or 10 minutes. 5 is the default.

If averaging is disabled, detailed data keeps being written, but the averaged history is not updated.

1. Choose a period.
2. Press “Save”.

## How to enable monitoring

Periodic sending of SNMP traps to an external receiver.

- **Enable periodic SNMP trap sending** — disabled by default.
- **Trap receiver IP** — required when enabled.
- **Period (seconds)** — an integer from 30 to 3600. 300 is the default.

1. Enable the sending.
2. Specify the receiver IP and the period.
3. Press “Save”.

The receiver IP and period are configurable. The other receiver parameters are fixed and are not shown in the interface.

## How to change the password

1. Enter the new password twice.
2. Press “Change password”.

Rules: minimum 4 characters; control characters, colon, and line breaks are prohibited.

The new password applies to both web sign-in and SSH — for the `malina` and `root` users. After the change all sessions are ended, sign in again.

The access point password is changed separately, on the “Network settings” page.

## How to reboot or shut down

- **Reboot** — a clean reboot. A waiting page will open.
- **Shut down** — a clean shutdown. Disconnect power after it has fully stopped.

Both actions require a confirmation.

## How to check it worked

- After saving a success message appears and the value stays in the field.
- The hostname and timezone apply immediately.
- Log forwarding: entries appear on the syslog server.
- MAP time sync: within 10 minutes the MAP time matches the device time.
- Statistics: new averaged rows appear in “Statistics → Tables”.
- Monitoring: when enabled, the external receiver gets periodic notifications.

## If it did not work

- **“Invalid hostname”** — remove invalid characters, spaces, and hyphens at the edges.
- **“Invalid time zone”** — choose a value from the list.
- **“Invalid remote syslog host”** — check the address format.
- **The statistics period does not save** — only 1, 2, 3, 4, 5, 6, and 10 minutes are allowed.
- **Monitoring does not save** — check the IPv4 address and the 30–3600 second period.
- **“Password too short (min 4)”** — minimum 4 characters.
- **“Passwords do not match”** — enter the same password in both fields.
- **Signed out after changing the password** — this is normal, sign in with the new password.
- **The MAP time does not change** — check that synchronization is enabled, the MAP is reachable, and the device time and timezone are correct; wait up to 10 minutes.

## Related

- [Table of contents](../README.md)
- [Network settings](network.md)
- [Administrator manual](../admin.md)
- [Statistics: charts](stats-charts.md)