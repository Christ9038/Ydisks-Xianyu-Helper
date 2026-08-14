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

## Mandatory refactoring governance — DO NOT SKIP

The repository is following the authoritative long-term plan in
`docs/architecture/refactoring-master-plan.md`. Before changing Go package boundaries, HTTP APIs, database
access, account credentials, application wiring, React page structure, tests, CI or compatibility behavior,
agents MUST read all of:

- `docs/architecture/refactoring-master-plan.md`
- `docs/architecture/dependency-rules.md`
- `docs/architecture/comment-standard.md`
- `docs/slider-captcha-frozen-spec.md` when credentials, login, engine, account, server or browser call paths are involved

The following rules are mandatory:

1. Identify the current plan stage and the smallest applicable work item before editing. Do not perform a
   repository-wide rewrite or create a long-lived big-bang refactor.
2. Preserve a compiling, runnable and tested state at every merge point. Add or strengthen characterization
   tests before moving high-risk behavior.
3. Keep each change to one reviewable vertical slice. Do not combine broad file moves, behavior changes,
   schema migrations, compatibility deletion and mass comment backfill in the same change.
4. Update the master plan status table, completion evidence, update log and next safe entry point whenever a
   plan work item is completed or its scope materially changes.
5. Do not weaken, skip or delete tests to make a refactor pass. Existing test failures must be explained and
   resolved or reported as blockers.
6. Preserve unrelated working-tree changes. Do not reformat or rename unrelated code as cleanup.
7. Compatibility adapters remain until every known caller is migrated and contract tests prove the old path
   can be removed. New compatibility aliases require an explicit removal condition in the plan.

Emergency bug or security fixes may precede the planned sequence, but they must stay narrowly scoped, preserve
the same architectural constraints, and add a note to the plan update log explaining why the sequence changed.

## Mandatory Chinese comments for all functions and variables

`docs/architecture/comment-standard.md` is authoritative. This requirement is intentionally stricter than
normal Go exported-symbol documentation rules and applies to Go, TypeScript and TSX production and test code.

Agents MUST:

- add accurate Chinese comments for every new or modified function, method, anonymous function, parameter,
  return value, package/module variable, constant, field, local variable, short declaration, loop variable,
  callback parameter, React state value, setter, ref and memoized value;
- explain all variables in a multi-variable declaration in one nearby comment when appropriate;
- explain business meaning, input/output semantics, units, lifecycle, ownership, cancellation, concurrency,
  sensitivity or compatibility constraints instead of translating the identifier or restating syntax;
- update comments in the same change whenever behavior or ownership changes;
- clear historical comment-baseline entries for every file or logical unit materially refactored;
- keep generated code, third-party code and `internal/webui/static` out of manual comment backfills.

Automated checks can prove only presence and Chinese text. Reviewers and agents remain responsible for semantic
accuracy. Placeholder comments such as “err 表示错误” or “count 表示数量” are forbidden. Comments must never
contain real Cookie, Token, password, SMTP credential or production secret values.

Until the historical baseline is removed, untouched legacy declarations may remain exempt, but no task may add
new debt or use the baseline to exempt newly created or semantically modified declarations.

Run `make comments` (or `go run ./tools/commentlint -mode check -root . -baseline .commentlint/go-baseline.json`
and `npm --prefix frontend run comments:check`) for the mandatory AST presence gate. Baseline regeneration is a
reviewed maintenance operation only: use `make comments-baseline` after clearing the affected file's historical
debt and record the scope in `docs/architecture/refactoring-master-plan.md`.

## Mandatory target dependency boundaries

`docs/architecture/dependency-rules.md` is authoritative. During migration, existing violations may remain only
until their recorded vertical slice is completed; agents MUST NOT add new violations.

- `cmd` handles configuration, dependency construction, signals and lifecycle only; no new business rules, SQL,
  HTTP handlers or protocol parsing belong there.
- `internal/server` is the HTTP/SPA transport. Do not add new direct `Store.DB`, transaction, MTOP, browser,
  credential-renewal, business-worker or platform-session logic. New use cases go through application services.
- Application services own use-case orchestration, authorization and transaction boundaries and must not depend
  on `net/http`, chi or frontend compatibility fields.
- Interfaces are defined by consumers and kept minimal. Do not add a service locator, universal repository
  interface or runtime setter for a required dependency.
- `internal/db` owns SQL, dialects, migrations, encryption-at-rest and repository implementations. It must not
  import upper layers or decide HTTP responses.
- `internal/xianyu` and `internal/browser` own platform and browser implementation. They must not write business
  data directly, decide automation rules or depend on the HTTP/application layer.
- `internal/engine` and `internal/automation` must remain independent of Server. New mutable concurrent state
  requires explicit ownership, lock, Context, goroutine and shutdown documentation plus focused tests.
- Cross-repository atomic work belongs behind an application-level Unit of Work. New handlers must not call
  `BeginTx` directly.

## Mandatory sensitive-data boundaries

- Account summaries and ownership checks MUST NOT read or decrypt Cookie, Token, password or encrypted metadata.
- Platform credentials and password-login secrets use separate models and purpose-specific repository methods.
- Sensitive persistence models MUST NOT be serialized as HTTP responses or frontend state.
- Logs, notifications, API errors and test failure output MUST NOT contain plaintext credentials.
- New ownership checks return existence/non-sensitive identity rather than maps of decrypted Cookie values.
- Do not hold credential locks across slow external I/O unless an authoritative protocol invariant explicitly
  requires it and the lock order is documented and tested.

## Mandatory HTTP API constraints

- New APIs use the `/api/v1` prefix unless the current task is an explicit compatibility fix.
- New request and response bodies use named DTOs. Do not add anonymous handler request structs, dynamic
  `map[string]any` response contracts or direct DB-model serialization.
- New failures use the unified error envelope and correct HTTP status. Do not add HTTP 200 + `success:false` or
  new `detail`/`msg`/`error` aliases.
- DB models, domain models and transport DTOs remain distinct. Compatibility normalization belongs only at the
  transport adapter boundary.
- Route-prefix changes require `frontend/vite.config.ts`, frontend API callers, contract tests and embedded assets
  to be updated together.

## Mandatory React constraints

- New frontend code follows `app -> features -> shared`; a feature must not import another feature's internal
  files, and shared code must not import features.
- React components must not call `fetch` or `axios` directly. Use the shared HTTP client through a feature API
  adapter; keep domain normalization out of the generic client.
- Do not store derivable values in state. Put user-triggered side effects in event handlers, split effects with
  different dependencies, and use functional state updates when the next value depends on the previous value.
- Every async effect or request must define cancellation or latest-generation behavior so stale responses cannot
  replace current state. Independent requests should start in parallel.
- Do not add memoization without a concrete expensive calculation or stable-child-prop reason. Do not define
  child components inside parent components.
- Keep server data, form state and transient UI state separate. Heavy pages and heavy optional dependencies use
  route/feature-level lazy loading where practical.
- New or materially changed user flows require behavior tests for success, failure, cancellation, switching and
  stale responses. Source-string tests are reserved for static architecture rules.
- Generated API types are read-only; feature adapters convert transport DTOs to UI models.

## Mandatory database, transaction and multi-dialect constraints

- Do not expose new raw `*sql.DB` access to upper layers. Add narrow repository methods or an explicit Unit of
  Work instead.
- SQL row structs, persistence models, domain models and HTTP DTOs must not be collapsed into one convenience
  type when they have different sensitivity or ownership.
- Every migration keeps SQLite, MySQL and PostgreSQL numbering and final schema aligned.
- Database behavior changes require focused SQLite tests and, when available, MySQL/Postgres regression or
  `cmd/dbverify` evidence.
- Repository/package splitting is permitted only after consumer interfaces and transaction boundaries are clear;
  directory count is not a goal and package cycles must not be hidden behind globals or reflection.

## Mandatory lifecycle and concurrency constraints

- Background goroutines require a documented owner, Context source, cancellation path and Wait/Join path.
- Start methods must not make partially constructed objects observable. Stop/Close must be idempotent when the
  component contract allows repeated shutdown.
- The sender closes a channel unless a more specific documented ownership rule applies.
- Do not perform uncontrolled network, browser or user-wait I/O while holding a mutex.
- Types with concurrent use must document which locks protect which fields and the allowed lock order.
- Required dependencies are constructor inputs and validated before Start; mutable setters are only for optional
  runtime configuration or isolated tests and must not create an invalid intermediate production state.

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

The following paragraphs describe the current runtime wiring. They are not permission to add more business
logic to HTTP handlers. The mandatory target architecture and the allowed transition path are defined in
`docs/architecture/refactoring-master-plan.md` and `docs/architecture/dependency-rules.md`.

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
