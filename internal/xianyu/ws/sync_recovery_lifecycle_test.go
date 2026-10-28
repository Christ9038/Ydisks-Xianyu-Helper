package ws

import (
	"testing"
	"time"
)

// TestStartSyncRecoveryLifecycle 验证同步恢复的生命周期契约：
// 非恢复类 Push 不启动 goroutine；连续的恢复类 Push 合并执行；Close 会等待 goroutine 退出，关闭后不再启动新任务。
func TestStartSyncRecoveryLifecycle(t *testing.T) {
	// conn 是连到本地桩服务的连接，getState/ackDiff 均返回 200。
	conn := dialLocal(t, startScriptedAPIServer(t, nil, ""), Config{})
	// plain 是不含 syncExtraType 的普通 Push。
	plain := map[string]any{"body": map[string]any{}}
	// recovery 是要求恢复同步状态的 Push。
	recovery := map[string]any{"body": map[string]any{"syncExtraType": map[string]any{"type": float64(1)}}}

	conn.startSyncRecovery(plain)
	conn.syncMu.Lock()
	// running 表示普通 Push 之后是否错误地启动了恢复。
	running := conn.syncRunning
	conn.syncMu.Unlock()
	if running {
		t.Fatal("非恢复类 Push 不应启动同步恢复")
	}

	// 连续到达多条恢复类 Push：必须合并，不得因此并发启动多个 goroutine。
	for i := 0; i < 5; i++ {
		conn.startSyncRecovery(recovery)
	}
	// closeDone 在 Close 返回（含等待恢复 goroutine）后关闭。
	closeDone := make(chan struct{})
	go func() {
		_ = conn.Close()
		close(closeDone)
	}()
	select {
	case <-closeDone:
	case <-time.After(10 * time.Second):
		t.Fatal("Close 应等待同步恢复 goroutine 并在有限时间内返回")
	}

	conn.syncMu.Lock()
	// stillRunning、rerun 是关闭后残留的运行与补跑标记。
	stillRunning, rerun := conn.syncRunning, conn.syncRerun
	conn.syncMu.Unlock()
	if stillRunning || rerun {
		t.Fatalf("Close 返回后不应残留恢复任务：running=%v rerun=%v", stillRunning, rerun)
	}

	conn.startSyncRecovery(recovery)
	conn.syncMu.Lock()
	// startedAfterClose 表示关闭后是否又启动了恢复。
	startedAfterClose := conn.syncRunning
	conn.syncMu.Unlock()
	if startedAfterClose {
		t.Fatal("关闭后不得再启动同步恢复")
	}
}
