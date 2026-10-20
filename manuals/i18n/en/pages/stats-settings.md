# Backup settings

The **Statistics → Settings** page manages backup of the telemetry database. A copy is needed so the history survives a device reinstall or a memory card loss.

## When to use this page

- You need to save the measurement history to a network drive or a flash drive.
- After a reinstall you need to bring back the accumulated data.
- You need to check that the backup is actually running.
- You need to temporarily disable the backup without deleting the archive already made.

## What you will see

A **Database backup** card and a status card.

In the settings:

- the backup toggle (shows whether backup is currently enabled);
- destination selection: **network share** or **flash drive**;
- the scan interval for the averaged database;
- logging level (`info` by default);
- network parameters group or flash partition choice — depending on the destination.
Fields that do not apply to the chosen destination or protocol are hidden. If there is no archive, a single “Enable backup” button is shown. Avg files appear after a segment is sealed; the active segment is not copied to the archive.

The password is shown only while typing. After saving it is not displayed; an empty field means "leave empty to keep the previous password".

The status card shows: whether backup is enabled, the current phase, the archive session ID, the time of the last backup of detailed and averaged data, the archive size, progress, and the last error.

## How to set up a network archive

1. Select the **Network share** destination.
2. Enter the protocol, host, port, path, user, and password.
3. For SFTP, paste the server host key given by the network administrator.
4. Click **Save**.
5. Click **Test access**. A success message should appear.
6. Enable the backup with the restore or archive-erase action — see below.

## How to set up a flash drive

1. Insert the external drive and wait for it to be detected.
2. Select the **Flash drive** destination.
3. Choose a partition from the list. The device's system card is not shown in the list — it cannot be used.
4. Click **Save**, then **Test access**.
5. Enable the backup.

If the flash drive is removed, the backup pauses. After it is reconnected, the backup resumes automatically.

## How to enable the backup

When enabled, the device checks whether a ready archive exists at the selected destination.

- **No archive** — the backup turns on immediately and a new session starts.
- **Archive exists** — a choice appears:
  - **Restore both DBs** — restore raw and avg from the archive. Current local data will be replaced. During an automatic restore at boot, avg is restored only if it is absent on the device.
  - **Enable and erase archive** — start a new session and delete the old archive.
  - **Leave disabled** — change nothing.

Before a restore, measurement recording stops. The web page stays available: it shows progress and a cancel button.

## How to disable the backup

Click **Disable**. The backup and archive trimming will stop, the service stays running, and the archive already made is kept. You can enable the backup again later. If the device has not rebooted, the archive continues; after a reboot you will need to restore or start a new session.

## How to check that it worked

- State is "enabled" and the last error is empty.
- The time of the last detailed-data backup is updating.
- The time of the last averaged-data backup is updating at the selected interval.
- The archive size is growing or stays stable.
- After testing access, archives appear as found.

## If it does not work

- **"Failed to decrypt secret"** — enter the password again and save the settings.
- **State "credentials_invalid"** — the device key or password does not match. Enter the password again; if that does not help, contact the administrator.
- **State "degraded"** — the backup destination is temporarily unavailable. Check the network or the flash drive; once access returns the backup will continue on its own.
- **State "restore_required"** — an archive is found at the destination but it is not applied automatically. Choose a restore or an archive erase.
- **"Test access" fails** — check the address, path, login, password, and server host key.
- **Restore failed** — raw and avg are cleared, backup and its setting are disabled. Fix the cause and enable backup again.
- **The restore takes long** — do not turn off the device. Wait for it to finish or click "Cancel restore".
- **"No segments to copy"** — the averaged database has not accumulated data yet. This is not an error; a copy will appear later.
- **The flash drive is not in the list** — check that it is an external drive and not the system card, and that it is formatted.

## Related

- [Statistics: tables](stats-tables.md)
- [Statistics: charts](stats-charts.md)
- [Services](services.md)