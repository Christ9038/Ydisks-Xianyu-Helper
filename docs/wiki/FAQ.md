# FAQ

[中文](常见问题.zh-CN) | English

If your account is risk-controlled or QR login keeps saying "security verification required", go straight to [What to do when your account is risk-controlled](Risk-Control-Troubleshooting). Everything else people commonly ask is below.

## Installing and upgrading

**Which account is the administrator? I forgot the password.**
The username is always `admin`; the password is the one you set the first time you opened the console. To reset it from the command line:
`go run ./cmd/server -init-admin -db <database path> -admin-password 'new password'`. Docker users run `/app/xianyu-server -init-admin -admin-password 'new password'` inside the container; the bundled `docker-compose.debian13-postgres17.yml` has an `init-admin` service (profile `init`) that reads `XIANYU_ADMIN_PASSWORD` and performs the same reset. Changing the environment variable alone, without running `-init-admin`, does not change an existing administrator's password (`-ensure-admin` only creates the administrator when it is missing).

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
When "Restrict user-configured HTTP outbound requests to public addresses" is enabled in System and AI (off by default), the program refuses notification URLs that resolve to a private IP; the switch covers API delivery, AI, HTTP notifications, remote images and the remote slider service, while email (SMTP) notifications and image-URL downloads for bulk listing are not affected by the switch and always connect to public addresses only. The usual cause is a proxy in fake-ip mode (mihomo, for example) answering your notification server's domain with a private address. Add that domain to the proxy's fake-ip filter.

**Browser notifications say "permission denied", or play no sound.**
Allow notifications for this address in the browser's site settings (browsers generally require HTTPS or `localhost`). The sound switch is "New-message browser notification sound" under *System and AI → Basic settings* (it only applies to the current browser, requires browser reminders to be enabled on the Online Chat page first, and that page is visible to administrators only).

**How does it send and receive messages? The official API?**
It follows Xianyu's own web version protocol (WebSocket plus the MTOP interfaces). It does not drive a headless browser to scrape pages.

## Products, cards and delivery

**I saved an API card's headers, but I cannot see them in the list.**
That is on purpose: API cards may contain third-party keys, so card lists and details never return them. To review or change them, open the card's API editor (owner only, read on demand, not cached); secret inputs are masked until you click to reveal, and the draft is cleared when the dialog closes or the card switches.

**A multi-spec product never matches auto delivery.**
The rule has to match the order's **complete spec combination**; the program never guesses from a partial match. Upgrade to a recent version and check that every spec combination has a rule.

**I can't pick an account in the automation rules.**
The account drop-down lists all accounts owned by the signed-in user and is not filtered by enabled state or card stock. Check that the account was added under the signed-in user, then reload the page.

**Can I connect a card dealer's API for automatic delivery?**
Yes. Choose the API type in Card Inventory and fill in the request template.

**Two-person haggle orders are never delivered, I need free-group (免拼).**
Free-group is a separate switch in Account Management. The flow: when the "waiting for haggle to complete" message arrives, free-group runs once if it is enabled; the card is sent only after the "haggle succeeded, awaiting shipment" message arrives; after the card is sent, shipping is confirmed according to the "auto confirm shipment" switch. Without free-group the account simply waits for the "awaiting shipment" stage before sending the card.

**Why was a ¥0 appointment/service order not delivered?**
By design: paid auto delivery checks the deal amount, and ¥0 orders never receive a card, to avoid mistaken delivery.

**Publishing fails with "the system cannot locate your administrative area".**
Physical goods need a location; virtual goods usually don't. Newer versions let you choose the shipping region when publishing.

**Where do I download the "import orders" spreadsheet template?**
There isn't one; the feature has been removed. The front end no longer offers it, and the old import endpoint always returns 501 ("manual order insertion has been removed, please use one-click order sync"). Just use "Sync orders".

**The account's default reply also fires when I am the buyer.**
Auto replies (keyword, API, AI and default reply) fire only when the chat's item is in the account's local item list and the account is the seller of that chat; buyer-side chats, or items not yet synced into the local list, are saved but never auto-replied. So asking about someone else's item from your own account as a buyer does not trigger it; if an item has not been synced yet, sync items first.

## Reviews and AI

**Auto review suddenly stopped working.**
Upgrade first. Two bugs were fixed: the order number in the "received" notice was parsed wrongly, and the review candidate list only looked at the first 200 orders, so once those were all reviewed new orders could never get in. Candidates are now only locally confirmed-received orders that have no review run yet (or can still be retried), at most 200 per round. Also, Xianyu itself no longer accepts reviews for orders older than 30 days; the system treats that as a non-retryable failure.

**After re-enabling the "ask for review" rule it messaged a pile of old orders.**
Fixed. The rule now has "only orders shipped within the last N hours"; older orders are left alone. See [Automated Delivery and Replies](Automated-Delivery-and-Replies).

**AI reply does nothing even though the model test succeeds.**
Check three things: the API URL must be OpenAI-compatible, usually ending in `/v1`; run "Test connection" in AI settings; and the AI mode defaults to "haggle only" (AI is called only when a haggling intent is detected; other messages keep using the default reply), so for other questions switch the AI mode in the account's AI settings in Account Management to "full customer service". Keyword replies still take priority over AI.

**Can I get notified when a buyer sends something like "talk to a human"?**
Not at the moment. Buyer message alerts still come from the Xianyu app's own push; there is no keyword notification in this program.
