package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// orderRecoveryPlatform 在已有本地平台替身上记录分页，并拒绝使用旧买家的凭证发起请求；仅由同步测试调用。
type orderRecoveryPlatform struct {
	// orderRuntimeFetchFake 返回预置已售分页及详情，所有请求均留在进程内。
	*orderRuntimeFetchFake
	// t 接收分页顺序和请求身份的断言，不输出明文凭证。
	t *testing.T
	// pages 保存各次请求页码，刷新返回后由测试核对完整分页及重跑行为。
	pages []int
}

// FetchSoldOrdersPage 使用 f 的本地页替身响应 ctx、cookies、pageNumber、pageSize；身份不符时终止测试，返回值不访问外网。
func (f *orderRecoveryPlatform) FetchSoldOrdersPage(ctx context.Context, cookies string, pageNumber, pageSize int) (*mtop.SoldOrdersPage, error) {
	f.t.Helper()
	if !strings.Contains(cookies, "unb=1") || pageSize != 30 {
		f.t.Fatal("已售请求必须使用卖家身份及生产分页大小")
	}
	f.pages = append(f.pages, pageNumber)
	return f.orderRuntimeFetchFake.FetchSoldOrdersPage(ctx, cookies, pageNumber, pageSize)
}

// orderRecoveryFixture 持有真实 SQLite、真实适配器组装的服务和本地平台替身，资源由测试清理。
type orderRecoveryFixture struct {
	// store 保存独立 SQLite 及全部已执行迁移，仅测试种子和断言可直接访问。
	store *db.Store
	// userID 是卖家与历史买家账号所属的管理用户。
	userID int64
	// service 经过真实订单仓储与平台运行时，验证应用层恢复编排。
	service *orderapp.RefreshService
	// platform 保存同步期间使用的纯本地平台结果和请求记录。
	platform *orderRecoveryPlatform
}

// newOrderRecoveryFixture 为 t 创建真实 SQLite 及空凭证买家账号；返回通过真实 adapter 装配的刷新环境，关闭由 t.Cleanup 负责。
func newOrderRecoveryFixture(t *testing.T) *orderRecoveryFixture {
	t.Helper()
	// store、cleanup 是已有夹具创建的独立数据库及其连接释放函数。
	store, cleanup := newAdapterTestStore(t)
	t.Cleanup(cleanup)
	// ctx 仅用于本地种子数据，不启动账号引擎或真实浏览器。
	ctx := context.Background()
	// admin、err 确认已有卖家 cid 的管理用户，失败时禁止继续构造不完整夹具。
	admin, err := store.Users.GetByUsername(ctx, "admin")
	if err != nil || admin == nil {
		t.Fatal("读取测试管理员失败")
	}
	// buyerID 是历史错绑账号的虚构平台 UID；空凭证确保修复不依赖旧买家登录。
	for _, buyerID := range []string{"recovery-buyer-1", "recovery-buyer-2"} {
		// saveErr 保存空凭证账号种子的写入失败，不允许退回真实平台凭证。
		if saveErr := store.Cookies.Save(ctx, buyerID, "", admin.ID); saveErr != nil {
			t.Fatal("创建空凭证买家账号失败")
		}
	}
	// platform 的详情结果和分页都在本地提供，覆盖新增订单后续补全详情流程。
	platform := &orderRecoveryPlatform{t: t, orderRuntimeFetchFake: &orderRuntimeFetchFake{
		orderRuntimeMTopFake: &orderRuntimeMTopFake{},
		detail:               &mtop.OrderDetailResult{OrderStatus: "交易成功", Quantity: "1", Amount: "12.50"},
		soldPages:            make(map[int]*mtop.SoldOrdersPage),
	}}
	// runtime 的 Client 回调只返回本地平台替身；持久化仍使用真实数据库。
	runtime := NewOrderRuntime(store, OrderRuntimeHooks{Client: func() mtop.Client { return platform }}, nil, nil)
	return &orderRecoveryFixture{store: store, userID: admin.ID, platform: platform,
		service: orderapp.NewRefreshService(NewOrderRepository(store), runtime, 2)}
}

// seed 为 t 在 f 的数据库创建 cookieID 所属的 remote 历史订单；deleted 决定是否预置软删除，失败立即终止。
func (f *orderRecoveryFixture) seed(t *testing.T, cookieID string, remote mtop.SoldOrder, deleted bool) {
	t.Helper()
	// ctx 控制夹具写入生命周期；未经过刷新服务的写入只用于构造历史状态。
	ctx := context.Background()
	// err 保存旧订单写入失败，不允许刷新用例把缺失种子误判为恢复成功。
	if err := f.store.Orders.Upsert(ctx, remote.OrderID, db.OrderUpsertOpts{CookieID: cookieID,
		ItemID: remote.ItemID, BuyerID: remote.BuyerID, OrderStatus: "completed", Amount: "12.50", Quantity: "1"}); err != nil {
		t.Fatal(err)
	}
	if deleted {
		// changed、err 确认旧行确实进入软删除状态，普通订单查询应无法读取。
		changed, err := f.store.Orders.SoftDelete(ctx, remote.OrderID)
		if err != nil || !changed {
			t.Fatal("历史订单软删除种子失败")
		}
	}
}

// recoverySoldOrder 为 orderID、buyerID 构造无真实个人信息的完整已售事实，返回值在测试间互不共享。
func recoverySoldOrder(orderID, buyerID string) mtop.SoldOrder {
	return mtop.SoldOrder{OrderID: orderID, ItemID: "item-" + orderID, BuyerID: buyerID,
		OrderStatus: "4", Quantity: "1", Amount: "12.50"}
}

// assertRecoveryVisible 为 t 核对 f 中 cookieID 的全部可见订单与 wantIDs 精确一致，排除缺单、重复或错误迁移。
func assertRecoveryVisible(t *testing.T, f *orderRecoveryFixture, cookieID string, wantIDs []string) {
	t.Helper()
	// rows、err 是真实 SQLite 返回的账号可见订单，限制足以容纳全部测试数据。
	rows, err := f.store.Orders.ByCookieCursor(context.Background(), cookieID, 100, "", "")
	if err != nil || len(rows) != len(wantIDs) {
		t.Fatalf("账号可见订单数=%d，预期=%d，查询失败=%t", len(rows), len(wantIDs), err != nil)
	}
	// expected 是仍待匹配的订单集合，删除匹配键也能发现重复行。
	expected := make(map[string]bool, len(wantIDs))
	// orderID 是当前应属于该账号的合成订单标识。
	for _, orderID := range wantIDs {
		expected[orderID] = true
	}
	// row 是当前 SQLite 可见行，只检查身份及平台事实，不输出收货字段。
	for _, row := range rows {
		if !expected[row.OrderID] || row.CookieID != cookieID || row.Amount != "12.50" {
			t.Fatal("可见订单集合、归属或金额不符")
		}
		delete(expected, row.OrderID)
	}
}

// TestOrderRecoveryIntegrationHistorical13 用 t 验证十笔正常单、两笔买家错绑软删除单和一笔断线新单归于卖家，且重跑幂等。
func TestOrderRecoveryIntegrationHistorical13(t *testing.T) {
	// fixture 包含真实 SQLite 及应用服务链路，不启动实际账号运行时。
	fixture := newOrderRecoveryFixture(t)
	// remote、wantIDs 分别保存完整平台快照和卖家应可见的十三个订单标识。
	remote, wantIDs := make([]mtop.SoldOrder, 0, 13), make([]string, 0, 13)
	// index 区分十笔正常历史单、两笔错绑单和最后一笔断线期间新订单。
	for index := 0; index < 13; index++ {
		// buyerID 是远端买家身份，错绑场景要求与旧账号键吻合。
		buyerID := "ordinary-buyer"
		if index == 10 || index == 11 {
			buyerID = fmt.Sprintf("recovery-buyer-%d", index-9)
		}
		// order 是当前平台订单事实，其标识在整个重跑期间保持一致。
		order := recoverySoldOrder(fmt.Sprintf("recovery-%02d", index), buyerID)
		remote, wantIDs = append(remote, order), append(wantIDs, order.OrderID)
		if index < 10 {
			fixture.seed(t, "cid", order, false)
		} else if index < 12 {
			fixture.seed(t, buyerID, order, true)
		}
	}
	fixture.platform.soldPages[1] = &mtop.SoldOrdersPage{Items: remote[:10], NextPage: true}
	fixture.platform.soldPages[2] = &mtop.SoldOrdersPage{Items: remote[10:]}
	// pass 区分首次恢复及重复刷新；每轮都核对真实数据库结果和持久化审计数量。
	for pass := 0; pass < 2; pass++ {
		// result、err 保存完整应用刷新结果，统计只允许首次迁移和首次发现时增加。
		result, err := fixture.service.Refresh(context.Background(), fixture.userID, "cid", "all")
		// wantReassigned、wantDiscovered 是本轮实际应发生的迁移及新增数量。
		wantReassigned, wantDiscovered := 2, 1
		if pass == 1 {
			wantReassigned, wantDiscovered = 0, 0
		}
		if err != nil || result.PartialFailure || result.Summary.Failed != 0 || result.Summary.Reassigned != wantReassigned ||
			result.Summary.Discovered != wantDiscovered || result.Summary.Restored != 0 || result.Summary.SoftDeleted != 0 {
			t.Fatalf("第%d次刷新计数异常: %+v，调用失败=%t", pass+1, result.Summary, err != nil)
		}
		assertRecoveryVisible(t, fixture, "cid", wantIDs)
		assertRecoveryVisible(t, fixture, "recovery-buyer-1", nil)
		assertRecoveryVisible(t, fixture, "recovery-buyer-2", nil)
		// auditCount 统计已提交归属修复记录，重跑不得追加恢复审计。
		var auditCount int
		// auditErr 是真实迁移四十二审计表的查询错误。
		if auditErr := fixture.store.DB.QueryRow(`SELECT COUNT(*) FROM order_ownership_repairs`).Scan(&auditCount); auditErr != nil || auditCount != 2 {
			t.Fatalf("恢复审计数=%d，查询失败=%t", auditCount, auditErr != nil)
		}
	}
	if fmt.Sprint(fixture.platform.pages) != "[1 2 1 2]" {
		t.Fatalf("两次刷新分页请求=%v", fixture.platform.pages)
	}
}

// TestOrderRecoveryIntegrationPreservesKnownStatus 用 t 验证缺少平台状态时普通合并、同账号恢复及错绑修复都保留已完成状态，重跑不重复计数。
func TestOrderRecoveryIntegrationPreservesKnownStatus(t *testing.T) {
	// status 是原始平台状态；空字段及无法识别的字段都会由真实运行时归一为 unknown。
	for _, status := range []string{"", "unknown"} {
		// t 是当前原始状态下的独立 SQLite 集成断言上下文。
		t.Run("status="+status, func(t *testing.T) {
			// fixture 拥有隔离数据库、真实应用和适配器，本地平台替身不会调用真实闲鱼。
			fixture := newOrderRecoveryFixture(t)
			// active、deleted、misassigned 覆盖三种已售持久化分支，种子状态均为 completed。
			active, deleted, misassigned := recoverySoldOrder("status-active", "ordinary-buyer"), recoverySoldOrder("status-deleted", "ordinary-buyer"), recoverySoldOrder("status-misassigned", "recovery-buyer-1")
			active.OrderStatus, deleted.OrderStatus, misassigned.OrderStatus = status, status, status
			fixture.seed(t, "cid", active, false)
			fixture.seed(t, "cid", deleted, true)
			fixture.seed(t, "recovery-buyer-1", misassigned, true)
			fixture.platform.soldPages[1] = &mtop.SoldOrdersPage{Items: []mtop.SoldOrder{active, deleted, misassigned}}
			// pass 区分首次恢复和重跑；已完成筛选不补详情，避免详情结果掩盖状态回退。
			for pass := 0; pass < 2; pass++ {
				// result、err 保存真实刷新统计及调用失败，恢复不应产生新增或失败订单。
				result, err := fixture.service.Refresh(context.Background(), fixture.userID, "cid", "completed")
				// wantRecoveries 是每种恢复操作本轮应提交的数量，重跑必须归零。
				wantRecoveries := 1 - pass
				if err != nil || result.PartialFailure || result.Summary.Failed != 0 || result.Summary.Discovered != 0 || result.Summary.SoftDeleted != 0 || result.Summary.Restored != wantRecoveries || result.Summary.Reassigned != wantRecoveries {
					t.Fatalf("恢复统计异常: %+v，调用失败=%t", result.Summary, err != nil)
				}
				assertRecoveryVisible(t, fixture, "cid", []string{active.OrderID, deleted.OrderID, misassigned.OrderID})
				assertRecoveryVisible(t, fixture, "recovery-buyer-1", nil)
				// orderID 是本轮必须保留已完成状态的历史订单标识。
				for _, orderID := range []string{active.OrderID, deleted.OrderID, misassigned.OrderID} {
					// order、readErr 保存持久化订单及查询失败，不输出收货信息。
					order, readErr := fixture.store.Orders.Get(context.Background(), orderID)
					if readErr != nil || order == nil {
						t.Fatal("恢复后订单不可读取")
					}
					if order.OrderStatus != "completed" {
						t.Errorf("订单 %s 状态回退为 %q", orderID, order.OrderStatus)
					}
				}
			}
		})
	}
}

// TestOrderRecoveryIntegrationUnsafeKeepsMissing 用 t 验证有自动化运行历史的订单隔离失败，新单仍保存且其他本地单不会被缺失清理。
func TestOrderRecoveryIntegrationUnsafeKeepsMissing(t *testing.T) {
	// fixture 包含安全新单与不安全旧单共用的完整应用链路。
	fixture := newOrderRecoveryFixture(t)
	// unsafe、fresh、retained 分别是被自动化痕迹阻止的错绑单、新单和远端列表中缺失但必须保留的本地单。
	unsafe, fresh, retained := recoverySoldOrder("unsafe-old", "recovery-buyer-1"), recoverySoldOrder("safe-new", "ordinary-buyer"), recoverySoldOrder("keep-local", "ordinary-buyer")
	fixture.seed(t, "recovery-buyer-1", unsafe, true)
	fixture.seed(t, "cid", retained, false)
	// rule、err 创建仅供自动化历史引用的停用规则，不启动自动化执行。
	rule, err := fixture.store.DB.Exec(`INSERT INTO automation_rules(user_id,cookie_id,name,trigger_type,enabled) VALUES(?,'recovery-buyer-1','历史规则','order_paid',0)`, fixture.userID)
	if err != nil {
		t.Fatal(err)
	}
	// ruleID、idErr 保存运行历史的合法外键，避免使用真实规则。
	ruleID, idErr := rule.LastInsertId()
	if idErr != nil {
		t.Fatal(idErr)
	}
	// runErr 保存已完成自动化运行的种子错误；即使运行已终结也应阻止迁移。
	if _, runErr := fixture.store.DB.Exec(`INSERT INTO automation_runs(rule_id,cookie_id,order_id,trigger_type,trigger_key,status) VALUES(?,'recovery-buyer-1',?,'order_paid','fixture-run','completed')`, ruleID, unsafe.OrderID); runErr != nil {
		t.Fatal(runErr)
	}
	fixture.platform.soldPages[1] = &mtop.SoldOrdersPage{Items: []mtop.SoldOrder{unsafe}, NextPage: true}
	fixture.platform.soldPages[2] = &mtop.SoldOrdersPage{Items: []mtop.SoldOrder{fresh}}
	// result、refreshErr 保存业务冲突隔离后的统计，不能把成功新单回滚或把冲突伪装为全成功。
	result, refreshErr := fixture.service.Refresh(context.Background(), fixture.userID, "cid", "all")
	if refreshErr != nil || !result.PartialFailure || result.Summary.Failed != 1 || result.Summary.Discovered != 1 ||
		result.Summary.Reassigned != 0 || result.Summary.Restored != 0 || result.Summary.SoftDeleted != 0 {
		t.Fatalf("隔离失败统计异常: %+v，调用失败=%t", result.Summary, refreshErr != nil)
	}
	assertRecoveryVisible(t, fixture, "cid", []string{fresh.OrderID, retained.OrderID})
	// old、ownershipErr 验证危险单仍属于旧买家且保持软删除，失败不能影响其可见性。
	old, ownershipErr := fixture.store.Orders.FindOwnership(context.Background(), fixture.userID, unsafe.OrderID)
	if ownershipErr != nil || old.CookieID != "recovery-buyer-1" || !old.Deleted {
		t.Fatal("不安全历史订单被迁移或恢复")
	}
	// failedRecovery 记录是否返回逐单恢复失败，避免只给出无定位信息的账号级错误。
	failedRecovery := false
	// row 是当前账号的公开刷新结果，不应包含历史运行原文。
	for _, row := range result.Results {
		if row.Stage == "recover" && row.OrderID == unsafe.OrderID && !row.Success && row.Error != "" {
			failedRecovery = true
		}
	}
	if !failedRecovery {
		t.Fatal("缺少危险订单的逐单恢复失败结果")
	}
}

// TestOrderRecoveryIntegrationCrossUserNoLeak 用 t 验证跨用户冲突不可迁移、不可公开旧身份或收货字段，同时其他新单继续保存。
func TestOrderRecoveryIntegrationCrossUserNoLeak(t *testing.T) {
	// fixture 提供发起刷新用户的卖家账号和本地平台替身。
	fixture := newOrderRecoveryFixture(t)
	// ctx 用于建立另一个管理用户与私有订单，不访问真实平台。
	ctx := context.Background()
	// created、err 保存隔离用户创建结果，密码仅是本地测试合成值且不输出。
	created, err := fixture.store.Users.Create(ctx, "foreign-user", "foreign@example.test", "local-fixture")
	if err != nil || !created {
		t.Fatal("创建隔离用户失败")
	}
	// foreign、readErr 是另一个用户的账号归属，不应进入发起者的应用结果。
	foreign, readErr := fixture.store.Users.GetByUsername(ctx, "foreign-user")
	if readErr != nil || foreign == nil {
		t.Fatal("读取隔离用户失败")
	}
	// saveErr 保存旧账号种子错误，空凭证确保其不参与任何平台请求。
	if saveErr := fixture.store.Cookies.Save(ctx, "private-owner-marker", "", foreign.ID); saveErr != nil {
		t.Fatal("创建隔离账号失败")
	}
	// conflicted、fresh 是平台返回的冲突订单及仍可保存的新订单事实。
	conflicted, fresh := recoverySoldOrder("cross-user-order", "remote-buyer"), recoverySoldOrder("cross-user-new", "ordinary-buyer")
	fixture.seed(t, "private-owner-marker", conflicted, true)
	// writeErr 为旧订单注入仅属于另一个用户的字段，用唯一标记检测公开结果泄漏。
	if _, writeErr := fixture.store.DB.Exec(`UPDATE orders SET item_id='private-item-marker',buyer_id='private-buyer-marker',receiver_name='私有收件人标记',receiver_phone='私有电话标记',receiver_address='私有地址标记' WHERE order_id=?`, conflicted.OrderID); writeErr != nil {
		t.Fatal(writeErr)
	}
	fixture.platform.soldPages[1] = &mtop.SoldOrdersPage{Items: []mtop.SoldOrder{conflicted, fresh}}
	// ownership、lookupErr 核对真实 adapter 的非敏感归属接口在跨用户时返回零值。
	ownership, lookupErr := NewOrderRepository(fixture.store).FindOrderOwnership(ctx, fixture.userID, conflicted.OrderID)
	if lookupErr != nil || ownership != (orderapp.RefreshOwnership{}) {
		t.Fatal("跨用户归属接口泄漏旧身份")
	}
	// result、refreshErr 保存隔离冲突后的应用结果，必须保持明确的部分失败。
	result, refreshErr := fixture.service.Refresh(ctx, fixture.userID, "cid", "all")
	if refreshErr != nil || !result.PartialFailure || result.Summary.Failed != 1 || result.Summary.Discovered != 1 || result.Summary.Reassigned != 0 {
		t.Fatal("跨用户订单冲突未隔离或影响了正常新单")
	}
	// payload、encodeErr 检查整个可公开应用结果，包括账号级和逐单错误文本。
	payload, encodeErr := json.Marshal(result)
	if encodeErr != nil {
		t.Fatal("刷新结果无法序列化")
	}
	// marker 是只存在于另一个用户旧订单中的合成私有字段，不应出现在结果任何位置。
	for _, marker := range []string{"private-owner-marker", "private-item-marker", "private-buyer-marker", "私有收件人标记", "私有电话标记", "私有地址标记"} {
		if strings.Contains(string(payload), marker) {
			t.Fatal("跨用户刷新结果泄漏私有字段")
		}
	}
	assertRecoveryVisible(t, fixture, "cid", []string{fresh.OrderID})
	// old、oldErr 以真正所有者身份验证原订单仍处于旧账号且保持软删除。
	old, oldErr := fixture.store.Orders.FindOwnership(ctx, foreign.ID, conflicted.OrderID)
	if oldErr != nil || old.CookieID != "private-owner-marker" || old.ItemID != "private-item-marker" || !old.Deleted {
		t.Fatal("跨用户冲突修改了私有订单")
	}
}

// TestOrderRecoveryIntegrationRestoreAfterWriteFailure 用 t 验证同账号软删除恢复的真实写入失败不计成功，故障解除后仅计 Restored 一次。
func TestOrderRecoveryIntegrationRestoreAfterWriteFailure(t *testing.T) {
	// fixture 保存本账号恢复及失败后重试共用的真实数据库与服务。
	fixture := newOrderRecoveryFixture(t)
	// remote、retained 分别为待恢复单和故障时必须保留的远端缺失本地单。
	remote, retained := recoverySoldOrder("restore-local", "ordinary-buyer"), recoverySoldOrder("restore-retained", "ordinary-buyer")
	fixture.seed(t, "cid", remote, true)
	fixture.seed(t, "cid", retained, false)
	fixture.platform.soldPages[1] = &mtop.SoldOrdersPage{Items: []mtop.SoldOrder{remote}}
	// triggerErr 通过本地 SQLite 触发器注入恢复写入失败，避免替换应用仓储或只测 fake 分支。
	if _, triggerErr := fixture.store.DB.Exec(`CREATE TRIGGER fail_local_restore BEFORE UPDATE ON orders WHEN OLD.order_id='restore-local' BEGIN SELECT RAISE(ABORT,'fixture restore write failed'); END`); triggerErr != nil {
		t.Fatal(triggerErr)
	}
	// failed、failedErr 是写入故障下的刷新结果；错误只能计失败且不得触发缺失清理。
	failed, failedErr := fixture.service.Refresh(context.Background(), fixture.userID, "cid", "all")
	if failedErr != nil || !failed.PartialFailure || failed.Summary.Failed != 1 || failed.Summary.Restored != 0 ||
		failed.Summary.Discovered != 0 || failed.Summary.Reassigned != 0 || failed.Summary.SoftDeleted != 0 {
		t.Fatalf("恢复失败计数异常: %+v", failed.Summary)
	}
	assertRecoveryVisible(t, fixture, "cid", []string{retained.OrderID})
	// dropErr 仅解除本测试创建的故障注入，随后必须经过同一服务完成恢复。
	if _, dropErr := fixture.store.DB.Exec(`DROP TRIGGER fail_local_restore`); dropErr != nil {
		t.Fatal(dropErr)
	}
	fixture.platform.soldPages[1].Items = append(fixture.platform.soldPages[1].Items, retained)
	// pass 区分故障解除后的首次恢复与同一完整列表的幂等重跑。
	for pass := 0; pass < 2; pass++ {
		// result、err 是本轮恢复结果，只有第一次允许 Restored=1。
		result, err := fixture.service.Refresh(context.Background(), fixture.userID, "cid", "all")
		if err != nil || result.PartialFailure || result.Summary.Restored != 1-pass || result.Summary.Discovered != 0 ||
			result.Summary.Reassigned != 0 || result.Summary.Failed != 0 || result.Summary.SoftDeleted != 0 {
			t.Fatalf("同账号恢复计数异常: %+v，调用失败=%t", result.Summary, err != nil)
		}
		assertRecoveryVisible(t, fixture, "cid", []string{remote.OrderID, retained.OrderID})
	}
}
