# FAQ

[中文](常见问题.zh-CN) | English

If your account is risk-controlled or QR login keeps saying "security verification required", go straight to [What to do when your account is risk-controlled](Risk-Control-Troubleshooting). Everything else people commonly ask is below.

## Installing and upgrading

**Which account is the administrator? I forgot the password.**
The username is always `admin`; the password is the one you set the first time you opened the console. To reset it from the command line:
`go run ./cmd/server -init-admin -db <database path> -admin-password 'new password'`. Docker users just change `XIANYU_ADMIN_PASSWORD`.

**How do I upgrade Docker? Will I lose data?**
No. Pull the latest image, remove the old container and create it again with the same parameters. With Compose run `docker compose pull && docker compose up -d`; with Portainer pull the image and click Recreate. As long as you keep the same data volume and `XIANYU_DATA_KEY`, your accounts and orders stay. Note that redeploying without pulling a new image just runs the old version again.

**Where should I run it?**
At home if you can: a NAS or a soft router with Docker. Cloud and data-center IPs are easy for Xianyu's risk control to flag.

**Can I use MySQL?**
Yes. Besides SQLite, MySQL and PostgreSQL are supported; see [Deployment and Database](Deployment-and-Database). The community group is on Telegram only, there is no WeChat group.

**The macOS installer won't install.**
An unsigned installer gets blocked by the system. Open *System Settings → Privacy & Security*, find the blocked item and click "Open Anyway", then install again. If it still fails, when reporting include your macOS version, chip (Apple silicon or Intel), the installer file name and the full error.

**Updating on Windows says "Windows service registration failed".**
We have no confirmed cause yet. Try running the installer as administrator after stopping the old `YdisksXianyuHelper` service; if it still fails, open an issue with the full error.

**The notification test says "access to non-public address denied".**
For safety the program refuses notification URLs that resolve to a private IP. The usual cause is a proxy in fake-ip mode (mihomo, for example) answering your notification server's domain with a private address. Add that domain to the proxy's fake-ip filter.

**Browser notifications say "permission denied", or play no sound.**
Allow notifications for this address in the browser's site settings (browsers generally require HTTPS or `localhost`). The sound switch is under *System Settings → Basic Settings*.

**How does it send and receive messages? The official API?**
It follows Xianyu's own web version protocol (WebSocket plus the MTOP interfaces). It does not drive a headless browser to scrape pages.

## Products, cards and delivery

**I saved an API card's headers, but they are gone when I reopen it.**
That is on purpose. API cards can contain third-party keys, so they are never shown on the page again; just type them in again when editing.

**A multi-spec product never matches auto delivery.**
The rule has to match the order's **complete spec combination**; the program never guesses from a partial match. Upgrade to a recent version and check that every spec combination has a rule.

**I can't pick an account in the automation rules.**
Make sure a card stock exists and the account is enabled.

**Can I connect a card dealer's API for automatic delivery?**
Yes. Choose the API type in Card Inventory and fill in the request template.

**Two-person haggle orders are never delivered, I need free-group (免拼).**
Free-group is a separate switch in Account Management. The flow: when the "waiting for haggle to complete" message arrives, free-group runs once if it is enabled; the card is sent only after the "haggle succeeded, awaiting shipment" message arrives; after the card is sent, shipping is confirmed according to the "auto confirm shipment" switch. Without free-group the account simply waits for the "awaiting shipment" stage before sending the card.

**Why was a ¥0 appointment/service order not delivered?**
By design: paid auto delivery checks the deal amount, and ¥0 orders never receive a card, to avoid mistaken delivery.

**Publishing fails with "the system cannot locate your administrative area".**
Physical goods need a location; virtual goods usually don't. Newer versions let you choose the shipping region when publishing.

**Where do I download the "import orders" spreadsheet template?**
There isn't one; it is a leftover feature. Just use "Sync orders".

**The account's default reply also fires when I am the buyer.**
Default replies apply per account and can't tell roles apart yet. Use keyword rules to narrow the scope.

## Reviews and AI

**Auto review suddenly stopped working.**
Upgrade first. Two bugs were fixed: the order number in the "received" notice was parsed wrongly, and the review candidate list only looked at the first 200 orders, so once those were all reviewed new orders could never get in. Also, Xianyu itself no longer accepts reviews for orders older than 30 days.

**After re-enabling the "ask for review" rule it messaged a pile of old orders.**
Fixed. The rule now has "only orders shipped within the last N hours"; older orders are left alone. See [Automated Delivery and Replies](Automated-Delivery-and-Replies).

**AI reply does nothing even though the model test succeeds.**
Check three things: the API URL must be OpenAI-compatible, usually ending in `/v1`; run "Test connection" in AI settings; and AI replies mainly handle haggling, so for other questions switch the account's AI mode to "full customer service".

**Can I get notified when a buyer sends something like "talk to a human"?**
Not at the moment. Buyer message alerts still come from the Xianyu app's own push; there is no keyword notification in this program.
