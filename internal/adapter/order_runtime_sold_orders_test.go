package adapter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
)

// soldRuntimePageFake 在基础替身上注入分页行为，避免任何真实平台调用。
type soldRuntimePageFake struct {
	// orderRuntimeMTopFake 提供测试不使用的基础客户端方法。
	*orderRuntimeMTopFake
	// fetch 接收请求上下文、扁平凭证、页码和页大小，返回预置页及错误。
	fetch func(context.Context, string, int, int) (*mtop.SoldOrdersPage, error)
}

// FetchSoldOrdersPage 让 f 转发 ctx、cookies、pageNumber、pageSize 到本地替身并保留结果与错误。
func (f *soldRuntimePageFake) FetchSoldOrdersPage(ctx context.Context, cookies string, pageNumber, pageSize int) (*mtop.SoldOrdersPage, error) {
	return f.fetch(ctx, cookies, pageNumber, pageSize)
}

// TestOrderRuntimeSoldPaginationCompleteness 用 t 验证分页必须明确结束，失败保留此前订单及 Cookie 更新。
func TestOrderRuntimeSoldPaginationCompleteness(t *testing.T) {
	// cases 包含成功边界、上限、空页、空结果和中途平台错误。
	cases := []struct {
		// name 标识当前分页场景。
		name string
		// pages 是预期请求次数；最后一次响应由场景决定。
		pages int
		// terminal 指定末次响应形状，普通页包含一条订单且继续分页。
		terminal string
		// wantCount 指定完成前保留下来的订单数。
		wantCount int
		// wantError 指定错误分类，空值要求正常完成。
		wantError string
	}{
		{name: "complete twelve pages", pages: 12, wantCount: 12},
		{name: "complete hundredth page", pages: 100, wantCount: 100},
		{name: "limit still has next", pages: 100, terminal: "next", wantCount: 100, wantError: "100"},
		{name: "empty first page", pages: 1, terminal: "empty"},
		{name: "empty terminal page", pages: 2, terminal: "empty", wantCount: 1},
		{name: "empty first has next", pages: 1, terminal: "empty-next", wantError: "空页"},
		{name: "empty later has next", pages: 2, terminal: "empty-next", wantCount: 1, wantError: "空页"},
		{name: "nil later page", pages: 2, terminal: "nil", wantCount: 1, wantError: "未返回"},
		{name: "failure later page", pages: 2, terminal: "error", wantCount: 1, wantError: "平台失败"},
		{name: "missing order identity", pages: 2, terminal: "invalid-order", wantCount: 1, wantError: "订单号"},
	}
	// testCase 是当前分页场景，状态仅由同步请求回调拥有。
	for _, testCase := range cases {
		// t 接收当前场景的断言结果。
		t.Run(testCase.name, func(t *testing.T) {
			// calls 统计请求次数，用于防止越过上限或过早结束。
			calls := 0
			// expectedErr 是平台失败场景应原样传播的错误身份。
			expectedErr := errors.New("平台失败")
			// fake 的 fetch 回调使用 ctx 更新会话、核对 pageNumber/pageSize，忽略扁平凭证参数。
			fake := &soldRuntimePageFake{orderRuntimeMTopFake: &orderRuntimeMTopFake{}, fetch: func(ctx context.Context, _ string, pageNumber, pageSize int) (*mtop.SoldOrdersPage, error) {
				calls++
				if pageNumber != calls || pageSize != 30 || calls > testCase.pages {
					t.Fatal("分页请求顺序、页大小或停止条件错误")
				}
				mtop.CookieSessionFromContext(ctx).ReplaceSnapshot([]cookierefresh.BrowserCookie{{Name: "sid", Value: fmt.Sprintf("rotated-%d", calls), Domain: ".goofish.com", Path: "/"}})
				if calls == testCase.pages {
					switch testCase.terminal {
					case "empty":
						return &mtop.SoldOrdersPage{}, nil
					case "empty-next":
						return &mtop.SoldOrdersPage{NextPage: true}, nil
					case "nil":
						return nil, nil
					case "error":
						return nil, expectedErr
					case "invalid-order":
						return &mtop.SoldOrdersPage{Items: []mtop.SoldOrder{{OrderID: " "}}}, nil
					}
				}
				return &mtop.SoldOrdersPage{NextPage: calls < testCase.pages || testCase.terminal == "next", Items: []mtop.SoldOrder{{OrderID: fmt.Sprintf("order-%d", calls), BuyerID: "buyer-safe", ItemID: "item-safe"}}}, nil
			}}
			// runtime 的 Client 回调为本次同步返回相同替身。
			runtime := NewOrderRuntime(nil, OrderRuntimeHooks{Client: func() mtop.Client { return fake }}, nil, nil)
			// result、err 保存分页聚合结果与完整性错误，不输出 Cookie 内容。
			result, err := runtime.FetchSoldOrders(context.Background(), &orderapp.PlatformRuntimeData{Value: "sid=initial"})
			if (testCase.wantError == "" && err != nil) || (testCase.wantError != "" && (err == nil || !strings.Contains(err.Error(), testCase.wantError))) {
				t.Fatal("分页完成状态未匹配预期")
			}
			if calls != testCase.pages || len(result.Orders) != testCase.wantCount {
				t.Fatalf("请求次数=%d，保留订单=%d", calls, len(result.Orders))
			}
			if testCase.terminal == "error" && !errors.Is(err, expectedErr) {
				t.Fatal("平台错误身份丢失")
			}
			if !result.CookieUpdate.Handled || !result.CookieUpdate.Changed || !strings.Contains(result.CookieUpdate.Value, fmt.Sprintf("sid=rotated-%d", calls)) {
				t.Fatal("最后响应的 Cookie 更新丢失")
			}
			if len(result.Orders) > 0 && (result.Orders[0].OrderID != "order-1" || result.Orders[0].BuyerID != "buyer-safe" || result.Orders[0].ItemID != "item-safe") {
				t.Fatal("订单、买家或商品身份映射发生变化")
			}
		})
	}
}

// TestOrderRuntimeSoldRequestIdentity 用 t 验证成功结果只声明实际请求的 unb，且分页切换身份不能得到成功快照。
func TestOrderRuntimeSoldRequestIdentity(t *testing.T) {
	// cases 覆盖扁平与权威 Jar 的来源、URL 作用域和身份变化。
	cases := []struct {
		// name 标识当前身份场景。
		name string
		// flat 是仅供请求的合成扁平 Cookie，不在断言失败时输出。
		flat string
		// snapshot 是权威 Jar；nil 表示沿用扁平凭证。
		snapshot []cookierefresh.BrowserCookie
		// rotateTo 是首个响应写入的新 UID，空值表示不改变身份。
		rotateTo string
		// nextPage 表示首个响应后是否继续请求。
		nextPage bool
		// wantID 是所有已完成请求共同使用的非敏感 UID，无法证明时为空。
		wantID string
		// wantCalls 是冲突发生前允许执行的请求次数。
		wantCalls int
		// wantError 指定身份冲突是否必须阻断同步。
		wantError bool
	}{
		{name: "flat uid", flat: "unb=101; sid=fixture", wantID: "101", wantCalls: 1},
		{name: "missing uid", flat: "sid=fixture", wantCalls: 1},
		{name: "blank uid", flat: "unb= ; unb; sid=fixture", wantCalls: 1},
		{name: "snapshot uid only", snapshot: []cookierefresh.BrowserCookie{{Name: "unb", Value: "202", Domain: ".goofish.com", Path: "/"}}, wantID: "202", wantCalls: 1},
		{name: "matching uid", flat: "unb=101", snapshot: []cookierefresh.BrowserCookie{{Name: "unb", Value: "101", Domain: ".goofish.com", Path: "/"}}, wantID: "101", wantCalls: 1},
		{name: "conflicting uid", flat: "unb=101", snapshot: []cookierefresh.BrowserCookie{{Name: "unb", Value: "202", Domain: ".goofish.com", Path: "/"}}, wantError: true},
		{name: "empty authoritative jar", flat: "unb=101", snapshot: []cookierefresh.BrowserCookie{}, wantCalls: 1},
		{name: "im only uid not sent", flat: "unb=101", snapshot: []cookierefresh.BrowserCookie{{Name: "unb", Value: "101", Domain: ".goofish.com", Path: "/im"}}, wantCalls: 1},
		{name: "different domain uid not sent", flat: "unb=101", snapshot: []cookierefresh.BrowserCookie{{Name: "unb", Value: "202", Domain: "seller.goofish.com", Path: "/"}}, wantCalls: 1},
		{name: "http only request uid", snapshot: []cookierefresh.BrowserCookie{{Name: "unb", Value: "202", Domain: ".goofish.com", Path: "/h5", HTTPOnly: true}}, wantID: "202", wantCalls: 1},
		{name: "duplicate conflicting flat uid", flat: "unb=101; unb=202", wantError: true},
		{name: "empty duplicate uid is ambiguous", flat: "unb=; unb=101", wantError: true},
		{name: "ambiguous fallback with valid jar", flat: "unb=101; unb=202", snapshot: []cookierefresh.BrowserCookie{{Name: "unb", Value: "101", Domain: ".goofish.com", Path: "/"}}, wantError: true},
		{name: "conflicting request jar uid", snapshot: []cookierefresh.BrowserCookie{{Name: "unb", Value: "101", Domain: ".goofish.com", Path: "/"}, {Name: "unb", Value: "202", Domain: ".goofish.com", Path: "/h5"}}, wantError: true},
		{name: "duplicate matching uid", flat: "unb=101; unb=101", wantID: "101", wantCalls: 1},
		{name: "response uid is not request uid", flat: "unb=101", rotateTo: "202", wantID: "101", wantCalls: 1},
		{name: "later request conflicts with flat", flat: "unb=101", rotateTo: "202", nextPage: true, wantCalls: 1, wantError: true},
		{name: "later jar only request changes identity", snapshot: []cookierefresh.BrowserCookie{{Name: "unb", Value: "101", Domain: ".goofish.com", Path: "/"}}, rotateTo: "202", nextPage: true, wantCalls: 1, wantError: true},
		{name: "missing first identity remains unknown", rotateTo: "202", nextPage: true, wantCalls: 2},
		{name: "missing later identity becomes unknown", flat: "unb=101", rotateTo: " ", nextPage: true, wantCalls: 2},
	}
	// testCase 是当前身份场景，子测试独占请求会话。
	for _, testCase := range cases {
		// t 接收身份来源和冲突保护的断言结果。
		t.Run(testCase.name, func(t *testing.T) {
			// calls 统计实际发出的订单请求。
			calls := 0
			// fake 的回调用 ctx 模拟响应 Cookie 更新；其余请求参数不参与此身份测试。
			fake := &soldRuntimePageFake{orderRuntimeMTopFake: &orderRuntimeMTopFake{}, fetch: func(ctx context.Context, _ string, _, _ int) (*mtop.SoldOrdersPage, error) {
				calls++
				if calls > 2 {
					t.Fatal("身份测试出现多余分页请求")
				}
				if testCase.rotateTo != "" {
					mtop.CookieSessionFromContext(ctx).ReplaceSnapshot([]cookierefresh.BrowserCookie{{Name: "unb", Value: testCase.rotateTo, Domain: ".goofish.com", Path: "/"}})
				}
				return &mtop.SoldOrdersPage{NextPage: testCase.nextPage && calls == 1, Items: []mtop.SoldOrder{{OrderID: fmt.Sprintf("order-%d", calls)}}}, nil
			}}
			// detail 是当前账号请求视图，完整 Jar 存在时必须覆盖扁平凭证。
			detail := &orderapp.PlatformRuntimeData{Value: testCase.flat}
			if testCase.snapshot != nil {
				detail.MetadataJSON = cookierefresh.MetadataWithSnapshot("", testCase.snapshot)
			}
			// runtime 的 Client 回调固定返回本地分页替身。
			runtime := NewOrderRuntime(nil, OrderRuntimeHooks{Client: func() mtop.Client { return fake }}, nil, nil)
			// result、err 保存实际请求身份及冲突诊断，错误不得包含 UID 或凭证。
			result, err := runtime.FetchSoldOrders(context.Background(), detail)
			if (err != nil) != testCase.wantError || calls != testCase.wantCalls || result.SellerID != testCase.wantID {
				t.Fatalf("身份完成状态异常: 有错误=%t，请求次数=%d，身份符合=%t", err != nil, calls, result.SellerID == testCase.wantID)
			}
			if err != nil && (!strings.Contains(err.Error(), "身份") || strings.Contains(err.Error(), "101") || strings.Contains(err.Error(), "202")) {
				t.Fatal("身份冲突错误未正确脱敏")
			}
			if testCase.rotateTo != "" && calls > 0 && !result.CookieUpdate.Changed {
				t.Fatal("响应 Cookie 更新被身份校验丢弃")
			}
		})
	}
}

// TestOrderRuntimeSoldIdentityMatchesHTTP 用 t 验证真实 MTOP 客户端的请求 Header 与成功返回的 SellerID 一致。
func TestOrderRuntimeSoldIdentityMatchesHTTP(t *testing.T) {
	// calls 由服务请求回调记录，FetchSoldOrders 返回后才读取。
	calls := 0
	// server 的回调检查 r 实际携带的身份，通过 w 返回合法空列表及响应 Cookie 更新。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		// uid、cookieErr 是请求实际发送的非敏感 UID 及提取错误，不输出 Cookie Header。
		uid, cookieErr := r.Cookie("unb")
		if cookieErr != nil || uid.Value != "202" {
			t.Error("实际请求没有使用指定路径下的 Jar 身份")
		}
		w.Header().Add("Set-Cookie", "sid=rotated; Path=/")
		_, _ = io.WriteString(w, `{"ret":["SUCCESS::调用成功"],"data":{"module":{"items":[],"nextPage":false}}}`)
	}))
	defer server.Close()
	// endpoint、parseErr 解析本地端点以为完整 Jar 指定精确请求域名。
	endpoint, parseErr := url.Parse(server.URL)
	if parseErr != nil {
		t.Fatal("本地测试端点无法解析")
	}
	// snapshot 分离卖家页面签名令牌、实际请求 UID 以及 /im 诱饵 UID；均为合成值。
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "_m_h5_tk", Value: "fixture_1", Domain: ".goofish.com", Path: "/"},
		{Name: "unb", Value: "202", Domain: endpoint.Hostname(), Path: "/sold", HTTPOnly: true},
		{Name: "unb", Value: "303", Domain: ".goofish.com", Path: "/im"},
	}
	// client 使用自定义端点，身份检查必须采用与它一致的 URL。
	client := &mtop.ClientImpl{HTTPClient: server.Client(), SoldOrdersURL: server.URL + "/sold"}
	// runtime 的 Client 回调为本次同步返回固定的真实 MTOP 客户端。
	runtime := NewOrderRuntime(nil, OrderRuntimeHooks{Client: func() mtop.Client { return client }}, nil, nil)
	// result、err 保存请求前的卖家身份及返回状态；扁平值不包含 UID，禁止从 /im 视图补齐。
	result, err := runtime.FetchSoldOrders(context.Background(), &orderapp.PlatformRuntimeData{Value: "sid=initial", MetadataJSON: cookierefresh.MetadataWithSnapshot("", snapshot)})
	if err != nil || calls != 1 || result.SellerID != "202" || !result.CookieUpdate.Changed || !result.CookieUpdate.Handled {
		t.Fatal("实际 HTTP 身份、请求结果或响应 Cookie 收集异常")
	}
}
