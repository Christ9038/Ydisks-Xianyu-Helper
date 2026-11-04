# Runtime Logs

[中文](运行日志.zh-CN) | English

**Runtime Logs** (`/app/logs`) shows the most recent log lines kept in the server's memory, so you can diagnose problems without opening log files. **Only administrators can open this page.**

## What you can do

- **Filter** by minimum level (all levels / Debug and above / Info and above / Warn and above / Error only), by **source** and by keyword (case-insensitive; matches the message, request method, path, API name, failure category, `ret`, error text or attribute values).
- Sources: internal API (`http`), Xianyu API (`mtop`), automation, message engine, WebSocket, credential renewal, notification delivery, other.
- The page reads new entries incrementally every 2 seconds; **pause / resume** the automatic polling, **refresh** to reload from the beginning, **clear** the screen (display only), and toggle **follow latest**.
- Each row shows labels directly (only when they have a value): request, route, status code, duration, failure category, platform return (`ret`), account, item, order, chat, run ID and request ID, plus the source location that wrote the entry; click "属性" (attributes) to expand the remaining attributes. Values are redacted before they leave the server; Cookies and tokens never appear.

## Things to know

- Only a limited number of recent entries is kept in memory (2000 by default; the page shows "服务端保留 N 条", the number the server keeps). A yellow notice appears if older entries were dropped, and the buffer is empty after a restart. For permanent history use the log files (see [Deployment and Database](Deployment-and-Database)).
- Whether an entry reaches the buffer depends on the log level. At the default `info` level, `debug` entries such as successful internal read requests and MTOP request starts do not appear; successful write operations and MTOP request completions (with API name, status code and duration) are `info`, and failures are `warn`/`error`. To see everything, set the log level to `debug` in System Settings; the level also accepts per-module overrides such as `info,mtop=debug`.
- Repeated `warn`/`error` entries from the same call site are limited to a few per minute; the rest are only counted and later added as a summary entry.
- The page's own polling is not written to the request log, so it does not flood itself.
- Typical use: filter source `mtop` and keyword `RGV587` to confirm risk control, see [Risk-Control Troubleshooting](Risk-Control-Troubleshooting).
