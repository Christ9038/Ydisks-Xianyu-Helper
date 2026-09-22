package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/auth"
)

// itemAISettingsUpdateRequest 是保存商品级 AI 配置的 HTTP 请求 DTO。
type itemAISettingsUpdateRequest struct {
	// AIOverride 是 inherit、enabled 或 disabled 三态值。
	AIOverride string `json:"ai_override"`
	// ItemContext 是商品专属客服资料。
	ItemContext string `json:"item_context"`
}

// itemAISettingsResponse 是商品级 AI 配置接口的具名响应 DTO。
type itemAISettingsResponse struct {
	// CookieID 是商品所属账号标识。
	CookieID string `json:"cookie_id"`
	// ItemID 是平台商品标识。
	ItemID string `json:"item_id"`
	// AIOverride 是 inherit、enabled 或 disabled。
	AIOverride string `json:"ai_override"`
	// ItemContext 是商品专属客服资料。
	ItemContext string `json:"item_context"`
}

// getItemAISettings 返回单件商品的完整 AI 配置；无独立记录时返回继承账号配置。
func (s *Server) getItemAISettings(w http.ResponseWriter, r *http.Request) {
	// cookieID 和 itemID 是当前请求定位商品的稳定标识。
	cookieID, itemID := chi.URLParam(r, "cookie_id"), chi.URLParam(r, "item_id")
	if !s.requireCookieOwnership(w, r, cookieID) {
		return
	}
	// session 保存认证中间件注入的当前用户身份。
	session := auth.SessionFromContext(r.Context())
	// settings 和 err 是应用服务完成归属校验后的商品配置结果。
	settings, err := s.itemAISettingsApplication().Get(r.Context(), session.UserID, cookieID, itemID)
	if err != nil {
		s.writeItemAISettingsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, itemAISettingsHTTPResponse(settings))
}

// updateItemAISettings 校验并保存单件商品的 AI 覆盖状态和专属资料。
func (s *Server) updateItemAISettings(w http.ResponseWriter, r *http.Request) {
	// cookieID 和 itemID 是当前请求定位商品的稳定标识。
	cookieID, itemID := chi.URLParam(r, "cookie_id"), chi.URLParam(r, "item_id")
	if !s.requireCookieOwnership(w, r, cookieID) {
		return
	}
	// request 保存解码后的商品 AI 配置。
	var request itemAISettingsUpdateRequest
	if // err 是请求正文解码错误。
	err := decodeJSON(r, &request); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// session 保存认证中间件注入的当前用户身份。
	session := auth.SessionFromContext(r.Context())
	// settings 是传入应用服务的商品级 AI 配置模型。
	settings := itemapp.ItemAISettings{CookieID: cookieID, ItemID: itemID, Override: request.AIOverride, Context: request.ItemContext}
	if // err 是商品 AI 配置保存失败原因。
	err := s.itemAISettingsApplication().Save(r.Context(), session.UserID, settings); err != nil {
		s.writeItemAISettingsError(w, err)
		return
	}
	// saved 是删除默认记录或规范化保存后的最终配置。
	// saved 和 err 是保存后重新读取的规范化配置及读取错误。
	saved, err := s.itemAISettingsApplication().Get(r.Context(), session.UserID, cookieID, itemID)
	if err != nil {
		s.writeItemAISettingsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, itemAISettingsHTTPResponse(saved))
}

// writeItemAISettingsError 将应用层稳定错误映射为 HTTP 状态。
func (s *Server) writeItemAISettingsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, itemapp.ErrItemAISettingsForbidden):
		writeErr(w, http.StatusForbidden, err.Error())
	case errors.Is(err, itemapp.ErrItemAISettingsItemNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, itemapp.ErrItemAISettingsInvalidOverride), errors.Is(err, itemapp.ErrItemAISettingsContextTooLong), errors.Is(err, itemapp.ErrItemAISettingsInvalidContext):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, "商品 AI 配置操作失败")
	}
}

// itemAISettingsHTTPResponse 将应用模型转换为具名成功响应 DTO。
func itemAISettingsHTTPResponse(settings itemapp.ItemAISettings) itemAISettingsResponse {
	return itemAISettingsResponse{CookieID: settings.CookieID, ItemID: settings.ItemID, AIOverride: settings.Override, ItemContext: settings.Context}
}
