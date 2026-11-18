package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"xianyu-go/internal/application/apikey"
	"xianyu-go/internal/db"
)

// TestRequiredAPIKeyScopeCoversEveryAllowlistBranch 逐条覆盖 allowlist 中尚未被基础用例触达的路由分支，
// 同时验证同一路径下未登记的 HTTP 方法保持默认拒绝。
func TestRequiredAPIKeyScopeCoversEveryAllowlistBranch(t *testing.T) {
	// cases 保存期望的最小权限；wantScope 为空表示该方法与路径组合必须被拒绝。
	cases := []struct {
		method    string
		path      string
		wantScope string
	}{
		// 账号只读的三个集合入口。
		{http.MethodGet, "/api/v1/accounts", apikey.ScopeAccountsRead},
		{http.MethodGet, "/api/v1/accounts/details", apikey.ScopeAccountsRead},
		{http.MethodGet, "/api/v1/accounts/runtime-status", apikey.ScopeAccountsRead},
		{http.MethodPost, "/api/v1/accounts", ""},
		{http.MethodDelete, "/api/v1/accounts/acc-1", ""},
		// 商品读取、发布与批次生命周期。
		{http.MethodGet, "/api/v1/items", apikey.ScopeItemsRead},
		{http.MethodGet, "/api/v1/items/publish-batches", apikey.ScopeItemsRead},
		{http.MethodGet, "/api/v1/items/publish-batches/batch-1/result.csv", apikey.ScopeItemsRead},
		{http.MethodPost, "/api/v1/items/get-all-from-account", apikey.ScopeItemsRead},
		{http.MethodPost, "/api/v1/items/get-by-page", apikey.ScopeItemsRead},
		{http.MethodPost, "/api/v1/items/publish-categories/recommend", apikey.ScopeItemsRead},
		{http.MethodPost, "/api/v1/items/publish", apikey.ScopeItemsPublish},
		{http.MethodPost, "/api/v1/items/publish-batches", apikey.ScopeItemsPublish},
		{http.MethodPost, "/api/v1/items/publish-batches/batch-1/cancel", apikey.ScopeItemsPublish},
		{http.MethodPost, "/api/v1/items/publish-batches/batch-1/retry-failed", apikey.ScopeItemsPublish},
		{http.MethodPost, "/api/v1/items/publish-batches/batch-1/delete", ""},
		{http.MethodPut, "/api/v1/items/publish", ""},
		{http.MethodPost, "/api/v1/items/acc-1/item-1/delete", ""},
		{http.MethodGet, "/api/v1/items/acc-1/item-1/unpublish", ""},
		// 聊天读取只开放明确列出的 GET 入口。
		{http.MethodGet, "/api/v1/chat/sessions", apikey.ScopeChatRead},
		{http.MethodGet, "/api/v1/chat/items", apikey.ScopeChatRead},
		{http.MethodGet, "/api/v1/chat/quick-replies", apikey.ScopeChatRead},
		{http.MethodGet, "/api/v1/chat/buyer-notes/buyer-1", apikey.ScopeChatRead},
		{http.MethodDelete, "/api/v1/chat/messages", ""},
		{http.MethodGet, "/api/v1/chat/manual-takeover/acc-1", ""},
		// 订单只开放读取。
		{http.MethodPost, "/api/v1/orders", ""},
		{http.MethodDelete, "/api/v1/orders/order-1", ""},
		// 自动化读取、管理与处理。
		{http.MethodGet, "/api/v1/automation-issues", apikey.ScopeAutomationRead},
		{http.MethodGet, "/api/v1/automation-rules", apikey.ScopeAutomationRead},
		{http.MethodGet, "/api/v1/delivery-templates", apikey.ScopeAutomationRead},
		{http.MethodGet, "/api/v1/delivery-templates/tpl-1", apikey.ScopeAutomationRead},
		{http.MethodPost, "/api/v1/automation-rules", apikey.ScopeAutomationManage},
		{http.MethodPost, "/api/v1/delivery-templates", apikey.ScopeAutomationManage},
		{http.MethodPost, "/api/v1/account-tasks/acc-1/run", apikey.ScopeAutomationManage},
		{http.MethodPut, "/api/v1/automation-rules/rule-1", apikey.ScopeAutomationManage},
		{http.MethodDelete, "/api/v1/automation-rules/rule-1", apikey.ScopeAutomationManage},
		{http.MethodPut, "/api/v1/delivery-templates/tpl-1", apikey.ScopeAutomationManage},
		{http.MethodDelete, "/api/v1/delivery-templates/tpl-1", apikey.ScopeAutomationManage},
		{http.MethodPost, "/api/v1/automation-runs/run-1/resolve", apikey.ScopeAutomationManage},
		{http.MethodPost, "/api/v1/automation-pending-tasks/task-1/resolve", apikey.ScopeAutomationManage},
		{http.MethodPost, "/api/v1/automation-runs/acc-1/run-1/resolve", ""},
		{http.MethodPost, "/api/v1/automation-runs/run-1/purge", ""},
		{http.MethodGet, "/api/v1/automation-runs/run-1/resolve", ""},
		{http.MethodDelete, "/api/v1/account-tasks/acc-1", ""},
		// 空方法、大小写和首尾空白被规范化后仍按精确方法匹配。
		{"get", "/api/v1/items", apikey.ScopeItemsRead},
		{" GET ", " /api/v1/items ", apikey.ScopeItemsRead},
		{http.MethodGet, "/api/v1/items/../settings/system", ""},
		{http.MethodGet, "", ""},
	}
	// testCase 是当前要验证的方法、路径与期望权限。
	for _, testCase := range cases {
		// scope、allowed 是该方法与路径的权限映射结果。
		scope, allowed := requiredAPIKeyScope(testCase.method, testCase.path)
		if allowed != (testCase.wantScope != "") || scope != testCase.wantScope {
			t.Errorf("requiredAPIKeyScope(%q %q) = (%q, %v), want scope %q", testCase.method, testCase.path, scope, allowed, testCase.wantScope)
		}
	}
}

// TestReplyManagementRoutesAreExactMethodAllowlist 验证回复管理每条已注册路由的方法白名单与深度限制。
func TestReplyManagementRoutesAreExactMethodAllowlist(t *testing.T) {
	// allowed 是必须授予回复管理权限的路由；denied 是必须拒绝的相邻路由。
	allowed := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/reply-rules/items"},
		{http.MethodGet, "/api/v1/reply-rules/items/acc-1/item-1"},
		{http.MethodPut, "/api/v1/reply-rules/items/acc-1/item-1"},
		{http.MethodDelete, "/api/v1/reply-rules/items/acc-1/item-1"},
		{http.MethodGet, "/api/v1/reply-rules/greetings"},
		{http.MethodPost, "/api/v1/reply-rules/greetings"},
		{http.MethodPut, "/api/v1/reply-rules/greetings/g-1"},
		{http.MethodDelete, "/api/v1/reply-rules/greetings/g-1"},
		{http.MethodPost, "/api/v1/reply-rules/greetings/clear-records/acc-1"},
		{http.MethodGet, "/api/v1/reply-rules/acc-1"},
		{http.MethodPost, "/api/v1/reply-rules/acc-1"},
		{http.MethodGet, "/api/v1/reply-rules/acc-1/items"},
		{http.MethodPost, "/api/v1/reply-rules/acc-1/items"},
		{http.MethodGet, "/api/v1/reply-rules/acc-1/typed"},
		{http.MethodPut, "/api/v1/reply-rules/acc-1/typed/3"},
		{http.MethodDelete, "/api/v1/reply-rules/acc-1/typed/3"},
		{http.MethodDelete, "/api/v1/reply-rules/acc-1/index/3"},
		{http.MethodGet, "/api/v1/default-replies"},
		{http.MethodGet, "/api/v1/default-replies/list"},
		{http.MethodGet, "/api/v1/default-replies/acc-1"},
		{http.MethodPut, "/api/v1/default-replies/acc-1"},
		{http.MethodDelete, "/api/v1/default-replies/acc-1"},
		{http.MethodPost, "/api/v1/default-replies/acc-1/clear-records"},
		{http.MethodGet, "/api/v1/agent-replies/acc-1"},
		{http.MethodPut, "/api/v1/agent-replies/acc-1"},
		{http.MethodPost, "/api/v1/agent-replies/acc-1/test"},
		{http.MethodGet, "/api/v1/settings/ai-reply"},
		{http.MethodGet, "/api/v1/settings/ai-reply/acc-1"},
		{http.MethodPut, "/api/v1/settings/ai-reply/acc-1"},
	}
	// route 是当前必须被授权的回复管理方法与路径。
	for _, route := range allowed {
		if !isReplyManagementRoute(route.method, route.path) {
			t.Errorf("isReplyManagementRoute(%s %s) = false, want true", route.method, route.path)
		}
		// scope、ok 是该路由经 requiredAPIKeyScope 得到的最小权限与是否放行，必须是回复管理权限。
		if scope, ok := requiredAPIKeyScope(route.method, route.path); !ok || scope != apikey.ScopeRepliesManage {
			t.Errorf("requiredAPIKeyScope(%s %s) = (%q, %v), want replies:manage", route.method, route.path, scope, ok)
		}
	}
	// denied 覆盖方法不匹配、路径过深、空段、未知子资源和系统设置等相邻路由。
	denied := []struct{ method, path string }{
		{http.MethodPatch, "/api/v1/reply-rules/items/acc-1/item-1"},
		{http.MethodDelete, "/api/v1/reply-rules/greetings"},
		{http.MethodGet, "/api/v1/reply-rules/greetings/g-1"},
		{http.MethodGet, "/api/v1/reply-rules/greetings/clear-records/acc-1"},
		{http.MethodDelete, "/api/v1/reply-rules/acc-1"},
		{http.MethodPut, "/api/v1/reply-rules/acc-1/items"},
		{http.MethodPost, "/api/v1/reply-rules/acc-1/typed"},
		{http.MethodGet, "/api/v1/reply-rules/acc-1/typed/3"},
		{http.MethodPut, "/api/v1/reply-rules/acc-1/index/3"},
		{http.MethodGet, "/api/v1/reply-rules/acc-1/index/3"},
		{http.MethodGet, "/api/v1/reply-rules/acc-1/typed/3/extra"},
		{http.MethodPost, "/api/v1/default-replies"},
		{http.MethodPost, "/api/v1/default-replies/list"},
		{http.MethodGet, "/api/v1/default-replies/acc-1/clear-records"},
		{http.MethodDelete, "/api/v1/agent-replies/acc-1"},
		{http.MethodGet, "/api/v1/agent-replies/acc-1/test"},
		{http.MethodPut, "/api/v1/settings/ai-reply"},
		{http.MethodDelete, "/api/v1/settings/ai-reply/acc-1"},
		{http.MethodGet, "/api/v1/settings/system"},
	}
	// route 是当前必须被默认拒绝的相邻路由。
	for _, route := range denied {
		if isReplyManagementRoute(route.method, route.path) {
			t.Errorf("isReplyManagementRoute(%s %s) = true, want false", route.method, route.path)
		}
	}
}

// TestExactPathSegmentsAndScopeHelpers 验证路径分段与权限包含判断的边界输入。
func TestExactPathSegmentsAndScopeHelpers(t *testing.T) {
	// cases 保存前缀、段数和期望结果；覆盖前缀不匹配、空后缀、空段与重复斜杠。
	cases := []struct {
		path   string
		prefix string
		count  int
		want   bool
	}{
		{"/api/v1/x/a", "/api/v1/x/", 1, true},
		{"/api/v1/x/a/b", "/api/v1/x/", 2, true},
		{"/api/v1/x/a/b", "/api/v1/x/", 1, false},
		{"/api/v1/y/a", "/api/v1/x/", 1, false},
		{"/api/v1/x/", "/api/v1/x/", 1, false},
		{"/api/v1/x//a", "/api/v1/x/", 2, false},
		{"/api/v1/x/a/", "/api/v1/x/", 1, false},
		{"/api/v1/x/a/", "/api/v1/x/", 2, false},
	}
	// testCase 是当前路径分段样本。
	for _, testCase := range cases {
		// got 是 exactPathSegments 对当前样本的判断结果，需与期望一致。
		if got := exactPathSegments(testCase.path, testCase.prefix, testCase.count); got != testCase.want {
			t.Errorf("exactPathSegments(%q,%q,%d)=%v want %v", testCase.path, testCase.prefix, testCase.count, got, testCase.want)
		}
	}
	if replyRuleTailMatches("/api/v1/reply-rules/acc-1/typed", "typed", 1) {
		t.Error("深度不符时 replyRuleTailMatches 必须返回 false")
	}
	if !hasAPIKeyScope([]string{apikey.ScopeChatRead}, apikey.ScopeChatRead) || hasAPIKeyScope([]string{apikey.ScopeChatRead}, apikey.ScopeChatSend) || hasAPIKeyScope(nil, apikey.ScopeChatRead) {
		t.Error("hasAPIKeyScope 必须只做精确包含判断")
	}
}

// TestParseBearerTokenAcceptsOnlyStandardForm 验证 Authorization 头只接受 Bearer + 带本产品前缀的单一令牌。
func TestParseBearerTokenAcceptsOnlyStandardForm(t *testing.T) {
	// valid 是带固定前缀的假令牌，不对应任何真实凭证。
	valid := apikey.TokenPrefix + "fake-material"
	// cases 覆盖大小写、缺少令牌、多余字段、其他方案与缺前缀。
	cases := []struct {
		header string
		want   bool
	}{
		{"Bearer " + valid, true},
		{"bearer " + valid, true},
		{"BEARER   " + valid, true},
		{"Bearer", false},
		{"Bearer " + valid + " extra", false},
		{"Basic " + valid, false},
		{"Bearer not-our-prefix", false},
		{valid, false},
		{"", false},
	}
	// testCase 是当前待解析的认证头样本。
	for _, testCase := range cases {
		// token、ok 是解析认证头得到的令牌和是否合法；非法时令牌必须为空串。
		token, ok := parseBearerToken(testCase.header)
		if ok != testCase.want || (ok && token != valid) || (!ok && token != "") {
			t.Errorf("parseBearerToken(%q) = (%q,%v), want ok=%v", testCase.header, token, ok, testCase.want)
		}
	}
}

// TestRejectAPIKeyBlocksOnlyBearerIdentity 验证公开会话入口拒绝 Bearer 身份，对匿名与 Cookie 请求保持透传。
func TestRejectAPIKeyBlocksOnlyBearerIdentity(t *testing.T) {
	// reached 记录下游处理器是否被调用。
	reached := false
	// handler 是被 RejectAPIKey 包裹的下游处理器。
	handler := RejectAPIKey(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusNoContent)
	}))
	// anonymousRecorder 捕获无任何身份时的响应。
	anonymousRecorder := httptest.NewRecorder()
	handler.ServeHTTP(anonymousRecorder, httptest.NewRequest(http.MethodPost, "/api/v1/session/login", nil))
	if anonymousRecorder.Code != http.StatusNoContent || !reached {
		t.Fatalf("匿名请求应透传: status=%d reached=%v", anonymousRecorder.Code, reached)
	}
	reached = false
	// bearerRequest 携带外部令牌身份访问会话入口。
	bearerRequest := httptest.NewRequest(http.MethodPost, "/api/v1/session/login", nil)
	bearerRequest = bearerRequest.WithContext(WithAPIKey(bearerRequest.Context(), APIKeyIdentity{ID: "k1"}))
	// bearerRecorder 捕获被拒绝的响应。
	bearerRecorder := httptest.NewRecorder()
	handler.ServeHTTP(bearerRecorder, bearerRequest)
	if bearerRecorder.Code != http.StatusForbidden || reached {
		t.Fatalf("Bearer 身份必须被拒绝: status=%d reached=%v", bearerRecorder.Code, reached)
	}
}

// TestRequireAuthRejectsBearerWithoutRequiredScope 验证未登记路径与缺失权限均返回 403，且不会调用下游。
func TestRequireAuthRejectsBearerWithoutRequiredScope(t *testing.T) {
	// reached 记录下游是否被调用。
	reached := false
	// handler 是仅在授权通过时才会执行的下游处理器。
	handler := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusNoContent)
	}))
	// cases 覆盖已授予、缺权限、未登记路径三种 Bearer 场景。
	cases := []struct {
		name     string
		method   string
		path     string
		scopes   []string
		wantCode int
	}{
		{"granted", http.MethodGet, "/api/v1/items", []string{apikey.ScopeItemsRead}, http.StatusNoContent},
		{"missing-scope", http.MethodGet, "/api/v1/items", []string{apikey.ScopeChatRead}, http.StatusForbidden},
		{"empty-scopes", http.MethodGet, "/api/v1/items", nil, http.StatusForbidden},
		{"unlisted-route", http.MethodGet, "/api/v1/settings/system", []string{apikey.ScopeItemsRead}, http.StatusForbidden},
	}
	// testCase 是当前 Bearer 授权场景。
	for _, testCase := range cases {
		reached = false
		// request 模拟已通过 Middleware 认证的 Bearer 请求上下文。
		request := httptest.NewRequest(testCase.method, testCase.path, nil)
		request = request.WithContext(WithAPIKey(WithSession(request.Context(), &db.Session{UserID: 1}), APIKeyIdentity{ID: "k1", Scopes: testCase.scopes}))
		// recorder 捕获该场景的响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != testCase.wantCode {
			t.Errorf("%s: status=%d want %d", testCase.name, recorder.Code, testCase.wantCode)
		}
		if reached != (testCase.wantCode == http.StatusNoContent) {
			t.Errorf("%s: 下游不应被调用", testCase.name)
		}
	}
}

// TestRequireSessionAcceptsOnlyPlainBrowserSession 验证令牌管理入口的守卫：匿名拒绝、Cookie 会话放行。
func TestRequireSessionAcceptsOnlyPlainBrowserSession(t *testing.T) {
	// handler 是仅在浏览器会话下才会执行的下游。
	handler := RequireSession(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	// anonymous 是没有任何身份的请求，必须被拒绝。
	anonymous := httptest.NewRecorder()
	handler.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/api/v1/access-tokens", nil))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("匿名请求 status=%d", anonymous.Code)
	}
	// browserRequest 携带真实浏览器会话身份（无令牌身份）。
	browserRequest := httptest.NewRequest(http.MethodGet, "/api/v1/access-tokens", nil)
	browserRequest = browserRequest.WithContext(WithSession(browserRequest.Context(), &db.Session{UserID: 1}))
	// browser 捕获放行结果。
	browser := httptest.NewRecorder()
	handler.ServeHTTP(browser, browserRequest)
	if browser.Code != http.StatusNoContent {
		t.Fatalf("浏览器会话应放行 status=%d", browser.Code)
	}
}
