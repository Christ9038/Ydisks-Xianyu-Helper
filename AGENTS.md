# AGENTS.md

## Repository shape

The active application contains:

- `cmd/server/` — server entrypoint, administrator bootstrap (`/initialize` and `-init-admin`), and HTTP server.
- `cmd/init-admin/` — interactive administrator initialization CLI.
- `cmd/dbverify/` — migration + core CRUD verification tool across SQLite/MySQL/Postgres.
- `internal/server/` — chi HTTP API and SPA serving.
- `internal/adapter/` — wiring layer that implements `engine.Handler` and `automation.OrderDetailFetcher` (system events → automation center, order-detail fetch / password-login refresh → browser, account alerts → notifier).
- `internal/account/` — enabled-account supervisor.
- `internal/engine/` — per-account runtime, replies, and delivery behavior.
- `internal/automation/` — unified automation center (paid delivery, review gifts, review requests) + scheduler.
- `internal/xianyu/` — MTOP, WebSocket, QR login, and protocol code.
- `internal/browser/` — in-process Chromium automation through playwright-go.
- `internal/db/` — multi-database access (SQLite/MySQL/Postgres) with embedded Goose migrations per dialect.
- `frontend/` — active React/Vite source.
- `internal/webui/static/` — embedded frontend build output.

## Common commands

```bash
cd /Users/christ/Workspace/git/xianyu/Ydisks-Xianyu-Helper

make build      # go build ./cmd/server
make test       # go test ./...
make vet        # go vet ./...
make lint       # golangci-lint run ./... (0 issues baseline)
make check      # vet + lint + test
make frontend   # build frontend into internal/webui/static
```

Run the server (SQLite by default; MySQL/Postgres via `-db-url` or `DATABASE_URL`):

```bash
go run ./cmd/server -db data/xianyu_data.db -addr :59188
DATABASE_URL="mysql://user:pass@tcp(host:3306)/db" go run ./cmd/server -addr :59188
```

On a new database, open the management page after starting the server. The first-run page accepts
and confirms an administrator password, creates the `admin` user, and signs the user in automatically.
The CLI bootstrap remains available for headless or operational environments.

Disable browser automation when Chromium is unavailable:

```bash
go run ./cmd/server -db data/xianyu_data.db -addr :59188 -no-browser
```

Initialize or reset the administrator:

```bash
go run ./cmd/server -init-admin -db data/xianyu_data.db -admin-password '...'
```

Verify a database (migration + CRUD across dialects):

```bash
go run ./cmd/dbverify "mysql://user:pass@tcp(host:3306)/db"
```

Run a focused test:

```bash
go test ./internal/server -run TestName -v
go test ./internal/db -run TestMigrate -v
```

Cross-database regression (requires Docker containers or external DBs):

```bash
TEST_MYSQL_URL="mysql://root:pass@tcp(host:3306)/db" \
TEST_POSTGRES_URL="postgres://user:pass@host:5432/db" \
go test ./internal/db -run TestMultiDB -v
```

Build the frontend:

```bash
cd /Users/christ/Workspace/git/xianyu/Ydisks-Xianyu-Helper/frontend
npm install
npm run build
```

Run the frontend development server:

```bash
npm run dev
```

Vite proxies backend routes to `localhost:59188`. Production builds are written to `internal/webui/static/` and embedded by the Go server.

## Desktop packaging and service behavior

The application defaults to port `59188`; commands using `-addr :59188` listen on all interfaces. Desktop
packages explicitly bind the server to `127.0.0.1:59188` and keep the server and tray as separate processes:

- Windows installs the `YdisksXianyuHelper` Windows Service and starts `xianyu-tray.exe` for the current
  user. The installer grants the interactive user only service status/start/stop rights, so tray service
  actions do not launch UAC prompts after installation. Service configuration and deletion remain admin-only.
- macOS registers `com.ydisks.xianyu-helper.server` and `com.ydisks.xianyu-helper.tray` LaunchAgents.
  The app is `/Applications/Ydisks闲鱼助手/Ydisks闲鱼助手.app`, and the tray executable is named
  `Ydisks闲鱼助手` with `LSUIElement=true` so it does not appear in the Dock.
- Linux packages are architecture-specific tar archives. `install.sh` must run as root on a native matching
  architecture, installs `ydisks-xianyu-helper.service`, and keeps data in `/var/lib/ydisks-xianyu-helper`.

All desktop packages contain the matching Playwright driver, Chromium and headless shell. Do not add a
Debian Chromium package or download a second browser during installation. The Docker final image uses
`node:24-trixie-slim`, copies the cached runtime prepared by CI, installs only the Chromium system libraries
through the bundled Playwright driver, and clears apt indexes and temporary caches in the same image layer.
The desktop CI workflow runs on `main` and `dev`; formal desktop builds are invoked by the unified
`release.yml` workflow for `v*.*.*` version tags. Linux amd64 and arm64 jobs use native GitHub-hosted runners and
must not use QEMU or cross-architecture emulation. Docker publishing also builds each architecture on its native
runner: branch builds publish `main` or `dev` plus a full-commit `sha-*` tag, while formal builds publish semantic
version tags, `latest`, and the full-commit `sha-*` tag only after the `production-release` Environment approval.
Version tags create a GitHub Release containing all platform packages and SHA-256 checksums. Never publish a Docker
manifest until Go/frontend tests, an actual Chromium launch, and the packaged server health check have passed for
every architecture.

The tray state machine is shared by Windows and macOS: it serializes actions, shows transition states,
waits for a healthy `/health` response after start/restart, waits for the endpoint to become unreachable
after stop, and stops the server before exiting. The tray also provides an “Open log directory” action.

Desktop first-run initialization is done in the web UI at `http://127.0.0.1:59188`; users enter and confirm
the initial administrator password. `-init-admin` remains an operational/headless fallback, while Docker
Compose uses `XIANYU_ADMIN_PASSWORD` for non-interactive initialization.

## macOS 本地安装包构建

macOS 安装包必须通过 `packaging/macos/build-pkg.sh` 构建，禁止手工复制 Chromium、Playwright
driver 或 headless shell 到 `dist`。打包脚本会检查目标架构的 runtime；runtime 不完整时自动调用
`packaging/macos/prepare-runtime.sh`，从本机 Playwright 缓存整理出与 driver 匹配的 Chromium
和 Chromium headless shell。

本机 arm64 打包流程：

```bash
npm ci --prefix frontend
npm run build --prefix frontend
mkdir -p dist/macos/arm64
go build -trimpath -ldflags='-s -w' -o dist/macos/arm64/xianyu-server ./cmd/server
go build -trimpath -ldflags='-s -w' -o dist/macos/arm64/browser-install ./cmd/browser-install
go build -trimpath -ldflags='-s -w' -o dist/macos/arm64/xianyu-tray ./cmd/tray
packaging/macos/build-pkg.sh 0.0.0-local "$PWD/dist/macos" arm64
```

`prepare-runtime.sh` 默认读取：

- `~/Library/Caches/ms-playwright-go`：Playwright Go driver
- `~/Library/Caches/ms-playwright`：Chromium 与 `chromium_headless_shell`

如果本机缓存尚未准备好，先运行编译出的 `browser-install`；不要只复制
`chromium-<revision>`，服务启动还需要同版本的 `chromium_headless_shell-<revision>`。打包前必须确认
服务能使用包内 runtime 启动并访问健康检查；可用临时端口验证：

```bash
runtime_app="$PWD/dist/macos/pkgroot-arm64/Applications/Ydisks闲鱼助手/Ydisks闲鱼助手.app"
mkdir -p /tmp/ydisks-local-data
"$runtime_app/Contents/Helpers/xianyu-server" \
  -addr 127.0.0.1:59189 \
  -workdir /tmp/ydisks-local-data \
  -playwright-runtime-root "$runtime_app/Contents/Resources/playwright-runtime"
curl -fsS http://127.0.0.1:59189/health
```

本地没有签名身份时可以生成未签名 pkg，但必须明确告知未签名状态；CI 使用固定签名 Secrets 生成可分发包。

## CI desktop package rules

Do not manually assemble desktop packages in CI. The desktop workflow must build the embedded frontend,
compile the platform binaries, restore or populate the architecture-specific Playwright runtime cache,
assemble the package with the platform packaging script, and run the package-specific signing step.
Windows uses `packaging/windows/installer.iss`; macOS uses `packaging/macos/build-pkg.sh`; Linux archives
the directory containing `install.sh`, `uninstall.sh`, the systemd unit, binaries and matching runtime.

## Architecture

`cmd/server/main.go` opens the database (SQLite/MySQL/Postgres by URL scheme), constructs the adapter + account manager + automation center + notifier, starts enabled account runtimes, initializes the optional in-process browser manager, and starts the HTTP server. Business logic does not live in `main.go` — it delegates to `internal/adapter` (Handler/OrderDetailFetcher wiring), `internal/engine`, `internal/account`, `internal/automation`, and domain-specific server handlers.

`internal/xianyu` owns lower-level platform integration:

- `mtop` for signed HTTP calls (interface `mtop.Client` allows test mocking).
- `ws` for WebSocket transport.
- `qrlogin` for QR login.
- `protocol` for cookies, signing, decoding, and message IDs.

Browser-backed verification, password login refresh, and order-detail fallbacks live in `internal/browser`. Keep the browser contract and its server/engine callers aligned.

## Frozen slider CAPTCHA logic — DO NOT MODIFY

The current slider CAPTCHA implementation is production-frozen. Its authoritative behavior is documented in `docs/slider-captcha-frozen-spec.md`.

Unless the user explicitly requests a slider CAPTCHA behavior change in the current task, agents MUST NOT:

- edit, refactor, optimize, rename, move, delete, or reformat the protected slider implementation or its tests;
- change selectors or selector priority, same-frame visibility checks, the exact `300px - 42px = 258px` standard NC distance, trajectories, point counts, timing, mouse-event order, or main-engine no-overshoot behavior;
- change fresh-`x5sec` success criteria, punish/captcha URL checks, retry selectors, retry text checks, origin checks, reload recovery, retry counts, or timeouts;
- change Playwright-first / direct-Chromium-CDP-fallback ordering, persistent-profile reuse and locking, verification-URL refresh timing, Cookie merge behavior, browser flags, environment defaults, or engine result labels;
- weaken, skip, delete, or rewrite slider tests to permit different behavior;
- modify a caller or shared helper in another file when that would indirectly change any frozen behavior above.

Directly protected files are:

- `internal/browser/slider.go`
- `internal/browser/slider_test.go`
- `internal/browser/token_captcha.go`
- `internal/browser/token_captcha_test.go`
- `internal/browser/token_captcha_fallback.go`
- `internal/browser/token_captcha_fallback_integration_test.go`
- `internal/browser/token_captcha_orchestrator_test.go`

Only an explicit user instruction in the current task can authorize a change. When authorized, update the implementation, tests, and frozen specification together, and run every verification required by the specification. Do not treat one authorization as permission for later tasks.

## Editing notes

- Preserve unrelated working-tree changes.
- Update `frontend/vite.config.ts` if API route prefixes change.
- Rebuild the frontend after source changes so embedded assets stay current.
- Keep protocol and database behavior covered by focused tests.
