package mtop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchChatUserInfoUsesConversationAndParsesIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api") != "mtop.taobao.idlemessage.pc.user.query" || r.URL.Query().Get("v") != "4.0" {
			t.Fatalf("query=%v", r.URL.Query())
		}
		if r.URL.Query().Get("spm_cnt") != "a21ybx.im.0.0" || r.URL.Query().Get("spm_pre") == "" || r.URL.Query().Get("log_id") == "" {
			t.Fatalf("missing official IM query context: %v", r.URL.Query())
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(r.Form.Get("data")), &payload); err != nil || payload["sessionId"] != "chat-1" || payload["isOwner"] != false {
			t.Fatalf("payload=%v err=%v", payload, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":["SUCCESS::调用成功"],"data":{"userInfo":{"fishNick":"闲鱼真实昵称","nick":"x***3","logo":"https://cdn/avatar.jpg"}}}`))
	}))
	defer server.Close()
	client := &ClientImpl{HTTPClient: server.Client(), ChatUserQueryURL: server.URL}
	info, err := client.FetchChatUserInfo(context.Background(), "unb=123; _m_h5_tk=token_1", "chat-1")
	if err != nil || info.Nickname != "闲鱼真实昵称" || info.AvatarURL != "https://cdn/avatar.jpg" {
		t.Fatalf("info=%+v err=%v", info, err)
	}
}

func TestFetchChatUserInfoFallsBackToNickWhenFishNickMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":["SUCCESS::调用成功"],"data":{"userInfo":{"nick":"兼容昵称","logo":"https://cdn/fallback.jpg"}}}`))
	}))
	defer server.Close()
	client := &ClientImpl{HTTPClient: server.Client(), ChatUserQueryURL: server.URL}
	info, err := client.FetchChatUserInfo(context.Background(), "unb=123; _m_h5_tk=token_1", "chat-2")
	if err != nil || info.Nickname != "兼容昵称" {
		t.Fatalf("info=%+v err=%v", info, err)
	}
}
