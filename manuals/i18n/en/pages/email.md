# E-Mail

![E-Mail settings](../img/email/screen.png)

## When to come here

- You need the device to send notifications by e-mail.
- You need to check that the mail is configured correctly.
- You need to see why an email is not being sent.

The user does not write emails manually: the mail is needed for notifications from “Events”.

## How to configure

1. Open “System → E-Mail settings”.
2. Choose a service: Mail.ru, Yandex Mail, Gmail, hoster.by, QQ Mail, or “Other SMTP”.
3. Specify the sender and recipient addresses.
4. Paste the app password (key). This is not your regular mail password.
5. For “Other SMTP”, additionally fill in the login, server, port, and security.
6. Press “Save”.
7. Press “Send test” and check the recipient's inbox.

Below the form there is a hint on how to get an app password for the selected service.

For well-known services the server, port, and security are filled in automatically. For your own server, SSL, STARTTLS, and operation without encryption are available.

## Outbox

Below, letters that have not been sent yet or whose sending failed are shown: time, subject, recipient, number of attempts, and the last error. Successfully sent letters disappear from the list.

A row can be deleted, and the whole queue can be cleared. The queue itself does not refresh on its own: the page is re-read after actions.

The queue is limited to 200 letters: on overflow the oldest one is removed. After a device reboot the queue is cleared.

## How to check it worked

- After saving the “Saved” message appears.
- The test letter arrives at the recipient's address.
- The queue is empty or shows only letters with an understandable error.

## If it did not work

- **Authorization error** — an app password is required, not your mail password. Check that access for mail programs is enabled.
- **Connection error** — check the network and the mail server reachability; for your own server — host, port, and security.
- **The test passed but notifications do not arrive** — check the recipient in the “Events” rule and the mail settings; look at the outbox and its errors.
- **A letter is stuck in the outbox** — look at “Error” and “Attempts”; fix the settings or delete the letter.
- **The queue is empty after a reboot** — this is by design.
- **“Invalid address” / “Server not set”** — check the corresponding fields.

## Related

- [Events](events.md)
- [System parameters](system.md)