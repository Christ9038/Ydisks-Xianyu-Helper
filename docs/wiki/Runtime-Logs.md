# Runtime Logs

[中文](运行日志.zh-CN) | English

**Runtime Logs** (`/app/logs`) shows the most recent log lines kept in the server's memory, so you can diagnose problems without opening log files. **Only administrators can open this page.**

## What you can do

- **Filter** by minimum level (Debug / Info / Warn / Error only), by **source** and by keyword (interface name, path, error text or attribute value).
- Sources: internal API (`http`), Xianyu API (`mtop`), automation, message engine, WebSocket, credential renewal, notification delivery, other.
- **Pause / resume** the automatic polling, **refresh** to reload from the beginning, **clear** the screen (display only), and toggle **follow latest**.
- Expand a row to see attributes such as request ID, API name, failure category, `ret` summary, account ID and item ID. Values are redacted before they leave the server; Cookies and tokens never appear.

## Things to know

- Only a limited number of recent entries is kept in memory (2000 by default). A yellow notice appears if older entries were dropped. For permanent history use the log files (see [Deployment and Database](Deployment-and-Database)).
- At the default `info` level only failures and warnings are recorded for interface calls. To see every call, raise the system log level to `debug` in System Settings.
- The page's own polling is not written to the request log, so it does not flood itself.
- Typical use: filter source `mtop` and keyword `RGV587` to confirm risk control, see [Risk-Control Troubleshooting](Risk-Control-Troubleshooting).
