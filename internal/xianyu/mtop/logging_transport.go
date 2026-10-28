package mtop

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"xianyu-go/internal/logging"
)

// logInfo 记录不包含 Cookie、签名和响应正文的 MTOP 诊断信息；未注入日志器时沿用进程默认日志器。
func (c *ClientImpl) logInfo(message string, args ...any) {
	// logger 保存当前客户端实际使用的安全日志器。
	logger := c.Logger
	if logger == nil {
		logger = slog.Default()
	}
	// skip=1 让调用位置指向 logInfo 的调用者，而不是这个封装函数本身。
	logging.LogAt(context.Background(), logger, 1, slog.LevelInfo, message, append([]any{"log_source", "mtop"}, args...)...)
}

// loggingTransport 用于本次流程后续判断的loggingTransport
type loggingTransport struct {
	base   http.RoundTripper
	logger *slog.Logger
}

// mtopAPIFromPath 从 MTOP 请求路径（形如 /h5/mtop.xxx.yyy/1.0/）解析接口名与版本。
// 路径不符合该形态时返回空字符串，调用方仍可使用完整 path 定位请求。
func mtopAPIFromPath(path string) (api, version string) {
	// parts 是去掉首尾斜杠后的路径段。
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 3 && parts[0] == "h5" {
		return parts[1], parts[2]
	}
	return "", ""
}

// RoundTrip 记录每个 MTOP 请求的完成摘要：接口名、HTTP 状态、网络往返耗时、节流排队时长与后台标记。
// 账号、request_id、run_id 等关联字段由请求 context 上的日志属性自动合并，因此一行日志即可回答“哪个账号调了哪个接口”。
// 成功为 Info，HTTP 4xx/5xx 与网络失败为 Warn，主动取消为 Debug；请求频率由节流器保证，此处每请求至多一行。
func (t loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// logger 是本次请求使用的日志器，未注入时沿用进程默认日志器。
	logger := t.logger
	if logger == nil {
		logger = slog.Default()
	}
	// ctx 是携带日志关联属性的请求上下文；api、version 是从路径解析的接口名与版本。
	ctx := req.Context()
	// api 是从请求路径解析出的 MTOP 接口名。version 是从请求路径解析出的接口版本。
	api, version := mtopAPIFromPath(req.URL.Path)
	// started 是网络往返起点，不含节流排队时间。
	started := time.Now()
	logger.LogAttrs(ctx, slog.LevelDebug, "MTOP 请求开始", slog.String("log_source", "mtop"), slog.String("api", api), slog.String("method", req.Method), slog.String("path", req.URL.Path))
	// resp、err 是下层传输的响应与错误。
	resp, err := t.base.RoundTrip(req)
	// background 表示请求属于后台批量任务；wait 是节流器记录的排队时长。
	background, _ := ctx.Value(pacingContextKey{}).(bool)
	// wait 是节流器记录的账号时隙排队时长，缺失时为零。
	wait, _ := ctx.Value(pacerWaitContextKey{}).(time.Duration)
	// attrs 是所有结果共有的摘要字段。
	attrs := []slog.Attr{
		slog.String("log_source", "mtop"),
		slog.String("api", api),
		slog.String("v", version),
		slog.String("method", req.Method),
		slog.String("path", req.URL.Path),
		slog.Duration("duration", time.Since(started).Round(time.Millisecond)),
		slog.Duration("wait", wait.Round(time.Millisecond)),
		slog.Bool("background", background),
	}
	if err != nil {
		attrs = append(attrs, slog.Any("err", err))
		if errors.Is(err, context.Canceled) {
			logger.LogAttrs(ctx, slog.LevelDebug, "MTOP 请求取消", attrs...)
		} else {
			logger.LogAttrs(ctx, slog.LevelWarn, "MTOP 请求失败", attrs...)
		}
		return nil, err
	}
	attrs = append(attrs, slog.Int("status", resp.StatusCode), slog.Int64("content_length", resp.ContentLength))
	if resp.StatusCode >= http.StatusBadRequest {
		logger.LogAttrs(ctx, slog.LevelWarn, "MTOP HTTP 响应异常", attrs...)
	} else {
		logger.LogAttrs(ctx, slog.LevelInfo, "MTOP 请求完成", attrs...)
	}
	return resp, nil
}
