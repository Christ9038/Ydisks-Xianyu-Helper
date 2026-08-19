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
| 5. 自动化、设置和通知动态契约 | 当前阶段 | 迁移 rules、automation、settings、notifications 和账号绑定。 |
| 6. 全量封闭与旧手写契约退场 | 待执行 | 关闭原始 HTTP client 旁路，删除手写 transport DTO 和名单式门禁。 |

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

阶段最终验收失败时状态仍为当前阶段，修复后重跑完整命令；只有全部命令成功后才一次更新状态、证据和下一阶段入口并创建最终中文提交。不得扩大白名单、baseline、忽略路径或 warning-only 旁路；后续缺陷修复保持全部已启用门禁永久有效。
