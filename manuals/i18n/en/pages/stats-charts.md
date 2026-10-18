# Statistics: charts

![Charts: parameter dynamics](../img/stats-charts/screen.png)

## When to come here

- You need to see how parameters changed over time.
- You need to compare the operation of several grid-tie inverters.
- You need to watch the device in real time.

## What you will see

At the top — device tabs and period buttons. Below — chart cards. Each device type has its own set of cards: voltages, currents, powers, temperatures, etc.

If a device has not refreshed for more than an hour, its tab is hidden. To see all devices, enable “Show inactive devices”.

## How to build a chart

1. Choose a device on its tab.
2. Choose a period: “24 hours”, “Today”, “Yesterday”, “7 days”, “Month”, “Latest” — or specify an “from/to” interval.
3. If necessary, press “Show”.

“Month” is 30 days, not a calendar month.

## What you can do with a chart

- **Hide lines** — click on a label in the legend. Double-click leaves only that line, the “All” button brings all back.
- **Change the card order** — with the arrows in the card corner.
- **Zoom in** — `Ctrl` + mouse wheel or `Ctrl` + selecting an area. On a touch screen — pinch.
- **Shift in time** — by mouse drag or swipe.
- **Reset the zoom** — double-tap on a touch screen; on a computer — choose the period again.
- **Read values** — hover the mouse and hold it for a moment; on a touch screen hold a finger. The cursor is synchronized across all charts.

The minimum scale is 5 minutes. Too-short selections are ignored.

## Real-time mode

The “ONLINE” checkbox in the top right enables following the latest data. The window depth is 1 to 10 minutes, 5 is the default. Data refreshes once a second. The period buttons, “Refresh”, and “Compare” are unavailable in this mode.

The mode turns off when the device changes, when zooming, or on manual actions.

## Comparing inverters

For grid-tie inverters there is a “Compare” checkbox. It builds charts of active and reactive powers per inverter and the sum of active powers — total and by location.

If an inverter has no data at the same moment, the total line is not drawn at that point.

## How to check it worked

- Lines for the selected period appeared on the cards.
- Hovering shows the values at the selected moment.
- When the period or device changes, the charts rebuild.

## If it did not work

- **No needed device** — it has not refreshed for more than an hour. Enable “Show inactive devices”.
- **“No data”** — the device is silent or an empty period is selected. Try “Latest”.
- **The chart looks “sparse”** — on a wide period the points are thinned. Zoom into an area to see details.
- **The zoom was reset** — the period buttons and “Show” reset the scale. To refresh without resetting, use “Refresh”.
- **Some lines are missing** — there is no data for those parameters yet.
- **The tooltip does not appear** — hold the mouse still or hold the finger without moving.
- **Detailed history lost after a reboot** — detailed data is stored temporarily and is lost on reboot; the averaged history remains.

## Related

- [Statistics: tables](stats-tables.md)
- [System parameters](system.md)