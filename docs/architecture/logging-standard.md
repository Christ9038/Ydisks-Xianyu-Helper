# 日志规范

本文约束 `log/slog` 日志的字段、等级和降噪方式。目标是每条日志都能回答三件事：**谁写的**（调用位置）、**为哪件事**（request_id / run_id / account / order_id）、**调了什么外部接口**（api / host / status / duration）。同时不制造噪音。

## 1. 自动附带的字段

`internal/logging` 的处理器链为：模块等级裁剪 → 重复抑制 → 脱敏 → 运行时缓冲 → 文本/JSON 格式化。

| 字段 | 来源 | 说明 |
|---|---|---|
| `caller` | 记录的 PC 自动解析 | 相对 `internal/` 的文件路径加行号，例如 `engine/reply.go:312` |
| `func` | 记录的 PC 自动解析 | `包名.方法名`，例如 `engine.(*ReplyService).Handle` |
| ctx 关联属性 | `logging.With(ctx, ...)` | 使用 `*Context` 日志方法（`InfoContext` 等）时自动合并；记录自己显式传入的同名键优先 |

封装函数（如 `mtop.logInfo`）必须使用 `logging.LogAt(ctx, logger, skip, ...)`，使 `caller` 指向真正的业务调用方而不是封装函数。

## 2. 关联字段注入点

只在入口注入一次，下游用 `*Context` 方法即可带上：

| 入口 | 注入字段 |
|---|---|
| HTTP `requestLogger` | `request_id`、`req`（方法加路径） |
| `auth.Middleware` | `user_id` |
| 账号连接协调器 `run` | `account` |
| 消息分发 `handleMessageContext` / 防抖回复 | `account`、`order_id`、`trigger`、`chat_id`、`item_id` |
| 自动化运行 `executeRule` | `run_id`、`account`、`order_id`、`trigger`、`rule_id` |
| 调度器每轮扫描 | `job`、`tick_id`（`logging.NewTickID()`） |

## 3. 键名约定

固定键名：`account`、`order_id`、`item_id`、`chat_id`、`run_id`、`rule_id`、`request_id`、`api`、`method`、`path`、`route`、`status`、`duration`、`attempt`、`err`、`trigger`、`job`、`tick_id`、`host`。

- 新代码不得再用 `cookie_id`、`account_id` 作为日志键；运行日志适配器仍兼容读取旧键。
- message 使用“动词 + 对象 + 结果”的固定中文文本，**不拼接变量**；变量一律放进属性。这样同一个调用点的 message 稳定，去重与检索才可靠。
- 凭证、Cookie、Token、密码不得出现在任何字段。第三方 API 卡和 AI 调用只记录 `host`，不记录路径、查询和正文。

## 4. 等级规则

| 等级 | 用于 |
|---|---|
| Debug | 状态说明和空转：冷却中、等待下次扫描、请求开始、未命中规则、GET 读成功 |
| Info | 状态迁移、操作审计（HTTP 写成功）、外部调用完成摘要、业务动作结果、有工作量的调度汇总 |
| Warn | 可恢复的失败：外部调用失败、单个订单或账号执行失败、HTTP 4xx（401/404 除外） |
| Error | 需要人关注：HTTP 5xx、转人工核对（`needs_review`）、风控中止批次、持久化失败 |

HTTP 请求完成日志的等级由 `server.httpLogLevel` 决定；耗时超过 3 秒至少为 Warn 并带 `slow=true`。

## 5. 降噪机制

- **重复抑制**：同一调用点（文件:行号）、等级、message 和关键业务标识（account、order_id、err 等）相同的 Warn/Error，每 60 秒窗口内最多放行 5 条（Error 为 20 条），其余计数。窗口过期后由下一次同类事件或状态表清理时补一条 `已抑制重复日志`，带 `suppressed`、`first_at`、`caller`。Info 及以下不参与抑制。
- **按模块设置等级**：`LOG_LEVEL` 与系统设置 `log_level` 支持 `info,mtop=debug,ws=warn`。模块名取 logger 绑定的 `subsys` 或记录的 `log_source`。非法配置整体拒绝，不会改动现有等级。
- **记录或返回，不要两者都做**：底层只用 `fmt.Errorf("...: %w")` 补充上下文后返回；由边界层（调度器每轮、WS 消息处理、HTTP handler、自动化运行）统一记录一次。外部调用的摘要行是例外。
- **调度器**：一轮扫描只在真正做了事情时输出一行 Info 汇总；等待、冷却、WebSocket 未就绪等逐订单状态说明使用 Debug。

## 6. 新增日志点位检查清单

1. 这条日志是边界层记录，还是底层应该直接返回错误？
2. 使用的是 `*Context` 方法吗？没有 ctx 时，是否自己带了 `account` / `order_id`？
3. message 是否固定文本，变量是否都在属性里？
4. 在循环或定时器里吗？如果是，每轮最多输出几条？是否应改为汇总？
5. 等级是否符合第 4 节？状态说明不要用 Warn。
6. 字段里有没有凭证、完整 URL 或第三方密钥？
7. 新增或修改的函数和变量是否有中文注释，是否补了聚焦测试？
