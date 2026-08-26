package server

import (
	"errors"
	"testing"

	orderapp "xianyu-go/internal/application/orders"
)

// TestOrderResponseConversionHelpers 覆盖订单兼容响应的指针字段、结果行转换和错误分类。
func TestOrderResponseConversionHelpers(t *testing.T) {
	// intValue、boolValue 保存零值指针转换结果。
	intValue := intPointer(0)
	// boolValue 保存零值布尔指针转换结果。
	boolValue := boolPointer(false)
	if intValue == nil || *intValue != 0 || boolValue == nil || *boolValue {
		t.Fatal("pointer conversion incorrect")
	}
	// results 保存不同字段组合的应用层订单刷新结果。
	results := refreshResultsFromApplication([]orderapp.RefreshOrderResult{
		{CookieID: "cid", Success: true, Discovered: 2, Updated: 1, SoftDeleted: 1, Stage: "list", Message: "ok", OldStatus: "待付款", NewStatus: "已付款"},
		{OrderID: "order", Success: false, Error: "failed"},
		{Success: false},
	})
	if len(results) != 3 || results[0].CookieID != "cid" || results[0].SoftDeleted == nil || !*results[0].SoftDeleted || results[1].OrderID != "order" || results[2].Success {
		t.Fatalf("results=%+v", results)
	}
	// cause 保存订单应用错误的底层原因。
	cause := errors.New("字段无效")
	// wrapped 保存带业务分类的订单错误。
	wrapped := &orderApplicationError{kind: orderErrorBadRequest, err: cause}
	if wrapped.Error() != cause.Error() || !errors.Is(wrapped, cause) || wrapped.Unwrap() != cause {
		t.Fatal("order error chain incorrect")
	}
	// kind、ok 保存订单错误分类读取结果。
	kind, ok := orderErrorKindOf(wrapped)
	if !ok || kind != orderErrorBadRequest {
		t.Fatalf("kind=%v ok=%v", kind, ok)
	}
	// missingKind、missingOK 保存普通错误的分类读取结果。
	missingKind, missingOK := orderErrorKindOf(errors.New("普通错误"))
	if missingOK || missingKind != 0 {
		t.Fatalf("missing kind=%v ok=%v", missingKind, missingOK)
	}
}

// TestPublishBatchErrorWrappers 覆盖批量发布错误包装的错误链语义。
func TestPublishBatchErrorWrappers(t *testing.T) {
	// cause 保存批量发布错误的底层原因。
	cause := errors.New("发布阶段失败")
	// postError 保存普通发布后错误包装结果。
	postError := &postPublishError{err: cause}
	if postError.Error() != cause.Error() || !errors.Is(postError, cause) || postError.Unwrap() != cause {
		t.Fatal("post publish error chain incorrect")
	}
	// uncertainError 保存远端结果不确定错误包装结果。
	uncertainError := &uncertainRemotePublishError{err: cause}
	if uncertainError.Error() != cause.Error() || !errors.Is(uncertainError, cause) || uncertainError.Unwrap() != cause {
		t.Fatal("uncertain publish error chain incorrect")
	}
}
