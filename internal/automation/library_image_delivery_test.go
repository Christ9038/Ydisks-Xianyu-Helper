package automation

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/db"
	"xianyu-go/internal/deliverytemplate"
)

// libraryImageSender 在文本发送替身上增加图片库图片发送能力，并记录每次发送的图片主键与发送顺序。
type libraryImageSender struct {
	// testSender 提供文本发送与 Cookie 更新的默认实现。
	*testSender
	// imageIDs 保存已发送的图片库图片主键。
	imageIDs []int64
	// cardIDs 保存发送时关联的卡密组主键。
	cardIDs []int64
	// err 是图片库图片发送返回的错误。
	err error
	// order 保存文本与图片的混合发送顺序（"text:..." 或 "image:ID"）。
	order []string
}

// SendText 记录文本发送顺序后复用默认文本发送。
func (s *libraryImageSender) SendText(ctx context.Context, chatID, toUserID, text string) error {
	s.order = append(s.order, "text:"+text)
	return s.testSender.SendText(ctx, chatID, toUserID, text)
}

// SendLibraryImage 记录图片库图片发送，失败时返回预置错误。
func (s *libraryImageSender) SendLibraryImage(_ context.Context, _, _ string, imageID, cardID int64) error {
	if s.err != nil {
		return s.err
	}
	s.imageIDs = append(s.imageIDs, imageID)
	s.cardIDs = append(s.cardIDs, cardID)
	s.order = append(s.order, "image:"+string(rune('0'+imageID%10)))
	return nil
}

// TestLibraryImageCardDeliveryRecordsSnapshotAndHandlesFailures 验证图片库图片卡密的成功、确定未发送、结果不明和发送器不支持分支。
func TestLibraryImageCardDeliveryRecordsSnapshotAndHandlesFailures(t *testing.T) {
	// ctx 与 task 是调用共用的上下文和完整会话信息的发货任务。
	ctx := context.Background()
	// task 为本处局部值。
	task := Task{AccountID: "cid", OrderID: "lib-card", ChatID: "chat", BuyerID: "buyer", Quantity: "2"}
	// newAction 创建图片库图片卡密组并返回发卡动作。
	newAction := func(t *testing.T, store *db.Store) db.AutomationAction {
		t.Helper()
		// admin 为本处局部值。
		admin, _ := store.Users.GetByUsername(ctx, "admin")
		// cardID 为本处局部值；err 保存被测调用的失败原因，断言时按预期分支检查。
		cardID, err := store.Cards.Create(ctx, &db.CardFull{Name: "lib", Type: "image", LibraryImageID: 42, Enabled: true, UserID: admin.ID})
		if err != nil {
			t.Fatal(err)
		}
		return db.AutomationAction{ActionType: ActionSendCard, Enabled: true, CardID: cardID, DeliveryCount: 1, ConfigJSON: `{}`}
	}

	t.Run("成功按数量发送并记录图片库快照", func(t *testing.T) {
		// cleanup 关闭测试数据库并释放临时资源；store 为本处局部值。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		// sender 为本处局部值。
		sender := &libraryImageSender{testSender: &testSender{}}
		// center 为本处局部值。
		center := New(store, anySenderProvider{sender: sender}, nil)
		// err 保存被测调用的失败原因，断言时按预期分支检查；result 是插入用户后的执行结果，用于取得新用户主键。
		result, err := center.actions.sendCardWithProof(ctx, task, newAction(t, store))
		if err != nil || result.sent != 2 || len(sender.imageIDs) != 2 || sender.imageIDs[0] != 42 || sender.cardIDs[0] == 0 {
			t.Fatalf("result=%+v ids=%v err=%v", result, sender.imageIDs, err)
		}
		if len(result.proof.messages) != 2 || result.proof.messages[0].Kind != "library_image" || result.proof.messages[0].Content != "42" || len(result.proof.picList) != 0 {
			t.Fatalf("快照异常: %+v", result.proof)
		}
	})
	t.Run("读取失败为确定未发送且不留待核对凭证", func(t *testing.T) {
		// cleanup 关闭测试数据库并释放临时资源；store 为本处局部值。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		// sender 为本处局部值。
		sender := &libraryImageSender{testSender: &testSender{}, err: errors.Join(ErrMessageNotSent, errors.New("图片已删除"))}
		// center 为本处局部值。
		center := New(store, anySenderProvider{sender: sender}, nil)
		// err 保存被测调用的失败原因，断言时按预期分支检查；result 是插入用户后的执行结果，用于取得新用户主键。
		result, err := center.actions.sendCardWithProof(ctx, task, newAction(t, store))
		if !errors.Is(err, ErrMessageNotSent) || len(result.reviewProof.messages) != 0 {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})
	t.Run("结果不明保留图片库待核对凭证", func(t *testing.T) {
		// cleanup 关闭测试数据库并释放临时资源；store 为本处局部值。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		// sender 为本处局部值。
		sender := &libraryImageSender{testSender: &testSender{}, err: errors.New("连接中断")}
		// center 为本处局部值。
		center := New(store, anySenderProvider{sender: sender}, nil)
		// err 保存被测调用的失败原因，断言时按预期分支检查；result 是插入用户后的执行结果，用于取得新用户主键。
		result, err := center.actions.sendCardWithProof(ctx, task, newAction(t, store))
		if err == nil || len(result.reviewProof.messages) != 1 || result.reviewProof.messages[0].Kind != "library_image" || result.reviewProof.unknownUnits != 1 {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})
	t.Run("发送器不支持图片库图片为确定未发送", func(t *testing.T) {
		// cleanup 关闭测试数据库并释放临时资源；store 为本处局部值。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		// center 为本处局部值。
		center := New(store, testSenderProvider{sender: &testSender{}}, nil)
		// err 保存被测调用的失败原因，断言时按预期分支检查。
		_, err := center.actions.sendCardWithProof(ctx, task, newAction(t, store))
		if !errors.Is(err, ErrMessageNotSent) {
			t.Fatalf("err=%v", err)
		}
	})
}

// TestSendTemplateSendsLibraryImageMessagesInOrder 验证模板中的图片消息与文字消息按模板顺序发送，图片不参与变量替换和卡密预留。
func TestSendTemplateSendsLibraryImageMessagesInOrder(t *testing.T) {
	// ctx 是本用例共用的请求上下文。
	ctx := context.Background()
	// cleanup 关闭测试数据库并释放临时资源；store 为本处局部值。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// sender 为本处局部值。
	sender := &libraryImageSender{testSender: &testSender{}}
	// executor 为本处局部值。
	executor := automationActionExecutor{store: store, senders: anySenderProvider{sender: sender}}
	// action 为本处局部值。
	action := db.AutomationAction{ActionType: ActionSendTemplate, ConfigJSON: "{}",
		TemplateMessages: []string{"你好 {{buyer_nickname}}", deliverytemplate.ImageToken(7), "再见"}}
	// err 保存被测调用的失败原因，断言时按预期分支检查；result 是插入用户后的执行结果，用于取得新用户主键。
	result, err := executor.sendTemplate(ctx, Task{AccountID: "cid", OrderID: "o", ChatID: "chat", BuyerID: "buyer", BuyerNickname: "小鱼", Quantity: "1", TriggerType: TriggerOrderPaid}, action)
	if err != nil || result.sent != 3 || result.proof.preparedUnits != 3 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if len(sender.order) != 3 || sender.order[0] != "text:你好 小鱼" || sender.order[1] != "image:7" || sender.order[2] != "text:再见" {
		t.Fatalf("发送顺序错误: %v", sender.order)
	}
	// kinds 保存凭证中各消息的类型，补发时据此恢复发送方式。
	kinds := []string{result.proof.messages[0].Kind, result.proof.messages[1].Kind, result.proof.messages[2].Kind}
	if kinds[0] != "text" || kinds[1] != "library_image" || kinds[2] != "text" || result.proof.messages[1].Content != "7" {
		t.Fatalf("凭证消息异常: %+v", result.proof.messages)
	}
}

// TestSendTemplateLibraryImageFailureBranches 验证图片消息确定未发送与结果不明时的凭证处理。
func TestSendTemplateLibraryImageFailureBranches(t *testing.T) {
	// ctx 是本用例共用的请求上下文。
	ctx := context.Background()
	// task 为本处局部值。
	task := Task{AccountID: "cid", OrderID: "o", ChatID: "chat", BuyerID: "buyer", Quantity: "1", TriggerType: TriggerOrderPaid}
	// action 为本处局部值。
	action := db.AutomationAction{ActionType: ActionSendTemplate, ConfigJSON: "{}", TemplateMessages: []string{deliverytemplate.ImageToken(7)}}

	// cleanup 关闭测试数据库并释放临时资源；store 为本处局部值。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// notSent 为本处局部值。
	notSent := &libraryImageSender{testSender: &testSender{}, err: errors.Join(ErrMessageNotSent, errors.New("图片已删除"))}
	// err 保存被测调用的失败原因，断言时按预期分支检查。
	if _, err := (&automationActionExecutor{store: store, senders: anySenderProvider{sender: notSent}}).sendTemplate(ctx, task, action); !errors.Is(err, ErrMessageNotSent) {
		t.Fatalf("确定未发送应保留 ErrMessageNotSent: %v", err)
	}
	// uncertain 为本处局部值。
	uncertain := &libraryImageSender{testSender: &testSender{}, err: errors.New("连接中断")}
	// err 保存被测调用的失败原因，断言时按预期分支检查；result 是插入用户后的执行结果，用于取得新用户主键。
	result, err := (&automationActionExecutor{store: store, senders: anySenderProvider{sender: uncertain}}).sendTemplate(ctx, task, action)
	if err == nil || errors.Is(err, ErrMessageNotSent) || len(result.reviewProof.messages) != 1 || result.reviewProof.messages[0].Kind != "library_image" {
		t.Fatalf("结果不明应保留待核对凭证: result=%+v err=%v", result, err)
	}
}

// TestManualRedeliveryReplaysLibraryImageSnapshot 验证人工原样补发会按快照中的图片库图片主键重新发送，且无效主键保留人工核对。
func TestManualRedeliveryReplaysLibraryImageSnapshot(t *testing.T) {
	t.Run("按快照主键补发", func(t *testing.T) {
		// ctx、store、center、order、cleanup 保存人工发货夹具。
		ctx, store, center, _, mtopMock, order, cleanup := newManualDeliveryFixture(t, "library-image-replay")
		defer cleanup()
		// sender 替换夹具发送器，记录图片库图片补发。
		sender := &libraryImageSender{testSender: &testSender{}}
		center.actions.senders = anySenderProvider{sender: sender}
		seedQuarantinedManualReplay(t, ctx, store, order, db.AutomationDeliveryProof{Messages: []db.AutomationDeliveryMessage{{Kind: "library_image", Content: "42"}}, ExpectedUnits: 1, PreparedUnits: 1}, 1)
		// err 保存被测调用的失败原因，断言时按预期分支检查；sent 为本处局部值。
		if sent, err := center.ManualFullDelivery(ctx, order); err != nil || sent != 1 || mtopMock.consignCalls != 1 {
			t.Fatalf("sent=%d err=%v", sent, err)
		}
		if len(sender.imageIDs) != 1 || sender.imageIDs[0] != 42 || sender.cardIDs[0] != 0 {
			t.Fatalf("应按快照补发图片库图片: ids=%v", sender.imageIDs)
		}
	})
	t.Run("无效主键不发送也不确认", func(t *testing.T) {
		// center 为本处局部值；cleanup 关闭测试数据库并释放临时资源；ctx 是本用例共用的请求上下文；mtopMock 为本处局部值；order 为本处局部值；store 为本处局部值。
		ctx, store, center, _, mtopMock, order, cleanup := newManualDeliveryFixture(t, "library-image-bad-id")
		defer cleanup()
		// sender 为本处局部值。
		sender := &libraryImageSender{testSender: &testSender{}}
		center.actions.senders = anySenderProvider{sender: sender}
		seedQuarantinedManualReplay(t, ctx, store, order, db.AutomationDeliveryProof{Messages: []db.AutomationDeliveryMessage{{Kind: "library_image", Content: "abc"}}, ExpectedUnits: 1, PreparedUnits: 1}, 1)
		// err 保存被测调用的失败原因，断言时按预期分支检查。
		if _, err := center.ManualFullDelivery(ctx, order); err == nil || mtopMock.consignCalls != 0 || len(sender.imageIDs) != 0 {
			t.Fatalf("无效主键应报错且不发送、不确认: err=%v consign=%d", err, mtopMock.consignCalls)
		}
	})
}
