package db

import (
	"context"
	"errors"
	"testing"
)

// TestGetDisplayNamePrecedenceAndNoDecryption 验证展示名称按备注、昵称、账号 ID 依次回退，且不读取或解密敏感列。
func TestGetDisplayNamePrecedenceAndNoDecryption(t *testing.T) {
	// store、cleanup 提供隔离的 SQLite repository 聚合器及关闭责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是名称查询共用的本地上下文。
	ctx := context.Background()
	// ownerID 保存测试账号所属用户主键，用于满足 cookies 表的外键约束。
	var ownerID int64
	// ownerErr 表示创建测试用户失败的原因。
	if ownerErr := store.DB.QueryRowContext(ctx,
		`INSERT INTO users (username,email,password_hash) VALUES (?,?,?) RETURNING id`,
		"display-owner", "display-owner@example.com", "test-hash").Scan(&ownerID); ownerErr != nil {
		t.Fatalf("创建测试用户: %v", ownerErr)
	}
	// fixtures 保存三个名称来源组合，value/password/metadata 故意写入非法密文以证明名称查询不解密敏感列。
	// 每项依次是账号 ID、备注、昵称。
	fixtures := []struct {
		// id 是账号主键，也是最终回退名称。
		id string
		// remark、nickname 是用户备注和平台昵称缓存。
		remark, nickname string
	}{
		{id: "both", remark: "主账号", nickname: "平台昵称"},
		{id: "nickname-only", remark: "", nickname: "昵称B"},
		{id: "id-only", remark: "   ", nickname: " "},
	}
	// fixture 表示当前待写入的账号名称组合。
	for _, fixture := range fixtures {
		// insertErr 表示写入非敏感名称夹具失败的原因。
		if _, insertErr := store.DB.ExecContext(ctx, `
			INSERT INTO cookies (id,value,user_id,remark,nickname,password,metadata_json)
			VALUES (?, 'not-a-ciphertext', ?, ?, ?, 'not-a-password', 'not-a-metadata')`,
			fixture.id, ownerID, fixture.remark, fixture.nickname); insertErr != nil {
			t.Fatalf("写入账号 %s: %v", fixture.id, insertErr)
		}
	}
	// cases 保存账号 ID 到期望展示名称的确定性映射。
	cases := map[string]string{
		"both":          "主账号",
		"nickname-only": "昵称B",
		"id-only":       "id-only",
	}
	// cookieID、want 是当前待验证的账号主键与期望名称。
	for cookieID, want := range cases {
		// got、lookupErr 保存展示名称查询结果及错误；非法密文不应导致查询失败。
		got, lookupErr := store.Cookies.GetDisplayName(ctx, cookieID)
		if lookupErr != nil || got != want {
			t.Fatalf("GetDisplayName(%s)=%q err=%v want=%q", cookieID, got, lookupErr, want)
		}
	}
	// missing、missingErr 表示不存在账号的查询结果，应返回 ErrNotFound 而不是回退账号 ID。
	missing, missingErr := store.Cookies.GetDisplayName(ctx, "missing-account")
	if !errors.Is(missingErr, ErrNotFound) || missing != "" {
		t.Fatalf("缺失账号结果=%q err=%v", missing, missingErr)
	}
}
