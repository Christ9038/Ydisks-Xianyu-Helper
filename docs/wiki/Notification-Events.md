

## Channel types and fields

| Channel | Required | Optional |
| --- | --- | --- |
| Bark | Bark server (e.g. `https://api.day.app`), Device Key | title, sound, group |
| DingTalk bot | Webhook URL | signing secret (generated when "sign" is ticked, starts with `SEC`) |
| Feishu bot | Webhook URL | signature secret |
| WeCom bot | Webhook URL | — |
| Telegram | Bot Token, Chat ID (`-100…` for groups, your user ID for private chat) | — |
| Email | recipient address (sent through the system SMTP, or enable "use separate SMTP") | separate SMTP server, login, password/app password, sender name, SSL/STARTTLS |
| Custom webhook | Webhook URL | method (default POST), headers (JSON) |

A custom webhook receives a JSON body: `{"message": "...", "timestamp": "2026-01-02 15:04:05", "source": "xianyu-auto-reply"}`; a status below 300 counts as success and 300+ as failure. QQ Mail needs an authorization code, not the login password.

Use "Test notification" after saving. A legacy combined "delivery" subscription is expanded on edit into the four automation events plus "manual delivery result" and "manual handling required".
