package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"xianyu-go/internal/agentclient"
	"xianyu-go/internal/db"
)

// recordingDelivery 记录每次交给聊天应用的完整回复，并可注入失败或不确定结果。
type recordingDelivery struct {
	// mu 保护 messages 和 failAt，测试 goroutine 与被测轮次会并发访问。
	mu sync.Mutex
	// messages 是按发送顺序记录的回复。
	messages []ReplyMessage
	// failAt 大于 0 时，第 failAt 次发送返回 err（并按 uncertain 标记结果）。
	failAt    int
	err       error
	uncertain bool
}

// SendReply 记录消息并按配置返回确认结果。
func (d *recordingDelivery) SendReply(_ context.Context, message ReplyMessage) (ReplySendResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.messages = append(d.messages, message)
	if d.failAt > 0 && len(d.messages) == d.failAt {
		return ReplySendResult{Uncertain: d.uncertain}, d.err
	}
	return ReplySendResult{TextSent: message.Text != "", ImageSent: message.ImageURL != ""}, nil
}

// count 返回已记录的回复条数。
func (d *recordingDelivery) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.messages)
}

// scriptedAgent 是按顺序返回预设响应的本地 Agent，并记录每个请求体与 Authorization 头。
type scriptedAgent struct {
	// mu 保护 requests 和 index。
	mu       sync.Mutex
	requests []agentclient.Request
	auth     []string
	index    int
	// responses 是依次返回的响应；超出后重复返回最后一项。状态码非 0 时返回该状态码。
	responses []scriptedResponse
	// block 非空时，每个请求先等待它被关闭或客户端断开。
	block chan struct{}
}

// scriptedResponse 描述一次预设响应。
type scriptedResponse struct {
	status int
	body   string
}

// serve 启动 httptest 服务并注册清理，返回其地址。
func (a *scriptedAgent) serve(t *testing.T) string {
	t.Helper()
	// server 是本地测试 Agent，按脚本顺序返回响应并记录每个请求。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// raw 保存请求体原文，用于解码为协议请求。
		raw, _ := io.ReadAll(r.Body)
		// request 保存解码后的协议请求，供测试断言事件、序号和标识。
		var request agentclient.Request
		_ = json.Unmarshal(raw, &request)
		a.mu.Lock()
		a.requests = append(a.requests, request)
		a.auth = append(a.auth, r.Header.Get("Authorization"))
		// current 是本次请求对应的预设响应；超出脚本长度时重复最后一项。
		current := a.responses[min(a.index, len(a.responses)-1)]
		a.index++
		// block 是可选的阻塞通道，用于模拟迟迟不返回的 Agent。
		block := a.block
		a.mu.Unlock()
		if block != nil {
			select {
			case <-block:
			case <-r.Context().Done():
				return
			}
		}
		if current.status != 0 {
			http.Error(w, "boom", current.status)
			return
		}
		_, _ = w.Write([]byte(current.body))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// seen 返回已收到请求的快照。
func (a *scriptedAgent) seen() []agentclient.Request {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]agentclient.Request(nil), a.requests...)
}

// agentFixture 组装 Agent 回复器、记录投递、已配置的数据库与脚本 Agent。
type agentFixture struct {
	store    *db.Store
	delivery *recordingDelivery
	agent    *scriptedAgent
	replier  *AgentReplier
	// sleeps 记录轮询等待时长，测试中不做真实等待。
	sleeps []time.Duration
}

// newAgentFixture 创建启用 Agent 的夹具；responses 为脚本响应。
func newAgentFixture(t *testing.T, responses ...scriptedResponse) *agentFixture {
	t.Helper()
	// store、cleanup 是隔离的数据库聚合及清理函数。
	store, cleanup := newReplyStore(t)
	t.Cleanup(cleanup)
	// agent 是按脚本返回响应的本地 Agent。
	agent := &scriptedAgent{responses: responses}
	// url 是本地 Agent 的访问地址，写入账号配置。
	url := agent.serve(t)
	// err 保存写入启用状态与令牌配置的错误。
	if err := store.AgentReply.Upsert(context.Background(), "cid", db.AgentReplyWrite{Enabled: true, EndpointURL: url,
		RequestTimeoutSeconds: 5, MaxSessionSeconds: 30, TokenAction: db.AgentTokenReplace, Token: "agent-token"}); err != nil {
		t.Fatal(err)
	}
	// fixture 汇总数据库、投递记录和脚本 Agent，供各用例共用。
	fixture := &agentFixture{store: store, delivery: &recordingDelivery{}, agent: agent}
	fixture.replier = NewAgentReplier("cid", store, fixture.delivery, nil)
	fixture.replier.pollSleep = func(ctx context.Context, d time.Duration) error {
		fixture.sleeps = append(fixture.sleeps, d)
		return ctx.Err()
	}
	return fixture
}

// TestAgentReplyNotEngaged 验证未配置、未启用、缺少投递端口、缺少会话标识时回复器不接手。
func TestAgentReplyNotEngaged(t *testing.T) {
	// store、cleanup 是隔离的数据库聚合及清理函数。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 是本用例共用的上下文。
	ctx := context.Background()
	// delivery 是记录所有发送内容的投递替身。
	delivery := &recordingDelivery{}
	// replier 是被测的 Agent 回复器。
	replier := NewAgentReplier("cid", store, delivery, nil)
	// res、err 保存未配置账号的回复结果及错误，期望不接手。
	if res, err := replier.Reply(ctx, chatMsg("你好", "item", "chat")); res != nil || err != nil {
		t.Fatalf("未配置不应接手: %+v %v", res, err)
	}
	// 配置存在但未启用。
	if err := store.AgentReply.Upsert(ctx, "cid", db.AgentReplyWrite{EndpointURL: "http://127.0.0.1:1", RequestTimeoutSeconds: 5, MaxSessionSeconds: 30}); err != nil {
		t.Fatal(err)
	}
	// res、err 保存已配置但未启用账号的回复结果及错误，期望不接手。
	if res, err := replier.Reply(ctx, chatMsg("你好", "item", "chat")); res != nil || err != nil {
		t.Fatalf("未启用不应接手: %+v %v", res, err)
	}
	// err 保存启用 Agent 的配置写入错误。
	if err := store.AgentReply.Upsert(ctx, "cid", db.AgentReplyWrite{Enabled: true, EndpointURL: "http://127.0.0.1:1", RequestTimeoutSeconds: 5, MaxSessionSeconds: 30}); err != nil {
		t.Fatal(err)
	}
	// res、err 保存缺少投递端口时的回复结果及错误，期望不接手。
	if res, err := NewAgentReplier("cid", store, nil, nil).Reply(ctx, chatMsg("你好", "item", "chat")); res != nil || err != nil {
		t.Fatalf("缺少投递端口不应接手: %+v %v", res, err)
	}
	// res、err 保存缺少会话标识时的回复结果及错误，期望不接手。
	if res, err := replier.Reply(ctx, chatMsg("你好", "item", "")); res != nil || err != nil {
		t.Fatalf("缺少会话标识不应接手: %+v %v", res, err)
	}
	// res、err 保存 nil 回复器的防御性结果，期望不接手也不崩溃。
	if res, err := (*AgentReplier)(nil).Reply(ctx, chatMsg("你好", "item", "chat")); res != nil || err != nil {
		t.Fatalf("nil 回复器不应接手: %+v %v", res, err)
	}
	if delivery.count() != 0 {
		t.Fatal("不接手时不应发送任何消息")
	}
}

// TestAgentReplySingleReply 验证单次响应：请求字段、Bearer 令牌、逐条发送与 Skip 结果。
func TestAgentReplySingleReply(t *testing.T) {
	// f 是返回一条文字和一条带图片回复的 Agent 夹具。
	f := newAgentFixture(t, scriptedResponse{body: `{"replies":[{"text":"您好，在的"},{"text":"请看图","image_url":"https://img.example.com/a.png"}]}`})
	// message 是带平台消息标识的买家文字消息。
	message := chatMsg("在吗", "item-1", "chat-1")
	message.MessageID = "m-1"
	// res、err 保存回复器的接手结果及错误。
	res, err := f.replier.Reply(context.Background(), message)
	if err != nil || res == nil || !res.Skip || res.Source != "Agent" || res.AutoPriceQuote != nil {
		t.Fatalf("应接手并返回 Skip: %+v %v", res, err)
	}
	// requests 是 Agent 实际收到的请求快照。
	requests := f.agent.seen()
	if len(requests) != 1 || f.agent.auth[0] != "Bearer agent-token" {
		t.Fatalf("请求数或令牌异常: %d %v", len(requests), f.agent.auth)
	}
	// got 是首个 message 事件，用于逐字段核对协议。
	got := requests[0]
	if got.Version != 1 || got.Event != "message" || got.Sequence != 1 || got.TurnID == "" || got.AccountID != "cid" || got.ChatID != "chat-1" || got.ItemID != "item-1" ||
		got.Buyer == nil || got.Buyer.UserID != "buyer1" || got.Buyer.Name != "买家" || got.Message == nil || got.Message.ID != "m-1" || got.Message.Type != "text" || got.Message.Text != "在吗" {
		t.Fatalf("message 事件不符合协议: %+v", got)
	}
	if len(f.delivery.messages) != 2 || f.delivery.messages[0].Text != "您好，在的" || f.delivery.messages[1].ImageURL != "https://img.example.com/a.png" ||
		f.delivery.messages[0].ChatID != "chat-1" || f.delivery.messages[0].ToUserID != "buyer1" || f.delivery.messages[0].AccountID != "cid" {
		t.Fatalf("发送内容异常: %+v", f.delivery.messages)
	}
	if len(f.sleeps) != 0 {
		t.Fatal("working=false 不应轮询等待")
	}
}

// TestAgentReplyHandledFalseAndSilent 验证 handled=false 降级，以及 handled 缺省且无回复表示 Agent 有意沉默（仍接手，抑制后续规则）。
func TestAgentReplyHandledFalseAndSilent(t *testing.T) {
	// f 是返回 handled=false 的 Agent 夹具。
	f := newAgentFixture(t, scriptedResponse{body: `{"handled":false,"replies":[{"text":"忽略"}]}`})
	// res、err 保存 handled=false 时的结果，期望降级且不发送。
	if res, err := f.replier.Reply(context.Background(), chatMsg("hi", "", "chat")); res != nil || err != nil || f.delivery.count() != 0 {
		t.Fatalf("handled=false 应降级且不发送: %+v %v", res, err)
	}
	// silent 是返回空对象（handled 缺省且无回复）的 Agent 夹具，表示 Agent 有意沉默。
	silent := newAgentFixture(t, scriptedResponse{body: `{}`})
	// res、err 保存沉默场景的结果，期望仍接手并抑制后续规则。
	if res, err := silent.replier.Reply(context.Background(), chatMsg("hi", "", "chat")); err != nil || res == nil || !res.Skip || silent.delivery.count() != 0 {
		t.Fatalf("沉默应接手并抑制后续规则: %+v %v", res, err)
	}
}

// TestAgentReplyPollingFlow 验证 working=true 的轮询：poll 事件不带消息、序号递增、间隔夹取，并逐步发送每个响应里的回复。
func TestAgentReplyPollingFlow(t *testing.T) {
	// f 是依次返回 working=true、working=true、working=false 的 Agent 夹具。
	f := newAgentFixture(t,
		scriptedResponse{body: `{"working":true,"poll_after_ms":100,"replies":[{"text":"稍等"}]}`},
		scriptedResponse{body: `{"working":true,"poll_after_ms":999999}`},
		scriptedResponse{body: `{"working":false,"replies":[{"text":"处理好了"}]}`},
	)
	// res、err 保存整轮轮询结束后的结果及错误。
	res, err := f.replier.Reply(context.Background(), chatMsg("帮我查", "item", "chat"))
	if err != nil || res == nil || !res.Skip {
		t.Fatalf("轮询应完成: %+v %v", res, err)
	}
	// requests 是 Agent 实际收到的三个请求。
	requests := f.agent.seen()
	if len(requests) != 3 {
		t.Fatalf("应发出 3 个请求，实际 %d", len(requests))
	}
	// i、want 是请求序号及其期望的事件类型。
	for i, want := range []string{"message", "poll", "poll"} {
		if requests[i].Event != want || requests[i].Sequence != i+1 || requests[i].TurnID != requests[0].TurnID || requests[i].ChatID != "chat" {
			t.Fatalf("第 %d 个请求不符: %+v", i+1, requests[i])
		}
	}
	if requests[1].Message != nil || requests[1].Buyer != nil {
		t.Fatalf("poll 不应携带买家消息: %+v", requests[1])
	}
	if len(f.sleeps) != 2 || f.sleeps[0] != time.Second || f.sleeps[1] != 10*time.Second {
		t.Fatalf("轮询间隔应被夹取到 [1s,10s]: %v", f.sleeps)
	}
	if f.delivery.count() != 2 || f.delivery.messages[0].Text != "稍等" || f.delivery.messages[1].Text != "处理好了" {
		t.Fatalf("应逐步发送: %+v", f.delivery.messages)
	}
}

// TestAgentReplyLaterHandledFalseEnds 验证接手后某次响应 handled=false 视为 Agent 已结束，不再轮询也不降级。
func TestAgentReplyLaterHandledFalseEnds(t *testing.T) {
	// f 是先 working=true 再返回 handled=false 的 Agent 夹具。
	f := newAgentFixture(t,
		scriptedResponse{body: `{"working":true,"replies":[{"text":"稍等"}]}`},
		scriptedResponse{body: `{"handled":false,"working":true}`},
	)
	// res、err 保存整轮结果，期望在 handled=false 时结束。
	res, err := f.replier.Reply(context.Background(), chatMsg("x", "", "chat"))
	if err != nil || res == nil || !res.Skip || len(f.agent.seen()) != 2 {
		t.Fatalf("应在 handled=false 时结束: %+v %v n=%d", res, err, len(f.agent.seen()))
	}
}

// TestAgentReplyFailureSemantics 验证失败语义：接手前失败降级并返回错误；接手后失败只结束本轮；非 2xx 与非法 JSON 同样处理。
func TestAgentReplyFailureSemantics(t *testing.T) {
	// 首个请求失败 → 降级（返回错误，由 resolve 记录后继续后续规则）。
	for name, response := range map[string]scriptedResponse{
		"状态码":    {status: 502},
		"非法JSON": {body: "not json"},
	} {
		// f 是首个响应即失败的 Agent 夹具。
		f := newAgentFixture(t, response)
		// res、err 保存接手前失败的结果，期望返回 nil 与错误以便降级。
		res, err := f.replier.Reply(context.Background(), chatMsg("x", "", "chat"))
		if res != nil || err == nil || f.delivery.count() != 0 {
			t.Fatalf("%s: 接手前失败应降级: %+v %v", name, res, err)
		}
	}
	// 接手后第二个请求失败 → 不降级、不报错，已发送的保留。
	f := newAgentFixture(t, scriptedResponse{body: `{"working":true,"replies":[{"text":"稍等"}]}`}, scriptedResponse{status: 500})
	// res、err 保存接手后失败的结果，期望不降级且无错误。
	res, err := f.replier.Reply(context.Background(), chatMsg("x", "", "chat"))
	if err != nil || res == nil || !res.Skip || f.delivery.count() != 1 {
		t.Fatalf("接手后失败应只结束本轮: %+v %v sent=%d", res, err, f.delivery.count())
	}
}

// TestAgentReplyLimits 验证请求数、回复条数和图片数的硬性上限。
func TestAgentReplyLimits(t *testing.T) {
	// 永远 working：恰好发出 60 个请求后结束。
	forever := newAgentFixture(t, scriptedResponse{body: `{"working":true}`})
	// res、err 保存永远 working 的对话在上限处结束的结果。
	res, err := forever.replier.Reply(context.Background(), chatMsg("x", "", "chat"))
	if err != nil || res == nil || !res.Skip || len(forever.agent.seen()) != agentMaxRequestsPerTurn || len(forever.sleeps) != agentMaxRequestsPerTurn {
		t.Fatalf("应在上限处结束: %+v %v n=%d", res, err, len(forever.agent.seen()))
	}
	// 12 条回复，其中 5 条带图片：最多发送 10 条，图片最多 3 张（超限图片被丢弃但文字保留）。
	replies := make([]string, 0, 12)
	// i 是待生成回复的序号，前 5 条带图片。
	for i := 0; i < 12; i++ {
		if i < 5 {
			replies = append(replies, fmt.Sprintf(`{"text":"t%d","image_url":"https://img.example.com/%d.png"}`, i, i))
		} else {
			replies = append(replies, fmt.Sprintf(`{"text":"t%d"}`, i))
		}
	}
	// many 是一次返回 12 条回复的 Agent 夹具。
	many := newAgentFixture(t, scriptedResponse{body: `{"replies":[` + strings.Join(replies, ",") + `]}`})
	// err 保存超量回复场景的调用错误。
	if _, err := many.replier.Reply(context.Background(), chatMsg("x", "", "chat")); err != nil {
		t.Fatal(err)
	}
	// images 统计实际携带图片的消息数。
	images := 0
	// message 是已发送的单条回复，用于统计图片数。
	for _, message := range many.delivery.messages {
		if message.ImageURL != "" {
			images++
		}
	}
	if many.delivery.count() != agentMaxRepliesPerTurn || images != agentMaxImagesPerTurn {
		t.Fatalf("上限异常: sent=%d images=%d", many.delivery.count(), images)
	}
}

// TestAgentReplyImageURLValidation 验证非 http(s) 图片地址被忽略（只发文字），纯图片且地址非法的回复被丢弃。
func TestAgentReplyImageURLValidation(t *testing.T) {
	// f 是返回非法图片地址回复的 Agent 夹具。
	f := newAgentFixture(t, scriptedResponse{body: `{"replies":[{"text":"文字","image_url":"file:///etc/passwd"},{"image_url":"javascript:alert(1)"}]}`})
	// err 保存非法图片地址场景的调用错误。
	if _, err := f.replier.Reply(context.Background(), chatMsg("x", "", "chat")); err != nil {
		t.Fatal(err)
	}
	if f.delivery.count() != 1 || f.delivery.messages[0].ImageURL != "" || f.delivery.messages[0].Text != "文字" {
		t.Fatalf("非法图片地址应被忽略: %+v", f.delivery.messages)
	}
}

// TestAgentReplySendFailureStopsTurn 验证发送失败和结果不确定都会立即结束本轮，不重试也不继续轮询。
func TestAgentReplySendFailureStopsTurn(t *testing.T) {
	// name、uncertain 是发送失败场景名及是否标记为结果不确定。
	for name, uncertain := range map[string]bool{"失败": false, "不确定": true} {
		// f 是返回两条回复且仍在处理的 Agent 夹具。
		f := newAgentFixture(t, scriptedResponse{body: `{"working":true,"replies":[{"text":"一"},{"text":"二"}]}`})
		f.delivery.failAt, f.delivery.err, f.delivery.uncertain = 1, errors.New("send failed"), uncertain
		if !uncertain {
			f.delivery.err = errors.New("send failed")
		}
		// res、err 保存首条发送失败后的结果，期望结束本轮。
		res, err := f.replier.Reply(context.Background(), chatMsg("x", "", "chat"))
		if err != nil || res == nil || !res.Skip || f.delivery.count() != 1 || len(f.agent.seen()) != 1 {
			t.Fatalf("%s: 应结束本轮: %+v %v sent=%d requests=%d", name, res, err, f.delivery.count(), len(f.agent.seen()))
		}
	}
}

// TestAgentReplyInboundImage 验证买家图片消息携带公网图片地址，缺少地址时按文字转发。
func TestAgentReplyInboundImage(t *testing.T) {
	// f 是返回空对象的 Agent 夹具，用于观察转发的请求。
	f := newAgentFixture(t, scriptedResponse{body: `{}`})
	// message 是带公网图片地址的买家图片消息。
	message := chatMsg("[图片]", "item", "chat")
	message.Raw = map[string]any{"1": map[string]any{"10": map[string]any{"image": map[string]any{"pics": []any{map[string]any{"url": "https://img.example.com/buyer.png"}}}}}}
	// err 保存图片消息回复的调用错误。
	if _, err := f.replier.Reply(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	// 第二条没有可提取地址的图片占位消息。
	if _, err := f.replier.Reply(context.Background(), chatMsg("[图片]", "item", "chat")); err != nil {
		t.Fatal(err)
	}
	// requests 是 Agent 实际收到的两个请求。
	requests := f.agent.seen()
	if len(requests) != 2 || requests[0].Message.Type != "image" || requests[0].Message.ImageURL != "https://img.example.com/buyer.png" ||
		requests[1].Message.Type != "text" || requests[1].Message.ImageURL != "" {
		t.Fatalf("图片消息转发异常: %+v", requests)
	}
}

// TestAgentReplyContextCancelBeforeEngaged 验证首个响应前 ctx 被取消（账号停止或被顶替）时抑制回复而不是降级到默认回复。
func TestAgentReplyContextCancelBeforeEngaged(t *testing.T) {
	// f 是会被阻塞的 Agent 夹具，用来模拟首个响应前取消。
	f := newAgentFixture(t, scriptedResponse{body: `{"replies":[{"text":"晚到"}]}`})
	f.agent.block = make(chan struct{})
	defer close(f.agent.block)
	// ctx、cancel 是会在短暂延迟后被取消的上下文及其取消函数。
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	// res、err 保存被取消时的结果，期望抑制回复。
	res, err := f.replier.Reply(ctx, chatMsg("x", "", "chat"))
	if err != nil || res == nil || !res.Skip || f.delivery.count() != 0 {
		t.Fatalf("取消应抑制回复: %+v %v", res, err)
	}
}

// TestAgentReplyNewMessageSupersedesRunningTurn 验证同一会话的新消息会取消并等待旧轮次退出，旧轮次不再发送，新轮次正常完成。
func TestAgentReplyNewMessageSupersedesRunningTurn(t *testing.T) {
	// f 是先返回 working=true、再返回新轮次回复的 Agent 夹具。
	f := newAgentFixture(t,
		scriptedResponse{body: `{"working":true,"replies":[{"text":"旧-稍等"}]}`},
		scriptedResponse{body: `{"replies":[{"text":"新-回复"}]}`},
	)
	// inSleep 在旧轮次进入轮询等待时关闭；阻塞等待直到 ctx 被取消。
	inSleep := make(chan struct{})
	// once 保证轮询等待只通知一次旧轮次已进入等待。
	var once sync.Once
	f.replier.pollSleep = func(ctx context.Context, d time.Duration) error {
		once.Do(func() { close(inSleep) })
		<-ctx.Done()
		return ctx.Err()
	}
	// oldDone 接收旧轮次的结果。
	oldDone := make(chan *ReplyResult, 1)
	go func() {
		// res 保存旧轮次的结果，通过通道交给主测试 goroutine。
		res, _ := f.replier.Reply(context.Background(), chatMsg("旧消息", "", "chat"))
		oldDone <- res
	}()
	<-inSleep
	// res、err 保存新消息轮次的结果及错误。
	res, err := f.replier.Reply(context.Background(), chatMsg("新消息", "", "chat"))
	if err != nil || res == nil || !res.Skip {
		t.Fatalf("新轮次应完成: %+v %v", res, err)
	}
	select {
	// old 是旧轮次退出后返回的结果。
	case old := <-oldDone:
		if old == nil || !old.Skip {
			t.Fatalf("旧轮次应以 Skip 结束: %+v", old)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("旧轮次未退出")
	}
	// 旧轮次只发出了第一条；新轮次发出新回复；请求按“旧 message → 新 message”顺序，旧轮次没有再 poll。
	texts := []string{}
	// message 是按顺序记录的已发送回复。
	for _, message := range f.delivery.messages {
		texts = append(texts, message.Text)
	}
	if strings.Join(texts, ",") != "旧-稍等,新-回复" {
		t.Fatalf("发送序列异常: %v", texts)
	}
	// requests 是 Agent 实际收到的请求，用于核对旧轮次没有再轮询。
	requests := f.agent.seen()
	if len(requests) != 2 || requests[0].Event != "message" || requests[1].Event != "message" || requests[0].TurnID == requests[1].TurnID {
		t.Fatalf("新消息应开启新轮次: %+v", requests)
	}
}

// TestAgentReplyConfigReadError 验证配置读取失败（密钥错误）按错误降级，不泄漏令牌。
func TestAgentReplyConfigReadError(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "agent-engine-key")
	// store、cleanup 是隔离的数据库聚合及清理函数。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 是本用例共用的上下文。
	ctx := context.Background()
	// err 保存带数据密钥写入加密令牌的错误。
	if err := store.AgentReply.Upsert(ctx, "cid", db.AgentReplyWrite{Enabled: true, EndpointURL: "http://127.0.0.1:1", RequestTimeoutSeconds: 5,
		MaxSessionSeconds: 30, TokenAction: db.AgentTokenReplace, Token: "engine-secret"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XIANYU_DATA_KEY", "")
	// wrong 是未配置数据密钥的新仓储，无法解密已有令牌。
	wrong := db.NewStore(store.DB, db.DialectSQLite)
	// res、err 保存配置读取失败时的结果，期望返回错误且不泄漏令牌。
	res, err := NewAgentReplier("cid", wrong, &recordingDelivery{}, nil).Reply(ctx, chatMsg("x", "", "chat"))
	if res != nil || err == nil || strings.Contains(err.Error(), "engine-secret") {
		t.Fatalf("读取失败应返回不含令牌的错误: %+v %v", res, err)
	}
}

// TestAgentTurnRegistry 验证轮次登记：总时长上限、结束函数幂等、等待旧轮次期间 parent 取消。
func TestAgentTurnRegistry(t *testing.T) {
	// registry 是被测的轮次登记表，零值即可使用。
	var registry agentTurnRegistry
	// 总时长上限。
	ctx, finish, err := registry.begin(context.Background(), "chat", 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("应因总时长到期: %v", ctx.Err())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("总时长上限未生效")
	}
	finish()
	finish() // 幂等
	// 旧轮次迟迟不退出时，新轮次的等待受 parent 取消约束。
	oldCtx, oldFinish, _ := registry.begin(context.Background(), "chat", time.Minute)
	defer oldFinish()
	// parent、cancel 是会很快超时的父上下文及其取消函数，用来验证等待旧轮次期间受其约束。
	parent, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	// err 保存新轮次等待旧轮次时因父上下文超时而返回的错误。
	if _, _, err := registry.begin(parent, "chat", time.Minute); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("等待旧轮次时 parent 超时应返回其错误: %v", err)
	}
	if oldCtx.Err() == nil {
		t.Fatal("新轮次应取消旧轮次")
	}
}

// TestAgentHelpers 验证轮询间隔夹取与图片地址判断的边界。
func TestAgentHelpers(t *testing.T) {
	// in、want 是建议轮询间隔及其夹取后的期望值。
	for in, want := range map[time.Duration]time.Duration{
		0: 2 * time.Second, -1: 2 * time.Second, time.Millisecond: time.Second, 3 * time.Second: 3 * time.Second, time.Hour: 10 * time.Second,
	} {
		// got 是实际夹取结果。
		if got := clampPollInterval(in); got != want {
			t.Fatalf("clampPollInterval(%v)=%v want %v", in, got, want)
		}
	}
	// raw、want 是图片地址及其是否应被接受。
	for raw, want := range map[string]bool{"http://a.example.com/x": true, " HTTPS://a.example.com ": true, "ftp://a": false, "": false, "//a.example.com": false} {
		if isHTTPURL(raw) != want {
			t.Fatalf("isHTTPURL(%q)!=%v", raw, want)
		}
	}
}

// TestReplyServiceAgentPrecedence 验证完整回复链：关键词优先于 Agent；Agent 接手后不再发送默认回复；未配置 Agent 时默认回复照旧。
func TestReplyServiceAgentPrecedence(t *testing.T) {
	// f 是返回 Agent 回复的夹具，用于验证完整回复链优先级。
	f := newAgentFixture(t, scriptedResponse{body: `{"replies":[{"text":"Agent回复"}]}`})
	// ctx 是本用例共用的上下文。
	ctx := context.Background()
	f.store.DB.ExecContext(ctx, `INSERT INTO keywords (cookie_id,keyword,reply,type) VALUES ('cid','价格','关键词回复','text')`)
	// err 保存写入账号默认回复的错误。
	if err := f.store.DefaultReps.Upsert(ctx, "cid", db.DefaultReply{Enabled: true, ReplyContent: "默认回复"}); err != nil {
		t.Fatal(err)
	}
	// service 与 Agent 共用同一投递端口，便于观察完整发送序列。
	service := NewReplyService("cid", f.store, f.delivery, f.replier, nil, nil)
	// err 保存关键词命中场景的处理错误。
	if err := service.Handle(ctx, chatMsg("价格多少", "item", "chat-kw")); err != nil {
		t.Fatal(err)
	}
	if len(f.agent.seen()) != 0 || f.delivery.count() != 1 || f.delivery.messages[0].Text != "关键词回复" {
		t.Fatalf("关键词命中不应询问 Agent: requests=%d sent=%+v", len(f.agent.seen()), f.delivery.messages)
	}
	// err 保存 Agent 接手场景的处理错误。
	if err := service.Handle(ctx, chatMsg("随便聊聊", "item", "chat-agent")); err != nil {
		t.Fatal(err)
	}
	if f.delivery.count() != 2 || f.delivery.messages[1].Text != "Agent回复" {
		t.Fatalf("Agent 接手后不应再发默认回复: %+v", f.delivery.messages)
	}
	// 关闭 Agent 后默认回复照旧。
	if err := f.store.AgentReply.Upsert(ctx, "cid", db.AgentReplyWrite{Enabled: false, EndpointURL: "http://127.0.0.1:1", RequestTimeoutSeconds: 5, MaxSessionSeconds: 30}); err != nil {
		t.Fatal(err)
	}
	// err 保存 Agent 关闭后默认回复场景的处理错误。
	if err := service.Handle(ctx, chatMsg("再聊聊", "item", "chat-off")); err != nil {
		t.Fatal(err)
	}
	if f.delivery.count() != 3 || f.delivery.messages[2].Text != "默认回复" {
		t.Fatalf("关闭 Agent 后应走默认回复: %+v", f.delivery.messages)
	}
	// Agent 接手前失败降级到默认回复。
	failing := newAgentFixture(t, scriptedResponse{status: 500})
	failing.store.DefaultReps.Upsert(ctx, "cid", db.DefaultReply{Enabled: true, ReplyContent: "默认回复"})
	// failService 是 Agent 首个请求失败时的回复服务，用于验证降级到默认回复。
	failService := NewReplyService("cid", failing.store, failing.delivery, failing.replier, nil, nil)
	// err 保存 Agent 失败降级场景的处理错误。
	if err := failService.Handle(ctx, chatMsg("随便聊聊", "item", "chat-fail")); err != nil {
		t.Fatal(err)
	}
	if failing.delivery.count() != 1 || failing.delivery.messages[0].Text != "默认回复" {
		t.Fatalf("Agent 失败应降级到默认回复: %+v", failing.delivery.messages)
	}
}
