package db

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestCreateOwnedConcurrentUsersNeverTransfersOwner(t *testing.T) {
	store, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := store.Users.Create(ctx, "owner-a", "owner-a@example.com", "pw"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Users.Create(ctx, "owner-b", "owner-b@example.com", "pw"); err != nil {
		t.Fatal(err)
	}
	a, _ := store.Users.GetByUsername(ctx, "owner-a")
	b, _ := store.Users.GetByUsername(ctx, "owner-b")

	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, input := range []struct {
		userID int64
		value  string
	}{{a.ID, "cookie-a"}, {b.ID, "cookie-b"}} {
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

	successes := 0
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
	var owner int64
	var value string
	if err := store.DB.QueryRowContext(ctx, `SELECT user_id,value FROM cookies WHERE id=?`, "shared-account").Scan(&owner, &value); err != nil {
		t.Fatal(err)
	}
	if owner == a.ID && value != "cookie-a" {
		t.Fatalf("owner A received other user's cookie: %q", value)
	}
	if owner == b.ID && value != "cookie-b" {
		t.Fatalf("owner B received other user's cookie: %q", value)
	}
}

func TestUpdateValueExistingCannotResurrectDeletedAccount(t *testing.T) {
	store, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := store.Users.Create(ctx, "delete-race-owner", "delete-race-owner@example.com", "pw"); err != nil {
		t.Fatal(err)
	}
	admin, _ := store.Users.GetByUsername(ctx, "delete-race-owner")
	if err := store.Cookies.CreateOwned(ctx, "deleted-account", "old", admin.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Cookies.Delete(ctx, "deleted-account"); err != nil {
		t.Fatal(err)
	}
	if err := store.Cookies.UpdateValueExisting(ctx, "deleted-account", "new"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update deleted account err=%v want ErrNotFound", err)
	}
	var count int
	if err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM cookies WHERE id=?`, "deleted-account").Scan(&count); err != nil || count != 0 {
		t.Fatalf("deleted account resurrected: count=%d err=%v", count, err)
	}
}

func TestDeleteUserCleansNonForeignKeyAccountData(t *testing.T) {
	store, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := store.Users.Create(ctx, "delete-owner", "delete-owner@example.com", "pw"); err != nil {
		t.Fatal(err)
	}
	owner, _ := store.Users.GetByUsername(ctx, "delete-owner")
	if err := store.Cookies.CreateOwned(ctx, "delete-owned-account", "cookie", owner.ID); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`INSERT INTO item_replay (item_id,cookie_id,reply_content) VALUES ('item','delete-owned-account','secret reply')`,
		`INSERT INTO scheduled_cookies_refresh_log (cookie_id,status) VALUES ('delete-owned-account','failed')`,
		`INSERT INTO scheduled_login_renew_log (cookie_id,status) VALUES ('delete-owned-account','failed')`,
		`INSERT INTO scheduled_api_cookie_renew_log (cookie_id,status) VALUES ('delete-owned-account','failed')`,
		`INSERT INTO account_login_logs (cookie_id,user_id,method,status,created_at) VALUES ('delete-owned-account',0,'password','failed',1)`,
	} {
		if _, err := store.DB.ExecContext(ctx, query); err != nil {
			t.Fatalf("seed orphan candidate: %v", err)
		}
	}
	if err := store.Users.Delete(ctx, owner.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{
		"cookies", "item_replay", "scheduled_cookies_refresh_log", "scheduled_login_renew_log",
		"scheduled_api_cookie_renew_log", "account_login_logs",
	} {
		var count int
		if err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE cookie_id=?`, "delete-owned-account").Scan(&count); err != nil {
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
