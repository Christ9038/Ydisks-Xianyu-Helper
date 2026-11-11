package mtop

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// TestConsignSensitiveWordIsDefiniteRejection 验证确认发货命中平台敏感词时按“明确拒绝”返回 ok=false、err=nil，且不重试（#113）。
// 若误判为系统错误，上层会把确认发货当作结果不确定而永久禁止自动补做。
func TestConsignSensitiveWordIsDefiniteRejection(t *testing.T) {
	// requests 统计平台收到的请求数，必须恰好一次。
	var requests atomic.Int32
	// server 模拟平台以 HTTP 200 返回敏感词拒绝。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"ret":["HIT_SENSITIVE_WORD::命中敏感字符"]}`)
	}))
	defer server.Close()

	// client 是注入本地测试端点的 MTOP 客户端。
	client := &ClientImpl{HTTPClient: server.Client(), ConsignURL: server.URL + "/"}
	// ok、ret、err 是确认发货的业务结果、平台返回和调用错误。
	ok, ret, _, err := client.ConsignContextWithDelivery(context.Background(), consignCookies, "order-1", "网盘链接", nil)
	if err != nil || ok || len(ret) != 1 || ret[0] != "HIT_SENSITIVE_WORD::命中敏感字符" {
		t.Fatalf("敏感词拒绝应作为普通业务失败返回: ok=%v ret=%v err=%v", ok, ret, err)
	}
	if requests.Load() != 1 {
		t.Fatalf("明确拒绝不得重试: requests=%d", requests.Load())
	}
}

// TestIsMTopBusinessRetDefiniteRejectionCodes 覆盖确定拒绝码的前缀匹配、大小写与不误伤说明文字的边界。
func TestIsMTopBusinessRetDefiniteRejectionCodes(t *testing.T) {
	// cases 是平台返回与是否应判为业务结果。
	cases := []struct {
		// ret 是平台返回标记列表。
		ret []string
		// want 是期望的业务判定。
		want bool
	}{
		{ret: []string{"HIT_SENSITIVE_WORD::命中敏感字符"}, want: true},
		{ret: []string{"hit_sensitive_word::lower"}, want: true},
		{ret: []string{"HIT_SENSITIVE_WORD"}, want: true},
		{ret: []string{"FAIL_BIZ_ORDER_STATUS_ERROR::订单状态错误"}, want: true},
		{ret: []string{"FAIL_SYS_SERVICE_UNAVAILABLE::说明里提到 HIT_SENSITIVE_WORD"}, want: false},
		{ret: []string{"FAIL_SYS_UNKNOWN::未知"}, want: false},
		{ret: nil, want: false},
	}
	// tc 是当前用例。
	for _, tc := range cases {
		// got 是当前返回标记的实际业务判定。
		if got := isMTopBusinessRet(tc.ret); got != tc.want {
			t.Fatalf("isMTopBusinessRet(%v)=%v want %v", tc.ret, got, tc.want)
		}
	}
	// failure 验证统一失败分类同样把敏感词拒绝归为业务错误。
	failure := (&ClientImpl{}).mtopResponseFailure("确认发货接口", http.StatusOK, []string{"HIT_SENSITIVE_WORD::命中敏感字符"}, "平台 ret 未包含 SUCCESS")
	// kind、classified 是统一失败分类结果及其是否可识别。
	if kind, classified := MTopErrorKindOf(failure); !classified || kind != MTopErrorBusiness {
		t.Fatalf("敏感词拒绝分类错误: kind=%v classified=%v", kind, classified)
	}
}
