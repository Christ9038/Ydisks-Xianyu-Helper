# 管理 API

[English](Admin-API) | 中文

这些接口**没有对应的后台页面**，面向运维人员和脚本。唯一的契约来源是 `api/openapi.yaml`；调用前需要先通过 `POST /api/v1/session/login` 登录并携带会话 Cookie（`cookieAuth`），错误统一使用标准错误响应。未登录返回 401；除“只含调用者自己的渠道”的通知列表外，下表接口都要求管理员，非管理员返回 403。

| 接口 | 方法 | 作用 |
| --- | --- | --- |
| `/api/v1/admin/stats` | GET | 汇总数：`total_users`、`total_cookies` / `active_cookies`（`active_cookies` 为未被停用的账号数）、`total_cards`（卡密组）、`total_keywords`、`total_orders`（不含已删除订单）。 |
| `/api/v1/admin/users` | GET | 用户列表（按 `id` 升序）：`id`、`username`、`email`、`is_active`、`is_admin`、`created_at`、`cookie_count`。 |
| `/api/v1/admin/users/{user_id}` | DELETE | 删除用户。`user_id` 不是整数或删除当前登录用户都会返回 400；删除前会先停止该用户名下所有账号的运行实例，任一账号停止失败则返回 500 且不删除。 |
| `/api/v1/admin/cookies` | GET | 跨用户的账号摘要（按 `created_at` 倒序）：`id`、`user_id`、`remark`、`owner`（所属用户名）、`enabled`、`created_at`，**不会返回 Cookie 或 Token 内容**；没有账号时响应为 `null`。 |
| `/api/v1/admin/tasks` | GET | 当前服务进程记录的后台任务（按开始时间倒序；`limit` 可选，1–128，默认 128，超出范围返回 400）：`id`（进程内标识，不是数据库主键）、`name`、`state`（`running` / `succeeded` / `failed` / `canceled` / `timed_out`）、`started_at`，以及可能缺省的 `finished_at`、`deadline_at`。只保存名称、时间和状态，历史有上限，服务重启后清空。 |
| `/api/v1/admin/notifications/outbox/uncertain` | GET | 全部用户被隔离为“不确定”的通知，按 `uncertain_at` 倒序：`total` 与 `items`（`id`、`channel_id`、`owner_user_id`、`event_type`、`attempt_count`、`uncertain_at`（Unix 秒）、`has_error`），不暴露正文和错误文本。可选 `limit`（`api/openapi.yaml` 未声明），默认 50，小于 1 或大于 100 时按 20 处理。 |
| `/api/v1/notifications/outbox/uncertain` | GET | 同一份列表，但任何已登录用户都可调用，只包含调用者自己渠道的记录，且不含 `owner_user_id`；`limit` 默认 20，范围规则同上。 |
| `/api/v1/admin/password` | POST | 修改当前管理员的密码，请求体 `current_password`、`new_password`（至少 8 个字符，否则 400）；当前密码错误返回 401。成功后清除会话 Cookie，响应 `requires_relogin: true`，需重新登录。（后台页面使用 `/api/v1/session/password` 与 `/api/v1/session/credentials`。） |

说明：

- “不确定”通知表示外部服务大概率已收到，但本地确认失败。它们不会自动重发，请先到目标渠道核对再决定，见[通知事件参考](通知事件参考.zh-CN)。
- 任务接口可与[运行日志](运行日志.zh-CN)配合，判断后台任务是否卡住。

相关：[配置参考](配置参考.zh-CN)、[系统与AI](系统设置.zh-CN)。
