package adapter

import (
	"fmt"

	adminapp "xianyu-go/internal/application/admin"
	analyticsapp "xianyu-go/internal/application/analytics"
	automationapp "xianyu-go/internal/application/automation"
	cardsapp "xianyu-go/internal/application/cards"
	defaultreplyapp "xianyu-go/internal/application/defaultreply"
	keywordsapp "xianyu-go/internal/application/keywords"
	notificationsapp "xianyu-go/internal/application/notifications"
	settingsapp "xianyu-go/internal/application/settings"
)

// TransportApplicationServiceOptions 收集由进程组合根提供的 transport-facing 应用服务构造输入。
type TransportApplicationServiceOptions struct {
	// AutomationDependencies 提供自动化领域的窄仓储工厂。
	AutomationDependencies *AutomationDependencies
	// MiscDependencies 提供通知、分析和卡券领域的窄仓储工厂。
	MiscDependencies *MiscDependencies
	// AdminSettingsDependencies 提供管理员和系统设置领域的窄仓储工厂。
	AdminSettingsDependencies *AdminSettingsDependencies
	// AdminRuntime 在删除用户前停止其账号运行时；调用方可在离线场景显式传入 nil。
	AdminRuntime adminapp.Runtime
	// AccountTaskRunner 执行用户手动触发的账号自动化任务。
	AccountTaskRunner automationapp.Runner
	// ChannelSender 向指定通知渠道发送测试消息；为空时仅允许渠道 CRUD。
	ChannelSender notificationsapp.ChannelSender
	// ModelClient 读取远端 AI 模型目录，不得记录 API 密钥。
	ModelClient settingsapp.ModelClient
}

// TransportApplicationServices 是由进程组合根一次性构造并注入 Server 的应用服务集合。
// Server 在 New 阶段复制其引用到私有服务集合，运行期不再访问或修改本结构。
type TransportApplicationServices struct {
	// Settings 提供系统、用户和账号 AI 设置用例。
	Settings *settingsapp.Service
	// Admin 提供管理员用户管理和全局统计用例。
	Admin *adminapp.Service
	// AccountTasks 提供账号任务设置、查询和手动执行用例。
	AccountTasks *automationapp.Service
	// UncertainNotifications 提供通知不确定状态运维查询。
	UncertainNotifications *notificationsapp.Service
	// NotificationChannels 提供通知渠道 CRUD、绑定和测试发送。
	NotificationChannels *notificationsapp.ChannelService
	// Analytics 提供订单统计分析用例。
	Analytics *analyticsapp.Service
	// AutomationIssues 提供自动化异常查询和人工处理用例。
	AutomationIssues *automationapp.IssueService
	// AutomationRules 提供自动化规则校验、分页和持久化用例。
	AutomationRules *automationapp.RuleService
	// Cards 提供卡券库存 CRUD 用例。
	Cards *cardsapp.Service
	// PublishAutomationRules 提供发布后自动化规则准备流程。
	PublishAutomationRules *automationapp.PublishRuleService
	// DefaultReplies 提供默认回复配置与投递记录用例。
	DefaultReplies *defaultreplyapp.Service
	// Keywords 提供关键词和指定商品回复规则用例。
	Keywords *keywordsapp.Service
}

// NewTransportApplicationServices 在进程启动期构造不依赖 Server callback 的 transport-facing 应用服务集合。
func NewTransportApplicationServices(options TransportApplicationServiceOptions) (*TransportApplicationServices, error) {
	if options.AutomationDependencies == nil {
		return nil, fmt.Errorf("构造 transport 应用服务缺少自动化依赖")
	}
	if options.MiscDependencies == nil {
		return nil, fmt.Errorf("构造 transport 应用服务缺少通知分析卡券依赖")
	}
	if options.AdminSettingsDependencies == nil {
		return nil, fmt.Errorf("构造 transport 应用服务缺少管理员设置依赖")
	}
	if options.AccountTaskRunner == nil {
		return nil, fmt.Errorf("构造 transport 应用服务缺少账号任务运行端口")
	}
	if options.ModelClient == nil {
		return nil, fmt.Errorf("构造 transport 应用服务缺少 AI 模型客户端")
	}
	// automationRepository 为规则、异常和发布后规则三个用例共享同一窄持久化适配器。
	automationRepository := options.AutomationDependencies.NewAutomationRepository()
	if automationRepository == nil {
		return nil, fmt.Errorf("构造 transport 应用服务缺少自动化仓储")
	}
	// services 是进程启动期完成构造、随后作为只读依赖交给 Server 的服务集合。
	services := &TransportApplicationServices{
		Settings:               settingsapp.NewService(options.AdminSettingsDependencies.NewSettingsRepository(), options.ModelClient),
		Admin:                  adminapp.NewServiceWithRuntime(options.AdminSettingsDependencies.NewAdminRepository(), options.AdminRuntime),
		AccountTasks:           automationapp.NewService(options.AutomationDependencies.NewAccountTaskRepository(), options.AccountTaskRunner),
		UncertainNotifications: notificationsapp.New(options.MiscDependencies.NewNotificationUncertainRepository()),
		NotificationChannels:   notificationsapp.NewChannelService(options.MiscDependencies.NewNotificationChannelRepository(), options.ChannelSender),
		Analytics:              analyticsapp.NewService(options.MiscDependencies.NewAnalyticsRepository()),
		AutomationIssues:       automationapp.NewIssueService(automationRepository),
		AutomationRules:        automationapp.NewRuleService(automationRepository, automationRepository),
		Cards:                  cardsapp.NewService(options.MiscDependencies.NewCardsRepository()),
		PublishAutomationRules: automationapp.NewPublishRuleService(automationRepository),
		DefaultReplies:         defaultreplyapp.NewService(options.AutomationDependencies.NewDefaultReplyRepository()),
		Keywords:               keywordsapp.NewService(options.AutomationDependencies.NewKeywordRepository()),
	}
	// validationErr 表示服务集合字段缺失或半初始化，启动流程必须立即终止。
	if validationErr := services.Validate(); validationErr != nil {
		return nil, validationErr
	}
	return services, nil
}

// Validate 确保服务集合没有半初始化字段，避免 Server 在请求期发现遗漏依赖。
func (services *TransportApplicationServices) Validate() error {
	if services == nil {
		return fmt.Errorf("transport 应用服务集合不能为空")
	}
	if services.Settings == nil || services.Admin == nil || services.AccountTasks == nil || services.UncertainNotifications == nil || services.NotificationChannels == nil || services.Analytics == nil || services.AutomationIssues == nil || services.AutomationRules == nil || services.Cards == nil || services.PublishAutomationRules == nil || services.DefaultReplies == nil || services.Keywords == nil {
		return fmt.Errorf("transport 应用服务集合存在未装配服务")
	}
	return nil
}
