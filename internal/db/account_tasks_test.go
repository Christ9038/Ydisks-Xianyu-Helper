package db

import (
	"context"
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
	if recentErr != nil || len(recent) != 1 || recent[0].RunKey != run.RunKey || recent[0].Status != "running" {
		t.Fatalf("任务历史异常: runs=%+v err=%v", recent, recentErr)
	}
}
