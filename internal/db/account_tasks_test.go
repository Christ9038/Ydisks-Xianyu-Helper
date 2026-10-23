package db

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

// TestAccountTaskStoreSQLiteLifecycle 验证账号任务设置、运行声明、重试和历史读取的 SQLite 闭环。
func TestAccountTaskStoreSQLiteLifecycle(t *testing.T) {
	// store、cleanup 保存本地迁移数据库及其释放函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是账号任务仓储测试使用的数据库上下文。
	ctx := context.Background()
	// created、createErr 保存测试用户初始化结果。
	created, createErr := store.Users.Create(ctx, "task-user", "task-user@example.com", "pw")
	if createErr != nil || !created {
		t.Fatalf("创建测试用户失败: created=%v err=%v", created, createErr)
	}
	// user、userErr 保存测试用户及其查询错误。
	user, userErr := store.Users.GetByUsername(ctx, "task-user")
	if userErr != nil {
		t.Fatal(userErr)
	}
	// cookieErr 保存测试账号 Cookie 初始化错误。
	if cookieErr := store.Cookies.Save(ctx, "task-cookie", "sid=test", user.ID); cookieErr != nil {
		t.Fatal(cookieErr)
	}
	// defaultSettings、settingsErr 保存未配置账号的默认任务设置。
	defaultSettings, settingsErr := store.AccountTasks.Get(ctx, "task-cookie")
	if settingsErr != nil || defaultSettings.RateContent == "" || defaultSettings.PolishTime != "03:00" {
		t.Fatalf("默认任务设置异常: settings=%+v err=%v", defaultSettings, settingsErr)
	}
	// settings 保存待写入的启用任务设置。
	settings := AccountTaskSettings{CookieID: "task-cookie", AutoRateEnabled: true, AutoPolishEnabled: true}
	// upsertErr 保存任务设置写入错误。
	if upsertErr := store.AccountTasks.Upsert(ctx, settings); upsertErr != nil {
		t.Fatal(upsertErr)
	}
	// stored、storedErr 保存写入后的任务设置。
	stored, storedErr := store.AccountTasks.Get(ctx, "task-cookie")
	if storedErr != nil || !stored.AutoRateEnabled || !stored.AutoPolishEnabled || stored.RateContent == "" || stored.PolishTime != "03:00" {
		t.Fatalf("任务设置写入异常: settings=%+v err=%v", stored, storedErr)
	}
	// enabled、enabledErr 保存启用任务列表。
	enabled, enabledErr := store.AccountTasks.Enabled(ctx)
	if enabledErr != nil || len(enabled) != 1 || enabled[0].CookieID != "task-cookie" {
		t.Fatalf("启用任务列表异常: settings=%+v err=%v", enabled, enabledErr)
	}
	// markErr 保存任务执行时间标记更新错误。
	if markErr := store.AccountTasks.MarkRateScan(ctx, "task-cookie", 100); markErr != nil {
		t.Fatal(markErr)
	}
	// markErr 保存抛光日期和时间标记更新错误。
	if markErr := store.AccountTasks.MarkPolished(ctx, "task-cookie", "2026-08-26", 200); markErr != nil {
		t.Fatal(markErr)
	}
	// marked、markedErr 保存标记更新后的任务设置。
	marked, markedErr := store.AccountTasks.Get(ctx, "task-cookie")
	if markedErr != nil || marked.LastRateScanAt != 100 || marked.LastPolishDate != "2026-08-26" || marked.LastPolishAt != 200 {
		t.Fatalf("任务标记更新异常: settings=%+v err=%v", marked, markedErr)
	}
	// run 描述一条可重试的账号任务运行记录。
	run := AccountTaskRun{RunKey: "task-run-1", CookieID: "task-cookie", TaskType: "auto_rate", TargetID: "order-1", RunDate: "2026-08-26"}
	// claimed、claimErr 保存首次声明运行结果。
	claimed, claimErr := store.AccountTasks.ClaimRun(ctx, run, 300)
	if claimErr != nil || !claimed {
		t.Fatalf("首次声明运行失败: claimed=%v err=%v", claimed, claimErr)
	}
	// duplicateClaim、duplicateErr 保存重复声明运行结果。
	duplicateClaim, duplicateErr := store.AccountTasks.ClaimRun(ctx, run, 300)
	if duplicateErr != nil || duplicateClaim {
		t.Fatalf("重复声明运行应失败: claimed=%v err=%v", duplicateClaim, duplicateErr)
	}
	// finishErr 保存首次运行失败及下次重试时间的写入错误。
	if finishErr := store.AccountTasks.FinishRun(ctx, run.RunKey, "failed", 0, 1, "retry", 500); finishErr != nil {
		t.Fatal(finishErr)
	}
	// earlyClaim、earlyErr 保存尚未到重试时间的声明结果。
	earlyClaim, earlyErr := store.AccountTasks.ClaimRun(ctx, run, 499)
	if earlyErr != nil || earlyClaim {
		t.Fatalf("提前重试不应成功: claimed=%v err=%v", earlyClaim, earlyErr)
	}
	// immediateClaim、immediateErr 保存用户主动立即重试的声明结果。
	immediateClaim, immediateErr := store.AccountTasks.ClaimRunImmediately(ctx, run, 499)
	if immediateErr != nil || !immediateClaim {
		t.Fatalf("立即重试应成功: claimed=%v err=%v", immediateClaim, immediateErr)
	}
	// recent、recentErr 保存任务历史查询结果。
	recent, recentErr := store.AccountTasks.RecentRuns(ctx, "task-cookie", 0)
	if recentErr != nil || len(recent) != 1 || recent[0].RunKey != run.RunKey || recent[0].Status != "running" || recent[0].AttemptCount != 2 {
		t.Fatalf("任务历史异常: runs=%+v err=%v", recent, recentErr)
	}
	// retryNumber 验证第 2 至第 5 次失败重试仍可按上限继续执行。
	for retryNumber := 2; retryNumber <= accountTaskMaxRetries; retryNumber++ {
		// finishErr 保存本次失败重试结果及下一次重试时间的写入错误。
		if finishErr := store.AccountTasks.FinishRun(ctx, run.RunKey, "failed", 0, 1, "retry", int64(600+retryNumber)); finishErr != nil {
			t.Fatal(finishErr)
		}
		// retryClaim、retryClaimErr 保存当前允许的失败重试抢占结果。
		retryClaim, retryClaimErr := store.AccountTasks.ClaimRun(ctx, run, int64(600+retryNumber))
		if retryClaimErr != nil || !retryClaim {
			t.Fatalf("第 %d 次重试应成功: claimed=%v err=%v", retryNumber, retryClaim, retryClaimErr)
		}
	}
	// finishErr 保存达到最大执行次数后的最终失败状态。
	if finishErr := store.AccountTasks.FinishRun(ctx, run.RunKey, "failed", 0, 1, "retry exhausted", 999); finishErr != nil {
		t.Fatal(finishErr)
	}
	// exhausted、exhaustedErr 保存达到上限后的运行历史和读取错误。
	exhausted, exhaustedErr := store.AccountTasks.RecentRuns(ctx, "task-cookie", 0)
	if exhaustedErr != nil || len(exhausted) != 1 || exhausted[0].AttemptCount != accountTaskMaxAttempts || exhausted[0].NextRetryAt != 0 || exhausted[0].Status != "failed" {
		t.Fatalf("重试上限状态错误: runs=%+v err=%v", exhausted, exhaustedErr)
	}
	// finalClaim、finalClaimErr 验证自动与人工入口都不能越过五次重试上限。
	finalClaim, finalClaimErr := store.AccountTasks.ClaimRun(ctx, run, 1000)
	if finalClaimErr != nil || finalClaim {
		t.Fatalf("达到上限后自动重试不应成功: claimed=%v err=%v", finalClaim, finalClaimErr)
	}
	finalClaim, finalClaimErr = store.AccountTasks.ClaimRunImmediately(ctx, run, 1000)
	if !errors.Is(finalClaimErr, ErrAccountTaskRetryLimit) || finalClaim {
		t.Fatalf("达到上限后人工重试应返回限制错误: claimed=%v err=%v", finalClaim, finalClaimErr)
	}
}

// TestDueAutoRateOrderIDsOnlyReturnsLocallyCompletedOrders 验证自动评价候选只来自本地确认收货事实，且不会混入其他状态或账号订单。
func TestDueAutoRateOrderIDsOnlyReturnsLocallyCompletedOrders(t *testing.T) {
	// store、cleanup 保存本地迁移数据库及其释放函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本地订单事实写入和候选读取共用的数据库上下文。
	ctx := context.Background()
	// created、createErr 保存候选订单测试用户初始化结果。
	created, createErr := store.Users.Create(ctx, "rate-candidate-user", "rate-candidate@example.com", "pw")
	if createErr != nil || !created {
		t.Fatalf("创建测试用户失败: created=%v err=%v", created, createErr)
	}
	// user、userErr 保存账号归属所需用户身份及读取错误。
	user, userErr := store.Users.GetByUsername(ctx, "rate-candidate-user")
	if userErr != nil || user == nil {
		t.Fatalf("读取测试用户失败: user=%+v err=%v", user, userErr)
	}
	// primaryCookieErr 保存目标账号凭证归属写入错误。
	primaryCookieErr := store.Cookies.Save(ctx, "rate-candidate-account", "sid=primary", user.ID)
	// otherCookieErr 保存其他账号凭证归属写入错误。
	otherCookieErr := store.Cookies.Save(ctx, "rate-other-account", "sid=other", user.ID)
	if primaryCookieErr != nil || otherCookieErr != nil {
		t.Fatalf("保存测试账号失败: primary=%v other=%v", primaryCookieErr, otherCookieErr)
	}
	// completedErr 保存有完整确认收货事实的订单写入错误。
	completedErr := store.Orders.Upsert(ctx, "completed-order", OrderUpsertOpts{CookieID: "rate-candidate-account", OrderStatus: "completed"})
	// reviewedErr 保存买家评价已发生但不应触发自动评价的订单写入错误。
	reviewedErr := store.Orders.Upsert(ctx, "reviewed-order", OrderUpsertOpts{CookieID: "rate-candidate-account", OrderStatus: "reviewed"})
	// unmarkedErr 保存缺少确认收货时间的已完成订单写入错误。
	unmarkedErr := store.Orders.Upsert(ctx, "unmarked-order", OrderUpsertOpts{CookieID: "rate-candidate-account", OrderStatus: "completed"})
	// otherErr 保存其他账号已完成订单写入错误。
	otherErr := store.Orders.Upsert(ctx, "other-account-order", OrderUpsertOpts{CookieID: "rate-other-account", OrderStatus: "completed"})
	if completedErr != nil || reviewedErr != nil || unmarkedErr != nil || otherErr != nil {
		t.Fatalf("写入测试订单失败: completed=%v reviewed=%v unmarked=%v other=%v", completedErr, reviewedErr, unmarkedErr, otherErr)
	}
	// completedTimeErr 保存目标订单确认收货事实的写入错误。
	completedTimeErr := store.Automation.MarkOrderEventTime(ctx, "completed-order", "completed_at")
	// reviewedTimeErr 保存评价事实的写入错误，证明买家评价不是自动评价候选条件。
	reviewedTimeErr := store.Automation.MarkOrderEventTime(ctx, "reviewed-order", "buyer_reviewed_at")
	// otherTimeErr 保存其他账号订单确认收货事实的写入错误。
	otherTimeErr := store.Automation.MarkOrderEventTime(ctx, "other-account-order", "completed_at")
	if completedTimeErr != nil || reviewedTimeErr != nil || otherTimeErr != nil {
		t.Fatalf("写入订单事件时间失败: completed=%v reviewed=%v other=%v", completedTimeErr, reviewedTimeErr, otherTimeErr)
	}
	// candidates、candidateErr 保存目标账号的本地自动评价候选和查询错误。
	candidates, candidateErr := store.AccountTasks.DueAutoRateOrderIDs(ctx, "rate-candidate-account", 1)
	if candidateErr != nil || len(candidates) != 1 || candidates[0] != "completed-order" {
		t.Fatalf("本地自动评价候选错误: candidates=%v err=%v", candidates, candidateErr)
	}
}

// TestDueAutoRateOrderIDsSkipsOrdersThatCannotBeClaimedAgain 回归 issue #96：
// 累计已办完订单超过候选窗口后，新订单仍必须进入候选；仍可重试的失败记录继续保留。
func TestDueAutoRateOrderIDsSkipsOrdersThatCannotBeClaimedAgain(t *testing.T) {
	// store、cleanup 保存本地迁移数据库及其释放函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试共用的数据库上下文。
	ctx := context.Background()
	if created, err := store.Users.Create(ctx, "rate-window-user", "rate-window@example.com", "pw"); err != nil || !created { // created、err 保存测试用户初始化结果。
		t.Fatalf("创建测试用户失败: created=%v err=%v", created, err)
	}
	// user、userErr 保存账号归属用户。
	user, userErr := store.Users.GetByUsername(ctx, "rate-window-user")
	if userErr != nil || user == nil {
		t.Fatalf("读取测试用户失败: %v", userErr)
	}
	if cookieErr := store.Cookies.Save(ctx, "rate-window-account", "sid=window", user.ID); cookieErr != nil { // cookieErr 保存账号写入错误。
		t.Fatal(cookieErr)
	}
	// seed 写入一个已确认收货的订单；completedAt 控制它在候选窗口中的先后。
	seed := func(orderID, completedAt string) {
		t.Helper()
		if err := store.Orders.Upsert(ctx, orderID, OrderUpsertOpts{CookieID: "rate-window-account", OrderStatus: "completed"}); err != nil { // err 保存订单写入错误。
			t.Fatal(err)
		}
		if _, err := store.DB.ExecContext(ctx, `UPDATE orders SET completed_at=? WHERE order_id=?`, completedAt, orderID); err != nil { // err 保存确认收货时间写入错误。
			t.Fatal(err)
		}
	}
	// finish 为订单写入指定终态的评价运行记录。
	finish := func(orderID, status, message string, attempts int, nextRetryAt int64) {
		t.Helper()
		// insertErr 保存运行记录写入错误。
		if _, insertErr := store.DB.ExecContext(ctx, `INSERT INTO account_task_runs
			(run_key,cookie_id,task_type,target_id,run_date,status,error_message,attempt_count,next_retry_at,started_at,finished_at)
			VALUES (?,?,?,?,'',?,?,?,?,1,1)`, "rate:rate-window-account:"+orderID, "rate-window-account", "auto_rate", orderID, status, message, attempts, nextRetryAt); insertErr != nil {
			t.Fatal(insertErr)
		}
	}
	// 窗口为 200：先放入 200 个更早的已评价成功订单，把窗口占满。
	for index := 0; index < 200; index++ {
		// orderID 是第 index 个历史已评价订单，时间戳按序递增保证排在新订单之前。
		orderID := fmt.Sprintf("done-%03d", index)
		seed(orderID, fmt.Sprintf("2024-01-01 00:%02d:%02d", index/60, index%60))
		finish(orderID, "success", "", 1, 0)
	}
	// 其余订单均晚于历史订单：新订单、各类终态记录与可重试失败。
	seed("fresh-1", "2024-02-01 00:00:01")
	seed("fresh-2", "2024-02-01 00:00:02")
	seed("review", "2024-02-01 00:00:03")
	finish("review", "needs_review", "人工核对", 1, 0)
	seed("stuck-running", "2024-02-01 00:00:04")
	finish("stuck-running", "running", "", 1, 0)
	seed("no-retry", "2024-02-01 00:00:05")
	finish("no-retry", "failed", NoRetryErrorPrefix+": 超出期限", 1, 0)
	seed("exhausted", "2024-02-01 00:00:06")
	finish("exhausted", "failed", "平台错误", accountTaskMaxAttempts, 0)
	seed("cooling", "2024-02-01 00:00:07")
	finish("cooling", "failed", "平台错误", 1, 4102444800)
	seed("retryable", "2024-02-01 00:00:08")
	finish("retryable", "failed", "平台错误", 1, 1)

	// candidates、candidatesErr 保存窗口内的候选订单；必须只含两个新订单与可重试订单。
	candidates, candidatesErr := store.AccountTasks.DueAutoRateOrderIDs(ctx, "rate-window-account", 200)
	if candidatesErr != nil {
		t.Fatal(candidatesErr)
	}
	// want 是期望的候选集合，按完成时间升序。
	want := []string{"fresh-1", "fresh-2", "retryable"}
	if !reflect.DeepEqual(candidates, want) {
		t.Fatalf("候选应排除不可再领取的订单: got=%v want=%v", candidates, want)
	}
}

// TestAccountTaskStoreNoRetryRunIsNotReclaimed 验证账号任务永久失败记录不会被自动或人工入口再次抢占。
func TestAccountTaskStoreNoRetryRunIsNotReclaimed(t *testing.T) {
	// store、cleanup 保存本地迁移数据库及其释放函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是账号任务永久失败状态测试使用的数据库上下文。
	ctx := context.Background()
	// user、userErr 保存测试用户初始化结果。
	user, userErr := store.Users.Create(ctx, "no-retry-user", "no-retry-user@example.com", "pw")
	if userErr != nil || !user {
		t.Fatalf("创建测试用户失败: created=%v err=%v", user, userErr)
	}
	// cookieErr 保存测试账号初始化错误。
	if cookieErr := store.Cookies.Save(ctx, "no-retry-cookie", "sid=test", 1); cookieErr != nil {
		t.Fatal(cookieErr)
	}
	// run 描述一条评价任务运行记录。
	run := AccountTaskRun{RunKey: "rate:no-retry-cookie:old-order", CookieID: "no-retry-cookie", TaskType: "auto_rate", TargetID: "old-order"}
	// claimed、claimErr 保存永久失败运行的首次抢占结果。
	claimed, claimErr := store.AccountTasks.ClaimRun(ctx, run, 100)
	if claimErr != nil || !claimed {
		t.Fatalf("首次抢占失败: claimed=%v err=%v", claimed, claimErr)
	}
	// finishErr 保存永久失败终态及不可重试标记的写入错误。
	if finishErr := store.AccountTasks.FinishRun(ctx, run.RunKey, "failed", 0, 1, NoRetryErrorPrefix+": 超出30天的订单不允许评价", 0); finishErr != nil {
		t.Fatal(finishErr)
	}
	// scheduledClaim、scheduledErr 保存自动扫描再次抢占的结果。
	scheduledClaim, scheduledErr := store.AccountTasks.ClaimRun(ctx, run, 101)
	if scheduledErr != nil || scheduledClaim {
		t.Fatalf("永久失败记录不应自动重试: claimed=%v err=%v", scheduledClaim, scheduledErr)
	}
	// manualClaim、manualErr 保存人工立即入口再次抢占的结果。
	manualClaim, manualErr := store.AccountTasks.ClaimRunImmediately(ctx, run, 101)
	if manualErr != nil || manualClaim {
		t.Fatalf("永久失败记录不应被人工入口重新抢占: claimed=%v err=%v", manualClaim, manualErr)
	}
}
