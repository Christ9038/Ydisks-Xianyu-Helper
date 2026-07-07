package db

import (
	"context"
	"testing"
	"time"
)

// TestAccountTokens_CRUD 覆盖 account_tokens 的 Get/Save(upsert)/Clear。
func TestAccountTokens_CRUD(t *testing.T) {
	store, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()

	// 前置：admin 用户 + cookie 行（account_tokens 有 FK→cookies）。
	store.Users.Create(ctx, "admin", "a@e.com", "pw")
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if err := store.Cookies.Save(ctx, "cid", "unb=1; _m_h5_tk=t;", admin.ID); err != nil {
		t.Fatalf("Save cookie: %v", err)
	}

	// 不存在 → ErrNotFound。
	if _, err := store.Tokens.Get(ctx, "cid"); err != ErrNotFound {
		t.Fatalf("不存在应返回 ErrNotFound，got %v", err)
	}

	// Save（首次写入）。
	expire := time.Now().Add(time.Hour).Unix()
	if err := store.Tokens.Save(ctx, "cid", "dev-1", "tok-1", expire); err != nil {
		t.Fatalf("Save: %v", err)
	}
	tk, err := store.Tokens.Get(ctx, "cid")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if tk.DeviceID != "dev-1" || tk.AccessToken != "tok-1" || tk.ExpireAt != expire {
		t.Fatalf("Get 返回不匹配: %+v", tk)
	}

	// 再次 Save 覆盖（upsert）。
	if err := store.Tokens.Save(ctx, "cid", "dev-2", "tok-2", expire+1); err != nil {
		t.Fatalf("Save upsert: %v", err)
	}
	tk, _ = store.Tokens.Get(ctx, "cid")
	if tk.DeviceID != "dev-2" || tk.AccessToken != "tok-2" || tk.ExpireAt != expire+1 {
		t.Fatalf("upsert 后字段应更新: %+v", tk)
	}

	// Clear → 再 Get 应 ErrNotFound。
	if err := store.Tokens.Clear(ctx, "cid"); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if _, err := store.Tokens.Get(ctx, "cid"); err != ErrNotFound {
		t.Fatalf("Clear 后应 ErrNotFound，got %v", err)
	}

	// Clear 不存在的行不应报错。
	if err := store.Tokens.Clear(ctx, "absent"); err != nil {
		t.Fatalf("Clear 不存在的行不应报错: %v", err)
	}
}
