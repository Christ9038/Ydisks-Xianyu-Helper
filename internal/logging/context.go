package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"strconv"
	"time"
)

// contextAttrsKey 是在 context 中保存日志关联属性的私有键，避免与其他包的键冲突。
type contextAttrsKey struct{}

// With 返回携带日志关联属性的派生 context。
// 属性会与上游已挂载的属性叠加；同名键以后挂载的值为准。属性值只应是 request_id、run_id、
// account 这类关联标识，调用方不得放入凭证，脱敏处理器仍会在写出前统一清理。
// ctx 为空时按 context.Background 处理，attrs 为空时原样返回 ctx。
func With(ctx context.Context, attrs ...slog.Attr) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(attrs) == 0 {
		return ctx
	}
	// existing 是上游已经挂载的属性，需要复制后再追加，避免并发的兄弟 context 互相污染。
	existing := AttrsFrom(ctx)
	// merged 保存叠加后的属性，同名键被后挂载的属性替换并保持首次出现的位置。
	merged := make([]slog.Attr, 0, len(existing)+len(attrs))
	merged = append(merged, existing...)
	// attr 是本次新增的关联属性。
	for _, attr := range attrs {
		merged = upsertAttr(merged, attr)
	}
	return context.WithValue(ctx, contextAttrsKey{}, merged)
}

// AttrsFrom 返回 ctx 上挂载的日志关联属性副本；没有挂载时返回 nil。
func AttrsFrom(ctx context.Context) []slog.Attr {
	if ctx == nil {
		return nil
	}
	// stored 是 With 保存的属性切片；它不可变，因此直接复制即可得到安全副本。
	stored, _ := ctx.Value(contextAttrsKey{}).([]slog.Attr)
	if len(stored) == 0 {
		return nil
	}
	return append([]slog.Attr(nil), stored...)
}

// upsertAttr 在 attrs 中按键名替换同名属性，不存在时追加，返回更新后的切片。
func upsertAttr(attrs []slog.Attr, attr slog.Attr) []slog.Attr {
	// index、existing 是已有属性的下标与值，用于按键名原位替换。
	for index, existing := range attrs {
		if existing.Key == attr.Key {
			attrs[index] = attr
			return attrs
		}
	}
	return append(attrs, attr)
}

// NewTickID 生成调度器单轮扫描的短标识（8 位十六进制），用于把同一轮产生的日志关联起来。
// 标识只用于日志关联，不具备安全含义；随机源不可用时退化为基于时间的标识，保证不会返回空串。
func NewTickID() string {
	// raw 保存 4 字节随机数。
	var raw [4]byte
	// err 是读取系统随机源的失败原因，失败时退化为时间标识。
	if _, err := rand.Read(raw[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano()&0xffffffff, 16)
	}
	return hex.EncodeToString(raw[:])
}
