# HTTP API 版本化迁移边界

本文记录 API 版本化边界。它不定义重构阶段编号、当前状态或完成结论；新版本路由必须先完成前端调用方迁移和契约测试，再删除旧别名。

## 当前兼容路径

| 领域 | 现有路径 | 目标 `/api/v1` 路径 | 迁移状态 |
| --- | --- | --- | --- |
| 会话 | `/login`、`/initialize`、`/verify`、`/logout`、`/change-password`、`/account/credentials` | `/api/v1/session/login`、`/api/v1/session/initialize`、`/api/v1/session`、`/api/v1/session/logout`、`/api/v1/session/password`、`/api/v1/session/credentials` | React 会话、密码修改和登录凭证调用已迁移到版本化入口；旧路径保留并由同一 handler 提供 |
| 账号 | `/cookies`、`/cookies/details`、`/cookies/runtime-status`、`/cookies/{cid}/...` | `/api/v1/accounts`、`/api/v1/accounts/details`、`/api/v1/accounts/runtime-status`、`/api/v1/accounts/{cid}/...` | React 摘要、详情、运行状态、启停状态、设置、备注、暂停、自动确认、长登录、资料刷新、Cookie 更新、登录信息设置和删除调用已迁移；旧路径保留并由同一 handler 提供 |
| 商品 | `/items...` | `/api/v1/items...` | React 列表、按账号列表、详情、创建、发布、更新、删除、多规格/多数量设置、同步、类目推荐和批量发布调用已迁移；旧路径保留并由同一 handler 提供 |
| 商品批量 | `/items/publish-batches...` | `/api/v1/items/publish-batches...` | 旧路径保留，类目推荐/预检/任务 DTO 已落地 |
| 自动化 | `/automation-rules...`、`/automation-issues`、`/automation-runs...`、`/automation-pending-tasks...` | `/api/v1/automation-rules...`、`/api/v1/automation-issues`、`/api/v1/automation-runs...`、`/api/v1/automation-pending-tasks...` | React 规则分页、增删改、异常查询和异常处理调用已迁移；旧路径保留并由同一 handler 提供 |
| 订单 | `/api/orders...` | `/api/v1/orders...` | React 列表、详情、更新、删除、刷新、单订单刷新、手动发货和导入调用已迁移；旧路径保留并由同一 handler 提供 |
| 设置 | `/system-settings...`、`/user-settings...`、`/ai-reply-settings...`、`/ai-models` | `/api/v1/settings/system...`、`/api/v1/settings/user...`、`/api/v1/settings/ai-reply...`、`/api/v1/settings/ai-models` | React 系统设置、AI 设置调用已迁移；版本化入口覆盖公开设置、管理员设置、用户设置和 AI 模型查询；旧路径保留 |
| 卡券 | `/cards...` | `/api/v1/cards...` | React 卡券列表、CRUD、批量创建和追加调用已迁移；旧路径保留并由同一 handler 提供 |
| 通知 | `/notification-channels...`、`/message-notifications...` | `/api/v1/notifications/channels...`、`/api/v1/notifications/messages...`、`/api/v1/notifications/accounts/{cid}/bindings` | React 通知渠道、消息绑定和账号绑定调用已迁移；旧路径保留并由同一 handler 提供 |
| 聊天 | `/api/chat...` | `/api/v1/chat...` | React 会话、消息、图片、已读和 WebSocket 调用已迁移；旧路径保留并由同一 handler 提供 |
| 关键词回复 | `/keywords...`、`/keywords-with-item-id...`、`/keywords-with-type...`、`/item-reply...`、`/itemReplays` | `/api/v1/reply-rules/{cid}...`、`/api/v1/reply-rules/items...` | React 类型规则、指定商品回复调用已迁移；版本化入口覆盖基础/商品/类型规则和指定商品回复；旧路径保留并由同一 handler 提供 |
| 默认回复 | `/default-replies...`、`/api/default-replies`、`/api/default-reply...` | `/api/v1/default-replies`、`/api/v1/default-replies/list`、`/api/v1/default-replies/{cid}...` | React 单账号、列表、删除和记录清理调用已迁移；旧路径保留并由同一 handler 提供 |
| 账号任务 | `/api/account-tasks...` | `/api/v1/account-tasks...` | React 设置和执行调用已迁移；版本化入口覆盖设置、运行记录和执行摘要；旧路径保留并由同一 handler 提供 |
| 管理员 | `/admin/users...`、`/admin/cookies...`、`/admin/stats`、`/change-admin-password` | `/api/v1/admin/users...`、`/api/v1/admin/cookies...`、`/api/v1/admin/stats`、`/api/v1/admin/password` | React 管理员统计调用已迁移；管理员密码也提供版本化兼容入口并保留权限边界；旧路径保留并由同一 handler 提供 |
| 统计 | `/dashboard/stats`、`/analytics/orders...` | `/api/v1/analytics/dashboard`、`/api/v1/analytics/orders...` | React 仪表盘、订单分析和有效订单调用已迁移；旧路径保留并由同一 handler 提供 |
| 二维码登录 | `/qr-login/generate`、`/qr-login/check/{session_id}`、`/qr-login/status/{session_id}`、`/qr-login/complete-verification/{session_id}` | `/api/v1/qr-login/generate`、`/api/v1/qr-login/check/{session_id}`、`/api/v1/qr-login/status/{session_id}`、`/api/v1/qr-login/complete-verification/{session_id}` | React 二维码生成、状态轮询和风控验证完成调用已迁移；状态持久化兼容入口同步提供；旧路径保留并由同一 handler 提供 |
| 密码登录 | `/password-login`、`/password-login/check/{session_id}`、`/password-login/cancel/{session_id}` | `/api/v1/password-login`、`/api/v1/password-login/check/{session_id}`、`/api/v1/password-login/cancel/{session_id}` | React 密码登录、状态查询和取消调用已迁移；当前仍返回明确的禁用错误；旧路径保留并由同一 handler 提供 |

## 迁移规则

1. 新增接口只能使用 `/api/v1` 前缀，禁止继续增加未版本化的业务路径。
2. 旧路径通过同一 handler 或薄适配器保留，不复制业务逻辑；适配器只负责路径和请求/响应兼容。
3. 前端请求层先切换到 `/api/v1`，并保留旧响应字段的边界归一；Go、React 和外部调用方契约测试全部通过后，才可以删除旧别名。
4. 删除旧路径前必须记录调用方、发布日期、迁移版本和回滚方案；发布说明要明确旧路径的移除版本。
5. 错误响应始终使用 `code`、`message`、`request_id`，恢复信息只能放入 `details`；版本适配器不得重新引入 `detail`、`msg` 或 HTTP 200 + `success:false`。

## API 版本化证据

- 认证会话校验使用 `sessionVerificationResponse`。
- 账号详情、设置、资料刷新和暂停查询使用 `cookieDetailResponse`、`cookieSettingsResponse`、`cookieProfileResponse` 与 `pauseDurationResponse`。
- 商品发布、同步、列表和详情使用 `itemPublishResponse`、`itemSyncResponse`、`itemPageSyncResponse`、`itemListResponse` 与 `itemDetailResponse`。
- 商品批量发布使用 `categoryRecommendationResponse`、`itemPublishBatchPreviewResponse`、`batchIDResponse`、`batchCancelResponse`、`itemPublishBatchResponse` 与 `itemPublishBatchListResponse`。
- 自动化规则分页、动作和异常列表使用 `automationRulePageResponse`、`automationRuleResponse`、`automationActionResponse` 与 `automationIssuesResponse`。
- 订单列表、详情、刷新和批量外层结果使用 `orderListResponse`、`orderDetailResponse`、`orderRefreshResponse` 与 `manualShipResponse`/`importOrdersResponse`。
- 设置、卡券和通知分别使用 `operationResponse`、`aiReplySettingsResponse`、`aiModelsResponse`、`userSettingResponse`、`cardResponse`、`cardBatchResponse`、`notificationChannelResponse`、`notificationBindingResponse` 与 `accountBindingsResponse`。
- 关键词回复、默认回复和账号任务分别使用 `keywordBasicResponse`/`keywordItemResponse`/`keywordTypedResponse`、`itemReplyResponse`、`defaultReplyResponse`、`accountTaskSettingsResponse`、`accountTaskRunsResponse` 与 `accountTaskRunResponseEnvelope`。
- 管理员和统计分别使用 `adminUserResponse`/`adminCookieResponse`/`adminStatsResponse`、`dashboardStatsResponse`、`orderAnalyticsResponse` 与 `validOrdersResponse`；二维码生成使用 `qrLoginGenerateResponse`。
- 动态设置和二维码兼容边界分别使用 `settingsResponse`、`qrLoginStatusResponse` 与 `qrLoginVerificationResponse`；二维码状态契约测试确认 `cookies` 等敏感字段不会回传。
- 会话登录、初始化、校验、登出、密码修改和登录凭证更新已由 React 请求层迁移到 `/api/v1/session/...`；管理员密码提供 `/api/v1/admin/password` 兼容入口，Go 契约测试确认版本化入口与旧路径共用 handler 且旧路径仍可用。
- 账号摘要、非敏感详情、运行状态、启停状态、设置、备注、暂停、自动确认、长登录、资料刷新、Cookie 更新和登录信息设置已由 React 请求层迁移到 `/api/v1/accounts...`；Go 契约测试确认账号详情与凭证变更响应不泄露 Cookie/密码，且旧详情、设置和登录信息入口仍可用。
- 订单列表、详情和更新已由 React 请求层迁移到 `/api/v1/orders...`；Go 契约测试确认版本化入口与旧更新入口共用 handler，订单归属校验和状态更新语义保持不变。
- 订单刷新、单订单刷新、手动发货和导入已由 React 请求层迁移到 `/api/v1/orders...`；Go 契约测试确认新旧批量入口保持状态码、具名响应和参数校验语义一致。
- 商品列表、按账号列表、详情、创建、发布、更新、删除、多规格/多数量设置已由版本化路由提供，React 请求层已迁移实际调用；Go 契约测试确认新旧入口保持商品所有权校验、具名响应和持久化语义一致。
- 商品同步、类目推荐、批量发布预检、任务查询、取消、重试和结果下载已由版本化路由提供；React 现有同步/批量发布调用已迁移，Go 契约测试确认新旧入口状态码和参数校验语义一致。
- 系统/用户/AI 设置、卡券 CRUD/批量/追加和通知渠道/消息/账号绑定已由版本化路由提供；React 设置、卡券和通知调用已迁移，Go 契约测试确认新旧入口权限、状态码和参数校验语义一致。
- 聊天会话、消息、图片、已读和 WebSocket，以及账号任务设置、运行记录和执行已由版本化路由提供；React REST/WebSocket 调用已迁移，Go 契约测试确认新旧入口权限、状态码和参数校验语义一致。
- 关键词基础/商品/类型规则、指定商品回复和默认回复列表/单账号/清理已由版本化路由提供；React 关键词与默认回复调用已迁移，Go 契约测试确认新旧入口权限、状态码和参数校验语义一致。
- 管理员用户/账号/统计、仪表盘统计、订单分析和有效订单分页已由版本化路由提供；React 管理员统计与仪表盘调用已迁移，Go 契约测试确认新旧入口权限、状态码和参数校验语义一致，普通用户无法访问管理员版本化入口。
- 二维码生成、扫码状态查询、状态持久化和验证完成已由版本化路由提供；React 生成、轮询和验证完成调用已迁移，Go/React 契约测试确认新旧入口认证、会话所有权和状态码语义一致，状态响应继续过滤 Cookie 等敏感字段。
- 会话密码修改、登录凭证更新、账号删除、密码登录禁用入口、自动化规则/异常处理、订单删除和商品创建已由版本化路由提供；React 对应调用已迁移，Go/React 契约测试确认新旧入口权限、状态码和参数校验语义一致。
- 聊天会话、消息分页和发送结果使用独立 DTO，不直接暴露数据库模型。
- React 的 feature API adapter 已直接使用 `frontend/shared/api-contract/*` 中的具名响应 DTO，并在 adapter 边界完成兼容字段归一；通用请求、错误解析和契约校验位于 `frontend/shared/http/*`。
- 会话、账号、订单、商品、自动化、设置、卡券、通知、聊天、账号任务、关键词、默认回复、管理员、统计、二维码登录和密码登录版本化入口已可用；旧路径继续保留。
- 最终调用方审计确认 React 生产请求、批量结果下载和 Vite 开发代理均不再使用业务旧路径；旧路径仅作为服务端兼容入口保留，后续删除必须遵循调用方、发布日期和回滚条件记录。
