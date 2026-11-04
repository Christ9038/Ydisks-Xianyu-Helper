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

The sidebar footer shows the running version and short commit ID. Source runs normally show `dev`/`unknown`; release packages and Docker images show build-injected version and commit information (the commit ID is shown with at most its first 12 characters; a release version such as `1.2.3` is displayed as `v1.2.3`).

Windows registers the `YdisksXianyuHelper` service. macOS provides Apple Silicon (`arm64`) and Intel (`amd64`) packages and registers `com.ydisks.xianyu-helper.server` and `com.ydisks.xianyu-helper.tray`. Linux packages are distributed for `amd64` and `arm64`; `sudo ./install.sh` registers `ydisks-xianyu-helper.service`. All three package types prepare matching Chromium during the build and do not depend on a Debian repository browser.

## Read these pages first

- [Deployment and Database](Deployment-and-Database)
- [Dashboard](Dashboard)
- [Account Management](Account-Management)
- [Card Inventory](Card-Inventory)
- [Product Management](Product-Management) (sidebar entry: “商品列表”, Product List)
- [Orders](Orders)
- [Delivery Templates](Delivery-Templates)
- [Automated Delivery and Replies](Automated-Delivery-and-Replies) (sidebar entry: “自动化规则”, Automation Rules)
- [Chat and Account Automation](Chat-and-Account-Automation)
- [Bulk Listing](Bulk-Listing)
- [AI, Notifications and Operations](AI-Notifications-and-Operations) (sidebar entry: “通知设置”, Notification Settings)
- [System and AI](System-and-AI)
- [Accounts, Inventory and Products](Accounts-Inventory-and-Products) (pre-launch overview)
- [Resource Library](Resource-Library)
- [Sales Statistics](Sales-Statistics)
- [Runtime Logs](Runtime-Logs) (admin)
- [Bargain Orders and Delivery Flow](Bargain-Orders-and-Delivery-Flow)
- [Request Pacing and Risk Control](Request-Pacing-and-Risk-Control)
- [Notification Events](Notification-Events)
- [Admin API](Admin-API)
- [Configuration Reference](Configuration-Reference) (flags and environment variables)
- [FAQ](FAQ)
- [Risk-Control Troubleshooting](Risk-Control-Troubleshooting) — Cookie flagged by Xianyu? Start here.

## Essential security rules

- Never commit `.env`, database backups, Cookies, API keys, or notification Webhook URLs.
- `XIANYU_DATA_KEY` encrypts Cookies, passwords, AI/SMTP settings, and notification credentials. Changing it makes existing encrypted data unreadable. Back it up offline together with the database.
- Test automation with test products, test cards, and low-value orders first. When inventory is empty, an account is offline, or risk controls are triggered, do not assume delivery completed.
- This is an unofficial tool. Make sure your use complies with platform rules, local law, and the authorization scope of the account and data.

## GitHub Wiki synchronization

The Markdown files in `docs/wiki/` are the single source of truth. English pages use the default filenames and Chinese pages use the `.zh-CN` suffix; `_Sidebar.md` is the Wiki navigation. Changes on `main` are mirrored to the GitHub Wiki by `.github/workflows/sync-wiki.yml`, and the workflow can also be run manually from GitHub Actions. Keeping the source in the repository makes it reviewable in Pull Requests and version-controlled; direct editing in the GitHub Wiki repository is discouraged.

The online user-manual site also uses `docs/wiki/` as its only content source. `manual/` is a VitePress site; before each build, `manual/scripts/prepare.mjs` generates its pages from `docs/wiki/` (Chinese at the root path, English under `/en/`, with this “GitHub Wiki synchronization” section removed) and the build output is served under `/manual/`. `.github/workflows/manual.yml` runs on changes to `docs/**`, `manual/**`, or the workflow file on `main`, and can also be run manually; Pull Requests touching `docs/wiki/**`, `manual/**`, or the workflow file only build (including an internal dead-link check) and do not deploy. The build job uses Node.js 24 to run `npm ci --prefix manual` and `npm run build --prefix manual`, then assembles the Pages site from `docs/index.html`, `docs/CNAME`, `docs/assets`, `docs/img`, and the manual output (placed under `/manual/`); the deploy job publishes it to GitHub Pages with `actions/deploy-pages`, using the domain configured in `docs/CNAME`.

The deploy job takes effect only after Settings → Pages → Source is switched to “GitHub Actions” in the repository. The site (home page and `/manual/`) is then published as a whole by this workflow instead of from the `/docs` directory of `main`. GitHub Wiki synchronization (`sync-wiki.yml`) and Pages publishing (`manual.yml`) are separate workflows; editing `docs/wiki/` triggers each of them independently.
