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

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/auth"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// maxPublishBatchRows 保存max发布批次Rows，供当前处理流程使用
const (
	maxPublishBatchRows    = 50
	publishBatchLease      = 5 * time.Minute
	publishBatchJobTimeout = 2 * time.Hour
)

// postPublishError 保存post发布错误，供当前处理流程使用
type postPublishError struct{ err error }

// Error 负责错误相关处理。
func (e *postPublishError) Error() string { return e.err.Error() }

// Unwrap 负责Unwrap相关处理。
func (e *postPublishError) Unwrap() error { return e.err }

// uncertainRemotePublishError 保存uncertainRemote发布错误，供当前处理流程使用
type uncertainRemotePublishError struct{ err error }

// Error 负责错误相关处理。
func (e *uncertainRemotePublishError) Error() string { return e.err.Error() }

// Unwrap 负责Unwrap相关处理。
func (e *uncertainRemotePublishError) Unwrap() error { return e.err }

// publishBatchPreviewRow 保存发布批次PreviewRow，供当前处理流程使用
type publishBatchPreviewRow struct {
	RowNo      int                     `json:"row_no"`
	Valid      bool                    `json:"valid"`
	Errors     []string                `json:"errors,omitempty"`
	CookieID   string                  `json:"cookie_id"`
	Title      string                  `json:"title"`
	Price      string                  `json:"price"`
	Quantity   int                     `json:"quantity"`
	Images     []string                `json:"images"`
	Category   mtop.PublishCategory    `json:"category"`
	Automation publishAutomationConfig `json:"automation"`
}

// publishBatchParsedRow 保存发布批次解析结果Row，供当前处理流程使用
type publishBatchParsedRow struct {
	RowNo         int
	CookieID      string
	Title         string
	Description   string
	Price         string
	OriginalPrice string
	Quantity      int
	PostageMode   string
	Postage       string
	Images        []string
	Category      mtop.PublishCategory
	Automation    publishAutomationConfig
	Errors        []string
	Raw           map[string]any
}

// publishAutomationConfig 保存发布自动化配置，供当前处理流程使用
type publishAutomationConfig struct {
	PaidDelivery  publishCardAutomation   `json:"paid_delivery"`
	ReviewGift    publishCardAutomation   `json:"review_gift"`
	ReviewRequest publishReviewRequestCfg `json:"review_request"`
}

// publishCardAutomation 保存发布卡密自动化，供当前处理流程使用
type publishCardAutomation struct {
	Enabled    bool                `json:"enabled"`
	Actions    []publishCardAction `json:"actions"`
	ParseError string              `json:"-"`
}

// publishCardAction 保存发布卡密动作，供当前处理流程使用
type publishCardAction struct {
	CardID        int64 `json:"card_id"`
	DeliveryCount int   `json:"delivery_count"`
	DelaySeconds  int   `json:"delay_seconds"`
}

// publishReviewRequestCfg 保存发布Review请求Cfg，供当前处理流程使用
type publishReviewRequestCfg struct {
	Enabled           bool   `json:"enabled"`
	AfterShippedHours int    `json:"after_shipped_hours"`
	Message           string `json:"message"`
	MaxAttempts       int    `json:"max_attempts"`
	DelaySeconds      int    `json:"delay_seconds"`
}

// publishCategoryRecommender 保存发布分类Recommender，供当前处理流程使用
type publishCategoryRecommender interface {
	RecommendPublishCategory(ctx context.Context, cookiesStr, keyword string) (mtop.PublishCategory, string, error)
}

// recommendItemPublishCategory 解析类目关键词并返回平台推荐类目。
func (s *Server) recommendItemPublishCategory(w http.ResponseWriter, r *http.Request) {
	// req 保存req，供当前处理流程使用
	var req struct {
		CookieID string `json:"cookie_id"`
		Keyword  string `json:"keyword"`
	}
	if // err 保存err，供当前处理流程使用
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
	// userID、ok 保存用户ID、ok，供当前处理流程使用
	_, userID, ok := s.cookieForCurrentUser(w, r, req.CookieID)
	if !ok {
		return
	}
	// category、callErr 保存category、callErr，供当前处理流程使用
	category, callErr := s.itemPublishApplication().RecommendCategory(r.Context(), userID, req.CookieID, req.Keyword)
	if callErr != nil {
		if strings.Contains(callErr.Error(), "当前 MTOP 客户端不支持") {
			writeErr(w, http.StatusNotImplemented, callErr.Error())
			return
		}
		if strings.Contains(callErr.Error(), "账号凭证已变化") {
			writeErr(w, http.StatusConflict, callErr.Error())
			return
		}
		if strings.Contains(callErr.Error(), "保存账号登录状态") {
			writeErr(w, http.StatusInternalServerError, callErr.Error())
			return
		}
		if errors.Is(callErr, mtop.ErrPublishCategoryUnrecognized) {
			writeErr(w, http.StatusNotFound, "没有匹配到可发布类目，请换一个关键词")
			return
		}
		writeErr(w, http.StatusBadGateway, callErr.Error())
		return
	}
	writeJSON(w, http.StatusOK, categoryRecommendationResponse{Success: true, Category: category})
}

// previewItemPublishBatch 处理表格上传、图片归档和批量发布预检。
func (s *Server) previewItemPublishBatch(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	s.cleanupExpiredPublishUploads(r.Context())
	// 表格最大 20 MiB，图片压缩包最大 200 MiB，额外预留 multipart 元数据空间。
	r.Body = http.MaxBytesReader(w, r.Body, maxItemPublishBatchBytes)
	// #nosec G120 -- 请求体已由 MaxBytesReader 限制。
	if err := r.ParseMultipartForm(maxItemPublishBatchParseBytes); err != nil {
		writeErr(w, http.StatusBadRequest, "解析上传文件失败")
		return
	}
	// defaultCookieID 保存default登录凭证ID，供当前处理流程使用
	defaultCookieID := strings.TrimSpace(r.FormValue("default_cookie_id"))
	if defaultCookieID == "" {
		writeErr(w, http.StatusBadRequest, "请选择默认发布账号")
		return
	}
	if !s.cookieOwnedByUser(r.Context(), sess.UserID, defaultCookieID) {
		writeErr(w, http.StatusForbidden, "默认账号不属于当前用户")
		return
	}
	// fallbackCategory 保存fallback分类，供当前处理流程使用
	fallbackCategory := mtop.PublishCategory{
		CatID:        strings.TrimSpace(r.FormValue("fallback_category_id")),
		CatName:      strings.TrimSpace(r.FormValue("fallback_category_name")),
		ChannelCatID: strings.TrimSpace(r.FormValue("fallback_channel_category_id")),
		TBCatID:      strings.TrimSpace(r.FormValue("fallback_tb_category_id")),
	}
	// batchLocation 保存批次地址，供当前处理流程使用
	var batchLocation mtop.PublishLocation
	// locationJSON 保存地址JSON，供当前处理流程使用
	locationJSON := strings.TrimSpace(r.FormValue("location"))
	if locationJSON != "" {
		if json.Unmarshal([]byte(locationJSON), &batchLocation) != nil {
			writeErr(w, http.StatusBadRequest, "发货地格式错误，请重新定位")
			return
		}
	}
	// hasDefaultCategory 保存hasDefault分类，供当前处理流程使用
	hasDefaultCategory := fallbackCategory.CatID != "" || fallbackCategory.CatName != "" || fallbackCategory.ChannelCatID != "" || fallbackCategory.TBCatID != ""
	if hasDefaultCategory && (fallbackCategory.CatID == "" || fallbackCategory.CatName == "" || fallbackCategory.ChannelCatID == "") {
		writeErr(w, http.StatusBadRequest, "默认类目信息不完整，请重新通过关键词获取")
		return
	}
	// source、sourceHeader、err 保存source、sourceHeader、err，供当前处理流程使用
	source, sourceHeader, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "缺少商品表格文件")
		return
	}
	defer source.Close()
	// sourceBytes、tooLarge、err 保存sourceBytes、tooLarge、err，供当前处理流程使用
	sourceBytes, tooLarge, err := readLimitedBytes(source, 20<<20)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "读取商品表格失败")
		return
	}
	if tooLarge {
		writeErr(w, http.StatusBadRequest, "商品表格不能超过 20 MiB")
		return
	}
	// batchID 保存批次ID，供当前处理流程使用
	batchID := "batch_" + randomHex(12)
	// uploadDir 保存uploadDir，供当前处理流程使用
	uploadDir := filepath.Join(s.publishUploadRoot(), "publish_batches", batchID)
	if // err 保存err，供当前处理流程使用
	err := os.MkdirAll(uploadDir, 0o750); err != nil {
		writeErr(w, http.StatusInternalServerError, "创建上传目录失败")
		return
	}
	// keepUpload 保存keepUpload，供当前处理流程使用
	keepUpload := false
	defer func() {
		if !keepUpload {
			_ = os.RemoveAll(uploadDir)
		}
	}()
	// sourceName 保存source名称，供当前处理流程使用
	sourceName := safeBaseName(sourceHeader.Filename)
	if sourceName == "" {
		sourceName = "products.csv"
	}
	if // err 保存err，供当前处理流程使用
	err := writeFileWithinRoot(uploadDir, sourceName, sourceBytes); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存商品表格失败")
		return
	}

	if // zipFile、zipHeader、err 保存zipFile、zipHeader、err，供当前处理流程使用
	zipFile, zipHeader, err := r.FormFile("images_zip"); err == nil {
		defer zipFile.Close()
		// zipBytes、tooLarge、err 保存zipBytes、tooLarge、err，供当前处理流程使用
		zipBytes, tooLarge, err := readLimitedBytes(zipFile, 200<<20)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "读取图片 zip 失败")
			return
		}
		if tooLarge {
			writeErr(w, http.StatusBadRequest, "图片 zip 不能超过 200 MiB")
			return
		}
		// zipName 保存zip名称，供当前处理流程使用
		zipName := safeBaseName(zipHeader.Filename)
		if zipName == "" {
			zipName = "images.zip"
		}
		if // err 保存err，供当前处理流程使用
		err := writeFileWithinRoot(uploadDir, zipName, zipBytes); err != nil {
			writeErr(w, http.StatusInternalServerError, "保存图片 zip 失败")
			return
		}
		if // err 保存err，供当前处理流程使用
		err := extractPublishImagesZip(zipBytes, uploadDir); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	// maps、err 保存maps、err，供当前处理流程使用
	maps, err := parsePublishSheetBytesWithLimit(sourceBytes, sourceName, maxPublishBatchRows)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(maps) > maxPublishBatchRows {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("单个批次最多支持 %d 条商品", maxPublishBatchRows))
		return
	}
	// parsed 保存解析结果，供当前处理流程使用
	parsed := s.parsePublishRows(r.Context(), sess.UserID, defaultCookieID, uploadDir, fallbackCategory, maps)
	// preview、err 保存preview、err，供当前处理流程使用
	preview, err := s.itemPublishApplication().PersistPreview(r.Context(), itemPublishPreviewInput{
		UserID: sess.UserID, BatchID: batchID, DefaultCookieID: defaultCookieID,
		Filename: sourceName, UploadDir: uploadDir, Location: batchLocation, Rows: parsed,
	})
	if err != nil {
		if errors.Is(err, errItemPublishBatchNoRows) {
			writeErr(w, http.StatusBadRequest, "表格中没有有效数据行")
		} else {
			writeErr(w, http.StatusInternalServerError, "保存预检结果失败")
		}
		return
	}
	keepUpload = true
	writeJSON(w, http.StatusOK, preview)
	/*
		rows := make([]db.ItemPublishBatchRow, 0, len(parsed))
		previewRows := make([]publishBatchPreviewRow, 0, len(parsed))
		valid, invalid := 0, 0
		for _, p := range parsed {
			isValid := len(p.Errors) == 0
			if isValid {
				valid++
			} else {
				invalid++
			}
			imagesJSON, _ := json.Marshal(p.Images)
			categoryJSON, _ := json.Marshal(p.Category)
			automationJSON, _ := json.Marshal(p.Automation)
			rawJSON, _ := json.Marshal(p.Raw)
			status := "pending"
			errMsg := ""
			if !isValid {
				status = "failed"
				errMsg = strings.Join(p.Errors, "；")
			}
			rows = append(rows, db.ItemPublishBatchRow{
				RowNo:          p.RowNo,
				CookieID:       p.CookieID,
				Title:          p.Title,
				Description:    p.Description,
				Price:          p.Price,
				OriginalPrice:  p.OriginalPrice,
				Quantity:       p.Quantity,
				PostageMode:    p.PostageMode,
				Postage:        p.Postage,
				ImagesJSON:     string(imagesJSON),
				CategoryJSON:   string(categoryJSON),
				AutomationJSON: string(automationJSON),
				Status:         status,
				ErrorMessage:   errMsg,
				FailureKind:    map[bool]string{true: "", false: "validation"}[isValid],
				RawJSON:        string(rawJSON),
			})
			previewRows = append(previewRows, publishBatchPreviewRow{
				RowNo:      p.RowNo,
				Valid:      isValid,
				Errors:     p.Errors,
				CookieID:   p.CookieID,
				Title:      p.Title,
				Price:      p.Price,
				Quantity:   p.Quantity,
				Images:     p.Images,
				Category:   p.Category,
				Automation: p.Automation,
			})
		}
		if len(rows) == 0 {
			writeErr(w, http.StatusBadRequest, "表格中没有有效数据行")
			return
		}
		if err := s.itemPublishRepositoryForServer().CreateBatch(r.Context(), &db.ItemPublishBatch{
			ID:              batchID,
			UserID:          sess.UserID,
			DefaultCookieID: defaultCookieID,
			Filename:        sourceName,
			UploadDir:       uploadDir,
			LocationJSON:    string(locationBytes),
			Status:          "preview",
		}, rows); err != nil {
			writeErr(w, http.StatusInternalServerError, "保存预检结果失败")
			return
		}
		keepUpload = true
		_ = s.itemPublishRepositoryForServer().RecountBatch(r.Context(), batchID)
		// 预检响应保留逐行错误，客户端据此决定是否允许启动发布。
		// preview_id 是后续启动、轮询和放弃预检的稳定标识。
		// total、valid 和 invalid 继续使用旧统计口径。
		// rows 保留类目和自动化配置，避免前端重复解析上传表格。
		// 预检成功不代表远端商品已经发布。
		// 远端发布失败仍由批次明细状态表达。
		// 该 DTO 不改变上传目录清理和批次持久化时序。
		writeJSON(w, http.StatusOK, itemPublishBatchPreviewResponse{Success: true, PreviewID: batchID, Total: len(rows), Valid: valid, Invalid: invalid, Rows: previewRows})*/
}

// startItemPublishBatch 启动指定批次的后台发布 worker。
func (s *Server) startItemPublishBatch(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// req 保存req，供当前处理流程使用
	var req struct {
		PreviewID string `json:"preview_id"`
		BatchID   string `json:"batch_id"`
	}
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// batchID 保存批次ID，供当前处理流程使用
	batchID := strings.TrimSpace(req.PreviewID)
	if batchID == "" {
		batchID = strings.TrimSpace(req.BatchID)
	}
	if batchID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 preview_id")
		return
	}
	// startedID、err 保存startedID、err，供当前处理流程使用
	startedID, err := s.itemPublishApplication().StartBatch(r.Context(), sess.UserID, batchID)
	if err != nil {
		switch {
		case errors.Is(err, errItemPublishBatchNotFound):
			writeErr(w, http.StatusNotFound, "批量任务不存在")
		case errors.Is(err, errItemPublishBatchConflict):
			writeErr(w, http.StatusConflict, "任务正在由其他 worker 运行")
		case errors.Is(err, errItemPublishBatchInvalidState):
			writeErr(w, http.StatusBadRequest, "当前任务状态不能开始发布")
		case errors.Is(err, errItemPublishBatchNoRows):
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
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// limit 保存上限，供当前处理流程使用
	limit := atoiDefault(r.URL.Query().Get("limit"), 20)
	// result、err 保存result、err，供当前处理流程使用
	result, err := s.itemPublishApplication().ListBatches(r.Context(), sess.UserID, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取批量任务失败")
		return
	}
	writeJSON(w, http.StatusOK, itemPublishBatchListResponse{Batches: result})
}

// getItemPublishBatch 返回指定批次及其发布明细。
func (s *Server) getItemPublishBatch(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// batchID 保存批次ID，供当前处理流程使用
	batchID := chi.URLParam(r, "batch_id")
	// result、err 保存result、err，供当前处理流程使用
	result, err := s.itemPublishApplication().GetBatch(r.Context(), sess.UserID, batchID)
	if err != nil {
		if errors.Is(err, errItemPublishBatchNotFound) {
			writeErr(w, http.StatusNotFound, "批量任务不存在")
		} else {
			writeErr(w, http.StatusInternalServerError, "读取任务明细失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// cancelItemPublishBatch 请求取消指定批次并通知运行中的 worker。
func (s *Server) cancelItemPublishBatch(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// batchID 保存批次ID，供当前处理流程使用
	batchID := chi.URLParam(r, "batch_id")
	// status、err 保存status、err，供当前处理流程使用
	status, err := s.itemPublishApplication().CancelBatch(r.Context(), sess.UserID, batchID)
	if err != nil {
		if errors.Is(err, errItemPublishBatchNotFound) {
			writeErr(w, http.StatusNotFound, "批量任务不存在")
		} else if errors.Is(err, errItemPublishBatchConflict) {
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
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// batchID 保存批次ID，供当前处理流程使用
	batchID := chi.URLParam(r, "batch_id")
	// uploadDir、err 保存uploadDir、err，供当前处理流程使用
	uploadDir, err := s.itemPublishApplication().DeleteBatch(r.Context(), sess.UserID, batchID)
	if err != nil {
		if errors.Is(err, errItemPublishBatchNotFound) {
			writeErr(w, http.StatusNotFound, "批量任务不存在")
		} else if errors.Is(err, errItemPublishBatchConflict) {
			writeErr(w, http.StatusConflict, "运行中的任务不能删除，请先取消")
		} else {
			writeErr(w, http.StatusInternalServerError, "删除批量任务失败")
		}
		return
	}
	if strings.TrimSpace(uploadDir) != "" {
		_ = os.RemoveAll(uploadDir)
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// retryFailedItemPublishBatch 重置失败明细并启动批次重试 worker。
func (s *Server) retryFailedItemPublishBatch(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// batchID 保存批次ID，供当前处理流程使用
	batchID := chi.URLParam(r, "batch_id")
	// startedID、err 保存startedID、err，供当前处理流程使用
	startedID, err := s.itemPublishApplication().RetryFailedBatch(r.Context(), sess.UserID, batchID)
	if err != nil {
		if errors.Is(err, errItemPublishBatchNotFound) {
			writeErr(w, http.StatusNotFound, "批量任务不存在")
		} else if errors.Is(err, errItemPublishBatchConflict) {
			writeErr(w, http.StatusConflict, "任务正在运行，不能重复重试")
		} else if errors.Is(err, errItemPublishBatchNoRows) {
			writeErr(w, http.StatusBadRequest, "没有可重试的失败项")
		} else {
			writeErr(w, http.StatusInternalServerError, "启动重试失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, batchIDResponse{Success: true, BatchID: startedID})
}

// startPublishBatchWorker 负责开始发布批次工作器相关处理。
func (s *Server) startPublishBatchWorker(parent context.Context, userID int64, batchID, workerToken string) {
	// workerDone 保存工作器Done，供当前处理流程使用
	workerDone := s.beginWorker()
	// #nosec G118 -- worker 由超时和 Server 根 context 共同约束。
	go func() {
		defer workerDone()
		// jobCtx、cancel 保存jobCtx、cancel，供当前处理流程使用
		jobCtx, cancel := context.WithTimeout(parent, publishBatchJobTimeout)
		s.registerPublishBatchCancel(batchID, workerToken, cancel)
		defer cancel()
		defer s.unregisterPublishBatchCancel(batchID, workerToken)
		// runner 负责批量发布的租约、逐行失败记录和最终状态收口。
		if runErr := s.itemBatchRunnerApplication().Run(jobCtx, userID, batchID, workerToken, false); runErr != nil && s.Logger != nil {
			s.Logger.Warn("批量发布 worker 结束", "batch", batchID, "err", runErr)
		}
	}()
}

// RunPublishBatchRecovery 定期接管租约过期或明确因进程中断失败的批次。
func (s *Server) RunPublishBatchRecovery(ctx context.Context) {
	s.recoverPublishBatchesOnce(ctx)
	// ticker 保存ticker，供当前处理流程使用
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.recoverPublishBatchesOnce(ctx)
		}
	}
}

// StartPublishBatchRecovery 先登记生命周期，再启动恢复循环，避免关闭流程在
// goroutine 尚未调度时误判扫描器已经退出。
// StartPublishBatchRecovery 启动发布批次Recovery。
func (s *Server) StartPublishBatchRecovery(ctx context.Context) string {
	return s.startBackgroundTaskContext("批量发布恢复扫描器", ctx, func() {
		s.RunPublishBatchRecovery(ctx)
	})
}

// recoverPublishBatchesOnce 负责recover发布批次列表Once相关处理。
func (s *Server) recoverPublishBatchesOnce(ctx context.Context) {
	// batches、err 保存batches、err，供当前处理流程使用
	batches, err := s.itemPublishRepositoryForServer().RecoverableBatches(ctx, time.Now().UTC().Unix(), 20)
	if err != nil {
		s.Logger.Warn("扫描可恢复批量发布任务失败", "err", err)
		return
	}
	// batch 表示当前遍历过程中的批次
	for _, batch := range batches {
		if batch.Status == "canceling" {
			_, _ = s.itemPublishRepositoryForServer().FinalizeExpiredCancellation(ctx, batch.ID, time.Now().UTC().Unix())
			continue
		}
		// workerToken 保存工作器令牌，供当前处理流程使用
		workerToken := randomHex(16)
		// claimed、claimErr 保存claimed、claimErr，供当前处理流程使用
		claimed, claimErr := s.itemPublishRepositoryForServer().ClaimBatch(ctx, batch.ID, workerToken, time.Now().UTC().Add(publishBatchLease).Unix())
		if claimErr != nil || !claimed {
			continue
		}
		if // err 保存err，供当前处理流程使用
		err := s.itemPublishRepositoryForServer().ResetInterrupted(ctx, batch.ID); err != nil {
			s.failClaimedPublishBatch(batch.ID, workerToken)
			continue
		}
		_ = s.itemPublishRepositoryForServer().RecountBatch(ctx, batch.ID)
		// pending、pendingErr 保存pending、pendingErr，供当前处理流程使用
		pending, pendingErr := s.itemPublishRepositoryForServer().PendingRows(ctx, batch.ID, false)
		if pendingErr != nil || len(pending) == 0 {
			if pendingErr == nil {
				_, _, _ = s.itemPublishRepositoryForServer().FinalizeBatch(ctx, batch.ID, workerToken)
			} else {
				s.failClaimedPublishBatch(batch.ID, workerToken)
			}
			continue
		}
		s.startPublishBatchWorker(ctx, batch.UserID, batch.ID, workerToken)
	}
}

// failClaimedPublishBatch 负责failClaimed发布批次相关处理。
func (s *Server) failClaimedPublishBatch(batchID, workerToken string) {
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if // released、err 保存released、err，供当前处理流程使用
	released, err := s.itemPublishRepositoryForServer().FailClaimedBatch(ctx, batchID, workerToken); err != nil {
		s.Logger.Warn("释放异常批量发布任务失败", "batch", batchID, "err", err)
	} else if !released {
		s.Logger.Debug("批量发布任务租约已转移，无需释放", "batch", batchID)
	}
}

// downloadItemPublishBatchResult 负责download商品发布批次结果相关处理。
func (s *Server) downloadItemPublishBatchResult(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// batchID 保存批次ID，供当前处理流程使用
	batchID := chi.URLParam(r, "batch_id")
	// batch、err 保存batch、err，供当前处理流程使用
	batch, err := s.itemPublishRepositoryForServer().GetBatch(r.Context(), sess.UserID, batchID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "批量任务不存在")
		return
	}
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := s.itemPublishRepositoryForServer().ListBatchRows(r.Context(), batch.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取任务明细失败")
		return
	}
	// buf 保存buf，供当前处理流程使用
	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF")
	// cw 保存cw，供当前处理流程使用
	cw := csv.NewWriter(&buf)
	_ = cw.Write([]string{"行号", "状态", "账号ID", "标题", "价格", "库存", "默认类目ID", "默认类目名称", "商品ID", "商品URL", "错误原因"})
	// row 表示当前导出的批量明细行。
	for _, row := range rows {
		// category 保存分类，供当前处理流程使用
		var category mtop.PublishCategory
		_ = json.Unmarshal([]byte(row.CategoryJSON), &category)
		_ = cw.Write([]string{
			strconv.Itoa(row.RowNo), safeCSVCell(row.Status), safeCSVCell(row.CookieID), safeCSVCell(row.Title), safeCSVCell(row.Price),
			strconv.Itoa(row.Quantity), safeCSVCell(category.CatID), safeCSVCell(category.CatName),
			safeCSVCell(row.ItemID), safeCSVCell(row.ItemURL), safeCSVCell(row.ErrorMessage),
		})
	}
	cw.Flush()
	// filename 保存filename，供当前处理流程使用
	filename := fmt.Sprintf("publish_result_%s.csv", batch.ID)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(filename))
	_, _ = w.Write(buf.Bytes())
}

// safeCSVCell 防止用户可控内容被电子表格应用解释为公式。开头的单引号
// 在 Excel/LibreOffice 中作为文本标记，不改变导出的可见内容。
// safeCSVCell 负责safeCSVCell相关处理。
func safeCSVCell(value string) string {
	// trimmed 保存trimmed，供当前处理流程使用
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

// publishBatchFailure 负责发布批次Failure相关处理。
func publishBatchFailure(err error, batchStatus string) (string, string) {
	// message 保存消息，供当前处理流程使用
	message := err.Error()
	// failureKind 保存failure类型，供当前处理流程使用
	failureKind := "publish"
	// postErr 保存postErr，供当前处理流程使用
	var postErr *postPublishError
	// uncertainErr 保存uncertainErr，供当前处理流程使用
	var uncertainErr *uncertainRemotePublishError
	if errors.As(err, &uncertainErr) {
		failureKind = "uncertain_remote"
		message += "；远端结果未能可靠落库，禁止自动重试，请人工核对闲鱼商品列表"
	} else if errors.As(err, &postErr) {
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

// registerPublishBatchCancel 负责register发布批次取消相关处理。
func (s *Server) registerPublishBatchCancel(batchID, workerToken string, cancel context.CancelFunc) {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	if s.publishCancels == nil {
		s.publishCancels = make(map[string]publishBatchWorker)
	}
	if // old 保存old，供当前处理流程使用
	old := s.publishCancels[batchID]; old.cancel != nil {
		old.cancel()
	}
	s.publishCancels[batchID] = publishBatchWorker{token: workerToken, cancel: cancel}
}

// unregisterPublishBatchCancel 负责unregister发布批次取消相关处理。
func (s *Server) unregisterPublishBatchCancel(batchID, workerToken string) {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	if // current 保存current，供当前处理流程使用
	current := s.publishCancels[batchID]; current.token == workerToken {
		delete(s.publishCancels, batchID)
	}
}

// cancelPublishBatch 负责取消发布批次相关处理。
func (s *Server) cancelPublishBatch(batchID, workerToken string) bool {
	s.publishMu.Lock()
	// worker 保存工作器，供当前处理流程使用
	worker := s.publishCancels[batchID]
	s.publishMu.Unlock()
	if worker.token != workerToken || worker.cancel == nil {
		return false
	}
	worker.cancel()
	return true
}

// publishStatusContext 负责发布状态上下文相关处理。
func publishStatusContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent != nil && parent.Err() == nil {
		return context.WithTimeout(parent, 5*time.Second)
	}
	return context.WithTimeout(context.Background(), 5*time.Second)
}

// finalPublishBatchStatus 负责final发布批次状态相关处理。
func finalPublishBatchStatus(batch *db.ItemPublishBatch) string {
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

// publishBatchRow 负责发布批次Row相关处理。
func (s *Server) publishBatchRow(ctx context.Context, userID int64, client mtop.Client, row db.ItemPublishBatchRow, workerToken string) error {
	// batch、err 保存batch、err，供当前处理流程使用
	batch, err := s.itemPublishRepositoryForServer().GetBatch(ctx, userID, row.BatchID)
	if err != nil {
		return errors.New("批量任务不存在")
	}
	// location 保存地址，供当前处理流程使用
	var location mtop.PublishLocation
	// locationJSON 保存地址JSON，供当前处理流程使用
	locationJSON := strings.TrimSpace(batch.LocationJSON)
	if locationJSON == "" {
		locationJSON = "{}"
	}
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal([]byte(locationJSON), &location); err != nil {
		return errors.New("批量任务发货地配置损坏，请重新创建任务")
	}
	// selectedLocation 保存selected地址，供当前处理流程使用
	var selectedLocation *mtop.PublishLocation
	if strings.TrimSpace(location.DivisionID) != "" {
		selectedLocation = &location
	}
	// cookieValue、err 保存登录凭证Value、err，供当前处理流程使用
	cookieValue, err := s.cookieValueForUser(ctx, userID, row.CookieID)
	if err != nil {
		return err
	}
	// priceCents、err 保存priceCents、err，供当前处理流程使用
	priceCents, err := parseMoneyCents(row.Price)
	if err != nil || priceCents <= 0 {
		return errors.New("商品价格必须大于 0")
	}
	// origCents 保存origCents，供当前处理流程使用
	origCents, _ := parseMoneyCents(row.OriginalPrice)
	// postageCents 保存postageCents，供当前处理流程使用
	postageCents, _ := parseMoneyCents(row.Postage)
	// res 保存响应，供当前处理流程使用
	res := &mtop.PublishItemResult{ItemID: row.ItemID, ItemURL: row.ItemURL, Title: row.Title, PriceText: row.Price, Quantity: row.Quantity}
	// responseCookieErr 保存响应登录凭证Err，供当前处理流程使用
	var responseCookieErr error
	if row.ItemID == "" {
		// preferredCategory 保存preferred分类，供当前处理流程使用
		var preferredCategory *mtop.PublishCategory
		// rawCategory 保存原始分类，供当前处理流程使用
		rawCategory := strings.TrimSpace(row.CategoryJSON)
		if rawCategory != "" && rawCategory != "{}" {
			// configured 保存configured，供当前处理流程使用
			var configured mtop.PublishCategory
			if // err 保存err，供当前处理流程使用
			err := json.Unmarshal([]byte(rawCategory), &configured); err != nil {
				return errors.New("默认类目配置损坏，请重新创建批量任务")
			}
			if strings.TrimSpace(configured.CatID) != "" || strings.TrimSpace(configured.CatName) != "" || strings.TrimSpace(configured.ChannelCatID) != "" || strings.TrimSpace(configured.TBCatID) != "" {
				if strings.TrimSpace(configured.CatID) == "" || strings.TrimSpace(configured.CatName) == "" || strings.TrimSpace(configured.ChannelCatID) == "" {
					return errors.New("默认类目信息不完整，请重新创建批量任务")
				}
				preferredCategory = &configured
			}
		}
		// images、err 保存images、err，供当前处理流程使用
		images, err := loadBatchPublishImages(ctx, batch.UploadDir, row)
		if err != nil {
			return err
		}
		// markCtx、markCancel 保存markCtx、mark取消，供当前处理流程使用
		markCtx, markCancel := publishStatusContext(ctx)
		// remoteStarted、markErr 保存remoteStarted、markErr，供当前处理流程使用
		remoteStarted, markErr := s.itemPublishRepositoryForServer().MarkClaimedRemoteStarted(markCtx, row.ID, workerToken)
		markCancel()
		if markErr != nil || !remoteStarted {
			return fmt.Errorf("保存远端发布前检查点失败: %w", firstError(markErr, errors.New("批次租约已失效")))
		}
		// runtimeCookie 保存runtime登录凭证，供当前处理流程使用
		runtimeCookie := ""
		// runtimeCookieChanged 保存runtime登录凭证Changed，供当前处理流程使用
		runtimeCookieChanged := false
		res, err = func() (*mtop.PublishItemResult, error) {
			// credentialUnlock 保存credentialUnlock，供当前处理流程使用
			credentialUnlock := s.itemPublishRepositoryForServer().LockCredentials(row.CookieID)
			defer credentialUnlock()
			// latest、latestErr 保存latest、latestErr，供当前处理流程使用
			latest, latestErr := s.loadCookiePlatformDetail(ctx, row.CookieID)
			if latestErr != nil {
				return nil, latestErr
			}
			if latest == nil || latest.UserID != userID {
				return nil, db.ErrForbidden
			}
			if !hasStoredCookieCredential(latest) {
				return nil, errors.New("账号 Cookie 为空")
			}
			cookieValue = latest.Value
			// pctx、cancel 保存pctx、cancel，供当前处理流程使用
			pctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			// mtopCtx、cookieSession 保存mtopCtx、cookie会话，供当前处理流程使用
			mtopCtx, cookieSession := withMTopCookieSnapshot(pctx, latest)
			// published、publishErr 保存published、publishErr，供当前处理流程使用
			published, publishErr := client.PublishItem(mtopCtx, cookieValue, mtop.PublishItemRequest{
				Title:              row.Title,
				Description:        firstNonEmpty(row.Description, row.Title),
				PriceCents:         priceCents,
				OriginalPriceCents: origCents,
				Quantity:           row.Quantity,
				PostageMode:        row.PostageMode,
				PostageCents:       postageCents,
				Virtual:            true,
				Location:           selectedLocation,
				PreferredCategory:  preferredCategory,
				Images:             images,
			})
			// value、valueChanged、handled、persistErr 保存value、valueChanged、handled、persistErr，供当前处理流程使用
			value, valueChanged, handled, persistErr := s.persistMTopCookieSessionLocked(ctx, latest, cookieSession)
			if persistErr != nil {
				// cookieErr 保存登录凭证Err，供当前处理流程使用
				cookieErr := fmt.Errorf("发布商品后保存响应 Cookie Jar: %w", persistErr)
				if publishErr != nil {
					return published, errors.Join(publishErr, cookieErr)
				}
				responseCookieErr = cookieErr
			} else if handled {
				if valueChanged {
					runtimeCookie = value
					runtimeCookieChanged = true
				}
			} else if publishErr == nil && published != nil && published.UpdatedCookies != "" && published.UpdatedCookies != cookieValue {
				if // saveErr 保存saveErr，供当前处理流程使用
				saveErr := s.itemPublishRepositoryForServer().UpdateCookieValueOwned(ctx, row.CookieID, published.UpdatedCookies, userID); saveErr != nil {
					responseCookieErr = fmt.Errorf("发布商品后保存响应 Cookie: %w", saveErr)
				} else {
					runtimeCookie = published.UpdatedCookies
					runtimeCookieChanged = true
				}
			}
			if publishErr != nil {
				return published, publishErr
			}
			if published == nil {
				return nil, errors.New("发布商品接口未返回结果")
			}
			return published, nil
		}()
		if runtimeCookieChanged {
			s.updateRunningCookie(ctx, row.CookieID, runtimeCookie)
		}
		if err != nil {
			s.recoverExpiredMTOPSession(ctx, row.CookieID, err)
			if ctx.Err() != nil {
				return &uncertainRemotePublishError{err: fmt.Errorf("取消时远端发布结果未知: %w", err)}
			}
			// perr 保存perr，供当前处理流程使用
			var perr *mtop.PublishError
			if errors.As(err, &perr) {
				if perr.Code == mtop.PublishErrorStockPermissionMissing {
					return errors.New("该账号没有库存发布权限，无法按库存数量发布商品")
				}
				return err
			}
			if errors.Is(err, mtop.ErrPublishCategoryUnrecognized) {
				return err
			}
			return &uncertainRemotePublishError{err: fmt.Errorf("远端发布调用失败且结果未知: %w", err)}
		}
		// rawJSON 保存原始JSON，供当前处理流程使用
		rawJSON, _ := json.Marshal(res.RawData)
		// saveCtx、saveCancel 保存saveCtx、save取消，供当前处理流程使用
		saveCtx, saveCancel := publishStatusContext(ctx)
		// saved、saveErr 保存saved、saveErr，供当前处理流程使用
		saved, saveErr := s.itemPublishRepositoryForServer().SaveClaimedRemoteResult(saveCtx, row.ID, workerToken, res.ItemID, res.ItemURL, string(rawJSON))
		saveCancel()
		if saveErr != nil || !saved {
			return &uncertainRemotePublishError{err: fmt.Errorf("保存远端发布结果失败: %w", firstError(saveErr, errors.New("批次租约已失效")))}
		}
		if responseCookieErr != nil {
			return &postPublishError{err: responseCookieErr}
		}
	} else if strings.TrimSpace(row.RawJSON) != "" {
		_ = json.Unmarshal([]byte(row.RawJSON), &res.RawData)
	}
	if ctx.Err() != nil {
		return &postPublishError{err: ctx.Err()}
	}
	// currentBatch、err 保存currentBatch、err，供当前处理流程使用
	currentBatch, err := s.itemPublishRepositoryForServer().GetBatch(ctx, userID, row.BatchID)
	if err != nil || currentBatch.Status == "canceled" || currentBatch.WorkerToken != workerToken {
		return &postPublishError{err: context.Canceled}
	}
	if res.ItemID != "" {
		// detail 保存detail，供当前处理流程使用
		detail := map[string]any{
			"item_image":    res.ImageURL,
			"web_url":       res.ItemURL,
			"category_name": res.CategoryName,
			"quantity":      res.Quantity,
			"publish_raw":   res.RawData,
		}
		// detailJSON 保存detailJSON，供当前处理流程使用
		detailJSON, _ := json.Marshal(detail)
		if // err 保存err，供当前处理流程使用
		err := s.itemPublishRepositoryForServer().UpsertItem(ctx, &db.ItemInfoRow{
			CookieID:              row.CookieID,
			ItemID:                res.ItemID,
			ItemTitle:             firstNonEmpty(res.Title, row.Title),
			ItemDescription:       row.Description,
			ItemCategory:          res.CategoryID,
			ItemPrice:             res.PriceText,
			ItemDetail:            string(detailJSON),
			MultiQuantityDelivery: row.Quantity > 1,
		}); err != nil {
			return &postPublishError{err: fmt.Errorf("保存发布商品信息: %w", err)}
		}
		if // err 保存err，供当前处理流程使用
		err := s.createPublishAutomationRules(ctx, userID, row, res); err != nil {
			return &postPublishError{err: fmt.Errorf("创建发布商品自动化规则: %w", err)}
		}
	}
	// rawJSON 保存原始JSON，供当前处理流程使用
	rawJSON, _ := json.Marshal(res.RawData)
	// marked、err 保存marked、err，供当前处理流程使用
	marked, err := s.itemPublishRepositoryForServer().MarkClaimedRowSuccess(ctx, row.ID, workerToken, res.ItemID, res.ItemURL, string(rawJSON))
	if err != nil {
		return err
	}
	if !marked {
		return errors.New("批量任务租约已失效")
	}
	return nil
}

// firstError 负责first错误相关处理。
func firstError(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}

// createPublishAutomationRules 负责create发布自动化规则列表相关处理。
func (s *Server) createPublishAutomationRules(ctx context.Context, userID int64, row db.ItemPublishBatchRow, res *mtop.PublishItemResult) error {
	// cfg 保存cfg，供当前处理流程使用
	var cfg publishAutomationConfig
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal([]byte(row.AutomationJSON), &cfg); err != nil {
		return err
	}
	// title 保存标题，供当前处理流程使用
	title := firstNonEmpty(res.Title, row.Title)
	if cfg.PaidDelivery.Enabled {
		// actions 保存动作列表，供当前处理流程使用
		actions := make([]db.AutomationActionInput, 0, len(cfg.PaidDelivery.Actions)+1)
		// index、action 表示当前遍历过程中的index、action
		for index, action := range cfg.PaidDelivery.Actions {
			// actionConfig 保存动作配置，供当前处理流程使用
			actionConfig, _ := json.Marshal(map[string]any{"delay_override": true})
			actions = append(actions, db.AutomationActionInput{
				ActionType: automation.ActionSendCard, CardID: action.CardID,
				DeliveryCount: action.DeliveryCount, DelaySeconds: action.DelaySeconds,
				ConfigJSON: string(actionConfig), Enabled: true, SortOrder: index + 1,
			})
		}
		actions = append(actions, db.AutomationActionInput{
			ActionType: automation.ActionConfirmShipment, Enabled: true, SortOrder: len(actions) + 1,
		})
		if // err 保存err，供当前处理流程使用
		err := s.ensurePublishAutomationRule(ctx, db.AutomationRuleInput{
			UserID: userID, CookieID: row.CookieID, ItemID: res.ItemID,
			Name: "付款后自动发货 - " + title, TriggerType: automation.TriggerOrderPaid,
			Enabled: true, Priority: 100, ConfigJSON: "{}",
			Actions: actions,
		}); err != nil {
			return err
		}
	}
	if cfg.ReviewGift.Enabled {
		// actions 保存动作列表，供当前处理流程使用
		actions := make([]db.AutomationActionInput, 0, len(cfg.ReviewGift.Actions))
		// index、action 表示当前遍历过程中的index、action
		for index, action := range cfg.ReviewGift.Actions {
			// actionConfig 保存动作配置，供当前处理流程使用
			actionConfig, _ := json.Marshal(map[string]any{"delay_override": true})
			actions = append(actions, db.AutomationActionInput{
				ActionType: automation.ActionSendCard, CardID: action.CardID,
				DeliveryCount: action.DeliveryCount, DelaySeconds: action.DelaySeconds,
				ConfigJSON: string(actionConfig), Enabled: true, SortOrder: index + 1,
			})
		}
		if // err 保存err，供当前处理流程使用
		err := s.ensurePublishAutomationRule(ctx, db.AutomationRuleInput{
			UserID: userID, CookieID: row.CookieID, ItemID: res.ItemID,
			Name: "评价后发送赠品 - " + title, TriggerType: automation.TriggerBuyerReviewed,
			Enabled: true, Priority: 100, ConfigJSON: "{}",
			Actions: actions,
		}); err != nil {
			return err
		}
	}
	if cfg.ReviewRequest.Enabled {
		// cfgJSON 保存cfgJSON，供当前处理流程使用
		cfgJSON, _ := json.Marshal(map[string]any{"after_shipped_hours": cfg.ReviewRequest.AfterShippedHours, "max_attempts": cfg.ReviewRequest.MaxAttempts})
		if // err 保存err，供当前处理流程使用
		err := s.ensurePublishAutomationRule(ctx, db.AutomationRuleInput{
			UserID: userID, CookieID: row.CookieID, ItemID: res.ItemID,
			Name: "超时未评价求评价 - " + title, TriggerType: automation.TriggerReviewMissingTimeout,
			Enabled: true, Priority: 100, ConfigJSON: string(cfgJSON),
			Actions: []db.AutomationActionInput{
				{ActionType: automation.ActionSendText, MessageTemplate: cfg.ReviewRequest.Message, DelaySeconds: cfg.ReviewRequest.DelaySeconds, Enabled: true, SortOrder: 1},
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

// ensurePublishAutomationRule 负责ensure发布自动化规则相关处理。
func (s *Server) ensurePublishAutomationRule(ctx context.Context, input db.AutomationRuleInput) error {
	// exists、err 保存exists、err，供当前处理流程使用
	exists, err := s.Store.Automation.ExistsPublishRule(ctx, input)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = s.Store.Automation.Create(ctx, input)
	return err
}

// parsePublishRows 负责parse发布Rows相关处理。
func (s *Server) parsePublishRows(ctx context.Context, userID int64, defaultCookieID, uploadDir string, fallbackCategory mtop.PublishCategory, input []map[string]any) []publishBatchParsedRow {
	// out 保存out，供当前处理流程使用
	out := make([]publishBatchParsedRow, 0, len(input))
	// i、m 表示当前遍历过程中的i、m
	for i, m := range input {
		// row 保存row，供当前处理流程使用
		row := publishBatchParsedRow{
			RowNo:         i + 2,
			CookieID:      firstImportString(m, "cookie_id", "账号ID", "账号id", "账号"),
			Title:         firstImportString(m, "title", "标题", "商品标题", "商品名称"),
			Description:   firstImportString(m, "description", "描述", "商品描述", "商品详情"),
			Price:         firstImportString(m, "price", "价格", "商品价格"),
			OriginalPrice: firstImportString(m, "original_price", "原价"),
			PostageMode:   firstImportString(m, "postage_mode", "邮费模式"),
			Postage:       firstImportString(m, "postage", "邮费"),
			Raw:           m,
		}
		if row.CookieID == "" {
			row.CookieID = defaultCookieID
		}
		if row.Description == "" {
			row.Description = row.Title
		}
		if row.PostageMode == "" {
			row.PostageMode = "free"
		}
		row.PostageMode = strings.ToLower(row.PostageMode)
		if row.PostageMode == "包邮" || row.PostageMode == "free_shipping" {
			row.PostageMode = "free"
		}
		if row.PostageMode == "固定邮费" || row.PostageMode == "一口价邮费" {
			row.PostageMode = "fixed"
		}
		row.Quantity = atoiPublishDefault(firstImportString(m, "quantity", "库存", "数量"), 1)
		// rowCategory 保存row分类，供当前处理流程使用
		rowCategory := mtop.PublishCategory{
			CatID:        firstImportString(m, "category_id", "类目ID", "商品类目ID"),
			CatName:      firstImportString(m, "category_name", "类目名称", "商品类目名称", "类目"),
			ChannelCatID: firstImportString(m, "channel_category_id", "频道类目ID"),
			TBCatID:      firstImportString(m, "tb_category_id", "淘宝类目ID"),
		}
		// hasRowCategory 保存hasRow分类，供当前处理流程使用
		hasRowCategory := rowCategory.CatID != "" || rowCategory.CatName != "" || rowCategory.ChannelCatID != "" || rowCategory.TBCatID != ""
		if hasRowCategory {
			row.Category = rowCategory
			if rowCategory.CatID == "" || rowCategory.CatName == "" || rowCategory.ChannelCatID == "" {
				row.Errors = append(row.Errors, "指定行类目时必须同时填写类目ID、类目名称和频道类目ID")
			}
		} else {
			row.Category = fallbackCategory
		}
		row.Automation = parsePublishAutomation(m)
		row.Images = splitImageRefs(firstImportString(m, "images", "image", "图片", "商品图片"))
		if row.CookieID == "" {
			row.Errors = append(row.Errors, "缺少账号ID")
		} else if !s.cookieOwnedByUser(ctx, userID, row.CookieID) {
			row.Errors = append(row.Errors, "账号不存在或不属于当前用户")
		}
		if strings.TrimSpace(row.Title) == "" {
			row.Errors = append(row.Errors, "缺少标题")
		}
		if // cents、err 保存cents、err，供当前处理流程使用
		cents, err := parseMoneyCents(row.Price); err != nil || cents <= 0 {
			row.Errors = append(row.Errors, "价格必须大于 0")
		}
		if strings.TrimSpace(row.OriginalPrice) != "" {
			if // cents、err 保存cents、err，供当前处理流程使用
			cents, err := parseMoneyCents(row.OriginalPrice); err != nil || cents <= 0 {
				row.Errors = append(row.Errors, "原价格式错误")
			}
		}
		if row.Quantity <= 0 {
			row.Errors = append(row.Errors, "库存必须大于 0")
		}
		if row.PostageMode != "free" && row.PostageMode != "fixed" {
			row.Errors = append(row.Errors, "邮费模式必须是 free 或 fixed")
		}
		if row.PostageMode == "fixed" {
			if // cents、err 保存cents、err，供当前处理流程使用
			cents, err := parseMoneyCents(row.Postage); err != nil || cents < 0 {
				row.Errors = append(row.Errors, "固定邮费格式错误")
			}
		}
		if len(row.Images) == 0 {
			row.Errors = append(row.Errors, "缺少图片")
		}
		if len(row.Images) > 9 {
			row.Errors = append(row.Errors, "商品图片最多 9 张")
		}
		// ref 表示当前遍历过程中的ref
		for _, ref := range row.Images {
			if // err 保存err，供当前处理流程使用
			err := validateBatchImageRef(uploadDir, ref); err != nil {
				row.Errors = append(row.Errors, err.Error())
			}
		}
		row.Errors = append(row.Errors, s.validatePublishAutomation(ctx, userID, row.Automation)...)
		out = append(out, row)
	}
	return out
}

// parsePublishAutomation 负责parse发布自动化相关处理。
func parsePublishAutomation(m map[string]any) publishAutomationConfig {
	// cfg 保存cfg，供当前处理流程使用
	cfg := publishAutomationConfig{}
	// paidActions、paidParseErr 保存paidActions、paidParseErr，供当前处理流程使用
	paidActions, paidParseErr := parsePublishCardActions(firstImportString(m, "paid_delivery_contents", "付款发货内容"))
	cfg.PaidDelivery = publishCardAutomation{
		Enabled:    parseLooseBool(firstImportString(m, "paid_delivery_enabled", "付款发货启用")),
		Actions:    paidActions,
		ParseError: paidParseErr,
	}
	// reviewGiftActions、reviewGiftParseErr 保存reviewGiftActions、reviewGiftParseErr，供当前处理流程使用
	reviewGiftActions, reviewGiftParseErr := parsePublishCardActions(firstImportString(m, "review_gift_contents", "评价赠品内容"))
	cfg.ReviewGift = publishCardAutomation{
		Enabled:    parseLooseBool(firstImportString(m, "review_gift_enabled", "评价赠品启用")),
		Actions:    reviewGiftActions,
		ParseError: reviewGiftParseErr,
	}
	cfg.ReviewRequest = publishReviewRequestCfg{
		Enabled:           parseLooseBool(firstImportString(m, "review_request_enabled", "求评价启用")),
		AfterShippedHours: atoiPublishDefault(firstImportString(m, "review_request_after_hours", "求评价等待小时"), 72),
		Message:           firstImportString(m, "review_request_message", "求评价文案"),
		MaxAttempts:       atoiPublishDefault(firstImportString(m, "review_request_max_attempts", "求评价最多次数"), 1),
		DelaySeconds:      atoiPublishDefault(firstImportString(m, "review_request_delay_seconds", "求评价延迟秒"), 0),
	}
	return cfg
}

// parsePublishCardActions 解析“卡密组ID:每件份数:延迟秒”，多条内容用分号或换行分隔。
func parsePublishCardActions(raw string) ([]publishCardAction, string) {
	// entries 保存entries，供当前处理流程使用
	entries := strings.FieldsFunc(strings.TrimSpace(raw), func(r rune) bool {
		return r == ';' || r == '；' || r == '\n' || r == '\r'
	})
	if len(entries) == 0 {
		return nil, ""
	}
	// actions 保存动作列表，供当前处理流程使用
	actions := make([]publishCardAction, 0, len(entries))
	// index、entry 表示当前遍历过程中的index、entry
	for index, entry := range entries {
		// parts 保存parts，供当前处理流程使用
		parts := strings.Split(strings.ReplaceAll(strings.TrimSpace(entry), "：", ":"), ":")
		if len(parts) < 1 || len(parts) > 3 || strings.TrimSpace(parts[0]) == "" {
			return nil, fmt.Sprintf("第%d项格式错误，应为 卡密组ID:每件份数:延迟秒", index+1)
		}
		// cardID、err 保存卡密ID、err，供当前处理流程使用
		cardID, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		if err != nil || cardID <= 0 {
			return nil, fmt.Sprintf("第%d项卡密组ID无效", index+1)
		}
		// count 保存数量，供当前处理流程使用
		count := 1
		if len(parts) >= 2 && strings.TrimSpace(parts[1]) != "" {
			count, err = strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil || count <= 0 {
				return nil, fmt.Sprintf("第%d项每件份数必须大于0", index+1)
			}
		}
		// delay 保存延迟，供当前处理流程使用
		delay := 0
		if len(parts) == 3 && strings.TrimSpace(parts[2]) != "" {
			delay, err = strconv.Atoi(strings.TrimSpace(parts[2]))
			if err != nil || delay < 0 {
				return nil, fmt.Sprintf("第%d项延迟秒不能小于0", index+1)
			}
		}
		actions = append(actions, publishCardAction{CardID: cardID, DeliveryCount: count, DelaySeconds: delay})
	}
	return actions, ""
}

// validatePublishAutomation 负责validate发布自动化相关处理。
func (s *Server) validatePublishAutomation(ctx context.Context, userID int64, cfg publishAutomationConfig) []string {
	// errs 保存errs，供当前处理流程使用
	var errs []string
	// validateCards 保存validate卡密列表，供当前处理流程使用
	validateCards := func(config publishCardAutomation, label string) {
		if !config.Enabled {
			return
		}
		if config.ParseError != "" {
			errs = append(errs, label+config.ParseError)
			return
		}
		if len(config.Actions) == 0 {
			errs = append(errs, label+"需要至少配置一条发货内容")
			return
		}
		// index、action 表示当前遍历过程中的index、action
		for index, action := range config.Actions {
			// prefix 保存prefix，供当前处理流程使用
			prefix := fmt.Sprintf("%s第%d项", label, index+1)
			if !s.cardOwnedByUser(ctx, userID, action.CardID) {
				errs = append(errs, prefix+"卡密组不存在或不属于当前用户")
			}
			if action.DeliveryCount <= 0 {
				errs = append(errs, prefix+"每件份数必须大于0")
			}
			if action.DelaySeconds < 0 || action.DelaySeconds > 3600 {
				errs = append(errs, prefix+"延迟秒必须在 0 到 3600 之间")
			}
		}
	}
	validateCards(cfg.PaidDelivery, "付款发货")
	validateCards(cfg.ReviewGift, "评价赠品")
	if cfg.ReviewRequest.Enabled {
		if cfg.ReviewRequest.AfterShippedHours <= 0 {
			errs = append(errs, "求评价等待小时必须大于 0")
		}
		if strings.TrimSpace(cfg.ReviewRequest.Message) == "" {
			errs = append(errs, "求评价文案不能为空")
		}
		if cfg.ReviewRequest.MaxAttempts <= 0 {
			errs = append(errs, "求评价最多次数必须大于 0")
		}
	}
	return errs
}

// publishBatchToMap 负责发布批次ToMap相关处理。
func publishBatchToMap(batch *db.ItemPublishBatch, rows []db.ItemPublishBatchRow) itemPublishBatchResponse {
	// locationJSON 保存地址JSON，供当前处理流程使用
	locationJSON := strings.TrimSpace(batch.LocationJSON)
	if locationJSON == "" {
		locationJSON = "{}"
	}
	// outRows 保存outRows，供当前处理流程使用
	outRows := make([]itemPublishBatchRowResponse, 0, len(rows))
	// pending 保存pending，供当前处理流程使用
	pending := 0
	// running 保存running，供当前处理流程使用
	running := 0
	// row 表示当前待转换的批量明细行。
	for _, row := range rows {
		if row.Status == "pending" {
			pending++
		}
		if row.Status == "running" {
			running++
		}
		// refs 保存refs，供当前处理流程使用
		var refs []string
		_ = json.Unmarshal([]byte(row.ImagesJSON), &refs)
		// category 保存分类，供当前处理流程使用
		var category mtop.PublishCategory
		_ = json.Unmarshal([]byte(row.CategoryJSON), &category)
		// automationCfg 保存自动化Cfg，供当前处理流程使用
		var automationCfg publishAutomationConfig
		_ = json.Unmarshal([]byte(row.AutomationJSON), &automationCfg)
		outRows = append(outRows, itemPublishBatchRowResponse{
			ID: row.ID, RowNo: row.RowNo, CookieID: row.CookieID, Title: row.Title,
			Price: row.Price, Quantity: row.Quantity, Images: refs,
			Category:   category,
			Automation: automationCfg,
			Status:     row.Status, ItemID: row.ItemID, ItemURL: row.ItemURL,
			ErrorMessage: row.ErrorMessage, FailureKind: row.FailureKind,
		})
	}
	// retryable 保存retryable，供当前处理流程使用
	retryable := 0
	// row 表示当前用于统计可重试数量的批量明细行。
	for _, row := range rows {
		if row.Status == "failed" && row.FailureKind != "validation" && row.FailureKind != "uncertain_remote" {
			retryable++
		}
	}
	return itemPublishBatchResponse{
		ID: batch.ID, Status: batch.Status, Filename: batch.Filename,
		Total: batch.TotalCount, Success: batch.SuccessCount, Failed: batch.FailedCount,
		Pending: pending, Running: running, Retryable: retryable, Rows: outRows,
		Location:  json.RawMessage(locationJSON),
		CreatedAt: batch.CreatedAt, UpdatedAt: batch.UpdatedAt,
	}
}

// removePublishUploadDir 负责remove发布UploadDir相关处理。
func (s *Server) removePublishUploadDir(ctx context.Context, batch *db.ItemPublishBatch) {
	if batch == nil || strings.TrimSpace(batch.UploadDir) == "" {
		return
	}
	_ = os.RemoveAll(batch.UploadDir)
	_ = s.itemPublishRepositoryForServer().ClearUploadDir(ctx, batch.ID)
}

// cleanupExpiredPublishUploads 负责cleanupExpired发布Uploads相关处理。
func (s *Server) cleanupExpiredPublishUploads(ctx context.Context) {
	// cutoff 保存cutoff，供当前处理流程使用
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	// batches、err 保存batches、err，供当前处理流程使用
	batches, err := s.itemPublishRepositoryForServer().ExpiredUploads(ctx, cutoff, 100)
	if err != nil {
		return
	}
	// i 表示当前遍历过程中的i
	for i := range batches {
		s.removePublishUploadDir(ctx, &batches[i])
	}
}

// cookieOwnedByUser 判断指定账号是否属于用户，不读取或解密 Cookie 明文。
func (s *Server) cookieOwnedByUser(ctx context.Context, userID int64, cookieID string) bool {
	// owned 表示数据库中是否存在匹配用户和账号 ID 的记录。
	owned, err := s.itemPublishRepositoryForServer().ExistsOwned(ctx, userID, cookieID)
	return err == nil && owned
}

// cookieValueForUser 读取指定用户拥有的单个账号 Cookie 明文。
func (s *Server) cookieValueForUser(ctx context.Context, userID int64, cookieID string) (string, error) {
	// value 是按 user_id 与账号 ID 联合过滤后解密的单个 Cookie 明文。
	value, err := s.itemPublishRepositoryForServer().GetCookieValueOwned(ctx, userID, cookieID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
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
	// exists 和 err 表示卡券组所有权查询结果及错误。
	exists, err := s.Store.Cards.ExistsOwned(ctx, cardID, userID)
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

// serverBatchRepository 将数据库批次仓储转换为应用层批量发布端口。
type serverBatchRepository struct {
	// server 保存文件清理和数据库适配所需的 Server 引用。
	server *Server
	// repository 保存批量状态的窄数据库接口。
	repository itemPublishRepository
}

// PendingRows 查询数据库明细并移除数据库模型依赖。
func (r serverBatchRepository) PendingRows(ctx context.Context, batchID string, failedOnly bool) ([]itemapp.BatchRow, error) {
	// rows、err 保存数据库批量明细及查询错误。
	rows, err := r.repository.PendingRows(ctx, batchID, failedOnly)
	if err != nil {
		return nil, err
	}
	// result 保存应用层批量明细。
	result := make([]itemapp.BatchRow, 0, len(rows))
	// row 表示当前转换的数据库批量明细。
	for _, row := range rows {
		result = append(result, toApplicationBatchRow(row))
	}
	return result, nil
}

// RenewBatchLease 委托数据库续租并保持应用层只接收基础类型。
func (r serverBatchRepository) RenewBatchLease(ctx context.Context, batchID, workerToken string, leaseExpiresAt int64) (bool, error) {
	return r.repository.RenewBatchLease(ctx, batchID, workerToken, leaseExpiresAt)
}

// GetBatch 查询数据库批次并转换为应用层状态模型。
func (r serverBatchRepository) GetBatch(ctx context.Context, userID int64, batchID string) (itemapp.BatchInfo, error) {
	// batch、err 保存数据库批次及查询错误。
	batch, err := r.repository.GetBatch(ctx, userID, batchID)
	if err != nil {
		return itemapp.BatchInfo{}, err
	}
	return itemapp.BatchInfo{ID: batch.ID, UserID: batch.UserID, Status: batch.Status, WorkerToken: batch.WorkerToken, UploadDir: batch.UploadDir}, nil
}

// ClaimRow 委托数据库抢占明细租约。
func (r serverBatchRepository) ClaimRow(ctx context.Context, rowID int64, workerToken string) (bool, error) {
	return r.repository.ClaimRow(ctx, rowID, workerToken)
}

// BatchStatus 委托数据库读取批次状态用于失败分类。
func (r serverBatchRepository) BatchStatus(ctx context.Context, batchID string) (string, error) {
	return r.repository.BatchStatus(ctx, batchID)
}

// MarkClaimedRowFailed 委托数据库记录当前 worker 的失败明细。
func (r serverBatchRepository) MarkClaimedRowFailed(ctx context.Context, rowID int64, workerToken, message, kind string) (bool, error) {
	return r.repository.MarkClaimedRowFailed(ctx, rowID, workerToken, message, kind)
}

// RecountBatch 委托数据库重算批次进度。
func (r serverBatchRepository) RecountBatch(ctx context.Context, batchID string) error {
	return r.repository.RecountBatch(ctx, batchID)
}

// FinalizeBatch 委托数据库正常收口批次。
func (r serverBatchRepository) FinalizeBatch(ctx context.Context, batchID, workerToken string) (string, bool, error) {
	return r.repository.FinalizeBatch(ctx, batchID, workerToken)
}

// FinalizeCanceled 委托数据库收口取消中的批次。
func (r serverBatchRepository) FinalizeCanceled(ctx context.Context, batchID, workerToken string) (bool, error) {
	return r.repository.FinalizeCanceled(ctx, batchID, workerToken)
}

// FinalizeInterrupted 委托数据库收口中断批次。
func (r serverBatchRepository) FinalizeInterrupted(ctx context.Context, batchID, workerToken, message string) (string, bool, error) {
	return r.repository.FinalizeInterrupted(ctx, batchID, workerToken, message)
}

// DeleteUpload 删除已完成批次的上传目录并清除数据库路径。
func (r serverBatchRepository) DeleteUpload(ctx context.Context, batchID, uploadDir string) error {
	if uploadDir == "" {
		return nil
	}
	// removeErr 保存上传目录删除错误；数据库记录仍需尝试清理。
	removeErr := os.RemoveAll(uploadDir)
	// clearErr 保存数据库上传目录字段清理错误。
	clearErr := r.repository.ClearUploadDir(ctx, batchID)
	if removeErr != nil {
		return removeErr
	}
	return clearErr
}

// serverBatchPublisher 将 Server 的商品发布细节适配为应用层批量发布端口。
type serverBatchPublisher struct {
	// server 保存平台发布和本地商品结果适配所需的 Server 引用。
	server *Server
}

// PublishRow 调用现有单行发布适配，应用层不再接触数据库或 MTOP 类型。
func (p serverBatchPublisher) PublishRow(ctx context.Context, userID int64, row itemapp.BatchRow, workerToken string) error {
	return p.server.publishBatchRow(ctx, userID, p.server.mtopClient(), fromApplicationBatchRow(row), workerToken)
}

// toApplicationBatchRow 将数据库行映射为纯应用 DTO。
func toApplicationBatchRow(row db.ItemPublishBatchRow) itemapp.BatchRow {
	return itemapp.BatchRow{ID: row.ID, BatchID: row.BatchID, CookieID: row.CookieID, Title: row.Title, Description: row.Description, Price: row.Price, OriginalPrice: row.OriginalPrice, Quantity: row.Quantity, PostageMode: row.PostageMode, Postage: row.Postage, ImagesJSON: row.ImagesJSON, CategoryJSON: row.CategoryJSON, AutomationJSON: row.AutomationJSON, RawJSON: row.RawJSON, ItemID: row.ItemID, ItemURL: row.ItemURL}
}

// fromApplicationBatchRow 将应用 DTO 还原为 Server 平台适配所需的数据库行模型。
func fromApplicationBatchRow(row itemapp.BatchRow) db.ItemPublishBatchRow {
	return db.ItemPublishBatchRow{ID: row.ID, BatchID: row.BatchID, CookieID: row.CookieID, Title: row.Title, Description: row.Description, Price: row.Price, OriginalPrice: row.OriginalPrice, Quantity: row.Quantity, PostageMode: row.PostageMode, Postage: row.Postage, ImagesJSON: row.ImagesJSON, CategoryJSON: row.CategoryJSON, AutomationJSON: row.AutomationJSON, RawJSON: row.RawJSON, ItemID: row.ItemID, ItemURL: row.ItemURL}
}

// newItemBatchRunnerApplication 创建批量发布应用层 worker 编排器。
func newItemBatchRunnerApplication(server *Server) (*itemapp.BatchRunner, error) {
	// options 配置批量 worker 的租约、间隔和平台错误语义。
	options := itemapp.BatchRunOptions{
		LeaseDuration: publishBatchLease,
		IsSessionExpired: func(err error) bool {
			return mtop.IsSessionExpiredErr(err)
		},
		ClassifyFailure: publishBatchFailure,
	}
	return itemapp.NewBatchRunner(serverBatchRepository{server: server, repository: newStoreItemPublishRepository(server.Store)}, serverBatchPublisher{server: server}, options)
}
