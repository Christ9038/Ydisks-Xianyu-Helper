# Ydisks 闲鱼助手重构总计划

## 1. 计划目标

本文是仓库重构的唯一阶段路线图，只记录目标架构、十个正式迭代、阶段验收条件和强制约束。
本文不记录开发日期、提交列表、阶段内进度、完成日志、人员记录或临时工作说明。

阶段 0 是执行重构前的治理前置条件，不计入正式迭代。阶段 1 至阶段 10 各自对应一个完整
迭代和一个阶段级 PR/最终合并。阶段内部允许同步修改多个包、feature、测试、门禁和文档；
agent 不得将阶段拆成多个独立交付。阶段进行中的本地工作区或临时提交可以暂时不可编译，
但阶段最终提交必须可编译、可启动、可测试并通过本阶段全部强制验证。

滑块 CAPTCHA 行为继续由 `docs/slider-captcha-frozen-spec.md` 保护。本文不是修改冻结行为的授权。

### 1.1 最终成功标准

1. `cmd` 只负责配置、依赖构造、信号和进程生命周期。
2. `internal/server` 只负责 HTTP/WebSocket transport、鉴权、中间件、具名 DTO 和 SPA。
3. 应用服务负责完整用例、所有权校验和事务边界，且不依赖 HTTP、数据库或平台实现类型。
4. `internal/db` 独占 SQL、迁移、方言、加密持久化和 repository 实现。
5. 账号摘要、平台凭证、密码登录秘密和运行配置使用不同模型和最小查询。
6. Engine 与 Automation 的并发状态、凭证协调、外部动作和生命周期由明确组件拥有。
7. React 遵守 `app -> features -> shared`，业务请求、状态和 DTO 转换归属对应 feature。
8. SQLite、MySQL、PostgreSQL 的迁移、事务和核心业务行为一致。
9. 旧 API 和兼容字段只在有调用方、遥测和删除条件时保留，最终按明确 Sunset 退场。
10. 架构、注释、race、多数据库、前端类型和构建均成为自动门禁，无历史豁免。

## 2. 目标架构

```text
cmd
  -> application services / lifecycle
       -> consumer-defined ports
            <- adapter implementations
                 -> db / xianyu / browser / notify

HTTP server
  -> application services

React app shell
  -> feature page
       -> feature hooks / state / API adapter / UI model
            -> shared HTTP client and shared UI
```

接口由消费者定义并保持最小。禁止全局 service locator、万能 repository、通过 `any`/反射隐藏
依赖、或仅为绕过架构检查增加中转层。详细依赖规则以
`docs/architecture/dependency-rules.md` 为准。

## 3. 当前代码基线

本节只描述制定后续迭代所需的当前事实，不保存重构历史。

- 当前开发分支从本地 `main` 演进；本地 `main` 落后 `xianyu-go/main`，功能对照必须使用
  `xianyu-go/main`，不能只对照本地分支。
- 当前工作树包含大量未提交和未跟踪文件。后续迭代必须原地保护这些修改，禁止 reset、覆盖或
  以重建分支代替审计。
- 远端 `main` 的空卡密修复与当前前端 API 文件存在文本合并冲突。当前代码必须持续保证服务端
  空列表编码为 `[]`，前端同时接受数组、包裹对象和 `null`；对应回归测试是强制契约。
- 远端 `main` 的文档链接变更不影响运行功能。生成前端资产的文本冲突必须通过当前源码重新构建
  解决，禁止手工合并压缩产物。
- 先前关于 Server 组合根、应用生命周期、React Feature 化和 DB 治理的完成声明均撤销为“待重新验收”。
  当前 `internal/server` 仍构造应用服务和 worker 并持有基础设施相关依赖；阶段 4 必须先完成组合根迁移。
- 账号敏感摘要与秘密访问已经分离，并已有审计和多数据库覆盖；阶段 2 可视为完成，但后续迭代
  不得重新扩大读取、解密、序列化或日志范围。
- HTTP `/api/v1` 版本化、具名 DTO、统一错误、前端兼容归一和调用方审计已完成阶段 3 验收；
  受控的设置键、账号绑定键和触发统计键仍按兼容清单保留，删除条件记录在
  `docs/architecture/api-compatibility-matrix.md`。
- Engine `Account` 与 Automation `Center` 已有部分组件化实现，但其生命周期、关闭顺序和并发状态仍须在
  阶段 5 重新验收。集中前端 API/类型文件和商品发布异步状态属于阶段 6 的后续范围。
- 三方言迁移编号当前对齐，订单已有游标和批量写入能力；跨 repository 事务、查询计划、补偿和
  大数据量行为仍需阶段 8 统一验收。
- 注释历史基线文件当前为空，但源码仍存在“保存 X 供当前流程使用”等模板化注释。机械基线清零
  不等于注释质量达标，阶段 10 尚未验收。
- 与远端 `main` 相比，受冻结规范保护的 CAPTCHA 文件存在差异。其当前行为必须以仓库内冻结规范
  和全部冻结测试为准；没有用户明确授权不得修改、回退或借重构重新合并这些文件。

## 4. 迭代状态与唯一顺序

阶段状态只允许使用 `前置完成`、`已完成`、`当前迭代`、`待执行`、`阻塞`。只有一个阶段可以是
`当前迭代`；后续阶段已有的提前实现代码只作为该阶段未来验收的输入，不代表阶段已启动或完成。

| 阶段 | 状态 | 迭代目标 |
| --- | --- | --- |
| 0. 治理与强约束 | 前置完成 | 建立计划、依赖、注释、冻结规范和自动检查 |
| 1. CI 与测试基础 | 已完成 | 建立独立 CI、分层 race 和三数据库门禁 |
| 2. 敏感数据访问边界 | 已完成 | 分离摘要、平台凭证、登录秘密和敏感设置 |
| 3. HTTP API 契约 | 已完成 | 完成版本化、具名 DTO、统一错误和前端契约 |
| 4. Server 应用服务 | 当前迭代 | 把完整业务用例和事务边界移出 transport，并完成启动稳定性修复 |
| 5. 应用装配、生命周期、Engine 与 Automation | 待执行 | 消除隐式装配、业务 worker 和游离后台任务并重新验收状态所有权 |
| 6. React Feature 化与异步状态 | 待执行 | 完成 feature 边界、请求归属、状态和按路由加载 |
| 7. DB 与事务治理 | 待执行 | 完成窄 repository、Unit of Work 和三方言验收 |
| 8. 架构门禁与兼容退场 | 待执行 | 封死依赖旁路并按 Sunset 删除兼容层 |
| 9. 注释与复杂度收口 | 待执行 | 清除模板注释和复杂度债务，形成最终质量门禁 |
| 10. 最终全栈复验 | 待执行 | 以完整 Go、Browser、三方言和前端证据关闭计划 |

执行顺序固定为 `0 -> 1 -> 2 -> 3 -> 4 -> 5 -> 6 -> 7 -> 8 -> 9 -> 10`。除阻塞性
安全修复、构建修复或用户明确改序外，不得跳过当前阶段推进后续阶段。例外只解决阻塞问题，不能
借机开启另一个重构阶段。

### 4.1 阶段级实施矩阵

| 阶段 | 主要改动位置与实施方式 | 最终验收与禁止项 |
| --- | --- | --- |
| 4. Server 应用服务（当前） | `cmd/server` 先同步 `Bind`，再启动协调器，最后 `Serve`；数据密钥采用 `O_CREATE|O_EXCL` 并处理竞争、空文件和写入失败。由 `cmd/server` 与 `internal/adapter` 组装不可变 `server.Dependencies`/`ApplicationServices`，`internal/server` 仅保留 transport、DTO 和路由；删除 Server worker factory、`ApplicationLifecycleComponents()` 与基础设施生产持有。 | 运行本阶段 Server/application/adapter race、`go vet`、lint、comments、architecturecheck 和 `git diff --check`。保留 `:59188` 与远程网页首次初始化，只输出无秘密高等级警告并在文档说明部署者风险。 |
| 5. 生命周期、Engine 与 Automation | `cmd` 独占 coordinator；为账户、自动化、续期、通知、浏览器、订单刷新、批量发布和 reconciliation 补齐 owner、Context、Cancel、Wait/Join、失败回滚和关闭顺序测试。 | 全量 Go、指定 race 和浏览器集成测试；不编辑任何冻结 CAPTCHA 文件、调用顺序或规范。 |
| 6. React Feature 化与异步状态 | 将 `shared/api-contract/index.ts` 按 session/accounts/items/orders/automation/notifications/settings/chat/cards-admin/common 拆分，feature adapter 直接导入领域契约并转换 UI model；地图服务归入 items feature。统一 `ApiError`，为定位与批量轮询使用独立 AbortController/generation/timeout；清理未使用符号后启用 TypeScript 未使用检查。 | Vitest、typecheck、comments、build、V8 coverage 与嵌入资源重建。不得让组件直接请求网络或以大型 barrel 隐藏跨领域依赖。 |
| 7. DB 与事务治理 | 清除上层 `Store.DB`、`*sql.DB`、`*sql.Tx` 与 row model 泄露；跨 repository 原子操作通过应用层 Unit of Work；补齐 claim、lease、取消、重试、不确定结果和补偿测试。 | SQLite 并发/事务测试、MySQL/PostgreSQL 迁移/CRUD/并发 claim、`cmd/dbverify` 完整证据。 |
| 8. 架构门禁与兼容退场 | 扩展 AST/源码检查以覆盖 Server 低层依赖、应用 Port 泄露、service locator、动态 import、前端依赖方向和根 services 旁路；仅在遥测、Sunset、调用方迁移和契约测试齐备时删除兼容入口。 | 不得新增白名单或扩大 baseline；同步旧路由、Vite 代理、契约测试与嵌入资源。 |
| 9. 注释与复杂度收口 | 清除模板化中文注释，处理超大文件/高复杂度与依赖绕过门禁；不以机械注释或格式化代替责任拆分。 | `make comments`、全量质量门禁和覆盖率；不扩大历史基线。 |
| 10. 最终全栈复验 | 只处理前序阶段验收暴露的阻塞缺陷，并汇总最终提交、完整命令、Go/前端覆盖率和真实外部例外。 | 运行全量 Go/race/lint/comments/architecturecheck/前端门禁，适用时运行浏览器和三方言验证；最终提交可启动。 |

## 5. 阶段 0：治理与强约束

### 目标

建立所有后续迭代共同遵守的边界、冻结行为和检查入口。阶段 0 是前置治理，不占十个正式迭代。

### 范围

- 维护本计划、`dependency-rules.md`、`comment-standard.md` 和 `AGENTS.md`。
- 冻结滑块 CAPTCHA 实现、调用链和测试。
- 提供架构检查、Go/TypeScript 注释检查和统一 Make 入口。

### 完成条件

- 文档规则互不矛盾，执行单位明确为一个阶段对应一个 PR/迭代。
- 自动检查可在本地和 CI 运行，新增债务不能借历史基线通过。
- 本计划不包含任何历史日志或阶段内完成记录。

### 强制验证

```bash
go run ./tools/architecturecheck
go run ./tools/commentlint -mode check -root .
npm --prefix frontend run comments:check
```

## 6. 阶段 1：CI 与测试基础

### 目标

让每个后续迭代都能在独立、稳定、耗时可控的门禁上验收。

### 范围

- PR CI：Go test/vet/lint、架构、注释、前端 test/typecheck/build。
- 普通测试与生命周期/凭证并发 race 分层。
- SQLite 快速回归和 Docker MySQL/PostgreSQL 严格回归。
- 测试数据库模板、确定性 fixture 和覆盖率产物隔离。

### 完成条件

- CI 不依附发布 workflow，失败会阻止合并。
- race 子集覆盖高风险并发路径，完整 race 可在需要时执行。
- `make test-multidb` 缺少任一外部数据库 URL 时明确失败，而不是静默跳过。

### 强制验证

```bash
make check
make test-server-race
TEST_MYSQL_URL=... TEST_POSTGRES_URL=... make test-multidb
npm test --prefix frontend
npm run build --prefix frontend
```

## 7. 阶段 2：敏感数据访问边界

### 目标

让每个用例只读取完成任务所需的最小数据，阻止秘密进入摘要、所有权查询、HTTP DTO 和前端状态。

### 范围

- 分离 `AccountSummary`、`AccountCredential`、`AccountLoginSecret` 和运行配置。
- 所有权查询只返回存在性、所有者 ID 或非敏感身份。
- 敏感读取使用目的明确的方法，管理/一次性读取有审计且失败时 fail closed。
- 日志、通知、错误和测试输出集中脱敏。
- SQLite、MySQL、PostgreSQL 验证加密、损坏密文、跨用户和迁移行为。

### 完成条件

- 账号列表、详情、状态、商品、订单、聊天和规则校验不读取 Cookie、Token 或密码。
- 敏感持久化模型不被 HTTP 序列化，前端类型不包含可回填的明文秘密。
- 高频运行时不会为普通摘要读取反复解密或写审计。
- 三数据库的敏感字段 CRUD、迁移和失败语义一致。

### 强制验证

```bash
go test ./internal/db ./internal/account ./internal/server -count=1
go test -race ./internal/account ./internal/server -count=1
TEST_MYSQL_URL=... TEST_POSTGRES_URL=... make test-multidb
make comments
```

## 8. 阶段 3：HTTP API 契约

### 目标

在不破坏当前客户端的前提下完成统一 `/api/v1` 契约、具名 DTO、统一错误和可验证的前端类型边界。

### 范围

- 为全部业务入口提供 `/api/v1` 路径；旧路径只作为调用同一 handler 的兼容适配器。
- 请求、成功响应、分页结果、批量行结果和错误详情全部使用具名 DTO。
- 清除 QR、订单导入/批量结果、聊天和设置 transport 中作为响应契约的动态 map。
- 统一错误 envelope、错误码和 HTTP 状态；禁止新增 `HTTP 200 + success:false`。
- 建立单一契约来源和只读生成类型；feature adapter 将 transport DTO 转换为 UI model。
- 固定空集合为 `[]`，并兼容当前发布链路可能返回的 `null` 或历史包裹对象。
- 契约测试同时覆盖版本化路径和仍被支持的旧路径。

### 完成条件

- 生产 handler 不直接返回数据库模型、领域模型、匿名 struct 或动态 map 契约。
- 前端业务 API 不再依靠 `any` 猜测响应形状；兼容归一只发生在 feature/transport 边界。
- 所有新增失败使用统一错误结构和正确状态码。
- 当前前端只调用 `/api/v1`；旧路径保留范围和删除条件可由测试清点。
- 空卡密、空订单、空商品等集合不会因 `null` 阻断页面其他并行请求。

### 强制验证

```bash
go test ./internal/server -count=1
npm test --prefix frontend
npm run typecheck --prefix frontend
npm run build --prefix frontend
go run ./tools/architecturecheck
make comments
```

## 9. 阶段 4：Server 应用服务

### 目标

使 Server 成为纯 transport，把订单、商品、账号、登录、聊天、通知、自动化和管理员用例交给应用服务。

### 范围

- handler 只做鉴权上下文、参数解析、DTO 转换和应用错误到 HTTP 的映射。
- 应用服务负责所有权、业务校验、跨步骤编排、事务选择和补偿决策。
- Port 由应用消费者定义，不暴露 `db.*`、`*sql.Tx`、HTTP、Server、MTOP 或 browser 类型。
- 删除 Server 中遗留的 `*sql.Tx` 执行器、无调用兼容服务和低层 helper。
- 审查集中式 `adapter.Dependencies`；改为显式、类型化构造，不得成为隐藏 Store 的 service locator。
- 把仍在 Server 内的账号登录持久化、平台会话和业务 worker 操作迁到应用边界。

### 完成条件

- Server 生产代码不导入 `database/sql`、`internal/db`、`internal/xianyu` 或 `internal/browser`。
- Server 不持有 repository 聚合入口，不创建事务，不执行平台业务动作。
- 应用包不依赖 HTTP、chi、DB、Server 或平台实现。
- 每个完整用例的正常、越权、缺失、失败、取消和补偿分支有应用层测试。
- 构造器缺失必需依赖时立即返回错误，不在请求期 panic 或延迟发现。

### 强制验证

```bash
go test ./internal/application/... ./internal/adapter ./internal/server -count=1
go test -race ./internal/application/... ./internal/adapter ./internal/server -count=1
go vet ./...
go run ./tools/architecturecheck
make comments
```

## 10. 阶段 5：应用装配与生命周期

### 目标

明确所有组件、goroutine、连接、timer、channel 和 worker 的所有者、Context、停止与等待边界。

### 范围

- `cmd` 显式构造全部必需依赖并负责启动顺序和反向关闭顺序。
- 消除必需依赖 setter、请求期懒装配和部分构造可见状态。
- 把订单刷新、批量发布、恢复、reconciliation 和实时连接等业务 worker 移出 Server transport。
- 每个后台任务提供 owner、Context 来源、Cancel、Wait/Join、超时和观测状态。
- 所有通知、平台、续期、二维码、WebSocket 和浏览器外部调用都必须响应所属 Context；无法中断的同步调用必须有明确硬超时并在清单中记录。
- 账号删除先 fencing，受限等待运行时退出，再复核归属并删除。
- Stop/Close 在契约允许时幂等；晚到写入由 generation、lease 或 token 拒绝。
- 清空 `lifecycle-inventory.md` 中的剩余风险后，将其保留为静态组件清单而非进度日志。

### 完成条件

- Server 不保存业务 worker 的锁、cancel map、任务状态或平台会话状态。
- 所有 goroutine 都能由拥有者取消并等待，不使用超时后遗留的等待 goroutine。
- 重复 Start/Stop、先 Stop、启动失败回滚、超时和晚到写入有确定性测试。
- 关闭顺序不持锁等待网络、浏览器或用户操作。
- 生命周期审计清单记录每个外部调用的取消路径、硬超时、无法取消的实现约束和可重试 Join 入口。

### 强制验证

```bash
go test ./internal/server ./internal/account ./internal/engine ./internal/automation ./internal/renewal -count=1
go test -race ./internal/server ./internal/account ./internal/engine ./internal/automation ./internal/renewal -count=1
go test ./... -count=1
go run ./tools/architecturecheck
```

## 11. 阶段 6：Engine 与 Automation

### 目标

将大型 facade 拆成状态所有权明确、可独立测试的组件，同时保持平台协议和冻结 CAPTCHA 行为不变。

### 范围

- Engine 分离连接会话、消息分发、去重、防抖、凭证协调、重试和运行状态。
- Automation 分离规则解析、调度、运行协调、动作执行、库存分配、通知和补偿。
- 建立统一账号凭证协调器：快照、版本校验、锁外外部 I/O、条件提交和冲突观测。
- 外部动作使用幂等键和 `succeeded/failed/uncertain` 三态；不确定结果进入人工核对或补偿。
- 明确凭证锁、自动化库存锁和 worker 锁的顺序。
- `Account` 与 `Center` 只保留稳定 facade，不继续拥有全部内部可变状态。

### 完成条件

- facade 的体积和字段职责显著收敛，每个并发组件有独立生命周期和 race 测试。
- 未在任何互斥锁或数据库事务内执行不可控平台、浏览器或用户等待 I/O。
- 凭证冲突、取消、超时、旧响应、重复事件和重连均有确定性测试。
- 发货、评价、求评价、卡密和通知的幂等及不确定态不发生自动重复动作。
- 所有冻结 CAPTCHA 测试通过，受保护文件没有未经授权的行为变化。

### 强制验证

```bash
go test ./internal/engine ./internal/automation ./internal/browser -count=1
go test -race ./internal/engine ./internal/automation -count=1
RUN_BROWSER_INTEGRATION=1 go test ./internal/browser -count=1
go run ./tools/architecturecheck
make comments
```

## 12. 阶段 7：React Feature 化

### 目标

完成 `app -> features -> shared` 的真实所有权拆分，而不是只移动页面文件或增加 barrel export。

### 范围

- `app` 只保留 Provider、认证壳、路由和错误边界。
- 每个 feature 拥有 page、components、hooks/state、API adapter、UI model 和行为测试。
- 拆除集中式 `frontend/services/api.ts` 和 `frontend/types.ts` 的业务职责。
- shared HTTP client 只处理超时、取消、认证和统一错误，不包含领域归一。
- 服务端数据、表单状态和短暂 UI 状态分离；派生值不重复存入 state。
- 异步 effect 使用取消或 generation，独立请求并行启动，旧响应不得覆盖新状态。
- 页面和重型可选依赖按路由/feature 懒加载，并设置初始包体预算。

### 完成条件

- feature 不导入其他 feature 内部文件，shared 不反向导入 feature。
- React 组件不直接 `fetch`/`axios`，业务 DTO 转换只在本 feature API adapter。
- 不再存在承担多个领域的集中 API/类型文件或超大 Hook。
- 关键流程具有成功、失败、取消、切换、乱序响应和卸载测试。
- 构建产物由当前源码生成并更新嵌入目录，初始包体满足预算。

### 强制验证

```bash
npm test --prefix frontend
npm run typecheck --prefix frontend
npm run comments:check --prefix frontend
npm run build --prefix frontend
make cover-frontend
git diff --check
```

## 13. 阶段 8：DB 与事务治理

### 目标

完成窄 repository、应用层 Unit of Work、批量查询写入、补偿持久化和三方言一致性。

### 范围

- SQL、方言、row model 和加密只留在 `internal/db` 与基础设施 adapter。
- 跨 repository 原子操作通过消费者定义的 Unit of Work 执行。
- 清除上层 `*sql.DB`、`*sql.Tx`、DB row 和完整 `Store` 暴露。
- 商品/订单同步使用游标、批量读取、批量 UPSERT 和明确缓存失效，消除 N+1。
- 不可逆外部动作的补偿记录、lease、幂等键和人工核对状态跨重启持久化。
- 所有迁移在 SQLite、MySQL、PostgreSQL 使用相同编号和最终 schema。
- 对高频查询提供三方言查询计划和大数据量回归。

### 完成条件

- handler、React 和领域组件不控制数据库事务。
- 跨 repository 失败原子回滚；外部成功后的本地失败进入可恢复或不确定状态。
- 三方言迁移、CRUD、事务、并发 claim、软删除、批量写入和补偿行为一致。
- 大数据量测试证明没有逐账号、逐订单或逐商品的无界查询放大。

### 强制验证

```bash
go test ./internal/db ./internal/application/... ./internal/adapter -count=1
go test -race ./internal/db ./internal/application/... ./internal/adapter -count=1
TEST_MYSQL_URL=... TEST_POSTGRES_URL=... make test-multidb
go run ./cmd/dbverify "$TEST_MYSQL_URL"
go run ./cmd/dbverify "$TEST_POSTGRES_URL"
```

## 14. 阶段 9：架构门禁与兼容退场

### 目标

把目标依赖图和兼容删除条件变为 fail-closed 门禁，删除所有临时例外和已到期兼容路径。

### 范围

- 检查 Server 的 `database/sql`、低层 import、业务 worker、裸 Store 和事务暴露。
- 检查应用 Port 是否泄露 DB/HTTP/Server/平台实现类型。
- 检查 service locator、运行时必需 setter、反射/动态 import 隐藏依赖。
- 检查前端 app/features/shared 依赖方向、组件直接请求和集中式 barrel。
- 为旧路径和兼容字段建立调用遥测、Deprecation、Sunset 版本和删除测试。
- 删除调用方已迁移的旧路由、别名、适配器、白名单和死代码。

### 完成条件

- 架构检查覆盖完整目标图且无临时白名单。
- 所有保留兼容入口都有已知调用方、遥测和明确删除版本。
- 到期旧路径删除后，前端、契约测试、Vite 代理和嵌入资产同步更新。
- `go list`、AST、TypeScript/ESLint 规则无法通过简单中转层绕过。

### 强制验证

```bash
go run ./tools/architecturecheck
go test ./... -count=1
go test -race ./internal/server ./internal/engine ./internal/automation -count=1
npm test --prefix frontend
npm run typecheck --prefix frontend
npm run build --prefix frontend
```

## 15. 阶段 10：注释与复杂度收口

### 目标

让中文注释准确描述业务、敏感性和并发约束，并把过长函数和高复杂度热点降到可维护范围。

### 范围

- 删除“保存 X”“负责 X”“表示错误”等模板化或复述语法的注释。
- 为函数、参数、返回值、变量、字段、闭包和 React 状态补充准确中文语义。
- 并发代码说明 owner、锁保护字段、锁顺序、Context、Cancel 和 Wait。
- 敏感值说明明文作用域、日志/序列化禁令和清理责任。
- 增加模板短语、复杂度和超大文件门禁；冻结文件只使用精确例外。
- 删除全部注释历史基线机制和已无必要的例外。

### 完成条件

- Go、TypeScript、TSX 无历史注释豁免，无模板化占位注释。
- 自动检查和人工抽查同时通过，注释与当前行为一致。
- 高复杂度函数被按业务责任拆分，未用注释掩盖结构问题。
- 全量测试、race、覆盖率、架构、lint、前端构建和三数据库门禁全部通过。

### 强制验证

```bash
make comments
make check
make cover
make cover-browser
make cover-frontend
TEST_MYSQL_URL=... TEST_POSTGRES_URL=... make test-multidb
npm run build --prefix frontend
```

## 16. 迭代强制约束

### 16.1 执行单位与提交

1. 阶段 1 至阶段 10 各是一个完整迭代、一个阶段级 PR 和一个最终交付提交。
2. 阶段可同时修改多个 Go 包、多个前端 feature、测试、架构门禁和文档；不得把阶段内工作再拆成独立交付、独立验收或独立 PR。
3. 本地可以使用临时提交或工作区操作，阶段内部允许暂时不可编译；最终阶段提交必须可编译、可启动、可测试并通过全部适用门禁。
4. 阶段未满足全部完成条件时不得标记为“已完成”，也不得以大提交掩盖未完成分支、错误路径或覆盖率缺口。
5. 不改写已有 Git 历史，不覆盖无关工作树修改，不以重建分支代替合并和冲突审计。
6. 仍禁止无关重命名、全仓格式化、削弱/删除测试与覆盖率、扩大兼容白名单和修改冻结 CAPTCHA。
7. 阶段完成证据必须绑定最终阶段提交和完整命令输出；中间提交、局部命令或先前声明不能单独关闭阶段。

### 16.2 行为与兼容

1. 重构默认不改变业务行为、HTTP 状态、JSON 字段、重试、超时、幂等或错误优先级。
2. 与 `main` 比较必须使用最新远端目标；先分析文本冲突，再用契约测试判断功能冲突。
3. 空集合统一输出 `[]`，前端兼容当前仍可能存在的 `null` 和历史包裹对象。
4. 新 API 使用 `/api/v1` 和具名 DTO；禁止新增动态 map 响应或 `HTTP 200 + success:false`。
5. 兼容适配器必须记录调用方、删除条件和 Sunset 版本；条件满足前不得删除。
6. 前端源码变化后必须重新构建 `internal/webui/static`，禁止手改哈希资产。

### 16.3 敏感数据

1. 摘要和所有权检查不得读取或解密 Cookie、Token、密码或加密 metadata。
2. 平台凭证和登录秘密使用不同模型及目的明确的 repository 方法。
3. 敏感持久化模型不得进入 HTTP、前端状态、日志、通知、错误或测试失败输出。
4. 管理/一次性敏感读取必须审计且 fail closed，高频运行时使用窄读取。
5. 不得持凭证锁执行未受规范保护的慢速网络、浏览器或用户等待 I/O。

### 16.4 数据库与事务

1. 上层不得新增裸 `*sql.DB`、`*sql.Tx` 或完整 Store；跨 repository 原子操作使用应用 Unit of Work。
2. SQL row、持久化模型、应用模型和 HTTP DTO 不得合并为同一便利类型。
3. 数据库变化必须同时维护 SQLite、MySQL、PostgreSQL 编号和最终 schema。
4. 迁移、方言 SQL、claim、锁、批量写入或补偿变化必须执行 Docker 三数据库回归。
5. 禁止通过逐行查询、无界并发或扩大事务范围掩盖 repository 设计问题。

### 16.5 生命周期与并发

1. goroutine、timer、channel、WebSocket、browser 和 worker 必须有 owner、Context、Cancel、Wait/Join。
2. 必需依赖由构造器输入并在 Start 前验证；禁止生产路径使用 setter 补齐必需依赖。
3. Stop/Close 按契约幂等，超时不得制造新的游离等待 goroutine。
4. channel 由约定发送方关闭；晚到响应由 generation、lease、版本或 token fencing 拒绝。
5. 锁保护字段和锁顺序必须注释并测试；禁止持锁等待不可控外部 I/O。
6. 外部动作必须有幂等键、成功/失败/不确定三态、补偿、重试和人工核对边界。

### 16.6 React

1. 依赖方向固定为 `app -> features -> shared`；feature 不导入其他 feature 内部文件。
2. 组件不直接请求网络，shared HTTP client 不包含领域归一逻辑。
3. 服务端数据、表单状态和短暂 UI 状态分离；可派生值不重复存 state。
4. 异步 effect/request 必须可取消或有 latest-generation 保护；独立请求并行启动。
5. 不因习惯增加 memo；重型页面和可选依赖按路由/feature 懒加载。
6. 新或重大流程必须覆盖成功、失败、取消、切换、乱序和卸载，不用源码字符串测试替代行为测试。

### 16.7 冻结 CAPTCHA

1. 未经用户在当前任务明确授权，不得编辑、移动、格式化、回退或间接改变受保护文件和调用链。
2. 不得改变选择器、258px 标准距离、轨迹、时序、fresh `x5sec`、重试、profile、Cookie 合并、
   Playwright/CDP 顺序、启动参数或结果标签。
3. 发现与基线差异时只做只读审计；任何行为调整必须同时更新实现、全部测试和冻结规范。
4. 涉及 browser、登录、凭证、engine、account 或 server 调用路径时必须运行冻结规范要求的验证。

### 16.8 注释、测试与覆盖率

1. 所有新增或修改的 Go/TypeScript/TSX 函数和变量必须有准确中文注释。
2. 禁止“err 表示错误”“变量 X 保存 X”等模板化注释，自动检查通过不能替代语义审查。
3. 每个确定性函数、分支和错误路径必须有聚焦测试；禁止删除、跳过或放宽测试来通过重构。
4. 测试可跳过的范围仅限真实账号、私有平台状态或不可用外部服务；本地浏览器和数据库不得假跳过。
5. 覆盖率声明必须写明命令、浏览器开关、Go/前端百分比和真实外部例外，但不得写入本计划。

## 17. 阶段统一验收矩阵

| 变化范围 | 最低验证 |
| --- | --- |
| 任意 Go | 聚焦测试、`go test ./... -count=1`、`go vet ./...`、架构、Go 注释、`git diff --check` |
| HTTP/应用服务 | Server 与应用契约测试、错误/越权/取消测试、Server race |
| Engine/Automation/生命周期 | 聚焦 race、Stop/Close/超时/晚到写入、全量 Go test |
| 数据库/事务/迁移 | SQLite 聚焦、Docker MySQL/PostgreSQL、`cmd/dbverify`、并发 claim/回滚 |
| React/前端 API | Vitest、typecheck、前端注释、V8 coverage、生产构建、嵌入资产校验 |
| Browser/登录/凭证 | 冻结测试、浏览器集成、凭证 race、日志脱敏检查 |
| 阶段完成 | 该阶段全部强制验证 + `make check` + 适用覆盖率，不允许只凭单项门禁关闭 |

## 18. 计划维护规则

1. 本文只允许修改目标、阶段范围、状态、验收条件、固定顺序和强制约束。
2. 禁止在本文新增阶段内部的临时提交、逐文件进度或人员记录；阶段级完成证据维护在 `refactoring-progress.md`。
3. 阶段内临时发现写入 PR 描述或 issue，不写入总计划；只有改变阶段范围或验收条件时才更新本文。
4. 阶段完成时只更新一次状态表，并在验收记录中一次性写入最终提交、完整命令输出、覆盖率和下一阶段入口。
5. 若阶段被阻塞，只将状态改为 `阻塞`；阻塞原因和尝试过程留在任务报告，不写入本文。
