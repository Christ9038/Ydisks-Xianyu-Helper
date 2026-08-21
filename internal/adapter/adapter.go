// Package adapter 是账号运行时与外部能力（风控验证、通知、自动化中心）的装配层。
//
// 它实现 engine.Handler 与 automation.OrderDetailFetcher，把系统事件转发到自动化中心、
// 把订单详情抓取/凭证续期接到 Go 协议客户端、把账号告警推到通知器。业务逻辑集中在此，
// cmd/server 只负责构造与接线。
package adapter

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	accountmanager "xianyu-go/internal/account"
	accountapp "xianyu-go/internal/application/account"
	automationapp "xianyu-go/internal/application/automation"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/browser"
	"xianyu-go/internal/chat"
	"xianyu-go/internal/db"
	"xianyu-go/internal/notify"
	"xianyu-go/internal/renewal"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
	xrenew "xianyu-go/internal/xianyu/renew"
)

// browserManager 只暴露风控验证能力。普通 Token、Cookie 续期、订单和
// WebSocket 流程不得通过 Chromium 实现。
// browserManager 用于本次流程后续判断的浏览器Manager
type browserManager interface {
	TokenCaptchaRecover(ctx context.Context, cookieID, cookieStr, verificationURL string, headless bool, provider browser.TokenCaptchaURLProvider) (string, error)
}

// browserTokenCaptchaRecoverer 用于本次流程后续判断的浏览器令牌CaptchaRecoverer
type browserTokenCaptchaRecoverer interface {
	TokenCaptchaRecover(ctx context.Context, cookieID, cookieStr, verificationURL string, headless bool, provider browser.TokenCaptchaURLProvider) (string, error)
}

// browserTokenCaptchaEngineRecoverer 用于本次流程后续判断的浏览器令牌CaptchaEngineRecoverer
type browserTokenCaptchaEngineRecoverer interface {
	TokenCaptchaRecoverWithEngine(ctx context.Context, cookieID, cookieStr, verificationURL string, headless bool, provider browser.TokenCaptchaURLProvider) (cookies, engine string, err error)
}

// browserTokenCaptchaSnapshotReader 用于本次流程后续判断的浏览器令牌CaptchaSnapshotReader
type browserTokenCaptchaSnapshotReader interface {
	TokenCaptchaCookieSnapshot(ctx context.Context, cookieID string, headless bool) (cookies string, snapshot []cookierefresh.BrowserCookie, err error)
}

// tokenCaptchaRequester 用于本次流程后续判断的令牌CaptchaRequester
type tokenCaptchaRequester interface {
	RequestFreshCaptchaURLContext(ctx context.Context, cookiesStr, deviceID string) (*mtop.FreshCaptchaResult, error)
}

// orderDetailClient 用于本次流程后续判断的订单DetailClient
type orderDetailClient interface {
	FetchOrderDetail(ctx context.Context, cookiesStr, orderID string) (*mtop.OrderDetailResult, error)
}

// Adapter 实现 engine.Handler 与 automation.OrderDetailFetcher，
// 把系统消息、订单详情抓取和协议级凭证续期接到 Go 客户端与自动化中心。
//
// 自动发货只走 automation.Center；用户聊天消息由 Account 内部 ReplyService 处理，
// 故 HandleChatMessage 为空实现。
// Adapter 用于本次流程后续判断的Adapter
type Adapter struct {
	store      *db.Store
	browser    browserManager
	logger     *slog.Logger
	automation *automation.Center
	notifier   notifyNotifier
	// credentialWake 负责凭证写回后的自动化任务唤醒；Adapter 只依赖应用端口，不直接决定任务状态。
	credentialWake accountapp.CredentialWakePort
	renewSvc       xrenew.Service
	cooldown       *renewal.CooldownManager
	captchaReq     tokenCaptchaRequester
	orderMTop      orderDetailClient
	chat           *chat.Service

	orderFetchMu   sync.Mutex
	lastOrderFetch time.Time
	// passwordCoordinator 按账号协调协议凭证恢复，避免重复外部续期并允许不同账号并行执行。
	passwordCoordinator *accountapp.CredentialRefreshCoordinator
	// passwordResultMu 仅保护 passwordResults；持锁期间不执行凭证读取或平台 I/O。
	passwordResultMu sync.Mutex
	// passwordResults 保存正在执行的协议续期结果，后到调用方可等待同一账号的结果。
	passwordResults map[string]*passwordRenewalResult
}

// passwordRenewalResult 是同账号并发续期调用共享的完成信号与最终成功状态。
type passwordRenewalResult struct {
	// done 由首次续期调用关闭，所有等待者只接收不关闭。
	done chan struct{}
	// renewed 在关闭 done 前写入，关闭 channel 建立等待者读取该字段的 happens-before 关系。
	renewed bool
}

// notifyNotifier 是 *notify.Notifier 的最小接口，避免 adapter 直接依赖 notify 包
// （notify 包未来若反向引用 adapter 也不会形成循环）。
// notifyNotifier 用于本次流程后续判断的notifyNotifier
type notifyNotifier interface {
	NotifyAccountAlert(cookieID, level, title, body string)
}

// notifyEventNotifier 用于本次流程后续判断的notifyEventNotifier
type notifyEventNotifier interface {
	NotifyAccountEvent(cookieID, eventType, level, title, body string)
}

// New 构造可隔离测试的 Adapter；生产进程必须使用 NewRuntimeBundle 完成不可变运行时装配。
func New(store *db.Store, bm *browser.Manager, logger *slog.Logger) *Adapter {
	if logger == nil {
		logger = slog.Default()
	}
	return &Adapter{
		store:               store,
		browser:             browserManagerOrNil(bm),
		logger:              logger,
		credentialWake:      newCredentialWakeService(store),
		cooldown:            renewal.GlobalCooldown,
		captchaReq:          mtop.NewClient(),
		orderMTop:           mtop.NewClient(),
		passwordCoordinator: accountapp.NewCredentialRefreshCoordinator(),
		passwordResults:     make(map[string]*passwordRenewalResult),
	}
}

// RuntimeBundle 聚合账号运行时闭环所需的适配器、账号管理器、通知器、自动化中心和聊天服务。
// 它只在构造期解决 Adapter、Manager 与 Automation 的有向环，组件启动前不向外暴露半成品。
type RuntimeBundle struct {
	// Adapter 是 engine.Handler 与订单详情抓取端口的实现。
	Adapter *Adapter
	// Manager 是拥有每个已启用账号运行实例的 supervisor。
	Manager *accountmanager.Manager
	// Notifier 是账号和自动化事件的通知实现。
	Notifier *notify.Notifier
	// Automation 是处理自动化任务和订单详情动作的统一中心。
	Automation *automation.Center
	// Chat 是处理聊天持久化与实时事件的领域服务。
	Chat *chat.Service
}

// NewRuntimeBundle 在进程启动前一次性完成运行时闭环装配，禁止通过运行期 setter 补齐必需依赖。
func NewRuntimeBundle(store *db.Store, bm *browser.Manager, logger *slog.Logger) (*RuntimeBundle, error) {
	if store == nil {
		return nil, fmt.Errorf("运行时装配需要数据库存储")
	}
	if logger == nil {
		logger = slog.Default()
	}
	// runtimeAdapter 是尚未启动的事件与平台适配器，后续字段只在本构造函数内写入。
	runtimeAdapter := New(store, bm, logger)
	// chatService 是账号实时消息落库和广播服务，必须先于账号引擎启动完成注入。
	chatService := chat.New(store)
	// manager 是自动化中心的在线发送器来源，同时在启动期把 Adapter 固定为账号事件处理器。
	manager := accountmanager.NewManager(store, runtimeAdapter, logger)
	// notifier 是自动化与账号告警共用的通知出口，构造完成后不可替换。
	notifier := notify.New("", store, logger)
	// automationSenders 为自动化图片卡密注入“临时下载、平台上传、WebSocket 发送”链路，不在本地保存图片。
	automationSenders := NewAutomationImageSenderProvider(store, manager, func() mtop.Client { return mtop.NewClient() })
	// autoCenter 依赖已构造但尚未启动的 manager 与 adapter，避免运行期形成部分可用状态。
	autoCenter := automation.NewWithDependencies(store, automationSenders, logger, automation.CenterDependencies{
		OrderDetailFetcher: runtimeAdapter,
		Notifier:           notifier,
		APICardFetcher:     newAPIDeliveryClient(store, logger),
	})
	runtimeAdapter.chat = chatService
	runtimeAdapter.automation = autoCenter
	runtimeAdapter.notifier = notifier
	return &RuntimeBundle{
		Adapter:    runtimeAdapter,
		Manager:    manager,
		Notifier:   notifier,
		Automation: autoCenter,
		Chat:       chatService,
	}, nil
}

// newCredentialWakeService 构造凭证唤醒应用服务；数据库仓储仅在适配器边界内创建并隐藏。
func newCredentialWakeService(store *db.Store) accountapp.CredentialWakePort {
	if store == nil {
		return nil
	}
	// repository 保存凭证唤醒所需的窄数据库适配器。
	repository := NewAutomationCredentialWakeRepository(store)
	// service、err 保存凭证唤醒应用服务及其装配错误。
	service, err := automationapp.NewCredentialWakeService(repository)
	if err != nil {
		return nil
	}
	return service
}

// browserManagerOrNil 把 *browser.Manager 转为接口；nil 时返回 nil 接口。
func browserManagerOrNil(bm *browser.Manager) browserManager {
	if bm == nil {
		return nil
	}
	return bm
}

// SetAutomation 注入系统事件的自动化中心；该 setter 仅保留给构造环过渡和隔离测试，待生产装配改为构造注入且旧测试清零后删除。
func (a *Adapter) SetAutomation(c *automation.Center) { a.automation = c }

// SetNotifier 注入账号告警通知器；该 setter 仅保留给构造环过渡和隔离测试，待生产装配改为构造注入且旧测试清零后删除。
func (a *Adapter) SetNotifier(n notifyNotifier) { a.notifier = n }

// SetCredentialWakeService 注入凭证恢复后的自动化唤醒端口；仅供构造环过渡和隔离测试，待统一应用装配后删除。
func (a *Adapter) SetCredentialWakeService(service accountapp.CredentialWakePort) {
	a.credentialWake = service
}

// SetBrowser 覆盖浏览器实现；生产启动后不得替换必需依赖，待测试改用构造选项后删除此兼容入口。
func (a *Adapter) SetBrowser(b browserManager) { a.browser = b }

// SetRenewService 覆盖轻量续期服务；该 setter 是测试替身覆盖点，待续期端口构造注入并迁移现有测试后删除。
func (a *Adapter) SetRenewService(s xrenew.Service) { a.renewSvc = s }

// SetTokenCaptchaRequester 覆盖 token 风控验证链接刷新器；仅供测试隔离网络，冻结验证码生产路径不得运行时替换。
func (a *Adapter) SetTokenCaptchaRequester(r tokenCaptchaRequester) { a.captchaReq = r }

// SetOrderDetailClient 覆盖纯 Go 订单详情客户端；该 setter 是测试替身覆盖点，待订单端口构造注入并迁移测试后删除。
func (a *Adapter) SetOrderDetailClient(c orderDetailClient) { a.orderMTop = c }

// SetChatService 注入用户聊天旁路服务；它只负责持久化和广播，不改变自动回复路径，待构造环拆除后删除兼容 setter。
func (a *Adapter) SetChatService(service *chat.Service) { a.chat = service }

// HandleChatMessage 用户聊天消息由 Account 内部 ReplyService 处理，此处空实现满足接口。
