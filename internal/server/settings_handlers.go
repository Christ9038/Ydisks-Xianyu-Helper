package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

// authSess 从上下文取会话。
func authSess(r *http.Request) *db.Session {
	return auth.SessionFromContext(r.Context())
}

// mountSettingsReal 系统设置端点（已认证）。public 单独挂载在顶层。
func (s *Server) mountSettingsReal(r chi.Router) {
	r.Get("/system-settings", s.allSettings)
	r.Put("/system-settings/{key}", s.setSetting)
	r.Post("/ai-models", s.listAIModels)
}

func (s *Server) publicSettings(w http.ResponseWriter, r *http.Request) {
	m, err := s.Store.Settings.Public(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) allSettings(w http.ResponseWriter, r *http.Request) {
	m, err := s.Store.Settings.All(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) setSetting(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	var req struct {
		Value string `json:"value"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := s.Store.Settings.Set(r.Context(), key, req.Value); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// ---- AI 回复设置 ----

func (s *Server) mountAIReplyReal(r chi.Router) {
	r.Get("/ai-reply-settings", s.listAIReply)
	r.Get("/ai-reply-settings/{cookie_id}", s.getAIReply)
	r.Put("/ai-reply-settings/{cookie_id}", s.setAIReply)
}

func (s *Server) listAIReply(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.DB.QueryContext(r.Context(),
		`SELECT cookie_id, ai_enabled, max_discount_percent, max_discount_amount,
		        max_bargain_rounds, COALESCE(custom_prompts, '')
		   FROM ai_reply_settings`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()

	result := make(map[string]any)
	for rows.Next() {
		var cookieID, customPrompts string
		var enabled, maxDiscountPercent, maxDiscountAmount, maxBargainRounds int
		if err := rows.Scan(&cookieID, &enabled, &maxDiscountPercent, &maxDiscountAmount, &maxBargainRounds, &customPrompts); err != nil {
			writeErr(w, http.StatusInternalServerError, "查询失败")
			return
		}
		result[cookieID] = map[string]any{
			"cookie_id":            cookieID,
			"ai_enabled":           enabled != 0,
			"max_discount_percent": maxDiscountPercent,
			"max_discount_amount":  maxDiscountAmount,
			"max_bargain_rounds":   maxBargainRounds,
			"custom_prompts":       customPrompts,
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) getAIReply(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cookie_id")
	cfg, err := s.Store.AIReply.Get(r.Context(), cid)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ai_enabled":           false,
			"max_discount_percent": 10,
			"max_discount_amount":  100,
			"max_bargain_rounds":   3,
			"custom_prompts":       "",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cookie_id":            cfg.CookieID,
		"ai_enabled":           cfg.AIEnabled,
		"max_discount_percent": cfg.MaxDiscountPercent,
		"max_discount_amount":  cfg.MaxDiscountAmount,
		"max_bargain_rounds":   cfg.MaxBargainRounds,
		"custom_prompts":       cfg.CustomPrompts,
	})
}

func (s *Server) setAIReply(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cookie_id")
	var req struct {
		AIEnabled          bool   `json:"ai_enabled"`
		MaxDiscountPercent int    `json:"max_discount_percent"`
		MaxDiscountAmount  int    `json:"max_discount_amount"`
		MaxBargainRounds   int    `json:"max_bargain_rounds"`
		CustomPrompts      string `json:"custom_prompts"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	_, err := s.Store.DB.ExecContext(r.Context(),
		`INSERT INTO ai_reply_settings
		 (cookie_id, ai_enabled, max_discount_percent, max_discount_amount,
		  max_bargain_rounds, custom_prompts, updated_at)
		 VALUES (?,?,?,?,?,?,CURRENT_TIMESTAMP)
		 ON CONFLICT(cookie_id) DO UPDATE SET
		   ai_enabled=excluded.ai_enabled,
		   max_discount_percent=excluded.max_discount_percent,
		   max_discount_amount=excluded.max_discount_amount,
		   max_bargain_rounds=excluded.max_bargain_rounds,
		   custom_prompts=excluded.custom_prompts,
		   updated_at=CURRENT_TIMESTAMP`,
		cid, btoi(req.AIEnabled), req.MaxDiscountPercent, req.MaxDiscountAmount,
		req.MaxBargainRounds, nullIfEmpty(req.CustomPrompts))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) listAIModels(w http.ResponseWriter, r *http.Request) {
	var req struct {
		BaseURL string `json:"base_url"`
		APIKey  string `json:"api_key"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	baseURL := strings.TrimSpace(req.BaseURL)
	if baseURL == "" {
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
	apiKey := strings.TrimSpace(req.APIKey)
	if apiKey == "" {
		v, err := s.Store.Settings.Get(r.Context(), "ai_api_key")
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "读取AI Key失败")
			return
		}
		apiKey = v
	}

	models, err := fetchOpenAIModels(r.Context(), baseURL, apiKey)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": models})
}

func fetchOpenAIModels(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("AI API 地址为空")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(apiKey))
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("读取模型失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("读取模型失败: HTTP %d %s", resp.StatusCode, truncate(string(raw), 180))
	}
	models, err := parseOpenAIModels(raw)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("模型列表为空")
	}
	return models, nil
}

func parseOpenAIModels(raw []byte) ([]string, error) {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("解析模型列表失败: %w", err)
	}
	seen := make(map[string]bool)
	var out []string
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			return
		}
		seen[v] = true
		out = append(out, v)
	}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case []any:
			for _, item := range x {
				walk(item)
			}
		case map[string]any:
			if id, _ := x["id"].(string); id != "" {
				add(id)
			} else if name, _ := x["name"].(string); name != "" {
				add(name)
			}
		case string:
			add(x)
		}
	}
	if root, ok := payload.(map[string]any); ok {
		if data, ok := root["data"]; ok {
			walk(data)
		} else if models, ok := root["models"]; ok {
			walk(models)
		}
	} else {
		walk(payload)
	}
	return out, nil
}

// ---- 用户设置 ----

func (s *Server) mountUserReal(r chi.Router) {
	r.Get("/user-settings", s.listUserSettings)
	r.Put("/user-settings/{key}", s.setUserSetting)
	r.Get("/user-settings/{key}", s.getUserSetting)
}

func (s *Server) listUserSettings(w http.ResponseWriter, r *http.Request) {
	sess := authSess(r)
	rows, err := s.Store.DB.QueryContext(r.Context(),
		`SELECT key, value FROM user_settings WHERE user_id=?`, sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()
	m := make(map[string]string)
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			m[k] = v
		}
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) getUserSetting(w http.ResponseWriter, r *http.Request) {
	sess := authSess(r)
	key := chi.URLParam(r, "key")
	var v string
	err := s.Store.DB.QueryRowContext(r.Context(),
		`SELECT value FROM user_settings WHERE user_id=? AND key=?`, sess.UserID, key).Scan(&v)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"value": ""})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"value": v})
}

func (s *Server) setUserSetting(w http.ResponseWriter, r *http.Request) {
	sess := authSess(r)
	key := chi.URLParam(r, "key")
	var req struct {
		Value string `json:"value"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	_, err := s.Store.DB.ExecContext(r.Context(),
		`INSERT OR REPLACE INTO user_settings (user_id, key, value, updated_at) VALUES (?,?,?,CURRENT_TIMESTAMP)`,
		sess.UserID, key, req.Value)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}
