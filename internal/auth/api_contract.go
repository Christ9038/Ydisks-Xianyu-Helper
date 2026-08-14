package auth

import (
	"net/http"

	"github.com/go-chi/chi/v5/middleware"

	"xianyu-go/internal/httpapi"
)

// writeAuthError 输出认证中间件统一使用的错误 DTO，并关联 chi 请求追踪标识。
func writeAuthError(w http.ResponseWriter, r *http.Request, status int, code, message string) {
	httpapi.WriteError(w, status, code, message, middleware.GetReqID(r.Context()))
}
