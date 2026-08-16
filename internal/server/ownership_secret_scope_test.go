package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

// TestRequireCookieOwnerSkipsLoginSecret 验证账号所有权校验不依赖登录密码密文。
func TestRequireCookieOwnerSkipsLoginSecret(t *testing.T) {
	// srv、store 和 cleanup 分别保存测试服务器、数据库和资源清理函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 是本测试共用的数据库上下文。
	ctx := context.Background()
	// _, updateErr 表示将登录密码替换为损坏密文的数据库操作结果；所有权摘要不应读取该字段。
	if _, updateErr := store.DB.ExecContext(ctx,
		`UPDATE cookies SET password=? WHERE id=?`, "not-a-password-ciphertext", "acc1"); updateErr != nil {
		t.Fatalf("损坏登录密码密文失败: %v", updateErr)
	}
	// request 是携带默认测试用户会话的所有权请求。
	request := httptest.NewRequest(http.MethodGet, "/cookies/acc1", nil)
	request = request.WithContext(auth.WithSession(request.Context(), &db.Session{UserID: 1}))
	// recorder 捕获所有权辅助函数可能写入的 HTTP 错误响应。
	recorder := httptest.NewRecorder()
	// detail、owned 表示所有权校验返回的非敏感应用摘要及校验结果。
	detail, owned := srv.requireCookieOwner(recorder, request, "acc1")
	if !owned || detail.ID != "acc1" || detail.UserID != 1 {
		t.Fatalf("detail=%+v owned=%v status=%d body=%s", detail, owned, recorder.Code, recorder.Body.String())
	}
}
