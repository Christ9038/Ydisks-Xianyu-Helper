package db

import (
	"context"
	"testing"
	"time"
)

func TestNotificationOutboxLeaseFencesStaleWorker(t *testing.T) {
	store, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	if ok, err := store.Users.Create(ctx, "notify-owner", "notify-owner@example.com", "pw"); err != nil || !ok {
		t.Fatalf("create owner: ok=%v err=%v", ok, err)
	}
	owner, _ := store.Users.GetByUsername(ctx, "notify-owner")
	result, err := store.DB.ExecContext(ctx, `INSERT INTO notification_channels (name,type,config,enabled,user_id) VALUES (?,?,?,1,?)`, "test", "webhook", `{}`, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	channelID, _ := result.LastInsertId()
	if err := store.Notifications.EnqueueOutbox(ctx, []NotificationOutboxInput{{ChannelID: channelID, EventType: "test", Body: "body"}}); err != nil {
		t.Fatal(err)
	}
	var status, workerToken, lastError string
	var attempts int
	var nextAttemptAt, leaseExpiresAt int64
	if err := store.DB.QueryRowContext(ctx, `SELECT status,attempt_count,next_attempt_at,lease_expires_at,worker_token,last_error
		FROM notification_outbox WHERE channel_id=?`, channelID).
		Scan(&status, &attempts, &nextAttemptAt, &leaseExpiresAt, &workerToken, &lastError); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || attempts != 0 || nextAttemptAt != 0 || leaseExpiresAt != 0 || workerToken != "" || lastError != "" {
		t.Fatalf("unexpected initial outbox state: status=%q attempts=%d next=%d lease=%d worker=%q error=%q",
			status, attempts, nextAttemptAt, leaseExpiresAt, workerToken, lastError)
	}
	now := time.Unix(100, 0)
	first, err := store.Notifications.ClaimOutbox(ctx, "worker-1", now, 10)
	if err != nil || len(first) != 1 || first[0].AttemptCount != 1 {
		t.Fatalf("first claim: messages=%+v err=%v", first, err)
	}
	second, err := store.Notifications.ClaimOutbox(ctx, "worker-2", now.Add(time.Minute), 10)
	if err != nil || len(second) != 1 || second[0].AttemptCount != 2 {
		t.Fatalf("reclaim: messages=%+v err=%v", second, err)
	}
	if completed, err := store.Notifications.CompleteOutbox(ctx, first[0].ID, "worker-1"); err != nil || completed {
		t.Fatalf("stale completion: completed=%v err=%v", completed, err)
	}
	if retried, err := store.Notifications.RetryOutbox(ctx, second[0].ID, "worker-2", "temporary", now.Add(2*time.Minute).Unix(), false); err != nil || !retried {
		t.Fatalf("retry: retried=%v err=%v", retried, err)
	}
	if early, err := store.Notifications.ClaimOutbox(ctx, "worker-3", now.Add(90*time.Second), 10); err != nil || len(early) != 0 {
		t.Fatalf("early retry claim: messages=%+v err=%v", early, err)
	}
	due, err := store.Notifications.ClaimOutbox(ctx, "worker-3", now.Add(3*time.Minute), 10)
	if err != nil || len(due) != 1 {
		t.Fatalf("due retry claim: messages=%+v err=%v", due, err)
	}
	if completed, err := store.Notifications.CompleteOutbox(ctx, due[0].ID, "worker-3"); err != nil || !completed {
		t.Fatalf("complete: completed=%v err=%v", completed, err)
	}
}
