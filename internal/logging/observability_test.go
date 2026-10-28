package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// nilContext 是未赋值的 context 接口，用于验证被测函数对空 context 的容错；
// 通过变量传入可以避免静态检查把字面量 nil 误判为误用。
var nilContext context.Context

// resetLevels 把全局与模块等级恢复为默认值，避免用例之间互相影响。
func resetLevels(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		Level.Set(slog.LevelInfo)
		moduleLevels.Store(nil)
	})
}

// TestLoggerAddsCallerAndFunc 验证每条日志自动携带指向真实调用点的文件行号与方法名。
func TestLoggerAddsCallerAndFunc(t *testing.T) {
	resetLevels(t)
	// buf 收集文本输出。
	var buf bytes.Buffer
	// logger 是写入测试缓冲区的日志器，用于检查最终输出。
	logger := NewLogger(&buf, "text")
	logger.Info("调用位置测试")
	// out 是写出的日志文本。
	out := buf.String()
	if !strings.Contains(out, "caller=logging/observability_test.go:") {
		t.Fatalf("缺少调用位置: %s", out)
	}
	if !strings.Contains(out, "func=logging.TestLoggerAddsCallerAndFunc") {
		t.Fatalf("缺少方法名: %s", out)
	}
}

// logViaWrapper 模拟封装函数：通过 LogAt 跳过自身，让调用位置指向它的调用者。
func logViaWrapper(logger *slog.Logger) {
	LogAt(context.Background(), logger, 1, slog.LevelInfo, "封装日志", "k", "v")
}

// TestLogAtSkipsWrapper 验证 LogAt 的 skip 使记录指向封装函数的调用方而不是封装函数本身。
func TestLogAtSkipsWrapper(t *testing.T) {
	resetLevels(t)
	// buf 收集文本输出。
	var buf bytes.Buffer
	logViaWrapper(NewLogger(&buf, "text"))
	// out 是写出的日志文本。
	out := buf.String()
	if strings.Contains(out, "logViaWrapper") {
		t.Fatalf("调用位置不应指向封装函数: %s", out)
	}
	if !strings.Contains(out, "func=logging.TestLogAtSkipsWrapper") || !strings.Contains(out, "k=v") {
		t.Fatalf("调用位置或属性缺失: %s", out)
	}
}

// TestLogAtFilteredAndNilArguments 验证被等级过滤的记录不输出，以及 nil logger/ctx 回退。
func TestLogAtFilteredAndNilArguments(t *testing.T) {
	resetLevels(t)
	// buf 收集文本输出。
	var buf bytes.Buffer
	// logger 是写入测试缓冲区的日志器，用于检查最终输出。
	logger := NewLogger(&buf, "text")
	LogAt(nilContext, logger, 0, slog.LevelDebug, "不应输出")
	if buf.Len() != 0 {
		t.Fatalf("debug 应被过滤: %s", buf.String())
	}
	// previous 是测试前的默认 logger，结束后恢复。
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	slog.SetDefault(logger)
	LogAt(context.Background(), nil, 0, slog.LevelInfo, "默认日志器")
	if !strings.Contains(buf.String(), "默认日志器") {
		t.Fatalf("nil logger 应回退到默认日志器: %s", buf.String())
	}
}

// TestContextAttrsMergedAndRedacted 验证 ctx 关联属性自动合并、叠加覆盖，且不覆盖记录显式属性，敏感键被脱敏。
func TestContextAttrsMergedAndRedacted(t *testing.T) {
	resetLevels(t)
	// buf 收集文本输出。
	var buf bytes.Buffer
	// logger 是写入测试缓冲区的日志器，用于检查最终输出。
	logger := NewLogger(&buf, "text")
	// ctx 是携带日志关联属性的上下文。
	ctx := With(context.Background(), slog.String("request_id", "req-1"), slog.String("account", "acc-a"))
	ctx = With(ctx, slog.String("account", "acc-b"), slog.String("cookie", "secret-value"))
	logger.InfoContext(ctx, "带关联属性", "order_id", "o-1", "account", "explicit")
	// out 是写出的日志文本。
	out := buf.String()
	// want 是当前断言要求输出中出现的内容片段。
	for _, want := range []string{"request_id=req-1", "account=explicit", "order_id=o-1", "cookie=<redacted>"} {
		if !strings.Contains(out, want) {
			t.Fatalf("缺少 %q: %s", want, out)
		}
	}
	if strings.Contains(out, "secret-value") || strings.Contains(out, "acc-b") {
		t.Fatalf("敏感值泄漏或显式属性被覆盖: %s", out)
	}
}

// TestWithAndAttrsFromEdgeCases 验证 With 的空参数与 AttrsFrom 的空 ctx 边界。
func TestWithAndAttrsFromEdgeCases(t *testing.T) {
	// got 是被测函数的实际返回值，用于与期望值比较。
	if got := AttrsFrom(nilContext); got != nil {
		t.Fatalf("nil ctx 应返回 nil: %v", got)
	}
	// base 是无属性的 ctx；With 无属性时应原样返回。
	base := context.Background()
	if With(base) != base {
		t.Fatal("无属性时应返回原 ctx")
	}
	if With(nilContext, slog.String("a", "1")) == nil {
		t.Fatal("nil ctx 应回退为 Background")
	}
	// parent、childA、childB 验证兄弟 context 的属性互不污染。
	parent := With(base, slog.String("run_id", "r1"))
	// childA 是在共同父 context 上叠加订单 a 的派生 context。
	childA := With(parent, slog.String("order_id", "a"))
	// childB 是在共同父 context 上叠加订单 b 的派生 context，用于验证兄弟 context 互不污染。
	childB := With(parent, slog.String("order_id", "b"))
	// got 是被测函数的实际返回值，用于与期望值比较。
	if got := AttrsFrom(childA); len(got) != 2 || got[1].Value.String() != "a" {
		t.Fatalf("childA 属性错误: %v", got)
	}
	// got 是被测函数的实际返回值，用于与期望值比较。
	if got := AttrsFrom(childB); len(got) != 2 || got[1].Value.String() != "b" {
		t.Fatalf("childB 属性错误: %v", got)
	}
}

// TestShortCallerPathAndFuncName 验证路径缩短与函数名裁剪的各个分支。
func TestShortCallerPathAndFuncName(t *testing.T) {
	// cases 是输入路径到期望输出的映射。
	cases := map[string]string{
		"/Users/x/repo/internal/engine/reply.go": "engine/reply.go",
		`C:\repo\internal\engine\reply.go`:       "engine/reply.go",
		"/Users/x/repo/cmd/server/main.go":       "cmd/server/main.go",
		"/tmp/other/file.go":                     "file.go",
		"bare.go":                                "bare.go",
	}
	// in、want 是单个用例的输入与期望。
	for in, want := range cases {
		// got 是被测函数的实际返回值，用于与期望值比较。
		if got := shortCallerPath(in); got != want {
			t.Fatalf("shortCallerPath(%q)=%q want %q", in, got, want)
		}
	}
	// got 是被测函数的实际返回值，用于与期望值比较。
	if got := shortFuncName("xianyu-go/internal/engine.(*ReplyService).Handle"); got != "engine.(*ReplyService).Handle" {
		t.Fatalf("shortFuncName=%q", got)
	}
	// got 是被测函数的实际返回值，用于与期望值比较。
	if got := shortFuncName("main.main"); got != "main.main" {
		t.Fatalf("shortFuncName=%q", got)
	}
	if callerAttrs(0) != nil {
		t.Fatal("pc 为零不应产生调用位置")
	}
}

// TestParseLevelSpec 验证全局与模块等级配置的解析和错误分支。
func TestParseLevelSpec(t *testing.T) {
	// err 是该步骤返回的错误，用于判断成功或失败路径。global 是解析出的全局等级。modules 是解析出的模块覆盖等级表。
	global, modules, err := ParseLevelSpec("warn, MTOP=debug ,ws=error")
	if err != nil || global != slog.LevelWarn || modules["mtop"] != slog.LevelDebug || modules["ws"] != slog.LevelError {
		t.Fatalf("解析结果错误: %v %v %v", global, modules, err)
	}
	if global, modules, err = ParseLevelSpec(""); err != nil || global != slog.LevelInfo || modules != nil {
		t.Fatalf("空配置应为 info: %v %v %v", global, modules, err)
	}
	// bad 是应当被拒绝的非法配置。
	for _, bad := range []string{"info,debug", "info,mtop=verbose", "=debug", "verbose"} {
		// err 是该步骤返回的错误，用于判断成功或失败路径。
		if _, _, err := ParseLevelSpec(bad); err == nil {
			t.Fatalf("%q 应返回错误", bad)
		}
	}
}

// TestSetLevelModuleOverride 验证模块覆盖只放开指定模块，且非法配置不改动现有等级。
func TestSetLevelModuleOverride(t *testing.T) {
	resetLevels(t)
	// buf 收集文本输出。
	var buf bytes.Buffer
	// logger 是写入测试缓冲区的日志器，用于检查最终输出。
	logger := NewLogger(&buf, "text")
	// err 是该步骤返回的错误，用于判断成功或失败路径。
	if err := SetLevel("info,mtop=debug,ws=error"); err != nil {
		t.Fatalf("SetLevel: %v", err)
	}
	logger.Debug("全局调试", "k", 1)
	logger.With("subsys", "mtop").Debug("mtop 调试")
	logger.Debug("记录级来源", "log_source", "mtop")
	logger.With("subsys", "ws").Warn("ws 警告")
	logger.With("subsys", "ws").Error("ws 错误")
	// out 是写出的日志文本。
	out := buf.String()
	if strings.Contains(out, "全局调试") || strings.Contains(out, "ws 警告") {
		t.Fatalf("未放开模块不应输出: %s", out)
	}
	// want 是当前断言要求输出中出现的内容片段。
	for _, want := range []string{"mtop 调试", "记录级来源", "ws 错误"} {
		if !strings.Contains(out, want) {
			t.Fatalf("缺少 %q: %s", want, out)
		}
	}
	// err 是该步骤返回的错误，用于判断成功或失败路径。
	if err := SetLevel("info,bad"); err == nil {
		t.Fatal("非法配置应失败")
	}
	if effectiveLevel("mtop") != slog.LevelDebug {
		t.Fatal("非法配置不应改动现有模块等级")
	}
	// err 是该步骤返回的错误，用于判断成功或失败路径。
	if err := SetLevel("warn"); err != nil || effectiveLevel("mtop") != slog.LevelWarn {
		t.Fatalf("清空模块覆盖失败: %v", err)
	}
	// grouped 验证 WithGroup 保持模块名。
	logger.WithGroup("g").Info("分组被过滤")
}

// TestDedupSuppressesAndSummarizes 验证窗口内超限抑制、不同事件互不影响、过期后补记汇总以及 Info 不参与抑制。
func TestDedupSuppressesAndSummarizes(t *testing.T) {
	resetLevels(t)
	// now 是可控时钟；buf 收集输出。
	now := time.Unix(1000, 0)
	// state 是使用可控时钟的重复抑制状态。
	state := newDedupState(func() time.Time { return now })
	// buf 收集日志输出文本。
	var buf bytes.Buffer
	// logger 是写入测试缓冲区的日志器，用于检查最终输出。
	logger := slog.New(newDedupHandler(slog.NewTextHandler(&buf, nil), state)).With("account", "a1")
	// emit 保证每次告警来自同一调用点，从而落入同一个去重键。
	emit := func() { logger.Warn("重复告警", "err", "boom") }
	// i 是当前循环序号。
	for i := 0; i < 8; i++ {
		emit()
	}
	// i 是当前循环序号。
	for i := 0; i < 8; i++ {
		logger.Info("信息不抑制")
	}
	slog.New(newDedupHandler(slog.NewTextHandler(&buf, nil), state)).Warn("重复告警", "err", "other")
	// out 是写出的日志文本。
	out := buf.String()
	// got 是被测函数的实际返回值，用于与期望值比较。
	if got := strings.Count(out, "msg=重复告警"); got != dedupWarnLimit+1 {
		t.Fatalf("放行条数=%d want %d: %s", got, dedupWarnLimit+1, out)
	}
	// got 是被测函数的实际返回值，用于与期望值比较。
	if got := strings.Count(out, "信息不抑制"); got != 8 {
		t.Fatalf("Info 不应被抑制: %d", got)
	}
	now = now.Add(dedupWindow + time.Second)
	emit()
	out = buf.String()
	if !strings.Contains(out, "已抑制重复日志") || !strings.Contains(out, "suppressed=3") {
		t.Fatalf("缺少汇总: %s", out)
	}
	// 子 logger 的 WithGroup 与绑定属性保持状态共享。
	logger.WithGroup("g").Warn("分组告警")
	if !logger.Enabled(context.Background(), slog.LevelWarn) {
		t.Fatal("Enabled 应沿用下一层")
	}
}

// TestDedupErrorLimitAndSweep 验证 Error 的更高阈值，以及状态表过大时的过期清理汇总。
func TestDedupErrorLimitAndSweep(t *testing.T) {
	// now 是可控时钟。
	now := time.Unix(2000, 0)
	// state 是使用可控时钟的重复抑制状态。
	state := newDedupState(func() time.Time { return now })
	// i 是当前循环序号。
	for i := 0; i < dedupErrorLimit+2; i++ {
		// allowed 表示该条记录是否被放行。
		allowed, _ := state.admit("e", slog.LevelError, "错误", "x.go:1")
		if allowed != (i < dedupErrorLimit) {
			t.Fatalf("第 %d 条放行判断错误", i)
		}
	}
	// i 是当前循环序号。
	for i := 0; i < dedupSweepThreshold; i++ {
		state.admit(dedupKey("k"+string(rune('a'+i%26))+strings.Repeat("x", i)), slog.LevelWarn, "填充", "")
	}
	now = now.Add(2 * dedupWindow)
	// summaries 是窗口过期后需要补记的汇总。
	_, summaries := state.admit("new", slog.LevelWarn, "新事件", "")
	if len(summaries) != 1 || summaries[0].suppressed != 2 {
		t.Fatalf("清理汇总错误: %+v", summaries)
	}
	// allowed 表示该条记录是否被放行。summaries 是窗口过期后需要补记的汇总。
	if allowed, summaries := state.admit("info", slog.LevelInfo, "信息", ""); !allowed || summaries != nil {
		t.Fatal("Info 应直接放行")
	}
	if newDedupState(nil).now == nil {
		t.Fatal("nil 时钟应回退到系统时钟")
	}
	if newDedupHandler(slog.DiscardHandler, nil) == nil {
		t.Fatal("nil 状态应自动创建")
	}
}
