# AGENTS.md

## Repository shape

The active application contains:

- `cmd/server/` — server entrypoint, administrator bootstrap (`-init-admin`), and HTTP server.
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
go run ./cmd/server -db data/xianyu_data.db -addr :8080
DATABASE_URL="mysql://user:pass@tcp(host:3306)/db" go run ./cmd/server -addr :8080
```

Disable browser automation when Chromium is unavailable:

```bash
go run ./cmd/server -db data/xianyu_data.db -addr :8080 -no-browser
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

Vite proxies backend routes to `localhost:8080`. Production builds are written to `internal/webui/static/` and embedded by the Go server.

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
