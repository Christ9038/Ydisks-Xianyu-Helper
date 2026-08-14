package db

import (
	"context"
	"testing"
	"time"
)

// TestGetCookiePlatformRuntimeDataExcludesLoginSecrets 验证平台运行视图不解密登录密码。
func TestGetCookiePlatformRuntimeDataExcludesLoginSecrets(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "platform-runtime-query-key")
	// store 是本测试使用的 SQLite repository 聚合器；cleanup 负责关闭数据库。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是平台运行视图测试共用的上下文。
	ctx := context.Background()
	// userID 是测试账号所属的本地用户 ID。
	var userID int64
	// createErr 表示创建测试用户失败的原因。
	if createErr := store.DB.QueryRowContext(ctx,
		`INSERT INTO users (username,email,password_hash) VALUES (?,?,?) RETURNING id`,
		"platform-runtime-user", "platform-runtime@example.com", "test-hash").Scan(&userID); createErr != nil {
		t.Fatalf("create user: %v", createErr)
	}
	// saveErr 表示创建可解密测试账号失败的原因。
	if saveErr := store.Cookies.Save(ctx, "platform-runtime", "unb=1; _m_h5_tk=platform", userID); saveErr != nil {
		t.Fatalf("save cookie: %v", saveErr)
	}
	// metadata 是平台请求所需的权威 Cookie 快照 metadata。
	metadata := `{"note":"platform-runtime"}`
	// updateErr 表示写入测试 metadata 失败的原因。
	if updateErr := store.Cookies.UpdateRenewalCookie(ctx, "platform-runtime", "unb=1; _m_h5_tk=platform", metadata, time.Now().Unix()); updateErr != nil {
		t.Fatalf("update metadata: %v", updateErr)
	}
	// showBrowserErr 表示设置 token 风控浏览器偏好失败的原因。
	if _, showBrowserErr := store.DB.ExecContext(ctx, `UPDATE cookies SET show_browser=1, username=?, password=? WHERE id=?`,
		"platform-user", "not-a-password-ciphertext", "platform-runtime"); showBrowserErr != nil {
		t.Fatalf("corrupt login secret: %v", showBrowserErr)
	}

	// data 是平台调用流程获得的最小账号视图。
	data, dataErr := store.Cookies.GetCookiePlatformRuntimeData(ctx, "platform-runtime")
	if dataErr != nil {
		t.Fatalf("GetCookiePlatformRuntimeData: %v", dataErr)
	}
	if data.ID != "platform-runtime" || data.UserID != userID || data.Value != "unb=1; _m_h5_tk=platform" || data.MetadataJSON != metadata || !data.ShowBrowser {
		t.Fatalf("platform runtime data=%+v", data)
	}
}
