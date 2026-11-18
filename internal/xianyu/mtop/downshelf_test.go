package mtop

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestDownShelfItemUsesCapturedPCRequest 验证商品下架调用使用已记录的 PC MTOP API、版本和 itemId 参数。
func TestDownShelfItemUsesCapturedPCRequest(t *testing.T) {
	// requestQuery、requestData、requestReferer 保存测试服务收到的请求证据。
	var requestQuery url.Values
	// requestData 保存 MTOP 表单 data 字段解析后的业务参数。
	var requestData map[string]any
	// requestReferer 保存本次平台请求携带的商品详情页来源地址。
	var requestReferer string
	// server 是只返回成功响应的本地 MTOP 测试服务。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestQuery = r.URL.Query()
		requestReferer = r.Header.Get("Referer")
		// body 保存 MTOP 表单请求体。
		// body、err 保存本地服务收到的原始 MTOP 表单数据和读取错误。
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		// form 保存请求体中的 MTOP 表单字段。
		// form、err 保存解码后的 MTOP 表单字段及解析错误。
		form, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatal(err)
		}
		// err 是把 MTOP data JSON 解码为测试请求参数时的格式错误。
		if err := json.Unmarshal([]byte(form.Get("data")), &requestData); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":["SUCCESS::调用成功"],"data":{"success":true}}`))
	}))
	defer server.Close()
	// client 是将商品下架端点指向本地测试服务的 MTOP 客户端。
	client := &ClientImpl{HTTPClient: server.Client(), DownShelfItemURL: server.URL}
	// result、err 保存下架调用结果。
	result, err := client.DownShelfItem(context.Background(), "unb=1; _m_h5_tk=token_abc; cookie2=c2;", "item-123")
	if err != nil || result == nil || !result.Success || result.UpdatedCookies == "" && result.Message == "" {
		t.Fatalf("down-shelf result=%+v err=%v", result, err)
	}
	if requestQuery.Get("api") != "mtop.taobao.idle.item.downshelf" || requestQuery.Get("v") != "2.0" {
		t.Fatalf("unexpected MTOP operation: %v", requestQuery)
	}
	if requestQuery.Get("spm_cnt") != "a21ybx.item.0.0" || requestQuery.Get("spm_pre") != "a21ybx.item.0.0" || requestQuery.Get("log_id") != "xianyu_item_downshelf" {
		t.Fatalf("missing captured PC request metadata: %v", requestQuery)
	}
	if requestData["itemId"] != "item-123" {
		t.Fatalf("unexpected MTOP data: %+v", requestData)
	}
	if !strings.Contains(requestReferer, "/item?id=item-123") {
		t.Fatalf("unexpected referer: %q", requestReferer)
	}
}

// TestDownShelfItemRejectsEmptyIDAndPropagatesRisk 验证空商品 ID 不发请求，平台风控响应不被伪装成成功。
func TestDownShelfItemRejectsEmptyIDAndPropagatesRisk(t *testing.T) {
	// calls 记录本地测试服务收到的请求数量。
	calls := 0
	// server 是返回风控验证错误的本地 MTOP 测试服务。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":["FAIL_SYS_USER_VALIDATE::请完成安全验证"],"data":{}}`))
	}))
	defer server.Close()
	// client 是指向本地风控响应服务的 MTOP 客户端。
	client := &ClientImpl{HTTPClient: server.Client(), DownShelfItemURL: server.URL}
	// err 是空商品 ID 被下架客户端拒绝时的参数错误。
	if _, err := client.DownShelfItem(context.Background(), "unb=1; _m_h5_tk=token_abc;", " "); err == nil || calls != 0 {
		t.Fatalf("empty item ID should not call platform: calls=%d err=%v", calls, err)
	}
	// err 是本地服务返回风控响应后由 MTOP 客户端保留的风险错误。
	if _, err := client.DownShelfItem(context.Background(), "unb=1; _m_h5_tk=token_abc;", "item-123"); err == nil || !IsRiskVerificationErr(err) {
		t.Fatalf("risk response should remain a risk error: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected exactly one platform request, got %d", calls)
	}
}

// TestDownShelfItemRequiresExplicitPlatformSuccess 验证 ret 成功但 data.success 缺失或 false 时不能报告商品已下架。
func TestDownShelfItemRequiresExplicitPlatformSuccess(t *testing.T) {
	// cases 保存平台未明确确认下架的响应体。
	cases := []string{
		`{"ret":["SUCCESS::调用成功"],"data":{}}`,
		`{"ret":["SUCCESS::调用成功"],"data":{"success":false}}`,
	}
	// body 是当前需要验证不能被误判为下架成功的平台响应体。
	for _, body := range cases {
		// server 是返回当前待验证平台响应的本地 MTOP 服务。
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		}))
		// client 是指向当前响应服务的商品下架 MTOP 客户端。
		client := &ClientImpl{HTTPClient: server.Client(), DownShelfItemURL: server.URL}
		// err 是平台风控响应被 MTOP 客户端保留时的风险错误。
		// err 是平台未明确返回 data.success=true 时的下架确认错误。
		if _, err := client.DownShelfItem(context.Background(), "unb=1; _m_h5_tk=token_abc;", "item-123"); err == nil || !strings.Contains(err.Error(), "未明确确认成功") {
			server.Close()
			t.Fatalf("unconfirmed response must not be reported as unlisted: body=%s err=%v", body, err)
		}
		server.Close()
	}
}
