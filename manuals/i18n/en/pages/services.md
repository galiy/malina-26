# Services

![Services: service state](../img/services/screen.png)

## When to come here

- You need to check whether the device services are running.
- You need to restart a service.
- You need to reboot the device.

## What you will see

Two tabs:

- **Malina** — subsystem services. These can be started and stopped.
- **System** — operating system services. View only.

Columns: service, description, state, actions, comment. States: running, stopped, failed, starting, stopping, rebooting, no data.

The list refreshes automatically every 5 seconds.

## How to start or stop a service

1. Open the “Malina” tab.
2. Find the needed service.
3. Press “Start” or “Stop”.

A button is unavailable if the action no longer makes sense: a running service cannot be started again, a stopped one cannot be stopped.

Before stopping, read the comment:

- “do not stop” — a critical service;
- “stopping will break the network/AP” — access will be lost;
- “stopping will close the UI” — the interface will close;
- “runs on a schedule” — a timer;
- “do not start manually” — the service is managed by automation.

Stopping is performed without an additional confirmation.

## How to reboot the device

1. Press “Reboot Malina”.
2. Confirm the action.
3. Wait for the waiting page.

The page checks availability for up to two minutes and returns you to the main page on its own. If it takes longer — refresh the page manually.

## How to check it worked

- The state of the needed service changed to “running” or “stopped”.
- After a reboot the waiting page returned you to the main page.

## If it did not work

- **Access lost after stopping a service** — reboot the device.
- **The interface closed** — the web admin was stopped; after it starts or after a reboot, sign in again.
- **The service is in the “failed” state** — open “Logs” and look at the cause.
- **The action “did not work”** — wait a few seconds, the list will refresh; the cause is usually visible in the logs.
- **The page showed an error after the reboot** — the device may take longer to boot; refresh the page.

## Related

- [Logs](logs.md)
- [System information](info.md)
- [Administrator manual](../admin.md)