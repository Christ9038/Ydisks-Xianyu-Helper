package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apikeyapp "xianyu-go/internal/application/apikey"
	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
)

// apiAccessTokensPortStub 是令牌管理 Handler 测试使用的可控应用端口替身。
type apiAccessTokensPortStub struct {
	// createErr、listErr、revokeErr 是各操作预设返回的应用层错误。
	createErr, listErr, revokeErr error
}

// Create 返回预设错误，成功时给出固定摘要。
func (port apiAccessTokensPortStub) Create(context.Context, int64, apikeyapp.CreateInput) (apikeyapp.Created, error) {
	return apikeyapp.Created{Token: "ydk_live_stub", Summary: apikeyapp.Summary{ID: "stub"}}, port.createErr
}

// List 返回预设错误。
func (port apiAccessTokensPortStub) List(context.Context, int64) ([]apikeyapp.Summary, error) {
	return nil, port.listErr
}

// Revoke 返回预设错误。
func (port apiAccessTokensPortStub) Revoke(context.Context, int64, string) error {
	return port.revokeErr
}

// externalAPILogin 用测试管理员凭据登录并返回浏览器会话 Cookie。
func externalAPILogin(t *testing.T, handler http.Handler) *http.Cookie {
	t.Helper()
	// recorder 捕获登录响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/session/login", strings.NewReader(`{"username":"admin","password":"pw"}`)))
	if recorder.Code != http.StatusOK || len(recorder.Result().Cookies()) == 0 {
		t.Fatalf("login status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	return recorder.Result().Cookies()[0]
}

// externalAPIBearer 为管理员签发仅含指定权限的测试令牌，并返回明文。
func externalAPIBearer(t *testing.T, store *db.Store, id string, scopes ...string) string {
	t.Helper()
	// owner 是令牌归属用户。
	owner, err := store.Users.GetAdmin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// token 是仅用于测试的假令牌。
	token := apikeyapp.TokenPrefix + id + "-fake-material"
	// now 是签发时间。
	now := time.Now().Unix()
	// err 是把测试令牌摘要写入隔离数据库时的错误，应为空。
	if err := store.APIAccessTokens.Create(context.Background(), db.APIAccessToken{ID: id, UserID: owner.ID, Name: id, TokenHash: apikeyapp.HashToken(token), Prefix: token[:len(apikeyapp.TokenPrefix)+8], Scopes: scopes, CreatedAt: now, ExpiresAt: now + 3600}); err != nil {
		t.Fatal(err)
	}
	return token
}

// TestAPIAccessTokenHandlersRejectUnauthenticatedAndBearer 验证三个管理接口对匿名与 Bearer 请求都拒绝，且不触达应用端口。
func TestAPIAccessTokenHandlersRejectUnauthenticatedAndBearer(t *testing.T) {
	// srv、store、cleanup 是完整测试服务、其隔离数据库与资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是包含真实认证与版本化路由的路由树。
	handler := srv.Router()
	// bearer 拥有全部相关权限，仍不得管理令牌。
	bearer := externalAPIBearer(t, store, "manage-attempt", apikeyapp.SupportedScopes()...)
	// routes 是三个令牌管理操作。
	routes := []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/access-tokens", ""},
		{http.MethodPost, "/api/v1/access-tokens", `{"name":"x","scopes":["items:read"]}`},
		{http.MethodDelete, "/api/v1/access-tokens/some-id", ""},
	}
	// route 是当前被测操作。
	for _, route := range routes {
		// authorization 是当前请求携带的认证头；空串表示匿名，另一项为外部 Bearer 令牌。
		for _, authorization := range []string{"", "Bearer " + bearer} {
			// request 是对当前令牌管理路由发起的请求。
			request := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
			if authorization != "" {
				request.Header.Set("Authorization", authorization)
			}
			// recorder 捕获当前请求的响应。
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized {
				t.Errorf("%s %s bearer=%v status=%d body=%s", route.method, route.path, authorization != "", recorder.Code, recorder.Body.String())
			}
		}
	}
	// 直接调用 Handler（绕过路由中间件），验证 Handler 自身也拒绝无会话请求（纵深防御）。
	for name, call := range map[string]func(http.ResponseWriter, *http.Request){
		"list": srv.listAPIAccessTokens, "create": srv.createAPIAccessToken, "revoke": srv.revokeAPIAccessToken,
	} {
		// direct 捕获无会话时 Handler 自身的响应。
		direct := httptest.NewRecorder()
		call(direct, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`)))
		if direct.Code != http.StatusUnauthorized {
			t.Errorf("%s 无会话直接调用 status=%d", name, direct.Code)
		}
	}
}

// TestAPIAccessTokenHandlersValidationAndErrors 验证请求格式、业务校验与错误映射，且错误响应不含令牌材料。
func TestAPIAccessTokenHandlersValidationAndErrors(t *testing.T) {
	// srv、cleanup 是完整测试服务及其资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是包含真实认证与版本化路由的路由树。
	handler := srv.Router()
	// cookie 是管理员登录后取得的浏览器会话 Cookie。
	cookie := externalAPILogin(t, handler)
	// do 发送带会话 Cookie 的请求。
	do := func(method, path, body string) *httptest.ResponseRecorder {
		// request 是带管理员会话 Cookie 的请求。
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.AddCookie(cookie)
		// recorder 捕获当前请求的响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	// 真实服务：非法 JSON、未知字段权限、空权限和不存在的令牌。
	for name, testCase := range map[string]struct {
		method, path, body string
		want               int
	}{
		"bad-json":        {http.MethodPost, "/api/v1/access-tokens", `{`, http.StatusBadRequest},
		"unknown-scope":   {http.MethodPost, "/api/v1/access-tokens", `{"name":"x","scopes":["admin:*"]}`, http.StatusBadRequest},
		"no-scope":        {http.MethodPost, "/api/v1/access-tokens", `{"name":"x","scopes":[]}`, http.StatusBadRequest},
		"too-long-expiry": {http.MethodPost, "/api/v1/access-tokens", `{"name":"x","scopes":["items:read"],"expires_in_days":9999}`, http.StatusBadRequest},
		"revoke-missing":  {http.MethodDelete, "/api/v1/access-tokens/missing", "", http.StatusNotFound},
	} {
		// recorder 是当前非法输入请求的响应，状态码需与期望一致。
		if recorder := do(testCase.method, testCase.path, testCase.body); recorder.Code != testCase.want {
			t.Errorf("%s: status=%d want %d body=%s", name, recorder.Code, testCase.want, recorder.Body.String())
		}
	}
	// 替身端口：覆盖各应用错误到 HTTP 状态的映射。
	for name, testCase := range map[string]struct {
		stub   apiAccessTokensPortStub
		method string
		path   string
		body   string
		want   int
	}{
		"list-failure":     {apiAccessTokensPortStub{listErr: errors.New("db secret detail")}, http.MethodGet, "/api/v1/access-tokens", "", http.StatusInternalServerError},
		"create-internal":  {apiAccessTokensPortStub{createErr: errors.New("db secret detail")}, http.MethodPost, "/api/v1/access-tokens", `{"name":"x","scopes":["items:read"]}`, http.StatusInternalServerError},
		"create-user":      {apiAccessTokensPortStub{createErr: apikeyapp.ErrInvalidUser}, http.MethodPost, "/api/v1/access-tokens", `{"name":"x","scopes":["items:read"]}`, http.StatusUnauthorized},
		"revoke-not-found": {apiAccessTokensPortStub{revokeErr: apikeyapp.ErrNotFound}, http.MethodDelete, "/api/v1/access-tokens/a", "", http.StatusNotFound},
		"revoke-internal":  {apiAccessTokensPortStub{revokeErr: errors.New("db secret detail")}, http.MethodDelete, "/api/v1/access-tokens/a", "", http.StatusInternalServerError},
		"revoke-bad-input": {apiAccessTokensPortStub{revokeErr: apikeyapp.ErrInvalidInput}, http.MethodDelete, "/api/v1/access-tokens/a", "", http.StatusBadRequest},
	} {
		srv.applications.apiAccessTokens = testCase.stub
		// recorder 是端口替身返回预设错误后的响应；内部错误细节不得透出。
		recorder := do(testCase.method, testCase.path, testCase.body)
		if recorder.Code != testCase.want {
			t.Errorf("%s: status=%d want %d body=%s", name, recorder.Code, testCase.want, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), "db secret detail") {
			t.Errorf("%s: 内部错误细节不得出现在响应中: %s", name, recorder.Body.String())
		}
	}
}

// TestItemPlatformUnpublishErrorMapping 验证下架 Handler 对每类应用错误给出稳定状态，并拒绝未确认的"成功"结果。
func TestItemPlatformUnpublishErrorMapping(t *testing.T) {
	// srv、store、cleanup 是完整测试服务、其隔离数据库与资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是包含真实认证与版本化路由的路由树。
	handler := srv.Router()
	// token 是仅授予商品平台下架权限的外部令牌明文。
	token := externalAPIBearer(t, store, "unpublish-map", apikeyapp.ScopeItemsUnpublish)
	// do 以 Bearer 调用下架路由。
	do := func(path string) *httptest.ResponseRecorder {
		// request 是携带 Bearer 令牌的下架请求。
		request := httptest.NewRequest(http.MethodPost, path, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		// recorder 捕获当前下架请求的响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	// good 是完整成功的结果，用作"部分字段缺失"场景的基线。
	good := itemapp.DownShelfOutcome{ItemID: "item-1", Status: "off_sale", PlatformSuccess: true, CookiePersisted: true}
	// name、testCase 是下架错误映射场景的名称与其预设的端口结果、期望状态码。
	for name, testCase := range map[string]struct {
		outcome itemapp.DownShelfOutcome
		err     error
		want    int
	}{
		"invalid-input":       {itemapp.DownShelfOutcome{}, itemapp.ErrDownShelfInvalidInput, http.StatusBadRequest},
		"credential-changed":  {itemapp.DownShelfOutcome{}, itemapp.ErrDownShelfCredentialChanged, http.StatusConflict},
		"unsupported":         {itemapp.DownShelfOutcome{}, itemapp.ErrDownShelfUnsupported, http.StatusNotImplemented},
		"platform-failure":    {itemapp.DownShelfOutcome{}, errors.New("upstream raw detail"), http.StatusBadGateway},
		"no-platform-success": {itemapp.DownShelfOutcome{ItemID: "item-1", Status: "off_sale", CookiePersisted: true}, nil, http.StatusBadGateway},
		"wrong-status":        {itemapp.DownShelfOutcome{ItemID: "item-1", Status: "on_sale", PlatformSuccess: true, CookiePersisted: true}, nil, http.StatusBadGateway},
		"cookie-not-saved":    {itemapp.DownShelfOutcome{ItemID: "item-1", Status: "off_sale", PlatformSuccess: true}, nil, http.StatusBadGateway},
		"success":             {good, nil, http.StatusOK},
	} {
		srv.applications.itemPlatformLifecycle = &itemPlatformLifecyclePortStub{outcome: testCase.outcome, err: testCase.err}
		// recorder 是当前场景下架请求的响应；原始平台错误不得透出。
		recorder := do("/api/v1/items/acc1/item-1/unpublish")
		if recorder.Code != testCase.want {
			t.Errorf("%s: status=%d want %d body=%s", name, recorder.Code, testCase.want, recorder.Body.String())
		}
		if strings.Contains(recorder.Body.String(), "upstream raw detail") {
			t.Errorf("%s: 平台原始错误不得透出: %s", name, recorder.Body.String())
		}
	}
	// 空白商品 ID 在 TrimSpace 后为空，必须在触达端口前以 400 拒绝。
	port := &itemPlatformLifecyclePortStub{outcome: good}
	srv.applications.itemPlatformLifecycle = port
	// recorder 是商品 ID 为空白时的响应，必须在触达应用端口前以 400 拒绝。
	if recorder := do("/api/v1/items/acc1/%20/unpublish"); recorder.Code != http.StatusBadRequest || port.itemID != "" {
		t.Errorf("空白商品 ID: status=%d port=%+v", recorder.Code, port)
	}
	// 非本人或不存在的账号不得触达下架端口。
	port.cookieID = ""
	// recorder 是账号不属于当前用户时的响应，不得触达下架端口。
	if recorder := do("/api/v1/items/not-mine/item-1/unpublish"); recorder.Code == http.StatusOK || port.cookieID != "" {
		t.Errorf("无归属账号不得下架: status=%d port=%+v", recorder.Code, port)
	}
}

// TestManualTakeoverHandlerValidation 验证人工介入接口的请求校验、单字段更新和账号归属。
func TestManualTakeoverHandlerValidation(t *testing.T) {
	// srv、store、cleanup 是完整测试服务、其隔离数据库与资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是包含真实认证与版本化路由的路由树。
	handler := srv.Router()
	// token 是仅授予人工介入权限的外部令牌明文。
	token := externalAPIBearer(t, store, "takeover-edge", apikeyapp.ScopeChatTakeover)
	// do 以 Bearer 调用人工介入接口。
	do := func(account, body string) *httptest.ResponseRecorder {
		// request 是携带 Bearer 令牌的人工介入配置请求。
		request := httptest.NewRequest(http.MethodPut, "/api/v1/chat/manual-takeover/"+account, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+token)
		// recorder 捕获当前请求的响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	// name、body 是非法人工介入请求的场景名称与请求体。
	for name, body := range map[string]string{
		"bad-json":     `{`,
		"empty-object": `{}`,
		"null-fields":  `{"manual_takeover_minutes":null,"manual_takeover_scope":null}`,
		"bad-scope":    `{"manual_takeover_scope":"everything"}`,
		"negative":     `{"manual_takeover_minutes":-5}`,
	} {
		// recorder 是当前非法请求体的响应，必须为 400。
		if recorder := do("acc1", body); recorder.Code != http.StatusBadRequest {
			t.Errorf("%s: status=%d body=%s", name, recorder.Code, recorder.Body.String())
		}
	}
	// 仅更新分钟数时，范围保持账号当前值并在响应中回显。
	first := do("acc1", `{"manual_takeover_minutes":10,"manual_takeover_scope":"ai"}`)
	if first.Code != http.StatusOK {
		t.Fatalf("首次更新失败: %d %s", first.Code, first.Body.String())
	}
	// second 是只提交分钟数的第二次更新响应，范围应沿用首次保存的值。
	second := do("acc1", `{"manual_takeover_minutes":20}`)
	if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), `"manual_takeover_minutes":20`) || !strings.Contains(second.Body.String(), `"manual_takeover_scope":"ai"`) {
		t.Fatalf("单字段更新应保留另一字段: %d %s", second.Code, second.Body.String())
	}
	// recorder 是更新不存在或非本人账号的响应，不得成功。
	if recorder := do("not-mine", `{"manual_takeover_minutes":1}`); recorder.Code == http.StatusOK {
		t.Fatalf("不存在或非本人账号不得更新: %d %s", recorder.Code, recorder.Body.String())
	}
	// got 是响应的 Cache-Control 头，必须包含 no-store。
	if got := second.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Errorf("Cache-Control=%q", got)
	}
}
