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
	// staleUncertain、err 保存旧 worker 尝试隔离当前消息的结果和数据库错误。
	staleUncertain, err := store.Notifications.MarkOutboxUncertain(ctx, first[0].ID, "worker-1", "旧 worker 确认失败")
	if err != nil || staleUncertain {
		t.Fatalf("stale uncertain: uncertain=%v err=%v", staleUncertain, err)
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
	// uncertain、err 保存不确定状态隔离结果和数据库错误。
	uncertain, err := store.Notifications.MarkOutboxUncertain(ctx, due[0].ID, "worker-3", "本地确认失败")
	if err != nil || !uncertain {
		t.Fatalf("mark uncertain: uncertain=%v err=%v", uncertain, err)
	}
	// status、workerToken、lastError、uncertainAt 保存隔离后的状态、租约和诊断信息。
	var uncertainStatus, uncertainWorkerToken, uncertainLastError string
	// uncertainAt 保存消息进入不确定隔离态的 Unix 时间戳。
	var uncertainAt int64
	// queryErr 保存读取不确定状态时的数据库错误。
	if queryErr := store.DB.QueryRowContext(ctx, `SELECT status,worker_token,last_error,uncertain_at
		FROM notification_outbox WHERE id=?`, due[0].ID).
		Scan(&uncertainStatus, &uncertainWorkerToken, &uncertainLastError, &uncertainAt); queryErr != nil {
		t.Fatal(queryErr)
	}
	if uncertainStatus != "uncertain" || uncertainWorkerToken != "" || uncertainLastError != "本地确认失败" || uncertainAt == 0 {
		t.Fatalf("unexpected uncertain state: status=%q worker=%q error=%q at=%d", uncertainStatus, uncertainWorkerToken, uncertainLastError, uncertainAt)
	}
	// afterUncertain、err 保存隔离消息再次领取的结果，确保不会自动重发。
	afterUncertain, err := store.Notifications.ClaimOutbox(ctx, "worker-4", now.Add(4*time.Minute), 10)
	if err != nil || len(afterUncertain) != 0 {
		t.Fatalf("uncertain message was claimable: messages=%+v err=%v", afterUncertain, err)
	}
}

// TestNotificationOutboxPermanentRetry 将达到重试上限的发送失败消息标记为 dead 隔离。
func TestNotificationOutboxPermanentRetry(t *testing.T) {
	// store、cleanup 保存测试数据库及其关闭责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存本测试使用的根上下文。
	ctx := context.Background()
	// ok、err 保存测试用户创建结果和数据库错误。
	ok, err := store.Users.Create(ctx, "notify-dead", "notify-dead@example.com", "pw")
	if err != nil || !ok {
		t.Fatalf("create owner: ok=%v err=%v", ok, err)
	}
	// owner、ownerErr 保存通知渠道所属用户和查询错误。
	owner, ownerErr := store.Users.GetByUsername(ctx, "notify-dead")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// result、err 保存通知渠道插入结果和数据库错误。
	result, err := store.DB.ExecContext(ctx, `INSERT INTO notification_channels (name,type,config,enabled,user_id) VALUES (?,?,?,?,?)`, "dead", "webhook", `{}`, 1, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	// channelID、err 保存渠道标识和读取标识时的错误。
	channelID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	// enqueueErr 保存写入待发送消息时的数据库错误。
	if enqueueErr := store.Notifications.EnqueueOutbox(ctx, []NotificationOutboxInput{{ChannelID: channelID, EventType: "test", Body: "body"}}); enqueueErr != nil {
		t.Fatal(enqueueErr)
	}
	// claimed、err 保存领取到的消息和数据库错误。
	claimed, err := store.Notifications.ClaimOutbox(ctx, "worker-dead", time.Unix(100, 0), 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim: messages=%+v err=%v", claimed, err)
	}
	// updated、err 保存永久失败状态更新结果和数据库错误。
	updated, err := store.Notifications.RetryOutbox(ctx, claimed[0].ID, "worker-dead", "远端发送失败", time.Unix(200, 0).Unix(), true)
	if err != nil || !updated {
		t.Fatalf("retry dead: updated=%v err=%v", updated, err)
	}
	// status 保存永久失败消息的最终隔离状态。
	var status string
	// queryErr 保存读取永久失败状态时的数据库错误。
	if queryErr := store.DB.QueryRowContext(ctx, `SELECT status FROM notification_outbox WHERE id=?`, claimed[0].ID).Scan(&status); queryErr != nil {
		t.Fatal(queryErr)
	}
	if status != "dead" {
		t.Fatalf("status=%q want dead", status)
	}
}
