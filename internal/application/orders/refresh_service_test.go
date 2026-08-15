package orders

import (
	"context"
	"errors"
	"testing"
)

// refreshRepositoryFake 是订单刷新应用服务使用的内存持久化 Port。
type refreshRepositoryFake struct {
	// owned 保存账号归属关系。
	owned map[string]bool
	// order 保存订单实体。
	order *Order
	// orders 保存按订单号索引的订单实体。
	orders map[string]*Order
	// rows 保存按账号读取的订单列表。
	rows []OrderRow
	// detail 保存账号平台请求视图。
	detail *PlatformRuntimeData
	// soldDeleteCount 保存软删除数量。
	soldDeleteCount int
	// upsertCount 保存订单写入次数。
	upsertCount int
	// batchUpsertCount 保存详情分片批量写入调用次数。
	batchUpsertCount int
	// batchFindCount 保存订单发现批量读取调用次数。
	batchFindCount int
	// batchFindErr 保存测试批量读取错误。
	batchFindErr error
	// batchUpsertErr 保存测试批量写入错误。
	batchUpsertErr error
	// transactionErr 保存事务错误。
	transactionErr error
	// loadErr 保存账号视图读取错误。
	loadErr error
}

// ExistsOwned 判断测试账号是否属于指定用户。
func (f *refreshRepositoryFake) ExistsOwned(context.Context, int64, string) (bool, error) {
	return f.owned["cookie-1"], nil
}

// ListOwnedIDs 返回测试用户账号列表。
func (f *refreshRepositoryFake) ListOwnedIDs(context.Context, int64) ([]string, error) {
	return []string{"cookie-1"}, nil
}

// GetOrder 返回测试订单实体。
func (f *refreshRepositoryFake) GetOrder(_ context.Context, orderID string) (*Order, error) {
	if f.orders != nil {
		return f.orders[orderID], nil
	}
	if f.order == nil || f.order.OrderID != orderID {
		return nil, nil
	}
	return f.order, nil
}

// FindOrder 返回测试订单及存在标记。
func (f *refreshRepositoryFake) FindOrder(_ context.Context, orderID string) (*Order, bool, error) {
	if f.orders != nil {
		// order、ok 保存测试订单及存在标记。
		order, ok := f.orders[orderID]
		return order, ok, nil
	}
	// order、err 保存测试订单及查询错误。
	order, err := f.GetOrder(context.Background(), orderID)
	return order, order != nil, err
}

// FindOrdersByIDs 返回测试订单发现批量读取结果。
func (f *refreshRepositoryFake) FindOrdersByIDs(_ context.Context, orderIDs []string) (map[string]*Order, error) {
	f.batchFindCount++
	if f.batchFindErr != nil {
		return nil, f.batchFindErr
	}
	// result 保存批量读取到的测试订单。
	result := make(map[string]*Order, len(orderIDs))
	if f.orders == nil {
		return result, nil
	}
	// orderID 是当前批量读取的订单标识。
	for _, orderID := range orderIDs {
		// order 保存当前标识对应的测试订单。
		if order := f.orders[orderID]; order != nil {
			result[orderID] = order
		}
	}
	return result, nil
}

// LockCredentials 返回无需等待的测试凭证锁。
func (f *refreshRepositoryFake) LockCredentials(string) func() {
	return func() {}
}

// LoadCookiePlatformDetail 返回测试平台请求视图。
func (f *refreshRepositoryFake) LoadCookiePlatformDetail(context.Context, string) (*PlatformRuntimeData, error) {
	return f.detail, f.loadErr
}

// UpdateRenewalCookie 接受测试 Cookie 更新。
func (f *refreshRepositoryFake) UpdateRenewalCookie(context.Context, string, string, string, int64) error {
	return nil
}

// UpsertOrder 记录测试订单写入。
func (f *refreshRepositoryFake) UpsertOrder(_ context.Context, orderID string, options UpsertOptions) error {
	f.upsertCount++
	if f.orders == nil {
		f.orders = map[string]*Order{}
	}
	// order 保存待更新的测试订单。
	order := f.orders[orderID]
	if order == nil {
		order = &Order{OrderID: orderID}
		f.orders[orderID] = order
	}
	order.CookieID, order.OrderStatus, order.Amount = options.CookieID, options.OrderStatus, options.Amount
	return nil
}

// BatchUpsertOrders 记录测试详情分片批量写入。
func (f *refreshRepositoryFake) BatchUpsertOrders(ctx context.Context, rows []RefreshOrderWrite) error {
	f.batchUpsertCount++
	if f.batchUpsertErr != nil {
		return f.batchUpsertErr
	}
	// row 是当前测试批量写入的订单详情。
	for _, row := range rows {
		// err 保存测试订单写入错误。
		if err := f.UpsertOrder(ctx, row.OrderID, row.Options); err != nil {
			return err
		}
	}
	return nil
}

// SoftDeleteMissingOrders 返回测试软删除数量。
func (f *refreshRepositoryFake) SoftDeleteMissingOrders(context.Context, string, map[string]struct{}) (int, error) {
	return f.soldDeleteCount, nil
}

// ListOrdersByCookieCursor 返回测试详情目标。
func (f *refreshRepositoryFake) ListOrdersByCookieCursor(context.Context, string, int, string, string) ([]OrderRow, error) {
	return f.rows, nil
}

// WithTransaction 执行测试事务回调并返回预置错误。
func (f *refreshRepositoryFake) WithTransaction(ctx context.Context, work func(Writer) error) error {
	if f.transactionErr != nil {
		return f.transactionErr
	}
	return work(refreshWriterFake{repository: f})
}

// refreshWriterFake 是订单刷新事务写入器。
type refreshWriterFake struct {
	// repository 指向内存持久化 Port。
	repository *refreshRepositoryFake
}

// PatchOrder 不执行测试补丁写入。
func (w refreshWriterFake) PatchOrder(context.Context, string, OrderPatch) error { return nil }

// UpsertItemBasic 不执行测试商品写入。
func (w refreshWriterFake) UpsertItemBasic(context.Context, ItemWrite) error { return nil }

// UpsertOrder 委托内存持久化 Port 写入订单。
func (w refreshWriterFake) UpsertOrder(ctx context.Context, orderID string, options UpsertOptions) error {
	return w.repository.UpsertOrder(ctx, orderID, options)
}

// refreshRuntimeFake 是订单刷新应用服务使用的平台运行时 Port。
type refreshRuntimeFake struct {
	// detailAvailable 表示详情接口是否可用。
	detailAvailable bool
	// soldAvailable 表示订单列表接口是否可用。
	soldAvailable bool
	// detailResult 保存详情请求结果。
	detailResult RefreshDetailFetchResult
	// soldResult 保存订单列表请求结果。
	soldResult RefreshSoldFetchResult
	// fetchErr 保存平台请求错误。
	fetchErr error
	// expired 表示请求错误是否为会话过期。
	expired bool
	// recovered 保存是否执行了会话恢复。
	recovered bool
	// updatedCookie 保存同步到运行时的 Cookie。
	updatedCookie string
}

// DetailAvailable 返回详情接口可用状态。
func (f *refreshRuntimeFake) DetailAvailable() bool { return f.detailAvailable }

// SoldAvailable 返回订单列表接口可用状态。
func (f *refreshRuntimeFake) SoldAvailable() bool { return f.soldAvailable }

// CredentialAvailable 判断测试平台请求视图是否有效。
func (f *refreshRuntimeFake) CredentialAvailable(detail *PlatformRuntimeData) bool {
	return detail != nil && detail.Value != ""
}

// FetchOrderDetail 返回预置订单详情结果。
func (f *refreshRuntimeFake) FetchOrderDetail(context.Context, *PlatformRuntimeData, string) (RefreshDetailFetchResult, error) {
	return f.detailResult, f.fetchErr
}

// FetchSoldOrders 返回预置订单列表结果。
func (f *refreshRuntimeFake) FetchSoldOrders(context.Context, *PlatformRuntimeData) (RefreshSoldFetchResult, error) {
	return f.soldResult, f.fetchErr
}

// PersistCookieSession 返回预置 Cookie 会话变化。
func (f *refreshRuntimeFake) PersistCookieSession(context.Context, *PlatformRuntimeData, RefreshCookieUpdate) (string, bool, bool, error) {
	return f.soldResult.CookieUpdate.Value, f.soldResult.CookieUpdate.Changed, true, nil
}

// UpdateRunningCookie 记录运行时 Cookie 更新。
func (f *refreshRuntimeFake) UpdateRunningCookie(_ context.Context, _, value string) {
	f.updatedCookie = value
}

// RecoverExpiredSession 记录会话恢复调用。
func (f *refreshRuntimeFake) RecoverExpiredSession(context.Context, string, error) bool {
	f.recovered = true
	return true
}

// IsSessionExpired 返回预置会话过期标记。
func (f *refreshRuntimeFake) IsSessionExpired(error) bool { return f.expired }

// TestRefreshSingleSuccess 验证单订单刷新会写入详情并返回兼容结果。
func TestRefreshSingleSuccess(t *testing.T) {
	// repository 保存本用例的内存持久化依赖。
	repository := &refreshRepositoryFake{owned: map[string]bool{"cookie-1": true}, order: &Order{OrderID: "order-1", CookieID: "cookie-1", OrderStatus: "processing"}, detail: &PlatformRuntimeData{UserID: 7, Value: "cookie"}}
	// runtime 保存本用例的平台运行时依赖。
	runtime := &refreshRuntimeFake{detailAvailable: true, detailResult: RefreshDetailFetchResult{Detail: &RefreshDetail{OrderStatus: "2", Quantity: "2", Amount: "12.00"}}}
	// result、err 保存单订单刷新结果和错误。
	result, err := NewRefreshService(repository, runtime, 1).RefreshSingle(context.Background(), 7, "order-1")
	if err != nil || !result.Success || repository.upsertCount != 1 || result.Detail.OrderStatus != "pending_ship" {
		t.Fatalf("单订单刷新结果异常: result=%+v err=%v", result, err)
	}
}

// TestRefreshSingleRejectsUnsupportedAndCredentialChanges 验证单订单刷新拒绝不支持接口和失效凭证。
func TestRefreshSingleRejectsUnsupportedAndCredentialChanges(t *testing.T) {
	// baseRepository 保存本用例的基础持久化依赖。
	baseRepository := &refreshRepositoryFake{owned: map[string]bool{"cookie-1": true}, order: &Order{OrderID: "order-1", CookieID: "cookie-1"}, detail: &PlatformRuntimeData{UserID: 7, Value: "cookie"}}
	// unsupportedRuntime 保存不支持详情接口的运行时依赖。
	unsupportedRuntime := &refreshRuntimeFake{}
	// err 保存详情接口不支持错误。
	if _, err := NewRefreshService(baseRepository, unsupportedRuntime, 1).RefreshSingle(context.Background(), 7, "order-1"); !errors.Is(err, ErrRefreshDetailUnsupported) {
		t.Fatalf("未返回详情接口不支持错误: %v", err)
	}
	// credentialRepository 保存用户不匹配的持久化依赖。
	credentialRepository := &refreshRepositoryFake{owned: map[string]bool{"cookie-1": true}, order: baseRepository.order, detail: &PlatformRuntimeData{UserID: 8, Value: "cookie"}}
	// credentialRuntime 保存可用详情接口运行时依赖。
	credentialRuntime := &refreshRuntimeFake{detailAvailable: true}
	// err 保存凭证变化错误。
	if _, err := NewRefreshService(credentialRepository, credentialRuntime, 1).RefreshSingle(context.Background(), 7, "order-1"); !errors.Is(err, ErrRefreshCredentialChanged) {
		t.Fatalf("未返回凭证变化错误: %v", err)
	}
}

// TestRefreshBatchDiscoveryAndDetails 验证批量刷新会发现订单、清理缺失记录并补全详情。
func TestRefreshBatchDiscoveryAndDetails(t *testing.T) {
	// repository 保存批量刷新使用的内存持久化依赖。
	repository := &refreshRepositoryFake{
		owned:           map[string]bool{"cookie-1": true},
		detail:          &PlatformRuntimeData{UserID: 7, Value: "cookie"},
		orders:          map[string]*Order{"order-1": {OrderID: "order-1", CookieID: "cookie-1", OrderStatus: "processing"}},
		rows:            []OrderRow{{OrderID: "order-1", OrderStatus: "processing", CreatedAt: "1"}},
		soldDeleteCount: 1,
	}
	// runtime 保存批量刷新使用的平台运行时依赖。
	runtime := &refreshRuntimeFake{
		soldAvailable: true, detailAvailable: true,
		soldResult:   RefreshSoldFetchResult{Orders: []RefreshSoldOrder{{OrderID: "order-1", OrderStatus: "2", Amount: "¥12.00"}, {OrderID: "order-2", OrderStatus: "2"}}},
		detailResult: RefreshDetailFetchResult{Detail: &RefreshDetail{OrderStatus: "3", Amount: "12.00"}},
	}
	// result、err 保存批量刷新结果和错误。
	result, err := NewRefreshService(repository, runtime, 1).Refresh(context.Background(), 7, "", "all")
	if err != nil || result.Summary.Discovered != 1 || result.Summary.SoftDeleted != 1 || result.Summary.DetailTotal == 0 || repository.upsertCount == 0 || repository.batchFindCount != 1 || repository.batchUpsertCount != 2 {
		t.Fatalf("批量刷新结果异常: result=%+v err=%v repository=%+v", result, err, repository)
	}
}

// TestPersistSoldOrdersBatchesLookupAndWrite 验证订单发现会去重并批量读取、写入本地订单。
func TestPersistSoldOrdersBatchesLookupAndWrite(t *testing.T) {
	// repository 保存订单发现使用的内存持久化依赖。
	repository := &refreshRepositoryFake{orders: map[string]*Order{"existing": {OrderID: "existing", CookieID: "cookie-1", OrderStatus: "processing", Amount: "1.00"}}}
	// service 保存仅用于调用订单发现持久化的应用服务。
	service := &RefreshService{repository: repository}
	// discovered、updated、newIDs、remoteIDs、err 保存批量发现结果。
	discovered, updated, newIDs, remoteIDs, err := service.persistSoldOrders(context.Background(), "cookie-1", []RefreshSoldOrder{
		{OrderID: " existing ", OrderStatus: "unknown", Amount: "2.00"},
		{OrderID: "new-order", OrderStatus: "processing", Amount: "3.00"},
		{OrderID: "new-order", OrderStatus: "processing", Amount: "3.00"},
	})
	if err != nil || discovered != 1 || updated != 1 || len(newIDs) != 1 || len(remoteIDs) != 2 || repository.batchFindCount != 1 || repository.batchUpsertCount != 1 || repository.upsertCount != 2 || repository.orders["existing"].OrderStatus != "processing" {
		t.Fatalf("批量订单发现结果异常: discovered=%d updated=%d new=%v remote=%v repository=%+v err=%v", discovered, updated, newIDs, remoteIDs, repository, err)
	}
}

// TestPersistSoldOrdersReturnsBatchWriteError 验证批量写入失败不会返回虚假的发现统计。
func TestPersistSoldOrdersReturnsBatchWriteError(t *testing.T) {
	// writeErr 保存预置的批量写入错误。
	writeErr := errors.New("批量写入失败")
	// repository 保存返回批量写入错误的内存依赖。
	repository := &refreshRepositoryFake{batchUpsertErr: writeErr}
	// service 保存订单刷新应用服务。
	service := &RefreshService{repository: repository}
	// discovered、updated、newIDs、remoteIDs、err 保存批量写入失败结果。
	discovered, updated, newIDs, remoteIDs, err := service.persistSoldOrders(context.Background(), "cookie-1", []RefreshSoldOrder{{OrderID: "failed-order", OrderStatus: "processing"}})
	if err == nil || discovered != 0 || updated != 0 || len(newIDs) != 0 || len(remoteIDs) != 1 || !errors.Is(err, writeErr) {
		t.Fatalf("批量写入失败结果异常: discovered=%d updated=%d new=%v remote=%v err=%v", discovered, updated, newIDs, remoteIDs, err)
	}
}
