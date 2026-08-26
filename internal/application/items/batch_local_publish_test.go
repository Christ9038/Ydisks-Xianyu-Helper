package items

import (
	"context"
	"errors"
	"testing"

	automationapp "xianyu-go/internal/application/automation"
)

// batchCompletionRepositoryFake 保存批次收口测试所需的状态和调用记录。
type batchCompletionRepositoryFake struct {
	// batch 保存当前用户可见的批次状态。
	batch BatchInfo
	// marked 保存成功检查点写入次数。
	marked int
	// markErr 模拟成功检查点写入失败。
	markErr error
	// getErr 模拟批次租约复核查询失败。
	getErr error
	// markSuccess 控制成功检查点是否匹配到当前租约；为空时默认成功。
	markSuccess *bool
}

// GetBatch 返回测试预置的批次状态。
func (repository *batchCompletionRepositoryFake) GetBatch(context.Context, int64, string) (BatchInfo, error) {
	if repository.getErr != nil {
		return BatchInfo{}, repository.getErr
	}
	return repository.batch, nil
}

// MarkClaimedRowSuccess 记录成功检查点写入并返回预置错误。
func (repository *batchCompletionRepositoryFake) MarkClaimedRowSuccess(_ context.Context, _ int64, _ string, _, _, _ string) (bool, error) {
	repository.marked++
	if repository.markErr != nil {
		return false, repository.markErr
	}
	if repository.markSuccess != nil {
		return *repository.markSuccess, nil
	}
	return true, nil
}

// batchPublishedItemRepositoryFake 保存本地商品写入测试结果。
type batchPublishedItemRepositoryFake struct {
	// item 保存最近一次写入的商品模型。
	item BatchPublishedItem
	// err 模拟本地商品写入失败。
	err error
}

// UpsertPublishedItem 记录商品写入并返回预置错误。
func (repository *batchPublishedItemRepositoryFake) UpsertPublishedItem(_ context.Context, item BatchPublishedItem) error {
	repository.item = item
	return repository.err
}

// batchPublishRuleRepositoryFake 保存自动化规则写入测试结果。
type batchPublishRuleRepositoryFake struct {
	// inputs 保存幂等规则写入请求。
	inputs []automationapp.RuleInput
	// err 模拟规则写入失败。
	err error
}

// EnsurePublishRule 记录规则请求并返回预置错误。
func (repository *batchPublishRuleRepositoryFake) EnsurePublishRule(_ context.Context, input automationapp.RuleInput) error {
	repository.inputs = append(repository.inputs, input)
	return repository.err
}

// TestBatchLocalPublishServiceCompletesLocalState 验证远端成功后的本地商品、规则和检查点收口顺序。
func TestBatchLocalPublishServiceCompletesLocalState(t *testing.T) {
	// completionRepository 保存租约状态和成功检查点记录。
	completionRepository := &batchCompletionRepositoryFake{batch: BatchInfo{Status: "running", WorkerToken: "worker"}}
	// itemRepository 保存本地商品写入结果。
	itemRepository := &batchPublishedItemRepositoryFake{}
	// ruleRepository 保存自动化规则写入结果。
	ruleRepository := &batchPublishRuleRepositoryFake{}
	// service 是待验证的批量本地收口服务。
	service, err := NewBatchLocalPublishService(completionRepository, itemRepository, ruleRepository)
	if err != nil {
		t.Fatal(err)
	}
	// row 保存导入的商品和自动化配置。
	row := BatchRow{ID: 7, BatchID: "batch-1", CookieID: "cookie-1", Title: "导入标题", Description: "商品描述", Quantity: 2, AutomationJSON: `{"paid_delivery":{"enabled":true,"actions":[{"card_id":11,"delivery_count":2,"delay_seconds":3}]},"review_request":{"enabled":true,"after_shipped_hours":24,"message":"请评价","max_attempts":2,"delay_seconds":5}}`}
	// result 保存平台返回的非敏感商品结果。
	result := &BatchPublishResult{ItemID: "item-1", ItemURL: "https://example/item-1", Title: "平台标题", PriceText: "12.00", CategoryID: "cat-1", RawData: map[string]any{"ok": true}}
	// completeErr 保存本地收口结果。
	completeErr := service.Complete(context.Background(), 9, row, "worker", result)
	if completeErr != nil {
		t.Fatalf("本地收口失败: %v", completeErr)
	}
	if completionRepository.marked != 1 || itemRepository.item.ItemID != "item-1" || !itemRepository.item.MultiQuantityDelivery {
		t.Fatalf("本地商品或检查点状态异常: marked=%d item=%+v", completionRepository.marked, itemRepository.item)
	}
	if len(ruleRepository.inputs) != 2 || ruleRepository.inputs[0].Actions[1].ActionType != automationapp.ActionConfirmShipment || ruleRepository.inputs[1].TriggerType != automationapp.TriggerReviewMissingTimeout {
		t.Fatalf("自动化规则转换异常: %+v", ruleRepository.inputs)
	}
}

// TestBatchLocalPublishServiceRejectsLeaseLoss 验证租约丢失时不写入本地商品或成功检查点。
func TestBatchLocalPublishServiceRejectsLeaseLoss(t *testing.T) {
	// completionRepository 返回已转移到其他 worker 的批次租约。
	completionRepository := &batchCompletionRepositoryFake{batch: BatchInfo{Status: "running", WorkerToken: "other"}}
	// itemRepository 保存本地商品写入调用次数的替身。
	itemRepository := &batchPublishedItemRepositoryFake{}
	// ruleRepository 保存规则写入调用次数的替身。
	ruleRepository := &batchPublishRuleRepositoryFake{}
	// service 是待验证的批量本地收口服务。
	service, err := NewBatchLocalPublishService(completionRepository, itemRepository, ruleRepository)
	if err != nil {
		t.Fatal(err)
	}
	// completeErr 保存租约丢失后的错误。
	completeErr := service.Complete(context.Background(), 9, BatchRow{BatchID: "batch-1", AutomationJSON: `{}`}, "worker", &BatchPublishResult{ItemID: "item-1"})
	if !errors.Is(completeErr, context.Canceled) || itemRepository.item.ItemID != "" || completionRepository.marked != 0 {
		t.Fatalf("租约丢失处理异常: err=%v item=%+v marked=%d", completeErr, itemRepository.item, completionRepository.marked)
	}
}

// TestBatchLocalPublishServiceClassifiesPostWriteFailure 验证商品或规则写入失败会标记为不可自动重试的后置错误。
func TestBatchLocalPublishServiceClassifiesPostWriteFailure(t *testing.T) {
	// completionRepository 保存可用的当前 worker 租约。
	completionRepository := &batchCompletionRepositoryFake{batch: BatchInfo{Status: "running", WorkerToken: "worker"}}
	// itemRepository 模拟数据库商品写入故障。
	itemRepository := &batchPublishedItemRepositoryFake{err: errors.New("商品数据库不可用")}
	// ruleRepository 保存未被调用的规则写入替身。
	ruleRepository := &batchPublishRuleRepositoryFake{}
	// service 是待验证的批量本地收口服务。
	service, err := NewBatchLocalPublishService(completionRepository, itemRepository, ruleRepository)
	if err != nil {
		t.Fatal(err)
	}
	// completeErr 保存本地写入失败后的错误。
	completeErr := service.Complete(context.Background(), 9, BatchRow{BatchID: "batch-1", AutomationJSON: `{}`}, "worker", &BatchPublishResult{ItemID: "item-1"})
	// postErr 保存后置本地收口错误的类型化视图。
	var postErr *PostPublishError
	if !errors.As(completeErr, &postErr) || completionRepository.marked != 0 || len(ruleRepository.inputs) != 0 {
		t.Fatalf("后置错误分类异常: err=%v marked=%d rules=%d", completeErr, completionRepository.marked, len(ruleRepository.inputs))
	}
}
