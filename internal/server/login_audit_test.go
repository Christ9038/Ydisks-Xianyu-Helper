package server

import (
	"context"
	"testing"
)

// TestMarkSuccessfulLoginUsesApplicationAuditService 验证 Server 仅调用应用审计服务并保持登录成功持久化语义。
func TestMarkSuccessfulLoginUsesApplicationAuditService(t *testing.T) {
	// srv、store 和 cleanup 保存带固定管理员账号的 HTTP 测试服务及资源清理函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// admin 保存触发登录审计的非敏感管理员身份。
	admin, adminErr := store.Users.GetByUsername(context.Background(), "admin")
	if adminErr != nil {
		t.Fatal(adminErr)
	}
	// srv.markSuccessfulLogin 执行账号密码登录别名的成功审计。
	srv.markSuccessfulLogin(context.Background(), "acc1", admin.ID, "password_login", "账号登录成功")
	// summary 保存应用服务写入后的账号非敏感摘要。
	summary, summaryErr := store.Cookies.GetSummaryOwned(context.Background(), admin.ID, "acc1")
	if summaryErr != nil || summary.LoginMethod != loginMethodPassword || summary.LastLoginAt == 0 {
		t.Fatalf("登录方式未由应用审计服务保存 summary=%+v err=%v", summary, summaryErr)
	}
	// logs 保存应用审计服务写入的数据库记录。
	logs, logsErr := store.LoginLogs.ListByCookie(context.Background(), "acc1", 10)
	if logsErr != nil || len(logs) != 1 || logs[0].Method != loginMethodPassword || logs[0].Status != "success" || logs[0].TriggerReason != "账号密码登录" {
		t.Fatalf("登录审计记录异常 logs=%+v err=%v", logs, logsErr)
	}
}

// TestMarkSuccessfulLoginSkipsEmptyMethod 验证 Server 对缺少登录方式的兼容空操作不创建审计记录。
func TestMarkSuccessfulLoginSkipsEmptyMethod(t *testing.T) {
	// srv、store 和 cleanup 保存 HTTP 测试服务及资源清理函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// srv.markSuccessfulLogin 执行缺少方式的兼容调用。
	srv.markSuccessfulLogin(context.Background(), "acc1", 1, "", "不应记录")
	// logs 保存兼容调用后的数据库审计记录。
	logs, logsErr := store.LoginLogs.ListByCookie(context.Background(), "acc1", 10)
	if logsErr != nil {
		t.Fatal(logsErr)
	}
	if len(logs) != 0 {
		t.Fatalf("空登录方式不应写入审计记录: %+v", logs)
	}
}
