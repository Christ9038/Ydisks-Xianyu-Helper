# Notification Events

[中文](通知事件参考.zh-CN) | English

This page explains what each channel on the "Notification Settings" page needs, which events you can subscribe to, when each event fires, and how notifications are delivered and retried. For the step-by-step setup see [AI, Notifications and Operations](AI-Notifications-and-Operations).

## Event categories

A channel with no event selected under "Notification content" receives every event; once you tick events, it receives only those. Notifications go only to channels that are **bound to the account** (Account Management → Edit → notification channel binding) and **enabled**.

| UI name | Code | When it fires |
| --- | --- | --- |
| Offline | `account_offline` | The account's message connection drops, goes offline, its login credential expires, or automatic recovery fails (critical level once consecutive failures reach the limit); one notification per offline episode. |
| Recovered | `account_recovered` | The account reconnects to the Xianyu message service and is online again; the body includes when it went offline. |
| Disabled | `account_disabled` | UI description: the account was stopped because of consecutive failures, wrong account password and similar. |
| Risk verification | `security_verification` | Xianyu asks for a slider, face or QR-code security check. |
| Order-created price change | `automation_order_created` | The "order-created price change" automation task reaches a success or failure end state. |
| Paid delivery | `automation_order_paid` | The "paid delivery" automation task reaches a success or failure end state (post-payment delivery of bargain orders belongs here too). |
| Review gift | `automation_buyer_reviewed` | The "review gift" automation task reaches a success or failure end state. |
| Review request | `automation_review_missing_timeout` | The "review request" automation task reaches a success or failure end state. |
| Manual delivery result | `manual_delivery_result` | Result of a manual full delivery or manual confirm-shipment on the order page; the title reads "[Manual delivery] success/failure". |
| Manual handling required | `manual_intervention_required` | An automation stopped because the outcome is uncertain, state is missing, or it cannot safely continue, and you must check the order; critical level. |
| Renewal | `token_renewal` | One notification when API automatic renewal fails 3 times in a row. |
| System error | `system_error` | UI description: background task or system-level exception. |

Notes:

- When an automation run reaches the "needs manual review" state, the notification sent is "Manual handling required"; for backward compatibility, channels that subscribed only to the matching automation category (for example "Paid delivery") receive it too.
- The legacy combined "trade" subscription (`delivery_result`) still works: it lets through the four automation events, "Manual delivery result" and "Manual handling required", and editing that channel in the UI expands it into those six.
- The first line of every notification is "[Type] success/failure" (no suffix for events without a success/failure meaning), so a phone push preview shows the event at a glance; it is followed by the level (info/warning/critical), account, time and event details.

## Channel types and fields

| Channel | Required | Optional |
| --- | --- | --- |
| Bark | Bark server (e.g. `https://api.day.app`), Device Key | title (defaults to the first line of the notification), sound (default `default`), group (default `xianyu`) |
| DingTalk bot | Webhook URL | signing secret (generated when "sign" is ticked, starts with `SEC`) |
| Feishu bot | Webhook URL | signature secret (generated when "signature verification" is chosen) |
| WeCom bot | Webhook URL | — |
| Telegram | Bot Token, Chat ID (`-100…` for groups, your user ID for private chat) | — |
| Email | recipient address; sender is the system SMTP, or turn on "use separate SMTP" and fill in a complete set | in separate-SMTP mode: SMTP server, port (default 587), login, password/app password and sender address are all required; sender display name is optional; STARTTLS and SSL/TLS direct are mutually exclusive |
| Custom webhook | Webhook URL | method (default POST), headers (JSON) |

Three more points:

- DingTalk receives a Markdown message, with timestamp and signature added when a signing secret is set; Feishu receives a text message, signed when a secret is set; WeCom and Telegram receive text messages. Telegram always goes through `api.telegram.org`.
- A custom webhook receives a JSON body: `{"message": "...", "timestamp": "2026-01-02 15:04:05", "source": "xianyu-auto-reply"}`, always with `Content-Type: application/json` (the "headers" field can add or override headers); a status below 300 counts as success and 300+ as failure, and the response body is not inspected.
- For the other HTTP channels (Bark, DingTalk, Feishu, WeCom, Telegram) the status must be below 300 and, if the response is JSON, it must not carry a failure marker (`errcode` or `StatusCode` non-zero, `code` neither 0 nor 200, or `ok` equal to `false`); otherwise the send counts as failed.

After saving a channel, verify it with the "Test" button on the channel row. It sends a fixed test message straight to that channel, independent of event subscriptions and account bindings.

## Email and SMTP

- The system SMTP is saved by an administrator in "SMTP mail settings" at the bottom of the Notification Settings page (visible to administrators only) and is inherited in full by email channels by default; with "use separate SMTP" on, that channel uses only its own complete set, and the two sets are never mixed.
- Sending needs at least an SMTP server, a login and a recipient; if no sender address is set, the login address is used as the sender. QQ Mail needs an authorization code, not the login password.
- STARTTLS is on by default (typical for port 587); ticking "SSL/TLS direct" (typical for port 465) switches to direct TLS, and the two options are mutually exclusive. If STARTTLS is required but the server does not support it, the send fails.
- Authentication prefers `PLAIN`; when the server offers only `LOGIN`, `LOGIN` is used automatically.
- With SendCloud, enter `API_USER` as the SMTP login and the matching `API_KEY` as the password; the sender address is entered separately.
- The subject is always "闲鱼自动发货通知" and the body is plain text.

## Delivery, retries and "uncertain" notifications

Business notifications are first written to the database outbox, then a background worker claims a batch (up to 20) every 2 seconds and sends it, so order and account handling never wait for the external network.

- A failed send is retried at intervals starting at 5 seconds and doubling each time (capped at 640 seconds); if the 10th attempt still fails the message is marked failed (`dead`) and is not retried.
- Automation end-state notifications use "run ID + end state" as an idempotency key, so the same end state is never queued twice.
- When the external channel accepted the message but the local confirmation write failed, the notification is quarantined as *uncertain* and is **not re-sent automatically**, to avoid duplicate pushes. Administrators can list them through the admin API, see [Admin API](Admin-API).
- Creating a channel alone does not make an account use it: automation result notifications also require the account to be bound to the channel and the channel's event filter to include the matching category.

Webhook and bot tokens are usually equivalent to passwords; keep them out of screenshots, issues and chat logs.
