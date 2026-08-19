package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
)

// ItemBatchPublishPort 将批量发布的 MTOP 调用、凭证会话和远端检查点适配为应用端口。
type ItemBatchPublishPort struct {
	// store 提供批次检查点、平台凭证和 Cookie 会话持久化能力。
	store *db.Store
	// client 返回当前批量发布使用的平台客户端，允许测试替换实现。
	client func() mtop.Client
	// logger 记录不含凭证明文的会话提交错误。
	logger *slog.Logger
	// updateRunningCookie 将平台返回的新 Cookie 同步到运行中的账号实例。
	updateRunningCookie func(context.Context, string, string)
	// recoverExpiredSession 在平台报告会话失效时触发账号恢复协调。
	recoverExpiredSession func(context.Context, string, error)
	// readImage 从受控上传目录读取并校验本地图片。
	readImage ReadPublishImageFile
	// downloadImage 下载并校验批量发布使用的远程图片。
	downloadImage DownloadPublishImageURL
}

// NewItemBatchPublishPort 构造批量发布远端适配器并注入图片安全回调。
func NewItemBatchPublishPort(store *db.Store, client func() mtop.Client, logger *slog.Logger, updateRunningCookie func(context.Context, string, string), recoverExpiredSession func(context.Context, string, error), readImage ReadPublishImageFile, downloadImage DownloadPublishImageURL) *ItemBatchPublishPort {
	// resolvedLogger 保存可用于记录适配器错误的日志器。
	resolvedLogger := logger
	if resolvedLogger == nil {
		resolvedLogger = slog.Default()
	}
	return &ItemBatchPublishPort{store: store, client: client, logger: resolvedLogger, updateRunningCookie: updateRunningCookie, recoverExpiredSession: recoverExpiredSession, readImage: readImage, downloadImage: downloadImage}
}

// PublishRemoteRow 执行单行商品远端发布并保存远端结果检查点，不写入本地商品或自动化规则。
func (p *ItemBatchPublishPort) PublishRemoteRow(ctx context.Context, userID int64, row itemapp.BatchRow, workerToken string, beforePublish func(context.Context) error) (itemapp.BatchPublishOutcome, error) {
	// validateErr 保存适配器依赖校验错误。
	if validateErr := p.validate(); validateErr != nil {
		return itemapp.BatchPublishOutcome{}, validateErr
	}
	// batch、batchErr 保存按用户隔离后的批次配置。
	batch, batchErr := p.store.PublishBatches.Get(ctx, userID, row.BatchID)
	if batchErr != nil || batch == nil {
		return itemapp.BatchPublishOutcome{}, errors.New("批量任务不存在")
	}
	// savedResult 保存已完成远端发布的断点，重试时禁止再次调用平台。
	if strings.TrimSpace(row.ItemID) != "" {
		return itemapp.BatchPublishOutcome{Result: batchPublishResultFromRow(row)}, nil
	}
	// imageDependencyErr 保存新建远端商品所需的图片读取依赖校验错误；已保存检查点的重试已在此前返回。
	if imageDependencyErr := p.validateImageDependencies(); imageDependencyErr != nil {
		return itemapp.BatchPublishOutcome{}, imageDependencyErr
	}
	// location、locationErr 保存批次发货地配置及解析错误。
	location, locationErr := batchPublishLocation(batch.LocationJSON)
	if locationErr != nil {
		return itemapp.BatchPublishOutcome{}, locationErr
	}
	// priceCents、priceErr 保存商品售价的分值表示。
	priceCents, priceErr := parseBatchMoneyCents(row.Price)
	if priceErr != nil || priceCents <= 0 {
		return itemapp.BatchPublishOutcome{}, errors.New("商品价格必须大于 0")
	}
	// originalPriceCents、postageCents 保存原价和邮费的分值表示。
	originalPriceCents, _ := parseBatchMoneyCents(row.OriginalPrice)
	// postageCents 保存邮费的分值表示。
	postageCents, _ := parseBatchMoneyCents(row.Postage)
	// preferredCategory 保存批次配置的可选发布类目。
	preferredCategory, categoryErr := batchPublishCategory(row.CategoryJSON)
	if categoryErr != nil {
		return itemapp.BatchPublishOutcome{}, categoryErr
	}
	// images、imageErr 保存平台发布需要的本地或远程图片内容。
	images, imageErr := LoadBatchPublishImages(ctx, batch.UploadDir, row.ImagesJSON, p.readImage, p.downloadImage)
	if imageErr != nil {
		return itemapp.BatchPublishOutcome{}, imageErr
	}
	// markCtx、markCancel 限制远端开始检查点写入时间，避免租约失效时无限等待。
	markCtx, markCancel := batchPublishStatusContext(ctx)
	// remoteStarted、markErr 保存远端开始检查点的写入结果。
	remoteStarted, markErr := p.store.PublishBatches.MarkClaimedRemoteStarted(markCtx, row.ID, workerToken)
	markCancel()
	if markErr != nil || !remoteStarted {
		return itemapp.BatchPublishOutcome{}, fmt.Errorf("保存远端发布前检查点失败: %w", firstBatchError(markErr, itemapp.ErrBatchLeaseLost))
	}
	// unlock、latest、loadErr 保存凭证快照读取及其串行化锁释放结果。
	unlock := p.store.LockAccountCredentials(row.CookieID)
	// latest、loadErr 保存加锁读取的平台凭证视图及错误。
	latest, loadErr := p.store.Cookies.GetCookiePlatformRuntimeData(ctx, row.CookieID)
	if loadErr != nil || latest.UserID != userID || !hasStoredCredential(latest) {
		unlock()
		return itemapp.BatchPublishOutcome{}, errors.New("账号凭证已变化，请重试")
	}
	// initialValue、initialMetadata 用于远端返回后复核期间的凭证一致性。
	initialValue, initialMetadata := latest.Value, latest.MetadataJSON
	// requestCtx、cancel 控制单行远端发布的最长执行时间。
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	// mtopCtx、cookieSession 挂载本次调用使用的 Cookie 会话。
	mtopCtx, cookieSession := withCookieSnapshot(requestCtx, latest)
	// unlock 在远端 I/O 前释放，避免凭证锁覆盖慢速平台请求。
	unlock()
	// result、callErr 保存平台返回结果及调用错误。
	result, callErr := p.mtopClient().PublishItem(mtopCtx, latest.Value, mtop.PublishItemRequest{
		Title: row.Title, Description: firstBatchNonEmpty(row.Description, row.Title), PriceCents: priceCents,
		OriginalPriceCents: originalPriceCents, Quantity: row.Quantity, PostageMode: row.PostageMode,
		PostageCents: postageCents, Virtual: true, Location: location, PreferredCategory: preferredCategory, Images: images,
		BeforePublish: beforePublish,
	})
	cancel()
	// runtimeCookie、persistErr 保存会话写回后的运行时 Cookie 和错误。
	runtimeCookie, persistErr := p.persistBatchSession(ctx, userID, row.CookieID, initialValue, initialMetadata, cookieSession, result, callErr)
	if runtimeCookie != "" && p.updateRunningCookie != nil {
		p.updateRunningCookie(ctx, row.CookieID, runtimeCookie)
	}
	if persistErr != nil && callErr != nil {
		callErr = errors.Join(callErr, fmt.Errorf("保存发布响应 Cookie: %w", persistErr))
	}
	if callErr != nil {
		p.recoverExpired(ctx, row.CookieID, callErr)
		if ctx.Err() != nil {
			return itemapp.BatchPublishOutcome{}, &itemapp.UncertainRemotePublishError{Err: fmt.Errorf("取消时远端发布结果未知: %w", ctx.Err())}
		}
		// publishErr 保存库存权限等可直接展示的确定性平台错误。
		var publishErr *mtop.PublishError
		if errors.As(callErr, &publishErr) && publishErr.Code == mtop.PublishErrorStockPermissionMissing {
			return itemapp.BatchPublishOutcome{}, errors.New("该账号没有库存发布权限，无法按库存数量发布商品")
		}
		if errors.Is(callErr, mtop.ErrPublishCategoryUnrecognized) {
			return itemapp.BatchPublishOutcome{}, callErr
		}
		return itemapp.BatchPublishOutcome{}, &itemapp.UncertainRemotePublishError{Err: fmt.Errorf("远端发布调用失败且结果未知: %w", callErr)}
	}
	if result == nil {
		return itemapp.BatchPublishOutcome{}, errors.New("发布商品接口未返回结果")
	}
	// rawJSON 保存远端原始结果，供重试恢复和本地详情持久化使用。
	rawJSON, _ := json.Marshal(result.RawData)
	// saveCtx、saveCancel 限制远端结果检查点写入时间。
	saveCtx, saveCancel := batchPublishStatusContext(ctx)
	// saved、saveErr 保存远端结果检查点的写入结果。
	saved, saveErr := p.store.PublishBatches.SaveClaimedRemoteResult(saveCtx, row.ID, workerToken, result.ItemID, result.ItemURL, string(rawJSON))
	saveCancel()
	if saveErr != nil || !saved {
		return itemapp.BatchPublishOutcome{}, &itemapp.UncertainRemotePublishError{Err: fmt.Errorf("保存远端发布结果失败: %w", firstBatchError(saveErr, itemapp.ErrBatchLeaseLost))}
	}
	// outcome 保存平台结果和已完成远端调用后的 Cookie 后置错误。
	outcome := itemapp.BatchPublishOutcome{Result: batchPublishResultFromMTOP(result)}
	if persistErr != nil {
		outcome.ResponseCookieErr = persistErr
	}
	return outcome, nil
}

// persistBatchSession 在远端调用结束后复核凭证并写回 Cookie 会话变化。
func (p *ItemBatchPublishPort) persistBatchSession(ctx context.Context, userID int64, cookieID, initialValue, initialMetadata string, session *mtop.CookieSession, result *mtop.PublishItemResult, callErr error) (string, error) {
	// unlock 保护远端调用完成后的凭证复核和持久化。
	unlock := p.store.LockAccountCredentials(cookieID)
	defer unlock()
	// latest、loadErr 保存远端调用完成后的最新平台凭证视图。
	latest, loadErr := p.store.Cookies.GetCookiePlatformRuntimeData(ctx, cookieID)
	if loadErr != nil || latest.UserID != userID || latest.Value != initialValue || latest.MetadataJSON != initialMetadata {
		return "", errors.New("账号凭证已变化，请重试")
	}
	// value、valueChanged、handled、persistErr 保存会话持久化状态。
	value, valueChanged, handled, persistErr := persistBatchCookieSession(ctx, p.store, latest, session)
	if persistErr != nil {
		if p.logger != nil {
			p.logger.Error("保存批量发布响应 Cookie Jar 失败", "cookie_id", cookieID, "err", persistErr)
		}
		return "", persistErr
	}
	if handled && valueChanged {
		return value, nil
	}
	if !handled && callErr == nil && result != nil && result.UpdatedCookies != "" && result.UpdatedCookies != latest.Value {
		// saveErr 保存平台返回平面 Cookie 的写回错误。
		if saveErr := p.store.Cookies.UpdateValueOwned(ctx, cookieID, result.UpdatedCookies, userID); saveErr != nil {
			return "", saveErr
		}
		return result.UpdatedCookies, nil
	}
	return "", nil
}

// persistBatchCookieSession 保存完整 Cookie Jar 或兼容的平面 Cookie 会话。
func persistBatchCookieSession(ctx context.Context, store *db.Store, detail db.CookiePlatformRuntimeData, session *mtop.CookieSession) (string, bool, bool, error) {
	if session == nil {
		return "", false, false, nil
	}
	// value、snapshot、changed 保存平台会话当前状态。
	value, snapshot, changed := session.State()
	if !changed {
		return detail.Value, false, snapshot != nil, nil
	}
	// metadata 保存移除旧快照后待写入的新 metadata。
	metadata := cookierefresh.MetadataWithoutSnapshot(detail.MetadataJSON)
	if snapshot != nil {
		metadata = cookierefresh.MetadataWithSnapshot(detail.MetadataJSON, snapshot)
	}
	// persistErr 保存 Cookie 会话的加密持久化结果。
	persistErr := store.Cookies.UpdateRenewalCookie(ctx, detail.ID, value, metadata, time.Now().Unix())
	return value, value != detail.Value, true, persistErr
}

// validate 检查批量远端适配器所需的数据库、图片和平台依赖。
func (p *ItemBatchPublishPort) validate() error {
	if p == nil || p.store == nil || p.store.Cookies == nil || p.store.PublishBatches == nil {
		return errors.New("批量发布远端适配器未初始化")
	}
	return nil
}

// validateImageDependencies 检查新建远端商品时必需的图片安全回调。
func (p *ItemBatchPublishPort) validateImageDependencies() error {
	if p.readImage == nil || p.downloadImage == nil {
		return errors.New("批量发布图片读取端口未初始化")
	}
	return nil
}

// mtopClient 返回当前平台客户端，并在测试构造缺失时提供默认实现。
func (p *ItemBatchPublishPort) mtopClient() mtop.Client {
	if p != nil && p.client != nil {
		// resolvedClient 保存回调返回的平台客户端。
		if resolvedClient := p.client(); resolvedClient != nil {
			return resolvedClient
		}
	}
	return mtop.NewClient()
}

// recoverExpired 在平台会话失效时通知账号恢复协调器。
func (p *ItemBatchPublishPort) recoverExpired(ctx context.Context, cookieID string, err error) {
	if p != nil && p.recoverExpiredSession != nil && err != nil {
		p.recoverExpiredSession(ctx, cookieID, err)
	}
}

// batchPublishResultFromMTOP 将平台结果转换为不泄露平台类型的应用结果。
func batchPublishResultFromMTOP(result *mtop.PublishItemResult) *itemapp.BatchPublishResult {
	if result == nil {
		return nil
	}
	return &itemapp.BatchPublishResult{ItemID: result.ItemID, ItemURL: result.ItemURL, Title: result.Title, PriceText: result.PriceText, CategoryID: result.CategoryID, CategoryName: result.CategoryName, ImageURL: result.ImageURL, Quantity: result.Quantity, RawData: result.RawData}
}

// batchPublishResultFromRow 将已有远端检查点转换为应用结果，避免重试重复调用平台。
func batchPublishResultFromRow(row itemapp.BatchRow) *itemapp.BatchPublishResult {
	// rawData 保存已有远端结果的结构化 JSON。
	rawData := map[string]any{}
	if strings.TrimSpace(row.RawJSON) != "" {
		_ = json.Unmarshal([]byte(row.RawJSON), &rawData)
	}
	return &itemapp.BatchPublishResult{ItemID: row.ItemID, ItemURL: row.ItemURL, Title: row.Title, PriceText: row.Price, Quantity: row.Quantity, RawData: rawData}
}

// batchPublishLocation 解析批次级别的发货地配置。
func batchPublishLocation(raw string) (*mtop.PublishLocation, error) {
	if strings.TrimSpace(raw) == "" {
		raw = "{}"
	}
	// location 保存批次发货地配置。
	var location mtop.PublishLocation
	// decodeErr 保存发货地 JSON 解析错误。
	if decodeErr := json.Unmarshal([]byte(raw), &location); decodeErr != nil {
		return nil, errors.New("批量任务发货地配置损坏，请重新创建任务")
	}
	if strings.TrimSpace(location.DivisionID) == "" {
		// legacyLocation 保存 v1.0.2 曾持久化的 PascalCase 发货地格式，仅用于恢复和重试已有批次。
		var legacyLocation legacyBatchPublishLocation
		// legacyDecodeErr 保存历史格式 JSON 的解码错误；当前格式已成功解码时才尝试此兼容分支。
		if legacyDecodeErr := json.Unmarshal([]byte(raw), &legacyLocation); legacyDecodeErr != nil {
			return nil, errors.New("批量任务发货地配置损坏，请重新创建任务")
		}
		if strings.TrimSpace(legacyLocation.DivisionID) != "" {
			location = mtop.PublishLocation{
				Area: legacyLocation.Area, City: legacyLocation.City, DivisionID: legacyLocation.DivisionID,
				Longitude: legacyLocation.Longitude, Latitude: legacyLocation.Latitude, POIID: legacyLocation.POIID,
				POIName: legacyLocation.POIName, Province: legacyLocation.Province,
			}
		}
	}
	if strings.TrimSpace(location.DivisionID) == "" {
		return nil, nil
	}
	return &location, nil
}

// legacyBatchPublishLocation 保存 v1.0.2 因应用模型缺少 JSON 标签而写入数据库的历史发货地格式。
// 该类型只用于读取旧批次；新批次始终使用 snake_case 字段，避免再次扩大持久化兼容面。
type legacyBatchPublishLocation struct {
	// Area 保存历史批次的区县名称。
	Area string `json:"Area"`
	// City 保存历史批次的城市名称。
	City string `json:"City"`
	// DivisionID 保存历史批次的平台行政区划标识。
	DivisionID string `json:"DivisionID"`
	// Longitude 保存历史批次的经度。
	Longitude float64 `json:"Longitude"`
	// Latitude 保存历史批次的纬度。
	Latitude float64 `json:"Latitude"`
	// POIID 保存历史批次的地图兴趣点标识。
	POIID string `json:"POIID"`
	// POIName 保存历史批次的地图兴趣点名称。
	POIName string `json:"POIName"`
	// Province 保存历史批次的省份名称。
	Province string `json:"Province"`
}

// batchPublishCategory 解析批次默认类目并校验平台必需字段。
func batchPublishCategory(raw string) (*mtop.PublishCategory, error) {
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(raw) == "{}" {
		return nil, nil
	}
	// category 保存批次默认类目配置。
	var category mtop.PublishCategory
	// decodeErr 保存类目 JSON 解析错误。
	if decodeErr := json.Unmarshal([]byte(raw), &category); decodeErr != nil {
		return nil, errors.New("默认类目配置损坏，请重新创建批量任务")
	}
	if strings.TrimSpace(category.CatID) == "" && strings.TrimSpace(category.CatName) == "" && strings.TrimSpace(category.ChannelCatID) == "" && strings.TrimSpace(category.TBCatID) == "" {
		return nil, nil
	}
	if strings.TrimSpace(category.CatID) == "" || strings.TrimSpace(category.CatName) == "" || strings.TrimSpace(category.ChannelCatID) == "" {
		return nil, errors.New("默认类目信息不完整，请重新创建批量任务")
	}
	return &category, nil
}

// parseBatchMoneyCents 将用户金额文本转换为平台使用的分值。
func parseBatchMoneyCents(raw string) (int64, error) {
	raw = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(raw, "¥"), "￥"))
	if raw == "" {
		return 0, nil
	}
	// sign、unsigned 保存金额符号和去除符号后的文本。
	sign := int64(1)
	// unsigned 保存去除金额符号后的文本。
	unsigned := raw
	if strings.HasPrefix(unsigned, "-") {
		sign, unsigned = -1, strings.TrimPrefix(unsigned, "-")
	} else {
		unsigned = strings.TrimPrefix(unsigned, "+")
	}
	// parts 保存整数和小数部分。
	parts := strings.Split(unsigned, ".")
	if len(parts) > 2 {
		return 0, errors.New("金额格式错误")
	}
	// yuan、err 保存元金额及解析错误。
	yuan, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil {
		return 0, err
	}
	// cents 保存补齐两位小数后的分金额。
	cents := int64(0)
	if len(parts) == 2 {
		// fraction 保存金额的小数部分。
		fraction := strings.TrimSpace(parts[1])
		if len(fraction) > 2 {
			return 0, errors.New("金额最多支持两位小数")
		}
		for len(fraction) < 2 {
			fraction += "0"
		}
		cents, err = strconv.ParseInt(fraction, 10, 64)
		if err != nil {
			return 0, err
		}
	}
	return sign * (yuan*100 + cents), nil
}

// batchPublishStatusContext 为远端检查点写入提供独立的短超时。
func batchPublishStatusContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent != nil && parent.Err() == nil {
		return context.WithTimeout(parent, 5*time.Second)
	}
	return context.WithTimeout(context.Background(), 5*time.Second)
}

// firstBatchError 返回第一个有效错误，确保租约错误不会覆盖数据库错误。
func firstBatchError(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}

// firstBatchNonEmpty 返回候选文本中第一个非空白值。
func firstBatchNonEmpty(values ...string) string {
	// value 表示当前候选文本。
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// 确保批量远端适配器实现应用层定义的端口。
var _ itemapp.BatchPublishPort = (*ItemBatchPublishPort)(nil)

// IsSessionExpiredError 判断批量发布错误是否要求终止剩余明细并进入账号恢复。
func IsSessionExpiredError(err error) bool {
	return mtop.IsSessionExpiredErr(err)
}
