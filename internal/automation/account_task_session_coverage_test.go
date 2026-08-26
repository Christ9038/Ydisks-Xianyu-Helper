package automation

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

// TestAccountTaskSessionAndCookiePersistence 验证账号任务会话阻断指纹和响应 Cookie 收口分支。
func TestAccountTaskSessionAndCookiePersistence(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是本测试所有数据库调用共用的非取消上下文。
	ctx := context.Background()
	// sender 保存响应 Cookie 同步到在线运行时的测试发送器。
	sender := &testSender{}
	// coordinator 是绑定任务仓储和测试发送器的账号任务协调器。
	coordinator := &accountTaskCoordinator{repository: newStoreAccountTaskRepository(store), senders: testSenderProvider{sender: sender}, logger: slog.Default()}
	// unchangedValue、unchangedErr 保存空响应或相同 Cookie 的幂等结果。
	unchangedValue, unchangedErr := coordinator.persistTaskCookies(ctx, "cid", "sid=old", " sid=old ")
	if unchangedErr != nil || unchangedValue != "sid=old" || len(sender.cookieUpdates) != 0 {
		t.Fatalf("unchanged value=%q err=%v updates=%v", unchangedValue, unchangedErr, sender.cookieUpdates)
	}
	// updatedValue、updatedErr 保存新 Cookie 的本地持久化和运行时同步结果。
	updatedValue, updatedErr := coordinator.persistTaskCookies(ctx, "cid", "sid=old", " sid=new ")
	if updatedErr != nil || updatedValue != "sid=new" || len(sender.cookieUpdates) != 1 || sender.cookieUpdates[0] != "sid=new" {
		t.Fatalf("updated value=%q err=%v updates=%v", updatedValue, updatedErr, sender.cookieUpdates)
	}
	// fingerprint、fingerprintErr 保存当前平台凭证的不可逆阻断指纹。
	fingerprint, fingerprintErr := coordinator.accountCredentialFingerprint(ctx, "cid")
	if fingerprintErr != nil || fingerprint == "" {
		t.Fatalf("fingerprint=%q err=%v", fingerprint, fingerprintErr)
	}
	coordinator.sessionExpired.Store("cid", fingerprint)
	// blocked、blockedErr 保存凭证未变化时的会话阻断结果。
	blocked, blockedErr := coordinator.accountTaskSessionBlocked(ctx, "cid")
	if blockedErr != nil || !blocked {
		t.Fatalf("blocked=%v err=%v", blocked, blockedErr)
	}
	// replaceErr 保存模拟续期替换 Cookie 的错误。
	replaceErr := store.Cookies.UpdateValueExisting(ctx, "cid", "sid=renewed")
	if replaceErr != nil {
		t.Fatal(replaceErr)
	}
	// unblocked、unblockedErr 保存凭证变化后的会话解除结果。
	unblocked, unblockedErr := coordinator.accountTaskSessionBlocked(ctx, "cid")
	if unblockedErr != nil || unblocked {
		t.Fatalf("unblocked=%v err=%v", unblocked, unblockedErr)
	}
	// closedStoreCoordinator 是底层数据库关闭后的错误传播测试对象。
	closedStoreCoordinator := &accountTaskCoordinator{repository: newStoreAccountTaskRepository(store), senders: testSenderProvider{sender: sender}, logger: slog.Default()}
	// closeErr 保存关闭测试数据库的错误。
	closeErr := store.DB.Close()
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	// failedValue、failedErr 保存 Cookie 写回失败时的旧值和包装错误。
	failedValue, failedErr := closedStoreCoordinator.persistTaskCookies(ctx, "cid", "sid=renewed", "sid=failed")
	if failedValue != "sid=renewed" || failedErr == nil || errors.Is(failedErr, context.Canceled) {
		t.Fatalf("failed value=%q err=%v", failedValue, failedErr)
	}
}
