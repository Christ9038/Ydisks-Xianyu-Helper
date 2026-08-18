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

当前阶段为阶段四：React Feature 化和异步状态修复。阶段四开始前先运行 architecturecheck，激活已有 React
feature、传输契约、网络旁路、动态 import 和严格 TypeScript 门禁；在同一阶段完成 ApiError、领域契约拆分、
items 地图迁移、定位取消/超时、批量轮询与嵌入产物重建。不得提前进入数据库或最终收口阶段，也不得修改冻结
CAPTCHA。
