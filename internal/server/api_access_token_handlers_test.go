package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xianyu-go/internal/application/apikey"
)

// TestAPIAccessTokenManagementAndBearerUse 验证创建、一次性展示、列表脱敏、权限控制和撤销后的即时失效。
func TestAPIAccessTokenManagementAndBearerUse(t *testing.T) {
	// srv 是使用隔离 SQLite 数据库和本地平台替身构造的完整测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是包含版本化路由和认证中间件的测试路由树。
	handler := srv.Router()
	// loginRequest 是用于取得真实浏览器会话的登录请求。
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/session/login", strings.NewReader(`{"username":"admin","password":"pw"}`))
	// loginRecorder 捕获登录响应和 Set-Cookie。
	loginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(loginRecorder, loginRequest)
	if loginRecorder.Code != http.StatusOK || len(loginRecorder.Result().Cookies()) == 0 {
		t.Fatalf("login status=%d body=%s", loginRecorder.Code, loginRecorder.Body.String())
	}
	// sessionCookie 是登录成功后下发的 HttpOnly 浏览器会话 Cookie。
	sessionCookie := loginRecorder.Result().Cookies()[0]
	// createRequest 是签发只读商品权限令牌的管理请求。
	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/access-tokens", strings.NewReader(`{"name":"商品读取 Agent","scopes":["items:read"],"expires_in_days":30}`))
	createRequest.AddCookie(sessionCookie)
	// createRecorder 捕获创建结果，其中 Token 只允许出现一次。
	createRecorder := httptest.NewRecorder()
	handler.ServeHTTP(createRecorder, createRequest)
	assertOpenAPIExpectedStatusResponse(t, createRequest, createRecorder, http.StatusCreated)
	if !strings.Contains(createRecorder.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("create response must not be cached: headers=%v", createRecorder.Header())
	}
	// created 是创建响应中的一次性令牌和非敏感摘要。
	var created apiAccessTokenCreatedResponse
	// err 是解析一次性令牌创建响应时的 JSON 契约错误。
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if !created.Success || !strings.HasPrefix(created.Token, apikey.TokenPrefix) || created.Data.Name != "商品读取 Agent" {
		t.Fatalf("created response=%+v", created)
	}
	// listRequest 通过浏览器会话读取令牌列表，确认不会再次返回明文。
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/access-tokens", nil)
	listRequest.AddCookie(sessionCookie)
	// listRecorder 捕获脱敏后的令牌列表。
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, listRequest)
	assertOpenAPISuccessResponse(t, listRequest, listRecorder)
	if strings.Contains(listRecorder.Body.String(), created.Token) || strings.Contains(listRecorder.Body.String(), "token_hash") {
		t.Fatalf("list response must not expose token material: body=%s", listRecorder.Body.String())
	}
	// bearerRequest 使用刚签发的令牌读取商品列表。
	bearerRequest := httptest.NewRequest(http.MethodGet, "/api/v1/items", nil)
	bearerRequest.Header.Set("Authorization", "Bearer "+created.Token)
	// bearerRecorder 捕获商品列表响应。
	bearerRecorder := httptest.NewRecorder()
	handler.ServeHTTP(bearerRecorder, bearerRequest)
	assertOpenAPISuccessResponse(t, bearerRequest, bearerRecorder)
	// deniedRequest 使用商品只读令牌尝试访问未授权的聊天接口。
	deniedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/chat/messages", nil)
	deniedRequest.Header.Set("Authorization", "Bearer "+created.Token)
	// deniedRecorder 捕获权限不足响应。
	deniedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deniedRecorder, deniedRequest)
	if deniedRecorder.Code != http.StatusForbidden {
		t.Fatalf("insufficient scope status=%d body=%s", deniedRecorder.Code, deniedRecorder.Body.String())
	}
	// manageRequest 尝试使用 API 令牌管理自身，必须要求浏览器会话。
	manageRequest := httptest.NewRequest(http.MethodGet, "/api/v1/access-tokens", nil)
	manageRequest.Header.Set("Authorization", "Bearer "+created.Token)
	// manageRecorder 捕获仅浏览器会话可访问的拒绝响应。
	manageRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manageRecorder, manageRequest)
	if manageRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("bearer must not manage tokens, status=%d body=%s", manageRecorder.Code, manageRecorder.Body.String())
	}
	// revokeRequest 使用原浏览器会话撤销令牌。
	revokeRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/access-tokens/"+created.Data.ID, nil)
	revokeRequest.AddCookie(sessionCookie)
	// revokeRecorder 捕获撤销操作结果。
	revokeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(revokeRecorder, revokeRequest)
	assertOpenAPISuccessResponse(t, revokeRequest, revokeRecorder)
	// revokedRequest 在撤销后再次使用原令牌，必须立即得到 401。
	revokedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/items", nil)
	revokedRequest.Header.Set("Authorization", "Bearer "+created.Token)
	// revokedRecorder 捕获撤销后的认证结果。
	revokedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(revokedRecorder, revokedRequest)
	if revokedRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token status=%d body=%s", revokedRecorder.Code, revokedRecorder.Body.String())
	}
}
