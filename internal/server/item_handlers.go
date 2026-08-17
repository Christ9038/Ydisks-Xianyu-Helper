package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	r.Post("/items/publish-categories/recommend", s.recommendItemPublishCategory)
	r.Post("/items/publish-batches/preview", s.previewItemPublishBatch)
	r.Post("/items/publish-batches", s.startItemPublishBatch)
	r.Get("/items/publish-batches", s.listItemPublishBatches)
	r.Get("/items/publish-batches/{batch_id}", s.getItemPublishBatch)
	r.Delete("/items/publish-batches/{batch_id}", s.deleteItemPublishBatch)
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
	r.Body = http.MaxBytesReader(w, r.Body, maxItemPublishBytes)
	// #nosec G120 -- 请求体已由 MaxBytesReader 限制。
	if err := r.ParseMultipartForm(maxOrderImportBytes); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误，请使用 multipart/form-data")
		return
	}
	cookieID := strings.TrimSpace(r.FormValue("cookie_id"))
	if cookieID == "" {
		writeErr(w, http.StatusBadRequest, "请选择发布账号")
		return
	}
	_, userID, ok := s.cookieForCurrentUser(w, r, cookieID)
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
	origCents, err := parseMoneyCents(r.FormValue("original_price"))
	if err != nil || origCents < 0 {
		writeErr(w, http.StatusBadRequest, "商品原价格式错误")
		return
	}
	quantity, err := strconv.Atoi(strings.TrimSpace(r.FormValue("quantity")))
	if err != nil || quantity <= 0 {
		writeErr(w, http.StatusBadRequest, "库存数量必须大于 0")
		return
	}
	postageMode := strings.TrimSpace(r.FormValue("postage_mode"))
	if postageMode == "" {
		postageMode = "free"
	}
	postageCents, err := parseMoneyCents(r.FormValue("postage"))
	if err != nil || postageCents < 0 || (postageMode == "fixed" && postageCents <= 0) {
		writeErr(w, http.StatusBadRequest, "固定邮费必须大于 0")
		return
	}
	images, err := readPublishImages(r, 9)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var location mtop.PublishLocation
	var selectedLocation *mtop.PublishLocation
	if rawLocation := strings.TrimSpace(r.FormValue("location")); rawLocation != "" {
		if err := json.Unmarshal([]byte(rawLocation), &location); err != nil {
			writeErr(w, http.StatusBadRequest, "发货地格式错误，请重新定位")
			return
		}
		selectedLocation = &location
	}
	credentialUnlock := s.Store.LockAccountCredentials(cookieID)
	latest, err := s.Store.Cookies.GetDetails(r.Context(), cookieID)
	if err != nil || latest == nil || latest.UserID != userID || !hasStoredCookieCredential(latest) {
		credentialUnlock()
		writeErr(w, http.StatusConflict, "账号凭证已变化，请重试")
		return
	}
	cookieValue := latest.Value
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	client := s.mtopClient()
	mtopCtx, cookieSession := withMTopCookieSnapshot(ctx, latest)
	publishReq := mtop.PublishItemRequest{
		Title:              title,
		Description:        description,
		PriceCents:         priceCents,
		OriginalPriceCents: origCents,
		Quantity:           quantity,
		PostageMode:        postageMode,
		PostageCents:       postageCents,
		Virtual:            true,
		Location:           selectedLocation,
		Images:             images,
	}
	res, callErr := client.PublishItem(mtopCtx, cookieValue, publishReq)
	runtimeCookie := ""
	runtimeCookieChanged := false
	var responseCookieErr error
	value, valueChanged, handled, persistErr := s.persistMTopCookieSessionLocked(r.Context(), latest, cookieSession)
	if persistErr != nil {
		s.Logger.Error("保存发布响应 Cookie Jar 失败", "cookie_id", cookieID, "err", persistErr)
		responseCookieErr = persistErr
	} else if handled {
		if valueChanged {
			runtimeCookie = value
			runtimeCookieChanged = true
		}
	} else if callErr == nil && res != nil && res.UpdatedCookies != "" && res.UpdatedCookies != cookieValue {
		if saveErr := s.Store.Cookies.UpdateValueOwned(r.Context(), cookieID, res.UpdatedCookies, userID); saveErr != nil {
			s.Logger.Error("保存刷新后的 cookie 失败", "cookie_id", cookieID, "err", saveErr)
			responseCookieErr = saveErr
		} else {
			runtimeCookie = res.UpdatedCookies
			runtimeCookieChanged = true
		}
	}
	credentialUnlock()
	if runtimeCookieChanged {
		s.updateRunningCookie(r.Context(), cookieID, runtimeCookie)
	}
	if callErr != nil {
		if responseCookieErr != nil {
			callErr = errors.Join(callErr, fmt.Errorf("保存发布响应 Cookie: %w", responseCookieErr))
		}
		// Session 失效是账号级凭证问题。续期成功后，当前发布请求仍然
		// 可以安全重试一次：平台已经明确拒绝了这次业务调用，不会把
		// 同一商品重复发布。重试前重新读取 Cookie，避免继续使用旧会话。
		if mtop.IsSessionExpiredErr(callErr) && s.recoverExpiredMTOPSession(r.Context(), cookieID, callErr) {
			retryRes, retryErr, retryCookieErr, credentialChanged := s.retryPublishItemAfterRenewal(ctx, cookieID, userID, publishReq)
			if credentialChanged {
				writeErr(w, http.StatusConflict, "账号凭证已变化，请重试")
				return
			}
			if retryErr == nil && retryRes != nil {
				res = retryRes
				callErr = nil
				responseCookieErr = retryCookieErr
			} else if retryErr != nil {
				callErr = retryErr
				responseCookieErr = retryCookieErr
			}
		}
		if callErr == nil {
			// The renewed attempt succeeded; continue through the normal local
			// persistence and success response path below.
		} else {
			var perr *mtop.PublishError
			if errors.As(callErr, &perr) {
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
			writeErr(w, http.StatusBadGateway, callErr.Error())
			return
		}
	}
	if res == nil || strings.TrimSpace(res.ItemID) == "" {
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"success": false,
			"code":    "publish_result_missing_item_id",
			"message": "平台返回发布成功，但缺少商品 ID，无法确认发布结果",
		})
		return
	}
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
	}); err != nil {
		if s.Logger != nil {
			s.Logger.Error("平台已发布但保存本地商品失败", "cookie_id", cookieID, "item_id", res.ItemID, "err", err)
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"success":  false,
			"code":     "remote_published_local_save_failed",
			"message":  "商品已在平台发布，但本地保存失败，请勿重复发布并根据商品 ID 人工核对",
			"item_id":  res.ItemID,
			"item_url": res.ItemURL,
		})
		return
	}
	if responseCookieErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"success":  false,
			"code":     "remote_published_cookie_save_failed",
			"message":  "商品已在平台发布并保存到本地，但登录凭证更新保存失败，请勿重复发布并尽快重新登录",
			"item_id":  res.ItemID,
			"item_url": res.ItemURL,
		})
		return
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

// retryPublishItemAfterRenewal re-reads the account credential after a
// successful Session renewal and repeats one rejected publish attempt. The
// caller already owns the parsed images and request payload, so this method
// deliberately does not re-parse the multipart body.
func (s *Server) retryPublishItemAfterRenewal(
	ctx context.Context,
	cookieID string,
	userID int64,
	req mtop.PublishItemRequest,
) (result *mtop.PublishItemResult, callErr, cookieErr error, credentialChanged bool) {
	credentialUnlock := s.Store.LockAccountCredentials(cookieID)
	defer credentialUnlock()
	latest, err := s.Store.Cookies.GetDetails(ctx, cookieID)
	if err != nil || latest == nil || latest.UserID != userID || !hasStoredCookieCredential(latest) {
		return nil, nil, nil, true
	}
	cookieValue := latest.Value
	mtopCtx, cookieSession := withMTopCookieSnapshot(ctx, latest)
	result, callErr = s.mtopClient().PublishItem(mtopCtx, cookieValue, req)
	value, valueChanged, handled, persistErr := s.persistMTopCookieSessionLocked(ctx, latest, cookieSession)
	if persistErr != nil {
		cookieErr = persistErr
		if s.Logger != nil {
			s.Logger.Error("保存重试发布响应 Cookie Jar 失败", "cookie_id", cookieID, "err", persistErr)
		}
	} else if handled {
		if valueChanged {
			s.updateRunningCookie(ctx, cookieID, value)
		}
	} else if callErr == nil && result != nil && result.UpdatedCookies != "" && result.UpdatedCookies != cookieValue {
		if err := s.Store.Cookies.UpdateValueOwned(ctx, cookieID, result.UpdatedCookies, userID); err != nil {
			cookieErr = err
			if s.Logger != nil {
				s.Logger.Error("保存重试发布响应 Cookie 失败", "cookie_id", cookieID, "err", err)
			}
		} else {
			s.updateRunningCookie(ctx, cookieID, result.UpdatedCookies)
		}
	}
	if cookieErr != nil && callErr != nil {
		callErr = errors.Join(callErr, fmt.Errorf("保存发布响应 Cookie: %w", cookieErr))
	}
	return result, callErr, cookieErr, false
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
		data, tooLarge, err := readLimitedBytes(f, 10<<20)
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("读取图片失败: %w", err)
		}
		if tooLarge {
			return nil, errors.New("单张图片不能超过 10 MiB")
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
	sign := int64(1)
	if strings.HasPrefix(raw, "-") {
		sign = -1
		raw = strings.TrimPrefix(raw, "-")
	} else {
		raw = strings.TrimPrefix(raw, "+")
	}
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
			return 0, fmt.Errorf("金额最多支持两位小数")
		}
		for len(frac) < 2 {
			frac += "0"
		}
		cents, err = strconv.ParseInt(frac, 10, 64)
		if err != nil {
			return 0, err
		}
	}
	return sign * (yuan*100 + cents), nil
}

func (s *Server) listItems(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	all, err := s.Store.Cookies.AllForUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	cookieID := strings.TrimSpace(r.URL.Query().Get("cookie_id"))
	if cookieID != "" {
		if _, ok := all[cookieID]; !ok {
			writeErr(w, http.StatusForbidden, "无权限操作该账号")
			return
		}
		all = map[string]string{cookieID: all[cookieID]}
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
	_, userID, ok := s.cookieForCurrentUser(w, r, req.CookieID)
	if !ok {
		return
	}
	credentialUnlock := s.Store.LockAccountCredentials(req.CookieID)
	latest, err := s.Store.Cookies.GetDetails(r.Context(), req.CookieID)
	if err != nil || latest == nil || latest.UserID != userID || !hasStoredCookieCredential(latest) {
		credentialUnlock()
		writeErr(w, http.StatusConflict, "账号凭证已变化，请重试")
		return
	}
	cookieValue := latest.Value
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	client := s.mtopClient()
	mtopCtx, cookieSession := withMTopCookieSnapshot(ctx, latest)
	res, callErr := client.FetchAllItems(mtopCtx, cookieValue, req.PageSize, req.MaxPages)
	if callErr == nil && res == nil {
		callErr = errors.New("商品列表接口未返回结果")
	}
	if callErr != nil {
		value, valueChanged, _, persistErr := s.persistMTopCookieSessionLocked(r.Context(), latest, cookieSession)
		credentialUnlock()
		if persistErr != nil {
			s.Logger.Error("保存商品同步响应 Cookie Jar 失败", "cookie_id", req.CookieID, "err", persistErr)
			writeErr(w, http.StatusInternalServerError, "商品同步响应凭证保存失败")
			return
		} else if valueChanged {
			s.updateRunningCookie(r.Context(), req.CookieID, value)
		}
		s.recoverExpiredMTOPSession(r.Context(), req.CookieID, callErr)
		writeErr(w, http.StatusBadGateway, callErr.Error())
		return
	}
	detailCookies := cookieValue
	if res.UpdatedCookies != "" {
		detailCookies = res.UpdatedCookies
	}
	detailErr := s.enrichSyncedItemMultiSpec(mtopCtx, client, detailCookies, req.CookieID, res.Items)
	runtimeCookie := ""
	runtimeCookieChanged := false
	value, valueChanged, handled, persistErr := s.persistMTopCookieSessionLocked(r.Context(), latest, cookieSession)
	if persistErr != nil {
		s.Logger.Error("保存商品同步响应 Cookie Jar 失败", "cookie_id", req.CookieID, "err", persistErr)
		credentialUnlock()
		writeErr(w, http.StatusInternalServerError, "商品同步响应凭证保存失败")
		return
	} else if handled {
		if valueChanged {
			runtimeCookie = value
			runtimeCookieChanged = true
		}
	} else if res.UpdatedCookies != "" && res.UpdatedCookies != cookieValue {
		if saveErr := s.Store.Cookies.UpdateValueOwned(r.Context(), req.CookieID, res.UpdatedCookies, userID); saveErr != nil {
			s.Logger.Error("保存刷新后的 cookie 失败", "cookie_id", req.CookieID, "err", saveErr)
			credentialUnlock()
			writeErr(w, http.StatusInternalServerError, "商品同步响应凭证保存失败")
			return
		} else {
			runtimeCookie = res.UpdatedCookies
			runtimeCookieChanged = true
		}
	}
	credentialUnlock()
	if runtimeCookieChanged {
		s.updateRunningCookie(r.Context(), req.CookieID, runtimeCookie)
	}
	if detailErr != nil {
		s.recoverExpiredMTOPSession(r.Context(), req.CookieID, detailErr)
		writeErr(w, http.StatusBadGateway, detailErr.Error())
		return
	}
	syncResult, syncErr := s.syncSyncedItems(r.Context(), req.CookieID, res.Items)
	if syncErr != nil {
		if s.Logger != nil {
			s.Logger.Error("同步商品到本地失败", "cookie_id", req.CookieID, "err", syncErr)
		}
		writeErr(w, http.StatusInternalServerError, "保存商品同步结果失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"message":       "成功获取商品，共 " + strconv.Itoa(len(res.Items)) + " 件，保存 " + strconv.Itoa(syncResult.Saved) + " 件，删除 " + strconv.Itoa(syncResult.Deleted) + " 件",
		"total_count":   len(res.Items),
		"total_pages":   res.TotalPages,
		"saved_count":   syncResult.Saved,
		"deleted_count": syncResult.Deleted,
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
	_, userID, ok := s.cookieForCurrentUser(w, r, req.CookieID)
	if !ok {
		return
	}
	credentialUnlock := s.Store.LockAccountCredentials(req.CookieID)
	latest, err := s.Store.Cookies.GetDetails(r.Context(), req.CookieID)
	if err != nil || latest == nil || latest.UserID != userID || !hasStoredCookieCredential(latest) {
		credentialUnlock()
		writeErr(w, http.StatusConflict, "账号凭证已变化，请重试")
		return
	}
	cookieValue := latest.Value
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	client := s.mtopClient()
	mtopCtx, cookieSession := withMTopCookieSnapshot(ctx, latest)
	res, callErr := client.FetchItemsPage(mtopCtx, cookieValue, req.PageNumber, req.PageSize)
	if callErr == nil && res == nil {
		callErr = errors.New("商品列表接口未返回结果")
	}
	if callErr != nil {
		value, valueChanged, _, persistErr := s.persistMTopCookieSessionLocked(r.Context(), latest, cookieSession)
		credentialUnlock()
		if persistErr != nil {
			s.Logger.Error("保存商品分页响应 Cookie Jar 失败", "cookie_id", req.CookieID, "err", persistErr)
			writeErr(w, http.StatusInternalServerError, "商品分页响应凭证保存失败")
			return
		} else if valueChanged {
			s.updateRunningCookie(r.Context(), req.CookieID, value)
		}
		s.recoverExpiredMTOPSession(r.Context(), req.CookieID, callErr)
		writeErr(w, http.StatusBadGateway, callErr.Error())
		return
	}
	detailCookies := cookieValue
	if res.UpdatedCookies != "" {
		detailCookies = res.UpdatedCookies
	}
	detailErr := s.enrichSyncedItemMultiSpec(mtopCtx, client, detailCookies, req.CookieID, res.Items)
	runtimeCookie := ""
	runtimeCookieChanged := false
	value, valueChanged, handled, persistErr := s.persistMTopCookieSessionLocked(r.Context(), latest, cookieSession)
	if persistErr != nil {
		s.Logger.Error("保存商品分页响应 Cookie Jar 失败", "cookie_id", req.CookieID, "err", persistErr)
		credentialUnlock()
		writeErr(w, http.StatusInternalServerError, "商品分页响应凭证保存失败")
		return
	} else if handled {
		if valueChanged {
			runtimeCookie = value
			runtimeCookieChanged = true
		}
	} else if res.UpdatedCookies != "" && res.UpdatedCookies != cookieValue {
		if saveErr := s.Store.Cookies.UpdateValueOwned(r.Context(), req.CookieID, res.UpdatedCookies, userID); saveErr != nil {
			s.Logger.Error("保存刷新后的 cookie 失败", "cookie_id", req.CookieID, "err", saveErr)
			credentialUnlock()
			writeErr(w, http.StatusInternalServerError, "商品分页响应凭证保存失败")
			return
		} else {
			runtimeCookie = res.UpdatedCookies
			runtimeCookieChanged = true
		}
	}
	credentialUnlock()
	if runtimeCookieChanged {
		s.updateRunningCookie(r.Context(), req.CookieID, runtimeCookie)
	}
	if detailErr != nil {
		s.recoverExpiredMTOPSession(r.Context(), req.CookieID, detailErr)
		writeErr(w, http.StatusBadGateway, detailErr.Error())
		return
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
			if item.IsMultiSpec {
				if multiErr := s.Store.Items.SetMultiSpec(ctx, cookieID, item.ID, true); multiErr != nil && s.Logger != nil {
					s.Logger.Warn("保存商品多规格状态失败", "cookie_id", cookieID, "item_id", item.ID, "err", multiErr)
				}
			}
			saved++
		} else if s.Logger != nil {
			s.Logger.Warn("保存商品失败", "cookie_id", cookieID, "item_id", item.ID, "err", err)
		}
	}
	return saved
}

func (s *Server) syncSyncedItems(ctx context.Context, cookieID string, items []mtop.ItemListItem) (db.ItemSyncResult, error) {
	rows := make([]db.ItemInfoRow, 0, len(items))
	for _, item := range items {
		priceText := item.PriceText
		if priceText == "" {
			priceText = item.Price
		}
		rows = append(rows, db.ItemInfoRow{
			CookieID:     cookieID,
			ItemID:       item.ID,
			ItemTitle:    item.Title,
			ItemCategory: item.CategoryID,
			ItemPrice:    priceText,
			ItemDetail:   item.ItemDetail,
			IsMultiSpec:  item.IsMultiSpec,
		})
	}
	return s.Store.Items.SyncFromRemote(ctx, cookieID, rows)
}

func (s *Server) enrichSyncedItemMultiSpec(ctx context.Context, client mtop.Client, cookies, cookieID string, items []mtop.ItemListItem) error {
	fetcher, ok := client.(mtop.ItemDetailFetcher)
	if !ok {
		return nil
	}
	for index := range items {
		if items[index].IsMultiSpec || s.Store.Items.IsMultiSpec(ctx, cookieID, items[index].ID) {
			items[index].IsMultiSpec = true
			continue
		}
		isMultiSpec, err := fetcher.DetectItemMultiSpec(ctx, cookies, items[index].ID)
		if err != nil {
			if mtop.IsSessionExpiredErr(err) {
				return err
			}
			if s.Logger != nil {
				s.Logger.Warn("识别商品多规格状态失败", "cookie_id", cookieID, "item_id", items[index].ID, "err", err)
			}
			continue
		}
		items[index].IsMultiSpec = isMultiSpec
	}
	return nil
}

func (s *Server) listItemsByCookie(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cookie_id")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
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
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
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
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
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
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	itemID := chi.URLParam(r, "item_id")
	var req struct {
		ItemTitle             *string `json:"item_title"`
		ItemDescription       *string `json:"item_description"`
		ItemCategory          *string `json:"item_category"`
		ItemPrice             *string `json:"item_price"`
		ItemDetail            *string `json:"item_detail"`
		IsMultiSpec           *bool   `json:"is_multi_spec"`
		MultiQuantityDelivery *bool   `json:"multi_quantity_delivery"`
		IsMultiQtyShip        *bool   `json:"is_multi_qty_ship"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	existing, err := s.Store.Items.Get(r.Context(), cid, itemID)
	if errors.Is(err, db.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "商品不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	row := &db.ItemInfoRow{
		CookieID:              cid,
		ItemID:                itemID,
		ItemTitle:             existing.ItemTitle,
		ItemDescription:       existing.ItemDescription,
		ItemCategory:          existing.ItemCategory,
		ItemPrice:             existing.ItemPrice,
		ItemDetail:            existing.ItemDetail,
		IsMultiSpec:           existing.IsMultiSpec,
		MultiQuantityDelivery: existing.MultiQuantityDelivery,
	}
	if req.ItemTitle != nil {
		row.ItemTitle = *req.ItemTitle
	}
	if req.ItemDescription != nil {
		row.ItemDescription = *req.ItemDescription
	}
	if req.ItemCategory != nil {
		row.ItemCategory = *req.ItemCategory
	}
	if req.ItemPrice != nil {
		row.ItemPrice = *req.ItemPrice
	}
	if req.ItemDetail != nil {
		row.ItemDetail = *req.ItemDetail
	}
	if req.IsMultiSpec != nil {
		row.IsMultiSpec = *req.IsMultiSpec
	}
	if req.MultiQuantityDelivery != nil {
		row.MultiQuantityDelivery = *req.MultiQuantityDelivery
	}
	if req.IsMultiQtyShip != nil {
		row.MultiQuantityDelivery = *req.IsMultiQtyShip
	}
	if err := s.Store.Items.Upsert(r.Context(), row); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) deleteItem(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cookie_id")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	itemID := chi.URLParam(r, "item_id")
	if err := s.Store.Items.Delete(r.Context(), cid, itemID); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) setItemMultiSpec(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cookie_id")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
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
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
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
