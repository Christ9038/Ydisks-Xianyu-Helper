package automation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xianyu-go/internal/db"
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
}

func (f *fakeMTop) FetchUserProfile(context.Context, string) (*mtop.UserProfileResult, error) {
	return nil, nil
}
func (f *fakeMTop) ConsignContext(_ context.Context, cookiesStr, orderID string) (bool, []string, string, error) {
	f.consignCalls++
	f.consignCookieIn = cookiesStr
	f.consignOrderIn = orderID
	return f.consignOk, f.consignRet, f.consignUpdated, f.consignErr
}
func (f *fakeMTop) FetchItemsPage(context.Context, string, int, int) (*mtop.ItemListResult, error) {
	return nil, nil
}
func (f *fakeMTop) FetchAllItems(context.Context, string, int, int) (*mtop.ItemListResult, error) {
	return nil, nil
}
func (f *fakeMTop) PublishItem(context.Context, string, mtop.PublishItemRequest) (*mtop.PublishItemResult, error) {
	return nil, nil
}
func (f *fakeMTop) RefreshTokenWithDeviceIDContext(context.Context, string, string) (*mtop.RefreshResult, error) {
	return nil, nil
}

// TestCenterConfirmShipment_MockMTopConsigError 用 mock mtop 验证：
// ConsignContext 返回错误时 confirmShipment 透传错误，不写 system_shipped；
// ok=false 但无错误时返回"确认发货失败"错误。
func TestCenterConfirmShipment_MockMTopConsigError(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
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
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	center.SetMTop(mtopMock)
	center.SetOrderDetailFetcher(testFetcher{detail: &OrderDetail{Quantity: "1", Amount: "9.9"}})

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
	center2 := New(store, testSenderProvider{sender: &testSender{}}, nil)
	center2.SetMTop(&fakeMTop{consignOk: false, consignRet: []string{"FAIL_SHIP"}})
	center2.SetOrderDetailFetcher(testFetcher{detail: &OrderDetail{Quantity: "1", Amount: "9.9"}})
	_ = center2.HandleTask(ctx, Task{
		Source: "ws", AccountID: "cid", CookieStr: "unb=1; _m_h5_tk=tk;", TriggerType: TriggerOrderPaid,
		ChatID: "chat-2", OrderID: "order-mock2", ItemID: "item-1", BuyerID: "buyer-1",
	})
	store.DB.QueryRowContext(ctx, `SELECT status FROM automation_runs WHERE order_id='order-mock2'`).Scan(&runStatus)
	if runStatus != "failed" {
		t.Fatalf("ok=false 应记 failed，got %q", runStatus)
	}
}

func TestConfirmShipmentQuarantinesKnownRemoteSuccessWhenLocalPersistenceFails(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	if err := store.Orders.Upsert(ctx, "persist-failure", db.OrderUpsertOpts{CookieID: "cid", ItemID: "item-1", BuyerID: "buyer"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.ExecContext(ctx, `CREATE TRIGGER reject_shipped_state
		BEFORE UPDATE OF system_shipped ON orders
		WHEN NEW.system_shipped=1
		BEGIN SELECT RAISE(FAIL, 'forced shipment persistence failure'); END`); err != nil {
		t.Fatal(err)
	}
	mtopMock := &fakeMTop{consignOk: true, consignUpdated: "unb=123; _m_h5_tk=updated_1;"}
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	center.SetMTop(mtopMock)
	err := center.confirmShipment(ctx, Task{
		AccountID: "cid", OrderID: "persist-failure", ItemID: "item-1", BuyerID: "buyer", ChatID: "chat",
	})
	var uncertain *uncertainActionError
	if !errors.As(err, &uncertain) {
		t.Fatalf("known remote success with local failure must be quarantined, got %v", err)
	}
	if !strings.Contains(err.Error(), "闲鱼已确认发货") || !strings.Contains(err.Error(), "本地状态保存失败") {
		t.Fatalf("unexpected error: %v", err)
	}
	order, getErr := store.Orders.Get(ctx, "persist-failure")
	if getErr != nil {
		t.Fatal(getErr)
	}
	if order.SystemShipped {
		t.Fatal("failed local write must not be reported as persisted")
	}
}

func TestConfirmShipmentKeepsFlatMockFallbackWhenSessionUnchanged(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	ctx := context.Background()
	initial := "unb=123; _m_h5_tk=old_1;"
	metadata := cookierefresh.MetadataWithSnapshot(`{"preserved":true}`, []cookierefresh.BrowserCookie{
		{Name: "unb", Value: "123", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "_m_h5_tk", Value: "old_1", Domain: ".goofish.com", Path: "/", Secure: true},
	})
	if err := store.Cookies.UpdateRenewalCookie(ctx, "cid", initial, metadata, 1); err != nil {
		t.Fatal(err)
	}
	updated := "unb=123; _m_h5_tk=mock_new_2;"
	sender := &testSender{}
	center := New(store, testSenderProvider{sender: sender}, nil)
	center.SetMTop(&fakeMTop{consignOk: false, consignRet: []string{"FAIL_SHIP"}, consignUpdated: updated})
	err := center.confirmShipment(ctx, Task{
		AccountID: "cid", OrderID: "flat-mock-fallback", ForceConfirmShipment: true,
	})
	if err == nil || !strings.Contains(err.Error(), "FAIL_SHIP") {
		t.Fatalf("mock 业务失败应保留原返回语义: %v", err)
	}
	detail, getErr := store.Cookies.GetDetails(ctx, "cid")
	if getErr != nil {
		t.Fatal(getErr)
	}
	if detail.Value != updated {
		t.Fatalf("session 未 changed 时未保留扁平/mock 写回路径: %q", detail.Value)
	}
	if _, ok := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON); ok {
		t.Fatalf("扁平 mock 结果不得伪装成权威 Jar: %s", detail.MetadataJSON)
	}
	if !strings.Contains(detail.MetadataJSON, `"preserved":true`) {
		t.Fatalf("清理旧 snapshot 时丢失其他 metadata: %s", detail.MetadataJSON)
	}
	if len(sender.cookieUpdates) != 1 || sender.cookieUpdates[0] != updated {
		t.Fatalf("扁平/mock 更新未同步运行实例: %+v", sender.cookieUpdates)
	}
}
