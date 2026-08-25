package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"xianyu-go/internal/adapter"
	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/auth"
	composition "xianyu-go/internal/composition"
	"xianyu-go/internal/server"
)

// accountLoginTransport 将组合层账号登录服务适配为 HTTP transport 消费的公开 Port。
type accountLoginTransport struct {
	// service 是完成构造的账号登录应用服务，运行期不替换。
	service composition.AccountLogin
}

// CreateCookie 透传新增 Cookie 登录命令到组合层账号登录服务。
func (transport accountLoginTransport) CreateCookie(ctx context.Context, accountID, cookies string, userID int64, loginMethod string) error {
	return transport.service.CreateCookie(ctx, accountID, cookies, userID, loginMethod)
}

// UpdateCookie 透传 Cookie 更新命令到组合层账号登录服务。
func (transport accountLoginTransport) UpdateCookie(ctx context.Context, accountID, cookies string, userID int64, loginMethod string, expectedRevision int64) error {
	return transport.service.UpdateCookie(ctx, accountID, cookies, userID, loginMethod, expectedRevision)
}

// PersistQRLoginSuccess 将组合层的持久化结果转换为 Server 可公开的非敏感结果。
func (transport accountLoginTransport) PersistQRLoginSuccess(ctx context.Context, userID int64, sessionID string, result map[string]any, targetAccountID string) (server.AccountLoginResult, error) {
	// persisted、persistErr 分别是应用层已脱敏的登录结果及其持久化错误。
	persisted, persistErr := transport.service.PersistQRLoginSuccess(ctx, userID, sessionID, result, targetAccountID)
	if persistErr != nil {
		return server.AccountLoginResult{}, persistErr
	}
	return server.AccountLoginResult{AccountID: persisted.AccountID, IsNew: persisted.IsNew}, nil
}

// RegisterQRSession 注册二维码会话所有权。
func (transport accountLoginTransport) RegisterQRSession(sessionID string, userID int64, createdAt time.Time) {
	transport.service.RegisterQRSession(sessionID, userID, createdAt)
}

// AuthorizeQRSession 验证二维码会话所有权。
func (transport accountLoginTransport) AuthorizeQRSession(sessionID string, userID int64) error {
	return transport.service.AuthorizeQRSession(sessionID, userID)
}

// CleanupQRSessions 清理过期的二维码会话。
func (transport accountLoginTransport) CleanupQRSessions(now time.Time) []string {
	return transport.service.CleanupQRSessions(now)
}

// qrLoginTransport 以应用 Port 封装底层二维码会话协议和可选清理操作。
type qrLoginTransport struct {
	// service 是组合根创建的二维码平台能力，不进入 Server 状态。
	service adapter.QRLoginService
}

// GenerateQRCode 创建二维码平台会话。
func (transport qrLoginTransport) GenerateQRCode(ctx context.Context) (string, string, error) {
	return transport.service.GenerateQRCode(ctx)
}

// GetSessionStatus 读取二维码平台会话状态。
func (transport qrLoginTransport) GetSessionStatus(sessionID string) map[string]any {
	return transport.service.GetSessionStatus(sessionID)
}

// CompleteVerification 完成二维码平台的风控验证。
func (transport qrLoginTransport) CompleteVerification(ctx context.Context, sessionID string) (string, string, error) {
	return transport.service.CompleteVerification(ctx, sessionID)
}

// DeleteSession 尽力释放终态或过期的二维码平台会话。
func (transport qrLoginTransport) DeleteSession(sessionID string) {
	// cleaner、ok 分别是可选会话清理能力及其实现存在标记。
	if cleaner, ok := transport.service.(interface{ DeleteSession(string) }); ok {
		cleaner.DeleteSession(sessionID)
	}
}

// sessionRecoveryTransport 将 adapter 中的平台错误分类限制在组合层，并向 Server 暴露恢复用例。
type sessionRecoveryTransport struct {
	// handler 是组合根创建的脱敏会话恢复回调。
	handler adapter.SessionRecoveryHandler
}

// ordersTransport 将应用层订单服务集合投影为 HTTP transport 消费的扁平 Port。
// 它只转发用例调用，不承担 HTTP 错误映射或业务规则。
type ordersTransport struct {
	// services 是组合根已经验证完整性的订单应用服务集合。
	services *orderapp.ServiceSet
}

// orderRefreshJobsTransport 将订单刷新后台任务的请求边界与进程生命周期边界分离。
// requestCtx 只用于本次 HTTP 归属校验和任务持久化；lifecycleContext 始终由协调器拥有，确保 worker 不会随请求结束取消。
type orderRefreshJobsTransport struct {
	// service 是组合根已经完成构造的订单刷新任务应用服务。
	service *orderapp.RefreshJobService
	// lifecycleContext 返回协调器启动后拥有的进程生命周期 Context。
	lifecycleContext func() context.Context
}

// CreateAndStart 使用请求 Context 完成同步数据库操作，并使用进程 Context 注册后台 worker。
func (transport orderRefreshJobsTransport) CreateAndStart(requestCtx context.Context, userID int64, cookieID, status string) (orderapp.RefreshJobStartResult, error) {
	// lifecycleCtx 是当前进程拥有的后台 worker 生命周期；空函数属于构造错误而非 HTTP 请求错误。
	lifecycleCtx := context.Context(nil)
	if transport.lifecycleContext != nil {
		lifecycleCtx = transport.lifecycleContext()
	}
	return transport.service.CreateAndStart(requestCtx, lifecycleCtx, userID, cookieID, status)
}

// GetJob 读取当前用户拥有的订单刷新任务快照。
func (transport orderRefreshJobsTransport) GetJob(ctx context.Context, userID int64, jobID string) (*orderapp.RefreshJob, error) {
	return transport.service.GetJob(ctx, userID, jobID)
}

// CancelForUser 取消当前用户尚未结束的订单刷新任务。
func (transport orderRefreshJobsTransport) CancelForUser(ctx context.Context, userID int64, jobID string) (orderapp.RefreshJobCancelResult, error) {
	return transport.service.CancelForUser(ctx, userID, jobID)
}

// RefreshSingle 转发单订单刷新用例。
func (transport ordersTransport) RefreshSingle(ctx context.Context, userID int64, orderID string) (orderapp.SingleRefreshResult, error) {
	return transport.services.Refresh.RefreshSingle(ctx, userID, orderID)
}

// Refresh 转发批量订单刷新用例。
func (transport ordersTransport) Refresh(ctx context.Context, userID int64, cookieID, status string) (orderapp.RefreshResult, error) {
	return transport.services.Refresh.Refresh(ctx, userID, cookieID, status)
}

// List 转发订单列表查询用例。
func (transport ordersTransport) List(ctx context.Context, query orderapp.ListQuery) (orderapp.ListResult, error) {
	return transport.services.List.List(ctx, query)
}

// Get 转发订单详情查询用例。
func (transport ordersTransport) Get(ctx context.Context, userID int64, orderID string) (*orderapp.Order, error) {
	return transport.services.Detail.Get(ctx, userID, orderID)
}

// GetView 转发订单详情视图查询用例。
func (transport ordersTransport) GetView(ctx context.Context, userID int64, orderID string) (orderapp.DetailResult, error) {
	return transport.services.Detail.GetView(ctx, userID, orderID)
}

// Delete 转发订单逻辑删除用例。
func (transport ordersTransport) Delete(ctx context.Context, userID int64, orderID string) error {
	return transport.services.Delete.Delete(ctx, userID, orderID)
}

// Update 转发订单更新用例。
func (transport ordersTransport) Update(ctx context.Context, userID int64, orderID string, request orderapp.UpdateRequest) error {
	return transport.services.Update.Update(ctx, userID, orderID, request)
}

// Import 转发订单导入用例。
func (transport ordersTransport) Import(ctx context.Context, userID int64, inputs []orderapp.ImportOrder) (orderapp.ImportResult, error) {
	return transport.services.Import.Import(ctx, userID, inputs)
}

// ManualShip 转发订单手动发货用例。
func (transport ordersTransport) ManualShip(ctx context.Context, request orderapp.ManualShipRequest) (orderapp.ManualShipResult, error) {
	return transport.services.ManualShip.ManualShip(ctx, request)
}

// Recover 对确认会话失效的平台错误启动账号恢复。
func (transport sessionRecoveryTransport) Recover(ctx context.Context, accountID string, err error) bool {
	return transport.handler != nil && transport.handler(ctx, accountID, err)
}

// HTTPDependencies 是组合根创建 Server 所需的非业务 transport 基础依赖。
type HTTPDependencies struct {
	// Auth 是会话认证服务。
	Auth *auth.Service
	// WebDir 是静态前端资源目录。
	WebDir string
	// Addr 是已经由 cmd 配置解析的 HTTP 监听地址。
	Addr string
	// Logger 是不记录敏感凭证的结构化日志器。
	Logger *slog.Logger
	// DatabaseHealth 是健康检查使用的窄数据库探测 Port。
	DatabaseHealth server.DatabaseHealthPort
}

// ServerDependencies 将组合层服务投影为 HTTP Server 需要的不可变最小 Port 快照。
func ServerDependencies(services *composition.Services, base HTTPDependencies, sessionRecovery adapter.SessionRecoveryHandler) (server.Dependencies, error) {
	if services == nil {
		return server.Dependencies{}, fmt.Errorf("组合层账号登录服务未初始化")
	}
	// ports 是组合层将具体服务投影出的最小 HTTP transport Port 集合。
	ports := services.TransportPorts()
	if ports.AccountLogin == nil || ports.QRLogin == nil {
		return server.Dependencies{}, fmt.Errorf("组合层 transport Port 未初始化")
	}
	return server.Dependencies{
		Auth: base.Auth, WebDir: base.WebDir, Addr: base.Addr, Logger: base.Logger, DatabaseHealth: base.DatabaseHealth,
		Applications: server.NewApplicationPorts(server.ApplicationPortsInput{
			Orders: ordersTransport{services: ports.Orders}, OrderRefreshJobs: orderRefreshJobsTransport{service: ports.OrderRefreshJobs, lifecycleContext: services.LifecycleContext},
			ItemSinglePublish: ports.ItemSinglePublish, ItemBatchPreview: ports.ItemBatchPreview,
			ItemBatchManagement: ports.ItemBatchManagement, ItemCategoryRecommendation: ports.ItemCategoryRecommendation,
			ItemBatchPreviewPersistence: ports.ItemBatchPreviewPersistence, ItemBatchLocalPublish: ports.ItemBatchLocalPublish,
			ItemSync: ports.ItemSync, ItemCatalog: ports.ItemCatalog, ItemCatalogMutation: ports.ItemCatalogMutation,
			AccountLogin:        accountLoginTransport{service: ports.AccountLogin},
			QRLogin:             qrLoginTransport{service: ports.QRLogin},
			SessionRecovery:     sessionRecoveryTransport{handler: sessionRecovery},
			PlatformCredentials: ports.PlatformCredentials, Authentication: ports.Authentication, LoginAudit: ports.LoginAudit,
			PasswordLogin: ports.PasswordLogin, AccountDelete: ports.AccountDelete, AccountProfile: ports.AccountProfile,
			AccountLongLogin: ports.AccountLongLogin, AccountSettings: ports.AccountSettings, AccountRuntime: ports.AccountRuntime,
			AccountSummaries: ports.AccountSummaries, AccountTasks: ports.AccountTasks, Chat: ports.Chat,
			UncertainNotifications: ports.UncertainNotifications, NotificationChannels: ports.NotificationChannels,
			Analytics: ports.Analytics, AutomationIssues: ports.AutomationIssues, AutomationRules: ports.AutomationRules, DeliveryTemplates: ports.DeliveryTemplates,
			Cards: ports.Cards, APIRequestTester: ports.APICardTester, PublishAutomationRules: ports.PublishAutomationRules, DefaultReplies: ports.DefaultReplies,
			Keywords: ports.Keywords, Settings: ports.Settings, Admin: ports.Admin,
		}),
	}, nil
}
