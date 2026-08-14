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

	"xianyu-go/internal/auth"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

const (
	maxPublishBatchRows    = 50
	publishBatchLease      = 5 * time.Minute
	publishBatchJobTimeout = 2 * time.Hour
)

type postPublishError struct{ err error }

func (e *postPublishError) Error() string { return e.err.Error() }
func (e *postPublishError) Unwrap() error { return e.err }

type uncertainRemotePublishError struct{ err error }

func (e *uncertainRemotePublishError) Error() string { return e.err.Error() }
func (e *uncertainRemotePublishError) Unwrap() error { return e.err }

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

type publishAutomationConfig struct {
	PaidDelivery  publishCardAutomation   `json:"paid_delivery"`
	ReviewGift    publishCardAutomation   `json:"review_gift"`
	ReviewRequest publishReviewRequestCfg `json:"review_request"`
}

type publishCardAutomation struct {
	Enabled    bool                `json:"enabled"`
	Actions    []publishCardAction `json:"actions"`
	ParseError string              `json:"-"`
}

type publishCardAction struct {
	CardID        int64 `json:"card_id"`
	DeliveryCount int   `json:"delivery_count"`
	DelaySeconds  int   `json:"delay_seconds"`
}

type publishReviewRequestCfg struct {
	Enabled           bool   `json:"enabled"`
	AfterShippedHours int    `json:"after_shipped_hours"`
	Message           string `json:"message"`
	MaxAttempts       int    `json:"max_attempts"`
	DelaySeconds      int    `json:"delay_seconds"`
}

type publishCategoryRecommender interface {
	RecommendPublishCategory(ctx context.Context, cookiesStr, keyword string) (mtop.PublishCategory, string, error)
}

func (s *Server) recommendItemPublishCategory(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CookieID string `json:"cookie_id"`
		Keyword  string `json:"keyword"`
	}
	if err := decodeJSON(r, &req); err != nil {
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
	_, userID, ok := s.cookieForCurrentUser(w, r, req.CookieID)
	if !ok {
		return
	}
	recommender, ok := s.mtopClient().(publishCategoryRecommender)
	if !ok {
		writeErr(w, http.StatusNotImplemented, "当前 MTOP 客户端不支持类目推荐")
		return
	}

	credentialUnlock := s.Store.LockAccountCredentials(req.CookieID)
	runtimeCookie := ""
	defer func() {
		credentialUnlock()
		if runtimeCookie != "" {
			s.updateRunningCookie(context.Background(), req.CookieID, runtimeCookie)
		}
	}()
	latest, err := s.loadCookiePlatformDetail(r.Context(), req.CookieID)
	if err != nil || latest == nil || latest.UserID != userID || !hasStoredCookieCredential(latest) {
		writeErr(w, http.StatusConflict, "账号凭证已变化，请重试")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	mtopCtx, cookieSession := withMTopCookieSnapshot(ctx, latest)
	category, updatedCookies, callErr := recommender.RecommendPublishCategory(mtopCtx, latest.Value, req.Keyword)
	value, valueChanged, handled, persistErr := s.persistMTopCookieSessionLocked(r.Context(), latest, cookieSession)
	if persistErr != nil {
		writeErr(w, http.StatusInternalServerError, "保存账号登录状态失败")
		return
	}
	if !handled && updatedCookies != "" && updatedCookies != latest.Value {
		if err := s.Store.Cookies.UpdateValueOwned(r.Context(), req.CookieID, updatedCookies, userID); err != nil {
			writeErr(w, http.StatusInternalServerError, "保存账号登录状态失败")
			return
		}
		runtimeCookie = updatedCookies
	} else if handled && valueChanged {
		runtimeCookie = value
	}
	if callErr != nil {
		if errors.Is(callErr, mtop.ErrPublishCategoryUnrecognized) {
			writeErr(w, http.StatusNotFound, "没有匹配到可发布类目，请换一个关键词")
			return
		}
		writeErr(w, http.StatusBadGateway, callErr.Error())
		return
	}
	writeJSON(w, http.StatusOK, categoryRecommendationResponse{Success: true, Category: category})
}

func (s *Server) previewItemPublishBatch(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	s.cleanupExpiredPublishUploads(r.Context())
	// 表格最大 20 MiB，图片压缩包最大 200 MiB，额外预留 multipart 元数据空间。
	r.Body = http.MaxBytesReader(w, r.Body, maxItemPublishBatchBytes)
	// #nosec G120 -- 请求体已由 MaxBytesReader 限制。
	if err := r.ParseMultipartForm(maxItemPublishBatchParseBytes); err != nil {
		writeErr(w, http.StatusBadRequest, "解析上传文件失败")
		return
	}
	defaultCookieID := strings.TrimSpace(r.FormValue("default_cookie_id"))
	if defaultCookieID == "" {
		writeErr(w, http.StatusBadRequest, "请选择默认发布账号")
		return
	}
	if !s.cookieOwnedByUser(r.Context(), sess.UserID, defaultCookieID) {
		writeErr(w, http.StatusForbidden, "默认账号不属于当前用户")
		return
	}
	fallbackCategory := mtop.PublishCategory{
		CatID:        strings.TrimSpace(r.FormValue("fallback_category_id")),
		CatName:      strings.TrimSpace(r.FormValue("fallback_category_name")),
		ChannelCatID: strings.TrimSpace(r.FormValue("fallback_channel_category_id")),
		TBCatID:      strings.TrimSpace(r.FormValue("fallback_tb_category_id")),
	}
	var batchLocation mtop.PublishLocation
	locationJSON := strings.TrimSpace(r.FormValue("location"))
	if locationJSON != "" {
		if json.Unmarshal([]byte(locationJSON), &batchLocation) != nil {
			writeErr(w, http.StatusBadRequest, "发货地格式错误，请重新定位")
			return
		}
	}
	locationBytes, _ := json.Marshal(batchLocation)
	hasDefaultCategory := fallbackCategory.CatID != "" || fallbackCategory.CatName != "" || fallbackCategory.ChannelCatID != "" || fallbackCategory.TBCatID != ""
	if hasDefaultCategory && (fallbackCategory.CatID == "" || fallbackCategory.CatName == "" || fallbackCategory.ChannelCatID == "") {
		writeErr(w, http.StatusBadRequest, "默认类目信息不完整，请重新通过关键词获取")
		return
	}
	source, sourceHeader, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "缺少商品表格文件")
		return
	}
	defer source.Close()
	sourceBytes, tooLarge, err := readLimitedBytes(source, 20<<20)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "读取商品表格失败")
		return
	}
	if tooLarge {
		writeErr(w, http.StatusBadRequest, "商品表格不能超过 20 MiB")
		return
	}
	batchID := "batch_" + randomHex(12)
	uploadDir := filepath.Join(s.publishUploadRoot(), "publish_batches", batchID)
	if err := os.MkdirAll(uploadDir, 0o750); err != nil {
		writeErr(w, http.StatusInternalServerError, "创建上传目录失败")
		return
	}
	keepUpload := false
	defer func() {
		if !keepUpload {
			_ = os.RemoveAll(uploadDir)
		}
	}()
	sourceName := safeBaseName(sourceHeader.Filename)
	if sourceName == "" {
		sourceName = "products.csv"
	}
	if err := writeFileWithinRoot(uploadDir, sourceName, sourceBytes); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存商品表格失败")
		return
	}

	if zipFile, zipHeader, err := r.FormFile("images_zip"); err == nil {
		defer zipFile.Close()
		zipBytes, tooLarge, err := readLimitedBytes(zipFile, 200<<20)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "读取图片 zip 失败")
			return
		}
		if tooLarge {
			writeErr(w, http.StatusBadRequest, "图片 zip 不能超过 200 MiB")
			return
		}
		zipName := safeBaseName(zipHeader.Filename)
		if zipName == "" {
			zipName = "images.zip"
		}
		if err := writeFileWithinRoot(uploadDir, zipName, zipBytes); err != nil {
			writeErr(w, http.StatusInternalServerError, "保存图片 zip 失败")
			return
		}
		if err := extractPublishImagesZip(zipBytes, uploadDir); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	maps, err := parsePublishSheetBytesWithLimit(sourceBytes, sourceName, maxPublishBatchRows)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(maps) > maxPublishBatchRows {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("单个批次最多支持 %d 条商品", maxPublishBatchRows))
		return
	}
	parsed := s.parsePublishRows(r.Context(), sess.UserID, defaultCookieID, uploadDir, fallbackCategory, maps)
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
	if err := s.Store.PublishBatches.Create(r.Context(), &db.ItemPublishBatch{
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
	_ = s.Store.PublishBatches.Recount(r.Context(), batchID)
	// 预检响应保留逐行错误，客户端据此决定是否允许启动发布。
	// preview_id 是后续启动、轮询和放弃预检的稳定标识。
	// total、valid 和 invalid 继续使用旧统计口径。
	// rows 保留类目和自动化配置，避免前端重复解析上传表格。
	// 预检成功不代表远端商品已经发布。
	// 远端发布失败仍由批次明细状态表达。
	// 该 DTO 不改变上传目录清理和批次持久化时序。
	writeJSON(w, http.StatusOK, itemPublishBatchPreviewResponse{Success: true, PreviewID: batchID, Total: len(rows), Valid: valid, Invalid: invalid, Rows: previewRows})
}

func (s *Server) startItemPublishBatch(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	var req struct {
		PreviewID string `json:"preview_id"`
		BatchID   string `json:"batch_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	batchID := strings.TrimSpace(req.PreviewID)
	if batchID == "" {
		batchID = strings.TrimSpace(req.BatchID)
	}
	if batchID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 preview_id")
		return
	}
	batch, err := s.Store.PublishBatches.Get(r.Context(), sess.UserID, batchID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "批量任务不存在")
		return
	}
	activeRunning := batch.Status == "running" && batch.LeaseExpiresAt > time.Now().UTC().Unix()
	if activeRunning {
		writeErr(w, http.StatusConflict, "任务正在由其他 worker 运行")
		return
	}
	if batch.Status != "preview" && batch.Status != "pending" && batch.Status != "completed" && batch.Status != "running" {
		writeErr(w, http.StatusBadRequest, "当前任务状态不能开始发布")
		return
	}
	workerToken := randomHex(16)
	claimed, err := s.Store.PublishBatches.ClaimBatch(r.Context(), batch.ID, workerToken, time.Now().UTC().Add(publishBatchLease).Unix())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "启动任务失败")
		return
	}
	if !claimed {
		writeErr(w, http.StatusConflict, "任务已被其他 worker 启动")
		return
	}
	pending, err := s.Store.PublishBatches.PendingRows(r.Context(), batch.ID, false)
	if err != nil {
		s.failClaimedPublishBatch(batch.ID, workerToken)
		writeErr(w, http.StatusInternalServerError, "读取任务失败")
		return
	}
	if len(pending) == 0 {
		_, _, _ = s.Store.PublishBatches.FinalizeBatch(r.Context(), batch.ID, workerToken)
		writeErr(w, http.StatusBadRequest, "没有可发布的商品行")
		return
	}
	s.startPublishBatchWorker(s.lifecycleContext(), sess.UserID, batch.ID, workerToken)
	writeJSON(w, http.StatusOK, batchIDResponse{Success: true, BatchID: batch.ID})
}

func (s *Server) listItemPublishBatches(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	limit := atoiDefault(r.URL.Query().Get("limit"), 20)
	batches, err := s.Store.PublishBatches.ListForUser(r.Context(), sess.UserID, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取批量任务失败")
		return
	}
	result := make([]itemPublishBatchResponse, 0, len(batches))
	for i := range batches {
		result = append(result, publishBatchToMap(&batches[i], nil))
	}
	writeJSON(w, http.StatusOK, itemPublishBatchListResponse{Batches: result})
}

func (s *Server) getItemPublishBatch(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	batchID := chi.URLParam(r, "batch_id")
	batch, err := s.Store.PublishBatches.Get(r.Context(), sess.UserID, batchID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "批量任务不存在")
		return
	}
	rows, err := s.Store.PublishBatches.Rows(r.Context(), batch.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取任务明细失败")
		return
	}
	writeJSON(w, http.StatusOK, publishBatchToMap(batch, rows))
}

func (s *Server) cancelItemPublishBatch(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	batchID := chi.URLParam(r, "batch_id")
	_, err := s.Store.PublishBatches.Get(r.Context(), sess.UserID, batchID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "批量任务不存在")
		return
	}
	workerToken, running, err := s.Store.PublishBatches.RequestCancel(r.Context(), batchID)
	if err != nil {
		if errors.Is(err, db.ErrPublishBatchChanged) {
			writeErr(w, http.StatusConflict, "任务状态刚刚发生变化，请重试")
			return
		}
		writeErr(w, http.StatusInternalServerError, "取消任务失败")
		return
	}
	if running {
		s.cancelPublishBatch(batchID, workerToken)
	}
	writeJSON(w, http.StatusOK, batchCancelResponse{Success: true, Status: map[bool]string{true: "canceling", false: "canceled"}[running]})
}

func (s *Server) deleteItemPublishBatch(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	batchID := chi.URLParam(r, "batch_id")
	batch, err := s.Store.PublishBatches.Get(r.Context(), sess.UserID, batchID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "批量任务不存在")
		return
	}
	if batch.Status == "running" || batch.Status == "canceling" {
		writeErr(w, http.StatusConflict, "运行中的任务不能删除，请先取消")
		return
	}
	if err := s.Store.PublishBatches.Delete(r.Context(), sess.UserID, batchID); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除批量任务失败")
		return
	}
	if strings.TrimSpace(batch.UploadDir) != "" {
		_ = os.RemoveAll(batch.UploadDir)
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

func (s *Server) retryFailedItemPublishBatch(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	batchID := chi.URLParam(r, "batch_id")
	batch, err := s.Store.PublishBatches.Get(r.Context(), sess.UserID, batchID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "批量任务不存在")
		return
	}
	if batch.Status == "running" && batch.LeaseExpiresAt > time.Now().UTC().Unix() {
		writeErr(w, http.StatusConflict, "任务正在运行，不能重复重试")
		return
	}
	workerToken := randomHex(16)
	claimed, err := s.Store.PublishBatches.ClaimBatch(r.Context(), batchID, workerToken, time.Now().UTC().Add(publishBatchLease).Unix())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "启动重试失败")
		return
	}
	if !claimed {
		writeErr(w, http.StatusConflict, "任务已被其他 worker 启动")
		return
	}
	if err := s.Store.PublishBatches.ResetFailed(r.Context(), batchID); err != nil {
		s.failClaimedPublishBatch(batchID, workerToken)
		writeErr(w, http.StatusInternalServerError, "重置失败项失败")
		return
	}
	_ = s.Store.PublishBatches.Recount(r.Context(), batchID)
	pending, err := s.Store.PublishBatches.PendingRows(r.Context(), batchID, false)
	if err != nil {
		s.failClaimedPublishBatch(batchID, workerToken)
		writeErr(w, http.StatusInternalServerError, "读取可重试项失败")
		return
	}
	if len(pending) == 0 {
		_, _, _ = s.Store.PublishBatches.FinalizeBatch(r.Context(), batchID, workerToken)
		writeErr(w, http.StatusBadRequest, "没有可重试的失败项")
		return
	}
	s.startPublishBatchWorker(s.lifecycleContext(), sess.UserID, batchID, workerToken)
	writeJSON(w, http.StatusOK, batchIDResponse{Success: true, BatchID: batchID})
}

func (s *Server) startPublishBatchWorker(parent context.Context, userID int64, batchID, workerToken string) {
	workerDone := s.beginWorker()
	// #nosec G118 -- worker 由超时和 Server 根 context 共同约束。
	go func() {
		defer workerDone()
		jobCtx, cancel := context.WithTimeout(parent, publishBatchJobTimeout)
		s.registerPublishBatchCancel(batchID, workerToken, cancel)
		defer cancel()
		defer s.unregisterPublishBatchCancel(batchID, workerToken)
		s.runItemPublishBatch(jobCtx, userID, batchID, workerToken, false)
	}()
}

// RunPublishBatchRecovery 定期接管租约过期或明确因进程中断失败的批次。
func (s *Server) RunPublishBatchRecovery(ctx context.Context) {
	s.recoverPublishBatchesOnce(ctx)
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
func (s *Server) StartPublishBatchRecovery(ctx context.Context) {
	s.recoveryWG.Add(1)
	go func() {
		defer s.recoveryWG.Done()
		s.RunPublishBatchRecovery(ctx)
	}()
}

func (s *Server) recoverPublishBatchesOnce(ctx context.Context) {
	batches, err := s.Store.PublishBatches.Recoverable(ctx, time.Now().UTC().Unix(), 20)
	if err != nil {
		s.Logger.Warn("扫描可恢复批量发布任务失败", "err", err)
		return
	}
	for _, batch := range batches {
		if batch.Status == "canceling" {
			_, _ = s.Store.PublishBatches.FinalizeExpiredCancellation(ctx, batch.ID, time.Now().UTC().Unix())
			continue
		}
		workerToken := randomHex(16)
		claimed, claimErr := s.Store.PublishBatches.ClaimBatch(ctx, batch.ID, workerToken, time.Now().UTC().Add(publishBatchLease).Unix())
		if claimErr != nil || !claimed {
			continue
		}
		if err := s.Store.PublishBatches.ResetInterrupted(ctx, batch.ID); err != nil {
			s.failClaimedPublishBatch(batch.ID, workerToken)
			continue
		}
		_ = s.Store.PublishBatches.Recount(ctx, batch.ID)
		pending, pendingErr := s.Store.PublishBatches.PendingRows(ctx, batch.ID, false)
		if pendingErr != nil || len(pending) == 0 {
			if pendingErr == nil {
				_, _, _ = s.Store.PublishBatches.FinalizeBatch(ctx, batch.ID, workerToken)
			} else {
				s.failClaimedPublishBatch(batch.ID, workerToken)
			}
			continue
		}
		s.startPublishBatchWorker(ctx, batch.UserID, batch.ID, workerToken)
	}
}

func (s *Server) failClaimedPublishBatch(batchID, workerToken string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if released, err := s.Store.PublishBatches.FailClaimedBatch(ctx, batchID, workerToken); err != nil {
		s.Logger.Warn("释放异常批量发布任务失败", "batch", batchID, "err", err)
	} else if !released {
		s.Logger.Debug("批量发布任务租约已转移，无需释放", "batch", batchID)
	}
}

func (s *Server) downloadItemPublishBatchResult(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	batchID := chi.URLParam(r, "batch_id")
	batch, err := s.Store.PublishBatches.Get(r.Context(), sess.UserID, batchID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "批量任务不存在")
		return
	}
	rows, err := s.Store.PublishBatches.Rows(r.Context(), batch.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取任务明细失败")
		return
	}
	var buf bytes.Buffer
	buf.WriteString("\xEF\xBB\xBF")
	cw := csv.NewWriter(&buf)
	_ = cw.Write([]string{"行号", "状态", "账号ID", "标题", "价格", "库存", "默认类目ID", "默认类目名称", "商品ID", "商品URL", "错误原因"})
	for _, row := range rows {
		var category mtop.PublishCategory
		_ = json.Unmarshal([]byte(row.CategoryJSON), &category)
		_ = cw.Write([]string{
			strconv.Itoa(row.RowNo), safeCSVCell(row.Status), safeCSVCell(row.CookieID), safeCSVCell(row.Title), safeCSVCell(row.Price),
			strconv.Itoa(row.Quantity), safeCSVCell(category.CatID), safeCSVCell(category.CatName),
			safeCSVCell(row.ItemID), safeCSVCell(row.ItemURL), safeCSVCell(row.ErrorMessage),
		})
	}
	cw.Flush()
	filename := fmt.Sprintf("publish_result_%s.csv", batch.ID)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(filename))
	_, _ = w.Write(buf.Bytes())
}

// safeCSVCell 防止用户可控内容被电子表格应用解释为公式。开头的单引号
// 在 Excel/LibreOffice 中作为文本标记，不改变导出的可见内容。
func safeCSVCell(value string) string {
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

func (s *Server) runItemPublishBatch(ctx context.Context, userID int64, batchID, workerToken string, failedOnly bool) {
	rows, err := s.Store.PublishBatches.PendingRows(ctx, batchID, failedOnly)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn("读取批量发布行失败", "batch", batchID, "err", err)
		}
		if ctx.Err() != nil {
			s.finishInterruptedPublishBatch(ctx, userID, batchID, workerToken)
		}
		return
	}
	client := s.mtopClient()
	for idx, row := range rows {
		if ctx.Err() != nil {
			s.finishInterruptedPublishBatch(ctx, userID, batchID, workerToken)
			return
		}
		wctx, cancel := publishStatusContext(ctx)
		renewed, renewErr := s.Store.PublishBatches.RenewBatchLease(wctx, batchID, workerToken, time.Now().UTC().Add(publishBatchLease).Unix())
		cancel()
		if renewErr != nil || !renewed {
			s.finishInterruptedPublishBatch(ctx, userID, batchID, workerToken)
			return
		}
		wctx, cancel = publishStatusContext(ctx)
		batch, err := s.Store.PublishBatches.Get(wctx, userID, batchID)
		cancel()
		if err != nil || batch.Status != "running" || batch.WorkerToken != workerToken {
			s.finishInterruptedPublishBatch(ctx, userID, batchID, workerToken)
			return
		}
		wctx, cancel = publishStatusContext(ctx)
		claimed, claimErr := s.Store.PublishBatches.ClaimRow(wctx, row.ID, workerToken)
		cancel()
		if claimErr != nil {
			s.finishInterruptedPublishBatch(ctx, userID, batchID, workerToken)
			return
		}
		if !claimed {
			continue
		}
		if rowErr := s.publishBatchRow(ctx, userID, client, row, workerToken); rowErr != nil {
			sessionExpired := mtop.IsSessionExpiredErr(rowErr)
			statusCtx, statusCancel := context.WithTimeout(context.Background(), 5*time.Second)
			status, _ := s.Store.PublishBatches.BatchStatus(statusCtx, batchID)
			statusCancel()
			message, failureKind := publishBatchFailure(rowErr, status)
			wctx, cancel := publishStatusContext(ctx)
			marked, markErr := s.Store.PublishBatches.MarkClaimedRowFailed(wctx, row.ID, workerToken, message, failureKind)
			cancel()
			if markErr != nil || !marked {
				if s.Logger != nil {
					s.Logger.Warn("保存批量发布失败状态失败，等待租约恢复", "batch", batchID, "row", row.ID, "err", markErr)
				}
				return
			}
			if sessionExpired {
				s.finishInterruptedPublishBatch(ctx, userID, batchID, workerToken)
				return
			}
		}
		wctx, cancel = publishStatusContext(ctx)
		if err := s.Store.PublishBatches.Recount(wctx, batchID); err != nil && s.Logger != nil {
			s.Logger.Warn("重算批量发布进度失败", "batch", batchID, "err", err)
		}
		cancel()
		if idx < len(rows)-1 {
			delay := time.Duration(10+idx%21) * time.Second
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				s.finishInterruptedPublishBatch(ctx, userID, batchID, workerToken)
				return
			case <-timer.C:
			}
		}
	}
	s.finishPublishBatch(ctx, userID, batchID, workerToken)
}

func publishBatchFailure(err error, batchStatus string) (string, string) {
	message := err.Error()
	failureKind := "publish"
	var postErr *postPublishError
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

func (s *Server) registerPublishBatchCancel(batchID, workerToken string, cancel context.CancelFunc) {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	if s.publishCancels == nil {
		s.publishCancels = make(map[string]publishBatchWorker)
	}
	if old := s.publishCancels[batchID]; old.cancel != nil {
		old.cancel()
	}
	s.publishCancels[batchID] = publishBatchWorker{token: workerToken, cancel: cancel}
}

func (s *Server) unregisterPublishBatchCancel(batchID, workerToken string) {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	if current := s.publishCancels[batchID]; current.token == workerToken {
		delete(s.publishCancels, batchID)
	}
}

func (s *Server) cancelPublishBatch(batchID, workerToken string) bool {
	s.publishMu.Lock()
	worker := s.publishCancels[batchID]
	s.publishMu.Unlock()
	if worker.token != workerToken || worker.cancel == nil {
		return false
	}
	worker.cancel()
	return true
}

func publishStatusContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent != nil && parent.Err() == nil {
		return context.WithTimeout(parent, 5*time.Second)
	}
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func (s *Server) finishInterruptedPublishBatch(ctx context.Context, userID int64, batchID, workerToken string) {
	wctx, cancel := publishStatusContext(ctx)
	defer cancel()
	batch, err := s.Store.PublishBatches.Get(wctx, userID, batchID)
	if err != nil {
		return
	}
	if batch.Status == "canceled" || batch.Status == "canceling" {
		if batch.Status == "canceling" {
			if batch.WorkerToken != workerToken {
				return
			}
			_, _ = s.Store.PublishBatches.FinalizeCanceled(wctx, batchID, workerToken)
		}
		// 取消产生的 interrupted 失败行允许用户稍后重试，图片由统一的过期清理保留 7 天。
		return
	}
	_, _, finalizeErr := s.Store.PublishBatches.FinalizeInterrupted(wctx, batchID, workerToken, "任务超时或已中断")
	if finalizeErr != nil && s.Logger != nil {
		s.Logger.Warn("结束中断的批量发布任务失败，等待租约恢复", "batch", batchID, "err", finalizeErr)
	}
}

func (s *Server) finishPublishBatch(ctx context.Context, userID int64, batchID, workerToken string) {
	wctx, cancel := publishStatusContext(ctx)
	defer cancel()
	batch, err := s.Store.PublishBatches.Get(wctx, userID, batchID)
	if err != nil || batch.Status == "canceled" || batch.WorkerToken != workerToken {
		return
	}
	if batch.Status == "canceling" {
		_, _ = s.Store.PublishBatches.FinalizeCanceled(wctx, batchID, workerToken)
		// 取消任务仍可“重试失败项”，不能在此删除重试所需的本地图片。
		return
	}
	finalStatus, finished, finishErr := s.Store.PublishBatches.FinalizeBatch(wctx, batchID, workerToken)
	if finishErr != nil {
		if s.Logger != nil {
			s.Logger.Warn("结束批量发布任务失败，等待租约恢复", "batch", batchID, "err", finishErr)
		}
		return
	}
	if finished && finalStatus == "completed" && strings.TrimSpace(batch.UploadDir) != "" {
		s.removePublishUploadDir(wctx, batch)
	}
}

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

func (s *Server) publishBatchRow(ctx context.Context, userID int64, client mtop.Client, row db.ItemPublishBatchRow, workerToken string) error {
	batch, err := s.Store.PublishBatches.Get(ctx, userID, row.BatchID)
	if err != nil {
		return errors.New("批量任务不存在")
	}
	var location mtop.PublishLocation
	locationJSON := strings.TrimSpace(batch.LocationJSON)
	if locationJSON == "" {
		locationJSON = "{}"
	}
	if err := json.Unmarshal([]byte(locationJSON), &location); err != nil {
		return errors.New("批量任务发货地配置损坏，请重新创建任务")
	}
	var selectedLocation *mtop.PublishLocation
	if strings.TrimSpace(location.DivisionID) != "" {
		selectedLocation = &location
	}
	cookieValue, err := s.cookieValueForUser(ctx, userID, row.CookieID)
	if err != nil {
		return err
	}
	priceCents, err := parseMoneyCents(row.Price)
	if err != nil || priceCents <= 0 {
		return errors.New("商品价格必须大于 0")
	}
	origCents, _ := parseMoneyCents(row.OriginalPrice)
	postageCents, _ := parseMoneyCents(row.Postage)
	res := &mtop.PublishItemResult{ItemID: row.ItemID, ItemURL: row.ItemURL, Title: row.Title, PriceText: row.Price, Quantity: row.Quantity}
	var responseCookieErr error
	if row.ItemID == "" {
		var preferredCategory *mtop.PublishCategory
		rawCategory := strings.TrimSpace(row.CategoryJSON)
		if rawCategory != "" && rawCategory != "{}" {
			var configured mtop.PublishCategory
			if err := json.Unmarshal([]byte(rawCategory), &configured); err != nil {
				return errors.New("默认类目配置损坏，请重新创建批量任务")
			}
			if strings.TrimSpace(configured.CatID) != "" || strings.TrimSpace(configured.CatName) != "" || strings.TrimSpace(configured.ChannelCatID) != "" || strings.TrimSpace(configured.TBCatID) != "" {
				if strings.TrimSpace(configured.CatID) == "" || strings.TrimSpace(configured.CatName) == "" || strings.TrimSpace(configured.ChannelCatID) == "" {
					return errors.New("默认类目信息不完整，请重新创建批量任务")
				}
				preferredCategory = &configured
			}
		}
		images, err := loadBatchPublishImages(ctx, batch.UploadDir, row)
		if err != nil {
			return err
		}
		markCtx, markCancel := publishStatusContext(ctx)
		remoteStarted, markErr := s.Store.PublishBatches.MarkClaimedRemoteStarted(markCtx, row.ID, workerToken)
		markCancel()
		if markErr != nil || !remoteStarted {
			return fmt.Errorf("保存远端发布前检查点失败: %w", firstError(markErr, errors.New("批次租约已失效")))
		}
		runtimeCookie := ""
		runtimeCookieChanged := false
		res, err = func() (*mtop.PublishItemResult, error) {
			credentialUnlock := s.Store.LockAccountCredentials(row.CookieID)
			defer credentialUnlock()
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
			pctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			mtopCtx, cookieSession := withMTopCookieSnapshot(pctx, latest)
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
			value, valueChanged, handled, persistErr := s.persistMTopCookieSessionLocked(ctx, latest, cookieSession)
			if persistErr != nil {
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
				if saveErr := s.Store.Cookies.UpdateValueOwned(ctx, row.CookieID, published.UpdatedCookies, userID); saveErr != nil {
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
		rawJSON, _ := json.Marshal(res.RawData)
		saveCtx, saveCancel := publishStatusContext(ctx)
		saved, saveErr := s.Store.PublishBatches.SaveClaimedRemoteResult(saveCtx, row.ID, workerToken, res.ItemID, res.ItemURL, string(rawJSON))
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
	currentBatch, err := s.Store.PublishBatches.Get(ctx, userID, row.BatchID)
	if err != nil || currentBatch.Status == "canceled" || currentBatch.WorkerToken != workerToken {
		return &postPublishError{err: context.Canceled}
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
		if err := s.Store.Items.Upsert(ctx, &db.ItemInfoRow{
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
		if err := s.createPublishAutomationRules(ctx, userID, row, res); err != nil {
			return &postPublishError{err: fmt.Errorf("创建发布商品自动化规则: %w", err)}
		}
	}
	rawJSON, _ := json.Marshal(res.RawData)
	marked, err := s.Store.PublishBatches.MarkClaimedRowSuccess(ctx, row.ID, workerToken, res.ItemID, res.ItemURL, string(rawJSON))
	if err != nil {
		return err
	}
	if !marked {
		return errors.New("批量任务租约已失效")
	}
	return nil
}

func firstError(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}

func (s *Server) createPublishAutomationRules(ctx context.Context, userID int64, row db.ItemPublishBatchRow, res *mtop.PublishItemResult) error {
	var cfg publishAutomationConfig
	if err := json.Unmarshal([]byte(row.AutomationJSON), &cfg); err != nil {
		return err
	}
	title := firstNonEmpty(res.Title, row.Title)
	if cfg.PaidDelivery.Enabled {
		actions := make([]db.AutomationActionInput, 0, len(cfg.PaidDelivery.Actions)+1)
		for index, action := range cfg.PaidDelivery.Actions {
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
		if err := s.ensurePublishAutomationRule(ctx, db.AutomationRuleInput{
			UserID: userID, CookieID: row.CookieID, ItemID: res.ItemID,
			Name: "付款后自动发货 - " + title, TriggerType: automation.TriggerOrderPaid,
			Enabled: true, Priority: 100, ConfigJSON: "{}",
			Actions: actions,
		}); err != nil {
			return err
		}
	}
	if cfg.ReviewGift.Enabled {
		actions := make([]db.AutomationActionInput, 0, len(cfg.ReviewGift.Actions))
		for index, action := range cfg.ReviewGift.Actions {
			actionConfig, _ := json.Marshal(map[string]any{"delay_override": true})
			actions = append(actions, db.AutomationActionInput{
				ActionType: automation.ActionSendCard, CardID: action.CardID,
				DeliveryCount: action.DeliveryCount, DelaySeconds: action.DelaySeconds,
				ConfigJSON: string(actionConfig), Enabled: true, SortOrder: index + 1,
			})
		}
		if err := s.ensurePublishAutomationRule(ctx, db.AutomationRuleInput{
			UserID: userID, CookieID: row.CookieID, ItemID: res.ItemID,
			Name: "评价后发送赠品 - " + title, TriggerType: automation.TriggerBuyerReviewed,
			Enabled: true, Priority: 100, ConfigJSON: "{}",
			Actions: actions,
		}); err != nil {
			return err
		}
	}
	if cfg.ReviewRequest.Enabled {
		cfgJSON, _ := json.Marshal(map[string]any{"after_shipped_hours": cfg.ReviewRequest.AfterShippedHours, "max_attempts": cfg.ReviewRequest.MaxAttempts})
		if err := s.ensurePublishAutomationRule(ctx, db.AutomationRuleInput{
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

func (s *Server) ensurePublishAutomationRule(ctx context.Context, input db.AutomationRuleInput) error {
	var exists bool
	err := s.Store.DB.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM automation_rules
		 WHERE user_id=? AND cookie_id=? AND item_id=? AND trigger_type=? AND name=? AND deleted_at IS NULL
	)`, input.UserID, input.CookieID, input.ItemID, input.TriggerType, input.Name).Scan(&exists)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = s.Store.Automation.Create(ctx, input)
	return err
}

func (s *Server) parsePublishRows(ctx context.Context, userID int64, defaultCookieID, uploadDir string, fallbackCategory mtop.PublishCategory, input []map[string]any) []publishBatchParsedRow {
	out := make([]publishBatchParsedRow, 0, len(input))
	for i, m := range input {
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
		rowCategory := mtop.PublishCategory{
			CatID:        firstImportString(m, "category_id", "类目ID", "商品类目ID"),
			CatName:      firstImportString(m, "category_name", "类目名称", "商品类目名称", "类目"),
			ChannelCatID: firstImportString(m, "channel_category_id", "频道类目ID"),
			TBCatID:      firstImportString(m, "tb_category_id", "淘宝类目ID"),
		}
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
		if cents, err := parseMoneyCents(row.Price); err != nil || cents <= 0 {
			row.Errors = append(row.Errors, "价格必须大于 0")
		}
		if strings.TrimSpace(row.OriginalPrice) != "" {
			if cents, err := parseMoneyCents(row.OriginalPrice); err != nil || cents <= 0 {
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
			if cents, err := parseMoneyCents(row.Postage); err != nil || cents < 0 {
				row.Errors = append(row.Errors, "固定邮费格式错误")
			}
		}
		if len(row.Images) == 0 {
			row.Errors = append(row.Errors, "缺少图片")
		}
		if len(row.Images) > 9 {
			row.Errors = append(row.Errors, "商品图片最多 9 张")
		}
		for _, ref := range row.Images {
			if err := validateBatchImageRef(uploadDir, ref); err != nil {
				row.Errors = append(row.Errors, err.Error())
			}
		}
		row.Errors = append(row.Errors, s.validatePublishAutomation(ctx, userID, row.Automation)...)
		out = append(out, row)
	}
	return out
}

func parsePublishAutomation(m map[string]any) publishAutomationConfig {
	cfg := publishAutomationConfig{}
	paidActions, paidParseErr := parsePublishCardActions(firstImportString(m, "paid_delivery_contents", "付款发货内容"))
	cfg.PaidDelivery = publishCardAutomation{
		Enabled:    parseLooseBool(firstImportString(m, "paid_delivery_enabled", "付款发货启用")),
		Actions:    paidActions,
		ParseError: paidParseErr,
	}
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
	entries := strings.FieldsFunc(strings.TrimSpace(raw), func(r rune) bool {
		return r == ';' || r == '；' || r == '\n' || r == '\r'
	})
	if len(entries) == 0 {
		return nil, ""
	}
	actions := make([]publishCardAction, 0, len(entries))
	for index, entry := range entries {
		parts := strings.Split(strings.ReplaceAll(strings.TrimSpace(entry), "：", ":"), ":")
		if len(parts) < 1 || len(parts) > 3 || strings.TrimSpace(parts[0]) == "" {
			return nil, fmt.Sprintf("第%d项格式错误，应为 卡密组ID:每件份数:延迟秒", index+1)
		}
		cardID, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		if err != nil || cardID <= 0 {
			return nil, fmt.Sprintf("第%d项卡密组ID无效", index+1)
		}
		count := 1
		if len(parts) >= 2 && strings.TrimSpace(parts[1]) != "" {
			count, err = strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil || count <= 0 {
				return nil, fmt.Sprintf("第%d项每件份数必须大于0", index+1)
			}
		}
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

func (s *Server) validatePublishAutomation(ctx context.Context, userID int64, cfg publishAutomationConfig) []string {
	var errs []string
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
		for index, action := range config.Actions {
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

func publishBatchToMap(batch *db.ItemPublishBatch, rows []db.ItemPublishBatchRow) itemPublishBatchResponse {
	locationJSON := strings.TrimSpace(batch.LocationJSON)
	if locationJSON == "" {
		locationJSON = "{}"
	}
	outRows := make([]itemPublishBatchRowResponse, 0, len(rows))
	pending := 0
	running := 0
	for _, row := range rows {
		if row.Status == "pending" {
			pending++
		}
		if row.Status == "running" {
			running++
		}
		var refs []string
		_ = json.Unmarshal([]byte(row.ImagesJSON), &refs)
		var category mtop.PublishCategory
		_ = json.Unmarshal([]byte(row.CategoryJSON), &category)
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
	retryable := 0
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

func (s *Server) removePublishUploadDir(ctx context.Context, batch *db.ItemPublishBatch) {
	if batch == nil || strings.TrimSpace(batch.UploadDir) == "" {
		return
	}
	_ = os.RemoveAll(batch.UploadDir)
	_ = s.Store.PublishBatches.ClearUploadDir(ctx, batch.ID)
}

func (s *Server) cleanupExpiredPublishUploads(ctx context.Context) {
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour).Format("2006-01-02 15:04:05")
	batches, err := s.Store.PublishBatches.ExpiredUploads(ctx, cutoff, 100)
	if err != nil {
		return
	}
	for i := range batches {
		s.removePublishUploadDir(ctx, &batches[i])
	}
}

// cookieOwnedByUser 判断指定账号是否属于用户，不读取或解密 Cookie 明文。
func (s *Server) cookieOwnedByUser(ctx context.Context, userID int64, cookieID string) bool {
	// owned 表示数据库中是否存在匹配用户和账号 ID 的记录。
	owned, err := s.Store.Cookies.ExistsOwned(ctx, userID, cookieID)
	return err == nil && owned
}

// cookieValueForUser 读取指定用户拥有的单个账号 Cookie 明文。
func (s *Server) cookieValueForUser(ctx context.Context, userID int64, cookieID string) (string, error) {
	// value 是按 user_id 与账号 ID 联合过滤后解密的单个 Cookie 明文。
	value, err := s.Store.Cookies.GetValueOwned(ctx, userID, cookieID)
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
	// exists 表示数据库中是否存在匹配用户和卡券组 ID 的记录。
	var exists bool
	// err 表示卡券组所有权查询失败的原因。
	err := s.Store.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM cards WHERE id=? AND user_id=?)`, cardID, userID).Scan(&exists)
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
