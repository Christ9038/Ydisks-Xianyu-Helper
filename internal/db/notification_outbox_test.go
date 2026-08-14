package db

import (
	"context"
	"testing"
	"time"
)

// TestNotificationOutboxLeaseFencesStaleWorker 负责Test通知OutboxLeaseFencesStale工作器相关处理。
func TestNotificationOutboxLeaseFencesStaleWorker(t *testing.T) {
	// store、cleanup 保存store、cleanup，供当前处理流程使用
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	if // ok、err 保存ok、err，供当前处理流程使用
	ok, err := store.Users.Create(ctx, "notify-owner", "notify-owner@example.com", "pw"); err != nil || !ok {
		t.Fatalf("create owner: ok=%v err=%v", ok, err)
	}
	// owner 保存所有者，供当前处理流程使用
	owner, _ := store.Users.GetByUsername(ctx, "notify-owner")
	// result、err 保存result、err，供当前处理流程使用
	result, err := store.DB.ExecContext(ctx, `INSERT INTO notification_channels (name,type,config,enabled,user_id) VALUES (?,?,?,1,?)`, "test", "webhook", `{}`, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	// channelID 保存渠道ID，供当前处理流程使用
	channelID, _ := result.LastInsertId()
	if // err 保存err，供当前处理流程使用
	err := store.Notifications.EnqueueOutbox(ctx, []NotificationOutboxInput{{ChannelID: channelID, EventType: "test", Body: "body"}}); err != nil {
		t.Fatal(err)
	}
	// status、workerToken、lastError 保存status、workerToken、last错误，供当前处理流程使用
	var status, workerToken, lastError string
	// attempts 保存尝试次数，供当前处理流程使用
	var attempts int
	// nextAttemptAt、leaseExpiresAt 保存next尝试次数At、leaseExpiresAt，供当前处理流程使用
	var nextAttemptAt, leaseExpiresAt int64
	if // err 保存err，供当前处理流程使用
	err := store.DB.QueryRowContext(ctx, `SELECT status,attempt_count,next_attempt_at,lease_expires_at,worker_token,last_error
		FROM notification_outbox WHERE channel_id=?`, channelID).
		Scan(&status, &attempts, &nextAttemptAt, &leaseExpiresAt, &workerToken, &lastError); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || attempts != 0 || nextAttemptAt != 0 || leaseExpiresAt != 0 || workerToken != "" || lastError != "" {
		t.Fatalf("unexpected initial outbox state: status=%q attempts=%d next=%d lease=%d worker=%q error=%q",
			status, attempts, nextAttemptAt, leaseExpiresAt, workerToken, lastError)
	}
	// now 保存now，供当前处理流程使用
	now := time.Unix(100, 0)
	// first、err 保存first、err，供当前处理流程使用
	first, err := store.Notifications.ClaimOutbox(ctx, "worker-1", now, 10)
	if err != nil || len(first) != 1 || first[0].AttemptCount != 1 {
		t.Fatalf("first claim: messages=%+v err=%v", first, err)
	}
	// second、err 保存second、err，供当前处理流程使用
	second, err := store.Notifications.ClaimOutbox(ctx, "worker-2", now.Add(time.Minute), 10)
	if err != nil || len(second) != 1 || second[0].AttemptCount != 2 {
		t.Fatalf("reclaim: messages=%+v err=%v", second, err)
	}
	if // completed、err 保存completed、err，供当前处理流程使用
	completed, err := store.Notifications.CompleteOutbox(ctx, first[0].ID, "worker-1"); err != nil || completed {
		t.Fatalf("stale completion: completed=%v err=%v", completed, err)
	}
	if // retried、err 保存retried、err，供当前处理流程使用
	retried, err := store.Notifications.RetryOutbox(ctx, second[0].ID, "worker-2", "temporary", now.Add(2*time.Minute).Unix(), false); err != nil || !retried {
		t.Fatalf("retry: retried=%v err=%v", retried, err)
	}
	if // early、err 保存early、err，供当前处理流程使用
	early, err := store.Notifications.ClaimOutbox(ctx, "worker-3", now.Add(90*time.Second), 10); err != nil || len(early) != 0 {
		t.Fatalf("early retry claim: messages=%+v err=%v", early, err)
	}
	// due、err 保存due、err，供当前处理流程使用
	due, err := store.Notifications.ClaimOutbox(ctx, "worker-3", now.Add(3*time.Minute), 10)
	if err != nil || len(due) != 1 {
		t.Fatalf("due retry claim: messages=%+v err=%v", due, err)
	}
	if // completed、err 保存completed、err，供当前处理流程使用
	completed, err := store.Notifications.CompleteOutbox(ctx, due[0].ID, "worker-3"); err != nil || !completed {
		t.Fatalf("complete: completed=%v err=%v", completed, err)
	}
}
