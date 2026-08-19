# 生命周期与后台任务清单

本文是生命周期静态审计清单。它记录后台组件的所有者、Context 来源、停止和等待边界，
用于防止后续迭代重新引入游离 goroutine；它不定义阶段编号、阶段状态或验收结论。

## 组件清单

| 组件/入口 | 所有者 | Context 来源 | 停止/关闭 | 等待/观测 | 当前证据 | 剩余风险 |
| --- | --- | --- | --- | --- | --- | --- |
| `lifecycle.Coordinator` | `cmd/server` 进程组合根 | `Start(parent)` 创建共享应用生命周期 Context；每次 `Close(ctx)` 使用调用方关闭预算 | 取消共享 Context，按登记逆序调用组件 `Close(ctx)`；成功组件记录完成，失败组件保留以便更长 Context 重试 | `Close` 返回本次聚合诊断；并发调用等待本次尝试；`WaitContext` 仅在全部组件成功收束后完成 | 启动失败回滚、重复 Start/Close、关闭竞态、超时诊断和可重试 Join 测试 | 关闭组件必须自行响应 Context；同步外部调用依赖明确硬超时 |
| `server.Server` HTTP 与请求后台任务 | `cmd/server`（HTTP）/`server.Server`（请求任务观测） | `Server.Start` 接收的进程 Context；请求任务使用调用方显式 Context | `Server.Stop(ctx)` 只关闭 HTTP 与请求任务等待；应用 worker 不由 Server 关闭 | `Wait`、`WaitForBackground`、任务注册表状态查询 | Server 生命周期、超时等待和任务注册表测试 | 业务 worker 已移出 Server；任务业务语义继续由应用服务维护 |
| 订单刷新 worker/recovery | `lifecycle.Coordinator` → `orders.RefreshJobService` | 协调器共享应用生命周期 Context；请求只负责创建任务 | 协调器逆序 `Close(ctx)`，runner 通过租约 token 和 Context 收束 | 应用任务状态、租约终态和 runner `Wait/Close` | 订单刷新生命周期、租约、取消和晚到写入测试 | 统一运维查询属于后续观测迭代，不改变生命周期所有权 |
| 商品批量发布 worker/recovery | `lifecycle.Coordinator` → `items.BatchWorkerCoordinator` | 协调器共享应用生命周期 Context；取消后的本地收口使用受控补偿 Context | 协调器逆序 `Close(ctx)`，worker cancel、租约 token 和终态补偿由应用协调器负责 | 批次状态、协调器 `Wait/Close` 与错误回调 | 批量取消 race、应用 BatchRunner 和协调器测试 | 恢复指标属于后续观测迭代，不改变生命周期所有权 |
| 订单 reconciliation worker | `lifecycle.Coordinator` → `orders.ReconciliationRecoveryCoordinator` | 协调器共享应用生命周期 Context | 协调器逆序 `Close(ctx)` 取消当前扫描并等待退出 | 补偿记录状态与协调器 `Wait/Close` | reconciliation 成功/失败重试、取消和协调器测试 | 指数退避属于后续补偿策略迭代 |
| `account.Manager` 账号运行时集合 | `lifecycle.Coordinator`（由 `cmd/server` 装配） | 协调器共享应用生命周期 Context | 协调器逆序调用 `StopAllContext`，保留全局 stopping fence | 等待单账号或全部账号结束 | Manager fencing、删除 fencing、生命周期和 race 测试 | 运行时内部任务的细分由正式总计划决定，不改变进程级 owner |
| `engine.Account` 单账号运行时 | `account.Manager`；连接、出站、凭证和迟到续期分别由 `connectionCoordinator`、`outgoingMessageCoordinator`、`credentialCoordinator`、`pendingRenewalCoordinator` 负责 | `Account.Run(parent)` | `StopContext` 取消运行 Context；连接协调器以受限预算等待自有 worker | `StopContext` 返回错误；共享完成信号支持超时后的再次 Join；运行状态可查询 | Engine lifecycle、连接协调器、出站发送、credential coordinator、刷新取消与迟到续期测试 | recorder 子任务继续沿用账号运行 Context，不允许脱离 facade 生命周期 |
| 浏览器 `Manager` | `lifecycle.Coordinator`（由 `cmd/server` 装配） | 每次调用的独立 Context；初始化和关闭受协调器 Context/Close Context 约束 | 协调器逆序调用 `CloseContext`，关闭 fencing 并等待活动调用归零 | `CloseContext` 返回超时并可重试 | Browser lifecycle 与 race 测试 | 底层同步 Playwright Close 无法被 Context 中断，这是实现约束 |
| 二维码 `qrlogin.Manager` | `lifecycle.Coordinator`（由组合层装配） | `Start` 从进程生命周期 Context 建立可取消根 Context；每个扫码会话从该根派生 Context | `DeleteSession` 取消该会话；`CloseContext` 拒绝新会话、取消根与全部会话任务 | Manager 用 `WaitGroup` 等待扫码轮询和人脸验证任务退出；关闭超时可用更长 Context 重试等待 | QR Manager 关闭、删除会话、重复关闭和关闭后拒绝新会话测试 | 平台 HTTP 请求本身仍受各会话调用超时约束 |
| 续期 `renewal.Scheduler` | `lifecycle.Coordinator`（由 `cmd/server` 装配） | 协调器共享应用生命周期 Context | 协调器逆序调用 `StopContext`，幂等并阻止 Stop 后 Run | `WaitContext` | 先 Stop、重复 Stop、零值和 race 测试 | 迟到 Cookie 合并语义由续期组件自身 generation/锁测试保护 |
| 自动化 `automation.Scheduler` | `lifecycle.Coordinator`（由 `cmd/server` 装配） | 协调器共享应用生命周期 Context；nil Context 明确拒绝启动 | 协调器逆序取消 Context 并调用 `WaitContext` | `WaitContext` | scheduler、nil 输入与结果收口测试 | 自动化外部动作语义归属由正式总计划决定，不改变调度器 owner |
| 通知 outbox worker | `lifecycle.Coordinator`（由 `cmd/server` 装配） | 协调器共享应用生命周期 Context | 协调器逆序调用 `WaitContext`，由共享 Context 停止拉取与发送 | `WaitContext`、uncertain 状态查询 | notify worker、uncertain 状态和三库测试 | 运维重试与人工核对流程属于通知业务迭代 |
| WebSocket/聊天后台发送任务 | `chat.Service` 与请求级连接 owner | 请求 Context 和连接关闭信号；不继承 Server 业务 worker Context | 请求取消或连接关闭时先取消发送/读取任务，再由连接 owner 等待 Join | WebSocket handler 显式等待读取 goroutine，发送结果和连接状态保留可观测字段 | WebSocket 事件流测试、Server race；`chatWebSocket` 已显式 Wait/Join 读取任务 | 更细的消息分发状态由正式总计划安排，不新增 Server 生命周期入口 |

## 外部调用取消与超时审计

| 调用域 | Context/取消路径 | 硬超时或边界 | 验收结论 |
| --- | --- | --- | --- |
| 通知 HTTP（钉钉、飞书、Bark、Webhook、企业微信、Telegram） | Outbox worker 使用协调器共享 Context；数据库领取/确认响应取消 | `netguard.PublicHTTPClient` 默认 10 秒；Webhook 复用同一 client timeout | 外部 HTTP 不会无限等待；发送 helper 当前依赖有界 client timeout，停止时不会无限阻塞；后续可在通知业务迭代中进一步传递 worker Context |
| SMTP 通知 | 当前发送 helper 为每次发送创建独立 Context；拨号和 TLS 使用该 Context | SMTP 总体 Context 20 秒，公网拨号 10 秒，连接 deadline 20 秒 | 无法取消的 SMTP 库调用有明确上限；停止时依赖发送级预算，不会无限等待 |
| MTOP HTTP | 调用方 Context 贯穿 `http.NewRequestWithContext` 和响应读取 | 默认 HTTP client 30 秒；调用方更短 Context 可提前取消 | 请求取消和 client 硬超时均存在，未发现脱离调用 Context 的生产路径 |
| QR 登录 HTTP/轮询 | 生成请求使用调用 Context；扫码监控和人脸验证使用会话有效期独立 Context | QR HTTP client 60 秒；人脸换 Cookie client 30 秒；会话窗口到期自动结束 | HTTP 调用有上限；独立会话任务由 QR Manager 拥有，不进入 Server worker 集合 |
| Cookie/静默续期 | 续期请求绑定调用 Context；后台 Promise 窗口结束后仍由私有 Context 管理 | 普通请求 30 秒；后台 fetch 30 秒硬上限，Promise 窗口仅控制调用方返回时机 | `WithoutCancel` 是保持平台 Cookie 收口的明确例外，仍受硬超时约束并由续期 scheduler Join |
| WebSocket | 建连、注册、请求、心跳和 ACK 均由连接/请求 Context 控制 | 建连、注册、心跳默认 30 秒；ACK 2 秒 | 连接关闭会取消读取与等待，已有连接 Join 测试覆盖 |
| 浏览器/Playwright | `CloseContext` 拒绝新调用并以 Context 轮询等待活动调用归零 | 活动调用等待服从调用 Context；底层同步 `pw.Stop` 本身无法被 Context 中断 | 不启动关闭 goroutine；超时返回后保持 closing 状态，可用更长 Context 重试，避免伪造完成状态 |
| 二维码登录 | 扫码轮询和人脸验证从 Manager 根 Context 派生，单个会话另有可取消 Context | 删除会话或进程关闭都会取消会话任务；关闭时等待 Manager 的任务组退出 | HTTP 生成二维码请求仍服从调用方请求 Context；创建成功后的后台轮询不继承该请求 Context |

审计结论：`notify.Notifier.Start(nil)` 和 `automation.Scheduler.Run(nil)` 已明确拒绝启动并有确定性测试；生产组合根始终传入非 nil Context。通知 HTTP helper 仍以 10 秒 client timeout 作为无法直接继承 worker Context 的边界，未形成无限等待。

## 并发与锁边界

| 锁/协调状态 | 所有者与保护范围 | 明确禁止 | 锁顺序/验证 |
| --- | --- | --- | --- |
| `Store` 账号凭证锁 | 保护 Cookie、metadata、Token 绑定指纹的快照读取和条件写回 | 不得跨 MTOP、WebSocket、浏览器、通知或用户等待 I/O | `refreshGate → credential lock`；锁外 I/O、旧响应冲突和锁外 Handler 通知测试 |
| Engine `refreshGate` | 用单令牌通道串行化同一账号 Token/续期状态机及迟到响应收口，不持有互斥锁等待外部 I/O | 不得反向取得其他 worker 锁；等待必须响应调用 Context 取消 | `refreshGate → credential lock`；Token、API 续期、迟到 Cookie、取消和 race 测试 |
| Engine `outgoingMessageCoordinator` | 先短暂读取 `runtimeMu` 中的连接，再短暂读取凭证锁中的 `unb` 身份；发送与历史查询在两把锁外执行 | 不得同时持有 `runtimeMu` 与凭证锁，不得持锁执行 WebSocket I/O | `runtime snapshot → credential snapshot`，两段锁不重叠；出站发送与连接 race 测试 |
| Automation `cardLocks` | 保护同一卡密组库存消费与确定未发送时恢复 | 不得覆盖 WebSocket 消息发送、图片发送或人工等待 | 每次库存操作独立加锁；并发卡密发送阻塞测试 |
| Automation 运行租约/动作检查点 | 由数据库租约拥有跨进程 worker 的唯一执行权；结果通知以 `runID + status` 写入持久化 outbox | 不得在本地互斥锁内持有外部动作；不确定结果不得自动重放或重新入队 | `StartRunAction` 后执行外部动作；通知 outbox 的成功/失败/不确定隔离与重复恢复测试 |

## 使用规则

1. 新增 goroutine 必须在本表增加一行，注明启动者、Context 来源、取消责任和等待方式。
2. 新组件必须同时提供取消、超时、重复停止和晚到写入测试；测试通过不等于其他组件自动继承其业务语义。
3. 删除账号前必须先建立账号级 stopping fence，再在受限 Context 内停止运行时，最后重新校验归属并删除持久化记录。
4. 本表只记录 owner 与边界；业务策略、运维观测和实时连接细分风险不得重新转化为 Server 生命周期入口。
