package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// mountItemsReal 物品端点（真实实现）。
func (s *Server) mountItemsReal(r chi.Router) {
	r.Get("/items", s.listItems)
	r.Post("/items/get-all-from-account", s.syncItemsFromAccount)
	r.Post("/items/get-by-page", s.syncItemsPageFromAccount)
	r.Post("/items/publish", s.publishItem)
	r.Post("/items/publish-batches/preview", s.previewItemPublishBatch)
	r.Post("/items/publish-batches", s.startItemPublishBatch)
	r.Get("/items/publish-batches/{batch_id}", s.getItemPublishBatch)
	r.Post("/items/publish-batches/{batch_id}/cancel", s.cancelItemPublishBatch)
	r.Post("/items/publish-batches/{batch_id}/retry-failed", s.retryFailedItemPublishBatch)
	r.Get("/items/publish-batches/{batch_id}/result.csv", s.downloadItemPublishBatchResult)
	r.Get("/items/cookie/{cookie_id}", s.listItemsByCookie)
	r.Post("/items/{cookie_id}", s.createItem)
	r.Get("/items/{cookie_id}/{item_id}", s.getItem)
	r.Put("/items/{cookie_id}/{item_id}", s.updateItem)
	r.Delete("/items/{cookie_id}/{item_id}", s.deleteItem)
	r.Put("/items/{cookie_id}/{item_id}/multi-spec", s.setItemMultiSpec)
	r.Put("/items/{cookie_id}/{item_id}/multi-quantity-delivery", s.setItemMultiQuantity)
}

func (s *Server) publishItem(w http.ResponseWriter, r *http.Request) {
	// 最多 9 张 10 MiB 图片，额外预留 multipart 元数据空间。
	r.Body = http.MaxBytesReader(w, r.Body, 96<<20)
	// #nosec G120 -- 请求体已由 MaxBytesReader 限制为 96 MiB。
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误，请使用 multipart/form-data")
		return
	}
	cookieID := strings.TrimSpace(r.FormValue("cookie_id"))
	if cookieID == "" {
		writeErr(w, http.StatusBadRequest, "请选择发布账号")
		return
	}
	cookieValue, userID, ok := s.cookieForCurrentUser(w, r, cookieID)
	if !ok {
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	description := strings.TrimSpace(r.FormValue("description"))
	priceCents, err := parseMoneyCents(r.FormValue("price"))
	if err != nil || priceCents <= 0 {
		writeErr(w, http.StatusBadRequest, "商品价格必须大于 0")
		return
	}
	origCents, _ := parseMoneyCents(r.FormValue("original_price"))
	quantity, err := strconv.Atoi(strings.TrimSpace(r.FormValue("quantity")))
	if err != nil || quantity <= 0 {
		writeErr(w, http.StatusBadRequest, "库存数量必须大于 0")
		return
	}
	postageMode := strings.TrimSpace(r.FormValue("postage_mode"))
	if postageMode == "" {
		postageMode = "free"
	}
	postageCents, _ := parseMoneyCents(r.FormValue("postage"))
	images, err := readPublishImages(r, 9)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	client := s.MTop
	if client == nil {
		client = &mtop.Client{}
	}
	res, err := client.PublishItem(ctx, cookieValue, mtop.PublishItemRequest{
		Title:              title,
		Description:        description,
		PriceCents:         priceCents,
		OriginalPriceCents: origCents,
		Quantity:           quantity,
		PostageMode:        postageMode,
		PostageCents:       postageCents,
		Images:             images,
	})
	if err != nil {
		var perr *mtop.PublishError
		if errors.As(err, &perr) {
			status := http.StatusBadGateway
			msg := perr.Error()
			if perr.Code == mtop.PublishErrorStockPermissionMissing {
				status = http.StatusForbidden
				msg = "该账号没有库存发布权限，无法按库存数量发布商品"
			}
			writeJSON(w, status, map[string]any{
				"success": false,
				"code":    perr.Code,
				"message": msg,
				"ret":     perr.Ret,
			})
			return
		}
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if res.UpdatedCookies != "" && res.UpdatedCookies != cookieValue {
		_ = s.Store.Cookies.Save(r.Context(), cookieID, res.UpdatedCookies, userID)
	}
	if res.ItemID != "" {
		detail := map[string]any{
			"item_image":    res.ImageURL,
			"web_url":       res.ItemURL,
			"category_name": res.CategoryName,
			"quantity":      res.Quantity,
			"publish_raw":   res.RawData,
		}
		detailJSON, _ := json.Marshal(detail)
		if err := s.Store.Items.Upsert(r.Context(), &db.ItemInfoRow{
			CookieID:              cookieID,
			ItemID:                res.ItemID,
			ItemTitle:             res.Title,
			ItemDescription:       description,
			ItemCategory:          res.CategoryID,
			ItemPrice:             res.PriceText,
			ItemDetail:            string(detailJSON),
			MultiQuantityDelivery: quantity > 1,
		}); err != nil && s.Logger != nil {
			s.Logger.Warn("保存发布商品失败", "cookie_id", cookieID, "item_id", res.ItemID, "err", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"message":       "商品发布成功",
		"item_id":       res.ItemID,
		"item_url":      res.ItemURL,
		"item_image":    res.ImageURL,
		"item_title":    res.Title,
		"item_price":    res.PriceText,
		"quantity":      res.Quantity,
		"category_id":   res.CategoryID,
		"category_name": res.CategoryName,
	})
}

func readPublishImages(r *http.Request, maxImages int) ([]mtop.PublishImage, error) {
	if r.MultipartForm == nil || r.MultipartForm.File == nil {
		return nil, errors.New("至少上传 1 张商品图片")
	}
	files := r.MultipartForm.File["images"]
	if len(files) == 0 {
		files = r.MultipartForm.File["image"]
	}
	if len(files) == 0 {
		return nil, errors.New("至少上传 1 张商品图片")
	}
	if len(files) > maxImages {
		return nil, fmt.Errorf("商品图片最多 %d 张", maxImages)
	}
	images := make([]mtop.PublishImage, 0, len(files))
	for _, fh := range files {
		f, err := fh.Open()
		if err != nil {
			return nil, fmt.Errorf("读取图片失败: %w", err)
		}
		data, err := io.ReadAll(io.LimitReader(f, 10<<20))
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("读取图片失败: %w", err)
		}
		if len(data) == 0 {
			return nil, errors.New("图片文件为空")
		}
		contentType := fh.Header.Get("Content-Type")
		if contentType == "" {
			contentType = http.DetectContentType(data)
		}
		if !strings.HasPrefix(contentType, "image/") {
			return nil, errors.New("只能上传图片文件")
		}
		images = append(images, mtop.PublishImage{Filename: fh.Filename, ContentType: contentType, Data: data})
	}
	return images, nil
}

func parseMoneyCents(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	raw = strings.TrimPrefix(raw, "¥")
	raw = strings.TrimPrefix(raw, "￥")
	parts := strings.Split(raw, ".")
	if len(parts) > 2 {
		return 0, fmt.Errorf("金额格式错误")
	}
	yuan, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil {
		return 0, err
	}
	cents := int64(0)
	if len(parts) == 2 {
		frac := strings.TrimSpace(parts[1])
		if len(frac) > 2 {
			frac = frac[:2]
		}
		for len(frac) < 2 {
			frac += "0"
		}
		cents, err = strconv.ParseInt(frac, 10, 64)
		if err != nil {
			return 0, err
		}
	}
	return yuan*100 + cents, nil
}

func (s *Server) listItems(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	all, err := s.Store.Cookies.AllForUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	result := []map[string]any{}
	for cid := range all {
		items, _ := s.Store.Items.AllForCookie(r.Context(), cid)
		for _, it := range items {
			result = append(result, itemToMap(it))
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) syncItemsFromAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CookieID string `json:"cookie_id"`
		PageSize int    `json:"page_size"`
		MaxPages int    `json:"max_pages"`
	}
	if err := decodeJSON(r, &req); err != nil || req.CookieID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 cookie_id 参数")
		return
	}
	cookieValue, userID, ok := s.cookieForCurrentUser(w, r, req.CookieID)
	if !ok {
		return
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	client := &mtop.Client{}
	res, err := client.FetchAllItems(ctx, cookieValue, req.PageSize, req.MaxPages)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if res.UpdatedCookies != "" && res.UpdatedCookies != cookieValue {
		_ = s.Store.Cookies.Save(r.Context(), req.CookieID, res.UpdatedCookies, userID)
	}
	saved := s.saveSyncedItems(r.Context(), req.CookieID, res.Items)
	writeJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"message":     "成功获取商品，共 " + strconv.Itoa(len(res.Items)) + " 件，保存 " + strconv.Itoa(saved) + " 件",
		"total_count": len(res.Items),
		"total_pages": res.TotalPages,
		"saved_count": saved,
	})
}

func (s *Server) syncItemsPageFromAccount(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CookieID   string `json:"cookie_id"`
		PageNumber int    `json:"page_number"`
		PageSize   int    `json:"page_size"`
	}
	if err := decodeJSON(r, &req); err != nil || req.CookieID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 cookie_id 参数")
		return
	}
	cookieValue, userID, ok := s.cookieForCurrentUser(w, r, req.CookieID)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	client := &mtop.Client{}
	res, err := client.FetchItemsPage(ctx, cookieValue, req.PageNumber, req.PageSize)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if res.UpdatedCookies != "" && res.UpdatedCookies != cookieValue {
		_ = s.Store.Cookies.Save(r.Context(), req.CookieID, res.UpdatedCookies, userID)
	}
	saved := s.saveSyncedItems(r.Context(), req.CookieID, res.Items)
	writeJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"message":       "成功获取第" + strconv.Itoa(res.PageNumber) + "页 " + strconv.Itoa(len(res.Items)) + " 个商品",
		"page_number":   res.PageNumber,
		"page_size":     res.PageSize,
		"current_count": len(res.Items),
		"saved_count":   saved,
	})
}

func (s *Server) cookieForCurrentUser(w http.ResponseWriter, r *http.Request, cookieID string) (string, int64, bool) {
	sess := auth.SessionFromContext(r.Context())
	all, err := s.Store.Cookies.AllForUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询账号失败")
		return "", 0, false
	}
	value, ok := all[cookieID]
	if !ok {
		writeErr(w, http.StatusForbidden, "无权限操作该账号")
		return "", 0, false
	}
	if value == "" {
		writeErr(w, http.StatusBadRequest, "账号 cookie 为空")
		return "", 0, false
	}
	return value, sess.UserID, true
}

func (s *Server) saveSyncedItems(ctx context.Context, cookieID string, items []mtop.ItemListItem) int {
	saved := 0
	for _, item := range items {
		priceText := item.PriceText
		if priceText == "" {
			priceText = item.Price
		}
		err := s.Store.Items.UpsertBasic(ctx, &db.ItemInfoRow{
			CookieID:        cookieID,
			ItemID:          item.ID,
			ItemTitle:       item.Title,
			ItemDescription: "",
			ItemCategory:    item.CategoryID,
			ItemPrice:       priceText,
			ItemDetail:      item.ItemDetail,
		})
		if err == nil {
			saved++
		} else if s.Logger != nil {
			s.Logger.Warn("保存商品失败", "cookie_id", cookieID, "item_id", item.ID, "err", err)
		}
	}
	return saved
}

func (s *Server) listItemsByCookie(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cookie_id")
	items, err := s.Store.Items.AllForCookie(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		out = append(out, itemToMap(it))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getItem(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cookie_id")
	itemID := chi.URLParam(r, "item_id")
	it, err := s.Store.Items.Get(r.Context(), cid, itemID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "商品不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cookie_id": it.CookieID, "item_id": it.ItemID, "item_title": it.ItemTitle,
		"item_description": it.ItemDescription, "item_category": it.ItemCategory,
		"item_price": it.ItemPrice, "item_detail": it.ItemDetail,
		"is_multi_spec": it.IsMultiSpec, "multi_quantity_delivery": it.MultiQuantityDelivery,
	})
}

func (s *Server) createItem(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cookie_id")
	var req struct {
		ItemID                string `json:"item_id"`
		ItemTitle             string `json:"item_title"`
		ItemDescription       string `json:"item_description"`
		ItemCategory          string `json:"item_category"`
		ItemPrice             string `json:"item_price"`
		ItemDetail            string `json:"item_detail"`
		IsMultiSpec           bool   `json:"is_multi_spec"`
		MultiQuantityDelivery bool   `json:"multi_quantity_delivery"`
		IsMultiQtyShip        bool   `json:"is_multi_qty_ship"`
	}
	if err := decodeJSON(r, &req); err != nil || req.ItemID == "" {
		writeErr(w, http.StatusBadRequest, "缺少商品 ID")
		return
	}
	if req.MultiQuantityDelivery || req.IsMultiQtyShip {
		req.MultiQuantityDelivery = true
	}
	if err := s.Store.Items.Upsert(r.Context(), &db.ItemInfoRow{
		CookieID: cid, ItemID: req.ItemID, ItemTitle: req.ItemTitle, ItemDescription: req.ItemDescription,
		ItemCategory: req.ItemCategory, ItemPrice: req.ItemPrice, ItemDetail: req.ItemDetail,
		IsMultiSpec: req.IsMultiSpec, MultiQuantityDelivery: req.MultiQuantityDelivery,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "新增失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) updateItem(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cookie_id")
	itemID := chi.URLParam(r, "item_id")
	var req struct {
		ItemTitle             string `json:"item_title"`
		ItemDescription       string `json:"item_description"`
		ItemCategory          string `json:"item_category"`
		ItemPrice             string `json:"item_price"`
		ItemDetail            string `json:"item_detail"`
		IsMultiSpec           bool   `json:"is_multi_spec"`
		MultiQuantityDelivery bool   `json:"multi_quantity_delivery"`
		IsMultiQtyShip        bool   `json:"is_multi_qty_ship"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := s.Store.Items.Upsert(r.Context(), &db.ItemInfoRow{
		CookieID: cid, ItemID: itemID, ItemTitle: req.ItemTitle, ItemDescription: req.ItemDescription,
		ItemCategory: req.ItemCategory, ItemPrice: req.ItemPrice, ItemDetail: req.ItemDetail,
		IsMultiSpec: req.IsMultiSpec, MultiQuantityDelivery: req.MultiQuantityDelivery || req.IsMultiQtyShip,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) deleteItem(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cookie_id")
	itemID := chi.URLParam(r, "item_id")
	if err := s.Store.Items.Delete(r.Context(), cid, itemID); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) setItemMultiSpec(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cookie_id")
	itemID := chi.URLParam(r, "item_id")
	var req struct {
		IsMultiSpec bool `json:"is_multi_spec"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := s.Store.Items.SetMultiSpec(r.Context(), cid, itemID, req.IsMultiSpec); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) setItemMultiQuantity(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cookie_id")
	itemID := chi.URLParam(r, "item_id")
	var req struct {
		MultiQuantityDelivery bool `json:"multi_quantity_delivery"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := s.Store.Items.SetMultiQuantity(r.Context(), cid, itemID, req.MultiQuantityDelivery); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func itemToMap(it db.ItemInfoRow) map[string]any {
	imageURL := itemImageFromDetail(it.ItemDetail)
	return map[string]any{
		"id":        it.ID,
		"cookie_id": it.CookieID, "item_id": it.ItemID, "item_title": it.ItemTitle,
		"item_description": it.ItemDescription, "item_category": it.ItemCategory,
		"item_price": it.ItemPrice, "item_detail": it.ItemDetail,
		"item_image":    imageURL,
		"is_multi_spec": it.IsMultiSpec, "multi_quantity_delivery": it.MultiQuantityDelivery,
		"is_multi_qty_ship": it.MultiQuantityDelivery,
	}
}

func itemImageFromDetail(detail string) string {
	if detail == "" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(detail), &m); err != nil {
		return ""
	}
	if pic, ok := m["pic_info"].(map[string]any); ok {
		if url, ok := pic["picUrl"].(string); ok {
			return url
		}
	}
	if url, ok := m["item_image"].(string); ok {
		return url
	}
	return ""
}
