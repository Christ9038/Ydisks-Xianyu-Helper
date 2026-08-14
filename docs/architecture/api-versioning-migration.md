# HTTP API 版本化迁移边界

本文记录阶段三当前切片完成后的 API 版本化边界。当前仓库尚未切换现有路径；新版本路由必须先完成前端调用方迁移和契约测试，再删除旧别名。

## 当前兼容路径

| 领域 | 现有路径 | 目标 `/api/v1` 路径 | 迁移状态 |
| --- | --- | --- | --- |
| 会话 | `/login`、`/initialize`、`/verify`、`/logout` | `/api/v1/session/login`、`/api/v1/session/initialize`、`/api/v1/session`、`/api/v1/session/logout` | React 会话调用方已迁移到版本化入口；旧路径保留并由同一 handler 提供 |
| 账号 | `/cookies`、`/cookies/details`、`/cookies/runtime-status`、`/cookies/{cid}/...` | `/api/v1/accounts`、`/api/v1/accounts/details`、`/api/v1/accounts/runtime-status`、`/api/v1/accounts/{cid}/...` | React 摘要、详情、运行状态、启停状态、设置、备注、暂停、自动确认、长登录、资料刷新、Cookie 更新和登录信息设置调用已迁移；旧路径保留并由同一 handler 提供 |
| 商品 | `/items...` | `/api/v1/items...` | React 列表、详情、发布、更新、删除、同步、类目推荐和批量发布调用已迁移；旧路径保留并由同一 handler 提供 |
| 商品批量 | `/items/publish-batches...` | `/api/v1/items/publish-batches...` | 旧路径保留，类目推荐/预检/任务 DTO 已落地 |
| 自动化 | `/automation-rules...` | `/api/v1/automation-rules...` | 旧路径保留，规则分页/异常 DTO 已落地 |
| 订单 | `/api/orders...` | `/api/v1/orders...` | React 列表、详情、更新、刷新、单订单刷新、手动发货和导入调用已迁移；旧路径保留并由同一 handler 提供 |
| 设置 | `/system-settings...`、`/user-settings...`、`/ai-reply-settings...`、`/ai-models` | `/api/v1/settings/system...`、`/api/v1/settings/user...`、`/api/v1/settings/ai-reply...`、`/api/v1/settings/ai-models` | React 系统设置、AI 设置调用已迁移；版本化入口覆盖公开设置、管理员设置、用户设置和 AI 模型查询；旧路径保留 |
| 卡券 | `/cards...` | `/api/v1/cards...` | React 卡券列表、CRUD、批量创建和追加调用已迁移；旧路径保留并由同一 handler 提供 |
| 通知 | `/notification-channels...`、`/message-notifications...` | `/api/v1/notifications/channels...`、`/api/v1/notifications/messages...`、`/api/v1/notifications/accounts/{cid}/bindings` | React 通知渠道、消息绑定和账号绑定调用已迁移；旧路径保留并由同一 handler 提供 |
| 聊天 | `/api/chat...` | `/api/v1/chat...` | React 会话、消息、图片、已读和 WebSocket 调用已迁移；旧路径保留并由同一 handler 提供 |
| 关键词回复 | `/keywords...`、`/keywords-with-item-id...`、`/keywords-with-type...`、`/item-reply...` | `/api/v1/reply-rules...` | 旧路径保留，基础/商品/类型规则与指定商品回复 DTO 已落地 |
| 默认回复 | `/default-replies...`、`/api/default-reply...` | `/api/v1/default-replies...` | 旧路径保留，单账号、列表和映射 DTO 已落地 |
| 账号任务 | `/api/account-tasks...` | `/api/v1/account-tasks...` | React 设置和执行调用已迁移；版本化入口覆盖设置、运行记录和执行摘要；旧路径保留并由同一 handler 提供 |
| 管理员 | `/admin/users...`、`/admin/cookies...`、`/admin/stats` | `/api/v1/admin/...` | 旧路径保留，用户/账号/全局统计 DTO 已落地 |
| 统计 | `/dashboard/stats`、`/analytics/orders...` | `/api/v1/analytics/...` | 旧路径保留，概览、收益、维度和有效订单分页 DTO 已落地 |
| 二维码生成 | `/qr-login/generate` | `/api/v1/qr-login/generate` | 旧路径保留，二维码生成、状态和验证完成 DTO 已落地；`qrLoginStatusResponse` 仅保留非敏感动态字段，状态响应继续保留兼容扩展 |

## 迁移规则

1. 新增接口只能使用 `/api/v1` 前缀，禁止继续增加未版本化的业务路径。
2. 旧路径通过同一 handler 或薄适配器保留，不复制业务逻辑；适配器只负责路径和请求/响应兼容。
3. 前端请求层先切换到 `/api/v1`，并保留旧响应字段的边界归一；Go、React 和外部调用方契约测试全部通过后，才可以删除旧别名。
4. 删除旧路径前必须记录调用方、发布日期、迁移版本和回滚方案；发布说明要明确旧路径的移除版本。
5. 错误响应始终使用 `code`、`message`、`request_id`，恢复信息只能放入 `details`；版本适配器不得重新引入 `detail`、`msg` 或 HTTP 200 + `success:false`。

## 当前切片证据

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
- 会话登录、初始化、校验和登出已由 React 请求层迁移到 `/api/v1/session/...`，Go 契约测试确认版本化入口与旧路径共用 handler 且旧路径仍可用。
- 账号摘要、非敏感详情、运行状态、启停状态、设置、备注、暂停、自动确认、长登录、资料刷新、Cookie 更新和登录信息设置已由 React 请求层迁移到 `/api/v1/accounts...`；Go 契约测试确认账号详情与凭证变更响应不泄露 Cookie/密码，且旧详情、设置和登录信息入口仍可用。
- 订单列表、详情和更新已由 React 请求层迁移到 `/api/v1/orders...`；Go 契约测试确认版本化入口与旧更新入口共用 handler，订单归属校验和状态更新语义保持不变。
- 订单刷新、单订单刷新、手动发货和导入已由 React 请求层迁移到 `/api/v1/orders...`；Go 契约测试确认新旧批量入口保持状态码、具名响应和参数校验语义一致。
- 商品列表、详情、发布、更新和删除已由 React 请求层迁移到 `/api/v1/items...`；Go 契约测试确认新旧入口保持商品所有权校验、具名响应和持久化语义一致。
- 商品同步、类目推荐、批量发布预检、任务查询、取消、重试和结果下载已由版本化路由提供；React 现有同步/批量发布调用已迁移，Go 契约测试确认新旧入口状态码和参数校验语义一致。
- 系统/用户/AI 设置、卡券 CRUD/批量/追加和通知渠道/消息/账号绑定已由版本化路由提供；React 设置、卡券和通知调用已迁移，Go 契约测试确认新旧入口权限、状态码和参数校验语义一致。
- 聊天会话、消息、图片、已读和 WebSocket，以及账号任务设置、运行记录和执行已由版本化路由提供；React REST/WebSocket 调用已迁移，Go 契约测试确认新旧入口权限、状态码和参数校验语义一致。
- 聊天会话、消息分页和发送结果使用独立 DTO，不直接暴露数据库模型。
- React `frontend/types.ts` 与 `frontend/services/api.ts` 已同步具名成功响应类型，同时保留旧字段归一逻辑。
- 会话、账号、订单、商品、设置、卡券、通知、聊天和账号任务版本化入口已可用；关键词、默认回复、管理员与统计等领域仍未宣称可用，旧路径继续保留。
