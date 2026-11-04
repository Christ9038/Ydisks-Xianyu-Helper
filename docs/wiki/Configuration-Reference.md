# Configuration Reference

[中文](配置参考.zh-CN) | English

Every way to configure the server: command-line flags, environment variables and the Docker Compose `.env` file. Flags win over environment variables for log level and format; **the database connection is the exception: the `DATABASE_URL` environment variable wins over `-db-url`, which wins over `-db`.**

## Command-line flags (`cmd/server`)

| Flag | Default | Purpose |
| --- | --- | --- |
| `-db` | `data/xianyu_data.db` | SQLite file path (legacy form). |
| `-db-url` | empty | Database URL (`sqlite://`, `mysql://`, `postgres://`). Overrides `-db`, but is itself overridden by a set `DATABASE_URL` environment variable. |
| `-addr` | `:59188` | HTTP listen address. `:59188` listens on all interfaces; desktop packages use `127.0.0.1:59188`. |
| `-web` | embedded | Directory with a front-end build (`index.html`) instead of the embedded one. |
| `-workdir` | current dir | Working directory; desktop services use it to pin data and browser directories. |
| `-data-key-file` | empty | File that persists `XIANYU_DATA_KEY`; generated if missing. Used when the variable is not set. |
| `-secure` | off | HTTPS mode: session cookies get the `Secure` attribute. Turn on behind an HTTPS reverse proxy. |
| `-no-browser` | off | Disable Chromium. Browser fingerprint reading and automatic slider handling become unavailable. Only use on hosts that really cannot run Chromium. |
| `-playwright-runtime-root`, `-playwright-driver-dir`, `-playwright-browser-dir` | empty | Locations of the Playwright runtime shipped with installers. Normally set by the packaged service, not by hand. |
| `-log-level` | empty | `debug` / `info` / `warn` / `error`. Falls back to `LOG_LEVEL`, then the System Settings value. |
| `-log-format` | empty | `text` or `json`. Falls back to `LOG_FORMAT`. |
| `-v` | off | Debug logging. |
| `-init-admin` | off | Create or reset the `admin` user, then exit. |
| `-ensure-admin` | off | Create `admin` only if it does not exist; never changes an existing password. |
| `-admin-password`, `-admin-email` | empty / `admin@example.com` | Credentials for `-init-admin` / `-ensure-admin`. The password can also come from `XIANYU_ADMIN_PASSWORD`. |
| `-service` | off | Run as a Windows Service (set by the installer). |
| `-version` | off | Print version and build info, then exit. |

## Environment variables

| Variable | Purpose |
| --- | --- |
| `DATABASE_URL` | Database connection URL; when set it wins over `-db-url` and `-db`. URL-encode special characters in the password. |
| `XIANYU_DATA_KEY` | AES-256-GCM key for Cookies, passwords, AI keys, SMTP and notification secrets. **Fix it once and back it up with the database**; changing it makes existing encrypted data unreadable. |
| `XIANYU_ADMIN_PASSWORD` | Initial `admin` password for non-interactive deployments (Docker Compose). Later restarts do not reset it. |
| `XIANYU_UPLOAD_DIR` | Root directory of uploaded images and library files. Defaults to `data/uploads`; Docker uses `/app/data/uploads`. |
| `XIANYU_LOG_DIR` | Directory for log files. When unset, logs go to standard output (Windows service: the `logs` folder under its data directory). |
| `LOG_LEVEL`, `LOG_FORMAT` | Same as the flags; `LOG_FORMAT=json` suits log collectors. |
| `BROWSER_HEADLESS` | `true`/`1`/`yes`/`on` forces headless Chromium, `false`/`0`/`no`/`off` forces a visible window; unset keeps the built-in default. |
| `XIANYU_PENDING_SHIP_CATCHUP` | Default on. Set `0` to turn off both fallback chains (scanning pending-ship orders after a lost payment message, and resuming unfinished runs). See [Bargain Orders and Delivery Flow](Bargain-Orders-and-Delivery-Flow). |
| `XIANYU_CONTINUE_AFTER_UNCERTAIN` | Default on. After an uncertain message result, if every remaining action is `confirm_shipment` it still runs (it never contacts the buyer and is idempotent on the platform), and the run still ends in manual review. Set `0` to stop the whole chain instead. |
| `TZ` | Time zone of the process; the Compose file defaults to `Asia/Shanghai`. Daily polishing always uses Beijing time. |

## Docker Compose `.env`

`.env.example` documents the variables the Compose files read (`XIANYU_PENDING_SHIP_CATCHUP` and `XIANYU_CONTINUE_AFTER_UNCERTAIN` are commented-out options; Compose passes the latter as `1` by default): `POSTGRES_IMAGE/DB/USER/PASSWORD`, `DATABASE_URL`, `XIANYU_IMAGE`, `XIANYU_DATA_KEY`, `XIANYU_BIND_ADDRESS` (default `0.0.0.0`), `XIANYU_HTTP_PORT` (default `59188`), `XIANYU_UPLOAD_DIR`, `TZ`, `LOG_LEVEL`, `LOG_FORMAT`, `BROWSER_HEADLESS` and `XIANYU_ADMIN_PASSWORD`. Never commit `.env`.

## Health check

`GET /health` returns the service status without logging in; Docker health checks, the desktop tray and `make smoke` all use it.

See also [Deployment and Database](Deployment-and-Database) and [Runtime Logs](Runtime-Logs).
