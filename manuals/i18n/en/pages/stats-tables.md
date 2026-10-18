# Statistics: tables

![Tables: exact values](../img/stats-tables/screen.png)

## When to come here

- You need exact values and time, not a general trend.
- You need to see all device fields, including discrete and service ones.
- You need to understand a specific snapshot or error.

## What you will see

Device tabs, period buttons, “from/to” fields, and a table. The table has a time column, a version column (for BMS — the number of cells), and parameter columns. The second header row is the units.

Above the table the available edge points are shown: separately for detailed and averaged data.

## How to view values

1. Choose a device.
2. Choose a period: “24 hours”, “Today”, “Yesterday”, “7 days”, “Month”, “Latest” — or specify an interval.
3. Press “Show”.

The table shows 200 rows at a time. The “Up” button loads the previous 200 rows, “Down” — the next. A single row at the junction may repeat — this is done so as not to lose data.

## Detailed and averaged data

The “Detailed data” checkbox in the top right:

- unchecked — averaged rows are shown;
- checked — detailed and averaged, detailed take priority.

The detailed history is shorter and is lost when the device reboots. The averaged one is stored longer.

If there is no averaged data yet, the table shows detailed data on its own.

## Column explanations

Click on a column header — the parameter description appears. Clicking again or clicking outside the description closes it.

A dash means “no data”, not zero. Relays are shown as 0/1; inverter alarm words — in hexadecimal.

## Why “from/to” may be narrower than the period

After loading, the “from/to” fields show the actual boundaries of the 200 rows displayed, not the selected period. This is not an error. To get to fresh data, press “Down” or choose “Latest”.

## How to check it worked

- Rows for the selected period appeared in the table.
- The time column and values match the selected device.
- “Up” and “Down” page through the history.

## If it did not work

- **The table opened on a short interval** — this is the first page of 200 rows. Press “Down” or “Latest”.
- **The values are too sparse** — enable “Detailed data”.
- **No needed device** — it has not refreshed for more than an hour. Enable “Show inactive devices”.
- **“No data”** — the device is silent or an empty period is selected.
- **“Up” is inactive** — the upper edge of the available data has been reached.
- **Detailed history lost** — it is stored temporarily and is lost when the device reboots.
- **A dash in a cell** — there is no value, sensor, or dependent field.

## Related

- [Statistics: charts](stats-charts.md)
- [System parameters](system.md)