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

const maxPublishBatchRows = 50

type publishBatchPreviewRow struct {
	RowNo      int                     `json:"row_no"`
	Valid      bool                    `json:"valid"`
	Errors     []string                `json:"errors,omitempty"`
	CookieID   string                  `json:"cookie_id"`
	Title      string                  `json:"title"`
	Price      string                  `json:"price"`
	Quantity   int                     `json:"quantity"`
	Images     []string                `json:"images"`
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

func (s *Server) previewItemPublishBatch(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	// 表格最大 20 MiB，图片压缩包最大 200 MiB，额外预留 multipart 元数据空间。
	r.Body = http.MaxBytesReader(w, r.Body, maxItemPublishBatchBytes)
	// #nosec G120 -- 请求体已由 MaxBytesReader 限制。
	if err := r.ParseMultipartForm(maxItemPublishBatchParseBytes); err != nil {
		writeErr(w, http.StatusBadRequest, "解析上传文件失败")
		return
	}
	defaultCookieID := strings.TrimSpace(r.FormValue("default_cookie_id"))
	if defaultCookieID != "" && !s.cookieOwnedByUser(r.Context(), sess.UserID, defaultCookieID) {
		writeErr(w, http.StatusForbidden, "默认账号不属于当前用户")
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
	parsed := s.parsePublishRows(r.Context(), sess.UserID, defaultCookieID, uploadDir, maps)
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
			AutomationJSON: string(automationJSON),
			Status:         status,
			ErrorMessage:   errMsg,
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
		Status:          "preview",
	}, rows); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存预检结果失败")
		return
	}
	_ = s.Store.PublishBatches.Recount(r.Context(), batchID)
	writeJSON(w, http.StatusOK, map[string]any{
		"success":    true,
		"preview_id": batchID,
		"total":      len(rows),
		"valid":      valid,
		"invalid":    invalid,
		"rows":       previewRows,
	})
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
	if batch.Status != "preview" && batch.Status != "pending" && batch.Status != "completed" {
		writeErr(w, http.StatusBadRequest, "当前任务状态不能开始发布")
		return
	}
	pending, err := s.Store.PublishBatches.PendingRows(r.Context(), batch.ID, false)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取任务失败")
		return
	}
	if len(pending) == 0 {
		writeErr(w, http.StatusBadRequest, "没有可发布的商品行")
		return
	}
	if err := s.Store.PublishBatches.SetBatchStatus(r.Context(), batch.ID, "running"); err != nil {
		writeErr(w, http.StatusInternalServerError, "启动任务失败")
		return
	}
	// #nosec G118 -- 批处理必须在请求结束后继续，并由 30 分钟超时保证退出。
	go func() {
		jobCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		s.registerPublishBatchCancel(batch.ID, cancel)
		defer cancel()
		defer s.unregisterPublishBatchCancel(batch.ID)
		s.runItemPublishBatch(jobCtx, sess.UserID, batch.ID, false)
	}()
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "batch_id": batch.ID})
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
	if _, err := s.Store.PublishBatches.Get(r.Context(), sess.UserID, batchID); err != nil {
		writeErr(w, http.StatusNotFound, "批量任务不存在")
		return
	}
	if err := s.Store.PublishBatches.SetBatchStatus(r.Context(), batchID, "canceled"); err != nil {
		writeErr(w, http.StatusInternalServerError, "取消任务失败")
		return
	}
	s.cancelPublishBatch(batchID)
	_ = s.Store.PublishBatches.MarkUnfinishedFailed(r.Context(), batchID, "任务已取消")
	_ = s.Store.PublishBatches.Recount(r.Context(), batchID)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) retryFailedItemPublishBatch(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	batchID := chi.URLParam(r, "batch_id")
	if _, err := s.Store.PublishBatches.Get(r.Context(), sess.UserID, batchID); err != nil {
		writeErr(w, http.StatusNotFound, "批量任务不存在")
		return
	}
	if err := s.Store.PublishBatches.ResetFailed(r.Context(), batchID); err != nil {
		writeErr(w, http.StatusInternalServerError, "重置失败项失败")
		return
	}
	_ = s.Store.PublishBatches.Recount(r.Context(), batchID)
	if err := s.Store.PublishBatches.SetBatchStatus(r.Context(), batchID, "running"); err != nil {
		writeErr(w, http.StatusInternalServerError, "启动重试失败")
		return
	}
	// #nosec G118 -- 批处理必须在请求结束后继续，并由 30 分钟超时保证退出。
	go func() {
		jobCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		s.registerPublishBatchCancel(batchID, cancel)
		defer cancel()
		defer s.unregisterPublishBatchCancel(batchID)
		s.runItemPublishBatch(jobCtx, sess.UserID, batchID, false)
	}()
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "batch_id": batchID})
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
	_ = cw.Write([]string{"行号", "状态", "账号ID", "标题", "价格", "库存", "商品ID", "商品URL", "错误原因"})
	for _, row := range rows {
		_ = cw.Write([]string{
			strconv.Itoa(row.RowNo), row.Status, row.CookieID, row.Title, row.Price,
			strconv.Itoa(row.Quantity), row.ItemID, row.ItemURL, row.ErrorMessage,
		})
	}
	cw.Flush()
	filename := fmt.Sprintf("publish_result_%s.csv", batch.ID)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(filename))
	_, _ = w.Write(buf.Bytes())
}

func (s *Server) runItemPublishBatch(ctx context.Context, userID int64, batchID string, failedOnly bool) {
	rows, err := s.Store.PublishBatches.PendingRows(ctx, batchID, failedOnly)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn("读取批量发布行失败", "batch", batchID, "err", err)
		}
		return
	}
	client := s.mtopClient()
	for idx, row := range rows {
		if ctx.Err() != nil {
			s.finishInterruptedPublishBatch(ctx, userID, batchID)
			return
		}
		wctx, cancel := publishStatusContext(ctx)
		status, err := s.Store.PublishBatches.BatchStatus(wctx, batchID)
		cancel()
		if err != nil || status == "canceled" {
			wctx, cancel := publishStatusContext(ctx)
			if status == "canceled" {
				_ = s.Store.PublishBatches.MarkUnfinishedFailed(wctx, batchID, "任务已取消")
			}
			_ = s.Store.PublishBatches.Recount(wctx, batchID)
			cancel()
			return
		}
		wctx, cancel = publishStatusContext(ctx)
		_ = s.Store.PublishBatches.MarkRowRunning(wctx, row.ID)
		cancel()
		if err := s.publishBatchRow(ctx, userID, client, row); err != nil {
			message := err.Error()
			if status, _ := s.Store.PublishBatches.BatchStatus(context.Background(), batchID); status == "canceled" {
				message = "任务已取消"
			}
			wctx, cancel := publishStatusContext(ctx)
			_ = s.Store.PublishBatches.MarkRowFailed(wctx, row.ID, message)
			cancel()
		}
		wctx, cancel = publishStatusContext(ctx)
		_ = s.Store.PublishBatches.Recount(wctx, batchID)
		cancel()
		if idx < len(rows)-1 {
			delay := time.Duration(10+idx%21) * time.Second
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				s.finishInterruptedPublishBatch(ctx, userID, batchID)
				return
			case <-timer.C:
			}
		}
	}
	s.finishPublishBatch(ctx, userID, batchID)
}

func (s *Server) registerPublishBatchCancel(batchID string, cancel context.CancelFunc) {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	if s.publishCancels == nil {
		s.publishCancels = make(map[string]context.CancelFunc)
	}
	if old := s.publishCancels[batchID]; old != nil {
		old()
	}
	s.publishCancels[batchID] = cancel
}

func (s *Server) unregisterPublishBatchCancel(batchID string) {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	delete(s.publishCancels, batchID)
}

func (s *Server) cancelPublishBatch(batchID string) {
	s.publishMu.Lock()
	cancel := s.publishCancels[batchID]
	s.publishMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func publishStatusContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent != nil && parent.Err() == nil {
		return context.WithTimeout(parent, 5*time.Second)
	}
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func (s *Server) finishInterruptedPublishBatch(ctx context.Context, userID int64, batchID string) {
	wctx, cancel := publishStatusContext(ctx)
	defer cancel()
	if status, _ := s.Store.PublishBatches.BatchStatus(wctx, batchID); status == "canceled" {
		_ = s.Store.PublishBatches.MarkUnfinishedFailed(wctx, batchID, "任务已取消")
		_ = s.Store.PublishBatches.Recount(wctx, batchID)
		return
	}
	_ = s.Store.PublishBatches.MarkUnfinishedFailed(wctx, batchID, "任务超时或已中断")
	_ = s.Store.PublishBatches.Recount(wctx, batchID)
	s.finishPublishBatch(wctx, userID, batchID)
}

func (s *Server) finishPublishBatch(ctx context.Context, userID int64, batchID string) {
	wctx, cancel := publishStatusContext(ctx)
	defer cancel()
	_ = s.Store.PublishBatches.Recount(wctx, batchID)
	batch, err := s.Store.PublishBatches.Get(wctx, userID, batchID)
	if err != nil || batch.Status == "canceled" {
		return
	}
	finalStatus := finalPublishBatchStatus(batch)
	_ = s.Store.PublishBatches.SetBatchStatus(wctx, batchID, finalStatus)
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

func (s *Server) publishBatchRow(ctx context.Context, userID int64, client mtop.Client, row db.ItemPublishBatchRow) error {
	batch, err := s.Store.PublishBatches.Get(ctx, userID, row.BatchID)
	if err != nil {
		return errors.New("批量任务不存在")
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
	images, err := loadBatchPublishImages(ctx, batch.UploadDir, row)
	if err != nil {
		return err
	}
	pctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	res, err := client.PublishItem(pctx, cookieValue, mtop.PublishItemRequest{
		Title:              row.Title,
		Description:        firstNonEmpty(row.Description, row.Title),
		PriceCents:         priceCents,
		OriginalPriceCents: origCents,
		Quantity:           row.Quantity,
		PostageMode:        row.PostageMode,
		PostageCents:       postageCents,
		Images:             images,
	})
	if err != nil {
		var perr *mtop.PublishError
		if errors.As(err, &perr) && perr.Code == mtop.PublishErrorStockPermissionMissing {
			return errors.New("该账号没有库存发布权限，无法按库存数量发布商品")
		}
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if status, _ := s.Store.PublishBatches.BatchStatus(ctx, row.BatchID); status == "canceled" {
		return context.Canceled
	}
	if res.UpdatedCookies != "" && res.UpdatedCookies != cookieValue {
		if err := s.Store.Cookies.Save(ctx, row.CookieID, res.UpdatedCookies, userID); err != nil {
			s.Logger.Error("发布商品后保存刷新 cookie 失败", "cookie_id", row.CookieID, "err", err)
		}
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
			s.Logger.Error("发布商品后保存商品信息失败", "cookie_id", row.CookieID, "item_id", res.ItemID, "err", err)
		}
		if err := s.createPublishAutomationRules(ctx, userID, row, res); err != nil {
			s.Logger.Error("发布商品后创建自动化规则失败", "cookie_id", row.CookieID, "item_id", res.ItemID, "err", err)
		}
	}
	rawJSON, _ := json.Marshal(res.RawData)
	return s.Store.PublishBatches.MarkRowSuccess(ctx, row.ID, res.ItemID, res.ItemURL, string(rawJSON))
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
			actions = append(actions, db.AutomationActionInput{
				ActionType: automation.ActionSendCard, CardID: action.CardID,
				DeliveryCount: action.DeliveryCount, DelaySeconds: action.DelaySeconds,
				Enabled: true, SortOrder: index + 1,
			})
		}
		actions = append(actions, db.AutomationActionInput{
			ActionType: automation.ActionConfirmShipment, Enabled: true, SortOrder: len(actions) + 1,
		})
		_, _ = s.Store.Automation.Create(ctx, db.AutomationRuleInput{
			UserID: userID, CookieID: row.CookieID, ItemID: res.ItemID,
			Name: "付款后自动发货 - " + title, TriggerType: automation.TriggerOrderPaid,
			Enabled: true, Priority: 100, ConfigJSON: "{}",
			Actions: actions,
		})
	}
	if cfg.ReviewGift.Enabled {
		actions := make([]db.AutomationActionInput, 0, len(cfg.ReviewGift.Actions))
		for index, action := range cfg.ReviewGift.Actions {
			actions = append(actions, db.AutomationActionInput{
				ActionType: automation.ActionSendCard, CardID: action.CardID,
				DeliveryCount: action.DeliveryCount, DelaySeconds: action.DelaySeconds,
				Enabled: true, SortOrder: index + 1,
			})
		}
		_, _ = s.Store.Automation.Create(ctx, db.AutomationRuleInput{
			UserID: userID, CookieID: row.CookieID, ItemID: res.ItemID,
			Name: "评价后发送赠品 - " + title, TriggerType: automation.TriggerBuyerReviewed,
			Enabled: true, Priority: 100, ConfigJSON: "{}",
			Actions: actions,
		})
	}
	if cfg.ReviewRequest.Enabled {
		cfgJSON, _ := json.Marshal(map[string]any{"after_shipped_hours": cfg.ReviewRequest.AfterShippedHours, "max_attempts": cfg.ReviewRequest.MaxAttempts})
		_, _ = s.Store.Automation.Create(ctx, db.AutomationRuleInput{
			UserID: userID, CookieID: row.CookieID, ItemID: res.ItemID,
			Name: "超时未评价求评价 - " + title, TriggerType: automation.TriggerReviewMissingTimeout,
			Enabled: true, Priority: 100, ConfigJSON: string(cfgJSON),
			Actions: []db.AutomationActionInput{
				{ActionType: automation.ActionSendText, MessageTemplate: cfg.ReviewRequest.Message, DelaySeconds: cfg.ReviewRequest.DelaySeconds, Enabled: true, SortOrder: 1},
			},
		})
	}
	return nil
}

func (s *Server) parsePublishRows(ctx context.Context, userID int64, defaultCookieID, uploadDir string, input []map[string]any) []publishBatchParsedRow {
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
			if action.DelaySeconds < 0 {
				errs = append(errs, prefix+"延迟秒不能小于0")
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

func publishBatchToMap(batch *db.ItemPublishBatch, rows []db.ItemPublishBatchRow) map[string]any {
	outRows := make([]map[string]any, 0, len(rows))
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
		var automationCfg publishAutomationConfig
		_ = json.Unmarshal([]byte(row.AutomationJSON), &automationCfg)
		outRows = append(outRows, map[string]any{
			"id": row.ID, "row_no": row.RowNo, "cookie_id": row.CookieID, "title": row.Title,
			"price": row.Price, "quantity": row.Quantity, "images": refs,
			"automation": automationCfg,
			"status":     row.Status, "item_id": row.ItemID, "item_url": row.ItemURL,
			"error_message": row.ErrorMessage,
		})
	}
	return map[string]any{
		"id": batch.ID, "status": batch.Status, "filename": batch.Filename,
		"total": batch.TotalCount, "success": batch.SuccessCount, "failed": batch.FailedCount,
		"pending": pending, "running": running, "rows": outRows,
		"created_at": batch.CreatedAt, "updated_at": batch.UpdatedAt,
	}
}

func (s *Server) cookieOwnedByUser(ctx context.Context, userID int64, cookieID string) bool {
	all, err := s.Store.Cookies.AllForUser(ctx, userID)
	if err != nil {
		return false
	}
	_, ok := all[cookieID]
	return ok
}

func (s *Server) cookieValueForUser(ctx context.Context, userID int64, cookieID string) (string, error) {
	all, err := s.Store.Cookies.AllForUser(ctx, userID)
	if err != nil {
		return "", err
	}
	value, ok := all[cookieID]
	if !ok || strings.TrimSpace(value) == "" {
		return "", errors.New("账号不存在或 Cookie 为空")
	}
	return value, nil
}

func (s *Server) cardOwnedByUser(ctx context.Context, userID int64, cardID int64) bool {
	var exists bool
	err := s.Store.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM cards WHERE id=? AND user_id=?)`, cardID, userID).Scan(&exists)
	return err == nil && exists
}

func (s *Server) publishUploadRoot() string {
	return defaultPublishUploadRoot()
}

func defaultPublishUploadRoot() string {
	if v := strings.TrimSpace(os.Getenv("XIANYU_UPLOAD_DIR")); v != "" {
		return v
	}
	return filepath.Join("data", "uploads")
}

func parseLooseBool(raw string) bool {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch raw {
	case "1", "true", "yes", "y", "on", "是", "开启", "启用":
		return true
	default:
		return false
	}
}

func atoiPublishDefault(raw string, def int) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def
	}
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		return int(f)
	}
	return def
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
