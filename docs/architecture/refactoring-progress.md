# 重构阶段验收记录

本文件只记录已经完成阶段的最终提交与验收证据。唯一阶段状态和顺序由
`refactoring-master-plan.md` 定义；阶段内工作、临时提交、切片和局部成功一律不记录。

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

## 下一阶段入口

阶段四已完成，阶段五：DB 与事务治理重新验收为唯一当前阶段。开始阶段五前先运行 architecturecheck，确认
前序 React feature、传输契约、网络旁路和严格 TypeScript 门禁继续 fail-closed；不得提前进入阶段六最终收口，
也不得修改冻结 CAPTCHA。

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

## 阶段五入口

阶段五现在是唯一当前阶段。下一步只处理 DB 与事务治理重新验收：清除上层裸 `Store.DB`、`sql.DB`、`sql.Tx` 和 row model 泄露，核对 Unit of Work、claim/lease、取消、重试及补偿，并在可用时运行 SQLite、MySQL、PostgreSQL 和 `cmd/dbverify` 证据。不得在阶段五提前执行阶段六兼容退场、复杂度或全量注释收口。

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

## 下一阶段入口

阶段五已完成，阶段六：全量架构、兼容退场和注释收口为唯一当前阶段。开始前必须先运行
`go run ./tools/architecturecheck`，使已完成阶段的一至五门禁和新激活的质量门禁同时 fail-closed；不得修改冻结 CAPTCHA，也不得把 `frontend/coverage/` 纳入提交。
