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
	// store、cleanup 保存store、cleanup，供当前处理流程使用
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()

	// 前置：admin 用户 + cookie 行（account_tokens 有 FK→cookies）。
	store.Users.Create(ctx, "admin", "a@e.com", "pw")
	// admin 保存admin，供当前处理流程使用
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.Save(ctx, "cid", "unb=1; _m_h5_tk=t;", admin.ID); err != nil {
		t.Fatalf("Save cookie: %v", err)
	}

	// 不存在 → ErrNotFound。
	if _, err := store.Tokens.Get(ctx, "cid"); err != ErrNotFound {
		t.Fatalf("不存在应返回 ErrNotFound，got %v", err)
	}

	// SaveBound（首次写入）。
	expire := time.Now().Add(time.Hour).Unix()
	if // err 保存err，供当前处理流程使用
	err := store.Tokens.SaveBound(ctx, "cid", "dev-1", "tok-1", expire, "cookie-hash-1"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// tk、err 保存tk、err，供当前处理流程使用
	tk, err := store.Tokens.Get(ctx, "cid")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if tk.DeviceID != "dev-1" || tk.AccessToken != "tok-1" || tk.ExpireAt != expire || tk.CookieFingerprint != "cookie-hash-1" {
		t.Fatalf("Get 返回不匹配: %+v", tk)
	}

	// 页面运行实例变化后，再次 Save 同步更新 device ID 和 token。
	if err := store.Tokens.SaveBound(ctx, "cid", "dev-2", "tok-2", expire+1, "cookie-hash-2"); err != nil {
		t.Fatalf("Save upsert: %v", err)
	}
	tk, _ = store.Tokens.Get(ctx, "cid")
	if tk.DeviceID != "dev-2" || tk.AccessToken != "tok-2" || tk.ExpireAt != expire+1 || tk.CookieFingerprint != "cookie-hash-2" {
		t.Fatalf("upsert 后字段应更新: %+v", tk)
	}

	// Clear 只清 token，保留最近一次页面运行实例的 device ID。
	if err := store.Tokens.Clear(ctx, "cid"); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	tk, err = store.Tokens.Get(ctx, "cid")
	if err != nil || tk.DeviceID != "dev-2" || tk.AccessToken != "" || tk.ExpireAt != 0 || tk.CookieFingerprint != "" {
		t.Fatalf("Clear 后 device ID 应保留且 token 清空: tk=%+v err=%v", tk, err)
	}

	// Clear 不存在的行不应报错。
	if err := store.Tokens.Clear(ctx, "absent"); err != nil {
		t.Fatalf("Clear 不存在的行不应报错: %v", err)
	}
}

// TestAccountTokens_SaveTracksLatestRuntimeDeviceID 负责Test账号TokensSaveTracksLatestRuntimeDeviceID相关处理。
func TestAccountTokens_SaveTracksLatestRuntimeDeviceID(t *testing.T) {
	// store、cleanup 保存store、cleanup，供当前处理流程使用
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	store.Users.Create(ctx, "admin", "a@e.com", "pw")
	// admin 保存admin，供当前处理流程使用
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.Save(ctx, "cid", "unb=1; _m_h5_tk=t;", admin.ID); err != nil {
		t.Fatal(err)
	}
	// first、err 保存first、err，供当前处理流程使用
	first, err := store.Tokens.GetOrCreateDeviceID(ctx, "cid", "device-first")
	if err != nil {
		t.Fatal(err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.Tokens.Save(ctx, "cid", "device-replacement", "token", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.Tokens.Clear(ctx, "cid"); err != nil {
		t.Fatal(err)
	}
	// afterRestart、err 保存afterRestart、err，供当前处理流程使用
	afterRestart, err := store.Tokens.GetOrCreateDeviceID(ctx, "cid", "device-after-restart")
	if err != nil || first != "device-first" || afterRestart != "device-replacement" {
		t.Fatalf("未保留最近一次运行时 device ID: first=%q after=%q err=%v", first, afterRestart, err)
	}
}

// TestAccountTokens_ConcurrentDeviceIDCreationConverges 负责Test账号TokensConcurrentDeviceIDCreationConverges相关处理。
func TestAccountTokens_ConcurrentDeviceIDCreationConverges(t *testing.T) {
	// store、cleanup 保存store、cleanup，供当前处理流程使用
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	store.Users.Create(ctx, "admin", "a@e.com", "pw")
	// admin 保存admin，供当前处理流程使用
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.Save(ctx, "cid", "unb=1; _m_h5_tk=t;", admin.ID); err != nil {
		t.Fatal(err)
	}

	// workers 保存workers，供当前处理流程使用
	const workers = 12
	// results 保存results，供当前处理流程使用
	results := make(chan string, workers)
	// errs 保存errs，供当前处理流程使用
	errs := make(chan error, workers)
	// wg 保存wg，供当前处理流程使用
	var wg sync.WaitGroup
	for // i 保存i，供当前处理流程使用
	i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// deviceID、err 保存deviceID、err，供当前处理流程使用
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
	// err 表示当前遍历过程中的err
	for err := range errs {
		t.Fatal(err)
	}
	// permanent 保存permanent，供当前处理流程使用
	var permanent string
	// deviceID 表示当前遍历过程中的deviceID
	for deviceID := range results {
		if permanent == "" {
			permanent = deviceID
		}
		if deviceID != permanent {
			t.Fatalf("concurrent device IDs diverged: first=%q got=%q", permanent, deviceID)
		}
	}
	// stored、err 保存stored、err，供当前处理流程使用
	stored, err := store.Tokens.Get(ctx, "cid")
	if err != nil || stored.DeviceID != permanent {
		t.Fatalf("stored=%+v permanent=%q err=%v", stored, permanent, err)
	}
}
