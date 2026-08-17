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

	"xianyu-go/internal/account"
	"xianyu-go/internal/adapter"
	"xianyu-go/internal/auth"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/chat"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
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
	if chatDependencies == nil || systemDependencies == nil || adminSettingsDependencies == nil {
		t.Fatal("显式 Server 依赖构造失败")
	}
	// platformDependencies 保存测试服务器显式注入的平台客户端集合。
	platformDependencies, platformDependencyErr := adapter.NewDefaultPlatformDependencies(nil)
	if platformDependencyErr != nil {
		t.Fatalf("NewPlatformDependencies: %v", platformDependencyErr)
	}
	// authentication 保存测试 HTTP 会话中间件需要的认证服务。
	authentication := &auth.Service{Store: store}
	// srv、err 保存测试 HTTP 服务构造结果及失败原因。
	srv, err := New(authentication, mgr, "", ":0", nil, nil, nil, WithChatDependencies(chatDependencies), WithSystemDependencies(systemDependencies), WithOrderDependencies(orderDependencies), WithAccountDependencies(accountDependencies), WithItemDependencies(itemDependencies), WithAutomationDependencies(automationDependencies), WithMiscDependencies(miscDependencies), WithAdminSettingsDependencies(adminSettingsDependencies), WithPlatformDependencies(platformDependencies))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// mtopClient 保存mtopClient，供当前处理流程使用
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
	srv.MTop = mtopClient
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
	// srv.chat 在测试构造阶段一次性注入通信服务，模拟构造 option 的效果。
	srv.chat = chatService
	// applications.chat 重新绑定测试聊天服务，确保实时订阅适配器与 srv.chat 使用同一事件中心。
	srv.applications.chat = newChatSendingApplication(srv)
	return srv, store, cleanup
}

// newUninitializedTestServer 负责newUninitializedTestServer相关处理。
func newUninitializedTestServer(t *testing.T) (*Server, *db.Store, func()) {
	t.Helper()
	// dbPath 保存db路径，供当前处理流程使用
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
	if chatDependencies == nil || systemDependencies == nil || adminSettingsDependencies == nil {
		t.Fatal("显式 Server 依赖构造失败")
	}
	// platformDependencies 保存未初始化测试服务器显式注入的平台客户端集合。
	platformDependencies, platformDependencyErr := adapter.NewDefaultPlatformDependencies(nil)
	if platformDependencyErr != nil {
		t.Fatalf("NewPlatformDependencies: %v", platformDependencyErr)
	}
	// authentication 保存未初始化数据库上的会话中间件依赖。
	authentication := &auth.Service{Store: store}
	// srv、err 保存未初始化测试服务构造结果及失败原因。
	srv, err := New(authentication, mgr, "", ":0", nil, nil, nil, WithChatDependencies(chatDependencies), WithSystemDependencies(systemDependencies), WithOrderDependencies(orderDependencies), WithAccountDependencies(accountDependencies), WithItemDependencies(itemDependencies), WithAutomationDependencies(automationDependencies), WithMiscDependencies(miscDependencies), WithAdminSettingsDependencies(adminSettingsDependencies), WithPlatformDependencies(platformDependencies))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv, store, func() {
		mgr.StopAll()
		_ = d.Close()
	}
}

// roundTripFunc 保存roundTripFunc，供当前处理流程使用
type roundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip 负责RoundTrip相关处理。
func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// noopHandler 保存noopHandler，供当前处理流程使用
type noopHandler struct{}

// HandleChatMessage 处理聊天消息。
func (noopHandler) HandleChatMessage(context.Context, engine.ChatMessage) error { return nil }

// HandleSystemEvent 处理系统Event。
func (noopHandler) HandleSystemEvent(context.Context, automation.Task) error { return nil }

// OnPasswordLoginRefresh 负责On密码登录Refresh相关处理。
func (noopHandler) OnPasswordLoginRefresh(context.Context, string) bool { return false }

// OnAccountAlert 负责On账号Alert相关处理。
func (noopHandler) OnAccountAlert(context.Context, string, string, string, string) {}

// TestLoginVerifyLogout 登录→verify→登出 全链路。
func TestLoginVerifyLogout(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()

	// 1) 登录（用户名密码）。
	body := `{"username":"admin","password":"pw"}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("login status=%d body=%s", rec.Code, rec.Body.String())
	}
	// lr 保存lr，供当前处理流程使用
	var lr loginResponse
	json.Unmarshal(rec.Body.Bytes(), &lr)
	if !lr.Success || !lr.IsAdmin {
		t.Fatalf("登录响应异常: %+v", lr)
	}
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := rec.Result().Cookies()[0]
	if cookie.Name != "session" || cookie.Value == "" || !cookie.HttpOnly {
		t.Fatalf("Cookie 异常: %+v", cookie)
	}

	// 2) verify 带 cookie 应 authenticated。
	req2 := httptest.NewRequest(http.MethodGet, "/verify", nil)
	req2.AddCookie(cookie)
	// rec2 保存rec2，供当前处理流程使用
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	// v 保存v，供当前处理流程使用
	var v map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &v)
	if v["authenticated"] != true || v["initialized"] != true {
		t.Fatalf("verify 异常: %+v", v)
	}

	// 3) 无 cookie verify 应未认证。
	req3 := httptest.NewRequest(http.MethodGet, "/verify", nil)
	// rec3 保存rec3，供当前处理流程使用
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	json.Unmarshal(rec3.Body.Bytes(), &v)
	if v["authenticated"] == true {
		t.Fatal("无 cookie 应未认证")
	}

	// 4) 登出。
	req4 := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req4.AddCookie(cookie)
	// rec4 保存rec4，供当前处理流程使用
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, req4)
	// 登出后 cookie 应被清除（MaxAge=-1）。
	dc := rec4.Result().Cookies()
	// cleared 保存cleared，供当前处理流程使用
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
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()

	// body 保存请求体，供当前处理流程使用
	body := `{"username":"admin","password":"wrong"}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// lr 保存lr，供当前处理流程使用
	var lr loginResponse
	json.Unmarshal(rec.Body.Bytes(), &lr)
	if lr.Success {
		t.Fatal("错误密码不应成功")
	}
}

// TestInitializeFromWebUI 负责TestInitializeFromWebUI相关处理。
func TestInitializeFromWebUI(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newUninitializedTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()

	// shortReq 保存shortReq，供当前处理流程使用
	shortReq := httptest.NewRequest(http.MethodPost, "/initialize", strings.NewReader(`{"password":"short"}`))
	// shortRec 保存shortRec，供当前处理流程使用
	shortRec := httptest.NewRecorder()
	h.ServeHTTP(shortRec, shortReq)
	if shortRec.Code != http.StatusBadRequest {
		t.Fatalf("短密码应返回 400，got %d body=%s", shortRec.Code, shortRec.Body.String())
	}

	// initReq 保存initReq，供当前处理流程使用
	initReq := httptest.NewRequest(http.MethodPost, "/initialize", strings.NewReader(`{"password":"newpassword"}`))
	// initRec 保存initRec，供当前处理流程使用
	initRec := httptest.NewRecorder()
	h.ServeHTTP(initRec, initReq)
	if initRec.Code != http.StatusOK {
		t.Fatalf("初始化 status=%d body=%s", initRec.Code, initRec.Body.String())
	}
	// initResult 保存init结果，供当前处理流程使用
	var initResult loginResponse
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(initRec.Body.Bytes(), &initResult); err != nil {
		t.Fatalf("解析初始化响应: %v", err)
	}
	if !initResult.Success || !initResult.IsAdmin || initResult.Username != "admin" {
		t.Fatalf("初始化响应异常: %+v", initResult)
	}

	// admin、err 保存admin、err，供当前处理流程使用
	admin, err := store.Users.GetAdmin(context.Background())
	if err != nil || admin == nil {
		t.Fatalf("初始化后 admin 不存在: admin=%+v err=%v", admin, err)
	}
	if // ok、err 保存ok、err，供当前处理流程使用
	_, ok, err := store.Users.VerifyAndUpgrade(context.Background(), "admin", "newpassword"); err != nil || !ok {
		t.Fatalf("初始化密码不可用: ok=%v err=%v", ok, err)
	}

	// cookies 保存cookies，供当前处理流程使用
	cookies := initRec.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != "session" || cookies[0].Value == "" {
		t.Fatalf("初始化后应自动建立会话: %+v", cookies)
	}
	// verifyReq 保存verifyReq，供当前处理流程使用
	verifyReq := httptest.NewRequest(http.MethodGet, "/verify", nil)
	verifyReq.AddCookie(cookies[0])
	// verifyRec 保存verifyRec，供当前处理流程使用
	verifyRec := httptest.NewRecorder()
	h.ServeHTTP(verifyRec, verifyReq)
	// verifyResult 保存verify结果，供当前处理流程使用
	var verifyResult map[string]any
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(verifyRec.Body.Bytes(), &verifyResult); err != nil {
		t.Fatalf("解析 verify 响应: %v", err)
	}
	if verifyResult["authenticated"] != true || verifyResult["initialized"] != true {
		t.Fatalf("初始化后 verify 异常: %+v", verifyResult)
	}

	// secondReq 保存secondReq，供当前处理流程使用
	secondReq := httptest.NewRequest(http.MethodPost, "/initialize", strings.NewReader(`{"password":"anotherpw"}`))
	// secondRec 保存secondRec，供当前处理流程使用
	secondRec := httptest.NewRecorder()
	h.ServeHTTP(secondRec, secondReq)
	if secondRec.Code != http.StatusConflict {
		t.Fatalf("重复初始化应返回 409，got %d body=%s", secondRec.Code, secondRec.Body.String())
	}
}

// TestUpdateCredentialsRenamesUserAndRevokesSessions 负责TestUpdateCredentialsRenames用户AndRevokesSessions相关处理。
func TestUpdateCredentialsRenamesUserAndRevokesSessions(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()

	// loginReq 保存登录Req，供当前处理流程使用
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"admin","password":"pw"}`))
	// loginRec 保存登录Rec，供当前处理流程使用
	loginRec := httptest.NewRecorder()
	h.ServeHTTP(loginRec, loginReq)
	// sessionCookie 保存会话登录凭证，供当前处理流程使用
	sessionCookie := loginRec.Result().Cookies()[0]

	// updateReq 保存updateReq，供当前处理流程使用
	updateReq := httptest.NewRequest(http.MethodPut, "/account/credentials", strings.NewReader(
		`{"current_password":"pw","new_username":"operator","new_password":"newpassword"}`,
	))
	updateReq.AddCookie(sessionCookie)
	// updateRec 保存updateRec，供当前处理流程使用
	updateRec := httptest.NewRecorder()
	h.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updateRec.Code, updateRec.Body.String())
	}
	// updateResult 保存update结果，供当前处理流程使用
	var updateResult map[string]any
	json.Unmarshal(updateRec.Body.Bytes(), &updateResult)
	if updateResult["success"] != true || updateResult["requires_relogin"] != true {
		t.Fatalf("update result=%+v", updateResult)
	}

	// verifyReq 保存verifyReq，供当前处理流程使用
	verifyReq := httptest.NewRequest(http.MethodGet, "/verify", nil)
	verifyReq.AddCookie(sessionCookie)
	// verifyRec 保存verifyRec，供当前处理流程使用
	verifyRec := httptest.NewRecorder()
	h.ServeHTTP(verifyRec, verifyReq)
	// verifyResult 保存verify结果，供当前处理流程使用
	var verifyResult map[string]any
	json.Unmarshal(verifyRec.Body.Bytes(), &verifyResult)
	if verifyResult["authenticated"] == true || verifyResult["initialized"] != true {
		t.Fatalf("旧会话应失效且系统仍已初始化: %+v", verifyResult)
	}

	// oldLoginReq 保存old登录Req，供当前处理流程使用
	oldLoginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"admin","password":"pw"}`))
	// oldLoginRec 保存old登录Rec，供当前处理流程使用
	oldLoginRec := httptest.NewRecorder()
	h.ServeHTTP(oldLoginRec, oldLoginReq)
	// oldLogin 保存old登录，供当前处理流程使用
	var oldLogin loginResponse
	json.Unmarshal(oldLoginRec.Body.Bytes(), &oldLogin)
	if oldLogin.Success {
		t.Fatal("旧用户名不应继续登录")
	}

	// newLoginReq 保存new登录Req，供当前处理流程使用
	newLoginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"operator","password":"newpassword"}`))
	// newLoginRec 保存new登录Rec，供当前处理流程使用
	newLoginRec := httptest.NewRecorder()
	h.ServeHTTP(newLoginRec, newLoginReq)
	// newLogin 保存new登录，供当前处理流程使用
	var newLogin loginResponse
	json.Unmarshal(newLoginRec.Body.Bytes(), &newLogin)
	if !newLogin.Success {
		t.Fatalf("新凭据登录失败: %s", newLoginRec.Body.String())
	}
}

// TestCookiesDetailsRequiresAuth 未登录访问受保护端点应 401。
func TestCookiesDetailsRequiresAuth(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodGet, "/cookies/details", nil)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应 401，got %d", rec.Code)
	}
}

// TestCookiesDetailsWithAuth 登录后能取账号详情。
func TestCookiesDetailsWithAuth(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()

	// 登录拿 cookie。
	body := `{"username":"admin","password":"pw"}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := rec.Result().Cookies()[0]

	// 取详情。
	req2 := httptest.NewRequest(http.MethodGet, "/cookies/details", nil)
	req2.AddCookie(cookie)
	// rec2 保存rec2，供当前处理流程使用
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	// arr 保存arr，供当前处理流程使用
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

// TestCookieRuntimeStatusWithAuth 负责Test登录凭证Runtime状态WithAuth相关处理。
func TestCookieRuntimeStatusWithAuth(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()

	// loginReq 保存登录Req，供当前处理流程使用
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"admin","password":"pw"}`))
	// loginRec 保存登录Rec，供当前处理流程使用
	loginRec := httptest.NewRecorder()
	h.ServeHTTP(loginRec, loginReq)
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginRec.Result().Cookies()[0]

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodGet, "/cookies/runtime-status", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// statuses 保存statuses，供当前处理流程使用
	var statuses map[string]engine.RuntimeStatus
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(rec.Body.Bytes(), &statuses); err != nil {
		t.Fatal(err)
	}
	if statuses["acc1"].State != engine.RuntimeError {
		t.Fatalf("未启动账号应返回 error，got %+v", statuses["acc1"])
	}
}

// TestAddAndDeleteCookie 添加/删除账号。
func TestAddAndDeleteCookie(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()

	// body 保存请求体，供当前处理流程使用
	body := `{"username":"admin","password":"pw"}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := rec.Result().Cookies()[0]

	// 添加。
	addBody := `{"id":"acc2","value":"unb=456; _m_h5_tk=tk2_2;"}`
	// req2 保存req2，供当前处理流程使用
	req2 := httptest.NewRequest(http.MethodPost, "/cookies", strings.NewReader(addBody))
	req2.AddCookie(cookie)
	// rec2 保存rec2，供当前处理流程使用
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("add status=%d body=%s", rec2.Code, rec2.Body.String())
	}

	// 删除。
	req3 := httptest.NewRequest(http.MethodDelete, "/cookies/acc2", nil)
	req3.AddCookie(cookie)
	// rec3 保存rec3，供当前处理流程使用
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("delete status=%d body=%s", rec3.Code, rec3.Body.String())
	}
}

// TestHealth 健康检查无需认证。
func TestHealth(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("health status=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"version":"dev"`) {
		t.Fatalf("health response missing build version: %s", rec.Body.String())
	}
}

// TestHealthReportsUnavailableDatabase 负责TestHealthReportsUnavailableDatabase相关处理。
func TestHealthReportsUnavailableDatabase(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	if // err 保存err，供当前处理流程使用
	err := store.DB.Close(); err != nil {
		t.Fatal(err)
	}
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("health status=%d body=%s", rec.Code, rec.Body.String())
	}
	// response 保存响应，供当前处理流程使用
	var response map[string]any
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["status"] != "degraded" || response["database"] != "unavailable" {
		t.Fatalf("health response=%+v", response)
	}
}
