# Ydisks Xianyu Helper

An Xianyu multi-account management, messaging, and automated-delivery system built with Go and React.

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)
![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-17-4169E1?logo=postgresql&logoColor=white)
[![Release](https://github.com/Christ9038/Ydisks-Xianyu-Helper/actions/workflows/release.yml/badge.svg)](https://github.com/Christ9038/Ydisks-Xianyu-Helper/actions/workflows/release.yml)

English documentation · [中文 README](README.zh-CN.md) · [English Wiki](docs/wiki/Home.md) ·
[中文 Wiki](docs/wiki/Home.zh-CN.md) · [Contributing](CONTRIBUTING.md) ·
[Security policy](SECURITY.md) · [Code of Conduct](CODE_OF_CONDUCT.md) · [License](#license)

The [English Wiki](docs/wiki/Home.md) is the default operational guide. The [Chinese Wiki](docs/wiki/Home.zh-CN.md)
is available for the primary Chinese-speaking Xianyu community.

> [!IMPORTANT]
> This project is for personal technical learning and research only. It has no official authorization from Xianyu
> or Alibaba.
>
> This is an unofficial community tool and has no affiliation, partnership, or authorization relationship with Xianyu,
> Alibaba Group, or their affiliates.
>
> Use it only in compliance with applicable law, platform rules, and the scope of your account authorization. Users
> are solely responsible for all consequences of using this code; the author assumes no responsibility.
>
> If the platform requests that this project be taken down, it may be deleted or archived at any time.
>
> Do not restart the program frequently while accounts are logged in. Every restart requests login credentials from
> Xianyu, and repeated requests may trigger risk controls.

## Overview

Ydisks Xianyu Helper is a self-hosted management system for Xianyu sellers. It combines account runtimes, instant
messaging, orders, products, card inventory, automation rules, AI replies, and exception notifications in one web
console. It is intended for individuals and small teams managing multiple Xianyu accounts or delivering virtual goods.

The Go client implements Xianyu login, Cookie renewal, MTOP requests, and the WebSocket message path. QR login,
face-verification flows, message connections, credential renewal, and most business logic are handled by the Go client.
Chromium is used for features that require a browser environment.

### Use with Ydisks Referral Assistant

If you promote Xianyu products through cloud-drive resources, tutorials, or digital packages, you can use
[Ydisks Referral Assistant](https://www.ydisks.com) alongside this project. Create separate short links for products,
channels, or promotion accounts, then inspect PV, UV, trends, rankings, and channel/account reports in the Ydisks
console.

Ydisks identifies where visits and conversion leads come from, while this project handles account messages, orders,
and automated delivery. See the [Ydisks Referral Assistant guide](https://docs.ydisks.com/guide/quick-start) to create
links and view data.

### Use cases

- Unified online status and message management for multiple Xianyu accounts
- Automated delivery of digital goods, redemption codes, links, or images
- Paid delivery, staged bargain free shipping, review gifts, overdue review reminders, and unpaid-order price changes
- Product and order synchronization, list-based single- or multi-variant detection and publishing, and CSV/XLSX/TSV
  bulk listing with optional ZIP images
- Controlled AI customer service through OpenAI-compatible endpoints, with an end-to-end connection test

### Community

![Ydisks Xianyu Helper Telegram group](https://raw.githubusercontent.com/Christ9038/Ydisks-Xianyu-Helper/main/docs/img/telegram.jpg)

## Features

| Module | Capabilities |
| --- | --- |
| Account management | Multi-account start/stop, QR login, profile refresh, online status, login audit, and notes. |
| Credential renewal | Scheduled Cookie/token renewal, WebSocket credential updates, recovery, cooldowns, and failure disabling. |
| Instant messaging | Account-isolated chat, history, Xianyu emoji, text/images, official system-message recognition, keyword replies (multiple expressions, contains or regular-expression matching, multi-product scope), default replies, and one-reply limits. |
| Automation center | Unpaid-order price changes, paid delivery, staged bargain free shipping, review gifts, review reminders, failure recovery, manual-intervention notifications, and idempotency checkpoints. |
| Account tasks | Event-driven automatic reviews based on locally recorded buyer-confirmed-receipt events, daily product refresh, manual execution, execution records, and idempotency protection. |
| Card inventory | Text, batch cards, images, and API delivery, with batch import, quantity, and delay settings. |
| Product management | Synchronization, manual association, list-card `isSKU`-based single- or multi-variant detection (missing or `false` means single-variant), publishing, image ordering, category lookup, and bulk listing. |
| Delivery templates | Ordered multi-message delivery and order, buyer, card, and custom variables. |
| Orders | Synchronization, editing, platform delivery, card resending, and exception handling. |
| AI replies | OpenAI-compatible APIs, model discovery, end-to-end connection testing, Bargaining Only or Full Service modes, per-product enable/disable and product-specific reference material, custom prompts, bargaining rounds, and discount limits. |
| Notifications | Bark, DingTalk, Feishu, WeCom, Telegram, email, and custom Webhooks. |
| Storage and security | SQLite/MySQL/PostgreSQL, embedded Goose migrations, AES-256-GCM sensitive-field encryption, log redaction, and outbound-address validation. |
| Container deployment | PostgreSQL 17, health checks, persistent volumes, and multi-architecture GHCR images. |

## Architecture

~~~mermaid
flowchart LR
    UI["React admin console"] --> API["Go / chi HTTP API"]
    API --> Store["SQLite / MySQL / PostgreSQL"]
    API --> Manager["Account manager"]
    Manager --> Engine["Per-account runtime"]
    Engine --> WS["Xianyu WebSocket"]
    Engine --> MTOP["Xianyu MTOP"]
    Engine --> Automation["Automation center"]
    Automation --> Notify["Notification channels"]
    Engine -. "browser capability" .-> Chromium["Playwright / Chromium"]
~~~

Core responsibilities:

- internal/composition: the single production composition root
- internal/application: use-case orchestration, ownership checks, transactions, and compensation
- internal/xianyu: login protocol, Cookies, MTOP, WebSocket, and message protocol
- internal/engine: per-account lifecycle, message handling, replies, and delivery
- internal/automation: delivery, review gifts, reminders, and scheduling
- internal/browser: Chromium automation, browser-fingerprint reading, and token slider capabilities
- internal/db: database access, encryption, dialect-specific migrations, and repositories
- internal/server: HTTP/SPA transport, management authentication, and embedded frontend assets

## Screenshots

![Account management](https://raw.githubusercontent.com/Christ9038/Ydisks-Xianyu-Helper/main/docs/img/preview_account.png)

![Instant messaging](https://raw.githubusercontent.com/Christ9038/Ydisks-Xianyu-Helper/main/docs/img/preview_im.png)

![Automation](https://raw.githubusercontent.com/Christ9038/Ydisks-Xianyu-Helper/main/docs/img/preview_auto.png)

## Quick start

### Docker Compose (recommended)

Docker Compose supports Linux x86_64, Linux ARM64, and Apple Silicon. Docker Engine or Docker Desktop and Docker
Compose v2 are required.

~~~bash
git clone https://github.com/Christ9038/Ydisks-Xianyu-Helper.git
cd Ydisks-Xianyu-Helper
cp .env.example .env
~~~

Set at least POSTGRES_DB, POSTGRES_USER, POSTGRES_PASSWORD, DATABASE_URL, XIANYU_DATA_KEY, and
XIANYU_ADMIN_PASSWORD in .env:

~~~dotenv
POSTGRES_DB=xianyu
POSTGRES_USER=xianyu
POSTGRES_PASSWORD=replace-with-a-strong-password
DATABASE_URL=postgres://xianyu:url-encoded-password@postgres:5432/xianyu?sslmode=disable
XIANYU_DATA_KEY=replace-with-a-long-lived-random-key
XIANYU_ADMIN_PASSWORD=replace-with-an-admin-password
~~~

Generate random values with:

~~~bash
openssl rand -hex 24
openssl rand -base64 48
~~~

Start the application and PostgreSQL:

~~~bash
docker compose up -d
~~~

Open http://localhost:59188 and sign in as admin with the configured administrator password. The Compose file uses
persistent PostgreSQL, application-data, and browser-data volumes. Back up the database and .env before upgrading.

GHCR images can be pulled anonymously when public. If the image is private, log in to ghcr.io with a Personal Access
Token that has read:packages permission.

### Run from source

Requirements are Go 1.26.4 or compatible, Node.js 24 with npm, Chromium system dependencies, and optionally SQLite.

~~~bash
git clone https://github.com/Christ9038/Ydisks-Xianyu-Helper.git
cd Ydisks-Xianyu-Helper
npm --prefix frontend ci
npm --prefix frontend run build
go run ./cmd/server -db data/xianyu_data.db -addr 127.0.0.1:59188
~~~

When the database has no administrator, the first visit shows the initialization form. For headless or operational
environments:

~~~bash
go run ./cmd/server -init-admin -db data/xianyu_data.db -admin-password 'replace-with-a-strong-password'
~~~

The frontend is embedded at build time through Go go:embed. Restart the Go service after rebuilding it.

### Desktop packages

Desktop packages contain xianyu-server for the backend and Chromium, and xianyu-tray for the Windows tray or macOS
menu bar. Packages include the matching Playwright driver and Chromium runtime; users do not need to download a
second browser.

| Platform | Service | Controller | Data and logs |
| --- | --- | --- | --- |
| Windows | YdisksXianyuHelper Windows Service | xianyu-tray.exe | C:\ProgramData\YdisksXianyuHelper |
| macOS | com.ydisks.xianyu-helper.server LaunchAgent | Ydisks闲鱼助手.app menu-bar program | ~/Library/Application Support/YdisksXianyuHelper and ~/Library/Logs/YdisksXianyuHelper |
| Linux | ydisks-xianyu-helper.service systemd unit | No desktop tray | /var/lib/ydisks-xianyu-helper and /var/log/ydisks-xianyu-helper |

Windows and macOS controllers show checking, starting, healthy, and stopping states; they can control the backend
and open the log directory. Exiting the controller stops the backend first. Linux installation must run install.sh as
root on the matching architecture and uses systemd. Uninstall retains /var/lib/ydisks-xianyu-helper by default.

Formal releases provide Windows, macOS arm64/amd64, and Linux amd64/arm64 packages. Release tags use v1.2.3 format
and must point to main. The release workflow builds and tests every native architecture, launches Chromium, checks
/health, waits for production-release Environment approval, and then publishes packages, SHA256SUMS, Docker tags,
and a GitHub Release. Windows and macOS signing requires the configured certificate Secrets; Linux does not.

Local macOS packaging:

~~~bash
npm ci --prefix frontend
npm run build --prefix frontend
mkdir -p dist/macos/arm64
go build -trimpath -ldflags='-s -w' -o dist/macos/arm64/xianyu-server ./cmd/server
go build -trimpath -ldflags='-s -w' -o dist/macos/arm64/browser-install ./cmd/browser-install
go build -trimpath -ldflags='-s -w' -o dist/macos/arm64/xianyu-tray ./cmd/tray
packaging/macos/build-pkg.sh 0.0.0-local "$PWD/dist/macos" arm64
~~~

The packaging script prepares matching runtime files from the local Playwright cache when needed. Use amd64 for
Intel macOS. Without a signing identity, the result is an unsigned pkg.

The desktop first-run URL is http://127.0.0.1:59188. Set and confirm the administrator password in the web page.
Source examples using -addr :59188 listen on all interfaces; restrict the port with a firewall, security group, or
reverse proxy. Use -no-browser only when Chromium is unavailable; browser-fingerprint and token-slider features
will then be unavailable.

## First use

1. Sign in as administrator.
2. In Account Management, scan the QR code with the Xianyu app.
3. Complete face or security verification only through the official flow.
4. Wait until the account is online, then sync products and orders.
5. Create delivery content in Card Inventory.
6. Associate products, cards, specifications, and triggers in Automation.
7. Use Chat to inspect history and test text, emoji, or image messages.
8. Configure automatic reviews and daily refresh under Account Management; if needed, enable the independent bargain free-shipping switch for the “bargain pending” stage (it does not replace automatic delivery or shipment confirmation).
9. Configure keyword replies, default replies, AI replies, and notifications as needed.

The Go client currently supports QR login only. Do not share Cookies, passwords, QR codes, or verification URLs.

## Configuration

Database connection precedence is:

~~~text
DATABASE_URL > -db-url > -db
~~~

Important environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| DATABASE_URL | Empty | SQLite, MySQL, or PostgreSQL connection URL. |
| XIANYU_DATA_KEY | Empty | Long-lived encryption key for Cookies, passwords, tokens, AI, SMTP, and notification credentials. |
| XIANYU_ADMIN_PASSWORD | Empty | Docker Compose non-interactive administrator initialization. |
| XIANYU_UPLOAD_DIR | data/uploads | Bulk-listing uploads and temporary resources. |
| LOG_LEVEL | info | debug, info, warn, or error. |
| LOG_FORMAT | text | text or json. |
| BROWSER_HEADLESS | Per-account; true in Docker | Force headless Chromium on or off. |
| PLAYWRIGHT_DRIVER_PATH | Auto-discovered | Playwright driver directory. |
| PLAYWRIGHT_BROWSERS_PATH | Auto-discovered; /ms-playwright in Docker | Chromium runtime directory. |
| PLAYWRIGHT_NODEJS_PATH | Auto-discovered | Node.js executable used by Playwright. |
| PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH | Auto-discovered | External Chromium executable when required. |
| PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD | false in source; true in Docker | Skip browser download. |
| CAPTCHA_BROWSER_PROXY | Empty | Credential-free http(s), socks4, or socks5 proxy for token CAPTCHA Chromium. |
| CAPTCHA_IGNORE_CERT_ERRORS | false | Use true only in a controlled TLS-inspection environment. |
| TZ | System timezone; Asia/Shanghai in Docker | Container and log timezone. |
| XIANYU_PENDING_SHIP_CATCHUP | enabled | Stop pending-shipment catch-up and checkpoint-resume scans by setting it to `0`. |
| XIANYU_CONTINUE_AFTER_UNCERTAIN | enabled | Allow idempotent state actions after an uncertain message action; set it to `0` to keep the circuit breaker closed. |

Docker Compose also supports COMPOSE_PROJECT_NAME, POSTGRES_IMAGE, POSTGRES_DB, POSTGRES_USER, POSTGRES_PASSWORD,
XIANYU_IMAGE, XIANYU_BIND_ADDRESS, and XIANYU_HTTP_PORT. Pin XIANYU_IMAGE to a release or SHA tag in production.

The main command-line options are -db, -db-url, -addr, -web, -workdir, -playwright-runtime-root,
-playwright-driver-dir, -playwright-browser-dir, -data-key-file, -secure, -no-browser, -log-level, -log-format, -v,
-init-admin, -ensure-admin, -admin-email, -admin-password, -service, and -version. See the
[Deployment and Database Wiki](docs/wiki/Deployment-and-Database.md) for detailed tables and examples.

The management console stores account state, automation, templates, reply rules, AI settings, bargaining strategy,
notifications, logs, and administrator credentials in the database. Send data to external AI, SMTP, Webhook, or
remote verification services only when their trust and data flow are understood.

## Docker operations

The default image is multi-architecture:

~~~text
ghcr.io/christ9038/ydisks-xianyu-helper:latest
├── linux/amd64
└── linux/arm64
~~~

The dev branch publishes dev and sha-full-commit tags. A v1.2.3 tag publishes v1.2.3, 1.2.3, 1.2, latest, and
sha-full-commit tags after all tests, native Chromium launches, health checks, desktop builds, and approval.

~~~bash
docker compose up -d
docker compose ps
curl -fsS http://127.0.0.1:59188/health
docker compose logs --tail=200 app
~~~

The health response contains status, database, version, commit, and build_time. Update after backing up:

~~~bash
docker compose pull app
docker compose up -d
docker compose logs --tail=100 app
~~~

PostgreSQL backup:

~~~bash
set -a
. ./.env
set +a
docker compose exec -T postgres pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
  > "xianyu-$(date +%Y%m%d-%H%M%S).sql"
~~~

Restore only after stopping app and validating the backup. docker compose down retains volumes; do not use down -v
in production because it deletes database and application-data volumes. Use Caddy, Nginx, Traefik, or a cloud
load balancer for HTTPS and add -secure to the application command.

## Development

Repository areas include cmd/server, cmd/browser-install, cmd/tray, cmd/init-admin, cmd/dbverify, and cmd/dbseed;
internal/account, application, adapter, automation, browser, composition, db, engine, notify, renewal, server, and
xianyu; frontend; docs; packaging; and scripts.

Local commands:

~~~bash
go run ./cmd/server -db data/xianyu_data.db -addr :59188
npm --prefix frontend ci
npm --prefix frontend run dev
make frontend
make build
~~~

The Vite development server is available at http://localhost:3000 and proxies API requests to
http://localhost:59188.

Quality checks:

~~~bash
make fmt
make vet
make lint
make test
make smoke   # start the real binary and verify health, setup, login, core lists, the front page, and graceful exit
make cover
make check
npm --prefix frontend run typecheck
npm --prefix frontend test
npm --prefix frontend run build
./scripts/docker-full-test.sh
~~~

Use local fixtures, httptest, mock accounts, and local browser pages. Never use real credentials in tests.

## Troubleshooting

### Administrator initialization

Open the management page after starting the service. If no administrator exists, set and confirm a password of at
least eight characters; the system creates admin and signs in. Docker Compose requires a non-empty
XIANYU_ADMIN_PASSWORD. Headless or password-reset environments can use:

~~~bash
go run ./cmd/server -init-admin -db data/xianyu_data.db -admin-password 'new-password'
~~~

### PostgreSQL connection

Confirm that POSTGRES_DB, POSTGRES_USER, POSTGRES_PASSWORD, and DATABASE_URL match. The Compose hostname is postgres,
not localhost. URL-encode password characters such as @, :, /, and #. Confirm PostgreSQL health with docker compose ps.

### Chromium or Playwright

Prefer the project image or a standalone package, which contains the matching driver and Chromium runtime. Source
runs need permission to download the driver and browser. Set PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH for an external
browser and PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1 when appropriate. Containers need sufficient shared memory and a
writable browser_data volume.

### Offline accounts or security verification

Check Account Management status and renewal logs, complete official verification in the Xianyu app, and check server
time, timezone, and connectivity. Do not run one account in multiple instances. If verification loops, clear
credentials, disable the account, complete verification in Xianyu’s official web version, wait, and try again.

### Lost XIANYU_DATA_KEY

This is expected security behavior. Restore the original key. If it cannot be recovered, log in again and re-enter
protected keys. Do not clear a production database or bypass decryption checks.

## Security recommendations

- Expose the management console through HTTPS and restrict trusted sources.
- Use separate strong credentials for administrator, database, GHCR, AI, SMTP, and notification services.
- Keep XIANYU_DATA_KEY fixed and back it up offline with the database.
- Never commit .env, database files, Cookies, QR codes, logs, browser data, API keys, or Webhook tokens.
- Back up PostgreSQL and test restoration.
- Use version or SHA image tags in production.
- Stop an account and inspect audit and renewal logs when behavior is abnormal.
- Report vulnerabilities privately through [SECURITY.md](SECURITY.md).

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) before opening an Issue or Pull Request. Chinese is the primary community
language, and English contributions are welcome. Follow the [Code of Conduct](CODE_OF_CONDUCT.md).

Before submitting, separate unrelated topics into independent commits, add focused tests for protocol/database/key
business behavior, run make check and frontend tests, and remove all real accounts, Cookies, orders, cards, keys,
and other sensitive data.

## License

Licensed under the [Apache License 2.0](LICENSE), Copyright © 2026 Christ9038. Preserve [NOTICE](NOTICE) and the
applicable license notices when distributing original or derivative works, and clearly mark modified files.

## Disclaimer

This project provides technical research and self-hosted management capabilities. It does not guarantee compatibility
with future Xianyu changes, account risk-control decisions, business continuity, or data integrity. Users are solely
responsible for legal compliance, platform compliance, account authorization, backups, account restrictions,
transaction disputes, data loss, and all other consequences. Do not use it for spam, fraud, unauthorized access,
bypassing platform restrictions, or illegal activity.

## Community support

This project receives discussion and feedback from the LINUX DO community:

[Visit the LINUX DO community](https://linux.do/)

### Sponsor the project

If this project is useful to you, you can support its continued development with USDT on the BNB Smart Chain
(BSC, BEP-20):

`0xc7b77931af39e292bfcadef6228a067ad5a69e3a`

Send USDT only on BNB Smart Chain (BEP-20). Transfers on other networks may be lost.
