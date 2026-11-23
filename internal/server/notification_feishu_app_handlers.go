package server

import (
	"encoding/base64"
	"errors"
	"io"
	"net/http"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/go-chi/chi/v5"

	notificationsapp "xianyu-go/internal/application/notifications"
)

// feishuAppBindingStartRequest 是开始飞书应用扫码绑定的具名请求 DTO，字段与通知渠道表单共用。
type feishuAppBindingStartRequest struct {
	// Name 是渠道名称；空白时由应用层使用默认名称。
	Name string `json:"name"`
	// EventTypes 是订阅事件类型 JSON 数组文本或兼容分隔文本；空值表示接收全部事件。
	EventTypes string `json:"event_types"`
	// Enabled 表示绑定成功后渠道是否启用；nil 按 true 处理。
	Enabled *bool `json:"enabled"`
	// AccountIDs 是绑定成功后渠道应绑定的账号集合；省略（nil）表示不改动现有绑定，空数组表示不绑定任何账号。
	AccountIDs []string `json:"account_ids"`
}

// feishuAppBindingStartResponse 是开始扫码绑定的具名响应 DTO。
type feishuAppBindingStartResponse struct {
	// SessionID 是进程内扫码会话标识。
	SessionID string `json:"session_id"`
	// QRCodeURL 是可直接展示的二维码 PNG data URL。
	QRCodeURL string `json:"qr_code_url"`
	// ExpiresIn 是扫码会话有效秒数。
	ExpiresIn int `json:"expires_in"`
}

// feishuAppBindingStatusResponse 是扫码绑定状态的具名响应 DTO。
type feishuAppBindingStatusResponse struct {
	// Status 是 waiting、success、expired 或 error。
	Status string `json:"status"`
	// Message 是可展示的中文说明，不包含平台原始响应或凭据。
	Message string `json:"message,omitempty"`
	// ChannelID 是成功时已创建或更新的通知渠道标识。
	ChannelID int64 `json:"channel_id,omitempty"`
}

// startFeishuAppBinding 发起飞书应用扫码绑定并返回二维码 PNG data URL。
func (s *Server) startFeishuAppBinding(w http.ResponseWriter, r *http.Request) {
	// 二维码包含短期设备码，禁止浏览器或中间层缓存该响应。
	w.Header().Set("Cache-Control", "no-store")
	// request 保存可选绑定请求；空请求体按默认名称、全部事件、启用处理。
	var request feishuAppBindingStartRequest
	// decodeErr 是可选请求体解码错误；仅 EOF 表示允许的空请求体。
	if decodeErr := decodeJSON(r, &request); decodeErr != nil && !errors.Is(decodeErr, io.EOF) {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// enabled 是最终启用状态；请求省略时默认为 true。
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	// start、startErr 保存开始绑定结果及应用错误。
	start, startErr := s.notificationChannelsApplication().StartFeishuAppBinding(r.Context(), authSess(r).UserID, notificationsapp.FeishuBindingOptions{
		Name:       request.Name,
		EventTypes: request.EventTypes,
		Enabled:    enabled,
	})
	if startErr != nil {
		if errors.Is(startErr, notificationsapp.ErrChannelInvalidInput) {
			writeErr(w, http.StatusBadRequest, "飞书绑定参数无效")
			return
		}
		if errors.Is(startErr, notificationsapp.ErrFeishuUnavailable) {
			writeInternalErr(w, r, http.StatusInternalServerError, "飞书应用绑定未启用", startErr)
			return
		}
		writeInternalErr(w, r, http.StatusInternalServerError, "发起飞书应用绑定失败", startErr)
		return
	}
	// qrDataURL、qrErr 保存二维码 PNG data URL 及生成错误。
	qrDataURL, qrErr := feishuQRCodeDataURL(start.QRCodeURL)
	if qrErr != nil {
		writeInternalErr(w, r, http.StatusInternalServerError, "生成飞书绑定二维码失败", qrErr)
		return
	}
	writeJSON(w, http.StatusOK, feishuAppBindingStartResponse{SessionID: start.SessionID, QRCodeURL: qrDataURL, ExpiresIn: start.ExpiresIn})
}

// getFeishuAppBindingStatus 查询当前用户的飞书扫码绑定状态。
func (s *Server) getFeishuAppBindingStatus(w http.ResponseWriter, r *http.Request) {
	// 绑定状态与会话标识相关，禁止浏览器或中间层缓存该响应。
	w.Header().Set("Cache-Control", "no-store")
	// sessionID 是路径中的扫码会话标识。
	sessionID := chi.URLParam(r, "session_id")
	// status、statusErr 保存扫码状态及应用错误。
	status, statusErr := s.notificationChannelsApplication().GetFeishuAppBindingStatus(r.Context(), authSess(r).UserID, sessionID)
	if statusErr != nil {
		if errors.Is(statusErr, notificationsapp.ErrChannelInvalidInput) {
			writeErr(w, http.StatusBadRequest, "飞书绑定会话参数无效")
			return
		}
		if errors.Is(statusErr, notificationsapp.ErrFeishuSessionNotFound) {
			writeErr(w, http.StatusNotFound, "飞书绑定会话不存在或已过期")
			return
		}
		if errors.Is(statusErr, notificationsapp.ErrFeishuUnavailable) {
			writeInternalErr(w, r, http.StatusInternalServerError, "飞书应用绑定未启用", statusErr)
			return
		}
		writeInternalErr(w, r, http.StatusInternalServerError, "查询飞书绑定状态失败", statusErr)
		return
	}
	writeJSON(w, http.StatusOK, feishuAppBindingStatusResponse{Status: status.Status, Message: status.Message, ChannelID: status.ChannelID})
}

// feishuQRCodeDataURL 把扫码地址编码为 PNG data URL，供前端直接放入图片源。
// content 是飞书扫码地址；返回值以 data:image/png;base64, 开头，错误不包含原始地址。
func feishuQRCodeDataURL(content string) (string, error) {
	// qrCode、qrErr 保存二维码编码器及构造错误。
	qrCode, qrErr := qrcode.New(content, qrcode.Medium)
	if qrErr != nil {
		return "", errors.New("编码飞书二维码失败")
	}
	qrCode.DisableBorder = false
	// pngBytes、pngErr 保存二维码 PNG 字节及编码错误。
	pngBytes, pngErr := qrCode.PNG(320)
	if pngErr != nil {
		return "", errors.New("生成飞书二维码图片失败")
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes), nil
}
