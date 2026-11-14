package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// TestReplyOrderContextSwitchContract 验证 AI 与 Agent 两个订单阶段开关的真实契约：
// 默认关闭；显式开启与关闭可持久化；旧客户端省略该字段保存时保留已保存的值。
func TestReplyOrderContextSwitchContract(t *testing.T) {
	// srv、store、cleanup 是被测 HTTP 服务、测试数据库聚合和资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是被测服务的完整路由树。
	handler := srv.Router()
	// session 是管理员登录后的认证会话 Cookie。
	session := loginHelper(t, handler)
	// err 保存插入测试账号的错误。
	if _, err := store.DB.ExecContext(context.Background(), `INSERT OR IGNORE INTO cookies (id,value,user_id) VALUES ('acc1','cookie-value',1)`); err != nil {
		t.Fatal(err)
	}
	// cases 列出两个设置接口的路径、不含开关的合法请求体前缀，以及列表接口路径（没有列表接口时为空）。
	cases := map[string]struct {
		path, base, list string
	}{
		"AI": {"/api/v1/settings/ai-reply/acc1",
			`"ai_enabled":true,"auto_adjust_price_enabled":false,"max_discount_percent":10,"max_discount_amount":50,"max_bargain_rounds":3,"custom_prompts":"","ai_mode":"full_service"`,
			"/api/v1/settings/ai-reply"},
		"Agent": {"/api/v1/agent-replies/acc1", `"enabled":true,"endpoint_url":"https://agent.example.com/v1"`, ""},
	}
	// name、tc 表示当前接口名称及其路径与请求体。
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// expect 读取当前设置并断言开关取值。
			expect := func(step string, want bool) {
				t.Helper()
				// got 保存读取设置的响应。
				got := greetingRequest(t, handler, session, http.MethodGet, tc.path, "")
				// fragment 是响应中必须出现的开关 JSON 片段。
				fragment := `"order_context_enabled":false`
				if want {
					fragment = `"order_context_enabled":true`
				}
				if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), fragment) {
					t.Fatalf("%s: status=%d body=%s", step, got.Code, got.Body.String())
				}
			}
			// save 保存设置；extra 是追加在合法前缀之后的 JSON 片段（可为空）。
			save := func(step, extra string) {
				t.Helper()
				// saved 保存写入设置的响应。
				saved := greetingRequest(t, handler, session, http.MethodPut, tc.path, `{`+tc.base+extra+`}`)
				if saved.Code != http.StatusOK {
					t.Fatalf("%s: status=%d body=%s", step, saved.Code, saved.Body.String())
				}
			}
			expect("未保存时默认关闭", false)
			save("旧客户端首次保存", "")
			expect("省略字段首次保存后仍关闭", false)
			save("显式开启", `,"order_context_enabled":true`)
			expect("显式开启后", true)
			save("旧客户端再次保存", "")
			expect("省略字段不得重置已开启的开关", true)
			if tc.list != "" {
				// listed 保存列表接口响应，列表与单条读取必须一致。
				if listed := greetingRequest(t, handler, session, http.MethodGet, tc.list, ""); listed.Code != http.StatusOK ||
					!strings.Contains(listed.Body.String(), `"order_context_enabled":true`) {
					t.Fatalf("列表应包含开关: status=%d body=%s", listed.Code, listed.Body.String())
				}
			}
			save("显式关闭", `,"order_context_enabled":false`)
			expect("显式关闭后", false)
		})
	}
}
