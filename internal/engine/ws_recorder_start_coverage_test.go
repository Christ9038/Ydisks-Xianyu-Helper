package engine

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// TestWSRecorderStartCoversCleanupQueueFlushAndCancellation 验证记录器启动后的清理、批量刷盘和取消收束路径。
func TestWSRecorderStartCoversCleanupQueueFlushAndCancellation(t *testing.T) {
	// account、store 和 cleanup 提供带真实 SQLite 存储的测试账号生命周期。
	account, _, store, cleanup := newAccountForTest(t)
	defer cleanup()
	// ctx 和 cancel 控制记录器 worker 的可回收生命周期。
	ctx, cancel := context.WithCancel(context.Background())
	// recorder 是绑定测试账号的 WebSocket 诊断记录器。
	recorder := newWSRecorder(store, account.CookieID, slog.Default())
	if recorder == nil {
		t.Fatal("应构造 WebSocket 记录器")
	}
	recorder.start(ctx)
	recorder.start(ctx)
	// message 是触发满批次刷盘的诊断报文。
	message := db.WSMessage{CookieID: account.CookieID, Direction: "in", RawText: "raw", ParseStatus: "ok"}
	// i 表示为触发整批刷盘而填充的诊断报文序号。
	for i := 0; i < WSRecordBatchSize; i++ {
		recorder.queue <- message
	}
	// deadline 限制异步 worker 的数据库刷盘等待时间，避免测试在异常时悬挂。
	deadline := time.Now().Add(2 * time.Second)
	for {
		// count 保存当前账号已经持久化的诊断报文数量。
		var count int
		// err 表示查询异步刷盘结果时遇到的数据库错误。
		if err := store.DB.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM ws_messages WHERE cookie_id=?", account.CookieID).Scan(&count); err != nil {
			t.Fatalf("查询诊断报文数量失败: %v", err)
		}
		if count >= WSRecordBatchSize {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("批量刷盘未完成，当前数量=%d", count)
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if !recorder.waitContext(context.Background()) {
		t.Fatal("取消后记录器应退出")
	}
}
