# 生命周期与后台任务清单

本文是重构计划阶段 5 的持续审计清单。它记录后台组件的所有者、Context 来源、停止和等待边界，
用于逐项关闭“生命周期与删除 fencing”父切片；清单本身不代表对应组件已经满足完成条件。

## 组件清单

| 组件/入口 | 所有者 | Context 来源 | 停止/关闭 | 等待/观测 | 当前证据 | 剩余风险 |
| --- | --- | --- | --- | --- | --- | --- |
| `server.Server` HTTP 与后台任务 | `cmd/server` | `Server.Start` 接收的进程 Context；后台任务继承生命周期 Context | `Server.Stop(ctx)`，关闭 HTTP、恢复任务和后台计数 | `Wait`、`WaitForBackground`、任务注册表状态查询 | Server 生命周期、超时等待和任务注册表测试 | 仍需把所有新后台入口统一登记并审计启动顺序 |
| 订单刷新 worker/recovery | `server.Server` | Server 生命周期 Context；恢复扫描使用调用方 Context | worker 终止、租约 fencing、终态写入使用受控 Context | 任务注册表记录 running/succeeded/failed/canceled/timed_out | 订单刷新生命周期与租约测试 | 取消端点和统一运维查询仍待收口 |
| 商品批量发布 worker/recovery | `server.Server` | Server 生命周期 Context；取消后的本地收口使用独立补偿 Context | worker cancel、租约 token、批次终态补偿写入 | 任务注册表与批次状态 | 批量取消 race、应用 BatchRunner 测试 | 单行平台适配仍在 Server，恢复指标和统一查询待补齐 |
| 订单 reconciliation worker | `server.Server` | Server 生命周期 Context | 扫描器停止时取消当前扫描 | 后台任务注册表和补偿记录状态 | reconciliation 成功/失败重试测试 | 指数退避和更完整运维操作待补齐 |
| `account.Manager` 账号运行时集合 | `cmd/server` | `StartAll`/`Start` 调用方 Context | `StopContext`、`StopAllContext`，全局 stopping fence | 等待单账号或全部账号结束 | Manager fencing、删除 fencing 和 race 测试 | 运行时内部任务仍需纳入统一清单 |
| `engine.Account` 单账号运行时 | `account.Manager` | `Account.Run(parent)` | `StopContext` 取消运行 Context 并等待连接/任务退出 | `StopContext` 返回错误；运行状态可查询 | Engine lifecycle 与 credential coordinator 测试 | 消息发送、重连和 recorder 子任务需要逐一核对 Join 边界 |
| 浏览器 `Manager` | `cmd/server`/账号流程 | 每次调用的 Context；关闭使用 `CloseContext` | 关闭 fencing，等待活动调用归零，再关闭 Playwright | `CloseContext` 返回超时并可重试 | Browser lifecycle 与 race 测试 | 底层同步 Playwright Close 无法被 Context 中断 |
| 续期 `renewal.Scheduler` | `cmd/server` | `Run(ctx)` 调用方 Context | `StopContext` 幂等并阻止 Stop 后 Run | `WaitContext` | 先 Stop、重复 Stop、零值和 race 测试 | 迟到 Cookie 合并调用方仍需统一审计 |
| 自动化 `automation.Scheduler` | `cmd/server` | `Run(ctx)` 调用方 Context | Context 取消后停止调度 | `WaitContext` | scheduler 与结果收口测试 | 自动化准备和外部动作旁路仍需完整盘点 |
| 通知 outbox worker | `cmd/server` | `Start(ctx)` 调用方 Context | Context 取消后停止拉取与发送 | `WaitContext`、uncertain 状态查询 | notify worker、uncertain 状态和三库测试 | 运维重试与人工核对流程仍待补齐 |
| WebSocket/聊天后台发送任务 | `server.Server`/`chat.Service` | 请求或服务生命周期 Context | 请求取消、连接关闭或服务停止；handler 清理阶段先取消并关闭连接 | handler 等待读取 goroutine 退出，发送结果和连接状态仍由连接生命周期观测 | WebSocket 事件流测试、Server race；`chatWebSocket` 已显式 Wait/Join 读取任务 | 其他实时连接的统一 owner/Join 证据仍待完善 |

## 使用规则

1. 新增 goroutine 必须在本表增加一行，注明启动者、Context 来源、取消责任和等待方式。
2. 每个组件完成生命周期迁移后，必须同时提供取消、超时、重复停止和晚到写入测试；测试通过不等于其他组件自动完成。
3. 删除账号前必须先建立账号级 stopping fence，再在受限 Context 内停止运行时，最后重新校验归属并删除持久化记录。
4. 本表中标记为“剩余风险”的项目未满足父切片关闭条件前，阶段 5 必须保持“进行中”。
