package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
	"xianyu-go/internal/logging"
	"xianyu-go/internal/netguard"
)

// maxOpenAIModelsResponseBytes 保存maxOpenAI模型列表响应Bytes，供当前处理流程使用
const maxOpenAIModelsResponseBytes = 4 << 20

// authSess 从上下文取会话。
func authSess(r *http.Request) *db.Session {
	return auth.SessionFromContext(r.Context())
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

// setSettings 负责set设置相关处理。
func (s *Server) setSettings(w http.ResponseWriter, r *http.Request) {
	// raw 保存原始，供当前处理流程使用
	var raw map[string]any
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &raw); err != nil || len(raw) == 0 {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// values 保存values，供当前处理流程使用
	values := make(map[string]string, len(raw))
	// key、value 表示当前遍历过程中的key、value
	for key, value := range raw {
		key = strings.TrimSpace(key)
		if key == "" || len(key) > 100 || value == nil {
			writeErr(w, http.StatusBadRequest, "设置键或值无效")
			return
		}
		values[key] = stringFromAny(value)
	}
	if // level、ok 保存level、ok，供当前处理流程使用
	level, ok := values["log_level"]; ok {
		if // err 保存err，供当前处理流程使用
		_, err := logging.ParseLevel(level); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if // err 保存err，供当前处理流程使用
	err := s.Store.Settings.SetMany(r.Context(), values); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	if // level、ok 保存level、ok，供当前处理流程使用
	level, ok := values["log_level"]; ok {
		_ = logging.SetLevel(level)
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// publicSettings 负责public设置相关处理。
func (s *Server) publicSettings(w http.ResponseWriter, r *http.Request) {
	// m、err 保存m、err，供当前处理流程使用
	m, err := s.Store.Settings.Public(r.Context())
	if err != nil {
		writeErrRequest(w, r, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, settingsResponse(m))
}

// allSettings 负责all设置相关处理。
func (s *Server) allSettings(w http.ResponseWriter, r *http.Request) {
	// m、err 保存m、err，供当前处理流程使用
	m, err := s.Store.Settings.All(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, settingsResponse(m))
}

// setSetting 负责set设置相关处理。
func (s *Server) setSetting(w http.ResponseWriter, r *http.Request) {
	// key 保存key，供当前处理流程使用
	key := chi.URLParam(r, "key")
	// req 保存req，供当前处理流程使用
	var req struct {
		Value string `json:"value"`
	}
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if key == "log_level" {
		if // err 保存err，供当前处理流程使用
		err := logging.SetLevel(req.Value); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if // err 保存err，供当前处理流程使用
	err := s.Store.Settings.Set(r.Context(), key, req.Value); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// ---- AI 回复设置 ----

// mountAIReplyReal 负责mountAI回复Real相关处理。
func (s *Server) mountAIReplyReal(r chi.Router) {
	r.Get("/ai-reply-settings", s.listAIReply)
	r.Get("/ai-reply-settings/{cookie_id}", s.getAIReply)
	r.Put("/ai-reply-settings/{cookie_id}", s.setAIReply)
}

// listAIReply 负责listAI回复相关处理。
func (s *Server) listAIReply(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := authSess(r)
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := s.Store.AIReply.ListForUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	// result 保存结果，供当前处理流程使用
	result := make(map[string]aiReplySettingsResponse)
	// row 表示当前遍历过程中的row
	for _, row := range rows {
		result[row.CookieID] = aiReplySettingsResponse{
			CookieID: row.CookieID, AIEnabled: row.AIEnabled, MaxDiscountPercent: row.MaxDiscountPercent,
			MaxDiscountAmount: row.MaxDiscountAmount, MaxBargainRounds: row.MaxBargainRounds, CustomPrompts: row.CustomPrompts,
			// 账号标识和五项配置字段保持旧 JSON 名称。
			// 布尔值继续由数据库整数转换得到。
			// 自定义提示词不做额外格式化。
			// DTO 转换不改变查询失败处理。
		}
	}
	writeJSON(w, http.StatusOK, result)
}

// getAIReply 负责getAI回复相关处理。
func (s *Server) getAIReply(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cookie_id")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// cfg、err 保存cfg、err，供当前处理流程使用
	cfg, err := s.Store.AIReply.Get(r.Context(), cid)
	if err != nil {
		if !errors.Is(err, db.ErrNotFound) {
			writeErr(w, http.StatusInternalServerError, "查询失败")
			return
		}
		// 未保存配置使用与旧接口一致的默认值。
		// 默认响应不携带 cookie_id，避免误认为已持久化账号配置。
		// max_discount_percent 保持默认 10 的业务约束。
		// max_discount_amount 保持默认 100 的业务约束。
		// max_bargain_rounds 保持默认 3 的业务约束。
		// custom_prompts 为空表示未配置自定义提示词。
		writeJSON(w, http.StatusOK, aiReplySettingsResponse{AIEnabled: false, MaxDiscountPercent: 10, MaxDiscountAmount: 100, MaxBargainRounds: 3, CustomPrompts: ""})
		return
	}
	// 已保存配置返回账号标识，客户端可据此区分账号级设置。
	// AIEnabled 表示账号 AI 回复开关。
	// 折扣上限字段保持原有数值类型和命名。
	// 砍价轮次字段保持原有校验范围。
	// CustomPrompts 仍返回原始提示词文本。
	// 该响应仅静态化 JSON 结构，不改变存储或校验逻辑。
	// 旧客户端可以继续直接读取这些字段。
	writeJSON(w, http.StatusOK, aiReplySettingsResponse{CookieID: cfg.CookieID, AIEnabled: cfg.AIEnabled, MaxDiscountPercent: cfg.MaxDiscountPercent, MaxDiscountAmount: cfg.MaxDiscountAmount, MaxBargainRounds: cfg.MaxBargainRounds, CustomPrompts: cfg.CustomPrompts})
}

// setAIReply 负责setAI回复相关处理。
func (s *Server) setAIReply(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cookie_id")
	// req 保存req，供当前处理流程使用
	var req struct {
		AIEnabled          bool   `json:"ai_enabled"`
		MaxDiscountPercent int    `json:"max_discount_percent"`
		MaxDiscountAmount  int    `json:"max_discount_amount"`
		MaxBargainRounds   int    `json:"max_bargain_rounds"`
		CustomPrompts      string `json:"custom_prompts"`
	}
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if !s.requireCookieOwnership(w, r, cid) {
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
	// err 是 AI 回复配置写入错误。
	err := s.Store.AIReply.UpsertSettings(r.Context(), cid, db.AIReplySettings{
		AIEnabled: req.AIEnabled, MaxDiscountPercent: req.MaxDiscountPercent,
		MaxDiscountAmount: req.MaxDiscountAmount, MaxBargainRounds: req.MaxBargainRounds,
		CustomPrompts: req.CustomPrompts,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// listAIModels 负责listAI模型列表相关处理。
func (s *Server) listAIModels(w http.ResponseWriter, r *http.Request) {
	// req 保存req，供当前处理流程使用
	var req struct {
		BaseURL string `json:"base_url"`
		APIKey  string `json:"api_key"`
	}
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// baseURL 保存baseURL，供当前处理流程使用
	baseURL := strings.TrimSpace(req.BaseURL)
	if baseURL == "" {
		// v、err 保存v、err，供当前处理流程使用
		v, err := s.Store.Settings.Get(r.Context(), "ai_api_url")
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "读取AI地址失败")
			return
		}
		baseURL = v
	}
	if baseURL == "" {
		baseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	}
	// apiKey 保存apiKey，供当前处理流程使用
	apiKey := strings.TrimSpace(req.APIKey)
	if apiKey == "" {
		// v、err 保存v、err，供当前处理流程使用
		v, err := s.Store.Settings.Get(r.Context(), "ai_api_key")
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "读取AI Key失败")
			return
		}
		apiKey = v
	}

	// models、err 保存models、err，供当前处理流程使用
	models, err := fetchOpenAIModels(r.Context(), baseURL, apiKey)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, aiModelsResponse{Models: models})
}

// newSettingsOutboundHTTPClient 保存new设置OutboundHTTPClient，供当前处理流程使用
var newSettingsOutboundHTTPClient = func(baseURL string) (*http.Client, error) {
	return netguard.TrustedEndpointHTTPClient(baseURL, 20*time.Second)
}

// fetchOpenAIModels 负责fetchOpenAI模型列表相关处理。
func fetchOpenAIModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("AI API 地址为空")
	}
	// req、err 保存req、err，供当前处理流程使用
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	}
	// client、err 保存client、err，供当前处理流程使用
	client, err := newSettingsOutboundHTTPClient(baseURL)
	if err != nil {
		return nil, fmt.Errorf("AI API 地址无效: %w", err)
	}
	// resp、err 保存resp、err，供当前处理流程使用
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("读取模型失败: %w", err)
	}
	defer resp.Body.Close()
	// raw、err 保存raw、err，供当前处理流程使用
	raw, err := readOpenAIModelsBody(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("读取模型失败: HTTP %d %s", resp.StatusCode, truncate(string(raw), 180))
	}
	// models、err 保存models、err，供当前处理流程使用
	models, err := parseOpenAIModels(raw)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("模型列表为空")
	}
	return models, nil
}

// readOpenAIModelsBody 负责readOpenAI模型列表请求体相关处理。
func readOpenAIModelsBody(r io.Reader) ([]byte, error) {
	// raw、err 保存raw、err，供当前处理流程使用
	raw, err := io.ReadAll(io.LimitReader(r, maxOpenAIModelsResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxOpenAIModelsResponseBytes {
		return nil, fmt.Errorf("模型列表响应超过 %d MiB", maxOpenAIModelsResponseBytes>>20)
	}
	return raw, nil
}

// parseOpenAIModels 负责parseOpenAI模型列表相关处理。
func parseOpenAIModels(raw []byte) ([]string, error) {
	// payload 保存请求载荷，供当前处理流程使用
	var payload any
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("解析模型列表失败: %w", err)
	}
	// seen 保存seen，供当前处理流程使用
	seen := make(map[string]bool)
	// out 保存out，供当前处理流程使用
	var out []string
	// add 保存add，供当前处理流程使用
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		out = append(out, v)
	}
	// walk 保存walk，供当前处理流程使用
	var walk func(any)
	walk = func(v any) {
		switch // x 保存x，供当前处理流程使用
		x := v.(type) {
		case []any:
			// item 表示当前遍历过程中的商品
			for _, item := range x {
				walk(item)
			}
		case map[string]any:
			if // id 保存标识，供当前处理流程使用
			id, _ := x["id"].(string); id != "" {
				add(id)
			} else if // name 保存名称，供当前处理流程使用
			name, _ := x["name"].(string); name != "" {
				add(name)
			}
		case string:
			add(x)
		}
	}
	if // root、ok 保存root、ok，供当前处理流程使用
	root, ok := payload.(map[string]any); ok {
		if // data、ok 保存data、ok，供当前处理流程使用
		data, ok := root["data"]; ok {
			walk(data)
		} else if // models、ok 保存models、ok，供当前处理流程使用
		models, ok := root["models"]; ok {
			walk(models)
		}
	} else {
		walk(payload)
	}
	return out, nil
}

// ---- 用户设置 ----

// mountUserReal 负责mount用户Real相关处理。
func (s *Server) mountUserReal(r chi.Router) {
	r.Get("/user-settings", s.listUserSettings)
	r.Put("/user-settings/{key}", s.setUserSetting)
	r.Get("/user-settings/{key}", s.getUserSetting)
}

// listUserSettings 负责list用户设置相关处理。
func (s *Server) listUserSettings(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := authSess(r)
	// settings、err 保存settings、err，供当前处理流程使用
	settings, err := s.Store.UserSettings.AllForUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, settingsResponse(settings))
}

// getUserSetting 负责get用户设置相关处理。
func (s *Server) getUserSetting(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := authSess(r)
	// key 保存key，供当前处理流程使用
	key := chi.URLParam(r, "key")
	// v、err 保存v、err，供当前处理流程使用
	v, err := s.Store.UserSettings.GetForUser(r.Context(), sess.UserID, key)
	if err != nil {
		writeJSON(w, http.StatusOK, userSettingResponse{Value: ""})
		return
	}
	writeJSON(w, http.StatusOK, userSettingResponse{Value: v})
}

// setUserSetting 负责set用户设置相关处理。
func (s *Server) setUserSetting(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := authSess(r)
	// key 保存key，供当前处理流程使用
	key := chi.URLParam(r, "key")
	// req 保存req，供当前处理流程使用
	var req struct {
		Value string `json:"value"`
	}
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// err 是用户设置写入错误。
	err := s.Store.UserSettings.SetForUser(r.Context(), sess.UserID, key, req.Value)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}
