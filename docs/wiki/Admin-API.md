# Admin API

[中文](管理API.zh-CN) | English

The admin endpoints in the first section have **no console page** and are meant for operators and scripts. The external API section below documents the separate console-managed, user-scoped API access tokens. The only contract source is `api/openapi.yaml`; every call needs a logged-in session cookie (`cookieAuth`) from `POST /api/v1/session/login`, and errors use the unified error envelope. Unauthenticated calls get 401; apart from the notification list restricted to the caller's own channels, every endpoint below requires an administrator and returns 403 to other users.

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


## External API access tokens

The console now provides a dedicated **API Management** page in the main navigation. A signed token represents the current Ydisks user; it does not expose any Xianyu Cookie.

- `GET /api/v1/access-tokens` lists the current user's token summaries.
- `POST /api/v1/access-tokens` issues a token. The body contains `name`, `scopes`, and optional `expires_in_days` (90 days by default; 1–365 days allowed). The plaintext `token` is returned once, only in the creation response.
- `DELETE /api/v1/access-tokens/{token_id}` revokes a token owned by the current user. New Bearer requests using it are rejected immediately.

Token-management endpoints require a browser Cookie session and cannot be called with an API token. The server stores only the token's SHA-256 digest. List responses expose `id`, `name`, `prefix`, `scopes`, and creation/expiry/last-used/revocation times; they never expose the digest or plaintext. Send the token in the standard header:

```http
Authorization: Bearer ydk_live_…
```

### Scopes

| Scope | Allowed capability |
| --- | --- |
| `accounts:read` | Read the current user's account IDs, non-sensitive summaries, and single-account details; Cookie and login passwords are excluded. |
| `items:read` | Read items, sync item lists, request publish-category candidates, and read batch results. |
| `items:publish` | Publish a single item, preview/start a batch, cancel a batch, and retry failed rows. |
| `items:unpublish` | Unlist an item owned by the current user on Xianyu; it does not delete the platform item or local record. |
| `orders:read` | Read order lists and details, including stored recipient name, phone, and address; delivered-card endpoints are excluded. |
| `chat:read` | Read chat sessions, messages, item context, quick replies, and buyer notes. |
| `chat:send` | Send ordinary text messages through the existing chat application. |
| `chat:takeover` | Update only the manual-intervention pause duration and scope; account credentials and Cookie fields are not writable. |
| `replies:manage` | Manage AI and item-level AI settings, keyword, item, first-contact, default, and external Agent reply settings. |
| `automation:read` | Read automation rules, issues, audit events, account-task settings, and task runs. |
| `automation:manage` | Manage automation rules and delivery templates, update and trigger existing account tasks, and resolve pending runs/tasks. |

Authorization is enforced by HTTP method and path. An endpoint not explicitly included in the external-token allowlist returns 403 even when the token belongs to the same user. Existing browser Cookie behavior is unchanged. API tokens never receive administrator privileges and cannot call account login, credential updates, system settings, card-secret, notification-secret, or token-management endpoints.

### Manual intervention and scheduled tasks

- `PUT /api/v1/chat/manual-takeover/{cookie_id}` uses `chat:takeover` to update the post-human-reply pause window (0–1440 minutes) and scope (`ai` or `all`). This narrow endpoint does not accept Cookie, login-password, or other account fields. Actual human messages continue to be recognized by the existing chat-send/message-echo path; no second takeover state system is introduced.
- `POST /api/v1/chat/messages` uses the normal outbound-message path and preserves existing human-message detection. If a manual-takeover pause is configured, it may pause AI/Agent auto-replies. Configure the existing `agent-replies` integration for automated bot replies; do not use `chat:send` as the bot delivery path.
- `GET /api/v1/accounts/{cid}` is available under `accounts:read` and returns safe account details, including manual-intervention settings. Account writes, credentials, Cookie, and login-information endpoints remain browser-session only.
- `GET/PUT /api/v1/account-tasks/{cid}`, `GET /api/v1/account-tasks/{cid}/runs`, and `POST /api/v1/account-tasks/{cid}/run` expose existing account-task settings, run history, and manual trigger for existing auto-rating/auto-polish tasks. This is not scheduled product publishing.
- `chat:read` exposes HTTP session/message queries, not the chat WebSocket stream. The automation audit endpoint is not a per-message AI/Agent provenance log; no dedicated reply-source audit endpoint currently exists.

The current code has no dedicated delayed/scheduled product-publishing endpoint or scheduler owner. This work does not create a second publishing queue; batch publishing continues to use the existing preview, start, status-readback, cancel, and failed-row retry APIs.

### Existing item, account, chat, and automation routes

The external token scopes expose the following existing application routes; these are not new workers or parallel business implementations:

- `POST /api/v1/items/publish` publishes one item through the existing publish use case.
- `POST /api/v1/items/publish-batches/preview` previews an uploaded sheet using `multipart/form-data`; `POST /api/v1/items/publish-batches` starts the existing batch worker.
- `GET /api/v1/automation-audit-events` reads existing automation audit summaries.
- `GET /api/v1/accounts/{cid}` reads a safe account detail summary.
- `GET/PUT /api/v1/account-tasks/{cid}` reads/updates existing account-task settings; `GET /api/v1/account-tasks/{cid}/runs` reads task runs; `POST /api/v1/account-tasks/{cid}/run` manually triggers the existing auto-polish/auto-rate task.
- `GET/PUT /api/v1/settings/ai-reply/{cookie_id}` manages the existing account AI reply settings.
- `GET/PUT /api/v1/agent-replies/{cookie_id}` manages the existing external Agent reply settings. The read response includes `token_configured`, never the saved token plaintext. `POST /api/v1/agent-replies/{cookie_id}/test` tests an endpoint/token draft before saving. Settings also carry `protocol` (`v1` default, `openai`, `anthropic`), `model` and `system_prompt` (omitted on write = keep the stored value); the test request accepts `protocol` and `model`. `POST /api/v1/settings/ai-models` and `/ai-test` accept an optional `protocol` (`openai`/`anthropic`; default is the saved `ai_api_protocol`).
- `GET /api/v1/chat/sessions?account_id={cookie_id}` reads chat sessions; `refresh=1` requests a refresh through the existing platform-contact path.
- `GET /api/v1/chat/messages?account_id={cookie_id}&chat_id={chat_id}` reads message history.
- `POST /api/v1/chat/messages` sends text using the existing chat application. Its request includes `account_id`, `chat_id`, `peer_user_id`, and `text`.
- `PUT /api/v1/chat/manual-takeover/{cookie_id}` updates the narrow manual-takeover settings: `manual_takeover_minutes` and `manual_takeover_scope`.

### Agent integration

The existing outbound Agent HTTP protocol (v1), request/response fields, `working` polling, and reply priority are documented in [Automated Delivery and Replies](Automated-Delivery-and-Replies). The API token only configures and tests that existing Agent integration; it does not create another reply worker. The `AGENT_ENDPOINT_TOKEN` used in examples is the external Agent service credential, not the Ydisks API token.

### Platform item unlisting

**Platform item unlisting** uses the separate `POST /api/v1/items/{cookie_id}/{item_id}/unpublish` endpoint and the `items:unpublish` scope. It handles one item per request and unlists the item on Xianyu without deleting the platform item or local record. The existing `DELETE /api/v1/items/{cookie_id}/{item_id}` still deletes only the local record. The implementation follows the public PC item-detail-page network capture; real-account acceptance is still pending, so local mock tests are not evidence of a successful platform operation. Protocol capture: [request documentation](https://github.com/DoLovya/pyxianyu/blob/main/docs/mtop_taobao_idle_item_downshelf.md); request-parameter implementation reference: [item_api.py](https://github.com/DoLovya/pyxianyu/blob/main/src/pyxianyu/apis/item_api.py).

**Request rate.** Each unlist call sends one platform request for one item; there is no batch form and no fallback request. Requests go through the same per-account pacer as every other platform call (at least 2 seconds between requests to the same account, plus jitter), so looping over many items is slowed down but not prevented. Do not loop: stop sending further requests when you receive a `502` or the platform asks for security verification, refresh the item status first, and do not retry automatically.

Related: [Configuration Reference](Configuration-Reference), [System and AI](System-and-AI).
