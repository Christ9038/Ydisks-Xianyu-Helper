package agentplatform

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// recordedRequest 保存测试服务端收到的一次请求，便于断言路径、令牌与请求体。
type recordedRequest struct {
	// Path 是请求路径；Auth 是 Authorization 头；Body 是解析后的 JSON 请求体。
	Path, Auth string
	Body       map[string]any
}

// serve 启动一个按顺序返回给定处理函数的测试服务端，并记录每次收到的请求。
func serve(t *testing.T, handlers ...func(w http.ResponseWriter, call int)) (*httptest.Server, *[]recordedRequest) {
	t.Helper()
	// recorded 保存已收到的请求；calls 是已处理的请求数。
	recorded, calls := &[]recordedRequest{}, 0
	// server 是本地测试服务端，按用例预设响应并记录请求。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// raw 是请求体原文；body 是解析结果。
		raw, _ := io.ReadAll(r.Body)
		// body 是解析后的请求体或序列化后的字节。
		body := map[string]any{}
		_ = json.Unmarshal(raw, &body)
		*recorded = append(*recorded, recordedRequest{Path: r.URL.Path, Auth: r.Header.Get("Authorization"), Body: body})
		// handler 是本次请求对应的处理函数；超出个数时沿用最后一个。
		handler := handlers[min(calls, len(handlers)-1)]
		handler(w, calls)
		calls++
	}))
	t.Cleanup(server.Close)
	return server, recorded
}

// endpointFor 构造指向测试服务端的 Endpoint。
func endpointFor(server *httptest.Server, suffix string) Endpoint {
	return Endpoint{BaseURL: server.URL + suffix, Token: "secret-key", HTTPClient: server.Client()}
}

// sse 把若干事件对象写成 SSE 响应，每个事件一行 data，并夹带一行 ping 注释。
func sse(w http.ResponseWriter, events ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = io.WriteString(w, ": ping\n\n")
	// event 是当前遍历的事件或解析出的事件。
	for _, event := range events {
		_, _ = io.WriteString(w, "data: "+event+"\n\n")
	}
}

// TestResolveURL 验证地址补全规则：域名根、基础路径、完整接口地址与非法地址。
func TestResolveURL(t *testing.T) {
	// cases 是各种用户可能填写的地址及期望的接口地址。
	cases := []struct{ raw, want string }{
		{"https://api.dify.ai/v1", "https://api.dify.ai/v1/chat-messages"},
		{"https://api.dify.ai/v1/", "https://api.dify.ai/v1/chat-messages"},
		{"https://dify.example.com", "https://dify.example.com/v1/chat-messages"},
		{"https://dify.example.com/v1/chat-messages?x=1", "https://dify.example.com/v1/chat-messages"},
		{"http://10.0.0.5:5001/proxy", "http://10.0.0.5:5001/proxy/v1/chat-messages"},
	}
	// tc 是当前表驱动用例。
	for _, tc := range cases {
		// err 是该步骤的错误，期望为空或为被测的特定错误；got 是读取或解析得到的实际值，用于与期望值比对。
		if got, err := resolveURL(tc.raw, "/chat-messages", "/v1/chat-messages", pathRule{"/v1", "/chat-messages"}); err != nil || got != tc.want {
			t.Errorf("Dify %q => %q err=%v, want %q", tc.raw, got, err, tc.want)
		}
	}
	// fastCases 是 FastGPT 的地址及期望的接口地址。
	fastCases := []struct{ raw, want string }{
		{"https://cloud.fastgpt.cn/api", "https://cloud.fastgpt.cn/api/v1/chat/completions"},
		{"https://fastgpt.example.com", "https://fastgpt.example.com/api/v1/chat/completions"},
		{"https://fastgpt.example.com/api/v2", "https://fastgpt.example.com/api/v2/chat/completions"},
		{"https://fastgpt.example.com/api/v1/chat/completions", "https://fastgpt.example.com/api/v1/chat/completions"},
	}
	// tc 是当前表驱动用例。
	for _, tc := range fastCases {
		// err 是该步骤的错误，期望为空或为被测的特定错误；got 是读取或解析得到的实际值，用于与期望值比对。
		if got, err := resolveURL(tc.raw, "/chat/completions", "/api/v1/chat/completions",
			pathRule{"/api", "/v1/chat/completions"}, pathRule{"/api/v1", "/chat/completions"}, pathRule{"/api/v2", "/chat/completions"},
			pathRule{"/v1", "/chat/completions"}, pathRule{"/v2", "/chat/completions"}); err != nil || got != tc.want {
			t.Errorf("FastGPT %q => %q err=%v, want %q", tc.raw, got, err, tc.want)
		}
	}
	// bad 是非法的平台地址。
	for _, bad := range []string{"", "ftp://x/y", "not a url", "https://"} {
		// err 是该步骤的错误，期望为空或为被测的特定错误。
		if _, err := resolveURL(bad, "/chat-messages", "/v1/chat-messages"); err == nil {
			t.Errorf("非法地址 %q 应被拒绝", bad)
		}
	}
}

// TestCallRejectsUnknownProtocol 验证 Call 按协议分发且拒绝未知协议。
func TestCallRejectsUnknownProtocol(t *testing.T) {
	// err 是该步骤的错误，期望为空或为被测的特定错误。
	if _, err := Call(context.Background(), "other", Endpoint{}, Request{}); err == nil {
		t.Fatal("未知协议应返回错误")
	}
}

// TestDifyBlockingFlow 验证 Dify 正常流程：请求体与令牌、片段拼接、会话标识返回与续用。
func TestDifyBlockingFlow(t *testing.T) {
	// recorded 保存服务端收到的全部请求记录；server 是本地测试服务端，按用例预设响应并记录请求。
	server, recorded := serve(t, func(w http.ResponseWriter, _ int) {
		sse(w, `{"event":"message","answer":"你","conversation_id":"conv-1"}`, `{"event":"message","answer":"好 ","conversation_id":"conv-1"}`, `{"event":"message_end","conversation_id":"conv-1"}`)
	})
	// result、err 保存首轮结果（无会话标识）。
	result, err := Dify(context.Background(), endpointFor(server, "/v1"), Request{Text: "在吗", User: "u1", ImageURLs: []string{"https://img/1.png"}})
	if err != nil || result.Text != "你好" || result.SessionID != "conv-1" || result.ImageDropped {
		t.Fatalf("首轮结果错误: %+v err=%v", result, err)
	}
	// first 是服务端记录的首轮请求。
	first := (*recorded)[0]
	if first.Path != "/v1/chat-messages" || first.Auth != "Bearer secret-key" || first.Body["query"] != "在吗" || first.Body["user"] != "u1" ||
		first.Body["response_mode"] != "streaming" || first.Body["conversation_id"] != "" {
		t.Fatalf("首轮请求错误: %+v", first)
	}
	// files 是请求里的图片引用。
	files, _ := first.Body["files"].([]any)
	if len(files) != 1 || files[0].(map[string]any)["transfer_method"] != "remote_url" || files[0].(map[string]any)["url"] != "https://img/1.png" {
		t.Fatalf("图片引用错误: %+v", first.Body["files"])
	}
	// 第二轮带上会话标识，请求体不再含 files。
	if _, err = Dify(context.Background(), endpointFor(server, "/v1"), Request{Text: "再问", User: "u1", SessionID: "conv-1"}); err != nil {
		t.Fatal(err)
	}
	// second 是第二轮请求的记录。
	if second := (*recorded)[1]; second.Body["conversation_id"] != "conv-1" || second.Body["files"] != nil {
		t.Fatalf("第二轮请求错误: %+v", second)
	}
}

// TestDifyStreamEventKinds 验证 agent_message 去重、message_replace 替换、error 事件与空回复。
func TestDifyStreamEventKinds(t *testing.T) {
	// agent 场景：agent_message 分片后结尾又有一条完整 message，不应重复。
	server, _ := serve(t, func(w http.ResponseWriter, _ int) {
		sse(w, `{"event":"agent_thought","thought":"想"}`, `{"event":"agent_message","answer":"A","conversation_id":"c"}`, `{"event":"agent_message","answer":"B","conversation_id":"c"}`, `{"event":"message","answer":"AB","conversation_id":"c"}`)
	})
	// err 是该步骤的错误，期望为空或为被测的特定错误；result 是被测调用的返回结果。
	if result, err := Dify(context.Background(), endpointFor(server, ""), Request{Text: "x"}); err != nil || result.Text != "AB" {
		t.Fatalf("agent 去重失败: %+v err=%v", result, err)
	}
	// 内容审核替换：整体替换已累积的文字。
	server, _ = serve(t, func(w http.ResponseWriter, _ int) {
		sse(w, `{"event":"message","answer":"敏感"}`, `{"event":"message_replace","answer":"已替换"}`)
	})
	// err 是该步骤的错误，期望为空或为被测的特定错误；result 是被测调用的返回结果。
	if result, err := Dify(context.Background(), endpointFor(server, ""), Request{Text: "x"}); err != nil || result.Text != "已替换" {
		t.Fatalf("替换失败: %+v err=%v", result, err)
	}
	// error 事件：带状态与错误码；缺状态时按 502。
	server, _ = serve(t, func(w http.ResponseWriter, _ int) {
		sse(w, `{"event":"error","status":400,"code":"provider_quota_exceeded"}`)
	})
	// err 是 error 事件转换出的状态错误。
	_, err := Dify(context.Background(), endpointFor(server, ""), Request{Text: "x"})
	if StatusCode(err) != 400 || !strings.Contains(err.Error(), "provider_quota_exceeded") {
		t.Fatalf("error 事件错误: %v", err)
	}
	server, _ = serve(t, func(w http.ResponseWriter, _ int) { sse(w, `{"event":"error","code":"x"}`) })
	if _, err = Dify(context.Background(), endpointFor(server, ""), Request{Text: "x"}); StatusCode(err) != 502 {
		t.Fatalf("缺状态应为 502: %v", err)
	}
	// 没有任何文字：空回复。
	server, _ = serve(t, func(w http.ResponseWriter, _ int) { sse(w, `{"event":"message_end"}`) })
	if _, err = Dify(context.Background(), endpointFor(server, ""), Request{Text: "x"}); !errors.Is(err, ErrEmptyReply) {
		t.Fatalf("空回复应返回 ErrEmptyReply: %v", err)
	}
}

// TestDifyErrorsAndImageFallback 验证会话失效、普通状态错误、图片降级与传输错误脱敏。
func TestDifyErrorsAndImageFallback(t *testing.T) {
	// 带会话标识时 404 与 conversation_completed 视为会话失效。
	for _, status := range []struct {
		code int
		body string
	}{{404, `{"code":"not_found"}`}, {400, `{"code":"conversation_completed"}`}} {
		// server 是本地测试服务端，按用例预设响应并记录请求。
		server, _ := serve(t, func(w http.ResponseWriter, _ int) { w.WriteHeader(status.code); _, _ = io.WriteString(w, status.body) })
		// err 是该步骤的错误，期望为空或为被测的特定错误。
		if _, err := Dify(context.Background(), endpointFor(server, ""), Request{Text: "x", SessionID: "old"}); !errors.Is(err, ErrSessionGone) {
			t.Errorf("%d 应为会话失效: %v", status.code, err)
		}
	}
	// 没带会话标识时 404 只是普通状态错误。
	server, _ := serve(t, func(w http.ResponseWriter, _ int) {
		w.WriteHeader(404)
		_, _ = io.WriteString(w, `{"code":"not_found"}`)
	})
	// err 是该步骤的错误，期望为空或为被测的特定错误。
	if _, err := Dify(context.Background(), endpointFor(server, ""), Request{Text: "x"}); StatusCode(err) != 404 || errors.Is(err, ErrSessionGone) {
		t.Fatalf("无会话的 404: %v", err)
	}
	// 401：状态错误，不泄露令牌。
	server, _ = serve(t, func(w http.ResponseWriter, _ int) {
		w.WriteHeader(401)
		_, _ = io.WriteString(w, `{"code":"unauthorized"}`)
	})
	// err 是 401 对应的状态错误。
	_, err := Dify(context.Background(), endpointFor(server, ""), Request{Text: "x"})
	if StatusCode(err) != 401 || strings.Contains(err.Error(), "secret-key") {
		t.Fatalf("401 错误: %v", err)
	}
	// 图片被 400 拒绝：去掉图片重试，成功后标记 ImageDropped。
	server, recorded := serve(t,
		func(w http.ResponseWriter, _ int) {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"code":"bad_request"}`)
		},
		func(w http.ResponseWriter, _ int) {
			sse(w, `{"event":"message","answer":"文字回复","conversation_id":"c"}`)
		})
	// result 是降级重试的结果。
	result, err := Dify(context.Background(), endpointFor(server, ""), Request{Text: "x", ImageURLs: []string{"https://i/1.png"}})
	if err != nil || result.Text != "文字回复" || !result.ImageDropped || len(*recorded) != 2 || (*recorded)[1].Body["files"] != nil {
		t.Fatalf("图片降级失败: %+v err=%v rec=%d", result, err, len(*recorded))
	}
	// 非法地址与传输错误。
	if _, err = Dify(context.Background(), Endpoint{BaseURL: "ftp://x"}, Request{}); err == nil {
		t.Fatal("非法地址应报错")
	}
	// dead 是已关闭的服务端，用于触发传输错误。
	dead := httptest.NewServer(http.NotFoundHandler())
	// deadEndpoint 指向已关闭的服务端，用于模拟无法连接。
	deadEndpoint := endpointFor(dead, "")
	dead.Close()
	if _, err = Dify(context.Background(), deadEndpoint, Request{Text: "x"}); err == nil || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("传输错误应脱敏: %v", err)
	}
	// 上下文已取消时保留取消语义。
	// canceled 是已取消的上下文。
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	server, _ = serve(t, func(w http.ResponseWriter, _ int) { sse(w, `{"event":"message","answer":"x"}`) })
	if _, err = Dify(canceled, endpointFor(server, ""), Request{Text: "x"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("应保留取消语义: %v", err)
	}
}

// TestFastGPTFlow 验证 FastGPT 正常流程：路径、chatId、纯文字内容、图片数组与回复解析。
func TestFastGPTFlow(t *testing.T) {
	// recorded 保存服务端收到的全部请求记录；server 是本地测试服务端，按用例预设响应并记录请求。
	server, recorded := serve(t, func(w http.ResponseWriter, _ int) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":" 你好 "}}]}`)
	})
	// result、err 是纯文字请求的结果。
	result, err := FastGPT(context.Background(), endpointFor(server, "/api"), Request{Text: "在吗", SessionID: "chat-1"})
	if err != nil || result.Text != "你好" || result.SessionID != "chat-1" {
		t.Fatalf("结果错误: %+v err=%v", result, err)
	}
	// first 是服务端记录的请求。
	first := (*recorded)[0]
	// messages 是请求体中的消息列表。
	messages, _ := first.Body["messages"].([]any)
	if first.Path != "/api/v1/chat/completions" || first.Auth != "Bearer secret-key" || first.Body["chatId"] != "chat-1" ||
		first.Body["stream"] != false || len(messages) != 1 || messages[0].(map[string]any)["content"] != "在吗" {
		t.Fatalf("请求错误: %+v", first)
	}
	// 带图片时 content 是 text + image_url 数组。
	if _, err = FastGPT(context.Background(), endpointFor(server, "/api"), Request{Text: "看图", SessionID: "chat-1", ImageURLs: []string{"https://i/1.png"}}); err != nil {
		t.Fatal(err)
	}
	// parts 是带图片请求里的 content 数组。
	parts, _ := (*recorded)[1].Body["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if len(parts) != 2 || parts[1].(map[string]any)["type"] != "image_url" {
		t.Fatalf("图片内容错误: %+v", parts)
	}
}

// TestFastGPTContentShapes 验证 content 为数组、空 choices 与空文字的处理。
func TestFastGPTContentShapes(t *testing.T) {
	// cases 是各种响应体及期望文字（空串表示应报空回复）。
	cases := []struct{ body, want string }{
		{`{"choices":[{"message":{"content":[{"type":"text","text":"甲"},{"type":"tool"},{"type":"text","text":{"content":"乙"}}]}}]}`, "甲乙"},
		{`{"choices":[]}`, ""}, {`not json`, ""}, {`{"choices":[{"message":{"content":"  "}}]}`, ""}, {`{"choices":[{"message":{"content":5}}]}`, ""},
	}
	// tc 是当前表驱动用例。
	for _, tc := range cases {
		// server 是本地测试服务端，按用例预设响应并记录请求。
		server, _ := serve(t, func(w http.ResponseWriter, _ int) { _, _ = io.WriteString(w, tc.body) })
		// result、err 是该响应体的解析结果。
		result, err := FastGPT(context.Background(), endpointFor(server, ""), Request{Text: "x"})
		if tc.want == "" && !errors.Is(err, ErrEmptyReply) || tc.want != "" && (err != nil || result.Text != tc.want) {
			t.Errorf("%s => %+v err=%v, want %q", tc.body, result, err, tc.want)
		}
	}
}

// TestFastGPTRetriesAndFallbacks 验证 409 忙碌重试、/v2 回退、图片降级与错误归类。
func TestFastGPTRetriesAndFallbacks(t *testing.T) {
	// waits 记录被请求的等待时长；测试期间替换 sleep 避免真实等待。
	var waits []time.Duration
	// original 保存被替换前的真实 sleep，测试结束后恢复。
	original := sleep
	sleep = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	t.Cleanup(func() { sleep = original })

	// ok 表示相应的存在性或类型判断是否成立。
	ok := func(w http.ResponseWriter, _ int) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"好"}}]}`)
	}
	// busy 是返回 409 的处理函数，模拟会话仍在生成。
	busy := func(w http.ResponseWriter, _ int) {
		w.WriteHeader(409)
		_, _ = io.WriteString(w, `{"statusText":"chatIsGenerating"}`)
	}
	// 忙两次后成功：等待 2s、4s。
	server, _ := serve(t, busy, busy, ok)
	// err 是该步骤的错误，期望为空或为被测的特定错误；result 是被测调用的返回结果。
	if result, err := FastGPT(context.Background(), endpointFor(server, ""), Request{Text: "x", SessionID: "c"}); err != nil || result.Text != "好" {
		t.Fatalf("重试后应成功: %+v err=%v", result, err)
	}
	if len(waits) != 2 || waits[0] != 2*time.Second || waits[1] != 4*time.Second {
		t.Fatalf("等待序列错误: %v", waits)
	}
	// 一直忙：重试用尽后返回 409 状态错误。
	server, recorded := serve(t, busy)
	// err 是重试用尽后的错误。
	_, err := FastGPT(context.Background(), endpointFor(server, ""), Request{Text: "x", SessionID: "c"})
	if StatusCode(err) != 409 || !strings.Contains(err.Error(), "chatIsGenerating") || len(*recorded) != fastGPTBusyRetries+1 {
		t.Fatalf("重试用尽: %v 请求数=%d", err, len(*recorded))
	}
	// 实测形态：FastGPT 忙碌时返回 HTTP 500 加 statusText=chatIsGenerating，同样应重试；
	// 等待依次为 2s、4s、6s，之后封顶在 6s。
	waits = nil
	// busy500 是返回 HTTP 500 加 chatIsGenerating 的处理函数，模拟实测的 FastGPT 忙碌响应。
	busy500 := func(w http.ResponseWriter, _ int) {
		w.WriteHeader(500)
		_, _ = io.WriteString(w, `{"code":500,"statusText":"chatIsGenerating"}`)
	}
	server, _ = serve(t, busy500, busy500, busy500, busy500, busy500, ok)
	// result、err 是连续忙碌五次后第六次成功的调用结果与错误。
	if result, err := FastGPT(context.Background(), endpointFor(server, ""), Request{Text: "x", SessionID: "c"}); err != nil || result.Text != "好" {
		t.Fatalf("HTTP 500 忙碌重试后应成功: %+v err=%v", result, err)
	}
	// want 是期望的等待序列：线性增长 2s、4s、6s 后封顶在 6s。
	if want := []time.Duration{2 * time.Second, 4 * time.Second, 6 * time.Second, 6 * time.Second, 6 * time.Second}; !reflect.DeepEqual(waits, want) {
		t.Fatalf("等待序列应线性增长并封顶: %v", waits)
	}
	// 500 但不是忙碌（其他错误名）不重试。
	other500 := func(w http.ResponseWriter, _ int) {
		w.WriteHeader(500)
		_, _ = io.WriteString(w, `{"statusText":"unAuthApiKey"}`)
	}
	waits = nil
	server, recorded = serve(t, other500)
	if _, err = FastGPT(context.Background(), endpointFor(server, ""), Request{Text: "x", SessionID: "c"}); StatusCode(err) != 500 || len(*recorded) != 1 || len(waits) != 0 {
		t.Fatalf("非忙碌的 500 不应重试: %v 请求数=%d 等待=%v", err, len(*recorded), waits)
	}
	// 等待期间上下文取消：直接返回取消错误。
	sleep = func(ctx context.Context, _ time.Duration) error { return context.Canceled }
	server, _ = serve(t, busy)
	if _, err = FastGPT(context.Background(), endpointFor(server, ""), Request{Text: "x"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("应返回取消: %v", err)
	}
	sleep = func(_ context.Context, d time.Duration) error { return nil }

	// v1 返回 404 时回退到 /v2。
	notFound := func(w http.ResponseWriter, _ int) { w.WriteHeader(404) }
	server, recorded = serve(t, notFound, ok)
	// err 是该步骤的错误，期望为空或为被测的特定错误；result 是被测调用的返回结果。
	if result, err := FastGPT(context.Background(), endpointFor(server, "/api"), Request{Text: "x"}); err != nil || result.Text != "好" ||
		(*recorded)[1].Path != "/api/v2/chat/completions" {
		t.Fatalf("v2 回退失败: %+v err=%v", result, err)
	}
	// v1 与 v2 都 404：返回 404。
	server, _ = serve(t, notFound)
	if _, err = FastGPT(context.Background(), endpointFor(server, "/api"), Request{Text: "x"}); StatusCode(err) != 404 {
		t.Fatalf("双 404: %v", err)
	}
	// v2 回退时遇到非 404 错误：返回那个错误。
	server, _ = serve(t, notFound, func(w http.ResponseWriter, _ int) { w.WriteHeader(401) })
	if _, err = FastGPT(context.Background(), endpointFor(server, "/api"), Request{Text: "x"}); StatusCode(err) != 401 {
		t.Fatalf("回退后的 401: %v", err)
	}
	// 用户给出完整的 /v2 接口地址时不再回退。
	server, recorded = serve(t, notFound)
	if _, err = FastGPT(context.Background(), endpointFor(server, "/api/v2/chat/completions"), Request{Text: "x"}); StatusCode(err) != 404 || len(*recorded) != 1 {
		t.Fatalf("显式 v2 不应回退: %v 请求数=%d", err, len(*recorded))
	}
	// 图片被 422 拒绝：去掉图片重试。
	server, recorded = serve(t, func(w http.ResponseWriter, _ int) { w.WriteHeader(422) }, ok)
	// result 是降级重试结果。
	result, err := FastGPT(context.Background(), endpointFor(server, ""), Request{Text: "x", ImageURLs: []string{"https://i/1.png"}})
	if err != nil || !result.ImageDropped || len(*recorded) != 2 {
		t.Fatalf("图片降级: %+v err=%v", result, err)
	}
	// 非法地址、传输错误与错误体非 JSON。
	if _, err = FastGPT(context.Background(), Endpoint{BaseURL: "x"}, Request{}); err == nil {
		t.Fatal("非法地址应报错")
	}
	server, _ = serve(t, func(w http.ResponseWriter, _ int) { w.WriteHeader(500); _, _ = io.WriteString(w, "boom") })
	if _, err = FastGPT(context.Background(), endpointFor(server, ""), Request{Text: "x"}); StatusCode(err) != 500 {
		t.Fatalf("500: %v", err)
	}
	// dead 是已关闭的服务端，用于触发传输错误。
	dead := httptest.NewServer(http.NotFoundHandler())
	// deadEndpoint 指向已关闭的服务端，用于模拟无法连接。
	deadEndpoint := endpointFor(dead, "")
	dead.Close()
	if _, err = FastGPT(context.Background(), deadEndpoint, Request{Text: "x"}); err == nil || StatusCode(err) != 0 {
		t.Fatalf("传输错误: %v", err)
	}
}

// TestStatusErrorAndLimits 验证状态错误文案、StatusCode 对普通错误返回 0，以及响应过大的拒绝。
func TestStatusErrorAndLimits(t *testing.T) {
	if (&StatusError{Code: 500}).Error() != "平台返回 HTTP 500" || StatusCode(errors.New("x")) != 0 {
		t.Fatal("状态错误文案或 StatusCode 错误")
	}
	// err 是该步骤的错误，期望为空或为被测的特定错误。
	if _, err := readLimited(strings.NewReader(strings.Repeat("a", 11)), 10); err == nil {
		t.Fatal("超限应报错")
	}
	// data 是读取到的数据；err 是该步骤的错误，期望为空或为被测的特定错误。
	if data, err := readLimited(strings.NewReader("abc"), 10); err != nil || string(data) != "abc" {
		t.Fatalf("未超限读取失败: %q %v", data, err)
	}
	// 真实 sleep 在已取消的上下文上立即返回，在正常情况下等待后返回 nil。
	// canceled 是已取消的上下文。
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	// err 是该步骤的错误，期望为空或为被测的特定错误。
	if err := sleep(canceled, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("sleep 取消: %v", err)
	}
	// err 是该步骤的错误，期望为空或为被测的特定错误。
	if err := sleep(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("sleep: %v", err)
	}
}
