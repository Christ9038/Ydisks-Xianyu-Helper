package db

import (
	"context"
	"fmt"
	"sync"
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

	// 再次 Save 更新 token，但永久 device ID 不允许覆盖。
	if err := store.Tokens.Save(ctx, "cid", "dev-2", "tok-2", expire+1); err != nil {
		t.Fatalf("Save upsert: %v", err)
	}
	tk, _ = store.Tokens.Get(ctx, "cid")
	if tk.DeviceID != "dev-1" || tk.AccessToken != "tok-2" || tk.ExpireAt != expire+1 {
		t.Fatalf("upsert 后字段应更新: %+v", tk)
	}

	// Clear 只清 token，永久 device ID 必须保留。
	if err := store.Tokens.Clear(ctx, "cid"); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	tk, err = store.Tokens.Get(ctx, "cid")
	if err != nil || tk.DeviceID != "dev-1" || tk.AccessToken != "" || tk.ExpireAt != 0 {
		t.Fatalf("Clear 后 device ID 应保留且 token 清空: tk=%+v err=%v", tk, err)
	}

	// Clear 不存在的行不应报错。
	if err := store.Tokens.Clear(ctx, "absent"); err != nil {
		t.Fatalf("Clear 不存在的行不应报错: %v", err)
	}
}

func TestAccountTokens_DeviceIDSurvivesTokenLifecycle(t *testing.T) {
	store, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	store.Users.Create(ctx, "admin", "a@e.com", "pw")
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if err := store.Cookies.Save(ctx, "cid", "unb=1; _m_h5_tk=t;", admin.ID); err != nil {
		t.Fatal(err)
	}
	first, err := store.Tokens.GetOrCreateDeviceID(ctx, "cid", "device-first")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Tokens.Save(ctx, "cid", "device-replacement", "token", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if err := store.Tokens.Clear(ctx, "cid"); err != nil {
		t.Fatal(err)
	}
	afterRestart, err := store.Tokens.GetOrCreateDeviceID(ctx, "cid", "device-after-restart")
	if err != nil || first != "device-first" || afterRestart != first {
		t.Fatalf("device ID changed across token lifecycle: first=%q after=%q err=%v", first, afterRestart, err)
	}
}

func TestAccountTokens_ConcurrentDeviceIDCreationConverges(t *testing.T) {
	store, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	store.Users.Create(ctx, "admin", "a@e.com", "pw")
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if err := store.Cookies.Save(ctx, "cid", "unb=1; _m_h5_tk=t;", admin.ID); err != nil {
		t.Fatal(err)
	}

	const workers = 12
	results := make(chan string, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			deviceID, err := store.Tokens.GetOrCreateDeviceID(ctx, "cid", fmt.Sprintf("device-%d", i))
			if err != nil {
				errs <- err
				return
			}
			results <- deviceID
		}(i)
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	var permanent string
	for deviceID := range results {
		if permanent == "" {
			permanent = deviceID
		}
		if deviceID != permanent {
			t.Fatalf("concurrent device IDs diverged: first=%q got=%q", permanent, deviceID)
		}
	}
	stored, err := store.Tokens.Get(ctx, "cid")
	if err != nil || stored.DeviceID != permanent {
		t.Fatalf("stored=%+v permanent=%q err=%v", stored, permanent, err)
	}
}
