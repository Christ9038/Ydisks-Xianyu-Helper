// reply.go 四级回复引擎：API → 关键词 → AI → 默认回复。
// 实现关键词回复、默认回复和 AI 回复的调度。
//
// Phase 3 实现：关键词（含商品ID优先+变量替换+空回复标记）、默认回复（指定商品优先+reply_once+变量替换）。
// API 回复（调外部 /xianyu/reply 接口）和 AI 回复（OpenAI 兼容）留接口注入。

package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/ws"
)

// ReplyResult 回复结果。
type ReplyResult struct {
	Text      string // 文本回复（可空）
	ImageURL  string // 图片回复（可空）
	Source    string // 回复来源：API/关键词/AI/默认
	Skip      bool   // true 表示匹配到空回复，不发送任何内容
	ReplyOnce bool   // 仅默认回复使用，发送状态由 Handle 持久化
	// AutoPriceQuote 是 AI 已明确承诺且通过价格边界校验的可执行报价。
	AutoPriceQuote *AIPriceQuoteProposal
}

// AIPriceQuoteProposal 是等待回复发送成功后绑定到会话的 AI 报价。
type AIPriceQuoteProposal struct {
	// PriceCents 是以分为单位的单件成交报价；订单执行时按已知购买数量折算总价。
	PriceCents int64
}

// aiQuoteValidity 是 AI 报价从成功发送开始允许自动应用到新订单的时长。
const aiQuoteValidity = 30 * time.Minute

// replyRecordPersistTimeout 是发送结果不确定时写入隔离状态允许使用的最长时间。
const replyRecordPersistTimeout = 5 * time.Second

// APIReplier 外部 API 回复（优先级1）。返回 nil 表示无回复。
type APIReplier interface {
	Reply(ctx context.Context, m ChatMessage) (*ReplyResult, error)
}

// AIReplier AI 回复（优先级3）。返回 nil 表示无回复。
type AIReplier interface {
	Reply(ctx context.Context, m ChatMessage) (*ReplyResult, error)
}

// ReplyMessage 是引擎生成并交给聊天应用的一条完整回复消息。
type ReplyMessage struct {
	// AccountID 是回复所属账号标识。
	AccountID string
	// ChatID 是目标聊天会话标识。
	ChatID string
	// ToUserID 是回复接收方的平台用户标识。
	ToUserID string
	// Text 是可选的文字回复。
	Text string
	// ImageURL 是可选的来源图片地址；上传和尺寸处理由聊天应用完成。
	ImageURL string
}

// ReplySendResult 是聊天应用投递完整回复后的分段确认结果。
type ReplySendResult struct {
	// ImageSent 表示图片已由平台确认接收。
	ImageSent bool
	// TextSent 表示文字已由平台确认接收。
	TextSent bool
	// Uncertain 表示平台动作可能已经发生，调用方不得安全重试。
	Uncertain bool
}

// ReplyDelivery 是回复服务使用的完整消息投递端口。
type ReplyDelivery interface {
	// SendReply 发送完整回复；实现方负责复用消息页面的图片上传和文字发送规则。
	SendReply(ctx context.Context, message ReplyMessage) (ReplySendResult, error)
}

// ReplyService 单账号回复服务。
type ReplyService struct {
	cookieID string
	store    *db.Store
	api      APIReplier // 可为 nil
	ai       AIReplier  // 可为 nil
	// delivery 是生产自动回复使用的聊天应用完整消息发送端口。
	delivery ReplyDelivery
	logger   *slog.Logger
}

// NewReplyService 构造。
func NewReplyService(cookieID string, store *db.Store, delivery ReplyDelivery,
	api APIReplier, ai AIReplier, logger *slog.Logger) *ReplyService {
	return newReplyService(cookieID, store, delivery, api, ai, logger)
}

// newReplyService 统一构造回复解析和完整消息投递依赖。
func newReplyService(cookieID string, store *db.Store, delivery ReplyDelivery, api APIReplier, ai AIReplier, logger *slog.Logger) *ReplyService {
	if logger == nil {
		logger = slog.Default()
	}
	// service 保存统一装配的回复解析、状态存储和消息投递端口。
	service := &ReplyService{
		cookieID: cookieID,
		store:    store,
		api:      api,
		ai:       ai,
		delivery: delivery,
		logger:   logger.With("account", cookieID, "subsys", "reply"),
	}
	return service
}

// Handle 收到一条聊天消息，按四级优先级回复。
// 由 Account 在防抖后调用。返回是否产生了回复。
// Handle 处理当前值。
func (r *ReplyService) Handle(ctx context.Context, m ChatMessage) error {
	// res 用于本次流程后续判断的响应
	res := r.resolve(ctx, m)
	if res == nil || res.Skip {
		return nil
	}
	// 发送：图片优先，文本随后。reply_once 使用持久化分段状态，失败时只重试
	// 尚未成功的部分。
	if r.delivery == nil {
		return nil
	}
	// record 用于本次流程后续判断的record
	record := db.DefaultReplyRecord{}
	if res.ReplyOnce && m.ChatID != "" {
		// claimed 用于本次流程后续判断的claimed
		var claimed bool
		// err 用于本次流程后续判断的err
		var err error
		record, claimed, err = r.store.DefaultReps.ClaimRecord(ctx, r.cookieID, m.ChatID, res.Text != "", res.ImageURL != "")
		if err != nil {
			return fmt.Errorf("领取默认回复发送任务: %w", err)
		}
		if !claimed {
			return nil
		}
	}
	return r.handleWithDelivery(ctx, m, res, record)
}

// handleWithDelivery 把完整回复交给聊天应用，并把其分段结果同步到 reply_once 状态。
func (r *ReplyService) handleWithDelivery(ctx context.Context, m ChatMessage, res *ReplyResult, record db.DefaultReplyRecord) error {
	// message 保存本次仍需发送的完整回复；已成功的 reply_once 分段会被剔除。
	message := ReplyMessage{AccountID: r.cookieID, ChatID: m.ChatID, ToUserID: m.SenderUserID, Text: res.Text, ImageURL: res.ImageURL}
	if record.ImageSent {
		message.ImageURL = ""
	}
	if record.TextSent {
		message.Text = ""
	}
	if strings.TrimSpace(message.Text) == "" && strings.TrimSpace(message.ImageURL) == "" {
		return nil
	}
	// sent、sendErr 保存消息页面完整发送入口返回的分段确认和错误。
	sent, sendErr := r.delivery.SendReply(ctx, message)
	if sent.ImageSent && !record.ImageSent && res.ReplyOnce && m.ChatID != "" {
		// markErr 保存图片分段状态持久化结果。
		if markErr := r.store.DefaultReps.MarkPartSent(ctx, r.cookieID, m.ChatID, "image"); markErr != nil {
			r.markReplyUncertain(ctx, res, m, markErr)
			return markErr
		}
	}
	if sent.TextSent && !record.TextSent && res.ReplyOnce && m.ChatID != "" {
		// markErr 保存文字分段状态持久化结果。
		if markErr := r.store.DefaultReps.MarkPartSent(ctx, r.cookieID, m.ChatID, "text"); markErr != nil {
			r.markReplyUncertain(ctx, res, m, markErr)
			return markErr
		}
	}
	if sendErr != nil {
		r.logger.Error("通过聊天应用发送回复失败", "err", sendErr)
		if sent.Uncertain {
			r.markReplyUncertain(ctx, res, m, sendErr)
			return sendErr
		}
		// persistErr 保存确定未发送状态写入错误。
		if persistErr := r.markReplyFailure(ctx, res, m, sendErr); persistErr != nil {
			return errors.Join(sendErr, persistErr)
		}
		return sendErr
	}
	if sent.TextSent && res.Source == "AI" && res.AutoPriceQuote != nil && m.ChatID != "" && m.SenderUserID != "" && m.ItemID != "" {
		// quote 保存完整回复文字已确认后才允许执行的 AI 报价。
		quote := db.AIBargainQuote{CookieID: r.cookieID, ChatID: m.ChatID, BuyerID: m.SenderUserID, ItemID: m.ItemID, PriceCents: res.AutoPriceQuote.PriceCents}
		// expiresAt 保存固定 30 分钟报价有效期的 Unix 秒时间。
		expiresAt := time.Now().UTC().Add(aiQuoteValidity).Unix()
		// quoteErr 保存已发送报价写入失败原因。
		if quoteErr := r.store.AIReply.ReplacePendingQuote(ctx, quote, expiresAt); quoteErr != nil {
			return fmt.Errorf("保存已发送的 AI 自动改价报价: %w", quoteErr)
		}
	}
	if res.ReplyOnce && m.ChatID != "" {
		// recordErr 保存完整回复记录收口结果。
		if recordErr := r.store.DefaultReps.MarkRecordSent(ctx, r.cookieID, m.ChatID); recordErr != nil {
			r.markReplyUncertain(ctx, res, m, recordErr)
			return recordErr
		}
	}
	return nil
}

// markReplyFailure 持久化确定未发送的默认回复失败状态，并反馈状态写入错误。
func (r *ReplyService) markReplyFailure(ctx context.Context, res *ReplyResult, m ChatMessage, sendErr error) error {
	if res.ReplyOnce && m.ChatID != "" {
		// transportErr 只接受聊天传输层明确声明的未知发送结果；普通本地错误仍允许重试。
		var transportErr *ws.SendError
		if errors.As(sendErr, &transportErr) && ws.SendResultKind(sendErr) == ws.SendUncertain {
			r.markReplyUncertain(ctx, res, m, sendErr)
			return nil
		}
		// persistCtx 使用独立的短超时，保证发送方取消上下文不会遗留不可领取的 sending 状态。
		persistCtx, cancel := context.WithTimeout(context.Background(), replyRecordPersistTimeout)
		defer cancel()
		// persistErr 保存确定未发送状态写入结果。
		if persistErr := r.store.DefaultReps.MarkRecordFailed(persistCtx, r.cookieID, m.ChatID, sendErr.Error()); persistErr != nil {
			return fmt.Errorf("保存默认回复失败状态: %w", persistErr)
		}
	}
	return nil
}

// markReplyUncertain 在消息已经可能送达后本地状态写入失败时隔离一次性默认回复。
func (r *ReplyService) markReplyUncertain(ctx context.Context, res *ReplyResult, m ChatMessage, cause error) {
	if res.ReplyOnce && m.ChatID != "" {
		// persistCtx 让取消的发送上下文不会阻止未知结果进入隔离状态。
		persistCtx, cancel := context.WithTimeout(context.Background(), replyRecordPersistTimeout)
		defer cancel()
		// persistErr 保存未知投递结果隔离失败，便于运维发现无法持久化的人工核对状态。
		if persistErr := r.store.DefaultReps.MarkRecordUncertain(persistCtx, r.cookieID, m.ChatID, cause.Error()); persistErr != nil {
			r.logger.Error("隔离未知默认回复结果失败", "err", persistErr)
		}
	}
}

// resolve 按优先级确定回复内容（不发送）。
func (r *ReplyService) resolve(ctx context.Context, m ChatMessage) *ReplyResult {
	// 优先级1：API 回复。
	if r.api != nil {
		if // res、err 用于本次流程后续判断的res、err
		res, err := r.api.Reply(ctx, m); err != nil {
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
		if // res、err 用于本次流程后续判断的res、err
		res, err := r.ai.Reply(ctx, m); err != nil {
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
// keywordReply 封装关键词回复业务协调。
func (r *ReplyService) keywordReply(ctx context.Context, m ChatMessage) *ReplyResult {
	// kws、err 保存当前账号的关键词规则及数据库读取错误。
	kws, err := r.store.Keywords.AllWithType(ctx, r.cookieID)
	if err != nil || len(kws) == 0 {
		return nil
	}

	// 1. 商品ID关键词优先；一条规则可关联多个商品，命中任一即可。
	if m.ItemID != "" {
		// kw 表示当前遍历过程中的关键词规则。
		for _, kw := range kws {
			if containsItemID(kw.ItemID, m.ItemID) && r.keywordMatches(kw, m.Text) {
				return r.keywordResult(kw, m)
			}
		}
	}
	// 2. 通用关键词（无 item_id）。
	// kw 表示当前遍历过程中的账号级关键词规则。
	for _, kw := range kws {
		if kw.ItemID == "" && r.keywordMatches(kw, m.Text) {
			return r.keywordResult(kw, m)
		}
	}
	return nil
}

// keywordMatches 判断一条规则的多个表达式是否命中消息文本。
// contains 模式保持历史大小写不敏感的字面包含语义；regexp 模式使用 Go/RE2，
// 单个遗留非法表达式只记录并跳过，不影响同一规则的其他表达式或后续降级。
func (r *ReplyService) keywordMatches(kw db.Keyword, text string) bool {
	// expressions 保存兼容数据库行转换出的匹配表达式集合。
	expressions := kw.Expressions
	if len(expressions) == 0 && strings.TrimSpace(kw.Keyword) != "" {
		expressions = []string{kw.Keyword}
	}
	// matched、invalidIndexes、unknownMode 保存纯匹配结果和需要对外记录的稳定诊断。
	matched, invalidIndexes, unknownMode := matchKeywordExpressions(expressions, kw.MatchType, text)
	// logger 保存带账号上下文的诊断日志端口；正常构造路径总会提供该端口。
	logger := r.logger
	if logger == nil {
		logger = slog.Default().With("account", r.cookieID, "subsys", "reply")
	}
	// expressionIndex 表示发生编译失败的表达式零基位置。
	for _, expressionIndex := range invalidIndexes {
		// errorType 只记录稳定的诊断类别，避免把表达式正文和底层编译文本写入日志。
		errorType := "regexp_compile"
		logger.Warn("关键词正则编译失败，已跳过", "expression_index", expressionIndex, "error_type", errorType)
	}
	if unknownMode {
		// errorType 只标识未知匹配模式，不记录调用方传入的原始模式文本。
		errorType := "unknown_match_type"
		logger.Warn("关键词匹配模式未知，已跳过", "error_type", errorType)
	}
	return matched
}

// matchKeywordExpressions 对表达式集合执行无副作用的 OR 匹配。
// expressions 是规则表达式，matchType 是规范或历史匹配模式，text 是待匹配消息；返回命中结果、非法正则的零基位置和未知模式标记。
func matchKeywordExpressions(expressions []string, matchType, text string) (bool, []int, bool) {
	// normalizedMatchType 保存兼容历史模式后的匹配模式。
	normalizedMatchType := strings.ToLower(strings.TrimSpace(matchType))
	if normalizedMatchType == "" || normalizedMatchType == "fuzzy" || normalizedMatchType == "exact" {
		normalizedMatchType = "contains"
	}
	if normalizedMatchType != "contains" && normalizedMatchType != "regexp" {
		return false, nil, true
	}
	// invalidIndexes 保存编译失败但不阻断同规则其他表达式的零基位置。
	invalidIndexes := make([]int, 0)
	// expressionIndex、rawExpression 表示当前遍历的表达式位置和原始文本。
	for expressionIndex, rawExpression := range expressions {
		// expression 是去除边界空白后的匹配表达式。
		expression := strings.TrimSpace(rawExpression)
		if expression == "" {
			continue
		}
		if normalizedMatchType == "regexp" {
			// pattern 是保持默认大小写不敏感且允许 RE2 内联标志覆盖的包装表达式。
			pattern := "(?i:" + expression + ")"
			// compiled、compileErr 保存当前正则的编译结果。
			compiled, compileErr := regexp.Compile(pattern)
			if compileErr != nil {
				invalidIndexes = append(invalidIndexes, expressionIndex)
				continue
			}
			if compiled.MatchString(text) {
				return true, invalidIndexes, false
			}
			continue
		}
		if strings.Contains(strings.ToLower(text), strings.ToLower(expression)) {
			return true, invalidIndexes, false
		}
	}
	return false, invalidIndexes, false
}

// containsItemID 判断规则的商品范围字段是否覆盖目标商品标识。
// 字段为逗号分隔的商品集合时命中任一元素即视为覆盖；空字段不会匹配任何商品。
// 引擎直接读取持久化字段，故分隔符必须与关键词应用服务的存储约定保持一致。
func containsItemID(rawItemIDs string, target string) bool {
	// itemID 表示规则商品范围中的单个商品标识。
	for _, itemID := range strings.Split(rawItemIDs, ",") {
		if strings.TrimSpace(itemID) == target {
			return true
		}
	}
	return false
}

// keywordResult 封装关键词结果业务协调。
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
// defaultReply 封装default回复业务协调。
func (r *ReplyService) defaultReply(ctx context.Context, m ChatMessage) *ReplyResult {
	// 1. 指定商品回复。
	if m.ItemID != "" {
		if // ir、err 用于本次流程后续判断的ir、err
		ir, err := r.store.ItemReps.Get(ctx, r.cookieID, m.ItemID); err == nil && ir != nil && strings.TrimSpace(ir.ReplyContent) != "" {
			return &ReplyResult{Text: formatReplyWithItem(ir.ReplyContent, m), Source: "默认"}
		}
	}
	// 2. 账号默认回复。
	dr, err := r.store.DefaultReps.Get(ctx, r.cookieID)
	if err != nil || dr == nil || !dr.Enabled {
		return nil
	}
	// 文字和图片都为空 → 空回复标记。
	if strings.TrimSpace(dr.ReplyContent) == "" && strings.TrimSpace(dr.ReplyImageURL) == "" {
		return &ReplyResult{Skip: true, Source: "默认"}
	}
	// res 用于本次流程后续判断的响应
	res := &ReplyResult{Source: "默认", ReplyOnce: dr.ReplyOnce}
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
// formatReply 封装format回复业务协调。
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
	// out 用于本次流程后续判断的out
	out := template
	// k、v 表示当前遍历过程中的k、v
	for k, v := range vars {
		out = strings.ReplaceAll(out, "{"+k+"}", v)
	}
	return out
}
