package items

import (
	"context"
	"errors"
	"testing"

	automationapp "xianyu-go/internal/application/automation"
)

// TestBatchLocalPublishCoversConstructionAndInputGuards 验证本地收口服务的依赖校验和输入门禁。
func TestBatchLocalPublishCoversConstructionAndInputGuards(t *testing.T) {
	// completionRepository 是批次完成端口替身。
	completionRepository := &batchCompletionRepositoryFake{}
	// itemRepository 是本地商品端口替身。
	itemRepository := &batchPublishedItemRepositoryFake{}
	// ruleRepository 是自动化规则端口替身。
	ruleRepository := &batchPublishRuleRepositoryFake{}
	// constructorCases 保存缺失依赖的构造结果。
	constructorCases := []struct {
		// name 是当前缺失依赖场景名称。
		name string
		// completion 是批次完成端口。
		completion BatchCompletionRepository
		// item 是本地商品端口。
		item BatchPublishedItemRepository
		// rule 是规则端口。
		rule automationapp.PublishRuleRepository
	}{
		{name: "completion", completion: nil, item: itemRepository, rule: ruleRepository},
		{name: "item", completion: completionRepository, item: nil, rule: ruleRepository},
		{name: "rule", completion: completionRepository, item: itemRepository, rule: nil},
	}
	// testCase 表示当前缺失依赖构造场景。
	for _, testCase := range constructorCases {
		t.Run(testCase.name, func(t *testing.T) {
			// _, err 保存构造失败结果。
			_, err := NewBatchLocalPublishService(testCase.completion, testCase.item, testCase.rule)
			if err == nil {
				t.Fatal("缺失依赖应构造失败")
			}
		})
	}
	// service 是完整依赖构造出的本地收口服务。
	service, err := NewBatchLocalPublishService(completionRepository, itemRepository, ruleRepository)
	if err != nil {
		t.Fatal(err)
	}
	// nilResultErr 保存缺失平台结果的收口错误。
	nilResultErr := service.Complete(context.Background(), 1, BatchRow{}, "worker", nil)
	if nilResultErr == nil {
		t.Fatal("缺失平台结果应失败")
	}
	// nilRuleResultErr 保存规则入口缺失平台结果的错误。
	nilRuleResultErr := service.EnsureAutomationRules(context.Background(), 1, BatchRow{}, nil)
	if nilRuleResultErr == nil {
		t.Fatal("规则入口缺失平台结果应失败")
	}
	// invalidService 是未装配本地收口依赖的服务。
	invalidService := &BatchLocalPublishService{}
	// invalidCompleteErr 保存未装配服务的收口错误。
	invalidCompleteErr := invalidService.Complete(context.Background(), 1, BatchRow{}, "worker", &BatchPublishResult{ItemID: "item"})
	if invalidCompleteErr == nil {
		t.Fatal("未装配服务收口应失败")
	}
	// invalidRuleErr 保存未装配规则端口时规则入口的错误。
	invalidRuleErr := invalidService.EnsureAutomationRules(context.Background(), 1, BatchRow{}, &BatchPublishResult{ItemID: "item"})
	if invalidRuleErr == nil {
		t.Fatal("未装配规则端口不应成功")
	}
}

// TestBatchLocalPublishCoversCompletionFailures 验证租约查询、规则写入和成功检查点失败分支。
func TestBatchLocalPublishCoversCompletionFailures(t *testing.T) {
	// baseRow 是所有本地收口失败场景共享的批次明细。
	baseRow := BatchRow{ID: 1, BatchID: "batch", CookieID: "cookie", Title: "title", AutomationJSON: `{}`}
	// result 是平台返回的最小成功结果。
	result := &BatchPublishResult{ItemID: "item", Title: "title"}
	// getError 是租约复核查询的底层错误。
	getError := errors.New("get batch failed")
	// getService 是租约复核查询失败场景的服务。
	getCompletion := &batchCompletionRepositoryFake{getErr: getError}
	// getService、err 保存租约复核场景的构造结果。
	getService, err := NewBatchLocalPublishService(getCompletion, &batchPublishedItemRepositoryFake{}, &batchPublishRuleRepositoryFake{})
	if err != nil {
		t.Fatal(err)
	}
	// getResultErr 保存租约复核查询错误。
	getResultErr := getService.Complete(context.Background(), 1, baseRow, "worker", result)
	if getResultErr == nil {
		t.Fatal("租约复核查询错误应返回后置错误")
	}
	// canceledCompletion 保存已取消批次的状态。
	canceledCompletion := &batchCompletionRepositoryFake{batch: BatchInfo{Status: "canceled", WorkerToken: "worker"}}
	// canceledService 是已取消批次场景的服务。
	canceledService, err := NewBatchLocalPublishService(canceledCompletion, &batchPublishedItemRepositoryFake{}, &batchPublishRuleRepositoryFake{})
	if err != nil {
		t.Fatal(err)
	}
	// canceledResultErr 保存已取消批次错误。
	canceledResultErr := canceledService.Complete(context.Background(), 1, baseRow, "worker", result)
	if !errors.Is(canceledResultErr, context.Canceled) {
		t.Fatalf("已取消批次错误=%v", canceledResultErr)
	}
	// ruleError 是自动化规则写入错误。
	ruleError := errors.New("rule write failed")
	// ruleRow 是包含评价赠品规则的批次明细。
	ruleRow := baseRow
	ruleRow.AutomationJSON = `{"review_gift":{"enabled":true,"actions":[{"card_id":1}]}}`
	// ruleService 是规则写入失败场景的服务。
	ruleService, err := NewBatchLocalPublishService(&batchCompletionRepositoryFake{batch: BatchInfo{Status: "running", WorkerToken: "worker"}}, &batchPublishedItemRepositoryFake{}, &batchPublishRuleRepositoryFake{err: ruleError})
	if err != nil {
		t.Fatal(err)
	}
	// ruleResultErr 保存规则写入后置错误。
	ruleResultErr := ruleService.Complete(context.Background(), 1, ruleRow, "worker", result)
	if !errors.Is(ruleResultErr, ruleError) {
		t.Fatalf("规则写入错误=%v", ruleResultErr)
	}
	// markError 是成功检查点写入错误。
	markError := errors.New("mark success failed")
	// markService 是成功检查点写入失败场景的服务。
	markService, err := NewBatchLocalPublishService(&batchCompletionRepositoryFake{batch: BatchInfo{Status: "running", WorkerToken: "worker"}, markErr: markError}, &batchPublishedItemRepositoryFake{}, &batchPublishRuleRepositoryFake{})
	if err != nil {
		t.Fatal(err)
	}
	// markResultErr 保存成功检查点写入错误。
	markResultErr := markService.Complete(context.Background(), 1, baseRow, "worker", result)
	if !errors.Is(markResultErr, markError) {
		t.Fatalf("成功检查点错误=%v", markResultErr)
	}
	// markFalse 保存成功检查点未匹配当前租约的结果。
	markFalse := false
	// leaseService 是成功检查点租约丢失场景的服务。
	leaseService, err := NewBatchLocalPublishService(&batchCompletionRepositoryFake{batch: BatchInfo{Status: "running", WorkerToken: "worker"}, markSuccess: &markFalse}, &batchPublishedItemRepositoryFake{}, &batchPublishRuleRepositoryFake{})
	if err != nil {
		t.Fatal(err)
	}
	// leaseResultErr 保存成功检查点租约丢失错误。
	leaseResultErr := leaseService.Complete(context.Background(), 1, baseRow, "worker", result)
	if !errors.Is(leaseResultErr, ErrBatchLeaseLost) {
		t.Fatalf("租约丢失错误=%v", leaseResultErr)
	}
}

// TestBatchLocalPublishCoversAllAutomationRuleKinds 验证付款发货、评价赠品和求评价规则全部转换分支。
func TestBatchLocalPublishCoversAllAutomationRuleKinds(t *testing.T) {
	// rules 保存三类规则写入结果。
	rules := &batchPublishRuleRepositoryFake{}
	// service 保存完整依赖的本地收口服务。
	service, serviceErr := NewBatchLocalPublishService(&batchCompletionRepositoryFake{}, &batchPublishedItemRepositoryFake{}, rules)
	if serviceErr != nil {
		t.Fatal(serviceErr)
	}
	// config 保存三类发布后自动化规则及导入标题回退配置。
	config := `{"paid_delivery":{"enabled":true,"actions":[{"card_id":3,"delivery_count":2,"delay_seconds":1}]},"review_gift":{"enabled":true,"actions":[{"card_id":4,"delivery_count":1,"delay_seconds":2}]},"review_request":{"enabled":true,"after_shipped_hours":12,"message":"请评价","max_attempts":2,"delay_seconds":3}}`
	// ensureErr 保存三类自动化规则的转换结果。
	ensureErr := service.EnsureAutomationRules(context.Background(), 1, BatchRow{CookieID: "cid", Title: "导入标题", AutomationJSON: config}, &BatchPublishResult{ItemID: "item"})
	if ensureErr != nil || len(rules.inputs) != 3 || rules.inputs[0].ItemID != "item" || rules.inputs[2].Actions[0].MessageTemplate != "请评价" {
		t.Fatalf("三类规则转换错误=%v inputs=%+v", ensureErr, rules.inputs)
	}
	// paidRuleError 保存付款发货规则写入错误。
	paidRuleError := errors.New("付款规则写入失败")
	// paidErrorRules 保存仅启用付款发货规则的错误仓储。
	paidErrorRules := &batchPublishRuleRepositoryFake{err: paidRuleError}
	// paidErrorService 保存付款规则写入失败场景的服务。
	paidErrorService, err := NewBatchLocalPublishService(&batchCompletionRepositoryFake{}, &batchPublishedItemRepositoryFake{}, paidErrorRules)
	if err != nil {
		t.Fatal(err)
	}
	// ensureErr 保存付款规则写入失败的校验结果。
	if ensureErr := paidErrorService.EnsureAutomationRules(context.Background(), 1, BatchRow{CookieID: "cid", AutomationJSON: `{"paid_delivery":{"enabled":true}}`}, &BatchPublishResult{ItemID: "item"}); !errors.Is(ensureErr, paidRuleError) {
		t.Fatalf("付款规则错误=%v", ensureErr)
	}
	// reviewRequestError 保存求评价规则写入错误。
	reviewRequestError := errors.New("求评价规则写入失败")
	// reviewRequestService 保存仅启用求评价规则的错误服务。
	reviewRequestService, err := NewBatchLocalPublishService(&batchCompletionRepositoryFake{}, &batchPublishedItemRepositoryFake{}, &batchPublishRuleRepositoryFake{err: reviewRequestError})
	if err != nil {
		t.Fatal(err)
	}
	// ensureErr 保存求评价规则写入失败的校验结果。
	if ensureErr := reviewRequestService.EnsureAutomationRules(context.Background(), 1, BatchRow{CookieID: "cid", AutomationJSON: `{"review_request":{"enabled":true,"after_shipped_hours":1,"message":"请评价","max_attempts":1}}`}, &BatchPublishResult{ItemID: "item"}); !errors.Is(ensureErr, reviewRequestError) {
		t.Fatalf("求评价规则错误=%v", ensureErr)
	}
}
