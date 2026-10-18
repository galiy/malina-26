# Master node settings

![Master node settings](../img/master-settings/screen.png)

## When to come here

You need to add, change, or delete the devices visible on the master node overview panel.

## What you will see

The list of nodes: name, scheme, host or IP, port. The “+” button adds a row, “✕” deletes one. At the bottom — “Save” and “Cancel”.

## How to configure

1. Open “MASTER NODE → Settings”.
2. Add rows with the “+” button.
3. For each row specify:
   - **Name** — 1–32 characters;
   - **Scheme** — `http` or `https`;
   - **Host / IP** — an address or name;
   - **Port** — 1–65535, 80 is the default.
4. Delete unneeded rows with the “✕” button.
5. Press “Save”.

“Cancel” returns the rows to the state at the moment the page was opened.

You can add up to 30 nodes. `host:port` duplicates are prohibited. If the list is empty, the local node “Local” (`localhost:80`) is shown in the form right away.

## How to check

- After saving a success message appears.
- Go to “Display” — the added nodes appear on the panel.
- A responding node is marked green, an unreachable one — red.

## If it did not work

- **“Invalid host”** — check the address.
- **“Duplicate node”** — this `host:port` is already in the list.
- **“Name 1..32 characters”** — shorten or lengthen the name.
- **“Too many nodes”** — delete the extra ones, maximum 30.
- **A node is unreachable** — check the network, port, scheme, and device power.

## Related

- [Master node](master.md)