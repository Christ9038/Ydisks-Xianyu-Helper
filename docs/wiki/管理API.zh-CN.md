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

## 外部 API 访问令牌

控制台主导航新增独立的「API 管理」页面。令牌代表当前 Ydisks 用户，不包含闲鱼 Cookie，也不会获得管理员权限。

- `GET /api/v1/access-tokens`：读取当前用户已签发令牌的摘要。
- `POST /api/v1/access-tokens`：签发令牌，请求字段为 `name`、`scopes` 和可选的 `expires_in_days`（默认 90 天，范围 1–365 天）。
- `DELETE /api/v1/access-tokens/{token_id}`：撤销当前用户拥有的令牌。撤销后，新请求立即失效。
- 明文令牌只在签发响应中返回一次；服务端只保存 SHA-256 摘要。列表不返回明文或摘要。

调用时使用标准 Bearer 请求头：

```http
Authorization: Bearer ydk_live_…
```

### 快速调用示例

先在控制台「API 管理」页面签发令牌，并只把明文保存在调用方的 Secret 管理位置：

```bash
export YDISKS_BASE_URL="https://your-ydisks-host"
export YDISKS_API_TOKEN="ydk_live_..."
export COOKIE_ID="your-account-id"

# 读取当前用户的账号摘要
curl -fsS "$YDISKS_BASE_URL/api/v1/accounts/details" -H "Authorization: Bearer $YDISKS_API_TOKEN"

# 查询账号会话；refresh=1 会调用现有平台联系人刷新链路
curl -fsS "$YDISKS_BASE_URL/api/v1/chat/sessions?account_id=$COOKIE_ID&refresh=1" -H "Authorization: Bearer $YDISKS_API_TOKEN"

# 下架一个商品；这是平台状态变更，不是本地删除
curl -fsS -X POST "$YDISKS_BASE_URL/api/v1/items/$COOKIE_ID/your-item-id/unpublish" -H "Authorization: Bearer $YDISKS_API_TOKEN"
```

配置现有外部 Agent 回复（需要 `replies:manage`）：

```bash
curl -fsS -X PUT "$YDISKS_BASE_URL/api/v1/agent-replies/$COOKIE_ID" -H "Authorization: Bearer $YDISKS_API_TOKEN" -H "Content-Type: application/json" -d '{"enabled":true,"endpoint_url":"https://agent.example.com/xianyu","request_timeout_seconds":10,"max_session_seconds":60,"order_context_enabled":true,"token":{"action":"replace","value":"AGENT_ENDPOINT_TOKEN"}}'
```

保存前应先用 `POST /api/v1/agent-replies/{cookie_id}/test` 测试 Agent 地址和协议响应。上面的 `AGENT_ENDPOINT_TOKEN` 是你自己的 Agent 服务访问令牌，不是 Ydisks API 令牌。

令牌管理接口只接受浏览器登录 Cookie，不接受 Bearer Token。登录、初始化、会话验证/退出、密码和账号凭据接口也拒绝 Bearer Token。

### 权限范围

| Scope | 能力 |
| --- | --- |
| `accounts:read` | 读取当前用户的账号 ID、非敏感摘要和单账号详情；不返回 Cookie 或登录密码。 |
| `items:read` | 读取商品、同步商品列表、请求发布类目候选、读取批量发布结果。 |
| `items:publish` | 单商品发布、批量发布预检/启动、取消批次和重试失败行。 |
| `items:unpublish` | 将当前用户拥有的商品从闲鱼平台下架；不删除平台商品或本地记录。 |
| `orders:read` | 读取订单列表和详情，**包含订单已有的收件人姓名、电话和地址**；不开放已发货卡密接口。 |
| `chat:read` | 读取聊天会话、消息正文、商品上下文、快捷回复和买家备注。 |
| `chat:send` | 通过现有聊天应用发送普通文本消息。 |
| `chat:takeover` | 仅修改人工回复后的暂停时长和范围，不允许写入账号凭据或 Cookie。 |
| `replies:manage` | 管理 AI/商品 AI 设置、关键词、商品首次回复、默认回复和外部 Agent 回复配置。 |
| `automation:read` | 读取自动化规则、异常、自动化审计事件、账号任务设置和运行记录。 |
| `automation:manage` | 管理自动化规则和发货模板，更新/手动触发现有账号任务，处理待处理运行和任务。 |

授权由 HTTP 方法和路径共同决定。未明确加入白名单的接口即使属于同一用户，也会返回 403。浏览器 Cookie 会话行为不变。API 令牌不能调用管理员接口、账号登录/凭据更新、系统设置、卡密秘密、通知渠道秘密或令牌管理接口。

### 账号详情与账号任务

- `GET /api/v1/accounts/{cid}`：读取当前用户拥有的单账号非敏感详情。
- `GET/PUT /api/v1/account-tasks/{cid}`：读取/更新现有账号任务设置。
- `GET /api/v1/account-tasks/{cid}/runs`：读取账号任务运行记录。
- `POST /api/v1/account-tasks/{cid}/run`：手动触发现有自动擦亮/自动评价任务；这不是商品定时上架。

### 商品发布与下架

- 单商品发布和批量发布复用现有 `POST /api/v1/items/publish`、`POST /api/v1/items/publish-batches/preview`、`POST /api/v1/items/publish-batches`。
- 批次可读取状态和结果、取消或重试失败行。批量预检使用 `multipart/form-data`，具体字段以 `api/openapi.yaml` 为准。
- 当前没有专门的延迟/定时商品发布接口或调度 Owner。本次不新建第二套发布队列；批量发布仍由现有预检、启动和批次管理链路执行。
- 平台下架使用独立的 `POST /api/v1/items/{cookie_id}/{item_id}/unpublish` 和 `items:unpublish` 权限。它只在闲鱼平台下架商品，不删除平台商品或本地商品记录；现有 `DELETE /api/v1/items/{cookie_id}/{item_id}` 仍只删除本地记录。
- **请求频率**：每次下架调用只对一个商品发出一次平台请求，没有批量形式，也没有备用接口。请求与其他平台调用一样经过账号级节流器（同一账号请求起点间隔至少 2 秒并附加随机抖动），因此循环调用会被拉慢但不会被禁止。请不要循环下架：收到 `502` 或平台要求安全验证后必须停止后续请求，先刷新商品状态确认，不要自动重试。
- 下架接口一次处理一个商品；当前没有批量下架端点，外部调用方如需批量处理，应逐个调用并逐项核对结果。
- 下架协议依据公开 PC 商品详情页抓包资料接入，但尚未使用真实账号完成外部验收；本地 Mock 测试不能证明真实平台已下架。本任务没有执行生产下架或发布操作。

### AI 客服与 Agent 回复

- `GET/PUT /api/v1/settings/ai-reply/{cookie_id}`：读取/更新账号 AI 回复配置。
- `GET/PUT /api/v1/agent-replies/{cookie_id}`：读取/更新账号外部 Agent 回复配置；配置另含 `protocol`（`v1` 默认、`openai`、`anthropic`）、`model` 与 `system_prompt`（写入时省略表示保留已存值）。`POST /api/v1/settings/ai-models` 与 `/ai-test` 可选携带 `protocol`（`openai`/`anthropic`，缺省取已保存的 `ai_api_protocol`），测试请求可携带 `protocol` 与 `model`。
- `POST /api/v1/agent-replies/{cookie_id}/test`：按提交的地址和令牌草稿测试 Agent 连接。令牌写入支持 `retain`、`replace`、`clear`；读取响应只返回 `token_configured`，不会返回令牌明文。
- 现有回复优先级和 Agent 协议见[自动化发货与回复](自动化发货与回复.zh-CN)。本次只通过 API 暴露现有 AI/关键词/Agent 能力，不创建第二套回复 Worker。

### 消息查询、发送与人工接管

- `GET /api/v1/chat/sessions?account_id={cookie_id}`：读取账号会话；需要刷新平台联系人时可传 `refresh=1`。
- `GET /api/v1/chat/messages?account_id={cookie_id}&chat_id={chat_id}`：读取消息历史。
- `POST /api/v1/chat/messages`：发送文本消息，请求至少包含 `account_id`、`chat_id`、`peer_user_id`、`text`。该接口走普通外发路径，会沿用现有人工消息识别；当账号设置了人工接管暂停时，可能暂停 AI/Agent 自动回复。
- `PUT /api/v1/chat/manual-takeover/{cookie_id}`：只更新人工介入暂停设置，字段为 `manual_takeover_minutes`（0–1440）、`manual_takeover_scope`（`ai` 或 `all`）和 `reply_merge_seconds`（买家消息合并等待秒数，1–10）。
- `chat:read` 提供 HTTP 会话/消息查询，不开放 WebSocket 实时订阅。外部调用方应按需查询，不要高频刷新平台历史。
- 外部机器人自动回复应配置现有 Agent 回复接口（`agent-replies`）并遵循既有 Agent 协议；不要把 `chat:send` 当作自动回复发送端，否则会被人工外发识别。

### 自动化审计边界

`GET /api/v1/automation-audit-events` 返回现有自动化规则行为审计摘要，不含消息正文、卡密正文或凭证。聊天消息历史可用于核对已保存的收发消息；当前没有单独的逐条 AI/Agent 回复来源审计接口，不能把自动化规则审计误认为完整的 AI 回复审计。

### 安全与验收边界

- 所有资源操作仍按当前令牌所属用户进行账号归属校验；未授权的路径/方法默认拒绝。
- `orders:read` 会返回订单 DTO 中已有的收件人姓名、电话和地址，签发该权限前应确认外部系统确有读取这些个人信息的必要。
- 下架真实平台结果仍待真实账号验收；不得以本地 Mock PASS 代替平台验收。
