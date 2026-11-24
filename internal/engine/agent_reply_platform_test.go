package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"xianyu-go/internal/db"
)

// platformServer 是记录请求并按路径返回 Dify/FastGPT 响应的本地平台；handler 为空时返回固定的成功响应。
type platformServer struct {
	// mu 保护 requests。
	mu sync.Mutex
	// requests 按顺序保存收到的请求（路径与解析后的 JSON 正文）。
	requests []map[string]any
	// handler 可覆盖默认响应，参数是请求序号（从 0 起）；返回 false 表示使用默认响应。
	handler func(w http.ResponseWriter, index int, body map[string]any) bool
	// server 是底层测试服务端。
	server *httptest.Server
}

// newPlatformServer 创建本地平台；Dify 默认返回两段流式文字与会话标识 conv-1，FastGPT 默认返回一条非流式回复。
func newPlatformServer(t *testing.T) *platformServer {
	t.Helper()
	// p 是被构造的本地平台夹具。
	p := &platformServer{}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// raw、body 是请求原文与解析结果；path 记录到 body 里便于断言。
		raw, _ := io.ReadAll(r.Body)
		// body 是解析后的请求体或序列化后的字节。
		body := map[string]any{}
		_ = json.Unmarshal(raw, &body)
		body["_path"] = r.URL.Path
		p.mu.Lock()
		p.requests = append(p.requests, body)
		// index 是当前遍历的序号。
		index := len(p.requests) - 1
		p.mu.Unlock()
		if p.handler != nil && p.handler(w, index, body) {
			return
		}
		if strings.HasSuffix(r.URL.Path, "/chat-messages") {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"event\":\"message\",\"answer\":\"工作流\",\"conversation_id\":\"conv-1\"}\n\ndata: {\"event\":\"message\",\"answer\":\"回复\",\"conversation_id\":\"conv-1\"}\n\n")
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"知识库回复"}}]}`)
	}))
	t.Cleanup(p.server.Close)
	return p
}

// count 返回已收到的请求数。
func (p *platformServer) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

// at 返回第 index 个请求。
func (p *platformServer) at(index int) map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requests[index]
}

// TestAgentReplyViaDifyKeepsConversation 验证 Dify 模式：首轮开启新会话并保存 conversation_id，第二轮续用，
// 请求只带合并输入与派生的 user 标识（不含平台会话号原文），并只发送一条文字回复。
func TestAgentReplyViaDifyKeepsConversation(t *testing.T) {
	// platform 是记录请求并可定制响应的本地平台服务端。
	platform := newPlatformServer(t)
	// replier、delivery、store 是被测回复器、投递记录与测试库；地址只填域名根，验证自动补全 /v1。
	replier, delivery, store := newModelAgentFixture(t, platform.server.URL, db.AgentProtocolDify, "", "", false)
	// result、err 是首轮结果。
	result, err := replier.Reply(context.Background(), chatMsg("在吗", "item", "chat-secret"))
	if err != nil || result == nil || !result.Skip || delivery.count() != 1 || delivery.messages[0].Text != "工作流回复" {
		t.Fatalf("首轮: result=%+v err=%v sent=%v", result, err, delivery.messages)
	}
	// first 是首轮请求；key 是派生的会话标识。
	first, key := platform.at(0), platformSessionKey("cid", "chat-secret")
	if first["_path"] != "/v1/chat-messages" || first["conversation_id"] != "" || first["user"] != key || first["query"] != "在吗" || strings.Contains(key, "chat-secret") {
		t.Fatalf("首轮请求: %+v", first)
	}
	// saved 是数据库里已保存的平台会话标识，没有记录时为空。
	if saved, _ := store.AgentRemoteSessions.Get(context.Background(), "cid", "chat-secret"); saved != "conv-1" {
		t.Fatalf("应保存会话标识: %q", saved)
	}
	// 第二轮续用已保存的会话。
	if _, err = replier.Reply(context.Background(), chatMsg("再问一句", "item", "chat-secret")); err != nil {
		t.Fatal(err)
	}
	// second 是第二轮请求的记录。
	if second := platform.at(1); second["conversation_id"] != "conv-1" || second["query"] != "再问一句" {
		t.Fatalf("第二轮请求: %+v", second)
	}
}

// TestAgentReplyViaDifyRecoversFromStaleSession 验证已保存的会话在平台侧失效时：清除映射、用新会话重试一次并保存新标识。
func TestAgentReplyViaDifyRecoversFromStaleSession(t *testing.T) {
	// platform 是记录请求并可定制响应的本地平台服务端。
	platform := newPlatformServer(t)
	platform.handler = func(w http.ResponseWriter, index int, body map[string]any) bool {
		if body["conversation_id"] == "stale" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"code":"not_found","message":"Conversation Not Exists."}`)
			return true
		}
		return false
	}
	// delivery 记录实际发给买家的回复；replier 是被测 Agent 回复器；store 是临时测试数据库。
	replier, delivery, store := newModelAgentFixture(t, platform.server.URL+"/v1", db.AgentProtocolDify, "", "", false)
	// err 是该步骤的错误，期望为空或为被测的特定错误。
	if err := store.AgentRemoteSessions.Put(context.Background(), "cid", "chat-1", "stale"); err != nil {
		t.Fatal(err)
	}
	// err 是该步骤的错误，期望为空或为被测的特定错误。
	if _, err := replier.Reply(context.Background(), chatMsg("你好", "item", "chat-1")); err != nil || delivery.count() != 1 {
		t.Fatalf("失效会话应重试成功: err=%v sent=%d", err, delivery.count())
	}
	if platform.count() != 2 || platform.at(1)["conversation_id"] != "" {
		t.Fatalf("应先用 stale 再用新会话重试: %d", platform.count())
	}
	// saved 是数据库里已保存的平台会话标识，没有记录时为空。
	if saved, _ := store.AgentRemoteSessions.Get(context.Background(), "cid", "chat-1"); saved != "conv-1" {
		t.Fatalf("应保存新的会话标识: %q", saved)
	}
}

// TestAgentReplyViaPlatformFailuresFallThrough 验证平台出错、空回复时不接手（交给后续 AI/默认回复），地址非法返回错误。
func TestAgentReplyViaPlatformFailuresFallThrough(t *testing.T) {
	// platform 是记录请求并可定制响应的本地平台服务端。
	platform := newPlatformServer(t)
	// mode 控制本地平台的返回：error 返回 500，empty 返回没有文字的流。
	mode := "error"
	platform.handler = func(w http.ResponseWriter, _ int, _ map[string]any) bool {
		if mode == "error" {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			_, _ = io.WriteString(w, "data: {\"event\":\"message_end\"}\n\n")
		}
		return true
	}
	// delivery 记录实际发给买家的回复；replier 是被测 Agent 回复器。
	replier, delivery, _ := newModelAgentFixture(t, platform.server.URL, db.AgentProtocolDify, "", "", false)
	// result、err 是 500 时的结果：返回错误、不接手。
	result, err := replier.Reply(context.Background(), chatMsg("你好", "item", "c"))
	if err == nil || result != nil || delivery.count() != 0 {
		t.Fatalf("500 应不接手并返回错误: result=%+v err=%v", result, err)
	}
	mode = "empty"
	if result, err = replier.Reply(context.Background(), chatMsg("你好", "item", "c")); err != nil || result != nil || delivery.count() != 0 {
		t.Fatalf("空回复应不接手: result=%+v err=%v", result, err)
	}
	// 非法地址（出站策略拒绝）。
	bad, _, _ := newModelAgentFixture(t, "ftp://example.com", db.AgentProtocolDify, "", "", false)
	if result, err = bad.Reply(context.Background(), chatMsg("你好", "item", "c")); err == nil || result != nil {
		t.Fatalf("非法地址应返回错误: result=%+v err=%v", result, err)
	}
}

// TestAgentReplyViaDifyToleratesBrokenSessionStore 验证会话映射表不可用时仍能回复：读取、保存、清除失败都只记录日志，
// 并覆盖没有装配会话仓储的情形。
func TestAgentReplyViaDifyToleratesBrokenSessionStore(t *testing.T) {
	// platform 是记录请求并可定制响应的本地平台服务端。
	platform := newPlatformServer(t)
	// delivery 记录实际发给买家的回复；replier 是被测 Agent 回复器；store 是临时测试数据库。
	replier, delivery, store := newModelAgentFixture(t, platform.server.URL, db.AgentProtocolDify, "", "", false)
	// err 是该步骤的错误，期望为空或为被测的特定错误。
	if _, err := store.DB.Exec(`DROP TABLE agent_remote_sessions`); err != nil {
		t.Fatal(err)
	}
	// err 是该步骤的错误，期望为空或为被测的特定错误。
	if _, err := replier.Reply(context.Background(), chatMsg("你好", "item", "c")); err != nil || delivery.count() != 1 {
		t.Fatalf("映射表损坏时应仍能回复: err=%v sent=%d", err, delivery.count())
	}
	// 失效会话分支下清除映射失败：仍用新会话重试。
	platform.handler = func(w http.ResponseWriter, _ int, body map[string]any) bool {
		if body["conversation_id"] != "" {
			w.WriteHeader(http.StatusNotFound)
			return true
		}
		return false
	}
	// 没有装配会话仓储：每次都是新会话。
	store.AgentRemoteSessions = nil
	// err 是该步骤的错误，期望为空或为被测的特定错误。
	if _, err := replier.Reply(context.Background(), chatMsg("你好", "item", "c")); err != nil || delivery.count() != 2 {
		t.Fatalf("无会话仓储时应仍能回复: err=%v sent=%d", err, delivery.count())
	}
}

// TestAgentReplyViaDifyDeleteFailureStillRetries 验证清除失效会话映射失败时仍用新会话重试。
func TestAgentReplyViaDifyDeleteFailureStillRetries(t *testing.T) {
	// platform 是记录请求并可定制响应的本地平台服务端。
	platform := newPlatformServer(t)
	platform.handler = func(w http.ResponseWriter, _ int, body map[string]any) bool {
		if body["conversation_id"] == "stale" {
			w.WriteHeader(http.StatusNotFound)
			return true
		}
		return false
	}
	// delivery 记录实际发给买家的回复；replier 是被测 Agent 回复器；store 是临时测试数据库。
	replier, delivery, store := newModelAgentFixture(t, platform.server.URL, db.AgentProtocolDify, "", "", false)
	// err 是该步骤的错误，期望为空或为被测的特定错误。
	if err := store.AgentRemoteSessions.Put(context.Background(), "cid", "c", "stale"); err != nil {
		t.Fatal(err)
	}
	// 读取成功后把表改名使 Delete 与 Put 失败：先让 Get 读到 stale，再破坏表。
	platform.handler = func(w http.ResponseWriter, _ int, body map[string]any) bool {
		if body["conversation_id"] == "stale" {
			_, _ = store.DB.Exec(`DROP TABLE agent_remote_sessions`)
			w.WriteHeader(http.StatusNotFound)
			return true
		}
		return false
	}
	// err 是该步骤的错误，期望为空或为被测的特定错误。
	if _, err := replier.Reply(context.Background(), chatMsg("你好", "item", "c")); err != nil || delivery.count() != 1 {
		t.Fatalf("清除失败仍应重试并回复: err=%v sent=%d", err, delivery.count())
	}
}

// TestAgentReplyViaFastGPTUsesStableChatID 验证 FastGPT 模式：chatId 稳定且由账号与会话派生，不使用映射表，
// 图片被拒绝时降级为纯文字，回复只发送一条。
func TestAgentReplyViaFastGPTUsesStableChatID(t *testing.T) {
	// platform 是记录请求并可定制响应的本地平台服务端。
	platform := newPlatformServer(t)
	// delivery 记录实际发给买家的回复；replier 是被测 Agent 回复器；store 是临时测试数据库。
	replier, delivery, store := newModelAgentFixture(t, platform.server.URL+"/api", db.AgentProtocolFastGPT, "", "", false)
	// key 是由账号和会话号派生的稳定标识。
	key := platformSessionKey("cid", "chat-9")
	// round 是当前对话轮序号。
	for round := 0; round < 2; round++ {
		// err 是该步骤的错误，期望为空或为被测的特定错误。
		if _, err := replier.Reply(context.Background(), chatMsg("你好", "item", "chat-9")); err != nil {
			t.Fatal(err)
		}
	}
	if delivery.count() != 2 || delivery.messages[0].Text != "知识库回复" {
		t.Fatalf("应各发送一条回复: %v", delivery.messages)
	}
	// index 是当前遍历的序号。
	for index := 0; index < 2; index++ {
		// request 是第 index 次收到的请求记录。
		if request := platform.at(index); request["_path"] != "/api/v1/chat/completions" || request["chatId"] != key {
			t.Fatalf("第 %d 次请求: %+v", index, request)
		}
	}
	// saved 是数据库里已保存的平台会话标识，没有记录时为空。
	if saved, _ := store.AgentRemoteSessions.Get(context.Background(), "cid", "chat-9"); saved != "" {
		t.Fatalf("FastGPT 不应写会话映射: %q", saved)
	}
	// 买家图片被平台以 400 拒绝：降级为纯文字重试。
	platform.handler = func(w http.ResponseWriter, _ int, body map[string]any) bool {
		// ok 表示相应的存在性或类型判断是否成立；parts 是消息的 content 数组。
		if parts, ok := body["messages"].([]any)[0].(map[string]any)["content"].([]any); ok && len(parts) > 1 {
			w.WriteHeader(http.StatusBadRequest)
			return true
		}
		return false
	}
	// before 是降级重试前已收到的请求数。
	before := platform.count()
	// err 是该步骤的错误，期望为空或为被测的特定错误。
	if _, err := replier.Reply(context.Background(), imageMessage()); err != nil || platform.count() != before+2 || delivery.count() != 3 {
		t.Fatalf("图片降级: err=%v 请求数增量=%d sent=%d", err, platform.count()-before, delivery.count())
	}
}

// TestAgentReplyViaDifySupersededSendsNothing 验证请求进行中被买家新消息顶替：请求被取消、不发送任何回复，
// 也不保存会话标识（被中断的这一轮内容会随合并后的下一次请求重新发送，不会丢失）。
func TestAgentReplyViaDifySupersededSendsNothing(t *testing.T) {
	// platform 是记录请求并可定制响应的本地平台服务端。
	platform := newPlatformServer(t)
	// delivery 记录实际发给买家的回复；replier 是被测 Agent 回复器；store 是临时测试数据库。
	replier, delivery, store := newModelAgentFixture(t, platform.server.URL, db.AgentProtocolDify, "", "", false)
	// coordinator、ctx 是被测协调器与账号上下文；老轮次开始后，平台收到请求时又来了新消息。
	var coordinator replyTurnCoordinator
	// ctx 是本用例共用的上下文。
	ctx := context.Background()
	coordinator.enqueue(chatMsg("旧消息", "", "chat-s"))
	// parts 是消息的 content 数组；turn 是协调器开启的回复轮次。
	turn, parts := mustBegin(t, &coordinator, ctx, "chat-s")
	// message 是带轮次的合并消息；platform.handler 在请求到达服务端时让新消息顶替旧轮次并阻塞到连接被取消。
	message := mergeChatMessages(parts, turn)
	platform.handler = func(w http.ResponseWriter, _ int, _ map[string]any) bool {
		coordinator.enqueue(chatMsg("新消息", "", "chat-s"))
		<-turn.Context(ctx).Done()
		return true
	}
	// err 是该步骤的错误，期望为空或为被测的特定错误；result 是被测调用的返回结果。
	if result, err := replier.Reply(ctx, message); err != nil || result == nil || !result.Skip || delivery.count() != 0 {
		t.Fatalf("被顶替的轮次不应发送: result=%+v err=%v sent=%d", result, err, delivery.count())
	}
	// saved 是数据库里已保存的平台会话标识，没有记录时为空。
	if saved, _ := store.AgentRemoteSessions.Get(ctx, "cid", "chat-s"); saved != "" {
		t.Fatalf("被取消的请求不应保存会话标识: %q", saved)
	}
}
