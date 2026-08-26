package mtop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestFetchChatUserInfoUsesConversationAndParsesIdentity 封装TestFetch聊天用户InfoUsesConversationAndParsesIdentity业务协调。
func TestFetchChatUserInfoUsesConversationAndParsesIdentity(t *testing.T) {
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api") != "mtop.taobao.idlemessage.pc.user.query" || r.URL.Query().Get("v") != "4.0" {
			t.Fatalf("query=%v", r.URL.Query())
		}
		if r.URL.Query().Get("spm_cnt") != "a21ybx.im.0.0" || r.URL.Query().Get("spm_pre") == "" || r.URL.Query().Get("log_id") == "" {
			t.Fatalf("missing official IM query context: %v", r.URL.Query())
		}
		if // err 用于本次流程后续判断的err
		err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		// payload 用于本次流程后续判断的请求载荷
		var payload map[string]any
		if // err 用于本次流程后续判断的err
		err := json.Unmarshal([]byte(r.Form.Get("data")), &payload); err != nil || payload["sessionId"] != "chat-1" || payload["isOwner"] != false {
			t.Fatalf("payload=%v err=%v", payload, err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":["SUCCESS::调用成功"],"data":{"userInfo":{"fishNick":"闲鱼真实昵称","nick":"x***3","logo":"https://cdn/avatar.jpg"}}}`))
	}))
	defer server.Close()
	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), ChatUserQueryURL: server.URL}
	// info、err 用于本次流程后续判断的info、err
	info, err := client.FetchChatUserInfo(context.Background(), "unb=123; _m_h5_tk=token_1", "chat-1")
	if err != nil || info.Nickname != "闲鱼真实昵称" || info.AvatarURL != "https://cdn/avatar.jpg" {
		t.Fatalf("info=%+v err=%v", info, err)
	}
}

// TestFetchChatUserInfoFallsBackToNickWhenFishNickMissing 封装TestFetch聊天用户InfoFallsBackToNickWhenFishNickMissing业务协调。
func TestFetchChatUserInfoFallsBackToNickWhenFishNickMissing(t *testing.T) {
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ret":["SUCCESS::调用成功"],"data":{"userInfo":{"nick":"兼容昵称","logo":"https://cdn/fallback.jpg"}}}`))
	}))
	defer server.Close()
	// client 用于本次流程后续判断的client
	client := &ClientImpl{HTTPClient: server.Client(), ChatUserQueryURL: server.URL}
	// info、err 用于本次流程后续判断的info、err
	info, err := client.FetchChatUserInfo(context.Background(), "unb=123; _m_h5_tk=token_1", "chat-2")
	if err != nil || info.Nickname != "兼容昵称" {
		t.Fatalf("info=%+v err=%v", info, err)
	}
}

// TestUploadChatImageMapsUploadedMedia 覆盖聊天图片上传入口对图片地址、尺寸和更新 Cookie 的映射。
func TestUploadChatImageMapsUploadedMedia(t *testing.T) {
	// server 保存图片上传响应的本地 HTTP 服务。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "fresh_upload", Path: "/"})
		_, _ = w.Write([]byte(`{"object":{"url":"https://cdn/chat.png","pix":"640x480"}}`))
	}))
	defer server.Close()
	// transport 保存把固定上传地址重写到本地服务的传输适配器。
	transport := &rewriteTransport{base: server.Client().Transport, target: server.URL}
	// client 保存聊天图片上传入口使用的 MTOP 客户端。
	client := &ClientImpl{HTTPClient: &http.Client{Transport: transport}}
	// upload、uploadErr 保存聊天图片上传结果和错误。
	upload, uploadErr := client.UploadChatImage(context.Background(), consignCookies, "chat.png", "image/png", tinyPNG(t))
	if uploadErr != nil || upload.URL != "https://cdn/chat.png" || upload.Width != 640 || upload.Height != 480 || !strings.Contains(upload.UpdatedCookies, "fresh_upload") {
		t.Fatalf("聊天图片上传异常 upload=%+v err=%v", upload, uploadErr)
	}
}
