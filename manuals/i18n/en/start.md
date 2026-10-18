# Where to start

This page is for first power-on. It walks you from applying power to the first settings worth doing right away.

![Sign-in page](img/start/login.png)

## What you will need

- A Malina device with a power supply.
- A phone or laptop with Wi-Fi.
- An Ethernet cable — optional, if you need to connect the device to a shared network.

## Step 1. Turn on and wait

Connect the power. The device boots and brings up its own Wi-Fi access point on its own. This takes about a minute, in rare cases up to two. Nothing needs to be pressed.

## Step 2. Connect to the Wi-Fi

On your phone or laptop, open the list of Wi-Fi networks and connect to:

- network name — **Malina**;
- password — **malina-26**.

If the device has already been configured and the network name has been changed, use the new name.

## Step 3. Open the admin panel

After connecting, the system may offer “Sign in to the network” — accept it. This is the admin sign-in page.

If the offer does not appear, open a browser and enter the address:
**http://10.10.100.254/**

## Step 4. Sign in

Enter the password **malina-26**. The username is not requested.

The interface defaults to English. To switch the language, use the switch in the top right — the choice is remembered.

## Step 5. Make sure everything works

Open “System → System information”. You should see data about the host, processor, memory, disks, and network — it refreshes automatically.

If the devices (MAP, KES, BMS, inverters) are not connected yet, the main page and the device sections will be empty. This is normal, not a fault.

## Step 6. Change the passwords right away

The default passwords are known to everyone, so they must be replaced.

1. Open “System → System parameters → Change password”.
2. Enter a new password twice (minimum 4 characters) and save.

This password applies to the web admin and SSH — including for the `root` user. After the change all sessions will be ended — sign in again.

The access point password is changed separately: “System → Network → Mode 1 → Password”. Minimum 8 characters.

## Step 7. Check the key settings

- **Device name** — “System parameters → Host”.
- **Timezone** — in the same place. Pages and history times depend on it.
- **Log forwarding** — “System parameters → Logging”, if you have a syslog server. An empty field means take the address automatically from the network (DHCP).
- **MAP time synchronization** — “System parameters → MAP time sync”.
- **Devices** — if the MAP has not been detected yet, check the connection. The KES and BMS are not polled by default — enable them on the settings pages.

## Connect by cable

If you connect the device to a shared network with an Ethernet cable, it will receive an address automatically. Find this address in your router's client list and open `http://<address>/` in a browser.

The address by the name `malina.local` is not guaranteed — use the IP address.

Access through the access point remains available at the same time.

## Connect the device to an external Wi-Fi

If you want Malina to connect to your Wi-Fi network by itself:

1. Open “System → Network” and select the “Wi-Fi client” mode.
2. Specify the network (you can press “Scan”) and the password.
3. Save the settings.

After saving, the access address changes: the device becomes a client of the external network. Find its new address in the router's client list. The local network `10.10.100.x` moves to the wired port.

If the connection fails, the device brings up the **Malina** access point by itself so you can return and fix the settings.

## If something goes wrong

- **The Malina network is not visible.** Wait up to two minutes. Check that the device is on. If the network disappeared after changing settings — connect by cable or reboot the device.
- **The page does not open.** Make sure you are on the `Malina` network and are entering exactly `http://10.10.100.254/`.
- **The password does not work.** Check the keyboard layout and case. If you changed the password and forgot it — see the [Administrator manual](admin.md).
- **Access is lost after changing the network mode.** Find the new address on the router or connect to the access point that the device brings up on failure.

## First-run checklist

- [ ] connected to the `Malina` network;
- [ ] opened the admin panel and signed in;
- [ ] chose a language;
- [ ] changed the web admin and SSH password;
- [ ] changed the access point name and password;
- [ ] checked the device name and timezone;
- [ ] reviewed the “System information”;
- [ ] if necessary, configured the network and enabled the needed devices.

## Related

- [Table of contents](README.md)
- [Administrator manual](admin.md)
- [Network settings](pages/network.md)