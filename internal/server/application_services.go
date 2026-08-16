package server

import (
	"context"

	"xianyu-go/internal/adapter"
	accountapp "xianyu-go/internal/application/account"
	adminapp "xianyu-go/internal/application/admin"
	analyticsapp "xianyu-go/internal/application/analytics"
	automationapp "xianyu-go/internal/application/automation"
	cardsapp "xianyu-go/internal/application/cards"
	chatapp "xianyu-go/internal/application/chat"
	defaultreplyapp "xianyu-go/internal/application/defaultreply"
	itemapp "xianyu-go/internal/application/items"
	keywordsapp "xianyu-go/internal/application/keywords"
	notificationsapp "xianyu-go/internal/application/notifications"
	orderapp "xianyu-go/internal/application/orders"
	settingsapp "xianyu-go/internal/application/settings"
)

// applicationServices 聚合 Server 使用的应用服务实例，统一管理共享基础设施依赖。
type applicationServices struct {
	// orders 是订单 HTTP 适配器；业务服务集合由应用层统一构造。
	orders *orderHTTPAdapter
	// itemSinglePublish 是仅负责单商品发布用例的纯应用服务。
	itemSinglePublish *itemapp.Service
	// itemBatchRunner 是商品批量发布 worker 的应用层编排器。
	itemBatchRunner *itemapp.BatchRunner
	// itemBatchRecovery 是过期批量发布任务的租约接管应用服务。
	itemBatchRecovery *itemapp.BatchRecoveryService
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
	// cards 是卡券 CRUD、输入校验和所有权编排应用服务。
	cards *cardsapp.Service
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

// newApplicationServices 为指定 Server 装配全部应用服务实例。
func newApplicationServices(server *Server) *applicationServices {
	// orderRepository 保存订单应用服务共享的基础设施适配器。
	orderRepository := server.dependencies.NewOrderRepository()
	// orderReconciliation 保存订单补偿写入应用 Port 的数据库适配器。
	orderReconciliation := server.dependencies.NewOrderReconciliationRepository()
	// orderRuntime 保存订单服务共享的运行时能力适配器。
	orderRuntime := newServerOrderRuntime(server, orderReconciliation)
	// orderServices 保存应用层统一构造的订单业务服务集合。
	orderServices := orderapp.NewServiceSet(orderRepository, orderRepository, orderRuntime, orderRuntime, server.dependencies.NewOrderRefreshJobRepository(), refreshOrderChunkSize)
	// accountProfile 保存账号资料刷新用例的构造结果。
	// accountRepository 提供账号登录、资料摘要和删除共用的数据库适配器。
	accountRepository := server.dependencies.NewAccountLoginRepository()
	// accountProfile 由应用层服务编排平台资料刷新与非敏感摘要持久化。
	accountProfile, profileErr := accountapp.NewProfileService(accountRepository, adapter.NewAccountProfilePort(accountRepository, server.mtopClient, server.updateRunningCookie, server.recoverExpiredMTOPSession, server.Logger))
	if profileErr != nil {
		panic(profileErr)
	}
	// accountLongLogin 由适配器承载平台调用、Cookie 快照合并和凭证写回。
	accountLongLogin, longLoginErr := accountapp.NewLongLoginService(
		accountRepository,
		adapter.NewLongLoginAdapter(accountRepository, func() adapter.LongLoginClient { return server.CookieRenew }, server.updateRunningCookie, server.Logger),
	)
	if longLoginErr != nil {
		panic(longLoginErr)
	}
	// accountSettings 由独立适配器承载账号设置写入与敏感登录信息更新。
	var settingsRuntime accountapp.SettingsRuntime
	if server.Manager != nil {
		settingsRuntime = server.Manager
	}
	// accountSettings、accountSettingsErr 保存账号设置应用服务及其装配错误。
	accountSettings, accountSettingsErr := accountapp.NewSettingsService(server.dependencies.NewAccountSettingsRepository(), settingsRuntime)
	if accountSettingsErr != nil {
		panic(accountSettingsErr)
	}
	// accountSummaryRepository 保存普通用户与管理员共用的摘要查询适配器。
	accountSummaryRepository := server.dependencies.NewAccountSummaryRepository()
	// accountSummaries、accountSummariesErr 保存账号摘要应用服务及其装配错误。
	accountSummaries, accountSummariesErr := accountapp.NewSummaryService(accountSummaryRepository, accountSummaryRepository)
	if accountSummariesErr != nil {
		panic(accountSummariesErr)
	}
	// credentialWake 负责将凭证恢复后的任务唤醒写入收口到适配器。
	credentialWake, credentialWakeErr := automationapp.NewCredentialWakeService(server.dependencies.NewAutomationCredentialWakeRepository())
	if credentialWakeErr != nil {
		panic(credentialWakeErr)
	}
	// deleteRuntime 是可选的账号运行时端口；显式保持 nil，避免把 nil *Manager 装入非空接口后触发 panic。
	var deleteRuntime accountapp.DeleteRuntime
	if server.Manager != nil {
		deleteRuntime = server.Manager
	}
	// accountDelete 是账号删除用例的构造结果，运行时端口可为空以兼容无 Manager 的测试 Server。
	accountDelete, deleteErr := accountapp.NewDeleteService(accountRepository, deleteRuntime)
	if deleteErr != nil {
		panic(deleteErr)
	}
	// itemBatchPublish 是批量远端发布适配器，图片安全回调由 Server 装配时注入。
	itemBatchPublish := server.dependencies.NewItemBatchPublishPort(server.mtopClient, server.Logger, server.updateRunningCookie, func(ctx context.Context, cookieID string, err error) {
		server.recoverExpiredMTOPSession(ctx, cookieID, err)
	}, readBatchImageFile, downloadImageURL)
	// itemBatchLocalPublish 将远端发布成功后的本地商品、规则和检查点一次性装配。
	itemBatchLocalPublish, itemBatchLocalPublishErr := itemapp.NewBatchLocalPublishService(
		server.dependencies.NewItemBatchRepository(),
		server.dependencies.NewItemCatalogRepository(),
		server.dependencies.NewAutomationRepository(),
	)
	if itemBatchLocalPublishErr != nil {
		panic(itemBatchLocalPublishErr)
	}
	// itemBatchRunner 是批量发布 worker 的构造结果，失败表示必需端口装配错误。
	itemBatchRunner, batchRunnerErr := newItemBatchRunnerApplication(server, itemBatchPublish, itemBatchLocalPublish)
	if batchRunnerErr != nil {
		panic(batchRunnerErr)
	}
	// itemBatchRecovery 负责恢复扫描的批次状态编排，worker 启动仍由 Server 生命周期边界提供。
	itemBatchRecovery, batchRecoveryErr := itemapp.NewBatchRecoveryService(
		server.dependencies.NewItemBatchRepository(),
		itemapp.BatchRecoveryOptions{
			LeaseDuration: publishBatchLease,
			StartWorker: func(ctx context.Context, userID int64, batchID, workerToken string) {
				server.startPublishBatchWorker(ctx, userID, batchID, workerToken)
			},
		},
	)
	if batchRecoveryErr != nil {
		panic(batchRecoveryErr)
	}
	// itemBatchPreviewPort 提供批量预检所需的非敏感归属与本地图片校验能力。
	itemBatchPreviewPort := server.dependencies.NewItemBatchPreviewPort()
	// itemBatchPreview 是批量发布预检应用服务的构造结果。
	itemBatchPreview, itemBatchPreviewErr := itemapp.NewBatchPreviewService(itemBatchPreviewPort, itemBatchPreviewPort)
	if itemBatchPreviewErr != nil {
		panic(itemBatchPreviewErr)
	}
	// itemBatchManagement 是批次管理应用服务的构造结果。
	itemBatchManagement, itemBatchManagementErr := itemapp.NewBatchManagementService(server.dependencies.NewItemBatchRepository(), serverBatchManagementRuntime{server: server})
	if itemBatchManagementErr != nil {
		panic(itemBatchManagementErr)
	}
	// accountLoginCreate 是手动 Cookie 登录应用服务的构造结果。
	accountLoginCreate, accountLoginCreateErr := newAccountLoginCreateApplication(server)
	if accountLoginCreateErr != nil {
		panic(accountLoginCreateErr)
	}
	// accountQRLogin 是扫码成功凭证持久化应用服务的构造结果；零值测试 Server 暂不装配数据库端口。
	var accountQRLogin *accountapp.QRLoginService
	if server.dependencies != nil {
		// accountQRLoginErr 保存扫码应用服务装配失败原因。
		var accountQRLoginErr error
		accountQRLogin, accountQRLoginErr = accountapp.NewQRLoginService(server.dependencies.NewQRLoginRepository(), serverQRLoginLifecycle{server: server})
		if accountQRLoginErr != nil {
			panic(accountQRLoginErr)
		}
	}
	// automationRepository 复用同一个数据库适配器，保持自动化异常与规则查询的基础设施边界一致。
	automationRepository := server.dependencies.NewAutomationRepository()
	// settingsService 统一装配设置数据库与远端模型目录适配器。
	settingsService := settingsapp.NewService(server.dependencies.NewSettingsRepository(), adapter.NewAIModelClient())
	// adminService 负责管理员用户与仪表盘查询，HTTP 层只做 DTO 映射。
	adminService := adminapp.NewService(server.dependencies.NewAdminRepository())
	// itemCatalogRepository 是商品读写用例共用的数据库适配器。
	itemCatalogRepository := server.dependencies.NewItemCatalogRepository()
	// itemCatalog 是商品列表和详情读取用例的应用服务。
	itemCatalog, itemCatalogErr := itemapp.NewCatalogService(itemCatalogRepository)
	if itemCatalogErr != nil {
		panic(itemCatalogErr)
	}
	// itemCatalogMutation 是商品写入用例的应用服务。
	itemCatalogMutation, itemCatalogMutationErr := itemapp.NewCatalogMutationService(itemCatalogRepository)
	if itemCatalogMutationErr != nil {
		panic(itemCatalogMutationErr)
	}
	// itemPublishPort 是单商品与批量发布共享的平台凭证适配器。
	itemPublishPort := server.dependencies.NewItemPublishPort(server.mtopClient, server.Logger, server.updateRunningCookie, func(ctx context.Context, cookieID string, err error) {
		server.recoverExpiredMTOPSession(ctx, cookieID, err)
	})
	// itemCategoryRecommendation 复用商品发布端口承载类目推荐和响应会话写回。
	itemCategoryRecommendation, itemCategoryRecommendationErr := itemapp.NewCategoryRecommendationService(itemPublishPort)
	if itemCategoryRecommendationErr != nil {
		panic(itemCategoryRecommendationErr)
	}
	// itemBatchPreviewPersistence 将预检结果持久化到批次仓储，隔离数据库模型转换。
	itemBatchPreviewPersistence, itemBatchPreviewPersistenceErr := itemapp.NewBatchPreviewPersistenceService(server.dependencies.NewItemBatchRepository())
	if itemBatchPreviewPersistenceErr != nil {
		panic(itemBatchPreviewPersistenceErr)
	}
	// itemSinglePublish 是单商品发布应用服务及其基础设施端口的构造结果。
	itemSinglePublish, itemSinglePublishErr := itemapp.NewService(
		itemPublishPort,
		server.dependencies.NewItemPublishRepository(),
	)
	if itemSinglePublishErr != nil {
		panic(itemSinglePublishErr)
	}
	return &applicationServices{
		orders:                      &orderHTTPAdapter{services: orderServices, repository: orderRepository},
		itemSinglePublish:           itemSinglePublish,
		itemBatchRunner:             itemBatchRunner,
		itemBatchRecovery:           itemBatchRecovery,
		itemBatchPreview:            itemBatchPreview,
		itemBatchManagement:         itemBatchManagement,
		itemCategoryRecommendation:  itemCategoryRecommendation,
		itemBatchPreviewPersistence: itemBatchPreviewPersistence,
		itemBatchLocalPublish:       itemBatchLocalPublish,
		itemSync: itemapp.NewSyncService(server.dependencies.NewItemSyncRepository(server.mtopClient, server.Logger, server.updateRunningCookie, func(ctx context.Context, cookieID string, err error) {
			server.recoverExpiredMTOPSession(ctx, cookieID, err)
		})),
		itemCatalog:            itemCatalog,
		itemCatalogMutation:    itemCatalogMutation,
		accountLogin:           &accountLoginService{server: server, repository: accountRepository, summaryRepository: accountSummaryRepository, createApplication: accountLoginCreate, qrApplication: accountQRLogin},
		authentication:         newAuthenticationApplication(server),
		loginAudit:             accountapp.NewLoginAuditService(server.dependencies.NewAccountLoginAuditRepository()),
		passwordLogin:          accountapp.NewPasswordLoginService(),
		accountDelete:          accountDelete,
		accountProfile:         accountProfile,
		accountLongLogin:       accountLongLogin,
		accountSettings:        accountSettings,
		accountSummaries:       accountSummaries,
		accountTasks:           automationapp.NewService(server.dependencies.NewAccountTaskRepository(), adapter.NewAccountTaskRunner(server.automation)),
		credentialWake:         credentialWake,
		chat:                   newChatSendingApplication(server),
		uncertainNotifications: notificationsapp.New(server.dependencies.NewNotificationUncertainRepository()),
		notificationChannels:   notificationsapp.NewChannelService(server.dependencies.NewNotificationChannelRepository(), server.notifier),
		analytics:              analyticsapp.NewService(server.dependencies.NewAnalyticsRepository()),
		automationRules:        automationapp.NewRuleService(automationRepository, automationRepository),
		cards:                  cardsapp.NewService(server.dependencies.NewCardsRepository()),
		publishAutomationRules: automationapp.NewPublishRuleService(automationRepository),
		automationIssues:       automationapp.NewIssueService(automationRepository),
		defaultReplies:         defaultreplyapp.NewService(server.dependencies.NewDefaultReplyRepository()),
		keywords:               keywordsapp.NewService(server.dependencies.NewKeywordRepository()),
		settings:               settingsService,
		admin:                  adminService,
	}
}

// newAuthenticationApplication 构造用户认证应用服务及其数据库适配器。
func newAuthenticationApplication(server *Server) *accountapp.AuthenticationService {
	// authentication、authenticationErr 保存认证应用服务及其装配错误。
	authentication, authenticationErr := accountapp.NewAuthenticationService(server.dependencies.NewAuthenticationRepository())
	if authenticationErr != nil {
		panic(authenticationErr)
	}
	return authentication
}

// itemCatalogApplication 返回当前 Server 绑定的商品读取应用服务。
func (s *Server) itemCatalogApplication() *itemapp.CatalogService {
	return s.applicationServiceSet().itemCatalog
}

// itemSinglePublishApplication 返回当前 Server 绑定的单商品发布应用服务。
func (s *Server) itemSinglePublishApplication() *itemapp.Service {
	return s.applicationServiceSet().itemSinglePublish
}

// itemCatalogMutationApplication 返回当前 Server 绑定的商品写入应用服务。
func (s *Server) itemCatalogMutationApplication() *itemapp.CatalogMutationService {
	return s.applicationServiceSet().itemCatalogMutation
}

// itemSyncApplication 返回当前 Server 绑定的商品同步应用服务。
func (s *Server) itemSyncApplication() *itemapp.SyncService {
	return s.applicationServiceSet().itemSync
}

// cardsApplication 返回当前 Server 绑定的卡券 CRUD 应用服务。
func (s *Server) cardsApplication() *cardsapp.Service {
	return s.applicationServiceSet().cards
}

// accountTaskApplication 返回当前 Server 绑定的账号任务应用服务。
func (s *Server) accountTaskApplication() *automationapp.Service {
	return s.applicationServiceSet().accountTasks
}

// credentialWakeApplication 返回当前 Server 绑定的凭证恢复唤醒应用服务。
func (s *Server) credentialWakeApplication() *automationapp.CredentialWakeService {
	return s.applicationServiceSet().credentialWake
}

// automationIssuesApplication 返回当前 Server 绑定的自动化异常应用服务。
func (s *Server) automationIssuesApplication() *automationapp.IssueService {
	return s.applicationServiceSet().automationIssues
}

// automationRulesApplication 返回当前 Server 绑定的自动化规则应用服务。
func (s *Server) automationRulesApplication() *automationapp.RuleService {
	return s.applicationServiceSet().automationRules
}

// itemBatchRunnerApplication 返回当前 Server 绑定的批量发布 worker 编排器。
func (s *Server) itemBatchRunnerApplication() *itemapp.BatchRunner {
	return s.applicationServiceSet().itemBatchRunner
}

// itemBatchRecoveryApplication 返回当前 Server 绑定的批量恢复应用服务。
func (s *Server) itemBatchRecoveryApplication() *itemapp.BatchRecoveryService {
	return s.applicationServiceSet().itemBatchRecovery
}

// itemBatchPreviewApplication 返回当前 Server 绑定的批量预检应用服务。
func (s *Server) itemBatchPreviewApplication() *itemapp.BatchPreviewService {
	return s.applicationServiceSet().itemBatchPreview
}

// itemBatchManagementApplication 返回当前 Server 绑定的批次管理应用服务。
func (s *Server) itemBatchManagementApplication() *itemapp.BatchManagementService {
	return s.applicationServiceSet().itemBatchManagement
}

// itemCategoryRecommendationApplication 返回商品类目推荐应用服务。
func (s *Server) itemCategoryRecommendationApplication() *itemapp.CategoryRecommendationService {
	return s.applicationServiceSet().itemCategoryRecommendation
}

// itemBatchPreviewPersistenceApplication 返回预检结果持久化应用服务。
func (s *Server) itemBatchPreviewPersistenceApplication() *itemapp.BatchPreviewPersistenceService {
	return s.applicationServiceSet().itemBatchPreviewPersistence
}

// accountProfileApplication 返回当前 Server 绑定的账号资料应用服务。
func (s *Server) accountProfileApplication() *accountapp.ProfileService {
	return s.applicationServiceSet().accountProfile
}

// accountLongLoginApplication 返回当前 Server 绑定的长登录应用服务。
func (s *Server) accountLongLoginApplication() *accountapp.LongLoginService {
	return s.applicationServiceSet().accountLongLogin
}

// accountSettingsApplication 返回当前 Server 绑定的账号设置应用服务。
func (s *Server) accountSettingsApplication() *accountapp.SettingsService {
	return s.applicationServiceSet().accountSettings
}

// authenticationApplication 返回当前 Server 绑定的用户认证应用服务。
func (s *Server) authenticationApplication() *accountapp.AuthenticationService {
	return s.applicationServiceSet().authentication
}

// accountSummaryApplication 返回当前 Server 绑定的账号摘要应用服务。
func (s *Server) accountSummaryApplication() *accountapp.SummaryService {
	return s.applicationServiceSet().accountSummaries
}

// accountDeleteApplication 返回当前 Server 绑定的账号删除应用服务。
func (s *Server) accountDeleteApplication() *accountapp.DeleteService {
	return s.applicationServiceSet().accountDelete
}

// loginAuditApplication 返回当前 Server 绑定的登录成功审计应用服务。
func (s *Server) loginAuditApplication() *accountapp.LoginAuditService {
	return s.applicationServiceSet().loginAudit
}

// settingsApplication 返回当前 Server 绑定的设置应用服务。
func (s *Server) settingsApplication() *settingsapp.Service {
	return s.applicationServiceSet().settings
}

// applicationServiceSet 返回当前 Server 的应用服务集合，并兼容测试 Server。
func (s *Server) applicationServiceSet() *applicationServices {
	if s == nil {
		return &applicationServices{}
	}
	if s.applications != nil {
		return s.applications
	}
	if s.dependencies == nil {
		// accountLogin 仅用于零值 Server 的输入校验测试，不触发任何持久化访问。
		return &applicationServices{accountLogin: &accountLoginService{server: s}}
	}
	return newApplicationServices(s)
}
