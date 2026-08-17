package server

import "testing"

// TestServerCookieWriterRejectsMissingRepository 验证 Server 缺少窄凭证端口时快速失败。
func TestServerCookieWriterRejectsMissingRepository(t *testing.T) {
	// srv 模拟尚未完成账号应用服务装配的 Server。
	srv := &Server{applications: &applicationServices{accountLogin: &accountLoginService{}}}
	// sessionPort 保存缺少凭证仓储时的会话写回端口。
	sessionPort := srv.platformCredentialSessionPort()
	if sessionPort != nil {
		t.Fatal("缺少凭证 Port 时 Server 不应暴露伪造的会话端口")
	}
}
