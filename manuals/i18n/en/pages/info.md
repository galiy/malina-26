# System information

![System information: host, processor, disks, network](../img/info/screen.png)

## When to come here

- You want to understand the overall device state: is it overheating, is there enough space, are there any failures.
- Something is not working correctly and you need to find the cause.
- You need to see which devices are connected and who is using the processor.

## What you will see

The page consists of cards. Data refreshes automatically once a second. The “Auto-refresh” checkbox in the top right stops the refresh — handy if you want to read values calmly. Only the auto-refresh state is remembered.

### Host

Device name, model, OS and kernel versions, uptime, boot time, and current time. Below — two blocks:

- **Network state** — which mode is selected and which is active now. If a temporary mode (override) is shown, the device switched by itself to stay reachable. Here you also see whether the device is serving addresses on the wired port and whether a foreign DHCP server has been detected.
- **Power** — warnings about undervoltage and overheating, core voltage. If there are no limits, it says so.

### Processor

CPU load and temperature — as two bars. Below: model, number of cores, average load, and current frequency. Further below — memory: used, free, available, cached, and swap.

### Disks

Partition usage: system, data, boot. If space is running out, this is visible right away — free up space or contact the administrator.

### Network

Gateway, DNS servers, and the list of interfaces: addresses, speed, received and transmitted. Useful when you need to understand which network the device is on.

### Failed units

Services that are not running properly. If the list is empty — “none”. Refreshes no more often than once every 30 seconds.

### Connected devices

- **Wi-Fi clients** of the access point: name, IP, MAC. For access point clients, signal and rate may be unavailable.
- **USB devices**: what is connected to the ports.

### Processes

To find out what loads the device:

1. Open the “Processes” block.
2. Choose the “by CPU” or “by memory” sorting.
3. Press “Refresh”.

The list is re-read only manually. Page auto-refresh does not change it, so as not to load the device.

### External command errors

This card appears only if there were recent data-fetching failures. If it is absent — there were no failures.

### Logs

At the bottom — quick links to the logs: `syslog`, `auth.log`, `kern.log`, `user.log`, `cron.log`, `mail.log`, `daemon.log`, `messages`, `dpkg.log`.

## How to check it worked

- The page opened, cards are filled, the refresh time changes.
- Under “Host” the desired network mode is shown and there are no power warnings.
- Under “Failed units” — “none” or only services you know about.
- Under “Disks” there is enough free space.

## If something is off

- **Red temperature bar or power warning** — check the cooling and the power supply.
- **There are failed services** — open “Services” and “Logs”.
- **“DHCP monitor” shows “foreign”** — another DHCP server is running on the wired segment. The device has paused its own distribution but stays reachable.
- **External command errors card** — wait for the next refresh. If it repeats, reboot the device and look at the logs.
- **The process list does not change** — it only refreshes with the “Refresh” button.
- **There is no history in statistics** — check the accumulation settings under “System parameters”.

## Related

- [Services](services.md)
- [Logs](logs.md)
- [System settings](system.md)