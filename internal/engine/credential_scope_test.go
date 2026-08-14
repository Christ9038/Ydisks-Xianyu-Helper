package engine

import (
	"context"
	"testing"
	"time"

	"xianyu-go/internal/xianyu/cookierefresh"
)

// TestCookieSnapshotMatchesDBSkipsLoginSecret 验证 WS 注册前凭证校验不解密登录密码。
func TestCookieSnapshotMatchesDBSkipsLoginSecret(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "engine-ws-register-query-key")
	// acc 和 store 是本测试的账号及数据库；cleanup 负责关闭临时数据库。
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// ctx 是 WS 注册前凭证校验共用的上下文。
	ctx := context.Background()
	// cookieValue 是数据库中待校验的 Cookie 明文。
	cookieValue := "unb=123; _m_h5_tk=ws-register"
	// metadata 是数据库中待校验的权威 Cookie 快照 metadata。
	metadata := cookierefresh.MetadataWithSnapshot(`{"note":"ws-register"}`, []cookierefresh.BrowserCookie{{Name: "sid", Value: "ws", Domain: ".goofish.com", Path: "/"}})
	// updateErr 表示预置 WS 注册凭证失败的原因。
	if updateErr := store.Cookies.UpdateRenewalCookie(ctx, acc.CookieID, cookieValue, metadata, time.Now().Unix()); updateErr != nil {
		t.Fatalf("preset ws register credential: %v", updateErr)
	}
	// corruptErr 表示写入故意损坏的登录密码密文失败的原因。
	if _, corruptErr := store.DB.ExecContext(ctx,
		`UPDATE cookies SET username=?,password=? WHERE id=?`,
		"ws-register-user", "not-a-password-ciphertext", acc.CookieID); corruptErr != nil {
		t.Fatalf("corrupt password: %v", corruptErr)
	}
	// expectedFP 是当前 Cookie 与 metadata 组合的权威指纹。
	expectedFP := credentialStateFingerprint(cookieValue, metadata)
	if !acc.cookieSnapshotMatchesDB(ctx, expectedFP) {
		t.Fatal("损坏登录密码不应阻断 WS 注册前凭证校验")
	}
}

// TestUpdateCookieUsesRuntimeData 验证运行时 Cookie 同步不解密无关的登录密码。
func TestUpdateCookieUsesRuntimeData(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "engine-update-cookie-query-key")
	// acc 和 store 是本测试的账号及数据库；cleanup 负责关闭临时数据库。
	acc, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// ctx 是运行时 Cookie 同步共用的上下文。
	ctx := context.Background()
	// cookieValue 是数据库中待同步到运行时的 Cookie 明文。
	cookieValue := "unb=123; _m_h5_tk=update-runtime"
	// metadata 是数据库中待同步的权威 Cookie 快照 metadata。
	metadata := cookierefresh.MetadataWithSnapshot(`{"note":"update"}`, []cookierefresh.BrowserCookie{{Name: "sid", Value: "update", Domain: ".goofish.com", Path: "/"}})
	// updateErr 表示预置运行时 Cookie 失败的原因。
	if updateErr := store.Cookies.UpdateRenewalCookie(ctx, acc.CookieID, cookieValue, metadata, time.Now().Unix()); updateErr != nil {
		t.Fatalf("preset update credential: %v", updateErr)
	}
	// corruptErr 表示写入故意损坏的登录密码密文失败的原因。
	if _, corruptErr := store.DB.ExecContext(ctx,
		`UPDATE cookies SET username=?,password=? WHERE id=?`,
		"engine-update-user", "not-a-password-ciphertext", acc.CookieID); corruptErr != nil {
		t.Fatalf("corrupt password: %v", corruptErr)
	}

	acc.UpdateCookie("")
	// got 是运行时同步完成后的 Cookie 明文。
	got := acc.currentCookieStr()
	if got != cookieValue {
		t.Fatalf("运行时 Cookie=%q want %q", got, cookieValue)
	}
	// acc.mu 保护 credentialFP，读取后用于验证 Cookie 与 metadata 均已同步。
	acc.mu.Lock()
	// currentFP 是运行时记录的 Cookie 与 metadata 组合指纹。
	currentFP := acc.credentialFP
	acc.mu.Unlock()
	if currentFP != credentialStateFingerprint(cookieValue, metadata) {
		t.Fatalf("runtime credential fingerprint=%q", currentFP)
	}
}
