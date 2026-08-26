package logging

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// TestParseLevelAndSetLevel 封装TestParseLevelAndSetLevel业务协调。
func TestParseLevelAndSetLevel(t *testing.T) {
	defer Level.Set(slog.LevelInfo)

	// cases 用于本次流程后续判断的cases
	cases := map[string]slog.Level{
		"":        slog.LevelInfo,
		"debug":   slog.LevelDebug,
		"INFO":    slog.LevelInfo,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
	}
	// in、want 表示当前遍历过程中的in、want
	for in, want := range cases {
		// got、err 用于本次流程后续判断的got、err
		got, err := ParseLevel(in)
		if err != nil {
			t.Fatalf("ParseLevel(%q): %v", in, err)
		}
		if got != want {
			t.Fatalf("ParseLevel(%q)=%v want %v", in, got, want)
		}
	}
	if // err 用于本次流程后续判断的err
	_, err := ParseLevel("verbose"); err == nil {
		t.Fatal("invalid level should fail")
	}
	if // err 用于本次流程后续判断的err
	err := SetLevel("debug"); err != nil {
		t.Fatalf("SetLevel: %v", err)
	}
	if // err 是无效日志等级返回的配置错误。
	err := SetLevel("verbose"); err == nil {
		t.Fatal("无效日志等级应返回错误")
	}
	if // got 用于本次流程后续判断的got
	got := Level.Level(); got != slog.LevelDebug {
		t.Fatalf("global level=%v want debug", got)
	}
}

// TestRedactionHandlerCoversFallbackTypes 验证日志处理器的空实现、分组和基础类型分支。
func TestRedactionHandlerCoversFallbackTypes(t *testing.T) {
	// handler 是空底层处理器回退后的脱敏处理器。
	handler := newRedactingHandler(nil)
	if handler == nil {
		t.Fatal("空底层处理器应回退为可用处理器")
	}
	// grouped 是保留分组语义的脱敏处理器。
	grouped := handler.WithGroup("diagnostic")
	if grouped == nil {
		t.Fatal("分组处理器不应为空")
	}
	// defaultAttr 是整数属性走 redactAttr 默认保留分支的结果。
	defaultAttr := redactAttr(slog.Int("count", 1))
	if defaultAttr.Value.Int64() != 1 {
		t.Fatalf("基础整数属性被错误改写: %+v", defaultAttr)
	}
	// nilAttr、bytesAttr、numberAttr、unknownAttr 覆盖 Any 属性的 nil、字节、数字和未知类型。
	nilAttr := redactAnyAttr("nil", nil)
	// bytesAttr 是字节切片 Any 属性的脱敏结果。
	bytesAttr := redactAnyAttr("bytes", []byte("safe"))
	// stringAttr 是普通字符串 Any 属性的脱敏结果。
	stringAttr := redactAnyAttr("string", "safe")
	// numberAttr 是数字 Any 属性的保留结果。
	numberAttr := redactAnyAttr("number", int64(7))
	// unknownAttr 是未知结构 Any 属性的整体替换结果。
	unknownAttr := redactAnyAttr("unknown", struct{ Value string }{Value: "secret"})
	if nilAttr.Key == "" || bytesAttr.String() == "" || stringAttr.String() == "" || numberAttr.Key == "" || unknownAttr.Value.String() != "<redacted>" {
		t.Fatalf("Any 属性分支结果异常: nil=%+v bytes=%+v string=%+v number=%+v unknown=%+v", nilAttr, bytesAttr, stringAttr, numberAttr, unknownAttr)
	}
}

// TestNewLoggerHonorsDynamicLevel 封装TestNewLoggerHonorsDynamicLevel业务协调。
func TestNewLoggerHonorsDynamicLevel(t *testing.T) {
	defer Level.Set(slog.LevelInfo)
	// buf 用于本次流程后续判断的buf
	var buf bytes.Buffer
	// logger 用于本次流程后续判断的logger
	logger := NewLogger(&buf, "text")

	Level.Set(slog.LevelWarn)
	logger.Info("hidden")
	logger.Warn("visible")
	// out 用于本次流程后续判断的out
	out := buf.String()
	if strings.Contains(out, "hidden") {
		t.Fatalf("info log should be filtered: %s", out)
	}
	if !strings.Contains(out, "visible") {
		t.Fatalf("warn log should be emitted: %s", out)
	}
}

// TestNewLoggerRedactsAttributes 验证文本和 JSON 日志都会集中清理凭证、URL 查询及错误属性。
func TestNewLoggerRedactsAttributes(t *testing.T) {
	// formats 覆盖生产支持的两种日志编码格式。
	formats := []string{"text", "json"}
	// format 表示当前测试所使用的日志编码格式。
	for _, format := range formats {
		// format 保存当前子测试名称，避免闭包引用变化后的循环变量。
		format := format
		t.Run(format, func(t *testing.T) {
			// buf 捕获当前格式的日志输出，供后续秘密泄露断言使用。
			var buf bytes.Buffer
			// logger 是启用集中式脱敏处理器的测试日志实例。
			logger := NewLogger(&buf, format)
			logger = logger.With("password", "bound-password")
			logger.Error("业务失败",
				slog.String("url", "https://example.test/api?access_token=url-token#fragment"),
				slog.String("token", "attribute-token"),
				slog.String("cookie_value", "cookie-attribute-value"),
				slog.String("cookie_count", "2"),
				slog.Any("err", errors.New(`request failed: password=error-password`)),
				slog.Group("nested", slog.String("authorization", "Bearer nested-token")),
				slog.Any("payload", map[string]string{"secret": "map-secret"}),
			)
			// output 保存当前格式最终写出的日志文本。
			output := buf.String()
			// secret 表示不允许出现在任何日志编码结果中的模拟秘密。
			for _, secret := range []string{"bound-password", "url-token", "attribute-token", "cookie-attribute-value", "error-password", "nested-token", "map-secret"} {
				if strings.Contains(output, secret) {
					t.Fatalf("日志格式 %s 泄露秘密 %q: %s", format, secret, output)
				}
			}
			if !strings.Contains(output, "业务失败") {
				t.Fatalf("日志格式 %s 丢失业务文案: %s", format, output)
			}
			if !strings.Contains(output, "<redacted>") {
				t.Fatalf("日志格式 %s 未写出脱敏占位符: %s", format, output)
			}
			if !strings.Contains(output, "cookie_count") || !strings.Contains(output, "2") {
				t.Fatalf("日志格式 %s 不应隐藏非敏感 Cookie 数量: %s", format, output)
			}
		})
	}
}
