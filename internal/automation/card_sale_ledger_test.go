package automation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// ledgerFixture 保存售出台账测试共用的隔离数据库、执行器、任务和卡密组所有者。
type ledgerFixture struct {
	// store 是隔离的测试数据库。
	store *db.Store
	// userID 是卡密组所有者，也是台账查询的隔离条件。
	userID int64
	// task 是带订单上下文的发货任务。
	task Task
}

// newLedgerFixture 创建隔离数据库和订单上下文；cleanup 由测试在结束时调用。
func newLedgerFixture(t *testing.T) (ledgerFixture, func()) {
	t.Helper()
	// store、cleanup 是隔离数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	// admin、adminErr 是默认管理员，作为卡密组所有者。
	admin, adminErr := store.Users.GetByUsername(context.Background(), "admin")
	if adminErr != nil {
		cleanup()
		t.Fatal(adminErr)
	}
	return ledgerFixture{store: store, userID: admin.ID,
		task: Task{AccountID: "cid", OrderID: "order-1", ItemID: "item-1", BuyerID: "buyer-1", ChatID: "chat", Quantity: "1"}}, cleanup
}

// createCard 创建指定类型的卡密组并返回标识。
func (f ledgerFixture) createCard(t *testing.T, card *db.CardFull) int64 {
	t.Helper()
	card.UserID, card.Enabled = f.userID, true
	// id、createErr 保存卡密组创建结果。
	id, createErr := f.store.Cards.Create(context.Background(), card)
	if createErr != nil {
		t.Fatal(createErr)
	}
	return id
}

// records 返回当前订单在台账中的全部记录。
func (f ledgerFixture) records(t *testing.T) []db.CardSaleRecord {
	t.Helper()
	// records、listErr 保存按订单读取台账的结果。
	records, listErr := f.store.Cards.ListSaleRecordsByOrder(context.Background(), f.userID, f.task.AccountID, f.task.OrderID)
	if listErr != nil {
		t.Fatal(listErr)
	}
	return records
}

// TestDataCardDeliveryWritesSoldLedger 验证批量卡密发送成功后台账为已售并带完整订单上下文。
func TestDataCardDeliveryWritesSoldLedger(t *testing.T) {
	// cleanup 是释放隔离测试数据库的清理函数，由测试在结束时调用；fixture 是隔离的测试夹具，包含数据库、任务和卡密组所有者。
	fixture, cleanup := newLedgerFixture(t)
	defer cleanup()
	// cardID 是被测卡密组标识。
	cardID := fixture.createCard(t, &db.CardFull{Name: "批量", Type: "data", DataContent: "K1\nK2"})
	// executor 是被测自动化动作执行器，注入了测试数据库和发送器替身。
	executor := automationActionExecutor{store: fixture.store, senders: testSenderProvider{sender: &testSender{}}}

	// result、err 保存发卡结果。
	result, err := executor.sendCardWithProof(context.Background(), fixture.task, db.AutomationAction{ActionType: ActionSendCard, CardID: cardID, DeliveryCount: 1, ConfigJSON: `{}`})
	if err != nil || result.sent != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	// records 保存读取到的台账记录，用于断言数量、状态与内容。
	records := fixture.records(t)
	if len(records) != 1 || records[0].Status != db.SaleStatusSold || records[0].Content != "K1" || records[0].ItemID != "item-1" ||
		records[0].BuyerID != "buyer-1" || records[0].CardID != cardID || records[0].SoldAt == 0 {
		t.Fatalf("台账应为已售且带订单上下文: %#v", records)
	}
}

// TestDataCardDeliveryNotSentRestoresLedger 验证确定未发送时库存恢复且台账撤销。
func TestDataCardDeliveryNotSentRestoresLedger(t *testing.T) {
	// cleanup 是释放隔离测试数据库的清理函数，由测试在结束时调用；fixture 是隔离的测试夹具，包含数据库、任务和卡密组所有者。
	fixture, cleanup := newLedgerFixture(t)
	defer cleanup()
	// cardID 是被测卡密组标识。
	cardID := fixture.createCard(t, &db.CardFull{Name: "批量", Type: "data", DataContent: "K1\nK2"})
	// executor 是被测自动化动作执行器，注入了测试数据库和发送器替身。
	executor := automationActionExecutor{store: fixture.store, senders: testSenderProvider{sender: &testSender{err: fmt.Errorf("%w: 离线", ErrMessageNotSent)}}}

	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := executor.sendCardWithProof(context.Background(), fixture.task, db.AutomationAction{ActionType: ActionSendCard, CardID: cardID, DeliveryCount: 1, ConfigJSON: `{}`}); !errors.Is(err, ErrMessageNotSent) {
		t.Fatalf("err=%v", err)
	}
	// records 保存读取到的台账记录，用于断言数量、状态与内容。
	if records := fixture.records(t); len(records) != 0 {
		t.Fatalf("确定未发送不得保留台账: %#v", records)
	}
	// content 应是恢复到头部的第一条卡密。
	content, _, reserveErr := fixture.store.Cards.ReserveBatchData(context.Background(), cardID, db.CardSaleMeta{})
	if reserveErr != nil || content != "K1" {
		t.Fatalf("库存应恢复, content=%q err=%v", content, reserveErr)
	}
}

// TestDataCardDeliveryUncertainKeepsReservedLedger 验证发送结果不确定时台账保持待核对，库存不回滚。
func TestDataCardDeliveryUncertainKeepsReservedLedger(t *testing.T) {
	// cleanup 是释放隔离测试数据库的清理函数，由测试在结束时调用；fixture 是隔离的测试夹具，包含数据库、任务和卡密组所有者。
	fixture, cleanup := newLedgerFixture(t)
	defer cleanup()
	// cardID 是被测卡密组标识。
	cardID := fixture.createCard(t, &db.CardFull{Name: "批量", Type: "data", DataContent: "K1\nK2"})
	// executor 是被测自动化动作执行器，注入了测试数据库和发送器替身。
	executor := automationActionExecutor{store: fixture.store, senders: testSenderProvider{sender: &testSender{err: errors.New("连接中断")}}}

	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := executor.sendCardWithProof(context.Background(), fixture.task, db.AutomationAction{ActionType: ActionSendCard, CardID: cardID, DeliveryCount: 1, ConfigJSON: `{}`}); err == nil {
		t.Fatal("结果不确定应返回错误")
	}
	// records 保存读取到的台账记录，用于断言数量、状态与内容。
	records := fixture.records(t)
	if len(records) != 1 || records[0].Status != db.SaleStatusReserved || records[0].Content != "K1" {
		t.Fatalf("应保留待核对台账: %#v", records)
	}
	// content 保存扣减出的卡密正文；err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if content, _, err := fixture.store.Cards.ReserveBatchData(context.Background(), cardID, db.CardSaleMeta{}); err != nil || content != "K2" {
		t.Fatalf("库存不应回滚, content=%q err=%v", content, err)
	}
}

// TestAPICardDeliveryWritesLedger 验证 API 卡发送成功记已售、发送结果不明记待核对。
func TestAPICardDeliveryWritesLedger(t *testing.T) {
	// scenarios 覆盖发送成功与发送结果不明两种台账状态。
	scenarios := []struct {
		name       string
		sendErr    error
		wantStatus string
	}{
		{"发送成功", nil, db.SaleStatusSold},
		{"发送结果不明", errors.New("连接中断"), db.SaleStatusReserved},
	}
	for /* scenario 是当前 API 卡场景 */ _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			// cleanup 是释放隔离测试数据库的清理函数，由测试在结束时调用；fixture 是隔离的测试夹具，包含数据库、任务和卡密组所有者。
			fixture, cleanup := newLedgerFixture(t)
			defer cleanup()
			// cardID 是被测卡密组标识。
			cardID := fixture.createCard(t, &db.CardFull{Name: "接口卡", Type: "api", APIConfig: `{}`})
			// fetcher 是返回固定内容的 API 卡取回替身。
			fetcher := &scriptedAPIFetcher{result: APICardResult{Content: "API-SECRET"}}
			// executor 是被测自动化动作执行器，注入了测试数据库和发送器替身。
			executor := automationActionExecutor{store: fixture.store, senders: anySenderProvider{sender: &testSender{err: scenario.sendErr}},
				apiFetcher: func() APICardFetcher { return fetcher }}
			// card 保存被测卡密组数据。
			card, _ := fixture.store.Cards.GetForDelivery(context.Background(), cardID)

			_, _ = executor.sendAPICardWithProof(context.Background(), fixture.task, db.AutomationAction{ID: 3}, card, 1)
			// records 保存读取到的台账记录，用于断言数量、状态与内容。
			records := fixture.records(t)
			if len(records) != 1 || records[0].Status != scenario.wantStatus || records[0].Content != "API-SECRET" || records[0].CardType != "api" {
				t.Fatalf("API 台账错误: %#v", records)
			}
		})
	}
}

// TestTemplateDeliveryLedger 验证模板批量卡密：发送成功记已售；API 变量先记待核对，不影响发货。
func TestTemplateDeliveryLedger(t *testing.T) {
	// cleanup 是释放隔离测试数据库的清理函数，由测试在结束时调用；fixture 是隔离的测试夹具，包含数据库、任务和卡密组所有者。
	fixture, cleanup := newLedgerFixture(t)
	defer cleanup()
	// dataID 是批量卡密组的标识。
	dataID := fixture.createCard(t, &db.CardFull{Name: "批量", Type: "data", DataContent: "T1\nT2"})
	// apiID 是 API 类型卡密组的标识。
	apiID := fixture.createCard(t, &db.CardFull{Name: "接口", Type: "api", APIConfig: `{}`})
	// fetcher 是返回固定内容的 API 卡取回替身。
	fetcher := &scriptedAPIFetcher{result: APICardResult{Content: "API-T"}}
	// executor 是被测自动化动作执行器，注入了测试数据库和发送器替身。
	executor := automationActionExecutor{store: fixture.store, senders: testSenderProvider{sender: &testSender{}},
		apiFetcher: func() APICardFetcher { return fetcher }}

	// err 保存被测调用返回的错误，用于断言成功或期望的失败；result 保存被测调用的返回结果。
	result, err := executor.sendTemplate(context.Background(), fixture.task, db.AutomationAction{ConfigJSON: `{}`,
		TemplateMessages: []string{"卡密 {{cards.code}} / {{cards.api}}"},
		TemplateBindings: []db.DeliveryTemplateBinding{{VariableKey: "code", CardID: dataID}, {VariableKey: "api", CardID: apiID}}})
	if err != nil || result.sent != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	// byContent 按内容索引台账状态。
	byContent := map[string]string{}
	for /* record 是当前订单的一条台账 */ _, record := range fixture.records(t) {
		byContent[record.Content] = record.Status
	}
	if len(byContent) != 2 || byContent["T1"] != db.SaleStatusSold || byContent["API-T"] != db.SaleStatusReserved {
		t.Fatalf("模板台账状态错误: %#v", byContent)
	}
}

// TestRunIDFromContext 验证持久化运行租约提供运行标识，其他上下文返回零。
func TestRunIDFromContext(t *testing.T) {
	if runIDFromContext(nil) != 0 || runIDFromContext(context.Background()) != 0 { //nolint:staticcheck // 历史独立动作测试允许 nil Context。
		t.Fatal("没有租约的上下文应返回 0")
	}
	// ctx 携带一个固定运行标识的租约。
	ctx := context.WithValue(context.Background(), runLeaseContextKey{}, &runExecutionLease{runID: 42})
	// got 保存被测调用的实际返回值，用于与期望值比较。
	if got := runIDFromContext(ctx); got != 42 {
		t.Fatalf("runID=%d", got)
	}
	// nilLease 是显式放入的空租约指针，必须按没有租约处理。
	nilLease := context.WithValue(context.Background(), runLeaseContextKey{}, (*runExecutionLease)(nil))
	// got 保存被测调用的实际返回值，用于与期望值比较。
	if got := runIDFromContext(nilLease); got != 0 {
		t.Fatalf("空租约应返回 0, got %d", got)
	}
}

// TestLedgerWriteFailuresNeverBreakDelivery 验证台账不可用或写入失败时只记警告，不 panic、不泄露卡密内容。
func TestLedgerWriteFailuresNeverBreakDelivery(t *testing.T) {
	// nilStore 模拟只注入取卡与发送依赖的独立动作测试：没有仓储时必须静默跳过。
	nilStore := automationActionExecutor{}
	nilStore.markSold(context.Background(), 1)
	nilStore.recordAPISale(context.Background(), Task{}, 1, "SECRET", db.SaleStatusSold)

	// fixture、cleanup 是隔离的测试夹具及其清理函数。
	fixture, cleanup := newLedgerFixture(t)
	defer cleanup()
	// logBuffer 捕获警告日志，用于断言不包含卡密内容。
	var logBuffer bytes.Buffer
	// executor 是注入了测试数据库和日志捕获器的动作执行器。
	executor := automationActionExecutor{store: fixture.store, logger: slog.New(slog.NewTextHandler(&logBuffer, nil))}
	// 关闭数据库后所有台账写入都会失败，但调用必须正常返回。
	_ = fixture.store.DB.Close()
	executor.markSold(context.Background(), 1)
	executor.recordAPISale(context.Background(), fixture.task, 1, "SECRET-CONTENT", db.SaleStatusSold)
	// logged 是捕获到的日志文本。
	logged := logBuffer.String()
	if !strings.Contains(logged, "确认卡密售出台账失败") || !strings.Contains(logged, "写入 API 卡密售出台账失败") {
		t.Fatalf("应记录两条警告: %q", logged)
	}
	if strings.Contains(logged, "SECRET-CONTENT") {
		t.Fatalf("日志不得包含卡密内容: %q", logged)
	}
}
