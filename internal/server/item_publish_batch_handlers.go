package server

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/adapter"
	accountapp "xianyu-go/internal/application/account"
	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/auth"
)

// maxPublishBatchRows 用于本次流程后续判断的max发布批次Rows
const (
	maxPublishBatchRows = 50
	publishBatchLease   = 5 * time.Minute
)

// postPublishError 用于本次流程后续判断的post发布错误
type postPublishError struct{ err error }

// Error 封装错误业务协调。
func (e *postPublishError) Error() string { return e.err.Error() }

// Unwrap 封装Unwrap业务协调。
func (e *postPublishError) Unwrap() error { return e.err }

// uncertainRemotePublishError 用于本次流程后续判断的uncertainRemote发布错误
type uncertainRemotePublishError struct{ err error }

// Error 封装错误业务协调。
func (e *uncertainRemotePublishError) Error() string { return e.err.Error() }

// Unwrap 封装Unwrap业务协调。
func (e *uncertainRemotePublishError) Unwrap() error { return e.err }

// publishBatchPreviewRow 用于本次流程后续判断的发布批次PreviewRow
type publishBatchPreviewRow struct {
	RowNo      int                     `json:"row_no"`
	Valid      bool                    `json:"valid"`
	Errors     []string                `json:"errors,omitempty"`
	CookieID   string                  `json:"cookie_id"`
	Title      string                  `json:"title"`
	Price      string                  `json:"price"`
	Quantity   int                     `json:"quantity"`
	Images     []string                `json:"images"`
	Category   publishCategoryResponse `json:"category"`
	Automation publishAutomationConfig `json:"automation"`
}

// publishCategoryRecommendationRequest 是请求指定账号商品类目推荐的 HTTP 请求 DTO。
type publishCategoryRecommendationRequest struct {
	// CookieID 是发起平台类目推荐的已归属账号标识。
	CookieID string `json:"cookie_id"`
	// Keyword 是用于匹配平台类目的用户输入关键词。
	Keyword string `json:"keyword"`
}

// itemPublishBatchStartRequest 是启动已预检批次的 HTTP 请求 DTO，保留 batch_id 兼容旧客户端。
type itemPublishBatchStartRequest struct {
	// PreviewID 是当前客户端使用的预检批次标识。
	PreviewID string `json:"preview_id"`
	// BatchID 是历史客户端发送的兼容批次标识。
	BatchID string `json:"batch_id"`
}

// publishBatchPreviewApplicationResponse 将应用预检结果映射为既有 HTTP DTO。
func publishBatchPreviewApplicationResponse(result itemapp.BatchPreviewPersistenceResult) itemPublishBatchPreviewResponse {
	// rows 保存兼容前端字段命名的逐行预检结果。
	rows := make([]publishBatchPreviewRow, 0, len(result.Rows))
	// row 表示当前待映射的应用预检行。
	for _, row := range result.Rows {
		// previewRow 保存当前应用行对应的 HTTP 边界模型。
		previewRow := publishBatchPreviewRow{
			RowNo: row.RowNo, Valid: len(row.Errors) == 0, Errors: row.Errors, CookieID: row.CookieID,
			Title: row.Title, Price: row.Price, Quantity: row.Quantity, Images: row.Images,
			Category: publishCategoryResponse{CatID: row.Category.CatID, CatName: row.Category.CatName, ChannelCatID: row.Category.ChannelCatID, TBCatID: row.Category.TBCatID},
			Automation: publishAutomationConfig{
				PaidDelivery:  publishCardAutomation{Enabled: row.Automation.PaidDelivery.Enabled, ParseError: row.Automation.PaidDelivery.ParseError},
				ReviewGift:    publishCardAutomation{Enabled: row.Automation.ReviewGift.Enabled, ParseError: row.Automation.ReviewGift.ParseError},
				ReviewRequest: publishReviewRequestCfg{Enabled: row.Automation.ReviewRequest.Enabled, AfterShippedHours: row.Automation.ReviewRequest.AfterShippedHours, Message: row.Automation.ReviewRequest.Message, MaxAttempts: row.Automation.ReviewRequest.MaxAttempts, DelaySeconds: row.Automation.ReviewRequest.DelaySeconds},
			},
		}
		// action 表示当前应用行中的付款发货动作。
		for _, action := range row.Automation.PaidDelivery.Actions {
			previewRow.Automation.PaidDelivery.Actions = append(previewRow.Automation.PaidDelivery.Actions, publishCardAction{CardID: action.CardID, DeliveryCount: action.DeliveryCount, DelaySeconds: action.DelaySeconds})
		}
		// action 表示当前应用行中的评价赠品动作。
		for _, action := range row.Automation.ReviewGift.Actions {
			previewRow.Automation.ReviewGift.Actions = append(previewRow.Automation.ReviewGift.Actions, publishCardAction{CardID: action.CardID, DeliveryCount: action.DeliveryCount, DelaySeconds: action.DelaySeconds})
		}
		rows = append(rows, previewRow)
	}
	return itemPublishBatchPreviewResponse{Success: result.Success, PreviewID: result.PreviewID, Total: result.Total, Valid: result.Valid, Invalid: result.Invalid, Rows: rows}
}

// recommendItemPublishCategory 解析类目关键词并返回平台推荐类目。
func (s *Server) recommendItemPublishCategory(w http.ResponseWriter, r *http.Request) {
	// req 是商品类目推荐请求的具名传输 DTO。
	var req publishCategoryRecommendationRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	req.CookieID = strings.TrimSpace(req.CookieID)
	req.Keyword = strings.TrimSpace(req.Keyword)
	if req.CookieID == "" {
		writeErr(w, http.StatusBadRequest, "请先选择发布账号")
		return
	}
	if req.Keyword == "" {
		writeErr(w, http.StatusBadRequest, "请输入类目关键词")
		return
	}
	// userID、ok 用于本次流程后续判断的用户ID、ok
	_, userID, ok := s.cookieForCurrentUser(w, r, req.CookieID)
	if !ok {
		return
	}
	// category、callErr 用于本次流程后续判断的category、callErr
	category, callErr := s.itemCategoryRecommendationApplication().Recommend(r.Context(), userID, req.CookieID, req.Keyword)
	if callErr != nil {
		if errors.Is(callErr, itemapp.ErrCategoryUnsupported) {
			writeErr(w, http.StatusNotImplemented, callErr.Error())
			return
		}
		if errors.Is(callErr, itemapp.ErrCategoryCredentialChanged) {
			writeErr(w, http.StatusConflict, callErr.Error())
			return
		}
		if errors.Is(callErr, itemapp.ErrCategoryPersistence) {
			writeErr(w, http.StatusInternalServerError, callErr.Error())
			return
		}
		if errors.Is(callErr, itemapp.ErrCategoryUnrecognized) {
			writeErr(w, http.StatusNotFound, "没有匹配到可发布类目，请换一个关键词")
			return
		}
		writeErr(w, http.StatusBadGateway, callErr.Error())
		return
	}
	writeJSON(w, http.StatusOK, categoryRecommendationResponse{Success: true, Category: publishCategoryResponse{CatID: category.CatID, CatName: category.CatName, ChannelCatID: category.ChannelCatID, TBCatID: category.TBCatID}})
}

// previewItemPublishBatch 处理表格上传、图片归档和批量发布预检。
func (s *Server) previewItemPublishBatch(w http.ResponseWriter, r *http.Request) {
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	s.cleanupExpiredPublishUploads(r.Context())
	// 表格最大 20 MiB，图片压缩包最大 200 MiB，额外预留 multipart 元数据空间。
	r.Body = http.MaxBytesReader(w, r.Body, maxItemPublishBatchBytes)
	// #nosec G120 -- 请求体已由 MaxBytesReader 限制。
	if err := r.ParseMultipartForm(maxItemPublishBatchParseBytes); err != nil {
		writeErr(w, http.StatusBadRequest, "解析上传文件失败")
		return
	}
	// defaultCookieID 用于本次流程后续判断的default登录凭证ID
	defaultCookieID := strings.TrimSpace(r.FormValue("default_cookie_id"))
	if defaultCookieID == "" {
		writeErr(w, http.StatusBadRequest, "请选择默认发布账号")
		return
	}
	// defaultOwned 和 ownershipErr 表示默认账号归属复核结果及基础设施错误。
	defaultOwned, ownershipErr := s.itemBatchPreviewApplication().CookieOwned(r.Context(), sess.UserID, defaultCookieID)
	if ownershipErr != nil {
		writeErr(w, http.StatusInternalServerError, "校验默认发布账号失败")
		return
	}
	if !defaultOwned {
		writeErr(w, http.StatusForbidden, "默认账号不属于当前用户")
		return
	}
	// fallbackCategory 用于本次流程后续判断的fallback分类
	fallbackCategory := publishCategoryResponse{
		CatID:        strings.TrimSpace(r.FormValue("fallback_category_id")),
		CatName:      strings.TrimSpace(r.FormValue("fallback_category_name")),
		ChannelCatID: strings.TrimSpace(r.FormValue("fallback_channel_category_id")),
		TBCatID:      strings.TrimSpace(r.FormValue("fallback_tb_category_id")),
	}
	// batchLocation 用于本次流程后续判断的批次地址
	var batchLocation publishBatchLocation
	// publishIntervalSeconds 保存用户为本批次设置的最终发布最小间隔，单位为秒。
	publishIntervalSeconds, intervalErr := parsePublishIntervalSeconds(r.FormValue("publish_interval_seconds"))
	if intervalErr != nil {
		writeErr(w, http.StatusBadRequest, intervalErr.Error())
		return
	}
	// locationJSON 用于本次流程后续判断的地址JSON
	locationJSON := strings.TrimSpace(r.FormValue("location"))
	if locationJSON != "" {
		if json.Unmarshal([]byte(locationJSON), &batchLocation) != nil {
			writeErr(w, http.StatusBadRequest, "发货地格式错误，请重新定位")
			return
		}
	}
	// hasDefaultCategory 用于本次流程后续判断的hasDefault分类
	hasDefaultCategory := fallbackCategory.CatID != "" || fallbackCategory.CatName != "" || fallbackCategory.ChannelCatID != "" || fallbackCategory.TBCatID != ""
	if hasDefaultCategory && (fallbackCategory.CatID == "" || fallbackCategory.CatName == "" || fallbackCategory.ChannelCatID == "") {
		writeErr(w, http.StatusBadRequest, "默认类目信息不完整，请重新通过关键词获取")
		return
	}
	// source、sourceHeader、err 用于本次流程后续判断的source、sourceHeader、err
	source, sourceHeader, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "缺少商品表格文件")
		return
	}
	defer source.Close()
	// sourceBytes、tooLarge、err 用于本次流程后续判断的sourceBytes、tooLarge、err
	sourceBytes, tooLarge, err := readLimitedBytes(source, 20<<20)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "读取商品表格失败")
		return
	}
	if tooLarge {
		writeErr(w, http.StatusBadRequest, "商品表格不能超过 20 MiB")
		return
	}
	// batchID 用于本次流程后续判断的批次ID
	batchID := "batch_" + randomHex(12)
	// uploadDir 用于本次流程后续判断的uploadDir
	uploadDir := filepath.Join(s.publishUploadRoot(), "publish_batches", batchID)
	if // err 用于本次流程后续判断的err
	err := os.MkdirAll(uploadDir, 0o750); err != nil {
		writeErr(w, http.StatusInternalServerError, "创建上传目录失败")
		return
	}
	// keepUpload 用于本次流程后续判断的keepUpload
	keepUpload := false
	defer func() {
		if !keepUpload {
			_ = os.RemoveAll(uploadDir)
		}
	}()
	// sourceName 用于本次流程后续判断的source名称
	sourceName := safeBaseName(sourceHeader.Filename)
	if sourceName == "" {
		sourceName = "products.csv"
	}
	if // err 用于本次流程后续判断的err
	err := writeFileWithinRoot(uploadDir, sourceName, sourceBytes); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存商品表格失败")
		return
	}

	if // zipFile、zipHeader、err 用于本次流程后续判断的zipFile、zipHeader、err
	zipFile, zipHeader, err := r.FormFile("images_zip"); err == nil {
		defer zipFile.Close()
		// zipBytes、tooLarge、err 用于本次流程后续判断的zipBytes、tooLarge、err
		zipBytes, tooLarge, err := readLimitedBytes(zipFile, 200<<20)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "读取图片 zip 失败")
			return
		}
		if tooLarge {
			writeErr(w, http.StatusBadRequest, "图片 zip 不能超过 200 MiB")
			return
		}
		// zipName 用于本次流程后续判断的zip名称
		zipName := safeBaseName(zipHeader.Filename)
		if zipName == "" {
			zipName = "images.zip"
		}
		if // err 用于本次流程后续判断的err
		err := writeFileWithinRoot(uploadDir, zipName, zipBytes); err != nil {
			writeErr(w, http.StatusInternalServerError, "保存图片 zip 失败")
			return
		}
		if // err 用于本次流程后续判断的err
		err := extractPublishImagesZip(zipBytes, uploadDir); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	// maps、err 用于本次流程后续判断的maps、err
	maps, err := itemapp.ParseSheet(sourceBytes, sourceName, maxPublishBatchRows)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(maps) > maxPublishBatchRows {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("单个批次最多支持 %d 条商品", maxPublishBatchRows))
		return
	}
	// previewRows 保存应用服务归一化并校验后的逐行结果。
	previewRows, err := s.itemBatchPreviewApplication().Preview(r.Context(), itemapp.BatchPreviewInput{
		UserID: sess.UserID, DefaultCookieID: defaultCookieID, UploadDir: uploadDir,
		FallbackCategory: itemapp.BatchPreviewCategory{CatID: fallbackCategory.CatID, CatName: fallbackCategory.CatName, ChannelCatID: fallbackCategory.ChannelCatID, TBCatID: fallbackCategory.TBCatID},
		Rows:             maps,
	})
	if err != nil {
		if errors.Is(err, itemapp.ErrBatchPreviewNoRows) {
			writeErr(w, http.StatusBadRequest, "表格中没有有效数据行")
		} else {
			writeErr(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	// preview、err 保存预检结果持久化服务返回的应用模型及错误。
	preview, err := s.itemBatchPreviewPersistenceApplication().Persist(r.Context(), itemapp.BatchPreviewPersistenceBatch{
		ID: batchID, UserID: sess.UserID, DefaultCookieID: defaultCookieID, Filename: sourceName,
		UploadDir: uploadDir, PublishIntervalSeconds: publishIntervalSeconds,
		Location: itemapp.Location{Area: batchLocation.Area, City: batchLocation.City, DivisionID: batchLocation.DivisionID, Longitude: batchLocation.Longitude, Latitude: batchLocation.Latitude, POIID: batchLocation.POIID, POIName: batchLocation.POIName, Province: batchLocation.Province},
	}, previewRows)
	if err != nil {
		if errors.Is(err, itemapp.ErrBatchPreviewNoRows) {
			writeErr(w, http.StatusBadRequest, "表格中没有有效数据行")
		} else {
			writeErr(w, http.StatusInternalServerError, "保存预检结果失败")
		}
		return
	}
	keepUpload = true
	writeJSON(w, http.StatusOK, publishBatchPreviewApplicationResponse(preview))
}

// startItemPublishBatch 启动指定批次的后台发布 worker。
func (s *Server) startItemPublishBatch(w http.ResponseWriter, r *http.Request) {
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	// req 是启动商品批量发布请求的具名传输 DTO。
	var req itemPublishBatchStartRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// batchID 用于本次流程后续判断的批次ID
	batchID := strings.TrimSpace(req.PreviewID)
	if batchID == "" {
		batchID = strings.TrimSpace(req.BatchID)
	}
	if batchID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 preview_id")
		return
	}
	// startedID、err 用于本次流程后续判断的startedID、err
	startedID, err := s.itemBatchManagementApplication().StartBatch(r.Context(), sess.UserID, batchID, publishBatchLease)
	if err != nil {
		switch {
		case errors.Is(err, itemapp.ErrBatchNotFound):
			writeErr(w, http.StatusNotFound, "批量任务不存在")
		case errors.Is(err, itemapp.ErrBatchConflict):
			writeErr(w, http.StatusConflict, "任务正在由其他 worker 运行")
		case errors.Is(err, itemapp.ErrBatchInvalidState):
			writeErr(w, http.StatusBadRequest, "当前任务状态不能开始发布")
		case errors.Is(err, itemapp.ErrBatchNoRows):
			writeErr(w, http.StatusBadRequest, "没有可发布的商品行")
		default:
			writeErr(w, http.StatusInternalServerError, "启动任务失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, batchIDResponse{Success: true, BatchID: startedID})
}

// listItemPublishBatches 返回当前用户的批量发布任务列表。
func (s *Server) listItemPublishBatches(w http.ResponseWriter, r *http.Request) {
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	// limit 用于本次流程后续判断的上限
	limit := atoiDefault(r.URL.Query().Get("limit"), 20)
	// result、err 用于本次流程后续判断的result、err
	batches, err := s.itemBatchManagementApplication().ListBatches(r.Context(), sess.UserID, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取批量任务失败")
		return
	}
	// result 保存应用批次转换后的兼容 HTTP 响应。
	result := make([]itemPublishBatchResponse, 0, len(batches))
	// batch 表示当前待转换的应用批次摘要。
	for _, batch := range batches {
		result = append(result, publishBatchApplicationToResponse(batch, nil))
	}
	writeJSON(w, http.StatusOK, itemPublishBatchListResponse{Batches: result})
}

// getItemPublishBatch 返回指定批次及其发布明细。
func (s *Server) getItemPublishBatch(w http.ResponseWriter, r *http.Request) {
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	// batchID 用于本次流程后续判断的批次ID
	batchID := chi.URLParam(r, "batch_id")
	// result、err 用于本次流程后续判断的result、err
	details, err := s.itemBatchManagementApplication().GetBatch(r.Context(), sess.UserID, batchID)
	if err != nil {
		if errors.Is(err, itemapp.ErrBatchNotFound) {
			writeErr(w, http.StatusNotFound, "批量任务不存在")
		} else {
			writeErr(w, http.StatusInternalServerError, "读取任务明细失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, publishBatchApplicationToResponse(details.Batch, details.Rows))
}

// cancelItemPublishBatch 请求取消指定批次并通知运行中的 worker。
func (s *Server) cancelItemPublishBatch(w http.ResponseWriter, r *http.Request) {
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	// batchID 用于本次流程后续判断的批次ID
	batchID := chi.URLParam(r, "batch_id")
	// status、err 用于本次流程后续判断的status、err
	status, err := s.itemBatchManagementApplication().CancelBatch(r.Context(), sess.UserID, batchID)
	if err != nil {
		if errors.Is(err, itemapp.ErrBatchNotFound) {
			writeErr(w, http.StatusNotFound, "批量任务不存在")
		} else if errors.Is(err, itemapp.ErrBatchConflict) {
			writeErr(w, http.StatusConflict, "任务状态刚刚发生变化，请重试")
		} else {
			writeErr(w, http.StatusInternalServerError, "取消任务失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, batchCancelResponse{Success: true, Status: status})
}

// deleteItemPublishBatch 删除已结束的批次及其上传目录。
func (s *Server) deleteItemPublishBatch(w http.ResponseWriter, r *http.Request) {
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	// batchID 用于本次流程后续判断的批次ID
	batchID := chi.URLParam(r, "batch_id")
	// uploadDir、err 用于本次流程后续判断的uploadDir、err
	err := s.itemBatchManagementApplication().DeleteBatch(r.Context(), sess.UserID, batchID)
	if err != nil {
		if errors.Is(err, itemapp.ErrBatchNotFound) {
			writeErr(w, http.StatusNotFound, "批量任务不存在")
		} else if errors.Is(err, itemapp.ErrBatchConflict) {
			writeErr(w, http.StatusConflict, "运行中的任务不能删除，请先取消")
		} else {
			writeErr(w, http.StatusInternalServerError, "删除批量任务失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// retryFailedItemPublishBatch 重置失败明细并启动批次重试 worker。
func (s *Server) retryFailedItemPublishBatch(w http.ResponseWriter, r *http.Request) {
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	// batchID 用于本次流程后续判断的批次ID
	batchID := chi.URLParam(r, "batch_id")
	// startedID、err 用于本次流程后续判断的startedID、err
	startedID, err := s.itemBatchManagementApplication().RetryFailedBatch(r.Context(), sess.UserID, batchID, publishBatchLease)
	if err != nil {
		if errors.Is(err, itemapp.ErrBatchNotFound) {
			writeErr(w, http.StatusNotFound, "批量任务不存在")
		} else if errors.Is(err, itemapp.ErrBatchConflict) {
			writeErr(w, http.StatusConflict, "任务正在运行，不能重复重试")
		} else if errors.Is(err, itemapp.ErrBatchNoRows) {
			writeErr(w, http.StatusBadRequest, "没有可重试的失败项")
		} else {
			writeErr(w, http.StatusInternalServerError, "启动重试失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, batchIDResponse{Success: true, BatchID: startedID})
}

// downloadItemPublishBatchResult 封装download商品发布批次结果业务协调。
func (s *Server) downloadItemPublishBatchResult(w http.ResponseWriter, r *http.Request) {
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	// batchID 用于本次流程后续判断的批次ID
	batchID := chi.URLParam(r, "batch_id")
	// details、err 保存应用服务返回的批次及明细。
	details, err := s.itemBatchManagementApplication().GetBatch(r.Context(), sess.UserID, batchID)
	if err != nil {
		if errors.Is(err, itemapp.ErrBatchNotFound) {
			writeErr(w, http.StatusNotFound, "批量任务不存在")
		} else {
			writeErr(w, http.StatusInternalServerError, "读取任务明细失败")
		}
		return
	}
	// rows 保存应用层批次明细，后续只负责转换为下载 DTO。
	rows := details.Rows
	// buf 用于本次流程后续判断的buf
	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF")
	// cw 用于本次流程后续判断的cw
	cw := csv.NewWriter(&buf)
	_ = cw.Write([]string{"行号", "状态", "账号ID", "标题", "价格", "库存", "默认类目ID", "默认类目名称", "商品ID", "商品URL", "错误原因"})
	// row 表示当前导出的批量明细行。
	for _, row := range rows {
		// category 用于本次流程后续判断的分类
		var category publishCategoryResponse
		_ = json.Unmarshal([]byte(row.CategoryJSON), &category)
		_ = cw.Write([]string{
			strconv.Itoa(row.RowNo), safeCSVCell(row.Status), safeCSVCell(row.CookieID), safeCSVCell(row.Title), safeCSVCell(row.Price),
			strconv.Itoa(row.Quantity), safeCSVCell(category.CatID), safeCSVCell(category.CatName),
			safeCSVCell(row.ItemID), safeCSVCell(row.ItemURL), safeCSVCell(row.ErrorMessage),
		})
	}
	cw.Flush()
	// filename 用于本次流程后续判断的filename
	filename := fmt.Sprintf("publish_result_%s.csv", details.Batch.ID)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(filename))
	_, _ = w.Write(buf.Bytes())
}

// safeCSVCell 防止用户可控内容被电子表格应用解释为公式。开头的单引号
// 在 Excel/LibreOffice 中作为文本标记，不改变导出的可见内容。
// safeCSVCell 封装safeCSVCell业务协调。
func safeCSVCell(value string) string {
	// trimmed 用于本次流程后续判断的trimmed
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed == "" {
		return value
	}
	switch trimmed[0] {
	case '=', '+', '-', '@':
		return "'" + value
	default:
		return value
	}
}

// publishBatchFailure 封装发布批次Failure业务协调。
func publishBatchFailure(err error, batchStatus string) (string, string) {
	// message 用于本次流程后续判断的消息
	message := err.Error()
	// failureKind 用于本次流程后续判断的failure类型
	failureKind := "publish"
	// postErr 用于本次流程后续判断的postErr
	var postErr *postPublishError
	// uncertainErr 用于本次流程后续判断的uncertainErr
	var uncertainErr *uncertainRemotePublishError
	// appUncertainErr 保存应用批量端口返回的远端不确定错误。
	var appUncertainErr *itemapp.UncertainRemotePublishError
	// appPostErr 保存应用批量端口返回的后置处理错误。
	var appPostErr *itemapp.PostPublishError
	if errors.As(err, &uncertainErr) || errors.As(err, &appUncertainErr) {
		failureKind = "uncertain_remote"
		message += "；远端结果未能可靠落库，禁止自动重试，请人工核对闲鱼商品列表"
	} else if errors.As(err, &postErr) || errors.As(err, &appPostErr) {
		failureKind = "post_publish"
	}
	if batchStatus == "canceled" || batchStatus == "canceling" {
		if failureKind == "uncertain_remote" {
			message = "任务已取消；" + message
		} else {
			message = "任务已取消"
		}
	}
	return message, failureKind
}

// finalPublishBatchStatus 根据应用批次统计返回兼容的最终状态。
func finalPublishBatchStatus(batch *itemapp.BatchInfo) string {
	if batch == nil {
		return "failed"
	}
	if batch.FailedCount > 0 {
		if batch.SuccessCount > 0 {
			return "partially_failed"
		}
		return "failed"
	}
	return "completed"
}

// publishBatchApplicationToResponse 将应用批次模型映射为现有 HTTP 响应 DTO。
func publishBatchApplicationToResponse(batch itemapp.BatchInfo, rows []itemapp.BatchRow) itemPublishBatchResponse {
	// locationJSON 保存批次发货地配置；空值时保持旧响应的空对象语义。
	locationJSON := strings.TrimSpace(batch.LocationJSON)
	if locationJSON == "" {
		locationJSON = "{}"
	}
	// outRows 保存转换后的批次明细响应。
	outRows := make([]itemPublishBatchRowResponse, 0, len(rows))
	// pending、running 保存当前批次明细状态计数。
	pending, running := 0, 0
	// row 表示当前待转换的应用批次明细。
	for _, row := range rows {
		if row.Status == "pending" {
			pending++
		}
		if row.Status == "running" {
			running++
		}
		// refs 保存当前明细的图片路径列表。
		var refs []string
		_ = json.Unmarshal([]byte(row.ImagesJSON), &refs)
		// category 保存当前明细的类目配置。
		var category publishCategoryResponse
		_ = json.Unmarshal([]byte(row.CategoryJSON), &category)
		// automationCfg 保存当前明细的发布后自动化配置。
		var automationCfg publishAutomationConfig
		_ = json.Unmarshal([]byte(row.AutomationJSON), &automationCfg)
		outRows = append(outRows, itemPublishBatchRowResponse{
			ID: row.ID, RowNo: row.RowNo, CookieID: row.CookieID, Title: row.Title,
			Price: row.Price, Quantity: row.Quantity, Images: refs,
			Category: category, Automation: automationCfg, Status: row.Status,
			ItemID: row.ItemID, ItemURL: row.ItemURL, ErrorMessage: row.ErrorMessage,
			FailureKind: row.FailureKind,
		})
	}
	// retryable 保存允许用户重新发起的平台失败明细数量。
	retryable := 0
	// row 表示当前用于统计可重试数量的应用批次明细。
	for _, row := range rows {
		if row.Status == "failed" && row.FailureKind != "validation" && row.FailureKind != "uncertain_remote" {
			retryable++
		}
	}
	return itemPublishBatchResponse{
		ID: batch.ID, Status: batch.Status, Filename: batch.Filename,
		Total: batch.TotalCount, Success: batch.SuccessCount, Failed: batch.FailedCount,
		Pending: pending, Running: running, Retryable: retryable, Rows: outRows,
		Location: json.RawMessage(locationJSON), PublishIntervalSeconds: batch.PublishIntervalSeconds, CreatedAt: batch.CreatedAt, UpdatedAt: batch.UpdatedAt,
	}
}

// cleanupExpiredPublishUploads 封装cleanupExpired发布Uploads业务协调。
func (s *Server) cleanupExpiredPublishUploads(ctx context.Context) {
	// err 表示过期上传目录清理结果；清理失败不影响当前预检请求。
	if err := s.itemBatchManagementApplication().CleanupExpiredUploads(ctx, time.Now().UTC(), 100); err != nil && s.Logger != nil {
		s.Logger.Warn("清理过期批量发布上传目录失败", "err", err)
	}
}

// cookieOwnedByUser 判断指定账号是否属于用户，不读取或解密 Cookie 明文。
func (s *Server) cookieOwnedByUser(ctx context.Context, userID int64, cookieID string) bool {
	// service 提供不读取凭证的账号归属查询能力。
	service := s.accountSummaryApplication()
	if service == nil {
		return false
	}
	// owned、err 表示账号摘要服务返回的归属结论及基础设施错误。
	owned, err := service.ExistsOwned(ctx, userID, cookieID)
	return err == nil && owned
}

// cookieValueForUser 读取指定用户拥有的单个账号 Cookie 明文。
func (s *Server) cookieValueForUser(ctx context.Context, userID int64, cookieID string) (string, error) {
	// service 提供消费者定义的平台凭证读取端口，明文只在当前平台请求边界短暂存在。
	service := s.platformCredentialApplication()
	if service == nil {
		return "", errors.New("平台凭证读取服务未初始化")
	}
	// value 和 err 保存已通过归属复核的 Cookie 明文及读取错误；value 不进入 HTTP 响应。
	value, err := service.LoadOwnedValue(ctx, userID, cookieID)
	if err != nil {
		if errors.Is(err, accountapp.ErrCredentialNotFound) || errors.Is(err, accountapp.ErrForbidden) || errors.Is(err, accountapp.ErrCredentialEmpty) {
			return "", errors.New("账号不存在或 Cookie 为空")
		}
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", errors.New("账号不存在或 Cookie 为空")
	}
	return value, nil
}

// cardOwnedByUser 判断指定卡券组是否属于用户。
func (s *Server) cardOwnedByUser(ctx context.Context, userID int64, cardID int64) bool {
	// exists 和 err 表示卡券应用服务返回的所有权结果及错误。
	exists, err := s.cardsApplication().ExistsOwned(ctx, userID, cardID)
	return err == nil && exists
}

// publishUploadRoot 返回发布图片上传文件的根目录。
func (s *Server) publishUploadRoot() string {
	return defaultPublishUploadRoot()
}

// defaultPublishUploadRoot 返回环境变量指定或默认的发布上传目录。
func defaultPublishUploadRoot() string {
	// v 是去除首尾空白后的上传目录环境变量值。
	if v := strings.TrimSpace(os.Getenv("XIANYU_UPLOAD_DIR")); v != "" {
		return v
	}
	return filepath.Join("data", "uploads")
}

// parseLooseBool 将常见的真值文本转换为布尔值。
func parseLooseBool(raw string) bool {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch raw {
	case "1", "true", "yes", "y", "on", "是", "开启", "启用":
		return true
	default:
		return false
	}
}

// atoiPublishDefault 将数字文本转换为整数，无法解析时返回给定默认值。
func atoiPublishDefault(raw string, def int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def
	}
	// f 和 err 分别表示解析出的浮点数及其错误。
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		return int(f)
	}
	return def
}

// parsePublishIntervalSeconds 解析批量铺货的最终发布最小间隔，空值使用五秒默认值。
func parsePublishIntervalSeconds(raw string) (int, error) {
	// normalized 保存去除空白后的用户输入；空值保持兼容默认五秒。
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		return 5, nil
	}
	// interval、err 保存严格解析后的秒数与输入错误。
	interval, err := strconv.Atoi(normalized)
	if err != nil {
		return 0, errors.New("发布间隔必须是整数秒")
	}
	if interval < 1 || interval > 3600 {
		return 0, errors.New("发布间隔必须是 1 到 3600 秒")
	}
	return interval, nil
}

// firstNonEmpty 返回参数中第一个非空白字符串。
func firstNonEmpty(values ...string) string {
	// v 是当前遍历到的候选字符串。
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// serverBatchPublisher 将 Server 的商品发布细节适配为应用层批量发布端口。
type serverBatchPublisher struct {
	// remotePort 保存已装配的平台与凭证端口，避免 worker 再进入 Server 远端逻辑。
	remotePort itemapp.BatchPublishPort
	// localService 保存已装配的本地商品、自动化规则和成功检查点收口服务。
	localService *itemapp.BatchLocalPublishService
}

// PublishRow 调用批量远端端口并收口本地结果，应用层不接触数据库或 MTOP 类型。
func (p serverBatchPublisher) PublishRow(ctx context.Context, userID int64, row itemapp.BatchRow, workerToken string, beforePublish func(context.Context) error) error {
	if p.remotePort == nil {
		return errors.New("批量发布端口未装配")
	}
	if p.localService == nil {
		return errors.New("批量发布本地收口端口未装配")
	}
	// outcome、err 保存远端发布结果及适配器错误。
	outcome, err := p.remotePort.PublishRemoteRow(ctx, userID, row, workerToken, beforePublish)
	if err != nil {
		// uncertainErr 表示远端调用已经发生但结果无法可靠确认。
		var uncertainErr *itemapp.UncertainRemotePublishError
		if errors.As(err, &uncertainErr) {
			return &uncertainRemotePublishError{err: err}
		}
		return err
	}
	if outcome.Result == nil {
		return errors.New("发布商品接口未返回结果")
	}
	if outcome.ResponseCookieErr != nil {
		return &itemapp.PostPublishError{Err: outcome.ResponseCookieErr}
	}
	if ctx.Err() != nil {
		return &itemapp.PostPublishError{Err: ctx.Err()}
	}
	return p.localService.Complete(ctx, userID, row, workerToken, outcome.Result)
}

// newItemBatchRunnerApplication 创建批量发布应用层 worker 编排器。
func newItemBatchRunnerApplication(server *Server, remotePort itemapp.BatchPublishPort, localService *itemapp.BatchLocalPublishService) (*itemapp.BatchRunner, error) {
	if server == nil || remotePort == nil || localService == nil {
		return nil, errors.New("批量发布端口未装配")
	}
	// options 配置批量 worker 的租约、间隔和平台错误语义。
	options := itemapp.BatchRunOptions{
		LeaseDuration:    publishBatchLease,
		IsSessionExpired: adapter.IsSessionExpiredError,
		ClassifyFailure:  publishBatchFailure,
	}
	return itemapp.NewBatchRunner(server.itemDependencies.NewItemBatchRepository(), serverBatchPublisher{remotePort: remotePort, localService: localService}, options)
}

// publishBatchLocation 保存批量发布请求中的发货地字段，不让平台 DTO 进入 HTTP 层。
type publishBatchLocation struct {
	Area       string  `json:"area"`
	City       string  `json:"city"`
	DivisionID string  `json:"division_id"`
	Longitude  float64 `json:"longitude"`
	Latitude   float64 `json:"latitude"`
	POIID      string  `json:"poi_id"`
	POIName    string  `json:"poi_name"`
	Province   string  `json:"province"`
}
