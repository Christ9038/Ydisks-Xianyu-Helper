# FAQ (Q&A from Issues)

[中文](常见问题.zh-CN) | English

Consultation-type questions collected from the issue tracker, grouped by topic. For Cookie / risk-control problems see [Risk-Control Troubleshooting](Risk-Control-Troubleshooting). Issue numbers are given in parentheses. "Fixed in vX" means upgrade to that version or later.

## Installation, upgrade and accounts

**Q: Which one is the administrator account? I forgot the password.**
The username is `admin`. The first-run page asks you to set and confirm the password. To reset: `go run ./cmd/server -init-admin -db <database path> -admin-password '<new password>'`; Docker Compose users set `XIANYU_ADMIN_PASSWORD` (#70).

**Q: How do I upgrade a Docker deployment? Is my data kept?**
Pull the new image, delete the old container and recreate it with the same parameters (with Compose: `docker compose pull && docker compose up -d`; with Portainer: pull the image and *Recreate*). Data lives in the mounted volume/database, so it is kept as long as you reuse the same volume and `XIANYU_DATA_KEY` (#59).

**Q: Where should I deploy it?**
At home on a NAS / soft router / home server with Docker. Data-center IPs are easier to flag (#100).

**Q: Can I use MySQL? Is there a WeChat group?**
MySQL and PostgreSQL are supported besides SQLite (see [Deployment and Database](Deployment-and-Database)). There is only a Telegram group, no WeChat group (#27).

**Q: The macOS installer fails.**
The package may be unsigned. Open *System Settings → Privacy & Security* and allow the blocked installer, then install again. When reporting, include macOS version, chip (arm64/Intel), package file name and the full error text (#58).

**Q: The Windows installer says "Windows service registration failed" when updating.**
The issue has no confirmed resolution yet. Try running the installer as administrator with the old `YdisksXianyuHelper` service stopped, and if it still fails, open an issue with the full error text (#26).

**Q: Notification test says "access to non-public address denied".**
The built-in safety policy blocks webhook targets that resolve to private IPs. With a proxy using fake-IP DNS (e.g. mihomo), add your notification server's domain to the fake-ip filter (#9).

**Q: The browser notification says "permission denied", or has no sound.**
Allow notifications for the site in the browser's site settings (browsers generally require HTTPS or `localhost`). The notification sound switch is under *System Settings → Basic Settings* (#78, #95).

**Q: How does the program receive and send messages? Does it use the official API?**
It follows the official web version's protocol (WebSocket + MTOP), not a headless browser polling pages (#17).

## Products, inventory and delivery

**Q: I can't delete a card stock, saving an API card's headers shows nothing when reopened.**
Stored API-card secrets are deliberately not echoed back to the page for security; re-enter them when editing (#60). Deleting a card stock that was bound to a deleted rule was fixed in v1.0.8 (#31).

**Q: Multi-spec products never match auto delivery.**
Rules must match the *complete* SKU combination of the order. Matching of multi-spec rules was fixed in v1.0.6 (#16). Quantity display was fixed in v1.0.7 (#24).

**Q: The "automatic rules" account dropdown is empty.**
Improved in v1.0.6; make sure you have created a card stock and the account is enabled (#20).

**Q: Can I deliver from a card dealer's API?**
Yes, since v1.0.5 an API card/auto-delivery source is supported (#12, #15).

**Q: Haggle ("小刀") orders are not delivered / need "free group" (免拼).**
Since v1.0.13 "Auto free-group" is a separate switch in Account Management. The order is: waiting-for-haggle message → free-group (if enabled) → only after the "haggle succeeded, awaiting shipment" message does delivery happen, then optional shipping confirmation (#49).

**Q: Why was a ¥0 service/appointment order not delivered?**
Paid auto-delivery has an amount gate (#104) so ¥0 orders do not receive cards.

**Q: Publishing fails with `INVALID_MAP_LOCATION`.**
Physical goods require a location; virtual goods usually do not. Newer versions let you choose the shipping region when publishing (#1).

**Q: What is the manual order import template?**
It is a historical leftover; use order sync instead (#36).

**Q: Default reply also triggers when I am the buyer.**
Account default replies apply to the account's chats; limit them via rules where possible (#37 is open for improvement).

## Reviews and AI

**Q: Auto-review stopped working.**
Known causes already fixed: confirm-receipt notices parsed the stage code as the order number (#76, #81, #83), and a 200-order candidate window filled with already reviewed orders (#96). Upgrade to the latest version. Orders older than 30 days cannot be reviewed on the platform (#42).

**Q: Enabling the "ask for review when overdue" rule sent ~100 messages at once.**
Fixed: the review-request rule now has a lookback window ("only orders shipped within the last N hours"); older orders are never messaged when you re-enable the rule (#86). See [Automated Delivery and Replies](Automated-Delivery-and-Replies).

**Q: AI reply does nothing / model test succeeds but no replies.**
Use an OpenAI-compatible base URL that ends with `/v1`, and run the AI model test (v1.0.13+). Currently AI reply is for automatic haggling answers only, not general questions (#50).

**Q: Can I get notified when a buyer sends "talk to a human"?**
Buyer-message pushes still come from the Xianyu app; keyword notification is not implemented (#34).
