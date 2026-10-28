package logging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
)

// moduleLevels 保存按模块覆盖的日志等级；nil 表示没有任何模块覆盖。
// 指针指向的 map 创建后只读，更新时整体替换，因此读取路径无需加锁。
var moduleLevels atomic.Pointer[map[string]slog.Level]

// levelFloor 是交给底层格式化处理器的等级下限：全局等级与全部模块覆盖等级的最小值。
// 模块等级的真正裁剪由 levelHandler 在 Handle 阶段完成，下限只保证被放开的模块记录能到达那里。
type levelFloor struct{}

// Level 返回当前全局等级与模块覆盖等级中的最小值，实现 slog.Leveler。
func (levelFloor) Level() slog.Level {
	// floor 是当前生效的最低等级，初始为全局等级。
	floor := Level.Level()
	// modules 是当前模块覆盖等级表快照；nil 表示没有覆盖。
	if modules := moduleLevels.Load(); modules != nil {
		// moduleLevel 是某个模块被覆盖后的等级。
		for _, moduleLevel := range *modules {
			if moduleLevel < floor {
				floor = moduleLevel
			}
		}
	}
	return floor
}

// effectiveLevel 返回指定模块当前生效的等级；模块没有覆盖时使用全局等级。
func effectiveLevel(module string) slog.Level {
	if module != "" {
		// modules 是当前模块覆盖等级表快照；nil 表示没有覆盖。
		if modules := moduleLevels.Load(); modules != nil {
			// ok 表示该模块存在覆盖等级。override 是模块被单独覆盖的日志等级。
			if override, ok := (*modules)[module]; ok {
				return override
			}
		}
	}
	return Level.Level()
}

// ParseLevelSpec 解析“全局等级[,模块=等级...]”形式的日志等级配置，例如 "info,mtop=debug,ws=warn"。
// 第一个不含等号的片段是全局等级，缺省为 info；模块名统一转为小写。任一片段非法时返回错误，不产生部分结果。
func ParseLevelSpec(raw string) (slog.Level, map[string]slog.Level, error) {
	// global 是全局等级；modules 是模块覆盖等级，没有覆盖时保持 nil。
	global := slog.LevelInfo
	// modules 是当前模块覆盖等级表快照；nil 表示没有覆盖。
	var modules map[string]slog.Level
	// globalSeen 标记已经读到全局等级，防止重复声明造成歧义。
	globalSeen := false
	// part 是以逗号分隔的单个配置片段。
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// hasModule 表示片段包含“模块=等级”的等号。name 是“模块=等级”片段中的模块名。value 是“模块=等级”片段中的等级文本。
		name, value, hasModule := strings.Cut(part, "=")
		if !hasModule {
			if globalSeen {
				return slog.LevelInfo, nil, fmt.Errorf("无效日志等级: %s", raw)
			}
			// parsed 是全局等级片段的解析结果。
			parsed, err := ParseLevel(part)
			if err != nil {
				return slog.LevelInfo, nil, err
			}
			global, globalSeen = parsed, true
			continue
		}
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			return slog.LevelInfo, nil, fmt.Errorf("无效日志等级: %s", raw)
		}
		// moduleLevel 是该模块的覆盖等级。
		moduleLevel, err := ParseLevel(value)
		if err != nil {
			return slog.LevelInfo, nil, err
		}
		if modules == nil {
			modules = make(map[string]slog.Level)
		}
		modules[name] = moduleLevel
	}
	return global, modules, nil
}

// levelHandler 按模块裁剪日志：同一进程可以让 mtop 输出 debug 而其他模块保持 info。
// 它位于处理器链最外层，模块名取自 logger 绑定的 subsys/log_source 属性，或单条记录携带的 log_source。
type levelHandler struct {
	// next 是实际处理记录的下一层处理器。
	next slog.Handler
	// module 是 logger 绑定属性推导出的模块名，为空表示按记录属性或全局等级处理。
	module string
}

// newLevelHandler 创建模块等级裁剪处理器。
func newLevelHandler(next slog.Handler) slog.Handler {
	return &levelHandler{next: next}
}

// Enabled 使用等级下限判断，被模块放开的低等级记录因此能进入 Handle 再做精确裁剪。
func (h *levelHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= (levelFloor{}).Level() && h.next.Enabled(ctx, level)
}

// Handle 根据记录所属模块的生效等级决定是否继续交给下一层。
func (h *levelHandler) Handle(ctx context.Context, record slog.Record) error {
	// module 优先使用 logger 绑定的模块名，其次读取记录自带的 log_source。
	module := h.module
	if module == "" {
		record.Attrs(func(attr slog.Attr) bool {
			if attr.Key == "log_source" || attr.Key == "subsys" {
				module = strings.ToLower(attr.Value.String())
				return false
			}
			return true
		})
	}
	if record.Level < effectiveLevel(module) {
		return nil
	}
	return h.next.Handle(ctx, record)
}

// WithAttrs 在绑定属性包含 subsys 或 log_source 时记住模块名，并继续向下层透传。
func (h *levelHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// module 是本次绑定后的模块名，默认沿用已有值。
	module := h.module
	// attr 是待绑定的属性；后绑定的模块名覆盖先绑定的。
	for _, attr := range attrs {
		if attr.Key == "subsys" || attr.Key == "log_source" {
			module = strings.ToLower(attr.Value.String())
		}
	}
	return &levelHandler{next: h.next.WithAttrs(attrs), module: module}
}

// WithGroup 保持模块名并向下层打开分组。
func (h *levelHandler) WithGroup(name string) slog.Handler {
	return &levelHandler{next: h.next.WithGroup(name), module: h.module}
}
