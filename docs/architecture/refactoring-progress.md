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

## 下一阶段入口

当前阶段为阶段三：生命周期、Engine 和 Automation 重新验收。先以 `lifecycle-inventory.md` 对照每个后台
worker 的 owner、Context、Cancel、Wait/Join 和锁顺序；阶段三门禁已经随状态切换启用。不得提前进入 React、
数据库或最终收口阶段，也不得修改冻结 CAPTCHA。
