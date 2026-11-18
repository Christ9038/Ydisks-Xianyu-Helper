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

// TestChatManualTakeoverBearerScopeUsesNarrowSettingsOnly 验证 chat:takeover 令牌只能修改人工介入字段，不能借通用账号设置接口改写凭据。
func TestChatManualTakeoverBearerScopeUsesNarrowSettingsOnly(t *testing.T) {
	// srv、store、cleanup 是当前测试使用的完整 HTTP 服务、隔离数据库和资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是包含真实认证与路由中间件的完整 HTTP 路由树。
	handler := srv.Router()
	// ctx 是本次令牌和账号数据操作共用的数据库上下文。
	ctx := context.Background()
	// owner 是签发测试 Bearer 令牌的本地管理员用户。
	owner, err := store.Users.GetAdmin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// originalCookie 是测试账号当前持久化的闲鱼 Cookie；下方请求不得改变它。
	originalCookie, err := store.Cookies.GetValue(ctx, "acc1")
	if err != nil {
		t.Fatal(err)
	}
	// now 是令牌签发和过期时间使用的 Unix 秒数。
	now := time.Now().Unix()
	// token 是仅用于本次测试的随机前缀模拟凭证。
	token := apikey.TokenPrefix + "manual-takeover-test-token-material"
	// record 是只保存摘要且仅授予人工介入配置权限的令牌记录。
	record := db.APIAccessToken{ID: "manual-takeover-key", UserID: owner.ID, Name: "manual takeover test", TokenHash: apikey.HashToken(token), Prefix: token[:len(apikey.TokenPrefix)+8], Scopes: []string{apikey.ScopeChatTakeover}, CreatedAt: now, ExpiresAt: now + 3600}
	// err 是将人工介入专用令牌摘要写入隔离数据库时的错误。
	if err := store.APIAccessTokens.Create(ctx, record); err != nil {
		t.Fatal(err)
	}
	// request 是仅包含人工介入配置，并故意附带通用账号设置字段的外部令牌请求。
	request := httptest.NewRequest(http.MethodPut, "/api/v1/chat/manual-takeover/acc1", strings.NewReader(`{"manual_takeover_minutes":15,"manual_takeover_scope":"all","cookie":"must-not-be-written","login_password":"must-not-be-written"}`))
	request.Header.Set("Authorization", "Bearer "+token)
	// recorder 捕获窄接口的 HTTP 响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"manual_takeover_minutes":15`) || !strings.Contains(recorder.Body.String(), `"manual_takeover_scope":"all"`) {
		t.Fatalf("manual takeover update failed: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	// requestForContract 是同一真实成功请求的契约校验副本；请求体已读完，重新构造以记录 OpenAPI operationId。
	requestForContract := httptest.NewRequest(http.MethodPut, "/api/v1/chat/manual-takeover/acc1", strings.NewReader(`{"manual_takeover_minutes":15,"manual_takeover_scope":"all"}`))
	requestForContract.Header.Set("Authorization", "Bearer "+token)
	assertOpenAPIExpectedStatusResponse(t, requestForContract, recorder, http.StatusOK)
	// saved 是数据库实际保存的人工介入配置，必须与响应一致。
	saved, err := store.Cookies.GetManualTakeoverSetting(ctx, "acc1")
	if err != nil || saved.Minutes != 15 || saved.Scope != db.ManualTakeoverScopeAll {
		t.Fatalf("manual takeover settings not persisted: saved=%+v err=%v", saved, err)
	}
	// cookieAfter 是请求完成后账号的 Cookie；窄接口不得读取或写入 Cookie 字段。
	cookieAfter, err := store.Cookies.GetValue(ctx, "acc1")
	if err != nil || cookieAfter != originalCookie {
		t.Fatalf("manual takeover endpoint changed account Cookie: before=%q after=%q err=%v", originalCookie, cookieAfter, err)
	}
	// forbiddenRequest 尝试使用同一令牌调用包含 Cookie/密码字段的通用账号设置接口。
	forbiddenRequest := httptest.NewRequest(http.MethodPut, "/api/v1/accounts/acc1/settings", strings.NewReader(`{"cookie":"must-not-be-written"}`))
	forbiddenRequest.Header.Set("Authorization", "Bearer "+token)
	// forbiddenRecorder 捕获越权请求结果。
	forbiddenRecorder := httptest.NewRecorder()
	handler.ServeHTTP(forbiddenRecorder, forbiddenRequest)
	if forbiddenRecorder.Code != http.StatusForbidden {
		t.Fatalf("chat:takeover token must not access general account settings: status=%d body=%s", forbiddenRecorder.Code, forbiddenRecorder.Body.String())
	}
}
