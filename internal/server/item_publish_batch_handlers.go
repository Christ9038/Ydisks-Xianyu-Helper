package server

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
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
	r.Body = http.MaxBytesReader(w, r.Body, 224<<20)
	// #nosec G120 -- 请求体已由 MaxBytesReader 限制为 224 MiB。
	if err := r.ParseMultipartForm(256 << 20); err != nil {
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
	sourceBytes, err := io.ReadAll(io.LimitReader(source, 20<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "读取商品表格失败")
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
		zipBytes, err := io.ReadAll(io.LimitReader(zipFile, 200<<20))
		if err != nil {
			writeErr(w, http.StatusBadRequest, "读取图片 zip 失败")
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

	maps, err := parsePublishSheetBytes(sourceBytes, sourceName)
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
		defer cancel()
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
		defer cancel()
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
	client := s.MTop
	if client == nil {
		client = &mtop.Client{}
	}
	for idx, row := range rows {
		status, err := s.Store.PublishBatches.BatchStatus(ctx, batchID)
		if err != nil || status == "canceled" {
			_ = s.Store.PublishBatches.Recount(ctx, batchID)
			return
		}
		_ = s.Store.PublishBatches.MarkRowRunning(ctx, row.ID)
		if err := s.publishBatchRow(ctx, userID, client, row); err != nil {
			_ = s.Store.PublishBatches.MarkRowFailed(ctx, row.ID, err.Error())
		}
		_ = s.Store.PublishBatches.Recount(ctx, batchID)
		if idx < len(rows)-1 {
			delay := time.Duration(10+idx%21) * time.Second
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
	_ = s.Store.PublishBatches.Recount(ctx, batchID)
	batch, err := s.Store.PublishBatches.Get(ctx, userID, batchID)
	if err == nil && batch.Status != "canceled" {
		finalStatus := "completed"
		if batch.SuccessCount == 0 && batch.FailedCount > 0 {
			finalStatus = "failed"
		}
		_ = s.Store.PublishBatches.SetBatchStatus(ctx, batchID, finalStatus)
	}
}

func (s *Server) publishBatchRow(ctx context.Context, userID int64, client *mtop.Client, row db.ItemPublishBatchRow) error {
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
	if res.UpdatedCookies != "" && res.UpdatedCookies != cookieValue {
		_ = s.Store.Cookies.Save(ctx, row.CookieID, res.UpdatedCookies, userID)
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
		_ = s.Store.Items.Upsert(ctx, &db.ItemInfoRow{
			CookieID:              row.CookieID,
			ItemID:                res.ItemID,
			ItemTitle:             firstNonEmpty(res.Title, row.Title),
			ItemDescription:       row.Description,
			ItemCategory:          res.CategoryID,
			ItemPrice:             res.PriceText,
			ItemDetail:            string(detailJSON),
			MultiQuantityDelivery: row.Quantity > 1,
		})
		_ = s.createPublishAutomationRules(ctx, userID, row, res)
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

func parsePublishSheetBytes(raw []byte, filename string) ([]map[string]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("导入内容为空")
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".xlsx":
		return parseXLSXPublishSheet(raw)
	case ".csv":
		return parseDelimitedPublishSheet(raw, ',')
	case ".tsv":
		return parseDelimitedPublishSheet(raw, '\t')
	case ".xls":
		return nil, fmt.Errorf("暂不支持旧版 .xls，请另存为 .xlsx 或 CSV 后导入")
	default:
		return parseDelimitedPublishSheet(raw, ',')
	}
}

func parseDelimitedPublishSheet(raw []byte, comma rune) ([]map[string]any, error) {
	reader := csv.NewReader(bytes.NewReader(raw))
	reader.Comma = comma
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("解析表格失败: %w", err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("表格至少需要表头和一行数据")
	}
	return rowsToPublishMaps(records[0], records[1:]), nil
}

func parseXLSXPublishSheet(raw []byte) ([]map[string]any, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("解析 xlsx 失败: %w", err)
	}
	shared := xlsxSharedStrings(zr)
	var sheet *zip.File
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			sheet = f
			break
		}
	}
	if sheet == nil {
		return nil, fmt.Errorf("xlsx 中未找到工作表")
	}
	rc, err := sheet.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var ws xlsxWorksheet
	if err := xml.NewDecoder(rc).Decode(&ws); err != nil {
		return nil, fmt.Errorf("解析工作表失败: %w", err)
	}
	rows := make([][]string, 0, len(ws.SheetData.Rows))
	for _, row := range ws.SheetData.Rows {
		values := []string{}
		for _, cell := range row.Cells {
			idx := xlsxCellIndex(cell.Ref)
			for len(values) <= idx {
				values = append(values, "")
			}
			values[idx] = xlsxCellValue(cell, shared)
		}
		rows = append(rows, values)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("xlsx 至少需要表头和一行数据")
	}
	return rowsToPublishMaps(rows[0], rows[1:]), nil
}

func rowsToPublishMaps(headers []string, rows [][]string) []map[string]any {
	keys := make([]string, len(headers))
	for i, h := range headers {
		keys[i] = normalizePublishHeader(h)
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		m := map[string]any{}
		nonEmpty := false
		for i, key := range keys {
			if key == "" || i >= len(row) {
				continue
			}
			value := strings.TrimSpace(row[i])
			if value != "" {
				nonEmpty = true
			}
			m[key] = value
		}
		if nonEmpty {
			out = append(out, m)
		}
	}
	return out
}

func normalizePublishHeader(header string) string {
	h := strings.ToLower(strings.TrimSpace(header))
	h = strings.NewReplacer(" ", "", "_", "", "-", "", "（", "(", "）", ")").Replace(h)
	switch h {
	case "cookieid", "账号id", "账号", "闲鱼账号":
		return "cookie_id"
	case "title", "itemtitle", "标题", "商品标题", "商品名称":
		return "title"
	case "description", "desc", "itemdescription", "描述", "商品描述", "商品详情":
		return "description"
	case "price", "itemprice", "价格", "商品价格":
		return "price"
	case "originalprice", "原价":
		return "original_price"
	case "quantity", "库存", "数量":
		return "quantity"
	case "postagemode", "邮费模式":
		return "postage_mode"
	case "postage", "邮费":
		return "postage"
	case "images", "image", "图片", "商品图片":
		return "images"
	case "paiddeliveryenabled", "付款发货启用", "付款后自动发货":
		return "paid_delivery_enabled"
	case "paiddeliverycontents", "付款发货内容", "付款后发送的卡密":
		return "paid_delivery_contents"
	case "reviewgiftenabled", "评价赠品启用", "评价后发送赠品":
		return "review_gift_enabled"
	case "reviewgiftcontents", "评价赠品内容", "评价后发送的卡密":
		return "review_gift_contents"
	case "reviewrequestenabled", "求评价启用", "超时未评价时提醒":
		return "review_request_enabled"
	case "reviewrequestafterhours", "求评价等待小时", "发货几小时后提醒":
		return "review_request_after_hours"
	case "reviewrequestmessage", "求评价文案", "提醒内容":
		return "review_request_message"
	case "reviewrequestmaxattempts", "求评价最多次数", "最多提醒几次":
		return "review_request_max_attempts"
	case "reviewrequestdelayseconds", "求评价延迟秒":
		return "review_request_delay_seconds"
	default:
		return strings.TrimSpace(header)
	}
}

func extractPublishImagesZip(raw []byte, dest string) error {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return fmt.Errorf("解析图片 zip 失败: %w", err)
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rel, err := safeZipPath(f.Name)
		if err != nil {
			return err
		}
		root, err := os.OpenRoot(dest)
		if err != nil {
			return err
		}
		if err := root.MkdirAll(filepath.Dir(rel), 0o750); err != nil {
			_ = root.Close()
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		data, err := io.ReadAll(io.LimitReader(rc, 10<<20))
		_ = rc.Close()
		if err != nil {
			return err
		}
		if len(data) == 0 {
			continue
		}
		if !strings.HasPrefix(http.DetectContentType(data), "image/") {
			continue
		}
		file, err := root.OpenFile(rel, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err == nil {
			_, err = file.Write(data)
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
		}
		_ = root.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func loadBatchPublishImages(ctx context.Context, uploadDir string, row db.ItemPublishBatchRow) ([]mtop.PublishImage, error) {
	var refs []string
	if err := json.Unmarshal([]byte(row.ImagesJSON), &refs); err != nil {
		return nil, fmt.Errorf("图片字段格式错误")
	}
	if len(refs) == 0 {
		return nil, errors.New("至少上传 1 张商品图片")
	}
	images := make([]mtop.PublishImage, 0, len(refs))
	for _, ref := range refs {
		var data []byte
		var contentType string
		var filename string
		var err error
		if isHTTPURL(ref) {
			data, contentType, err = downloadImageURL(ctx, ref)
			filename = pathBaseFromURL(ref)
		} else {
			data, contentType, filename, err = readBatchImageFile(uploadDir, ref)
		}
		if err != nil {
			return nil, err
		}
		images = append(images, mtop.PublishImage{Filename: filename, ContentType: contentType, Data: data})
	}
	return images, nil
}

func readBatchImageFile(uploadDir, ref string) ([]byte, string, string, error) {
	rel, err := safeZipPath(ref)
	if err != nil {
		return nil, "", "", err
	}
	root, err := os.OpenRoot(uploadDir)
	if err != nil {
		return nil, "", "", fmt.Errorf("打开图片目录失败")
	}
	defer root.Close()
	file, err := root.Open(rel)
	if err != nil {
		return nil, "", "", fmt.Errorf("读取图片失败: %s", ref)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (10<<20)+1))
	if err != nil || len(data) > 10<<20 {
		return nil, "", "", fmt.Errorf("读取图片失败或文件过大: %s", ref)
	}
	if len(data) == 0 {
		return nil, "", "", fmt.Errorf("图片文件为空: %s", ref)
	}
	contentType := http.DetectContentType(data)
	if !strings.HasPrefix(contentType, "image/") {
		return nil, "", "", fmt.Errorf("不是有效图片: %s", ref)
	}
	return data, contentType, filepath.Base(rel), nil
}

// writeFileWithinRoot 将上传文件限制在指定根目录内，拒绝符号链接和路径逃逸。
func writeFileWithinRoot(rootDir, name string, data []byte) error {
	root, err := os.OpenRoot(rootDir)
	if err != nil {
		return err
	}
	defer root.Close()
	file, err := root.OpenFile(name, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func downloadImageURL(ctx context.Context, rawURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") || req.URL.Hostname() == "" {
		return nil, "", fmt.Errorf("图片 URL 无效: %s", rawURL)
	}
	client := publicHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("下载图片失败: %s", rawURL)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("下载图片失败: %s HTTP %d", rawURL, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (10<<20)+1))
	if err != nil {
		return nil, "", fmt.Errorf("读取远程图片失败: %s", rawURL)
	}
	if len(data) > 10<<20 {
		return nil, "", fmt.Errorf("远程图片不能超过 10 MiB: %s", rawURL)
	}
	contentType := resp.Header.Get("Content-Type")
	if i := strings.Index(contentType, ";"); i >= 0 {
		contentType = contentType[:i]
	}
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = http.DetectContentType(data)
	}
	if !strings.HasPrefix(contentType, "image/") {
		return nil, "", fmt.Errorf("远程文件不是图片: %s", rawURL)
	}
	return data, contentType, nil
}

// publicHTTPClient 只允许连接公网地址，防止批量铺货图片 URL 访问本机或内网服务。
func publicHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, resolved := range ips {
				if isPublicIP(resolved.IP) {
					return dialer.DialContext(ctx, network, net.JoinHostPort(resolved.IP.String(), port))
				}
			}
			return nil, fmt.Errorf("拒绝访问非公网地址")
		},
		TLSHandshakeTimeout: 10 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("图片 URL 重定向次数过多")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("图片 URL 重定向协议不安全")
			}
			return nil
		},
	}
}

func isPublicIP(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() && !ip.IsUnspecified() && !ip.IsMulticast()
}

func validateBatchImageRef(uploadDir, ref string) error {
	if isHTTPURL(ref) {
		return nil
	}
	rel, err := safeZipPath(ref)
	if err != nil {
		return err
	}
	path := filepath.Join(uploadDir, rel)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return fmt.Errorf("图片文件不存在: %s", ref)
	}
	return nil
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

func (s *Server) cardOwnedByUser(ctx context.Context, userID, cardID int64) bool {
	var exists int
	err := s.Store.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM cards WHERE id=? AND user_id=?)`, cardID, userID).Scan(&exists)
	return err == nil && exists != 0
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

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(buf)
}

func safeBaseName(name string) string {
	name = strings.TrimSpace(filepath.Base(name))
	name = strings.ReplaceAll(name, string(os.PathSeparator), "_")
	if name == "." || name == "/" {
		return ""
	}
	return name
}

func safeZipPath(raw string) (string, error) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if raw == "" || strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("图片路径不安全: %s", raw)
	}
	clean := filepath.Clean(raw)
	if clean == "." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) || clean == ".." {
		return "", fmt.Errorf("图片路径不安全: %s", raw)
	}
	return clean, nil
}

func splitImageRefs(raw string) []string {
	raw = strings.ReplaceAll(raw, "\n", ";")
	raw = strings.ReplaceAll(raw, "；", ";")
	parts := strings.Split(raw, ";")
	out := []string{}
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
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

func isHTTPURL(ref string) bool {
	ref = strings.ToLower(strings.TrimSpace(ref))
	return strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://")
}

func pathBaseFromURL(rawURL string) string {
	base := filepath.Base(strings.Split(rawURL, "?")[0])
	if base == "." || base == "/" || base == "" {
		exts, _ := mime.ExtensionsByType(http.DetectContentType(nil))
		if len(exts) > 0 {
			return "image" + exts[0]
		}
		return "image.jpg"
	}
	return base
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
