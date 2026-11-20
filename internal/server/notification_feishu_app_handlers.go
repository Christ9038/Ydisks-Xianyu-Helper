package server

import (
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strconv"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/go-chi/chi/v5"

	notificationsapp "xianyu-go/internal/application/notifications"
)

// feishuAppBindingStartRequest 是开始飞书应用扫码绑定的具名请求 DTO。
type feishuAppBindingStartRequest struct {
	// BindAllAccounts 表示绑定成功后是否把新渠道绑定到当前用户全部账号；nil 按 true 处理。
	BindAllAccounts *bool `json:"bind_all_accounts"`
	// NotifyWarning 表示是否推送警告类通知；nil 按 true 处理。
	NotifyWarning *bool `json:"notify_warning"`
	// NotifyInfo 表示是否推送普通提示类通知；nil 按 true 处理。
	NotifyInfo *bool `json:"notify_info"`
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

// feishuChannelCategoriesResponse 是飞书应用渠道分类开关键控的具名响应 DTO。
type feishuChannelCategoriesResponse struct {
	// Warning 表示是否推送警告类通知。
	Warning bool `json:"warning"`
	// Info 表示是否推送普通提示类通知。
	Info bool `json:"info"`
}

// feishuChannelCategoriesUpdateRequest 是飞书应用渠道分类开关更新的具名请求 DTO；两个字段都必须提供。
type feishuChannelCategoriesUpdateRequest struct {
	// Warning 是警告类推送开关；nil 表示请求缺少必填字段。
	Warning *bool `json:"warning"`
	// Info 是普通提示类推送开关；nil 表示请求缺少必填字段。
	Info *bool `json:"info"`
}

// startFeishuAppBinding 发起飞书应用扫码绑定并返回二维码 PNG data URL。
func (s *Server) startFeishuAppBinding(w http.ResponseWriter, r *http.Request) {
	// 二维码包含短期设备码，禁止浏览器或中间层缓存该响应。
	w.Header().Set("Cache-Control", "no-store")
	// request 保存可选绑定请求；空请求体按默认绑定全部账号处理。
	var request feishuAppBindingStartRequest
	// decodeErr 是可选请求体解码错误；仅 EOF 表示允许的空请求体。
	if decodeErr := decodeJSON(r, &request); decodeErr != nil && !errors.Is(decodeErr, io.EOF) {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// bindAll 是最终账号绑定策略；请求省略时默认为 true。
	bindAll := true
	if request.BindAllAccounts != nil {
		bindAll = *request.BindAllAccounts
	}
	// notifyWarning 是最终警告类推送开关；请求省略时默认为 true。
	notifyWarning := true
	if request.NotifyWarning != nil {
		notifyWarning = *request.NotifyWarning
	}
	// notifyInfo 是最终普通提示类推送开关；请求省略时默认为 true。
	notifyInfo := true
	if request.NotifyInfo != nil {
		notifyInfo = *request.NotifyInfo
	}
	// start、startErr 保存开始绑定结果及应用错误。
	start, startErr := s.notificationChannelsApplication().StartFeishuAppBinding(r.Context(), authSess(r).UserID, notificationsapp.FeishuBindingOptions{
		BindAllAccounts: bindAll,
		NotifyWarning:   notifyWarning,
		NotifyInfo:      notifyInfo,
	})
	if startErr != nil {
		if errors.Is(startErr, notificationsapp.ErrChannelInvalidInput) {
			writeErr(w, http.StatusBadRequest, "飞书绑定参数无效")
			return
		}
		if errors.Is(startErr, notificationsapp.ErrFeishuUnavailable) {
			writeErr(w, http.StatusInternalServerError, "飞书应用绑定未启用")
			return
		}
		writeErr(w, http.StatusInternalServerError, "发起飞书应用绑定失败")
		return
	}
	// qrDataURL、qrErr 保存二维码 PNG data URL 及生成错误。
	qrDataURL, qrErr := feishuQRCodeDataURL(start.QRCodeURL)
	if qrErr != nil {
		writeErr(w, http.StatusInternalServerError, "生成飞书绑定二维码失败")
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
			writeErr(w, http.StatusInternalServerError, "飞书应用绑定未启用")
			return
		}
		writeErr(w, http.StatusInternalServerError, "查询飞书绑定状态失败")
		return
	}
	writeJSON(w, http.StatusOK, feishuAppBindingStatusResponse{Status: status.Status, Message: status.Message, ChannelID: status.ChannelID})
}

// getFeishuChannelCategories 返回飞书应用渠道当前的警告与普通提示开关。
func (s *Server) getFeishuChannelCategories(w http.ResponseWriter, r *http.Request) {
	// channelID、parseErr 保存路径中的渠道 ID 及解析错误。
	channelID, parseErr := strconv.ParseInt(chi.URLParam(r, "channel_id"), 10, 64)
	if parseErr != nil || channelID <= 0 {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	// categories、categoriesErr 保存分类开关读取结果及应用错误。
	categories, categoriesErr := s.notificationChannelsApplication().GetFeishuChannelCategories(r.Context(), authSess(r).UserID, channelID)
	if categoriesErr != nil {
		writeFeishuChannelCategoriesError(w, categoriesErr)
		return
	}
	writeJSON(w, http.StatusOK, feishuChannelCategoriesResponse{Warning: categories.Warning, Info: categories.Info})
}

// setFeishuChannelCategories 更新飞书应用渠道的警告与普通提示开关。
func (s *Server) setFeishuChannelCategories(w http.ResponseWriter, r *http.Request) {
	// channelID、parseErr 保存路径中的渠道 ID 及解析错误。
	channelID, parseErr := strconv.ParseInt(chi.URLParam(r, "channel_id"), 10, 64)
	if parseErr != nil || channelID <= 0 {
		writeErr(w, http.StatusBadRequest, "无效ID")
		return
	}
	// request 保存两个必填布尔开关；nil 表示客户端遗漏字段。
	var request feishuChannelCategoriesUpdateRequest
	// decodeErr 是分类开关请求体解码错误；nil 表示请求体可解析。
	if decodeErr := decodeJSON(r, &request); decodeErr != nil || request.Warning == nil || request.Info == nil {
		writeErr(w, http.StatusBadRequest, "warning 和 info 必填")
		return
	}
	// setErr 保存分类开关写回结果及应用错误。
	setErr := s.notificationChannelsApplication().SetFeishuChannelCategories(r.Context(), authSess(r).UserID, channelID, notificationsapp.FeishuChannelCategories{Warning: *request.Warning, Info: *request.Info})
	if setErr != nil {
		writeFeishuChannelCategoriesError(w, setErr)
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// writeFeishuChannelCategoriesError 将分类开关用例错误映射为统一错误 envelope，不返回渠道配置内容。
func writeFeishuChannelCategoriesError(w http.ResponseWriter, err error) {
	if errors.Is(err, notificationsapp.ErrChannelInvalidInput) {
		writeErr(w, http.StatusBadRequest, "该通知渠道不支持飞书分类开关")
		return
	}
	if errors.Is(err, notificationsapp.ErrChannelNotFound) {
		writeErr(w, http.StatusNotFound, "通知渠道不存在")
		return
	}
	if errors.Is(err, notificationsapp.ErrChannelForbidden) {
		writeErr(w, http.StatusForbidden, "无权操作该通知渠道")
		return
	}
	writeErr(w, http.StatusInternalServerError, "飞书渠道分类开关操作失败")
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
