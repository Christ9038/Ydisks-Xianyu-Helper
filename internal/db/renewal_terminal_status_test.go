package db

import (
	"context"
	"reflect"
	"testing"
)

// TestRecentTerminalAPICookieRenewStatusesSkipsNonTerminal 验证终态查询排除 skipped 与 pending，
// 保持最新在前，并且不改变原有查询对全部状态的返回；t 管理断言。
func TestRecentTerminalAPICookieRenewStatusesSkipsNonTerminal(t *testing.T) {
	// store、cleanup 提供迁移后的 SQLite 测试数据库。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试共用的数据库上下文。
	ctx := context.Background()
	// cookieID 是写入续期日志的账号标识。
	_, cookieID := seedAccount(t, store)
	// statuses 按时间先后写入，覆盖疲劳窗口导致的 skipped 夹在两次失败之间的场景。
	for _, status := range []string{"success", "failed", "skipped", "pending", "failed", "skipped", "failed"} {
		// err 是 store.Renewal.AddAPICookieRenewLog 返回的失败原因，用于断言成功或被拒绝的路径。
		if err := store.Renewal.AddAPICookieRenewLog(ctx, RenewalLog{BatchID: "b", CookieID: cookieID, Status: status, RenewMethod: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	// terminal、terminalErr 是只含终态的最近状态，最新在前。
	terminal, terminalErr := store.Renewal.RecentTerminalAPICookieRenewStatuses(ctx, cookieID, 4)
	if terminalErr != nil || !reflect.DeepEqual(terminal, []string{"failed", "failed", "failed", "success"}) {
		t.Fatalf("terminal=%v err=%v", terminal, terminalErr)
	}
	// all、allErr 是原有查询，仍返回全部状态，保持向后兼容。
	all, allErr := store.Renewal.RecentAPICookieRenewStatuses(ctx, cookieID, 4)
	if allErr != nil || !reflect.DeepEqual(all, []string{"failed", "skipped", "failed", "pending"}) {
		t.Fatalf("all=%v err=%v", all, allErr)
	}
	// zero、zeroErr 验证非正限制直接返回空结果。
	if zero, zeroErr := store.Renewal.RecentTerminalAPICookieRenewStatuses(ctx, cookieID, 0); zeroErr != nil || zero != nil {
		t.Fatalf("zero=%v err=%v", zero, zeroErr)
	}
	// other 是没有任何日志的账号，应返回空切片而不是错误。
	if other, otherErr := store.Renewal.RecentTerminalAPICookieRenewStatuses(ctx, "no-such-account", 3); otherErr != nil || len(other) != 0 {
		t.Fatalf("other=%v err=%v", other, otherErr)
	}
}
