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
	// chatIDs 保存按买家和商品组合匹配出的测试会话标识。
	chatIDs map[string][]string
	// chatMatchErr 保存测试会话匹配错误。
	chatMatchErr error
	// batchUpsertErr 保存测试批量写入错误。
	batchUpsertErr error
	// transactionErr 保存事务错误。
	transactionErr error
	// loadErr 保存账号视图读取错误。
	loadErr error
	// getErr 保存单订单读取错误。
	getErr error
	// ownedErr 保存账号归属查询错误。
	ownedErr error
	// listOwnedErr 保存用户账号列表查询错误。
	listOwnedErr error
	// existsResult 保存账号归属查询结果；未设置时沿用 owned 映射。
	existsResult *bool
	// rowsErr 保存详情目标扫描错误。
	rowsErr error
	// deleteErr 保存缺失订单清理错误。
	deleteErr error
	// updateCookieErr 保存扁平 Cookie 写入错误。
	updateCookieErr error
	// upsertErr 保存单订单详情写入错误。
	upsertErr error
	// loadDetails 保存按读取顺序返回的账号视图。
	loadDetails []*PlatformRuntimeData
	// loadErrors 保存按读取顺序返回的账号视图错误。
	loadErrors []error
	// loadCalls 保存账号视图读取次数。
	loadCalls int
}

// ExistsOwned 判断测试账号是否属于指定用户。
func (f *refreshRepositoryFake) ExistsOwned(context.Context, int64, string) (bool, error) {
	if f.ownedErr != nil {
		return false, f.ownedErr
	}
	if f.existsResult != nil {
		return *f.existsResult, nil
	}
	return f.owned["cookie-1"], nil
}

// ListOwnedIDs 返回测试用户账号列表。
func (f *refreshRepositoryFake) ListOwnedIDs(context.Context, int64) ([]string, error) {
	if f.listOwnedErr != nil {
		return nil, f.listOwnedErr
	}
	return []string{"cookie-1"}, nil
}

// GetOrder 返回测试订单实体。
func (f *refreshRepositoryFake) GetOrder(_ context.Context, orderID string) (*Order, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
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

// FindOrdersByIDs 返回 cookieID 内的测试订单；ctx 不访问外部资源，orderIDs 确定批量读取范围。
func (f *refreshRepositoryFake) FindOrdersByIDs(_ context.Context, cookieID string, orderIDs []string) (map[string]*Order, error) {
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
		if order := f.orders[orderID]; order != nil && (order.CookieID == cookieID || order.CookieID == "") {
			result[orderID] = order
		}
	}
	return result, nil
}

// FindChatIDsByBuyerAndItem 返回测试账号下按买家和商品匹配的会话标识。
func (f *refreshRepositoryFake) FindChatIDsByBuyerAndItem(_ context.Context, _, buyerID, itemID string) ([]string, error) {
	if f.chatMatchErr != nil {
		return nil, f.chatMatchErr
	}
	// key 保存测试买家与商品的组合键。
	key := buyerID + "\x00" + itemID
	return append([]string(nil), f.chatIDs[key]...), nil
}

// LockCredentials 返回无需等待的测试凭证锁。
func (f *refreshRepositoryFake) LockCredentials(string) func() {
	return func() {}
}

// LoadCookiePlatformDetail 返回测试平台请求视图。
func (f *refreshRepositoryFake) LoadCookiePlatformDetail(context.Context, string) (*PlatformRuntimeData, error) {
	// index 保存本次账号视图读取在预置序列中的位置。
	index := f.loadCalls
	f.loadCalls++
	if index < len(f.loadDetails) || index < len(f.loadErrors) {
		// detail 保存本次读取的预置账号视图。
		detail := f.detail
		if index < len(f.loadDetails) {
			detail = f.loadDetails[index]
		}
		// loadErr 保存本次读取的预置错误。
		var loadErr error
		if index < len(f.loadErrors) {
			loadErr = f.loadErrors[index]
		}
		return detail, loadErr
	}
	return f.detail, f.loadErr
}

// UpdateRenewalCookie 接受测试 Cookie 更新。
func (f *refreshRepositoryFake) UpdateRenewalCookie(context.Context, string, string, string, int64) error {
	return f.updateCookieErr
}

// UpsertOrder 记录测试订单写入。
func (f *refreshRepositoryFake) UpsertOrder(_ context.Context, orderID string, options UpsertOptions) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
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
	// options 中的非空字段模拟生产仓储的增量合并，避免未匹配会话覆盖已有关联。
	if options.CookieID != "" {
		order.CookieID = options.CookieID
	}
	if options.ItemID != "" {
		order.ItemID = options.ItemID
	}
	if options.BuyerID != "" {
		order.BuyerID = options.BuyerID
	}
	if options.ChatID != "" {
		order.ChatID = options.ChatID
	}
	if options.CreatedAt != "" {
		order.CreatedAt = options.CreatedAt
	}
	if options.OrderStatus != "" {
		order.OrderStatus = options.OrderStatus
	}
	if options.Amount != "" {
		order.Amount = options.Amount
	}
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
	if f.deleteErr != nil {
		return 0, f.deleteErr
	}
	return f.soldDeleteCount, nil
}

// ListOrdersByCookieCursor 返回测试详情目标。
func (f *refreshRepositoryFake) ListOrdersByCookieCursor(context.Context, string, int, string, string) ([]OrderRow, error) {
	return f.rows, f.rowsErr
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
	// detailResults 保存按请求顺序返回的详情结果。
	detailResults []RefreshDetailFetchResult
	// detailErrors 保存按请求顺序返回的详情错误。
	detailErrors []error
	// detailCalls 保存详情接口调用次数。
	detailCalls int
	// persistConfigured 表示是否使用显式会话持久化结果。
	persistConfigured bool
	// persistValue 保存显式会话持久化返回的 Cookie。
	persistValue string
	// persistChanged 保存显式会话持久化是否报告 Cookie 变化。
	persistChanged bool
	// persistHandled 保存显式会话持久化是否接管了完整 Cookie Jar。
	persistHandled bool
	// persistErr 保存显式会话持久化错误。
	persistErr error
	// recoverCalls 保存会话恢复调用次数。
	recoverCalls int
	// chatRefresh 保存按需刷新聊天联系人的测试回调。
	chatRefresh func(context.Context, string) error
	// chatRefreshCalls 保存按需刷新聊天联系人的调用次数。
	chatRefreshCalls int
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
	// index 保存本次详情请求在预置序列中的位置。
	index := f.detailCalls
	f.detailCalls++
	// result 保存本次详情请求的默认结果。
	result := f.detailResult
	if index < len(f.detailResults) {
		result = f.detailResults[index]
	}
	// fetchErr 保存本次详情请求的预置错误。
	fetchErr := f.fetchErr
	if index < len(f.detailErrors) {
		fetchErr = f.detailErrors[index]
	}
	return result, fetchErr
}

// FetchSoldOrders 返回预置订单列表结果。
func (f *refreshRuntimeFake) FetchSoldOrders(context.Context, *PlatformRuntimeData) (RefreshSoldFetchResult, error) {
	return f.soldResult, f.fetchErr
}

// PersistCookieSession 返回预置 Cookie 会话变化。
func (f *refreshRuntimeFake) PersistCookieSession(context.Context, *PlatformRuntimeData, RefreshCookieUpdate) (string, bool, bool, error) {
	if f.persistConfigured {
		return f.persistValue, f.persistChanged, f.persistHandled, f.persistErr
	}
	return f.soldResult.CookieUpdate.Value, f.soldResult.CookieUpdate.Changed, true, nil
}

// UpdateRunningCookie 记录运行时 Cookie 更新。
func (f *refreshRuntimeFake) UpdateRunningCookie(_ context.Context, _, value string) {
	f.updatedCookie = value
}

// RecoverExpiredSession 记录会话恢复调用。
func (f *refreshRuntimeFake) RecoverExpiredSession(context.Context, string, error) bool {
	f.recovered = true
	f.recoverCalls++
	return true
}

// IsSessionExpired 返回预置会话过期标记。
func (f *refreshRuntimeFake) IsSessionExpired(error) bool { return f.expired }

// RefreshChatConversations 执行测试预置的聊天联系人刷新回调。
func (f *refreshRuntimeFake) RefreshChatConversations(ctx context.Context, cookieID string) error {
	f.chatRefreshCalls++
	if f.chatRefresh == nil {
		return nil
	}
	return f.chatRefresh(ctx, cookieID)
}

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
	// result 保存批量刷新结果。
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
		{OrderID: " existing ", CreatedAt: "2024-01-02T03:04:05Z", OrderStatus: "unknown", Amount: "2.00"},
		{OrderID: "new-order", CreatedAt: "2024-01-03T03:04:05Z", OrderStatus: "processing", Amount: "3.00"},
		{OrderID: "new-order", CreatedAt: "2024-01-03T03:04:05Z", OrderStatus: "processing", Amount: "3.00"},
	})
	if err != nil || discovered != 1 || updated != 1 || len(newIDs) != 1 || len(remoteIDs) != 2 || repository.batchFindCount != 1 || repository.batchUpsertCount != 1 || repository.upsertCount != 2 || repository.orders["existing"].OrderStatus != "processing" || repository.orders["existing"].CreatedAt != "2024-01-02T03:04:05Z" || repository.orders["new-order"].CreatedAt != "2024-01-03T03:04:05Z" {
		t.Fatalf("批量订单发现结果异常: discovered=%d updated=%d new=%v remote=%v repository=%+v err=%v", discovered, updated, newIDs, remoteIDs, repository, err)
	}
}

// TestPersistSoldOrdersBackfillsUniqueChatID 验证订单同步能以账号、买家和商品唯一回填聊天会话。
func TestPersistSoldOrdersBackfillsUniqueChatID(t *testing.T) {
	// repository 保存唯一会话匹配结果和订单写入状态。
	repository := &refreshRepositoryFake{chatIDs: map[string][]string{"buyer-1\x00item-1": {"chat-1"}}}
	// service 保存仅用于调用订单发现持久化的应用服务。
	service := &RefreshService{repository: repository}
	// discovered、updated、newIDs、remoteIDs、err 保存本次订单同步结果。
	discovered, updated, newIDs, remoteIDs, err := service.persistSoldOrders(context.Background(), "cookie-1", []RefreshSoldOrder{{
		OrderID: "order-1", ItemID: "item-1", BuyerID: "buyer-1", OrderStatus: "pending_ship", Amount: "3.00",
	}})
	// order 保存同步后可供完整发货使用的本地订单。
	order := repository.orders["order-1"]
	if err != nil || discovered != 1 || updated != 0 || len(newIDs) != 1 || len(remoteIDs) != 1 || order == nil || order.ChatID != "chat-1" {
		t.Fatalf("同步会话回填异常: discovered=%d updated=%d new=%v remote=%v order=%+v err=%v", discovered, updated, newIDs, remoteIDs, order, err)
	}
}

// TestPersistSoldOrdersRefreshesChatsAndRetriesMatch 验证首次匹配不到时只刷新一次聊天并重试关联。
func TestPersistSoldOrdersRefreshesChatsAndRetriesMatch(t *testing.T) {
	// repository 保存首次为空、刷新后出现候选会话的内存状态。
	repository := &refreshRepositoryFake{chatIDs: map[string][]string{}}
	// runtime 保存刷新聊天后补入候选会话的测试运行时。
	runtime := &refreshRuntimeFake{chatRefresh: func(_ context.Context, cookieID string) error {
		if cookieID != "cookie-1" {
			t.Fatalf("聊天刷新账号错误: %q", cookieID)
		}
		repository.chatIDs["buyer-1\x00item-1"] = []string{"chat-after-refresh"}
		return nil
	}}
	// service 保存订单发现持久化及可选聊天刷新能力。
	service := &RefreshService{repository: repository, runtime: runtime}
	// prepareErr 保存锁外联系人准备结果，后续持久化只读取缓存。
	if prepareErr := service.prepareSoldChats(context.Background(), "cookie-1", []RefreshSoldOrder{{OrderID: "order-1", ItemID: "item-1", BuyerID: "buyer-1"}}); prepareErr != nil {
		t.Fatal(prepareErr)
	}

	// discovered、updated、newIDs、remoteIDs、err 保存聊天刷新重试后的订单同步结果。
	discovered, updated, newIDs, remoteIDs, err := service.persistSoldOrders(context.Background(), "cookie-1", []RefreshSoldOrder{{
		OrderID: "order-1", ItemID: "item-1", BuyerID: "buyer-1", OrderStatus: "pending_ship", Amount: "3.00",
	}})
	// order 保存刷新聊天后回填会话的订单。
	order := repository.orders["order-1"]
	if err != nil || discovered != 1 || updated != 0 || len(newIDs) != 1 || len(remoteIDs) != 1 || runtime.chatRefreshCalls != 1 || order == nil || order.ChatID != "chat-after-refresh" {
		t.Fatalf("聊天刷新重试异常: discovered=%d updated=%d new=%v remote=%v refresh=%d order=%+v err=%v", discovered, updated, newIDs, remoteIDs, runtime.chatRefreshCalls, order, err)
	}
}

// TestPersistSoldOrdersKeepsSyncWhenChatRefreshFails 验证聊天刷新失败时订单仍能同步且不会伪造会话关联。
func TestPersistSoldOrdersKeepsSyncWhenChatRefreshFails(t *testing.T) {
	// repository 保存没有本地候选会话的内存状态。
	repository := &refreshRepositoryFake{chatIDs: map[string][]string{}}
	// refreshErr 保存预置的聊天联系人刷新失败原因。
	refreshErr := errors.New("聊天联系人暂时不可用")
	// runtime 保存返回聊天刷新错误的测试运行时。
	runtime := &refreshRuntimeFake{chatRefresh: func(context.Context, string) error { return refreshErr }}
	// service 保存订单发现持久化及可选聊天刷新能力。
	service := &RefreshService{repository: repository, runtime: runtime}
	// prepareErr 保存锁外联系人准备结果，后续持久化只读取缓存。
	if prepareErr := service.prepareSoldChats(context.Background(), "cookie-1", []RefreshSoldOrder{{OrderID: "order-1", ItemID: "item-1", BuyerID: "buyer-1"}}); prepareErr != nil {
		t.Fatal(prepareErr)
	}

	// _, _, _, _, err 保存聊天刷新失败后的订单同步结果。
	_, _, _, _, err := service.persistSoldOrders(context.Background(), "cookie-1", []RefreshSoldOrder{{
		OrderID: "order-1", ItemID: "item-1", BuyerID: "buyer-1", OrderStatus: "pending_ship",
	}})
	// order 保存聊天刷新失败后仍应落库的订单。
	order := repository.orders["order-1"]
	if err != nil || runtime.chatRefreshCalls != 1 || repository.batchUpsertCount != 1 || order == nil || order.ChatID != "" {
		t.Fatalf("聊天刷新失败不应阻断订单同步: refresh=%d batch=%d order=%+v err=%v", runtime.chatRefreshCalls, repository.batchUpsertCount, order, err)
	}
}

// TestPersistSoldOrdersDoesNotGuessAmbiguousChatID 验证多个候选会话时不会擅自选择错误会话。
func TestPersistSoldOrdersDoesNotGuessAmbiguousChatID(t *testing.T) {
	// repository 保存多个候选会话及已有订单的内存状态。
	repository := &refreshRepositoryFake{
		orders:  map[string]*Order{"order-1": {OrderID: "order-1", CookieID: "cookie-1", ChatID: "old-chat"}},
		chatIDs: map[string][]string{"buyer-1\x00item-1": {"chat-1", "chat-2"}},
	}
	// service 保存仅用于调用订单发现持久化的应用服务。
	service := &RefreshService{repository: repository}
	// _, _, _, _, err 保存多会话歧义下的订单同步结果。
	_, _, _, _, err := service.persistSoldOrders(context.Background(), "cookie-1", []RefreshSoldOrder{{
		OrderID: "order-1", ItemID: "item-1", BuyerID: "buyer-1", OrderStatus: "pending_ship", Amount: "3.00",
	}})
	// order 保存同步后仍应保留的原聊天会话。
	order := repository.orders["order-1"]
	if err != nil || order == nil || order.ChatID != "old-chat" {
		t.Fatalf("多会话不应猜测 chat_id: order=%+v err=%v", order, err)
	}
}

// TestPersistSoldOrdersReturnsChatMatchError 验证会话匹配失败会阻止订单批量写入并返回原因。
func TestPersistSoldOrdersReturnsChatMatchError(t *testing.T) {
	// matchErr 保存预置的会话匹配错误。
	matchErr := errors.New("会话查询失败")
	// repository 保存返回会话匹配错误的内存依赖。
	repository := &refreshRepositoryFake{chatMatchErr: matchErr}
	// service 保存订单刷新应用服务。
	service := &RefreshService{repository: repository}
	// _, _, _, _, err 保存会话匹配失败结果。
	_, _, _, _, err := service.persistSoldOrders(context.Background(), "cookie-1", []RefreshSoldOrder{{
		OrderID: "failed-chat-order", ItemID: "item-1", BuyerID: "buyer-1", OrderStatus: "pending_ship",
	}})
	if err == nil || !errors.Is(err, matchErr) || repository.batchUpsertCount != 0 {
		t.Fatalf("会话匹配失败结果异常: batch=%d err=%v", repository.batchUpsertCount, err)
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
