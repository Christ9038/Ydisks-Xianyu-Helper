package mtop

import (
	"context"
	"fmt"
	"strings"
)

// ChatUserQueryAPI 保存聊天用户查询API，供当前处理流程使用
const ChatUserQueryAPI = "https://h5api.m.goofish.com/h5/mtop.taobao.idlemessage.pc.user.query/4.0/"

// ChatUserInfo 保存聊天用户Info，供当前处理流程使用
type ChatUserInfo struct {
	Nickname       string
	AvatarURL      string
	UpdatedCookies string
}

// ChatImageUpload 保存聊天图片Upload，供当前处理流程使用
type ChatImageUpload struct {
	URL            string
	Width          int
	Height         int
	UpdatedCookies string
}

// FetchChatUserInfo resolves the peer identity for a conversation. Xianyu's
// API expects the conversation id as sessionId rather than the user id.
// FetchChatUserInfo 负责Fetch聊天用户Info相关处理。
func (c *ClientImpl) FetchChatUserInfo(ctx context.Context, cookiesStr, chatID string) (*ChatUserInfo, error) {
	// decoded、updated、err 保存decoded、updated、err，供当前处理流程使用
	decoded, updated, err := c.accountTaskRequest(ctx, cookiesStr,
		firstNonEmptyURL(c.ChatUserQueryURL, ChatUserQueryAPI), "mtop.taobao.idlemessage.pc.user.query", "4.0",
		map[string]any{"type": 0, "sessionType": 1, "sessionId": strings.TrimSpace(chatID), "isOwner": false},
		"https://www.goofish.com/")
	if err != nil {
		return nil, err
	}
	// userInfo 保存用户Info，供当前处理流程使用
	userInfo, _ := decoded.Data["userInfo"].(map[string]any)
	if userInfo == nil {
		return nil, fmt.Errorf("会话用户接口响应缺少 userInfo")
	}
	// nickname 保存nickname，供当前处理流程使用
	nickname := strings.TrimSpace(mtopString(userInfo["fishNick"]))
	if nickname == "" {
		nickname = strings.TrimSpace(mtopString(userInfo["nick"]))
	}
	return &ChatUserInfo{Nickname: nickname,
		AvatarURL: strings.TrimSpace(mtopString(userInfo["logo"])), UpdatedCookies: updated}, nil
}

// UploadChatImage 负责Upload聊天图片相关处理。
func (c *ClientImpl) UploadChatImage(ctx context.Context, cookiesStr, filename, contentType string, data []byte) (*ChatImageUpload, error) {
	// uploaded、updated、err 保存uploaded、updated、err，供当前处理流程使用
	uploaded, updated, err := c.uploadPublishImage(ctx, cookiesStr, PublishImage{
		Filename: filename, ContentType: contentType, Data: data,
	})
	if err != nil {
		return nil, err
	}
	return &ChatImageUpload{URL: uploaded.URL, Width: uploaded.Width, Height: uploaded.Height, UpdatedCookies: updated}, nil
}
