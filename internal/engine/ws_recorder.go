package engine

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"xianyu-go/internal/db"
)

// wsRecorder 负责 WebSocket 报文诊断记录的有界队列和后台写入生命周期。
// once 保护单次启动；done 是写入 worker 的 Context-aware Join 信号，组件锁不覆盖数据库 I/O。
// wsRecorder 用于本次流程后续判断的wsRecorder
type wsRecorder struct {
	// store 提供 WebSocket 报文持久化 repository。
	store *db.Store
	// cookieID 是报文所属账号标识。
	cookieID string
	// logger 记录队列丢弃和数据库写入错误。
	logger *slog.Logger
	// once 保证同一个账号运行时只启动一个 recorder worker。
	once sync.Once
	// done 在 recorder worker 退出时关闭，供 Context-aware 等待直接选择而不创建旁路 goroutine。
	done chan struct{}
	// started 表示 recorder worker 是否实际启动；未启动的 recorder 无需等待。
	started atomic.Bool
	// queue 是有界的报文内存队列，满时丢弃诊断记录而不阻塞 WS。
	queue chan db.WSMessage
}

// newWSRecorder 构造 WebSocket 报文记录组件；数据库能力缺失时返回 nil。
func newWSRecorder(store *db.Store, cookieID string, logger *slog.Logger) *wsRecorder {
	if store == nil || store.WSMessages == nil {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &wsRecorder{
		store:    store,
		cookieID: cookieID,
		logger:   logger,
		queue:    make(chan db.WSMessage, 256),
		done:     make(chan struct{}),
	}
}

// callback 返回供 WebSocket 底层写入报文的非阻塞回调。
func (r *wsRecorder) callback() func(direction, rawText, parsedJSON, parseStatus, errMsg string) {
	if r == nil || r.queue == nil {
		return nil
	}
	return func(direction, rawText, parsedJSON, parseStatus, errMsg string) {
		// message 是当前待写入的 WebSocket 诊断报文。
		message := db.WSMessage{
			CookieID:    r.cookieID,
			Direction:   direction,
			RawText:     rawText,
			ParsedJSON:  parsedJSON,
			MessageKind: "",
			ParseStatus: parseStatus,
			Error:       errMsg,
		}
		select {
		case r.queue <- message:
		default:
			r.logger.Warn("WS 报文记录队列已满，丢弃诊断记录", "cookie_id", r.cookieID, "direction", direction)
		}
	}
}

// start 启动 recorder worker；重复调用只保留一个 worker。
func (r *wsRecorder) start(ctx context.Context) {
	if r == nil || r.store == nil || r.queue == nil {
		return
	}
	r.once.Do(func() {
		r.started.Store(true)
		go func() {
			defer close(r.done)
			// cleanupCtx 是清理历史报文时的有限时长上下文。
			cleanupCtx, cleanupCancel := context.WithTimeout(ctx, WSRecordWriteTimeout)
			// deleted 是本次清理删除的历史报文数量。
			deleted, cleanupErr := r.store.WSMessages.DeleteBefore(cleanupCtx, r.cookieID, time.Now().Add(-WSRecordRetention))
			cleanupCancel()
			if cleanupErr != nil && ctx.Err() == nil {
				r.logger.Warn("清理过期 WS 报文失败", "cookie_id", r.cookieID, "err", cleanupErr)
			} else if deleted > 0 {
				r.logger.Debug("已清理过期 WS 报文", "cookie_id", r.cookieID, "deleted", deleted)
			}

			// ticker 定期触发队列刷盘。
			ticker := time.NewTicker(WSRecordFlushInterval)
			defer ticker.Stop()
			// batch 是当前批次待写入的报文。
			batch := make([]db.WSMessage, 0, WSRecordBatchSize)
			// flush 将当前批次写入 repository，I/O 不在组件锁内执行。
			flush := func() {
				if len(batch) == 0 {
					return
				}
				// writeCtx 是本次批量写入的有限时长上下文。
				writeCtx, cancel := context.WithTimeout(ctx, WSRecordWriteTimeout)
				// err 是批量写入失败的原因。
				err := r.store.WSMessages.AddBatch(writeCtx, batch)
				cancel()
				if err != nil && ctx.Err() == nil {
					r.logger.Warn("记录 WS 报文失败", "cookie_id", r.cookieID, "count", len(batch), "err", err)
				}
				batch = batch[:0]
			}
			for {
				select {
				case <-ctx.Done():
					return
				case // message 用于本次流程后续判断的消息
				message := <-r.queue:
					batch = append(batch, message)
					if len(batch) >= WSRecordBatchSize {
						flush()
					}
				case <-ticker.C:
					flush()
				}
			}
		}()
	})
}

// waitContext 在 ctx 约束内等待 recorder worker 退出；数据库写入异常阻塞时及时把停止超时交给调用方。
func (r *wsRecorder) waitContext(ctx context.Context) bool {
	if r == nil {
		return true
	}
	if !r.started.Load() {
		return true
	}
	if ctx == nil {
		return false
	}
	// done 表示 recorder worker 已经退出，供当前等待调用选择上下文超时或正常完成。
	done := r.done
	if done == nil {
		return true
	}
	select {
	case <-done:
		return true
	case <-ctx.Done():
		return false
	}
}
