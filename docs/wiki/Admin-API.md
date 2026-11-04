# Admin API

[中文](管理API.zh-CN) | English

These endpoints have **no console page**. They are meant for operators and scripts. The only contract source is `api/openapi.yaml`; every call needs a logged-in session cookie (`cookieAuth`) from `POST /api/v1/session/login`, and errors use the unified error envelope. Unauthenticated calls get 401; apart from the notification list restricted to the caller's own channels, every endpoint below requires an administrator and returns 403 to other users.

| Endpoint | Method | Purpose |
| --- | --- | --- |
| `/api/v1/admin/stats` | GET | Totals: `total_users`, `total_cookies` / `active_cookies` (`active_cookies` counts accounts that are not disabled), `total_cards` (card groups), `total_keywords`, `total_orders` (deleted orders excluded). |
| `/api/v1/admin/users` | GET | List users (ascending by `id`): `id`, `username`, `email`, `is_active`, `is_admin`, `created_at`, `cookie_count`. |
| `/api/v1/admin/users/{user_id}` | DELETE | Delete a user. A non-integer `user_id`, or deleting yourself, returns 400. The runtimes of all accounts owned by that user are stopped first; if any account fails to stop, the call returns 500 and nothing is deleted. |
| `/api/v1/admin/cookies` | GET | Account summaries across users (newest `created_at` first): `id`, `user_id`, `remark`, `owner` (owner's username), `enabled`, `created_at`. **No Cookie or Token values are returned.** The response is `null` when there are no accounts. |
| `/api/v1/admin/tasks` | GET | Background tasks recorded by the current server process (newest start first; optional `limit`, 1–128, default 128, out-of-range returns 400): `id` (process-local, not a database key), `name`, `state` (`running` / `succeeded` / `failed` / `canceled` / `timed_out`), `started_at`, and `finished_at` / `deadline_at` when present. Only names, times and states are kept, history is capped, and it is cleared on restart. |
| `/api/v1/admin/notifications/outbox/uncertain` | GET | Notifications of all users quarantined as *uncertain*, newest `uncertain_at` first: `total` and `items` with `id`, `channel_id`, `owner_user_id`, `event_type`, `attempt_count`, `uncertain_at` (Unix seconds), `has_error`. Neither the message body nor the error text is exposed. Optional `limit` (not declared in `api/openapi.yaml`), default 50; a value below 1 or above 100 is treated as 20. |
| `/api/v1/notifications/outbox/uncertain` | GET | Same list, callable by any logged-in user, restricted to the caller's own channels and without `owner_user_id`; `limit` defaults to 20, same range rule. |
| `/api/v1/admin/password` | POST | Change the current administrator's password. Body: `current_password`, `new_password` (at least 8 characters, otherwise 400); a wrong current password returns 401. On success the session cookie is cleared and the response carries `requires_relogin: true`, so log in again. (The console uses `/api/v1/session/password` and `/api/v1/session/credentials`.) |

Notes:

- Uncertain notifications mean the external service probably accepted the message but the local confirmation failed. They are not re-sent automatically; check the target channel before deciding anything. See [Notification Events](Notification-Events).
- Use the tasks endpoint together with [Runtime Logs](Runtime-Logs) to see whether a background job is stuck.

Related: [Configuration Reference](Configuration-Reference), [System and AI](System-and-AI).
