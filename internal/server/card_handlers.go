package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	cardsapp "xianyu-go/internal/application/cards"
	"xianyu-go/internal/auth"
)

// cardMutationRequest 是卡券创建与更新接口共用的具名请求 DTO。
type cardMutationRequest struct {
	// Name 是用户可见的卡券组名称。
	Name string `json:"name"`
	// Type 是 text、data、image 或历史兼容的 api 类型。
	Type string `json:"type"`
	// APIConfig 同时接收新版具名对象和历史完整 JSON 字符串。
	APIConfig json.RawMessage `json:"api_config"`
	// TextContent 是 text 类型自动发货时发送的文本内容。
	TextContent string `json:"text_content"`
	// DataContent 是 data 类型尚未消费的逐行卡密库存。
	DataContent string `json:"data_content"`
	// ImageURL 是 image 类型自动发货时发送的图片地址。
	ImageURL string `json:"image_url"`
	// Description 是用户维护的卡券组说明。
	Description string `json:"description"`
	// Enabled 表示保存后是否允许自动化规则使用该卡券组。
	Enabled bool `json:"enabled"`
	// DelaySeconds 是自动发货前的延迟秒数。
	DelaySeconds int `json:"delay_seconds"`
	// IsMultiSpec 表示卡券组是否只匹配指定商品规格。
	IsMultiSpec bool `json:"is_multi_spec"`
	// SpecName 是多规格匹配使用的规格名称。
	SpecName string `json:"spec_name"`
	// SpecValue 是多规格匹配使用的规格值。
	SpecValue string `json:"spec_value"`
}

// cardAppendRequest 是 data 类型卡券追加库存接口的具名请求 DTO。
type cardAppendRequest struct {
	// Content 是待追加的逐行卡密文本，空行由数据库层按既有规则处理。
	Content string `json:"content"`
}

// cardAPITestRequest 是临时 API 测试请求的具名 DTO。
type cardAPITestRequest struct {
	// APIConfig 是与卡券保存接口一致的 API 配置对象或历史 JSON 字符串。
	APIConfig json.RawMessage `json:"api_config"`
}

// cardAPITestResponse 是临时 API 测试结果的具名脱敏 DTO。
type cardAPITestResponse struct {
	// Status 表示远端请求是否成功。
	Status string `json:"status"`
	// StatusCode 是远端 HTTP 状态码。
	StatusCode int `json:"status_code"`
	// ResponseContentType 是远端响应媒体类型。
	ResponseContentType string `json:"response_content_type"`
	// ResponseFields 是 JSON 顶层字段名称。
	ResponseFields []string `json:"response_fields"`
	// ExtractedValue 是按配置路径提取的值。
	ExtractedValue string `json:"extracted_value,omitempty"`
	// ResponsePreview 是限长响应预览。
	ResponsePreview string `json:"response_preview,omitempty"`
}

// mountCardsReal 挂载卡券 CRUD、批量创建和库存追加路由；发货规则由 automation_rules 负责。
func (s *Server) mountCardsReal(r chi.Router) {
	r.Get("/cards", s.listCards)
	r.Post("/cards", s.createCard)
	r.Post("/cards/test-api", s.testCardAPI)
	r.Post("/cards/batch", s.batchCreateCards)
	r.Post("/cards/{card_id}/append-data", s.appendCardData)
	r.Get("/cards/{card_id}/details", s.getCard)
	r.Get("/cards/{card_id}", s.getCard)
	r.Put("/cards/{card_id}", s.updateCard)
	r.Delete("/cards/{card_id}", s.deleteCard)
}

// testCardAPI 发送一次临时 API 请求并返回状态、字段和提取结果诊断。
func (s *Server) testCardAPI(w http.ResponseWriter, r *http.Request) {
	if s.applications == nil || s.applications.apiRequestTester == nil {
		writeErr(w, http.StatusServiceUnavailable, "API 测试服务不可用")
		return
	}
	// request 是当前 API 测试请求的具名 HTTP DTO。
	var request cardAPITestRequest
	// err 是当前请求 JSON 解码错误，不包含用户填写的敏感模板值。
	if err := decodeJSON(r, &request); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// config、err 分别是可供应用 Port 解析的配置文本和兼容解码错误。
	config, err := decodeCardAPIConfig(request.APIConfig)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// result、err 分别是临时远端调用的脱敏诊断和本地配置或网络错误。
	result, err := s.applications.apiRequestTester.Test(r.Context(), cardsapp.APIRequestTestInput{Config: config})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cardAPITestResponse{Status: result.Status, StatusCode: result.StatusCode, ResponseContentType: result.ResponseContentType, ResponseFields: result.ResponseFields, ExtractedValue: result.ExtractedValue, ResponsePreview: result.ResponsePreview})
}

// listCards 将认证用户交给应用服务，并把卡券应用模型转换为 HTTP DTO。
func (s *Server) listCards(w http.ResponseWriter, r *http.Request) {
	// session 是认证中间件注入的当前用户会话。
	session := auth.SessionFromContext(r.Context())
	// cards、err 保存应用服务返回的用户卡券列表及查询错误。
	cards, err := s.cardsApplication().List(r.Context(), session.UserID)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Error("查询卡密失败", "user_id", session.UserID, "err", err)
		}
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, newCardResponses(cards))
}

// getCard 解析路径标识并由应用服务统一校验资源存在性和用户归属。
func (s *Server) getCard(w http.ResponseWriter, r *http.Request) {
	// cardID、parseErr 保存路径中的卡券标识及数字解析错误。
	cardID, parseErr := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if parseErr != nil {
		writeErr(w, http.StatusBadRequest, "无效卡券ID")
		return
	}
	// session 是认证中间件注入的当前用户会话。
	session := auth.SessionFromContext(r.Context())
	// card、err 保存应用服务返回的卡券组及所有权或查询错误。
	card, err := s.cardsApplication().Get(r.Context(), session.UserID, cardID)
	if err != nil {
		writeCardReadError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newCardResponse(card))
}

// createCard 解码 HTTP 请求，并由应用服务完成输入校验、类型限制和所有者写入。
func (s *Server) createCard(w http.ResponseWriter, r *http.Request) {
	// draft、decodeErr 保存请求转换后的应用输入及 JSON 解码错误。
	draft, decodeErr := decodeCardDraft(r)
	if decodeErr != nil {
		writeErr(w, http.StatusBadRequest, decodeErr.Error())
		return
	}
	// session 是认证中间件注入的当前用户会话。
	session := auth.SessionFromContext(r.Context())
	// cardID、err 保存创建后的卡券标识及业务或持久化错误。
	cardID, err := s.cardsApplication().Create(r.Context(), session.UserID, draft)
	if err != nil {
		// validationError 用于识别可直接返回客户端的稳定业务校验提示。
		var validationError *cardsapp.ValidationError
		switch {
		case errors.As(err, &validationError):
			writeErr(w, http.StatusBadRequest, validationError.Error())
		default:
			writeErr(w, http.StatusInternalServerError, "创建失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, mutationIDResponse{Success: true, ID: cardID})
}

// updateCard 解码路径和请求体，并由应用服务完成输入校验、归属校验及更新。
func (s *Server) updateCard(w http.ResponseWriter, r *http.Request) {
	// cardID、parseErr 保存路径中的卡券标识及数字解析错误。
	cardID, parseErr := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if parseErr != nil {
		writeErr(w, http.StatusBadRequest, "无效卡券ID")
		return
	}
	// draft、decodeErr 保存请求转换后的应用输入及 JSON 解码错误。
	draft, decodeErr := decodeCardDraft(r)
	if decodeErr != nil {
		writeErr(w, http.StatusBadRequest, decodeErr.Error())
		return
	}
	// session 是认证中间件注入的当前用户会话。
	session := auth.SessionFromContext(r.Context())
	// err 是应用服务返回的业务校验、所有权或持久化错误。
	err := s.cardsApplication().Update(r.Context(), session.UserID, cardID, draft)
	if err != nil {
		// validationError 用于识别可直接返回客户端的稳定业务校验提示。
		var validationError *cardsapp.ValidationError
		switch {
		case errors.As(err, &validationError):
			writeErr(w, http.StatusBadRequest, validationError.Error())
		case errors.Is(err, cardsapp.ErrNotFound) || errors.Is(err, cardsapp.ErrInvalidCardID):
			writeErr(w, http.StatusNotFound, "卡券不存在")
		case errors.Is(err, cardsapp.ErrForbidden):
			writeErr(w, http.StatusForbidden, "无权操作该卡密组")
		default:
			writeErr(w, http.StatusInternalServerError, "更新失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// deleteCard 解析路径标识，并由应用服务在归属校验后删除卡券组。
func (s *Server) deleteCard(w http.ResponseWriter, r *http.Request) {
	// cardID、parseErr 保存路径中的卡券标识及数字解析错误。
	cardID, parseErr := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if parseErr != nil {
		writeErr(w, http.StatusBadRequest, "无效卡券ID")
		return
	}
	// session 是认证中间件注入的当前用户会话。
	session := auth.SessionFromContext(r.Context())
	// err 是应用服务返回的资源、所有权或删除持久化错误。
	err := s.cardsApplication().Delete(r.Context(), session.UserID, cardID)
	if err != nil {
		switch {
		case errors.Is(err, cardsapp.ErrNotFound) || errors.Is(err, cardsapp.ErrInvalidCardID):
			writeErr(w, http.StatusNotFound, "卡券不存在")
		case errors.Is(err, cardsapp.ErrForbidden):
			writeErr(w, http.StatusForbidden, "无权操作该卡密组")
		default:
			writeErr(w, http.StatusInternalServerError, "删除失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// decodeCardDraft 把具名 HTTP 请求 DTO 转换为应用输入；业务校验由应用服务统一负责。
func decodeCardDraft(r *http.Request) (cardsapp.Draft, error) {
	// request 是当前待解码的卡券创建或更新请求。
	var request cardMutationRequest
	// err 表示请求 JSON 无法解码为卡券输入 DTO 的格式错误。
	if err := decodeJSON(r, &request); err != nil {
		return cardsapp.Draft{}, err
	}
	// apiConfig、apiConfigErr 保存兼容解码后的 API 配置文本及格式错误。
	apiConfig, apiConfigErr := decodeCardAPIConfig(request.APIConfig)
	if apiConfigErr != nil {
		return cardsapp.Draft{}, apiConfigErr
	}
	return cardsapp.Draft{
		Name: request.Name, Type: request.Type, APIConfig: apiConfig,
		TextContent: request.TextContent, DataContent: request.DataContent, ImageURL: request.ImageURL,
		Description: request.Description, Enabled: request.Enabled, DelaySeconds: request.DelaySeconds,
		IsMultiSpec: request.IsMultiSpec, SpecName: request.SpecName, SpecValue: request.SpecValue,
	}, nil
}

// decodeCardAPIConfig 兼容历史字符串配置，并把新版对象保持为 JSON 文本交给应用服务校验。
func decodeCardAPIConfig(raw json.RawMessage) (string, error) {
	// trimmed 保存去除空白后的请求字段。
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", nil
	}
	if trimmed[0] == '"' {
		// legacy 保存旧客户端提交的完整 JSON 字符串。
		var legacy string
		// err 表示历史字符串字段解码错误。
		if err := json.Unmarshal(trimmed, &legacy); err != nil {
			return "", err
		}
		return legacy, nil
	}
	// object 保存新版具名 API 配置对象的字段。
	var object map[string]json.RawMessage
	// err 表示新版 API 配置对象解码错误。
	if err := json.Unmarshal(trimmed, &object); err != nil {
		return "", err
	}
	// encoded、err 保存新版 API 配置对象的规范 JSON 和编码错误。
	encoded, err := json.Marshal(object)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// writeCardReadError 将详情查询的应用错误映射为既有 HTTP 状态和统一错误响应。
func writeCardReadError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, cardsapp.ErrNotFound) || errors.Is(err, cardsapp.ErrInvalidCardID):
		writeErr(w, http.StatusNotFound, "卡券不存在")
	case errors.Is(err, cardsapp.ErrForbidden):
		writeErr(w, http.StatusForbidden, "无权操作该卡密组")
	default:
		writeErr(w, http.StatusInternalServerError, "查询失败")
	}
}
