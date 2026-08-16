package server

import (
	"context"
	"testing"
)

// TestServerCookieWriterRejectsMissingRepository 验证 Server 凭证写入适配器缺少 Port 时快速失败。
func TestServerCookieWriterRejectsMissingRepository(t *testing.T) {
	// writer 模拟请求边界持有明文 Cookie 但尚未完成凭证 Port 装配的状态。
	writer := serverCookieWriter{cookies: "sid=short-lived"}
	// createErr 保存新增账号写入的装配错误。
	createErr := writer.CreateOwnedCookie(context.Background(), "cid", 1)
	if createErr == nil {
		t.Fatal("缺少凭证 Port 时新增 Cookie 不应伪装成功")
	}
	// updateErr 保存既有账号更新的装配错误。
	updateErr := writer.UpdateOwnedCookie(context.Background(), "cid", 1, 0)
	if updateErr == nil {
		t.Fatal("缺少凭证 Port 时更新 Cookie 不应伪装成功")
	}
}
