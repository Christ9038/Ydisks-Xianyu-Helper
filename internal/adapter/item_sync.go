package adapter

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
)

// ItemSyncRepository 将商品同步应用 Port 适配到数据库、MTOP 和账号运行时。
type ItemSyncRepository struct {
	// store 提供账号凭证和商品持久化能力。
	store *db.Store
	// client 返回商品列表和详情平台调用能力，允许运行时注入测试客户端。
	client func() mtop.Client
	// logger 记录不含凭证的同步阶段信息。
	logger *slog.Logger
	// updateRunningCookie 将平台返回的新 Cookie 同步到运行中的账号实例。
	updateRunningCookie func(context.Context, string, string)
	// recoverExpiredSession 在平台报告会话过期时触发账号恢复。
	recoverExpiredSession func(context.Context, string, error)
	// cacheMu 保护商品多规格探测缓存。
	cacheMu sync.Mutex
	// cache 保存短期商品多规格探测结果。
	cache map[string]itemSpecCacheEntry
}

// itemSpecCacheEntry 保存商品多规格结果及过期时间。
type itemSpecCacheEntry struct {
	// isMultiSpec 表示商品是否包含多规格。
	isMultiSpec bool
	// expiresAt 表示缓存项失效时间。
	expiresAt time.Time
}

// NewItemSyncRepository 构造商品同步基础设施适配器。
func NewItemSyncRepository(store *db.Store, client func() mtop.Client, logger *slog.Logger, updateRunningCookie func(context.Context, string, string), recoverExpiredSession func(context.Context, string, error)) *ItemSyncRepository {
	if client == nil {
		client = func() mtop.Client { return mtop.NewClient() }
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &ItemSyncRepository{store: store, client: client, logger: logger, updateRunningCookie: updateRunningCookie, recoverExpiredSession: recoverExpiredSession, cache: make(map[string]itemSpecCacheEntry)}
}

// OwnsAccount 按非敏感所有者字段判断用户是否拥有账号。
func (r *ItemSyncRepository) OwnsAccount(ctx context.Context, userID int64, cookieID string) (bool, error) {
	if r == nil || r.store == nil || r.store.Cookies == nil {
		return false, syncStageError(itemapp.SyncErrorPersistence, errors.New("商品同步存储未初始化"))
	}
	// ownerID、ownerErr 保存只读取账号归属的查询结果。
	ownerID, ownerErr := r.store.Cookies.GetOwnerID(ctx, cookieID)
	if errors.Is(ownerErr, db.ErrNotFound) {
		return false, nil
	}
	if ownerErr != nil {
		return false, syncStageError(itemapp.SyncErrorPersistence, ownerErr)
	}
	return ownerID == userID, nil
}

// SyncAll 读取平台商品全集、探测多规格并完成本地 reconcile。
func (r *ItemSyncRepository) SyncAll(ctx context.Context, query itemapp.SyncQuery) (itemapp.SyncAllResult, error) {
	// requestCtx、cancel 控制全量同步的最长执行时间。
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	// latest、cookieValue、requestContext、session、unlock 保存远端请求使用的凭证快照及锁。
	latest, cookieValue, requestContext, session, unlock, err := r.begin(requestCtx, query)
	if err != nil {
		return itemapp.SyncAllResult{}, err
	}
	// unlock 在创建完整 Cookie 会话后立即释放，避免慢速平台 I/O 占用账号锁。
	unlock()
	// result、callErr 保存平台全集结果和调用错误。
	result, callErr := r.mtopClient().FetchAllItems(requestContext, cookieValue, query.PageSize, query.MaxPages)
	// latest、session 和 callErr 通过重新加锁的提交阶段处理平台 Cookie 变化。
	latest, session, callErr, err = r.finishRemote(requestCtx, query, latest, session, callErr)
	if err != nil {
		return itemapp.SyncAllResult{}, err
	}
	if callErr != nil {
		r.recoverExpired(requestCtx, query.CookieID, callErr)
		return itemapp.SyncAllResult{}, syncStageError(itemapp.SyncErrorPlatform, callErr)
	}
	if result == nil {
		return itemapp.SyncAllResult{}, syncStageError(itemapp.SyncErrorPlatform, errors.New("商品列表接口未返回结果"))
	}
	// detailCookies 保存规格探测使用的最新 Cookie 串。
	detailCookies := cookieValue
	if result.UpdatedCookies != "" {
		detailCookies = result.UpdatedCookies
	}
	// detailErr 保存远端多规格探测错误。
	detailErr := r.enrichMultiSpec(requestCtx, detailCookies, query.CookieID, result.Items)
	if detailErr != nil {
		r.recoverExpired(requestCtx, query.CookieID, detailErr)
		return itemapp.SyncAllResult{}, syncStageError(itemapp.SyncErrorPlatform, detailErr)
	}
	// persistErr 保存探测后 Cookie 会话提交错误。
	persistErr := r.persistAfterEnrich(requestCtx, query, latest, session, cookieValue, result.UpdatedCookies)
	if persistErr != nil {
		return itemapp.SyncAllResult{}, persistErr
	}
	// syncResult、syncErr 保存本地全集 reconcile 结果及错误。
	syncResult, syncErr := r.syncItems(requestCtx, query.CookieID, result.Items)
	if syncErr != nil {
		return itemapp.SyncAllResult{}, syncStageError(itemapp.SyncErrorPersistence, syncErr)
	}
	return itemapp.SyncAllResult{TotalCount: len(result.Items), TotalPages: result.TotalPages, SavedCount: syncResult.Saved, DeletedCount: syncResult.Deleted}, nil
}

// SyncPage 读取平台指定页、探测多规格并保存本页商品。
func (r *ItemSyncRepository) SyncPage(ctx context.Context, query itemapp.SyncQuery) (itemapp.SyncPageResult, error) {
	// requestCtx、cancel 控制分页同步的最长执行时间。
	requestCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	// latest、cookieValue、requestContext、session、unlock 保存远端请求使用的凭证快照及锁。
	latest, cookieValue, requestContext, session, unlock, err := r.begin(requestCtx, query)
	if err != nil {
		return itemapp.SyncPageResult{}, err
	}
	unlock()
	// result、callErr 保存平台分页结果和调用错误。
	result, callErr := r.mtopClient().FetchItemsPage(requestContext, cookieValue, query.PageNumber, query.PageSize)
	// latest、session 和 callErr 通过提交阶段处理平台 Cookie 变化。
	latest, session, callErr, err = r.finishRemote(requestCtx, query, latest, session, callErr)
	if err != nil {
		return itemapp.SyncPageResult{}, err
	}
	if callErr != nil {
		r.recoverExpired(requestCtx, query.CookieID, callErr)
		return itemapp.SyncPageResult{}, syncStageError(itemapp.SyncErrorPlatform, callErr)
	}
	if result == nil {
		return itemapp.SyncPageResult{}, syncStageError(itemapp.SyncErrorPlatform, errors.New("商品列表接口未返回结果"))
	}
	// detailCookies 保存规格探测使用的最新 Cookie 串。
	detailCookies := cookieValue
	if result.UpdatedCookies != "" {
		detailCookies = result.UpdatedCookies
	}
	// detailErr 保存远端多规格探测错误。
	detailErr := r.enrichMultiSpec(requestCtx, detailCookies, query.CookieID, result.Items)
	if detailErr != nil {
		r.recoverExpired(requestCtx, query.CookieID, detailErr)
		return itemapp.SyncPageResult{}, syncStageError(itemapp.SyncErrorPlatform, detailErr)
	}
	// persistErr 保存探测后 Cookie 会话提交错误。
	persistErr := r.persistAfterEnrich(requestCtx, query, latest, session, cookieValue, result.UpdatedCookies)
	if persistErr != nil {
		return itemapp.SyncPageResult{}, persistErr
	}
	// saved 保存本页成功写入商品的数量。
	saved := r.saveItems(requestCtx, query.CookieID, result.Items)
	return itemapp.SyncPageResult{PageNumber: result.PageNumber, PageSize: result.PageSize, CurrentCount: len(result.Items), SavedCount: saved}, nil
}

// begin 读取并校验账号平台视图，同时创建本次请求的 Cookie 会话。
func (r *ItemSyncRepository) begin(ctx context.Context, query itemapp.SyncQuery) (*db.CookiePlatformRuntimeData, string, context.Context, *mtop.CookieSession, func(), error) {
	if r == nil || r.store == nil || r.store.Cookies == nil || r.store.Items == nil {
		return nil, "", ctx, nil, func() {}, syncStageError(itemapp.SyncErrorPersistence, errors.New("商品同步存储未初始化"))
	}
	// unlock 保护账号凭证快照的读取和提交。
	unlock := r.store.LockAccountCredentials(query.CookieID)
	// latest、loadErr 保存平台凭证视图及读取错误。
	latest, loadErr := r.store.Cookies.GetCookiePlatformRuntimeData(ctx, query.CookieID)
	if errors.Is(loadErr, db.ErrNotFound) {
		unlock()
		return nil, "", ctx, nil, func() {}, itemapp.ErrSyncNotOwned
	}
	if loadErr != nil {
		unlock()
		return nil, "", ctx, nil, func() {}, syncStageError(itemapp.SyncErrorPersistence, loadErr)
	}
	if latest.UserID != query.UserID {
		unlock()
		return nil, "", ctx, nil, func() {}, itemapp.ErrSyncNotOwned
	}
	if !hasStoredCredential(latest) {
		unlock()
		return nil, "", ctx, nil, func() {}, syncStageError(itemapp.SyncErrorCredential, errors.New("账号凭证已变化，请重试"))
	}
	// requestContext、session 保存平台调用需要的 Cookie 会话上下文。
	requestContext, session := withCookieSnapshot(ctx, latest)
	return &latest, latest.Value, requestContext, session, unlock, nil
}

// finishRemote 在远端调用后重新读取账号并提交 Cookie 会话变化。
func (r *ItemSyncRepository) finishRemote(ctx context.Context, query itemapp.SyncQuery, detail *db.CookiePlatformRuntimeData, session *mtop.CookieSession, callErr error) (*db.CookiePlatformRuntimeData, *mtop.CookieSession, error, error) {
	// unlock 保护远端调用完成后的凭证复核和写回。
	unlock := r.store.LockAccountCredentials(query.CookieID)
	defer unlock()
	// latest、loadErr 保存远端调用后的最新账号凭证视图。
	latest, loadErr := r.store.Cookies.GetCookiePlatformRuntimeData(ctx, query.CookieID)
	if loadErr != nil || latest.UserID != query.UserID {
		return nil, session, callErr, syncStageError(itemapp.SyncErrorCredential, errors.New("账号凭证已变化，请重试"))
	}
	// changed 表示远端调用期间凭证是否被其他流程更新。
	changed := latest.Value != detail.Value || latest.MetadataJSON != detail.MetadataJSON
	if !changed {
		// value、valueChanged、handled、persistErr 保存 Cookie 会话提交结果。
		_, _, _, persistErr := r.persistSession(ctx, latest, session)
		if persistErr != nil {
			return nil, session, callErr, syncStageError(itemapp.SyncErrorPersistence, persistErr)
		}
	}
	return &latest, session, callErr, nil
}

// persistAfterEnrich 在多规格探测后复核账号并写回平台 Cookie 变化。
func (r *ItemSyncRepository) persistAfterEnrich(ctx context.Context, query itemapp.SyncQuery, detail *db.CookiePlatformRuntimeData, session *mtop.CookieSession, originalCookie, updatedCookie string) error {
	// unlock 保护探测完成后的凭证复核和写回。
	unlock := r.store.LockAccountCredentials(query.CookieID)
	defer unlock()
	// latest、loadErr 保存探测完成后的最新账号凭证视图。
	latest, loadErr := r.store.Cookies.GetCookiePlatformRuntimeData(ctx, query.CookieID)
	if loadErr != nil || latest.UserID != query.UserID {
		return syncStageError(itemapp.SyncErrorCredential, errors.New("账号凭证已变化，请重试"))
	}
	// changed 表示平台请求期间是否已有其他流程写入凭证。
	changed := latest.Value != detail.Value || latest.MetadataJSON != detail.MetadataJSON
	if !changed {
		// value、valueChanged、handled、persistErr 保存 Cookie 会话提交结果。
		value, valueChanged, handled, persistErr := r.persistSession(ctx, latest, session)
		if persistErr != nil {
			return syncStageError(itemapp.SyncErrorPersistence, persistErr)
		}
		if handled && valueChanged {
			r.notifyRunningCookie(ctx, query.CookieID, value)
		} else if !handled && updatedCookie != "" && updatedCookie != originalCookie {
			// saveErr 保存平台返回的新 Cookie 写回错误。
			if saveErr := r.store.Cookies.UpdateValueOwned(ctx, query.CookieID, updatedCookie, query.UserID); saveErr != nil {
				return syncStageError(itemapp.SyncErrorPersistence, saveErr)
			}
			r.notifyRunningCookie(ctx, query.CookieID, updatedCookie)
		}
	}
	return nil
}

// persistSession 保存完整 Cookie Jar 或平面 Cookie 的会话变化。
func (r *ItemSyncRepository) persistSession(ctx context.Context, detail db.CookiePlatformRuntimeData, session *mtop.CookieSession) (string, bool, bool, error) {
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
	err := r.store.Cookies.UpdateRenewalCookie(ctx, detail.ID, value, metadata, time.Now().Unix())
	return value, value != detail.Value, true, err
}

// saveItems 保存分页同步返回的商品基础信息并返回成功数。
func (r *ItemSyncRepository) saveItems(ctx context.Context, cookieID string, items []mtop.ItemListItem) int {
	// saved 保存成功写入的商品数量。
	saved := 0
	// item 表示当前待写入的分页商品。
	for _, item := range items {
		// price 保存优先使用格式化价格的商品价格文本。
		price := item.PriceText
		if price == "" {
			price = item.Price
		}
		// saveErr 保存当前商品基础信息写入错误。
		saveErr := r.store.Items.UpsertBasic(ctx, &db.ItemInfoRow{CookieID: cookieID, ItemID: item.ID, ItemTitle: item.Title, ItemCategory: item.CategoryID, ItemPrice: price, ItemDetail: item.ItemDetail})
		if saveErr != nil {
			r.logger.Warn("保存商品失败", "cookie_id", cookieID, "item_id", item.ID, "err", saveErr)
			continue
		}
		if item.IsMultiSpec {
			// multiErr 保存当前商品多规格标记写入错误。
			if multiErr := r.store.Items.SetMultiSpec(ctx, cookieID, item.ID, true); multiErr != nil {
				r.logger.Warn("保存商品多规格状态失败", "cookie_id", cookieID, "item_id", item.ID, "err", multiErr)
			}
		}
		saved++
	}
	return saved
}

// syncItems 将平台商品模型转换为数据库行并执行全集 reconcile。
func (r *ItemSyncRepository) syncItems(ctx context.Context, cookieID string, items []mtop.ItemListItem) (db.ItemSyncResult, error) {
	// rows 保存待写入数据库的商品行。
	rows := make([]db.ItemInfoRow, 0, len(items))
	// item 表示当前待转换的平台商品。
	for _, item := range items {
		// price 保存优先使用格式化价格的商品价格文本。
		price := item.PriceText
		if price == "" {
			price = item.Price
		}
		rows = append(rows, db.ItemInfoRow{CookieID: cookieID, ItemID: item.ID, ItemTitle: item.Title, ItemCategory: item.CategoryID, ItemPrice: price, ItemDetail: item.ItemDetail, IsMultiSpec: item.IsMultiSpec})
	}
	return r.store.Items.SyncFromRemote(ctx, cookieID, rows)
}

// enrichMultiSpec 使用本地标记和有限并发远端详情探测补全多规格字段。
func (r *ItemSyncRepository) enrichMultiSpec(ctx context.Context, cookies, cookieID string, items []mtop.ItemListItem) error {
	// fetcher、ok 保存可选商品详情探测能力及其存在状态。
	fetcher, ok := r.mtopClient().(mtop.ItemDetailFetcher)
	if !ok {
		return nil
	}
	// itemIDs 保存本次批量读取本地标记的商品标识。
	itemIDs := make([]string, 0, len(items))
	// item 表示当前待收集标识的平台商品。
	for _, item := range items {
		itemIDs = append(itemIDs, item.ID)
	}
	// localFlags、flagsErr 保存本地多规格标记及查询错误。
	localFlags, flagsErr := r.store.Items.MultiSpecFlags(ctx, cookieID, itemIDs)
	if flagsErr != nil {
		r.logger.Warn("批量读取商品多规格标记失败，将继续远端探测", "cookie_id", cookieID, "err", flagsErr)
	}
	// candidates 保存需要远端探测的商品下标。
	candidates := make([]int, 0, len(items))
	// index 表示当前商品在结果切片中的下标。
	for index := range items {
		if items[index].IsMultiSpec || localFlags[items[index].ID] {
			items[index].IsMultiSpec = true
			r.cacheSpec(cookieID, items[index].ID, true)
			continue
		}
		// cachedValue、cached 表示是否命中短期多规格缓存。
		cachedValue, cached := r.cachedSpec(cookieID, items[index].ID)
		if cached {
			items[index].IsMultiSpec = cachedValue
			continue
		}
		candidates = append(candidates, index)
	}
	if len(candidates) == 0 {
		return nil
	}
	// probeCtx、cancel 控制批量详情探测的收束。
	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// semaphore 限制同时进行的详情探测数量。
	semaphore := make(chan struct{}, 4)
	// waitGroup 等待所有详情探测 goroutine 结束。
	var waitGroup sync.WaitGroup
	// errorMu 保护 sessionErr 的并发写入。
	var errorMu sync.Mutex
	// sessionErr 保存首个会话过期错误。
	var sessionErr error
	// index 表示当前需要启动详情探测的商品下标。
	for _, index := range candidates {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			select {
			case semaphore <- struct{}{}:
			case <-probeCtx.Done():
				return
			}
			defer func() { <-semaphore }()
			// isMultiSpec、detectErr 保存当前商品详情探测结果及错误。
			isMultiSpec, detectErr := fetcher.DetectItemMultiSpec(probeCtx, cookies, items[index].ID)
			if detectErr != nil {
				if mtop.IsSessionExpiredErr(detectErr) {
					errorMu.Lock()
					if sessionErr == nil {
						sessionErr = detectErr
						cancel()
					}
					errorMu.Unlock()
				}
				return
			}
			r.cacheSpec(cookieID, items[index].ID, isMultiSpec)
			items[index].IsMultiSpec = isMultiSpec
		}(index)
	}
	waitGroup.Wait()
	return sessionErr
}

// cachedSpec 读取未过期的多规格探测缓存。
func (r *ItemSyncRepository) cachedSpec(cookieID, itemID string) (bool, bool) {
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	// key 是账号和商品组成的缓存键。
	key := cookieID + "\x00" + itemID
	// entry、ok 保存缓存项及存在状态。
	entry, ok := r.cache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		delete(r.cache, key)
		return false, false
	}
	return entry.isMultiSpec, true
}

// cacheSpec 写入多规格探测结果并清理已过期缓存。
func (r *ItemSyncRepository) cacheSpec(cookieID, itemID string, value bool) {
	if itemID == "" {
		return
	}
	r.cacheMu.Lock()
	defer r.cacheMu.Unlock()
	if r.cache == nil {
		r.cache = make(map[string]itemSpecCacheEntry)
	}
	// now 是本次缓存清理和写入的时间基准。
	now := time.Now()
	// key、entry 分别表示待清理的缓存键和缓存项。
	for key, entry := range r.cache {
		if now.After(entry.expiresAt) {
			delete(r.cache, key)
		}
	}
	r.cache[cookieID+"\x00"+itemID] = itemSpecCacheEntry{isMultiSpec: value, expiresAt: now.Add(10 * time.Minute)}
}

// withCookieSnapshot 创建带完整 Cookie Jar 或平面 Cookie 的平台上下文。
func withCookieSnapshot(ctx context.Context, detail db.CookiePlatformRuntimeData) (context.Context, *mtop.CookieSession) {
	// snapshot、ok 保存 metadata 中的完整 Cookie Jar 快照及解析状态。
	snapshot, ok := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON)
	if !ok {
		return mtop.WithFlatCookieSession(ctx, detail.Value)
	}
	return mtop.WithCookieSnapshot(ctx, snapshot)
}

// hasStoredCredential 判断账号平台视图是否包含可用 Cookie 凭证。
func hasStoredCredential(detail db.CookiePlatformRuntimeData) bool {
	if strings.TrimSpace(detail.Value) != "" {
		return true
	}
	// complete 表示 metadata 是否包含完整 Cookie Jar。
	_, complete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON)
	return complete
}

// syncStageError 将底层错误包装为不泄露基础设施类型的应用错误。
func syncStageError(kind itemapp.SyncErrorKind, err error) error {
	return &itemapp.SyncError{Kind: kind, Err: err}
}

// notifyRunningCookie 将平台返回的 Cookie 更新到运行中的账号实例。
func (r *ItemSyncRepository) notifyRunningCookie(ctx context.Context, cookieID, value string) {
	if r.updateRunningCookie != nil && value != "" {
		r.updateRunningCookie(ctx, cookieID, value)
	}
}

// recoverExpired 在平台会话过期时通知账号恢复协调器。
func (r *ItemSyncRepository) recoverExpired(ctx context.Context, cookieID string, err error) {
	if r.recoverExpiredSession != nil && err != nil {
		r.recoverExpiredSession(ctx, cookieID, err)
	}
}

// mtopClient 返回当前商品同步使用的平台客户端，并在测试构造缺失时提供默认实现。
func (r *ItemSyncRepository) mtopClient() mtop.Client {
	if r != nil && r.client != nil {
		// client 表示当前回调返回的平台客户端。
		if client := r.client(); client != nil {
			return client
		}
	}
	return mtop.NewClient()
}
