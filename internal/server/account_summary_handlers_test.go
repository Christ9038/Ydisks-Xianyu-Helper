package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

// TestRequireCookieOwnershipPreservesHTTPCompatibility 验证账号所有权辅助函数保留未登录、越权、缺失和成功状态码。
func TestRequireCookieOwnershipPreservesHTTPCompatibility(t *testing.T) {
	// srv、store、cleanup 保存当前测试使用的 HTTP 服务和数据库。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 是本测试共用的数据库上下文。
	ctx := context.Background()
	// _, createErr 保存第二个用户创建错误；用户主键随后从查询得到。
	_, createErr := store.Users.Create(ctx, "summary-other", "summary-other@example.com", "pw")
	if createErr != nil {
		t.Fatalf("创建第二用户失败: %v", createErr)
	}
	// other、otherLookupErr 保存第二个用户及读取错误。
	other, otherLookupErr := store.Users.GetByUsername(ctx, "summary-other")
	if otherLookupErr != nil {
		t.Fatalf("读取第二用户失败: %v", otherLookupErr)
	}
	// saveErr 保存第二个用户账号的创建错误。
	if saveErr := store.Cookies.Save(ctx, "other-account", "unb=other", other.ID); saveErr != nil {
		t.Fatalf("创建第二用户账号失败: %v", saveErr)
	}
	// cases 覆盖所有权辅助函数的 HTTP 兼容分支。
	cases := []struct {
		// name 是当前 HTTP 所有权场景名称。
		name string
		// session 是请求上下文中的认证会话；nil 表示未登录。
		session *db.Session
		// accountID 是待校验账号标识。
		accountID string
		// wantStatus 是预期错误响应状态；成功场景为 0。
		wantStatus int
		// wantOwned 表示预期的所有权结论。
		wantOwned bool
	}{
		{name: "unauthenticated", accountID: "acc1", wantStatus: http.StatusUnauthorized},
		{name: "owned", session: &db.Session{UserID: 1}, accountID: "acc1", wantOwned: true},
		{name: "forbidden", session: &db.Session{UserID: 1}, accountID: "other-account", wantStatus: http.StatusForbidden},
		{name: "missing", session: &db.Session{UserID: 1}, accountID: "missing-account", wantStatus: http.StatusNotFound},
	}
	// testCase 表示当前正在验证的 HTTP 所有权场景。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// request 是注入测试会话后的所有权请求。
			request := httptest.NewRequest(http.MethodGet, "/items", nil)
			if testCase.session != nil {
				request = request.WithContext(auth.WithSession(request.Context(), testCase.session))
			}
			// recorder 捕获所有权辅助函数的 HTTP 错误响应。
			recorder := httptest.NewRecorder()
			// owned 保存应用 Port 返回的所有权结论。
			owned := srv.requireCookieOwnership(recorder, request, testCase.accountID)
			if owned != testCase.wantOwned {
				t.Fatalf("owned=%v，期望=%v", owned, testCase.wantOwned)
			}
			if testCase.wantStatus != 0 && recorder.Code != testCase.wantStatus {
				t.Fatalf("status=%d，期望=%d body=%s", recorder.Code, testCase.wantStatus, recorder.Body.String())
			}
		})
	}
}

// TestLoadCookieSummaryDetailUsesNonSensitiveApplicationModel 验证 Server 摘要兼容模型不携带 Cookie 或密码。
func TestLoadCookieSummaryDetailUsesNonSensitiveApplicationModel(t *testing.T) {
	// srv、cleanup 保存当前测试使用的 HTTP 服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// detail、loadErr 保存应用 Port 返回的非敏感摘要及读取错误。
	detail, loadErr := srv.loadCookieSummaryDetail(context.Background(), 1, "acc1")
	if loadErr != nil {
		t.Fatalf("读取账号摘要失败: %v", loadErr)
	}
	if detail.ID != "acc1" || detail.UserID != 1 {
		t.Fatalf("摘要模型不完整: %+v", detail)
	}
}
