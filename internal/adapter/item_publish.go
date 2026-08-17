package adapter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
)

// ItemPublishPort 将 MTOP、Cookie 会话与运行时同步适配为商品发布端口。
type ItemPublishPort struct {
	// store 提供平台凭证读取、凭证锁和 Cookie 写回能力。
	store *db.Store
	// client 返回商品发布平台客户端，允许运行时注入测试客户端。
	client func() mtop.Client
	// logger 记录不含凭证内容的发布适配阶段信息。
	logger *slog.Logger
	// updateRunningCookie 将平台返回的新 Cookie 同步到运行中的账号实例。
	updateRunningCookie func(context.Context, string, string)
	// recoverExpiredSession 在平台报告会话过期时触发账号恢复并返回是否已获得可重试凭证。
	recoverExpiredSession func(context.Context, string, error) bool
}

// NewItemPublishPort 构造单商品发布基础设施端口。
func NewItemPublishPort(store *db.Store, client func() mtop.Client, logger *slog.Logger, updateRunningCookie func(context.Context, string, string), recoverExpiredSession func(context.Context, string, error) bool) *ItemPublishPort {
	// resolvedLogger 保存可用于记录发布适配错误的日志器。
	resolvedLogger := logger
	if resolvedLogger == nil {
		resolvedLogger = slog.Default()
	}
	return &ItemPublishPort{store: store, client: client, logger: resolvedLogger, updateRunningCookie: updateRunningCookie, recoverExpiredSession: recoverExpiredSession}
}

// categoryRecommender 是 MTOP 类目推荐能力的适配器内部契约。
type categoryRecommender interface {
	// RecommendPublishCategory 根据关键词返回平台类目及可能刷新的 Cookie。
	RecommendPublishCategory(context.Context, string, string) (mtop.PublishCategory, string, error)
}

// RecommendCategory 读取账号平台凭证、调用类目推荐并提交响应会话变化。
func (p *ItemPublishPort) RecommendCategory(ctx context.Context, userID int64, cookieID, keyword string) (itemapp.BatchPreviewCategory, error) {
	if p == nil || p.store == nil || p.store.Cookies == nil {
		return itemapp.BatchPreviewCategory{}, errors.New("商品发布存储未初始化")
	}
	// recommender 和 ok 表示当前 MTOP 客户端是否支持类目推荐。
	recommender, ok := p.mtopClient().(categoryRecommender)
	if !ok {
		return itemapp.BatchPreviewCategory{}, itemapp.ErrCategoryUnsupported
	}
	// unlock 保护凭证快照读取，远端调用前会释放该锁。
	unlock := p.store.LockAccountCredentials(cookieID)
	// latest 和 loadErr 保存账号平台凭证视图及读取错误。
	latest, loadErr := p.store.Cookies.GetCookiePlatformRuntimeData(ctx, cookieID)
	if loadErr != nil || latest.UserID != userID || !hasStoredCredential(latest) {
		unlock()
		return itemapp.BatchPreviewCategory{}, itemapp.ErrCategoryCredentialChanged
	}
	// requestCtx 和 cancel 控制类目推荐远端调用的最长时间。
	requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// mtopCtx 和 cookieSession 保存带凭证快照的平台上下文及会话。
	mtopCtx, cookieSession := withCookieSnapshot(requestCtx, latest)
	// initialValue 和 initialMetadata 保存远端调用前的凭证版本。
	initialValue, initialMetadata := latest.Value, latest.MetadataJSON
	unlock()

	// category、updatedCookies 和 callErr 保存平台推荐结果、刷新 Cookie 与调用错误。
	category, updatedCookies, callErr := recommender.RecommendPublishCategory(mtopCtx, latest.Value, strings.TrimSpace(keyword))
	// runtimeCookie 和 persistErr 保存会话写回后的运行时 Cookie 与错误。
	runtimeCookie, persistErr := p.persistCategorySession(ctx, userID, cookieID, initialValue, initialMetadata, cookieSession, updatedCookies)
	if runtimeCookie != "" && p.updateRunningCookie != nil {
		p.updateRunningCookie(ctx, cookieID, runtimeCookie)
	}
	if persistErr != nil {
		return itemapp.BatchPreviewCategory{}, errors.Join(itemapp.ErrCategoryPersistence, persistErr)
	}
	if callErr != nil {
		if errors.Is(callErr, mtop.ErrPublishCategoryUnrecognized) {
			return itemapp.BatchPreviewCategory{}, errors.Join(itemapp.ErrCategoryUnrecognized, callErr)
		}
		return itemapp.BatchPreviewCategory{}, callErr
	}
	return itemapp.BatchPreviewCategory{CatID: category.CatID, CatName: category.CatName, ChannelCatID: category.ChannelCatID, TBCatID: category.TBCatID}, nil
}

// persistCategorySession 在远端调用后复核凭证并保存完整会话或平面 Cookie。
func (p *ItemPublishPort) persistCategorySession(ctx context.Context, userID int64, cookieID, initialValue, initialMetadata string, session *mtop.CookieSession, updatedCookies string) (string, error) {
	// unlock 保护远端调用完成后的凭证复核与会话写回。
	unlock := p.store.LockAccountCredentials(cookieID)
	defer unlock()
	// latest 和 loadErr 保存远端调用后的凭证视图及读取错误。
	latest, loadErr := p.store.Cookies.GetCookiePlatformRuntimeData(ctx, cookieID)
	if loadErr != nil || latest.UserID != userID || latest.Value != initialValue || latest.MetadataJSON != initialMetadata {
		return "", itemapp.ErrCategoryCredentialChanged
	}
	// value、valueChanged、handled 和 persistErr 保存会话转换结果及写回错误。
	value, valueChanged, handled, persistErr := p.persistSession(ctx, latest, session)
	if persistErr != nil {
		return "", persistErr
	}
	if handled && valueChanged {
		return value, nil
	}
	if !handled && updatedCookies != "" && updatedCookies != latest.Value {
		// err 表示平面 Cookie 写回错误。
		if err := p.store.Cookies.UpdateValueOwned(ctx, cookieID, updatedCookies, userID); err != nil {
			return "", err
		}
		return updatedCookies, nil
	}
	return "", nil
}

// Publish 执行单商品平台发布及响应 Cookie 持久化，不向应用层泄露 MTOP 类型。
func (p *ItemPublishPort) Publish(ctx context.Context, input itemapp.PublishInput) (itemapp.PublishOutcome, error) {
	return p.publish(ctx, input, true)
}

// publish 执行一次商品发布；会话恢复成功时最多用新凭证重试一次。
func (p *ItemPublishPort) publish(ctx context.Context, input itemapp.PublishInput, allowRetry bool) (itemapp.PublishOutcome, error) {
	if p == nil || p.store == nil || p.store.Cookies == nil {
		return itemapp.PublishOutcome{}, errors.New("商品发布存储未初始化")
	}
	// unlock 保护账号凭证快照读取与远端调用后的提交复核。
	unlock := p.store.LockAccountCredentials(input.CookieID)
	// latest、loadErr 保存加锁后读取的平台凭证视图。
	latest, loadErr := p.store.Cookies.GetCookiePlatformRuntimeData(ctx, input.CookieID)
	if loadErr != nil || latest.UserID != input.UserID || !hasStoredCredential(latest) {
		unlock()
		return itemapp.PublishOutcome{}, errors.New("账号凭证已变化，请重试")
	}
	// requestCtx、cancel 控制单商品平台发布的最长执行时间。
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	// images 保存应用图片转换后的平台图片请求。
	images := make([]mtop.PublishImage, 0, len(input.Images))
	// image 表示当前待转换的应用图片。
	for _, image := range input.Images {
		images = append(images, mtop.PublishImage{Filename: image.Filename, ContentType: image.ContentType, Data: image.Data})
	}
	// location 保存应用发货地转换后的平台位置请求。
	var location *mtop.PublishLocation
	if input.Location != nil {
		location = &mtop.PublishLocation{
			Area: input.Location.Area, City: input.Location.City, DivisionID: input.Location.DivisionID,
			Longitude: input.Location.Longitude, Latitude: input.Location.Latitude, POIID: input.Location.POIID,
			POIName: input.Location.POIName, Province: input.Location.Province,
		}
	}
	// mtopCtx、cookieSession 保存带 Cookie 快照的平台调用上下文。
	mtopCtx, cookieSession := withCookieSnapshot(requestCtx, latest)
	// initialValue、initialMetadata 保存远端调用前的凭证快照，用于提交阶段复核。
	initialValue, initialMetadata := latest.Value, latest.MetadataJSON
	// unlock 在远端调用前释放，避免慢速平台 I/O 占用账号凭证锁。
	unlock()
	// result、callErr 保存平台发布结果及调用错误。
	result, callErr := p.mtopClient().PublishItem(mtopCtx, latest.Value, mtop.PublishItemRequest{
		Title: input.Title, Description: input.Description, PriceCents: input.PriceCents,
		OriginalPriceCents: input.OriginalPriceCents, Quantity: input.Quantity,
		PostageMode: input.PostageMode, PostageCents: input.PostageCents, Virtual: true,
		Location: location, Images: images,
	})
	// callErr 由适配器转换为应用层错误，保留原始错误链供基础设施恢复逻辑使用。
	callErr = publishErrorToApplication(callErr)
	// runtimeCookie、persistErr 保存响应 Cookie 提交后的运行时同步值和错误。
	runtimeCookie, persistErr := p.persistPublishSession(ctx, input, initialValue, initialMetadata, cookieSession, result, callErr)
	if runtimeCookie != "" && p.updateRunningCookie != nil {
		p.updateRunningCookie(ctx, input.CookieID, runtimeCookie)
	}
	if callErr != nil {
		if persistErr != nil {
			callErr = errors.Join(callErr, fmt.Errorf("保存发布响应 Cookie: %w", persistErr))
		}
		if allowRetry && mtop.IsSessionExpiredErr(callErr) && p.recoverExpired(ctx, input.CookieID, callErr) {
			return p.publish(ctx, input, false)
		}
		p.recoverExpired(ctx, input.CookieID, callErr)
		return itemapp.PublishOutcome{ResponseCookieErr: persistErr}, callErr
	}
	if result == nil {
		return itemapp.PublishOutcome{ResponseCookieErr: persistErr}, nil
	}
	return itemapp.PublishOutcome{Result: &itemapp.PublishResult{
		ItemID: result.ItemID, ItemURL: result.ItemURL, Title: result.Title, PriceText: result.PriceText,
		CategoryID: result.CategoryID, CategoryName: result.CategoryName, ImageURL: result.ImageURL,
		Quantity: result.Quantity, RawData: result.RawData,
	}, ResponseCookieErr: persistErr}, nil
}

// publishErrorToApplication 将平台发布错误转换为不依赖平台包的应用错误。
func publishErrorToApplication(err error) error {
	if err == nil {
		return nil
	}
	// platformErr 保存平台客户端返回的发布错误；其文本已由平台客户端截断或脱敏。
	var platformErr *mtop.PublishError
	if !errors.As(err, &platformErr) {
		return err
	}
	return &itemapp.PublishError{
		Code: itemapp.PublishErrorCode(platformErr.Code), Ret: append([]string(nil), platformErr.Ret...),
		Body: platformErr.Body, Err: err,
	}
}

// persistPublishSession 在远端调用结束后复核凭证并保存 Cookie 会话变化。
func (p *ItemPublishPort) persistPublishSession(ctx context.Context, input itemapp.PublishInput, initialValue, initialMetadata string, session *mtop.CookieSession, result *mtop.PublishItemResult, callErr error) (string, error) {
	// unlock 保护远端调用完成后的凭证复核和写回。
	unlock := p.store.LockAccountCredentials(input.CookieID)
	defer unlock()
	// latest、loadErr 保存远端调用完成后重新读取的平台凭证视图。
	latest, loadErr := p.store.Cookies.GetCookiePlatformRuntimeData(ctx, input.CookieID)
	if loadErr != nil || latest.UserID != input.UserID || latest.Value != initialValue || latest.MetadataJSON != initialMetadata {
		return "", errors.New("账号凭证已变化，请重试")
	}
	// value、valueChanged、handled、persistErr 保存 Cookie 会话提交结果。
	value, valueChanged, handled, persistErr := p.persistSession(ctx, latest, session)
	if persistErr != nil {
		if p.logger != nil {
			p.logger.Error("保存发布响应 Cookie Jar 失败", "cookie_id", input.CookieID, "err", persistErr)
		}
		return "", persistErr
	}
	if handled && valueChanged {
		return value, nil
	}
	if !handled && callErr == nil && result != nil && result.UpdatedCookies != "" && result.UpdatedCookies != latest.Value {
		// saveErr 保存平台返回新 Cookie 的错误。
		if saveErr := p.store.Cookies.UpdateValueOwned(ctx, input.CookieID, result.UpdatedCookies, input.UserID); saveErr != nil {
			return "", saveErr
		}
		return result.UpdatedCookies, nil
	}
	return "", nil
}

// persistSession 保存完整 Cookie Jar 或平面 Cookie 的会话变化。
func (p *ItemPublishPort) persistSession(ctx context.Context, detail db.CookiePlatformRuntimeData, session *mtop.CookieSession) (string, bool, bool, error) {
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
	// err 保存 Cookie 会话持久化错误。
	err := p.store.Cookies.UpdateRenewalCookie(ctx, detail.ID, value, metadata, time.Now().Unix())
	return value, value != detail.Value, true, err
}

// ItemPublishRepository 将本地商品写入适配为应用层商品仓储端口。
type ItemPublishRepository struct {
	// store 提供商品基础信息持久化能力。
	store *db.Store
}

// NewItemPublishRepository 构造本地商品发布仓储适配器。
func NewItemPublishRepository(store *db.Store) *ItemPublishRepository {
	return &ItemPublishRepository{store: store}
}

// Upsert 保存应用层商品记录，并转换为数据库行模型。
func (r *ItemPublishRepository) Upsert(ctx context.Context, record itemapp.ItemRecord) error {
	if r == nil || r.store == nil || r.store.Items == nil {
		return errors.New("商品发布存储未初始化")
	}
	return r.store.Items.Upsert(ctx, &db.ItemInfoRow{
		CookieID: record.CookieID, ItemID: record.ItemID, ItemTitle: record.ItemTitle,
		ItemDescription: record.ItemDescription, ItemCategory: record.ItemCategory,
		ItemPrice: record.ItemPrice, ItemDetail: record.ItemDetail,
		MultiQuantityDelivery: record.MultiQuantityDelivery,
	})
}

// mtopClient 返回当前商品发布使用的平台客户端，并在测试构造缺失时提供默认实现。
func (p *ItemPublishPort) mtopClient() mtop.Client {
	if p != nil && p.client != nil {
		// client 保存当前回调返回的平台客户端。
		if client := p.client(); client != nil {
			return client
		}
	}
	return mtop.NewClient()
}

// recoverExpired 在平台会话过期时通知账号恢复协调器。
func (p *ItemPublishPort) recoverExpired(ctx context.Context, cookieID string, err error) bool {
	if p != nil && p.recoverExpiredSession != nil && err != nil {
		return p.recoverExpiredSession(ctx, cookieID, err)
	}
	return false
}

// 确保发布端口和仓储实现覆盖应用层定义的最小接口。
var _ itemapp.PublishPort = (*ItemPublishPort)(nil)
var _ itemapp.ItemRepository = (*ItemPublishRepository)(nil)
