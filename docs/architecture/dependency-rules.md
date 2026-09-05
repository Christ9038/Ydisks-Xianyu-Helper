# 架构依赖与边界规则

## 1. 目的

本文定义重构期间和目标状态下的依赖方向。过渡期允许保留已有直接依赖，
但禁止新增同类耦合；每个正式阶段必须减少而不是扩大例外。阶段编号、状态和交付规则只由
refactoring-master-plan.md 定义，本文不声明当前阶段或完成状态。

## 2. 通用规则

- 接口由能力消费者定义，不在实现包中建立包含所有方法的万能接口；
- 上层依赖抽象，下层实现抽象；
- 不为消除 import 而创建没有业务语义的中转包；
- 不允许全局 service locator；
- 必需依赖在构造阶段提供并验证，不使用运行时 setter 回填；
- package 不得因测试方便而导出生产不需要的内部状态；
- 不允许通过 `any`、动态 map 或反射绕过清晰依赖。

## 3. Go 目标边界

### 3.1 `cmd`

允许：

- 读取配置和环境；
- 构造日志、数据库和应用；
- 处理系统信号；
- 调用应用 Start/Run/Stop。

禁止新增：

- 业务规则；
- SQL；
- HTTP handler；
- 平台协议解析；
- 账号状态机。

### 3.2 `internal/server`

允许：

- 路由和 middleware；
- 鉴权上下文；
- 请求解析、验证和 DTO；
- 调用应用服务；
- HTTP/WebSocket transport；
- SPA 静态资源。

目标状态禁止：

- 直接导入 `internal/db`；
- 直接导入 `internal/xianyu` 或 `internal/browser`；
- 调用 `BeginTx`；
- 保存业务 worker、锁、cancel map 或平台会话状态；
- 直接实现订单同步、商品发布、凭证续期或自动化动作。

过渡期已有依赖可以在对应正式阶段完成前保留，但不得新增新的直接调用点。

### 3.3 应用服务

允许：

- 编排一个完整用户用例；
- 所有权校验；
- 事务边界；
- 调用领域服务和消费者定义的 port；
- 将基础设施错误转换为应用错误。

禁止：

- 依赖 `http.Request`、`http.ResponseWriter` 或 chi；
- 依赖 React/前端字段别名；
- 拼装 SQL；
- 直接读取环境变量；
- 返回包含明文秘密的通用模型。

### 3.4 `internal/db`

允许：

- SQL、迁移、方言差异；
- repository 实现；
- 加密存储；
- 事务实现；
- 持久化查询模型。

禁止：

- 导入 server、应用服务、engine 或 automation；
- 依赖 HTTP DTO；
- 调用 MTOP、WebSocket 或 browser；
- 决定用户可见错误消息；
- 将敏感持久化模型直接暴露给 HTTP。

### 3.5 `internal/xianyu` 与 `internal/browser`

允许：

- 平台协议、传输和浏览器实现；
- 返回平台级结果和错误分类；
- 实现应用层或领域层定义的最小接口。

禁止：

- 导入 server；
- 直接写业务数据库；
- 决定自动化规则或 HTTP 状态；
- 反向启动 account manager；
- 绕过冻结滑块规范。

### 3.6 `internal/engine` 与 `internal/automation`

- Engine 负责单账号消息运行时，不负责 HTTP；
- Automation 负责规则运行和动作语义，不负责 HTTP DTO；
- 两者不得依赖 Server；
- 新依赖必须通过最小接口注入；
- 并发状态必须由明确组件拥有，禁止共享无边界的可变结构；
- 外部动作必须保留幂等、checkpoint 和结果不确定语义。

## 4. 数据与秘密边界

- AccountSummary 不包含 Cookie、Token、密码或加密 metadata；
- AccountCredential 只能进入平台调用和凭证更新流程；
- AccountLoginSecret 仅作为历史密码登录模型或明确授权的续期流程输入；当前产品密码登录入口和自动恢复路径均已关闭，生产账号恢复不得读取或调用浏览器密码登录；
- 所有权查询只能返回存在性或非敏感 ID；
- HTTP DTO 不得引用敏感模型；
- 日志、通知和错误响应不得包含秘密；
- 测试失败输出不得打印完整秘密；
- 锁住账号凭证期间不得执行未明确允许的慢外部 I/O。

## 5. API 边界

- 新 API 使用 `/api/v1`；
- 新请求和响应必须使用具名 DTO；
- 新错误使用统一错误 envelope；
- 禁止新增 HTTP 200 + `success:false`；
- 禁止新增 `value/cookie`、`remark/note` 一类双重字段；
- 兼容字段只能在边界 adapter 中归一；
- 删除兼容层必须有调用方迁移和契约测试证据。
- `/api/v1/**` 与 `/health` 的唯一 HTTP 契约源是 `api/openapi.yaml`；新增 operation 必须同步更新规范、生成
  TypeScript schema 和真实 handler 响应校验，禁止恢复手写 `transport.ts` 或 DTO 名单门禁。

## 6. React 边界

目标结构为 `app -> features -> shared`：

- `app` 负责 shell、路由和顶层认证；
- feature 负责自己的页面、Hook、组件、API adapter 和类型；
- shared 只包含真正跨 feature 的请求客户端、UI、Hook 和工具；
- feature 不得导入另一个 feature 的内部文件；
- shared 不得反向导入 feature；
- 组件不得直接调用 `fetch` 或 `axios`；
- 通用 HTTP client 不包含订单、账号等领域归一逻辑；
- 生成 API 类型只读，UI model 由 feature adapter 创建；
- 生成 schema 只能由 shared 契约层导入，feature、组件和 Hook 不得直接读取；
- 原始 `get/post/put/del/postForm` 只能留在旧客户端兼容实现，feature 不得导入或调用；
- 禁止通过大型 barrel 文件隐藏实际依赖。

## 7. 事务与生命周期边界

- 跨 repository 原子操作由应用服务通过 Unit of Work 执行；
- handler 和 React 层不得控制数据库事务；
- goroutine 必须有明确 owner、Context 和 Wait；
- channel 只能由约定的发送方关闭；
- Start 前不隐式运行后台任务；
- Stop/Close 必须幂等；
- 不得持锁等待无法控制的网络、浏览器或用户操作；
- 凭证锁、自动化卡密锁和 worker 锁的顺序必须被注释和测试保护。

## 8. 自动门禁路线

门禁应按目标边界逐步启用：

1. 当前正式阶段开始前先启用该阶段的 fail-closed AST、依赖或行为门禁；当前阶段违规必须立即失败。
2. 已完成阶段的门禁永久保留；后续阶段专有门禁只在该阶段成为当前阶段时启用。
3. 阶段目标边界禁止使用白名单、baseline、忽略目录或降级告警豁免；阶段内失败只能通过完成迁移消除。
4. 最终使用 `go list`、AST 与前端依赖规则检查完整依赖图，不能只依赖少量名称匹配。
5. 仅生成物和冻结文件可有精确范围排除；它们不是架构违规的通用例外，也不能覆盖生产源码。
6. 禁止为了让门禁通过而把依赖藏进反射、动态 import、service locator 或无语义中转层。
