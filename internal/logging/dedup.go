package logging

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
)

const (
	// dedupWindow 是统计重复日志的滚动窗口长度。
	dedupWindow = time.Minute
	// dedupWarnLimit 是窗口内同一事件放行的 Warn 条数，超出部分只计数。
	dedupWarnLimit = 5
	// dedupErrorLimit 是窗口内同一事件放行的 Error 条数；比 Warn 更宽，避免真正的故障被压得过狠。
	dedupErrorLimit = 20
	// dedupSweepThreshold 是触发过期条目清理的状态表大小，限制抑制状态的内存占用。
	dedupSweepThreshold = 512
)

// dedupKey 标识“同一个重复事件”：同一调用点、等级、消息与关键业务标识。
type dedupKey string

// dedupEntry 保存某个重复事件在当前窗口内的统计。
type dedupEntry struct {
	// windowStart 是当前窗口的起点。
	windowStart time.Time
	// emitted 是当前窗口内已放行的条数。
	emitted int
	// suppressed 是当前窗口内被抑制的条数。
	suppressed int
	// level 与 message 用于生成汇总记录。
	level   slog.Level
	message string
	// caller 是首次放行记录的调用位置，汇总时回填，便于定位噪音来源。
	caller string
}

// dedupState 是同一进程内所有 dedupHandler 共享的抑制状态。
// 并发约束：mu 保护 entries 与其中的全部字段；持锁期间不得写日志或执行 I/O。
type dedupState struct {
	// mu 保护 entries。
	mu sync.Mutex
	// entries 以 dedupKey 为键保存各事件的窗口统计。
	entries map[dedupKey]*dedupEntry
	// now 返回当前时间，测试可替换。
	now func() time.Time
}

// newDedupState 创建抑制状态，now 为空时使用系统时钟。
func newDedupState(now func() time.Time) *dedupState {
	if now == nil {
		now = time.Now
	}
	return &dedupState{entries: make(map[dedupKey]*dedupEntry), now: now}
}

// dedupSummary 是窗口结束后需要补记的“已抑制重复日志”信息。
type dedupSummary struct {
	// level、message、caller 来自被抑制的原事件。
	level   slog.Level
	message string
	caller  string
	// suppressed 是窗口内被抑制的条数，first 是窗口起点。
	suppressed int
	first      time.Time
}

// admit 判断一条 Warn/Error 记录是否放行，并返回需要补记的过期窗口汇总。
// Info 及以下等级不参与抑制，因为它们常是按对象区分的审计或摘要行。
func (s *dedupState) admit(key dedupKey, level slog.Level, message, caller string) (bool, []dedupSummary) {
	if level < slog.LevelWarn {
		return true, nil
	}
	// limit 是该等级在窗口内的放行上限。
	limit := dedupWarnLimit
	if level >= slog.LevelError {
		limit = dedupErrorLimit
	}
	// now、summaries 是当前时间与本次调用顺带收集的过期汇总。
	now := s.now()
	// summaries 收集本次调用顺带发现的已过期窗口汇总，由调用方补记。
	var summaries []dedupSummary
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.entries) >= dedupSweepThreshold {
		summaries = append(summaries, s.sweepLocked(now)...)
	}
	// entry 是该事件当前的窗口统计。
	entry := s.entries[key]
	if entry != nil && now.Sub(entry.windowStart) >= dedupWindow {
		if entry.suppressed > 0 {
			summaries = append(summaries, entry.summary())
		}
		delete(s.entries, key)
		entry = nil
	}
	if entry == nil {
		entry = &dedupEntry{windowStart: now, level: level, message: message, caller: caller}
		s.entries[key] = entry
	}
	if entry.emitted < limit {
		entry.emitted++
		return true, summaries
	}
	entry.suppressed++
	return false, summaries
}

// summary 把过期窗口统计转换为汇总记录信息。
func (e *dedupEntry) summary() dedupSummary {
	return dedupSummary{level: e.level, message: e.message, caller: e.caller, suppressed: e.suppressed, first: e.windowStart}
}

// sweepLocked 清理已过期的条目并返回它们的汇总；调用方必须持有 mu。
func (s *dedupState) sweepLocked(now time.Time) []dedupSummary {
	// summaries 保存被清理且有抑制记录的事件汇总。
	var summaries []dedupSummary
	// key、entry 是当前检查的状态表条目。
	for key, entry := range s.entries {
		if now.Sub(entry.windowStart) < dedupWindow {
			continue
		}
		if entry.suppressed > 0 {
			summaries = append(summaries, entry.summary())
		}
		delete(s.entries, key)
	}
	return summaries
}

// dedupHandler 抑制同一事件的高频重复 Warn/Error，窗口过期后在下一次触发时补记汇总。
// 汇总是惰性补记的：没有后续同类事件或清理触发时不会产生额外 goroutine 和定时器。
type dedupHandler struct {
	// next 是实际处理记录的下一层处理器。
	next slog.Handler
	// state 是进程内共享的抑制状态。
	state *dedupState
	// bound 保存 logger 绑定的已展开属性，参与去重键构造（例如 account）。
	bound []slog.Attr
}

// newDedupHandler 创建重复抑制处理器；state 为空时创建独立状态。
func newDedupHandler(next slog.Handler, state *dedupState) slog.Handler {
	if state == nil {
		state = newDedupState(nil)
	}
	return &dedupHandler{next: next, state: state}
}

// Enabled 沿用下一层的等级判断。
func (h *dedupHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// dedupKeyAttrs 是参与去重键的业务标识属性：它们不同的记录属于不同事件，不能互相抑制。
var dedupKeyAttrs = map[string]struct{}{
	"account": {}, "account_id": {}, "cookie_id": {}, "order_id": {}, "item_id": {}, "chat_id": {},
	"run_id": {}, "rule_id": {}, "path": {}, "route": {}, "api": {}, "err": {}, "task": {}, "job": {},
}

// Handle 判断记录是否放行；被抑制的记录直接丢弃，过期窗口先补记汇总。
func (h *dedupHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.Level < slog.LevelWarn {
		return h.next.Handle(ctx, record)
	}
	// builder 拼接去重键：调用点、等级、消息和业务标识值。
	var builder strings.Builder
	builder.WriteString(record.Message)
	builder.WriteByte('|')
	builder.WriteString(record.Level.String())
	builder.WriteByte('|')
	// caller 是调用位置（文件:行号），既用于汇总输出，也作为去重键；
	// 不直接用 PC，因为函数被内联到多个调用点时同一源码行会对应多个 PC。
	caller := ""
	// attrs 是记录调用位置的解析结果，第一项为 caller。
	if attrs := callerAttrs(record.PC); len(attrs) > 0 {
		caller = attrs[0].Value.String()
	}
	builder.WriteString(caller)
	// collect 把业务标识属性值追加到去重键。
	collect := func(attr slog.Attr) {
		// ok 表示该属性键属于参与去重键的业务标识集合。
		if _, ok := dedupKeyAttrs[attr.Key]; ok {
			builder.WriteByte('|')
			builder.WriteString(attr.Key)
			builder.WriteByte('=')
			builder.WriteString(attr.Value.String())
		}
	}
	// bound 是 logger 绑定属性，attr 是其中的单个属性。
	for _, attr := range h.bound {
		collect(attr)
	}
	record.Attrs(func(attr slog.Attr) bool {
		collect(attr)
		return true
	})
	// allowed 表示该条记录是否被放行。summaries 收集本次调用顺带发现的已过期窗口汇总，由调用方补记。
	allowed, summaries := h.state.admit(dedupKey(builder.String()), record.Level, record.Message, caller)
	// summary 是窗口结束后补记的“已抑制重复日志”。
	for _, summary := range summaries {
		// summaryRecord 是补记的“已抑制重复日志”记录，调用位置通过 caller 属性显式给出。
		summaryRecord := slog.NewRecord(h.state.now(), slog.LevelWarn, "已抑制重复日志", 0)
		summaryRecord.AddAttrs(
			slog.String("original_message", summary.message),
			slog.String("original_level", summary.level.String()),
			slog.String(CallerKey, summary.caller),
			slog.Int("suppressed", summary.suppressed),
			slog.Time("first_at", summary.first),
			slog.Duration("window", dedupWindow),
		)
		_ = h.next.Handle(ctx, summaryRecord)
	}
	if !allowed {
		return nil
	}
	return h.next.Handle(ctx, record)
}

// WithAttrs 记住绑定属性以参与去重键，并向下层透传。
func (h *dedupHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// combined 是历史绑定属性与新增属性的合并副本。
	combined := make([]slog.Attr, 0, len(h.bound)+len(attrs))
	combined = append(combined, h.bound...)
	combined = append(combined, attrs...)
	return &dedupHandler{next: h.next.WithAttrs(attrs), state: h.state, bound: combined}
}

// WithGroup 向下层打开分组；分组内属性不参与去重键。
func (h *dedupHandler) WithGroup(name string) slog.Handler {
	return &dedupHandler{next: h.next.WithGroup(name), state: h.state, bound: h.bound}
}
