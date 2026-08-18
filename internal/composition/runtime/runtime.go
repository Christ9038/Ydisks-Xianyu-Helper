package runtime

import (
	"context"
	"fmt"
	"log/slog"

	"xianyu-go/internal/adapter"
	"xianyu-go/internal/application/lifecycle"
	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/auth"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/browser"
	composition "xianyu-go/internal/composition"
	"xianyu-go/internal/db"
	"xianyu-go/internal/renewal"
	"xianyu-go/internal/server"
)

// RuntimeOptions 是 cmd 已解析后交给组合根的运行时配置。
type RuntimeOptions struct {
	// NoBrowser 表示操作者显式禁用浏览器自动化。
	NoBrowser bool
	// SecureCookie 控制 HTTP 会话 Cookie 的 Secure 属性。
	SecureCookie bool
	// WebDir 是前端静态资源路径。
	WebDir string
	// Addr 是 HTTP 监听地址。
	Addr string
}

// RuntimeInfrastructure 是 cmd 打开后交给组合根的基础设施资源。
type RuntimeInfrastructure struct {
	// Store 是已迁移并完成敏感字段升级的数据库仓储入口。
	Store *db.Store
	// Logger 是进程级脱敏结构化日志器。
	Logger *slog.Logger
}

// Runtime 是由组合根创建并由 cmd 启动、等待和关闭的完整进程运行时。
type Runtime struct {
	// HTTPServer 是只持有 transport Port 的 HTTP 服务。
	HTTPServer *server.Server
	// Lifecycle 是 cmd 独占启动和关闭的后台组件协调器。
	Lifecycle *lifecycle.Coordinator
}

// BuildRuntime 构造全部基础设施适配器、应用服务和生命周期组件，但不启动任何 worker。
func BuildRuntime(options RuntimeOptions, infrastructure RuntimeInfrastructure) (Runtime, error) {
	if infrastructure.Store == nil || infrastructure.Logger == nil {
		return Runtime{}, fmt.Errorf("组合根基础设施不完整")
	}
	// browserManager 是可选 Chromium 生命周期拥有者；禁用浏览器时保持 nil。
	var browserManager *browser.Manager
	if !options.NoBrowser {
		browserManager = browser.NewManager(infrastructure.Logger)
	}
	// runtimeBundle、bundleErr 分别是账号运行时依赖集合及其构造失败原因。
	runtimeBundle, bundleErr := adapter.NewRuntimeBundle(infrastructure.Store, browserManager, infrastructure.Logger)
	if bundleErr != nil {
		return Runtime{}, fmt.Errorf("构造账号运行时依赖失败: %w", bundleErr)
	}
	// lifecycleCoordinator 由 cmd 最终拥有，用于按顺序启动并逆序关闭后台组件。
	lifecycleCoordinator := lifecycle.NewCoordinator()
	if browserManager != nil {
		// addErr 是浏览器组件登记失败原因，失败时运行时不得继续暴露。
		if addErr := lifecycleCoordinator.Add(lifecycle.NamedComponent{Name: "browser", Component: lifecycle.FuncComponent{StartFunc: browserManager.InitializeContext, CloseFunc: browserManager.CloseContext}}); addErr != nil {
			return Runtime{}, fmt.Errorf("登记浏览器生命周期组件失败: %w", addErr)
		}
	}
	// automationScheduler、renewalScheduler 分别负责自动化延迟任务和凭证续期扫描。
	automationScheduler := automation.NewScheduler(runtimeBundle.Automation)
	// renewalScheduler 负责账号凭证的定期续期，其运行与停止由生命周期协调器统一拥有。
	renewalScheduler := renewal.NewScheduler(infrastructure.Store, runtimeBundle.Manager, runtimeBundle.Adapter, infrastructure.Logger, runtimeBundle.Notifier)
	// component 是按依赖顺序登记的基础后台组件，协调器负责后续取消和等待。
	for _, component := range []lifecycle.NamedComponent{
		{Name: "notifier", Component: lifecycle.FuncComponent{StartFunc: func(ctx context.Context) error { runtimeBundle.Notifier.Start(ctx); return nil }, CloseFunc: runtimeBundle.Notifier.WaitContext}},
		{Name: "account-manager", Component: lifecycle.FuncComponent{StartFunc: runtimeBundle.Manager.StartAll, CloseFunc: runtimeBundle.Manager.StopAllContext}},
		{Name: "automation-scheduler", Component: lifecycle.FuncComponent{StartFunc: func(ctx context.Context) error { go automationScheduler.Run(ctx); return nil }, CloseFunc: automationScheduler.WaitContext}},
		{Name: "renewal-scheduler", Component: lifecycle.FuncComponent{StartFunc: func(ctx context.Context) error { go renewalScheduler.Run(ctx); return nil }, CloseFunc: renewalScheduler.StopContext}},
	} {
		// addErr 是当前后台组件登记失败原因。
		if addErr := lifecycleCoordinator.Add(component); addErr != nil {
			return Runtime{}, fmt.Errorf("登记生命周期组件 %q 失败: %w", component.Name, addErr)
		}
	}
	// orderDependencies、orderErr 分别是订单应用服务的仓储适配器及其构造错误。
	orderDependencies, orderErr := adapter.NewOrderDependencies(infrastructure.Store)
	if orderErr != nil {
		return Runtime{}, fmt.Errorf("构造订单基础设施依赖失败: %w", orderErr)
	}
	// accountDependencies、accountErr 分别是账号应用服务的仓储适配器及其构造错误。
	accountDependencies, accountErr := adapter.NewAccountDependencies(infrastructure.Store)
	if accountErr != nil {
		return Runtime{}, fmt.Errorf("构造账号基础设施依赖失败: %w", accountErr)
	}
	// itemDependencies、itemErr 分别是商品应用服务的仓储适配器及其构造错误。
	itemDependencies, itemErr := adapter.NewItemDependencies(infrastructure.Store)
	if itemErr != nil {
		return Runtime{}, fmt.Errorf("构造商品基础设施依赖失败: %w", itemErr)
	}
	// automationDependencies、automationErr 分别是自动化应用服务的仓储适配器及其构造错误。
	automationDependencies, automationErr := adapter.NewAutomationDependencies(infrastructure.Store)
	if automationErr != nil {
		return Runtime{}, fmt.Errorf("构造自动化基础设施依赖失败: %w", automationErr)
	}
	// miscDependencies、miscErr 分别是通知、分析和卡券服务的仓储适配器及其构造错误。
	miscDependencies, miscErr := adapter.NewMiscDependencies(infrastructure.Store)
	if miscErr != nil {
		return Runtime{}, fmt.Errorf("构造通知分析卡券基础设施依赖失败: %w", miscErr)
	}
	// adminSettingsDependencies、chatDependencies、systemDependencies 分别提供管理设置、聊天和系统探测适配器。
	adminSettingsDependencies := adapter.NewAdminSettingsDependencies(infrastructure.Store)
	// chatDependencies 提供聊天用例需要的持久化和平台适配能力。
	chatDependencies := adapter.NewChatDependencies(infrastructure.Store)
	// systemDependencies 提供健康检查和订单补偿等系统级窄适配能力。
	systemDependencies := adapter.NewSystemDependencies(infrastructure.Store)
	if adminSettingsDependencies == nil || chatDependencies == nil || systemDependencies == nil {
		return Runtime{}, fmt.Errorf("构造 transport 基础设施依赖失败")
	}
	// orderReconciliationRecovery、reconciliationErr 分别是订单补偿协调器及其构造错误。
	orderReconciliationRecovery, reconciliationErr := orderapp.NewReconciliationRecoveryCoordinator(systemDependencies.NewReconciliationService(infrastructure.Logger))
	if reconciliationErr != nil {
		return Runtime{}, fmt.Errorf("构造订单补偿恢复协调器失败: %w", reconciliationErr)
	}
	// databaseHealth 是 HTTP 健康检查使用的窄数据库探测 Port。
	databaseHealth := systemDependencies.NewDatabaseHealth()
	if databaseHealth == nil {
		return Runtime{}, fmt.Errorf("构造数据库健康检查端口失败")
	}
	// platformDependencies、platformErr 分别是平台协议适配器集合及其构造错误，仅组合层可持有。
	platformDependencies, platformErr := adapter.NewDefaultPlatformDependencies(infrastructure.Logger)
	if platformErr != nil {
		return Runtime{}, fmt.Errorf("构造平台基础设施依赖失败: %w", platformErr)
	}
	// transportApplications、transportErr 分别是通知等共享应用服务集合及其构造错误。
	transportApplications, transportErr := adapter.NewTransportApplicationServices(adapter.TransportApplicationServiceOptions{
		AutomationDependencies: automationDependencies, MiscDependencies: miscDependencies, AdminSettingsDependencies: adminSettingsDependencies,
		AdminRuntime: runtimeBundle.Manager, AccountTaskRunner: adapter.NewAccountTaskRunner(runtimeBundle.Automation), ChannelSender: runtimeBundle.Notifier, ModelClient: adapter.NewAIModelClient(),
	})
	if transportErr != nil {
		return Runtime{}, fmt.Errorf("构造 transport 应用服务失败: %w", transportErr)
	}
	// services 是完成构造后才供回调使用的应用服务集合，构造期保持 nil 防止半初始化调用。
	var services *composition.Services
	// updateRunningCookie 将平台返回的新 Cookie 同步到运行时；值不记录到日志。
	updateRunningCookie := func(ctx context.Context, accountID, value string) {
		if services == nil {
			return
		}
		// updateErr 是 Cookie 写回账号运行时失败原因，不包含 Cookie 明文。
		if updateErr := services.UpdateRunningCookie(ctx, accountID, value); updateErr != nil {
			infrastructure.Logger.Warn("Cookie 更新后同步账号运行时失败", "cookie_id", accountID, "err", updateErr)
		}
	}
	// sessionRecovery 仅把确认的会话失效交给已构造的账号应用服务恢复。
	sessionRecovery := adapter.NewSessionRecoveryHandler(infrastructure.Logger, func(ctx context.Context, accountID string) bool {
		return services != nil && services.RecoverExpiredCredential(ctx, accountID)
	})
	// services、buildErr 分别是完成装配的应用服务集合及其构造错误。
	services, buildErr := composition.New(composition.Dependencies{
		OrderDependencies: orderDependencies, AccountDependencies: accountDependencies, ItemDependencies: itemDependencies,
		ChatDependencies: chatDependencies, AutomationDependencies: automationDependencies, TransportApplications: transportApplications,
		OrderReconciliationRecovery: orderReconciliationRecovery, Manager: runtimeBundle.Manager, Automation: runtimeBundle.Automation,
		Notifier: runtimeBundle.Notifier, Chat: runtimeBundle.Chat, Logger: infrastructure.Logger,
		MTopClient: platformDependencies.MTOPClient, LongLoginClient: platformDependencies.LongLoginClient, QRLogin: platformDependencies.QRLoginService(),
		UpdateRunningCookie: updateRunningCookie, SessionRecovery: sessionRecovery, LifecycleContext: lifecycleCoordinator.Context,
	})
	if buildErr != nil {
		return Runtime{}, fmt.Errorf("构造应用服务集合失败: %w", buildErr)
	}
	// serverDependencies、dependenciesErr 分别是投影给 HTTP transport 的依赖快照及其构造错误。
	serverDependencies, dependenciesErr := ServerDependencies(services, HTTPDependencies{
		Auth: &auth.Service{Store: infrastructure.Store, Logger: infrastructure.Logger, Secure: options.SecureCookie}, WebDir: options.WebDir, Addr: options.Addr,
		Logger: infrastructure.Logger, DatabaseHealth: databaseHealth,
	}, sessionRecovery)
	if dependenciesErr != nil {
		return Runtime{}, dependenciesErr
	}
	// httpServer、serverErr 分别是纯 HTTP transport 实例及其构造错误。
	httpServer, serverErr := server.New(serverDependencies)
	if serverErr != nil {
		return Runtime{}, fmt.Errorf("构造 HTTP 服务失败: %w", serverErr)
	}
	// component 是应用服务返回的 worker 生命周期组件，由协调器而非 Server 登记。
	for _, component := range services.LifecycleComponents() {
		// addErr 是应用 worker 组件登记失败原因。
		if addErr := lifecycleCoordinator.Add(component); addErr != nil {
			return Runtime{}, fmt.Errorf("登记应用 worker 生命周期组件 %q 失败: %w", component.Name, addErr)
		}
	}
	return Runtime{HTTPServer: httpServer, Lifecycle: lifecycleCoordinator}, nil
}
