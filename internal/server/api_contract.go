package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"

	"xianyu-go/internal/db"
	"xianyu-go/internal/httpapi"
)

// healthResponse 是健康检查接口的具名响应 DTO。
type healthResponse struct {
	// Status 表示服务总体健康状态。
	Status string `json:"status"`
	// Database 表示数据库连接状态。
	Database string `json:"database"`
	// Version 是当前构建版本。
	Version string `json:"version,omitempty"`
	// Commit 是当前构建对应的短提交标识。
	Commit string `json:"commit,omitempty"`
	// BuildTime 是构建时注入的时间信息。
	BuildTime string `json:"build_time,omitempty"`
}

// cookieSummaryResponse 是账号列表和账号详情共用的非敏感响应 DTO。
type cookieSummaryResponse struct {
	// ID 是闲鱼账号的稳定标识。
	ID string `json:"id"`
	// HasCookie 表示数据库中存在可用账号记录。
	HasCookie bool `json:"has_cookie"`
	// Enabled 表示账号是否允许运行。
	Enabled bool `json:"enabled"`
	// AutoConfirm 表示是否自动确认订单。
	AutoConfirm bool `json:"auto_confirm"`
	// Remark 是账号备注。
	Remark string `json:"remark"`
	// PauseDuration 是自动回复暂停时长，单位为分钟。
	PauseDuration int `json:"pause_duration"`
	// PausedUntil 是暂停结束时间的 Unix 秒。
	PausedUntil int64 `json:"paused_until"`
	// Paused 表示账号当前是否仍处于暂停状态。
	Paused bool `json:"paused"`
	// ShowBrowser 表示密码登录流程是否允许显示浏览器。
	ShowBrowser bool `json:"show_browser"`
	// Username 是登录用户名，不包含登录密码。
	Username string `json:"username"`
	// Nickname 是平台账号昵称缓存。
	Nickname string `json:"nickname"`
	// AvatarURL 是平台账号头像地址。
	AvatarURL string `json:"avatar_url"`
	// LoginMethod 是最近一次成功登录方式。
	LoginMethod string `json:"login_method"`
	// LastLoginAt 是最近一次成功登录时间。
	LastLoginAt int64 `json:"last_login_at"`
	// ProfileError 是资料刷新错误；当前列表接口保留为空字符串。
	ProfileError string `json:"profile_error"`
	// AIEnabled 表示账号级 AI 回复是否启用。
	AIEnabled bool `json:"ai_enabled"`
	// AutoRateEnabled 表示自动评价计划是否启用。
	AutoRateEnabled bool `json:"auto_rate_enabled"`
	// RateContent 是自动评价文案。
	RateContent string `json:"rate_content"`
	// AutoPolishEnabled 表示自动擦亮计划是否启用。
	AutoPolishEnabled bool `json:"auto_polish_enabled"`
	// PolishTime 是自动擦亮的本地时间。
	PolishTime string `json:"polish_time"`
	// LastRateScanAt 是最近一次自动评价扫描时间。
	LastRateScanAt int64 `json:"last_rate_scan_at"`
	// LastPolishDate 是最近一次自动擦亮日期。
	LastPolishDate string `json:"last_polish_date"`
	// LastPolishAt 是最近一次自动擦亮时间。
	LastPolishAt int64 `json:"last_polish_at"`
}

// writeErrRequest 写带请求追踪标识的统一错误响应。
func writeErrRequest(w http.ResponseWriter, r *http.Request, status int, msg string) {
	writeErrCode(w, status, "", msg, middleware.GetReqID(r.Context()))
}

// writeErrCode 写指定机器可读错误码和可选请求追踪标识的统一错误响应。
func writeErrCode(w http.ResponseWriter, status int, code, msg, requestID string) {
	if code == "" && requestID == "" {
		// payload 是兼容历史内部调用的统一错误对象。
		payload := unifiedErrorPayload(msg)
		payload["code"] = httpapi.CodeForStatus(status)
		writeJSON(w, status, payload)
		return
	}
	httpapi.WriteError(w, status, code, msg, requestID)
}

// writeCredentialVerificationError 将当前密码校验失败区分为认证失败或内部故障。
func writeCredentialVerificationError(w http.ResponseWriter, err error) {
	if errors.Is(err, db.ErrPasswordMismatch) {
		writeErrCode(w, http.StatusUnauthorized, "authentication_failed", "当前密码错误", "")
		return
	}
	writeErr(w, http.StatusInternalServerError, "验证当前密码失败")
}
