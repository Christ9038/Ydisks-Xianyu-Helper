package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"xianyu-go/internal/application/apikey"
	"xianyu-go/internal/db"
)

// TestBearerAPIKeyScopes 验证 Bearer 令牌按路径和权限授权，未登记的接口默认拒绝。
func TestBearerAPIKeyScopes(t *testing.T) {
	// database 是本测试独占的 SQLite 数据库。
	database, _, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "api-key.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	// store 是当前测试使用的数据库仓储聚合。
	store := db.NewStore(database, db.DialectSQLite)
	// created 表示测试用户是否创建成功。
	created, err := store.Users.Create(context.Background(), "api-user", "api-user@example.com", "password")
	if err != nil || !created {
		t.Fatalf("create user: created=%v err=%v", created, err)
	}
	// owner 是本次测试签发令牌的用户。
	owner, err := store.Users.GetByUsername(context.Background(), "api-user")
	if err != nil {
		t.Fatal(err)
	}
	// token 是模拟外部 Agent 使用的高熵 Bearer 凭证。
	token := apikey.TokenPrefix + "test-token-material-which-is-not-stored"
	// now 是令牌创建和认证使用的 Unix 秒数。
	now := time.Now().Unix()
	// record 是只存摘要的访问令牌记录。
	record := db.APIAccessToken{ID: "scope-test", UserID: owner.ID, Name: "scope-test", TokenHash: apikey.HashToken(token), Prefix: token[:len(apikey.TokenPrefix)+8], Scopes: []string{apikey.ScopeItemsRead}, CreatedAt: now, ExpiresAt: now + 3600}
	// err 是将测试令牌摘要和权限元数据写入隔离数据库时的错误。
	if err := store.APIAccessTokens.Create(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	// service 是处理 Bearer 令牌和原 Cookie 会话的认证中间件服务。
	service := &Service{Store: store}
	// handler 是验证令牌身份后返回 204 的受保护端点。
	handler := service.Middleware(RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// session 是中间件为兼容现有 handler 注入的最小用户身份。
		session := SessionFromContext(r.Context())
		// identity 是当前 Bearer 令牌的非秘密权限快照。
		identity := APIKeyFromContext(r.Context())
		if session == nil || session.UserID != owner.ID || session.IsAdmin || identity == nil || identity.ID != record.ID {
			t.Errorf("unexpected request identity: session=%+v key=%+v", session, identity)
		}
		w.WriteHeader(http.StatusNoContent)
	})))
	// allowedRequest 是携带商品只读权限的合法请求。
	allowedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/items", nil)
	allowedRequest.Header.Set("Authorization", "Bearer "+token)
	// allowedRecorder 捕获具备商品只读权限的合法 Bearer 请求响应。
	allowedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(allowedRecorder, allowedRequest)
	if allowedRecorder.Code != http.StatusNoContent {
		t.Fatalf("allowed request status=%d body=%s", allowedRecorder.Code, allowedRecorder.Body.String())
	}
	if allowedRecorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("valid Bearer response Cache-Control=%q; want no-store", allowedRecorder.Header().Get("Cache-Control"))
	}
	// deniedRequest 使用同一令牌尝试读取聊天消息，权限不足时必须返回 403。
	deniedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/chat/messages", nil)
	deniedRequest.Header.Set("Authorization", "Bearer "+token)
	// deniedRecorder 捕获令牌缺少聊天读取权限时的拒绝响应。
	deniedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deniedRecorder, deniedRequest)
	if deniedRecorder.Code != http.StatusForbidden {
		t.Fatalf("insufficient scope status=%d body=%s", deniedRecorder.Code, deniedRecorder.Body.String())
	}
	// unlistedRequest 尝试调用未纳入外部令牌白名单的系统设置接口。
	unlistedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/settings/system", nil)
	unlistedRequest.Header.Set("Authorization", "Bearer "+token)
	// unlistedRecorder 捕获未登记外部权限的系统设置请求拒绝响应。
	unlistedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unlistedRecorder, unlistedRequest)
	if unlistedRecorder.Code != http.StatusForbidden {
		t.Fatalf("unlisted route status=%d body=%s", unlistedRecorder.Code, unlistedRecorder.Body.String())
	}
}

// TestInvalidBearerDoesNotFallBackToCookie 验证显式无效 Bearer 不会借用同请求中的有效 Cookie 会话。
func TestInvalidBearerDoesNotFallBackToCookie(t *testing.T) {
	// database 是本测试独占的 SQLite 数据库。
	database, _, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "api-key-fallback.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	// store 是当前测试使用的数据库仓储聚合。
	store := db.NewStore(database, db.DialectSQLite)
	// created 表示测试用户是否创建成功。
	created, err := store.Users.Create(context.Background(), "cookie-user", "cookie-user@example.com", "password")
	if err != nil || !created {
		t.Fatalf("create user: created=%v err=%v", created, err)
	}
	// owner 是当前浏览器会话所属的用户。
	owner, err := store.Users.GetByUsername(context.Background(), "cookie-user")
	if err != nil {
		t.Fatal(err)
	}
	// sessionID 是有效浏览器 Cookie 会话的随机标识。
	sessionID, err := store.Sessions.Create(context.Background(), owner)
	if err != nil {
		t.Fatal(err)
	}
	// handler 是需要认证的端点；只有真正的会话身份才能通过。
	handler := (&Service{Store: store}).Middleware(RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	// request 同时携带有效 Cookie 和无效 Bearer，显式 Bearer 失败后不得回退。
	request := httptest.NewRequest(http.MethodGet, "/api/v1/items", nil)
	request.AddCookie(&http.Cookie{Name: CookieName, Value: sessionID})
	request.Header.Set("Authorization", "Bearer ydk_live_invalid")
	// recorder 捕获认证结果。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("invalid bearer should not fall back to cookie, status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

// TestRequireSessionRejectsAPIKey 验证外部令牌不能调用仅限浏览器会话的令牌管理入口。
func TestRequireSessionRejectsAPIKey(t *testing.T) {
	// request 是带有已认证令牌身份和兼容用户身份的请求。
	request := httptest.NewRequest(http.MethodGet, "/api/v1/access-tokens", nil)
	// identity 是测试令牌的非秘密权限快照。
	identity := APIKeyIdentity{ID: "key", Prefix: "ydk_live_12345678", Scopes: []string{apikey.ScopeItemsRead}}
	// session 是为既有业务 handler 注入的最小用户身份。
	session := &db.Session{UserID: 1, Username: "api-user"}
	// handler 是只允许浏览器会话访问的测试端点。
	handler := RequireSession(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	// recorder 捕获 RequireSession 的拒绝响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request.WithContext(WithAPIKey(WithSession(request.Context(), session), identity)))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("API key must not satisfy browser session requirement, status=%d", recorder.Code)
	}
}
