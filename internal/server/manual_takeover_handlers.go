package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	accountapp "xianyu-go/internal/application/account"
)

// manualTakeoverSettingsRequest 只允许更新人工介入暂停配置，不包含 Cookie、登录密码或其他账号凭据。
type manualTakeoverSettingsRequest struct {
	// Minutes 是人工回复后暂停自动回复的分钟数；0 表示关闭。
	Minutes *int `json:"manual_takeover_minutes"`
	// Scope 是暂停范围：ai 仅暂停 Agent 与 AI，all 暂停全部自动回复。
	Scope *string `json:"manual_takeover_scope"`
}

// manualTakeoverSettingsResponse 返回本次人工介入配置的非敏感状态。
type manualTakeoverSettingsResponse struct {
	// Success 表示设置已保存。
	Success bool `json:"success"`
	// Minutes 是当前生效的暂停分钟数。
	Minutes int `json:"manual_takeover_minutes"`
	// Scope 是当前生效的暂停范围。
	Scope string `json:"manual_takeover_scope"`
}

// updateChatManualTakeover 只更新人工介入暂停配置，避免把包含 Cookie 和密码字段的通用账号设置接口开放给外部令牌。
func (s *Server) updateChatManualTakeover(w http.ResponseWriter, r *http.Request) {
	// cookieID 是路由中的账号标识。
	cookieID := strings.TrimSpace(chi.URLParam(r, "cookie_id"))
	// ownedDetail、ok 保存账号非敏感摘要和当前用户归属校验结果。
	ownedDetail, ok := s.requireCookieOwner(w, r, cookieID)
	if !ok {
		return
	}
	setNoStore(w)
	// request 保存严格限于人工介入配置的请求字段。
	var request manualTakeoverSettingsRequest
	// err 是解析窄范围人工介入配置请求时的 JSON 格式错误。
	if err := decodeJSON(r, &request); err != nil || (request.Minutes == nil && request.Scope == nil) {
		writeErr(w, http.StatusBadRequest, "至少提供一个人工介入配置字段")
		return
	}
	// result、updateErr 保存应用层设置写入结果和错误。
	result, updateErr := s.accountSettingsApplication().UpdateSettings(r.Context(), accountapp.SettingsUpdateInput{
		UserID: ownedDetail.UserID, AccountID: cookieID,
		ManualTakeoverMinutes: request.Minutes, ManualTakeoverScope: request.Scope,
	})
	if updateErr != nil {
		switch {
		case errors.Is(updateErr, accountapp.ErrInvalidManualTakeover):
			writeErr(w, http.StatusBadRequest, updateErr.Error())
		case errors.Is(updateErr, accountapp.ErrForbidden):
			writeErr(w, http.StatusForbidden, "无权修改该账号的人工介入配置")
		case errors.Is(updateErr, accountapp.ErrNotFound):
			writeErr(w, http.StatusNotFound, "账号不存在")
		default:
			writeInternalErr(w, r, http.StatusInternalServerError, "保存人工介入配置失败", updateErr)
		}
		return
	}
	// minutes、scope 以当前账号摘要为基线，只覆盖本次明确提交的字段。
	minutes, scope := ownedDetail.ManualTakeoverMinutes, ownedDetail.ManualTakeoverScope
	if request.Minutes != nil {
		minutes = *request.Minutes
	}
	if request.Scope != nil {
		scope = *request.Scope
	}
	if result.RuntimeError != nil {
		writeErr(w, http.StatusServiceUnavailable, "人工介入配置已保存，但账号运行状态收口失败")
		return
	}
	setNoStore(w)
	writeJSON(w, http.StatusOK, manualTakeoverSettingsResponse{Success: true, Minutes: minutes, Scope: scope})
}
