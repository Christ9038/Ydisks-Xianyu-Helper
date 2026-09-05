# OpenAPI 契约收口重构总计划

## 唯一治理规则

本文是唯一阶段状态、顺序和验收依据。`refactoring-progress.md` 只保存旧六阶段最终提交与证据，不定义新阶段入口。旧六阶段成果是不可回退的已完成架构基线：应用服务、数据库、生命周期和 React feature 化均不重新实施，既有安全、依赖、注释、兼容和冻结 CAPTCHA 门禁继续有效。

本计划只有六个正式阶段，严格按 1 -> 2 -> 3 -> 4 -> 5 -> 6 执行。一个阶段是一个任务、一个评审单元和一条最终中文提交；阶段中不得创建中间提交、切片交付、提前更新状态或进入后续阶段。

## 目标与边界

唯一 HTTP 契约链路：`OpenAPI 3.1 -> 生成 TypeScript paths/types -> 类型化 HTTP client -> feature adapter -> UI model`。

`api/openapi.yaml` 是 `/api/v1/**` 与 `/health` 的唯一 HTTP 契约源；旧兼容路径不重复登记，继续由兼容矩阵和等价路由测试保护。生成的 `frontend/shared/api-contract/generated/schema.ts` 只读并提交。真实 handler 使用 `kin-openapi` 按同一规范校验。

契约校验采用非对称规则：OpenAPI 中前端必需字段缺失或类型错误失败；声明的可选字段类型错误失败、缺失允许；后端额外非敏感字段允许，且不会出现在生成的前端类型中。敏感字段泄漏仍由安全门禁和响应测试独立阻断。UI 派生模型、表单状态和兼容归一模型不是 HTTP DTO。对象默认允许额外属性；动态设置、账号通知绑定等动态对象显式声明 `additionalProperties` 值类型。每个 operation 必须有稳定 `operationId`、成功响应、统一错误响应、鉴权元数据和请求参数，不得用无约束 `{}`、`object` 或 `any` 代替已知业务字段。

## 当前状态

| 阶段 | 状态 | 严格结论 |
| --- | --- | --- |
| 既有架构基线：稳定性、组合根、生命周期、React、DB、质量收口 | 已完成（不可回退） | 历史实现和证据留在 `refactoring-progress.md`，不重做、不改写历史。 |
| 1. 契约基础设施与全路由登记 | 已完成 | OpenAPI、生成漂移和真实 Router 双向门禁已建立，未改变业务运行时形状。 |
| 2. 类型化客户端与登录账号主链路 | 已完成 | session、system、accounts、QR 风控和永久关闭的 password-login 已迁移并完成真实响应校验。 |
| 3. 查询、聊天与订单主链路 | 已完成 | dashboard、实际消费的 admin 摘要、chat、orders、刷新任务和 WebSocket 消息均已迁移并完成真实契约校验。 |
| 4. 商品、卡券和文件传输 | 已完成 | items、批量发布、cards 和上传 adapter 已迁移；原生 FormData、取消、重试和批次隔离均保留。 |
| 5. 自动化、设置和通知动态契约 | 已完成 | rules、automation、settings、notifications 和账号绑定均通过契约客户端；动态账号键有明确值类型约束。 |
| 6. 全量封闭与旧手写契约退场 | 已完成 | 已关闭原始 HTTP client 旁路，删除手写 transport DTO 和名单式门禁；全部 operation 已具备真实成功响应或明确特殊校验证据，契约门禁永久启用。 |

## 阶段一：契约基础设施与全路由登记

建立 `api/openapi.yaml`、固定版本的 `openapi-typescript`、`openapi-fetch`、`kin-openapi`、`make api-generate`、`make api-check` 和 CI 漂移检查。通过 `chi.Walk` 比较真实 Router 与规范，双向覆盖 `/api/v1/**`、`/health` 和动态订单刷新路由。每个 operation 立即登记稳定 ID、成功响应、统一错误响应、鉴权和路径参数；业务成功 schema 在后续阶段逐操作收紧。删除 `frontendDTOContractSpecs` 名单式门禁，保留二维码字段修复和通知摘要敏感字段保护，不改变任何 URL、状态码、包装或 feature 行为。

验收：

```text
make api-check
go run ./tools/architecturecheck
go test ./internal/server -count=1
npm run typecheck --prefix frontend
npm test --prefix frontend
make comments
npm run build --prefix frontend
git diff --check
```

最终提交：`阶段一：建立 OpenAPI 单一契约源与全路由门禁`。

## 阶段二：类型化客户端与登录账号主链路

把 Cookie、超时、外部 AbortSignal、401 合并登出、统一 ApiError 和 FormData 行为封装进类型化客户端运行时，迁移 session、system、accounts、QR 风控及永久关闭的 password-login。adapter 继续输出 UI model，生成类型不得直接进入 React state 或 props；补齐成功、未认证、越权、未找到、风控状态和敏感字段不泄漏的真实响应校验。冻结 CAPTCHA 文件、选择器、时序、Cookie 合并和浏览器调用顺序完全不变。

最终提交：`阶段二：迁移登录账号与风控接口到生成契约`。

## 阶段三：查询、聊天与订单主链路

迁移 dashboard、admin、chat、orders 查询和订单刷新任务。WebSocket 握手登记为 HTTP operation，消息体使用 OpenAPI component schema；聊天 adapter 保留唯一原生 WebSocket 实现。保留分页、游标、订单状态、旧包装归一化和晚到响应隔离，覆盖刷新成功、失败、取消、超时及消息字段类型。

最终提交：`阶段三：迁移查询聊天与订单接口到生成契约`。

## 阶段四：商品、卡券和文件传输

迁移 items、批量发布、cards、表格和图片上传、CSV 下载。OpenAPI 明确 multipart、二进制响应、Content-Disposition 和长请求超时；继续使用 FormData，保留批次代次隔离、取消、重试和不确定远端结果。覆盖格式错误、部分成功、取消、重试、CSV 和客户端取消。

最终提交：`阶段四：迁移商品卡券与文件接口到生成契约`。

## 阶段五：自动化、设置和通知动态契约

迁移 rules、automation、settings、notifications 和账号通知绑定。动态设置与按账号 ID 的动态键使用受约束 additionalProperties；通知摘要不返回 SMTP 密码、Token 或渠道秘密配置，编辑器不从摘要 DTO 恢复秘密。覆盖动态键类型、敏感配置三态变更、渠道别名归一、自动化问题和统一错误响应。

最终提交：`阶段五：迁移自动化设置与通知动态契约`。

## 阶段六：全量封闭与旧手写契约退场

禁止 feature、组件和 Hook 导入原始 `get/post/put/del/postForm`，只有共享契约客户端运行时可调用 fetch。删除手写 `transport.ts` 和废弃 DTO；门禁从 OpenAPI 自动发现 operation、生成类型和真实契约测试覆盖，不保留 DTO 名单、路径白名单或可扩展 baseline。每个 operation 必须有真实成功响应，或属于明确的 WebSocket/二进制特殊校验。更新 AGENTS、依赖规则、兼容矩阵和进度证据；新接口必须先改 OpenAPI、生成代码和服务端契约测试才能被前端使用。

最终验收：`make check`、`go test ./... -count=1`、Server race、全部前端测试、`make cover`、`make cover-frontend`、嵌入前端构建及二维码/浏览器回归。最终提交：`阶段六：完成生成契约迁移并永久关闭旁路`。

## 执行纪律

阶段最终验收失败时状态仍为当前阶段，修复后重跑完整命令；只有全部命令成功后才一次更新状态、证据和下一阶段入口并创建最终中文提交。六个阶段现已全部完成，后续不再开启新阶段入口。不得扩大白名单、baseline、忽略路径或 warning-only 旁路；后续缺陷修复保持全部已启用门禁永久有效。

## 后续窄范围安全修复记录

- 2026-09-05（本地修复，未发布）：修复再次审查确认的两项订单同步回归。恢复写入把平台 `unknown` 当作未提供状态，防止同账号软删除恢复和历史错绑修正覆盖本地已完成状态；会话匹配始终同时查询裸买家标识与 `@goofish` 后缀，保留候选歧义及账号、商品隔离。补充真实 SQLite 恢复和重复同步测试、状态转换单测及三方言会话匹配用例。无需迁移或契约变更，冻结 CAPTCHA、六阶段状态、注释基线和架构门禁不变；本轮验证记录见 `order-sync-ownership-fix-plan.md` 第 8 节。

- 2026-09-05（本地修复，未发布）：修复未提交内容审查确认的五项回归。规则删除保护补齐部分发送后的 `[safe_retry]` 与 `action_started` 未知结果，判定与 00043 升级清理一致；联系人分页允许向历史递减，按已见游标及页数预算阻止停滞和循环；订单同步在凭证锁外补联系人，重新持锁后复核取消、凭证及发现代次，再执行本地提交。通知编辑在脱敏配置加载完成后才开放表单，并隔离关闭、新建和切换后的旧响应；兼容旧版 SMTP 覆盖，默认通过新增可选 `email_recipient` 更新语义保留所有服务端 SMTP 字段及秘密，仅在用户选择重新配置时替换完整配置。同步更新 OpenAPI、生成类型、真实 handler 契约、前端适配器及嵌入资源。六阶段状态、冻结 CAPTCHA、白名单和注释基线保持不变。验证命令、覆盖率和已知基线例外见 `order-sync-ownership-fix-plan.md` 第 7 节。

- 2026-09-04（已完成，本地已应用，未发布）：补齐已发布版本软删除自动化规则的升级清理。新增三方言 `00043_deleted_automation_rules_cleanup`，仅清理没有待处理运行的历史已删除规则，依靠现有外键级联删除其动作、模板绑定及已结束运行；卡密、订单、延期任务和 00042 独立执行守卫不删除。保留启用或仅停用的未删除规则，以及 running、needs_review、action_started 结果未知、普通可重试及部分发送后的 safe_retry 运行。不改变现行规则删除接口或冻结 CAPTCHA；六阶段状态、全部门禁和注释基线保持不变。定向 `go test ./internal/db -run 'TestMultiDB_DeletedAutomationRules|TestMultiDB_TargetMatrix' -count=1 -v`、严格 `make test-multidb`、旧迁移/规则删除回归、`make check`、注释、`git diff --check` 和服务构建通过；`make cover` Go statement 81.5%（未设置 RUN_BROWSER_INTEGRATION），`make cover-browser` 浏览器包 64.2%（RUN_BROWSER_INTEGRATION=1），`make cover-frontend` 前端 statement 79.19%。新增 SQL 业务分支均有 SQLite 确定性夹具，并使用 Docker 中的 MySQL 8.4、PostgreSQL 17 完成严格三方言实测；本修复无真实平台接口调用和新增真实账号测试例外。真实数据库副本经 `cmd/dbverify` 升级通过，3 组卡密逐一 DELETE 成功后回滚。停止旧服务后的 一致性备份为 `/tmp/xianyu-card-upgrade.0mhBkf/pre-restart.db`（私有目录、文件权限 0600，临时目录不是长期备份）；新服务启动自动推进至 43，清理 5 条旧规则及其 10 条终态运行，两类卡密引用均为 0。3 组卡密逐列比对备份无修改，18 条订单及 10 条独立执行守卫保留，外键检查和 `/health` 正常。数据清理不可逆，Down 仅回退迁移账本，不重建历史规则或运行；恢复必须使用升级前备份。新逻辑随下一版本分发，不需要用户提供账号/规则 ID 或执行手工 SQL；未结束运行继续受保护，不强行解除其引用。

- 2026-09-04（已完成，未发布）：修复买家侧 WS 评价误建卖家订单，以及历史错误归属导致一键同步整批失败。提供不依赖固定账号/订单的通用历史恢复；三方言 `00042_order_ownership_repairs` 保存非敏感修复审计、独立自动化执行痕迹及相关索引，保留普通写入归属 CAS、已有规则物理删除与通知变更；补齐完整分页、失败提示、恢复计数、凭证复核、账号发现代次和动作领取并发保护。真实已登录账号首次人工同步修正 2 单，卖家恢复到 12 单，重复及最终服务重启后人工同步均 0 失败、0 重复修正，自动化运行保持 10 条。最终 `make check`、注释、API、构建、`make cover`、`make cover-browser`、前端覆盖率及定向 race 通过；另以 Docker 中的 MySQL 8.4、PostgreSQL 17 连同 SQLite 完成严格 `make test-multidb`，全部 `TestMultiDB_*` 通过；普通 Go 语句覆盖率 81.5%（未设 RUN_BROWSER_INTEGRATION），浏览器包 64.2%（RUN_BROWSER_INTEGRATION=1），前端 79.19%；全库 DB race 一次十分钟超时不记通过，真实新交易断线流程未实测。所有证据、备份、恢复边界及例外集中记录于 `order-sync-ownership-fix-plan.md` 第 6 节。本次不新增上线自动同步、不改变冻结 CAPTCHA、不扩展白名单或注释基线；六阶段完成状态及全部门禁保持不变。

- 2026-09-01：通知外部请求错误在渠道边界统一移除 URL 用户信息、路径、查询参数和片段，HTTP 测试接口改为稳定公开错误，日志与 outbox `last_error` 仅保存脱敏诊断；旧 SHA-256 密码升级改为以用户标识和已验证摘要为条件的比较并交换写入，哈希、写入及驱动结果错误全部向认证层传播，避免并发改密被过期登录覆盖。本修复不改变 HTTP schema、数据库 schema、迁移编号、包边界或冻结 CAPTCHA 行为，也未新增白名单和 baseline。聚焦回归覆盖通知 Token/Webhook 泄漏、日志与持久化边界、并发改密、bcrypt 生成失败、数据库写入失败及受影响行数读取失败；`make check`、`go test ./... -count=1`、`make test-server-race`、`make comments` 和 `git diff --check` 通过，`make cover` 的 Go statement 覆盖率为 81.4%，`TestMultiDB_LegacyPasswordUpgradeCAS` 在 SQLite、MySQL 8.4 与 PostgreSQL 17 实测通过。额外执行的完整 `make test-multidb` 仅有与本修复无代码交集的既有 MySQL `TestMultiDB_OrdersUpsertManyMixedCreatedAt` 时间格式断言失败，SQLite、PostgreSQL 对应子测试及其余三方言用例通过；该基线问题未混入本次窄范围安全修复。
