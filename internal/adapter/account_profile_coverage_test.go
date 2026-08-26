package adapter

import (
	"context"
	"errors"
	"strings"
	"testing"

	accountapp "xianyu-go/internal/application/account"
)

// TestAccountProfilePureHelpers 覆盖账号资料适配器的缓存兜底、头像归一化和错误截断逻辑。
func TestAccountProfilePureHelpers(t *testing.T) {
	// cachedRemark 保存备注优先时的资料兜底结果。
	cachedRemark, cachedRemarkAvatar := cachedProfile(accountapp.Summary{ID: "account-123456", Remark: "备注", Nickname: "昵称", AvatarURL: "//img.test/a"})
	if cachedRemark != "备注" || cachedRemarkAvatar != "https://img.test/a" {
		t.Fatalf("cached remark=%q avatar=%q", cachedRemark, cachedRemarkAvatar)
	}
	// cachedNickname 保存备注为空时的昵称兜底结果。
	cachedNickname, _ := cachedProfile(accountapp.Summary{ID: "account-123456", Nickname: "昵称", AvatarURL: "http://img.test/a"})
	if cachedNickname != "昵称" {
		t.Fatalf("cached nickname=%q", cachedNickname)
	}
	// cachedID 保存没有资料时的短账号兜底结果。
	cachedID, _ := cachedProfile(accountapp.Summary{ID: "account-123456"})
	if cachedID != "账号 accoun" {
		t.Fatalf("cached id=%q", cachedID)
	}
	// avatarCases 保存不同头像协议的归一化结果。
	avatarCases := map[string]string{"//img.test/a": "https://img.test/a", "http://img.test/a": "https://img.test/a", "https://img.test/a": "https://img.test/a", "": ""}
	// raw、want 表示当前遍历中的头像输入及输出。
	for raw, want := range avatarCases {
		// got 保存当前头像协议归一化后的地址。
		if got := normalizeProfileAvatar(raw); got != want {
			t.Errorf("normalizeProfileAvatar(%q)=%q want %q", raw, got, want)
		}
	}
	// longError 保存超过响应上限的错误文本。
	longError := errors.New(strings.Repeat("x", 200))
	// got 保存截断后的错误文本长度。
	if got := truncateProfileError(longError); len(got) != 180 {
		t.Fatalf("truncated length=%d", len(got))
	}
	if truncateProfileError(nil) != "" || truncateProfileError(errors.New("short")) != "short" {
		t.Fatal("short error truncation incorrect")
	}
	if shortProfileID("abcdefghi") != "abcdef" || shortProfileID("abc") != "abc" {
		t.Fatal("short profile ID incorrect")
	}
	// flatCtx、flatSession 保存历史扁平 Cookie 会话转换结果。
	flatCtx, flatSession := withProfileCookieSession(context.Background(), &accountapp.CredentialDetail{Value: "sid=1"})
	if flatCtx == nil || flatSession == nil {
		t.Fatal("flat profile session missing")
	}
	// snapshotMetadata 保存完整浏览器 Cookie 快照的元数据。
	snapshotMetadata := `{"browser_cookie_snapshot":[{"name":"sid","value":"1","domain":".goofish.com","path":"/"}]}`
	// snapshotCtx、snapshotSession 保存完整快照会话转换结果。
	snapshotCtx, snapshotSession := withProfileCookieSession(context.Background(), &accountapp.CredentialDetail{MetadataJSON: snapshotMetadata})
	if snapshotCtx == nil || snapshotSession == nil {
		t.Fatal("snapshot profile session missing")
	}
}
