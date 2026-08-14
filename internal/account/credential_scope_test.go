package account

import (
	"context"
	"testing"
)

// TestRestartUsesCookieValueQuery 验证账号重启只读取 Cookie 明文，不解密登录密码。
func TestRestartUsesCookieValueQuery(t *testing.T) {
	// mgr、store 和 cleanup 分别是测试用账号管理器、数据库及资源清理函数。
	mgr, store, cleanup := newManagerWithAccount(t, "restart-scope", "unb=1; _m_h5_tk=old;")
	defer cleanup()
	// ctx 是账号重启共用的上下文。
	ctx := context.Background()
	// admin 是测试账号所属的管理员用户。
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// saveErr 表示写入重启用新 Cookie 失败的原因。
	saveErr := store.Cookies.Save(ctx, "restart-scope", "unb=1; _m_h5_tk=new;", admin.ID)
	if saveErr != nil {
		t.Fatalf("save refreshed cookie: %v", saveErr)
	}
	// corruptErr 表示写入故意损坏的登录密码密文失败的原因。
	if _, corruptErr := store.DB.ExecContext(ctx,
		`UPDATE cookies SET username=?,password=? WHERE id=?`,
		"restart-scope-user", "not-a-password-ciphertext", "restart-scope"); corruptErr != nil {
		t.Fatalf("corrupt password: %v", corruptErr)
	}

	// restartErr 表示读取窄 Cookie 并重启账号的结果。
	restartErr := mgr.Restart(ctx, "restart-scope")
	if restartErr != nil {
		t.Fatalf("restart with corrupt login secret: %v", restartErr)
	}
	// managed 是重启后新建的运行实例，用于核对实际使用的 Cookie。
	managed, ok := mgr.getInstanceInternal("restart-scope")
	if !ok || managed == nil {
		t.Fatal("Restart 后应存在新实例")
	}
	// got 是重启实例实际采用的 Cookie 明文。
	got := managed.acc.CurrentCookieStr()
	if got != "unb=1; _m_h5_tk=new;" {
		t.Fatalf("重启实例 Cookie=%q want new Cookie", got)
	}
	mgr.Stop("restart-scope")
}
