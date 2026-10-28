package server

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"xianyu-go/internal/logging"
)

// TestHTTPLogLevelRules 验证 HTTP 请求日志等级规则：写操作审计、探测降噪、慢请求与服务端错误。
func TestHTTPLogLevelRules(t *testing.T) {
	// cases 是方法、路径、状态与耗时到期望等级和慢标记的表驱动用例。
	cases := []struct {
		name     string
		method   string
		path     string
		status   int
		duration time.Duration
		level    slog.Level
		slow     bool
	}{
		{"读成功", http.MethodGet, "/api/v1/orders", 200, time.Millisecond, slog.LevelDebug, false},
		{"写成功", http.MethodPost, "/api/v1/cards", 201, time.Millisecond, slog.LevelInfo, false},
		{"删除成功", http.MethodDelete, "/api/v1/cards/1", 200, time.Millisecond, slog.LevelInfo, false},
		{"健康检查", http.MethodPost, "/health", 200, time.Millisecond, slog.LevelDebug, false},
		{"静态资源", http.MethodGet, "/assets/app.js", 200, time.Millisecond, slog.LevelDebug, false},
		{"未授权", http.MethodGet, "/api/v1/orders", 401, time.Millisecond, slog.LevelDebug, false},
		{"未找到", http.MethodGet, "/api/v1/x", 404, time.Millisecond, slog.LevelDebug, false},
		{"参数错误", http.MethodPost, "/api/v1/cards", 400, time.Millisecond, slog.LevelWarn, false},
		{"服务端错误", http.MethodGet, "/api/v1/orders", 500, time.Millisecond, slog.LevelError, false},
		{"慢读请求", http.MethodGet, "/api/v1/orders", 200, 4 * time.Second, slog.LevelWarn, true},
		{"慢服务端错误", http.MethodGet, "/api/v1/orders", 502, 4 * time.Second, slog.LevelError, true},
	}
	// tc 是当前用例。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// level 是被测函数返回的日志等级。slow 表示请求是否被判定为慢请求。
			level, slow := httpLogLevel(tc.method, tc.path, tc.status, tc.duration)
			if level != tc.level || slow != tc.slow {
				t.Fatalf("level=%v slow=%v want %v %v", level, slow, tc.level, tc.slow)
			}
		})
	}
}

// TestRequestLoggerPropagatesRequestIDAndRoute 验证 request_id 经 context 传到下游日志，完成日志带路由模板且读成功默认不输出 Info。
func TestRequestLoggerPropagatesRequestIDAndRoute(t *testing.T) {
	// buf 收集日志；srv 只需要日志器。
	var buf bytes.Buffer
	// logger 是写入测试缓冲区的日志器，用于检查最终输出。
	logger := logging.NewLogger(&buf, "text")
	// srv 是只需要日志器的最小 Server。
	srv := &Server{Logger: logger}
	// router 是挂载了请求日志中间件的测试路由。
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(srv.requestLogger)
	router.Post("/api/v1/orders/{order_id}", func(w http.ResponseWriter, r *http.Request) {
		logger.InfoContext(r.Context(), "下游业务日志")
		w.WriteHeader(http.StatusCreated)
	})
	router.Get("/api/v1/orders/{order_id}", func(w http.ResponseWriter, r *http.Request) {})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/orders/o-9", nil))
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/orders/o-9", nil))
	// out 是写出的全部日志。
	out := buf.String()
	// downstream、completion 是下游业务日志行与 POST 完成日志行。
	var downstream, completion string
	// line 是当前检查的日志行。
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.Contains(line, "下游业务日志"):
			downstream = line
		case strings.Contains(line, "HTTP 请求完成"):
			completion = line
		}
	}
	// want 是当前断言要求输出中出现的内容片段。
	for _, want := range []string{"request_id=", "req=\"POST /api/v1/orders/o-9\""} {
		if !strings.Contains(downstream, want) {
			t.Fatalf("下游日志缺少 %q: %s", want, downstream)
		}
	}
	// want 是当前断言要求输出中出现的内容片段。
	for _, want := range []string{"route=/api/v1/orders/{order_id}", "status=201", "level=INFO"} {
		if !strings.Contains(completion, want) {
			t.Fatalf("完成日志缺少 %q: %s", want, completion)
		}
	}
	if strings.Count(out, "HTTP 请求完成") != 1 {
		t.Fatalf("读成功请求不应在 Info 级别输出: %s", out)
	}
}

// TestApplyLogLevelRecordsAuditAndKeepsLevelOnInvalid 验证日志等级更新留下含新旧值的审计日志，非法配置不改动现有等级。
func TestApplyLogLevelRecordsAuditAndKeepsLevelOnInvalid(t *testing.T) {
	_ = logging.SetLevel("info")
	t.Cleanup(func() { _ = logging.SetLevel("info") })
	// buf 收集日志；srv 只需要日志器。
	var buf bytes.Buffer
	// srv 是只带日志器的最小 Server。
	srv := &Server{Logger: logging.NewLogger(&buf, "text")}
	// req 是触发设置保存的请求。
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", nil)
	srv.applyLogLevel(req, "warn,mtop=debug")
	if logging.Level.Level() != slog.LevelWarn {
		t.Fatalf("全局等级未更新: %v", logging.Level.Level())
	}
	srv.applyLogLevel(req, "bad,level")
	// out 是写出的日志文本。
	out := buf.String()
	if !strings.Contains(out, "日志等级即将更新") || !strings.Contains(out, `to="warn,mtop=debug"`) {
		t.Fatalf("缺少审计日志: %s", out)
	}
	if !strings.Contains(out, "应用日志等级失败，保留现有等级") || logging.Level.Level() != slog.LevelWarn {
		t.Fatalf("非法配置处理错误: %s", out)
	}
}
