package engine

import (
	"context"
	"sync/atomic"
	"testing"

	"xianyu-go/internal/db"
)

// TestWSRecorderCoversUninitializedBoundaries 验证 WebSocket 报文记录器在未装配存储、队列和 Context 时快速返回。
func TestWSRecorderCoversUninitializedBoundaries(t *testing.T) {
	// nilRecorder 保存空接收者，覆盖所有不应触发 I/O 的入口。
	var nilRecorder *wsRecorder
	// callback 保存空记录器返回的报文回调。
	if callback := nilRecorder.callback(); callback != nil {
		t.Fatal("空记录器不应返回回调")
	}
	nilRecorder.start(context.Background())
	if !nilRecorder.waitContext(context.Background()) {
		t.Fatal("空记录器等待应立即完成")
	}
	// emptyQueueRecorder 保存未装配队列的记录器。
	emptyQueueRecorder := &wsRecorder{}
	// callback 保存未装配队列时返回的报文回调。
	if callback := emptyQueueRecorder.callback(); callback != nil {
		t.Fatal("未装配队列不应返回回调")
	}
	emptyQueueRecorder.start(context.Background())
	if !emptyQueueRecorder.waitContext(context.Background()) {
		t.Fatal("未启动记录器等待应立即完成")
	}
	// emptyStoreRecorder 保存缺少 WebSocket 存储端口的记录器。
	emptyStoreRecorder := newWSRecorder(&db.Store{}, "account", nil)
	if emptyStoreRecorder != nil {
		t.Fatal("缺少 WebSocket 存储时不应构造记录器")
	}
	// contextlessRecorder 保存已标记启动但没有等待 Context 的记录器。
	contextlessRecorder := &wsRecorder{}
	contextlessRecorder.started = atomic.Bool{}
	contextlessRecorder.started.Store(true)
	// nilContext 保存用于验证空 Context 防御的显式接口变量。
	var nilContext context.Context
	if contextlessRecorder.waitContext(nilContext) {
		t.Fatal("缺少等待 Context 不应伪造完成")
	}
}
