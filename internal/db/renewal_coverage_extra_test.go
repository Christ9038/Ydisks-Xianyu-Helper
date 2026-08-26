package db

import (
	"context"
	"errors"
	"testing"
)

// TestRenewalScheduleAndLogs 覆盖续期计划、三类日志和最近状态查询的确定性路径。
func TestRenewalScheduleAndLogs(t *testing.T) {
	// store、cleanup 提供迁移后的 SQLite 测试数据库。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试共用的数据库上下文。
	ctx := context.Background()
	// userID、cookieID 保存续期账号归属。
	_, cookieID := seedAccount(t, store)

	// missing、missingErr 验证不存在的续期计划返回领域未找到错误。
	missing, missingErr := store.Renewal.GetCookieRefreshSchedule(ctx, cookieID)
	if missing != nil || !errors.Is(missingErr, ErrNotFound) {
		t.Fatalf("missing=%+v err=%v", missing, missingErr)
	}
	// schedule 保存一条包含失败计数和状态信息的续期计划。
	schedule := CookieRefreshSchedule{CookieID: cookieID, ExpireAt: 100, Disabled: true, ConsecutiveFailures: 2, LastError: "error", LastStatus: "failed", LastErrorMessage: "message", LastRefreshAt: 90}
	// err 表示续期计划写入错误。
	if err := store.Renewal.UpsertCookieRefreshSchedule(ctx, schedule); err != nil {
		t.Fatal(err)
	}
	// loaded、loadedErr 保存重新读取的续期计划。
	loaded, loadedErr := store.Renewal.GetCookieRefreshSchedule(ctx, cookieID)
	if loadedErr != nil || loaded == nil || !loaded.Disabled || loaded.ConsecutiveFailures != 2 || loaded.LastStatus != "failed" {
		t.Fatalf("loaded=%+v err=%v", loaded, loadedErr)
	}

	// log 保存三类续期日志共用的输入字段。
	log := RenewalLog{BatchID: "batch", CookieID: cookieID, Status: "success", Message: "完成", UpdatedCookieNames: []string{"a", "b"}, RenewMethod: "test", DurationMS: 3, RequestCount: 1}
	// err 表示浏览器续期日志写入错误。
	if err := store.Renewal.AddBrowserCookieRenewLog(ctx, log); err != nil {
		t.Fatal(err)
	}
	// err 表示登录续期日志写入错误。
	if err := store.Renewal.AddLoginRenewLog(ctx, log); err != nil {
		t.Fatal(err)
	}
	// err 表示 API Cookie 续期日志写入错误。
	if err := store.Renewal.AddAPICookieRenewLog(ctx, log); err != nil {
		t.Fatal(err)
	}
	// apiStatuses、apiStatusErr 保存 API 续期最近状态。
	apiStatuses, apiStatusErr := store.Renewal.RecentAPICookieRenewStatuses(ctx, cookieID, 5)
	if apiStatusErr != nil || len(apiStatuses) != 1 || apiStatuses[0] != "success" {
		t.Fatalf("api statuses=%v err=%v", apiStatuses, apiStatusErr)
	}
	// browserStatuses、browserStatusErr 保存浏览器续期最近状态。
	browserStatuses, browserStatusErr := store.Renewal.RecentBrowserCookieRenewStatuses(ctx, cookieID, 5)
	if browserStatusErr != nil || len(browserStatuses) != 1 || browserStatuses[0] != "success" {
		t.Fatalf("browser statuses=%v err=%v", browserStatuses, browserStatusErr)
	}
	// zeroStatuses、zeroErr 验证非正限制直接返回空结果。
	zeroStatuses, zeroErr := store.Renewal.RecentAPICookieRenewStatuses(ctx, cookieID, 0)
	if zeroErr != nil || zeroStatuses != nil {
		t.Fatalf("zero statuses=%v err=%v", zeroStatuses, zeroErr)
	}
	// firstMessage、fallbackMessage 验证日志错误信息优先级和空值回退。
	if firstNonEmpty("", "  ", "first", "second") != "first" || firstNonEmpty("", " ") != "" {
		t.Fatal("firstNonEmpty result incorrect")
	}
	// err 表示正 retentionDays 的日志清理结果。
	if err := store.Renewal.CleanupLogs(ctx, 1); err != nil {
		t.Fatal(err)
	}
	// err 表示非正 retentionDays 的清理应安全跳过。
	if err := store.Renewal.CleanupLogs(ctx, 0); err != nil {
		t.Fatal(err)
	}
}
