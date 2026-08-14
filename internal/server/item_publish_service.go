package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

var (
	// errItemPublishBatchNotFound 表示当前用户无法访问指定批量任务。
	errItemPublishBatchNotFound = errors.New("商品批量任务不存在")
	// errItemPublishBatchConflict 表示批量任务的租约或状态发生并发冲突。
	errItemPublishBatchConflict = errors.New("商品批量任务状态冲突")
	// errItemPublishBatchInvalidState 表示批量任务当前状态不允许执行请求操作。
	errItemPublishBatchInvalidState = errors.New("商品批量任务状态不允许执行该操作")
	// errItemPublishBatchNoRows 表示批量任务没有可继续处理的明细行。
	errItemPublishBatchNoRows = errors.New("商品批量任务没有可处理的明细行")
)

// itemPublishInput 是单个商品发布用例的业务输入，不包含 HTTP 请求对象。
type itemPublishInput struct {
	// UserID 是发起发布操作的用户标识。
	UserID int64
	// CookieID 是执行商品发布的账号标识。
	CookieID string
	// Title 是商品标题。
	Title string
	// Description 是商品描述。
	Description string
	// PriceCents 是商品售价，单位为分。
	PriceCents int64
	// OriginalPriceCents 是商品原价，单位为分。
	OriginalPriceCents int64
	// Quantity 是商品库存数量。
	Quantity int
	// PostageMode 是邮费模式。
	PostageMode string
	// PostageCents 是邮费金额，单位为分。
	PostageCents int64
	// Location 是可选的发货地。
	Location *mtop.PublishLocation
	// Images 是待上传的商品图片。
	Images []mtop.PublishImage
}

// itemPublishOutcome 是单个商品发布用例的结果及发布后的持久化风险。
type itemPublishOutcome struct {
	// Result 是平台返回的商品信息。
	Result *mtop.PublishItemResult
	// ResponseCookieErr 是平台响应 Cookie 未能持久化时的错误。
	ResponseCookieErr error
	// LocalSaveErr 是平台成功后本地商品落库失败时的错误。
	LocalSaveErr error
}

// itemPublishPreviewInput 是批量发布预检结果持久化所需的业务输入。
type itemPublishPreviewInput struct {
	// UserID 是创建批次的用户标识。
	UserID int64
	// BatchID 是预检批次标识。
	BatchID string
	// DefaultCookieID 是未指定账号行使用的默认账号。
	DefaultCookieID string
	// Filename 是用户上传的表格文件名。
	Filename string
	// UploadDir 是批次图片和表格的受控目录。
	UploadDir string
	// Location 是批次统一发货地。
	Location mtop.PublishLocation
	// Rows 是解析并校验后的商品行。
	Rows []publishBatchParsedRow
}

// itemPublishService 是商品发布应用服务，隔离 HTTP 适配与业务状态变更。
type itemPublishService struct {
	// server 提供数据库、MTOP 客户端、运行时 Cookie 和 worker 生命周期依赖。
	server *Server
}

// itemPublishApplication 返回当前 Server 绑定的商品发布应用服务。
func (s *Server) itemPublishApplication() *itemPublishService {
	return s.applicationServiceSet().itemPublish
}

// PublishSingle 执行单商品发布、响应 Cookie 持久化和本地商品落库。
func (svc *itemPublishService) PublishSingle(ctx context.Context, input itemPublishInput) (itemPublishOutcome, error) {
	// s 是当前商品发布应用服务依赖的 Server。
	s := svc.server
	// unlock 释放当前账号的凭证串行化锁。
	unlock := s.Store.LockAccountCredentials(input.CookieID)
	// err 和 latest 保存账号平台详情查询结果。
	latest, err := s.loadCookiePlatformDetail(ctx, input.CookieID)
	if err != nil || latest == nil || latest.UserID != input.UserID || !hasStoredCookieCredential(latest) {
		unlock()
		return itemPublishOutcome{}, errors.New("账号凭证已变化，请重试")
	}
	// cookieValue 是本次 MTOP 调用使用的账号凭证。
	cookieValue := latest.Value
	// requestCtx 和 cancel 控制单商品发布的最长执行时间。
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	// client 是平台发布客户端。
	client := s.mtopClient()
	// mtopCtx 和 cookieSession 保存带 Cookie 快照的平台调用上下文。
	mtopCtx, cookieSession := withMTopCookieSnapshot(requestCtx, latest)
	// result 和 callErr 是平台发布结果及调用错误。
	result, callErr := client.PublishItem(mtopCtx, cookieValue, mtop.PublishItemRequest{
		Title: input.Title, Description: input.Description, PriceCents: input.PriceCents,
		OriginalPriceCents: input.OriginalPriceCents, Quantity: input.Quantity,
		PostageMode: input.PostageMode, PostageCents: input.PostageCents, Virtual: true,
		Location: input.Location, Images: input.Images,
	})
	// runtimeCookie 是需要同步到运行时账号的刷新凭证。
	runtimeCookie := ""
	// value、valueChanged、handled 和 persistErr 描述 Cookie Jar 持久化结果。
	value, valueChanged, handled, persistErr := s.persistMTopCookieSessionLocked(ctx, latest, cookieSession)
	if persistErr != nil {
		if s.Logger != nil {
			s.Logger.Error("保存发布响应 Cookie Jar 失败", "cookie_id", input.CookieID, "err", persistErr)
		}
	} else if handled && valueChanged {
		runtimeCookie = value
	} else if !handled && callErr == nil && result != nil && result.UpdatedCookies != "" && result.UpdatedCookies != cookieValue {
		// saveErr 表示保存平台返回新 Cookie 的错误。
		if saveErr := s.Store.Cookies.UpdateValueOwned(ctx, input.CookieID, result.UpdatedCookies, input.UserID); saveErr != nil {
			persistErr = saveErr
		} else {
			runtimeCookie = result.UpdatedCookies
		}
	}
	unlock()
	if runtimeCookie != "" {
		s.updateRunningCookie(ctx, input.CookieID, runtimeCookie)
	}
	if callErr != nil {
		if persistErr != nil {
			callErr = errors.Join(callErr, fmt.Errorf("保存发布响应 Cookie: %w", persistErr))
		}
		s.recoverExpiredMTOPSession(ctx, input.CookieID, callErr)
		return itemPublishOutcome{ResponseCookieErr: persistErr}, callErr
	}
	if result == nil || strings.TrimSpace(result.ItemID) == "" {
		return itemPublishOutcome{Result: result, ResponseCookieErr: persistErr}, nil
	}
	// detail 保存平台商品附加信息。
	detail := map[string]any{"item_image": result.ImageURL, "web_url": result.ItemURL, "category_name": result.CategoryName, "quantity": result.Quantity, "publish_raw": result.RawData}
	// detailJSON 是本地商品详情 JSON。
	detailJSON, _ := json.Marshal(detail)
	// localErr 表示平台发布成功后本地商品落库的错误。
	localErr := s.Store.Items.Upsert(ctx, &db.ItemInfoRow{
		CookieID: input.CookieID, ItemID: result.ItemID, ItemTitle: result.Title,
		ItemDescription: input.Description, ItemCategory: result.CategoryID,
		ItemPrice: result.PriceText, ItemDetail: string(detailJSON), MultiQuantityDelivery: input.Quantity > 1,
	})
	return itemPublishOutcome{Result: result, ResponseCookieErr: persistErr, LocalSaveErr: localErr}, nil
}

// RecommendCategory 调用平台类目推荐并持久化刷新后的账号登录状态。
func (svc *itemPublishService) RecommendCategory(ctx context.Context, userID int64, cookieID, keyword string) (mtop.PublishCategory, error) {
	// s 是当前商品发布应用服务依赖的 Server。
	s := svc.server
	// recommender 和 ok 表示 MTOP 客户端是否支持类目推荐。
	recommender, ok := s.mtopClient().(publishCategoryRecommender)
	if !ok {
		return mtop.PublishCategory{}, errors.New("当前 MTOP 客户端不支持类目推荐")
	}
	// unlock 释放当前账号的凭证串行化锁。
	unlock := s.Store.LockAccountCredentials(cookieID)
	// runtimeCookie 是需要在释放凭证锁后同步的刷新凭证。
	runtimeCookie := ""
	defer func() {
		unlock()
		if runtimeCookie != "" {
			s.updateRunningCookie(context.Background(), cookieID, runtimeCookie)
		}
	}()
	// err 和 latest 保存账号平台详情查询结果。
	latest, err := s.loadCookiePlatformDetail(ctx, cookieID)
	if err != nil || latest == nil || latest.UserID != userID || !hasStoredCookieCredential(latest) {
		return mtop.PublishCategory{}, errors.New("账号凭证已变化，请重试")
	}
	// requestCtx 和 cancel 控制类目推荐的最长执行时间。
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// mtopCtx 和 cookieSession 保存带 Cookie 快照的平台上下文。
	mtopCtx, cookieSession := withMTopCookieSnapshot(requestCtx, latest)
	// category、updatedCookies 和 callErr 是平台推荐结果、刷新凭证及调用错误。
	category, updatedCookies, callErr := recommender.RecommendPublishCategory(mtopCtx, latest.Value, keyword)
	// value、valueChanged、handled 和 persistErr 描述 Cookie Jar 持久化结果。
	value, valueChanged, handled, persistErr := s.persistMTopCookieSessionLocked(ctx, latest, cookieSession)
	if persistErr != nil {
		return mtop.PublishCategory{}, fmt.Errorf("保存账号登录状态失败: %w", persistErr)
	}
	if !handled && updatedCookies != "" && updatedCookies != latest.Value {
		// err 表示保存平台返回新 Cookie 的错误。
		if err := s.Store.Cookies.UpdateValueOwned(ctx, cookieID, updatedCookies, userID); err != nil {
			return mtop.PublishCategory{}, fmt.Errorf("保存账号登录状态失败: %w", err)
		}
		runtimeCookie = updatedCookies
	} else if handled && valueChanged {
		runtimeCookie = value
	}
	if callErr != nil {
		return mtop.PublishCategory{}, callErr
	}
	return category, nil
}

// PersistPreview 将预检行转换为数据库批次并返回兼容旧接口的预检 DTO。
func (svc *itemPublishService) PersistPreview(ctx context.Context, input itemPublishPreviewInput) (itemPublishBatchPreviewResponse, error) {
	// rows 是待写入数据库的批量明细行。
	rows := make([]db.ItemPublishBatchRow, 0, len(input.Rows))
	// previewRows 是返回给前端的逐行预检视图。
	previewRows := make([]publishBatchPreviewRow, 0, len(input.Rows))
	// valid 和 invalid 记录预检通过与失败的行数。
	valid, invalid := 0, 0
	// parsed 是当前正在转换的预检行。
	for _, parsed := range input.Rows {
		// isValid 表示当前行是否没有校验错误。
		isValid := len(parsed.Errors) == 0
		if isValid {
			valid++
		} else {
			invalid++
		}
		// imagesJSON、categoryJSON、automationJSON 和 rawJSON 是行数据的 JSON 表示。
		imagesJSON, _ := json.Marshal(parsed.Images)
		// categoryJSON 是商品类目的 JSON 表示。
		categoryJSON, _ := json.Marshal(parsed.Category)
		// automationJSON 是发布后自动化配置的 JSON 表示。
		automationJSON, _ := json.Marshal(parsed.Automation)
		// rawJSON 是原始导入行的 JSON 表示。
		rawJSON, _ := json.Marshal(parsed.Raw)
		// status、errorMessage 和 failureKind 保存数据库状态及失败分类。
		status, errorMessage, failureKind := "pending", "", ""
		if !isValid {
			status, errorMessage, failureKind = "failed", strings.Join(parsed.Errors, "；"), "validation"
		}
		rows = append(rows, db.ItemPublishBatchRow{RowNo: parsed.RowNo, CookieID: parsed.CookieID, Title: parsed.Title, Description: parsed.Description, Price: parsed.Price, OriginalPrice: parsed.OriginalPrice, Quantity: parsed.Quantity, PostageMode: parsed.PostageMode, Postage: parsed.Postage, ImagesJSON: string(imagesJSON), CategoryJSON: string(categoryJSON), AutomationJSON: string(automationJSON), Status: status, ErrorMessage: errorMessage, FailureKind: failureKind, RawJSON: string(rawJSON)})
		previewRows = append(previewRows, publishBatchPreviewRow{RowNo: parsed.RowNo, Valid: isValid, Errors: parsed.Errors, CookieID: parsed.CookieID, Title: parsed.Title, Price: parsed.Price, Quantity: parsed.Quantity, Images: parsed.Images, Category: parsed.Category, Automation: parsed.Automation})
	}
	if len(rows) == 0 {
		return itemPublishBatchPreviewResponse{}, errItemPublishBatchNoRows
	}
	// locationBytes 是批次发货地配置的 JSON 表示。
	locationBytes, _ := json.Marshal(input.Location)
	// err 表示批次及其明细的事务写入错误。
	if err := svc.server.Store.PublishBatches.Create(ctx, &db.ItemPublishBatch{ID: input.BatchID, UserID: input.UserID, DefaultCookieID: input.DefaultCookieID, Filename: input.Filename, UploadDir: input.UploadDir, LocationJSON: string(locationBytes), Status: "preview"}, rows); err != nil {
		return itemPublishBatchPreviewResponse{}, fmt.Errorf("保存预检结果失败: %w", err)
	}
	_ = svc.server.Store.PublishBatches.Recount(ctx, input.BatchID)
	return itemPublishBatchPreviewResponse{Success: true, PreviewID: input.BatchID, Total: len(rows), Valid: valid, Invalid: invalid, Rows: previewRows}, nil
}

// StartBatch 校验并声明批量任务租约，然后启动后台发布 worker。
func (svc *itemPublishService) StartBatch(ctx context.Context, userID int64, batchID string) (string, error) {
	// s 是当前商品发布应用服务依赖的 Server。
	s := svc.server
	// batch 和 err 保存批次查询结果。
	batch, err := s.Store.PublishBatches.Get(ctx, userID, batchID)
	if err != nil {
		return "", errItemPublishBatchNotFound
	}
	if batch.Status == "running" && batch.LeaseExpiresAt > time.Now().UTC().Unix() {
		return "", errItemPublishBatchConflict
	}
	if batch.Status != "preview" && batch.Status != "pending" && batch.Status != "completed" && batch.Status != "running" {
		return "", errItemPublishBatchInvalidState
	}
	// token 是本次 worker 租约的随机凭证。
	token := randomHex(16)
	// claimed 和 err 保存批次租约声明结果。
	claimed, err := s.Store.PublishBatches.ClaimBatch(ctx, batch.ID, token, time.Now().UTC().Add(publishBatchLease).Unix())
	if err != nil {
		return "", fmt.Errorf("启动任务失败: %w", err)
	}
	if !claimed {
		return "", errItemPublishBatchConflict
	}
	// pending 和 err 保存待处理明细查询结果。
	pending, err := s.Store.PublishBatches.PendingRows(ctx, batch.ID, false)
	if err != nil {
		s.failClaimedPublishBatch(batch.ID, token)
		return "", fmt.Errorf("读取任务失败: %w", err)
	}
	if len(pending) == 0 {
		_, _, _ = s.Store.PublishBatches.FinalizeBatch(ctx, batch.ID, token)
		return "", errItemPublishBatchNoRows
	}
	s.startPublishBatchWorker(s.lifecycleContext(), userID, batch.ID, token)
	return batch.ID, nil
}

// ListBatches 查询用户的批量任务并映射为 HTTP 无关的响应 DTO。
func (svc *itemPublishService) ListBatches(ctx context.Context, userID int64, limit int) ([]itemPublishBatchResponse, error) {
	// batches 和 err 保存用户批次列表查询结果。
	batches, err := svc.server.Store.PublishBatches.ListForUser(ctx, userID, limit)
	if err != nil {
		return nil, err
	}
	// result 是批次列表响应视图。
	result := make([]itemPublishBatchResponse, 0, len(batches))
	// i 是当前批次在查询结果中的索引。
	for i := range batches {
		result = append(result, publishBatchToMap(&batches[i], nil))
	}
	return result, nil
}

// GetBatch 查询用户拥有的批量任务及其明细行。
func (svc *itemPublishService) GetBatch(ctx context.Context, userID int64, batchID string) (itemPublishBatchResponse, error) {
	// batch 和 err 保存批次查询结果。
	batch, err := svc.server.Store.PublishBatches.Get(ctx, userID, batchID)
	if err != nil {
		return itemPublishBatchResponse{}, errItemPublishBatchNotFound
	}
	// rows 和 err 保存批次明细查询结果。
	rows, err := svc.server.Store.PublishBatches.Rows(ctx, batch.ID)
	if err != nil {
		return itemPublishBatchResponse{}, err
	}
	return publishBatchToMap(batch, rows), nil
}

// CancelBatch 请求批量任务取消并通知内存中的 worker。
func (svc *itemPublishService) CancelBatch(ctx context.Context, userID int64, batchID string) (string, error) {
	// s 是当前商品发布应用服务依赖的 Server。
	s := svc.server
	// err 表示批次所有权查询错误。
	if _, err := s.Store.PublishBatches.Get(ctx, userID, batchID); err != nil {
		return "", errItemPublishBatchNotFound
	}
	// token、running 和 err 保存取消请求及当前 worker 状态。
	token, running, err := s.Store.PublishBatches.RequestCancel(ctx, batchID)
	if err != nil {
		if errors.Is(err, db.ErrPublishBatchChanged) {
			return "", errItemPublishBatchConflict
		}
		return "", err
	}
	if running {
		s.cancelPublishBatch(batchID, token)
	}
	if running {
		return "canceling", nil
	}
	return "canceled", nil
}

// DeleteBatch 删除非运行中的批量任务并返回其上传目录供适配层清理。
func (svc *itemPublishService) DeleteBatch(ctx context.Context, userID int64, batchID string) (string, error) {
	// s 是当前商品发布应用服务依赖的 Server。
	s := svc.server
	// batch 和 err 保存批次查询结果。
	batch, err := s.Store.PublishBatches.Get(ctx, userID, batchID)
	if err != nil {
		return "", errItemPublishBatchNotFound
	}
	if batch.Status == "running" || batch.Status == "canceling" {
		return "", errItemPublishBatchConflict
	}
	// err 表示批次删除错误。
	if err := s.Store.PublishBatches.Delete(ctx, userID, batchID); err != nil {
		return "", err
	}
	return batch.UploadDir, nil
}

// RetryFailedBatch 重置失败明细并启动仅处理失败项的后台 worker。
func (svc *itemPublishService) RetryFailedBatch(ctx context.Context, userID int64, batchID string) (string, error) {
	// s 是当前商品发布应用服务依赖的 Server。
	s := svc.server
	// batch 和 err 保存批次查询结果。
	batch, err := s.Store.PublishBatches.Get(ctx, userID, batchID)
	if err != nil {
		return "", errItemPublishBatchNotFound
	}
	if batch.Status == "running" && batch.LeaseExpiresAt > time.Now().UTC().Unix() {
		return "", errItemPublishBatchConflict
	}
	// token 是本次重试 worker 的租约凭证。
	token := randomHex(16)
	// claimed 和 err 保存重试租约声明结果。
	claimed, err := s.Store.PublishBatches.ClaimBatch(ctx, batchID, token, time.Now().UTC().Add(publishBatchLease).Unix())
	if err != nil {
		return "", err
	}
	if !claimed {
		return "", errItemPublishBatchConflict
	}
	// err 表示重置失败明细的错误。
	if err := s.Store.PublishBatches.ResetFailed(ctx, batchID); err != nil {
		s.failClaimedPublishBatch(batchID, token)
		return "", err
	}
	_ = s.Store.PublishBatches.Recount(ctx, batchID)
	// pending 和 err 保存可重试明细查询结果。
	pending, err := s.Store.PublishBatches.PendingRows(ctx, batchID, false)
	if err != nil {
		s.failClaimedPublishBatch(batchID, token)
		return "", err
	}
	if len(pending) == 0 {
		_, _, _ = s.Store.PublishBatches.FinalizeBatch(ctx, batchID, token)
		return "", errItemPublishBatchNoRows
	}
	s.startPublishBatchWorker(s.lifecycleContext(), userID, batchID, token)
	return batchID, nil
}
