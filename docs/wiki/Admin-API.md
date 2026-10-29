# Admin API

[中文](管理API.zh-CN) | English

These endpoints have **no console page**. They are meant for operators and scripts. The only contract source is `api/openapi.yaml`; every call needs a logged-in session cookie (`cookieAuth`) from `POST /api/v1/session/login`, and errors use the unified error envelope.

| Endpoint | Method | Purpose |
| --- | --- | --- |
| `/api/v1/admin/stats` | GET | Totals: users, accounts (`total_cookies` / `active_cookies`), card groups, keywords, orders. |
| `/api/v1/admin/users` | GET | List users: `id`, `username`, `email`, `is_active`, `is_admin`, `created_at`, `cookie_count`. |
| `/api/v1/admin/users/{user_id}` | DELETE | Delete a user. Deleting yourself is rejected with 400. |
| `/api/v1/admin/cookies` | GET | Account summaries across users: `id`, `user_id`, `remark`, `owner`, `enabled`, `created_at`. **No Cookie or Token values are returned.** |
| `/api/v1/admin/tasks` | GET | Recent background tasks (`limit` 1–128): `id`, `name`, `state` (`running` / `succeeded` / `failed` / `canceled` / `timed_out`), `started_at`, `finished_at`, `deadline_at`. |
| `/api/v1/admin/notifications/outbox/uncertain` | GET | Notifications quarantined as *uncertain*: `total` and `items` with `id`, `channel_id`, `owner_user_id`, `event_type`, `attempt_count`, `uncertain_at`, `has_error`. The error text itself is not exposed. |
| `/api/v1/notifications/outbox/uncertain` | GET | Same list, restricted to the caller's own channels. |
| `/api/v1/admin/password` | POST | Change the admin password (the console uses `/api/v1/session/password`). |

Notes:

- Uncertain notifications mean the external service probably accepted the message but the local confirmation failed. They are not re-sent automatically; check the target channel before deciding anything. See [Notification Events](Notification-Events).
- Use the tasks endpoint together with [Runtime Logs](Runtime-Logs) to see whether a background job is stuck.

Related: [Configuration Reference](Configuration-Reference), [System and AI](System-and-AI).
