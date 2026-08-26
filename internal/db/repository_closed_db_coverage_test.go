package db

import (
	"context"
	"testing"
)

// TestAccountLoginLogsCoversClosedDatabaseOperations 验证登录审计仓储在数据库关闭后的读写错误传播。
func TestAccountLoginLogsCoversClosedDatabaseOperations(t *testing.T) {
	// store 是随后主动关闭数据库连接的测试存储。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// closeErr 表示主动关闭测试数据库连接时的资源释放错误。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// ctx 是本测试数据库操作使用的非取消上下文。
	ctx := context.Background()
	// addErr 表示关闭数据库后写入登录审计记录的基础设施错误。
	if addErr := store.LoginLogs.Add(ctx, AccountLoginLog{CookieID: "cid", UserID: 1, Method: "qr", Status: "failed"}); addErr == nil {
		t.Fatal("关闭数据库后登录审计写入不应成功")
	}
	// listErr 表示关闭数据库后读取登录审计记录的基础设施错误。
	if _, listErr := store.LoginLogs.ListByCookie(ctx, "cid", 0); listErr == nil {
		t.Fatal("关闭数据库后登录审计读取不应成功")
	}
}

// TestAccountTaskStoreCoversClosedDatabaseOperations 验证账号任务仓储所有数据库端点传播关闭连接错误。
func TestAccountTaskStoreCoversClosedDatabaseOperations(t *testing.T) {
	// store 是随后主动关闭数据库连接的测试存储。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// closeErr 表示主动关闭测试数据库连接时的资源释放错误。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// ctx 是本测试数据库操作使用的非取消上下文。
	ctx := context.Background()
	// run 是覆盖账号任务运行端点所需的最小运行记录。
	run := AccountTaskRun{RunKey: "closed-run", CookieID: "cid", TaskType: "rate", RunDate: "2026-08-26"}
	// operations 保存关闭数据库后各账号任务端点的错误结果。
	operations := []error{
		func() error {
			// err 表示任务设置读取在关闭数据库后的底层错误。
			_, err := store.AccountTasks.Get(ctx, "cid")
			return err
		}(),
		store.AccountTasks.Upsert(ctx, AccountTaskSettings{CookieID: "cid"}),
		func() error {
			// err 表示启用任务列表在关闭数据库后的底层错误。
			_, err := store.AccountTasks.Enabled(ctx)
			return err
		}(),
		func() error {
			// err 表示普通任务领取在关闭数据库后的底层错误。
			_, err := store.AccountTasks.ClaimRun(ctx, run, 1)
			return err
		}(),
		func() error {
			// err 表示立即任务领取在关闭数据库后的底层错误。
			_, err := store.AccountTasks.ClaimRunImmediately(ctx, run, 1)
			return err
		}(),
		store.AccountTasks.FinishRun(ctx, run.RunKey, "failed", 0, 1, "error", 2),
		store.AccountTasks.MarkRateScan(ctx, "cid", 1),
		store.AccountTasks.MarkPolished(ctx, "cid", "2026-08-26", 1),
		func() error {
			// err 表示任务历史读取在关闭数据库后的底层错误。
			_, err := store.AccountTasks.RecentRuns(ctx, "cid", 0)
			return err
		}(),
	}
	// operation 表示当前待验证的账号任务端点错误。
	for _, operation := range operations {
		if operation == nil {
			t.Fatal("关闭数据库后账号任务操作不应成功")
		}
	}
}

// TestCookieRepositoryCoversClosedDatabaseOperations 验证 Cookie 仓储在数据库关闭后统一传播基础设施错误。
func TestCookieRepositoryCoversClosedDatabaseOperations(t *testing.T) {
	// store 是随后主动关闭数据库连接的测试存储。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// closeErr 表示主动关闭测试数据库连接时的资源释放错误。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// ctx 是本测试数据库操作使用的非取消上下文。
	ctx := context.Background()
	// operations 保存关闭数据库后各 Cookie 仓储端点的错误结果。
	operations := []error{
		store.Cookies.CreateOwned(ctx, "closed-cookie", "sid=1", 1),
		store.Cookies.UpdateValueOwned(ctx, "closed-cookie", "sid=2", 1),
		store.Cookies.UpdateValueExisting(ctx, "closed-cookie", "sid=3"),
		store.Cookies.Delete(ctx, "closed-cookie"),
		func() error {
			// err 表示关闭数据库后 Cookie 值读取的基础设施错误。
			_, err := store.Cookies.GetValue(ctx, "closed-cookie")
			return err
		}(),
		func() error {
			// err 表示关闭数据库后 Cookie 详情读取的基础设施错误。
			_, err := store.Cookies.GetDetails(ctx, "closed-cookie")
			return err
		}(),
	}
	// operation 表示当前待验证的 Cookie 仓储错误结果。
	for _, operation := range operations {
		if operation == nil {
			t.Fatal("关闭数据库后 Cookie 操作不应成功")
		}
	}
}
