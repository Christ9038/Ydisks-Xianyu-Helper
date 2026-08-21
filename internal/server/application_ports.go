package server

import (
	"context"
	"fmt"
	"time"

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

// AccountLoginResult 是账号登录持久化后可供 HTTP 响应使用的非敏感结果。
type AccountLoginResult struct {
	// AccountID 是已创建或更新的账号标识。
	AccountID string
	// IsNew 表示本次是否创建了新账号。
	IsNew bool
}

// AccountLoginPort 定义 Cookie 与二维码登录 transport 消费的最小用例能力。
type AccountLoginPort interface {
	CreateCookie(context.Context, string, string, int64, string) error
	UpdateCookie(context.Context, string, string, int64, string, int64) error
	PersistQRLoginSuccess(context.Context, int64, string, map[string]any, string) (AccountLoginResult, error)
	RegisterQRSession(string, int64, time.Time)
	AuthorizeQRSession(string, int64) error
	CleanupQRSessions(time.Time) []string
}

// QRLoginPort 定义二维码平台流程的用例 Port；HTTP 层不接触具体平台实现。
type QRLoginPort interface {
	GenerateQRCode(context.Context) (string, string, error)
	GetSessionStatus(string) map[string]any
	CompleteVerification(context.Context, string) (string, string, error)
	DeleteSession(string)
}

// SessionRecoveryPort 定义已确认平台会话失效后的恢复用例。
type SessionRecoveryPort interface {
	Recover(context.Context, string, error) bool
}

// OrdersPort 定义订单 HTTP transport 实际消费的用例集合，避免持有应用层服务容器。
type OrdersPort interface {
	RefreshSingle(context.Context, int64, string) (orderapp.SingleRefreshResult, error)
	Refresh(context.Context, int64, string, string) (orderapp.RefreshResult, error)
	List(context.Context, orderapp.ListQuery) (orderapp.ListResult, error)
	Get(context.Context, int64, string) (*orderapp.Order, error)
	GetView(context.Context, int64, string) (orderapp.DetailResult, error)
	Delete(context.Context, int64, string) error
	Update(context.Context, int64, string, orderapp.UpdateRequest) error
	Import(context.Context, int64, []orderapp.ImportOrder) (orderapp.ImportResult, error)
	ManualShip(context.Context, orderapp.ManualShipRequest) (orderapp.ManualShipResult, error)
}

// OrderRefreshJobsPort 定义订单刷新任务 HTTP 接口所需的最小能力。
type OrderRefreshJobsPort interface {
	CreateAndStart(context.Context, int64, string, string) (orderapp.RefreshJobStartResult, error)
	GetJob(context.Context, int64, string) (*orderapp.RefreshJob, error)
	CancelForUser(context.Context, int64, string) (orderapp.RefreshJobCancelResult, error)
}

// ItemSinglePublishPort 定义单商品发布用例。
type ItemSinglePublishPort interface {
	PublishSingle(context.Context, itemapp.PublishInput) (itemapp.PublishOutcome, error)
}

// ItemBatchPreviewPort 定义批量发布预检所需能力。
type ItemBatchPreviewPort interface {
	CookieOwned(context.Context, int64, string) (bool, error)
	Preview(context.Context, itemapp.BatchPreviewInput) ([]itemapp.BatchPreviewRow, error)
}

// ItemBatchManagementPort 定义批量发布管理和清理能力。
type ItemBatchManagementPort interface {
	StartBatch(context.Context, int64, string, time.Duration) (string, error)
	ListBatches(context.Context, int64, int) ([]itemapp.BatchInfo, error)
	GetBatch(context.Context, int64, string) (itemapp.BatchDetails, error)
	CancelBatch(context.Context, int64, string) (string, error)
	DeleteBatch(context.Context, int64, string) error
	CleanupExpiredUploads(context.Context, time.Time, int) error
	RetryFailedBatch(context.Context, int64, string, time.Duration) (string, error)
}

// ItemCategoryRecommendationPort 定义批量发布类目推荐能力。
type ItemCategoryRecommendationPort interface {
	Recommend(context.Context, int64, string, string) (itemapp.BatchPreviewCategory, error)
}

// ItemBatchPreviewPersistencePort 定义批量预检结果保存能力。
type ItemBatchPreviewPersistencePort interface {
	Persist(context.Context, itemapp.BatchPreviewPersistenceBatch, []itemapp.BatchPreviewRow) (itemapp.BatchPreviewPersistenceResult, error)
}

// ItemBatchLocalPublishPort 定义批量远端发布后的本地持久化和规则收口能力。
type ItemBatchLocalPublishPort interface {
	Complete(context.Context, int64, itemapp.BatchRow, string, *itemapp.BatchPublishResult) error
	EnsureAutomationRules(context.Context, int64, itemapp.BatchRow, *itemapp.BatchPublishResult) error
}

// ItemSyncPort 定义商品同步能力。
type ItemSyncPort interface {
	SyncAll(context.Context, itemapp.SyncQuery) (itemapp.SyncAllResult, error)
	SyncPage(context.Context, itemapp.SyncQuery) (itemapp.SyncPageResult, error)
}

// ItemCatalogPort 定义商品目录读取能力。
type ItemCatalogPort interface {
	ListForUser(context.Context, int64, string) ([]itemapp.CatalogItem, error)
	ListByCookie(context.Context, string) ([]itemapp.CatalogItem, error)
	Get(context.Context, string, string) (itemapp.CatalogItem, error)
}

// ItemCatalogMutationPort 定义商品目录写入能力。
type ItemCatalogMutationPort interface {
	Create(context.Context, string, itemapp.CatalogWriteInput) error
	Update(context.Context, string, string, itemapp.CatalogPatchInput) error
	Delete(context.Context, string, string) error
	SetMultiSpec(context.Context, string, string, bool) error
	SetMultiQuantity(context.Context, string, string, bool) error
}

// PlatformCredentialPort 定义平台凭证受控读取能力。
type PlatformCredentialPort interface {
	LoadPlatformDetail(context.Context, string) (*accountapp.CredentialDetail, error)
	ValidateOwned(context.Context, int64, string) (int64, error)
	LoadOwnedValue(context.Context, int64, string) (string, error)
}

// AuthenticationPort 定义 HTTP 登录和管理员初始化能力。
type AuthenticationPort interface {
	IsSystemInitialized(context.Context) (bool, error)
	InitializeAdmin(context.Context, string, string) (bool, error)
	UsernameByEmail(context.Context, string) (string, error)
	Login(context.Context, string, string) (string, *accountapp.AuthUser, error)
	VerifyPassword(context.Context, string, string) (accountapp.AuthUser, bool, error)
	UpdatePassword(context.Context, string, string) (bool, error)
	UpdateCredentials(context.Context, int64, string, string) error
}

// LoginAuditPort 定义账号登录成功后的审计能力。
type LoginAuditPort interface {
	RecordSuccessfulLogin(context.Context, accountapp.SuccessfulLoginInput) error
}

// PasswordLoginPort 定义历史密码登录端点的关闭策略能力。
type PasswordLoginPort interface {
	Start(context.Context, accountapp.PasswordLoginStartInput) error
	Check(context.Context, accountapp.PasswordLoginSessionInput) error
	Cancel(context.Context, accountapp.PasswordLoginSessionInput) error
}

// AccountDeletePort 定义账号删除能力。
type AccountDeletePort interface {
	Delete(context.Context, int64, string) error
}

// AccountProfilePort 定义账号资料刷新能力。
type AccountProfilePort interface {
	RefreshProfile(context.Context, int64, string) (accountapp.ProfileResult, error)
}

// AccountLongLoginPort 定义账号长登录查询和修改能力。
type AccountLongLoginPort interface {
	Query(context.Context, int64, string) (accountapp.LongLoginResult, error)
	Set(context.Context, int64, string, bool) (accountapp.LongLoginResult, error)
}

// AccountSettingsPort 定义账号设置和状态更新能力。
type AccountSettingsPort interface {
	UpdateSettings(context.Context, accountapp.SettingsUpdateInput) (accountapp.SettingsResult, error)
	UpdateLoginInfo(context.Context, accountapp.LoginInfoUpdateInput) error
	SetStatus(context.Context, int64, string, bool) (accountapp.StatusResult, error)
	SetAutoConfirm(context.Context, int64, string, bool) (accountapp.SettingsResult, error)
	SetRemark(context.Context, int64, string, string) (accountapp.SettingsResult, error)
	SetPause(context.Context, int64, string, int) (accountapp.SettingsResult, error)
	GetPause(context.Context, int64, string) (accountapp.PauseState, error)
}

// AccountRuntimePort 定义账号运行时状态与 Cookie 同步能力。
type AccountRuntimePort interface {
	UpdateCookie(context.Context, string, string) error
	RuntimeStatuses(context.Context) (map[string]accountapp.RuntimeStatus, error)
	Restart(context.Context, string) error
	RecoverExpiredCredential(context.Context, string) bool
}

// AccountSummaryPort 定义账号摘要、归属和管理员视图能力。
type AccountSummaryPort interface {
	ListOwnedIDs(context.Context, int64) ([]string, error)
	ListSummaries(context.Context, int64) ([]accountapp.AccountSummary, error)
	GetOwnedSummary(context.Context, int64, string) (accountapp.AccountSummary, error)
	ExistsOwned(context.Context, int64, string) (bool, error)
	StatusOwned(context.Context, int64, string) (bool, error)
	RequireOwnership(context.Context, int64, string) error
	ListAdminSummaries(context.Context) ([]accountapp.AdminAccountSummary, error)
}

// AccountTasksPort 定义账号自动化任务配置和执行能力。
type AccountTasksPort interface {
	GetSettings(context.Context, string) (automationapp.AccountTaskSettings, error)
	UpdateSettings(context.Context, automationapp.AccountTaskSettings) (automationapp.AccountTaskSettings, error)
	ListRuns(context.Context, string, int) ([]automationapp.AccountTaskRun, error)
	Run(context.Context, string, string) (automationapp.TaskSummary, error)
}

// ChatPort 定义聊天 HTTP 端点消费的会话、消息和平台投递能力。
type ChatPort interface {
	SendingAvailable() bool
	ImageUploadAvailable() bool
	Subscribe(context.Context, int64) (<-chan chatapp.Event, func(), error)
	RefreshConversations(context.Context, string, int64, int) (chatapp.ConversationPage, error)
	RefreshHistory(context.Context, string, string, int64, int, chatapp.Session) (chatapp.HistoryPage, error)
	SendText(context.Context, chatapp.OutgoingInput) (*chatapp.Message, error)
	SendImage(context.Context, chatapp.ImageInput) (*chatapp.Message, error)
	ListStoredMessages(context.Context, int64, string, string, int64, int) (chatapp.Page, error)
	ListSessions(context.Context, int64, string, int) ([]chatapp.Session, error)
	FindSession(context.Context, int64, string, string) (chatapp.Session, error)
	ResolveReadMessageID(context.Context, string, string, string) string
	CleanupEmptySessions(context.Context, string) error
	OwnsAccount(context.Context, int64, string) (bool, error)
	MarkRead(context.Context, int64, string, string) error
	ReportPlatformRead(context.Context, string, string, []map[string]any) error
	ResolveSessionIdentity(context.Context, chatapp.Session) (chatapp.Session, error)
	RefreshSessionIdentities(context.Context, string, []chatapp.Session) ([]chatapp.Session, error)
	ListQuickReplies(context.Context, int64, string) ([]chatapp.QuickReply, error)
	CreateQuickReply(context.Context, int64, string, string) (chatapp.QuickReply, error)
	DeleteQuickReply(context.Context, int64, string, int64) error
	GetBuyerNote(context.Context, int64, string, string) (chatapp.BuyerNote, error)
	SaveBuyerNote(context.Context, int64, string, string, string) (chatapp.BuyerNote, error)
}

// UncertainNotificationsPort 定义通知不确定状态的查询能力。
type UncertainNotificationsPort interface {
	ListForUser(context.Context, int64, int) ([]notificationsapp.UncertainSummary, int, error)
	ListForAdmin(context.Context, int) ([]notificationsapp.UncertainSummary, int, error)
}

// NotificationChannelsPort 定义通知渠道和账号绑定的管理能力。
type NotificationChannelsPort interface {
	ListChannels(context.Context, int64) ([]notificationsapp.ChannelSummary, error)
	CreateChannel(context.Context, int64, notificationsapp.ChannelInput) (int64, error)
	UpdateChannel(context.Context, int64, int64, notificationsapp.ChannelPatch) error
	DeleteChannel(context.Context, int64, int64) error
	TestChannel(context.Context, int64, int64, time.Time) error
	ListBindings(context.Context, int64) ([]notificationsapp.BindingSummary, error)
	GetBindingIDs(context.Context, int64, string) ([]int64, error)
	SetBindings(context.Context, int64, string, []int64) error
	SetSingleBinding(context.Context, int64, string, int64, bool) error
	DeleteBinding(context.Context, int64, int64) error
	DeleteAccountBindings(context.Context, int64, string) error
}

// AnalyticsPort 定义仪表盘和订单分析能力。
type AnalyticsPort interface {
	DashboardStats(context.Context, int64) (analyticsapp.DashboardStats, error)
	OrderAnalytics(context.Context, analyticsapp.Query) (analyticsapp.OrderAnalytics, error)
	ValidOrders(context.Context, analyticsapp.Query, int, int) (analyticsapp.ValidOrders, error)
}

// AutomationIssuesPort 定义自动化异常的查询和人工处理能力。
type AutomationIssuesPort interface {
	ListIssues(context.Context, int64) ([]automationapp.RunIssue, []automationapp.DeferredIssue, error)
	ResolveRunIssue(context.Context, int64, int64, string) error
	ResolveDeferredIssue(context.Context, int64, int64, string) error
}

// AutomationRulesPort 定义自动化规则的查询、规范化和写入能力。
type AutomationRulesPort interface {
	ListForUser(context.Context, int64) ([]automationapp.Rule, error)
	ListPageForUser(context.Context, automationapp.RuleFilter) ([]automationapp.Rule, int, error)
	CountByTriggerForUser(context.Context, automationapp.RuleFilter) (map[string]int, error)
	Normalize(context.Context, int64, automationapp.RuleDraft) (automationapp.RuleInput, error)
	Create(context.Context, automationapp.RuleInput) (int64, error)
	Update(context.Context, int64, int64, automationapp.RuleInput) error
	Delete(context.Context, int64, int64) error
}

// PublishAutomationRulesPort 定义发布后自动化规则的幂等准备能力。
type PublishAutomationRulesPort interface {
	Ensure(context.Context, automationapp.RuleInput) error
}

// CardsPort 定义卡券库存 CRUD 与归属校验能力。
type CardsPort interface {
	List(context.Context, int64) ([]cardsapp.Card, error)
	Get(context.Context, int64, int64) (cardsapp.Card, error)
	ExistsOwned(context.Context, int64, int64) (bool, error)
	Create(context.Context, int64, cardsapp.Draft) (int64, error)
	Update(context.Context, int64, int64, cardsapp.Draft) error
	Delete(context.Context, int64, int64) error
	AppendData(context.Context, int64, int64, string) (int, error)
}

// APIRequestTesterPort 定义卡券 API 测试请求所需的最小应用能力。
type APIRequestTesterPort interface {
	Test(context.Context, cardsapp.APIRequestTestInput) (cardsapp.APIRequestTestResult, error)
}

// DefaultRepliesPort 定义默认回复配置能力。
type DefaultRepliesPort interface {
	Get(context.Context, int64, string) (defaultreplyapp.Reply, error)
	Upsert(context.Context, int64, string, defaultreplyapp.Reply) error
	List(context.Context, int64) ([]defaultreplyapp.Summary, error)
	Delete(context.Context, int64, string) error
	ClearRecords(context.Context, int64, string) error
}

// KeywordsPort 定义关键词与指定商品回复能力。
type KeywordsPort interface {
	List(context.Context, int64, string) ([]keywordsapp.Keyword, error)
	Add(context.Context, int64, string, keywordsapp.Draft) (int64, error)
	Replace(context.Context, int64, string, []keywordsapp.Draft) error
	Update(context.Context, int64, string, int64, keywordsapp.Draft) error
	DeleteByID(context.Context, int64, string, int64) error
	DeleteByIndex(context.Context, int64, string, int) error
	ListItemReplies(context.Context, int64) ([]keywordsapp.ItemReply, error)
	GetItemReply(context.Context, int64, string, string) (keywordsapp.ItemReply, error)
	SetItemReply(context.Context, int64, string, string, string) error
	DeleteItemReply(context.Context, int64, string, string) error
}

// SettingsPort 定义系统、用户和账号 AI 设置能力。
type SettingsPort interface {
	IsSensitiveSettingKey(string) bool
	PublicSystem(context.Context) (map[string]string, error)
	GetSystem(context.Context, int64) (map[string]string, error)
	ApplySystemChanges(context.Context, int64, map[string]string, map[string]settingsapp.SecretChange) error
	SetSystem(context.Context, int64, string, string, string) error
	ListUser(context.Context, int64) (map[string]string, error)
	GetUser(context.Context, int64, string) (string, error)
	SetUser(context.Context, int64, string, string) error
	ListAIReply(context.Context, int64) ([]settingsapp.AIReplySettings, error)
	GetAIReply(context.Context, int64, string) (settingsapp.AIReplySettings, error)
	UpsertAIReply(context.Context, int64, string, settingsapp.AIReplySettings) error
	ListAIModels(context.Context, int64, string, string) ([]string, error)
}

// AdminPort 定义管理员用户与统计能力。
type AdminPort interface {
	ListUsers(context.Context) ([]adminapp.UserSummary, error)
	DeleteUser(context.Context, int64, int64) error
	Stats(context.Context) (adminapp.Stats, error)
}

// ApplicationPorts 是构造期注入 HTTP transport 的不可变应用 Port 集合。
// 它不包含 adapter、数据库、平台 client、账号 Manager 或 worker 生命周期拥有权。
type ApplicationPorts struct {
	// orders 是订单应用服务集合，HTTP 仅做 DTO 转换和错误映射。
	orders OrdersPort
	// orderRefreshJobs 是订单刷新任务用例。
	orderRefreshJobs OrderRefreshJobsPort
	// itemSinglePublish 是单商品发布用例。
	itemSinglePublish ItemSinglePublishPort
	// itemBatchPreview 是批量发布预检用例。
	itemBatchPreview ItemBatchPreviewPort
	// itemBatchManagement 是批量发布查询、取消和重试用例。
	itemBatchManagement ItemBatchManagementPort
	// itemCategoryRecommendation 是发布类目推荐用例。
	itemCategoryRecommendation ItemCategoryRecommendationPort
	// itemBatchPreviewPersistence 是批量预检结果保存用例。
	itemBatchPreviewPersistence ItemBatchPreviewPersistencePort
	// itemBatchLocalPublish 是远端发布成功后的本地收口用例。
	itemBatchLocalPublish ItemBatchLocalPublishPort
	// itemSync 是商品同步用例。
	itemSync ItemSyncPort
	// itemCatalog 是商品目录读取用例。
	itemCatalog ItemCatalogPort
	// itemCatalogMutation 是商品目录写入用例。
	itemCatalogMutation ItemCatalogMutationPort
	// accountLogin 是 Cookie 与二维码登录用例 Port。
	accountLogin AccountLoginPort
	// qrLogin 是二维码平台流程用例 Port。
	qrLogin QRLoginPort
	// sessionRecovery 是平台会话恢复用例 Port。
	sessionRecovery SessionRecoveryPort
	// platformCredentials 是受控的平台凭证读取用例。
	platformCredentials PlatformCredentialPort
	// authentication 是用户登录认证用例。
	authentication AuthenticationPort
	// loginAudit 是登录审计用例。
	loginAudit LoginAuditPort
	// passwordLogin 是密码登录策略用例。
	passwordLogin PasswordLoginPort
	// accountDelete 是账号删除用例。
	accountDelete AccountDeletePort
	// accountProfile 是账号资料刷新用例。
	accountProfile AccountProfilePort
	// accountLongLogin 是账号长登录设置用例。
	accountLongLogin AccountLongLoginPort
	// accountSettings 是账号设置用例。
	accountSettings AccountSettingsPort
	// accountRuntime 是账号运行时状态用例。
	accountRuntime AccountRuntimePort
	// accountSummaries 是账号摘要用例。
	accountSummaries AccountSummaryPort
	// accountTasks 是账号自动化任务用例。
	accountTasks AccountTasksPort
	// chat 是聊天应用用例。
	chat ChatPort
	// uncertainNotifications 是通知不确定状态用例。
	uncertainNotifications UncertainNotificationsPort
	// notificationChannels 是通知渠道用例。
	notificationChannels NotificationChannelsPort
	// analytics 是订单分析用例。
	analytics AnalyticsPort
	// automationIssues 是自动化异常用例。
	automationIssues AutomationIssuesPort
	// automationRules 是自动化规则用例。
	automationRules AutomationRulesPort
	// cards 是卡券库存用例。
	cards CardsPort
	// apiRequestTester 执行临时 API 测试请求并返回非敏感诊断。
	apiRequestTester APIRequestTesterPort
	// publishAutomationRules 是发布后自动化规则用例。
	publishAutomationRules PublishAutomationRulesPort
	// defaultReplies 是默认回复用例。
	defaultReplies DefaultRepliesPort
	// keywords 是关键词回复用例。
	keywords KeywordsPort
	// settings 是系统设置用例。
	settings SettingsPort
	// admin 是管理员用户与统计用例。
	admin AdminPort
}

// ApplicationPortsInput 是组合根向 HTTP transport 交付的完整应用 Port 快照。
// 所有字段必须在进程启动期构造完成，Server 不会在请求期回填或替换它们。
type ApplicationPortsInput struct {
	Orders                      OrdersPort
	OrderRefreshJobs            OrderRefreshJobsPort
	ItemSinglePublish           ItemSinglePublishPort
	ItemBatchPreview            ItemBatchPreviewPort
	ItemBatchManagement         ItemBatchManagementPort
	ItemCategoryRecommendation  ItemCategoryRecommendationPort
	ItemBatchPreviewPersistence ItemBatchPreviewPersistencePort
	ItemBatchLocalPublish       ItemBatchLocalPublishPort
	ItemSync                    ItemSyncPort
	ItemCatalog                 ItemCatalogPort
	ItemCatalogMutation         ItemCatalogMutationPort
	AccountLogin                AccountLoginPort
	QRLogin                     QRLoginPort
	SessionRecovery             SessionRecoveryPort
	PlatformCredentials         PlatformCredentialPort
	Authentication              AuthenticationPort
	LoginAudit                  LoginAuditPort
	PasswordLogin               PasswordLoginPort
	AccountDelete               AccountDeletePort
	AccountProfile              AccountProfilePort
	AccountLongLogin            AccountLongLoginPort
	AccountSettings             AccountSettingsPort
	AccountRuntime              AccountRuntimePort
	AccountSummaries            AccountSummaryPort
	AccountTasks                AccountTasksPort
	Chat                        ChatPort
	UncertainNotifications      UncertainNotificationsPort
	NotificationChannels        NotificationChannelsPort
	Analytics                   AnalyticsPort
	AutomationIssues            AutomationIssuesPort
	AutomationRules             AutomationRulesPort
	Cards                       CardsPort
	APIRequestTester            APIRequestTesterPort
	PublishAutomationRules      PublishAutomationRulesPort
	DefaultReplies              DefaultRepliesPort
	Keywords                    KeywordsPort
	Settings                    SettingsPort
	Admin                       AdminPort
}

// NewApplicationPorts 将组合根已经验证的用例依赖冻结为 Server 私有快照。
func NewApplicationPorts(input ApplicationPortsInput) *ApplicationPorts {
	return &ApplicationPorts{
		orders: input.Orders, orderRefreshJobs: input.OrderRefreshJobs,
		itemSinglePublish: input.ItemSinglePublish, itemBatchPreview: input.ItemBatchPreview,
		itemBatchManagement: input.ItemBatchManagement, itemCategoryRecommendation: input.ItemCategoryRecommendation,
		itemBatchPreviewPersistence: input.ItemBatchPreviewPersistence, itemBatchLocalPublish: input.ItemBatchLocalPublish,
		itemSync: input.ItemSync, itemCatalog: input.ItemCatalog, itemCatalogMutation: input.ItemCatalogMutation,
		accountLogin: input.AccountLogin, qrLogin: input.QRLogin, sessionRecovery: input.SessionRecovery,
		platformCredentials: input.PlatformCredentials, authentication: input.Authentication, loginAudit: input.LoginAudit,
		passwordLogin: input.PasswordLogin, accountDelete: input.AccountDelete, accountProfile: input.AccountProfile,
		accountLongLogin: input.AccountLongLogin, accountSettings: input.AccountSettings, accountRuntime: input.AccountRuntime,
		accountSummaries: input.AccountSummaries, accountTasks: input.AccountTasks, chat: input.Chat,
		uncertainNotifications: input.UncertainNotifications, notificationChannels: input.NotificationChannels,
		analytics: input.Analytics, automationIssues: input.AutomationIssues, automationRules: input.AutomationRules,
		cards: input.Cards, apiRequestTester: input.APIRequestTester, publishAutomationRules: input.PublishAutomationRules, defaultReplies: input.DefaultReplies,
		keywords: input.Keywords, settings: input.Settings, admin: input.Admin,
	}
}

// validate 确认组合根已经为 HTTP 路由绑定全部必需应用 Port。
// 此处只校验启动期容器完整性，不在请求期创建、替换或查找任何依赖。
func (ports *ApplicationPorts) validate() error {
	if ports == nil {
		return fmt.Errorf("应用 Port 容器不能为空")
	}
	// required 保存每个 HTTP 路由组启动前必须可用的应用 Port。
	required := []struct {
		// name 是缺失依赖时返回的稳定诊断名称，不包含实现类型或敏感数据。
		name string
		// port 是构造期由 composition 注入的消费者接口。
		port any
	}{
		{"orders", ports.orders}, {"order_refresh_jobs", ports.orderRefreshJobs}, {"item_single_publish", ports.itemSinglePublish},
		{"item_batch_preview", ports.itemBatchPreview}, {"item_batch_management", ports.itemBatchManagement}, {"item_category_recommendation", ports.itemCategoryRecommendation},
		{"item_batch_preview_persistence", ports.itemBatchPreviewPersistence}, {"item_batch_local_publish", ports.itemBatchLocalPublish}, {"item_sync", ports.itemSync},
		{"item_catalog", ports.itemCatalog}, {"item_catalog_mutation", ports.itemCatalogMutation}, {"account_login", ports.accountLogin},
		{"qr_login", ports.qrLogin}, {"session_recovery", ports.sessionRecovery}, {"platform_credentials", ports.platformCredentials},
		{"authentication", ports.authentication}, {"login_audit", ports.loginAudit}, {"password_login", ports.passwordLogin},
		{"account_delete", ports.accountDelete}, {"account_profile", ports.accountProfile}, {"account_long_login", ports.accountLongLogin},
		{"account_settings", ports.accountSettings}, {"account_runtime", ports.accountRuntime}, {"account_summaries", ports.accountSummaries},
		{"account_tasks", ports.accountTasks}, {"chat", ports.chat}, {"uncertain_notifications", ports.uncertainNotifications},
		{"notification_channels", ports.notificationChannels}, {"analytics", ports.analytics}, {"automation_issues", ports.automationIssues},
		{"automation_rules", ports.automationRules}, {"cards", ports.cards}, {"publish_automation_rules", ports.publishAutomationRules},
		{"default_replies", ports.defaultReplies}, {"keywords", ports.keywords}, {"settings", ports.settings}, {"admin", ports.admin},
	}
	// requiredPort 是当前必须在 Server 构造前绑定的应用 Port 名称。
	for _, requiredPort := range required {
		if requiredPort.port == nil {
			return fmt.Errorf("应用 Port %s 未初始化", requiredPort.name)
		}
	}
	return nil
}

// emptyApplicationPorts 为零值 Server 的防御性读取提供不可变空快照。
var emptyApplicationPorts = &ApplicationPorts{}

// accountLoginApplication 返回 Cookie 与二维码登录用例 Port。
func (server *Server) accountLoginApplication() AccountLoginPort {
	return server.applicationServiceSet().accountLogin
}

// qrLoginApplication 返回二维码平台流程用例 Port。
func (server *Server) qrLoginApplication() QRLoginPort {
	return server.applicationServiceSet().qrLogin
}

// accountRuntimeApplication 返回账号运行时状态用例。
func (server *Server) accountRuntimeApplication() AccountRuntimePort {
	return server.applicationServiceSet().accountRuntime
}

// orderRefreshJobsApplication 返回订单刷新任务用例。
func (server *Server) orderRefreshJobsApplication() OrderRefreshJobsPort {
	return server.applicationServiceSet().orderRefreshJobs
}

// itemCatalogApplication 返回商品目录读取用例。
func (server *Server) itemCatalogApplication() ItemCatalogPort {
	return server.applicationServiceSet().itemCatalog
}

// itemSinglePublishApplication 返回单商品发布用例。
func (server *Server) itemSinglePublishApplication() ItemSinglePublishPort {
	return server.applicationServiceSet().itemSinglePublish
}

// itemCatalogMutationApplication 返回商品目录写入用例。
func (server *Server) itemCatalogMutationApplication() ItemCatalogMutationPort {
	return server.applicationServiceSet().itemCatalogMutation
}

// itemSyncApplication 返回商品同步用例。
func (server *Server) itemSyncApplication() ItemSyncPort {
	return server.applicationServiceSet().itemSync
}

// cardsApplication 返回卡券库存用例。
func (server *Server) cardsApplication() CardsPort {
	return server.applicationServiceSet().cards
}

// accountTaskApplication 返回账号自动化任务用例。
func (server *Server) accountTaskApplication() AccountTasksPort {
	return server.applicationServiceSet().accountTasks
}

// automationIssuesApplication 返回自动化异常用例。
func (server *Server) automationIssuesApplication() AutomationIssuesPort {
	return server.applicationServiceSet().automationIssues
}

// automationRulesApplication 返回自动化规则用例。
func (server *Server) automationRulesApplication() AutomationRulesPort {
	return server.applicationServiceSet().automationRules
}

// itemBatchPreviewApplication 返回批量发布预检用例。
func (server *Server) itemBatchPreviewApplication() ItemBatchPreviewPort {
	return server.applicationServiceSet().itemBatchPreview
}

// itemBatchManagementApplication 返回批量发布管理用例。
func (server *Server) itemBatchManagementApplication() ItemBatchManagementPort {
	return server.applicationServiceSet().itemBatchManagement
}

// itemCategoryRecommendationApplication 返回类目推荐用例。
func (server *Server) itemCategoryRecommendationApplication() ItemCategoryRecommendationPort {
	return server.applicationServiceSet().itemCategoryRecommendation
}

// itemBatchPreviewPersistenceApplication 返回批量预检保存用例。
func (server *Server) itemBatchPreviewPersistenceApplication() ItemBatchPreviewPersistencePort {
	return server.applicationServiceSet().itemBatchPreviewPersistence
}

// accountProfileApplication 返回账号资料刷新用例。
func (server *Server) accountProfileApplication() AccountProfilePort {
	return server.applicationServiceSet().accountProfile
}

// accountLongLoginApplication 返回账号长登录用例。
func (server *Server) accountLongLoginApplication() AccountLongLoginPort {
	return server.applicationServiceSet().accountLongLogin
}

// accountSettingsApplication 返回账号设置用例。
func (server *Server) accountSettingsApplication() AccountSettingsPort {
	return server.applicationServiceSet().accountSettings
}

// platformCredentialApplication 返回受控的平台凭证读取用例。
func (server *Server) platformCredentialApplication() PlatformCredentialPort {
	return server.applicationServiceSet().platformCredentials
}

// authenticationApplication 返回用户登录认证用例。
func (server *Server) authenticationApplication() AuthenticationPort {
	return server.applicationServiceSet().authentication
}

// accountSummaryApplication 返回账号摘要用例。
func (server *Server) accountSummaryApplication() AccountSummaryPort {
	return server.applicationServiceSet().accountSummaries
}

// accountDeleteApplication 返回账号删除用例。
func (server *Server) accountDeleteApplication() AccountDeletePort {
	return server.applicationServiceSet().accountDelete
}

// loginAuditApplication 返回登录审计用例。
func (server *Server) loginAuditApplication() LoginAuditPort {
	return server.applicationServiceSet().loginAudit
}

// settingsApplication 返回系统设置用例。
func (server *Server) settingsApplication() SettingsPort {
	return server.applicationServiceSet().settings
}

// applicationServiceSet 返回构造期注入的不可变 Port 快照；零值 Server 不会隐式装配业务服务。
func (server *Server) applicationServiceSet() *ApplicationPorts {
	if server == nil || server.applications == nil {
		return emptyApplicationPorts
	}
	return server.applications
}
