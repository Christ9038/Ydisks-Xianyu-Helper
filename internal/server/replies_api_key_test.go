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

// TestRepliesAPIKeyCanManageExistingAIKeywordAndAgentSettings 验证 replies:manage 令牌复用现有 AI、关键词与 Agent 回复用例。
func TestRepliesAPIKeyCanManageExistingAIKeywordAndAgentSettings(t *testing.T) {
	// srv、store、cleanup 是隔离数据库和完整 HTTP 服务。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 是测试账号和令牌数据库操作共用的上下文。
	ctx := context.Background()
	// owner 是当前外部 API 令牌所属用户。
	owner, err := store.Users.GetAdmin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// err 表示为本次测试准备账号失败；重复插入时保留既有夹具。
	if _, err := store.DB.ExecContext(ctx, `INSERT OR IGNORE INTO cookies (id,value,user_id) VALUES ('acc1','cookie-value',?)`, owner.ID); err != nil {
		t.Fatal(err)
	}
	// token 是只授予现有回复配置管理权限的测试 Bearer 凭证。
	token := apikey.TokenPrefix + "replies-management-api-test-token"
	// now 是令牌签发时间。
	now := time.Now().Unix()
	// record 是只授予 replies:manage 的令牌记录。
	record := db.APIAccessToken{ID: "replies-management-api-key", UserID: owner.ID, Name: "replies management API test", TokenHash: apikey.HashToken(token), Prefix: token[:len(apikey.TokenPrefix)+8], Scopes: []string{apikey.ScopeRepliesManage}, CreatedAt: now, ExpiresAt: now + 3600}
	// err 是将回复规则专用令牌摘要写入隔离数据库时的错误。
	if err := store.APIAccessTokens.Create(ctx, record); err != nil {
		t.Fatal(err)
	}
	// handler 是包含真实 Bearer 认证、scope 白名单和回复业务路由的完整 Router。
	handler := srv.Router()
	// doBearer 是通过当前 Bearer 凭证调用现有版本化回复 API 的测试请求器。
	doBearer := func(method, path, body string) *httptest.ResponseRecorder {
		// request 是当前回复配置请求。
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+token)
		// recorder 捕获当前业务 Handler 的 HTTP 响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}

	// AI 配置写入沿用现有账号级设置 Owner，不通过 API Token 暴露系统级模型密钥。
	aiUpdate := doBearer(http.MethodPut, "/api/v1/settings/ai-reply/acc1", `{"ai_enabled":true,"auto_adjust_price_enabled":true,"max_discount_percent":12,"max_discount_amount":88,"max_bargain_rounds":4,"custom_prompts":"API token contract"}`)
	if aiUpdate.Code != http.StatusOK || !strings.Contains(aiUpdate.Body.String(), `"success":true`) {
		t.Fatalf("AI reply update via Bearer failed: status=%d body=%s", aiUpdate.Code, aiUpdate.Body.String())
	}
	// aiRead 验证现有 AI 配置读取接口回读刚才写入的状态。
	aiRead := doBearer(http.MethodGet, "/api/v1/settings/ai-reply/acc1", "")
	if aiRead.Code != http.StatusOK || !strings.Contains(aiRead.Body.String(), `"max_discount_percent":12`) || !strings.Contains(aiRead.Body.String(), "API token contract") {
		t.Fatalf("AI reply readback via Bearer failed: status=%d body=%s", aiRead.Code, aiRead.Body.String())
	}

	// keywordCreate 验证现有关键词回复创建接口接受 replies:manage，而不是新建第二套回复规则。
	keywordCreate := doBearer(http.MethodPost, "/api/v1/reply-rules/acc1", `{"keyword":"api-contract-keyword","match_type":"contains","reply":"API contract reply"}`)
	if keywordCreate.Code != http.StatusOK || !strings.Contains(keywordCreate.Body.String(), `"success":true`) {
		t.Fatalf("keyword reply create via Bearer failed: status=%d body=%s", keywordCreate.Code, keywordCreate.Body.String())
	}
	// keywordRead 验证现有关键词列表可以读回外部 API 创建的规则。
	keywordRead := doBearer(http.MethodGet, "/api/v1/reply-rules/acc1", "")
	if keywordRead.Code != http.StatusOK || !strings.Contains(keywordRead.Body.String(), "api-contract-keyword") {
		t.Fatalf("keyword reply readback via Bearer failed: status=%d body=%s", keywordRead.Code, keywordRead.Body.String())
	}

	// agentUpdate 配置现有 Agent 回复入口；不触发外部 Agent 网络请求，也不写入 Agent Token。
	agentUpdate := doBearer(http.MethodPut, "/api/v1/agent-replies/acc1", `{"enabled":true,"endpoint_url":"https://agent.example.com/v1","request_timeout_seconds":15,"max_session_seconds":120,"order_context_enabled":true}`)
	if agentUpdate.Code != http.StatusOK || !strings.Contains(agentUpdate.Body.String(), `"enabled":true`) {
		t.Fatalf("Agent reply update via Bearer failed: status=%d body=%s", agentUpdate.Code, agentUpdate.Body.String())
	}
	// agentRead 验证现有 Agent 配置回读，且响应不包含任何访问令牌明文。
	agentRead := doBearer(http.MethodGet, "/api/v1/agent-replies/acc1", "")
	if agentRead.Code != http.StatusOK || !strings.Contains(agentRead.Body.String(), `"endpoint_url":"https://agent.example.com/v1"`) || strings.Contains(agentRead.Body.String(), "token_value") {
		t.Fatalf("Agent reply readback via Bearer failed or leaked a token: status=%d body=%s", agentRead.Code, agentRead.Body.String())
	}
}
