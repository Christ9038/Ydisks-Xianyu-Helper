package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	accountapp "xianyu-go/internal/application/account"
	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/auth"
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
	// location 保存带有 JSON 标签的 HTTP 发货地请求模型。
	var location itemPublishLocationRequest
	// selectedLocation 保存解析成功、待转换为应用模型的发货地。
	var selectedLocation *itemPublishLocationRequest
	if // rawLocation 保存原始地址，供当前处理流程使用
	rawLocation := strings.TrimSpace(r.FormValue("location")); rawLocation != "" {
		if // err 保存err，供当前处理流程使用
		err := json.Unmarshal([]byte(rawLocation), &location); err != nil {
			writeErr(w, http.StatusBadRequest, "发货地格式错误，请重新定位")
			return
		}
		selectedLocation = &location
	}
	// applicationLocation 是 HTTP DTO 转换后的应用发货地模型。
	var applicationLocation *itemapp.Location
	if selectedLocation != nil {
		applicationLocation = &itemapp.Location{
			Area: selectedLocation.Area, City: selectedLocation.City, DivisionID: selectedLocation.DivisionID,
			Longitude: selectedLocation.Longitude, Latitude: selectedLocation.Latitude, POIID: selectedLocation.POIID,
			POIName: selectedLocation.POIName, Province: selectedLocation.Province,
		}
	}
	// applicationImages 是 HTTP 上传图片转换后的应用图片模型。
	applicationImages := make([]itemapp.Image, 0, len(images))
	// image 表示当前待转换的 HTTP 上传图片。
	for _, image := range images {
		applicationImages = append(applicationImages, itemapp.Image{Filename: image.Filename, ContentType: image.ContentType, Data: image.Data})
	}
	// outcome、callErr 保存应用服务返回的发布结果及调用错误。
	outcome, callErr := s.itemSinglePublishApplication().PublishSingle(r.Context(), itemapp.PublishInput{
		UserID: userID, CookieID: cookieID, Title: title, Description: description,
		PriceCents: priceCents, OriginalPriceCents: origCents, Quantity: quantity,
		PostageMode: postageMode, PostageCents: postageCents, Location: applicationLocation, Images: applicationImages,
	})
	// res 保存响应，供当前处理流程使用
	res := outcome.Result
	if callErr != nil {
		// perr 保存perr，供当前处理流程使用
		var perr *itemapp.PublishError
		if errors.As(callErr, &perr) {
			// status 保存状态，供当前处理流程使用
			status := http.StatusBadGateway
			// msg 保存msg，供当前处理流程使用
			msg := perr.Error()
			if perr.Code == itemapp.PublishErrorStockPermissionMissing {
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
}

// readPublishImages 负责read发布Images相关处理。
func readPublishImages(r *http.Request, maxImages int) ([]itemapp.Image, error) {
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
	images := make([]itemapp.Image, 0, len(files))
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
		images = append(images, itemapp.Image{Filename: fh.Filename, ContentType: contentType, Data: data})
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

// itemPublishLocationRequest 是发布接口接收的发货地 HTTP DTO；平台字段转换由发布应用端口负责。
type itemPublishLocationRequest struct {
	// Area 是发货地的区县名称。
	Area string `json:"area"`
	// City 是发货地的城市名称。
	City string `json:"city"`
	// DivisionID 是平台行政区划标识。
	DivisionID string `json:"division_id"`
	// Longitude 是发货地经度。
	Longitude float64 `json:"longitude"`
	// Latitude 是发货地纬度。
	Latitude float64 `json:"latitude"`
	// POIID 是地图服务返回的兴趣点标识。
	POIID string `json:"poi_id"`
	// POIName 是地图服务返回的兴趣点名称。
	POIName string `json:"poi_name"`
	// Province 是发货地的省份名称。
	Province string `json:"province"`
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
	// items、err 保存应用层商品查询结果及错误。
	items, err := s.itemCatalogApplication().ListForUser(r.Context(), sess.UserID, cookieID)
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

// itemSyncRequest 是商品全量和分页同步接口共用的请求 DTO。
type itemSyncRequest struct {
	// CookieID 是待同步的平台账号标识。
	CookieID string `json:"cookie_id"`
	// PageNumber 是分页同步的平台页码。
	PageNumber int `json:"page_number"`
	// PageSize 是平台单页大小。
	PageSize int `json:"page_size"`
	// MaxPages 是全量同步允许读取的最大页数。
	MaxPages int `json:"max_pages"`
}

// syncItemsFromAccount 解析请求并调用商品全集同步应用服务。
func (s *Server) syncItemsFromAccount(w http.ResponseWriter, r *http.Request) {
	// req 保存解码后的商品全集同步请求。
	var req itemSyncRequest
	// err 保存商品全集同步请求解码失败的原因。
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.CookieID) == "" {
		writeErr(w, http.StatusBadRequest, "缺少 cookie_id 参数")
		return
	}
	// sess 保存认证中间件提供的当前用户会话。
	sess := auth.SessionFromContext(r.Context())
	// result、err 保存应用服务返回的全集同步结果及错误。
	result, err := s.itemSyncApplication().SyncAll(r.Context(), itemapp.SyncQuery{UserID: sess.UserID, CookieID: req.CookieID, PageSize: req.PageSize, MaxPages: req.MaxPages})
	if err != nil {
		s.writeItemSyncError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, itemSyncResponse{Success: true, Message: "成功获取商品，共 " + strconv.Itoa(result.TotalCount) + " 件，保存 " + strconv.Itoa(result.SavedCount) + " 件，删除 " + strconv.Itoa(result.DeletedCount) + " 件", TotalCount: result.TotalCount, TotalPages: result.TotalPages, SavedCount: result.SavedCount, DeletedCount: result.DeletedCount})
}

// syncItemsPageFromAccount 解析请求并调用商品分页同步应用服务。
func (s *Server) syncItemsPageFromAccount(w http.ResponseWriter, r *http.Request) {
	// req 保存解码后的商品分页同步请求。
	var req itemSyncRequest
	// err 保存商品分页同步请求解码失败的原因。
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.CookieID) == "" {
		writeErr(w, http.StatusBadRequest, "缺少 cookie_id 参数")
		return
	}
	// sess 保存认证中间件提供的当前用户会话。
	sess := auth.SessionFromContext(r.Context())
	// result、err 保存应用服务返回的分页同步结果及错误。
	result, err := s.itemSyncApplication().SyncPage(r.Context(), itemapp.SyncQuery{UserID: sess.UserID, CookieID: req.CookieID, PageNumber: req.PageNumber, PageSize: req.PageSize})
	if err != nil {
		s.writeItemSyncError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, itemPageSyncResponse{Success: true, Message: "成功获取第" + strconv.Itoa(result.PageNumber) + "页 " + strconv.Itoa(result.CurrentCount) + " 个商品", PageNumber: result.PageNumber, PageSize: result.PageSize, CurrentCount: result.CurrentCount, SavedCount: result.SavedCount})
}

// writeItemSyncError 将应用同步错误映射为稳定的 HTTP 状态和消息。
func (s *Server) writeItemSyncError(w http.ResponseWriter, err error) {
	if errors.Is(err, itemapp.ErrSyncNotOwned) {
		writeErr(w, http.StatusForbidden, "无权限操作该账号")
		return
	}
	if errors.Is(err, itemapp.ErrSyncInvalidUser) || errors.Is(err, itemapp.ErrSyncInvalidCookie) {
		writeErr(w, http.StatusBadRequest, "缺少 cookie_id 参数")
		return
	}
	// stageErr 保存应用服务提供的同步阶段错误。
	var stageErr *itemapp.SyncError
	if errors.As(err, &stageErr) {
		switch stageErr.Kind {
		case itemapp.SyncErrorCredential:
			writeErr(w, http.StatusConflict, "账号凭证已变化，请重试")
		case itemapp.SyncErrorPersistence:
			writeErr(w, http.StatusInternalServerError, "保存商品同步结果失败")
		default:
			writeErr(w, http.StatusBadGateway, stageErr.Error())
		}
		return
	}
	writeErr(w, http.StatusInternalServerError, "商品同步失败")
}

// cookieForCurrentUser 负责登录凭证ForCurrent用户相关处理。
func (s *Server) cookieForCurrentUser(w http.ResponseWriter, r *http.Request, cookieID string) (string, int64, bool) {
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// service 提供消费者定义的平台凭证只读端口；本校验不会把 Cookie 明文带入 HTTP 层。
	service := s.platformCredentialApplication()
	if service == nil {
		writeErr(w, http.StatusInternalServerError, "查询账号失败")
		return "", 0, false
	}
	// value、err 保存已完成归属复核的平台 Cookie 及校验错误；明文只交给当前平台调用链。
	value, err := service.LoadOwnedValue(r.Context(), sess.UserID, cookieID)
	if err != nil {
		if errors.Is(err, accountapp.ErrCredentialNotFound) || errors.Is(err, accountapp.ErrForbidden) {
			writeErr(w, http.StatusForbidden, "无权限操作该账号")
			return "", 0, false
		}
		if errors.Is(err, accountapp.ErrCredentialEmpty) {
			writeErr(w, http.StatusBadRequest, "账号 cookie 为空")
			return "", 0, false
		}
		writeErr(w, http.StatusInternalServerError, "查询账号失败")
		return "", 0, false
	}
	return value, sess.UserID, true
}

// listItemsByCookie 负责list商品列表By登录凭证相关处理。
func (s *Server) listItemsByCookie(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cookie_id")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// items、err 保存应用层商品查询结果及错误。
	items, err := s.itemCatalogApplication().ListByCookie(r.Context(), cid)
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
	it, err := s.itemCatalogApplication().Get(r.Context(), cid, itemID)
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

// itemCreateRequest 是创建本地商品接口的请求 DTO。
type itemCreateRequest struct {
	// ItemID 是平台商品标识。
	ItemID string `json:"item_id"`
	// ItemTitle 是商品标题。
	ItemTitle string `json:"item_title"`
	// ItemDescription 是商品描述。
	ItemDescription string `json:"item_description"`
	// ItemCategory 是平台商品类目标识。
	ItemCategory string `json:"item_category"`
	// ItemPrice 是商品价格文本。
	ItemPrice string `json:"item_price"`
	// ItemDetail 是商品扩展详情 JSON。
	ItemDetail string `json:"item_detail"`
	// IsMultiSpec 表示是否启用多规格交付。
	IsMultiSpec bool `json:"is_multi_spec"`
	// MultiQuantityDelivery 表示是否启用多数量交付。
	MultiQuantityDelivery bool `json:"multi_quantity_delivery"`
	// IsMultiQtyShip 是历史客户端使用的多数量交付别名。
	IsMultiQtyShip bool `json:"is_multi_qty_ship"`
}

// itemUpdateRequest 是局部更新本地商品接口的请求 DTO。
type itemUpdateRequest struct {
	// ItemTitle 是可选的商品标题。
	ItemTitle *string `json:"item_title"`
	// ItemDescription 是可选的商品描述。
	ItemDescription *string `json:"item_description"`
	// ItemCategory 是可选的商品类目标识。
	ItemCategory *string `json:"item_category"`
	// ItemPrice 是可选的商品价格文本。
	ItemPrice *string `json:"item_price"`
	// ItemDetail 是可选的商品扩展详情 JSON。
	ItemDetail *string `json:"item_detail"`
	// IsMultiSpec 是可选的多规格交付开关。
	IsMultiSpec *bool `json:"is_multi_spec"`
	// MultiQuantityDelivery 是可选的多数量交付开关。
	MultiQuantityDelivery *bool `json:"multi_quantity_delivery"`
	// IsMultiQtyShip 是历史客户端使用的多数量交付开关别名。
	IsMultiQtyShip *bool `json:"is_multi_qty_ship"`
}

// itemMultiSpecRequest 是更新商品多规格开关的请求 DTO。
type itemMultiSpecRequest struct {
	// IsMultiSpec 表示是否启用多规格交付。
	IsMultiSpec bool `json:"is_multi_spec"`
}

// itemMultiQuantityRequest 是更新商品多数量交付开关的请求 DTO。
type itemMultiQuantityRequest struct {
	// MultiQuantityDelivery 表示是否启用多数量交付。
	MultiQuantityDelivery bool `json:"multi_quantity_delivery"`
}

// createItem 创建本地商品记录并返回统一操作结果。
func (s *Server) createItem(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cookie_id")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// req 保存解码后的创建商品请求。
	var req itemCreateRequest
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil || req.ItemID == "" {
		writeErr(w, http.StatusBadRequest, "缺少商品 ID")
		return
	}
	if req.MultiQuantityDelivery || req.IsMultiQtyShip {
		req.MultiQuantityDelivery = true
	}
	// err 保存商品创建应用服务返回的错误。
	if err := s.itemCatalogMutationApplication().Create(r.Context(), cid, itemapp.CatalogWriteInput{
		ItemID: req.ItemID, ItemTitle: req.ItemTitle, ItemDescription: req.ItemDescription,
		ItemCategory: req.ItemCategory, ItemPrice: req.ItemPrice, ItemDetail: req.ItemDetail,
		IsMultiSpec: req.IsMultiSpec, MultiQuantityDelivery: req.MultiQuantityDelivery,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "新增失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// updateItem 更新本地商品的部分字段并保留未提交字段。
func (s *Server) updateItem(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cookie_id")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// itemID 保存商品ID，供当前处理流程使用
	itemID := chi.URLParam(r, "item_id")
	// req 保存解码后的局部更新商品请求。
	var req itemUpdateRequest
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// patch 保存 HTTP DTO 转换后的应用层局部更新输入。
	patch := itemapp.CatalogPatchInput{
		ItemTitle: req.ItemTitle, ItemDescription: req.ItemDescription, ItemCategory: req.ItemCategory,
		ItemPrice: req.ItemPrice, ItemDetail: req.ItemDetail, IsMultiSpec: req.IsMultiSpec,
		MultiQuantityDelivery: req.MultiQuantityDelivery,
	}
	if req.IsMultiQtyShip != nil {
		patch.MultiQuantityDelivery = req.IsMultiQtyShip
	}
	// err 保存商品更新应用服务返回的错误。
	if err := s.itemCatalogMutationApplication().Update(r.Context(), cid, itemID, patch); err != nil {
		if errors.Is(err, itemapp.ErrCatalogNotFound) {
			writeErr(w, http.StatusNotFound, "商品不存在")
			return
		}
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
	// err 保存商品删除应用服务返回的错误。
	if err := s.itemCatalogMutationApplication().Delete(r.Context(), cid, itemID); err != nil {
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
	// req 保存解码后的多规格开关请求。
	var req itemMultiSpecRequest
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// err 保存商品多规格应用服务返回的错误。
	if err := s.itemCatalogMutationApplication().SetMultiSpec(r.Context(), cid, itemID, req.IsMultiSpec); err != nil {
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
	// req 保存解码后的多数量交付开关请求。
	var req itemMultiQuantityRequest
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// err 保存商品多数量应用服务返回的错误。
	if err := s.itemCatalogMutationApplication().SetMultiQuantity(r.Context(), cid, itemID, req.MultiQuantityDelivery); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

func itemToMap(it itemapp.CatalogItem) itemListResponse { // itemToMap 将应用商品模型转换为商品列表 DTO。
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
