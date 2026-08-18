package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestDefaultReplyClaimReclaimsExpiredPendingLease 封装TestDefault回复ClaimReclaimsExpiredPendingLease业务协调。
func TestDefaultReplyClaimReclaimsExpiredPendingLease(t *testing.T) {
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// database、dialect、err 用于本次流程后续判断的database、dialect、err
	database, dialect, err := Open(ctx, filepath.Join(t.TempDir(), "reply-state.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	// store 用于本次流程后续判断的store
	store := NewStore(database, dialect)

	// result、err 用于本次流程后续判断的result、err
	result, err := database.ExecContext(ctx, `INSERT INTO users (username,email,password_hash) VALUES (?,?,?)`, "reply-test", "reply-test@example.com", "hash")
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	// userID、err 用于本次流程后续判断的用户ID、err
	userID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Cookies.Save(ctx, "cookie-1", "unb=1", userID); err != nil {
		t.Fatalf("save cookie: %v", err)
	}

	// initial、claimed、err 用于本次流程后续判断的initial、claimed、err
	initial, claimed, err := store.DefaultReps.ClaimRecord(ctx, "cookie-1", "chat-1", true, true)
	if err != nil || !claimed || initial.Status != "pending" {
		t.Fatalf("initial claim: record=%+v claimed=%v err=%v", initial, claimed, err)
	}
	if _, claimed, err = store.DefaultReps.ClaimRecord(ctx, "cookie-1", "chat-1", true, true); err != nil || claimed {
		t.Fatalf("active pending lease must not be claimed: claimed=%v err=%v", claimed, err)
	}

	_, err = database.ExecContext(ctx, `UPDATE default_reply_records SET lease_expires_at=? WHERE cookie_id=? AND chat_id=?`, time.Now().Add(-10*time.Minute).Unix(), "cookie-1", "chat-1")
	if err != nil {
		t.Fatalf("expire lease: %v", err)
	}
	// reclaimed、claimed、err 用于本次流程后续判断的reclaimed、claimed、err
	reclaimed, claimed, err := store.DefaultReps.ClaimRecord(ctx, "cookie-1", "chat-1", true, true)
	if err != nil || !claimed || reclaimed.Status != "pending" {
		t.Fatalf("expired pending lease should be reclaimed: record=%+v claimed=%v err=%v", reclaimed, claimed, err)
	}
}
