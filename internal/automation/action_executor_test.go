package automation

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"xianyu-go/internal/db"
)

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
