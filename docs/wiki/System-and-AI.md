# System and AI Guide

[中文](系统设置.zh-CN) | English

Only administrators can see System and AI (the page header reads "System Settings"). It configures global services, AI, remote slider verification, and login credentials. Notification channels and SMTP are configured on [AI, Notifications and Operations](AI-Notifications-and-Operations).

## Basic settings

1. Open System and AI.
2. Set the log output level (Debug / Info / Warn / Error) and log output format (Text / JSON) for the deployment. After saving, JSON format takes effect only after a service restart.
3. "Renewal log retention (days)" is described below. "New-message browser notification sound" is stored only in the current browser: it plays a sound when a new buyer message raises a browser notification, and requires browser reminders to be turned on first in the online chat page.
4. To restrict user-configured HTTP requests to public-network destinations, enable "Restrict user-configured outbound HTTP requests to the public network" and save. It constrains API delivery, AI, HTTP notifications, remote images, and the remote slider service, takes effect immediately after saving, and may block private-network services.
5. Click Save All Configuration at the bottom right. Restart the service for settings the page says require it.

Do not paste Cookies, notification tokens, or database passwords here. Sensitive values in system settings (AI API key, SMTP password, remote slider secret and so on) are stored encrypted with `XIANYU_DATA_KEY`, so back that key up; after saving the page never echoes them, it only shows that they are configured.

## Configure AI

1. Enter an OpenAI-compatible API base URL such as `https://api.openai.com/v1`; do not enter the complete `/chat/completions` path. If left empty, the Qwen-compatible endpoint `https://dashscope.aliyuncs.com/compatible-mode/v1` is used.
2. Enter the API key and click Read Models. If the provider does not expose a model list, enter the model name manually.
3. Click Save All Configuration.
4. Open the target account's AI Settings in Account Management, enable automatic AI replies, and set bargaining limits.

AI handles bargaining by default; an account can switch to Full Service mode to also answer consultation, shipping, and after-sales messages, and each product can override the account setting. The reply priority is external API reply, keyword reply, AI, then default reply, so keywords always win over AI and messages AI does not handle continue to the default reply. Test discount amount, percentage, and maximum rounds with a low-risk account before enabling it. The account-side settings are explained in [AI, Notifications and Operations](AI-Notifications-and-Operations).

Read Models uses the URL and key currently entered on the page (they need not be saved) and requests `<API URL>/models`; with the key left empty the saved key is used, and an empty list or an error from the service is shown on the page. Models are read once automatically when the page opens, and if the model field is empty the first model read is filled in. Saving clears the model list; click Read Models again if you need it.

## AI connection test

**Test connection** sends one real minimal chat request ("你好", at most 64 tokens in the reply) to `<API URL>/chat/completions` with the API URL, key and model currently entered on the page, so you can verify the whole chain before enabling AI for an account. On success it shows the model name, latency and the AI's reply; on failure it shows the error returned upstream. An empty URL, key or model falls back to the saved value, and if there is still no model it asks you to enter a model name or click Read Models. For OpenAI-compatible providers the base URL usually ends with `/v1`.

## Remote slider verification

Enter a service URL and secret only when you already have a trusted remote service; the feature is used only when both are filled in. The system calls it first, then falls back to the local engine only when the network is unavailable or the request times out. An explicit failure from the remote service does not trigger local verification again.

"Allow sending account Cookies to the remote service" is off by default; when on, requests carry the account Cookie and device ID. Enable it only when the service is trusted and needs to re-fetch an expired verification URL automatically.

## Change login credentials

The Login credentials area shows the current login username. Enter the current password to change the management username and password: the username must be 3–64 characters, the new password at least 8 characters, and the new password and confirmation must match; a username that is already taken is reported as a conflict. Leave the new-password fields blank to change only the username; if neither username nor password changes, the server rejects the request (HTTP 400). On success the current session is invalidated and the page reloads shortly after, so sign in again with the new credentials and keep them separate from the Xianyu account password.

## Renewal log retention

"Renewal log retention (days)" in Basic settings controls automatic cleanup of credential-renewal logs: the input accepts 0–365, default 10; 0 disables cleanup. (The range is enforced by the input only; the server does not validate it separately.)

## Login protection

Only **failed** logins are counted: 30 failures per IP or 10 per username/email within 5 minutes temporarily reject further attempts ("too many login attempts", HTTP 429 with `Retry-After`); a successful login clears the counters of that IP and that username/email. The IP is taken from the TCP connection's remote address and `X-Forwarded-For` is not read, so behind a reverse proxy all visitors share the proxy's address and the 30-failure IP limit is shared by everyone.

## Sensitive-settings audit

Accessing these sensitive system settings writes an audit record: `ai_api_key`, `smtp_password`, `qq_reply_secret_key`, `captcha.remote_secret_key`. It happens when system settings are read (page load), when they are changed, and when the system actually uses them (Read Models or Test connection with a key typed on the page, reading the SMTP password to send a notification, reading the remote slider secret). A record contains the user, action (`settings.read` / `settings.write` / `settings.use`), resource and key names, outcome and time; the secret values themselves are never recorded. If the audit record cannot be written, the related read is refused.
