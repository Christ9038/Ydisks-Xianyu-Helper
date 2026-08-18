package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/account"
	"xianyu-go/internal/adapter"
	lifecycleapp "xianyu-go/internal/application/lifecycle"
	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/auth"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/chat"
	"xianyu-go/internal/composition"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
	"xianyu-go/internal/notify"
	"xianyu-go/internal/xianyu/mtop"
)

func newTestServer(t *testing.T) (*Server, *db.Store, func()) { // newTestServer 构造已完成迁移且带固定管理员数据的 HTTP 测试服务器。
	t.Helper()
	dbPath := serverTestDatabasePath(t)      // dbPath 是已完成迁移且仅供当前测试使用的 SQLite 文件路径。
	d, err := openServerTestDatabase(dbPath) // d 是当前测试连接；err 表示打开独立副本失败的原因。
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// store 是当前测试数据库的 repository 聚合入口。
	store := db.NewStore(d, db.DialectSQLite)
	// 管理员和测试账号已经在共享 SQLite 模板中预置，避免每个测试重复执行 bcrypt。
	// 一个账号的 cookie 夹具由模板初始化阶段写入当前测试副本。
	// admin 账户的查询由各测试通过登录接口按需验证。
	// 当前测试副本保持模板中的固定账号数据不变。
	// 具体测试可以在自己的请求流程中修改这些副本数据。

	// mgr 是使用空处理器的测试账号管理器。
	mgr := account.NewManager(store, noopHandler{}, nil)
	// srv 是未启用聊天服务的基础测试 HTTP 服务。
	// orderDependencies 保存订单应用服务专用的测试装配能力，确保 Server 不从通用容器回退读取订单依赖。
	orderDependencies, orderDependencyErr := adapter.NewOrderDependencies(store)
	if orderDependencyErr != nil {
		t.Fatalf("NewOrderDependencies: %v", orderDependencyErr)
	}
	// accountDependencies 保存测试 Server 的账号专用装配能力，避免回退到通用容器。
	accountDependencies, accountDependencyErr := adapter.NewAccountDependencies(store)
	if accountDependencyErr != nil {
		t.Fatalf("NewAccountDependencies: %v", accountDependencyErr)
	}
	// itemDependencies 保存测试 Server 的商品专用装配能力，避免回退到通用容器。
	itemDependencies, itemDependencyErr := adapter.NewItemDependencies(store)
	if itemDependencyErr != nil {
		t.Fatalf("NewItemDependencies: %v", itemDependencyErr)
	}
	// chatDependencies 保存聊天测试使用的显式适配器工厂。
	chatDependencies := adapter.NewChatDependencies(store)
	// systemDependencies 保存健康检查与补偿扫描测试使用的显式适配器工厂。
	systemDependencies := adapter.NewSystemDependencies(store)
	// databaseHealth 是测试进程组合根创建后注入 Server 的健康检查端口。
	databaseHealth := systemDependencies.NewDatabaseHealth()
	// automationDependencies 保存自动化测试使用的显式适配器工厂及构造错误。
	automationDependencies, automationDependencyErr := adapter.NewAutomationDependencies(store)
	if automationDependencyErr != nil {
		t.Fatalf("NewAutomationDependencies: %v", automationDependencyErr)
	}
	// miscDependencies 保存通知、分析和卡券测试使用的显式适配器工厂及构造错误。
	miscDependencies, miscDependencyErr := adapter.NewMiscDependencies(store)
	if miscDependencyErr != nil {
		t.Fatalf("NewMiscDependencies: %v", miscDependencyErr)
	}
	// adminSettingsDependencies 保存管理员和系统设置测试使用的显式适配器工厂。
	adminSettingsDependencies := adapter.NewAdminSettingsDependencies(store)
	if chatDependencies == nil || systemDependencies == nil || databaseHealth == nil || adminSettingsDependencies == nil {
		t.Fatal("显式 Server 依赖构造失败")
	}
	// transportApplications 是测试组合根构造的 transport-facing 应用服务集合。
	transportApplications, transportApplicationsErr := adapter.NewTransportApplicationServices(adapter.TransportApplicationServiceOptions{
		AutomationDependencies:    automationDependencies,
		MiscDependencies:          miscDependencies,
		AdminSettingsDependencies: adminSettingsDependencies,
		AccountTaskRunner:         adapter.NewAccountTaskRunner(nil),
		ModelClient:               adapter.NewAIModelClient(),
	})
	if transportApplicationsErr != nil {
		t.Fatalf("NewTransportApplicationServices: %v", transportApplicationsErr)
	}
	// orderReconciliationRecovery 是按生产组合根路径创建的订单补偿扫描应用服务。
	orderReconciliationRecovery := newTestOrderReconciliationRecovery(t, systemDependencies)
	// platformDependencies 保存测试服务器显式注入的平台客户端集合。
	platformDependencies, platformDependencyErr := adapter.NewDefaultPlatformDependencies(nil)
	if platformDependencyErr != nil {
		t.Fatalf("NewPlatformDependencies: %v", platformDependencyErr)
	}
	// authentication 保存测试 HTTP 会话中间件需要的认证服务。
	authentication := &auth.Service{Store: store}
	// srv、err 保存测试 HTTP 服务构造结果及失败原因。
	srv, err := newTestServerFromComposition(authentication, mgr, nil, nil, nil, orderDependencies, accountDependencies, itemDependencies, chatDependencies, automationDependencies, transportApplications, platformDependencies, databaseHealth, orderReconciliationRecovery)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// mtopClient 用于本次流程后续判断的mtopClient
	mtopClient := mtop.NewClient()
	mtopClient.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(
				`{"ret":["SUCCESS::调用成功"],"data":{"module":{"base":{"displayName":"测试账号","avatar":"https://img.alicdn.com/test-avatar.jpg"}}}}`,
			)),
			Request: req,
		}, nil
	})}
	setTestMTop(srv, mtopClient)
	return srv, store, func() {
		mgr.StopAll()
		_ = d.Close()
	}
}

// newTestServerWithChat 构造启用通信应用服务的测试服务器，供聊天 REST/WebSocket 测试复用。
func newTestServerWithChat(t *testing.T) (*Server, *db.Store, func()) {
	// srv、store 和 cleanup 分别是基础 HTTP 服务、数据库聚合和资源释放函数。
	srv, store, cleanup := newTestServer(t)
	// chatService 是仅供聊天测试使用的通信应用服务实例。
	chatService := chat.New(store)
	// applications.chat 重新绑定测试聊天服务，确保实时订阅适配器使用同一事件中心。
	srv.applications.chat = adapter.NewChatSendingApplication(chatService, store, testAccountManager(srv), func() mtop.Client { return testMTop(srv) })
	registerTestChatDomain(srv, chatService)
	return srv, store, cleanup
}

// newUninitializedTestServer 封装newUninitializedTestServer业务协调。
func newUninitializedTestServer(t *testing.T) (*Server, *db.Store, func()) {
	t.Helper()
	// dbPath 用于本次流程后续判断的db路径
	dbPath := filepath.Join(t.TempDir(), "test.db")
	// d 是当前未初始化测试使用的数据库连接；err 是打开错误。
	d, _, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// store 是未初始化测试数据库的 repository 聚合入口。
	store := db.NewStore(d, db.DialectSQLite)
	// mgr 是未初始化测试使用的空处理器账号管理器。
	mgr := account.NewManager(store, noopHandler{}, nil)
	// srv 是未初始化测试使用的 HTTP 服务实例。
	// orderDependencies 保存未初始化测试的订单专用装配能力。
	orderDependencies, orderDependencyErr := adapter.NewOrderDependencies(store)
	if orderDependencyErr != nil {
		t.Fatalf("NewOrderDependencies: %v", orderDependencyErr)
	}
	// accountDependencies 保存未初始化测试的账号专用装配能力。
	accountDependencies, accountDependencyErr := adapter.NewAccountDependencies(store)
	if accountDependencyErr != nil {
		t.Fatalf("NewAccountDependencies: %v", accountDependencyErr)
	}
	// itemDependencies 保存未初始化测试的商品专用装配能力。
	itemDependencies, itemDependencyErr := adapter.NewItemDependencies(store)
	if itemDependencyErr != nil {
		t.Fatalf("NewItemDependencies: %v", itemDependencyErr)
	}
	// chatDependencies 保存未初始化测试使用的聊天适配器工厂。
	chatDependencies := adapter.NewChatDependencies(store)
	// systemDependencies 保存未初始化测试使用的系统适配器工厂。
	systemDependencies := adapter.NewSystemDependencies(store)
	// databaseHealth 是测试进程组合根创建后注入 Server 的健康检查端口。
	databaseHealth := systemDependencies.NewDatabaseHealth()
	// automationDependencies 保存未初始化测试使用的自动化适配器工厂及构造错误。
	automationDependencies, automationDependencyErr := adapter.NewAutomationDependencies(store)
	if automationDependencyErr != nil {
		t.Fatalf("NewAutomationDependencies: %v", automationDependencyErr)
	}
	// miscDependencies 保存未初始化测试使用的杂项适配器工厂及构造错误。
	miscDependencies, miscDependencyErr := adapter.NewMiscDependencies(store)
	if miscDependencyErr != nil {
		t.Fatalf("NewMiscDependencies: %v", miscDependencyErr)
	}
	// adminSettingsDependencies 保存未初始化测试使用的管理员设置适配器工厂。
	adminSettingsDependencies := adapter.NewAdminSettingsDependencies(store)
	if chatDependencies == nil || systemDependencies == nil || databaseHealth == nil || adminSettingsDependencies == nil {
		t.Fatal("显式 Server 依赖构造失败")
	}
	// transportApplications 是未初始化测试组合根构造的 transport-facing 应用服务集合。
	transportApplications, transportApplicationsErr := adapter.NewTransportApplicationServices(adapter.TransportApplicationServiceOptions{
		AutomationDependencies:    automationDependencies,
		MiscDependencies:          miscDependencies,
		AdminSettingsDependencies: adminSettingsDependencies,
		AccountTaskRunner:         adapter.NewAccountTaskRunner(nil),
		ModelClient:               adapter.NewAIModelClient(),
	})
	if transportApplicationsErr != nil {
		t.Fatalf("NewTransportApplicationServices: %v", transportApplicationsErr)
	}
	// orderReconciliationRecovery 是按生产组合根路径创建的订单补偿扫描应用服务。
	orderReconciliationRecovery := newTestOrderReconciliationRecovery(t, systemDependencies)
	// platformDependencies 保存未初始化测试服务器显式注入的平台客户端集合。
	platformDependencies, platformDependencyErr := adapter.NewDefaultPlatformDependencies(nil)
	if platformDependencyErr != nil {
		t.Fatalf("NewPlatformDependencies: %v", platformDependencyErr)
	}
	// authentication 保存未初始化数据库上的会话中间件依赖。
	authentication := &auth.Service{Store: store}
	// srv、err 保存未初始化测试服务构造结果及失败原因。
	srv, err := newTestServerFromComposition(authentication, mgr, nil, nil, nil, orderDependencies, accountDependencies, itemDependencies, chatDependencies, automationDependencies, transportApplications, platformDependencies, databaseHealth, orderReconciliationRecovery)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv, store, func() {
		mgr.StopAll()
		_ = d.Close()
	}
}

// newTestServerFromComposition 按生产组合根顺序构造测试 HTTP Server，禁止回退到已删除的 Server 内部装配器。
func newTestServerFromComposition(authentication *auth.Service, manager *account.Manager, autoCenter *automation.Center, notifier *notify.Notifier, chatService *chat.Service, orderDependencies *adapter.OrderDependencies, accountDependencies *adapter.AccountDependencies, itemDependencies *adapter.ItemDependencies, chatDependencies *adapter.ChatDependencies, automationDependencies *adapter.AutomationDependencies, transportApplications *adapter.TransportApplicationServices, platformDependencies *adapter.PlatformDependencies, databaseHealth DatabaseHealthPort, orderReconciliationRecovery *orderapp.ReconciliationRecoveryCoordinator) (*Server, error) {
	// testPlatform 将默认平台能力包装为每个 Server 独立可变的测试 Port。
	testPlatform := newTestPlatformPort(platformDependencies)
	// lifecycleCoordinator 保存测试应用 worker 的父 Context 所有者。
	lifecycleCoordinator := lifecycleapp.NewCoordinator()
	// applications 在平台回调触发时提供已完成装配的账号运行时服务。
	var applications *composition.Services
	// updateRunningCookie 把平台 Cookie 变化转交给应用服务，测试中忽略非敏感运行时同步错误。
	updateRunningCookie := func(ctx context.Context, accountID, value string) {
		if applications != nil {
			_ = applications.UpdateRunningCookie(ctx, accountID, value)
		}
	}
	// sessionRecovery 将确认过期的会话恢复请求委托给账号运行时应用服务。
	sessionRecovery := adapter.NewSessionRecoveryHandler(nil, func(ctx context.Context, accountID string) bool {
		return applications != nil && applications.RecoverExpiredCredential(ctx, accountID)
	})
	// applicationsErr 表示测试组合根装配应用服务失败的原因。
	var applicationsErr error
	applications, applicationsErr = composition.New(composition.Dependencies{
		OrderDependencies:           orderDependencies,
		AccountDependencies:         accountDependencies,
		ItemDependencies:            itemDependencies,
		ChatDependencies:            chatDependencies,
		AutomationDependencies:      automationDependencies,
		TransportApplications:       transportApplications,
		OrderReconciliationRecovery: orderReconciliationRecovery,
		Manager:                     manager,
		Automation:                  autoCenter,
		Notifier:                    notifier,
		Chat:                        chatService,
		MTopClient:                  testPlatform.MTOPClient,
		LongLoginClient:             testPlatform.LongLoginClient,
		QRLogin:                     testPlatform,
		UpdateRunningCookie:         updateRunningCookie,
		SessionRecovery:             sessionRecovery,
		LifecycleContext:            lifecycleCoordinator.Context,
	})
	if applicationsErr != nil {
		return nil, applicationsErr
	}
	// dependencies 保存测试组合层投影出的 HTTP transport 依赖。
	dependencies := testServerDependencies(authentication, databaseHealth, applications, sessionRecovery)
	// serverErr 保存 HTTP transport 注入完整 Port 快照时的构造失败。
	server, serverErr := New(dependencies)
	if serverErr != nil {
		return nil, serverErr
	}
	registerTestAccountManager(server, manager)
	registerTestPlatform(server, testPlatform)
	registerTestBatchLifecycle(server, applications.LifecycleComponents())
	registerTestOrderServices(server, applications.TransportPorts().Orders)
	return server, nil
}

// testAccountLoginAdapter 将组合层账号登录 Port 适配为 Server 的测试 transport Port。
type testAccountLoginAdapter struct {
	// service 是测试组合根创建的账号登录能力。
	service composition.AccountLogin
}

// CreateCookie 透传测试 Cookie 创建命令。
func (adapter testAccountLoginAdapter) CreateCookie(ctx context.Context, accountID, cookies string, userID int64, loginMethod string) error {
	return adapter.service.CreateCookie(ctx, accountID, cookies, userID, loginMethod)
}

// UpdateCookie 透传测试 Cookie 更新命令。
func (adapter testAccountLoginAdapter) UpdateCookie(ctx context.Context, accountID, cookies string, userID int64, loginMethod string, expectedRevision int64) error {
	return adapter.service.UpdateCookie(ctx, accountID, cookies, userID, loginMethod, expectedRevision)
}

// PersistQRLoginSuccess 将组合层二维码结果转换为 Server 可编码结果。
func (adapter testAccountLoginAdapter) PersistQRLoginSuccess(ctx context.Context, userID int64, sessionID string, result map[string]any, targetAccountID string) (AccountLoginResult, error) {
	// persisted、persistErr 分别是测试登录服务的脱敏持久化结果和失败。
	persisted, persistErr := adapter.service.PersistQRLoginSuccess(ctx, userID, sessionID, result, targetAccountID)
	if persistErr != nil {
		return AccountLoginResult{}, persistErr
	}
	return AccountLoginResult{AccountID: persisted.AccountID, IsNew: persisted.IsNew}, nil
}

// RegisterQRSession 登记测试二维码会话归属。
func (adapter testAccountLoginAdapter) RegisterQRSession(sessionID string, userID int64, createdAt time.Time) {
	adapter.service.RegisterQRSession(sessionID, userID, createdAt)
}

// AuthorizeQRSession 验证测试二维码会话归属。
func (adapter testAccountLoginAdapter) AuthorizeQRSession(sessionID string, userID int64) error {
	return adapter.service.AuthorizeQRSession(sessionID, userID)
}

// CleanupQRSessions 清理测试二维码会话。
func (adapter testAccountLoginAdapter) CleanupQRSessions(now time.Time) []string {
	return adapter.service.CleanupQRSessions(now)
}

// testQRLoginAdapter 将 adapter 二维码服务适配为 Server 的测试 transport Port。
type testQRLoginAdapter struct {
	// service 是组合根创建的二维码服务。
	service adapter.QRLoginService
}

// GenerateQRCode 创建测试二维码会话。
func (adapter testQRLoginAdapter) GenerateQRCode(ctx context.Context) (string, string, error) {
	return adapter.service.GenerateQRCode(ctx)
}

// GetSessionStatus 读取测试二维码状态。
func (adapter testQRLoginAdapter) GetSessionStatus(sessionID string) map[string]any {
	return adapter.service.GetSessionStatus(sessionID)
}

// CompleteVerification 完成测试二维码验证。
func (adapter testQRLoginAdapter) CompleteVerification(ctx context.Context, sessionID string) (string, string, error) {
	return adapter.service.CompleteVerification(ctx, sessionID)
}

// DeleteSession 尽力清理测试二维码平台会话。
func (adapter testQRLoginAdapter) DeleteSession(sessionID string) {
	// cleaner、ok 分别是测试二维码服务提供的可选会话清理能力及存在状态。
	if cleaner, ok := adapter.service.(interface{ DeleteSession(string) }); ok {
		cleaner.DeleteSession(sessionID)
	}
}

// testSessionRecoveryAdapter 将 adapter 的会话错误分类函数适配为 Server Port。
type testSessionRecoveryAdapter struct {
	// handler 是测试组合根构造的会话恢复函数。
	handler adapter.SessionRecoveryHandler
}

// Recover 处理确认失效的测试会话。
func (adapter testSessionRecoveryAdapter) Recover(ctx context.Context, accountID string, err error) bool {
	return adapter.handler != nil && adapter.handler(ctx, accountID, err)
}

// testServerDependencies 将组合层服务快照转换为 Server 测试构造所需的依赖。
func testServerDependencies(authentication *auth.Service, databaseHealth DatabaseHealthPort, services *composition.Services, sessionRecovery adapter.SessionRecoveryHandler) Dependencies {
	// ports 是测试组合根投影的完整 transport Port 集合。
	ports := services.TransportPorts()
	return Dependencies{Auth: authentication, Addr: ":0", DatabaseHealth: databaseHealth, Applications: NewApplicationPorts(ApplicationPortsInput{
		Orders: testOrdersTransport{services: ports.Orders}, OrderRefreshJobs: ports.OrderRefreshJobs, ItemSinglePublish: ports.ItemSinglePublish,
		ItemBatchPreview: ports.ItemBatchPreview, ItemBatchManagement: ports.ItemBatchManagement, ItemCategoryRecommendation: ports.ItemCategoryRecommendation,
		ItemBatchPreviewPersistence: ports.ItemBatchPreviewPersistence, ItemBatchLocalPublish: ports.ItemBatchLocalPublish,
		ItemSync: ports.ItemSync, ItemCatalog: ports.ItemCatalog, ItemCatalogMutation: ports.ItemCatalogMutation,
		AccountLogin: testAccountLoginAdapter{service: ports.AccountLogin}, QRLogin: testQRLoginAdapter{service: ports.QRLogin},
		SessionRecovery: testSessionRecoveryAdapter{handler: sessionRecovery}, PlatformCredentials: ports.PlatformCredentials,
		Authentication: ports.Authentication, LoginAudit: ports.LoginAudit, PasswordLogin: ports.PasswordLogin, AccountDelete: ports.AccountDelete,
		AccountProfile: ports.AccountProfile, AccountLongLogin: ports.AccountLongLogin, AccountSettings: ports.AccountSettings,
		AccountRuntime: ports.AccountRuntime, AccountSummaries: ports.AccountSummaries, AccountTasks: ports.AccountTasks, Chat: ports.Chat,
		UncertainNotifications: ports.UncertainNotifications, NotificationChannels: ports.NotificationChannels, Analytics: ports.Analytics,
		AutomationIssues: ports.AutomationIssues, AutomationRules: ports.AutomationRules, Cards: ports.Cards,
		PublishAutomationRules: ports.PublishAutomationRules, DefaultReplies: ports.DefaultReplies, Keywords: ports.Keywords,
		Settings: ports.Settings, Admin: ports.Admin,
	})}
}

// newTestOrderReconciliationRecovery 以与进程组合根一致的路径构造订单补偿扫描应用服务。
func newTestOrderReconciliationRecovery(t *testing.T, dependencies *adapter.SystemDependencies) *orderapp.ReconciliationRecoveryCoordinator {
	t.Helper()
	// recovery 是订单补偿扫描应用服务；测试不启动它，因此无需额外关闭。
	recovery, recoveryErr := orderapp.NewReconciliationRecoveryCoordinator(dependencies.NewReconciliationService(nil))
	if recoveryErr != nil {
		t.Fatalf("NewReconciliationRecoveryCoordinator: %v", recoveryErr)
	}
	return recovery
}

// roundTripFunc 用于本次流程后续判断的roundTripFunc
type roundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip 封装RoundTrip业务协调。
func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// noopHandler 用于本次流程后续判断的noopHandler
type noopHandler struct{}

// HandleChatMessage 处理聊天消息。
func (noopHandler) HandleChatMessage(context.Context, engine.ChatMessage) error { return nil }

// HandleSystemEvent 处理系统Event。
func (noopHandler) HandleSystemEvent(context.Context, automation.Task) error { return nil }

// OnPasswordLoginRefresh 封装On密码登录Refresh业务协调。
func (noopHandler) OnPasswordLoginRefresh(context.Context, string) bool { return false }

// OnAccountAlert 封装On账号Alert业务协调。
func (noopHandler) OnAccountAlert(context.Context, string, string, string, string) {}

// TestLoginVerifyLogout 登录→verify→登出 全链路。
func TestLoginVerifyLogout(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()

	// 1) 登录（用户名密码）。
	body := `{"username":"admin","password":"pw"}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("login status=%d body=%s", rec.Code, rec.Body.String())
	}
	// lr 用于本次流程后续判断的lr
	var lr loginResponse
	json.Unmarshal(rec.Body.Bytes(), &lr)
	if !lr.Success || !lr.IsAdmin {
		t.Fatalf("登录响应异常: %+v", lr)
	}
	// cookie 用于本次流程后续判断的登录凭证
	cookie := rec.Result().Cookies()[0]
	if cookie.Name != "session" || cookie.Value == "" || !cookie.HttpOnly {
		t.Fatalf("Cookie 异常: %+v", cookie)
	}

	// 2) verify 带 cookie 应 authenticated。
	req2 := httptest.NewRequest(http.MethodGet, "/verify", nil)
	req2.AddCookie(cookie)
	// rec2 用于本次流程后续判断的rec2
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	// v 用于本次流程后续判断的v
	var v map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &v)
	if v["authenticated"] != true || v["initialized"] != true {
		t.Fatalf("verify 异常: %+v", v)
	}

	// 3) 无 cookie verify 应未认证。
	req3 := httptest.NewRequest(http.MethodGet, "/verify", nil)
	// rec3 用于本次流程后续判断的rec3
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	json.Unmarshal(rec3.Body.Bytes(), &v)
	if v["authenticated"] == true {
		t.Fatal("无 cookie 应未认证")
	}

	// 4) 登出。
	req4 := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req4.AddCookie(cookie)
	// rec4 用于本次流程后续判断的rec4
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, req4)
	// 登出后 cookie 应被清除（MaxAge=-1）。
	dc := rec4.Result().Cookies()
	// cleared 用于本次流程后续判断的cleared
	cleared := false
	// c 表示当前遍历过程中的c
	for _, c := range dc {
		if c.Name == "session" && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("登出应清除 cookie")
	}
}

// TestLoginWrongPassword 错误密码。
func TestLoginWrongPassword(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()

	// body 用于本次流程后续判断的请求体
	body := `{"username":"admin","password":"wrong"}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// lr 用于本次流程后续判断的lr
	var lr loginResponse
	json.Unmarshal(rec.Body.Bytes(), &lr)
	if lr.Success {
		t.Fatal("错误密码不应成功")
	}
}

// TestInitializeFromWebUI 封装TestInitializeFromWebUI业务协调。
func TestInitializeFromWebUI(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newUninitializedTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()

	// shortReq 用于本次流程后续判断的shortReq
	shortReq := httptest.NewRequest(http.MethodPost, "/initialize", strings.NewReader(`{"password":"short"}`))
	// shortRec 用于本次流程后续判断的shortRec
	shortRec := httptest.NewRecorder()
	h.ServeHTTP(shortRec, shortReq)
	if shortRec.Code != http.StatusBadRequest {
		t.Fatalf("短密码应返回 400，got %d body=%s", shortRec.Code, shortRec.Body.String())
	}

	// initReq 用于本次流程后续判断的initReq
	initReq := httptest.NewRequest(http.MethodPost, "/initialize", strings.NewReader(`{"password":"newpassword"}`))
	// initRec 用于本次流程后续判断的initRec
	initRec := httptest.NewRecorder()
	h.ServeHTTP(initRec, initReq)
	if initRec.Code != http.StatusOK {
		t.Fatalf("初始化 status=%d body=%s", initRec.Code, initRec.Body.String())
	}
	// initResult 用于本次流程后续判断的init结果
	var initResult loginResponse
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal(initRec.Body.Bytes(), &initResult); err != nil {
		t.Fatalf("解析初始化响应: %v", err)
	}
	if !initResult.Success || !initResult.IsAdmin || initResult.Username != "admin" {
		t.Fatalf("初始化响应异常: %+v", initResult)
	}

	// admin、err 用于本次流程后续判断的admin、err
	admin, err := store.Users.GetAdmin(context.Background())
	if err != nil || admin == nil {
		t.Fatalf("初始化后 admin 不存在: admin=%+v err=%v", admin, err)
	}
	if // ok、err 用于本次流程后续判断的ok、err
	_, ok, err := store.Users.VerifyAndUpgrade(context.Background(), "admin", "newpassword"); err != nil || !ok {
		t.Fatalf("初始化密码不可用: ok=%v err=%v", ok, err)
	}

	// cookies 用于本次流程后续判断的cookies
	cookies := initRec.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != "session" || cookies[0].Value == "" {
		t.Fatalf("初始化后应自动建立会话: %+v", cookies)
	}
	// verifyReq 用于本次流程后续判断的verifyReq
	verifyReq := httptest.NewRequest(http.MethodGet, "/verify", nil)
	verifyReq.AddCookie(cookies[0])
	// verifyRec 用于本次流程后续判断的verifyRec
	verifyRec := httptest.NewRecorder()
	h.ServeHTTP(verifyRec, verifyReq)
	// verifyResult 用于本次流程后续判断的verify结果
	var verifyResult map[string]any
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal(verifyRec.Body.Bytes(), &verifyResult); err != nil {
		t.Fatalf("解析 verify 响应: %v", err)
	}
	if verifyResult["authenticated"] != true || verifyResult["initialized"] != true {
		t.Fatalf("初始化后 verify 异常: %+v", verifyResult)
	}

	// secondReq 用于本次流程后续判断的secondReq
	secondReq := httptest.NewRequest(http.MethodPost, "/initialize", strings.NewReader(`{"password":"anotherpw"}`))
	// secondRec 用于本次流程后续判断的secondRec
	secondRec := httptest.NewRecorder()
	h.ServeHTTP(secondRec, secondReq)
	if secondRec.Code != http.StatusConflict {
		t.Fatalf("重复初始化应返回 409，got %d body=%s", secondRec.Code, secondRec.Body.String())
	}
}

// TestUpdateCredentialsRenamesUserAndRevokesSessions 封装TestUpdateCredentialsRenames用户AndRevokesSessions业务协调。
func TestUpdateCredentialsRenamesUserAndRevokesSessions(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()

	// loginReq 用于本次流程后续判断的登录Req
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"admin","password":"pw"}`))
	// loginRec 用于本次流程后续判断的登录Rec
	loginRec := httptest.NewRecorder()
	h.ServeHTTP(loginRec, loginReq)
	// sessionCookie 用于本次流程后续判断的会话登录凭证
	sessionCookie := loginRec.Result().Cookies()[0]

	// updateReq 用于本次流程后续判断的updateReq
	updateReq := httptest.NewRequest(http.MethodPut, "/account/credentials", strings.NewReader(
		`{"current_password":"pw","new_username":"operator","new_password":"newpassword"}`,
	))
	updateReq.AddCookie(sessionCookie)
	// updateRec 用于本次流程后续判断的updateRec
	updateRec := httptest.NewRecorder()
	h.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updateRec.Code, updateRec.Body.String())
	}
	// updateResult 用于本次流程后续判断的update结果
	var updateResult map[string]any
	json.Unmarshal(updateRec.Body.Bytes(), &updateResult)
	if updateResult["success"] != true || updateResult["requires_relogin"] != true {
		t.Fatalf("update result=%+v", updateResult)
	}

	// verifyReq 用于本次流程后续判断的verifyReq
	verifyReq := httptest.NewRequest(http.MethodGet, "/verify", nil)
	verifyReq.AddCookie(sessionCookie)
	// verifyRec 用于本次流程后续判断的verifyRec
	verifyRec := httptest.NewRecorder()
	h.ServeHTTP(verifyRec, verifyReq)
	// verifyResult 用于本次流程后续判断的verify结果
	var verifyResult map[string]any
	json.Unmarshal(verifyRec.Body.Bytes(), &verifyResult)
	if verifyResult["authenticated"] == true || verifyResult["initialized"] != true {
		t.Fatalf("旧会话应失效且系统仍已初始化: %+v", verifyResult)
	}

	// oldLoginReq 用于本次流程后续判断的old登录Req
	oldLoginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"admin","password":"pw"}`))
	// oldLoginRec 用于本次流程后续判断的old登录Rec
	oldLoginRec := httptest.NewRecorder()
	h.ServeHTTP(oldLoginRec, oldLoginReq)
	// oldLogin 用于本次流程后续判断的old登录
	var oldLogin loginResponse
	json.Unmarshal(oldLoginRec.Body.Bytes(), &oldLogin)
	if oldLogin.Success {
		t.Fatal("旧用户名不应继续登录")
	}

	// newLoginReq 用于本次流程后续判断的new登录Req
	newLoginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"operator","password":"newpassword"}`))
	// newLoginRec 用于本次流程后续判断的new登录Rec
	newLoginRec := httptest.NewRecorder()
	h.ServeHTTP(newLoginRec, newLoginReq)
	// newLogin 用于本次流程后续判断的new登录
	var newLogin loginResponse
	json.Unmarshal(newLoginRec.Body.Bytes(), &newLogin)
	if !newLogin.Success {
		t.Fatalf("新凭据登录失败: %s", newLoginRec.Body.String())
	}
}

// TestCookiesDetailsRequiresAuth 未登录访问受保护端点应 401。
func TestCookiesDetailsRequiresAuth(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/cookies/details", nil)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应 401，got %d", rec.Code)
	}
}

// TestCookiesDetailsWithAuth 登录后能取账号详情。
func TestCookiesDetailsWithAuth(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()

	// 登录拿 cookie。
	body := `{"username":"admin","password":"pw"}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// cookie 用于本次流程后续判断的登录凭证
	cookie := rec.Result().Cookies()[0]

	// 取详情。
	req2 := httptest.NewRequest(http.MethodGet, "/cookies/details", nil)
	req2.AddCookie(cookie)
	// rec2 用于本次流程后续判断的rec2
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	// arr 用于本次流程后续判断的arr
	var arr []map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &arr)
	if len(arr) != 1 || arr[0]["id"] != "acc1" {
		t.Fatalf("账号详情异常: %+v", arr)
	}
	// 不应返回 cookie 明文（安全基线）。
	if _, has := arr[0]["value"]; has {
		t.Fatal("不应返回 cookie 明文")
	}
	if arr[0]["has_cookie"] != true {
		t.Fatal("应 has_cookie=true")
	}
}

// TestCookieRuntimeStatusWithAuth 封装Test登录凭证Runtime状态WithAuth业务协调。
func TestCookieRuntimeStatusWithAuth(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()

	// loginReq 用于本次流程后续判断的登录Req
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"admin","password":"pw"}`))
	// loginRec 用于本次流程后续判断的登录Rec
	loginRec := httptest.NewRecorder()
	h.ServeHTTP(loginRec, loginReq)
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginRec.Result().Cookies()[0]

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/cookies/runtime-status", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// statuses 用于本次流程后续判断的statuses
	var statuses map[string]engine.RuntimeStatus
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal(rec.Body.Bytes(), &statuses); err != nil {
		t.Fatal(err)
	}
	if statuses["acc1"].State != engine.RuntimeError {
		t.Fatalf("未启动账号应返回 error，got %+v", statuses["acc1"])
	}
}

// TestAddAndDeleteCookie 添加/删除账号。
func TestAddAndDeleteCookie(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()

	// body 用于本次流程后续判断的请求体
	body := `{"username":"admin","password":"pw"}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// cookie 用于本次流程后续判断的登录凭证
	cookie := rec.Result().Cookies()[0]

	// 添加。
	addBody := `{"id":"acc2","value":"unb=456; _m_h5_tk=tk2_2;"}`
	// req2 用于本次流程后续判断的req2
	req2 := httptest.NewRequest(http.MethodPost, "/cookies", strings.NewReader(addBody))
	req2.AddCookie(cookie)
	// rec2 用于本次流程后续判断的rec2
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("add status=%d body=%s", rec2.Code, rec2.Body.String())
	}

	// 删除。
	req3 := httptest.NewRequest(http.MethodDelete, "/cookies/acc2", nil)
	req3.AddCookie(cookie)
	// rec3 用于本次流程后续判断的rec3
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("delete status=%d body=%s", rec3.Code, rec3.Body.String())
	}
}

// TestHealth 健康检查无需认证。
func TestHealth(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()

	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("health status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"version":"dev"`) {
		t.Fatalf("health response missing build version: %s", rec.Body.String())
	}
}

// TestHealthReportsUnavailableDatabase 封装TestHealthReportsUnavailableDatabase业务协调。
func TestHealthReportsUnavailableDatabase(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	if // err 用于本次流程后续判断的err
	err := store.DB.Close(); err != nil {
		t.Fatal(err)
	}
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("health status=%d body=%s", rec.Code, rec.Body.String())
	}
	// response 用于本次流程后续判断的响应
	var response map[string]any
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["status"] != "degraded" || response["database"] != "unavailable" {
		t.Fatalf("health response=%+v", response)
	}
}
