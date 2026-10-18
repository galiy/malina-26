# Logs

![Viewing a log](../img/logs/screen.png)

## When to come here

- You need to understand why a service is not working.
- You need to see what happened with the device.
- You need to find an error by time.

## What you will see

The list of log files and a viewing window. Available: `syslog`, `auth.log`, `kern.log`, `user.log`, `cron.log`, `mail.log`, `daemon.log`, `messages`, `dpkg.log`.

The page is read-only: you cannot delete, download, or filter a log.

## How to view

1. Choose a file in the list.
2. Scroll down to follow new entries.
3. Scroll up to load older entries.

While the list is pinned to the bottom, it refreshes once a second. When scrolling up, the refresh pauses so as not to interfere with reading; older entries are loaded in chunks.

The selected file is remembered in the page address — you can save the link.

## How to check it worked

- The window shows lines of the selected log.
- New entries appear at the bottom, the refresh time changes.
- Scrolling up loads older entries.

## If it did not work

- **The list does not refresh** — scroll to the bottom: auto-refresh only works at the bottom edge.
- **The top lines “disappeared”** — this is older entries being loaded; scroll up further.
- **The file is empty** — the log may not have been created yet or was cleared after a reboot. Choose another file.
- **Signed out** — the session expired, sign in again.
- **The needed event is not there** — check another file; some events are written to separate logs.

## Related

- [Services](services.md)
- [System information](info.md)
- [System parameters](system.md)