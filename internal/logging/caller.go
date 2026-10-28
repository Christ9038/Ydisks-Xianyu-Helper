package logging

import (
	"context"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// 日志记录中调用位置属性的固定键名，运行日志适配器按这两个键提升为具名字段。
const (
	// CallerKey 是“相对 internal 的文件路径:行号”属性键，例如 engine/reply.go:312。
	CallerKey = "caller"
	// FuncKey 是“包名.方法名”属性键，例如 engine.(*ReplyService).Handle。
	FuncKey = "func"
)

// callerAttrs 根据日志记录的程序计数器解析调用位置属性。
// pc 为零（记录不是由 slog.Logger 产生）或无法解析时返回 nil，调用方不应因此失败。
func callerAttrs(pc uintptr) []slog.Attr {
	if pc == 0 {
		return nil
	}
	// frames、frame 是 pc 对应的栈帧迭代器与解析出的帧。
	frames := runtime.CallersFrames([]uintptr{pc})
	// frame 是 pc 解析出的栈帧，包含源文件、行号与函数名。
	frame, _ := frames.Next()
	if frame.File == "" {
		return nil
	}
	return []slog.Attr{
		slog.String(CallerKey, shortCallerPath(frame.File)+":"+strconv.Itoa(frame.Line)),
		slog.String(FuncKey, shortFuncName(frame.Function)),
	}
}

// shortCallerPath 把绝对源文件路径缩短为相对 internal 或 cmd 的路径，避免输出构建机目录。
func shortCallerPath(file string) string {
	file = strings.ReplaceAll(file, "\\", "/")
	// marker 是用于截取仓库内相对路径的目录标记，按出现顺序优先匹配最后一个 internal。
	for _, marker := range []string{"/internal/", "/cmd/", "/tools/"} {
		// index 是匹配片段在文本中的位置，负数表示未命中。
		if index := strings.LastIndex(file, marker); index >= 0 {
			if marker == "/internal/" {
				return file[index+len(marker):]
			}
			return file[index+1:]
		}
	}
	// index 是匹配片段在文本中的位置，负数表示未命中。
	if index := strings.LastIndex(file, "/"); index >= 0 {
		return file[index+1:]
	}
	return file
}

// shortFuncName 去掉函数全名中的模块导入路径，只保留“包名.方法名”。
func shortFuncName(name string) string {
	// index 是匹配片段在文本中的位置，负数表示未命中。
	if index := strings.LastIndex(name, "/"); index >= 0 {
		return name[index+1:]
	}
	return name
}

// LogAt 在指定调用栈深度处记录日志，使封装函数产生的记录指向真正的业务调用方。
// skip 为 0 表示 LogAt 的直接调用者，1 表示调用者的调用者；ctx 为空时按 context.Background 处理。
// 被等级过滤的记录不会解析调用栈。
func LogAt(ctx context.Context, logger *slog.Logger, skip int, level slog.Level, message string, args ...any) {
	if logger == nil {
		logger = slog.Default()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !logger.Enabled(ctx, level) {
		return
	}
	// pcs 保存调用栈中的单个程序计数器；runtime.Callers 的 skip=2 对应 LogAt 的直接调用者。
	var pcs [1]uintptr
	runtime.Callers(skip+2, pcs[:])
	// record 是携带调用者 PC 的日志记录，属性由 Add 展开键值对。
	record := slog.NewRecord(time.Now(), level, message, pcs[0])
	record.Add(args...)
	_ = logger.Handler().Handle(ctx, record)
}
