package orders

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// FindOrderOwnership 为既有刷新夹具提供非敏感归属读取，ctx/userID 不访问外部状态；缺失 orderID 返回稳定错误。
func (f *refreshRepositoryFake) FindOrderOwnership(ctx context.Context, userID int64, orderID string) (RefreshOwnership, error) {
	// order 保存本地测试订单；未设账号的旧夹具按该测试默认账号处理。
	order := f.orders[orderID]
	if order == nil {
		return RefreshOwnership{}, ErrNotFound
	}
	// cookieID 保存夹具记录的账号，空值兼容旧测试的 cookie-1。
	cookieID := order.CookieID
	if cookieID == "" {
		cookieID = "cookie-1"
	}
	return RefreshOwnership{OrderID: orderID, CookieID: cookieID, ItemID: order.ItemID, BuyerID: order.BuyerID, Version: order.Version, Owned: true}, nil
}

// RecoverSoldOwnership 为普通夹具拒绝迁移；专门恢复夹具才可实现成功路径，参数只用于满足生产窄接口。
func (f *refreshRepositoryFake) RecoverSoldOwnership(ctx context.Context, userID int64, cookieID string, expected RefreshOwnership, options UpsertOptions) error {
	return ErrSoldRecoveryUnsafe
}

// soldRecoveryRepository 用于验证恢复应用编排，嵌入普通订单夹具并记录归属和恢复调用。
type soldRecoveryRepository struct {
	// refreshRepositoryFake 保存正常批次及账号凭证测试行为。
	refreshRepositoryFake
	// ownership 保存含软删除记录的非敏感快照。
	ownership map[string]RefreshOwnership
	// recoveryErr 为恢复注入原子拒绝或基础设施错误。
	recoveryErr error
	// repairs 记录已完成的跨账号修正次数。
	repairs int
	// ownershipErr 模拟归属查询基础设施失败，不能当作订单不存在。
	ownershipErr error
}

// FindOrderOwnership 从夹具读取 orderID，不存在返回 ErrNotFound；ctx、userID 不执行外部鉴权。
func (f *soldRecoveryRepository) FindOrderOwnership(ctx context.Context, userID int64, orderID string) (RefreshOwnership, error) {
	if f.ownershipErr != nil {
		return RefreshOwnership{}, f.ownershipErr
	}
	// row、exists 保存预置快照和存在标记。
	row, exists := f.ownership[orderID]
	if !exists {
		return RefreshOwnership{}, ErrNotFound
	}
	return row, nil
}

// TestSoldRecoverySameAccountAndFailurePaths 验证同账号恢复和基础设施失败都具有明确统计，不执行虚假的缺失清理。
func TestSoldRecoverySameAccountAndFailurePaths(t *testing.T) {
	// failure 是不同存储阶段共同使用的确定性错误。
	failure := errors.New("模拟数据库不可用")
	// cases 覆盖恢复、读取错误、写入错误、未知动作和普通批次失败。
	cases := []struct {
		// name 是分支名称。
		name string
		// oldCookie 是历史归属，seller 表示同账号软删除。
		oldCookie string
		// lookupErr、repairErr、upsertErr、batchErr 分别控制四个可失败的持久化阶段。
		lookupErr, repairErr, upsertErr, batchErr error
		// wantErr 表示预期透传的错误，nil 表示成功。
		wantErr error
		// restored 是预期同账号恢复数量。
		restored int
	}{
		{name: "同账号软删除恢复", oldCookie: "seller", restored: 1},
		{name: "读取失败", oldCookie: "buyer", lookupErr: failure, wantErr: failure},
		{name: "恢复事务失败", oldCookie: "buyer", repairErr: failure, wantErr: failure},
		{name: "同账号恢复写入失败", oldCookie: "seller", upsertErr: failure, wantErr: failure},
		{name: "已有外部动作", oldCookie: "buyer", repairErr: ErrSoldRecoveryUnsafe, wantErr: ErrSoldRecoveryUnsafe},
		{name: "恢复后正常批次失败", oldCookie: "seller", batchErr: failure, wantErr: failure, restored: 1},
	}
	// testCase 是当前隔离数据库错误或恢复语义场景。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// repository 保存单一历史订单和当前场景的失败注入。
			repository := &soldRecoveryRepository{refreshRepositoryFake: refreshRepositoryFake{orders: map[string]*Order{}, upsertErr: testCase.upsertErr, batchUpsertErr: testCase.batchErr}, ownershipErr: testCase.lookupErr, recoveryErr: testCase.repairErr,
				ownership: map[string]RefreshOwnership{"old": {OrderID: "old", CookieID: testCase.oldCookie, BuyerID: "buyer", ItemID: "item", Deleted: true, Owned: true}}}
			// service 保存无网络调用的恢复编排。
			service := &RefreshService{repository: repository}
			// result、err 保存最终统计，先前已提交恢复不能在后续失败时消失。
			_, _, result, _, err := service.persistSoldSnapshot(context.Background(), 7, "seller", "seller-uid", []RefreshSoldOrder{{OrderID: "old", ItemID: "item", BuyerID: "buyer"}, {OrderID: "new"}})
			if !errors.Is(err, testCase.wantErr) || result.Restored != testCase.restored {
				t.Fatalf("恢复/失败结果不符: restored=%d err=%v", result.Restored, err)
			}
		})
	}
	// emptyService 用于验证非法平台结果不会走普通持久化或清理路径。
	emptyService := &RefreshService{repository: &refreshRepositoryFake{}}
	// err 是缺失订单号导致的快照拒绝，不能为空。
	if _, _, _, _, err := emptyService.persistSoldSnapshot(context.Background(), 7, "seller", "seller-uid", []RefreshSoldOrder{{OrderID: " "}}); err == nil {
		t.Fatal("缺少订单号不能视作完整快照")
	}
}

// soldConcurrentRepository 模拟归属预检后被占用的订单；首批必须整体回滚，其余订单重试仍可提交。
type soldConcurrentRepository struct {
	// refreshRepositoryFake 提供普通持久化行为。
	refreshRepositoryFake
	// first 表示第一次批次会出现归属竞争。
	first bool
	// persistent 表示指定 conflict 订单在单笔重试时仍被其他账号占用。
	persistent bool
	// storageErr 模拟单笔重试过程中数据库不可用。
	storageErr error
}

// BatchUpsertOrders 在首次或 conflict 行返回权限竞争，其他行委托真实夹具；ctx 和 rows 沿用生产调用语义。
func (f *soldConcurrentRepository) BatchUpsertOrders(ctx context.Context, rows []RefreshOrderWrite) error {
	if f.first {
		f.first = false
		return ErrForbidden
	}
	if f.storageErr != nil {
		return f.storageErr
	}
	if f.persistent && len(rows) == 1 && rows[0].OrderID == "conflict" {
		return ErrForbidden
	}
	return f.refreshRepositoryFake.BatchUpsertOrders(ctx, rows)
}

// TestSoldSnapshotIsolatesConcurrentOwnershipConflict 验证预检后的竞争也不会让正常订单永久被整批回滚阻塞。
func TestSoldSnapshotIsolatesConcurrentOwnershipConflict(t *testing.T) {
	// persistent 区分单笔重试能恢复和仍存在归属竞争的情况。
	for _, persistent := range []bool{false, true} {
		// repository 模拟首批失败后有界重试的数据库。
		repository := &soldConcurrentRepository{first: true, persistent: persistent}
		// service 保存被测同步编排。
		service := &RefreshService{repository: repository}
		// added、result、err 保存竞争隔离后的提交统计与明细。
		added, _, result, _, err := service.persistSoldSnapshot(context.Background(), 7, "seller", "seller-uid", []RefreshSoldOrder{{OrderID: "new"}, {OrderID: "conflict"}, {OrderID: "new"}})
		if persistent {
			if !errors.Is(err, ErrSoldRecoveryUnsafe) || added != 1 || len(result.Results) != 1 || len(result.NewOrderIDs) != 1 {
				t.Fatalf("持续竞争没有被隔离: added=%d result=%+v err=%v", added, result, err)
			}
		} else if err != nil || added != 2 || len(result.NewOrderIDs) != 2 {
			t.Fatalf("短暂竞争恢复失败: added=%d result=%+v err=%v", added, result, err)
		}
	}
	// failure 保存重试过程中应透传的数据库错误。
	failure := errors.New("单笔重试数据库失败")
	// service 模拟首批竞争后数据库不可用，不能无限重试。
	service := &RefreshService{repository: &soldConcurrentRepository{first: true, storageErr: failure}}
	// err 是有界单笔重试的数据库失败，必须保留错误链。
	if _, _, _, _, err := service.persistSoldSnapshot(context.Background(), 7, "seller", "seller-uid", []RefreshSoldOrder{{OrderID: "new"}}); !errors.Is(err, failure) {
		t.Fatalf("重试基础设施错误未传播: %v", err)
	}
}

// RecoverSoldOwnership 模拟同事务修正 expected 对应的记录并写入 options，错误时保持旧数据。
func (f *soldRecoveryRepository) RecoverSoldOwnership(ctx context.Context, userID int64, cookieID string, expected RefreshOwnership, options UpsertOptions) error {
	if f.recoveryErr != nil {
		return f.recoveryErr
	}
	f.repairs++
	expected.CookieID, expected.Deleted = cookieID, false
	f.ownership[expected.OrderID] = expected
	return f.UpsertOrder(ctx, expected.OrderID, options)
}

// TestSoldRecoveryHistoricalDeletedOrderAndIdempotency 验证旧版本错绑软删除单随正常新单恢复且重复同步不再迁移。
func TestSoldRecoveryHistoricalDeletedOrderAndIdempotency(t *testing.T) {
	// repository 保存一个已软删除的买家错绑订单，不提供买家登录或凭证。
	repository := &soldRecoveryRepository{refreshRepositoryFake: refreshRepositoryFake{orders: map[string]*Order{}}, ownership: map[string]RefreshOwnership{
		"historical": {OrderID: "historical", CookieID: "buyer", BuyerID: "buyer", ItemID: "item", Owned: true, Deleted: true, Version: 2},
	}}
	// service 仅使用持久化依赖，seller 身份由调用方传入已验证快照。
	service := &RefreshService{repository: repository}
	// remote 保存平台明确返回的历史订单和断线期间产生的新订单。
	remote := []RefreshSoldOrder{{OrderID: "historical", BuyerID: "buyer", ItemID: "item", OrderStatus: "completed"}, {OrderID: "new", BuyerID: "another", ItemID: "item", OrderStatus: "processing"}}
	// discovered、updated、result、ids、err 保存首次提交结果。
	discovered, updated, result, ids, err := service.persistSoldSnapshot(context.Background(), 7, "seller-account", "seller-uid", remote)
	if err != nil || discovered != 1 || updated != 0 || result.Reassigned != 1 || result.Restored != 0 || len(ids) != 2 || repository.repairs != 1 || repository.orders["historical"].CookieID != "seller-account" {
		t.Fatalf("历史恢复结果异常: 新增=%d 更新=%d 修正=%d err=%v", discovered, updated, result.Reassigned, err)
	}
	// repeated 保存再次同步历史单的恢复结果，证明操作幂等。
	_, _, repeated, _, err := service.persistSoldSnapshot(context.Background(), 7, "seller-account", "seller-uid", remote[:1])
	if err != nil || repeated.Reassigned != 0 || repository.repairs != 1 {
		t.Fatalf("重复同步不应重复修正: result=%+v err=%v", repeated, err)
	}
}

// TestSoldRecoveryRejectsUnprovenOwnershipButImportsOtherOrders 验证证据不足、跨用户和不确定动作不会阻塞其他订单。
func TestSoldRecoveryRejectsUnprovenOwnershipButImportsOtherOrders(t *testing.T) {
	// cases 列举不能跨账号恢复的独立边界，expected 仅包含非敏感测试数据。
	cases := []struct {
		// name 用于定位失败条件。
		name string
		// seller 保存待验证的平台会话身份，空值代表旧客户端未提供身份。
		seller string
		// old 保存旧归属快照。
		old RefreshOwnership
	}{
		{name: "未知卖家", old: RefreshOwnership{OrderID: "conflict", CookieID: "buyer", BuyerID: "buyer", ItemID: "item", Owned: true}},
		{name: "买家会话", seller: "buyer", old: RefreshOwnership{OrderID: "conflict", CookieID: "buyer", BuyerID: "buyer", ItemID: "item", Owned: true}},
		{name: "跨用户", seller: "seller", old: RefreshOwnership{OrderID: "conflict", Owned: false}},
		{name: "商品不符", seller: "seller", old: RefreshOwnership{OrderID: "conflict", CookieID: "buyer", BuyerID: "buyer", ItemID: "other", Owned: true}},
		{name: "旧账号别名无法核验", seller: "seller", old: RefreshOwnership{OrderID: "conflict", CookieID: "alias", BuyerID: "buyer", ItemID: "item", Owned: true}},
	}
	// testCase 是当前需要验证的拒绝条件。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// repository 保存冲突订单，普通订单批次仍可正常写入。
			repository := &soldRecoveryRepository{refreshRepositoryFake: refreshRepositoryFake{orders: map[string]*Order{}}, ownership: map[string]RefreshOwnership{"conflict": testCase.old}}
			// service 保存被测同步编排。
			service := &RefreshService{repository: repository}
			// discovered、result、err 保存部分成功结果，统计不能把冲突单当新增。
			discovered, _, result, _, err := service.persistSoldSnapshot(context.Background(), 7, "seller-account", testCase.seller, []RefreshSoldOrder{{OrderID: "conflict", ItemID: "item", BuyerID: "buyer"}, {OrderID: "new"}})
			if !errors.Is(err, ErrSoldRecoveryUnsafe) || discovered != 1 || repository.repairs != 0 || len(result.Results) != 1 || result.Results[0].Success || repository.orders["new"] == nil {
				t.Fatalf("业务冲突隔离失败: discovered=%d result=%+v err=%v", discovered, result, err)
			}
		})
	}
}

// TestFinishOrderRefreshNeverLabelsFailureAsSuccess 验证零详情失败仍显式报告未完成，并展示历史恢复统计。
func TestFinishOrderRefreshNeverLabelsFailureAsSuccess(t *testing.T) {
	// result 保存失败与恢复同时存在的任务摘要。
	result := finishOrderRefresh(RefreshSummary{Failed: 1, Restored: 2, Reassigned: 3}, nil, "订单列表同步完成")
	if !result.PartialFailure || strings.Contains(result.Message, "订单列表同步完成") || !strings.Contains(result.Message, "恢复 2") || !strings.Contains(result.Message, "修正 3") {
		t.Fatalf("失败提示与统计不一致: %+v", result)
	}
}

// TestSoldRecoveryOptionsKeepsPlatformFields 验证平台砍价、地址与时间字段进入恢复选项，旧会话及发货标记不会被伪造。
func TestSoldRecoveryOptionsKeepsPlatformFields(t *testing.T) {
	// options 是平台完整订单转换结果，收货数据仅为本地虚构夹具。
	options := soldRecoveryOptions("seller", RefreshSoldOrder{ItemID: "item", BuyerID: "buyer", CreatedAt: "2026-01-01 00:00:00", OrderStatus: "completed", Quantity: "2", Amount: "8.00", ReceiverName: "测试买家", ReceiverPhone: "000", ReceiverAddr: "测试地址", ReceiverCity: "测试城市", IsBargain: true})
	if options.IsBargain == nil || !*options.IsBargain || options.CookieID != "seller" || options.ItemID != "item" || options.BuyerID != "buyer" || options.CreatedAt == "" || options.Quantity != "2" || options.Amount != "8.00" || options.ReceiverName != "测试买家" || options.ReceiverPhone != "000" || options.ReceiverAddress != "测试地址" || options.ReceiverCity != "测试城市" || options.ChatID != "" || options.SystemShipped != nil {
		t.Fatal("平台恢复字段或禁止继承的账户上下文发生变化")
	}
}

// TestSoldRecoveryOptionsUnknownStatus 用 t 验证未知状态按缺省字段合并，可信状态及砍价标记保持原值。
func TestSoldRecoveryOptionsUnknownStatus(t *testing.T) {
	// status、want 分别是平台归一后的输入状态及持久化边界应收到的状态，空串表示保留旧值。
	for status, want := range map[string]string{"": "", "unknown": "", "processing": "processing", "shipped": "shipped", "completed": "completed", "cancelled": "cancelled"} {
		// t 是当前状态转换的独立断言上下文。
		t.Run("status="+status, func(t *testing.T) {
			// options 保存本次平台状态转换结果，不继承本地状态或伪造砍价标记。
			options := soldRecoveryOptions("seller", RefreshSoldOrder{OrderStatus: status})
			if options.OrderStatus != want || options.IsBargain != nil {
				t.Fatalf("状态转换=%q，预期=%q，砍价标记应保持未提供", options.OrderStatus, want)
			}
		})
	}
}
