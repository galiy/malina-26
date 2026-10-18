# Events

![Event manager](../img/events/screen.png)

## When to come here

- You need the device to perform an action under a certain condition: send an email, switch a relay, stop or start the MAP, send a notification to an external receiver.
- You need to edit or delete an existing rule.

## What you will see

The list of rules in a table and the “Add event” button. For each rule the source, condition, action, hold (seconds), and the “On” switch are visible. At the bottom — “Save” and “Cancel”.

## How to add a rule

1. Open “System → Event manager”.
2. Press “Add event”.
3. Choose whether the rule is enabled.
4. Choose a source: MAP or KES, and specify the serial number.
5. Choose the event type:
   - **Parameter** — comparing a numeric parameter with a threshold;
   - **Time** — triggering at a specified time `HH:MM`;
   - **Mode** — triggering at a certain MAP mode.
6. Set up the condition: a parameter and comparison (≤, ≥, =, ≠), or a time, or a mode.
7. Choose an action.
8. If necessary, specify the recipient and the hold (seconds).
9. Press “Save”.

Actions: email, turning a relay on/off, “Stop gen”, “Stop MAP”, “Start MAP”, SNMP notification.

- For email you can specify your own recipient; empty — the address from the mail settings.
- For relays and MAP commands you can specify a recipient; empty — the rule source.
- For an SNMP notification the receiver IP is specified.
- The hold (seconds) sets the minimum interval between triggers; the default is 30 seconds.

The “Cancel” button returns the page to the saved state. “✕” deletes a row, “Clear all” empties the table — changes take effect after “Save”.

## How it works

- Rules are checked roughly once every 30 seconds — the trigger may be delayed.
- For a parameter, the action runs when the condition is entered. On return to normal, the email reports it; an SNMP notification about the return is sent if such a notification is provided for the parameter. Relays and MAP commands have no reverse action.
- For time, the rule triggers in the specified minute. After saving it waits for the next occurrence.
- For mode, the action runs when the mode matches. For relays and MAP commands there is no reverse action; if the action is an email, when leaving the mode a separate notification arrives.

## How to check it worked

- After saving a success message appears, the rule stays in the list.
- When the condition occurs the action runs; the check happens roughly once every 30 seconds.
- The email arrives at the specified address; the relay changes state; a record about the trigger appears in the log.

## If it did not work

- **The rule does not trigger** — check whether it is enabled, whether the device is reachable, whether the serial number is correct, and whether the hold (seconds) is running. Wait up to 30 seconds.
- **The time did not trigger right after saving** — this is by design; wait for the next occurrence.
- **The relay switched and did not return** — there is no reverse action; add a separate rule.
- **The email did not arrive** — check the mail settings, the recipient, and the outbox.
- **An action on the KES does not work** — relays and MAP commands for the KES are not supported yet; SNMP notifications are sent.
- **“Stop gen” is ignored** — a new enough MAP firmware is required.
- **A save error** — read the text: it is usually a non-numeric serial number, an empty value, or an incorrect time.
- **Cannot save an empty list** — use “Clear all” and confirm.

## Related

- [E-Mail](email.md)
- [MAP](map.md)
- [KES](kes.md)