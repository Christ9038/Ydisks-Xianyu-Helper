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

func TestDetectItemMultiSpec(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api") != "mtop.taobao.idle.pc.detail" {
			t.Errorf("query=%s", r.URL.RawQuery)
		}
		rawBody, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(rawBody))
		if form.Get("data") != `{"itemId":"item-1"}` {
			t.Errorf("data=%q", form.Get("data"))
		}
		_, _ = io.WriteString(w, `{"ret":["SUCCESS::调用成功"],"data":{"skuDO":{"skuProperties":[{"name":"颜色"}],"skuList":[{"id":"1"},{"id":"2"}]}}}`)
	}))
	defer server.Close()
	client := &ClientImpl{HTTPClient: server.Client(), ItemDetailURL: server.URL}
	got, err := client.DetectItemMultiSpec(context.Background(), "_m_h5_tk=token_1", "item-1")
	if err != nil || !got {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

func TestDetectItemMultiSpecSignals(t *testing.T) {
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
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := detectItemMultiSpec(tc.data); got != tc.want {
				t.Fatalf("got=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestDetectItemMultiSpecRejectsMissingToken(t *testing.T) {
	client := &ClientImpl{}
	if _, err := client.DetectItemMultiSpec(context.Background(), "unb=1", "item-1"); err == nil || !strings.Contains(err.Error(), "_m_h5_tk") {
		t.Fatalf("err=%v", err)
	}
}
