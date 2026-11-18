package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/application/apikey"
	"xianyu-go/internal/db"
)

// TestBearerTokenCannotUseSessionBootstrapRoutes 验证 API Token 不能绕过 scope 白名单调用登录、初始化、会话校验或退出入口。
func TestBearerTokenCannotUseSessionBootstrapRoutes(t *testing.T) {
	// srv、store、cleanup 是当前测试使用的完整 HTTP 服务、隔离数据库和清理函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// owner 是本次测试令牌所属的有效用户。
	owner, err := store.Users.GetAdmin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// token 是仅用于本次测试的有效 Bearer 凭证。
	token := apikey.TokenPrefix + "session-route-rejection-test-token"
	// now 是本次测试令牌签发时间。
	now := time.Now().Unix()
	// record 是具备正常业务只读权限、但不应访问会话入口的令牌记录。
	record := db.APIAccessToken{ID: "session-route-key", UserID: owner.ID, Name: "session route test", TokenHash: apikey.HashToken(token), Prefix: token[:len(apikey.TokenPrefix)+8], Scopes: []string{apikey.ScopeAccountsRead}, CreatedAt: now, ExpiresAt: now + 3600}
	// err 是将仅有业务读取权限的令牌写入隔离数据库时的错误。
	if err := store.APIAccessTokens.Create(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	// cases 是必须拒绝 Bearer Token 的公开会话入口集合。
	cases := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodPost, path: "/api/v1/session/login", body: `{}`},
		{method: http.MethodPost, path: "/api/v1/session/initialize", body: `{}`},
		{method: http.MethodGet, path: "/api/v1/session"},
		{method: http.MethodPost, path: "/api/v1/session/logout"},
		{method: http.MethodPost, path: "/login", body: `{}`},
		{method: http.MethodPost, path: "/initialize", body: `{}`},
		{method: http.MethodGet, path: "/verify"},
		{method: http.MethodPost, path: "/logout"},
	}
	// handler 是包含认证中间件和版本化会话路由的完整 HTTP Router。
	handler := srv.Router()
	// testCase 是当前携带 Bearer Token 访问的登录或会话入口。
	for _, testCase := range cases {
		t.Run(testCase.method+" "+testCase.path, func(t *testing.T) {
			// request 是携带有效 Bearer Token 的会话入口请求。
			request := httptest.NewRequest(testCase.method, testCase.path, strings.NewReader(testCase.body))
			request.Header.Set("Authorization", "Bearer "+token)
			// recorder 捕获认证拒绝结果。
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusForbidden {
				t.Fatalf("Bearer token must be rejected on session route: status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if recorder.Header().Get("Set-Cookie") != "" {
				t.Fatalf("rejected Bearer request unexpectedly changed browser session: %v", recorder.Header())
			}
		})
	}
}
