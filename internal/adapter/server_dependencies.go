package adapter

import (
	"context"
	"errors"
	"log/slog"

	"xianyu-go/internal/account"
	accountapp "xianyu-go/internal/application/account"
	adminapp "xianyu-go/internal/application/admin"
	chatapp "xianyu-go/internal/application/chat"
	notificationsapp "xianyu-go/internal/application/notifications"
	orderapp "xianyu-go/internal/application/orders"
	domainchat "xianyu-go/internal/chat"
	"xianyu-go/internal/db"
	"xianyu-go/internal/reconciliation"
	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/qrlogin"
	xrenew "xianyu-go/internal/xianyu/renew"
)

// MTOPClient 是 Server 装配平台适配器时使用的客户端别名；HTTP 层不直接导入 MTOP 实现包。
type MTOPClient = mtop.Client

// QRLoginService 定义 HTTP 扫码端点所需的最小二维码会话协议。
type QRLoginService interface {
	// GenerateQRCode 创建一次待轮询的二维码会话并返回会话标识和二维码地址。
	GenerateQRCode(context.Context) (string, string, error)
	// GetSessionStatus 返回会话当前状态；返回值仅在 HTTP 适配器边界做兼容字段转换。
	GetSessionStatus(string) map[string]any
	// CompleteVerification 完成风控验证并返回本次请求作用域内的凭证值。
	CompleteVerification(context.Context, string) (string, string, error)
}

// Dependencies 封装 Server 装配所需的基础设施；底层 Store 仅在 adapter 内部可见。
type Dependencies struct {
	// store 保存所有数据库适配器共享的持久化入口，绝不向 HTTP 层公开。
	store *db.Store
}

// NewDependencies 构造 Server 使用的基础设施工厂并校验数据库 Store。
func NewDependencies(store *db.Store) (*Dependencies, error) {
	if store == nil {
		return nil, errors.New("server 基础设施 Store 不能为空")
	}
	return &Dependencies{store: store}, nil
}

// NewMTOPClient 创建默认平台客户端，供生产构造与测试替换使用。
func NewMTOPClient() MTOPClient {
	return mtop.NewClient()
}

// NewQRLoginService 创建默认二维码会话服务。
func NewQRLoginService(logger *slog.Logger) QRLoginService {
	return qrlogin.NewManager(logger)
}

// NewLongLoginClient 创建默认长登录平台客户端。
func NewLongLoginClient() LongLoginClient {
	return xrenew.Service{}
}

// NewOrderRepository 返回订单读写应用服务共用的数据库适配器。
func (d *Dependencies) NewOrderRepository() *OrderRepository { return NewOrderRepository(d.store) }

// NewOrderReconciliationRepository 返回订单补偿记录的持久化适配器。
func (d *Dependencies) NewOrderReconciliationRepository() *OrderReconciliationRepository {
	return NewOrderReconciliationRepository(d.store)
}

// NewOrderRuntime 返回订单运行时适配器，Server 只提供运行时回调而不访问持久化入口。
func (d *Dependencies) NewOrderRuntime(hooks OrderRuntimeHooks, reconciliation orderapp.ReconciliationRecorder, logger *slog.Logger) *OrderRuntime {
	return NewOrderRuntime(d.store, hooks, reconciliation, logger)
}

// NewOrderRefreshJobRepository 返回订单刷新任务的持久化适配器。
func (d *Dependencies) NewOrderRefreshJobRepository() orderapp.RefreshJobRepository {
	return NewOrderRefreshJobRepository(d.store)
}

// NewAccountLoginRepository 返回账号凭证和摘要适配器。
func (d *Dependencies) NewAccountLoginRepository() *AccountLoginRepository {
	return NewAccountLoginRepository(d.store)
}

// NewAccountSettingsRepository 返回账号设置写入适配器。
func (d *Dependencies) NewAccountSettingsRepository() *AccountSettingsRepository {
	return NewAccountSettingsRepository(d.store)
}

// NewAccountSummaryRepository 返回账号非敏感摘要适配器。
func (d *Dependencies) NewAccountSummaryRepository() *AccountSummaryRepository {
	return NewAccountSummaryRepository(d.store)
}

// NewAutomationCredentialWakeRepository 返回凭证恢复后的自动化任务唤醒适配器。
func (d *Dependencies) NewAutomationCredentialWakeRepository() *AutomationCredentialWakeRepository {
	return NewAutomationCredentialWakeRepository(d.store)
}

// NewItemBatchRepository 返回商品批量发布状态适配器。
func (d *Dependencies) NewItemBatchRepository() *ItemBatchRepository {
	return NewItemBatchRepository(d.store)
}

// NewItemBatchPreviewPort 返回批量预检所需的归属与本地文件适配器。
func (d *Dependencies) NewItemBatchPreviewPort() *ItemBatchPreviewPort {
	return NewItemBatchPreviewPort(d.store)
}

// NewItemBatchPublishPort 返回批量逐行远端发布适配器。
func (d *Dependencies) NewItemBatchPublishPort(client func() MTOPClient, logger *slog.Logger, update func(context.Context, string, string), recover func(context.Context, string, error), readFile ReadPublishImageFile, download DownloadPublishImageURL) *ItemBatchPublishPort {
	return NewItemBatchPublishPort(d.store, client, logger, update, recover, readFile, download)
}

// NewItemPublishPort 返回单商品发布和类目推荐共用的平台适配器。
func (d *Dependencies) NewItemPublishPort(client func() MTOPClient, logger *slog.Logger, update func(context.Context, string, string), recover func(context.Context, string, error)) *ItemPublishPort {
	return NewItemPublishPort(d.store, client, logger, update, recover)
}

// NewItemPublishRepository 返回单商品发布后的本地持久化适配器。
func (d *Dependencies) NewItemPublishRepository() *ItemPublishRepository {
	return NewItemPublishRepository(d.store)
}

// NewItemCatalogRepository 返回商品目录读写适配器。
func (d *Dependencies) NewItemCatalogRepository() *ItemCatalogRepository {
	return NewItemCatalogRepository(d.store)
}

// NewItemSyncRepository 返回商品同步适配器。
func (d *Dependencies) NewItemSyncRepository(client func() MTOPClient, logger *slog.Logger, update func(context.Context, string, string), recover func(context.Context, string, error)) *ItemSyncRepository {
	return NewItemSyncRepository(d.store, client, logger, update, recover)
}

// NewQRLoginRepository 返回扫码登录持久化适配器。
func (d *Dependencies) NewQRLoginRepository() accountapp.QRLoginRepository {
	return NewQRLoginRepository(d.store)
}

// NewAutomationRepository 返回自动化规则、异常和发布规则共用适配器。
func (d *Dependencies) NewAutomationRepository() *AutomationRepository {
	return NewAutomationRepository(d.store)
}

// NewSettingsRepository 返回系统设置适配器。
func (d *Dependencies) NewSettingsRepository() *SettingsRepository {
	return NewSettingsRepository(d.store)
}

// NewAuthenticationRepository 返回用户认证持久化适配器。
func (d *Dependencies) NewAuthenticationRepository() *AuthenticationRepository {
	return NewAuthenticationRepository(d.store)
}

// NewAccountLoginAuditRepository 返回登录审计持久化适配器。
func (d *Dependencies) NewAccountLoginAuditRepository() *AccountLoginAuditRepository {
	return NewAccountLoginAuditRepository(d.store)
}

// NewAccountTaskRepository 返回账号自动化任务适配器。
func (d *Dependencies) NewAccountTaskRepository() *AccountTaskRepository {
	return NewAccountTaskRepository(d.store)
}

// NewAdminRepository 返回管理员用户与统计适配器。
func (d *Dependencies) NewAdminRepository() adminapp.Repository {
	return NewAdminRepository(d.store)
}

// NewChatSendingApplication 装配聊天应用服务，聊天持久化 Store 不泄露给 HTTP 层。
func (d *Dependencies) NewChatSendingApplication(service *domainchat.Service, manager *account.Manager, client func() MTOPClient) *chatapp.Service {
	return NewChatSendingApplication(service, d.store, manager, client)
}

// NewNotificationUncertainRepository 返回通知不确定态查询适配器。
func (d *Dependencies) NewNotificationUncertainRepository() notificationsapp.Repository {
	return NewNotificationUncertainRepository(d.store)
}

// NewNotificationChannelRepository 返回通知渠道和账号绑定适配器。
func (d *Dependencies) NewNotificationChannelRepository() notificationsapp.ChannelRepository {
	return NewNotificationChannelRepository(d.store)
}

// NewAnalyticsRepository 返回订单分析只读适配器。
func (d *Dependencies) NewAnalyticsRepository() *AnalyticsRepository {
	return NewAnalyticsRepository(d.store)
}

// NewCardsRepository 返回卡券库存适配器。
func (d *Dependencies) NewCardsRepository() *CardsRepository { return NewCardsRepository(d.store) }

// NewDefaultReplyRepository 返回默认回复适配器。
func (d *Dependencies) NewDefaultReplyRepository() *DefaultReplyRepository {
	return NewDefaultReplyRepository(d.store)
}

// NewKeywordRepository 返回关键词和指定商品回复适配器。
func (d *Dependencies) NewKeywordRepository() *KeywordRepository {
	return NewKeywordRepository(d.store)
}

// NewTransactionRepository 返回应用事务执行适配器。
func (d *Dependencies) NewTransactionRepository() *TransactionRepository {
	return NewTransactionRepository(d.store)
}

// NewDatabaseHealth 返回数据库健康检查适配器。
func (d *Dependencies) NewDatabaseHealth() *DatabaseHealth { return NewDatabaseHealth(d.store) }

// NewReconciliationService 返回订单补偿扫描服务；它仅由 Server 生命周期调用。
func (d *Dependencies) NewReconciliationService(logger *slog.Logger) *reconciliation.Service {
	return reconciliation.New(d.store, logger)
}
