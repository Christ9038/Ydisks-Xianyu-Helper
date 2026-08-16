package automation

import (
	"context"
	"errors"
	"testing"
)

// publishRuleRepositoryFake 保存发布自动化规则应用服务测试所需的最小仓储替身。
type publishRuleRepositoryFake struct {
	// input 保存最近一次幂等准备收到的规则输入。
	input RuleInput
	// calls 记录幂等准备被调用的次数。
	calls int
	// err 是仓储替身需要返回的预设错误。
	err error
}

// EnsurePublishRule 记录发布规则输入并返回预设错误。
func (f *publishRuleRepositoryFake) EnsurePublishRule(_ context.Context, input RuleInput) error {
	f.input = input
	f.calls++
	return f.err
}

// TestPublishRuleServiceForwardsInput 验证发布规则应用服务只编排端口调用并保留输入。
func TestPublishRuleServiceForwardsInput(t *testing.T) {
	// repository 是记录应用服务调用的仓储替身。
	repository := &publishRuleRepositoryFake{}
	// service 是绑定测试仓储的发布规则应用服务。
	service := NewPublishRuleService(repository)
	// input 是代表批量发布结果的最小自动化规则输入。
	input := RuleInput{UserID: 7, CookieID: "account-1", ItemID: "item-1", Name: "付款后自动发货", TriggerType: TriggerOrderPaid, Actions: []ActionInput{{ActionType: ActionConfirmShipment, Enabled: true, SortOrder: 1}}}
	// err 保存应用服务调用结果。
	err := service.Ensure(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if repository.calls != 1 || repository.input.UserID != input.UserID || repository.input.Actions[0].ActionType != ActionConfirmShipment {
		t.Fatalf("应用服务未完整转发规则输入: calls=%d input=%+v", repository.calls, repository.input)
	}
}

// TestPublishRuleServicePropagatesRepositoryError 验证发布规则准备失败不会被应用层吞掉。
func TestPublishRuleServicePropagatesRepositoryError(t *testing.T) {
	// expectedErr 是仓储端模拟的持久化故障。
	expectedErr := errors.New("database unavailable")
	// service 是绑定故障仓储的发布规则应用服务。
	service := NewPublishRuleService(&publishRuleRepositoryFake{err: expectedErr})
	// err 保存应用服务返回的持久化错误。
	err := service.Ensure(context.Background(), RuleInput{UserID: 1})
	if !errors.Is(err, expectedErr) {
		t.Fatalf("应透传仓储错误，err=%v", err)
	}
}

// TestPublishRuleServiceRejectsMissingRepository 验证缺少发布规则持久化端口时返回参数错误。
func TestPublishRuleServiceRejectsMissingRepository(t *testing.T) {
	// err 保存缺少仓储时的应用服务错误。
	err := NewPublishRuleService(nil).Ensure(context.Background(), RuleInput{UserID: 1})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("缺少仓储应返回参数错误，err=%v", err)
	}
}
