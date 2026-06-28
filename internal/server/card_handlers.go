package server

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

// mountCardsReal 卡券与发货规则 CRUD（真实实现）。
func (s *Server) mountCardsReal(r chi.Router) {
	r.Get("/cards", s.listCards)
	r.Post("/cards", s.createCard)
	r.Get("/cards/{card_id}/details", s.getCard)
	r.Get("/cards/{card_id}", s.getCard)
	r.Put("/cards/{card_id}", s.updateCard)
	r.Delete("/cards/{card_id}", s.deleteCard)
	r.Get("/delivery-rules", s.listDeliveryRules)
	r.Post("/delivery-rules", s.createDeliveryRule)
	r.Put("/delivery-rules/{rule_id}", s.updateDeliveryRule)
	r.Delete("/delivery-rules/{rule_id}", s.deleteDeliveryRule)
}

func (s *Server) listCards(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	cards, err := s.Store.Cards.AllForUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, cards)
}

func (s *Server) getCard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效卡券ID")
		return
	}
	cf, err := s.Store.Cards.Get(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "卡券不存在")
		return
	}
	writeJSON(w, http.StatusOK, cf)
}

func (s *Server) createCard(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	cf, err := decodeCard(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	cf.UserID = sess.UserID
	id, err := s.Store.Cards.Create(r.Context(), cf)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "创建失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "id": id})
}

func (s *Server) updateCard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效卡券ID")
		return
	}
	cf, err := decodeCard(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	cf.ID = id
	if err := s.Store.Cards.Update(r.Context(), cf); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) deleteCard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效卡券ID")
		return
	}
	if err := s.Store.Cards.Delete(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func decodeCard(r *http.Request) (*db.CardFull, error) {
	var req struct {
		Name         string `json:"name"`
		Type         string `json:"type"`
		APIConfig    string `json:"api_config"`
		TextContent  string `json:"text_content"`
		DataContent  string `json:"data_content"`
		ImageURL     string `json:"image_url"`
		Description  string `json:"description"`
		Enabled      bool   `json:"enabled"`
		DelaySeconds int    `json:"delay_seconds"`
		IsMultiSpec  bool   `json:"is_multi_spec"`
		SpecName     string `json:"spec_name"`
		SpecValue    string `json:"spec_value"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return nil, err
	}
	if req.Name == "" || req.Type == "" {
		return nil, errStr("名称和类型不能为空")
	}
	return &db.CardFull{
		Name: req.Name, Type: req.Type, APIConfig: req.APIConfig, TextContent: req.TextContent,
		DataContent: req.DataContent, ImageURL: req.ImageURL, Description: req.Description,
		Enabled: req.Enabled, DelaySeconds: req.DelaySeconds, IsMultiSpec: req.IsMultiSpec,
		SpecName: req.SpecName, SpecValue: req.SpecValue,
	}, nil
}

// ---- 发货规则 CRUD ----

type deliveryVariantRequest struct {
	ID            int64  `json:"id,omitempty"`
	SpecName      string `json:"spec_name"`
	SpecValue     string `json:"spec_value"`
	CardID        int64  `json:"card_id"`
	DeliveryCount int    `json:"delivery_count"`
	Enabled       *bool  `json:"enabled,omitempty"`
}

type deliveryRuleRequest struct {
	Keyword       string                   `json:"keyword"`
	CookieID      string                   `json:"cookie_id"`
	ItemID        string                   `json:"item_id"`
	Enabled       bool                     `json:"enabled"`
	Description   string                   `json:"description"`
	Variants      []deliveryVariantRequest `json:"variants"`
	CardID        int64                    `json:"card_id"`
	DeliveryCount int                      `json:"delivery_count"`
}

func (s *Server) listDeliveryRules(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	rows, err := s.Store.DB.QueryContext(r.Context(),
		`SELECT dr.id,dr.keyword,COALESCE(dr.cookie_id,''),COALESCE(dr.item_id,''),
		        dr.enabled,COALESCE(dr.description,''),dr.delivery_times,
		        COALESCE((SELECT item_title FROM item_info i
		                  WHERE i.cookie_id=dr.cookie_id AND i.item_id=dr.item_id LIMIT 1),'')
		 FROM delivery_rules dr WHERE dr.user_id=? ORDER BY dr.id DESC`, sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id int64
		var deliveryTimes, enabled int
		var keyword, cookieID, itemID, desc, itemTitle string
		if err := rows.Scan(&id, &keyword, &cookieID, &itemID, &enabled, &desc, &deliveryTimes, &itemTitle); err != nil {
			writeErr(w, http.StatusInternalServerError, "读取发货规则失败")
			return
		}
		variants, err := s.deliveryRuleVariants(r.Context(), id)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "读取规格映射失败")
			return
		}
		var legacyCardID int64
		legacyCardName := ""
		legacyCount := 1
		if len(variants) > 0 {
			legacyCardID, _ = variants[0]["card_id"].(int64)
			legacyCardName, _ = variants[0]["card_name"].(string)
			legacyCount, _ = variants[0]["delivery_count"].(int)
		}
		out = append(out, map[string]any{
			"id": id, "keyword": keyword, "cookie_id": cookieID, "item_id": itemID, "item_title": itemTitle,
			"card_id": legacyCardID, "card_name": legacyCardName, "delivery_count": legacyCount,
			"enabled": enabled != 0, "description": desc, "delivery_times": deliveryTimes, "variants": variants,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createDeliveryRule(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	var req deliveryRuleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := normalizeDeliveryRuleRequest(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.deliveryRuleItemOwned(r, sess.UserID, req.CookieID, req.ItemID) {
		writeErr(w, http.StatusForbidden, "商品不属于当前用户")
		return
	}
	tx, err := s.Store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "创建失败")
		return
	}
	defer tx.Rollback()
	first := req.Variants[0]
	res, err := tx.ExecContext(r.Context(),
		`INSERT INTO delivery_rules
		 (keyword,card_id,delivery_count,enabled,description,user_id,cookie_id,item_id)
		 VALUES (?,?,?,?,?,?,?,?)`,
		req.Keyword, first.CardID, first.DeliveryCount, req.Enabled, req.Description,
		sess.UserID, req.CookieID, req.ItemID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "创建失败")
		return
	}
	id, _ := res.LastInsertId()
	if err := insertDeliveryVariants(r.Context(), tx, sess.UserID, id, req.Variants); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, http.StatusInternalServerError, "创建失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "id": id})
}

func (s *Server) updateDeliveryRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "rule_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效规则ID")
		return
	}
	var req deliveryRuleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := normalizeDeliveryRuleRequest(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	sess := auth.SessionFromContext(r.Context())
	if !s.deliveryRuleItemOwned(r, sess.UserID, req.CookieID, req.ItemID) {
		writeErr(w, http.StatusForbidden, "商品不属于当前用户")
		return
	}
	tx, err := s.Store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	defer tx.Rollback()
	first := req.Variants[0]
	res, err := tx.ExecContext(r.Context(),
		`UPDATE delivery_rules SET keyword=?,card_id=?,delivery_count=?,enabled=?,description=?,
		 cookie_id=?,item_id=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND user_id=?`,
		req.Keyword, first.CardID, first.DeliveryCount, req.Enabled, req.Description,
		req.CookieID, req.ItemID, id, sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, http.StatusNotFound, "发货规则不存在")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM delivery_rule_variants WHERE rule_id=?`, id); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新规格映射失败")
		return
	}
	if err := insertDeliveryVariants(r.Context(), tx, sess.UserID, id, req.Variants); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) deleteDeliveryRule(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "rule_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效规则ID")
		return
	}
	sess := auth.SessionFromContext(r.Context())
	res, err := s.Store.DB.ExecContext(r.Context(), `DELETE FROM delivery_rules WHERE id=? AND user_id=?`, id, sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeErr(w, http.StatusNotFound, "发货规则不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func normalizeDeliveryRuleRequest(req *deliveryRuleRequest) error {
	req.Keyword = strings.TrimSpace(req.Keyword)
	req.CookieID = strings.TrimSpace(req.CookieID)
	req.ItemID = strings.TrimSpace(req.ItemID)
	req.Description = strings.TrimSpace(req.Description)
	if len(req.Variants) == 0 && req.CardID > 0 {
		req.Variants = []deliveryVariantRequest{{CardID: req.CardID, DeliveryCount: req.DeliveryCount}}
	}
	if req.ItemID == "" && req.Keyword == "" {
		return errStr("请选择商品或填写旧规则关键词")
	}
	if len(req.Variants) == 0 {
		return errStr("至少需要一条卡密映射")
	}
	seen := map[string]bool{}
	for i := range req.Variants {
		v := &req.Variants[i]
		v.SpecName = strings.TrimSpace(v.SpecName)
		v.SpecValue = strings.TrimSpace(v.SpecValue)
		if v.CardID <= 0 {
			return errStr("每条规格映射都必须选择卡密")
		}
		if (v.SpecName == "") != (v.SpecValue == "") {
			return errStr("规格名称和规格值必须同时填写")
		}
		if v.DeliveryCount <= 0 {
			v.DeliveryCount = 1
		}
		key := v.SpecName + "\x00" + v.SpecValue
		if seen[key] {
			return errStr("同一规则中不能存在重复规格")
		}
		seen[key] = true
	}
	return nil
}

func (s *Server) deliveryRuleItemOwned(r *http.Request, userID int64, cookieID, itemID string) bool {
	if itemID == "" {
		return true
	}
	all, err := s.Store.Cookies.AllForUser(r.Context(), userID)
	if err != nil || cookieID == "" {
		return false
	}
	if _, ok := all[cookieID]; !ok {
		return false
	}
	_, err = s.Store.Items.Get(r.Context(), cookieID, itemID)
	return err == nil
}

func insertDeliveryVariants(ctx context.Context, tx *sql.Tx, userID, ruleID int64, variants []deliveryVariantRequest) error {
	for _, v := range variants {
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM cards WHERE id=? AND user_id=?)`, v.CardID, userID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return errStr("选择的卡密不存在或不属于当前用户")
		}
		enabled := true
		if v.Enabled != nil {
			enabled = *v.Enabled
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO delivery_rule_variants
			 (rule_id,spec_name,spec_value,card_id,delivery_count,enabled) VALUES (?,?,?,?,?,?)`,
			ruleID, v.SpecName, v.SpecValue, v.CardID, v.DeliveryCount, enabled); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) deliveryRuleVariants(ctx context.Context, ruleID int64) ([]map[string]any, error) {
	rows, err := s.Store.DB.QueryContext(ctx,
		`SELECT v.id,v.spec_name,v.spec_value,v.card_id,v.delivery_count,v.enabled,c.name,c.type
		 FROM delivery_rule_variants v JOIN cards c ON c.id=v.card_id
		 WHERE v.rule_id=? ORDER BY v.id`, ruleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, cardID int64
		var specName, specValue, cardName, cardType string
		var deliveryCount, enabled int
		if err := rows.Scan(&id, &specName, &specValue, &cardID, &deliveryCount, &enabled, &cardName, &cardType); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id, "spec_name": specName, "spec_value": specValue, "card_id": cardID,
			"card_name": cardName, "card_type": cardType, "delivery_count": deliveryCount, "enabled": enabled != 0,
		})
	}
	return out, rows.Err()
}

func errStr(s string) error { return &simpleError{s} }

type simpleError struct{ msg string }

func (e *simpleError) Error() string { return e.msg }
