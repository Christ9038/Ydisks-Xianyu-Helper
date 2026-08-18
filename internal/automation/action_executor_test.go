package automation

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// blockingAutomationSender 在发送入口阻塞，用于验证库存锁不会覆盖外部消息 I/O。
type blockingAutomationSender struct {
	// calls 记录进入发送入口的并发调用次数。
	calls int32
	// firstEntered 通知第一个发送调用已经拿到卡密并进入外部 I/O。
	firstEntered chan struct{}
	// secondEntered 通知第二个发送调用也已经进入外部 I/O。
	secondEntered chan struct{}
	// release 允许被阻塞的外部发送继续完成。
	release chan struct{}
}

// blockingSenderProvider 为并发测试提供阻塞发送器。
type blockingSenderProvider struct {
	// sender 保存待注入的阻塞发送器。
	sender MessageSender
}

// Sender 返回并发测试使用的阻塞发送器。
func (p blockingSenderProvider) Sender(string) (MessageSender, bool) {
	return p.sender, true
}

// SendText 模拟一个可控的慢速外部发送。
func (s *blockingAutomationSender) SendText(context.Context, string, string, string) error {
	// callNumber 保存本次发送在测试中的并发序号。
	callNumber := atomic.AddInt32(&s.calls, 1)
	if callNumber == 1 {
		close(s.firstEntered)
	}
	if callNumber == 2 {
		close(s.secondEntered)
	}
	<-s.release
	return nil
}

// SendImage 满足消息发送接口；数据卡测试不会调用图片发送。
func (s *blockingAutomationSender) SendImage(context.Context, string, string, string, int64, int, int) error {
	return nil
}

// UpdateCookie 满足消息发送接口；本测试不需要更新运行时 Cookie。
func (s *blockingAutomationSender) UpdateCookie(string) {}

// TestAutomationActionExecutorPreservesMessageNotSent 验证动作执行器保留“确定未发送”错误，供运行协调器安全重试。
func TestAutomationActionExecutorPreservesMessageNotSent(t *testing.T) {
	// sender 是返回确定未发送错误的测试发送器。
	sender := &testSender{err: fmt.Errorf("%w: websocket 尚未就绪", ErrMessageNotSent)}
	// executor 是仅注入消息发送器的动作执行器。
	executor := automationActionExecutor{senders: testSenderProvider{sender: sender}}
	// sent 是动作执行器报告的已发送数量。
	sent, err := executor.executeAction(context.Background(), Task{
		AccountID: "cid",
		ChatID:    "chat",
		BuyerID:   "buyer",
	}, db.AutomationAction{ActionType: ActionSendText, MessageTemplate: "hello"})
	if sent != 0 || !errors.Is(err, ErrMessageNotSent) {
		t.Fatalf("确定未发送错误未保留: sent=%d err=%v", sent, err)
	}
}

// TestSendDataCardReleasesInventoryLockBeforeExternalSend 验证第二个库存操作不会等待第一个外部发送完成。
func TestSendDataCardReleasesInventoryLockBeforeExternalSend(t *testing.T) {
	// store、cleanup 保存测试数据库及其清理函数。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 保存本测试共用的上下文。
	ctx := context.Background()
	// admin 保存创建卡券组所需的管理员用户。
	admin, err := store.Users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	// cardID 保存包含两条库存数据的卡券组标识。
	cardID, err := store.Cards.Create(ctx, &db.CardFull{
		Name: "concurrent-data", Type: "data", DataContent: "secret-1\nsecret-2", Enabled: true, UserID: admin.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	// sender 保存可控制外部发送完成时机的测试发送器。
	sender := &blockingAutomationSender{
		firstEntered:  make(chan struct{}),
		secondEntered: make(chan struct{}),
		release:       make(chan struct{}),
	}
	// center 保存注入测试发送器的自动化中心。
	center := New(store, blockingSenderProvider{sender: sender}, nil)
	// result 保存两个并发动作的返回结果。
	result := make(chan error, 2)
	// task 保存两个动作共用的订单消息上下文。
	task := Task{AccountID: "cid", ChatID: "chat", BuyerID: "buyer"}
	// action 保存每次发送一条数据卡密的动作配置。
	action := db.AutomationAction{ActionType: ActionSendCard, CardID: cardID, DeliveryCount: 1, ConfigJSON: `{}`}
	go func() {
		// runErr 保存第一个并发卡密动作的执行错误。
		_, runErr := center.sendCard(ctx, task, action)
		result <- runErr
	}()
	select {
	case <-sender.firstEntered:
	case <-time.After(time.Second):
		t.Fatal("第一个发送调用未进入外部 I/O")
	}
	go func() {
		// runErr 保存第二个并发卡密动作的执行错误。
		_, runErr := center.sendCard(ctx, task, action)
		result <- runErr
	}()
	select {
	case <-sender.secondEntered:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("第二个库存操作被第一个外部发送阻塞")
	}
	close(sender.release)
	for range 2 {
		// runErr 保存并发动作收口时的执行错误。
		if runErr := <-result; runErr != nil {
			t.Fatalf("并发数据卡发送失败: %v", runErr)
		}
	}
}
