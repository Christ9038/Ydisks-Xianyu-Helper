// Package httpapi 定义跨 HTTP handler 和认证中间件共享的响应契约。
package httpapi

import (
	"encoding/json"
	"net/http"
)

const (
	// CodeBadRequest 表示请求参数或请求体不符合接口契约。
	CodeBadRequest = "bad_request"
	// CodeUnauthorized 表示请求缺少有效认证或认证信息已失效。
	CodeUnauthorized = "unauthorized"
	// CodeForbidden 表示请求已认证但没有访问目标资源的权限。
	CodeForbidden = "forbidden"
	// CodeNotFound 表示请求的资源或接口不存在。
	CodeNotFound = "not_found"
	// CodeConflict 表示请求与当前资源状态冲突。
	CodeConflict = "conflict"
	// CodeTooManyRequests 表示请求触发了访问频率限制。
	CodeTooManyRequests = "too_many_requests"
	// CodeNotImplemented 表示当前服务尚未实现该能力。
	CodeNotImplemented = "not_implemented"
	// CodeBadGateway 表示依赖的上游服务返回了不可用结果。
	CodeBadGateway = "bad_gateway"
	// CodeServiceUnavailable 表示服务当前无法提供请求能力。
	CodeServiceUnavailable = "service_unavailable"
	// CodeInternalError 表示服务内部处理失败。
	CodeInternalError = "internal_error"
)

// ErrorResponse 是所有统一失败响应的具名 DTO。
type ErrorResponse struct {
	// Code 是供客户端稳定分支判断的机器可读错误码。
	Code string `json:"code"`
	// Message 是可以直接展示给用户的错误说明。
	Message string `json:"message"`
	// RequestID 是可选的请求追踪标识，便于服务端日志关联。
	RequestID string `json:"request_id,omitempty"`
}

// CodeForStatus 将 HTTP 状态码映射为稳定的通用错误码。
func CodeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return CodeBadRequest
	case http.StatusUnauthorized:
		return CodeUnauthorized
	case http.StatusForbidden:
		return CodeForbidden
	case http.StatusNotFound:
		return CodeNotFound
	case http.StatusConflict:
		return CodeConflict
	case http.StatusTooManyRequests:
		return CodeTooManyRequests
	case http.StatusNotImplemented:
		return CodeNotImplemented
	case http.StatusBadGateway:
		return CodeBadGateway
	case http.StatusServiceUnavailable:
		return CodeServiceUnavailable
	default:
		return CodeInternalError
	}
}

// WriteError 使用统一的 code、message 和可选 request_id 写入 JSON 错误响应。
func WriteError(w http.ResponseWriter, status int, code, message, requestID string) {
	// response 是本次请求要序列化的统一错误 DTO。
	response := ErrorResponse{Code: code, Message: message, RequestID: requestID}
	if response.Code == "" {
		response.Code = CodeForStatus(status)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}
