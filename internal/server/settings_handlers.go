package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	settingsapp "xianyu-go/internal/application/settings"
	"xianyu-go/internal/auth"
	"xianyu-go/internal/logging"
)

// systemSettingSecretChangeRequest 是 HTTP 层接收的敏感设置变更命令。
type systemSettingSecretChangeRequest struct {
	// Action 是 retain、replace 或 clear。
	Action string `json:"action"`
	// Value 是 replace 操作要保存的新秘密。
	Value string `json:"value,omitempty"`
}

// systemSettingUpdateRequest 是保存普通或敏感系统设置的 HTTP 请求 DTO。
type systemSettingUpdateRequest struct {
	// Value 是普通设置值或 replace 操作的新秘密。
	Value string `json:"value"`
	// Action 是敏感设置的 retain、replace 或 clear 命令。
	Action string `json:"action"`
}

// aiReplySettingsUpdateRequest 是保存账号 AI 回复策略的 HTTP 请求 DTO。
type aiReplySettingsUpdateRequest struct {
	// AIEnabled 是账号 AI 自动回复开关。
	AIEnabled bool `json:"ai_enabled"`
	// AutoAdjustPriceEnabled 是把 AI 有效报价自动应用到待付款订单的显式开关。
	AutoAdjustPriceEnabled bool `json:"auto_adjust_price_enabled"`
	// MaxDiscountPercent 是允许自动接受的最大折扣百分比。
	MaxDiscountPercent int `json:"max_discount_percent"`
	// MaxDiscountAmount 是允许自动让价的最大金额。
	MaxDiscountAmount int `json:"max_discount_amount"`
	// MaxBargainRounds 是自动砍价允许的最大轮次。
	MaxBargainRounds int `json:"max_bargain_rounds"`
	// CustomPrompts 是账号专用的补充提示词。
	CustomPrompts string `json:"custom_prompts"`
}

// aiModelListRequest 是读取指定 AI 服务模型目录的 HTTP 请求 DTO。
type aiModelListRequest struct {
	// BaseURL 是目标 AI 服务的基础地址。
	BaseURL string `json:"base_url"`
	// APIKey 是仅在请求作用域内转交给模型目录适配器的访问密钥。
	APIKey string `json:"api_key"`
}

// userSettingUpdateRequest 是保存用户范围设置的 HTTP 请求 DTO。
type userSettingUpdateRequest struct {
	// Value 是需要持久化的用户设置值。
	Value string `json:"value"`
}

// mountSettingsReal 系统设置端点（管理员专用）。public 单独挂载在顶层。
func (s *Server) mountSettingsReal(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAdmin)
		r.Get("/system-settings", s.allSettings)
		r.Put("/system-settings", s.setSettings)
		r.Put("/system-settings/{key}", s.setSetting)
		r.Post("/ai-models", s.listAIModels)
	})
}

// setSettings 封装set设置业务协调。
func (s *Server) setSettings(w http.ResponseWriter, r *http.Request) {
	// raw 保存兼容旧客户端和显式 DTO 的原始请求字段。
	var raw map[string]json.RawMessage
	// err 是设置请求 JSON 解析错误。
	if err := decodeJSON(r, &raw); err != nil || len(raw) == 0 {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// values 保存普通设置值。
	values := make(map[string]string, len(raw))
	// secrets 保存敏感设置的显式变更命令。
	secrets := make(map[string]settingsapp.SecretChange)
	// valuesRaw 是普通设置对象的原始 JSON；ok 表示请求是否包含该对象。
	if valuesRaw, ok := raw["values"]; ok {
		// explicitValues 是显式请求中的普通设置字段。
		var explicitValues map[string]json.RawMessage
		// err 是普通设置对象解析错误。
		if err := json.Unmarshal(valuesRaw, &explicitValues); err != nil {
			writeErr(w, http.StatusBadRequest, "普通设置格式错误")
			return
		}
		// key 是显式普通设置中的字段名。
		for key := range explicitValues {
			if s.settingsApplication().IsSensitiveSettingKey(key) {
				writeErr(w, http.StatusBadRequest, "敏感设置必须放入 secrets 命令")
				return
			}
		}
		// err 是普通设置字段校验错误。
		if err := collectSystemSettingValues(explicitValues, values, s.settingsApplication().IsSensitiveSettingKey); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	// secretsRaw 是敏感设置命令对象的原始 JSON；ok 表示请求是否包含该对象。
	if secretsRaw, ok := raw["secrets"]; ok {
		// explicitSecrets 是显式请求中的敏感设置命令。
		var explicitSecrets map[string]systemSettingSecretChangeRequest
		// err 是敏感设置命令对象解析错误。
		if err := json.Unmarshal(secretsRaw, &explicitSecrets); err != nil {
			writeErr(w, http.StatusBadRequest, "敏感设置命令格式错误")
			return
		}
		// key 是敏感设置键；change 是对应的三态命令。
		for key, change := range explicitSecrets {
			key = strings.TrimSpace(key)
			if !s.settingsApplication().IsSensitiveSettingKey(key) || !validSecretSettingAction(change.Action) || change.Action == "replace" && strings.TrimSpace(change.Value) == "" {
				writeErr(w, http.StatusBadRequest, "敏感设置命令无效")
				return
			}
			secrets[key] = settingsapp.SecretChange{Action: change.Action, Value: change.Value}
		}
	}
	// legacy 保存旧版顶层普通设置字段，确保兼容接口可以渐进迁移。
	legacy := make(map[string]json.RawMessage, len(raw))
	// key 是兼容顶层字段名；value 是其原始 JSON 值。
	for key, value := range raw {
		if key != "values" && key != "secrets" {
			if s.settingsApplication().IsSensitiveSettingKey(key) {
				writeErr(w, http.StatusBadRequest, "敏感设置必须放入 secrets 命令")
				return
			}
			legacy[key] = value
		}
	}
	// err 是兼容顶层字段校验错误。
	if err := collectSystemSettingValues(legacy, values, s.settingsApplication().IsSensitiveSettingKey); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(values) == 0 && len(secrets) == 0 {
		writeErr(w, http.StatusBadRequest, "设置不能为空")
		return
	}
	// err 是普通设置业务校验错误。
	if err := validateSystemSettingValues(values); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// sess 保存当前管理员会话，用于应用服务执行用户范围校验和敏感写入审计。
	sess := authSess(r)
	if sess == nil {
		writeErr(w, http.StatusInternalServerError, "审计失败")
		return
	}
	// err 是普通设置与敏感命令原子保存错误。
	if err := s.settingsApplication().ApplySystemChanges(r.Context(), sess.UserID, values, secrets); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	// level 是待应用的日志级别；ok 表示请求是否更新日志级别。
	if level, ok := values["log_level"]; ok {
		_ = logging.SetLevel(level)
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// collectSystemSettingValues 解析普通设置字段并拒绝敏感明文。
func collectSystemSettingValues(raw map[string]json.RawMessage, values map[string]string, isSensitive func(string) bool) error {
	// key 是当前设置键；rawValue 是尚未转换的 JSON 值。
	for key, rawValue := range raw {
		key = strings.TrimSpace(key)
		if key == "" || len(key) > 100 {
			return errors.New("设置键无效")
		}
		if isSensitive(key) {
			return fmt.Errorf("敏感设置 %q 必须使用 secrets 命令", key)
		}
		// value 是普通设置转换后的任意 JSON 值。
		var value any
		// err 是普通设置值 JSON 解析错误。
		if err := json.Unmarshal(rawValue, &value); err != nil || value == nil {
			return errors.New("设置值无效")
		}
		values[key] = stringFromAny(value)
	}
	return nil
}

// validateSystemSettingValues 校验批量更新中的普通设置值。
func validateSystemSettingValues(values map[string]string) error {
	// level 是待校验的日志级别；ok 表示普通设置中包含该字段。
	if level, ok := values["log_level"]; ok {
		// err 是日志级别解析错误。
		_, err := logging.ParseLevel(level)
		if err != nil {
			return err
		}
	}
	// raw、ok 保存公网限制设置的原始值及是否包含该字段。
	if raw, ok := values["outbound_http_public_only"]; ok {
		if !strings.EqualFold(strings.TrimSpace(raw), "true") && !strings.EqualFold(strings.TrimSpace(raw), "false") {
			return errors.New("outbound_http_public_only 必须是布尔值")
		}
	}
	return nil
}

// validSecretSettingAction 判断敏感设置命令是否属于受支持的三态语义。
func validSecretSettingAction(action string) bool {
	switch action {
	case "retain", "replace", "clear":
		return true
	default:
		return false
	}
}

// publicSettings 封装public设置业务协调。
func (s *Server) publicSettings(w http.ResponseWriter, r *http.Request) {
	// m、err 用于本次流程后续判断的m、err
	m, err := s.settingsApplication().PublicSystem(r.Context())
	if err != nil {
		writeErrRequest(w, r, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, newSettingsResponse(m))
}

// allSettings 封装all设置业务协调。
func (s *Server) allSettings(w http.ResponseWriter, r *http.Request) {
	// sess 保存当前管理员会话，用于应用服务执行敏感配置读取审计。
	sess := authSess(r)
	if sess == nil {
		writeErr(w, http.StatusInternalServerError, "审计失败")
		return
	}
	// m、err 保存脱敏设置及应用服务查询错误。
	m, err := s.settingsApplication().GetSystem(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, newSettingsResponse(m))
}

// setSetting 封装set设置业务协调。
func (s *Server) setSetting(w http.ResponseWriter, r *http.Request) {
	// key 用于本次流程后续判断的key
	key := chi.URLParam(r, "key")
	// req 用于本次流程后续判断的req
	var req systemSettingUpdateRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if s.settingsApplication().IsSensitiveSettingKey(key) {
		// action 是最终采用的敏感设置命令。
		action := req.Action
		if !validSecretSettingAction(action) {
			writeErr(w, http.StatusBadRequest, "敏感设置命令无效")
			return
		}
		// sess 保存当前管理员会话，用于应用服务执行单项敏感设置写入审计。
		sess := authSess(r)
		if sess == nil {
			writeErr(w, http.StatusInternalServerError, "审计失败")
			return
		}
		// err 是单项敏感设置原子保存或审计错误。
		if err := s.settingsApplication().SetSystem(r.Context(), sess.UserID, key, req.Value, action); err != nil {
			writeErr(w, http.StatusInternalServerError, "保存失败")
			return
		}
		writeJSON(w, http.StatusOK, operationResponse{Success: true})
		return
	}
	if key == "log_level" {
		// err 表示单项日志级别校验错误。
		if _, err := logging.ParseLevel(req.Value); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if key == "outbound_http_public_only" && !strings.EqualFold(strings.TrimSpace(req.Value), "true") && !strings.EqualFold(strings.TrimSpace(req.Value), "false") {
		writeErr(w, http.StatusBadRequest, "outbound_http_public_only 必须是布尔值")
		return
	}
	// sess 保存当前管理员会话，用于应用服务执行普通设置写入。
	sess := authSess(r)
	if sess == nil {
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	// err 表示普通设置保存或应用服务校验失败。
	if err := s.settingsApplication().SetSystem(r.Context(), sess.UserID, key, req.Value, ""); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	if key == "log_level" {
		_ = logging.SetLevel(req.Value)
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// ---- AI 回复设置 ----

// mountAIReplyReal 封装mountAI回复Real业务协调。
func (s *Server) mountAIReplyReal(r chi.Router) {
	r.Get("/ai-reply-settings", s.listAIReply)
	r.Get("/ai-reply-settings/{cookie_id}", s.getAIReply)
	r.Put("/ai-reply-settings/{cookie_id}", s.setAIReply)
}

// listAIReply 封装listAI回复业务协调。
func (s *Server) listAIReply(w http.ResponseWriter, r *http.Request) {
	// sess 保存当前认证会话，用于应用服务执行用户范围查询。
	sess := authSess(r)
	// rows、err 保存应用层 AI 设置摘要及查询错误。
	rows, err := s.settingsApplication().ListAIReply(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	// result 用于本次流程后续判断的结果
	result := make(map[string]aiReplySettingsResponse)
	// row 表示当前遍历过程中的row
	for _, row := range rows {
		result[row.CookieID] = aiReplySettingsResponse{
			CookieID: row.CookieID, AIEnabled: row.AIEnabled, AutoAdjustPriceEnabled: row.AutoAdjustPriceEnabled, MaxDiscountPercent: row.MaxDiscountPercent,
			MaxDiscountAmount: row.MaxDiscountAmount, MaxBargainRounds: row.MaxBargainRounds, CustomPrompts: row.CustomPrompts,
			// 账号标识和五项配置字段保持旧 JSON 名称。
			// 布尔值继续由数据库整数转换得到。
			// 自定义提示词不做额外格式化。
			// DTO 转换不改变查询失败处理。
		}
	}
	writeJSON(w, http.StatusOK, result)
}

// getAIReply 封装getAI回复业务协调。
func (s *Server) getAIReply(w http.ResponseWriter, r *http.Request) {
	// cid 用于本次流程后续判断的cid
	cid := chi.URLParam(r, "cookie_id")
	// sess 保存当前认证会话，用于应用服务执行账号归属校验。
	sess := authSess(r)
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	// cfg、err 用于本次流程后续判断的cfg、err
	cfg, err := s.settingsApplication().GetAIReply(r.Context(), sess.UserID, cid)
	if err != nil {
		if errors.Is(err, settingsapp.ErrConfigNotFound) {
			// 未保存配置使用与旧接口一致的默认值。
			writeJSON(w, http.StatusOK, aiReplySettingsResponse{AIEnabled: false, AutoAdjustPriceEnabled: false, MaxDiscountPercent: 10, MaxDiscountAmount: 100, MaxBargainRounds: 3, CustomPrompts: ""})
			return
		}
		if writeSettingsAccountError(w, err) {
			return
		}
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	// 已保存配置返回账号标识，客户端可据此区分账号级设置。
	// AIEnabled 表示账号 AI 回复开关。
	// 折扣上限字段保持原有数值类型和命名。
	// 砍价轮次字段保持原有校验范围。
	// CustomPrompts 仍返回原始提示词文本。
	// 该响应仅静态化 JSON 结构，不改变存储或校验逻辑。
	// 旧客户端可以继续直接读取这些字段。
	writeJSON(w, http.StatusOK, aiReplySettingsResponse{CookieID: cfg.CookieID, AIEnabled: cfg.AIEnabled, AutoAdjustPriceEnabled: cfg.AutoAdjustPriceEnabled, MaxDiscountPercent: cfg.MaxDiscountPercent, MaxDiscountAmount: cfg.MaxDiscountAmount, MaxBargainRounds: cfg.MaxBargainRounds, CustomPrompts: cfg.CustomPrompts})
}

// setAIReply 封装setAI回复业务协调。
func (s *Server) setAIReply(w http.ResponseWriter, r *http.Request) {
	// cid 用于本次流程后续判断的cid
	cid := chi.URLParam(r, "cookie_id")
	// req 用于本次流程后续判断的req
	var req aiReplySettingsUpdateRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// sess 保存当前认证会话，用于在字段校验前复用旧接口的账号归属错误优先级。
	sess := authSess(r)
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return
	}
	// ownershipErr 保存账号不存在、跨用户或数据库归属查询失败的结果；未配置 AI 设置不是账号错误。
	if _, ownershipErr := s.settingsApplication().GetAIReply(r.Context(), sess.UserID, cid); ownershipErr != nil && !errors.Is(ownershipErr, settingsapp.ErrConfigNotFound) {
		if writeSettingsAccountError(w, ownershipErr) {
			return
		}
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	if req.MaxDiscountPercent < 0 || req.MaxDiscountPercent > 100 {
		writeErr(w, http.StatusBadRequest, "最大折扣比例必须在 0 到 100 之间")
		return
	}
	if req.MaxDiscountAmount < 0 {
		writeErr(w, http.StatusBadRequest, "最大折扣金额不能小于 0")
		return
	}
	if req.MaxBargainRounds < 1 || req.MaxBargainRounds > 10 {
		writeErr(w, http.StatusBadRequest, "最大砍价轮次必须在 1 到 10 之间")
		return
	}
	if req.AutoAdjustPriceEnabled && !req.AIEnabled {
		writeErr(w, http.StatusBadRequest, "开启 AI 自动改价前必须先启用 AI 议价")
		return
	}
	// err 是 AI 回复配置写入错误。
	err := s.settingsApplication().UpsertAIReply(r.Context(), sess.UserID, cid, settingsapp.AIReplySettings{
		CookieID: cid, AIEnabled: req.AIEnabled, AutoAdjustPriceEnabled: req.AutoAdjustPriceEnabled, MaxDiscountPercent: req.MaxDiscountPercent,
		MaxDiscountAmount: req.MaxDiscountAmount, MaxBargainRounds: req.MaxBargainRounds,
		CustomPrompts: req.CustomPrompts,
	})
	if err != nil {
		if errors.Is(err, settingsapp.ErrPricingModeConflict) {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		if writeSettingsAccountError(w, err) {
			return
		}
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// writeSettingsAccountError 将应用层账号归属错误映射为旧接口兼容的 HTTP 状态。
func writeSettingsAccountError(w http.ResponseWriter, err error) bool {
	if errors.Is(err, settingsapp.ErrAccountNotFound) {
		writeErr(w, http.StatusNotFound, "账号不存在")
		return true
	}
	if errors.Is(err, settingsapp.ErrForbidden) {
		writeErr(w, http.StatusForbidden, "无权限操作该账号")
		return true
	}
	return false
}

// listAIModels 封装listAI模型列表业务协调。
func (s *Server) listAIModels(w http.ResponseWriter, r *http.Request) {
	// req 保存 HTTP 层接收的模型目录查询参数。
	var req aiModelListRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// sess 保存当前管理员会话，用于应用服务执行 AI 密钥审计和读取。
	sess := authSess(r)
	if sess == nil {
		writeErr(w, http.StatusInternalServerError, "审计失败")
		return
	}
	// models、err 保存应用服务读取的模型名称及错误。
	models, err := s.settingsApplication().ListAIModels(r.Context(), sess.UserID, req.BaseURL, req.APIKey)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, aiModelsResponse{Models: models})
}

// ---- 用户设置 ----

// mountUserReal 封装mount用户Real业务协调。
func (s *Server) mountUserReal(r chi.Router) {
	r.Get("/user-settings", s.listUserSettings)
	r.Put("/user-settings/{key}", s.setUserSetting)
	r.Get("/user-settings/{key}", s.getUserSetting)
}

// listUserSettings 封装list用户设置业务协调。
func (s *Server) listUserSettings(w http.ResponseWriter, r *http.Request) {
	// sess 保存当前认证会话，用于应用服务执行用户范围查询。
	sess := authSess(r)
	// settings、err 保存用户设置及查询错误。
	settings, err := s.settingsApplication().ListUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, newSettingsResponse(settings))
}

// getUserSetting 封装get用户设置业务协调。
func (s *Server) getUserSetting(w http.ResponseWriter, r *http.Request) {
	// sess 保存当前认证会话，用于应用服务执行用户范围查询。
	sess := authSess(r)
	// key 用于本次流程后续判断的key
	key := chi.URLParam(r, "key")
	// v、err 保存用户设置值及查询错误。
	v, err := s.settingsApplication().GetUser(r.Context(), sess.UserID, key)
	if err != nil {
		writeJSON(w, http.StatusOK, userSettingResponse{Value: ""})
		return
	}
	writeJSON(w, http.StatusOK, userSettingResponse{Value: v})
}

// setUserSetting 封装set用户设置业务协调。
func (s *Server) setUserSetting(w http.ResponseWriter, r *http.Request) {
	// sess 用于本次流程后续判断的sess
	sess := authSess(r)
	// key 用于本次流程后续判断的key
	key := chi.URLParam(r, "key")
	// req 用于本次流程后续判断的req
	var req userSettingUpdateRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// err 是用户设置写入错误。
	err := s.settingsApplication().SetUser(r.Context(), sess.UserID, key, req.Value)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}
