# 重构阶段验收记录

本文件只记录阶段整体的验收证据、更新日志和阶段边界。每个阶段对应一个 PR；阶段内并行实现不形成独立任务、独立验收或额外 PR。

## 阶段 3：HTTP API 契约

状态：已完成。

主要证据：业务入口使用 `/api/v1` 版本化路径、具名 DTO、统一错误 envelope 和前端兼容归一；空集合兼容、契约测试、前端构建和嵌入资源均已验证。

## 阶段 4：Server 应用服务

状态：已完成；对应一个阶段 4 PR 边界。

完成证据：

- Server 生产代码不再导入 `database/sql`、`internal/db`、`internal/xianyu` 或 `internal/browser`，不创建事务，也不持有通用 `adapter.Dependencies`。
- 订单刷新、订单补偿扫描、批量发布、二维码会话、账号登录、Cookie 写入、资料刷新、聊天刷新/订阅、商品发布和管理员删除均通过应用服务与消费者定义的最小 Port 编排。
- Session 失效恢复统一由 `adapter.SessionRecoveryHandler` 分类和记录脱敏诊断，再调用账号运行时应用端口；生产适配器不再把 `Server.recoverExpiredMTOPSession` 作为回调传入。
- 订单运行时使用 `adapter.OrderAutomation`、`adapter.OrderNotifier` 等 typed port；Server 不再编排 Manager、Automation 和 Notifier 闭包。订单补偿扫描由 `orders.ReconciliationRecoveryCoordinator` 拥有启动、取消、等待和关闭生命周期。
- 账号凭证恢复使用 `account.CredentialRefreshCoordinator` 按账号并发登记；慢速协议续期不在协调锁内执行，自动化唤醒经过 `CredentialWakePort`/`CredentialWakeService`。
- 所有必需领域依赖由显式类型化依赖组构造并在 `Server.New` 阶段校验；缺失依赖、越权、失败、取消、重复启动/关闭、租约 fencing、补偿和晚到结果均有聚焦测试。
- `MTop`、`CookieRenew`、`QRLogin` 以及少量 Adapter setter 仅作为测试/兼容入口保留。删除条件为已知生产与测试调用清零、契约测试证明新构造路径覆盖全部调用，并完成外部调用方审计；这些兼容入口不作为新增业务依赖。
- 冻结滑块 CAPTCHA 实现及其测试未修改。

统一验证证据：

```text
go test ./internal/application/... ./internal/adapter ./internal/server -count=1
go test -race ./internal/application/... ./internal/adapter ./internal/server -count=1
go vet ./...
go run ./tools/architecturecheck
go run ./tools/commentlint -mode check -root . -baseline .commentlint/go-baseline.json
git diff --check
```

以上命令均通过；race 运行实际覆盖应用层、适配器和 Server 生命周期/凭证并发路径。

## 更新日志

- 2026-08-17：完成阶段 4 整体 Server 应用服务改造，统一完成应用 Port、显式装配、凭证边界、Session 恢复、订单补偿生命周期和并发协调，并通过阶段 4 全部门禁。

## 阶段 5：应用装配与生命周期

状态：已完成；对应一个阶段 5 PR 边界。阶段内并行实现不形成独立任务、独立验收或额外 PR。

完成证据：

- 新增 `internal/application/lifecycle.Coordinator`，统一拥有共享 Context、登记顺序、启动失败逆序回滚、关闭逆序、并发 Close 等待、超时和错误聚合；组件回调始终在锁外执行。
- `cmd/server` 统一登记浏览器、Notifier、账号 Manager、Automation Scheduler、Renewal Scheduler 以及订单刷新、批量发布、订单 reconciliation 三类应用 worker；HTTP Server 只负责 HTTP 启停，退出时先停 HTTP，再由协调器逆序关闭应用组件。
- Server 删除业务 worker 的启动/关闭入口和应用 worker 等待逻辑；Server 仅通过只读生命周期 Context 为请求创建的应用 worker 提供父 Context，生命周期 owner 保持在协调器与应用服务。
- 启动失败回滚、重复 Start/Close、并发 Close、启动与关闭竞争、批次/订单 worker 取消、租约 fencing、账号 stopping fence、浏览器关闭超时重试和实时连接 Join 均有聚焦测试；冻结 CAPTCHA 文件未修改。
- `docs/architecture/lifecycle-inventory.md` 已转为静态 owner/Context/Stop/Wait 清单，后续 Engine/Automation 内部状态拆分和运维观测风险归入阶段 6 及后续迭代，不得重新引入 Server 生命周期入口。

统一验证证据：

```text
make check
go test -race ./internal/server ./internal/account ./internal/engine ./internal/automation ./internal/renewal -count=1
go test ./... -count=1
go run ./tools/architecturecheck
go run ./tools/commentlint -mode check -root . -baseline .commentlint/go-baseline.json
git diff --check
```

以上命令均通过；race 子集实际覆盖 Server、账号、Engine、Automation、续期和生命周期竞争路径。

下一安全入口：阶段 6「Engine 与 Automation」，只处理其内部状态所有权和动作语义拆分，不回退或扩大阶段 5 的进程生命周期边界。

## 更新日志

- 2026-08-17：完成阶段 5 应用装配与生命周期整体改造，统一 cmd 生命周期协调、Server HTTP 边界、应用 worker owner、启动回滚与关闭等待，并通过阶段 5 全部门禁。

## 阶段 6：Engine 与 Automation

状态：已完成；对应一个阶段 6 PR 边界。阶段内并行实现不形成独立任务、独立验收或额外 PR。

完成证据：

- `automationActionExecutor.sendDataCard` 的卡券锁仅覆盖库存预留与确定未发送时的恢复，外部 WebSocket 发送已移到锁外，并有并发阻塞测试证明第二个库存操作不会等待第一个外部发送。
- `confirmShipmentAttempt` 在凭证锁内只读取最小运行时快照，MTOP 调用在锁外执行；响应写回重新获取凭证锁并进行指纹条件校验，冲突时跳过旧响应并记录人工核对所需错误。
- 新增凭证锁锁外外部 I/O、并发新凭证不被旧响应覆盖的确定性测试；冻结 CAPTCHA 实现及测试未修改。
- `automation.NewWithDependencies` 固定 MTOP、账号任务、订单详情、通知和 Cookie 读取依赖；所有 `Center` setter、运行时依赖覆盖和其锁已删除，延迟恢复通过同一构造依赖重建 Center。
- `engine.Account` 已将连接、出站消息、凭证、消息分发、运行状态、生命周期和迟到续期收束到独立组件；凭证实现已物理迁入 `credentialCoordinator`，facade 仅保留稳定入口和必要兼容委托。
- `refreshGate` 支持 Context 取消；凭证锁只覆盖快照和条件提交，MTOP、WebSocket、浏览器、通知及 Handler 回调均在锁外执行。`pendingRenewalCoordinator` 与连接协调器以共享完成信号和有限预算 Join，不创建超时后无法回收的等待 goroutine。
- 自动化外部动作在运行检查点后执行，保留成功、失败和 `needs_review` 三态。结果通知以 `runID + status` 作为持久化 outbox 幂等键，重复恢复不会重复入队；外部投递成功但本地确认失败的 `uncertain` 记录不会自动重放。SQLite、MySQL、PostgreSQL 的 `00034` 通知幂等迁移已对齐。
- 冻结 CAPTCHA 受保护文件未改动；浏览器集成和全部冻结行为测试通过。

下一安全入口：阶段 7「React Feature 化」。仅处理 `app -> features -> shared`、feature API adapter、状态归属与路由加载，不回退阶段 6 的 Engine、Automation、凭证、通知或并发边界。

当前统一验证证据：

```text
make check                                                        # 通过
go test ./internal/engine ./internal/automation ./internal/browser -count=1 # 通过
go test -race ./internal/engine ./internal/automation -count=1              # 通过
RUN_BROWSER_INTEGRATION=1 go test ./internal/browser -count=1                # 通过
go run ./tools/architecturecheck                                                # 通过
make comments                                                                     # 通过
git diff --check                                                                  # 通过
make cover                                                                        # Go statements 66.5%
make cover-browser                                                                # Browser statements 60.9%（RUN_BROWSER_INTEGRATION=1）
make cover-frontend                                                               # Frontend statements 93.01%
```

覆盖率例外：未跳过本地确定性 Engine、Automation 或浏览器行为；仅保留项目既有的真实账号/外部平台不可用场景分类。

更新日志：

- 2026-08-17：完成阶段 6 整体 Engine 与 Automation 改造，统一收束 facade、凭证和 worker 生命周期、锁外外部 I/O、运行检查点及通知幂等/不确定态，并通过阶段 6 全部门禁。

## 阶段 7：React Feature 化

状态：已完成；对应一个阶段 7 PR 边界。阶段内并行实现不形成独立任务、独立验收或额外 PR。

完成证据：

- 根 `App` 仅装配错误边界、会话 Provider 和路由；认证表单归属 session feature，认证后导航、权限回退、侧边栏偏好和一次性跨页面载荷由 app 路由壳管理。
- 九个业务页面与其直属组件、Hook、状态、API adapter 和测试均归入各自 feature；账号自动任务归入 accounts，删除跨 feature 的 accountAutomation 目录。
- 已删除集中式 `frontend/services/api.ts`、`frontend/types.ts`、`frontend/request.ts` 和 `frontend/services/contract.ts`；feature API adapter 直接依赖 `shared/http`，只读传输契约位于 `shared/api-contract`，DTO 归一保留在所属 feature 边界。
- `shared` 只保留 HTTP transport、兼容 JSON contract、通用异步 gate、浏览器偏好和纯展示 UI；架构测试禁止 feature 相互导入、shared 反向依赖 feature、组件直连网络和根 components 回流。
- 所有业务页面由 `React.lazy` 按路由加载；构建验证九个独立页面分片、首屏不预加载 charts 分片且每页原始大小满足既有预算。仪表盘乱序、卡密/订单卸载取消、聊天切换与旧分页响应均有确定性行为测试。
- 冻结 CAPTCHA 实现、测试及调用语义均未修改。

当前统一验证证据：

```text
npm test --prefix frontend                         # 63 files, 383 tests 通过
npm run typecheck --prefix frontend                # 通过
npm run comments:check --prefix frontend           # 通过
npm run build --prefix frontend                    # 通过，已更新 internal/webui/static
make cover-frontend                                # Frontend statements 79.84%，RUN_BROWSER_INTEGRATION 未使用
git diff --check                                   # 通过
go run ./tools/architecturecheck                   # 通过
make comments                                      # 通过
make check                                         # 通过
```

覆盖率例外：纯 React 页面、展示组件与错误边界在本阶段的 V8 报告中存在未覆盖行；所有新改动的业务 API、Hook、状态、取消、切换和乱序行为均由确定性 Vitest 覆盖。无真实账号或外部平台调用。

下一安全入口：阶段 8「DB 与事务治理」，仅处理窄 repository、应用层 Unit of Work、批量与三方言事务验收；不得回退阶段 7 的 feature 边界、共享 HTTP 或路由加载约束。

更新日志：

- 2026-08-17：完成阶段 7 React Feature 化整体改造，统一落地 app -> features -> shared、feature API adapter、路由懒加载、异步请求生命周期与前端架构门禁，并通过阶段全部验证。
