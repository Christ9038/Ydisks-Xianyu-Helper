package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAgentReplyHandlersContract 覆盖 Agent 回复配置读取、保存、令牌三态、连接测试和越权的真实契约，并断言令牌永不出现在响应中。
func TestAgentReplyHandlersContract(t *testing.T) {
	// srv、store、cleanup 是被测 HTTP 服务、测试数据库聚合和资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是被测服务的完整路由树。
	handler := srv.Router()
	// session 是管理员登录后的认证会话 Cookie；ctx 是共用的请求上下文。
	session := loginHelper(t, handler)
	// ctx 是本用例共用的请求上下文。
	ctx := context.Background()
	// err 保存插入测试账号的错误。
	if _, err := store.DB.ExecContext(ctx, `INSERT OR IGNORE INTO cookies (id,value,user_id) VALUES ('acc1','cookie-value',1)`); err != nil {
		t.Fatal(err)
	}
	// 其他用户的账号用于越权断言。
	if _, err := store.Users.Create(ctx, "agent-other", "agent-other@example.com", "pw"); err != nil {
		t.Fatal(err)
	}
	// otherUser、err 是另一个本地用户及其查询错误，持有越权断言使用的账号。
	otherUser, err := store.Users.GetByUsername(ctx, "agent-other")
	if err != nil {
		t.Fatal(err)
	}
	// err 保存插入他人账号的错误。
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO cookies (id,value,user_id) VALUES ('other','cookie-value',?)`, otherUser.ID); err != nil {
		t.Fatal(err)
	}

	// 未保存时返回默认关闭值。
	got := greetingRequest(t, handler, session, http.MethodGet, "/api/v1/agent-replies/acc1", "")
	if got.Code != http.StatusOK || got.Header().Get("Cache-Control") == "" ||
		!strings.Contains(got.Body.String(), `"enabled":false`) || !strings.Contains(got.Body.String(), `"request_timeout_seconds":15`) {
		t.Fatalf("默认配置 status=%d body=%s", got.Code, got.Body.String())
	}

	// 保存并替换令牌；响应只含 token_configured。
	saved := greetingRequest(t, handler, session, http.MethodPut, "/api/v1/agent-replies/acc1",
		`{"enabled":true,"endpoint_url":" https://agent.example.com/v1 ","token":{"action":"replace","value":"super-secret-token"}}`)
	// settings 保存响应中的配置摘要。
	var settings agentReplySettingsResponse
	// err 保存解析保存响应的错误。
	if err := json.Unmarshal(saved.Body.Bytes(), &settings); err != nil || saved.Code != http.StatusOK || !settings.Enabled ||
		settings.EndpointURL != "https://agent.example.com/v1" || !settings.TokenConfigured || settings.MaxSessionSeconds != 120 {
		t.Fatalf("保存 status=%d body=%s err=%v", saved.Code, saved.Body.String(), err)
	}
	if strings.Contains(saved.Body.String(), "super-secret-token") {
		t.Fatal("响应泄漏了令牌")
	}
	// 重新读取同样不含令牌。
	if again := greetingRequest(t, handler, session, http.MethodGet, "/api/v1/agent-replies/acc1", ""); strings.Contains(again.Body.String(), "super-secret-token") || !strings.Contains(again.Body.String(), `"token_configured":true`) {
		t.Fatalf("读取响应异常: %s", again.Body.String())
	}

	// 连接测试：本地 Agent 校验 Bearer 令牌（retain 使用已保存令牌）。
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer super-secret-token" {
			http.Error(w, "denied", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"handled":true,"working":false,"replies":[{"text":"pong"}]}`))
	}))
	defer agent.Close()
	// 令牌只对保存时的地址来源有效：沿用已保存令牌测试其他来源必须被拒绝，且不会向该地址发出任何请求。
	if foreign := greetingRequest(t, handler, session, http.MethodPost, "/api/v1/agent-replies/acc1/test", `{"endpoint_url":"`+agent.URL+`"}`); foreign.Code != http.StatusBadRequest ||
		!strings.Contains(foreign.Body.String(), "重新输入访问令牌") {
		t.Fatalf("更换来源后沿用令牌应被拒绝 status=%d body=%s", foreign.Code, foreign.Body.String())
	}
	// 同样地，保存时更换来源且沿用令牌也必须被拒绝，已保存的配置保持不变。
	if foreign := greetingRequest(t, handler, session, http.MethodPut, "/api/v1/agent-replies/acc1", `{"enabled":true,"endpoint_url":"`+agent.URL+`"}`); foreign.Code != http.StatusBadRequest {
		t.Fatalf("更换来源后保存应被拒绝 status=%d body=%s", foreign.Code, foreign.Body.String())
	}
	// 重新输入令牌并绑定到本地 Agent 后，后续 retain 测试才允许使用该令牌。
	if rebound := greetingRequest(t, handler, session, http.MethodPut, "/api/v1/agent-replies/acc1",
		`{"enabled":true,"endpoint_url":"`+agent.URL+`","token":{"action":"replace","value":"super-secret-token"}}`); rebound.Code != http.StatusOK {
		t.Fatalf("重新输入令牌后应允许保存 status=%d body=%s", rebound.Code, rebound.Body.String())
	}
	// tested 保存带令牌连接测试的响应。
	tested := greetingRequest(t, handler, session, http.MethodPost, "/api/v1/agent-replies/acc1/test", `{"endpoint_url":"`+agent.URL+`"}`)
	if tested.Code != http.StatusOK || !strings.Contains(tested.Body.String(), `"status":"ok"`) || !strings.Contains(tested.Body.String(), `"reply_count":1`) {
		t.Fatalf("测试连接 status=%d body=%s", tested.Code, tested.Body.String())
	}
	// 清除令牌后同一 Agent 返回 401，归类为 http_error。
	denied := greetingRequest(t, handler, session, http.MethodPost, "/api/v1/agent-replies/acc1/test", `{"endpoint_url":"`+agent.URL+`","token":{"action":"clear"}}`)
	if denied.Code != http.StatusOK || !strings.Contains(denied.Body.String(), `"status":"http_error"`) || !strings.Contains(denied.Body.String(), `"status_code":401`) {
		t.Fatalf("http_error status=%d body=%s", denied.Code, denied.Body.String())
	}
	// 不可达地址归类为 unreachable，且响应不回显地址。
	closed := httptest.NewServer(http.NotFoundHandler())
	// closedURL 是已关闭测试服务的地址，用来构造不可达场景。
	closedURL := closed.URL
	closed.Close()
	// unreachable 保存不可达地址测试的响应。
	unreachable := greetingRequest(t, handler, session, http.MethodPost, "/api/v1/agent-replies/acc1/test", `{"endpoint_url":"`+closedURL+`","token":{"action":"clear"}}`)
	if !strings.Contains(unreachable.Body.String(), `"status":"unreachable"`) || strings.Contains(unreachable.Body.String(), closedURL) {
		t.Fatalf("unreachable body=%s", unreachable.Body.String())
	}
	// 非法响应归类为 invalid_response。
	garbage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("not json")) }))
	defer garbage.Close()
	// invalid 保存 Agent 返回非 JSON 时测试的响应。
	if invalid := greetingRequest(t, handler, session, http.MethodPost, "/api/v1/agent-replies/acc1/test", `{"endpoint_url":"`+garbage.URL+`","token":{"action":"clear"}}`); !strings.Contains(invalid.Body.String(), `"status":"invalid_response"`) {
		t.Fatalf("invalid_response body=%s", invalid.Body.String())
	}

	// retain 保留令牌并关闭；clear 后令牌标记消失。
	if got := greetingRequest(t, handler, session, http.MethodPut, "/api/v1/agent-replies/acc1", `{"enabled":false,"endpoint_url":"`+agent.URL+`"}`); !strings.Contains(got.Body.String(), `"token_configured":true`) {
		t.Fatalf("retain 应保留令牌: %s", got.Body.String())
	}
	// got 保存 retain 命令保存配置后的响应。
	if got := greetingRequest(t, handler, session, http.MethodPut, "/api/v1/agent-replies/acc1", `{"enabled":false,"endpoint_url":"","token":{"action":"clear"}}`); !strings.Contains(got.Body.String(), `"token_configured":false`) {
		t.Fatalf("clear 应移除令牌: %s", got.Body.String())
	}

	// 参数错误：启用无地址、协议非法、超时越界、令牌命令非法、坏 JSON。
	for name, body := range map[string]string{
		"enable-no-url": `{"enabled":true,"endpoint_url":""}`,
		"bad-scheme":    `{"enabled":true,"endpoint_url":"ftp://agent.example.com"}`,
		"bad-timeout":   `{"enabled":false,"endpoint_url":"","request_timeout_seconds":99}`,
		"bad-action":    `{"enabled":false,"endpoint_url":"","token":{"action":"x"}}`,
		"bad-json":      `not-json`,
	} {
		// got 保存当前非法参数场景的响应，期望 400。
		if got := greetingRequest(t, handler, session, http.MethodPut, "/api/v1/agent-replies/acc1", body); got.Code != http.StatusBadRequest {
			t.Errorf("%s status=%d body=%s", name, got.Code, got.Body.String())
		}
	}
	// got 保存缺少地址的连接测试响应，期望 400。
	if got := greetingRequest(t, handler, session, http.MethodPost, "/api/v1/agent-replies/acc1/test", `{"endpoint_url":""}`); got.Code != http.StatusBadRequest {
		t.Errorf("测试缺地址 status=%d", got.Code)
	}
	// got 保存请求体为坏 JSON 的连接测试响应，期望 400。
	if got := greetingRequest(t, handler, session, http.MethodPost, "/api/v1/agent-replies/acc1/test", `not-json`); got.Code != http.StatusBadRequest {
		t.Errorf("测试坏 JSON status=%d", got.Code)
	}

	// 越权与不存在：三个操作都不得触达。
	for _, call := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/agent-replies/other", ""},
		{http.MethodPut, "/api/v1/agent-replies/other", `{"enabled":false,"endpoint_url":""}`},
		{http.MethodPost, "/api/v1/agent-replies/other/test", `{"endpoint_url":"` + agent.URL + `"}`},
	} {
		// got 保存对他人账号发起请求的响应，期望 403。
		if got := greetingRequest(t, handler, session, call.method, call.path, call.body); got.Code != http.StatusForbidden {
			t.Errorf("%s %s 越权 status=%d", call.method, call.path, got.Code)
		}
	}
	// got 保存读取不存在账号的响应，期望 404。
	if got := greetingRequest(t, handler, session, http.MethodGet, "/api/v1/agent-replies/missing", ""); got.Code != http.StatusNotFound {
		t.Errorf("不存在账号 status=%d", got.Code)
	}
}
