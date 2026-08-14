# Ydisks 闲鱼助手渐进式重构总计划

## 1. 文档目的

本文是仓库后续结构治理的唯一长期路线图。任何涉及 Go 包边界、HTTP API、数据库访问、
账号凭证、React 页面结构、测试基础设施或依赖装配的修改，都必须先阅读本文、
`docs/architecture/dependency-rules.md` 与 `docs/architecture/comment-standard.md`。

本计划采用小步、可回滚的纵向切片，不允许建立一个长期脱离主分支的大型重构分支，
也不允许在一个变更中同时完成大规模移动、行为修改、数据库迁移和兼容清理。

滑块 CAPTCHA 逻辑继续受 `docs/slider-captcha-frozen-spec.md` 保护；本计划不构成修改授权。

## 2. 当前问题基线

截至计划建立时，仓库具有良好的测试和工程化基础，但存在以下主要维护风险：

- `internal/server` 同时承担 HTTP、业务编排、MTOP 调用、事务、QR 会话和批量发布 worker 生命周期；
- `internal/engine/account.go` 集中管理连接、凭证、重试、运行状态、去重、防抖和任务生命周期；
- `internal/automation/center.go` 同时管理规则、运行、外部动作、卡密分配、凭证恢复和通知；
- `internal/db.Store` 向上层暴露全部 repository 与裸 `*sql.DB`；
- 账号列表和所有权检查会读取或解密超过实际需要的敏感字段；
- HTTP 响应大量使用动态 map，错误结构和 API 路径不统一；
- 前端存在多个 900 至 1800 行的页面组件，API 与类型集中在单文件；
- 部分前端测试依赖源码字符串断言，难以保护真实交互；
- CI 主要依附发布 workflow，尚未形成独立 PR 质量门禁；
- server 测试重复执行全量 SQLite 迁移，使完整 race 测试耗时过高；
- 历史代码尚未满足全函数、全变量准确中文注释要求。

## 3. 不变量与成功标准

重构期间必须持续满足以下不变量：

1. 每个合并点都能编译、启动并通过适用测试。
2. 未经当前任务明确授权，不改变业务行为、API 兼容语义或冻结滑块行为。
3. 敏感凭证不得扩大读取、传递、序列化或日志记录范围。
4. SQLite、MySQL 和 PostgreSQL 的行为保持一致。
5. 前端源码变化后必须重建并校验 `internal/webui/static`。
6. 新增或修改的函数、变量、字段、参数和返回值必须符合中文注释规范。
7. 每次完成一个计划步骤，都必须更新本文的状态、验证结果和后续入口。
8. 不通过删除、跳过或放宽测试来完成重构。

最终成功标准：

- `internal/server` 只负责 HTTP、鉴权、中间件、DTO 映射和静态资源；
- 应用服务负责业务用例和事务边界，不依赖 `net/http`；
- handler 不直接访问 `Store.DB`、MTOP 或 browser；
- 账号摘要、平台凭证、密码登录秘密和运行配置使用不同模型与查询；
- HTTP API 使用具名 DTO、统一错误结构和统一版本前缀；
- React 按 feature 组织，页面容器、数据 Hook、表单和视图组件职责分离；
- `Account` 与 `Center` 成为 facade，内部状态由独立组件拥有；
- PR CI、目标 race、多数据库和架构规则均为自动门禁；
- Go 与 TypeScript/TSX 源码的中文注释基线清零。

## 4. 目标依赖方向

```text
cmd/server
  ├── application lifecycle
  └── HTTP server

HTTP server
  └── application services

application services
  ├── domain/runtime services
  └── consumer-defined ports

db / xianyu / browser / notify
  └── implement consumer-defined ports
```

前端目标方向：

```text
app shell / routes
  └── feature pages
        ├── feature hooks
        ├── feature components
        └── feature API adapters
              └── shared HTTP client
```

详细的允许与禁止依赖见 `docs/architecture/dependency-rules.md`。

## 5. 阶段状态总表

状态只允许使用：`未开始`、`进行中`、`已完成`、`阻塞`。

| 阶段 | 状态 | 目标 | 完成证据 |
| --- | --- | --- | --- |
| 0. 治理文档与强约束 | 已完成 | 总计划、依赖规则、注释规范、AGENTS 门禁、注释检查器 | 文档、门禁规则、Go/TypeScript 检查器和历史基线已落盘 |
| 1. PR CI 与测试基础 | 已完成 | 独立 CI、测试 DB 模板、可执行 race | CI、独立模板、server smoke race 和完整 race 均有验证 |
| 2. 敏感数据访问边界 | 已完成 | 摘要、凭证、登录秘密分离 | 生产完整详情白名单、所有权/凭证锁审计、SQLite/MySQL/Postgres 窄查询回归和全量门禁已完成 |
| 3. HTTP API 契约 | 已完成 | 统一错误、具名 DTO、版本化路径 | 统一错误 DTO、具名成功 DTO、所有前端业务调用方版本化、旧路径兼容和最终审计均已完成 |
| 4. Server 应用服务 | 已完成 | 订单、发布、登录、聊天纵向抽取 | 应用服务、事务边界、后台生命周期和 Server 查询依赖均已收口，handler 不再直接访问数据库 |
| 5. 应用生命周期装配 | 已完成 | 消除必需依赖 setter 回填 | 构造验证与幂等关闭测试 |
| 6. Engine 与 Automation | 已完成 | facade + 独立状态组件 | Engine/Automation 组件边界、race、生命周期与冻结规范测试均已通过 |
| 7. React Feature 化 | 进行中 | 页面、Hook、API、类型按领域拆分 | 行为测试、懒加载和 bundle 记录 |
| 8. DB 与事务治理 | 未开始 | 窄接口、事务执行器、方言门禁 | 上层无裸 DB，多数据库回归 |
| 9. 架构门禁与兼容清理 | 未开始 | 自动依赖规则、删除到期兼容层 | 架构检查与迁移说明 |
| 10. 注释基线清零 | 未开始 | 全仓严格中文注释检查 | baseline 文件删除 |

### 当前执行入口

- 当前阶段：阶段 7“React Feature 化”（进行中）；阶段 6 Engine 与 Automation、阶段 5 应用生命周期装配、阶段 4 Server 应用服务与阶段 3 HTTP API 契约已完成最终审计；
- 已完成：总计划、依赖规则、中文注释规范、`AGENTS.md` 强约束，以及 Go/TypeScript AST 注释检查器和历史基线；
- 阶段 1 已完成：server 测试模板预置管理员和账号 cookie，普通测试约 21.3 秒，完整 server race 约 194.3 秒通过；
- 已完成阶段 2 逻辑切片一“Repository 敏感数据边界”：建立 `CookieSummary`、`ListOwnedIDs`、`ExistsOwned`、`GetOwnerID`、`GetSummaryOwned` 和原子 `GetValueOwned`，覆盖跨用户、无效 user ID 及无效密文回归；
- 已完成阶段 2 逻辑切片二“Server 非敏感消费方”：账号列表、运行状态、账号详情、聊天、商品、关键词回复、卡券关联和管理员账号停止流程均迁移到窄查询，纯所有权流程不再解密完整凭证；
- 已完成阶段 2 逻辑切片三“订单消费方”：订单刷新、手动发货和订单导入使用账号 ID 列表与所有权判断，凭证流程按需读取单账号详情；
- 已完成阶段 2 后续切片：`internal/chat/service.go` 的订阅账号集合已迁移到 `ListOwnedIDs`，聊天订阅不再批量解密账号 Cookie；
- 已完成阶段 2 后续切片：`internal/account/manager.go` 已改用受控的 `ListEnabledRuntimeCredentials` 启动账号，只解密启用账号 Cookie，不再使用 `AllForUser(ctx, 0)` 加逐账号状态查询；
- 已完成阶段 2 后续切片：`internal/renewal/scheduler.go` 已改用 `RenewalRuntimeAccount` 窄模型及按账号重读接口，只解密 Cookie、续期 metadata 和启用状态，不再把登录密码/用户名带入续期调度器；
- 已完成阶段 2 后续切片：`internal/automation/account_tasks.go` 的 Session 阻断指纹已改用 `GetCookieRuntimeData`，只读取 Cookie 与 metadata，不再解密完整账号详情；
- 已完成阶段 2 后续切片：`internal/automation/center.go` 的确认发货流程已改用 `GetCookieRuntimeData`，只读取 Cookie 与 metadata，保留凭证锁、Cookie Jar 和重试行为；
- 已完成阶段 2 后续切片：`internal/automation/center.go` 的通用 `cookieValue` 回退读取已迁移到 `GetValue`，Automation 生产代码不再直接读取完整账号详情；
- 已完成阶段 2 后续切片：`internal/engine/account.go` 的 `tryLoginStatusCheck` 已改用 `GetCookieRuntimeData`，只读取 Cookie 与 metadata，保持重试、Cookie Jar 和锁语义；
- 已完成阶段 2 后续切片：`internal/engine/account.go` 的 `tryAPIRenewUsing` 已改用 `GetCookieRuntimeData`，只读取接口续期所需的 Cookie 与 metadata，保持续期、快照持久化、锁和 token 清理语义，并补充损坏登录密码回归测试；
- 已完成阶段 2 后续切片：新增 `GetCookieMetadata` 单字段接口，并将 `internal/engine/account.go` 的 `persistRenewFlatCookie` 迁移到该接口，只读取 metadata，保持扁平 Cookie 更新和 metadata 快照清理行为；
- 已完成阶段 2 后续切片：`internal/engine/account.go` 的 `handleMaxFailures` 已改用 `GetValue`，只读取恢复回调所需的 Cookie 明文，保持失败计数和重连行为，并补充损坏登录密码回归测试；
- 已完成阶段 2 后续切片：`internal/engine/account.go` 的 `persistPendingRenewCookies` 已改用 `GetCookieRuntimeData`，只读取异步续期所需的 Cookie 与 metadata，保持迟到 Cookie 合并、凭证锁和通知行为，并补充损坏登录密码回归测试；
- 已完成阶段 2 后续切片：`internal/engine/account.go` 的 `refreshTokenWithMinGap` metadata 读取已迁移到 `GetCookieMetadata`，只读取 token 请求所需的 Cookie 快照信息，保持 Cookie 快照请求上下文和 token 刷新行为，并补充损坏登录密码回归测试；
- 已完成阶段 2 后续切片：`internal/engine/account.go` 的 `adoptTokenResponseCookies` metadata 读取已迁移到 `GetCookieMetadata`，只读取 token 响应 Cookie 合并所需的快照信息，保持响应合并、快照持久化和错误语义，并补充损坏登录密码回归测试；
- 已完成阶段 2 后续切片：`internal/engine/account.go` 的 `databaseCredentialFingerprint` 已改用 `GetCookieRuntimeData`，只读取 token 凭证一致性校验所需的 Cookie 与 metadata，保持空值、指纹不一致和错误语义，并补充损坏登录密码回归测试；
- 已完成阶段 2 后续切片：`internal/engine/account.go` 的 `reloadCookieFromDB` 已改用 `GetCookieRuntimeData`，只读取外部 Cookie 更新检测所需的 Cookie 与 metadata，保持运行时替换、token 清理和错误行为，并补充损坏登录密码回归测试；
- 已完成阶段 2 当前 PR 切片四“Engine/账号运行时凭证边界”：`cookieSnapshotMatchesDB`、`UpdateCookie` 和账号管理器 `Restart` 已分别改用 `GetCookieRuntimeData` 或 `GetValue`，只读取运行实例所需的 Cookie 数据；损坏登录密码回归测试覆盖 WS 注册前校验、运行时同步和重启路径，且三项修改合并为一个可回滚提交；
- 已完成阶段 2 当前 PR 切片五“平台凭证流程统一窄查询”：新增不含用户名和登录密码的 `CookiePlatformRuntimeData`，并将 `internal/adapter` 的 token 风控、订单详情、协议续期以及 `internal/renewal` 的迟到 Cookie 合并统一迁移到该视图；损坏登录密码回归测试覆盖四条平台流程，且整批修改合并为一个可回滚提交；
- 已完成阶段 2 当前 PR 切片六“Server 平台凭证流程审计”：Server 的订单、发布、商品同步、二维码登录、长登录、资料刷新和账号生命周期路径已改用平台运行视图或非敏感摘要；完整 `CookieDetail` 仅保留给账号设置和登录信息更新这两个确实需要登录秘密的流程，新增 Server 窄查询回归测试并通过全量门禁，整批修改合并为一个可回滚提交；
- 已完成阶段 2 最终审计：生产代码中的 `GetDetails` 仅保留登录设置和登录信息更新两条完整详情白名单；平台流程统一使用 `GetCookiePlatformRuntimeData`，所有权流程统一使用摘要查询，凭证读取继续由 `LockAccountCredentials` 保护；新增 `TestMultiDB_CookieCredentialScope` 覆盖 SQLite，并在提供环境变量时覆盖 MySQL/Postgres，验证三种方言的 Cookie、metadata、平台视图和所有权摘要一致；阶段 2 全量门禁通过并合并为一个可回滚提交；
- 已完成阶段 3 第一个 PR 切片“HTTP 错误结构盘点与第一批契约测试”：新增共享 `httpapi.ErrorResponse`，统一 `code`/`message`/`request_id` 错误边界；认证失败改用 401 和稳定 `authentication_failed`，健康检查和账号列表改用具名 DTO；React 请求层移除 `detail/msg` 错误依赖并为账号列表补充具名类型；新增健康检查、认证失败和账号列表契约测试，Go/React 全量门禁通过并合并为一个可回滚提交；
- 已完成阶段 3 第二个 PR 切片“剩余认证与公共 API 错误迁移”：初始化冲突、管理员/用户密码修改、登录凭据校验、用户名冲突、公开设置故障和 SPA API 404 均返回统一错误 DTO 与正确状态码；新增状态码/错误码契约测试，Go 全量门禁通过并合并为一个可回滚提交；
- 已完成阶段 3 第三个 PR 切片“订单与账号业务 API 错误响应迁移”：订单刷新、手动发货和订单导入不再返回顶层 HTTP 200 + `success:false`，批量结果改用 `partial_failure`，刷新明细错误统一使用 `message`；账号任务的 502 错误改用统一错误 DTO；新增订单/账号契约测试并通过全量门禁，逐项结果中的 `success` 仅保留为批处理行状态；
- 已完成阶段 3 第四个 PR 切片“聊天、商品与自动化业务 API 错误响应迁移”：聊天发送失败和商品发布失败均改用统一 `code`/`message`/`request_id` 错误 DTO，商品远端已成功但本地保存失败的恢复信息迁移到可选 `details`；React 错误类型同步支持 `details`，新增 HTTP DTO、聊天/自动化/商品契约测试并通过前端门禁；
- 已完成阶段 3 第五个 PR 切片“二维码登录、密码登录和剩余公共业务错误响应迁移”：二维码生成、扫码状态持久化、风控验证失败和账号不匹配均改用正确的非 2xx 状态码与统一错误 DTO；历史密码登录入口返回明确的 `password_login_disabled` 未实现错误；新增二维码/密码登录契约测试并通过全量门禁；
- 已完成阶段 3 第六个 PR 切片“统一业务成功响应具名 DTO 与版本化路径准备”：认证会话、账号新增、订单列表、聊天会话/消息主链路已改用具名成功响应 DTO，新增响应契约测试，并记录 `/api/v1` 兼容迁移边界；未提前切换旧路径，保持可回滚；
- 已完成阶段 3 第七个 PR 切片“剩余业务成功响应 DTO 与客户端契约收口”：账号详情/设置、商品核心发布与同步、自动化规则/异常、订单详情/刷新/批量外层响应改用具名 DTO；React 客户端同步使用具名契约类型；新增跨领域成功响应契约测试；旧字段、旧路径和批处理逐行兼容字段保留；全量门禁通过并合并为一个可回滚提交；
- 已完成阶段 3 第八个 PR 切片“剩余商品批量操作与设置/卡券/通知成功响应收口”：商品类目推荐、批量发布预检/任务、系统与用户设置、账号 AI 设置、卡券 CRUD/批量、通知渠道与账号绑定均改用具名 DTO；React 客户端同步具名契约类型；补充跨领域契约测试；保留动态设置键、旧路径和批量逐行字段；全量门禁通过并合并为一个可回滚提交；
- 已完成阶段 3 第九个 PR 切片“剩余关键词回复、默认回复与账号任务成功响应收口”：关键词基础/商品/类型规则、指定商品回复、默认回复、账号任务设置与运行记录均改用具名 DTO；React 客户端同步具名契约类型；新增跨领域契约测试；保留旧路径和兼容字段；全量门禁通过并合并为一个可回滚提交；
- 已完成阶段 3 第十个 PR 切片“分析统计、管理员与剩余公共成功响应收口”：管理员用户/账号/统计、用户仪表盘、订单分析、有效订单分页和二维码生成均改用具名 DTO；React 客户端同步统计和二维码契约类型；新增跨领域契约测试；保留动态设置和二维码状态的兼容边界；全量门禁通过并合并为一个可回滚提交；
- 已完成阶段 3 第十二个 PR 切片“HTTP API 版本化兼容入口与会话调用方迁移”：新增 `/api/v1/session/login`、`/api/v1/session/initialize`、`/api/v1/session` 与 `/api/v1/session/logout` 薄适配入口，复用旧 handler；React 登录、初始化、会话校验和登出已迁移，旧路径保留；新增 Go/React 契约测试并保持独立可回滚提交；
- 已完成阶段 3 第十三个 PR 切片“账号 API 版本化兼容入口与调用方迁移”：新增 `/api/v1/accounts`、详情、运行状态、单账号详情和启停状态薄适配入口，复用旧 handler；React 账号摘要、详情、运行状态和启停状态调用已迁移，旧路径保留；新增 Go/React 契约测试并保持独立可回滚提交；
- 已完成阶段 3 第十四个 PR 切片“账号设置与资料 API 版本化兼容入口迁移”：新增账号聚合设置、备注、暂停、自动确认、长登录和资料刷新 `/api/v1/accounts/{cid}/...` 薄适配入口，复用旧 handler；React 相关调用已迁移，旧路径保留；新增 Go/React 契约测试并保持独立可回滚提交；
- 已完成阶段 3 第十五个 PR 切片“账号凭证与登录信息 API 版本化兼容入口迁移”：新增账号新增、Cookie 更新和登录信息设置 `/api/v1/accounts...` 薄适配入口，复用既有凭证锁与权限 handler；React 对应调用已迁移，旧路径保留；新增敏感字段边界契约测试并保持独立可回滚提交；
- 已完成阶段 3 第十六个 PR 切片“订单 API 版本化兼容入口与调用方迁移”：新增订单列表、详情和更新 `/api/v1/orders...` 薄适配入口，复用既有订单归属校验与 handler；React 对应调用已迁移，旧路径保留；新增订单契约测试并保持独立可回滚提交；
- 已完成阶段 3 第十七个 PR 切片“订单刷新与批量操作 API 版本化兼容入口迁移”：新增订单刷新、单订单刷新、手动发货和导入 `/api/v1/orders...` 薄适配入口，复用既有订单 handler；React 对应调用已迁移，旧路径保留；新增刷新/批量契约测试并保持独立可回滚提交；
- 已完成阶段 3 第十八个 PR 切片“商品 API 版本化兼容入口与调用方迁移”：新增商品列表、详情、发布、更新和删除 `/api/v1/items...` 薄适配入口，复用既有商品所有权校验与 handler；React 对应调用已迁移，旧路径保留；新增商品契约测试并保持独立可回滚提交；
- 已完成阶段 3 第十九个 PR 切片“商品同步与批量发布 API 版本化兼容入口迁移”：新增商品同步、类目推荐、批量发布预检、任务、详情、取消、重试和结果下载 `/api/v1/items...` 薄适配入口，复用既有商品 handler；React 对应同步/批量发布调用已迁移，旧路径保留；新增商品批量契约测试并保持独立可回滚提交；
- 已完成阶段 3 第二十个 PR 切片“设置、卡券与通知 API 版本化兼容入口迁移”：新增系统/用户/AI 设置、卡券和通知渠道/消息/账号绑定 `/api/v1/settings`、`/api/v1/cards`、`/api/v1/notifications` 薄适配入口，复用既有权限边界与 handler；React 系统设置、AI 设置、卡券和通知调用已迁移，旧路径保留；新增三领域契约测试并保持独立可回滚提交；
- 已完成阶段 3 第二十一个 PR 切片“聊天与账号任务 API 版本化兼容入口迁移”：新增聊天 REST/WebSocket 和账号任务设置/运行记录/执行 `/api/v1/chat...`、`/api/v1/account-tasks...` 薄适配入口，复用既有权限边界与 handler；React REST/WebSocket、账号任务设置和执行调用已迁移，旧路径保留；新增聊天/账号任务契约测试并保持独立可回滚提交；
- 已完成阶段 3 第二十二个 PR 切片“关键词回复与默认回复 API 版本化兼容入口迁移”：新增关键词基础/商品/类型规则、指定商品回复和默认回复 `/api/v1/reply-rules...`、`/api/v1/default-replies...` 薄适配入口，复用既有权限校验与 handler；React 关键词类型规则、指定商品回复和默认回复调用已迁移，旧路径保留；新增回复规则契约测试并保持独立可回滚提交；
- 已完成阶段 3 第二十三个 PR 切片“管理员、仪表盘与订单分析 API 版本化兼容入口迁移”：新增管理员用户/账号/统计、仪表盘统计、订单分析和有效订单 `/api/v1/admin...`、`/api/v1/analytics...` 薄适配入口，复用既有管理员权限与统计 handler；React 管理员统计、仪表盘和订单分析调用已迁移，旧路径保留；新增管理员/统计契约测试并保持独立可回滚提交；
- 已完成阶段 3 第二十四个 PR 切片“二维码登录 API 版本化兼容入口迁移”：新增二维码生成、状态查询、状态持久化和验证完成 `/api/v1/qr-login...` 薄适配入口，复用既有认证、会话所有权和敏感字段过滤 handler；React 二维码生成、轮询和验证完成调用已迁移，旧路径保留；新增二维码新旧入口契约测试并保持独立可回滚提交；
- 已完成阶段 3 第二十五个 PR 切片“密码登录及剩余公共调用方 API 版本化兼容入口迁移”：新增会话密码/凭证、账号删除、密码登录禁用、自动化规则/异常处理、订单删除和商品创建 `/api/v1/...` 薄适配入口，复用既有认证、权限和业务 handler；React 对应调用已迁移，旧路径保留；新增剩余公共调用方 Go/React 契约测试并保持独立可回滚提交；
- 已完成阶段 3 第二十六个 PR 切片“版本化入口最终审计与旧调用方清零”：前端生产调用和批量结果下载已无业务旧路径；Vite 代理收敛为 `/api` 与健康检查；服务端剩余商品兼容入口已补齐；新增旧代理别名和商品遗漏入口审计证据，阶段 3 完成条件满足并保持独立可回滚提交；
- 已完成阶段 4 第一个 PR 切片“订单应用服务边界提取”：订单列表、详情、更新、删除、批量导入、手动发货和批量刷新已由不依赖 `net/http` 的 `orderApplicationService` 编排；handler 仅保留 HTTP 解析、鉴权上下文和 DTO 编码；保留订单所有权、事务回滚、MTOP 凭证锁、Cookie Jar 持久化、Session 续期和逐单部分失败语义；新增服务层直接调用测试并通过 Go 全量门禁与 server race；
- 已完成阶段 4 第二个 PR 切片“订单响应 DTO 映射收口”：订单列表状态/图片映射、详情商品补全、单订单刷新和批量刷新响应均由应用服务返回稳定视图；更新校验改用业务错误分类，HTTP 层不再依赖错误文本判断状态码；旧/版本化入口契约测试和全量门禁通过；
- 已完成阶段 4 第三个 PR 切片“商品发布应用服务边界提取”：新增不依赖 `net/http` 的商品发布应用服务，覆盖单商品发布、类目推荐、批量预检持久化、批次启动/查询/取消/删除/失败重试；HTTP 层仅保留请求解析、鉴权和契约映射，保留 worker、锁、cancel map、租约和远端发布顺序；新增服务层直接调用测试并通过全量门禁，合并为一个可回滚提交；
- 已完成阶段 4 第四个 PR 切片“账号登录应用服务边界提取”：新增不依赖 `net/http` 的账号登录应用服务，覆盖扫码结果幂等持久化、Cookie Jar/扁平 Cookie 合并、账号新增与更新、资料刷新、登录审计和运行时重启；HTTP handler 仅保留请求解析、所有权校验和契约映射，保留扫码会话锁与账号凭证锁不变量；新增服务层测试并通过全量门禁，合并为一个可回滚提交；
- 已完成阶段 4 第五个 PR 切片“聊天与通知应用服务边界提取”：新增不依赖 `net/http` 的通信应用服务，覆盖账号任务设置/执行/记录、聊天文字与图片发送、聊天历史读取/已读、通知渠道 CRUD、账号绑定和删除；保留 WebSocket 订阅、聊天事件广播、通知 outbox 与失败状态语义；新增服务层测试并通过全量门禁，合并为一个可回滚提交；
- 已完成阶段 4 第六个 PR 切片“Server 事务与生命周期边界收口”：新增 Server 统一 Unit of Work，订单更新与导入不再直接管理事务；新增后台任务生命周期登记入口，HTTP 优雅关闭与批量发布恢复扫描器统一纳入等待流程；修复服务不可用时 handler 先做资源归属校验导致状态码变化的问题；新增事务提交/回滚测试并通过全量门禁，合并为一个可回滚提交；
- 已完成阶段 4 第七个 PR 切片“Server 直接依赖清理与应用服务装配收口”：统一装配订单、发布、登录、通信和分析应用服务；默认回复、账号设置、AI 设置、通知绑定、管理员查询及订单分析查询均迁移到 repository 或应用服务边界；handler 不再直接访问 `Store.DB`，新增装配一致性测试并通过全量门禁，合并为一个可回滚提交；
- 已完成阶段 5 PR 切片“应用构造依赖验证与生命周期接口”：`Server.New` 在构造阶段校验 `Store/Manager`，聊天服务通过 option 注入并移除生产运行时 setter；新增幂等 `Start/Wait/Stop`，`Stop` 统一等待 HTTP、后台扫描器和批量 worker；`cmd/server` 迁移到显式生命周期入口；新增构造失败、重复启动/停止和 worker 等待测试，并通过全量门禁，合并为一个可回滚提交；
- 已完成阶段 6 第一个 PR 切片“Engine 账号 facade 与运行状态边界”：将连接状态、失败计数、离线告警和业务任务生命周期分别提取为独立锁组件；`Account` 保留 facade，`Stop` 对并发调用保持幂等并等待已登记任务；新增生命周期并发回归测试，未改变 WebSocket、凭证、自动化和冻结风控逻辑；全量测试、Engine race、Server race、vet、lint、注释和 diff 门禁通过并合并为一个可回滚提交；
- 已完成阶段 6 第二个 PR 切片“Engine WebSocket 连接循环边界”：`registerConnection` 在凭证锁内统一快照复核与 WebSocket 注册，`runConnectionSession` 统一心跳、接收、Token 轮换 goroutine 的创建/取消/等待；`Account` 继续负责凭证错误、风控和重连结果解释；新增会话收束测试，未改变冻结风控行为；全量测试、Engine race、Server race、vet、lint、注释和 diff 门禁通过并合并为一个可回滚提交；
- 已完成阶段 6 第三个 PR 切片“Engine WebSocket 记录与消息分发边界”：将 WS 记录 worker、消息去重、防抖和并发信号量分别收口到独立组件；`Account` 保留兼容 facade，系统事件背压、聊天洪峰限流、消息顺序、自动化事件优先级和 Stop 等待语义保持不变；全量 Go 测试、Engine/Server race、vet、lint、注释和前端构建门禁通过并合并为一个可回滚提交；
- 已完成阶段 6 第四个 PR 切片“Engine 凭证状态与 Token 生命周期边界”：将 Cookie 快照、Token 缓存、刷新锁、设备指纹和刷新诊断状态收口到独立 `credentialState`；运行状态锁改为显式 `runtimeMu`，避免匿名组件锁冲突；保留风控恢复、刷新锁、Cookie/Token 绑定和 WebSocket 注册前快照复核语义；Engine 全量测试、Engine/Server race、vet、lint、注释和前端构建门禁通过并合并为一个可回滚提交；
- 已完成阶段 6 第五个 PR 切片“Automation 事件事实与规则匹配边界”：新增事件事实记录器、无动作副作用的规则匹配器和纯动作计划器；`Center` 与 `Scheduler` 统一通过组件查询规则和生成动作计划，规则匹配不执行外部动作，付款发货动作顺序、规格匹配、延迟快照和恢复语义保持不变；Automation/Server race、全量测试、vet、lint、注释和前端构建门禁通过并合并为一个可回滚提交；
- 已完成阶段 6 第六个 PR 切片“Automation 运行协调与动作执行边界”：新增 `automationRunCoordinator`，统一运行创建/恢复、动作前后检查点、延迟任务续租、账号门禁和不确定结果隔离；`Center` 保留兼容调用入口，外部动作已执行/未执行/结果未知三态语义、人工核对状态机和恢复游标保持不变；Automation 测试、注释和 diff 门禁通过，整批修改合并为一个可回滚提交；
- 已完成阶段 6 第七个 PR 切片“Automation 发货、卡密与通知动作边界”：新增 `automationActionExecutor` 与 `deliveryNotifier`，统一确认发货、Cookie/Jar 合并、卡券锁与库存消费、消息错误三态分类和结果通知；`Center` 保留兼容入口，凭证锁、卡券库存、恢复唤醒和通知文案语义保持不变；新增动作执行器回归测试，Automation 测试、注释和 diff 门禁通过，整批修改合并为一个可回滚提交；
- 已完成阶段 6 第八个 PR 切片“Automation 账号任务与凭证门禁边界”：新增 `accountTaskCoordinator`，统一账号状态门禁、自动评价/商品擦亮调度、任务租约、Session 指纹阻断、凭证恢复和 Cookie 同步；`Center` 保留公开任务入口，Session 失效恢复、任务幂等、失败重试和账号暂停语义保持不变；Automation 测试、注释和 diff 门禁通过，整批修改合并为一个可回滚提交；阶段 6 完成；
- 已完成阶段 7 第一个 PR 切片“React Rules feature 化与 API/行为测试边界”：按 `app/features/rules` 提取 Rules API 适配层、领域类型、数据 Hook、异常面板和交互状态模型；规则页通过 Hook 管理服务端数据与请求代次，保留旧组件入口和现有 API 路径；行为测试覆盖成功、失败、重复提交、过期响应和页签切换；前端类型检查、全量测试、注释检查和构建门禁通过，合并为一个可回滚提交；
- 已完成阶段 7 第二个 PR 切片“React ItemList feature 化与批量发布行为边界”：按 `app/features/items` 提取商品/批量 API 适配层、批量任务类型、批量 Hook 和阶段指示器组件；批量预检、任务恢复、轮询取消、失败重试和过期响应均由 feature 状态边界负责；行为测试覆盖预检门禁、取消状态、失败重试、历史任务筛选和过期轮询响应；前端类型检查、全量测试、注释检查和构建门禁通过，合并为一个可回滚提交；
- 已完成阶段 7 第三个 PR 切片“React AccountList feature 化与账号运行状态行为边界”：按 `app/features/accounts` 提取账号 API 适配层、账号数据与运行状态 Hook、运行状态展示模型、账号编辑弹窗和登录/暂停状态模型；账号加载与运行状态轮询支持取消和请求代次门禁，密码登录响应按账号与代次隔离，旧组件入口保留兼容转发；行为测试覆盖账号切换、暂停/恢复、凭证失败、风控验证和过期响应；前端类型检查、全量测试、注释检查和构建门禁通过，合并为一个可回滚提交；
- 已完成阶段 7 第四个 PR 切片“React CardList feature 化与卡密库存/批量追加行为边界”：按 `app/features/cards` 提取卡密 API 适配层、库存数据 Hook、批量导入/追加状态 Hook 和弹窗组件；卡密筛选、追加预览、提交取消、失败重试与账号切换过期响应均由 feature 状态边界负责，保留旧组件入口和 API 路径；新增状态模型测试并通过前端类型检查、全量测试、注释检查和构建门禁，合并为一个可回滚提交；
- 已完成阶段 7 第五个 PR 切片“React OrderList feature 化与订单导入/刷新行为边界”：按 `app/features/orders` 提取订单 API 适配层、查询/分页 Hook、导入 Hook、筛选栏和导入弹窗；订单查询与辅助数据支持并行加载、取消和代次门禁，导入支持文件预检、取消、失败重试和导入后刷新；新增筛选、导入归一化、API 取消信号和过期响应测试，保留旧组件入口和 API 路径；前端类型检查、全量测试、注释检查和构建门禁通过，合并为一个可回滚提交；
- 已完成阶段 7 第六个 PR 切片“React Notifications feature 化与渠道/事件绑定行为边界”：按 `app/features/notifications` 提取通知渠道/SMTP API 适配层、静态渠道配置、数据与动作 Hook、渠道列表、事件绑定选择器、渠道编辑弹窗和 SMTP 面板；渠道与 SMTP 请求支持取消和代次门禁，表单支持渠道字段/独立 SMTP 校验、保存失败重试和过期响应保护；更新架构字符串测试并新增状态/API 取消信号测试，保留旧页面入口和 API 路径；前端类型检查、全量测试、注释检查和构建门禁通过，合并为一个可回滚提交；
- 已完成阶段 7 第七个 PR 切片“React Dashboard feature 化与统计/趋势查询行为边界”：按 `app/features/dashboard` 提取 Dashboard API 适配层、统计数据 Hook、趋势/排行派生状态和请求边界；概览、趋势和有效订单支持并行加载、刷新取消、失败重试与过期响应保护；新增统计派生数据、API 取消信号和请求代次测试，保留旧页面入口和 API 路径；前端类型检查、全量测试、注释检查和构建门禁通过并合并为一个可回滚提交；
- 已完成阶段 7 第八个 PR 切片“React Settings feature 化与系统配置校验边界”：按 `app/features/settings` 提取系统配置/模型/凭据 API 适配层、配置常量、表单状态和敏感字段校验；配置读取与保存支持并行加载、请求取消、失败重试和过期响应保护，登录凭据提交保留现有校验与重登录语义；新增配置裁剪、凭据校验、API 取消信号和请求代次测试，保留旧页面入口和 API 路径；
- 阶段 7 下一 PR 切片为“React Chat feature 化与会话消息行为边界”：提取聊天 API、会话/消息分页 Hook 和消息发送状态，补充会话切换取消、分页过期响应和发送失败重试测试；
- 禁止跳过当前入口直接开始 Engine、Automation 或 DB 的大规模拆分。

## 6. 阶段 0：治理文档与强约束

### 工作项

- 创建本总计划；
- 创建依赖边界规则；
- 创建全函数、全变量中文注释规范；
- 在 `AGENTS.md` 中加入必须阅读、必须更新和禁止绕过的规则；
- 增加 Go AST 与 TypeScript AST 注释检查器和历史基线。

### 完成条件

- 所有长期约束均能从 `AGENTS.md` 链接到权威文档；
- 后续任务可以明确指出所处阶段、工作项和适用验证；
- 文档不与冻结滑块规范、打包规则或数据库规则冲突；
- `make comments` 与 `npm --prefix frontend run comments:check` 能阻止不在历史基线中的新增声明缺少中文注释。

### 6.1 注释检查器落地说明

- `tools/commentlint` 使用 Go AST 检查函数、类型、常量、变量、结构体/接口字段、短变量和范围变量；
- `frontend/scripts/check-comments.mjs` 使用 TypeScript Compiler API 检查变量、函数、方法、属性和函数表达式；
- `.commentlint/go-baseline.json` 与 `.commentlint/frontend-baseline.json` 只记录当前历史债务，新增或修改声明不得通过追加基线绕过；
- 基线使用文件、行号、类别和名称作为审查键。移动或重构文件时，必须清理对应文件的历史问题并重新生成基线；
- 检查器只验证“邻近注释 + 至少一个汉字”，注释准确性、参数语义、并发和敏感数据说明仍由人工审查负责；
- 注释债务按阶段逐文件清理，阶段 10 完成时删除两个基线文件并启用全仓零问题门禁。

## 7. 阶段 1：PR CI 与测试基础

### 1.1 独立 PR CI

新增 `.github/workflows/ci.yml`，在 pull request 以及 `main`、`dev` push 时执行：

- Go 格式只读检查；
- `go vet ./...`；
- `golangci-lint run ./...`；
- `go test ./...`；
- 中文注释检查；
- `npm ci`、typecheck、test、build；
- 嵌入式前端产物一致性检查。

发布 workflow 保持发布职责，不再作为唯一质量门禁。

### 1.2 Server 测试数据库模板

- 在测试入口迁移一次 SQLite 模板；
- 每个测试复制模板到独立临时目录；
- 测试之间不得共享可写数据库连接；
- 关闭无必要的 Goose 测试日志；
- 记录优化前后的普通测试和 race 时间。

当前实现：`internal/server/test_database_test.go` 在进程内只执行一次 Goose 迁移，并预置普通测试共同需要的
管理员和账号 cookie，随后按测试复制 SQLite 文件并直接打开副本。普通 `internal/server` 测试从约 48.2 秒
降至约 21.3 秒；完整 server race 从 267 秒未完成改善为约 194.3 秒通过。

### 1.3 Race 分层

普通 PR 先运行并发敏感包的目标 race；server fixture 优化后加入稳定的 server race 子集；
全仓 race 可以进入 nightly。任何 race 报告都必须修复，不得加入忽略名单。

当前 PR 门禁使用 `make test-server-race`，覆盖 server 启停、发布 worker、凭证状态转换和锁内所有权复核
等已验证的并发场景，实测约 12.4 秒通过；完整 `go test -race ./internal/server` 已在预置测试夹具后约 194.3 秒通过，
可作为 nightly 或手工发布前验证，不用 smoke race 代替完整覆盖。

### 完成条件

- PR 在合并前获得稳定、独立的质量结果；
- `internal/server` race 不再因重复迁移和重复密码哈希触发长时间超时；
- CI 不修改工作区后假装通过格式检查。

## 8. 阶段 2：敏感数据访问边界

### 2.1 模型拆分

建立互不混用的账号摘要、平台凭证、密码登录秘密和运行设置模型。

### 2.2 Repository 查询

增加用途明确的查询：

- `ListSummaries`；
- `ListOwnedIDs`；
- `ExistsOwned`；
- `GetCredential`；
- `GetLoginSecret`；
- `GetRuntimeSettings`。

当前实现先落地了 Cookie 领域的窄查询：`CookieSummary` 不包含 `Value`、`Password` 或 `MetadataJSON`；
`ListOwnedIDs` 只返回账号 ID；`ExistsOwned` 只返回布尔存在性，并拒绝 `userID=0` 的隐式管理员查询。
`GetOwnerID` 只返回所有者 ID；`GetSummaryOwned` 返回指定用户的单个非敏感摘要；`GetValueOwned` 在同一条带 user_id 过滤的查询中读取并解密单个 Cookie，避免
所有权检查与凭证读取之间的竞态窗口。测试使用故意无效的密文值验证摘要查询和所有权检查不会触发解密，
并使用正常加密值覆盖单值凭证读取。账号列表与详情 handler 已不再通过 `AllForUser` 或完整 `GetDetails` 读取敏感字段，
目前 server 生产代码和聊天订阅服务已不再调用 `Cookies.AllForUser`。账号管理器还保留一处管理员视角的全账号凭证加载，
该调用不能简单替换为 ID 列表，将通过受控的启用账号凭证接口单独治理。

逐步替换使用 `AllForUser` 进行所有权检查以及使用 `GetDetails` 获取非敏感字段的调用。最终审计已确认生产代码的完整详情读取白名单仅为登录设置和登录信息更新；平台调用、账号生命周期、订单、发布、二维码登录和资料流程均不再需要解密登录秘密。

### 2.3 安全不变量

- 列表和所有权检查不解密 Cookie、密码或 metadata；
- 只有平台调用流程读取平台凭证；
- 只有密码登录或续期流程读取登录秘密；
- 敏感模型不得用作 HTTP DTO；
- 敏感值不得写入普通日志、错误体或测试失败信息。

### 完成条件

- 账号列表没有 N+1 敏感查询；
- 用户 ID `0` 不再表示隐式管理员查询；
- 三种数据库的查询和并发测试通过；
- 完整详情读取白名单、所有权过滤和凭证锁不变量均有可复核证据。

## 9. 阶段 3：HTTP API 契约

### 3.1 统一错误结构

所有失败响应逐步统一为 `code`、`message` 和可选 `request_id`，并使用正确 HTTP 状态码。
禁止新增 HTTP 200 + `success:false`，禁止新增 `detail`、`msg`、`error` 等新的错误别名。

### 3.2 具名 DTO

- handler 不新增匿名请求结构或动态 map 响应；
- DB model 不直接作为 HTTP 响应；
- API DTO 与领域模型显式转换；
- 前端不新增 `Promise<any>` 或无边界 `Record<string, any>`。

### 3.3 版本化路径

新接口使用 `/api/v1`。旧接口以兼容别名保留，必须记录调用方、迁移步骤和删除条件。
只有在前端和外部调用方全部迁移、契约测试覆盖后才能删除兼容路由。

### 3.4 类型生成

账号和订单 DTO 稳定后再引入 OpenAPI。生成文件不得手工修改；历史兼容归一只存在于边界 adapter。

## 10. 阶段 4：Server 应用服务

按订单、发布、账号登录、聊天/通知顺序纵向提取应用服务。每次只处理一个业务切片。

### 4.1 订单服务

应用服务负责列表、详情、更新、导入、同步、手工发货、所有权和事务；handler 只负责 DTO。

### 4.2 发布服务

应用服务负责单商品发布、预检、批量任务、恢复、取消、重试和关联自动化规则。
完成后发布 worker、锁和 cancel map 不再属于 HTTP Server。

### 4.3 账号登录服务

应用服务负责 QR 会话所有权、结果持久化、资料刷新、Cookie 合并、账号重启和登录审计。
完成后 QR 状态和持久化锁不再属于 HTTP Server。

### 4.4 聊天与通知

继续收紧已有服务的接口，使 handler 不再直接查询 Store 或调用平台客户端。

### 4.5 事务

跨 repository 事务由应用服务通过明确的 Unit of Work 执行。禁止 handler 调用 `BeginTx`。

### 完成条件

- `internal/server` 不直接访问 `Store.DB`；
- handler 不直接调用 MTOP 或 browser；
- 应用服务不依赖 `net/http`；
- 业务分支由应用服务单元测试保护，HTTP 测试集中验证契约与鉴权。

## 11. 阶段 5：应用生命周期装配

应用服务边界稳定后建立应用装配与生命周期层：

- 构造所有服务并验证必需依赖；
- 按明确顺序启动，按逆序关闭；
- `Start` 前不得隐式启动 goroutine；
- `Stop` 必须幂等并等待其拥有的 worker；
- 必需依赖不得通过运行时 setter 回填；
- 测试替换通过构造参数或 option 提供。

`cmd/server` 最终只处理配置、环境、日志、信号和应用启动。

## 12. 阶段 6：Engine 与 Automation

### 6.1 Engine

保留 `Account` facade，按纯策略、运行状态、WS 记录、去重、防抖、任务生命周期、凭证状态、
连接循环的顺序提取。每个组件必须拥有自己的状态和锁，禁止只移动方法却继续共享整个 Account。

每个并发组件必须在中文注释中写明：

- 锁保护的字段；
- 是否允许嵌套持锁；
- 持锁时能否执行 I/O；
- goroutine 的创建、取消和等待责任；
- Stop 的幂等与等待语义。

### 6.2 Automation

逐步提取事件事实记录、规则匹配、运行协调、动作执行、发货、卡密分配、凭证门禁和结果通知。
规则匹配不得隐式执行外部动作；外部动作必须区分未执行、已执行和结果不确定。

### 6.3 冻结边界

本阶段默认不修改任何冻结滑块文件，也不得通过调用方重构改变冻结行为。
涉及凭证、连接或风控恢复时必须执行冻结规范规定的测试。

## 13. 阶段 7：React Feature 化

### 7.1 目标结构

按 `app`、`features`、`shared` 和 `generated` 分层。每个 feature 自有 API、类型、Hook、组件、页面和测试。
禁止使用会隐藏依赖来源的大型 barrel export。

### 7.2 拆分顺序

按 `Rules`、`ItemList`、`AccountList`、`CardList`、`OrderList`、`Notifications`、
`Dashboard`、`Settings`、`Chat` 的顺序拆分。

### 7.3 React 强约束

- 可由 props/state 计算的值不得重复存入 state；
- 用户操作副作用放在事件处理器中，不使用 state + effect 间接触发；
- 依赖不同的副作用拆成不同 effect；
- 基于旧值更新必须使用函数式 setState；
- 异步请求必须支持取消或 generation gate；
- 独立请求应并行启动；
- memo 只用于实际昂贵计算或稳定子组件输入；
- 不在组件内部声明子组件；
- 服务端数据、表单状态和短暂 UI 状态必须分开；
- 重页面和重依赖使用 `lazy`/`Suspense` 按页面加载；
- 组件不得直接调用 `fetch` 或 `axios`。

### 7.4 测试

关键流程使用行为测试覆盖成功、失败、取消、切换、过期响应和重复提交。
源码字符串测试只保留真正的静态架构规则。

## 14. 阶段 8：DB 与事务治理

- 上层逐步改为持有窄 repository 接口，而不是完整 Store；
- 上层不得访问裸 `*sql.DB`；
- SQL 行结构、持久化模型、领域模型和 HTTP DTO 分离；
- 是否拆物理 package 由稳定后的事务与依赖方向决定，不以目录数量为目标；
- 三套迁移编号和关键 schema 自动校验；
- 新迁移必须在 SQLite、MySQL、Postgres 上验证；
- Credential 锁最终迁移到职责明确的凭证协调组件。

## 15. 阶段 9：架构门禁与兼容清理

目标结构稳定后加入自动检查：

- `internal/server` 禁止导入 `internal/db` 和 `internal/xianyu`；
- `internal/db`、`internal/xianyu`、`internal/browser` 禁止导入上层应用包；
- 前端 feature 禁止跨 feature 导入内部文件；
- React 组件禁止直接调用网络客户端；
- 到期兼容字段和路由必须先确认调用方为零，再删除；
- 架构门禁只能约束目标结构，不得把过渡期错误依赖永久合法化。

## 16. 阶段 10：注释基线清零

注释治理贯穿所有阶段。最终阶段负责：

- 清理所有历史基线豁免；
- 检查 Go 与 TypeScript/TSX 生产和测试源码；
- 抽样审查注释准确性；
- 确认注释描述的是职责、语义、单位、敏感性和并发约束，而非复述语法；
- 将全仓严格检查加入 PR CI。

## 17. 推荐变更序列

1. 治理文档、AGENTS 强约束和注释基线工具；
2. 独立 PR CI；
3. server SQLite 测试模板与 race 优化；
4. 前端行为测试基础；
5. AccountSummary 与所有权查询；
6. AccountCredential 与 AccountLoginSecret；
7. 统一 API 错误；
8. 账号 DTO 与版本化接口；
9. 订单 DTO 与版本化接口；
10. 前端 shared client 与 feature API；
11. Rules 页面；
12. ItemList 页面；
13. 订单应用服务；
14. 发布应用服务与 worker；
15. 账号登录与 QR 服务；
16. 应用生命周期装配；
17. Automation 内部组件；
18. Engine 纯策略与状态组件；
19. Engine 连接与凭证组件；
20. DB Store 与事务边界；
21. 旧 API 和旧字段清理；
22. 架构门禁与注释基线清零。

## 18. 每个变更的执行模板

开始前：

1. 在本文状态表中确认所属阶段；
2. 写明本次改动的行为不变量；
3. 列出涉及文件、风险、回滚方式和验证命令；
4. 检查是否触及敏感凭证、多数据库、并发、前端产物或冻结滑块边界。

实施时：

1. 先补足或建立保护行为的测试；
2. 一次只移动一个职责；
3. 不顺手清理无关代码；
4. 为所有新增或修改声明补准确中文注释；
5. 保留兼容 adapter，直到调用方迁移完成。

完成时：

1. 执行适用验证矩阵；
2. 检查工作区没有意外生成物；
3. 更新本计划状态和完成证据；
4. 记录下一步最小安全入口；
5. 确认未放宽测试或安全边界。

## 19. 验证矩阵

所有 Go 修改至少执行：

```bash
go vet ./...
golangci-lint run ./...
go test ./...
```

并发和生命周期修改增加：

```bash
go test -race ./internal/engine ./internal/account ./internal/automation ./internal/renewal ./internal/notify
```

数据库修改增加：

```bash
go test ./internal/db
go run ./cmd/dbverify "sqlite:///tmp/xianyu-verify.db"
```

并在可用环境执行 MySQL/Postgres 多数据库回归。

前端修改执行：

```bash
npm --prefix frontend run typecheck
npm --prefix frontend test
npm --prefix frontend run build
```

涉及凭证、登录、engine、account、server 或 browser 调用关系时，额外执行
`docs/slider-captcha-frozen-spec.md` 中规定的测试，即使未直接修改受保护文件。

## 20. 计划更新记录

| 日期 | 变更 | 结果 | 下一步 |
| --- | --- | --- | --- |
| 2026-08-14 | 建立长期重构计划、依赖规则和中文注释规范 | 阶段 0 开始 | 将强约束接入 AGENTS，随后实现注释基线工具与独立 CI |
| 2026-08-14 | 将计划治理、注释、依赖、敏感数据、API、React、数据库和并发规则接入 AGENTS | 后续任务已有强制入口 | 实现注释检查器和历史基线 |
| 2026-08-14 | 落地 Go/TypeScript AST 注释检查器、Make 目标、npm 脚本和历史基线 | 阶段 0 完成；`make comments`、前端注释检查和 typecheck 通过 | 阶段 1.1：新增独立 PR CI |
| 2026-08-14 | 新增独立 Go/React PR CI，加入格式、注释、vet、lint、测试和嵌入产物一致性门禁 | 阶段 1 进行中；本地 Go/前端测试、构建和 YAML 解析通过 | 优化 server 测试数据库模板并记录 race 分层结果 |
| 2026-08-14 | server 测试改为一次迁移模板 + 每测独立副本 | 普通 server 测试约 48.2s 降至约 36.5s；稳定 server race 子集约 12.4s 通过；完整 race 运行 267s 后仍未完成，未发现 race 报告 | 定位完整 race 慢点，再进入敏感数据访问边界 |
| 2026-08-14 | 将稳定 server race 子集固化为 `make test-server-race` 并接入 PR CI | 启停、发布 worker、凭证状态转换和锁内所有权复核场景纳入合并前 smoke race | 定位完整 race 慢点 |
| 2026-08-14 | 将管理员与账号 cookie 预置到 server 测试模板 | 普通 server 测试约 21.3s；完整 `go test -race ./internal/server` 约 194.3s 通过；阶段 1 完成 | 阶段 2：盘点敏感数据查询调用方 |
| 2026-08-14 | 新增 `CookieSummary`、`ListOwnedIDs`、`ExistsOwned` 及跨用户/无效 user ID 回归测试 | 阶段 2 第一个数据边界切片完成；故意无效密文摘要查询通过 | 迁移 server ownership helper，移除 `AllForUser` 所有权读取 |
| 2026-08-14 | Engine 登录态检查与接口续期改用 `GetCookieRuntimeData` | 只解密 Cookie 与 metadata；接口续期窄查询回归测试通过，未改变锁、token 清理和快照持久化语义 | 迁移 `persistRenewFlatCookie` 的 metadata 窄查询 |
| 2026-08-14 | 新增 `GetCookieMetadata` 并收窄 `persistRenewFlatCookie` | 扁平 Cookie 写回不再读取旧 Cookie、用户名或登录密码；损坏旧凭证回归测试通过 | 迁移 `handleMaxFailures` 的单值 Cookie 读取 |
| 2026-08-14 | `handleMaxFailures` 改用 `GetValue` | 恢复回调只读取 Cookie 明文；损坏登录密码回归测试通过，失败计数和重连行为保持不变 | 迁移 `persistPendingRenewCookies` 的异步续期读取 |
| 2026-08-14 | `persistPendingRenewCookies` 改用 `GetCookieRuntimeData` | 迟到 Cookie 合并只解密 Cookie 与 metadata；锁、并发重放和通知行为保持不变，回归测试通过 | 迁移 `refreshTokenWithMinGap` 的 metadata 读取 |
| 2026-08-14 | `refreshTokenWithMinGap` 改用 `GetCookieMetadata` | token 请求只解密 Cookie 快照 metadata；快照上下文和 token 刷新行为保持不变，回归测试通过 | 迁移 `adoptTokenResponseCookies` 的 metadata 读取 |
| 2026-08-14 | `adoptTokenResponseCookies` 改用 `GetCookieMetadata` | token 响应合并只解密 metadata；快照持久化和错误语义保持不变，回归测试通过 | 迁移 `databaseCredentialFingerprint` 的运行时凭证读取 |
| 2026-08-14 | `databaseCredentialFingerprint` 改用 `GetCookieRuntimeData` | token 凭证一致性校验只解密 Cookie 与 metadata；空值、指纹和错误语义保持不变，回归测试通过 | 迁移 `reloadCookieFromDB` 的运行时凭证读取 |
| 2026-08-14 | 完成阶段 2 当前 PR 切片“Server 平台凭证流程审计” | Server 平台、订单、发布、二维码和资料流程均不再读取完整账号详情；完整详情仅保留给登录设置与登录信息更新；窄查询回归测试、全量测试、race、vet、lint 和注释门禁通过 | 进行敏感数据边界最终审计，确认阶段 2 完成证据后进入阶段 3 |
| 2026-08-14 | 完成阶段 2 最终审计 PR 切片 | 生产 `GetDetails` 白名单仅保留登录设置与登录信息更新；新增跨数据库 Cookie/metadata/平台视图/所有权窄查询回归；全量测试、race、vet、lint、注释和 diff 门禁通过 | 阶段 3：HTTP 错误结构盘点与第一批契约测试 |
| 2026-08-14 | 完成阶段 3 第一个 PR 切片“HTTP 错误结构盘点与第一批契约测试” | 共享错误 DTO、认证 401、健康检查和账号列表具名 DTO 已落地；React 请求层完成错误契约迁移；Go/React 全量测试、vet、lint、注释和前端构建通过 | 阶段 3：剩余认证与公共 API 错误迁移 |
| 2026-08-14 | 完成阶段 3 第二个 PR 切片“剩余认证与公共 API 错误迁移” | 初始化、密码修改、凭据校验、用户名冲突、公开设置故障和 SPA API 404 的状态码/错误码契约已统一；契约测试、全量测试、vet、lint、注释和 diff 门禁通过 | 阶段 3：订单与账号业务 API 错误响应迁移 |
| 2026-08-14 | 完成阶段 3 第三个 PR 切片“订单与账号业务 API 错误响应迁移” | 订单批量接口移除顶层 HTTP 200 + `success:false`，逐项失败保留行级状态并统一为 `message`；账号任务 502 改用统一错误 DTO；订单/账号契约测试、全量测试、race、vet、lint、注释和 diff 门禁通过 | 阶段 3：聊天、商品与自动化业务 API 错误响应迁移 |
| 2026-08-14 | 完成阶段 3 第四个 PR 切片“聊天、商品与自动化业务 API 错误响应迁移” | 聊天发送和商品发布失败统一为 `code`/`message`/`request_id`，远端发布后的商品核对信息迁移到 `details`；React 错误类型、HTTP DTO 测试和业务契约测试已更新；前端类型检查、测试、构建及 Go 注释门禁通过 | 阶段 3：二维码登录、密码登录和剩余公共业务错误响应迁移 |
| 2026-08-14 | 完成阶段 3 第五个 PR 切片“二维码登录、密码登录和剩余公共业务错误响应迁移” | 二维码与密码登录遗留 HTTP 200 + `success:false` 已迁移为非 2xx 错误；账号不匹配保留 `scanned_account_id` 到 `details`；二维码/密码登录契约测试、全量测试、race、vet、lint、注释和 diff 门禁通过 | 阶段 3：统一业务成功响应具名 DTO 与版本化路径准备 |
| 2026-08-14 | 完成阶段 3 第六个 PR 切片“统一业务成功响应具名 DTO 与版本化路径准备” | 认证会话、账号新增、订单列表、聊天会话/消息主链路已改用具名成功响应 DTO；新增响应契约测试和 `/api/v1` 迁移边界文档；全量测试、race、vet、lint、注释和 diff 门禁通过 | 阶段 3：剩余业务成功响应 DTO 与客户端契约收口 |
| 2026-08-14 | 完成阶段 3 第七个 PR 切片“剩余业务成功响应 DTO 与客户端契约收口” | 账号详情/设置、商品发布/同步、自动化规则/异常、订单详情/刷新/批量外层响应已具名化；React API 类型同步；跨领域契约测试、全量测试、race、vet、lint、注释、前端构建和 diff 门禁通过；所有修改合并为一个可回滚提交 | 阶段 3：剩余商品批量操作与设置/卡券/通知成功响应收口 |
| 2026-08-14 | 完成阶段 3 第八个 PR 切片“剩余商品批量操作与设置/卡券/通知成功响应收口” | 商品类目推荐、批量发布预检/任务、系统与用户设置、账号 AI 设置、卡券 CRUD/批量、通知渠道与账号绑定已具名化；React API 类型同步；跨领域契约测试、全量测试、race、vet、lint、注释、前端构建和 diff 门禁通过；所有修改合并为一个可回滚提交 | 阶段 3：剩余关键词回复、默认回复与账号任务成功响应收口 |
| 2026-08-14 | 完成阶段 3 第九个 PR 切片“剩余关键词回复、默认回复与账号任务成功响应收口” | 关键词基础/商品/类型规则、指定商品回复、默认回复、账号任务设置与运行记录已具名化；React API 类型同步；跨领域契约测试、全量测试、race、vet、lint、注释、前端构建和 diff 门禁通过；所有修改合并为一个可回滚提交 | 阶段 3：分析统计、管理员与剩余公共成功响应收口 |
| 2026-08-14 | 完成阶段 3 第十个 PR 切片“分析统计、管理员与剩余公共成功响应收口” | 管理员用户/账号/统计、用户仪表盘、订单分析、有效订单分页和二维码生成已具名化；React API 类型同步；跨领域契约测试、全量测试、race、vet、lint、注释、前端构建和 diff 门禁通过；所有修改合并为一个可回滚提交 | 阶段 3：公共成功响应兼容收尾与版本化迁移入口审计 |
| 2026-08-14 | 完成阶段 3 第十一个 PR 切片“公共成功响应兼容收尾与版本化迁移入口审计” | 系统、管理员和用户动态设置统一通过具名 map 边界类型；二维码状态保留非敏感扩展字段并过滤 Cookie，验证完成使用具名 DTO；React API 清理剩余 `Promise<any>`/`get<any>` 成功响应并补齐兼容类型；新增动态设置与二维码契约测试，明确旧路径仍保留、尚未宣称 `/api/v1` 可用；所有修改合并为一个可回滚提交 | 阶段 3：HTTP API 版本化兼容入口落地与调用方迁移 |
| 2026-08-14 | 完成阶段 3 第十二个 PR 切片“HTTP API 版本化兼容入口与会话调用方迁移” | 新增 `/api/v1/session/login`、`/api/v1/session/initialize`、`/api/v1/session` 与 `/api/v1/session/logout` 薄适配入口，全部复用既有认证 handler；React 登录、初始化、会话校验和登出调用已迁移；Go/React 契约测试确认新旧路径兼容；全量门禁通过并合并为一个可回滚提交 | 阶段 3：账号 API 版本化兼容入口与调用方迁移 |
| 2026-08-14 | 完成阶段 3 第十三个 PR 切片“账号 API 版本化兼容入口与调用方迁移” | 新增 `/api/v1/accounts`、`/api/v1/accounts/details`、`/api/v1/accounts/runtime-status`、`/api/v1/accounts/{cid}` 与启停状态薄适配入口，全部复用既有账号 handler；React 账号摘要、详情、运行状态和启停状态调用已迁移；Go/React 契约测试确认新旧路径兼容且详情不泄露凭证；全量门禁通过并合并为一个可回滚提交 | 阶段 3：账号设置与资料 API 版本化兼容入口迁移 |
| 2026-08-14 | 完成阶段 3 第十四个 PR 切片“账号设置与资料 API 版本化兼容入口迁移” | 新增 `/api/v1/accounts/{cid}/settings`、`remark`、`pause-duration`、`auto-confirm`、`long-login` 和 `refresh-profile` 薄适配入口，全部复用既有账号 handler；React 账号设置、备注、暂停、自动确认、长登录和资料刷新调用已迁移；Go/React 契约测试确认新旧路径兼容；全量门禁通过并合并为一个可回滚提交 | 阶段 3：账号凭证与登录信息 API 版本化兼容入口迁移 |
| 2026-08-14 | 完成阶段 3 第十五个 PR 切片“账号凭证与登录信息 API 版本化兼容入口迁移” | 新增 `/api/v1/accounts`、`/api/v1/accounts/{cid}`、`/api/v1/accounts/{cid}/login-info` 薄适配入口，复用既有 handler 和凭证锁；React 新增/更新 Cookie 与登录信息调用已迁移；Go/React 契约测试确认敏感字段不回传且旧路径兼容；全量门禁通过并合并为一个可回滚提交 | 阶段 3：订单 API 版本化兼容入口与调用方迁移 |
| 2026-08-14 | 完成阶段 3 第十六个 PR 切片“订单 API 版本化兼容入口与调用方迁移” | 新增 `/api/v1/orders`、`/api/v1/orders/{order_id}` 的列表、详情和更新薄适配入口，复用既有订单 handler 与归属校验；React 列表、详情和更新调用已迁移；Go/React 契约测试确认新旧路径兼容；全量门禁通过并合并为一个可回滚提交 | 阶段 3：订单刷新与批量操作 API 版本化兼容入口迁移 |
| 2026-08-14 | 完成阶段 3 第十七个 PR 切片“订单刷新与批量操作 API 版本化兼容入口迁移” | 新增订单刷新、单订单刷新、手动发货和导入 `/api/v1/orders...` 薄适配入口，复用既有订单 handler；React 刷新、单订单刷新、手动发货和导入调用已迁移；Go/React 契约测试确认新旧入口兼容；全量门禁通过并合并为一个可回滚提交 | 阶段 3：商品 API 版本化兼容入口与调用方迁移 |
| 2026-08-14 | 完成阶段 3 第十八个 PR 切片“商品 API 版本化兼容入口与调用方迁移” | 新增商品列表、详情、发布、更新和删除 `/api/v1/items...` 薄适配入口，复用既有商品 handler 与所有权校验；React 列表、详情、发布、更新和删除调用已迁移；Go/React 契约测试确认新旧入口兼容；全量门禁通过并合并为一个可回滚提交 | 阶段 3：商品同步与批量发布 API 版本化兼容入口迁移 |
| 2026-08-14 | 完成阶段 3 第十九个 PR 切片“商品同步与批量发布 API 版本化兼容入口迁移” | 新增商品同步、类目推荐、批量发布预检/任务/详情、取消、重试和结果下载 `/api/v1/items...` 薄适配入口，复用既有商品 handler；React 同步和批量发布调用已迁移；Go/React 契约测试确认新旧入口兼容；全量门禁通过并合并为一个可回滚提交 | 阶段 3：设置、卡券与通知 API 版本化兼容入口迁移 |
| 2026-08-14 | 完成阶段 3 第二十个 PR 切片“设置、卡券与通知 API 版本化兼容入口迁移” | 新增系统/用户/AI 设置、卡券和通知渠道/消息/账号绑定 `/api/v1/settings`、`/api/v1/cards`、`/api/v1/notifications` 薄适配入口，复用既有权限边界与 handler；React 设置、卡券和通知调用已迁移；Go/React 契约测试确认新旧入口兼容；全量门禁通过并合并为一个可回滚提交 | 阶段 3：聊天与账号任务 API 版本化兼容入口迁移 |
| 2026-08-14 | 完成阶段 3 第二十一个 PR 切片“聊天与账号任务 API 版本化兼容入口迁移” | 新增聊天 REST/WebSocket、账号任务设置/运行记录/执行 `/api/v1/chat...`、`/api/v1/account-tasks...` 薄适配入口，复用既有 handler 与权限校验；React 聊天 REST/WebSocket 和账号任务调用已迁移；Go/React 契约测试确认新旧入口兼容；全量门禁通过并合并为一个可回滚提交 | 阶段 3：关键词回复与默认回复 API 版本化兼容入口迁移 |
| 2026-08-14 | 完成阶段 3 第二十二个 PR 切片“关键词回复与默认回复 API 版本化兼容入口迁移” | 新增关键词基础/商品/类型规则、指定商品回复和默认回复 `/api/v1/reply-rules...`、`/api/v1/default-replies...` 薄适配入口，复用既有 handler 与权限校验；React 关键词、指定商品回复和默认回复调用已迁移；Go/React 契约测试确认新旧入口兼容；全量门禁通过并合并为一个可回滚提交 | 阶段 3：管理员、仪表盘与订单分析 API 版本化兼容入口迁移 |
| 2026-08-14 | 完成阶段 3 第二十三个 PR 切片“管理员、仪表盘与订单分析 API 版本化兼容入口迁移” | 新增管理员用户/账号/统计、仪表盘统计、订单分析和有效订单 `/api/v1/admin...`、`/api/v1/analytics...` 薄适配入口，复用既有权限边界与 handler；React 管理员统计、仪表盘和订单分析调用已迁移；Go/React 契约测试确认新旧入口兼容；全量门禁通过并合并为一个可回滚提交 | 阶段 3：二维码登录 API 版本化兼容入口迁移 |
| 2026-08-14 | 完成阶段 3 第二十四个 PR 切片“二维码登录 API 版本化兼容入口迁移” | 新增二维码生成、状态查询、状态持久化和验证完成 `/api/v1/qr-login...` 薄适配入口，复用既有认证、会话所有权和敏感字段过滤 handler；React 二维码生成、轮询和验证完成调用已迁移；Go/React 契约测试确认新旧入口兼容；全量门禁通过并合并为一个可回滚提交 | 阶段 3：密码登录及剩余公共调用方 API 版本化兼容入口迁移 |
| 2026-08-14 | 完成阶段 3 第二十五个 PR 切片“密码登录及剩余公共调用方 API 版本化兼容入口迁移” | 新增会话密码/凭证、账号删除、密码登录禁用、自动化规则/异常处理、订单删除和商品创建 `/api/v1/...` 薄适配入口，复用既有认证、权限和业务 handler；React 对应调用已迁移；Go/React 契约测试确认新旧入口兼容；全量门禁通过并合并为一个可回滚提交 | 阶段 3：版本化入口最终审计与旧调用方清零 |
| 2026-08-14 | 完成阶段 3 第二十六个 PR 切片“版本化入口最终审计与旧调用方清零” | 前端生产调用和批量结果下载已无业务旧路径；Vite 代理收敛为 `/api` 与健康检查；服务端补齐商品按账号列表、多规格和多数量兼容入口；Go/React/Vite 审计测试、全量测试、race、vet、lint、注释和构建门禁通过并合并为一个可回滚提交 | 阶段 4：订单应用服务边界提取 |
| 2026-08-14 | 完成阶段 4 第一个 PR 切片“订单应用服务边界提取” | 新增不依赖 `net/http` 的订单应用服务，覆盖列表、详情、更新、删除、导入、手动发货、批量刷新和单订单刷新；保留权限、事务、凭证锁、Cookie Jar 和 Session 续期语义；服务层测试、全量测试、race、vet、lint、注释门禁通过并合并为一个可回滚提交 | 阶段 4：订单响应 DTO 映射收口 |
| 2026-08-14 | 完成阶段 4 第二个 PR 切片“订单响应 DTO 映射收口” | 列表/详情/刷新响应视图由应用服务统一生成；错误分类模型替代 HTTP handler 中的错误文本判断；旧/版本化订单契约、全量测试、race、vet、lint、注释和 diff 门禁通过，合并为一个可回滚提交 | 阶段 4：商品发布应用服务边界提取 |
| 2026-08-14 | 完成阶段 4 第三个 PR 切片“商品发布应用服务边界提取” | 新增不依赖 `net/http` 的商品发布应用服务，覆盖单商品发布、类目推荐、批量预检持久化、批次启动/查询/取消/删除/失败重试；保留 worker、凭证锁、cancel map、租约、远端发布与自动化规则时序；新增服务层测试，Go 全量测试、vet、lint、注释和 diff 门禁通过并合并为一个可回滚提交 | 阶段 4：账号登录应用服务边界提取 |
| 2026-08-14 | 完成阶段 4 第四个 PR 切片“账号登录应用服务边界提取” | 新增不依赖 `net/http` 的账号登录应用服务，覆盖扫码结果幂等持久化、Cookie Jar/扁平 Cookie 合并、账号新增与更新、资料刷新、登录审计和运行时重启；保留扫码会话锁、账号凭证锁和旧/版本化 HTTP 契约；新增服务层测试，Go 全量测试、race、vet、lint、注释和 diff 门禁通过并合并为一个可回滚提交 | 阶段 4：聊天与通知应用服务边界提取 |
| 2026-08-14 | 完成阶段 4 第五个 PR 切片“聊天与通知应用服务边界提取” | 新增不依赖 `net/http` 的通信应用服务，覆盖账号任务设置/执行/记录、聊天文字与图片发送、聊天历史读取/已读、通知渠道 CRUD、账号绑定和删除；保留 WebSocket 订阅、聊天事件广播、通知 outbox 与失败状态语义；新增服务层测试，Go 全量测试、race、vet、lint、注释和 diff 门禁通过并合并为一个可回滚提交 | 阶段 4：Server 事务与生命周期边界收口 |
| 2026-08-14 | 完成阶段 4 第六个 PR 切片“Server 事务与生命周期边界收口” | 新增 Server 统一 Unit of Work，订单更新与导入不再直接管理事务；新增后台任务生命周期登记入口，HTTP 优雅关闭与批量发布恢复扫描器统一纳入等待流程；修复服务不可用时 handler 先做资源归属校验导致状态码变化的问题；事务测试、Go 全量测试、race、vet、lint、注释和 diff 门禁通过并合并为一个可回滚提交 | 阶段 4：Server 直接依赖清理与应用服务装配收口 |
| 2026-08-14 | 完成阶段 4 第七个 PR 切片“Server 直接依赖清理与应用服务装配收口” | 统一装配订单、发布、登录、通信和分析应用服务；默认回复、账号设置、AI 设置、通知绑定、管理员查询及订单分析查询迁移到 repository 或应用服务边界；handler 不再直接访问 `Store.DB`；装配测试、Go 全量测试、race、vet、lint、注释和 diff 门禁通过并合并为一个可回滚提交 | 阶段 5：应用构造依赖验证与生命周期接口 |
| 2026-08-14 | 完成阶段 5 PR 切片“应用构造依赖验证与生命周期接口” | `Server.New` 在构造阶段校验 `Store/Manager`，聊天服务改用 option 注入并移除生产运行时 setter；新增幂等 `Start/Wait/Stop`，`Stop` 统一等待 HTTP、后台扫描器和批量 worker；`cmd/server` 迁移到显式生命周期入口；构造失败、重复启动/停止和 worker 等待测试、全量测试、race、vet、lint、注释和 diff 门禁通过并合并为一个可回滚提交 | 阶段 6：Engine 账号 facade 与运行状态边界 |
| 2026-08-14 | 完成阶段 6 第一个 PR 切片“Engine 账号 facade 与运行状态边界” | 将连接状态、失败计数、离线告警和业务任务生命周期分别提取为独立锁组件；`Account` 保留 facade，`Stop` 对并发调用保持幂等并等待已登记任务；新增生命周期并发回归测试，未改变 WebSocket、凭证、自动化和冻结风控逻辑；全量测试、Engine race、Server race、vet、lint、注释和 diff 门禁通过并合并为一个可回滚提交 | 阶段 6：Engine WebSocket 连接循环边界 |
| 2026-08-14 | 完成阶段 6 第二个 PR 切片“Engine WebSocket 连接循环边界” | `registerConnection` 在凭证锁内统一快照复核与 WebSocket 注册，`runConnectionSession` 统一心跳、接收、Token 轮换 goroutine 的创建/取消/等待；`Account` 继续负责凭证错误、风控和重连结果解释；新增会话收束测试，未改变冻结风控行为；全量测试、Engine race、Server race、vet、lint、注释和 diff 门禁通过并合并为一个可回滚提交 | 阶段 6：Engine WebSocket 记录与消息分发边界 |
| 2026-08-14 | 完成阶段 6 第三个 PR 切片“Engine WebSocket 记录与消息分发边界” | 新增 `messageDispatcher` 与 `wsRecorder`，分别管理消息去重/防抖/并发投递和 WebSocket 诊断记录队列/worker；`Account` 仅保留 facade 与生命周期接线，动态 Handler、系统事件背压、聊天洪峰限流和 Stop 等待语义保持兼容；Engine 全量回归与并发测试、后续门禁通过并合并为一个可回滚提交 | 阶段 6：Engine 凭证状态与 Token 生命周期边界 |
| 2026-08-14 | 完成阶段 6 第四个 PR 切片“Engine 凭证状态与 Token 生命周期边界” | 新增 `credentialState`，集中 Cookie 快照、Token 缓存、刷新锁、设备指纹和刷新诊断状态；运行状态组件锁改名为 `runtimeMu`，保持 `Account` facade 字段访问兼容；Cookie 更新、Token 清理、风控恢复和 WS 注册前凭证快照复核语义保持不变；Engine 全量测试、Engine/Server race、vet、lint、注释和前端构建门禁通过并合并为一个可回滚提交 | 阶段 6：Automation 事件事实与规则匹配边界 |
| 2026-08-14 | 完成阶段 6 第五个 PR 切片“Automation 事件事实与规则匹配边界” | 新增 `eventFactRecorder`、`ruleMatcher` 和 `actionPlanner`；事件事实写入、规则查询和动作计划生成职责分离，`Center`/`Scheduler` 不再直接散落调用规则匹配；动作计划保持付款发卡优先、规格过滤和延迟快照语义，新增纯计划与无订单事实回归测试；Automation/Server race、全量测试、vet、lint、注释和前端构建门禁通过并合并为一个可回滚提交 | 阶段 6：Automation 运行协调与动作执行边界 |
| 2026-08-14 | 完成阶段 6 第六个 PR 切片“Automation 运行协调与动作执行边界” | 新增 `automationRunCoordinator`，统一运行创建/恢复、动作前后检查点、延迟任务续租、账号门禁和不确定结果隔离；`Center` 保留兼容调用入口，外部动作三态语义、人工核对状态机和恢复游标保持不变；Automation 测试、注释和 diff 门禁通过并合并为一个可回滚提交 | 阶段 6：Automation 发货、卡密与通知动作边界 |
| 2026-08-14 | 完成阶段 6 第七个 PR 切片“Automation 发货、卡密与通知动作边界” | 新增 `automationActionExecutor` 与 `deliveryNotifier`，统一确认发货、Cookie/Jar 合并、卡券锁与库存消费、消息错误三态分类和结果通知；`Center` 保留兼容入口，凭证锁、卡券库存、恢复唤醒和通知文案语义保持不变；新增动作执行器回归测试；Automation 测试、注释和 diff 门禁通过并合并为一个可回滚提交 | 阶段 6：Automation 账号任务与凭证门禁边界 |
| 2026-08-14 | 完成阶段 6 第八个 PR 切片“Automation 账号任务与凭证门禁边界” | 新增 `accountTaskCoordinator`，统一账号状态门禁、自动评价/商品擦亮调度、任务租约、Session 指纹阻断、凭证恢复和 Cookie 同步；`Center` 保留公开任务入口，Session 失效恢复、任务幂等、失败重试和账号暂停语义保持不变；Automation 测试、注释和 diff 门禁通过并合并为一个可回滚提交 | 阶段 7：React Rules feature 化与 API/行为测试边界 |
| 2026-08-14 | 完成阶段 7 第一个 PR 切片“React Rules feature 化与 API/行为测试边界” | 新增 `app/features/rules` 的 API 适配层、领域类型、数据 Hook、异常面板和交互状态模型；规则请求支持并行加载与请求代次门禁，保存动作阻断重复提交；行为测试覆盖成功、失败、重复提交、过期响应和页签切换；前端类型检查、全量测试、注释检查和构建门禁通过并合并为一个可回滚提交 | 阶段 7：React ItemList feature 化与批量发布行为边界 |
| 2026-08-14 | 完成阶段 7 第二个 PR 切片“React ItemList feature 化与批量发布行为边界” | 新增 `app/features/items` 的 API 适配层、批量任务类型、批量 Hook、批量状态模型和阶段指示器组件；批量预检、任务恢复、轮询过期响应、安全取消和失败重试均有独立状态边界；行为测试覆盖预检门禁、取消状态、失败重试、历史任务筛选和过期轮询响应；前端类型检查、全量测试、注释检查和构建门禁通过并合并为一个可回滚提交 | 阶段 7：React AccountList feature 化与账号运行状态行为边界 |
| 2026-08-15 | 完成阶段 7 第三个 PR 切片“React AccountList feature 化与账号运行状态行为边界” | 新增 `app/features/accounts` 的 API 适配层、账号数据与运行状态 Hook、账号编辑弹窗、登录/暂停状态模型；账号加载和运行状态轮询具备取消与代次门禁，密码登录响应按账号与代次隔离，兼容入口保留；行为测试覆盖账号切换、暂停/恢复、凭证失败、风控验证和过期响应；前端类型检查、全量测试、注释检查和构建门禁通过并合并为一个可回滚提交 | 阶段 7：React CardList feature 化与卡密库存/批量追加行为边界 |
| 2026-08-15 | 完成阶段 7 第四个 PR 切片“React CardList feature 化与卡密库存/批量追加行为边界” | 按 `app/features/cards` 提取卡密 API 适配层、库存数据 Hook、批量导入/追加状态 Hook 和弹窗组件；卡密筛选、追加预览、提交取消、失败重试与账号切换过期响应均由 feature 状态边界负责，保留旧组件入口和 API 路径；新增状态模型测试并通过前端类型检查、全量测试、注释检查和构建门禁，合并为一个可回滚提交 | 阶段 7：React OrderList feature 化与订单导入/刷新行为边界 |
| 2026-08-15 | 完成阶段 7 第五个 PR 切片“React OrderList feature 化与订单导入/刷新行为边界” | 按 `app/features/orders` 提取订单 API 适配层、查询/分页 Hook、导入 Hook、筛选栏和导入弹窗；订单查询与辅助数据支持并行加载、取消和代次门禁，导入支持文件预检、取消、失败重试和导入后刷新；新增筛选、导入归一化、API 取消信号和过期响应测试，保留旧组件入口和 API 路径；前端类型检查、全量测试、注释检查和构建门禁通过并合并为一个可回滚提交 | 阶段 7：React Notifications feature 化与渠道/事件绑定行为边界 |
| 2026-08-15 | 完成阶段 7 第六个 PR 切片“React Notifications feature 化与渠道/事件绑定行为边界” | 按 `app/features/notifications` 提取通知渠道/SMTP API 适配层、静态渠道配置、数据与动作 Hook、渠道列表、事件绑定选择器、渠道编辑弹窗和 SMTP 面板；渠道与 SMTP 请求支持取消和代次门禁，表单支持渠道字段/独立 SMTP 校验、保存失败重试和过期响应保护；更新架构字符串测试并新增状态/API 取消信号测试，保留旧页面入口和 API 路径；前端类型检查、全量测试、注释检查和构建门禁通过并合并为一个可回滚提交 | 阶段 7：React Dashboard feature 化与统计/趋势查询行为边界 |
| 2026-08-15 | 完成阶段 7 第七个 PR 切片“React Dashboard feature 化与统计/趋势查询行为边界” | 按 `app/features/dashboard` 提取 Dashboard API 适配层、统计数据 Hook、趋势/排行派生状态和请求边界；概览、趋势和有效订单支持并行加载、刷新取消、失败重试与过期响应保护；新增统计派生数据、API 取消信号和请求代次测试，保留旧页面入口和 API 路径；前端类型检查、全量测试、注释检查和构建门禁通过并合并为一个可回滚提交 | 阶段 7：React Settings feature 化与系统配置校验边界 |
| 2026-08-15 | 完成阶段 7 第八个 PR 切片“React Settings feature 化与系统配置校验边界” | 按 `app/features/settings` 提取系统配置/模型/凭据 API 适配层、配置常量、表单状态和敏感字段校验；配置读取与保存支持并行加载、请求取消、失败重试和过期响应保护，登录凭据提交保留现有校验与重登录语义；新增配置裁剪、凭据校验、API 取消信号和请求代次测试，保留旧页面入口和 API 路径 | 阶段 7：React Chat feature 化与会话消息行为边界 |
