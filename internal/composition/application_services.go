// Package composition 是进程唯一的跨层装配根。
package composition

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xianyu-go/internal/account"
	"xianyu-go/internal/adapter"
	accountapp "xianyu-go/internal/application/account"
	adminapp "xianyu-go/internal/application/admin"
	analyticsapp "xianyu-go/internal/application/analytics"
	automationapp "xianyu-go/internal/application/automation"
	cardsapp "xianyu-go/internal/application/cards"
	chatapp "xianyu-go/internal/application/chat"
	defaultreplyapp "xianyu-go/internal/application/defaultreply"
	deliveryapp "xianyu-go/internal/application/deliverytemplate"
	itemapp "xianyu-go/internal/application/items"
	keywordsapp "xianyu-go/internal/application/keywords"
	lifecycleapp "xianyu-go/internal/application/lifecycle"
	notificationsapp "xianyu-go/internal/application/notifications"
	orderapp "xianyu-go/internal/application/orders"
	settingsapp "xianyu-go/internal/application/settings"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/chat"
	"xianyu-go/internal/netguard"
	"xianyu-go/internal/notify"
)

const (
	// refreshOrderChunkSize 限制单次订单刷新在应用层处理的账号数量。
	refreshOrderChunkSize = 100
	// publishBatchLease 是发布 worker 对单批次持有租约的最长时间。
	publishBatchLease = 5 * time.Minute
)

// Services 聚合组合根已完成构造的应用服务与后台组件。
// 它只由 composition 创建，随后通过 server.Dependencies 的最小 Port 交给 HTTP transport。
type Services struct {
	// lifecycleContext 返回进程协调器拥有的 worker 根 Context；HTTP transport 只能通过组合层适配器使用它。
	lifecycleContext func() context.Context
	// qrLogin 是二维码平台协议的组合层私有实例，只通过 HTTP 用例 Port 暴露。
	qrLogin adapter.QRLoginService
	// platformCredentials 负责按需读取平台凭证窄视图并执行归属复核。
	platformCredentials *accountapp.PlatformCredentialService
	// orders 是订单 HTTP 适配器；业务服务集合由应用层统一构造。
	orders *orderapp.ServiceSet
	// orderRefreshJobs 是订单刷新任务创建、取消、恢复和 worker 生命周期应用服务。
	orderRefreshJobs *orderapp.RefreshJobService
	// orderReconciliationRecovery 是订单补偿扫描器的应用层生命周期协调器。
	orderReconciliationRecovery *orderapp.ReconciliationRecoveryCoordinator
	// itemSinglePublish 是仅负责单商品发布用例的纯应用服务。
	itemSinglePublish *itemapp.Service
	// itemBatchCoordinator 是商品批量发布 worker、恢复扫描和关闭等待的生命周期拥有者。
	itemBatchCoordinator *itemapp.BatchWorkerCoordinator
	// itemBatchPreview 是商品批量发布表格预检应用服务。
	itemBatchPreview *itemapp.BatchPreviewService
	// itemBatchManagement 是商品批次查询、取消和重试应用服务。
	itemBatchManagement *itemapp.BatchManagementService
	// itemCategoryRecommendation 是商品类目推荐应用服务。
	itemCategoryRecommendation *itemapp.CategoryRecommendationService
	// itemBatchPreviewPersistence 是预检结果持久化应用服务。
	itemBatchPreviewPersistence *itemapp.BatchPreviewPersistenceService
	// itemBatchLocalPublish 是远端发布成功后的本地商品与规则收口服务。
	itemBatchLocalPublish *itemapp.BatchLocalPublishService
	// itemSync 是商品全量和分页同步应用服务。
	itemSync *itemapp.SyncService
	// itemCatalog 是商品列表和详情读取应用服务。
	itemCatalog *itemapp.CatalogService
	// itemCatalogMutation 是商品创建、更新、删除和交付开关应用服务。
	itemCatalogMutation *itemapp.CatalogMutationService
	// accountLogin 是账号登录应用服务。
	accountLogin *accountLoginService
	// authentication 是用户会话、密码校验和登录凭据应用服务。
	authentication *accountapp.AuthenticationService
	// loginAudit 是登录成功审计应用服务，负责方式归一化和账号启用编排。
	loginAudit *accountapp.LoginAuditService
	// passwordLogin 是密码登录策略应用服务；当前只返回关闭策略，不保存登录秘密或会话。
	passwordLogin *accountapp.PasswordLoginService
	// accountDelete 是账号删除应用服务，负责停止 fencing 和归属复核后的持久化删除。
	accountDelete *accountapp.DeleteService
	// accountProfile 是账号资料刷新应用服务。
	accountProfile *accountapp.ProfileService
	// accountLongLogin 是账号长登录设置应用服务。
	accountLongLogin *accountapp.LongLoginService
	// accountSettings 是账号设置、登录信息、启停和暂停应用服务。
	accountSettings *accountapp.SettingsService
	// accountRuntime 是账号凭证写回后的运行时同步与状态快照应用服务。
	accountRuntime *accountapp.RuntimeService
	// accountSummaries 是账号摘要、所有权和管理员账号列表应用服务。
	accountSummaries *accountapp.SummaryService
	// accountTasks 是账号任务设置、历史和手动执行应用服务。
	accountTasks *automationapp.Service
	// credentialWake 是凭证恢复后唤醒自动化任务的应用服务。
	credentialWake *automationapp.CredentialWakeService
	// chat 是聊天历史查询应用服务，负责用户归属和分页编排。
	chat *chatapp.Service
	// uncertainNotifications 是通知不确定状态运维查询应用服务。
	uncertainNotifications *notificationsapp.Service
	// notificationChannels 是通知渠道 CRUD 与账号绑定应用服务。
	notificationChannels *notificationsapp.ChannelService
	// analytics 是订单分析应用服务。
	analytics *analyticsapp.Service
	// automationIssues 是自动化异常查询与人工处理应用服务。
	automationIssues *automationapp.IssueService
	// automationRules 是自动化规则校验、分页和持久化应用服务。
	automationRules *automationapp.RuleService
	// deliveryTemplates 是发货模板 CRUD 应用服务。
	deliveryTemplates *deliveryapp.Service
	// cards 是卡券 CRUD、输入校验和所有权编排应用服务。
	cards *cardsapp.Service
	// apiCardTester 是卡券 API 测试请求端口。
	apiCardTester cardsapp.APIRequestTester
	// publishAutomationRules 是批量发布成功后幂等准备自动化规则的应用服务。
	publishAutomationRules *automationapp.PublishRuleService
	// defaultReplies 是默认回复配置与投递记录应用服务。
	defaultReplies *defaultreplyapp.Service
	// keywords 是关键词规则与指定商品回复应用服务。
	keywords *keywordsapp.Service
	// settings 是系统、用户和账号 AI 设置应用服务。
	settings *settingsapp.Service
	// admin 是管理员用户管理与全局统计应用服务。
	admin *adminapp.Service
}

// LifecycleContext 返回已启动协调器拥有的进程生命周期 Context，供组合层 transport adapter 注册后台 worker。
func (services *Services) LifecycleContext() context.Context {
	if services == nil || services.lifecycleContext == nil {
		return nil
	}
	return services.lifecycleContext()
}

// Dependencies 是进程组合根构造应用服务所需的不可变依赖集合。
// 它不包含 HTTP Server；服务只能使用窄仓储工厂、运行时端口和受控回调。
type Dependencies struct {
	// OrderDependencies 创建订单用例的持久化与运行时适配器。
	OrderDependencies *adapter.OrderDependencies
	// AccountDependencies 创建账号、认证和登录审计用例的持久化适配器。
	AccountDependencies *adapter.AccountDependencies
	// ItemDependencies 创建商品同步、发布和批量 worker 用例的适配器。
	ItemDependencies *adapter.ItemDependencies
	// ChatDependencies 创建聊天发送应用服务。
	ChatDependencies *adapter.ChatDependencies
	// AutomationDependencies 创建自动化唤醒和发布后规则用例。
	AutomationDependencies *adapter.AutomationDependencies
	// TransportApplications 保存已由组合根装配的其余应用服务。
	TransportApplications *adapter.TransportApplicationServices
	// OrderReconciliationRecovery 保存订单补偿扫描生命周期服务。
	OrderReconciliationRecovery *orderapp.ReconciliationRecoveryCoordinator
	// Manager 提供账号运行时应用端口。
	Manager *account.Manager
	// Automation 提供订单手动发货应用端口。
	Automation *automation.Center
	// Notifier 提供订单通知应用端口。
	Notifier *notify.Notifier
	// Chat 提供聊天领域事件服务。
	Chat *chat.Service
	// Logger 记录不含秘密的后台任务诊断。
	Logger *slog.Logger
	// MTopClient 返回当前平台 MTOP 客户端。
	MTopClient func() adapter.MTOPClient
	// LongLoginClient 返回当前平台长登录客户端。
	LongLoginClient func() adapter.LongLoginClient
	// QRLogin 提供二维码生成、轮询和风控完成的底层平台能力。
	QRLogin adapter.QRLoginService
	// UpdateRunningCookie 同步平台响应 Cookie 到账号运行时。
	UpdateRunningCookie func(context.Context, string, string)
	// SessionRecovery 处理平台会话失效后的账号恢复。
	SessionRecovery adapter.SessionRecoveryHandler
	// LifecycleContext 返回进程协调器拥有的应用生命周期 Context。
	LifecycleContext func() context.Context
}

// settingsRuntimeTransport 将账号运行时控制投影给设置用例，并确保启用或 Cookie 重启永远使用进程生命周期 Context。
type settingsRuntimeTransport struct {
	// manager 是账号运行实例和停止 fencing 的唯一拥有者。
	manager *account.Manager
	// lifecycleContext 返回协调器已启动后的进程根 Context，不能由 HTTP 请求提供。
	lifecycleContext func() context.Context
}

// Restart 使用进程生命周期 Context 重新创建账号实例，避免请求断开后留下半完成转换。
func (transport settingsRuntimeTransport) Restart(_ context.Context, accountID string) error {
	// lifecycleCtx 是当前进程拥有的运行实例 Context；nil 表示组合根未正确启动。
	lifecycleCtx := context.Context(nil)
	if transport.lifecycleContext != nil {
		lifecycleCtx = transport.lifecycleContext()
	}
	if lifecycleCtx == nil {
		return errors.New("账号运行时生命周期 Context 未初始化")
	}
	return transport.manager.Restart(lifecycleCtx, accountID)
}

// BeginStopping 建立账号停止 fencing，阻止其他运行时入口在停用转换中重新启动实例。
func (transport settingsRuntimeTransport) BeginStopping(accountID string) bool {
	return transport.manager.BeginStopping(accountID)
}

// EndStopping 释放本次设置转换建立的账号停止 fencing。
func (transport settingsRuntimeTransport) EndStopping(accountID string) {
	transport.manager.EndStopping(accountID)
}

// StopContext 在 HTTP 关闭预算内停止账号实例，并将错误回传给设置用例映射为可重试冲突。
func (transport settingsRuntimeTransport) StopContext(ctx context.Context, accountID string) error {
	return transport.manager.StopContext(ctx, accountID)
}

// LifecycleComponents 返回由组合根登记的应用 worker 生命周期组件。
// 组件只暴露 Start/Close 端口，启动顺序与进程取消责任由 cmd 的协调器拥有。
func (services *Services) LifecycleComponents() []lifecycleapp.NamedComponent {
	if services == nil {
		return nil
	}
	// components 保存按应用依赖登记的 worker 生命周期组件。
	components := make([]lifecycleapp.NamedComponent, 0, 3)
	if services.orderRefreshJobs != nil {
		// service 保存订单刷新恢复与 worker 生命周期应用服务。
		service := services.orderRefreshJobs
		components = append(components, lifecycleapp.NamedComponent{
			Name: "order-refresh-recovery",
			Component: lifecycleapp.FuncComponent{
				StartFunc: service.StartRecovery,
				CloseFunc: service.Close,
			},
		})
	}
	if services.itemBatchCoordinator != nil {
		// coordinator 保存批量发布 worker 与恢复扫描生命周期协调器。
		coordinator := services.itemBatchCoordinator
		components = append(components, lifecycleapp.NamedComponent{
			Name: "publish-batch-workers",
			Component: lifecycleapp.FuncComponent{
				StartFunc: coordinator.StartRecovery,
				CloseFunc: coordinator.Close,
			},
		})
	}
	if services.orderReconciliationRecovery != nil {
		// coordinator 保存订单补偿扫描生命周期协调器。
		coordinator := services.orderReconciliationRecovery
		components = append(components, lifecycleapp.NamedComponent{
			Name: "order-reconciliation-recovery",
			Component: lifecycleapp.FuncComponent{
				StartFunc: coordinator.Start,
				CloseFunc: coordinator.Close,
			},
		})
	}
	return components
}

// UpdateRunningCookie 将平台响应中的 Cookie 变化同步到账号运行时并唤醒受影响任务。
func (services *Services) UpdateRunningCookie(ctx context.Context, accountID, value string) error {
	if services == nil || services.accountRuntime == nil {
		return nil
	}
	return services.accountRuntime.UpdateCookie(ctx, accountID, value)
}

// RecoverExpiredCredential 将已确认的会话失效转交给账号运行时恢复。
func (services *Services) RecoverExpiredCredential(ctx context.Context, accountID string) bool {
	return services != nil && services.accountRuntime != nil && services.accountRuntime.RecoverExpiredCredential(ctx, accountID)
}

// AccountLogin 是 transport 消费 Cookie 与二维码登录用例所需的最小组合层 Port。
type AccountLogin interface {
	CreateCookie(context.Context, string, string, int64, string) error
	UpdateCookie(context.Context, string, string, int64, string, int64) error
	PersistQRLoginSuccess(context.Context, int64, string, map[string]any, string) (CookieLoginResult, error)
	RegisterQRSession(string, int64, time.Time)
	AuthorizeQRSession(string, int64) error
	CleanupQRSessions(time.Time) []string
}

// TransportPorts 是组合层向 transport 投影层交付的只读应用服务快照。
type TransportPorts struct {
	Orders                      *orderapp.ServiceSet
	OrderRefreshJobs            *orderapp.RefreshJobService
	ItemSinglePublish           *itemapp.Service
	ItemBatchPreview            *itemapp.BatchPreviewService
	ItemBatchManagement         *itemapp.BatchManagementService
	ItemCategoryRecommendation  *itemapp.CategoryRecommendationService
	ItemBatchPreviewPersistence *itemapp.BatchPreviewPersistenceService
	ItemBatchLocalPublish       *itemapp.BatchLocalPublishService
	ItemSync                    *itemapp.SyncService
	ItemCatalog                 *itemapp.CatalogService
	ItemCatalogMutation         *itemapp.CatalogMutationService
	AccountLogin                AccountLogin
	QRLogin                     adapter.QRLoginService
	PlatformCredentials         *accountapp.PlatformCredentialService
	Authentication              *accountapp.AuthenticationService
	LoginAudit                  *accountapp.LoginAuditService
	PasswordLogin               *accountapp.PasswordLoginService
	AccountDelete               *accountapp.DeleteService
	AccountProfile              *accountapp.ProfileService
	AccountLongLogin            *accountapp.LongLoginService
	AccountSettings             *accountapp.SettingsService
	AccountRuntime              *accountapp.RuntimeService
	AccountSummaries            *accountapp.SummaryService
	AccountTasks                *automationapp.Service
	Chat                        *chatapp.Service
	UncertainNotifications      *notificationsapp.Service
	NotificationChannels        *notificationsapp.ChannelService
	Analytics                   *analyticsapp.Service
	AutomationIssues            *automationapp.IssueService
	AutomationRules             *automationapp.RuleService
	DeliveryTemplates           *deliveryapp.Service
	Cards                       *cardsapp.Service
	// APICardTester 是卡券 API 测试请求应用端口。
	APICardTester          cardsapp.APIRequestTester
	PublishAutomationRules *automationapp.PublishRuleService
	DefaultReplies         *defaultreplyapp.Service
	Keywords               *keywordsapp.Service
	Settings               *settingsapp.Service
	Admin                  *adminapp.Service
}

// TransportPorts 返回已完成构造的只读服务引用；调用方不得在运行期替换任何字段。
func (services *Services) TransportPorts() TransportPorts {
	if services == nil {
		return TransportPorts{}
	}
	return TransportPorts{
		Orders: services.orders, OrderRefreshJobs: services.orderRefreshJobs, ItemSinglePublish: services.itemSinglePublish,
		ItemBatchPreview: services.itemBatchPreview, ItemBatchManagement: services.itemBatchManagement,
		ItemCategoryRecommendation: services.itemCategoryRecommendation, ItemBatchPreviewPersistence: services.itemBatchPreviewPersistence,
		ItemBatchLocalPublish: services.itemBatchLocalPublish, ItemSync: services.itemSync, ItemCatalog: services.itemCatalog,
		ItemCatalogMutation: services.itemCatalogMutation, AccountLogin: services.accountLogin, QRLogin: services.qrLogin,
		PlatformCredentials: services.platformCredentials, Authentication: services.authentication, LoginAudit: services.loginAudit,
		PasswordLogin: services.passwordLogin, AccountDelete: services.accountDelete, AccountProfile: services.accountProfile,
		AccountLongLogin: services.accountLongLogin, AccountSettings: services.accountSettings, AccountRuntime: services.accountRuntime,
		AccountSummaries: services.accountSummaries, AccountTasks: services.accountTasks, Chat: services.chat,
		UncertainNotifications: services.uncertainNotifications, NotificationChannels: services.notificationChannels,
		Analytics: services.analytics, AutomationIssues: services.automationIssues, AutomationRules: services.automationRules, DeliveryTemplates: services.deliveryTemplates,
		Cards: services.cards, APICardTester: services.apiCardTester, PublishAutomationRules: services.publishAutomationRules, DefaultReplies: services.defaultReplies,
		Keywords: services.keywords, Settings: services.settings, Admin: services.admin,
	}
}

// New 由进程组合根装配全部应用服务，并在启动前拒绝半初始化依赖。
func New(dependencies Dependencies) (*Services, error) {
	if dependencies.OrderDependencies == nil || dependencies.AccountDependencies == nil || dependencies.ItemDependencies == nil || dependencies.ChatDependencies == nil || dependencies.AutomationDependencies == nil || dependencies.TransportApplications == nil || dependencies.OrderReconciliationRecovery == nil || dependencies.Manager == nil || dependencies.MTopClient == nil || dependencies.LongLoginClient == nil || dependencies.QRLogin == nil || dependencies.UpdateRunningCookie == nil || dependencies.LifecycleContext == nil {
		return nil, fmt.Errorf("应用服务组合依赖不完整")
	}
	// transportValidationErr 表示组合根预构造的 transport-facing 服务集合是否存在半初始化字段。
	if transportValidationErr := dependencies.TransportApplications.Validate(); transportValidationErr != nil {
		return nil, fmt.Errorf("transport 应用服务无效: %w", transportValidationErr)
	}
	// orderServices、orderRefreshJobs、orderBuildErr 分别是订单用例集合、刷新任务门面及其构造错误。
	orderServices, orderRefreshJobs, orderBuildErr := buildOrderServices(dependencies)
	if orderBuildErr != nil {
		return nil, orderBuildErr
	}
	// accountProfile 保存账号资料刷新用例的构造结果。
	// accountRepository 提供账号登录、资料摘要和删除共用的数据库适配器。
	accountRepository := dependencies.AccountDependencies.NewAccountLoginRepository()
	// platformCredentials 将账号凭证读取限制在消费者定义的只读应用端口。
	platformCredentials, platformCredentialsErr := accountapp.NewPlatformCredentialService(accountRepository)
	if platformCredentialsErr != nil {
		return nil, fmt.Errorf("构造平台凭证服务失败: %w", platformCredentialsErr)
	}
	// cookieWriterFactory 将明文 Cookie 封装在 adapter 的请求范围实例中，Server 不持有凭证仓储。
	cookieWriterFactory := func(cookies string) accountapp.CookieWriter {
		return adapter.NewAccountCookieWriter(accountRepository, platformCredentials, cookies, dependencies.Logger)
	}
	// cookieUpdaterFactory 将既有账号的明文 Cookie 封装在 adapter 的请求范围实例中。
	cookieUpdaterFactory := func(cookies string) accountapp.CookieUpdater {
		return adapter.NewAccountCookieWriter(accountRepository, platformCredentials, cookies, dependencies.Logger)
	}
	// sessionRecovery 统一适配平台 Session 失效分类、诊断日志和账号运行时恢复。
	sessionRecovery := dependencies.SessionRecovery
	// accountProfile 由应用层服务编排平台资料刷新与非敏感摘要持久化。
	accountProfile, profileErr := accountapp.NewProfileService(accountRepository, adapter.NewAccountProfilePort(accountRepository, dependencies.MTopClient, dependencies.UpdateRunningCookie, sessionRecovery, dependencies.Logger))
	if profileErr != nil {
		return nil, fmt.Errorf("构造账号资料服务失败: %w", profileErr)
	}
	// accountLongLogin 由适配器承载平台调用、Cookie 快照合并和凭证写回。
	accountLongLogin, longLoginErr := accountapp.NewLongLoginService(
		accountRepository,
		adapter.NewLongLoginAdapter(accountRepository, dependencies.LongLoginClient, dependencies.UpdateRunningCookie, dependencies.Logger),
	)
	if longLoginErr != nil {
		return nil, fmt.Errorf("构造账号长登录服务失败: %w", longLoginErr)
	}
	// settingsRuntime 将运行时启停绑定到协调器 Context，避免把请求 Context 错误当成账号实例生命周期。
	settingsRuntime := accountapp.SettingsRuntime(settingsRuntimeTransport{manager: dependencies.Manager, lifecycleContext: dependencies.LifecycleContext})
	// accountSettings、accountSettingsErr 保存账号设置应用服务及其装配错误。
	accountSettings, accountSettingsErr := accountapp.NewSettingsService(dependencies.AccountDependencies.NewAccountSettingsRepository(), settingsRuntime)
	if accountSettingsErr != nil {
		return nil, fmt.Errorf("构造账号设置服务失败: %w", accountSettingsErr)
	}
	// accountSummaryRepository 保存普通用户与管理员共用的摘要查询适配器。
	accountSummaryRepository := dependencies.AccountDependencies.NewAccountSummaryRepository()
	// accountSummaries、accountSummariesErr 保存账号摘要应用服务及其装配错误。
	accountSummaries, accountSummariesErr := accountapp.NewSummaryService(accountSummaryRepository, accountSummaryRepository)
	if accountSummariesErr != nil {
		return nil, fmt.Errorf("构造账号摘要服务失败: %w", accountSummariesErr)
	}
	// credentialWake 负责将凭证恢复后的任务唤醒写入收口到适配器。
	// automationDependencies 提供自动化、默认回复和关键词用例的显式适配器工厂。
	automationDependencies := dependencies.AutomationDependencies
	if automationDependencies == nil {
		return nil, fmt.Errorf("自动化专用依赖未装配")
	}
	// credentialWake、credentialWakeErr 保存凭证恢复后的自动化唤醒应用服务及装配错误。
	credentialWake, credentialWakeErr := automationapp.NewCredentialWakeService(automationDependencies.NewAutomationCredentialWakeRepository())
	if credentialWakeErr != nil {
		return nil, fmt.Errorf("构造凭证恢复唤醒服务失败: %w", credentialWakeErr)
	}
	// accountRuntime 将 Manager 与凭证恢复后的自动化唤醒收口到账号应用端口。
	accountRuntime := accountapp.NewRuntimeService(adapter.NewAccountRuntimePort(dependencies.Manager), credentialWake)
	// loginSuccess 负责登录成功后的资料刷新和启用账号运行时重启，避免 Server 持有业务编排。
	loginSuccess := accountapp.NewLoginSuccessService(accountRepository, accountProfile, accountRepository, accountRuntime, func(message string, err error) {
		if dependencies.Logger != nil {
			dependencies.Logger.Warn(message, "err", err)
		}
	})
	// loginAudit 负责登录方式、账号启用和审计日志写入，生命周期适配器只组合应用端口。
	loginAudit := accountapp.NewLoginAuditService(dependencies.AccountDependencies.NewAccountLoginAuditRepository())
	// loginLifecycle 将手动登录与扫码登录的成功后续动作收口到 adapter，不再反向持有 Server。
	loginLifecycle := adapter.NewAccountLoginLifecycle(loginAudit, loginSuccess, dependencies.Logger)
	// deleteRuntime 是可选的账号运行时端口；显式保持 nil，避免把 nil *Manager 装入非空接口后触发 panic。
	var deleteRuntime accountapp.DeleteRuntime
	if dependencies.Manager != nil {
		deleteRuntime = dependencies.Manager
	}
	// accountDelete 是账号删除用例的构造结果，运行时端口可为空以兼容无 Manager 的测试 Server。
	accountDelete, deleteErr := accountapp.NewDeleteService(accountRepository, deleteRuntime)
	if deleteErr != nil {
		return nil, fmt.Errorf("构造账号删除服务失败: %w", deleteErr)
	}
	// batchServices、batchServicesErr 分别是批量发布服务集合及其组合期错误。
	batchServices, batchServicesErr := buildItemBatchServices(dependencies, sessionRecovery, automationDependencies)
	if batchServicesErr != nil {
		return nil, batchServicesErr
	}
	// accountLoginCreate 是手动 Cookie 登录应用服务的构造结果。
	accountLoginCreate, accountLoginCreateErr := accountapp.NewLoginService(loginLifecycle)
	if accountLoginCreateErr != nil {
		return nil, fmt.Errorf("构造账号登录创建服务失败: %w", accountLoginCreateErr)
	}
	// accountQRLogin 是扫码成功凭证持久化应用服务的构造结果；零值测试 Server 暂不装配数据库端口。
	var accountQRLogin *accountapp.QRLoginService
	if dependencies.AccountDependencies != nil {
		// accountQRLoginErr 保存扫码应用服务装配失败原因。
		var accountQRLoginErr error
		accountQRLogin, accountQRLoginErr = accountapp.NewQRLoginService(dependencies.AccountDependencies.NewQRLoginRepository(), loginLifecycle)
		if accountQRLoginErr != nil {
			return nil, fmt.Errorf("构造扫码登录服务失败: %w", accountQRLoginErr)
		}
	}
	// qrSessionRegistry 将扫码会话所有权、过期回收和幂等结果放在应用边界内。
	qrSessionRegistry := accountapp.NewQRLoginSessionRegistry()
	// catalogServices、catalogServicesErr 分别是商品目录服务集合及其组合期错误。
	catalogServices, catalogServicesErr := buildItemCatalogServices(dependencies, sessionRecovery)
	if catalogServicesErr != nil {
		return nil, catalogServicesErr
	}
	// services 是完成构造后只读注入 Server 的应用服务集合。
	services := &Services{
		lifecycleContext:            dependencies.LifecycleContext,
		qrLogin:                     dependencies.QRLogin,
		platformCredentials:         platformCredentials,
		orders:                      orderServices,
		orderRefreshJobs:            orderRefreshJobs,
		orderReconciliationRecovery: dependencies.OrderReconciliationRecovery,
		itemSinglePublish:           catalogServices.singlePublish,
		itemBatchCoordinator:        batchServices.coordinator,
		itemBatchPreview:            batchServices.preview,
		itemBatchManagement:         batchServices.management,
		itemCategoryRecommendation:  catalogServices.categoryRecommendation,
		itemBatchPreviewPersistence: catalogServices.previewPersistence,
		itemBatchLocalPublish:       batchServices.localPublish,
		itemSync: itemapp.NewSyncService(dependencies.ItemDependencies.NewItemSyncRepository(dependencies.MTopClient, dependencies.Logger, dependencies.UpdateRunningCookie, func(ctx context.Context, cookieID string, err error) {
			if sessionRecovery != nil {
				sessionRecovery(ctx, cookieID, err)
			}
		})),
		itemCatalog:            catalogServices.catalog,
		itemCatalogMutation:    catalogServices.mutation,
		accountLogin:           &accountLoginService{cookieWriterFactory: cookieWriterFactory, cookieUpdaterFactory: cookieUpdaterFactory, sessionPort: accountRepository, createApplication: accountLoginCreate, qrApplication: accountQRLogin, qrSessions: qrSessionRegistry},
		authentication:         nil,
		loginAudit:             loginAudit,
		passwordLogin:          accountapp.NewPasswordLoginService(),
		accountDelete:          accountDelete,
		accountProfile:         accountProfile,
		accountLongLogin:       accountLongLogin,
		accountSettings:        accountSettings,
		accountRuntime:         accountRuntime,
		accountSummaries:       accountSummaries,
		accountTasks:           dependencies.TransportApplications.AccountTasks,
		credentialWake:         credentialWake,
		chat:                   dependencies.ChatDependencies.NewChatSendingApplication(dependencies.Chat, dependencies.Manager, dependencies.MTopClient),
		uncertainNotifications: dependencies.TransportApplications.UncertainNotifications,
		notificationChannels:   dependencies.TransportApplications.NotificationChannels,
		analytics:              dependencies.TransportApplications.Analytics,
		automationRules:        dependencies.TransportApplications.AutomationRules,
		deliveryTemplates:      dependencies.TransportApplications.DeliveryTemplates,
		cards:                  dependencies.TransportApplications.Cards,
		apiCardTester:          dependencies.TransportApplications.APICardTester,
		publishAutomationRules: dependencies.TransportApplications.PublishAutomationRules,
		automationIssues:       dependencies.TransportApplications.AutomationIssues,
		defaultReplies:         dependencies.TransportApplications.DefaultReplies,
		keywords:               dependencies.TransportApplications.Keywords,
		settings:               dependencies.TransportApplications.Settings,
		admin:                  dependencies.TransportApplications.Admin,
	}
	// authentication、authenticationErr 分别是认证应用服务及其构造错误。
	authentication, authenticationErr := accountapp.NewAuthenticationService(dependencies.AccountDependencies.NewAuthenticationRepository())
	if authenticationErr != nil {
		return nil, authenticationErr
	}
	services.authentication = authentication
	return services, nil
}

// buildOrderServices 构造订单用例集合及其刷新 worker 门面，并把后台诊断限制在组合根。
func buildOrderServices(dependencies Dependencies) (*orderapp.ServiceSet, *orderapp.RefreshJobService, error) {
	// orderRepository 保存订单应用服务共享的基础设施适配器。
	orderRepository := dependencies.OrderDependencies.NewOrderRepository()
	// orderReconciliation 保存订单补偿写入应用 Port 的数据库适配器。
	orderReconciliation := dependencies.OrderDependencies.NewOrderReconciliationRepository()
	// chatRefreshCallback 保存订单同步未匹配会话时按需刷新联系人缓存的组合回调。
	chatRefreshCallback := adapter.NewChatConversationRefreshCallback(adapter.NewChatRefreshProvider(dependencies.Chat, dependencies.Manager))
	// orderRuntime 保存订单服务共享的运行时能力适配器。
	orderRuntime := adapter.NewOrderRuntimeAdapter(dependencies.OrderDependencies, dependencies.Manager, dependencies.Automation, dependencies.Notifier, dependencies.MTopClient, dependencies.UpdateRunningCookie, dependencies.SessionRecovery, dependencies.Logger, orderReconciliation, chatRefreshCallback)
	// orderServices 保存应用层统一构造的订单业务服务集合。
	orderServices := orderapp.NewServiceSet(orderRepository, orderRepository, orderRuntime, orderRuntime, dependencies.OrderDependencies.NewOrderRefreshJobRepository(), refreshOrderChunkSize)
	// orderRefreshRunner 是订单刷新后台 worker 与恢复扫描的生命周期拥有者。
	orderRefreshRunner, orderRefreshRunnerErr := orderapp.NewRefreshJobRunner(
		orderServices.RefreshJobs,
		orderServices.Refresh,
		orderapp.RefreshJobRunnerOptions{
			OnWorkerError: func(jobID string, err error) {
				if dependencies.Logger != nil {
					dependencies.Logger.Warn("订单刷新后台任务结束", "job_id", jobID, "err", err)
				}
			},
			OnRecoveryError: func(err error) {
				if dependencies.Logger != nil {
					dependencies.Logger.Warn("扫描订单刷新恢复任务失败", "err", err)
				}
			},
		},
	)
	if orderRefreshRunnerErr != nil {
		return nil, nil, fmt.Errorf("构造订单刷新运行器失败: %w", orderRefreshRunnerErr)
	}
	// orderRefreshJobs 是订单刷新任务应用 facade，封装归属、租约和 worker 启动编排。
	orderRefreshJobs, orderRefreshJobsErr := orderapp.NewRefreshJobService(orderServices.RefreshJobs, orderServices.Refresh, orderRefreshRunner, orderapp.RefreshJobServiceOptions{})
	if orderRefreshJobsErr != nil {
		return nil, nil, fmt.Errorf("构造订单刷新应用服务失败: %w", orderRefreshJobsErr)
	}
	return orderServices, orderRefreshJobs, nil
}

// readBatchImageFile 从上传目录读取一张受路径边界保护的发布图片。
func readBatchImageFile(uploadDir, reference string) ([]byte, string, string, error) {
	// relativePath 是上传目录内的清理后相对图片路径，绝不允许越界到宿主文件系统。
	relativePath := filepath.Clean(strings.TrimSpace(reference))
	if relativePath == "." || filepath.IsAbs(relativePath) || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return nil, "", "", fmt.Errorf("图片路径无效")
	}
	// root、openErr 分别是受限上传目录句柄及其打开错误。
	root, openErr := os.OpenRoot(uploadDir)
	if openErr != nil {
		return nil, "", "", fmt.Errorf("打开图片目录失败")
	}
	defer root.Close()
	// file、fileErr 分别是受目录边界保护的图片文件及其读取错误。
	file, fileErr := root.Open(relativePath)
	if fileErr != nil {
		return nil, "", "", fmt.Errorf("读取图片失败: %s", reference)
	}
	defer file.Close()
	// data、readErr 分别是受 10 MiB 限制的图片字节及其读取错误。
	data, readErr := io.ReadAll(io.LimitReader(file, (10<<20)+1))
	if readErr != nil || len(data) == 0 || len(data) > 10<<20 {
		return nil, "", "", fmt.Errorf("读取图片失败或文件过大: %s", reference)
	}
	// contentType 是按实际字节识别的媒体类型，用于拒绝伪装的非图片文件。
	contentType := http.DetectContentType(data)
	if !strings.HasPrefix(contentType, "image/") {
		return nil, "", "", fmt.Errorf("不是有效图片: %s", reference)
	}
	return data, contentType, filepath.Base(relativePath), nil
}

// downloadImageURL 仅从公网地址下载受大小和媒体类型限制的发布图片。
func downloadImageURL(ctx context.Context, rawURL string) ([]byte, string, error) {
	// request、requestErr 分别是受调用方取消控制的公网图片下载请求及其构造错误。
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if requestErr != nil || (request.URL.Scheme != "http" && request.URL.Scheme != "https") || request.URL.Hostname() == "" {
		return nil, "", fmt.Errorf("图片 URL 无效: %s", rawURL)
	}
	// client 是限制内网访问的公网 HTTP 客户端；response、responseErr 是下载响应及其网络错误。
	client := netguard.ConfiguredHTTPClient(30 * time.Second)
	// response、responseErr 分别是远端图片响应及网络失败；正文必须在本函数返回前关闭。
	response, responseErr := client.Do(request)
	if responseErr != nil {
		return nil, "", fmt.Errorf("下载图片失败: %s", rawURL)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, "", fmt.Errorf("下载图片失败: %s HTTP %d", rawURL, response.StatusCode)
	}
	// data、readErr 分别是受大小限制的远端图片字节及其读取错误。
	data, readErr := io.ReadAll(io.LimitReader(response.Body, (10<<20)+1))
	if readErr != nil || len(data) > 10<<20 {
		return nil, "", fmt.Errorf("读取远程图片失败: %s", rawURL)
	}
	// contentType 是响应声明或字节探测得到的媒体类型。
	contentType := strings.Split(response.Header.Get("Content-Type"), ";")[0]
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = http.DetectContentType(data)
	}
	if !strings.HasPrefix(contentType, "image/") {
		return nil, "", fmt.Errorf("远程文件不是图片: %s", rawURL)
	}
	return data, contentType, nil
}

// publishBatchFailure 将应用层的远端不确定和后置持久化错误转换为稳定批次失败分类。
func publishBatchFailure(err error, batchStatus string) (string, string) {
	// message、failureKind 分别是可展示失败摘要和持久化的稳定失败分类。
	message := err.Error()
	// failureKind 是批次记录使用的稳定失败分类，供后续恢复流程避免误重试。
	failureKind := "publish"
	// uncertainErr、postErr 用于识别远端结果不确定和本地收口失败两种不可自动重试情形。
	var uncertainErr *itemapp.UncertainRemotePublishError
	// postErr 指向远端发布后本地持久化失败的应用错误，结果必须转人工核对。
	var postErr *itemapp.PostPublishError
	if errors.As(err, &uncertainErr) {
		failureKind = "uncertain_remote"
		message += "；远端结果未能可靠落库，禁止自动重试，请人工核对闲鱼商品列表"
	} else if errors.As(err, &postErr) {
		failureKind = "post_publish"
	}
	if batchStatus == "canceled" || batchStatus == "canceling" {
		if failureKind == "uncertain_remote" {
			message = "任务已取消；" + message
		} else {
			message = "任务已取消"
		}
	}
	return message, failureKind
}
