package server

import (
	"context"
	"log/slog"
	"net/http"

	"xianyu-go/internal/logging"
	"xianyu-go/internal/logsafe"
)

// logInternalFailure 在 HTTP 边界记录一次接口失败的底层原因。
//
// 接口响应只携带稳定的固定文案，数据库、应用服务或上游返回的真实错误如果不在这里记录就会彻底丢失。
// ctx 是请求上下文（自动带上 request_id、user_id）；status 是即将返回的 HTTP 状态码，用于决定日志等级：
// 500/502/504 记 Error，503（服务未启用等可恢复状态）与其余状态记 Warn；err 是底层错误，写入前经 logsafe 去除 URL 查询与凭证键值，不含请求正文。
// caller 经 logging.LogAt 指向调用 writeInternalErr 的业务位置，而不是本函数。
func logInternalFailure(ctx context.Context, status int, err error) {
	logInternalFailureAt(ctx, 2, status, err)
}

// logInternalFailureAt 是 logInternalFailure 的实现；skip 是 logging.LogAt 需要跳过的调用栈层数，
// 直接调用 logInternalFailure 时为 2（跳过本函数与 logInternalFailure），经 writeInternalErr* 再包一层时为 3。
func logInternalFailureAt(ctx context.Context, skip int, status int, err error) {
	if err == nil {
		return
	}
	// level 是本次失败的日志等级，服务端故障按 Error，客户端可纠正的失败按 Warn。
	level := slog.LevelWarn
	if status >= http.StatusInternalServerError && status != http.StatusServiceUnavailable {
		level = slog.LevelError
	}
	logging.LogAt(ctx, nil, skip, level, "接口处理失败", "status", status, "err", logsafe.Error(err))
}

// writeInternalErr 记录底层错误后写统一错误响应；响应内容与 writeErr 完全一致，不暴露 err。
// r 提供请求上下文，status 与 msg 是返回给前端的状态码和固定文案，err 是仅写入日志的底层错误。
func writeInternalErr(w http.ResponseWriter, r *http.Request, status int, msg string, err error) {
	logInternalFailureAt(r.Context(), 3, status, err)
	writeErr(w, status, msg)
}

// writeInternalErrRequest 记录底层错误后写带请求追踪标识的统一错误响应；响应内容与 writeErrRequest 完全一致。
// 参数含义同 writeInternalErr。
func writeInternalErrRequest(w http.ResponseWriter, r *http.Request, status int, msg string, err error) {
	logInternalFailureAt(r.Context(), 3, status, err)
	writeErrRequest(w, r, status, msg)
}
