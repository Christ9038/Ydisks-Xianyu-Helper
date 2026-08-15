package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	itemapp "xianyu-go/internal/application/items"
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
	// repository 提供商品发布用例所需的最小持久化能力。
	repository itemPublishRepository
}

// itemPublishApplication 返回当前 Server 绑定的商品发布应用服务。
func (s *Server) itemPublishApplication() *itemPublishService {
	return s.applicationServiceSet().itemPublish
}

// itemSinglePublishApplication 返回只负责单商品发布用例的应用服务。
func (s *Server) itemSinglePublishApplication() *itemapp.Service {
	return s.applicationServiceSet().itemSinglePublish
}

// itemPublishRepositoryForServer 返回当前 Server 装配的发布持久化边界。
func (s *Server) itemPublishRepositoryForServer() itemPublishRepository {
	return s.itemPublishApplication().repository
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
	unlock := svc.repository.LockCredentials(cookieID)
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
		if err := svc.repository.UpdateCookieValueOwned(ctx, cookieID, updatedCookies, userID); err != nil {
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
	if err := svc.repository.CreateBatch(ctx, &db.ItemPublishBatch{ID: input.BatchID, UserID: input.UserID, DefaultCookieID: input.DefaultCookieID, Filename: input.Filename, UploadDir: input.UploadDir, LocationJSON: string(locationBytes), Status: "preview"}, rows); err != nil {
		return itemPublishBatchPreviewResponse{}, fmt.Errorf("保存预检结果失败: %w", err)
	}
	_ = svc.repository.RecountBatch(ctx, input.BatchID)
	return itemPublishBatchPreviewResponse{Success: true, PreviewID: input.BatchID, Total: len(rows), Valid: valid, Invalid: invalid, Rows: previewRows}, nil
}

// StartBatch 校验并声明批量任务租约，然后启动后台发布 worker。
func (svc *itemPublishService) StartBatch(ctx context.Context, userID int64, batchID string) (string, error) {
	// s 是当前商品发布应用服务依赖的 Server。
	s := svc.server
	// batch 和 err 保存批次查询结果。
	batch, err := svc.repository.GetBatch(ctx, userID, batchID)
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
	claimed, err := svc.repository.ClaimBatch(ctx, batch.ID, token, time.Now().UTC().Add(publishBatchLease).Unix())
	if err != nil {
		return "", fmt.Errorf("启动任务失败: %w", err)
	}
	if !claimed {
		return "", errItemPublishBatchConflict
	}
	// pending 和 err 保存待处理明细查询结果。
	pending, err := svc.repository.PendingRows(ctx, batch.ID, false)
	if err != nil {
		s.failClaimedPublishBatch(batch.ID, token)
		return "", fmt.Errorf("读取任务失败: %w", err)
	}
	if len(pending) == 0 {
		_, _, _ = svc.repository.FinalizeBatch(ctx, batch.ID, token)
		return "", errItemPublishBatchNoRows
	}
	s.startPublishBatchWorker(s.lifecycleContext(), userID, batch.ID, token)
	return batch.ID, nil
}

// ListBatches 查询用户的批量任务并映射为 HTTP 无关的响应 DTO。
func (svc *itemPublishService) ListBatches(ctx context.Context, userID int64, limit int) ([]itemPublishBatchResponse, error) {
	// batches 和 err 保存用户批次列表查询结果。
	batches, err := svc.repository.ListBatchesForUser(ctx, userID, limit)
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
	batch, err := svc.repository.GetBatch(ctx, userID, batchID)
	if err != nil {
		return itemPublishBatchResponse{}, errItemPublishBatchNotFound
	}
	// rows 和 err 保存批次明细查询结果。
	rows, err := svc.repository.ListBatchRows(ctx, batch.ID)
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
	if _, err := svc.repository.GetBatch(ctx, userID, batchID); err != nil {
		return "", errItemPublishBatchNotFound
	}
	// token、running 和 err 保存取消请求及当前 worker 状态。
	token, running, err := svc.repository.RequestCancel(ctx, batchID)
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
	// batch 和 err 保存批次查询结果。
	batch, err := svc.repository.GetBatch(ctx, userID, batchID)
	if err != nil {
		return "", errItemPublishBatchNotFound
	}
	if batch.Status == "running" || batch.Status == "canceling" {
		return "", errItemPublishBatchConflict
	}
	// err 表示批次删除错误。
	if err := svc.repository.DeleteBatch(ctx, userID, batchID); err != nil {
		return "", err
	}
	return batch.UploadDir, nil
}

// RetryFailedBatch 重置失败明细并启动仅处理失败项的后台 worker。
func (svc *itemPublishService) RetryFailedBatch(ctx context.Context, userID int64, batchID string) (string, error) {
	// s 是当前商品发布应用服务依赖的 Server。
	s := svc.server
	// batch 和 err 保存批次查询结果。
	batch, err := svc.repository.GetBatch(ctx, userID, batchID)
	if err != nil {
		return "", errItemPublishBatchNotFound
	}
	if batch.Status == "running" && batch.LeaseExpiresAt > time.Now().UTC().Unix() {
		return "", errItemPublishBatchConflict
	}
	// token 是本次重试 worker 的租约凭证。
	token := randomHex(16)
	// claimed 和 err 保存重试租约声明结果。
	claimed, err := svc.repository.ClaimBatch(ctx, batchID, token, time.Now().UTC().Add(publishBatchLease).Unix())
	if err != nil {
		return "", err
	}
	if !claimed {
		return "", errItemPublishBatchConflict
	}
	// err 表示重置失败明细的错误。
	if err := svc.repository.ResetFailed(ctx, batchID); err != nil {
		s.failClaimedPublishBatch(batchID, token)
		return "", err
	}
	_ = svc.repository.RecountBatch(ctx, batchID)
	// pending 和 err 保存可重试明细查询结果。
	pending, err := svc.repository.PendingRows(ctx, batchID, false)
	if err != nil {
		s.failClaimedPublishBatch(batchID, token)
		return "", err
	}
	if len(pending) == 0 {
		_, _, _ = svc.repository.FinalizeBatch(ctx, batchID, token)
		return "", errItemPublishBatchNoRows
	}
	s.startPublishBatchWorker(s.lifecycleContext(), userID, batchID, token)
	return batchID, nil
}

// serverItemPublishPort 将 MTOP、Cookie 会话与运行时同步适配为应用商品发布端口。
type serverItemPublishPort struct {
	// server 保存平台调用和账号凭证适配所需的 Server 依赖。
	server *Server
	// repository 提供账号锁、平台凭证查询和 Cookie 写回能力。
	repository itemPublishRepository
}

// Publish 执行单商品平台发布及响应 Cookie 持久化，不向应用层泄露 MTOP 类型。
func (p serverItemPublishPort) Publish(ctx context.Context, input itemapp.PublishInput) (itemapp.PublishOutcome, error) {
	// unlock 释放当前账号的凭证串行化锁；锁覆盖现有平台会话不变量。
	unlock := p.repository.LockCredentials(input.CookieID)
	// latest 保存加锁后读取的平台运行凭证视图。
	latest, err := p.server.loadCookiePlatformDetail(ctx, input.CookieID)
	if err != nil || latest == nil || latest.UserID != input.UserID || !hasStoredCookieCredential(latest) {
		unlock()
		return itemapp.PublishOutcome{}, errors.New("账号凭证已变化，请重试")
	}
	// requestCtx 和 cancel 控制单商品平台发布的最长执行时间。
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	// images 保存应用图片 DTO 转换后的平台图片请求。
	images := make([]mtop.PublishImage, 0, len(input.Images))
	// image 表示当前待转换的应用图片。
	for _, image := range input.Images {
		images = append(images, mtop.PublishImage{Filename: image.Filename, ContentType: image.ContentType, Data: image.Data})
	}
	// location 保存应用发货地 DTO 转换后的平台位置请求。
	var location *mtop.PublishLocation
	if input.Location != nil {
		location = &mtop.PublishLocation{
			Area: input.Location.Area, City: input.Location.City, DivisionID: input.Location.DivisionID,
			Longitude: input.Location.Longitude, Latitude: input.Location.Latitude, POIID: input.Location.POIID,
			POIName: input.Location.POIName, Province: input.Location.Province,
		}
	}
	// mtopCtx 和 cookieSession 保存带 Cookie 快照的平台调用上下文。
	mtopCtx, cookieSession := withMTopCookieSnapshot(requestCtx, latest)
	// result 和 callErr 保存平台发布结果及调用错误。
	result, callErr := p.server.mtopClient().PublishItem(mtopCtx, latest.Value, mtop.PublishItemRequest{
		Title: input.Title, Description: input.Description, PriceCents: input.PriceCents,
		OriginalPriceCents: input.OriginalPriceCents, Quantity: input.Quantity,
		PostageMode: input.PostageMode, PostageCents: input.PostageCents, Virtual: true,
		Location: location, Images: images,
	})
	// runtimeCookie 保存需要在释放凭证锁后同步到运行时的刷新 Cookie。
	runtimeCookie := ""
	// value、valueChanged、handled 和 persistErr 描述 Cookie Jar 持久化结果。
	value, valueChanged, handled, persistErr := p.server.persistMTopCookieSessionLocked(ctx, latest, cookieSession)
	if persistErr != nil {
		if p.server.Logger != nil {
			p.server.Logger.Error("保存发布响应 Cookie Jar 失败", "cookie_id", input.CookieID, "err", persistErr)
		}
	} else if handled && valueChanged {
		runtimeCookie = value
	} else if !handled && callErr == nil && result != nil && result.UpdatedCookies != "" && result.UpdatedCookies != latest.Value {
		// saveErr 表示保存平台返回新 Cookie 的错误。
		if saveErr := p.repository.UpdateCookieValueOwned(ctx, input.CookieID, result.UpdatedCookies, input.UserID); saveErr != nil {
			persistErr = saveErr
		} else {
			runtimeCookie = result.UpdatedCookies
		}
	}
	unlock()
	if runtimeCookie != "" {
		p.server.updateRunningCookie(ctx, input.CookieID, runtimeCookie)
	}
	if callErr != nil {
		if persistErr != nil {
			callErr = errors.Join(callErr, fmt.Errorf("保存发布响应 Cookie: %w", persistErr))
		}
		p.server.recoverExpiredMTOPSession(ctx, input.CookieID, callErr)
		return itemapp.PublishOutcome{ResponseCookieErr: persistErr}, callErr
	}
	if result == nil || strings.TrimSpace(result.ItemID) == "" {
		return itemapp.PublishOutcome{ResponseCookieErr: persistErr}, nil
	}
	return itemapp.PublishOutcome{Result: &itemapp.PublishResult{
		ItemID: result.ItemID, ItemURL: result.ItemURL, Title: result.Title, PriceText: result.PriceText,
		CategoryID: result.CategoryID, CategoryName: result.CategoryName, ImageURL: result.ImageURL,
		Quantity: result.Quantity, RawData: result.RawData,
	}, ResponseCookieErr: persistErr}, nil
}

// storeItemPublishItemRepository 将商品数据库写入适配为应用层商品仓储端口。
type storeItemPublishItemRepository struct {
	// store 保存数据库聚合入口，仅在 Server 适配器内部使用。
	store *db.Store
}

// Upsert 保存应用层商品记录，并在边界转换为数据库行模型。
func (r storeItemPublishItemRepository) Upsert(ctx context.Context, record itemapp.ItemRecord) error {
	return r.store.Items.Upsert(ctx, &db.ItemInfoRow{
		CookieID: record.CookieID, ItemID: record.ItemID, ItemTitle: record.ItemTitle,
		ItemDescription: record.ItemDescription, ItemCategory: record.ItemCategory,
		ItemPrice: record.ItemPrice, ItemDetail: record.ItemDetail,
		MultiQuantityDelivery: record.MultiQuantityDelivery,
	})
}

// newItemPublishApplication 创建单商品发布应用服务并绑定 Server 适配器。
func newItemPublishApplication(server *Server) *itemapp.Service {
	// publisher 是商品发布平台端口适配器。
	publisher := serverItemPublishPort{server: server, repository: newStoreItemPublishRepository(server.Store)}
	// repository 是商品本地持久化端口适配器。
	repository := storeItemPublishItemRepository{store: server.Store}
	// service 和 err 保存应用服务构造结果。
	service, err := itemapp.NewService(publisher, repository)
	if err != nil {
		panic(fmt.Sprintf("商品发布应用服务装配失败: %v", err))
	}
	return service
}
