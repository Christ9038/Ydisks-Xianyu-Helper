package items

import (
	"context"
	"errors"
	"testing"

	automationapp "xianyu-go/internal/application/automation"
)

// TestBatchLocalPublishEnsureAutomationRuleBranches 验证空配置、评价赠品配置、坏 JSON 和规则端口错误。
func TestBatchLocalPublishEnsureAutomationRuleBranches(t *testing.T) {
	// completionRepository 保存构造服务所需的批次完成依赖替身。
	completionRepository := &batchCompletionRepositoryFake{}
	// itemRepository 保存商品目录收口依赖替身。
	itemRepository := &batchPublishedItemRepositoryFake{}
	// ruleRepository 保存规则写入调用及可控错误。
	ruleRepository := &batchPublishRuleRepositoryFake{}
	// service 是待验证的批量发布本地收口服务。
	service, serviceErr := NewBatchLocalPublishService(completionRepository, itemRepository, ruleRepository)
	if serviceErr != nil {
		t.Fatal(serviceErr)
	}
	// result 保存平台返回的商品结果。
	result := &BatchPublishResult{ItemID: "item-1", Title: "商品"}
	// emptyErr 保存没有任何自动化配置时的兼容成功结果。
	emptyErr := service.EnsureAutomationRules(context.Background(), 1, BatchRow{CookieID: "cid", Title: "导入商品", AutomationJSON: `{}`}, result)
	if emptyErr != nil || len(ruleRepository.inputs) != 0 {
		t.Fatalf("empty automation err=%v inputs=%v", emptyErr, ruleRepository.inputs)
	}
	// giftRow 保存评价赠品自动化配置。
	giftRow := BatchRow{CookieID: "cid", Title: "导入商品", AutomationJSON: `{"review_gift":{"enabled":true,"actions":[{"card_id":7,"delivery_count":2,"delay_seconds":4}]}}`}
	// giftErr 保存评价赠品规则转换结果。
	giftErr := service.EnsureAutomationRules(context.Background(), 1, giftRow, result)
	if giftErr != nil || len(ruleRepository.inputs) != 1 || ruleRepository.inputs[0].TriggerType != automationapp.TriggerBuyerReviewed || ruleRepository.inputs[0].Actions[0].CardID != 7 {
		t.Fatalf("gift automation err=%v inputs=%v", giftErr, ruleRepository.inputs)
	}
	// malformedErr 保存坏自动化配置 JSON 的解析错误。
	malformedErr := service.EnsureAutomationRules(context.Background(), 1, BatchRow{AutomationJSON: "not-json"}, result)
	if malformedErr == nil {
		t.Fatal("malformed automation JSON should fail")
	}
	// missingResultErr 保存缺少平台商品标识时的输入错误。
	missingResultErr := service.EnsureAutomationRules(context.Background(), 1, BatchRow{AutomationJSON: `{}`}, &BatchPublishResult{})
	if missingResultErr == nil {
		t.Fatal("missing publish result should fail")
	}
	// ruleRepository.err 模拟规则持久化失败，验证错误原样传递。
	ruleRepository.err = errors.New("规则存储失败")
	// ruleWriteErr 保存规则持久化失败结果。
	ruleWriteErr := service.EnsureAutomationRules(context.Background(), 1, giftRow, result)
	if !errors.Is(ruleWriteErr, ruleRepository.err) {
		t.Fatalf("rule write error=%v", ruleWriteErr)
	}
}
