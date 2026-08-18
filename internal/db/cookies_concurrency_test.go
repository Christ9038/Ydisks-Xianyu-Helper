package db

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// TestCreateOwnedConcurrentUsersNeverTransfersOwner 封装TestCreateOwnedConcurrent用户列表NeverTransfers所有者业务协调。
func TestCreateOwnedConcurrentUsersNeverTransfersOwner(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // err 用于本次流程后续判断的err
	_, err := store.Users.Create(ctx, "owner-a", "owner-a@example.com", "pw"); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	_, err := store.Users.Create(ctx, "owner-b", "owner-b@example.com", "pw"); err != nil {
		t.Fatal(err)
	}
	// a 用于本次流程后续判断的a
	a, _ := store.Users.GetByUsername(ctx, "owner-a")
	// b 用于本次流程后续判断的b
	b, _ := store.Users.GetByUsername(ctx, "owner-b")

	// start 用于本次流程后续判断的开始
	start := make(chan struct{})
	// results 用于本次流程后续判断的results
	results := make(chan error, 2)
	// wg 用于本次流程后续判断的wg
	var wg sync.WaitGroup
	// input 表示当前遍历过程中的input
	for _, input := range []struct {
		userID int64
		value  string
	}{{a.ID, "cookie-a"}, {b.ID, "cookie-b"}} {
		// input 用于本次流程后续判断的input
		input := input
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results <- store.Cookies.CreateOwned(ctx, "shared-account", input.value, input.userID)
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	// successes 用于本次流程后续判断的successes
	successes := 0
	// err 表示当前遍历过程中的err
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, ErrForbidden) && !errors.Is(err, ErrAlreadyExists) {
			t.Fatalf("unexpected create error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful creates=%d want 1", successes)
	}
	// owner 用于本次流程后续判断的所有者
	var owner int64
	// value 用于本次流程后续判断的值
	var value string
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT user_id,value FROM cookies WHERE id=?`, "shared-account").Scan(&owner, &value); err != nil {
		t.Fatal(err)
	}
	if owner == a.ID && value != "cookie-a" {
		t.Fatalf("owner A received other user's cookie: %q", value)
	}
	if owner == b.ID && value != "cookie-b" {
		t.Fatalf("owner B received other user's cookie: %q", value)
	}
}

// TestUpdateValueExistingCannotResurrectDeletedAccount 封装TestUpdate值ExistingCannotResurrectDeleted账号业务协调。
func TestUpdateValueExistingCannotResurrectDeletedAccount(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // err 用于本次流程后续判断的err
	_, err := store.Users.Create(ctx, "delete-race-owner", "delete-race-owner@example.com", "pw"); err != nil {
		t.Fatal(err)
	}
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "delete-race-owner")
	if // err 用于本次流程后续判断的err
	err := store.Cookies.CreateOwned(ctx, "deleted-account", "old", admin.ID); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Cookies.Delete(ctx, "deleted-account"); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Cookies.UpdateValueExisting(ctx, "deleted-account", "new"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update deleted account err=%v want ErrNotFound", err)
	}
	// count 用于本次流程后续判断的数量
	var count int
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM cookies WHERE id=?`, "deleted-account").Scan(&count); err != nil || count != 0 {
		t.Fatalf("deleted account resurrected: count=%d err=%v", count, err)
	}
}

// TestDeleteUserCleansNonForeignKeyAccountData 封装TestDelete用户CleansNonForeignKey账号数据业务协调。
func TestDeleteUserCleansNonForeignKeyAccountData(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // err 用于本次流程后续判断的err
	_, err := store.Users.Create(ctx, "delete-owner", "delete-owner@example.com", "pw"); err != nil {
		t.Fatal(err)
	}
	// owner 用于本次流程后续判断的所有者
	owner, _ := store.Users.GetByUsername(ctx, "delete-owner")
	if // err 用于本次流程后续判断的err
	err := store.Cookies.CreateOwned(ctx, "delete-owned-account", "cookie", owner.ID); err != nil {
		t.Fatal(err)
	}
	// query 表示当前遍历过程中的查询
	for _, query := range []string{
		`INSERT INTO item_replay (item_id,cookie_id,reply_content) VALUES ('item','delete-owned-account','secret reply')`,
		`INSERT INTO scheduled_cookies_refresh_log (cookie_id,status) VALUES ('delete-owned-account','failed')`,
		`INSERT INTO scheduled_login_renew_log (cookie_id,status) VALUES ('delete-owned-account','failed')`,
		`INSERT INTO scheduled_api_cookie_renew_log (cookie_id,status) VALUES ('delete-owned-account','failed')`,
		`INSERT INTO account_login_logs (cookie_id,user_id,method,status,created_at) VALUES ('delete-owned-account',0,'password','failed',1)`,
	} {
		if // err 用于本次流程后续判断的err
		_, err := store.DB.ExecContext(ctx, query); err != nil {
			t.Fatalf("seed orphan candidate: %v", err)
		}
	}
	if // err 用于本次流程后续判断的err
	err := store.Users.Delete(ctx, owner.ID); err != nil {
		t.Fatal(err)
	}
	// table 表示当前遍历过程中的table
	for _, table := range []string{
		"cookies", "item_replay", "scheduled_cookies_refresh_log", "scheduled_login_renew_log",
		"scheduled_api_cookie_renew_log", "account_login_logs",
	} {
		// count 用于本次流程后续判断的数量
		var count int
		if // err 用于本次流程后续判断的err
		err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE cookie_id=?`, "delete-owned-account").Scan(&count); err != nil {
			if table == "cookies" {
				err = store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM cookies WHERE id=?`, "delete-owned-account").Scan(&count)
			}
			if err != nil {
				t.Fatalf("count %s: %v", table, err)
			}
		}
		if count != 0 {
			t.Fatalf("%s retained %d rows", table, count)
		}
	}
}
