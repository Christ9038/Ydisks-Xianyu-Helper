package server

import (
	"context"
	"errors"
	"testing"

	orderapp "xianyu-go/internal/application/orders"
)

// serverOrderReconciliationRecorderFake 是 Server 订单运行时适配器测试使用的补偿 Port。
type serverOrderReconciliationRecorderFake struct {
	// recordID 保存测试补偿写入成功时返回的记录标识。
	recordID string
	// recordErr 保存测试补偿写入失败时返回的错误。
	recordErr error
	// orderID、cookieID、kind、message 保存最近一次收到的业务字段。
	orderID  string
	cookieID string
	kind     string
	message  string
}

// RecordReconciliation 记录 Server 适配器传入的补偿写入请求。
func (f *serverOrderReconciliationRecorderFake) RecordReconciliation(_ context.Context, orderID, cookieID, kind, message string) (string, error) {
	f.orderID, f.cookieID, f.kind, f.message = orderID, cookieID, kind, message
	return f.recordID, f.recordErr
}

// TestServerOrderRuntimeAdapterReconciliationPort 验证 Server 只通过订单应用 Port 转发补偿写入。
func TestServerOrderRuntimeAdapterReconciliationPort(t *testing.T) {
	// expectedErr 保存底层补偿写入失败时应原样返回的错误。
	expectedErr := errors.New("补偿数据库写入失败")
	// testCases 保存补偿 Port 成功、缺失和持久化失败三种 Server 边界场景。
	testCases := []struct {
		// name 是当前边界场景的测试名称。
		name string
		// recorder 是当前场景注入的补偿应用 Port。
		recorder *serverOrderReconciliationRecorderFake
		// wantID 是当前场景预期返回的补偿记录标识。
		wantID string
		// wantErr 是当前场景预期返回的错误。
		wantErr error
		// wantErrText 是无法共享错误实例时用于校验的错误文本。
		wantErrText string
	}{
		{name: "成功", recorder: &serverOrderReconciliationRecorderFake{recordID: "reconcile-1"}, wantID: "reconcile-1"},
		{name: "缺失依赖", wantErrText: "订单补偿存储未初始化"},
		{name: "持久化错误", recorder: &serverOrderReconciliationRecorderFake{recordErr: expectedErr}, wantErr: expectedErr},
	}
	// testCase 表示当前遍历的 Server 补偿边界场景。
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// recorder 是避免把空指针伪装成非空接口的补偿应用 Port。
			var recorder orderapp.ReconciliationRecorder
			if testCase.recorder != nil {
				recorder = testCase.recorder
			}
			// runtime 保存当前场景构造的 Server 订单运行时适配器。
			runtime := newServerOrderRuntime(nil, recorder)
			// recordID、recordErr 保存适配器转发后的补偿写入结果。
			recordID, recordErr := runtime.RecordOrderReconciliation(context.Background(), "order-1", "cookie-1", "manual_status_ship", "本地订单写入失败")
			// errMatches 表示当前返回错误是否符合场景预期；缺失依赖场景只校验错误文本。
			errMatches := recordErr == nil || testCase.wantErrText != ""
			if testCase.wantErr != nil {
				errMatches = errors.Is(recordErr, testCase.wantErr)
			}
			if testCase.wantErrText != "" {
				errMatches = recordErr != nil && recordErr.Error() == testCase.wantErrText
			}
			if recordID != testCase.wantID || !errMatches {
				t.Fatalf("补偿 Port 边界结果异常: id=%q err=%v wantID=%q wantErr=%v", recordID, recordErr, testCase.wantID, testCase.wantErr)
			}
			if testCase.recorder != nil && (testCase.recorder.orderID != "order-1" || testCase.recorder.cookieID != "cookie-1" || testCase.recorder.kind != "manual_status_ship" || testCase.recorder.message != "本地订单写入失败") {
				t.Fatalf("补偿字段未完整转发: %+v", testCase.recorder)
			}
		})
	}
}
