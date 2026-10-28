package mtop

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/logging"
)

// roundTripFunc 把函数适配为 http.RoundTripper，用于不经网络的传输层测试。
type roundTripFunc func(*http.Request) (*http.Response, error)

// RoundTrip 调用包装的函数。
func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// newLoggedRequest 构造带 ctx 的 MTOP 请求；ctx 为空时使用 Background。
func newLoggedRequest(t *testing.T, ctx context.Context) *http.Request {
	t.Helper()
	// err 是该步骤返回的错误，用于判断成功或失败路径。req 是待发送的 MTOP 测试请求。
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://h5api.m.goofish.com/h5/mtop.taobao.idle.logistic.consign.dummy/1.0/?x=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

// TestMtopAPIFromPath 验证接口名与版本解析及不符合形态时的空结果。
func TestMtopAPIFromPath(t *testing.T) {
	// api 是解析出的接口名，version 是解析出的接口版本。version 是从请求路径解析出的接口版本。
	api, version := mtopAPIFromPath("/h5/mtop.a.b/1.0/")
	if api != "mtop.a.b" || version != "1.0" {
		t.Fatalf("api=%q version=%q", api, version)
	}
	if api, version = mtopAPIFromPath("/other/path"); api != "" || version != "" {
		t.Fatalf("非 MTOP 路径应返回空: %q %q", api, version)
	}
}

// TestLoggingTransportSummarizesCalls 验证成功、HTTP 异常、失败与取消各自的等级和摘要字段，并继承 ctx 关联属性。
func TestLoggingTransportSummarizesCalls(t *testing.T) {
	t.Cleanup(func() { logging.Level.Set(slog.LevelInfo) })
	logging.Level.Set(slog.LevelDebug)
	// buf 收集日志；status、roundErr 控制下层传输返回值。
	var buf bytes.Buffer
	// logger 是写入测试缓冲区的日志器，用于检查最终输出。
	logger := logging.NewLogger(&buf, "text")
	// status 是下层传输下一次返回的 HTTP 状态码。
	status := http.StatusOK
	// roundErr 非空时下层传输返回该错误。
	var roundErr error
	// transport 是被测传输层。
	transport := loggingTransport{logger: logger, base: roundTripFunc(func(*http.Request) (*http.Response, error) {
		if roundErr != nil {
			return nil, roundErr
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("")), ContentLength: 2}, nil
	})}
	// ctx 是携带日志关联属性的上下文。
	ctx := logging.With(context.Background(), slog.String("account", "acc-1"), slog.String("run_id", "run-7"))
	ctx = context.WithValue(WithBackgroundPacing(ctx), pacerWaitContextKey{}, 3*time.Second)

	// err 是该步骤返回的错误，用于判断成功或失败路径。
	if _, err := transport.RoundTrip(newLoggedRequest(t, ctx)); err != nil {
		t.Fatal(err)
	}
	status = http.StatusBadGateway
	// err 是该步骤返回的错误，用于判断成功或失败路径。
	if _, err := transport.RoundTrip(newLoggedRequest(t, ctx)); err != nil {
		t.Fatal(err)
	}
	roundErr = errors.New("boom")
	// err 是该步骤返回的错误，用于判断成功或失败路径。
	if _, err := transport.RoundTrip(newLoggedRequest(t, ctx)); err == nil {
		t.Fatal("应返回下层错误")
	}
	roundErr = context.Canceled
	// err 是该步骤返回的错误，用于判断成功或失败路径。
	if _, err := transport.RoundTrip(newLoggedRequest(t, ctx)); err == nil {
		t.Fatal("应返回取消错误")
	}
	// lines 是按行拆分的日志，逐类断言。
	out := buf.String()
	// completed 是 MTOP 请求完成摘要所在的日志行。
	completed := findLine(out, "MTOP 请求完成")
	// want 是当前断言要求输出中出现的内容片段。
	for _, want := range []string{"level=INFO", "api=mtop.taobao.idle.logistic.consign.dummy", "v=1.0", "status=200", "account=acc-1", "run_id=run-7", "wait=3s", "background=true", "caller=xianyu/mtop/logging_transport.go:"} {
		if !strings.Contains(completed, want) {
			t.Fatalf("完成摘要缺少 %q: %s", want, completed)
		}
	}
	if strings.Contains(completed, "x=1") {
		t.Fatalf("摘要不得包含 query: %s", completed)
	}
	// level 是该消息期望出现的等级片段。message 是被检查的日志消息。
	for message, level := range map[string]string{"MTOP HTTP 响应异常": "level=WARN", "MTOP 请求失败": "level=WARN", "MTOP 请求取消": "level=DEBUG", "MTOP 请求开始": "level=DEBUG"} {
		// line 是当前检查的日志行。
		if line := findLine(out, message); !strings.Contains(line, level) {
			t.Fatalf("%s 等级错误: %s", message, line)
		}
	}
}

// TestPacingTransportWarnsOnSlowWait 验证节流排队超过阈值时输出告警并把排队时长写入请求上下文。
func TestPacingTransportWarnsOnSlowWait(t *testing.T) {
	// buf 收集日志；seenWait 保存下层看到的排队时长。
	var buf bytes.Buffer
	// logger 是写入测试缓冲区的日志器，用于检查最终输出。
	logger := logging.NewLogger(&buf, "text")
	// seenWait 保存下层传输从请求上下文读到的节流排队时长。
	var seenWait time.Duration
	// pacer 是使用极短睡眠的节拍器。
	pacer := NewPacer()
	pacer.skipLoopback = false
	pacer.sleep = func(context.Context, time.Duration) error {
		time.Sleep(20 * time.Millisecond)
		return nil
	}
	pacer.jitter = func() time.Duration { return 0 }
	// transport 是被测传输层。
	transport := pacingTransport{pacer: pacer, logger: logger, base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		seenWait, _ = req.Context().Value(pacerWaitContextKey{}).(time.Duration)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	// err 是该步骤返回的错误，用于判断成功或失败路径。
	if _, err := transport.RoundTrip(newLoggedRequest(t, context.Background())); err != nil {
		t.Fatal(err)
	}
	if seenWait < 20*time.Millisecond {
		t.Fatalf("排队时长未写入上下文: %v", seenWait)
	}
	if strings.Contains(buf.String(), "节流排队时间过长") {
		t.Fatalf("短排队不应告警: %s", buf.String())
	}
	// slow 把告警阈值降到 1ms，验证排队超过阈值时输出告警且不含 query。
	slow := transport
	slow.slowWait = time.Millisecond
	// err 是该步骤返回的错误，用于判断成功或失败路径。
	if _, err := slow.RoundTrip(newLoggedRequest(t, context.Background())); err != nil {
		t.Fatal(err)
	}
	// line 是当前检查的日志行。
	line := findLine(buf.String(), "节流排队时间过长")
	if !strings.Contains(line, "level=WARN") || !strings.Contains(line, "wait=") || strings.Contains(line, "x=1") {
		t.Fatalf("排队告警缺失或字段错误: %s", line)
	}
}

// findLine 返回 out 中包含 marker 的第一行，找不到时返回空字符串。
func findLine(out, marker string) string {
	// line 是当前检查的日志行。
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, marker) {
			return line
		}
	}
	return ""
}
