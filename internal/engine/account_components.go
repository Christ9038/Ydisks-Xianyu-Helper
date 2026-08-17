package engine

import (
	"log/slog"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// accountRuntimeComponents 统一拥有单账号运行时的可变状态、任务生命周期和消息分发器。
// 各子组件继续分别保护自己的字段与锁；该聚合对象只负责明确它们属于同一个账号运行时，
// 不允许把这些状态提升为包级共享变量，也不负责执行外部 I/O。
type accountRuntimeComponents struct {
	// credentialState 拥有 Cookie、Token、设备指纹和刷新串行化状态。
	credentialState
	// credentials 拥有凭证读取、续期、Token 缓存和外部回调边界；它固定服务于本账号 facade。
	credentials credentialCoordinator
	// accountRuntimeState 拥有 WebSocket 连接、状态快照和失败诊断计数。
	accountRuntimeState
	// lifecycle 拥有账号运行 Context、任务接入 fencing 以及 Stop/Wait 语义。
	lifecycle accountLifecycle
	// connection 连接协调器拥有 WebSocket 拨号、注册、重连和会话轮换编排。
	connection connectionCoordinator
	// messageDispatcher 拥有消息去重、防抖和并发投递状态。
	messageDispatcher
	// outgoing 拥有出站消息、聊天历史和会话查询的连接快照边界。
	outgoing outgoingMessageCoordinator
	// pendingRenewal 拥有迟到续期任务的登记、取消和 Join 语义。
	pendingRenewal pendingRenewalCoordinator
}

// accountDependencies 固定单账号运行时所需的基础设施和业务端口。
// 这些依赖在 New 中一次性装配，运行期间不可通过 setter 替换；需要替身的测试应使用 Config 注入。
type accountDependencies struct {
	// store 提供账号凭证、Token 缓存和自动回复所需的持久化端口。
	store *db.Store
	// mtop 提供 Token、登录态和平台 API 调用能力。
	mtop mtop.Client
	// renewer 提供启动阶段的 API 优先 Cookie 续期能力；为空表示不启用该流程。
	renewer cookieRenewer
	// wsDialer 提供 WebSocket 拨号能力，并隔离真实网络握手。
	wsDialer WSDialer
	// handler 接收系统事件、账号告警和凭证恢复回调。
	handler Handler
	// logger 记录账号运行时诊断信息，不得写入凭证原文。
	logger *slog.Logger
	// reply 提供账号内部的聊天回复链；它由 store 存在时创建。
	reply *ReplyService
	// recorder 拥有 WebSocket 报文记录 worker 的队列与等待边界。
	recorder *wsRecorder
}

// newAccountDependencies 创建通过构造函数验证后的账号基础设施依赖快照。
// store 是账号运行时使用的持久化端口；client 是固定 MTOP 客户端；renewer 是可选续期端口；
// dialer 是 WebSocket 拨号器；handler 是事件与告警端口；logger 是脱敏日志器；reply 是回复服务；
// recorder 是报文记录 worker owner。
func newAccountDependencies(store *db.Store, client mtop.Client, renewer cookieRenewer, dialer WSDialer, handler Handler, logger *slog.Logger, reply *ReplyService, recorder *wsRecorder) accountDependencies {
	return accountDependencies{
		store: store, mtop: client, renewer: renewer, wsDialer: dialer,
		handler: handler, logger: logger, reply: reply, recorder: recorder,
	}
}
