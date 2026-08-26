package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	deliveryapp "xianyu-go/internal/application/deliverytemplate"
	"xianyu-go/internal/auth"
)

// deliveryTemplateMessageRequest 是模板消息写入请求 DTO。
type deliveryTemplateMessageRequest struct {
	// Content 是一条独立发送消息正文。
	Content string `json:"content"`
}

// deliveryTemplateRequest 是模板创建和更新请求 DTO。
type deliveryTemplateRequest struct {
	// Name 是模板名称。
	Name string `json:"name"`
	// Enabled 是可选的启用状态，缺省时按启用处理。
	Enabled *bool `json:"enabled"`
	// Messages 是按发送顺序排列的消息。
	Messages []deliveryTemplateMessageRequest `json:"messages"`
}

// deliveryTemplateMessageResponse 是模板消息响应 DTO。
type deliveryTemplateMessageResponse struct {
	// ID 是消息标识。
	ID int64 `json:"id"`
	// SortOrder 是消息发送顺序。
	SortOrder int `json:"sort_order"`
	// Content 是消息正文。
	Content string `json:"content"`
}

// deliveryTemplateResponse 是模板查询响应 DTO。
type deliveryTemplateResponse struct {
	// ID 是模板标识。
	ID int64 `json:"id"`
	// Name 是模板名称。
	Name string `json:"name"`
	// Enabled 是模板启用状态。
	Enabled bool `json:"enabled"`
	// Messages 是有序消息列表。
	Messages []deliveryTemplateMessageResponse `json:"messages"`
	// Keys 是模板变量键。
	Keys []string `json:"keys"`
	// CustomKeys 是模板引用的发货规则自定义变量键。
	CustomKeys []string `json:"custom_keys"`
	// CreatedAt 是创建时间文本。
	CreatedAt string `json:"created_at"`
	// UpdatedAt 是更新时间文本。
	UpdatedAt string `json:"updated_at"`
}

// deliveryTemplateListResponse 是模板列表响应 DTO。
type deliveryTemplateListResponse struct {
	// Success 表示查询成功。
	Success bool `json:"success"`
	// Data 是模板列表。
	Data []deliveryTemplateResponse `json:"data"`
}

// deliveryTemplateMutationResponse 是模板变更响应 DTO。
type deliveryTemplateMutationResponse struct {
	// Success 表示变更成功。
	Success bool `json:"success"`
	// ID 是新建模板标识，更新和删除时为空。
	ID int64 `json:"id,omitempty"`
}

// listDeliveryTemplates 查询当前用户的发货模板列表。
func (s *Server) listDeliveryTemplates(w http.ResponseWriter, r *http.Request) {
	// session 保存认证中间件解析出的当前用户身份。
	session := auth.SessionFromContext(r.Context())
	// items、err 保存模板列表及查询错误。
	items, err := s.deliveryTemplatesApplication().List(r.Context(), session.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询发货模板失败")
		return
	}
	writeJSON(w, http.StatusOK, deliveryTemplateListResponse{Success: true, Data: deliveryTemplateResponses(items)})
}

// getDeliveryTemplate 查询当前用户拥有的单个发货模板。
func (s *Server) getDeliveryTemplate(w http.ResponseWriter, r *http.Request) {
	// templateID 保存路径中的模板标识。
	templateID, err := parseDeliveryTemplateID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// session 保存认证中间件解析出的当前用户身份。
	session := auth.SessionFromContext(r.Context())
	// item、err 保存单个模板及查询错误。
	item, err := s.deliveryTemplatesApplication().Get(r.Context(), session.UserID, templateID)
	if errors.Is(err, deliveryapp.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "发货模板不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询发货模板失败")
		return
	}
	writeJSON(w, http.StatusOK, deliveryTemplateResponseModel(item))
}

// createDeliveryTemplate 创建当前用户的发货模板。
func (s *Server) createDeliveryTemplate(w http.ResponseWriter, r *http.Request) {
	// session 保存认证中间件解析出的当前用户身份。
	session := auth.SessionFromContext(r.Context())
	// draft 保存经过 transport DTO 转换的模板草稿。
	// draft、err 保存模板草稿及请求解码错误。
	draft, err := decodeDeliveryTemplateDraft(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// templateID 保存新模板标识。
	// templateID、err 保存新模板标识及创建错误。
	templateID, err := s.deliveryTemplatesApplication().Create(r.Context(), session.UserID, draft)
	if err != nil {
		if errors.Is(err, deliveryapp.ErrInvalidInput) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "创建发货模板失败")
		return
	}
	writeJSON(w, http.StatusOK, deliveryTemplateMutationResponse{Success: true, ID: templateID})
}

// updateDeliveryTemplate 更新当前用户拥有的发货模板。
func (s *Server) updateDeliveryTemplate(w http.ResponseWriter, r *http.Request) {
	// templateID 保存路径中的模板标识。
	// templateID、err 保存路径模板标识及解析错误。
	templateID, err := parseDeliveryTemplateID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// session 保存认证中间件解析出的当前用户身份。
	session := auth.SessionFromContext(r.Context())
	// draft 保存经过 transport DTO 转换的模板草稿。
	// draft、err 保存模板草稿及请求解码错误。
	draft, err := decodeDeliveryTemplateDraft(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// err 保存模板更新错误。
	if err := s.deliveryTemplatesApplication().Update(r.Context(), session.UserID, templateID, draft); err != nil {
		if errors.Is(err, deliveryapp.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "发货模板不存在")
			return
		}
		if errors.Is(err, deliveryapp.ErrVariableConflict) {
			writeErr(w, http.StatusConflict, "发货模板变量已被自动化规则引用，不能不兼容修改")
			return
		}
		if errors.Is(err, deliveryapp.ErrInvalidInput) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "更新发货模板失败")
		return
	}
	writeJSON(w, http.StatusOK, deliveryTemplateMutationResponse{Success: true})
}

// deleteDeliveryTemplate 逻辑删除当前用户拥有的发货模板。
func (s *Server) deleteDeliveryTemplate(w http.ResponseWriter, r *http.Request) {
	// templateID 保存路径中的模板标识。
	// templateID、err 保存路径模板标识及解析错误。
	templateID, err := parseDeliveryTemplateID(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// session 保存认证中间件解析出的当前用户身份。
	session := auth.SessionFromContext(r.Context())
	// err 保存模板删除错误。
	if err := s.deliveryTemplatesApplication().Delete(r.Context(), session.UserID, templateID); err != nil {
		if errors.Is(err, deliveryapp.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "发货模板不存在")
			return
		}
		if errors.Is(err, deliveryapp.ErrReferenced) {
			writeErr(w, http.StatusConflict, "发货模板仍被自动化规则引用")
			return
		}
		writeErr(w, http.StatusInternalServerError, "删除发货模板失败")
		return
	}
	writeJSON(w, http.StatusOK, deliveryTemplateMutationResponse{Success: true})
}

// parseDeliveryTemplateID 解析并校验模板路径参数。
func parseDeliveryTemplateID(r *http.Request) (int64, error) {
	// rawID 保存路径中的原始模板标识。
	rawID := chi.URLParam(r, "template_id")
	// templateID 保存解析后的正数模板标识。
	templateID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || templateID <= 0 {
		return 0, errors.New("无效发货模板ID")
	}
	return templateID, nil
}

// decodeDeliveryTemplateDraft 将 HTTP 请求转换为应用层模板草稿。
func decodeDeliveryTemplateDraft(r *http.Request) (deliveryapp.Draft, error) {
	// request 保存解码后的请求 DTO。
	var request deliveryTemplateRequest
	// err 保存请求 JSON 解码错误。
	if err := decodeJSON(r, &request); err != nil {
		return deliveryapp.Draft{}, errors.New("请求格式错误")
	}
	// messages 保存去除消息字段包装后的正文列表。
	messages := make([]string, 0, len(request.Messages))
	for /* message 表示请求中的一条模板消息。 */ _, message := range request.Messages {
		messages = append(messages, message.Content)
	}
	// enabled 保存缺省启用状态。
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	return deliveryapp.Draft{Name: strings.TrimSpace(request.Name), Enabled: enabled, Messages: messages}, nil
}

// deliveryTemplateResponses 将应用模板列表转换为响应 DTO 列表。
func deliveryTemplateResponses(items []deliveryapp.Template) []deliveryTemplateResponse {
	// responses 保存模板列表 DTO。
	responses := make([]deliveryTemplateResponse, 0, len(items))
	for /* item 表示当前待转换的应用模板。 */ _, item := range items {
		responses = append(responses, deliveryTemplateResponseModel(item))
	}
	return responses
}

// deliveryTemplateResponseModel 将单个应用模板转换为响应 DTO。
func deliveryTemplateResponseModel(item deliveryapp.Template) deliveryTemplateResponse {
	// messages 保存消息响应 DTO 列表。
	messages := make([]deliveryTemplateMessageResponse, 0, len(item.Messages))
	for /* message 表示当前模板消息。 */ _, message := range item.Messages {
		messages = append(messages, deliveryTemplateMessageResponse{ID: message.ID, SortOrder: message.SortOrder, Content: message.Content})
	}
	return deliveryTemplateResponse{ID: item.ID, Name: item.Name, Enabled: item.Enabled, Messages: messages, Keys: append([]string{}, item.Keys...), CustomKeys: append([]string{}, item.CustomKeys...), CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}
