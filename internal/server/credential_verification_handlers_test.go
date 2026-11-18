package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	accountapp "xianyu-go/internal/application/account"
)

// credentialVerificationURL 是伪造的验证地址；使用非 /iv/ 路径，使后台验证任务不发起任何网络请求。
const credentialVerificationURL = "https://passport.example.invalid/other/page.htm?token=fixture-secret"

// TestCredentialVerificationHandlersContract 通过真实路由覆盖续期安全验证通道：
// 未登录、越权、无登记（409 回退）、创建会话并沿用扫码状态接口、重复启动被拒绝、运行状态的 verification_pending 标记；t 管理断言。
func TestCredentialVerificationHandlersContract(t *testing.T) {
	// srv、store、cleanup 是被测 HTTP 服务、测试数据库聚合和资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是被测服务的完整路由树；session 是管理员登录后的认证 Cookie。
	handler := srv.Router()
	// session 保存 loginHelper 的结果，供后续分支和断言使用。
	session := loginHelper(t, handler)
	// ctx 是本用例共用的请求上下文。
	ctx := context.Background()
	// registry 是测试 Server 与组合根共享的待验证登记表。
	registry := testVerificationRegistry(srv)
	if registry == nil {
		t.Fatal("测试 Server 未登记待验证登记表")
	}
	// err 是 store.DB.ExecContext 返回的失败原因，用于断言成功或被拒绝的路径。
	if _, err := store.DB.ExecContext(ctx, `INSERT OR IGNORE INTO cookies (id,value,user_id) VALUES ('acc1','unb=fixture; _m_h5_tk=a_1',1)`); err != nil {
		t.Fatal(err)
	}
	// 另一个用户的账号用于越权断言。
	if _, err := store.Users.Create(ctx, "verify-other", "verify-other@example.com", "pw"); err != nil {
		t.Fatal(err)
	}
	// err 是 store.Users.GetByUsername 返回的失败原因，用于断言成功或被拒绝的路径；otherUser 保存 store.Users.GetByUsername 的结果，供后续分支和断言使用。
	otherUser, err := store.Users.GetByUsername(ctx, "verify-other")
	if err != nil {
		t.Fatal(err)
	}
	// err 是 store.DB.ExecContext 返回的失败原因，用于断言成功或被拒绝的路径。
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO cookies (id,value,user_id) VALUES ('other','cookie-value',?)`, otherUser.ID); err != nil {
		t.Fatal(err)
	}

	// 未登录：被认证中间件拒绝。
	anonymous := httptest.NewRecorder()
	handler.ServeHTTP(anonymous, httptest.NewRequest(http.MethodPost, "/api/v1/accounts/acc1/credential-verification", nil))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应返回 401: %d", anonymous.Code)
	}

	// 越权：他人的账号不可见，登记保持不变。
	registry.Register("other", credentialVerificationURL)
	// foreign 保存 greetingRequest 的结果，供后续分支和断言使用。
	foreign := greetingRequest(t, handler, session, http.MethodPost, "/api/v1/accounts/other/credential-verification", "")
	if foreign.Code != http.StatusNotFound && foreign.Code != http.StatusForbidden {
		t.Fatalf("他人账号应被拒绝: %d %s", foreign.Code, foreign.Body.String())
	}
	if !registry.Pending("other") {
		t.Fatal("越权请求不得消费他人的登记")
	}

	// 没有登记：409 且错误码为 verification_expired，提示前端回退到重新扫码。
	none := greetingRequest(t, handler, session, http.MethodPost, "/api/v1/accounts/acc1/credential-verification", "")
	if none.Code != http.StatusConflict || !strings.Contains(none.Body.String(), "verification_expired") {
		t.Fatalf("无登记应返回 409 verification_expired: %d %s", none.Code, none.Body.String())
	}

	// 运行状态：登记前没有标记，登记后带 verification_pending。
	if strings.Contains(runtimeStatusBody(t, handler, session), "verification_pending") {
		t.Fatal("没有登记时不应带 verification_pending")
	}
	registry.Register("acc1", credentialVerificationURL)
	// body 保存 runtimeStatusBody 的结果，供后续分支和断言使用。
	if body := runtimeStatusBody(t, handler, session); !strings.Contains(body, `"verification_pending":true`) {
		t.Fatalf("登记后运行状态应带 verification_pending: %s", body)
	}

	// 成功：创建会话；响应只含会话标识，不含验证地址或令牌。
	started := greetingRequest(t, handler, session, http.MethodPost, "/api/v1/accounts/acc1/credential-verification", "")
	// response 是解析后的启动响应。
	var response credentialVerificationResponse
	// err 是 json.Unmarshal 返回的失败原因，用于断言成功或被拒绝的路径。
	if err := json.Unmarshal(started.Body.Bytes(), &response); err != nil || started.Code != http.StatusOK || !response.Success || response.SessionID == "" {
		t.Fatalf("启动 status=%d body=%s err=%v", started.Code, started.Body.String(), err)
	}
	if strings.Contains(started.Body.String(), "fixture-secret") || strings.Contains(started.Body.String(), "passport.example.invalid") {
		t.Fatalf("响应不得含验证地址: %s", started.Body.String())
	}

	// 会话沿用扫码状态接口：所有权已登记，状态为 verification_required。
	status := greetingRequest(t, handler, session, http.MethodGet, "/api/v1/qr-login/check/"+response.SessionID, "")
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), "verification_required") {
		t.Fatalf("验证会话应可经扫码状态接口读取: %d %s", status.Code, status.Body.String())
	}
	if strings.Contains(status.Body.String(), "fixture-secret") {
		t.Fatalf("状态响应不得含验证地址令牌: %s", status.Body.String())
	}

	// 登记已被一次性消费：再次启动返回 409，运行状态标记消失。
	again := greetingRequest(t, handler, session, http.MethodPost, "/api/v1/accounts/acc1/credential-verification", "")
	if again.Code != http.StatusConflict {
		t.Fatalf("重复启动应返回 409: %d %s", again.Code, again.Body.String())
	}
	if strings.Contains(runtimeStatusBody(t, handler, session), "verification_pending") {
		t.Fatal("会话创建后不应再带 verification_pending")
	}
}

// TestCredentialVerificationErrorMapping 覆盖应用错误到 HTTP 状态的映射，包含平台创建会话失败；t 管理断言。
func TestCredentialVerificationErrorMapping(t *testing.T) {
	// srv 仅用于调用错误映射方法；cleanup 释放测试资源。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// cases 列出应用层错误及期望的 HTTP 状态与错误码片段。
	cases := []struct {
		// name 是子用例名称。
		name string
		// err 是应用层返回的错误。
		err error
		// status 是期望的 HTTP 状态码。
		status int
		// contains 是响应体中应出现的片段。
		contains string
	}{
		{"未登记", accountapp.ErrVerificationNotPending, http.StatusConflict, "verification_expired"},
		{"越权", accountapp.ErrForbidden, http.StatusNotFound, "账号不存在"},
		{"账号不存在", accountapp.ErrNotFound, http.StatusNotFound, "账号不存在"},
		{"凭证不存在", accountapp.ErrCredentialNotFound, http.StatusNotFound, "账号不存在"},
		{"平台失败", context.DeadlineExceeded, http.StatusBadGateway, "credential_verification_failed"},
	}
	// sample 是当前被验证的错误映射样本。
	for _, sample := range cases {
		t.Run(sample.name, func(t *testing.T) {
			// recorder 保存 httptest.NewRecorder 的结果，供后续分支和断言使用。
			recorder := httptest.NewRecorder()
			srv.writeCredentialVerificationError(recorder, sample.err)
			if recorder.Code != sample.status || !strings.Contains(recorder.Body.String(), sample.contains) {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

// runtimeStatusBody 请求账号运行状态接口并返回响应正文。
func runtimeStatusBody(t *testing.T, handler http.Handler, session *http.Cookie) string {
	t.Helper()
	// response 是运行状态接口的响应；成功响应会同时记录 OpenAPI 契约断言。
	response := greetingRequest(t, handler, session, http.MethodGet, "/api/v1/accounts/runtime-status", "")
	if response.Code != http.StatusOK {
		t.Fatalf("运行状态应返回 200: %d %s", response.Code, response.Body.String())
	}
	return response.Body.String()
}
