# System and AI Guide

[中文](系统设置.zh-CN) | English

Only administrators can see System and AI. It configures global services, AI, remote verification, and login credentials. Notification channels and SMTP are configured on [AI, Notifications and Operations](AI-Notifications-and-Operations).

## Basic settings

1. Open System and AI.
2. Configure log level and log format for the deployment.
3. To restrict user-configured HTTP requests to public-network destinations, enable the corresponding option and save. It affects API delivery, AI, HTTP notifications, remote images, and remote verification, and may block private-network addresses.
4. Click Save All Configuration. Restart the service when the page says a setting requires it.

Do not paste Cookies, notification tokens, or database passwords here. Use environment variables or the encrypted form provided by the page, and back up `XIANYU_DATA_KEY`.

## Configure AI

1. Enter an OpenAI-compatible API base URL such as `https://api.openai.com/v1`; do not enter the complete `/chat/completions` path.
2. Enter the API key and click Read Models. If the provider does not expose a model list, enter the model name manually.
3. Save System and AI.
4. Open the target account’s AI Settings in Account Management, enable automatic AI replies, and set negotiation limits.

AI handles bargaining by default; an account can switch to Full Service mode to also answer consultation, shipping, and after-sales messages, and each product can override the account setting. Messages that AI does not handle continue through the keyword, AI, and default reply chain. Test discount amount, percentage, and maximum rounds with a low-risk account before enabling it.

## Remote verification

Enter a service URL and secret only when you already have a trusted remote service. The system calls it first, then falls back to the local engine only when the network is unavailable or the request times out. An explicit failure from the remote service does not trigger local verification again.

“Allow sending account Cookies to the remote service” is off by default. Enable it only when the service is trusted and needs the verification URL for automatic refresh.

## Change login credentials

Enter the current password to change the management username and password. The new password and confirmation must match. Leave the new-password fields blank to change only the username. Sign in again with the new credentials and keep them separate from the Xianyu account password.

## AI connection test

In the AI model settings, **Test connection** sends a real minimal chat request with the entered API URL, key and model, so you can verify the whole chain before enabling AI for an account. For OpenAI-compatible providers the base URL usually ends with `/v1`.

## Renewal log retention

"Renewal log retention (days)" in Basic settings controls automatic cleanup of credential-renewal logs: 0–365, default 10; 0 disables cleanup.

## Login protection

Only **failed** logins are counted: 30 failures per IP or 10 per username/email within 5 minutes temporarily reject further attempts ("too many login attempts", HTTP 429 with `Retry-After`); a successful login clears that account's counter. Behind a reverse proxy, make sure the real client IP is forwarded.

## Sensitive-settings audit

Reading or changing sensitive settings (API keys, SMTP, notification credentials) writes an audit record containing the user, action, resource and key names, outcome and time. The secret values themselves are never recorded.
