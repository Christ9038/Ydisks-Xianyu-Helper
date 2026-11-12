package adapter

import (
	"context"
	"strings"

	"xianyu-go/internal/engine"
)

// manualRequestTextLimit 是通知正文中买家原话保留的最大字符数，避免超长消息挤满手机推送预览。
const manualRequestTextLimit = 200

// RequestManualReply 把买家命中“通知人工”关键词的会话推送到账号绑定且订阅了“人工客服”类别的通知渠道。
// 正文只含买家昵称、会话与商品标识和买家原话，不含 Cookie、Token 等凭证；通知器未注入时只记录告警。
func (a *Adapter) RequestManualReply(ctx context.Context, request engine.ChatManualRequest) error {
	a.OnAccountEvent(ctx, request.AccountID, engine.EventChatManualRequested, engine.AlertLevelWarn, "买家请求人工客服", formatManualRequestBody(request))
	return nil
}

// formatManualRequestBody 生成“请求人工客服”通知正文；空字段省略，买家原话按字符截断。
func formatManualRequestBody(request engine.ChatManualRequest) string {
	// buyer 是通知中展示的买家称呼，优先昵称，缺失时回退平台用户标识。
	buyer := strings.TrimSpace(request.BuyerName)
	if buyer == "" {
		buyer = strings.TrimSpace(request.BuyerID)
	}
	if buyer == "" {
		buyer = "买家"
	}
	// lines 按展示顺序收集通知正文行。
	lines := []string{buyer + " 请求人工客服，请尽快在会话中回复。"}
	// chatID 是平台会话标识，便于在聊天页定位。
	if chatID := strings.TrimSpace(request.ChatID); chatID != "" {
		lines = append(lines, "会话: "+chatID)
	}
	// itemID 是会话关联的商品标识。
	if itemID := strings.TrimSpace(request.ItemID); itemID != "" {
		lines = append(lines, "商品: "+itemID)
	}
	// text 是按字符截断后的买家原话。
	if text := truncateRunes(strings.TrimSpace(request.Text), manualRequestTextLimit); text != "" {
		lines = append(lines, "买家消息: "+text)
	}
	return strings.Join(lines, "\n")
}

// truncateRunes 按 Unicode 字符截断文本，超出时追加省略号，避免把多字节中文截成乱码。
func truncateRunes(text string, limit int) string {
	// runes 是按字符拆分的文本。
	runes := []rune(text)
	if limit <= 0 || len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}
