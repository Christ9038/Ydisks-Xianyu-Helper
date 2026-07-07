// reply.go 四级回复引擎：API → 关键词 → AI → 默认回复。
// 实现关键词回复、默认回复和 AI 回复的调度。
//
// Phase 3 实现：关键词（含商品ID优先+变量替换+空回复标记）、默认回复（指定商品优先+reply_once+变量替换）。
// API 回复（调外部 /xianyu/reply 接口）和 AI 回复（OpenAI 兼容）留接口注入。

package engine

import (
	"context"
	"log/slog"
	"strings"

	"xianyu-go/internal/db"
)

// ReplyResult 回复结果。
type ReplyResult struct {
	Text     string // 文本回复（可空）
	ImageURL string // 图片回复（可空）
	Source   string // 回复来源：API/关键词/AI/默认
	Skip     bool   // true 表示匹配到空回复，不发送任何内容
}

// APIReplier 外部 API 回复（优先级1）。返回 nil 表示无回复。
type APIReplier interface {
	Reply(ctx context.Context, m ChatMessage) (*ReplyResult, error)
}

// AIReplier AI 回复（优先级3）。返回 nil 表示无回复。
type AIReplier interface {
	Reply(ctx context.Context, m ChatMessage) (*ReplyResult, error)
}

// MessageSender 是回复服务发送文本/图片所需的最小接口。
type MessageSender interface {
	SendText(ctx context.Context, chatID, toUserID, text string) error
	SendImage(ctx context.Context, chatID, toUserID, imageURL string, cardID int64) error
}

// ReplyService 单账号回复服务。
type ReplyService struct {
	cookieID string
	store    *db.Store
	api      APIReplier // 可为 nil
	ai       AIReplier  // 可为 nil
	sender   MessageSender
	logger   *slog.Logger
}

// NewReplyService 构造。
func NewReplyService(cookieID string, store *db.Store, sender MessageSender,
	api APIReplier, ai AIReplier, logger *slog.Logger) *ReplyService {
	if logger == nil {
		logger = slog.Default()
	}
	return &ReplyService{
		cookieID: cookieID,
		store:    store,
		api:      api,
		ai:       ai,
		sender:   sender,
		logger:   logger.With("account", cookieID, "subsys", "reply"),
	}
}

// Handle 收到一条聊天消息，按四级优先级回复。
// 由 Account 在防抖后调用。返回是否产生了回复。
func (r *ReplyService) Handle(ctx context.Context, m ChatMessage) error {
	res := r.resolve(ctx, m)
	if res == nil || res.Skip {
		return nil
	}
	// 发送：图片优先，文本随后。
	if r.sender == nil {
		return nil
	}
	if res.ImageURL != "" {
		if err := r.sender.SendImage(ctx, m.ChatID, m.SenderUserID, res.ImageURL, 0); err != nil {
			r.logger.Error("发送回复图片失败", "err", err)
		}
	}
	if res.Text != "" {
		if err := r.sender.SendText(ctx, m.ChatID, m.SenderUserID, res.Text); err != nil {
			r.logger.Error("发送回复文本失败", "err", err)
		}
	}
	return nil
}

// resolve 按优先级确定回复内容（不发送）。
func (r *ReplyService) resolve(ctx context.Context, m ChatMessage) *ReplyResult {
	// 优先级1：API 回复。
	if r.api != nil {
		if res, err := r.api.Reply(ctx, m); err != nil {
			r.logger.Error("API 回复失败", "err", err)
		} else if res != nil {
			res.Source = "API"
			return res
		}
	}

	// 优先级2：关键词匹配。
	if res := r.keywordReply(ctx, m); res != nil {
		return res
	}

	// 优先级3：AI 回复。
	if r.ai != nil {
		if res, err := r.ai.Reply(ctx, m); err != nil {
			r.logger.Error("AI 回复失败", "err", err)
		} else if res != nil {
			res.Source = "AI"
			return res
		}
	}

	// 优先级4：默认回复。
	return r.defaultReply(ctx, m)
}

// keywordReply 关键词匹配。返回 nil 表示无匹配；
// 返回 Skip=true 表示匹配到空回复（不发送）。
// 移植自 get_keyword_reply：商品ID关键词优先 → 通用关键词。
func (r *ReplyService) keywordReply(ctx context.Context, m ChatMessage) *ReplyResult {
	kws, err := r.store.Keywords.AllWithType(ctx, r.cookieID)
	if err != nil || len(kws) == 0 {
		return nil
	}
	msgLower := strings.ToLower(m.Text)

	// 1. 商品ID关键词优先。
	if m.ItemID != "" {
		for _, kw := range kws {
			if kw.ItemID == m.ItemID && strings.Contains(msgLower, strings.ToLower(kw.Keyword)) {
				return r.keywordResult(kw, m)
			}
		}
	}
	// 2. 通用关键词（无 item_id）。
	for _, kw := range kws {
		if kw.ItemID == "" && strings.Contains(msgLower, strings.ToLower(kw.Keyword)) {
			return r.keywordResult(kw, m)
		}
	}
	return nil
}

func (r *ReplyService) keywordResult(kw db.Keyword, m ChatMessage) *ReplyResult {
	if kw.Type == "image" && kw.ImageURL != "" {
		return &ReplyResult{ImageURL: kw.ImageURL, Source: "关键词"}
	}
	if strings.TrimSpace(kw.Reply) == "" {
		return &ReplyResult{Skip: true, Source: "关键词"} // EMPTY_REPLY
	}
	return &ReplyResult{Text: formatReply(kw.Reply, m), Source: "关键词"}
}

// defaultReply 默认回复。移植自 get_default_reply：
// 指定商品回复优先 → 账号默认回复（reply_once 防重复 + 变量替换）。
func (r *ReplyService) defaultReply(ctx context.Context, m ChatMessage) *ReplyResult {
	// 1. 指定商品回复。
	if m.ItemID != "" {
		if ir, err := r.store.ItemReps.Get(ctx, r.cookieID, m.ItemID); err == nil && ir != nil && strings.TrimSpace(ir.ReplyContent) != "" {
			return &ReplyResult{Text: formatReplyWithItem(ir.ReplyContent, m), Source: "默认"}
		}
	}
	// 2. 账号默认回复。
	dr, err := r.store.DefaultReps.Get(ctx, r.cookieID)
	if err != nil || dr == nil || !dr.Enabled {
		return nil
	}
	// reply_once：已回复过则跳过。
	if dr.ReplyOnce && m.ChatID != "" {
		if r.store.DefaultReps.HasRecord(ctx, r.cookieID, m.ChatID) {
			r.logger.Info("chat_id 已用过默认回复，跳过（reply_once）", "chat_id", m.ChatID)
			return nil
		}
	}
	// 文字和图片都为空 → 空回复标记。
	if strings.TrimSpace(dr.ReplyContent) == "" && strings.TrimSpace(dr.ReplyImageURL) == "" {
		return &ReplyResult{Skip: true, Source: "默认"}
	}
	// 记录已回复。
	if dr.ReplyOnce && m.ChatID != "" {
		if err := r.store.DefaultReps.AddRecord(ctx, r.cookieID, m.ChatID); err != nil {
			r.logger.Error("记录默认回复失败", "cookie_id", r.cookieID, "chat_id", m.ChatID, "err", err)
		}
	}
	res := &ReplyResult{Source: "默认"}
	if strings.TrimSpace(dr.ReplyContent) != "" {
		res.Text = formatReply(dr.ReplyContent, m)
	}
	if strings.TrimSpace(dr.ReplyImageURL) != "" {
		res.ImageURL = dr.ReplyImageURL
	}
	return res
}

// formatReply 变量替换：{send_user_name} {send_user_id} {send_message}。
// 替换回复模板变量；若替换出错则返回原文。
func formatReply(template string, m ChatMessage) string {
	return safeFormat(template, map[string]string{
		"send_user_name": m.SenderName,
		"send_user_id":   m.SenderUserID,
		"send_message":   m.Text,
	})
}

// formatReplyWithItem 含 {item_id} 变量。
func formatReplyWithItem(template string, m ChatMessage) string {
	return safeFormat(template, map[string]string{
		"send_user_name": m.SenderName,
		"send_user_id":   m.SenderUserID,
		"send_message":   m.Text,
		"item_id":        m.ItemID,
	})
}

// safeFormat 实现命名占位符替换。
func safeFormat(template string, vars map[string]string) string {
	out := template
	for k, v := range vars {
		out = strings.ReplaceAll(out, "{"+k+"}", v)
	}
	return out
}
