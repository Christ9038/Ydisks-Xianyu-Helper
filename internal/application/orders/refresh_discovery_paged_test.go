package orders

import (
	"context"
	"errors"
	"testing"
)

// pagedRepository 在通用刷新替身上记录缺失订单清理收到的完整快照集合。
type pagedRepository struct {
	*refreshRepositoryFake
	// deleteCalls 是缺失订单清理被调用的次数。
	deleteCalls int
	// deletedActive 是最近一次清理收到的平台订单标识集合。
	deletedActive map[string]struct{}
}

// SoftDeleteMissingOrders 记录清理调用及其收到的完整快照集合。
func (r *pagedRepository) SoftDeleteMissingOrders(ctx context.Context, cookieID string, activeIDs map[string]struct{}) (int, error) {
	r.deleteCalls++
	r.deletedActive = activeIDs
	return r.refreshRepositoryFake.SoftDeleteMissingOrders(ctx, cookieID, activeIDs)
}

// pagedRuntime 在通用运行时替身上提供脚本化的分页读取能力。
type pagedRuntime struct {
	*refreshRuntimeFake
	// pages 是按顺序交给 handler 的订单页。
	pages []RefreshSoldPage
	// failAfter 为非 nil 时，在提交完全部 pages 后返回该错误，模拟中途失败或取消。
	failAfter error
	// betweenPages 在每页提交成功后被调用，index 为刚提交页的下标，用于模拟页间的外部变化。
	betweenPages func(index int)
}

// FetchSoldOrdersPaged 按脚本逐页调用 handler；handler 报错立即终止。
func (r *pagedRuntime) FetchSoldOrdersPaged(ctx context.Context, _ *PlatformRuntimeData, handler RefreshSoldPageHandler) (RefreshSoldFetchResult, error) {
	// final 与真实运行时一致，携带最后一页的累计 Cookie 状态。
	final := RefreshSoldFetchResult{}
	// index、page 是当前提交的页下标和内容。
	for index, page := range r.pages {
		final.CookieUpdate = page.CookieUpdate
		// err 是 handler 提交当前页的结果；非空时按真实运行时语义立即终止后续分页。
		if err := handler(ctx, page); err != nil {
			return final, err
		}
		if r.betweenPages != nil {
			r.betweenPages(index)
		}
	}
	return final, r.failAfter
}

// newPagedFixture 构造带两页脚本的服务、仓储与运行时，账号 cookie-1 归用户 7 所有。
func newPagedFixture(pages ...RefreshSoldPage) (*RefreshService, *pagedRepository, *pagedRuntime) {
	// repository 是带清理记录的内存仓储。
	repository := &pagedRepository{refreshRepositoryFake: &refreshRepositoryFake{owned: map[string]bool{"cookie-1": true}, detail: &PlatformRuntimeData{ID: "cookie-1", UserID: 7, Value: "cookie"}}}
	// runtime 是脚本化的分页运行时。
	runtime := &pagedRuntime{refreshRuntimeFake: &refreshRuntimeFake{soldAvailable: true}, pages: pages}
	return NewRefreshService(repository, runtime, 1), repository, runtime
}

// pageOf 构造只含给定订单号的订单页。
func pageOf(orderIDs ...string) RefreshSoldPage {
	// orders 保存页内订单。
	orders := make([]RefreshSoldOrder, 0, len(orderIDs))
	// id 是页内订单号。
	for _, id := range orderIDs {
		orders = append(orders, RefreshSoldOrder{OrderID: id, OrderStatus: "pending_ship"})
	}
	return RefreshSoldPage{Orders: orders}
}

// TestPagedDiscoveryCommitsEveryPageThenCleansMissing 验证完整分页逐页落库，且缺失清理收到所有页的并集。
func TestPagedDiscoveryCommitsEveryPageThenCleansMissing(t *testing.T) {
	// service、repository 是被测刷新服务及可记录缺失清理的内存仓储。
	service, repository, _ := newPagedFixture(pageOf("o1", "o2"), pageOf("o3"))
	repository.soldDeleteCount = 4
	// result、err 保存完整刷新结果。
	result, err := service.Refresh(context.Background(), 7, "cookie-1", "")
	if err != nil || result.PartialFailure || result.Summary.Discovered != 3 || result.Summary.SoftDeleted != 4 {
		t.Fatalf("完整分页结果异常: result=%+v err=%v", result, err)
	}
	if repository.batchUpsertCount != 2 || repository.deleteCalls != 1 || len(repository.deletedActive) != 3 {
		t.Fatalf("应逐页写入并仅在末尾清理一次: batches=%d deletes=%d active=%v", repository.batchUpsertCount, repository.deleteCalls, repository.deletedActive)
	}
}

// TestPagedDiscoveryKeepsCommittedPagesWhenLaterPageFails 验证后续页失败时前序页保留且不做缺失清理。
func TestPagedDiscoveryKeepsCommittedPagesWhenLaterPageFails(t *testing.T) {
	// service、repository、runtime 是被测刷新服务、内存仓储和可在末尾注入失败的脚本化运行时。
	service, repository, runtime := newPagedFixture(pageOf("o1", "o2"))
	runtime.failAfter = errors.New("第 2 页平台超时")
	// result、err 保存刷新结果；账号级失败不以 error 返回，而计入失败统计。
	result, err := service.Refresh(context.Background(), 7, "cookie-1", "")
	if err != nil || !result.PartialFailure || result.Summary.Discovered != 2 || result.Summary.Failed != 1 {
		t.Fatalf("部分失败结果异常: result=%+v err=%v", result, err)
	}
	if repository.deleteCalls != 0 || repository.orders["o1"] == nil || repository.orders["o2"] == nil {
		t.Fatalf("前序页必须保留且禁止缺失清理: deletes=%d orders=%v", repository.deleteCalls, repository.orders)
	}
}

// TestPagedDiscoveryStopsWhenCredentialChangesBetweenPages 验证页间凭证变化会终止后续页而保留已提交页。
func TestPagedDiscoveryStopsWhenCredentialChangesBetweenPages(t *testing.T) {
	// service、repository、runtime 是被测服务、内存仓储和可在页间改写账号凭证的运行时。
	service, repository, runtime := newPagedFixture(pageOf("o1"), pageOf("o2"))
	runtime.betweenPages = func(int) { repository.detail = &PlatformRuntimeData{ID: "cookie-1", UserID: 7, Value: "relogin"} }
	// discovered、updated、_、err 保存单账号发现结果。
	discovered, _, _, err := service.discoverAccount(context.Background(), 7, "cookie-1")
	if !errors.Is(err, ErrRefreshCredentialChanged) || discovered != 1 {
		t.Fatalf("凭证变化应终止第二页并保留第一页: discovered=%d err=%v", discovered, err)
	}
	if repository.orders["o1"] == nil || repository.orders["o2"] != nil || repository.deleteCalls != 0 {
		t.Fatalf("第二页不得写入、不得清理: orders=%v deletes=%d", repository.orders, repository.deleteCalls)
	}
}

// TestPagedDiscoveryCancellationKeepsPriorPages 验证取消发生在第一页之后时已提交页保留并返回取消错误。
func TestPagedDiscoveryCancellationKeepsPriorPages(t *testing.T) {
	// service、repository、runtime 是被测服务、内存仓储和可在页间触发取消的运行时。
	service, repository, runtime := newPagedFixture(pageOf("o1"), pageOf("o2"))
	// ctx、cancel 模拟任务在第一页提交后被取消。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime.betweenPages = func(int) { cancel() }
	// discovered、_、_、err 保存单账号发现结果。
	discovered, _, _, err := service.discoverAccount(ctx, 7, "cookie-1")
	if !errors.Is(err, context.Canceled) || discovered != 1 || repository.deleteCalls != 0 || repository.orders["o2"] != nil {
		t.Fatalf("取消应保留第一页并跳过清理: discovered=%d err=%v deletes=%d orders=%v", discovered, err, repository.deleteCalls, repository.orders)
	}
}

// TestPagedDiscoveryEmptyListStillCleansMissing 验证平台省略 cardList 的空列表仍作为完整快照完成清理。
func TestPagedDiscoveryEmptyListStillCleansMissing(t *testing.T) {
	// service、repository 是被测刷新服务及记录缺失清理调用的内存仓储。
	service, repository, _ := newPagedFixture(pageOf())
	repository.soldDeleteCount = 2
	// result、err 保存空列表刷新结果。
	result, err := service.Refresh(context.Background(), 7, "cookie-1", "")
	if err != nil || result.PartialFailure || result.Summary.SoftDeleted != 2 || repository.deleteCalls != 1 || len(repository.deletedActive) != 0 {
		t.Fatalf("空列表应正常完成并执行清理: result=%+v err=%v deletes=%d", result, err, repository.deleteCalls)
	}
}

// TestPagedDiscoveryRefreshesChatCacheOnce 验证多页缺会话时一次发现只刷新一次联系人缓存。
func TestPagedDiscoveryRefreshesChatCacheOnce(t *testing.T) {
	// firstPage、secondPage 都带买家与商品但没有本地会话。
	firstPage := RefreshSoldPage{Orders: []RefreshSoldOrder{{OrderID: "o1", BuyerID: "b1", ItemID: "i1"}}}
	// secondPage 是第二页订单，买家商品同样没有本地会话。
	secondPage := RefreshSoldPage{Orders: []RefreshSoldOrder{{OrderID: "o2", BuyerID: "b2", ItemID: "i2"}}}
	// service、runtime 是被测服务及统计联系人刷新次数的运行时。
	service, _, runtime := newPagedFixture(firstPage, secondPage)
	// err 是整次刷新的返回错误，联系人刷新失败不应影响订单落库。
	if _, err := service.Refresh(context.Background(), 7, "cookie-1", ""); err != nil {
		t.Fatalf("刷新不应失败: %v", err)
	}
	if runtime.chatRefreshCalls != 1 {
		t.Fatalf("联系人缓存应只刷新一次，实际 %d", runtime.chatRefreshCalls)
	}
}

// TestPagedDiscoveryPersistsChangedCookieOnceAndRebasesCredential 验证 Cookie 变化只写入一次，且后续页以新凭证为基线不误判变化。
func TestPagedDiscoveryPersistsChangedCookieOnceAndRebasesCredential(t *testing.T) {
	// update 是第一页起就存在的累计 Cookie 变化。
	update := RefreshCookieUpdate{Value: "renewed", MetadataJSON: "{}", Changed: true, Handled: true}
	// firstPage、secondPage 是携带同一累计 Cookie 变化的两页订单。
	firstPage, secondPage := pageOf("o1"), pageOf("o2")
	firstPage.CookieUpdate, secondPage.CookieUpdate = update, update
	// repository、runtime 是内存仓储和脚本化运行时；服务在包装持久化计数后重建。
	_, repository, runtime := newPagedFixture(firstPage, secondPage)
	// persistCalls 统计会话持久化调用次数。
	persistCalls := 0
	// wrapped 在每次写入 Cookie 后让账号视图反映新凭证，模拟仓储真实行为。
	wrapped := &countingPersistRuntime{pagedRuntime: runtime, calls: &persistCalls, repository: repository}
	// service 使用计数包装后的运行时，统计 Cookie 会话实际写入次数。
	service := NewRefreshService(repository, wrapped, 1)
	// discovered、_、_、err 保存单账号发现结果。
	discovered, _, _, err := service.discoverAccount(context.Background(), 7, "cookie-1")
	if err != nil || discovered != 2 || persistCalls != 1 {
		t.Fatalf("Cookie 只应写入一次且两页都提交: discovered=%d persist=%d err=%v", discovered, persistCalls, err)
	}
}

// countingPersistRuntime 统计 Cookie 持久化次数，并在写入后让账号视图反映新 Cookie。
type countingPersistRuntime struct {
	*pagedRuntime
	// calls 指向外部持久化计数器。
	calls *int
	// repository 是需要在写入后更新账号视图的仓储。
	repository *pagedRepository
}

// PersistCookieSession 计数并模拟凭证写入后的账号视图变化。
func (r *countingPersistRuntime) PersistCookieSession(_ context.Context, _ *PlatformRuntimeData, update RefreshCookieUpdate) (string, bool, bool, error) {
	*r.calls++
	r.repository.detail = &PlatformRuntimeData{ID: "cookie-1", UserID: 7, Value: update.Value, MetadataJSON: update.MetadataJSON}
	return update.Value, true, true, nil
}

// TestPagedDiscoveryBlockedPageSkipsCleanupButContinues 验证归属冲突页被隔离：其余页继续同步，但不清理缺失订单。
func TestPagedDiscoveryBlockedPageSkipsCleanupButContinues(t *testing.T) {
	// repository、runtime 是内存仓储和两页脚本运行时；foreign 订单归属由包装仓储伪造。
	_, repository, runtime := newPagedFixture(pageOf("foreign"), pageOf("o2"))
	// 其他账号已拥有 foreign 订单，且无法证明可迁移。
	service := NewRefreshService(&ownershipRepository{pagedRepository: repository}, runtime, 1)
	// discovered、_、_、err 保存单账号发现结果。
	discovered, _, _, err := service.discoverAccount(context.Background(), 7, "cookie-1")
	if !errors.Is(err, ErrSoldRecoveryUnsafe) || discovered != 1 || repository.deleteCalls != 0 || repository.orders["o2"] == nil {
		t.Fatalf("冲突页应隔离且后续页继续: discovered=%d err=%v deletes=%d orders=%v", discovered, err, repository.deleteCalls, repository.orders)
	}
}

// ownershipRepository 让 foreign 订单报告为属于其他账号的活跃订单，其余订单不存在。
type ownershipRepository struct {
	*pagedRepository
}

// FindOrderOwnership 返回 foreign 属于其他账号，其余订单视为不存在。
func (r *ownershipRepository) FindOrderOwnership(_ context.Context, _ int64, orderID string) (RefreshOwnership, error) {
	if orderID == "foreign" {
		return RefreshOwnership{Owned: true, CookieID: "other", OrderID: orderID}, nil
	}
	return RefreshOwnership{}, ErrNotFound
}
