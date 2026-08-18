package server

import (
	"context"
	"fmt"

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
	lifecycleapp "xianyu-go/internal/application/lifecycle"
	notificationsapp "xianyu-go/internal/application/notifications"
	orderapp "xianyu-go/internal/application/orders"
	settingsapp "xianyu-go/internal/application/settings"
)

// applicationServices 聚合 Server 使用的应用服务实例，统一管理共享基础设施依赖。
type applicationServices struct {
	// platformCredentials 负责按需读取平台凭证窄视图并执行归属复核。
	platformCredentials *accountapp.PlatformCredentialService
	// orders 是订单 HTTP 适配器；业务服务集合由应用层统一构造。
	orders *orderHTTPAdapter
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

// emptyApplicationServices 是零值 Server 和隔离 handler 测试共享的只读空服务集合。
// 它不包含任何生产依赖，禁止写入；生产 Server 必须在 New 阶段注入完整集合。
var emptyApplicationServices = &applicationServices{accountLogin: &accountLoginService{}}

// ApplicationLifecycleComponents 返回 Server 装配的应用 worker 生命周期组件。
// 组件只暴露 Start/Close 端口，启动顺序与进程取消责任由 cmd 的协调器拥有。
func (s *Server) ApplicationLifecycleComponents() []lifecycleapp.NamedComponent {
	if s == nil || s.applications == nil {
		return nil
	}
	// components 保存按应用依赖登记的 worker 生命周期组件。
	components := make([]lifecycleapp.NamedComponent, 0, 3)
	if s.applications.orderRefreshJobs != nil {
		// service 保存订单刷新恢复与 worker 生命周期应用服务。
		service := s.applications.orderRefreshJobs
		components = append(components, lifecycleapp.NamedComponent{
			Name: "order-refresh-recovery",
			Component: lifecycleapp.FuncComponent{
				StartFunc: service.StartRecovery,
				CloseFunc: service.Close,
			},
		})
	}
	if s.applications.itemBatchCoordinator != nil {
		// coordinator 保存批量发布 worker 与恢复扫描生命周期协调器。
		coordinator := s.applications.itemBatchCoordinator
		components = append(components, lifecycleapp.NamedComponent{
			Name: "publish-batch-workers",
			Component: lifecycleapp.FuncComponent{
				StartFunc: coordinator.StartRecovery,
				CloseFunc: coordinator.Close,
			},
		})
	}
	if s.applications.orderReconciliationRecovery != nil {
		// coordinator 保存订单补偿扫描生命周期协调器。
		coordinator := s.applications.orderReconciliationRecovery
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

// newApplicationServices 为指定 Server 装配全部应用服务实例，并把任何依赖构造错误交给上层启动流程处理。
func newApplicationServices(server *Server) (*applicationServices, error) {
	// orderRepository 保存订单应用服务共享的基础设施适配器。
	orderRepository := server.orderDependencies.NewOrderRepository()
	// orderReconciliation 保存订单补偿写入应用 Port 的数据库适配器。
	orderReconciliation := server.orderDependencies.NewOrderReconciliationRepository()
	// orderRuntime 保存订单服务共享的运行时能力适配器。
	orderRuntime := newServerOrderRuntime(server, orderReconciliation)
	// orderServices 保存应用层统一构造的订单业务服务集合。
	orderServices := orderapp.NewServiceSet(orderRepository, orderRepository, orderRuntime, orderRuntime, server.orderDependencies.NewOrderRefreshJobRepository(), refreshOrderChunkSize)
	// orderRefreshRunner 是订单刷新后台 worker 与恢复扫描的生命周期拥有者。
	orderRefreshRunner, orderRefreshRunnerErr := orderapp.NewRefreshJobRunner(
		orderServices.RefreshJobs,
		orderServices.Refresh,
		orderapp.RefreshJobRunnerOptions{
			OnWorkerError: func(jobID string, err error) {
				if server.Logger != nil {
					server.Logger.Warn("订单刷新后台任务结束", "job_id", jobID, "err", err)
				}
			},
			OnRecoveryError: func(err error) {
				if server.Logger != nil {
					server.Logger.Warn("扫描订单刷新恢复任务失败", "err", err)
				}
			},
		},
	)
	if orderRefreshRunnerErr != nil {
		return nil, fmt.Errorf("构造订单刷新运行器失败: %w", orderRefreshRunnerErr)
	}
	// orderRefreshJobs 是订单刷新任务应用 facade，封装归属、租约和 worker 启动编排。
	orderRefreshJobs, orderRefreshJobsErr := orderapp.NewRefreshJobService(
		orderServices.RefreshJobs,
		orderServices.Refresh,
		orderRefreshRunner,
		orderapp.RefreshJobServiceOptions{},
	)
	if orderRefreshJobsErr != nil {
		return nil, fmt.Errorf("构造订单刷新应用服务失败: %w", orderRefreshJobsErr)
	}
	// accountProfile 保存账号资料刷新用例的构造结果。
	// accountRepository 提供账号登录、资料摘要和删除共用的数据库适配器。
	accountRepository := server.accountDependencies.NewAccountLoginRepository()
	// platformCredentials 将账号凭证读取限制在消费者定义的只读应用端口。
	platformCredentials, platformCredentialsErr := accountapp.NewPlatformCredentialService(accountRepository)
	if platformCredentialsErr != nil {
		return nil, fmt.Errorf("构造平台凭证服务失败: %w", platformCredentialsErr)
	}
	// cookieWriterFactory 将明文 Cookie 封装在 adapter 的请求范围实例中，Server 不持有凭证仓储。
	cookieWriterFactory := func(cookies string) accountapp.CookieWriter {
		return adapter.NewAccountCookieWriter(accountRepository, platformCredentials, cookies, server.Logger)
	}
	// cookieUpdaterFactory 将既有账号的明文 Cookie 封装在 adapter 的请求范围实例中。
	cookieUpdaterFactory := func(cookies string) accountapp.CookieUpdater {
		return adapter.NewAccountCookieWriter(accountRepository, platformCredentials, cookies, server.Logger)
	}
	// sessionRecovery 统一适配平台 Session 失效分类、诊断日志和账号运行时恢复。
	sessionRecovery := server.sessionRecoveryCallback()
	// accountProfile 由应用层服务编排平台资料刷新与非敏感摘要持久化。
	accountProfile, profileErr := accountapp.NewProfileService(accountRepository, adapter.NewAccountProfilePort(accountRepository, server.mtopClient, server.updateRunningCookie, sessionRecovery, server.Logger))
	if profileErr != nil {
		return nil, fmt.Errorf("构造账号资料服务失败: %w", profileErr)
	}
	// accountLongLogin 由适配器承载平台调用、Cookie 快照合并和凭证写回。
	accountLongLogin, longLoginErr := accountapp.NewLongLoginService(
		accountRepository,
		adapter.NewLongLoginAdapter(accountRepository, server.longLoginClient, server.updateRunningCookie, server.Logger),
	)
	if longLoginErr != nil {
		return nil, fmt.Errorf("构造账号长登录服务失败: %w", longLoginErr)
	}
	// accountSettings 由独立适配器承载账号设置写入与敏感登录信息更新。
	var settingsRuntime accountapp.SettingsRuntime
	if server.Manager != nil {
		settingsRuntime = server.Manager
	}
	// accountSettings、accountSettingsErr 保存账号设置应用服务及其装配错误。
	accountSettings, accountSettingsErr := accountapp.NewSettingsService(server.accountDependencies.NewAccountSettingsRepository(), settingsRuntime)
	if accountSettingsErr != nil {
		return nil, fmt.Errorf("构造账号设置服务失败: %w", accountSettingsErr)
	}
	// accountSummaryRepository 保存普通用户与管理员共用的摘要查询适配器。
	accountSummaryRepository := server.accountDependencies.NewAccountSummaryRepository()
	// accountSummaries、accountSummariesErr 保存账号摘要应用服务及其装配错误。
	accountSummaries, accountSummariesErr := accountapp.NewSummaryService(accountSummaryRepository, accountSummaryRepository)
	if accountSummariesErr != nil {
		return nil, fmt.Errorf("构造账号摘要服务失败: %w", accountSummariesErr)
	}
	// credentialWake 负责将凭证恢复后的任务唤醒写入收口到适配器。
	// automationDependencies 提供自动化、默认回复和关键词用例的显式适配器工厂。
	automationDependencies := server.automationDependencies
	if automationDependencies == nil {
		return nil, fmt.Errorf("自动化专用依赖未装配")
	}
	// credentialWake、credentialWakeErr 保存凭证恢复后的自动化唤醒应用服务及装配错误。
	credentialWake, credentialWakeErr := automationapp.NewCredentialWakeService(automationDependencies.NewAutomationCredentialWakeRepository())
	if credentialWakeErr != nil {
		return nil, fmt.Errorf("构造凭证恢复唤醒服务失败: %w", credentialWakeErr)
	}
	// accountRuntime 将 Manager 与凭证恢复后的自动化唤醒收口到账号应用端口。
	accountRuntime := accountapp.NewRuntimeService(adapter.NewAccountRuntimePort(server.Manager), credentialWake)
	// loginSuccess 负责登录成功后的资料刷新和启用账号运行时重启，避免 Server 持有业务编排。
	loginSuccess := accountapp.NewLoginSuccessService(accountRepository, accountProfile, accountRepository, accountRuntime, func(message string, err error) {
		if server.Logger != nil {
			server.Logger.Warn(message, "err", err)
		}
	})
	// loginAudit 负责登录方式、账号启用和审计日志写入，生命周期适配器只组合应用端口。
	loginAudit := accountapp.NewLoginAuditService(server.accountDependencies.NewAccountLoginAuditRepository())
	// loginLifecycle 将手动登录与扫码登录的成功后续动作收口到 adapter，不再反向持有 Server。
	loginLifecycle := adapter.NewAccountLoginLifecycle(loginAudit, loginSuccess, server.Logger)
	// deleteRuntime 是可选的账号运行时端口；显式保持 nil，避免把 nil *Manager 装入非空接口后触发 panic。
	var deleteRuntime accountapp.DeleteRuntime
	if server.Manager != nil {
		deleteRuntime = server.Manager
	}
	// accountDelete 是账号删除用例的构造结果，运行时端口可为空以兼容无 Manager 的测试 Server。
	accountDelete, deleteErr := accountapp.NewDeleteService(accountRepository, deleteRuntime)
	if deleteErr != nil {
		return nil, fmt.Errorf("构造账号删除服务失败: %w", deleteErr)
	}
	// itemBatchPublish 是批量远端发布适配器，图片安全回调由 Server 装配时注入。
	itemBatchPublish := server.itemDependencies.NewItemBatchPublishPort(server.mtopClient, server.Logger, server.updateRunningCookie, func(ctx context.Context, cookieID string, err error) {
		if sessionRecovery != nil {
			sessionRecovery(ctx, cookieID, err)
		}
	}, readBatchImageFile, downloadImageURL)
	// itemBatchLocalPublish 将远端发布成功后的本地商品、规则和检查点一次性装配。
	itemBatchLocalPublish, itemBatchLocalPublishErr := itemapp.NewBatchLocalPublishService(
		server.itemDependencies.NewItemBatchRepository(),
		server.itemDependencies.NewItemCatalogRepository(),
		automationDependencies.NewAutomationRepository(),
	)
	if itemBatchLocalPublishErr != nil {
		return nil, fmt.Errorf("构造批量发布本地收口服务失败: %w", itemBatchLocalPublishErr)
	}
	// itemBatchRunner 是批量发布 worker 的构造结果，失败表示必需端口装配错误。
	itemBatchRunner, batchRunnerErr := newItemBatchRunnerApplication(server, itemBatchPublish, itemBatchLocalPublish)
	if batchRunnerErr != nil {
		return nil, fmt.Errorf("构造批量发布运行器失败: %w", batchRunnerErr)
	}
	// itemBatchRecovery 负责恢复扫描的批次状态编排；worker 生命周期由协调器拥有。
	itemBatchRecovery, batchRecoveryErr := itemapp.NewBatchRecoveryService(
		server.itemDependencies.NewItemBatchRepository(),
		itemapp.BatchRecoveryOptions{LeaseDuration: publishBatchLease},
	)
	if batchRecoveryErr != nil {
		return nil, fmt.Errorf("构造批量发布恢复服务失败: %w", batchRecoveryErr)
	}
	// itemBatchCoordinator 负责批次 worker 的超时、恢复扫描和停止等待。
	itemBatchCoordinator, batchCoordinatorErr := itemapp.NewBatchWorkerCoordinator(itemBatchRunner, itemBatchRecovery, itemapp.BatchWorkerCoordinatorOptions{
		OnWorkerError: func(batchID string, err error) {
			if server.Logger != nil {
				server.Logger.Warn("批量发布 worker 结束", "batch", batchID, "err", err)
			}
		},
		OnRecoveryError: func(err error) {
			if server.Logger != nil {
				server.Logger.Warn("扫描可恢复批量发布任务失败", "err", err)
			}
		},
	})
	if batchCoordinatorErr != nil {
		return nil, fmt.Errorf("构造批量发布协调器失败: %w", batchCoordinatorErr)
	}
	// itemBatchPreviewPort 提供批量预检所需的非敏感归属与本地图片校验能力。
	itemBatchPreviewPort := server.itemDependencies.NewItemBatchPreviewPort()
	// itemBatchPreview 是批量发布预检应用服务的构造结果。
	itemBatchPreview, itemBatchPreviewErr := itemapp.NewBatchPreviewService(itemBatchPreviewPort, itemBatchPreviewPort)
	if itemBatchPreviewErr != nil {
		return nil, fmt.Errorf("构造批量发布预检服务失败: %w", itemBatchPreviewErr)
	}
	// itemBatchManagement 是批次管理应用服务的构造结果。
	itemBatchManagement, itemBatchManagementErr := itemapp.NewBatchManagementService(server.itemDependencies.NewItemBatchRepository(), serverBatchManagementRuntime{server: server, coordinator: itemBatchCoordinator})
	if itemBatchManagementErr != nil {
		return nil, fmt.Errorf("构造批量发布管理服务失败: %w", itemBatchManagementErr)
	}
	// accountLoginCreate 是手动 Cookie 登录应用服务的构造结果。
	accountLoginCreate, accountLoginCreateErr := newAccountLoginCreateApplication(loginLifecycle)
	if accountLoginCreateErr != nil {
		return nil, fmt.Errorf("构造账号登录创建服务失败: %w", accountLoginCreateErr)
	}
	// accountQRLogin 是扫码成功凭证持久化应用服务的构造结果；零值测试 Server 暂不装配数据库端口。
	var accountQRLogin *accountapp.QRLoginService
	if server.accountDependencies != nil {
		// accountQRLoginErr 保存扫码应用服务装配失败原因。
		var accountQRLoginErr error
		accountQRLogin, accountQRLoginErr = accountapp.NewQRLoginService(server.accountDependencies.NewQRLoginRepository(), loginLifecycle)
		if accountQRLoginErr != nil {
			return nil, fmt.Errorf("构造扫码登录服务失败: %w", accountQRLoginErr)
		}
	}
	// qrSessionRegistry 将扫码会话所有权、过期回收和幂等结果放在应用边界内。
	qrSessionRegistry := accountapp.NewQRLoginSessionRegistry()
	// itemCatalogRepository 是商品读写用例共用的数据库适配器。
	itemCatalogRepository := server.itemDependencies.NewItemCatalogRepository()
	// itemCatalog 是商品列表和详情读取用例的应用服务。
	itemCatalog, itemCatalogErr := itemapp.NewCatalogService(itemCatalogRepository)
	if itemCatalogErr != nil {
		return nil, fmt.Errorf("构造商品目录服务失败: %w", itemCatalogErr)
	}
	// itemCatalogMutation 是商品写入用例的应用服务。
	itemCatalogMutation, itemCatalogMutationErr := itemapp.NewCatalogMutationService(itemCatalogRepository)
	if itemCatalogMutationErr != nil {
		return nil, fmt.Errorf("构造商品目录写入服务失败: %w", itemCatalogMutationErr)
	}
	// itemPublishPort 是单商品与批量发布共享的平台凭证适配器。
	itemPublishPort := server.itemDependencies.NewItemPublishPort(server.mtopClient, server.Logger, server.updateRunningCookie, func(ctx context.Context, cookieID string, err error) bool {
		return sessionRecovery != nil && sessionRecovery(ctx, cookieID, err)
	})
	// itemCategoryRecommendation 复用商品发布端口承载类目推荐和响应会话写回。
	itemCategoryRecommendation, itemCategoryRecommendationErr := itemapp.NewCategoryRecommendationService(itemPublishPort)
	if itemCategoryRecommendationErr != nil {
		return nil, fmt.Errorf("构造商品类目推荐服务失败: %w", itemCategoryRecommendationErr)
	}
	// itemBatchPreviewPersistence 将预检结果持久化到批次仓储，隔离数据库模型转换。
	itemBatchPreviewPersistence, itemBatchPreviewPersistenceErr := itemapp.NewBatchPreviewPersistenceService(server.itemDependencies.NewItemBatchRepository())
	if itemBatchPreviewPersistenceErr != nil {
		return nil, fmt.Errorf("构造批量预检持久化服务失败: %w", itemBatchPreviewPersistenceErr)
	}
	// itemSinglePublish 是单商品发布应用服务及其基础设施端口的构造结果。
	itemSinglePublish, itemSinglePublishErr := itemapp.NewService(
		itemPublishPort,
		server.itemDependencies.NewItemPublishRepository(),
	)
	if itemSinglePublishErr != nil {
		return nil, fmt.Errorf("构造单商品发布服务失败: %w", itemSinglePublishErr)
	}
	// services 是完成构造后只读注入 Server 的应用服务集合。
	services := &applicationServices{
		platformCredentials:         platformCredentials,
		orders:                      &orderHTTPAdapter{services: orderServices},
		orderRefreshJobs:            orderRefreshJobs,
		orderReconciliationRecovery: server.orderReconciliationRecovery,
		itemSinglePublish:           itemSinglePublish,
		itemBatchCoordinator:        itemBatchCoordinator,
		itemBatchPreview:            itemBatchPreview,
		itemBatchManagement:         itemBatchManagement,
		itemCategoryRecommendation:  itemCategoryRecommendation,
		itemBatchPreviewPersistence: itemBatchPreviewPersistence,
		itemBatchLocalPublish:       itemBatchLocalPublish,
		itemSync: itemapp.NewSyncService(server.itemDependencies.NewItemSyncRepository(server.mtopClient, server.Logger, server.updateRunningCookie, func(ctx context.Context, cookieID string, err error) {
			if sessionRecovery != nil {
				sessionRecovery(ctx, cookieID, err)
			}
		})),
		itemCatalog:            itemCatalog,
		itemCatalogMutation:    itemCatalogMutation,
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
		accountTasks:           server.transportApplications.AccountTasks,
		credentialWake:         credentialWake,
		chat:                   newChatSendingApplication(server),
		uncertainNotifications: server.transportApplications.UncertainNotifications,
		notificationChannels:   server.transportApplications.NotificationChannels,
		analytics:              server.transportApplications.Analytics,
		automationRules:        server.transportApplications.AutomationRules,
		cards:                  server.transportApplications.Cards,
		publishAutomationRules: server.transportApplications.PublishAutomationRules,
		automationIssues:       server.transportApplications.AutomationIssues,
		defaultReplies:         server.transportApplications.DefaultReplies,
		keywords:               server.transportApplications.Keywords,
		settings:               server.transportApplications.Settings,
		admin:                  server.transportApplications.Admin,
	}
	// authentication、authenticationErr 分别是认证应用服务及其构造错误。
	authentication, authenticationErr := newAuthenticationApplication(server)
	if authenticationErr != nil {
		return nil, authenticationErr
	}
	services.authentication = authentication
	return services, nil
}

// accountRuntimeApplication 返回当前 Server 绑定的账号运行时应用服务。
func (s *Server) accountRuntimeApplication() *accountapp.RuntimeService {
	return s.applicationServiceSet().accountRuntime
}

// newAuthenticationApplication 构造用户认证应用服务及其数据库适配器。
func newAuthenticationApplication(server *Server) (*accountapp.AuthenticationService, error) {
	// authentication、authenticationErr 保存认证应用服务及其装配错误。
	authentication, authenticationErr := accountapp.NewAuthenticationService(server.accountDependencies.NewAuthenticationRepository())
	if authenticationErr != nil {
		return nil, fmt.Errorf("构造认证应用服务失败: %w", authenticationErr)
	}
	return authentication, nil
}

// orderRefreshJobsApplication 返回当前 Server 绑定的订单刷新任务应用服务。
func (s *Server) orderRefreshJobsApplication() *orderapp.RefreshJobService {
	return s.applicationServiceSet().orderRefreshJobs
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

// automationIssuesApplication 返回当前 Server 绑定的自动化异常应用服务。
func (s *Server) automationIssuesApplication() *automationapp.IssueService {
	return s.applicationServiceSet().automationIssues
}

// automationRulesApplication 返回当前 Server 绑定的自动化规则应用服务。
func (s *Server) automationRulesApplication() *automationapp.RuleService {
	return s.applicationServiceSet().automationRules
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

// platformCredentialApplication 返回当前 Server 绑定的平台凭证只读应用服务。
func (s *Server) platformCredentialApplication() *accountapp.PlatformCredentialService {
	if s == nil || s.applications == nil {
		return nil
	}
	return s.applications.platformCredentials
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

// applicationServiceSet 返回当前 Server 构造阶段注入的不可变应用服务集合。
// 零值 Server 返回共享空集合以支持防御性测试，绝不在请求期构造或缓存业务服务。
func (s *Server) applicationServiceSet() *applicationServices {
	if s == nil || s.applications == nil {
		return emptyApplicationServices
	}
	return s.applications
}
