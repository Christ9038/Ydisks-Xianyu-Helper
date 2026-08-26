package automation

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestAdjustOrderPriceRetryCoversCancellationAndExhaustion 验证改价重试等待取消和达到重试上限的确定性分支。
func TestAdjustOrderPriceRetryCoversCancellationAndExhaustion(t *testing.T) {
	// previousGap 保存生产重试间隔，测试结束后恢复全局调度参数。
	previousGap := adjustPriceTransientRetryGap
	t.Cleanup(func() { adjustPriceTransientRetryGap = previousGap })
	// store、cleanup 保存改价重试测试数据库及其关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// canceledContext 是会在首次平台尝试后超时的上下文，用于验证重试等待不会阻塞任务关闭。
	canceledContext, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	// canceledCenter 保存返回暂时性平台繁忙结果的改价中心。
	canceledCenter := NewWithDependencies(store, nil, nil, CenterDependencies{MTop: &fakeMTop{
		adjustResults: []fakeAdjustPriceResult{{ret: []string{"FAIL_BIZ_CANNOT_MODIFY_FEE::暂无法修改价格，请稍后重试"}}},
	}})
	// canceledErr 保存取消重试等待后的错误。
	canceledErr := canceledCenter.actions.adjustOrderPriceWithRetry(canceledContext, Task{AccountID: "cid", OrderID: "cancel-order"}, 990)
	if canceledErr == nil || !strings.Contains(canceledErr.Error(), "重试等待被取消") {
		t.Fatalf("canceled retry err=%v", canceledErr)
	}
	// adjustPriceTransientRetryGap 缩短后续用例的等待，以便快速验证达到重试上限。
	adjustPriceTransientRetryGap = time.Millisecond
	// exhaustedResults 保存每次都返回暂时性繁忙的远端结果序列。
	exhaustedResults := make([]fakeAdjustPriceResult, adjustPriceTransientRetryLimit+1)
	// resultIndex 表示当前待填充的远端结果序号。
	for resultIndex := range exhaustedResults {
		exhaustedResults[resultIndex] = fakeAdjustPriceResult{ret: []string{"FAIL_BIZ_CANNOT_MODIFY_FEE::暂无法修改价格，请稍后重试"}}
	}
	// exhaustedCenter 保存用于验证重试次数收口的改价中心。
	exhaustedCenter := NewWithDependencies(store, nil, nil, CenterDependencies{MTop: &fakeMTop{adjustResults: exhaustedResults}})
	// exhaustedErr 保存达到重试上限后的错误。
	exhaustedErr := exhaustedCenter.actions.adjustOrderPriceWithRetry(context.Background(), Task{AccountID: "cid", OrderID: "exhausted-order"}, 990)
	if exhaustedErr == nil || !strings.Contains(exhaustedErr.Error(), "CANNOT_MODIFY_FEE") {
		t.Fatalf("exhausted retry err=%v", exhaustedErr)
	}
}
