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

审计重开基线（2026-08-15）：此前将测试覆盖率收口误判为目标架构完成，现已撤销该判断。当前仍存在敏感设置明文进入前端、Server 持有基础设施编排、Port 泄露 `sql.Tx`/`db.*`、凭证锁覆盖慢速外部 I/O、外部成功后本地状态缺少统一补偿、N+1 同步、关闭游离 goroutine、大型页面同步加载和模板化注释假阴性等问题。覆盖率、lint、race 通过只能证明当前行为可验证，不能替代分层和状态正确性完成条件。

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
| 2. 敏感数据访问边界 | 进行中 | 摘要、凭证、登录秘密和系统设置分离 | Cookie/平台运行视图已收口；待完成系统设置脱敏读写、运维输出脱敏、敏感字段访问审计和三库回归 |
| 3. HTTP API 契约 | 已完成 | 统一错误、具名 DTO、版本化路径 | 统一错误 DTO、具名成功 DTO、所有前端业务调用方版本化、旧路径兼容和最终审计均已完成 |
| 4. Server 应用服务 | 进行中 | 订单、发布、登录、聊天纵向抽取 | 已完成文件级服务拆分；待完成应用 Port 独立包、Server 不持有业务服务定位器、handler 不依赖 db/xianyu/browser |
| 5. 应用生命周期装配 | 进行中 | 消除必需依赖 setter 回填并统一关闭边界 | 已有 Server 自有 worker 等待；待完成 Context-aware Stop、账号 StopAll 超时、删除任务登记和统一生命周期清单 |
| 6. Engine 与 Automation | 已完成 | facade + 独立状态组件 | Engine/Automation 组件边界、race、生命周期与冻结规范测试均已通过 |
| 7. React Feature 化 | 已完成 | 页面、Hook、API、类型按领域拆分 | 领域 feature、行为测试、依赖门禁和构建门禁已完成 |
| 8. DB 与事务治理 | 进行中 | 窄接口、事务执行器、方言门禁 | 已有部分 repository；待完成禁止 `sql.Tx`/`db.*` 泄露、UoW 在基础设施适配器实现、批量读写和补偿记录 |
| 9. 架构门禁与兼容清理 | 进行中 | 允许依赖图、临时例外、兼容路径退场 | 当前门禁只覆盖低层反向依赖；待完成 Server/application 允许边、Port 类型扫描、旧 API 遥测与 Sunset 条件 |
| 10. 注释基线清零 | 进行中 | 全仓准确中文注释检查 | 已有字符级/AST 门禁；待完成模板短语黑名单、业务语义抽查、复杂度门禁和冻结文件精确例外 |

### 当前执行入口

- 当前阶段：审计重开后的过渡架构治理；不得使用“全部完成”表述。当前先执行 P0 敏感设置边界，再执行应用 Port、生命周期、补偿模型、批量同步和前端页面性能切片；每个 PR 必须同时更新完成证据和剩余风险；
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
- 已完成阶段 7 第九个 PR 切片“React Chat feature 化与会话消息行为边界”：按 `app/features/chat` 提取聊天 API、会话筛选/消息合并状态、账号/会话/消息分页 Hook；会话切换、联系人分页和消息加载支持取消与请求代次保护，文本/图片发送支持失败重试，WebSocket 和滚动语义保持不变；新增会话筛选、消息去重、分页过期响应、发送取消与 API 取消信号测试，保留旧页面入口和 API 路径；
- 已完成阶段 7 第十个 PR 切片“React AccountAutomation feature 化与任务设置行为边界”：按 `app/features/accountAutomation` 提取账号任务 API、默认设置与重复执行状态模型、任务设置 Hook；账号切换支持取消和请求代次保护，保存/立即执行支持重复提交阻断、失败重试和结果刷新；新增默认值、动作门禁、API 取消信号测试，保留旧弹窗入口和 API 路径；
- 阶段 7 下一 PR 切片为“React AccountList 子模块收口与页面组合边界”：收口账号编辑、密码登录、AI 设置和通知绑定子模块的共享状态，补充账号编辑过期响应与跨子模块刷新测试；
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
| 2026-08-15 | 完成阶段 7 第九个 PR 切片“React Chat feature 化与会话消息行为边界” | 按 `app/features/chat` 提取聊天 API、会话筛选/消息合并状态、账号/会话/消息分页 Hook；会话切换、联系人分页和消息加载支持取消与请求代次保护，文本/图片发送支持失败重试，WebSocket 和滚动语义保持不变；新增会话筛选、消息去重、分页过期响应、发送取消与 API 取消信号测试，保留旧页面入口和 API 路径 | 阶段 7：React AccountAutomation feature 化与任务设置行为边界 |
| 2026-08-15 | 完成阶段 7 第十个 PR 切片“React AccountAutomation feature 化与任务设置行为边界” | 按 `app/features/accountAutomation` 提取账号任务 API、默认设置与重复执行状态模型、任务设置 Hook；账号切换支持取消和请求代次保护，保存/立即执行支持重复提交阻断、失败重试和结果刷新；新增默认值、动作门禁、API 取消信号测试，保留旧弹窗入口和 API 路径 | 阶段 7：React AccountList 子模块收口与页面组合边界 |
| 2026-08-15 | 完成阶段 7 第十一个 PR 切片“React AccountList 子模块收口与页面组合边界” | 新增 `useAccountSubmodules`，集中管理账号编辑、长登录、通知绑定、AI 设置和密码登录状态；编辑弹窗仅保留页面组合与二维码/删除职责，子模块请求支持并行加载、取消和账号/代次隔离；新增跨子模块过期响应、密码登录取消信号和路由边界测试；前端类型检查、全量测试和构建门禁通过并合并为一个可回滚提交 | 阶段 7：React feature 依赖门禁与旧页面入口瘦身 |
| 2026-08-15 | 完成阶段 7 第十二个 PR 切片“React feature 依赖门禁与旧页面入口瘦身” | 新增 feature 依赖架构测试，禁止生产页面绕过 API 适配层直接使用共享网络客户端或 `fetch`；会话与健康检查纳入 `session/system` feature API，Sidebar 和 App 不再直接访问共享网络层；账号、卡密、订单和规则的旧 `components/*State` 转发入口及其测试迁移/删除，状态测试归属各自 feature；前端类型检查、184 个测试、注释门禁和构建门禁通过 | 阶段 8：DB 与事务治理第一批窄 repository 边界 |
| 2026-08-15 | 完成阶段 8 第一个 PR 切片“Chat 窄 repository 接口与服务依赖收口” | 新增 `chat.Repository`，聊天服务只持有会话、消息和账号归属所需的最小持久化接口；完整 `db.Store` 仅在构造适配器时出现，新增 `NewWithRepository` 和内存替身测试，保持聊天消息幂等、会话归属与订阅语义不变；Chat 全量测试、Go vet、lint、注释和全量门禁通过并合并为一个可回滚提交 | 阶段 8：通知与账号任务窄 repository 边界 |
| 2026-08-15 | 完成阶段 8 第二个 PR 切片“账号任务窄 repository 与凭证门禁收口” | 新增 `automation.AccountTaskRepository`，账号任务协调器只持有账号启停/暂停、任务设置/租约、运行凭证和 Cookie 更新所需的最小接口；自动化中心保留 Store 装配适配器，Session 指纹阻断、续期恢复、任务幂等和 Cookie 同步语义保持不变；Automation 全量测试、Go vet、lint、注释和全量门禁通过并合并为一个可回滚提交 | 阶段 8：通知与应用服务 repository 边界 |
| 2026-08-15 | 完成阶段 8 第三个 PR 切片“通知与通信应用服务 repository 边界” | 新增 `notify.Repository`，通知器仅持有渠道、SMTP 系统设置和 outbox 所需的最小接口，保留同步发送、异步租约、重试和测试发送语义；新增通信应用服务窄 repository，账号任务、通知渠道/绑定和聊天历史持久化不再直接访问完整 `db.Store`，实时账号、MTOP 和聊天服务仍由 Server 装配；通知/Server 定向测试、Go vet、lint、注释和全量门禁通过并合并为一个可回滚提交 | 阶段 8：剩余应用服务与事务 repository 边界 |
| 2026-08-15 | 完成阶段 8 第四个 PR 切片“分析应用服务只读 repository 边界” | 新增分析服务窄 repository，统一封装 Dashboard 固定统计、订单分析聚合、有效订单分页、卡密库存读取和数据库方言；应用服务不再持有完整 `Server` 或直接访问 `Store.Analytics/Cards`，查询阶段错误、金额表达式和响应聚合语义保持不变；Server 全量测试、Go vet、lint、注释和全量门禁通过并合并为一个可回滚提交 | 阶段 8：订单与发布应用服务 repository 边界 |
| 2026-08-15 | 完成阶段 8 第五个 PR 切片“订单应用服务 repository 与事务边界” | 新增 `orderRepository`，订单应用服务的订单/商品读写、用户归属、事务、凭证锁、续期 Cookie 和远端缺失清理均通过窄接口；平台 MTOP、运行时 Cookie 更新、自动化发货和通知编排仍由 Server 负责；订单列表、详情、导入、手动发货、单笔/批量刷新语义保持不变；Server 全量测试、Go vet、lint、注释和全量门禁通过并合并为一个可回滚提交 | 阶段 8：发布应用服务 repository 与 worker 事务边界 |
| 2026-08-15 | 完成阶段 8 第六个 PR 切片“发布应用服务 repository 边界” | 新增 `itemPublishRepository`，单商品发布、类目推荐、预检批次创建、批次查询/取消/删除/失败重试及凭证 Cookie 持久化不再直接访问完整 `db.Store`；批量 worker 的租约状态机和后台生命周期暂保持原有 Server 接线，下一片整体迁移；单商品/批量发布测试、Go vet、lint、注释和全量门禁通过并合并为一个可回滚提交 | 阶段 8：发布 worker 租约状态机 repository 边界 |
| 2026-08-15 | 完成阶段 8 第七个 PR 切片“发布 worker 租约状态机 repository 边界” | 发布恢复扫描、批次租约续期、明细抢占、远端发布检查点、结果落库、取消/中断收口和过期上传清理统一通过 `itemPublishRepository`；worker 不再直接访问 `PublishBatches`、Cookies 或 Items，平台 MTOP、自动化规则和生命周期等待语义保持不变；Server 全量测试、Server race、Go vet、lint、注释和全量门禁通过并合并为一个可回滚提交 | 阶段 8：账号登录应用服务 repository 边界 |
| 2026-08-15 | 完成阶段 8 第八个 PR 切片“账号登录凭证与 Token repository 边界” | 新增 `accountLoginRepository`，账号登录服务的凭证创建/更新、Cookie Jar 元数据写回、凭证锁、Token 清理和运行状态读取统一通过窄接口；保留登录服务对资料刷新、登录审计和运行时重启的既有编排，未改变扫码幂等、Cookie 合并与凭证锁时序；Server 全量测试、Server race、Go vet、lint、注释和全量门禁通过并合并为一个可回滚提交 | 阶段 8：登录审计与资料刷新共享 repository 边界 |
| 2026-08-15 | 完成阶段 8 第九个 PR 切片“登录审计与资料刷新共享 repository 边界” | 登录方式/状态、登录审计日志、账号资料更新、资料刷新凭证锁与平台视图读取统一通过 `accountLoginRepository`；运行时 Cookie 更新复用同一账号状态边界，移除无调用方的旧扁平 Cookie helper，保持 MTOP Cookie Jar 合并、资料刷新失败恢复和运行时唤醒语义不变；Server 全量测试、Server race、Go vet、lint、注释和全量门禁通过并合并为一个可回滚提交 | 阶段 8：数据库直连与事务边界最终审计 |
| 2026-08-15 | 完成阶段 8 第十个 PR 切片“统一事务执行 repository 边界” | Server 统一事务入口不再直接持有 `Store.DB`，改由 `transactionRepository` 负责创建、提交和回滚事务；事务初始化失败、业务错误和提交失败均保持回滚语义，Server 依赖装配与事务回归测试已覆盖；Server 全量测试、Server race、Go vet、lint、注释和全量门禁通过并合并为一个可回滚提交 | 阶段 9：架构依赖门禁与数据库直连最终审计 |
| 2026-08-15 | 完成阶段 9 第一个 PR 切片“Go/React 架构依赖门禁与数据库直连审计” | 新增 `tools/architecturecheck`，阻止 `internal/db`、`internal/xianyu`、`internal/browser` 反向依赖上层应用包，并阻止 Server 业务层直接创建事务；门禁接入 Makefile、CI 和 `make check`，同步更新阶段状态；架构检查、Go 全量测试、vet、lint、注释和前端注释门禁通过并合并为一个可回滚提交 | 阶段 9：兼容路由/字段调用方最终清理 |
| 2026-08-15 | 完成阶段 9 第二个 PR 切片“前端泛化兼容响应类型清理” | 移除未被调用方使用的 `ApiResponse` 和含义不清的 `LoginResponse`，认证初始化/登录统一使用 `SessionResponse`，登出、密码修改、账号、聊天、订单等操作统一使用 `OperationResponse`；未改变 HTTP 路径和业务状态字段；前端类型检查、184 个测试、注释检查和构建门禁通过并合并为一个可回滚提交 | 阶段 9：账号 `cookie`/`note` 兼容字段调用方清理 |
| 2026-08-15 | 完成阶段 9 第三个 PR 切片“账号 cookie/note 兼容字段调用方清理” | `AccountDetail` 移除历史 `cookie`/`note` 别名，账号详情归一化、搜索/展示、编辑回填和运行状态测试统一使用 `value`/`remark`；编辑表单的 `cookie` 保留为真实用户输入字段；前端类型检查、184 个测试、注释检查、仓库 `make check` 和生产构建通过，嵌入式 bundle 已同步并合并为一个可回滚提交 | 阶段 9：旧路由与剩余兼容字段最终审计 |
| 2026-08-15 | 完成阶段 9 第四个 PR 切片“旧路由与剩余兼容字段最终审计” | 新增 API 兼容边界清单，明确旧服务端入口的保留条件、复用 handler 约束和删除证据要求；新增 React 架构测试，禁止生产 API 适配层重新调用未版本化 `/api/...` 路径；服务端新旧入口契约测试继续覆盖兼容行为，前端 185 个测试、类型检查和注释门禁通过并合并为一个可回滚提交 | 阶段 10：注释基线清零与严格门禁 |
| 2026-08-15 | 完成阶段 10 第一个 PR 切片“前端共享 types.ts 注释基线清零” | 为分页、会话、账号、聊天、订单、卡券、商品、自动化规则、统计、设置、AI、默认回复和通知 DTO 的全部字段补齐准确中文注释，解释字段职责、敏感性、状态和时间/数量单位；`frontend/types.ts` 历史注释基线清零，前端注释门禁、类型检查、185 个测试和生产构建通过并合并为一个可回滚提交 | 阶段 10：前端 services/api.ts 注释基线清零 |
| 2026-08-15 | 完成阶段 10 第二个 PR 切片“前端 services/api.ts 注释基线清零” | 为 API 适配层全部导出函数、局部变量、请求参数和内联响应字段补齐中文注释，明确认证、账号、聊天、订单、商品、卡密、自动化、设置和通知接口的数据职责；增强注释检查器对调用参数、类型字面量和循环变量内联注释的识别能力，`services/api.ts` 注释基线清零；前端注释门禁、类型检查和 185 个测试通过并合并为一个可回滚提交 | 阶段 10：前端 components/hooks 注释基线按领域清零 |
| 2026-08-15 | 完成阶段 10 第三个 PR 切片“前端 components/hooks 注释基线按领域清零” | 为账号、聊天、仪表盘、设置、规则、商品、卡密、订单、通知和二维码等组件、Hook、状态模块及其测试补齐函数、变量、回调和字段中文注释；补强注释检查器对箭头函数主体内联注释的识别，保持路由源码断言和 JSX 行为不变；目标目录注释基线清零，前端注释门禁、类型检查、185 个测试和生产构建通过并合并为一个可回滚提交 | 阶段 10：Go 核心领域注释基线按领域清零 |
| 2026-08-15 | 完成阶段 10 第四个 PR 切片“Go internal/server 注释基线按领域清零” | 为 HTTP handlers、应用服务、事务边界、批量发布、登录/二维码、请求解析、测试辅助和错误处理代码补齐函数、变量、字段及常量中文注释；增强 Go 注释检查器对多行文档块与分组 const/var 注释的识别；`internal/server` 注释基线清零，Server 定向测试和 Go 注释门禁通过，合并为一个可回滚提交 | 阶段 10：继续清理 Go internal/db 与 internal/xianyu 领域注释基线 |
| 2026-08-15 | 完成阶段 10 第五个 PR 切片“Go internal/db 注释基线按领域清零” | 为数据库 Store、repository、迁移/方言、多数据库适配、凭证与订单数据访问及其测试补齐函数、变量、字段和常量中文注释；`internal/db` 注释基线清零；数据库定向测试、SQLite 迁移与 CRUD 验证、Go 注释门禁通过，合并为一个可回滚提交 | 阶段 10：继续清理 Go internal/xianyu 领域注释基线 |
| 2026-08-15 | 完成阶段 10 第六个 PR 切片“Go internal/xianyu 注释基线按领域清零” | 为 MTOP 请求、协议编解码与签名、二维码登录、续期、WebSocket、Cookie 刷新和用户代理代码及其测试补齐函数、变量、字段和常量中文注释；`internal/xianyu` 及子包注释基线清零；平台协议定向测试和 Go 注释门禁通过，合并为一个可回滚提交 | 阶段 10：继续清理 Go internal/engine、internal/automation 与其他领域注释基线 |
| 2026-08-15 | 完成阶段 10 第七个 PR 切片“Go internal/engine 注释基线按领域清零” | 为账号运行时、连接循环、凭证作用域、消息分发、回复策略、AI、令牌缓存和生命周期测试补齐函数、变量、字段及常量中文注释；`internal/engine` 注释基线清零；Engine 定向测试、`go test -race ./internal/engine`、全量 Go/React 门禁通过，合并为一个可回滚提交 | 阶段 10：继续清理 Go internal/automation、internal/account、internal/adapter、internal/renewal、internal/notify、internal/chat 等领域注释基线 |
| 2026-08-15 | 完成阶段 10 第八个 PR 切片“Go internal/automation 注释基线按领域清零” | 为自动化中心、账号任务、动作执行器、事件流水线、运行协调器、调度器、通知器及其测试补齐函数、变量、字段和常量中文注释；`internal/automation` 注释基线清零；Automation 定向测试、`go test -race ./internal/automation`、全量 Go/React 门禁通过，合并为一个可回滚提交 | 阶段 10：继续清理 Go internal/account、internal/adapter、internal/renewal、internal/notify、internal/chat 等领域注释基线 |
| 2026-08-15 | 完成阶段 10 第九个 PR 切片“Go internal/account 注释基线按领域清零” | 为账号管理器、凭证作用域、运行时启动/停止和生命周期测试补齐函数、变量、字段及常量中文注释；`internal/account` 注释基线清零；Account 定向测试、`go test -race ./internal/account`、全量 Go/React 门禁通过，合并为一个可回滚提交 | 阶段 10：继续清理 Go internal/adapter、internal/renewal、internal/notify、internal/chat 等领域注释基线 |
| 2026-08-15 | 完成阶段 10 第十个 PR 切片“Go internal/adapter 注释基线按领域清零” | 为 adapter 装配、平台运行视图、远端 Token CAPTCHA 流程及其测试补齐函数、变量、字段和常量中文注释；`internal/adapter` 注释基线清零；Adapter 定向测试和全量 Go/React 门禁通过，合并为一个可回滚提交 | 阶段 10：继续清理 Go internal/renewal、internal/notify、internal/chat 等领域注释基线 |
| 2026-08-15 | 完成阶段 10 第十一个 PR 切片“Go internal/renewal 注释基线按领域清零” | 为续期冷却、调度器、平台运行凭证范围和相关测试补齐函数、变量、字段及常量中文注释；`internal/renewal` 注释基线清零；Renewal 定向测试、`go test -race ./internal/renewal`、全量 Go/React 门禁通过，合并为一个可回滚提交 | 阶段 10：继续清理 Go internal/notify、internal/chat 等领域注释基线 |
| 2026-08-15 | 完成阶段 10 第十二个 PR 切片“Go internal/notify 注释基线按领域清零” | 为通知器、渠道与账号绑定、通知 outbox、SMTP 守卫及其测试补齐函数、变量、字段和常量中文注释；`internal/notify` 注释基线清零；Notify 定向测试、`go test -race ./internal/notify`、全量 Go/React 门禁通过，合并为一个可回滚提交 | 阶段 10：继续清理 Go internal/chat 领域注释基线 |
| 2026-08-15 | 完成阶段 10 第十三个 PR 切片“Go internal/chat 注释基线按领域清零” | 为聊天 repository、消息服务、账号订阅、已读状态、事件广播及其测试补齐函数、变量、字段和常量中文注释；`internal/chat` 注释基线清零；Chat 定向测试、`go test -race ./internal/chat`、全量 Go/React 门禁通过，合并为一个可回滚提交 | 阶段 10：进入非冻结 browser、cmd 与 tools 注释基线清理 |
| 2026-08-15 | 完成阶段 10 第十四个 PR 切片“Go 非冻结 internal/browser 注释基线按领域清零” | 为 Cookie、订单、密码登录、二维码刷新、生命周期、用户数据目录和浏览器辅助测试补齐函数、变量、字段及常量中文注释；严格跳过冻结的 slider/CAPTCHA 实现与测试；非冻结 browser 注释基线清零，冻结文件保留 664 项原有基线；Browser 定向测试和全量 Go/React 门禁通过，合并为一个可回滚提交 | 阶段 10：继续清理 cmd 与 tools 注释基线，并保留冻结基线边界 |
| 2026-08-15 | 完成阶段 10 第十五个 PR 切片“Go cmd 与 tools 注释基线按领域清零” | 为 server、tray、dbseed、dbverify、init-admin、browser-install、spike 命令及 iconconv 工具补齐函数、变量、字段和常量中文注释；`cmd` 与 `tools` 注释基线清零；命令/工具定向测试和全量 Go/React 门禁通过，合并为一个可回滚提交 | 阶段 10：继续清理剩余基础内部包并保留冻结 browser 基线 |
| 2026-08-15 | 完成阶段 10 第十六个 PR 切片“Go 基础内部包注释基线按领域清零” | 为 `internal/auth`、`internal/netguard`、`internal/logging`、`internal/version`、`internal/logsafe` 和 `internal/webui` 补齐函数、变量、字段及常量中文注释；这些基础包注释基线清零；定向测试和全量 Go/React 门禁通过，合并为一个可回滚提交；当前 Go 基线仅保留冻结 browser CAPTCHA 文件 | 阶段 10：冻结基线边界审计、删除可清理基线并完成最终验收 |
| 2026-08-15 | 完成阶段 10 最终 PR 切片“冻结边界固化与全仓零基线验收” | Go/前端注释检查器显式识别冻结 CAPTCHA 文件并支持无基线严格模式；前端剩余注释债务清零；删除 `.commentlint/go-baseline.json` 与 `.commentlint/frontend-baseline.json`；无基线 Go/React 注释门禁、全量测试、类型检查和生产构建通过，冻结实现保持原样 | 阶段 10 完成：进入全仓重构计划最终审计 |
| 2026-08-15 | 完成测试覆盖率提升切片“确定性 Browser/命令入口覆盖与前端覆盖率采集” | 新增 Cookie 快照、浏览器池/持久化上下文、登录页、订单 DOM、滑块页面辅助、命令行入口测试；修复 Playwright 数值类型导致的登录成功误判；新增 Go `cover-browser`、前端 V8 `test:coverage` 与 Makefile 入口；普通 Go 全量覆盖率 70.9%，本地 Chromium 覆盖 Browser 约 60.0%，前端当前全源覆盖率为语句 19.22%；真实账号/外部平台流程仍明确不触网 | 下一步：按覆盖率报告继续补齐前端页面行为与 Browser 非账号错误分支，不降低门禁或伪造 100% |
| 2026-08-15 | 完成测试覆盖率提升切片“前端规则与领域状态边界补测” | 新增规则工具、账号运行态、批量任务、仪表盘、通知和聊天状态的确定性边界测试，覆盖默认值、空值、状态映射、过期响应、错误消息、SMTP 校验和时间格式化；前端 196 个测试、类型检查、注释门禁通过，全源语句覆盖率提升至 22.02%；页面组件与真实平台账号流程仍按覆盖率报告列为后续切片，不降低门禁或伪造 100% | 下一步：补齐 React 页面/Hook 行为测试，并继续覆盖 Browser 非账号错误分支 |
| 2026-08-15 | 完成测试覆盖率提升切片“React 纯展示组件静态渲染覆盖” | 新增无需浏览器环境的静态渲染测试，覆盖卡密图标类型、批量阶段高亮、通知事件选中状态和订单筛选栏结构；前端 200 个测试、类型检查、注释门禁通过，全源语句覆盖率提升至 22.53%；交互回调和页面 Hook 仍需后续使用可控测试替身继续补齐，不降低门禁或伪造 100% | 下一步：补齐 React 页面/Hook 行为测试，并继续覆盖 Browser 非账号错误分支 |
| 2026-08-15 | 完成测试覆盖率提升切片“React 通知与自动化异常组件分支覆盖” | 扩展静态渲染测试覆盖通知渠道空状态/停用/测试中状态，以及自动化运行异常和延迟任务的人工处理按钮；前端 202 个测试、类型检查、注释门禁通过，全源语句覆盖率提升至 23.00%；复杂表单交互与页面 Hook 仍需后续使用可控测试替身继续补齐，不降低门禁或伪造 100% | 下一步：补齐 React 页面/Hook 行为测试，并继续覆盖 Browser 非账号错误分支 |
| 2026-08-15 | 完成测试覆盖率提升切片“React SMTP 设置组件展示分支覆盖” | 新增 SMTP 设置面板静态渲染测试，覆盖密码显隐、保存中禁用状态、TLS/SSL 配置及发件人字段回填；前端 203 个测试、类型检查、注释门禁通过，全源语句覆盖率提升至 23.18%；表单事件回调和页面 Hook 仍需后续使用可控测试替身继续补齐，不降低门禁或伪造 100% | 下一步：补齐 React 页面/Hook 行为测试，并继续覆盖 Browser 非账号错误分支 |
| 2026-08-15 | 完成测试覆盖率提升切片“React Hook 可控运行时与请求分支覆盖” | 引入 `@testing-library/react` 与 `jsdom` 测试运行时；新增账号任务 Hook 的加载、保存、执行、失败重试和禁用账号门禁测试；新增仪表盘 Hook 的并行加载、刷新、概览失败和非法日期范围测试；前端 209 个测试、类型检查、注释门禁通过，全源语句覆盖率提升至 26.45%；真实账号、外部平台和未注入的生产页面组合仍列为后续切片，不降低门禁或伪造 100% | 下一步：继续补齐 Accounts/Cards/Chat/Items/Notifications/Orders/Settings/Rules Hook 与页面行为测试 |
| 2026-08-15 | 完成测试覆盖率提升切片“React Settings Hook 请求与凭据行为覆盖” | 新增系统设置 Hook 的成功加载/保存、模型发现失败、凭据前端校验、后端拒绝和初始读取失败测试；前端 213 个测试、类型检查、注释门禁通过，全源语句覆盖率提升至 28.96%；真实重新登录和浏览器页面重载仍只验证可控响应与调度，不伪造真实会话 | 下一步：继续补齐 Accounts/Cards/Chat/Items/Notifications/Orders/Rules Hook 与页面行为测试 |
| 2026-08-15 | 完成测试覆盖率提升切片“React Orders Hook 查询与导入行为覆盖” | 新增订单查询 Hook 的分页加载、辅助数据映射、账号/商品名称解析测试；新增订单导入 Hook 的成功刷新关闭、文件格式校验和服务失败状态测试；前端 216 个测试、类型检查、注释门禁通过，全源语句覆盖率提升至 31.69%；真实订单平台数据仍不触网，仅验证可控接口契约与状态机 | 下一步：继续补齐 Accounts/Cards/Chat/Notifications/Rules Hook 与页面行为测试 |
| 2026-08-15 | 完成测试覆盖率提升切片“React Notifications Hook 渠道与 SMTP 行为覆盖” | 新增通知 Hook 的管理员/普通用户加载、渠道新建、启用切换、测试通知、SMTP 保存、表单校验、删除和失败提示测试；前端 219 个测试、类型检查、注释门禁通过，全源语句覆盖率提升至 34.81%；真实推送服务和账号绑定数据仍不触网，仅验证可控接口契约与状态机 | 下一步：继续补齐 Accounts/Cards/Chat/Rules Hook 与页面行为测试 |
| 2026-08-15 | 完成测试覆盖率提升切片“React Accounts Hook 账号与运行状态覆盖” | 新增账号数据 Hook 的账号详情/AI 配置并行加载、AI 失败隔离、账号详情失败和运行状态轮询测试；前端 221 个测试、类型检查、注释门禁通过，全源语句覆盖率提升至 36.02%；真实账号凭证、远端资料和平台运行状态仍不触网，仅验证可控接口契约与生命周期清理 | 下一步：继续补齐 Cards/Chat/Items/Rules Hook 与页面行为测试 |
| 2026-08-15 | 完成测试覆盖率提升切片“React Cards Hook 库存与批量操作覆盖” | 新增卡密库存 Hook 的成功/失败加载测试；新增批量操作 Hook 的追加预览、追加成功、批量创建成功和追加失败测试；前端 223 个测试、类型检查、注释门禁通过，全源语句覆盖率提升至 38.19%；真实卡密库存和外部 API 卡密仍不触网，仅验证可控接口契约与状态机 | 下一步：继续补齐 Chat/Items/Rules Hook 与页面行为测试 |
| 2026-08-15 | 完成测试覆盖率提升切片“React Chat Hook 加载、分页与发送覆盖” | 新增聊天 Hook 的账号/运行状态加载、会话与消息分页、已读标记、文字/图片发送、发送失败重试和 WebSocket 清理测试；前端 225 个测试、类型检查、注释门禁通过，全源语句覆盖率提升至 44.11%；真实聊天账号、外部平台消息和 WebSocket 服务仍不触网，仅验证可控接口契约与状态机 | 下一步：继续补齐 Items/Rules Hook、账号子模块 Hook 与页面行为测试 |
| 2026-08-15 | 完成测试覆盖率提升切片“React Items 批量发布 Hook 行为覆盖” | 新增批量发布 Hook 的任务恢复、类目推荐、文件预检、任务启动、取消、最近结果、失败重试、关闭清理及表单守卫测试；修复批量状态变化后重试回调捕获旧状态的依赖问题；前端 227 个测试、类型检查、注释门禁通过，全源语句覆盖率提升至 46.88%；真实商品文件、账号凭证和外部发布平台仍不触网，仅验证可控接口契约与状态机 | 下一步：继续补齐 Rules Hook、账号子模块 Hook 与页面行为测试 |
| 2026-08-15 | 完成测试覆盖率提升切片“React Rules 数据 Hook 页签与异常隔离覆盖” | 新增规则数据 Hook 的参考数据并行加载、自动化规则分页/筛选、服务端页码修正、异常列表失败隔离、关键词规则、默认回复和无账号守卫测试；前端 229 个测试、类型检查、注释门禁通过，全源语句覆盖率提升至 48.38%；真实账号规则和外部平台数据仍不触网，仅验证可控接口契约、请求代次和页签状态机 | 下一步：继续补齐账号子模块 Hook 与页面行为测试 |
| 2026-08-15 | 完成测试覆盖率提升切片“React Accounts 子模块编辑、AI 与密码登录覆盖” | 新增账号子模块 Hook 的编辑弹窗初始化、通知渠道加载/切换、长期登录保存、AI 设置保存、暂停重启、密码登录成功/取消以及绑定/长登录/AI 请求失败隔离测试；前端 232 个测试、类型检查、注释门禁通过，全源语句覆盖率提升至 52.59%；真实账号凭证、密码和平台登录流程仍不触网，仅验证可控接口契约、请求取消和生命周期状态机 | 下一步：继续补齐页面组件行为与剩余非账号外部错误分支 |
| 2026-08-15 | 调整测试覆盖率目标为业务代码边界 | 根据最新要求移除本轮新增的纯 UI 页面组件测试，不再把 Sidebar、页面空状态、页面结构和展示分支作为覆盖率目标；保留账号、聊天、商品批量、通知、订单、设置、规则等 Hook、状态机、请求编排和领域工具测试；后续覆盖率统计以业务模块为主，真实账号/外部平台流程仍仅在确实无法确定性模拟时跳过 | 下一步：继续补齐业务 Hook、请求编排与领域状态的未覆盖分支 |
| 2026-08-15 | 完成业务测试覆盖切片“AMap 地点查询适配边界” | 新增高德地点查询的无效坐标、无数据、失败响应、POI 字段校验和结果映射测试；仅使用浏览器全局与 PlaceSearch 可控替身，不触发真实地图网络请求；前端测试、类型和注释门禁通过，纯 UI 组件不纳入覆盖率目标 | 下一步：补齐请求层与业务 Hook 的未覆盖错误分支 |
| 2026-08-15 | 完成业务测试覆盖切片“AMap 脚本加载错误边界” | 新增高德脚本首次注入、已有脚本节点、加载完成但对象缺失、脚本错误和超时测试；通过 jsdom 与动态模块隔离验证加载器生命周期，不访问真实地图网络；前端测试、类型和注释门禁通过 | 下一步：补齐业务 Hook 的未覆盖错误分支 |
| 2026-08-15 | 完成业务测试覆盖切片“请求层方法与取消/错误边界” | 新增 PUT/DELETE 请求、纯文本错误、损坏 JSON 错误体、外部 AbortSignal 及上传取消测试；验证统一错误消息、认证失败通知和超时/取消区分；前端请求测试、类型和注释门禁通过，纯 UI 组件不纳入覆盖率目标 | 下一步：补齐业务 Hook 的未覆盖错误分支 |
| 2026-08-15 | 完成业务测试覆盖切片“请求层网络异常与上传载荷” | 补充普通请求/上传请求网络异常透传、上传失败响应和原始错误载荷保留测试；覆盖请求层非业务网络分支，不依赖真实服务；前端请求测试、类型和注释门禁通过 | 下一步：补齐业务 Hook 的未覆盖错误分支 |
| 2026-08-15 | 完成业务测试覆盖切片“账号自动化 Hook 失败与重试” | 补充任务设置读取失败、任务执行失败和失败重试测试，覆盖业务错误提示、重试动作和状态清理；不触发真实账号任务；前端 Hook 测试、类型和注释门禁通过 | 下一步：补齐 Accounts/Items/Notifications 等 Hook 的未覆盖错误分支 |
| 2026-08-15 | 完成业务测试覆盖切片“商品批量发布 Hook 异常与轮询” | 补充类目推荐、批量预检、启动、取消、重试、最近结果和预检清理失败测试，并覆盖轮询完成后刷新商品/发货规则列表；使用 API 替身验证状态机，不触发真实商品文件或外部平台；前端 Hook 测试、类型和注释门禁通过 | 下一步：补齐 Accounts 子模块与 Notifications Hook 的未覆盖错误分支 |
| 2026-08-15 | 完成业务测试覆盖切片“账号子模块保存与密码登录异常” | 补充长期登录、AI、账号编辑、暂停保存失败，以及密码登录启动失败和状态查询失败测试；覆盖错误提示、状态收口和重试前置条件，不触发真实账号凭证或平台登录；前端 Hook 测试、类型和注释门禁通过 | 下一步：补齐 Notifications/Chat/Settings 等 Hook 的未覆盖错误分支 |
| 2026-08-15 | 完成业务测试覆盖切片“Notifications Hook 渠道与 SMTP 错误” | 补充通知渠道加载、编辑保存、启用切换和系统 SMTP 保存失败测试；验证错误提示、请求状态收口和管理员边界，不触发真实推送服务或邮件服务器；前端 Hook 测试、类型和注释门禁通过 | 下一步：补齐 Chat/Settings/Orders 等 Hook 的未覆盖错误分支 |
| 2026-08-15 | 完成业务测试覆盖切片“Chat Hook 实时连接与请求错误” | 新增 WebSocket 开关/消息/非法帧处理、联系人刷新、消息加载、历史分页和图片发送失败重试测试；仅使用 WebSocket 与 API 替身验证聊天状态机，不触发真实账号会话或平台消息；前端 Hook 测试、类型和注释门禁通过 | 下一步：补齐 Settings/Orders/Cards 等 Hook 的未覆盖错误分支 |
| 2026-08-15 | 完成业务测试覆盖切片“Orders Hook 部分失败导入与查询边界” | 补充订单账号回退名称、部分失败导入结果、导入重试、弹窗关闭清理测试；验证导入状态机和分页查询派生逻辑，不依赖真实订单文件或平台数据；前端 Hook 测试、类型和注释门禁通过 | 下一步：补齐 Settings/Cards/Rules 等 Hook 的未覆盖错误分支 |
| 2026-08-15 | 完成业务测试覆盖切片“Settings Hook 保存与凭据异常” | 补充系统配置保存失败、登录凭据网络异常、成功提示和重载调度测试；验证错误状态、成功消息和表单请求边界，不触发真实会话或账号凭据；前端 Hook 测试、类型和注释门禁通过 | 下一步：补齐 Cards/Rules 与剩余可控业务分支 |
| 2026-08-15 | 完成业务测试覆盖切片“Cards Hook 批量创建与追加重试” | 补充批量创建失败、追加失败重试和切换目标后阻止旧任务重试测试；验证库存加载、批量状态和目标隔离，不依赖真实卡密库存或外部服务；前端 Hook 测试、类型和注释门禁通过 | 下一步：补齐 Rules 与剩余可控业务分支 |
| 2026-08-15 | 完成业务测试覆盖切片“Rules Hook 参考数据与异常筛选” | 补充空账号参考数据默认选择、异常面板空筛选和延迟任务账号过滤测试；验证规则页数据边界，不触发真实规则或账号数据；前端 Hook/领域状态测试、类型和注释门禁通过 | 下一步：继续补齐剩余可控业务分支并分类外部依赖分支 |
| 2026-08-15 | 完成业务测试覆盖切片“Cards/Chat Hook 关闭、重试与滚动边界” | 补充卡密批量创建重试/关闭、聊天联系人分页失败、滚动距离策略和实时发送边界测试；验证请求清理与交互状态机，不触发真实卡密、账号或消息服务；前端 Hook 测试、类型和注释门禁通过 | 下一步：继续补齐剩余可控业务分支并分类外部依赖分支 |
| 2026-08-15 | 完成业务测试覆盖切片“通知/日期/SMTP 领域工具边界” | 补充邮件通知自定义 SMTP 开关解析、默认字段回填、日期自定义/昨天范围和 SMTP 布尔值归一化测试；覆盖纯业务工具的默认与异常分支，不依赖外部服务或 UI；前端测试、类型和注释门禁通过 | 下一步：继续补齐剩余可控业务分支并分类外部依赖分支 |
| 2026-08-15 | 完成业务测试覆盖切片“Accounts Hook 运行状态轮询错误” | 补充账号运行状态轮询失败测试，验证账号列表保留、错误记录和轮询生命周期清理；不触发真实账号运行时；前端 Hook 测试、类型和注释门禁通过 | 下一步：继续补齐剩余可控业务分支并分类外部依赖分支 |
| 2026-08-15 | 完成业务测试覆盖切片“账号子模块通知与密码登录中间状态” | 补充通知渠道独立失败、AI 弹窗关闭、密码登录处理中和编辑弹窗关闭取消测试；覆盖子模块请求隔离与生命周期清理，不触发真实账号凭证或平台登录；前端 Hook 测试、类型和注释门禁通过 | 下一步：继续补齐剩余可控业务分支并分类外部依赖分支 |
| 2026-08-15 | 完成阶段性业务覆盖率审计 | 前端全量测试 50 个文件、268 个用例通过；业务代码 V8 覆盖率达到语句 92.47%、分支 73.48%、函数 93.41%、行 96.46%；纯 UI 组件按用户要求排除，剩余未覆盖项集中在 API 适配器未调用接口、请求取消/过期保护和真实平台生命周期分支，下一轮继续按可控性补测并保留外部依赖跳过说明 | 下一步：继续补齐剩余可控业务分支，最终形成业务覆盖率验收清单 |
| 2026-08-15 | 完成业务测试覆盖切片“通知 API 序列化与绑定响应” | 补充通知渠道更新配置/事件序列化和消息通知数组展开、非法绑定值忽略测试；覆盖 API 适配层确定性响应归一化，不触发推送服务；前端 API 测试、类型和注释门禁通过 | 下一步：继续补齐剩余 API 适配器与请求取消分支 |
| 2026-08-15 | 完成业务测试覆盖切片“商品发布与通知事件 API 归一化” | 补充商品发布 multipart 图片/地点字段序列化，以及通知事件 JSON/分隔符兼容解析测试；覆盖 API 适配层剩余确定性分支，不触发真实商品发布或推送服务；前端 API 测试、类型和注释门禁通过 | 下一步：继续补齐剩余 API 适配器与请求取消分支 |
| 2026-08-15 | 完成业务测试覆盖切片“AMap 地点映射与并发加载边界” | 补充 POI 缺失标识、成功空结果和并发查询共享脚本 Promise 测试；覆盖地图适配器剩余确定性分支，不访问真实地图网络；前端测试、类型和注释门禁通过 | 下一步：继续补齐剩余 API 适配器与请求取消分支 |
| 2026-08-15 | 完成业务测试覆盖切片“请求层成功文本响应” | 补充普通请求成功返回非 JSON 文本的确定性测试，覆盖共享请求层文本分支，不依赖真实服务；前端请求测试、类型和注释门禁通过 | 下一步：继续补齐剩余 API 适配器与请求取消分支 |
| 2026-08-15 | 完成业务测试覆盖切片“Dashboard Hook 经营数据错误” | 补充趋势/有效订单并行请求失败和范围错误状态测试；验证错误信息收口，不触发真实统计或订单数据；前端 Hook 测试、类型和注释门禁通过 | 下一步：继续补齐剩余 API 适配器与请求取消分支 |
| 2026-08-15 | 完成业务测试覆盖切片“API 头像、订单分析、批量预检与卡密归一化” | 补充头像 URL 缓存参数/非法地址、数字天数分析、批量预检可选字段、无账号规则守卫和卡密 JSON 配置归一化测试；覆盖 API 适配层确定性分支，不依赖真实账号或平台数据；前端 API 测试、类型和注释门禁通过 | 下一步：继续补齐剩余 API 适配器与请求取消分支 |
| 2026-08-15 | 完成业务测试覆盖切片“订单筛选、通知数组与默认回复归一化” | 补充订单账号/状态筛选参数、通知事件数组格式和默认回复空字段保存载荷测试；覆盖 API 适配层剩余确定性分支，不依赖真实订单或推送服务；前端 API 测试、类型和注释门禁通过 | 下一步：继续补齐剩余 API 适配器与请求取消分支 |
| 2026-08-15 | 完成业务测试覆盖切片“请求层认证并发与预取消边界” | 补充并发认证失败去重、上传认证失败、非 JSON 上传错误读取失败和请求开始前已取消测试；验证统一请求层的认证事件、状态码兜底和取消错误收口，不依赖真实服务；前端请求测试、类型和注释门禁通过 | 下一步：继续补齐 Chat/Notifications/Orders 等 Hook 的可控生命周期分支 |
| 2026-08-15 | 完成业务测试覆盖切片“Chat Hook 初始化、轮询与实时生命周期” | 补充聊天初始化失败、运行状态轮询失败重调度、历史消息成功滚动恢复、未知实时会话刷新、空消息帧和 WebSocket 错误关闭测试；仅使用 API、定时器、WebSocket 与 DOM 替身，不触发真实账号会话；前端 Chat Hook 测试、类型和注释门禁通过 | 下一步：继续补齐 Notifications/Orders/Items 等 Hook 的可控生命周期分支 |
| 2026-08-15 | 完成业务测试覆盖切片“Notifications Hook 定时器、SMTP 与删除边界” | 补充 SMTP 初始加载失败、提示自动消失、弹窗关闭、用户拒绝删除和删除请求失败测试；验证通知状态清理和错误收口，不触发真实推送或邮件服务；前端 Notifications Hook 测试、类型和注释门禁通过 | 下一步：继续补齐 Orders/Items/Settings 等 Hook 的可控生命周期分支 |
| 2026-08-15 | 完成业务测试覆盖切片“Orders Hook 查询防抖与辅助错误” | 补充订单主查询失败、搜索文本防抖标准化和账号/商品辅助数据失败测试；验证订单分页状态、请求参数和辅助数据隔离，不依赖真实订单文件或平台数据；前端 Orders Hook 测试、类型和注释门禁通过 | 下一步：继续补齐 Items/Settings 等 Hook 的可控生命周期分支 |
| 2026-08-15 | 完成业务测试覆盖切片“Items Hook 批量任务守卫与轮询错误” | 补充无最近任务、无预检、无有效行、用户拒绝取消、不可重试任务和轮询失败测试；验证批量发布状态机前置条件与错误收口，不触发真实商品文件或外部发布平台；前端 Items Hook 测试、类型和注释门禁通过 | 下一步：继续补齐 Settings/Accounts 等 Hook 的可控生命周期分支 |
| 2026-08-15 | 完成阶段性业务覆盖收口切片“请求、账户、卡密、订单与设置边界” | 补充上传超时、批量发布空输入、卡密批量操作空守卫、订单无文件导入、设置外部点击与重载调度、账户绑定移除/AI 默认值/密码终态，以及仪表盘和聊天领域排序时间边界；前端全量 50 个文件、301 个用例通过，语句覆盖率提升至 96.58%、函数 97.25%、行 99.63%；纯 UI 和真实账号/平台流程仍按边界排除 | 下一步：继续处理剩余可控的过期响应、取消清理和领域分支，形成最终业务覆盖率验收清单 |
| 2026-08-15 | 完成业务测试覆盖切片“账号任务、账号轮询与聊天实时边界” | 补充账号任务保存并发门禁、账号运行状态下一轮定时轮询、通知绑定移除/暂停状态更新、密码登录处理中轮询与关闭清理、聊天空发送/滚动守卫/未知会话排序，以及规则分页兼容入口测试；前端注释门禁、定向测试和类型检查通过，阶段覆盖率提升至语句 97.49%、函数 99.08%、行 99.86%；过期响应保护仍按真实取消时序分类保留 | 下一步：继续评估通知、卡密、设置与请求层过期响应是否能在不引入不稳定时序的前提下补测 |
| 2026-08-15 | 完成业务测试覆盖切片“通知与批量任务过期响应并发” | 补充通知渠道刷新丢弃旧响应、批量轮询防并发和关闭后旧响应失效，以及普通请求纯文本错误读取失败测试；验证请求代次和轮询保护，不触发真实推送、商品或平台服务；前端注释门禁、定向测试和类型检查通过 | 下一步：执行全量回归、覆盖率终审并记录仍需真实取消时序或不可达代码分支 |
| 2026-08-15 | 完成业务测试覆盖切片“卡密、仪表盘、设置与订单过期响应” | 补充卡密库存刷新、仪表盘经营数据、系统设置和订单查询的重复请求旧响应隔离测试；前端全量覆盖率达到语句 98.00%、函数 100%、行 99.86%，分支 79.82%；剩余未覆盖主要为取消/卸载时序保护、订单相似标题不可达分支和防御性条件组合 | 下一步：执行最终全量回归并形成业务覆盖率验收与跳过项清单 |
| 2026-08-15 | 完成阶段性业务覆盖率终审 | 前端 50 个测试文件、312 个用例通过；业务代码覆盖率为语句 98.00%（2500/2551）、函数 100%（547/547）、行 99.86%（2178/2181）、分支 79.82%（1385/1735）；所有业务函数均有测试入口。剩余分支集中在请求取消/卸载后旧响应保护的极端时序、真实账号或外部平台会话生命周期，以及订单相似标题判断中被前置标题返回遮蔽的不可达路径；纯 UI 组件按约束排除 | 下一步：后续新增业务函数必须同步测试与中文注释，若改变取消时序或外部依赖契约需重新审计覆盖率 |

## 11. 审计重开后的执行切片

以下切片是当前唯一有效的后续入口。每个切片完成后必须：更新本表、补齐针对性测试、运行适用门禁，并创建一个中文提交；未满足“完成条件”不得标记为已完成。

| 优先级 | PR 切片 | 主要范围 | 完成条件 |
| --- | --- | --- | --- |
| P0 | 敏感系统设置与运维输出脱敏 | `SystemSettings.Redacted`、敏感值 retain/replace/clear、管理端响应、dbverify 指纹输出、前端敏感字段状态 | HTTP 响应和 React 状态不出现秘密；空值显式清除、缺省值保留；Go/React 回归覆盖；三库迁移测试通过；显式 `values` 不得携带敏感键 |
| P1 | 应用 Port 与架构允许边 | `internal/application/orders`、订单命令/结果 DTO、基础设施适配器、`architecturecheck` | Server 不再创建或持有订单应用实现；Port 不出现 `sql.Tx`、`db.*`、HTTP 类型；架构门禁能主动拒绝违规 |
| P1 | 凭证快照与账号操作协调器 | 凭证版本、短锁快照、外部调用重试/合并、每账号有界协调、锁指标和锁 Map 回收 | 慢速外部调用不持有共享凭证锁；并发更新有版本冲突测试；锁等待和队列可观测 |
| P1 | 生命周期与删除 fencing | `Server.Stop(ctx)`、`Manager.Stop(ctx)`、StopAll 并发收束、账号删除任务登记和状态 fencing | 所有等待受关闭 Context 限制；删除接口不会产生游离 Stop goroutine；超时有明确错误和可追踪任务 |
| P1 | 外部动作补偿与 reconciliation | 手动发货、本地订单状态、自动化准备检查点、reconciliation/outbox 表和重试 worker | 外部成功/本地失败统一返回三态结果；不会重复执行不可逆动作；补偿记录可恢复、可审计 |
| P2 | 商品/订单批量同步 | 用户范围 Join、游标分页、批量 Upsert、规格探测缓存和受限并发 | 消除逐账号/逐订单 N+1；错误不再被忽略；SQLite/MySQL/Postgres 查询计划和大数据量测试有证据 |
| P2 | HTTP 契约与兼容退场 | 批量 DTO、错误字段统一、旧路由使用量、Deprecation/Sunset 和删除版本 | 新增接口不使用匿名动态结果；旧入口有遥测、期限和兼容测试 |
| P2 | React 页面边界与初始包体 | 页面迁移到 feature、路由懒加载、Provider/路由壳、bundle budget | App 不同步加载全部页面；首屏预算门禁通过；遵循并行请求、动态导入和避免重复渲染规范 |
| P2 | 复杂度与注释真实性 | 长函数拆分、复杂度门禁、模板注释黑名单、冻结文件精确例外 | 新增/修改函数和变量有准确中文语义注释；模板注释不得通过门禁；冻结 CAPTCHA 仅按文件例外 |

### 11.1 当前切片完成记录

| 日期 | 切片 | 状态 | 证据 |
| --- | --- | --- | --- |
| 2026-08-15 | 审计重开与完成条件纠偏 | 已完成 | 已将阶段 2、4、5、8、9、10 从“已完成”改为“进行中”，建立九个可回滚 PR 切片，并明确“测试通过不等于架构完成”的新验收规则 |
| 2026-08-15 | P0 敏感系统设置与运维输出脱敏 | 已完成 | 新增 `SystemSettings.Redacted`；管理端不再返回 `ai_api_key`、SMTP 密码和验证码密钥明文；敏感设置缺省保留、显式空值清除；dbverify 改为长度+指纹；Go/React 回归、类型检查、前端构建和 `make check` 通过。阶段 2 仍保持“进行中”，因为凭证锁审计、访问审计和三库回归尚未全部完成 |
| 2026-08-15 | P1 订单读模型 Port 与架构依赖门禁 | 已完成 | 新增 `internal/application/orders` 纯业务 `OrderRow`/`ListFilter`/`Reader`；订单列表适配器负责 `db.*` 到应用模型的转换；架构门禁统一模块路径、禁止应用层依赖 `db/server/xianyu/browser/sql/http`，并对 Server 新增低层依赖要求临时白名单；新增门禁测试，`make check` 通过。订单写入事务 Port、其他应用服务和既有白名单仍未完成，阶段 4/8/9 继续保持“进行中” |
| 2026-08-15 | P1 订单事务 Writer Port | 已完成 | 新增 `orders.OrderPatch`、`ItemWrite`、`UpsertOptions`、`Writer` 和 `UnitOfWork`；订单更新、导入、手动发货和刷新统一通过应用 Writer，`*sql.Tx` 仅存在于 Server 基础设施适配器；保留现有提交/回滚语义并通过订单回归与 `make check`。订单实体读取、凭证 Port、其他应用服务和 Server 白名单仍未完成，阶段 4/8/9 继续保持“进行中” |
| 2026-08-15 | P1 订单实体读取与平台运行视图 Port | 已完成 | 新增纯应用层 `Order`、`ItemInfo`、`PlatformRuntimeData`；订单详情、商品读取、订单分页和刷新凭证读取不再通过 repository 暴露 `db.Order`、`db.ItemInfo`、`db.OrderRow` 或 `db.CookieDetail`；Server 到现有自动化/会话辅助函数保留显式边界适配；新增字段完整性转换测试，`make check` 和注释门禁通过。该三切片仅收口订单 Port，凭证协调器、生命周期、补偿、批量同步和其他领域依赖仍未完成，阶段 4/8/9 继续保持“进行中” |
| 2026-08-15 | P1 凭证快照与账号操作协调器（订单详情切片） | 已完成 | `Store.LockAccountCredentials` 增加引用计数和空闲 entry 回收，并保持同账号互斥与重复释放幂等；订单详情流程改为锁内读取、锁外执行慢速 MTOP、锁内重读提交，检测到期间凭证变化时丢弃旧响应；新增锁回收、并发互斥、慢速 I/O 不占锁和 race 回归测试，`make check`、注释门禁及聚焦 race 通过。其他凭证调用方仍待迁移，版本化持久化冲突控制和协调指标仍未完成，阶段 2/4/5/8/9 继续保持“进行中” |
| 2026-08-15 | P1 凭证快照与账号操作协调器（API 续期切片） | 已完成 | `apiCookieRenewOne` 改为锁内读取、锁外执行 `renewAPI`、锁内重读提交；外部调用期间凭证变化时使用 `RebaseResponseCookies` 基于最新 Cookie/metadata 重放 `Set-Cookie`，无可重放数据则拒绝旧快照写回；新增慢 I/O 不占锁和并发更新保留测试，续期全量测试、聚焦 race 与注释门禁通过。登录续期、Engine、Automation 和版本化持久化冲突控制仍未完成，阶段 2/4/5/8/9 继续保持“进行中” |
| 2026-08-15 | P1 凭证快照与账号操作协调器（登录态检查切片） | 已完成 | `loginRenewOne` 改为锁内读取、锁外执行 `loginuser.get`、锁内重读提交；检查期间凭证发生变化时丢弃基于旧快照的响应 Cookie，避免覆盖并发更新；保留完整 Cookie Jar 权威持久化、Session 过期恢复和禁用账号语义；新增慢 I/O 不占锁及旧响应拒绝测试，续期全量测试、聚焦 race、注释门禁通过。Engine、Automation、剩余 Server 调用方和版本化持久化冲突控制仍未完成，阶段 2/4/5/8/9 继续保持“进行中” |
| 2026-08-15 | P1 凭证快照与账号操作协调器（Engine 登录态检查切片） | 已完成 | `Account.tryLoginStatusCheck` 改为锁内读取、锁外执行登录态检查、锁内重读提交；检查期间数据库 Cookie/metadata 发生变化时丢弃旧会话响应，避免更新运行时和数据库为过期状态；新增 Engine 慢 I/O 不占锁及并发旧响应拒绝测试，Engine 全量测试、聚焦 race 和注释门禁通过。Engine 其他 token/续期路径、Automation、Server 其他调用方和版本化持久化冲突控制仍未完成，阶段 2/4/5/8/9 继续保持“进行中” |
| 2026-08-15 | P1 凭证快照与账号操作协调器（Engine API 续期切片） | 已完成 | `Account.tryAPIRenewUsing` 保留 `refreshMu` 的同账号刷新串行语义，但改为锁内读取、锁外执行续期回调、锁内重读提交；外部期间凭证变化时用 `RebaseResponseCookies` 基于最新状态合并 `Set-Cookie`，无可重放数据则拒绝旧状态；新增慢 I/O、并发锁获取和 Cookie 合并测试，Engine 全量测试、聚焦 race、注释门禁通过。Engine Token 刷新、Automation、Server 其他调用方和版本化持久化冲突控制仍未完成，阶段 2/4/5/8/9 继续保持“进行中” |
| 2026-08-15 | P1 凭证快照与账号操作协调器（Engine Token 刷新切片） | 已完成 | `refreshTokenWithMinGap` 保留 `refreshMu` 的同账号 Token 刷新和风控重试串行语义，但网络请求、风控恢复均在共享凭证锁外执行；每次响应提交前重读最新 Cookie/metadata，检测到并发变化时丢弃旧 Token 响应并使用最新快照重试；新增慢 I/O、锁可获取和并发 Cookie 变化重试测试，Engine 全量测试、聚焦 race、注释门禁通过。Automation、Server 其他调用方和版本化持久化冲突控制仍未完成，阶段 2/4/5/8/9 继续保持“进行中” |
| 2026-08-15 | P0 敏感系统设置命令语义切片 | 已完成 | 管理端系统设置更新新增 `values`/`secrets` 分离 DTO；敏感设置仅接受 `retain`、`replace`、`clear` 命令，普通 `values` 和旧版顶层字段均拒绝敏感键；数据库原子应用普通设置与敏感命令；补充 Go 数据库/HTTP 回归和 React API 请求体测试，定向 Go/React 测试与前端类型检查通过。P0 三库迁移回归、所有运维输出和敏感访问审计仍待完成 |
| 2026-08-15 | P1 订单运行时 Port 边界切片 | 已完成 | 订单应用服务不再持有 `*Server`，改由 `orderRuntimePort` 接收平台、自动化、通知和运行时 Cookie 能力；`serverOrderRuntimeAdapter` 仅在 Server 装配边界桥接现有实现；订单行为、凭证锁和错误语义保持不变；Server 全量测试、架构门禁和中文注释门禁通过。订单 Port 仍位于 `internal/server`，repository 仍有基础设施适配职责，其他应用服务和 `sql.Tx`/`db.*` 全面隔离尚未完成 |
| 2026-08-15 | P1 订单 Repository Port 迁移切片 | 已完成 | 将 `orderRepository` 从 `internal/server` 迁移为 `internal/application/orders.Repository`；应用 Port 只暴露领域模型、`Writer`/`UnitOfWork`、凭证协调和平台运行视图，不出现 `sql.Tx`、`db.*`、HTTP 或 Server 类型；Server 仅保留 `storeOrderRepository` 基础设施适配器；订单全量测试、架构门禁和中文注释门禁通过。订单运行时 Port 仍依赖 Server 适配边界，其他应用服务及事务/批量治理仍未完成 |
| 2026-08-15 | P0 敏感设置跨方言回归补强 | 已完成 | `TestMultiDB_SettingsQuoteKey` 新增敏感设置 `replace/retain/clear`、脱敏读取和密文解密回归；SQLite 在当前环境执行通过，MySQL/Postgres 仅在 `TEST_MYSQL_URL`/`TEST_POSTGRES_URL` 提供时自动加入，当前环境未提供外部数据库，因此不宣称三库实测完成。P0 的运维输出和敏感访问审计仍待完成 |
| 2026-08-15 | P1 应用 Port 类型架构门禁切片 | 已完成 | `architecturecheck` 改用完整 AST 扫描 `internal/application` 类型声明，主动拒绝 `db.*`、`sql.Tx` 和 `*Server` 泄露；新增正反例门禁测试；现有订单 Port 通过扫描，架构门禁与中文注释门禁通过。Server 允许边、订单平台 Port 和其他应用服务仍需继续迁移，不能据此宣称目标架构完成 |
| 2026-08-15 | P1 生命周期上下文边界切片 | 已完成 | `Account.StopContext`、`Manager.StopContext/StopAllContext` 和 `Server.Stop` 的 HTTP、后台任务及 worker 等待均受调用方 Context 限制；超时后未完成账号仍保留在管理表，避免误报已清理；`cmd/server` 使用关闭上下文停止账号；新增账号与 Server 超时回归及聚焦 race 测试通过。账号删除游离 goroutine、调度器等待和删除 fencing 仍未完成，阶段 5 继续保持“进行中” |
| 2026-08-15 | P1 账号删除停止任务登记切片 | 已完成 | 删除账号后的 `Manager.StopContext` 不再通过游离 goroutine 调用，而是登记到 Server `backgroundWG`；Server 关闭会等待或按 Context 收束该任务；新增删除 handler 等待后台任务回归。删除状态 fencing、任务 ID 和调度器 Context-aware 等待仍未完成，阶段 5 继续保持“进行中” |
