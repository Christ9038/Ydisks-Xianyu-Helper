# 管理 API

[English](Admin-API) | 中文

这些接口**没有对应的后台页面**，面向运维人员和脚本。唯一的契约来源是 `api/openapi.yaml`；调用前需要先通过 `POST /api/v1/session/login` 登录并携带会话 Cookie（`cookieAuth`），错误统一使用标准错误响应。

| 接口 | 方法 | 作用 |
| --- | --- | --- |
| `/api/v1/admin/stats` | GET | 汇总数：用户、账号（`total_cookies` / `active_cookies`）、卡密组、关键词、订单。 |
| `/api/v1/admin/users` | GET | 用户列表：`id`、`username`、`email`、`is_active`、`is_admin`、`created_at`、`cookie_count`。 |
| `/api/v1/admin/users/{user_id}` | DELETE | 删除用户；删除当前登录用户会返回 400。 |
| `/api/v1/admin/cookies` | GET | 跨用户的账号摘要：`id`、`user_id`、`remark`、`owner`、`enabled`、`created_at`，**不会返回 Cookie 或 Token 内容**。 |
| `/api/v1/admin/tasks` | GET | 最近的后台任务（`limit` 1–128）：`id`、`name`、`state`（`running` / `succeeded` / `failed` / `canceled` / `timed_out`）、`started_at`、`finished_at`、`deadline_at`。 |
| `/api/v1/admin/notifications/outbox/uncertain` | GET | 被隔离为“不确定”的通知：`total` 与 `items`（`id`、`channel_id`、`owner_user_id`、`event_type`、`attempt_count`、`uncertain_at`、`has_error`），不暴露错误文本。 |
| `/api/v1/notifications/outbox/uncertain` | GET | 同一份列表，但只包含调用者自己的渠道。 |
| `/api/v1/admin/password` | POST | 修改管理员密码（后台页面使用 `/api/v1/session/password`）。 |

说明：

- “不确定”通知表示外部服务大概率已收到，但本地确认失败。它们不会自动重发，请先到目标渠道核对再决定，见[通知事件参考](通知事件参考.zh-CN)。
- 任务接口可与[运行日志](运行日志.zh-CN)配合，判断后台任务是否卡住。

相关：[配置参考](配置参考.zh-CN)、[系统与AI](系统设置.zh-CN)。
