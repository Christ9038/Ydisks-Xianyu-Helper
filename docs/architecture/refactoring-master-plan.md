# Ydisks 闲鱼助手重构总计划

## 唯一治理规则

本文是唯一的阶段状态、顺序和验收依据。AGENTS.md 只负责要求读取本文；refactoring-progress.md 只
保存最终提交与验收证据；其他架构文档只记录领域约束，不定义阶段状态。

本重构只有六个正式阶段。旧的十阶段编号、阶段 0、纵向切片、当前切片、分轮迭代、局部完成声明和
一个改动一个提交的规则全部失效。阶段是唯一任务、评审和交付单位：一个阶段允许同时修改多个 Go
包、多个前端 feature、测试、门禁、文档和嵌入产物；agent 不得拆分子任务、垂直切片、独立 PR 或
中间提交。阶段最终提交必须可编译、可启动、可测试并通过全部强制验证。阶段完成前不得创建提交，
最终只创建一条中文阶段提交。

禁止无关重命名、全仓格式化、依赖升级、削弱测试或覆盖率、扩大兼容白名单、修改默认 :59188、改变
远程首次初始化行为，以及修改冻结 CAPTCHA 文件、测试、参数、调用顺序或规范。

## 基础验证层（非正式阶段）

完整架构门禁必须在业务迁移前建立，不属于阶段六的末尾补救工作。`tools/architecturecheck` 预先登记阶段二
至阶段六的全部规则，并从本文状态表读取唯一“当前阶段”：当前阶段及全部前序规则立即 fail-closed，后续
规则保持已定义但暂不阻断。状态表缺失、重复当前阶段或无法解析时，门禁自身失败；阶段六明确完成后
即使不再存在“当前阶段”，全量门禁仍永久开启。

门禁目录固定覆盖：Server 组合根与平台旁路、worker 生命周期与根 Context、React feature/契约/网络边界、
上层裸数据库与事务泄露、复杂度与超大文件、兼容登记与 Sunset。阶段只负责激活规则并清零真实违规，
不得到阶段执行时才临时设计验收逻辑。

## 目标边界

cmd 只处理配置、依赖构造、信号和生命周期。internal/server 只处理 HTTP/WebSocket transport、鉴权、
中间件、具名 DTO、错误映射和 SPA。应用服务拥有用例、所有权、事务和补偿，不依赖 HTTP、Server、
DB 或平台实现。internal/db 独占 SQL、方言、迁移、加密和 repository。React 使用 app -> features ->
shared，feature adapter 直接转换领域 DTO 为 feature-owned UI model。

## 当前状态

状态只能是 待验收、当前阶段、待执行、阻塞、已完成；已完成必须绑定最终中文提交和完整命令输出。

| 阶段 | 状态 | 严格结论 |
| --- | --- | --- |
| 1. 稳定性、安全和启动生命周期 | 已完成（既有基线） | 已按当前工作树复核原子 data key、同步 Bind、启动回滚、无秘密告警及该阶段命令。六阶段治理建立时这些改动已经存在；禁止为纠正历史提交名重新提交或改写历史。 |
| 2. Server 组合根和应用服务迁移 | 已完成 | `internal/composition` 已成为唯一生产组合根；旧 Server 组合根与生命周期反转已删除。Server 仅接收经校验的消费者 Port，阶段二 AST 门禁和完整验收均已通过。最终提交绑定见 `refactoring-progress.md`。 |
| 3. 生命周期、Engine 和 Automation 重新验收 | 已完成 | 已完成后台任务 owner/Context/Cancel/Wait 收口、同步凭证更新的调用 Context 传递、浏览器组合根生命周期接入和 AST 生命周期门禁；最终提交与完整证据见 `refactoring-progress.md`。 |
| 4. React Feature 化和异步状态修复 | 已完成 | 已完成领域契约直接模块、feature API adapter 边界、统一 ApiError、items 地图迁移、定位取消/超时、批量轮询代次隔离、严格 TypeScript 检查和嵌入产物重建；最终证据见 `refactoring-progress.md`。 |
| 5. DB 与事务治理重新验收 | 当前阶段 | 仅允许开展上层裸数据库、Unit of Work、claim/lease/补偿和多数据库验收；不得提前进入阶段六收口。 |
| 6. 全量架构、兼容退场和注释收口 | 待执行 | 门禁规则已在基础验证层预建；本阶段只激活最终质量规则、清零违规并完成兼容和注释退场。 |

顺序固定为 1 -> 2 -> 3 -> 4 -> 5 -> 6。阶段一是治理建立前的既有基线例外；从阶段二开始，每阶段必须有唯一最终中文提交和完整原始命令输出。阶段四已完成，阶段五是唯一允许继续执行的阶段；阶段六不得提前执行。

## 阶段一：稳定性、安全和启动生命周期

范围：data key 使用 O_CREATE|O_EXCL，覆盖竞争、空文件、写失败和读竞争；Bind 先于 worker 和
Serve；覆盖重复 Start/Stop、已 Bind 未 Serve、启动回滚；修复 SA1012；非 loopback 未初始化只输出
不含秘密的高等级告警并更新部署文档。

~~~text
go test ./cmd/server ./internal/server ./internal/notify -count=1
go test -race ./cmd/server ./internal/server ./internal/notify -count=1
go vet ./...
make lint
git diff --check
~~~

既有基线：不追补提交，不改写历史。后续涉及其代码时，必须在所属阶段重新运行相关命令。

## 阶段二：Server 组合根和应用服务迁移（已完成）

组合根必须由独立 composition 包或等价独立组合层负责；cmd/server 只构造基础设施并调用它；
Server 只接受不可变完成依赖。

必须完成：
- 移出 internal/server/application_services.go 中所有 service、runner、coordinator 和 adapter factory 的生产构造。
- 删除 server.NewApplicationServices 业务组合根、Server.ApplicationServices() 和 LifecycleComponents() 生命周期反转。
- Server 不得持有或调用 MTOP、长登录、QR 登录、Manager、automation、Notifier、DB、Store、session 或具体平台 implementation。
- 订单运行时、聊天发送、批量发布、账号登录、Session 恢复、通知测试发送、聊天已读上报全部通过应用 Port。
- architecturecheck 扫描真实生产源码，禁止 Server 调用 service/runner/coordinator 构造函数和通过 adapter factory 间接构造业务服务。
- 测试覆盖缺失依赖、正常、越权、失败、取消、补偿、启动回滚和独立关闭。

### 执行形态（对后续 agent 的硬性指导）

1. 新建独立组合层（建议 `internal/composition`）；它是唯一允许同时导入 `internal/application`、
   `internal/adapter`、`internal/account`、`internal/automation`、`internal/browser`、`internal/notify`
   与平台依赖的生产装配位置。`cmd/server/main.go` 只构造基础设施、调用组合层、登记其返回的生命周期
   组件并启动 HTTP Server。
2. 将 `internal/server/application_services.go` 的构造函数、`ApplicationServiceDependencies`、生命周期
   组件生成和 callback 闭包移到组合层及对应的应用/adapter 包。完成后删除该文件，而不是改名保留；
   `internal/server` 不得再出现 `New*Service`、`New*Runner`、`New*Coordinator` 或 adapter factory 调用。
3. `server.Dependencies` 只能包含 HTTP 所需的认证、日志、静态资源、健康检查与由 transport 消费者定义的
   最小应用 Port。它不得引用 `adapter` 的平台客户端接口、`account.Manager`、`automation.Center`、
   `notify.Notifier`、`chat.Service`、`db.Store`、`sql.DB` 或 `sql.Tx`。`PlatformPort`、`mtopClient`、
   `longLoginClient`、`qrLoginService`、`sessionRecoveryCallback` 与测试专用可变平台 Port 一并删除或迁至
   应用/adapter 测试。
4. 按用例而非基础设施对象迁移 handler：二维码登录、账号登录与长登录、Cookie/session 恢复、订单运行时、
   聊天发送和已读上报、通知测试发送、商品批量发布分别接收消费者定义的应用 Port。handler 只做 DTO
   解析、当前用户提取、调用、错误到 HTTP 的映射；不得传递 `Server` callback 给 adapter。
5. 组合层返回不可变的 `server.Dependencies` 和独立 `[]lifecycle.NamedComponent`。`cmd/server` 直接向
   `lifecycle.Coordinator` 登记该列表；禁止从 Server 或 Server 持有的服务集合反向读取生命周期组件。
6. 重写 `tools/architecturecheck` 为 fail-closed AST 扫描，并增加真实仓库扫描和正反例测试：扫描全部
   非测试 `internal/server/**/*.go` 与 `cmd/server/**/*.go`，拒绝上述类型、字段、构造调用、factory 调用、
   Server callback、service locator 及测试专用 setter 的生产使用。现有仅按少量函数名匹配的检查不能作为
   阶段完成证据。
7. 所有现有 Server 测试随用例归属迁移：transport 测试只替换应用 Port fake；业务、平台、恢复和 worker
   测试进入 application/adapter。为组合层补齐缺失依赖、构造失败、启动回滚和关闭顺序测试。不得因为迁移
   删除现有错误路径、越权、取消、租约或晚到结果断言。
8. 阶段二门禁在迁移开始时即启用，且没有临时白名单：`tools/architecturecheck` 必须拒绝
   `application_services.go`、Server/cmd 中的 adapter 或业务运行时依赖、Server 平台 Port、组合根 API、
   生命周期反转、应用/adapter 构造链，以及缺失或未被 `cmd/server` 调用的 `internal/composition`。阶段内
   门禁失败表示未完成迁移，不得降级为告警；最终提交前必须归零。

~~~text
go test ./internal/application/... ./internal/adapter ./internal/server -count=1
go test -race ./internal/application/... ./internal/adapter ./internal/server -count=1
go run ./tools/architecturecheck
go vet ./...
make lint
make comments
git diff --check
~~~

最终提交：阶段二：完成 Server 组合根和应用服务迁移。

完成结论：生产装配已迁入 `internal/composition` 与其 `runtime` 子层，`cmd/server` 只构造基础设施并调用
组合层；`internal/server` 不再构造业务服务、runner、coordinator 或平台实现，也不再反向返还生命周期组件。
Server 的应用依赖由消费者定义的不可变 Port 容器承载，构造期缺失任一必需 Port 即失败。完整原始验收输出、
冻结 CAPTCHA 差异检查和最终提交绑定只记录在 `refactoring-progress.md`。

## 阶段三：生命周期、Engine 和 Automation 重新验收（已完成）

明确每个 worker 的 owner、Context、Cancel、Wait/Join；cmd 独占协调器；Server Stop 只收束 HTTP；
覆盖启动失败、取消、超时、重复关闭、晚到结果和关闭顺序；可在本阶段修复 Engine/Automation 状态问题，
但不得改动冻结 CAPTCHA。

~~~text
go test ./... -count=1
go test -race ./internal/server ./internal/engine ./internal/automation ./internal/renewal
RUN_BROWSER_INTEGRATION=1 go test ./internal/browser -count=1
make comments
go run ./tools/architecturecheck
~~~

最终提交：阶段三：完成生命周期、Engine 和 Automation 重新验收。

## 阶段四：React Feature 化和异步状态修复（当前阶段）

这是一个完整前端阶段，不拆 HTTP 错误、契约、地图、轮询或构建交付：
- ApiError 保留 status、code、message、request_id、details、payload；JSON/FormData 共用错误解析。
- shared/api-contract/index.ts 拆为 session、accounts、items、orders、automation、notifications、settings、chat、cards、admin、common 直接模块；feature adapter 直接导入领域契约并输出 UI model。
- 地图物理迁入 items；locateForPublish 等待定位和搜索；每次请求 AbortController + generation + timeout，旧响应不得写回。
- pending、running、canceling 统一视为进行中；每个轮询 effect 独立取消；覆盖重启、关闭重开、晚到响应、取消和卸载。
- 清理未使用符号并开启 noUnusedLocals/noUnusedParameters；Vite 重建 internal/webui/static。

~~~text
npm test --prefix frontend
npm run typecheck --prefix frontend
npm run comments:check --prefix frontend
npm run build --prefix frontend
make cover-frontend
git diff --check
~~~

最终提交：阶段四：完成 React Feature 化和异步状态修复。

## 阶段五：DB 与事务治理重新验收

清除上层 Store.DB、sql.DB、sql.Tx 和 row model 泄露；跨 repository 原子操作使用 Unit of Work；验证
claim、lease、取消、重试、不确定远程结果和本地补偿；补齐 SQLite 并发和事务测试，并运行 MySQL、
PostgreSQL、cmd/dbverify。任何发现的 repository、迁移或补偿缺陷必须在本阶段同一提交中完成。

~~~text
go test ./internal/db ./internal/application/... ./internal/adapter -count=1
go test -race ./internal/db ./internal/application/... ./internal/adapter -count=1
TEST_MYSQL_URL=... TEST_POSTGRES_URL=... make test-multidb
go run ./cmd/dbverify "$TEST_MYSQL_URL"
go run ./cmd/dbverify "$TEST_POSTGRES_URL"
git diff --check
~~~

最终提交：阶段五：完成数据库与事务治理重新验收。

## 阶段六：全量架构、兼容退场和注释收口

门禁框架和全部阶段规则已经前置建立，本阶段不得重新发明或补写基础规则。本阶段激活最终质量门禁，
处理此前各阶段持续开启的全部边界，并清零 service locator、动态 import、前端依赖方向、根级 services
旁路、模板注释、复杂度和超大文件违规。兼容入口只在遥测、Sunset、调用方迁移和契约测试齐备后删除；
不得扩大 baseline、增加白名单或跳过测试。

~~~text
go test ./... -count=1
go vet ./...
make lint
make comments
go run ./tools/architecturecheck
go test -race ./internal/server ./internal/engine ./internal/automation
npm test --prefix frontend
npm run typecheck --prefix frontend
npm run comments:check --prefix frontend
npm run build --prefix frontend
make cover
make cover-frontend
git diff --check
~~~

适用时还运行浏览器集成、MySQL/PostgreSQL 和两个 cmd/dbverify 命令。最终提交：阶段六：完成全量架构、
兼容退场和注释收口。

## 执行纪律

1. 开始一个阶段前，只允许读取总计划状态表并审查当前工作树；不得根据旧阶段编号或已落地代码跳阶段。
2. 阶段进行期间不创建提交、不更新阶段状态、不写局部完成记录，也不向后续阶段交付任何实现。
3. 阶段最终验收失败时，状态仍是当前阶段；修复全部失败项后重新执行完整命令，不得只重跑失败命令。
4. 仅在全部命令成功后，按一次操作更新状态表、验收记录、完成证据和下一阶段入口，再创建唯一中文提交。
5. 已存在的正确实现只能作为后续阶段的输入；不能单独改变正式阶段状态，也不得被回滚、清理或拆分重做。

## 门禁激活规则

1. 全部阶段规则在基础验证层预先写入自动门禁并具备正反例测试；每个阶段开始时只激活对应规则，之后才允许迁移生产代码。
2. 当前阶段门禁必须 fail-closed。阶段内出现违规是未完成工作的可见清单，不得通过白名单、baseline、忽略目录、字符串规避、反射、动态 import 或降低严重级别使其通过。
3. 前序阶段已完成的门禁继续保持启用。后续阶段专有门禁只能在其成为当前阶段时启用，避免以尚未执行的设计阻断当前阶段；但任何全局安全、冻结 CAPTCHA、依赖方向、测试和注释规则始终生效。
4. 阶段最终验收只接受“当前阶段及全部既有门禁均为零违规”的完整输出。门禁失败时不得更新阶段状态、完成证据或创建提交。
