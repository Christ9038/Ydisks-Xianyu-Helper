package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"xianyu-go/internal/logsafe"
)

// Level is the process-wide dynamic slog level.
// Level 用于本次流程后续判断的Level
var Level slog.LevelVar

// init 封装init业务协调。
func init() {
	Level.Set(slog.LevelInfo)
}

// NewLogger creates a slog logger wired to the dynamic Level.
// NewLogger 封装NewLogger业务协调。
func NewLogger(w io.Writer, format string) *slog.Logger {
	// opts 用于本次流程后续判断的opts
	opts := &slog.HandlerOptions{Level: &Level}
	// handler 保存底层格式化处理器，并在写出前统一清理日志属性中的敏感数据。
	var handler slog.Handler
	if strings.EqualFold(strings.TrimSpace(format), "json") {
		handler = slog.NewJSONHandler(w, opts)
	} else {
		handler = slog.NewTextHandler(w, opts)
	}
	return slog.New(newRedactingHandler(handler))
}

// redactingHandler 在底层 slog 处理器前统一移除日志中的平台凭证和诊断秘密。
// 它只改变敏感属性值与错误文本，不改变业务日志消息和非敏感属性的语义。
type redactingHandler struct {
	// next 保存实际负责文本或 JSON 编码的底层处理器。
	next slog.Handler
}

// newRedactingHandler 创建带有集中式敏感信息清理策略的日志处理器。
func newRedactingHandler(next slog.Handler) slog.Handler {
	// handler 保存调用方传入的底层处理器；空值时使用标准丢弃处理器保持构造安全。
	if next == nil {
		next = slog.DiscardHandler
	}
	return &redactingHandler{next: next}
}

// Enabled 保持底层处理器的等级过滤策略，避免为被过滤的日志提前解析属性。
func (h *redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle 清理日志消息和全部嵌套属性后交给底层格式化处理器写出。
func (h *redactingHandler) Handle(ctx context.Context, record slog.Record) error {
	// sanitized 保存重建后的日志记录，避免修改 slog.Logger 仍在共享的原始记录。
	sanitized := slog.NewRecord(record.Time, record.Level, logsafe.Text(record.Message), record.PC)
	// attrs 保存经过递归清理的记录属性。
	attrs := make([]slog.Attr, 0, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, redactAttr(attr))
		return true
	})
	sanitized.AddAttrs(attrs...)
	return h.next.Handle(ctx, sanitized)
}

// WithAttrs 清理创建 logger 时绑定的属性，防止它们绕过单条记录的处理路径。
func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// sanitized 保存绑定属性的脱敏副本，底层处理器可以安全持有该切片。
	sanitized := make([]slog.Attr, 0, len(attrs))
	// attr 表示当前待绑定且需要先清理的日志属性。
	for _, attr := range attrs {
		sanitized = append(sanitized, redactAttr(attr))
	}
	return &redactingHandler{next: h.next.WithAttrs(sanitized)}
}

// WithGroup 保留底层处理器的分组语义，并继续对分组内属性执行统一清理。
func (h *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{next: h.next.WithGroup(name)}
}

// redactAttr 按属性键和属性类型清理单个日志属性，包括嵌套分组和错误值。
func redactAttr(attr slog.Attr) slog.Attr {
	// value 保存解析 LogValuer 后的属性值，确保自定义值也经过相同的安全策略。
	value := attr.Value.Resolve()
	if sensitiveAttrKey(attr.Key) {
		return slog.String(attr.Key, "<redacted>")
	}
	switch value.Kind() {
	case slog.KindString:
		return slog.String(attr.Key, logsafe.Text(value.String()))
	case slog.KindGroup:
		// groupAttrs 保存递归清理后的分组属性。
		groupAttrs := value.Group()
		// redacted 保存分组中每个属性的脱敏结果。
		redacted := make([]slog.Attr, 0, len(groupAttrs))
		// child 表示当前分组中的原始子属性。
		for _, child := range groupAttrs {
			redacted = append(redacted, redactAttr(child))
		}
		return slog.GroupAttrs(attr.Key, redacted...)
	case slog.KindAny:
		return redactAnyAttr(attr.Key, value.Any())
	default:
		return slog.Attr{Key: attr.Key, Value: value}
	}
}

// redactAnyAttr 清理 slog.Any 属性；未知复合类型整体替换，避免其自定义序列化泄露秘密。
func redactAnyAttr(key string, value any) slog.Attr {
	if value == nil {
		return slog.Any(key, nil)
	}
	// err、ok 表示属性是否为错误接口以及对应的断言结果。
	if err, ok := value.(error); ok {
		return slog.String(key, logsafe.Error(err))
	}
	// typed 表示已识别的基础属性类型，用于保留其原有结构化输出。
	switch typed := value.(type) {
	case string:
		return slog.String(key, logsafe.Text(typed))
	case []byte:
		return slog.String(key, logsafe.Text(string(typed)))
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, uintptr, float32, float64:
		return slog.Any(key, typed)
	default:
		return slog.String(key, "<redacted>")
	}
}

// sensitiveAttrKey 判断属性名是否代表必须整体隐藏的凭证或秘密。
func sensitiveAttrKey(key string) bool {
	// normalized 保存统一分隔符和大小写后的属性名，便于兼容 snake_case、kebab-case 和点号命名。
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.NewReplacer("-", "_", ".", "_", " ", "_").Replace(normalized)
	if normalized == "cookie_count" || strings.HasSuffix(normalized, "_cookie_count") || strings.HasSuffix(normalized, "_cookie_names") {
		return false
	}
	// term 表示当前需要匹配的敏感属性名或组成部分。
	for _, term := range []string{"cookie", "set_cookie", "x5sec", "token", "password", "passwd", "secret", "api_key", "authorization", "credential"} {
		if normalized == term || strings.HasPrefix(normalized, term+"_") || strings.Contains(normalized, "_"+term+"_") || strings.HasSuffix(normalized, "_"+term) {
			return true
		}
	}
	return false
}

// SetLevel updates the process-wide log level.
// SetLevel 设置Level。
func SetLevel(raw string) error {
	// lv、err 用于本次流程后续判断的lv、err
	lv, err := ParseLevel(raw)
	if err != nil {
		return err
	}
	Level.Set(lv)
	return nil
}

// ParseLevel parses debug/info/warn/error.
// ParseLevel 解析Level。
func ParseLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("无效日志等级: %s", raw)
	}
}
