package server

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"xianyu-go/internal/logging"
)

// slowRequestThreshold 是 HTTP 请求被标记为慢请求的耗时阈值，超过后日志等级至少为 Warn。
const slowRequestThreshold = 3 * time.Second

// httpLogLevel 根据请求方法、路径、响应状态和耗时决定“HTTP 请求完成”日志等级，并返回是否为慢请求。
// 规则：5xx 为 Error；401/404 为 Debug（探测与过期会话属常态）；其余 4xx 为 Warn；
// 成功的写操作（POST/PUT/PATCH/DELETE）为 Info 以保留操作审计；成功的读操作、健康检查与前端静态资源为 Debug；
// 耗时超过 slowRequestThreshold 时至少提升为 Warn；101 协议切换（WebSocket）是长连接，其耗时是连接存活时长而非处理耗时，不判定为慢请求。
func httpLogLevel(method, path string, status int, duration time.Duration) (level slog.Level, slow bool) {
	slow = status != http.StatusSwitchingProtocols && duration > slowRequestThreshold
	switch {
	case status >= 500:
		return slog.LevelError, slow
	case status == http.StatusUnauthorized || status == http.StatusNotFound:
		level = slog.LevelDebug
	case status >= 400:
		level = slog.LevelWarn
	case path == "/health" || !strings.HasPrefix(path, "/api/"):
		level = slog.LevelDebug
	case method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete:
		level = slog.LevelInfo
	default:
		level = slog.LevelDebug
	}
	if slow && level < slog.LevelWarn {
		level = slog.LevelWarn
	}
	return level, slow
}

// requestLogger 为请求注入 request_id 日志上下文，并在完成后记录状态、耗时与路由模板。
// request_id 通过 context 传播，下游应用服务、MTOP 与自动化日志只要使用 *Context 日志方法就会自动携带，
// 因此运行日志中可以按 request_id 串起一次请求引发的全部日志。
func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 运行日志查询接口自身的轮询请求不写入日志，避免前端刷新日志时产生自我刷屏。
		if r.URL.Path == runtimeLogsPath {
			next.ServeHTTP(w, r)
			return
		}
		// start 是请求开始时间，用于计算耗时。
		start := time.Now()
		// requestID 是 chi 生成的请求标识；req 是“方法 路径”，供下游日志定位入口接口。
		requestID := middleware.GetReqID(r.Context())
		r = r.WithContext(logging.With(r.Context(),
			slog.String("request_id", requestID),
			slog.String("req", r.Method+" "+r.URL.Path),
		))
		// ww 包装响应写入器以读取状态码和字节数。
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		// status 是响应状态码，处理器未写响应时按 200 记录。
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}
		// duration、level、slow 分别是处理耗时、日志等级与慢请求标记。
		duration := time.Since(start)
		// level 是该请求完成日志的等级。slow 表示请求耗时超过慢请求阈值。
		level, slow := httpLogLevel(r.Method, r.URL.Path, status, duration)
		// route 是 chi 匹配到的路由模板（如 /api/v1/orders/{order_id}），未匹配时为空。
		route := ""
		// routeContext 是 chi 路由上下文，匹配完成后可读取路由模板。
		if routeContext := chi.RouteContext(r.Context()); routeContext != nil {
			route = routeContext.RoutePattern()
		}
		// attrs 是请求完成日志的结构化属性。
		attrs := []slog.Attr{
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("route", route),
			slog.Int("status", status),
			slog.Int("bytes", ww.BytesWritten()),
			slog.Duration("duration", duration),
			slog.String("request_id", requestID),
			slog.String("remote", r.RemoteAddr),
			slog.Bool("legacy_api", isLegacyAPIRequest(r.Method, r.URL.Path)),
			slog.String("log_source", "http"),
		}
		if slow {
			attrs = append(attrs, slog.Bool("slow", true))
		}
		s.Logger.LogAttrs(r.Context(), level, "HTTP 请求完成", attrs...)
	})
}
