package server

import (
	"context"
	"testing"
)

// TestServerPlatformRuntimeDetailSkipsLoginSecrets 验证 Server 平台适配模型不解密登录密码。
func TestServerPlatformRuntimeDetailSkipsLoginSecrets(t *testing.T) {
	// srv、store 和 cleanup 分别是测试服务器、数据库和资源清理函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 是平台适配模型测试共用的上下文。
	ctx := context.Background()
	// corruptErr 表示写入故意损坏的登录密码密文失败的原因。
	if _, corruptErr := store.DB.ExecContext(ctx,
		`UPDATE cookies SET password=? WHERE id=?`, "not-a-password-ciphertext", "acc1"); corruptErr != nil {
		t.Fatalf("corrupt password: %v", corruptErr)
	}
	// platformDetail 是商品、订单和登录平台调用共享的浅层凭证适配模型。
	platformDetail, platformErr := srv.loadCookiePlatformDetail(ctx, "acc1")
	if platformErr != nil || platformDetail == nil || platformDetail.ID != "acc1" || platformDetail.Value == "" {
		t.Fatalf("platform detail=%+v err=%v", platformDetail, platformErr)
	}
	// ownerID 是测试账号的实际所有者 ID，避免依赖固定用户编号。
	ownerID, ownerErr := store.Cookies.GetOwnerID(ctx, "acc1")
	if ownerErr != nil {
		t.Fatalf("get owner: %v", ownerErr)
	}
	// summaryDetail 是 ownership 和资料刷新使用的非敏感摘要适配模型。
	summaryDetail, summaryErr := srv.loadCookieSummaryDetail(ctx, ownerID, "acc1")
	if summaryErr != nil || summaryDetail == nil || summaryDetail.ID != "acc1" || summaryDetail.UserID != ownerID {
		t.Fatalf("summary detail=%+v err=%v", summaryDetail, summaryErr)
	}
}
