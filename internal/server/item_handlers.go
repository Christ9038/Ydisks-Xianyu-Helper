package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
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

// publishItem 解析 HTTP 发布请求并调用商品发布应用服务完成单商品发布。
func (s *Server) publishItem(w http.ResponseWriter, r *http.Request) {
	// 最多 9 张 10 MiB 图片，额外预留 multipart 元数据空间。
	r.Body = http.MaxBytesReader(w, r.Body, maxItemPublishBytes)
	// #nosec G120 -- 请求体已由 MaxBytesReader 限制。
	if err := r.ParseMultipartForm(maxOrderImportBytes); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误，请使用 multipart/form-data")
		return
	}
	// cookieID 保存登录凭证ID，供当前处理流程使用
	cookieID := strings.TrimSpace(r.FormValue("cookie_id"))
	if cookieID == "" {
		writeErr(w, http.StatusBadRequest, "请选择发布账号")
		return
	}
	// userID、ok 保存用户ID、ok，供当前处理流程使用
	_, userID, ok := s.cookieForCurrentUser(w, r, cookieID)
	if !ok {
		return
	}
	// title 保存标题，供当前处理流程使用
	title := strings.TrimSpace(r.FormValue("title"))
	// description 保存description，供当前处理流程使用
	description := strings.TrimSpace(r.FormValue("description"))
	// priceCents、err 保存priceCents、err，供当前处理流程使用
	priceCents, err := parseMoneyCents(r.FormValue("price"))
	if err != nil || priceCents <= 0 {
		writeErr(w, http.StatusBadRequest, "商品价格必须大于 0")
		return
	}
	// origCents、err 保存origCents、err，供当前处理流程使用
	origCents, err := parseMoneyCents(r.FormValue("original_price"))
	if err != nil || origCents < 0 {
		writeErr(w, http.StatusBadRequest, "商品原价格式错误")
		return
	}
	// quantity、err 保存quantity、err，供当前处理流程使用
	quantity, err := strconv.Atoi(strings.TrimSpace(r.FormValue("quantity")))
	if err != nil || quantity <= 0 {
		writeErr(w, http.StatusBadRequest, "库存数量必须大于 0")
		return
	}
	// postageMode 保存postage模式，供当前处理流程使用
	postageMode := strings.TrimSpace(r.FormValue("postage_mode"))
	if postageMode == "" {
		postageMode = "free"
	}
	// postageCents、err 保存postageCents、err，供当前处理流程使用
	postageCents, err := parseMoneyCents(r.FormValue("postage"))
	if err != nil || postageCents < 0 || (postageMode == "fixed" && postageCents <= 0) {
		writeErr(w, http.StatusBadRequest, "固定邮费必须大于 0")
		return
	}
	// images、err 保存images、err，供当前处理流程使用
	images, err := readPublishImages(r, 9)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// location 保存地址，供当前处理流程使用
	var location mtop.PublishLocation
	// selectedLocation 保存selected地址，供当前处理流程使用
	var selectedLocation *mtop.PublishLocation
	if // rawLocation 保存原始地址，供当前处理流程使用
	rawLocation := strings.TrimSpace(r.FormValue("location")); rawLocation != "" {
		if // err 保存err，供当前处理流程使用
		err := json.Unmarshal([]byte(rawLocation), &location); err != nil {
			writeErr(w, http.StatusBadRequest, "发货地格式错误，请重新定位")
			return
		}
		selectedLocation = &location
	}
	// outcome、callErr 保存outcome、callErr，供当前处理流程使用
	outcome, callErr := s.itemPublishApplication().PublishSingle(r.Context(), itemPublishInput{
		UserID: userID, CookieID: cookieID, Title: title, Description: description,
		PriceCents: priceCents, OriginalPriceCents: origCents, Quantity: quantity,
		PostageMode: postageMode, PostageCents: postageCents, Location: selectedLocation, Images: images,
	})
	// res 保存响应，供当前处理流程使用
	res := outcome.Result
	if callErr != nil {
		// perr 保存perr，供当前处理流程使用
		var perr *mtop.PublishError
		if errors.As(callErr, &perr) {
			// status 保存状态，供当前处理流程使用
			status := http.StatusBadGateway
			// msg 保存msg，供当前处理流程使用
			msg := perr.Error()
			if perr.Code == mtop.PublishErrorStockPermissionMissing {
				status = http.StatusForbidden
				msg = "该账号没有库存发布权限，无法按库存数量发布商品"
			}
			writeErrCode(w, status, string(perr.Code), msg, "")
			return
		}
		if strings.Contains(callErr.Error(), "账号凭证已变化") {
			writeErr(w, http.StatusConflict, callErr.Error())
			return
		}
		writeErr(w, http.StatusBadGateway, callErr.Error())
		return
	}
	if res == nil || strings.TrimSpace(res.ItemID) == "" {
		writeErrCode(w, http.StatusBadGateway, "publish_result_missing_item_id", "平台返回发布成功，但缺少商品 ID，无法确认发布结果", "")
		return
	}
	if outcome.LocalSaveErr != nil {
		if s.Logger != nil {
			s.Logger.Error("平台已发布但保存本地商品失败", "cookie_id", cookieID, "item_id", res.ItemID, "err", outcome.LocalSaveErr)
		}
		writeErrDetails(w, http.StatusInternalServerError, "remote_published_local_save_failed", "商品已在平台发布，但本地保存失败，请勿重复发布并根据商品 ID 人工核对", "", map[string]any{"item_id": res.ItemID, "item_url": res.ItemURL})
		return
	}
	if outcome.ResponseCookieErr != nil {
		writeErrDetails(w, http.StatusInternalServerError, "remote_published_cookie_save_failed", "商品已在平台发布并保存到本地，但登录凭证更新保存失败，请勿重复发布并尽快重新登录", "", map[string]any{"item_id": res.ItemID, "item_url": res.ItemURL})
		return
	}
	writeJSON(w, http.StatusOK, itemPublishResponse{
		Success: true, Message: "商品发布成功", ItemID: res.ItemID, ItemURL: res.ItemURL,
		ItemImage: res.ImageURL, ItemTitle: res.Title, ItemPrice: res.PriceText, Quantity: res.Quantity,
		CategoryID: res.CategoryID, CategoryName: res.CategoryName,
	})

	/*
		旧实现保留在版本控制历史中，当前单商品发布由 itemPublishService 统一处理。
		服务边界保证 MTOP 调用、Cookie 更新和本地商品落库的顺序不变。
	*/
	/*res, callErr := client.PublishItem(mtopCtx, cookieValue, mtop.PublishItemRequest{
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
	})
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
		s.recoverExpiredMTOPSession(r.Context(), cookieID, callErr)
		var perr *mtop.PublishError
		if errors.As(callErr, &perr) {
			status := http.StatusBadGateway
			msg := perr.Error()
			if perr.Code == mtop.PublishErrorStockPermissionMissing {
				status = http.StatusForbidden
				msg = "该账号没有库存发布权限，无法按库存数量发布商品"
			}
			writeErrCode(
				w,
				status,
				string(perr.Code),
				msg,
				"")
			return
		}
		writeErr(w, http.StatusBadGateway, callErr.Error())
		return
	}
	if res == nil || strings.TrimSpace(res.ItemID) == "" {
		writeErrCode(w, http.StatusBadGateway,
			"publish_result_missing_item_id",
			"平台返回发布成功，但缺少商品 ID，无法确认发布结果",
			"",
		)
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
		writeErrDetails(
			w,
			http.StatusInternalServerError,
			"remote_published_local_save_failed",
			"商品已在平台发布，但本地保存失败，请勿重复发布并根据商品 ID 人工核对",
			"",
			map[string]any{"item_id": res.ItemID, "item_url": res.ItemURL})
		return
	}
	if responseCookieErr != nil {
		writeErrDetails(
			w,
			http.StatusInternalServerError,
			"remote_published_cookie_save_failed",
			"商品已在平台发布并保存到本地，但登录凭证更新保存失败，请勿重复发布并尽快重新登录",
			"",
			map[string]any{"item_id": res.ItemID, "item_url": res.ItemURL})
		return
	}
	writeJSON(w, http.StatusOK, itemPublishResponse{
		Success: true, Message: "商品发布成功", ItemID: res.ItemID, ItemURL: res.ItemURL,
		ItemImage: res.ImageURL, ItemTitle: res.Title, ItemPrice: res.PriceText, Quantity: res.Quantity,
		CategoryID: res.CategoryID, CategoryName: res.CategoryName,
	})*/
	// 商品发布 DTO 保留历史客户端使用的字段名称和成功语义。
	// 平台发布结果与本地保存结果的失败分支仍通过统一错误 DTO 返回。
	// item_id 和 item_url 可用于发布后人工核对平台商品。
	// item_image 和 item_title 供前端列表即时展示。
	// quantity、category_id 和 category_name 保留平台回传信息。
	// 本次迁移不改变发布接口的 HTTP 状态码。
	// 后续版本化路径迁移继续复用该具名 DTO。
}

// readPublishImages 负责read发布Images相关处理。
func readPublishImages(r *http.Request, maxImages int) ([]mtop.PublishImage, error) {
	if r.MultipartForm == nil || r.MultipartForm.File == nil {
		return nil, errors.New("至少上传 1 张商品图片")
	}
	// files 保存文件列表，供当前处理流程使用
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
	// images 保存images，供当前处理流程使用
	images := make([]mtop.PublishImage, 0, len(files))
	// fh 表示当前遍历过程中的fh
	for _, fh := range files {
		// f、err 保存f、err，供当前处理流程使用
		f, err := fh.Open()
		if err != nil {
			return nil, fmt.Errorf("读取图片失败: %w", err)
		}
		// data、tooLarge、err 保存data、tooLarge、err，供当前处理流程使用
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
		// contentType 保存内容类型，供当前处理流程使用
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

// parseMoneyCents 负责parseMoneyCents相关处理。
func parseMoneyCents(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	raw = strings.TrimPrefix(raw, "¥")
	raw = strings.TrimPrefix(raw, "￥")
	// sign 保存sign，供当前处理流程使用
	sign := int64(1)
	if strings.HasPrefix(raw, "-") {
		sign = -1
		raw = strings.TrimPrefix(raw, "-")
	} else {
		raw = strings.TrimPrefix(raw, "+")
	}
	// parts 保存parts，供当前处理流程使用
	parts := strings.Split(raw, ".")
	if len(parts) > 2 {
		return 0, fmt.Errorf("金额格式错误")
	}
	// yuan、err 保存yuan、err，供当前处理流程使用
	yuan, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil {
		return 0, err
	}
	// cents 保存cents，供当前处理流程使用
	cents := int64(0)
	if len(parts) == 2 {
		// frac 保存frac，供当前处理流程使用
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

// listItems 负责list商品列表相关处理。
func (s *Server) listItems(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// cookieID 是可选的账号筛选条件。
	cookieID := strings.TrimSpace(r.URL.Query().Get("cookie_id"))
	// cookieID 保存登录凭证ID，供当前处理流程使用
	if cookieID != "" {
		if !s.cookieOwnedByUser(r.Context(), sess.UserID, cookieID) {
			writeErr(w, http.StatusForbidden, "无权限操作该账号")
			return
		}
	}
	// items、err 保存用户范围商品查询结果及错误。
	items, err := s.Store.Items.ListForUser(r.Context(), sess.UserID, cookieID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	// result 保存结果，供当前处理流程使用。
	result := make([]itemListResponse, 0, len(items))
	// it 表示当前遍历过程中的商品行。
	for _, it := range items {
		result = append(result, itemToMap(it))
	}
	writeJSON(w, http.StatusOK, result)
}

// syncItemsFromAccount 负责sync商品列表From账号相关处理。
func (s *Server) syncItemsFromAccount(w http.ResponseWriter, r *http.Request) {
	// req 保存req，供当前处理流程使用
	var req struct {
		CookieID string `json:"cookie_id"`
		PageSize int    `json:"page_size"`
		MaxPages int    `json:"max_pages"`
	}
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil || req.CookieID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 cookie_id 参数")
		return
	}
	// userID、ok 保存用户ID、ok，供当前处理流程使用
	_, userID, ok := s.cookieForCurrentUser(w, r, req.CookieID)
	if !ok {
		return
	}
	// credentialUnlock 保存credentialUnlock，供当前处理流程使用
	credentialUnlock := s.Store.LockAccountCredentials(req.CookieID)
	// latest、err 保存latest、err，供当前处理流程使用
	latest, err := s.loadCookiePlatformDetail(r.Context(), req.CookieID)
	if err != nil || latest == nil || latest.UserID != userID || !hasStoredCookieCredential(latest) {
		credentialUnlock()
		writeErr(w, http.StatusConflict, "账号凭证已变化，请重试")
		return
	}
	// cookieValue 保存登录凭证值，供当前处理流程使用
	cookieValue := latest.Value
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	// client 保存client，供当前处理流程使用
	client := s.mtopClient()
	// mtopCtx、cookieSession 保存mtopCtx、cookie会话，供当前处理流程使用
	mtopCtx, cookieSession := withMTopCookieSnapshot(ctx, latest)
	// 账号凭证快照已读取完成；慢速商品列表请求不得继续持有共享凭证锁。
	credentialUnlock()
	// res、callErr 保存res、callErr，供当前处理流程使用
	res, callErr := client.FetchAllItems(mtopCtx, cookieValue, req.PageSize, req.MaxPages)
	// credentialUnlock 保存外部请求完成后重新进入凭证提交临界区的释放函数。
	credentialUnlock = s.Store.LockAccountCredentials(req.CookieID)
	// latestAfterFetch、reloadErr 保存外部请求完成后的最新凭证快照及重读错误。
	latestAfterFetch, reloadErr := s.loadCookiePlatformDetail(r.Context(), req.CookieID)
	if reloadErr != nil || latestAfterFetch == nil || latestAfterFetch.UserID != userID {
		credentialUnlock()
		writeErr(w, http.StatusConflict, "账号凭证已变化，请重试")
		return
	}
	// credentialSnapshotChanged 标记外部商品请求期间是否已有其他流程更新凭证。
	credentialSnapshotChanged := latestAfterFetch.Value != latest.Value || latestAfterFetch.MetadataJSON != latest.MetadataJSON
	latest = latestAfterFetch
	if callErr == nil && res == nil {
		callErr = errors.New("商品列表接口未返回结果")
	}
	if callErr != nil {
		// value、valueChanged、persistErr 保存value、valueChanged、persistErr，供当前处理流程使用
		value, valueChanged, _, persistErr := "", false, false, error(nil)
		if !credentialSnapshotChanged {
			value, valueChanged, _, persistErr = s.persistMTopCookieSessionLocked(r.Context(), latest, cookieSession)
		}
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
	// detailCookies 保存detailCookies，供当前处理流程使用
	detailCookies := cookieValue
	if res.UpdatedCookies != "" {
		detailCookies = res.UpdatedCookies
	}
	credentialUnlock()
	// detailErr 保存规格探测结果及错误。
	detailErr := s.enrichSyncedItemMultiSpec(mtopCtx, client, detailCookies, req.CookieID, res.Items)
	credentialUnlock = s.Store.LockAccountCredentials(req.CookieID)
	// latestAfterEnrich、enrichReloadErr 保存规格探测完成后的最新凭证快照及重读错误。
	latestAfterEnrich, enrichReloadErr := s.loadCookiePlatformDetail(r.Context(), req.CookieID)
	if enrichReloadErr != nil || latestAfterEnrich == nil || latestAfterEnrich.UserID != userID {
		credentialUnlock()
		writeErr(w, http.StatusConflict, "账号凭证已变化，请重试")
		return
	}
	credentialSnapshotChanged = credentialSnapshotChanged || latestAfterEnrich.Value != latest.Value || latestAfterEnrich.MetadataJSON != latest.MetadataJSON
	latest = latestAfterEnrich
	// runtimeCookie 保存runtime登录凭证，供当前处理流程使用
	runtimeCookie := ""
	// runtimeCookieChanged 保存runtime登录凭证Changed，供当前处理流程使用
	runtimeCookieChanged := false
	// value、valueChanged、handled、persistErr 保存value、valueChanged、handled、persistErr，供当前处理流程使用
	value, valueChanged, handled, persistErr := "", false, false, error(nil)
	if !credentialSnapshotChanged {
		value, valueChanged, handled, persistErr = s.persistMTopCookieSessionLocked(r.Context(), latest, cookieSession)
	}
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
	} else if !credentialSnapshotChanged && res.UpdatedCookies != "" && res.UpdatedCookies != cookieValue {
		if // saveErr 保存saveErr，供当前处理流程使用
		saveErr := s.Store.Cookies.UpdateValueOwned(r.Context(), req.CookieID, res.UpdatedCookies, userID); saveErr != nil {
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
	// syncResult、syncErr 保存syncResult、syncErr，供当前处理流程使用
	syncResult, syncErr := s.syncSyncedItems(r.Context(), req.CookieID, res.Items)
	if syncErr != nil {
		if s.Logger != nil {
			s.Logger.Error("同步商品到本地失败", "cookie_id", req.CookieID, "err", syncErr)
		}
		writeErr(w, http.StatusInternalServerError, "保存商品同步结果失败")
		return
	}
	writeJSON(w, http.StatusOK, itemSyncResponse{
		Success:    true,
		Message:    "成功获取商品，共 " + strconv.Itoa(len(res.Items)) + " 件，保存 " + strconv.Itoa(syncResult.Saved) + " 件，删除 " + strconv.Itoa(syncResult.Deleted) + " 件",
		TotalCount: len(res.Items), TotalPages: res.TotalPages, SavedCount: syncResult.Saved, DeletedCount: syncResult.Deleted,
	})
}

// syncItemsPageFromAccount 同步指定页商品并返回分页统计 DTO。
// 该接口沿用现有凭证锁和 Cookie 持久化流程。
// 分页成功响应使用 itemPageSyncResponse。
// syncItemsPageFromAccount 负责sync商品列表页码From账号相关处理。
func (s *Server) syncItemsPageFromAccount(w http.ResponseWriter, r *http.Request) {
	// req 保存req，供当前处理流程使用
	var req struct {
		CookieID   string `json:"cookie_id"`
		PageNumber int    `json:"page_number"`
		PageSize   int    `json:"page_size"`
	}
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil || req.CookieID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 cookie_id 参数")
		return
	}
	// userID、ok 保存用户ID、ok，供当前处理流程使用
	_, userID, ok := s.cookieForCurrentUser(w, r, req.CookieID)
	if !ok {
		return
	}
	// credentialUnlock 保存credentialUnlock，供当前处理流程使用
	credentialUnlock := s.Store.LockAccountCredentials(req.CookieID)
	// latest、err 保存latest、err，供当前处理流程使用
	latest, err := s.loadCookiePlatformDetail(r.Context(), req.CookieID)
	if err != nil || latest == nil || latest.UserID != userID || !hasStoredCookieCredential(latest) {
		credentialUnlock()
		writeErr(w, http.StatusConflict, "账号凭证已变化，请重试")
		return
	}
	// cookieValue 保存登录凭证值，供当前处理流程使用
	cookieValue := latest.Value
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	// client 保存client，供当前处理流程使用
	client := s.mtopClient()
	// mtopCtx、cookieSession 保存mtopCtx、cookie会话，供当前处理流程使用
	mtopCtx, cookieSession := withMTopCookieSnapshot(ctx, latest)
	// res、callErr 保存res、callErr，供当前处理流程使用
	res, callErr := client.FetchItemsPage(mtopCtx, cookieValue, req.PageNumber, req.PageSize)
	if callErr == nil && res == nil {
		callErr = errors.New("商品列表接口未返回结果")
	}
	if callErr != nil {
		// value、valueChanged、persistErr 保存value、valueChanged、persistErr，供当前处理流程使用
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
	// detailCookies 保存detailCookies，供当前处理流程使用
	detailCookies := cookieValue
	if res.UpdatedCookies != "" {
		detailCookies = res.UpdatedCookies
	}
	// detailErr 保存detailErr，供当前处理流程使用
	detailErr := s.enrichSyncedItemMultiSpec(mtopCtx, client, detailCookies, req.CookieID, res.Items)
	// runtimeCookie 保存runtime登录凭证，供当前处理流程使用
	runtimeCookie := ""
	// runtimeCookieChanged 保存runtime登录凭证Changed，供当前处理流程使用
	runtimeCookieChanged := false
	// value、valueChanged、handled、persistErr 保存value、valueChanged、handled、persistErr，供当前处理流程使用
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
		if // saveErr 保存saveErr，供当前处理流程使用
		saveErr := s.Store.Cookies.UpdateValueOwned(r.Context(), req.CookieID, res.UpdatedCookies, userID); saveErr != nil {
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
	// saved 保存saved，供当前处理流程使用
	saved := s.saveSyncedItems(r.Context(), req.CookieID, res.Items)
	writeJSON(w, http.StatusOK, itemPageSyncResponse{
		Success: true, Message: "成功获取第" + strconv.Itoa(res.PageNumber) + "页 " + strconv.Itoa(len(res.Items)) + " 个商品",
		PageNumber: res.PageNumber, PageSize: res.PageSize, CurrentCount: len(res.Items), SavedCount: saved,
	})
	// 分页同步 DTO 与全集同步 DTO 分离，避免不同统计语义混用。
	// page_number 和 page_size 描述平台请求页，current_count 描述实际返回数量。
	// saved_count 只统计本次写入本地的商品。
	// 失败仍由统一错误响应负责，不在成功 DTO 中嵌入错误别名。
}

// cookieForCurrentUser 负责登录凭证ForCurrent用户相关处理。
func (s *Server) cookieForCurrentUser(w http.ResponseWriter, r *http.Request, cookieID string) (string, int64, bool) {
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	value, err := s.Store.Cookies.GetValueOwned(r.Context(), sess.UserID, cookieID) // value 和 err 是单个 Cookie 明文及读取错误。
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeErr(w, http.StatusForbidden, "无权限操作该账号")
			return "", 0, false
		}
		writeErr(w, http.StatusInternalServerError, "查询账号失败")
		return "", 0, false
	}
	if value == "" {
		writeErr(w, http.StatusBadRequest, "账号 cookie 为空")
		return "", 0, false
	}
	return value, sess.UserID, true
}

// saveSyncedItems 以下历史同步流程保持原有行为。
func (s *Server) saveSyncedItems(ctx context.Context, cookieID string, items []mtop.ItemListItem) int {
	// saved 保存saved，供当前处理流程使用
	saved := 0
	// item 表示当前遍历过程中的商品
	for _, item := range items {
		// priceText 保存price文本，供当前处理流程使用
		priceText := item.PriceText
		if priceText == "" {
			priceText = item.Price
		}
		// err 保存err，供当前处理流程使用
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
				if // multiErr 保存multiErr，供当前处理流程使用
				multiErr := s.Store.Items.SetMultiSpec(ctx, cookieID, item.ID, true); multiErr != nil && s.Logger != nil {
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

// syncSyncedItems 负责syncSynced商品列表相关处理。
func (s *Server) syncSyncedItems(ctx context.Context, cookieID string, items []mtop.ItemListItem) (db.ItemSyncResult, error) {
	// rows 保存rows，供当前处理流程使用
	rows := make([]db.ItemInfoRow, 0, len(items))
	// item 表示当前遍历过程中的商品
	for _, item := range items {
		// priceText 保存price文本，供当前处理流程使用
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

// itemSpecProbeConcurrency 限制商品多规格远端探测的并发数，避免同步洪峰压垮平台接口。
const itemSpecProbeConcurrency = 4

// itemSpecCacheTTL 定义商品多规格探测结果的短期缓存时长。
const itemSpecCacheTTL = 10 * time.Minute

// itemSpecCacheEntry 保存商品多规格结果及其过期时间。
type itemSpecCacheEntry struct {
	// isMultiSpec 表示商品是否包含多规格。
	isMultiSpec bool
	// expiresAt 表示该缓存项失效的时间。
	expiresAt time.Time
}

// itemSpecCacheKey 生成账号与商品组合的缓存键，避免不同账号同商品 ID 相互污染。
func itemSpecCacheKey(cookieID, itemID string) string {
	return cookieID + "\x00" + itemID
}

// cachedItemSpec 读取未过期的商品多规格缓存。
func (s *Server) cachedItemSpec(cookieID, itemID string) (bool, bool) {
	if s == nil {
		return false, false
	}
	s.itemSpecCacheMu.Lock()
	defer s.itemSpecCacheMu.Unlock()
	// key 表示账号与商品组合的缓存键。
	key := itemSpecCacheKey(cookieID, itemID)
	// entry、exists 保存缓存项及其存在状态。
	entry, exists := s.itemSpecCache[key]
	if !exists {
		return false, false
	}
	if time.Now().After(entry.expiresAt) {
		delete(s.itemSpecCache, key)
		return false, false
	}
	return entry.isMultiSpec, true
}

// cacheItemSpec 写入商品多规格结果并清理已过期缓存项。
func (s *Server) cacheItemSpec(cookieID, itemID string, isMultiSpec bool) {
	if s == nil || itemID == "" {
		return
	}
	s.itemSpecCacheMu.Lock()
	defer s.itemSpecCacheMu.Unlock()
	if s.itemSpecCache == nil {
		s.itemSpecCache = make(map[string]itemSpecCacheEntry)
	}
	// now 表示本次写入缓存的时间基准。
	now := time.Now()
	// key、entry 分别表示缓存键及其缓存项。
	for key, entry := range s.itemSpecCache {
		if now.After(entry.expiresAt) {
			delete(s.itemSpecCache, key)
		}
	}
	s.itemSpecCache[itemSpecCacheKey(cookieID, itemID)] = itemSpecCacheEntry{isMultiSpec: isMultiSpec, expiresAt: now.Add(itemSpecCacheTTL)}
}

// enrichSyncedItemMultiSpec 批量读取本地标记并受限并发探测剩余商品的多规格状态。
func (s *Server) enrichSyncedItemMultiSpec(ctx context.Context, client mtop.Client, cookies, cookieID string, items []mtop.ItemListItem) error {
	// fetcher、ok 保存fetcher、ok，供当前处理流程使用
	fetcher, ok := client.(mtop.ItemDetailFetcher)
	if !ok {
		return nil
	}
	// itemIDs 保存待批量读取本地多规格标记的商品标识。
	itemIDs := make([]string, 0, len(items))
	// item 表示当前同步返回的商品。
	for _, item := range items {
		itemIDs = append(itemIDs, item.ID)
	}
	// localFlags、flagsErr 保存本地多规格标记及批量查询错误。
	localFlags, flagsErr := s.Store.Items.MultiSpecFlags(ctx, cookieID, itemIDs)
	if flagsErr != nil && s.Logger != nil {
		s.Logger.Warn("批量读取商品多规格标记失败，将继续远端探测", "cookie_id", cookieID, "err", flagsErr)
	}
	// candidates 保存需要访问远端接口的商品下标。
	candidates := make([]int, 0, len(items))
	// index 表示当前商品在同步列表中的下标。
	for index := range items {
		if items[index].IsMultiSpec || localFlags[items[index].ID] {
			items[index].IsMultiSpec = true
			s.cacheItemSpec(cookieID, items[index].ID, true)
			continue
		}
		// cachedValue、cached 表示当前商品是否命中短期探测缓存。
		cachedValue, cached := s.cachedItemSpec(cookieID, items[index].ID)
		if cached {
			items[index].IsMultiSpec = cachedValue
			continue
		}
		candidates = append(candidates, index)
	}
	if len(candidates) == 0 {
		return nil
	}
	// probeCtx、cancel 让 Session 过期时的其他探测尽快停止。
	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// semaphore 限制同时进行的远端商品详情请求数。
	semaphore := make(chan struct{}, itemSpecProbeConcurrency)
	// waitGroup 等待所有商品探测 goroutine 收束。
	var waitGroup sync.WaitGroup
	// errorMu 保护 sessionErr 的并发写入。
	var errorMu sync.Mutex
	// sessionErr 保存首个需要中断同步的 Session 过期错误。
	var sessionErr error
	// index 表示当前需要远端探测的商品下标。
	for _, index := range candidates {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			select {
			case semaphore <- struct{}{}:
			case <-probeCtx.Done():
				return
			}
			defer func() { <-semaphore }()
			// isMultiSpec、err 保存当前商品远端探测结果及错误。
			isMultiSpec, err := fetcher.DetectItemMultiSpec(probeCtx, cookies, items[index].ID)
			if err != nil {
				if mtop.IsSessionExpiredErr(err) {
					errorMu.Lock()
					if sessionErr == nil {
						sessionErr = err
						cancel()
					}
					errorMu.Unlock()
					return
				}
				if probeCtx.Err() != nil {
					return
				}
				if s.Logger != nil {
					s.Logger.Warn("识别商品多规格状态失败", "cookie_id", cookieID, "item_id", items[index].ID, "err", err)
				}
				return
			}
			s.cacheItemSpec(cookieID, items[index].ID, isMultiSpec)
			items[index].IsMultiSpec = isMultiSpec
		}(index)
	}
	waitGroup.Wait()
	return sessionErr
}

// listItemsByCookie 负责list商品列表By登录凭证相关处理。
func (s *Server) listItemsByCookie(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cookie_id")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// items、err 保存items、err，供当前处理流程使用
	items, err := s.Store.Items.AllForCookie(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	// out 保存out，供当前处理流程使用
	out := make([]itemListResponse, 0, len(items))
	// it 表示当前遍历过程中的it
	for _, it := range items {
		out = append(out, itemToMap(it))
	}
	writeJSON(w, http.StatusOK, out)
}

// getItem 负责get商品相关处理。
func (s *Server) getItem(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cookie_id")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// itemID 保存商品ID，供当前处理流程使用
	itemID := chi.URLParam(r, "item_id")
	// it、err 保存it、err，供当前处理流程使用
	it, err := s.Store.Items.Get(r.Context(), cid, itemID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "商品不存在")
		return
	}
	writeJSON(w, http.StatusOK, itemDetailResponse{
		CookieID: it.CookieID, ItemID: it.ItemID, ItemTitle: it.ItemTitle, ItemDescription: it.ItemDescription,
		ItemCategory: it.ItemCategory, ItemPrice: it.ItemPrice, ItemDetail: it.ItemDetail,
		IsMultiSpec: it.IsMultiSpec, MultiQuantityDelivery: it.MultiQuantityDelivery,
	})
}

// createItem 创建本地商品记录并返回统一操作结果。
func (s *Server) createItem(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cookie_id")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// req 保存req，供当前处理流程使用
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
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil || req.ItemID == "" {
		writeErr(w, http.StatusBadRequest, "缺少商品 ID")
		return
	}
	if req.MultiQuantityDelivery || req.IsMultiQtyShip {
		req.MultiQuantityDelivery = true
	}
	if // err 保存err，供当前处理流程使用
	err := s.Store.Items.Upsert(r.Context(), &db.ItemInfoRow{
		CookieID: cid, ItemID: req.ItemID, ItemTitle: req.ItemTitle, ItemDescription: req.ItemDescription,
		ItemCategory: req.ItemCategory, ItemPrice: req.ItemPrice, ItemDetail: req.ItemDetail,
		IsMultiSpec: req.IsMultiSpec, MultiQuantityDelivery: req.MultiQuantityDelivery,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "新增失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// updateItem 负责update商品相关处理。
func (s *Server) updateItem(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cookie_id")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// itemID 保存商品ID，供当前处理流程使用
	itemID := chi.URLParam(r, "item_id")
	// req 保存req，供当前处理流程使用
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
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// existing、err 保存existing、err，供当前处理流程使用
	existing, err := s.Store.Items.Get(r.Context(), cid, itemID)
	if errors.Is(err, db.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "商品不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	// row 保存row，供当前处理流程使用
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
	if // err 保存err，供当前处理流程使用
	err := s.Store.Items.Upsert(r.Context(), row); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// deleteItem 负责delete商品相关处理。
func (s *Server) deleteItem(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cookie_id")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// itemID 保存商品ID，供当前处理流程使用
	itemID := chi.URLParam(r, "item_id")
	if // err 保存err，供当前处理流程使用
	err := s.Store.Items.Delete(r.Context(), cid, itemID); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// setItemMultiSpec 负责set商品MultiSpec相关处理。
func (s *Server) setItemMultiSpec(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cookie_id")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// itemID 保存商品ID，供当前处理流程使用
	itemID := chi.URLParam(r, "item_id")
	// req 保存req，供当前处理流程使用
	var req struct {
		IsMultiSpec bool `json:"is_multi_spec"`
	}
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if // err 保存err，供当前处理流程使用
	err := s.Store.Items.SetMultiSpec(r.Context(), cid, itemID, req.IsMultiSpec); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// setItemMultiQuantity 负责set商品MultiQuantity相关处理。
func (s *Server) setItemMultiQuantity(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cookie_id")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// itemID 保存商品ID，供当前处理流程使用
	itemID := chi.URLParam(r, "item_id")
	// req 保存req，供当前处理流程使用
	var req struct {
		MultiQuantityDelivery bool `json:"multi_quantity_delivery"`
	}
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if // err 保存err，供当前处理流程使用
	err := s.Store.Items.SetMultiQuantity(r.Context(), cid, itemID, req.MultiQuantityDelivery); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

func itemToMap(it db.ItemInfoRow) itemListResponse { // itemToMap 将数据库商品行转换为商品列表 DTO。
	imageURL := itemImageFromDetail(it.ItemDetail)
	return itemListResponse{
		ID: it.ID, CookieID: it.CookieID, ItemID: it.ItemID, ItemTitle: it.ItemTitle,
		ItemDescription: it.ItemDescription, ItemCategory: it.ItemCategory, ItemPrice: it.ItemPrice,
		ItemDetail: it.ItemDetail, ItemImage: imageURL, IsMultiSpec: it.IsMultiSpec,
		MultiQuantityDelivery: it.MultiQuantityDelivery, IsMultiQtyShip: it.MultiQuantityDelivery,
	}
}

// itemImageFromDetail 解析本地商品详情中的主图地址。

// 商品详情解析失败时返回空字符串，保持列表响应可渲染。
func itemImageFromDetail(detail string) string {
	if detail == "" {
		return ""
	}
	// m 保存m，供当前处理流程使用
	var m map[string]any
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal([]byte(detail), &m); err != nil {
		return ""
	}
	if // pic、ok 保存pic、ok，供当前处理流程使用
	pic, ok := m["pic_info"].(map[string]any); ok {
		if // url、ok 保存url、ok，供当前处理流程使用
		url, ok := pic["picUrl"].(string); ok {
			return url
		}
	}
	if // url、ok 保存url、ok，供当前处理流程使用
	url, ok := m["item_image"].(string); ok {
		return url
	}
	return ""
}
