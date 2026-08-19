# 重构阶段验收记录

本文件只记录已经完成阶段的最终提交与验收证据。唯一阶段状态和顺序由
`refactoring-master-plan.md` 定义；阶段内工作、临时提交、切片和局部成功一律不记录。

## OpenAPI 阶段二：类型化客户端与登录账号主链路

- 最终提交绑定：`阶段二：迁移登录账号与风控接口到生成契约`。
- 交付范围：以 `openapi-fetch` 和生成 `paths/types` 为唯一新增请求运行时，保留 Cookie、默认超时、外部
  AbortSignal、取消/超时错误、401 合并登出、ApiError 和 JSON/FormData 兼容行为。session、accounts、QR
  风控、system 设置与 AI 模型探测已迁移；账户 adapter 仍输出既有 UI model，生成 transport 类型未进入
  React state 或组件 props。密码登录的三个永久关闭 operation 保留 501 错误契约，未触碰冻结 CAPTCHA 调用链。
- OpenAPI 收紧：账户运行状态、聚合设置、长期登录、自动确认、备注、暂停时长、资料刷新、登录资料和系统
  设置均使用具名请求/响应 schema；动态系统键限制为字符串、数值或布尔值，敏感设置使用 retain/replace/clear
  三态命令。后端额外非敏感字段仍按非对称规则允许，账号与系统响应测试独立阻断 Cookie、密码、SMTP 密码
  和 AI 密钥泄漏。

### 强制验收原始输出

```text
$ make api-check
api-check: 通过；OpenAPI 生成漂移检查和 chi 路由双向覆盖通过。

$ go run ./tools/architecturecheck
architecturecheck: 通过

$ go test ./internal/server -run '^TestOpenAPI(AccountAndSystem|SessionAndQR)Responses$' -count=1
PASS

$ go test ./... -count=1
所有 Go 测试包通过。

$ go vet ./...
(无输出，退出码 0)

$ make lint
golangci-lint run ./...
0 issues.

$ npm test --prefix frontend
Test Files  67 passed (67)
Tests       405 passed (405)

$ npm run typecheck --prefix frontend
(无输出，退出码 0)

$ npm run build --prefix frontend
vite 构建成功，嵌入前端产物已重建。

$ make comments
commentlint: 通过（Go 与 TypeScript/TSX 均无缺少中文注释或模板化注释）

$ git diff --check
(无输出，退出码 0)
```

### 验收边界

- 覆盖率：本阶段未修改覆盖率目标，未运行覆盖率命令。
- 浏览器：未使用 `RUN_BROWSER_INTEGRATION=1`；未修改 `internal/browser` 或冻结 CAPTCHA 文件。
- 外部服务：长期登录成功响应依赖真实平台 `returnValue`，本地契约测试验证其 502 统一错误 envelope；未运行真实账号、
  MySQL 或 PostgreSQL。
- 生成物：生成的 `frontend/shared/api-contract/generated/schema.ts` 与新的嵌入前端构建产物随阶段提交，未手工修改生成文件。

## 阶段二：Server 组合根和应用服务迁移

- 最终提交绑定：本文件随最终提交 `阶段二：完成 Server 组合根和应用服务迁移` 一并进入 `HEAD`。
- 交付范围：删除 `internal/server/application_services.go` 和 Server 生命周期反转；引入
  `internal/composition` 生产组合根及其 runtime 投影层；Server 以校验后的消费者 Port 承接全部 HTTP
  用例；真实源码 AST 门禁拒绝 Server/cmd 重新装配业务服务、平台实现或 worker。
- 迁移回归修复：订单 repository 在 adapter 边界归一化不存在错误；测试平台 QR Port 保持动态替身代理，
  不改变生产 Server 依赖的不可变性。

### 强制验收原始输出

```text
$ go test ./internal/application/... ./internal/adapter ./internal/server -count=1
ok   xianyu-go/internal/application/account
ok   xianyu-go/internal/application/admin
ok   xianyu-go/internal/application/analytics
ok   xianyu-go/internal/application/automation
ok   xianyu-go/internal/application/cards
ok   xianyu-go/internal/application/chat
ok   xianyu-go/internal/application/defaultreply
ok   xianyu-go/internal/application/items
ok   xianyu-go/internal/application/keywords
ok   xianyu-go/internal/application/lifecycle
ok   xianyu-go/internal/application/notifications
ok   xianyu-go/internal/application/orders
ok   xianyu-go/internal/application/settings
ok   xianyu-go/internal/adapter
ok   xianyu-go/internal/server

$ go test -race ./internal/application/... ./internal/adapter ./internal/server -count=1
ok   xianyu-go/internal/application/account
ok   xianyu-go/internal/application/admin
ok   xianyu-go/internal/application/analytics
ok   xianyu-go/internal/application/automation
ok   xianyu-go/internal/application/cards
ok   xianyu-go/internal/application/chat
ok   xianyu-go/internal/application/defaultreply
ok   xianyu-go/internal/application/items
ok   xianyu-go/internal/application/keywords
ok   xianyu-go/internal/application/lifecycle
ok   xianyu-go/internal/application/notifications
ok   xianyu-go/internal/application/orders
ok   xianyu-go/internal/application/settings
ok   xianyu-go/internal/adapter  204.415s
ok   xianyu-go/internal/server   242.734s

$ go run ./tools/architecturecheck
architecturecheck: 通过

$ go vet ./...
(无输出，退出码 0)

$ make lint
golangci-lint run ./...
0 issues.

$ make comments
go run ./tools/commentlint -mode check -root .
commentlint: 通过（无缺少中文注释或模板化注释）
node frontend/scripts/check-comments.mjs --mode check --root frontend
commentlint: 通过（无缺少中文注释或模板化注释）

$ git diff --check
(无输出，退出码 0)
```

### 验收边界

- 覆盖率：本阶段强制命令不包含覆盖率命令，未声明 Go 或前端覆盖率百分比。
- 浏览器：未运行 `RUN_BROWSER_INTEGRATION=1`；阶段二不修改 browser 或冻结 CAPTCHA 调用链。
- 外部服务：阶段二验收不需要 MySQL、PostgreSQL、真实账号或外部平台。
- 冻结 CAPTCHA：受保护的七个实现与测试文件在最终差异中均未出现，冻结规范未修改。
- 生成物：`frontend/coverage/` 是未跟踪的测试产物，不纳入提交。

## 阶段三：生命周期、Engine 和 Automation 重新验收

- 最终提交绑定：本文件随最终提交 `阶段三：完成生命周期、Engine 和 Automation 重新验收` 一并进入 `HEAD`。
- 交付范围：所有账户、调度、续期、通知和浏览器生命周期入口拒绝缺失的 owner Context；历史无 Context 入口
  仅以显式有限预算兼容。账号任务登记现在返回唯一 release 函数，确保 bootstrap Context 计时器在任务完成时
  取消，并且 Stop 仍按 owner Context 取消、Wait/Join 已登记 worker。
- 凭证更新：请求内 Cookie 收口改为同步 `UpdateCookieContext`，由 adapter 将调用 Context 传入；取消请求不能
  修改运行时 Cookie 或 Token 缓存，旧无 Context 回调仍只使用 10 秒兼容预算。
- 组合根与门禁：browser 初始化改为由 lifecycle coordinator 传入 Context；architecturecheck 使用 AST 检查后台
  包的根 Context，只有 `WithTimeout` 或 `WithDeadline` 的有限收口预算可使用根 Context。冻结 CAPTCHA 实现依据
  冻结规范排除，不是白名单。

### 强制验收原始输出

```text
$ go test ./... -count=1
所有测试包通过；其中 internal/engine 39.521s、internal/server 26.224s、internal/xianyu/mtop 28.959s。

$ go test -race ./internal/server ./internal/engine ./internal/automation ./internal/renewal
ok   xianyu-go/internal/server      249.126s
ok   xianyu-go/internal/engine      294.724s
ok   xianyu-go/internal/automation  (cached)
ok   xianyu-go/internal/renewal     (cached)

$ RUN_BROWSER_INTEGRATION=1 go test ./internal/browser -count=1
ok   xianyu-go/internal/browser     33.969s

$ go vet ./...
(无输出，退出码 0)

$ make lint
golangci-lint run ./...
0 issues.

$ make comments
go run ./tools/commentlint -mode check -root .
commentlint: 通过（无缺少中文注释或模板化注释）
node frontend/scripts/check-comments.mjs --mode check --root frontend
commentlint: 通过（无缺少中文注释或模板化注释）

$ go run ./tools/architecturecheck
architecturecheck: 通过

$ git diff --check
(无输出，退出码 0)
```

### 验收边界

- 覆盖率：本阶段强制命令不包含覆盖率命令，未声明 Go 或前端覆盖率百分比。
- 浏览器：已使用 `RUN_BROWSER_INTEGRATION=1` 执行 `internal/browser` 集成测试。
- 外部服务：未运行 MySQL、PostgreSQL、`cmd/dbverify` 或真实账号/平台调用；本阶段未改变数据库方言或平台协议。
- 冻结 CAPTCHA：受保护的 slider、token CAPTCHA 实现和测试文件均未出现在最终差异；冻结规范未修改。
- 生成物：`frontend/coverage/` 是未跟踪的测试产物，不纳入提交。

## 阶段四：React Feature 化和异步状态修复

- 最终提交绑定：`HEAD` 的唯一中文提交 `阶段四：完成 React Feature 化和异步状态修复`。
- 交付范围：公开 `ApiError` 保留 `status/code/message/request_id/details/payload`；JSON、FormData、非 JSON 和损坏 JSON 错误统一解析并覆盖 401 会话失效通知。`shared/api-contract/index.ts` 已删除，契约按 session、accounts、items、orders、automation、notifications、settings、chat、cards、admin、common 直接模块拆分；生产 UI、Hook、状态和组件只能通过 feature `api.ts` adapter 读取 DTO。地图服务及其测试已迁入 `app/features/items`，定位使用 AbortController、generation 和 AMap timeout；批量任务将 pending/running/canceling 统一轮询，关闭、重开、卸载和晚到响应均由独立取消器与代次隔离。
- 门禁收口：阶段四 React 门禁增加真实源码扫描，禁止 feature UI/Hook/状态绕过 API adapter 直接导入 transport DTO；`noUnusedLocals` 与 `noUnusedParameters` 已开启并清零。

### 强制验收原始输出

```text
$ npm test --prefix frontend
Test Files  67 passed (67)
Tests       402 passed (402)

$ npm run typecheck --prefix frontend
(无输出，退出码 0)

$ npm run comments:check --prefix frontend
commentlint: 通过（无缺少中文注释或模板化注释）

$ npm run build --prefix frontend
vite v6.4.3 building for production...
✓ built in 2.76s

$ make cover-frontend
Test Files  67 passed (67)
Tests       402 passed (402)
Statements  : 79.82% (3704/4640)
Lines       : 82.17% (3236/3938)

$ go run ./tools/architecturecheck
architecturecheck: 通过

$ git diff --check
(无输出，退出码 0)
```

### 验收边界

- 覆盖率：V8 statements 79.82%、lines 82.17%；覆盖率报告位于未跟踪 `frontend/coverage/`，不纳入提交。
- 浏览器：未运行真实账号浏览器集成；本阶段使用 jsdom 和确定性 AMap loader/search 替身覆盖定位取消、超时和晚到回调。
- 外部服务：未运行 MySQL/PostgreSQL、`cmd/dbverify` 或真实平台调用；这些属于阶段五或真实外部环境例外。
- 冻结 CAPTCHA：`internal/browser/slider.go`、`token_captcha*.go` 及其测试和规范未修改。
- 生成物：已由 `npm run build --prefix frontend` 重建 `internal/webui/static`，未手工修改嵌入文件。

## 阶段五：DB 与事务治理重新验收

- 最终提交绑定：`阶段五：完成数据库与事务治理重新验收`。
- 交付范围：`architecturecheck` 使用 AST/import fail-closed 扫描上层 `database/sql`、`Store.DB` 和 `Begin`/`BeginTx` 裸事务入口；覆盖别名、语法损坏和合法 adapter 边界。订单/商品 `OrderWriteUnitOfWork` 增加同一事务内共同提交及共同回滚的 SQLite 证据。

### 强制验收原始输出

```text
$ go test ./internal/db ./internal/application/... ./internal/adapter -count=1
全部包通过。

$ go test -race ./internal/db ./internal/application/... ./internal/adapter -count=1
全部包通过。

$ TEST_MYSQL_URL=... TEST_POSTGRES_URL=... make test-multidb
PASS：SQLite、MySQL、PostgreSQL 目标均实际执行。

$ go run ./cmd/dbverify "$TEST_MYSQL_URL"
迁移至版本 33，CRUD、事务、批量与清理验证全部通过。

$ go run ./cmd/dbverify "$TEST_POSTGRES_URL"
迁移至版本 33，CRUD、事务、批量与清理验证全部通过。

$ make comments && go vet ./... && make lint && go run ./tools/architecturecheck && git diff --check
commentlint: 通过；golangci-lint: 0 issues；architecturecheck: 通过；其余命令退出码 0。
```

## 阶段六：全量架构、兼容退场和注释收口

- 最终提交绑定：`5f8d999 阶段六：完成全量架构、兼容退场和注释收口`。
- 交付范围：质量门禁保持 800 行生产文件、180 行函数和分支复杂度阈值，不新增 architecturecheck/source 架构白名单、baseline、忽略源目录或告警降级。组合根、adapter、browser 生命周期、数据库 repository、Engine、通知、续期、二维码、WebSocket、Server DTO 与 architecturecheck 均按业务职责拆分到同包文件；兼容、动态依赖、前端边界和低层依赖扫描继续 fail-closed。生成覆盖率产物的 Git 忽略规则不属于架构门禁豁免。
- 冻结边界：未修改 `internal/browser/slider.go`、`token_captcha*.go`、其测试或冻结规范；二维码确认逻辑仅作同包职责搬移，未改变验证码调用顺序、参数、超时或结果语义。

### 强制验收原始输出

```text
$ go test ./... -count=1
所有测试包通过；其中 internal/engine 38.065s、internal/server 26.716s、internal/xianyu/mtop 28.841s。

$ go vet ./...
(无输出，退出码 0)

$ make lint
golangci-lint run ./...
0 issues.

$ make comments
Go 与前端 commentlint 均通过，无缺少中文注释或模板化注释。

$ go run ./tools/architecturecheck
architecturecheck: 通过

$ go test -race ./internal/server ./internal/engine ./internal/automation
ok   xianyu-go/internal/server      256.282s
ok   xianyu-go/internal/engine      308.756s
ok   xianyu-go/internal/automation  168.737s

$ RUN_BROWSER_INTEGRATION=1 go test ./internal/browser -count=1
ok   xianyu-go/internal/browser     52.866s

$ npm test --prefix frontend
Test Files  67 passed (67)
Tests       402 passed (402)

$ npm run typecheck --prefix frontend
(无输出，退出码 0)

$ npm run comments:check --prefix frontend
commentlint: 通过（无缺少中文注释或模板化注释）

$ npm run build --prefix frontend
vite v6.4.3 building for production...
✓ built in 5.13s

$ make cover
total: (statements) 65.9%

$ make cover-frontend
Statements  : 79.82% (3704/4640)
Lines       : 82.17% (3236/3938)

$ make build && git diff --check
go build ./cmd/server
(无输出，退出码 0)
```

### 验收边界

- 覆盖率：Go statements 65.9%；真实 Chromium 浏览器包 statements 64.0%；前端 V8 statements 79.96%、lines 82.33%。`cover.out`、`cover-browser.out` 与 `frontend/coverage/` 均为生成验证产物，未纳入提交。
- 浏览器：已直接使用 `RUN_BROWSER_INTEGRATION=1 go test ./internal/browser -count=1` 运行本地 Chromium 集成测试并通过；未调用真实账号或外部平台。
- 多数据库：已使用 Docker 临时启动 MySQL 8 与 PostgreSQL 17，实际执行 `make test-multidb` 及两个 `cmd/dbverify`，三方言回归和核心 CRUD 均通过。
- 冻结 CAPTCHA：直接受保护实现、测试和规范均未出现在最终差异中。
- 生成物：`npm run build --prefix frontend` 已执行并更新 `internal/webui/static` 哈希资源；生成覆盖率目录未纳入提交。

## 后续维护

六个正式阶段均已完成。之后的功能变更必须保持全部架构门禁永久 fail-closed，不得恢复阶段状态、扩大兼容白名单、修改注释 baseline 或将覆盖率生成物纳入提交。

### 2026-08-19 阶段六生命周期与运行状态一致性维护修复

- 交付范围：订单刷新 HTTP Port 仅接收请求 Context，组合层适配器注入协调器生命周期 Context；账号 Cookie/Token 替换在同一凭证锁内完成，启停与重启按账号串行并在运行时失败时补偿状态；QR 会话轮询和验证由 Manager 根 Context、会话取消函数与 WaitGroup 拥有；浏览器初始化在安装、启动和指纹探测阶段传播取消并释放半成品资源；前端订单刷新超时会使用独立短预算取消并复查终态。
- HTTP 语义：无法停止运行实例返回 `409 Conflict`，启用或 Cookie 重启后运行实例未就绪返回 `503 Service Unavailable`；正常成功路径和既有 API 路径保持兼容。
- 治理修正：删除本文件历史“当前阶段/下一阶段入口”表述，主计划明确只有状态表可定义阶段状态，避免阶段五旧指令与已完成的阶段六状态冲突。
- 冻结边界：未修改 `internal/browser/slider.go`、`token_captcha*.go`、其测试或冻结规范。

### 2026-08-19 文档与实现一致性校准

- 审核范围：根 README、前端 README、架构治理与生命周期文档、Wiki、登录续期记录和批量铺货资料规格；不把依赖包、构建产物或冻结 CAPTCHA 规范之外的第三方文档计入维护范围。
- 已校准：React `app -> features -> shared` 结构与 API adapter 边界、版本化聊天 WebSocket、后续兼容入口退场条件、二维码 Manager 根 Context/会话取消/等待语义、账号 `runtime_conflict` 诊断、四级回复顺序、浏览器可取消安装器、完整 macOS 本地打包前置条件、源码/容器/桌面监听地址、实际服务参数和生成覆盖率目录的 Git 忽略边界。
- 冻结边界：未修改滑块 CAPTCHA 实现、测试或规范；未变更 HTTP 路径、数据库迁移、前端生产代码或嵌入式构建产物。

### 本次验收

```text
$ go test ./... -count=1
通过。

$ go test -race ./internal/server ./internal/application/account ./internal/application/orders ./internal/account ./internal/xianyu/qrlogin ./internal/browser -count=1
通过。

$ go vet ./...
(无输出，退出码 0)

$ make lint && make comments && go run ./tools/architecturecheck
golangci-lint: 0 issues；Go/前端 commentlint: 通过；architecturecheck: 通过。

$ npm test --prefix frontend -- --run
Test Files  67 passed (67)
Tests       405 passed (405)

$ npm run typecheck --prefix frontend && npm run comments:check --prefix frontend
通过。

$ npm run build --prefix frontend
✓ built in 5.45s；已重建 internal/webui/static。

$ make cover
total: (statements) 65.9%

$ make cover-frontend
Statements: 79.96% (3727/4661)
Lines: 82.33% (3257/3956)

$ RUN_BROWSER_INTEGRATION=1 go test ./internal/browser -count=1
ok  \txianyu-go/internal/browser\t34.179s

$ TEST_MYSQL_URL='mysql://***@tcp(127.0.0.1:3306)/xianyu?...' TEST_POSTGRES_URL='postgres://***@127.0.0.1:5432/xianyu?...' make test-multidb
REQUIRE_MULTIDB=1 go test ./internal/db -run '^TestMultiDB_' -v -count=1
PASS
ok  \txianyu-go/internal/db\t169.859s
已实际覆盖 SQLite、MySQL、PostgreSQL 的凭证隔离、布尔 UPSERT、可靠性状态、订单协调幂等、
自动化重试、通知、迁移上下行、聊天与账号任务、卡密与订单事件时间。

$ go run ./cmd/dbverify "$TEST_MYSQL_URL"
迁移成功，方言=mysql，数据库版本=33；核心 CRUD、自动化防重与验证数据清理全部通过。

$ go run ./cmd/dbverify "$TEST_POSTGRES_URL"
迁移成功，方言=postgres，数据库版本=33；核心 CRUD、自动化防重与验证数据清理全部通过。

$ git diff --check
(无输出，退出码 0)
```

- 外部环境：2026-08-19 使用 Docker 29.4.0 临时启动 `mysql:8` 与 `postgres:17`，均绑定到本机回环地址并通过就绪检查；真实 Chromium 集成、MySQL/PostgreSQL 多方言回归和两个 `cmd/dbverify` 均已执行且通过。多数据库测试创建并清理独立 `xytest_*` 数据库，验收结束后已删除两个临时容器。未执行真实账号登录或外部平台调用。`cover.out`、`cover-browser.out` 和 `frontend/coverage/` 是生成验证产物，未纳入提交。

### 2026-08-19 阶段六二维码风控状态 DTO 完整性修复

- 交付范围：`publicQRStatus` 将底层二维码会话已生成的 `face_qr_url` 和 `verification_screenshot` 显式映射到具名 HTTP DTO；前端共享二维码状态契约同步声明这两个展示字段，账号 feature 的轮询类型经自身 API adapter 派生，避免共享 transport DTO 与 feature 类型漂移。
- 敏感数据边界：未重新暴露 `cookies`、`cookie_snapshot`、`unb` 或 `verification_url`。其中验证链接不属于 UI 消费字段，且既有前端契约明确禁止渲染；人脸二维码优先展示，截图只作为兜底。
- 回归保护：服务端契约测试同时断言二维码和截图透传、Cookie 脱敏；前端轮询与 Hook 测试仅使用当前公开状态字段；版本化与兼容路由继续复用同一 handler。
- 冻结边界：未修改 `internal/browser/slider.go`、`token_captcha*.go`、其测试或冻结规范。

### 本次验收

```text
$ go test ./internal/server ./internal/xianyu/qrlogin -count=1
ok   xianyu-go/internal/server
ok   xianyu-go/internal/xianyu/qrlogin

$ npm test --prefix frontend -- --run
Test Files  67 passed (67)
Tests       405 passed (405)

$ npm run typecheck --prefix frontend
(无输出，退出码 0)

$ npm run comments:check --prefix frontend && make comments
Go/前端 commentlint 均通过。

$ npm run build --prefix frontend
✓ built in 2.76s；已重建 internal/webui/static。

$ go run ./tools/architecturecheck && git diff --check
architecturecheck: 通过；git diff --check 无输出。
```

### 2026-08-19 阶段六前后端 DTO 字段完整性门禁

- 审计范围：共享 `frontend/shared/api-contract/transport.ts` 的全部导出对象 DTO，以及每个 `frontend/app/features/*/api.ts` 中的响应形态 DTO；健康检查 `BuildInfo` 也显式登记。门禁要求每个 DTO 都在 `frontendDTOContractSpecs` 登记，直接 HTTP DTO 的每个顶层字段必须在 `internal/server` 对应具名结构体中以 JSON 标签提供。
- 解析边界：门禁会展开 TypeScript `extends` 字段和 Go 匿名嵌入 DTO 字段，避免订单详情等兼容顶层字段产生误报；嵌套对象字段单独属于其行 DTO，不被错误归入外层响应。动态键、feature 归一化和脱敏模型必须逐项写明理由，不能静默跳过。
- 修复结果：二维码风控状态已补齐 `verification_screenshot` 和 `face_qr_url`；删除卡券时间字段、账号 AI 模型/密钥字段、通知渠道配置与时间字段等服务端从未提供且前端不消费的历史契约。通知渠道列表继续不返回配置，避免 SMTP 等秘密穿透摘要 DTO；编辑器用空配置初始化。
- 回归保护：新增真实临时仓库夹具测试，确认遗漏后端字段会触发门禁；新增匿名嵌入解析测试。冻结滑块验证码实现及其调用语义未修改。

### 2026-08-20 OpenAPI 契约阶段三：查询、聊天与订单主链路

- 交付范围：dashboard、订单分析、有效订单、订单分页/详情/更新/删除、异步刷新任务、聊天会话/消息/已读/图片/文本与管理员用户、账号、任务摘要均通过生成的 `/api/v1` operation 调用；UI 继续只消费 feature adapter 输出的派生模型。
- WebSocket：`/api/v1/chat/ws` 以 `x-websocket-message-schema` 引用 `ChatWebSocketEvent`。服务端实际 `ready` 和聊天事件 DTO 由同一 OpenAPI component 校验；原生 WebSocket 仍只由聊天通知 owner 使用。
- 契约修正：订单刷新项的 `soft_deleted` 由历史错误的整数收紧为真实 handler 输出的布尔值；管理员账号摘要 schema 不包含 Cookie、密码或其他凭证字段。
- 验收：`make api-check`、`go run ./tools/architecturecheck`、`go test ./internal/server -count=1`、`npm run typecheck --prefix frontend`、`npm test --prefix frontend`、`make comments` 与 `git diff --check` 均通过。未修改冻结 CAPTCHA 实现，未执行真实账号或外部平台调用。

### 2026-08-20 OpenAPI 契约阶段四：商品、卡券和文件传输

- 交付范围：items、发布批次、卡券和表格上传全部改由 feature adapter 通过生成 operation 发起；FormData 在共享运行时按 multipart Content-Type 原样重建，未引入 Base64。
- 回归保护：真实 Router 测试覆盖商品、卡券列表的成功和未认证响应，以及卡券文件上传的格式错误 envelope；原有批次取消、重试、代次隔离和 CSV 下载行为保持不变。

### 2026-08-20 OpenAPI 契约阶段五：自动化、设置和通知动态契约

- 交付范围：自动化规则、回复规则、默认回复、账号计划任务、账号 AI 设置、系统设置与通知渠道/绑定均通过生成 operation 访问；UI 归一化和秘密表单状态仍留在 feature adapter。
- 动态值：`NotificationBindingsByAccount` 使用 `additionalProperties` 限定每个账号键对应 `NotificationBinding[]`；通知渠道摘要不返回 SMTP 密码、Token 或完整秘密配置。
- 回归保护：真实 Router 响应测试验证渠道列表与动态账号绑定映射；`make api-check`、architecturecheck、前端 typecheck/tests 与注释门禁通过。

### 2026-08-20 OpenAPI 契约阶段六：全量封闭与旧手写契约退场

- 最终提交绑定：`阶段六：完成生成契约迁移并永久关闭旁路`。
- 交付范围：删除 `frontend/shared/api-contract/transport.ts`；原有 UI 派生模型、表单模型与兼容归一模型按 accounts、cards、chat、dashboard、items、notifications、orders、rules、session、settings 迁入各自 feature。共享领域契约模块只透明别名导出 `api/openapi.yaml` 生成 schema，feature adapter 保持唯一响应归一化边界。
- 永久门禁：architecturecheck 阶段六会拒绝恢复 `transport.ts`、任何 production import/re-export 的 `transport` 路径，以及 feature、组件或 Hook 绕过 shared 契约层直接导入生成 schema；阶段四门禁继续拒绝旧 `get/post/put/del/postForm`。此规则不使用 DTO 名单、路径白名单、baseline 或 warning-only 例外。
- 兼容与安全：旧路由继续复用已登记的版本化 handler，不在 OpenAPI 重复登记；允许未声明的非敏感额外响应字段不改变生成类型，Cookie、Token、密码、SMTP 密钥和加密 metadata 仍由既有安全响应测试独立阻断。未修改冻结 CAPTCHA 实现、选择器、时序或浏览器调用顺序，也未执行真实账号调用。
- 验收：`make api-check`、`go test ./tools/architecturecheck -count=1`、`go run ./tools/architecturecheck`、`npm run typecheck --prefix frontend`、`npm test --prefix frontend`（67 files、405 tests）、`make comments`、前端嵌入构建、`make check`、`go test ./... -count=1`、`make test-server-race`、`make cover`（Go statements 65.8%）、`make cover-frontend`（V8 statements 80.21%、lines 82.43%）和 `make cover-browser`（`RUN_BROWSER_INTEGRATION=1`，browser statements 64.1%）均通过。冻结浏览器普通单元测试也已通过；未执行真实账号或外部平台调用。覆盖率产物不纳入提交。

### 2026-08-20 阶段六 operation 自动覆盖门禁补强

- 门禁实现：新增 `internal/server/openapi_operation_coverage_test.go`，从 `api/openapi.yaml` 自动枚举全部 operationId、路径、方法、必需参数和请求体，直接调用真实 `chi` Router，并用 `kin-openapi` 校验响应状态、Content-Type 和 JSON schema；不维护 DTO、路径或 operation 名单。
- 契约修正：补齐通知不确定 outbox、自动化问题与规则、默认回复、商品回复、AI 设置、用户设置以及批量发布响应的具名 schema；动态对象使用受约束 `additionalProperties`，批量发布请求与成功响应同步登记。
- 门禁接入：`make api-check` 现在同时执行路由双向覆盖和 operation 自动响应覆盖。生成 schema 仍由临时目录逐字节比较，后端额外非敏感字段不改变生成类型，敏感字段继续由独立安全测试阻断。
- 验收：自动 operation 场景覆盖 137 个 operation；`make api-check`、`go test ./internal/server -run '^TestOpenAPIOperationsHaveContractScenarios$' -count=1` 和 `go test ./internal/server -count=1` 通过。未修改冻结 CAPTCHA 实现或真实账号调用。

### 2026-08-20 阶段六 operation 成功覆盖最终收口

- 完成范围：新增真实 Router 成功场景覆盖初始化、长登录、AI 模型、卡券上传、聊天读写/图片、商品同步/发布、批次、自动化、通知和账号任务等此前未触达的 operation；覆盖由 OpenAPI operationId 自动发现，不维护 DTO、路径或场景名单。
- 契约语义：`kin-openapi` 对真实响应执行状态码、Content-Type 和 JSON schema 校验；必需字段缺失或类型错误、声明字段类型错误、错误状态码和媒体类型都会失败；未声明的非敏感额外字段允许且不会进入生成 TypeScript 类型。敏感字段仍由独立响应安全测试阻断。
- 门禁结果：`make api-check`、`go run ./tools/architecturecheck`、`make comments`、`git diff --check`、前端 typecheck、前端 405 项测试和生产构建全部通过；`make check` 与 `go test ./... -count=1` 全部通过，Server race 通过。
- 覆盖率：`make cover` Go statements 66.2%；`make cover-frontend` V8 statements 80.2%、lines 82.43%。覆盖率文件和前端覆盖率目录均为生成产物，未纳入提交。
- 浏览器与外部环境：冻结 CAPTCHA 普通单元测试随全量 Go 测试通过；本次未执行真实账号或外部平台调用，未修改冻结浏览器实现。浏览器真实集成和多数据库证据继续沿用本文件此前已记录的通过结果。
