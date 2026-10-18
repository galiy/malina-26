# Master node

![Master node: overview panel](../img/master/screen.png)

## When to come here

You have several Malina devices and you need to see their state from a single page without going to each one separately.

The master node is an overview panel. You cannot control devices from it. The interface is ported from the old version and is available in Russian only.

## What you will see

An overview panel with all added nodes. For each node the state of the MAP, battery, MPPT, charger, relays, and errors is shown. Data refreshes roughly once every 3 seconds.

Node indicator:

- green — the node is responding;
- red — the node is unreachable.

An unreachable node or a missing subsystem is dimmed. This is not an error: there is simply no data. An error on one node does not prevent showing the others.

## How to add a node

1. Open “MASTER NODE → Settings”.
2. Press “+”.
3. Specify the name, scheme (`http` or `https`), host or IP, and port.
4. Repeat for the other nodes.
5. Press “Save”.
6. Go to “Display” to open the overview panel.

For the local device the name, `http`, `localhost`, and port `80` will work.

## How to check it worked

- The node on the panel is marked green and the data refreshes.
- An unreachable node is marked red — the other nodes keep working.

## If it did not work

- **A node is red or shows “no data”** — check the host, port, and scheme; make sure the device is on and reachable. On a remote node the old interfaces that the panel uses must be available.
- **Data lags by a couple of seconds** — this is a cache, not an error.
- **A node does not save** — check the name (1–32 characters), address, and port; `host:port` duplicates are prohibited; there can be no more than 30 nodes.
- **All nodes were deleted** — in an empty list the local node “Local” (`localhost:80`) is already shown.
- **The panel is inconvenient on a phone** — mobile layout is not supported.

## Related

- [Master node settings](master-settings.md)
- [MAP](map.md)