package mtop

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestDetectItemMultiSpec 负责TestDetect商品MultiSpec相关处理。
func TestDetectItemMultiSpec(t *testing.T) {
	// server 保存server，供当前处理流程使用
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api") != "mtop.taobao.idle.pc.detail" {
			t.Errorf("query=%s", r.URL.RawQuery)
		}
		// rawBody 保存原始请求体，供当前处理流程使用
		rawBody, _ := io.ReadAll(r.Body)
		// form 保存表单，供当前处理流程使用
		form, _ := url.ParseQuery(string(rawBody))
		if form.Get("data") != `{"itemId":"item-1"}` {
			t.Errorf("data=%q", form.Get("data"))
		}
		_, _ = io.WriteString(w, `{"ret":["SUCCESS::调用成功"],"data":{"skuDO":{"skuProperties":[{"name":"颜色"}],"skuList":[{"id":"1"},{"id":"2"}]}}}`)
	}))
	defer server.Close()
	// client 保存client，供当前处理流程使用
	client := &ClientImpl{HTTPClient: server.Client(), ItemDetailURL: server.URL}
	// got、err 保存got、err，供当前处理流程使用
	got, err := client.DetectItemMultiSpec(context.Background(), "_m_h5_tk=token_1", "item-1")
	if err != nil || !got {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

// TestDetectItemMultiSpecSignals 负责TestDetect商品MultiSpecSignals相关处理。
func TestDetectItemMultiSpecSignals(t *testing.T) {
	// cases 保存cases，供当前处理流程使用
	cases := []struct {
		name string
		data any
		want bool
	}{
		{name: "explicit", data: map[string]any{"multiSKU": true}, want: true},
		{name: "two skus", data: map[string]any{"skuList": []any{1, 2}}, want: true},
		{name: "sku props", data: map[string]any{"skuDO": map[string]any{"props": []any{"颜色"}}}, want: true},
		{name: "single", data: map[string]any{"multiSKU": false, "skuList": []any{1}}, want: false},
	}
	// tc 表示当前遍历过程中的tc
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if // got 保存got，供当前处理流程使用
			got := detectItemMultiSpec(tc.data); got != tc.want {
				t.Fatalf("got=%v want=%v", got, tc.want)
			}
		})
	}
}

// TestDetectItemMultiSpecRejectsMissingToken 负责TestDetect商品MultiSpecRejectsMissing令牌相关处理。
func TestDetectItemMultiSpecRejectsMissingToken(t *testing.T) {
	// client 保存client，供当前处理流程使用
	client := &ClientImpl{}
	if // err 保存err，供当前处理流程使用
	_, err := client.DetectItemMultiSpec(context.Background(), "unb=1", "item-1"); err == nil || !strings.Contains(err.Error(), "_m_h5_tk") {
		t.Fatalf("err=%v", err)
	}
}
