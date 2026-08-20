package automation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/reconciliation"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
)

// fakeMTop 是 mtop.Client 接口的纯内存实现，用于无需 HTTP 服务的单测。
type fakeMTop struct {
	consignErr      error
	consignOk       bool
	consignRet      []string
	consignUpdated  string
	consignCalls    int
	consignCookieIn string
	consignOrderIn  string
	consignCookies  []string
	consignResults  []fakeConsignResult
	// consignStarted 通知测试外部 Consign 调用已经开始。
	consignStarted chan struct{}
	// consignRelease 控制测试外部 Consign 调用何时返回。
	consignRelease chan struct{}
	// adjustErr 是订单改价调用的预置传输错误。
	adjustErr error
	// adjustOk 是订单改价调用的预置业务成功标志。
	adjustOk bool
	// adjustRet 是订单改价调用的预置业务返回。
	adjustRet []string
	// adjustCalls 统计订单改价被调用的次数。
	adjustCalls int
	// adjustOrderIn 记录最后一次订单改价的订单号入参。
	adjustOrderIn string
	// adjustCentsIn 记录最后一次订单改价的整数分价格入参。
	adjustCentsIn int64
}

// fakeConsignResult 用于本次流程后续判断的fakeConsign结果
type fakeConsignResult struct {
	ok      bool
	ret     []string
	updated string
	err     error
}

// FetchUserProfile 封装Fetch用户Profile业务协调。
func (f *fakeMTop) FetchUserProfile(context.Context, string) (*mtop.UserProfileResult, error) {
	return nil, nil
}

// AdjustOrderPriceContext 返回测试预置的订单改价结果并记录入参。
func (f *fakeMTop) AdjustOrderPriceContext(_ context.Context, _ string, orderID string, priceCents int64) (bool, []string, string, error) {
	f.adjustCalls++
	f.adjustOrderIn = orderID
	f.adjustCentsIn = priceCents
	return f.adjustOk, f.adjustRet, "", f.adjustErr
}

// ConsignContext 封装Consign上下文业务协调。
func (f *fakeMTop) ConsignContext(_ context.Context, cookiesStr, orderID string) (bool, []string, string, error) {
	if f.consignStarted != nil {
		close(f.consignStarted)
	}
	if f.consignRelease != nil {
		<-f.consignRelease
	}
	f.consignCalls++
	f.consignCookieIn = cookiesStr
	f.consignOrderIn = orderID
	f.consignCookies = append(f.consignCookies, cookiesStr)
	if len(f.consignResults) > 0 {
		// result 用于本次流程后续判断的结果
		result := f.consignResults[0]
		f.consignResults = f.consignResults[1:]
		return result.ok, result.ret, result.updated, result.err
	}
	return f.consignOk, f.consignRet, f.consignUpdated, f.consignErr
}

// TestConfirmShipmentReleasesCredentialLockBeforeExternalIO 验证 MTOP 外部调用期间同账号凭证锁可以被其他流程获取。
func TestConfirmShipmentReleasesCredentialLockBeforeExternalIO(t *testing.T) {
	// store、cleanup 保存测试数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 保存本测试共用的上下文。
	ctx := context.Background()
	// fake 保存带有可控阻塞点的 MTOP 客户端。
	fake := &fakeMTop{
		consignOk:      false,
		consignRet:     []string{"blocked"},
		consignStarted: make(chan struct{}),
		consignRelease: make(chan struct{}),
	}
	// center 保存注入测试 MTOP 客户端的自动化中心。
	center := NewWithDependencies(store, nil, nil, CenterDependencies{MTop: fake})
	// result 保存确认发货流程的最终错误。
	result := make(chan error, 1)
	go func() {
		// runErr 保存阻塞 MTOP 确认发货流程的错误。
		result <- center.confirmShipment(ctx, Task{AccountID: "cid", OrderID: "lock-free-mtop", ForceConfirmShipment: true})
	}()
	select {
	case <-fake.consignStarted:
	case <-time.After(time.Second):
		t.Fatal("MTOP 调用未开始")
	}
	// acquired 表示另一个流程已经成功获取并释放同账号凭证锁。
	acquired := make(chan struct{})
	go func() {
		// unlock 释放探测流程取得的账号凭证锁。
		unlock := store.LockAccountCredentials("cid")
		unlock()
		close(acquired)
	}()
	select {
	case <-acquired:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("MTOP 外部调用仍持有账号凭证锁")
	}
	close(fake.consignRelease)
	// runErr 保存确认发货业务失败流程的返回错误。
	if runErr := <-result; runErr == nil {
		t.Fatal("确认发货业务失败应返回错误")
	}
}

// TestConfirmShipmentDoesNotOverwriteConcurrentCredentialUpdate 验证旧 MTOP 响应不会覆盖并发完成的新凭证。
func TestConfirmShipmentDoesNotOverwriteConcurrentCredentialUpdate(t *testing.T) {
	// store、cleanup 保存测试数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 保存本测试共用的上下文。
	ctx := context.Background()
	// initial 保存外部请求开始前的旧 Cookie。
	initial := "sid=old"
	// err 保存写入外部请求初始凭证的错误。
	if err := store.Cookies.UpdateRenewalCookie(ctx, "cid", initial, `{"origin":"old"}`, 1); err != nil {
		t.Fatal(err)
	}
	// fake 保存返回旧请求结果但可控阻塞的 MTOP 客户端。
	fake := &fakeMTop{
		consignOk:      false,
		consignRet:     []string{"business-failure"},
		consignUpdated: "sid=stale-response",
		consignStarted: make(chan struct{}),
		consignRelease: make(chan struct{}),
	}
	// center 保存注入测试 MTOP 客户端的自动化中心。
	center := NewWithDependencies(store, nil, nil, CenterDependencies{MTop: fake})
	// result 保存确认发货流程的最终错误。
	result := make(chan error, 1)
	go func() {
		result <- center.confirmShipment(ctx, Task{AccountID: "cid", OrderID: "credential-conflict", ForceConfirmShipment: true})
	}()
	select {
	case <-fake.consignStarted:
	case <-time.After(time.Second):
		t.Fatal("MTOP 调用未开始")
	}
	// latest 保存并发凭证更新后的新 Cookie，旧响应不允许覆盖它。
	latest := "sid=new-runtime"
	// err 保存写入并发新凭证的错误。
	if err := store.Cookies.UpdateRenewalCookie(ctx, "cid", latest, `{"origin":"new"}`, 2); err != nil {
		t.Fatal(err)
	}
	close(fake.consignRelease)
	// runErr 保存并发凭证冲突场景的确认发货错误。
	if runErr := <-result; runErr == nil || !strings.Contains(runErr.Error(), "business-failure") {
		t.Fatalf("确认发货应保留业务失败: %v", runErr)
	}
	// detail 保存最终凭证运行视图。
	detail, err := store.Cookies.GetDetails(ctx, "cid")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Value != latest {
		t.Fatalf("并发新凭证被旧响应覆盖: got=%q want=%q", detail.Value, latest)
	}
}

// FetchItemsPage 封装Fetch商品列表页码业务协调。
func (f *fakeMTop) FetchItemsPage(context.Context, string, int, int) (*mtop.ItemListResult, error) {
	return nil, nil
}

// FetchAllItems 封装FetchAll商品列表业务协调。
func (f *fakeMTop) FetchAllItems(context.Context, string, int, int) (*mtop.ItemListResult, error) {
	return nil, nil
}

// PublishItem 封装发布商品业务协调。
func (f *fakeMTop) PublishItem(context.Context, string, mtop.PublishItemRequest) (*mtop.PublishItemResult, error) {
	return nil, nil
}

// RefreshTokenWithDeviceIDContext 刷新令牌WithDeviceID上下文。
func (f *fakeMTop) RefreshTokenWithDeviceIDContext(context.Context, string, string) (*mtop.RefreshResult, error) {
	return nil, nil
}

// fakeCredentialRecoverer 用于本次流程后续判断的fakeCredentialRecoverer
type fakeCredentialRecoverer struct {
	store *db.Store
	calls int
	fail  bool
}

// FetchOrderDetail 封装Fetch订单Detail业务协调。
func (f *fakeCredentialRecoverer) FetchOrderDetail(context.Context, string, string, string, string, string) (*OrderDetail, error) {
	return &OrderDetail{Quantity: "1", Amount: "9.9"}, nil
}

// RecoverExpiredCredential 封装RecoverExpiredCredential业务协调。
func (f *fakeCredentialRecoverer) RecoverExpiredCredential(ctx context.Context, cookieID string) bool {
	f.calls++
	if f.fail {
		return false
	}
	// detail、err 用于本次流程后续判断的detail、err
	detail, err := f.store.Cookies.GetDetails(ctx, cookieID)
	if err != nil {
		return false
	}
	return f.store.Cookies.UpdateRenewalCookie(ctx, cookieID, "unb=123; _m_h5_tk=fresh_1;", detail.MetadataJSON, time.Now().Unix()) == nil
}

// TestConfirmShipmentRetriesFromCheckpointWithoutResendingCard 封装TestConfirmShipmentRetriesFromCheckpointWithoutResending卡密业务协调。
func TestConfirmShipmentRetriesFromCheckpointWithoutResendingCard(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// res、err 用于本次流程后续判断的res、err
	res, err := store.DB.ExecContext(ctx, `INSERT INTO cards (name,type,text_content,enabled,user_id) VALUES ('gift','text','ONLY-ONCE',1,?)`, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	// cardID 用于本次流程后续判断的卡密ID
	cardID, _ := res.LastInsertId()
	if // err 用于本次流程后续判断的err
	_, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", ItemID: "checkpoint-item", Name: "checkpoint",
		TriggerType: TriggerOrderPaid, Enabled: true,
		Actions: []db.AutomationActionInput{
			{ActionType: ActionSendCard, CardID: cardID, DeliveryCount: 1, Enabled: true, SortOrder: 1},
			{ActionType: ActionConfirmShipment, Enabled: true, SortOrder: 2},
		},
	}); err != nil {
		t.Fatal(err)
	}
	// mtopMock 用于本次流程后续判断的mtopMock
	mtopMock := &fakeMTop{consignResults: []fakeConsignResult{
		{ret: []string{"FAIL_SYS_SESSION_EXPIRED::Session过期"}},
		{ok: true, ret: []string{"SUCCESS::调用成功"}},
	}}
	// recoverer 用于本次流程后续判断的recoverer
	recoverer := &fakeCredentialRecoverer{store: store, fail: true}
	// sender 用于本次流程后续判断的sender
	sender := &testSender{}
	// center 用于本次流程后续判断的center
	center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{
		MTop:               mtopMock,
		OrderDetailFetcher: recoverer,
	})
	// task 用于本次流程后续判断的任务
	task := Task{AccountID: "cid", TriggerType: TriggerOrderPaid, OrderID: "checkpoint-order",
		ItemID: "checkpoint-item", BuyerID: "buyer", ChatID: "chat", Quantity: "1", Amount: "9.9"}
	if // err 用于本次流程后续判断的err
	err := center.HandleTask(ctx, task); err == nil {
		t.Fatal("首次确认发货应因 Session 恢复失败而返回错误")
	}
	// status、errMsg 用于本次流程后续判断的status、errMsg
	var status, errMsg string
	// sent、cursor 用于本次流程后续判断的sent、cursor
	var sent, cursor int
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT status,error_message,sent_count,action_cursor FROM automation_runs WHERE order_id=?`, task.OrderID).
		Scan(&status, &errMsg, &sent, &cursor); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || !strings.HasPrefix(errMsg, db.SafeRetryErrorPrefix) || sent != 1 || cursor != 1 {
		t.Fatalf("status=%q err=%q sent=%d cursor=%d", status, errMsg, sent, cursor)
	}
	recoverer.fail = false
	if // err 用于本次流程后续判断的err
	_, err := store.DB.ExecContext(ctx, `UPDATE automation_runs SET next_retry_at=0 WHERE order_id=?`, task.OrderID); err != nil {
		t.Fatal(err)
	}
	NewScheduler(center).runRecoveryTasks(ctx)
	if len(sender.texts) != 1 || sender.texts[0] != "ONLY-ONCE" {
		t.Fatalf("恢复确认发货不得重复发送卡密: %v", sender.texts)
	}
	if mtopMock.consignCalls != 2 {
		t.Fatalf("consign calls=%d want 2", mtopMock.consignCalls)
	}
}

// TestConfirmShipmentRecoversExpiredSessionAndRetriesOnlyConsign 封装TestConfirmShipmentRecoversExpired会话AndRetriesOnlyConsign业务协调。
func TestConfirmShipmentRecoversExpiredSessionAndRetriesOnlyConsign(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// mtopMock 用于本次流程后续判断的mtopMock
	mtopMock := &fakeMTop{consignResults: []fakeConsignResult{
		{ret: []string{"FAIL_SYS_SESSION_EXPIRED::Session过期"}},
		{ok: true, ret: []string{"SUCCESS::调用成功"}},
	}}
	// recoverer 用于本次流程后续判断的recoverer
	recoverer := &fakeCredentialRecoverer{store: store}
	// center 用于本次流程后续判断的center
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{
		MTop:               mtopMock,
		OrderDetailFetcher: recoverer,
	})
	if // err 用于本次流程后续判断的err
	err := center.confirmShipment(ctx, Task{AccountID: "cid", OrderID: "session-order", ItemID: "item", BuyerID: "buyer", ChatID: "chat"}); err != nil {
		t.Fatal(err)
	}
	if recoverer.calls != 1 || mtopMock.consignCalls != 2 {
		t.Fatalf("recover calls=%d consign calls=%d want 1/2", recoverer.calls, mtopMock.consignCalls)
	}
	if len(mtopMock.consignCookies) != 2 || !strings.Contains(mtopMock.consignCookies[1], "fresh_1") {
		t.Fatalf("确认发货重试未使用续期 Cookie: %v", mtopMock.consignCookies)
	}
	// order、err 用于本次流程后续判断的order、err
	order, err := store.Orders.Get(ctx, "session-order")
	if err != nil || !order.SystemShipped {
		t.Fatalf("恢复后应记录系统发货: order=%+v err=%v", order, err)
	}
}

// TestCenterConfirmShipment_MockMTopConsigError 用 mock mtop 验证：
// ConsignContext 返回错误时 confirmShipment 透传错误，不写 system_shipped；
// ok=false 但无错误时返回"确认发货失败"错误。
// TestCenterConfirmShipment_MockMTopConsigError 封装TestCenterConfirmShipmentMockMTopConsig错误业务协调。
func TestCenterConfirmShipment_MockMTopConsigError(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(ctx, "admin")

	// 插入卡密 + 多规格商品 + 付款发货规则（含 confirm_shipment 动作）。
	store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title,is_multi_spec) VALUES ('cid','item-1','会员',1)`)
	store.DB.ExecContext(ctx, `INSERT INTO cards (id,name,type,data_content,enabled,user_id) VALUES (50,'卡','data','K1',1,?)`, admin.ID)
	store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: admin.ID, CookieID: "cid", ItemID: "item-1", Name: "付款发货",
		TriggerType: TriggerOrderPaid, Enabled: true, Priority: 100,
		Actions: []db.AutomationActionInput{
			{ActionType: ActionSendCard, CardID: 50, DeliveryCount: 1, Enabled: true, SortOrder: 1},
			{ActionType: ActionConfirmShipment, Enabled: true, SortOrder: 2},
		},
	})

	// ConsignContext 报错。
	mtopMock := &fakeMTop{consignErr: errors.New("network down")}
	// center 用于本次流程后续判断的center
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{
		MTop:               mtopMock,
		OrderDetailFetcher: testFetcher{detail: &OrderDetail{Quantity: "1", Amount: "9.9"}},
	})

	// HandleTask 内部记录 executeRule 失败到 automation_runs（不向上透传错误）。
	_ = center.HandleTask(ctx, Task{
		Source: "ws", AccountID: "cid", CookieStr: "unb=1; _m_h5_tk=tk;", TriggerType: TriggerOrderPaid,
		ChatID: "chat-1", OrderID: "order-mock", ItemID: "item-1", BuyerID: "buyer-1",
	})
	if mtopMock.consignCalls != 1 {
		t.Fatalf("ConsignContext 应被调用一次，got %d", mtopMock.consignCalls)
	}
	if mtopMock.consignOrderIn != "order-mock" {
		t.Errorf("传入 order_id 异常: %q", mtopMock.consignOrderIn)
	}
	// 失败不应标记 system_shipped。
	order, _ := store.Orders.Get(ctx, "order-mock")
	if order.SystemShipped {
		t.Fatal("consign 失败不应写 system_shipped=1")
	}
	// 网络错误无法确认远端是否已经发货，必须进入人工核对而不是自动重试。
	var runStatus, runErr string
	store.DB.QueryRowContext(ctx, `SELECT status, error_message FROM automation_runs WHERE order_id='order-mock'`).Scan(&runStatus, &runErr)
	if runStatus != "needs_review" {
		t.Fatalf("run status=%q want needs_review", runStatus)
	}
	if runErr == "" {
		t.Fatal("失败 run 应记录错误信息")
	}

	// 第二轮：ConsignContext 返回 ok=false（业务失败），run 同样记 failed。
	_ = store.Orders.Upsert(ctx, "order-mock2", db.OrderUpsertOpts{CookieID: "cid", ItemID: "item-1", BuyerID: "buyer-1"})
	// center2 用于本次流程后续判断的center2
	center2 := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{
		MTop:               &fakeMTop{consignOk: false, consignRet: []string{"FAIL_SHIP"}},
		OrderDetailFetcher: testFetcher{detail: &OrderDetail{Quantity: "1", Amount: "9.9"}},
	})
	_ = center2.HandleTask(ctx, Task{
		Source: "ws", AccountID: "cid", CookieStr: "unb=1; _m_h5_tk=tk;", TriggerType: TriggerOrderPaid,
		ChatID: "chat-2", OrderID: "order-mock2", ItemID: "item-1", BuyerID: "buyer-1",
	})
	store.DB.QueryRowContext(ctx, `SELECT status FROM automation_runs WHERE order_id='order-mock2'`).Scan(&runStatus)
	if runStatus != "failed" {
		t.Fatalf("ok=false 应记 failed，got %q", runStatus)
	}
}

// TestConfirmShipmentQuarantinesKnownRemoteSuccessWhenLocalPersistenceFails 封装TestConfirmShipmentQuarantinesKnownRemoteSuccessWhenLocalPersistenceFails业务协调。
func TestConfirmShipmentQuarantinesKnownRemoteSuccessWhenLocalPersistenceFails(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // err 用于本次流程后续判断的err
	err := store.Orders.Upsert(ctx, "persist-failure", db.OrderUpsertOpts{CookieID: "cid", ItemID: "item-1", BuyerID: "buyer"}); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	_, err := store.DB.ExecContext(ctx, `CREATE TRIGGER reject_shipped_state
		BEFORE UPDATE OF system_shipped ON orders
		WHEN NEW.system_shipped=1
		BEGIN SELECT RAISE(FAIL, 'forced shipment persistence failure'); END`); err != nil {
		t.Fatal(err)
	}
	// mtopMock 用于本次流程后续判断的mtopMock
	mtopMock := &fakeMTop{consignOk: true, consignUpdated: "unb=123; _m_h5_tk=updated_1;"}
	// center 用于本次流程后续判断的center
	center := NewWithDependencies(store, testSenderProvider{sender: &testSender{}}, nil, CenterDependencies{MTop: mtopMock})
	// err 用于本次流程后续判断的err
	err := center.confirmShipment(ctx, Task{
		AccountID: "cid", OrderID: "persist-failure", ItemID: "item-1", BuyerID: "buyer", ChatID: "chat",
	})
	// uncertain 用于本次流程后续判断的uncertain
	var uncertain *uncertainActionError
	if !errors.As(err, &uncertain) {
		t.Fatalf("known remote success with local failure must be quarantined, got %v", err)
	}
	if !strings.Contains(err.Error(), "闲鱼已确认发货") || !strings.Contains(err.Error(), "本地状态保存失败") {
		t.Fatalf("unexpected error: %v", err)
	}
	// order、getErr 用于本次流程后续判断的order、getErr
	order, getErr := store.Orders.Get(ctx, "persist-failure")
	if getErr != nil {
		t.Fatal(getErr)
	}
	if order.SystemShipped {
		t.Fatal("failed local write must not be reported as persisted")
	}
	// pendingReconciliations 保存远端成功后创建的本地订单补偿记录。
	pendingReconciliations, listErr := store.Reconciliations.ListPending(ctx, 10)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(pendingReconciliations) != 1 || pendingReconciliations[0].OrderID != "persist-failure" || pendingReconciliations[0].Kind != "manual_status_ship" {
		t.Fatalf("远端发货成功后必须创建订单补偿记录: %+v", pendingReconciliations)
	}
	// err 表示移除一次性故障触发器时产生的数据库错误。
	if _, err := store.DB.ExecContext(ctx, `DROP TRIGGER reject_shipped_state`); err != nil {
		t.Fatal(err)
	}
	// err 表示补偿 worker 首次重试时返回的扫描级错误。
	if err := reconciliation.New(store, nil).RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	// reconciledOrder、getErr 保存补偿后的订单状态及查询错误。
	reconciledOrder, getErr := store.Orders.Get(ctx, "persist-failure")
	if getErr != nil || reconciledOrder == nil || !reconciledOrder.SystemShipped || reconciledOrder.OrderStatus != "shipped" {
		t.Fatalf("补偿 worker 未修复订单状态: order=%+v err=%v", reconciledOrder, getErr)
	}
	// remainingReconciliations 保存补偿完成后仍待处理的记录。
	remainingReconciliations, listErr := store.Reconciliations.ListPending(ctx, 10)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(remainingReconciliations) != 0 {
		t.Fatalf("补偿完成后不应残留 pending 记录: %+v", remainingReconciliations)
	}
}

// TestConfirmShipmentKeepsAuthoritativeSnapshotWhenSessionUnchanged 封装TestConfirmShipmentKeepsAuthoritativeSnapshotWhen会话Unchanged业务协调。
func TestConfirmShipmentKeepsAuthoritativeSnapshotWhenSessionUnchanged(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// initial 用于本次流程后续判断的initial
	initial := "unb=123; _m_h5_tk=old_1;"
	// metadata 用于本次流程后续判断的metadata
	metadata := cookierefresh.MetadataWithSnapshot(`{"preserved":true}`, []cookierefresh.BrowserCookie{
		{Name: "unb", Value: "123", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "_m_h5_tk", Value: "old_1", Domain: ".goofish.com", Path: "/", Secure: true},
	})
	if // err 用于本次流程后续判断的err
	err := store.Cookies.UpdateRenewalCookie(ctx, "cid", initial, metadata, 1); err != nil {
		t.Fatal(err)
	}
	// updated 用于本次流程后续判断的updated
	updated := "unb=123; _m_h5_tk=mock_new_2;"
	// sender 用于本次流程后续判断的sender
	sender := &testSender{}
	// center 用于本次流程后续判断的center
	center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{
		MTop: &fakeMTop{consignOk: false, consignRet: []string{"FAIL_SHIP"}, consignUpdated: updated},
	})
	// err 用于本次流程后续判断的err
	err := center.confirmShipment(ctx, Task{
		AccountID: "cid", OrderID: "flat-mock-fallback", ForceConfirmShipment: true,
	})
	if err == nil || !strings.Contains(err.Error(), "FAIL_SHIP") {
		t.Fatalf("mock 业务失败应保留原返回语义: %v", err)
	}
	// detail、getErr 用于本次流程后续判断的detail、getErr
	detail, getErr := store.Cookies.GetDetails(ctx, "cid")
	if getErr != nil {
		t.Fatal(getErr)
	}
	if detail.Value != initial {
		t.Fatalf("完整 Jar 未变化时不得被扁平/mock 返回覆盖: %q", detail.Value)
	}
	if // snapshot、ok 用于本次流程后续判断的snapshot、ok
	snapshot, ok := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON); !ok || len(snapshot) != 2 {
		t.Fatalf("完整 Jar 未变化时必须继续保留: ok=%v snapshot=%+v metadata=%s", ok, snapshot, detail.MetadataJSON)
	}
	if !strings.Contains(detail.MetadataJSON, `"preserved":true`) {
		t.Fatalf("保留 snapshot 时丢失其他 metadata: %s", detail.MetadataJSON)
	}
	if len(sender.cookieUpdates) != 0 {
		t.Fatalf("被忽略的扁平/mock 返回不得同步运行实例: %+v", sender.cookieUpdates)
	}
}

// TestConfirmShipmentKeepsFlatMockFallbackWithoutSnapshot 封装TestConfirmShipmentKeepsFlatMockFallbackWithoutSnapshot业务协调。
func TestConfirmShipmentKeepsFlatMockFallbackWithoutSnapshot(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// initial 用于本次流程后续判断的initial
	initial := "unb=123; _m_h5_tk=old_1;"
	if // err 用于本次流程后续判断的err
	err := store.Cookies.UpdateRenewalCookie(ctx, "cid", initial, `{"preserved":true}`, 1); err != nil {
		t.Fatal(err)
	}
	// updated 用于本次流程后续判断的updated
	updated := "unb=123; _m_h5_tk=mock_new_2;"
	// sender 用于本次流程后续判断的sender
	sender := &testSender{}
	// center 用于本次流程后续判断的center
	center := NewWithDependencies(store, testSenderProvider{sender: sender}, nil, CenterDependencies{
		MTop: &fakeMTop{consignOk: false, consignRet: []string{"FAIL_SHIP"}, consignUpdated: updated},
	})
	// err 用于本次流程后续判断的err
	err := center.confirmShipment(ctx, Task{
		AccountID: "cid", OrderID: "flat-mock-fallback", ForceConfirmShipment: true,
	})
	if err == nil || !strings.Contains(err.Error(), "FAIL_SHIP") {
		t.Fatalf("mock 业务失败应保留原返回语义: %v", err)
	}
	// detail、getErr 用于本次流程后续判断的detail、getErr
	detail, getErr := store.Cookies.GetDetails(ctx, "cid")
	if getErr != nil {
		t.Fatal(getErr)
	}
	if detail.Value != updated {
		t.Fatalf("无完整 Jar 时未保留扁平/mock 写回路径: %q", detail.Value)
	}
	if // ok 用于本次流程后续判断的ok
	_, ok := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON); ok {
		t.Fatalf("扁平 mock 结果不得伪装成权威 Jar: %s", detail.MetadataJSON)
	}
	if !strings.Contains(detail.MetadataJSON, `"preserved":true`) {
		t.Fatalf("扁平写回时丢失其他 metadata: %s", detail.MetadataJSON)
	}
	if len(sender.cookieUpdates) != 1 || sender.cookieUpdates[0] != updated {
		t.Fatalf("扁平/mock 更新未同步运行实例: %+v", sender.cookieUpdates)
	}
}
