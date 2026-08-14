package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestDefaultReplyClaimReclaimsExpiredPendingLease 负责TestDefault回复ClaimReclaimsExpiredPendingLease相关处理。
func TestDefaultReplyClaimReclaimsExpiredPendingLease(t *testing.T) {
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// database、dialect、err 保存database、dialect、err，供当前处理流程使用
	database, dialect, err := Open(ctx, filepath.Join(t.TempDir(), "reply-state.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	// store 保存store，供当前处理流程使用
	store := NewStore(database, dialect)

	// result、err 保存result、err，供当前处理流程使用
	result, err := database.ExecContext(ctx, `INSERT INTO users (username,email,password_hash) VALUES (?,?,?)`, "reply-test", "reply-test@example.com", "hash")
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	// userID、err 保存用户ID、err，供当前处理流程使用
	userID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId: %v", err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.Save(ctx, "cookie-1", "unb=1", userID); err != nil {
		t.Fatalf("save cookie: %v", err)
	}

	// initial、claimed、err 保存initial、claimed、err，供当前处理流程使用
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
	// reclaimed、claimed、err 保存reclaimed、claimed、err，供当前处理流程使用
	reclaimed, claimed, err := store.DefaultReps.ClaimRecord(ctx, "cookie-1", "chat-1", true, true)
	if err != nil || !claimed || reclaimed.Status != "pending" {
		t.Fatalf("expired pending lease should be reclaimed: record=%+v claimed=%v err=%v", reclaimed, claimed, err)
	}
}
