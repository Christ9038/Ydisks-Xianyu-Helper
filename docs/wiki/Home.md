# Ydisks Xianyu Helper Wiki

[中文](Home.zh-CN) | English

This Wiki is for operators of the management console. Start with [Deployment and Database](Deployment-and-Database) and [Account Management](Account-Management), then configure inventory, products, delivery templates, chat, account tasks, transaction automation, AI, and notifications.

## Recommended launch path

1. Choose a deployment method: Docker Compose, a Linux package, a Windows package, a macOS package, or source execution.
2. For Docker Compose, set `POSTGRES_*`, `DATABASE_URL`, `XIANYU_DATA_KEY`, and `XIANYU_ADMIN_PASSWORD`. For other methods, open the management page after starting the service and set the password on the first-run initialization page.
3. Set and back up `XIANYU_DATA_KEY`; the default administrator username is `admin`.
4. Use QR login in Account Management and confirm that the account is enabled and online.
5. Create the content to be delivered in Card Inventory.
6. Sync existing products or publish products from Product Management.
7. Optional: create ordered multi-message templates in Delivery Templates and bind them to paid-delivery or review-gift rules.
8. Confirm sessions and history in Chat. Send a test text or image when needed.
9. Configure automatic reviews and daily listing refresh under Account Management. Add price-change, paid-delivery, review-gift, or review-reminder rules under Automation.
10. Optional: configure a model under System and AI, then enable AI for each account.
11. Create and bind notification channels, then send a test notification.
12. Use a low-value test order to verify card delivery and inventory deduction; when the account enables Automatic Shipping Confirmation, also verify that the platform status changes to shipped before listing real products.

For desktop packages, the management URL is `http://127.0.0.1:59188`. Windows and macOS packages include the backend service, tray/menu-bar controller, and matching Playwright driver and Chromium. Linux packages include the same browser runtime and only add system dependencies during installation. The desktop controller stops the backend before it exits.

The sidebar footer shows the running version and short commit ID. Source runs normally show `dev`/`unknown`; release packages and Docker images show build-injected version and commit information.

Windows registers the `YdisksXianyuHelper` service. macOS provides Apple Silicon (`arm64`) and Intel (`amd64`) packages and registers `com.ydisks.xianyu-helper.server` and `com.ydisks.xianyu-helper.tray`. Linux packages are distributed for `amd64` and `arm64`; `sudo ./install.sh` registers `ydisks-xianyu-helper.service`. All three package types prepare matching Chromium during the build and do not depend on a Debian repository browser.

## Read these pages first

- [Deployment and Database](Deployment-and-Database)
- [Dashboard](Dashboard)
- [Account Management](Account-Management)
- [Card Inventory](Card-Inventory)
- [Product Management](Product-Management)
- [Orders](Orders)
- [Delivery Templates](Delivery-Templates)
- [Automated Delivery and Replies](Automated-Delivery-and-Replies)
- [Chat and Account Automation](Chat-and-Account-Automation)
- [Bulk Listing](Bulk-Listing)
- [AI, Notifications and Operations](AI-Notifications-and-Operations)
- [System and AI](System-and-AI)
- [Accounts, Inventory and Products](Accounts-Inventory-and-Products)
- [Resource Library](Resource-Library)
- [Sales Statistics](Sales-Statistics) (admin)
- [Runtime Logs](Runtime-Logs) (admin)
- [FAQ](FAQ)
- [Risk-Control Troubleshooting](Risk-Control-Troubleshooting) — Cookie flagged by Xianyu? Start here.

## Essential security rules

- Never commit `.env`, database backups, Cookies, API keys, or notification Webhook URLs.
- `XIANYU_DATA_KEY` encrypts Cookies, passwords, AI/SMTP settings, and notification credentials. Changing it makes existing encrypted data unreadable. Back it up offline together with the database.
- Test automation with test products, test cards, and low-value orders first. When inventory is empty, an account is offline, or risk controls are triggered, do not assume delivery completed.
- This is an unofficial tool. Make sure your use complies with platform rules, local law, and the authorization scope of the account and data.

## GitHub Wiki synchronization

The Markdown files in `docs/wiki/` are the single source of truth. English pages use the default filenames and Chinese pages use the `.zh-CN` suffix; `_Sidebar.md` is the Wiki navigation. Changes on `main` are mirrored to the GitHub Wiki by `.github/workflows/sync-wiki.yml`, and the workflow can also be run manually from GitHub Actions. Keeping the source in the repository makes it reviewable in Pull Requests and version-controlled; direct editing in the GitHub Wiki repository is discouraged.

The Pages site is published from `/docs` on `main`, with the domain configured in `docs/CNAME`. Pages publishing and GitHub Wiki synchronization are separate workflows.
