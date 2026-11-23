package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"xianyu-go/internal/agentclient"
	"xianyu-go/internal/db"
	"xianyu-go/internal/llmclient"
	"xianyu-go/internal/netguard"
)

// 以下常量是单轮 Agent 对话的硬性上限，防止外部 Agent 造成无限轮询或回复风暴。
const (
	// agentMaxRequestsPerTurn 是单轮对话最多向 Agent 发出的请求数（含首个 message 请求）。
	agentMaxRequestsPerTurn = 60
	// agentMaxRepliesPerTurn 是单轮对话最多向买家发送的回复条数，超出部分丢弃。
	agentMaxRepliesPerTurn = 10
	// agentMaxImagesPerTurn 是单轮对话最多发送的图片条数；图片需要下载与上传，数量单独限制。
	agentMaxImagesPerTurn = 3
	// agentDefaultPollInterval 是 Agent 未指定 poll_after_ms 时的轮询间隔。
	agentDefaultPollInterval = 2 * time.Second
	// agentMinPollInterval 与 agentMaxPollInterval 是 Agent 建议的轮询间隔被夹取的区间。
	agentMinPollInterval = time.Second
	agentMaxPollInterval = 10 * time.Second
	// agentImagePlaceholder 是平台对买家图片消息使用的占位文字。
	agentImagePlaceholder = "[图片]"
	// agentReplySource 是 Agent 回复在日志中的来源标签。
	agentReplySource = "Agent"
)

// AgentReplier 把买家消息转发给用户自建的外部 Agent，并把 Agent 的回复逐条交给聊天应用发送。
//
// 它实现 APIReplier：未启用、未配置或 Agent 明确不接手（handled=false）时返回 nil 让后续 AI/默认回复接管；
// 一旦接手，回复由本类型自己发送并返回 Skip，避免下游重复回复。它永远不产生 AutoPriceQuote，
// 外部系统因此无法绕过折扣边界改价。
//
// 并发：同一会话的单飞与新消息顶替由 ReplyService 的 replyTurnCoordinator 负责（经 ChatMessage.Turn 传入）；
// 配置每次从数据库读取，修改后无需重启账号。
type AgentReplier struct {
	// cookieID 是所属账号标识。
	cookieID string
	// store 提供 Agent 配置；只读取，不写入。
	store *db.Store
	// orders 是买家订单阶段只读端口，仅在账号开启“携带订单阶段”时使用；为 nil 表示订单阶段不可用。
	orders buyerOrderFactsFinder
	// delivery 是完整消息投递端口，与普通自动回复共用图片上传和文字发送规则。
	delivery ReplyDelivery
	// logger 带账号字段；不得记录 Agent 地址、令牌和消息正文。
	logger *slog.Logger
	// pollSleep 是轮询等待函数，遵守 ctx 取消；测试可替换以免真实等待。
	pollSleep func(ctx context.Context, d time.Duration) error
}

// NewAgentReplier 构造账号的 Agent 回复器；delivery 为空时回复器始终不接手。
func NewAgentReplier(cookieID string, store *db.Store, delivery ReplyDelivery, logger *slog.Logger) *AgentReplier {
	if logger == nil {
		logger = slog.Default()
	}
	return &AgentReplier{
		cookieID: cookieID, store: store, delivery: delivery, orders: orderFactsFinderFromStore(store),
		logger:    logger.With("account", cookieID, "subsys", "agent"),
		pollSleep: sleepCtx,
	}
}

// Reply 实现 APIReplier：返回 nil 表示不接手；返回 Skip 表示已接手且已自行发送。
func (a *AgentReplier) Reply(ctx context.Context, m ChatMessage) (*ReplyResult, error) {
	if a == nil || a.store == nil || a.store.AgentReply == nil || a.delivery == nil || m.ChatID == "" {
		return nil, nil
	}
	// cfg、err 保存包含已解密令牌的运行时配置；未配置返回 nil 让后续规则接管。
	cfg, err := a.store.AgentReply.RuntimeConfig(ctx, a.cookieID)
	if errors.Is(err, db.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled || strings.TrimSpace(cfg.EndpointURL) == "" {
		return nil, nil
	}
	// turnCtx、cancel 是本轮次的上下文（随买家新消息取消，另受单轮总时长上限约束）及其取消函数。
	turnCtx, cancel := context.WithTimeout(m.Turn.Context(ctx), cfg.MaxSession)
	defer cancel()
	if cfg.Protocol == db.AgentProtocolOpenAI || cfg.Protocol == db.AgentProtocolAnthropic {
		return a.replyViaModel(turnCtx, m, cfg)
	}
	return a.runTurn(turnCtx, m, cfg)
}

// agentModelDefaultPrompt 是 openai/anthropic 协议未配置系统提示词时使用的内置客服提示词；
// 与全场景客服共用“不可覆盖的承诺边界”，避免模型口头承诺价格、退款或赔偿。
const agentModelDefaultPrompt = `你是闲鱼店铺的自动客服。请简短友好地回复买家，通常一两句话，不要编造信息；
无法确认时，明确建议买家联系人工客服。直接回复内容，不要加引号或解释。` + fullServiceCommitmentRule

// replyViaModel 处理 openai/anthropic 协议：把买家消息（含图片）交给大模型，并把文字回复发给买家。
//
// 它是单轮请求：不轮询、不带历史、最多发送一条文字回复，也不产生改价提案。
// 模型出错、拒绝回答或没有文字时返回 nil（让后续 AI 与默认回复接管）；回复一旦开始发送就视为已接手，不再降级。
// 买家图片随请求发送，端点不支持图片时 llmclient 自动降级为纯文字。日志不记录地址、密钥和消息正文。
func (a *AgentReplier) replyViaModel(ctx context.Context, m ChatMessage, cfg db.AgentReplyRuntime) (*ReplyResult, error) {
	if cfg.Protocol == db.AgentProtocolOpenAI && strings.TrimSpace(cfg.Model) == "" {
		// OpenAI 协议没有默认模型，未配置时不接手。
		return nil, nil
	}
	// client、clientErr 是按用户配置地址构造的受限 HTTP 客户端；地址被出站策略拒绝时不接手。
	client, clientErr := netguard.ConfiguredEndpointHTTPClient(cfg.EndpointURL, cfg.RequestTimeout)
	if clientErr != nil {
		return nil, fmt.Errorf("模型地址无效（Agent）: %w", clientErr)
	}
	// prompt 是系统提示词；开启订单阶段时追加低信任事实区。
	prompt := strings.TrimSpace(cfg.SystemPrompt)
	if prompt == "" {
		prompt = agentModelDefaultPrompt
	}
	if cfg.OrderContextEnabled {
		// stages、available、err 保存订单阶段摘要、事实是否可用及本地查询错误；失败只降级为“无法确认”。
		stages, available, err := loadOrderContext(ctx, a.orders, a.cookieID, m)
		if err != nil {
			a.logger.WarnContext(ctx, "读取买家订单阶段失败，按无法确认继续回复", "err", err)
		}
		prompt = appendOrderContext(prompt, stages, available)
	}
	// userText、images 是本轮买家输入（合并批次时是合并后的文字与最近的图片）：图片随请求发送，文字部分不再只是占位符。
	userText, images := modelUserInput(m)
	// completion、err 保存模型结果及调用错误。
	completion, err := llmclient.Complete(ctx, llmclient.Endpoint{
		Protocol: cfg.Protocol, BaseURL: cfg.EndpointURL, APIKey: cfg.Token, Model: cfg.Model, HTTPClient: client,
	}, llmclient.Request{System: prompt, UserText: userText, ImageURLs: images, User: opaqueChatID(m.ChatID)})
	if err != nil {
		return a.failTurn(ctx, &agentTurnState{}, err)
	}
	if completion.ImageDropped {
		a.logger.WarnContext(ctx, "Agent 模型端点不支持图片输入，已降级为纯文字", "status", completion.Status, "protocol", cfg.Protocol)
	}
	if completion.Refused || completion.Text == "" {
		return nil, nil
	}
	// sendCtx、committed 是提交后的发送上下文及提交是否成功；本轮已被买家新消息顶替时放弃发送，消息留给下一轮合并重发。
	sendCtx, committed := m.Turn.Commit(ctx, false)
	if !committed {
		return a.finishedResult(), nil
	}
	// state 记录本轮副作用计数；发送失败或结果不确定时 deliverReplies 已记录日志，本轮直接结束。
	state := &agentTurnState{engaged: true}
	a.deliverReplies(sendCtx, m, []agentclient.Reply{{Text: completion.Text}}, state)
	a.logger.InfoContext(ctx, "Agent 模型回复轮次结束", "protocol", cfg.Protocol, "replies_sent", state.sent)
	return a.finishedResult(), nil
}

// opaqueChatID 把会话标识转换为不可逆的短标识，供上游模型服务关联同一会话而不暴露平台会话号；空标识返回空串。
func opaqueChatID(chatID string) string {
	if chatID == "" {
		return ""
	}
	// sum 是会话标识的 SHA-256 摘要。
	sum := sha256.Sum256([]byte(chatID))
	return hex.EncodeToString(sum[:8])
}

// agentTurnState 保存一轮对话已经产生的副作用计数，用于执行硬性上限和日志摘要。
type agentTurnState struct {
	// engaged 表示 Agent 已经接手；接手后任何失败都只结束本轮，不再降级到其他回复。
	engaged bool
	// requests 是已发出的 Agent 请求数。
	requests int
	// sent 与 images 是已发送给买家的回复条数及其中图片条数。
	sent, images int
}

// runTurn 执行一轮对话：发送 message 事件，按 working 轮询，并把每个响应里的回复逐条发给买家。
func (a *AgentReplier) runTurn(ctx context.Context, m ChatMessage, cfg db.AgentReplyRuntime) (*ReplyResult, error) {
	// state 记录本轮副作用计数，用于执行请求数、回复数和图片数上限。
	state := &agentTurnState{}
	// session 是本轮复用连接的发送器：一轮最多 60 次请求只需一次 TCP/TLS 握手；令牌随它传递但不会写入日志。
	// 创建失败（地址无效）时按接手前失败处理，降级到后续回复规则。
	session, sessionErr := agentclient.NewSession(agentclient.Endpoint{URL: cfg.EndpointURL, Token: cfg.Token, Timeout: cfg.RequestTimeout})
	if sessionErr != nil {
		return a.failTurn(ctx, state, sessionErr)
	}
	// Close 在本轮结束（含被新消息顶替、账号停止）时释放空闲连接，连接不会跨轮次存活。
	defer session.Close()
	// request 是即将发送的请求，首个为 message 事件，之后由 pollRequest 派生 poll 事件。
	request := a.messageRequest(m)
	if cfg.OrderContextEnabled {
		request.OrderContext = a.orderContext(ctx, m)
	}
	// started 是对话开始时刻，用于输出耗时摘要。
	started := time.Now()
	defer func() {
		if state.engaged {
			a.logger.InfoContext(ctx, "Agent 回复轮次结束", "requests", state.requests, "replies_sent", state.sent, "duration", time.Since(started).Round(time.Millisecond))
		}
	}()
	// committed 表示本轮已确认开始向买家发送回复：此前被买家新消息顶替会放弃本轮并合并重发，此后只在两次发送之间停止。
	committed := false
	for state.requests < agentMaxRequestsPerTurn {
		state.requests++
		request.Sequence = state.requests
		// response、err 保存 Agent 响应及调用错误。
		response, err := session.Send(ctx, request)
		if err != nil {
			return a.failTurn(ctx, state, err)
		}
		if !state.engaged {
			if !response.Handled {
				return nil, nil
			}
			state.engaged = true
		} else if !response.Handled {
			return a.finishedResult(), nil
		}
		if !committed && len(response.Replies) > 0 {
			// 第一次真正要发送回复：提交后 Agent 已经对买家说话，之后再来新消息只顶替后续轮询，不再合并重发已回复的内容。
			if _, ok := m.Turn.Commit(ctx, true); !ok {
				return a.finishedResult(), nil
			}
			committed = true
		}
		// stop 为 true 表示发送失败、结果不确定或上下文被取消，本轮必须立即结束。
		if stop := a.deliverReplies(ctx, m, response.Replies, state); stop {
			return a.finishedResult(), nil
		}
		if state.sent >= agentMaxRepliesPerTurn {
			// 回复条数已达上限：之后到达的回复一律被丢弃，继续轮询只会白白占用外部 Agent，直接结束本轮。
			if response.Working {
				a.logger.WarnContext(ctx, "Agent 回复条数已达单轮上限，不再继续轮询", "max_replies", agentMaxRepliesPerTurn)
			}
			return a.finishedResult(), nil
		}
		if !response.Working {
			return a.finishedResult(), nil
		}
		// sleepErr 表示轮询等待被上下文取消；取消即结束本轮，不再请求 Agent。
		if sleepErr := a.pollSleep(ctx, clampPollInterval(response.PollAfter)); sleepErr != nil {
			return a.finishedResult(), nil
		}
		request = a.pollRequest(request)
	}
	a.logger.WarnContext(ctx, "Agent 对话达到单轮请求上限，已结束", "max_requests", agentMaxRequestsPerTurn)
	return a.finishedResult(), nil
}

// failTurn 处理 Agent 请求失败：接手前的失败降级到后续规则；上下文被取消（被新消息顶替或账号停止）抑制回复；
// 单轮总时长到期时，接手前同样降级（Agent 一直没有响应，买家不应因此收不到任何回复），接手后只结束本轮；
// 接手后的失败只记录日志并结束本轮，避免重复回复。
func (a *AgentReplier) failTurn(ctx context.Context, state *agentTurnState, err error) (*ReplyResult, error) {
	// ctxErr 是本轮上下文结束的原因：Canceled 表示被顶替或账号停止，DeadlineExceeded 表示单轮总时长到期。
	if ctxErr := ctx.Err(); ctxErr != nil {
		if !state.engaged && errors.Is(ctxErr, context.DeadlineExceeded) {
			// 取消（Canceled）表示被新消息顶替或账号停止，必须抑制；到期（DeadlineExceeded）说明 Agent 迟迟没有接手，交给 AI 与默认回复。
			return nil, fmt.Errorf("单轮总时长内外部 Agent 未接手: %w", ctxErr)
		}
		return a.finishedResult(), nil
	}
	if state.engaged {
		a.logger.WarnContext(ctx, "Agent 对话中途失败，已结束", "err", err, "replies_sent", state.sent)
		return a.finishedResult(), nil
	}
	return nil, err
}

// finishedResult 返回“已接手且已自行发送”的结果，Skip 使回复服务不再发送任何内容。
func (a *AgentReplier) finishedResult() *ReplyResult {
	return &ReplyResult{Skip: true, Source: agentReplySource}
}

// messageRequest 构造首个 message 事件。
//
// 合并批次时：message 是向后兼容的单条形态，文字为合并后的全部买家文字（只认 message 的旧 Agent 也能看到整段话），
// messages 按到达顺序列出批次里的每条原始消息（含各自图片），turn_id 沿用协调器分配的轮次标识，
// replaces_turn_id 在本轮顶替了一个尚未回复完的旧轮次时携带，Agent 据此丢弃旧任务。
func (a *AgentReplier) messageRequest(m ChatMessage) agentclient.Request {
	// message 是转发给 Agent 的兼容单条消息，默认按文字处理。
	message := agentclient.Message{ID: m.MessageID, Type: agentclient.MessageTypeText, Text: m.Text}
	// images 是本批最近的买家图片地址；纯图片批次的兼容单条消息携带最新一张。
	images := buyerImageURLs(m)
	if len(images) > 0 && strings.TrimSpace(m.Text) == agentImagePlaceholder {
		message.Type, message.ImageURL = agentclient.MessageTypeImage, images[len(images)-1]
	}
	// turnID 是本轮标识：经协调器时沿用其标识，直接调用时随机生成。
	turnID := m.Turn.ID()
	if turnID == "" {
		turnID = agentclient.NewTurnID()
	}
	return agentclient.Request{
		Event: agentclient.EventMessage, TurnID: turnID, ReplacesTurnID: m.Turn.ReplacesID(),
		AccountID: m.AccountID, ChatID: m.ChatID, ItemID: m.ItemID,
		Buyer:   &agentclient.Buyer{UserID: m.SenderUserID, Name: m.SenderName},
		Message: &message, Messages: agentMessages(m),
	}
}

// agentMessages 把本批买家消息逐条转换为协议消息；单条消息返回只含它自己的列表，保证 messages 始终至少有一项。
func agentMessages(m ChatMessage) []agentclient.Message {
	// parts 是本批原始买家消息；没有合并批次时就是 m 本身。
	parts := m.Parts
	if len(parts) == 0 {
		parts = []ChatMessage{m}
	}
	// messages 保存转换后的协议消息。
	messages := make([]agentclient.Message, 0, len(parts))
	// part 表示当前遍历的原始买家消息。
	for _, part := range parts {
		// item 默认按文字处理；图片消息携带公网图片地址。
		item := agentclient.Message{ID: part.MessageID, Type: agentclient.MessageTypeText, Text: part.Text}
		// imageURL 是该条买家消息中的图片地址，非图片消息为空。
		if imageURL := buyerImageURL(part); imageURL != "" {
			item.Type, item.ImageURL = agentclient.MessageTypeImage, imageURL
		}
		messages = append(messages, item)
	}
	return messages
}

// pollRequest 由上一个请求派生 poll 事件：保留标识，不再携带买家、消息与订单阶段。
func (a *AgentReplier) pollRequest(previous agentclient.Request) agentclient.Request {
	previous.Event, previous.Buyer, previous.Message, previous.OrderContext = agentclient.EventPoll, nil, nil, nil
	previous.Messages, previous.ReplacesTurnID = nil, ""
	return previous
}

// orderContext 读取买家在当前商品下的本地订单阶段并转换为协议字段。
// 查询失败、超时或无法安全匹配时返回 nil，请求照常发送且不携带该字段；返回的空数组表示已确认没有订单。
func (a *AgentReplier) orderContext(ctx context.Context, m ChatMessage) *agentclient.OrderContext {
	// stages、available、err 保存订单阶段摘要、事实是否可用及本地查询错误。
	stages, available, err := loadOrderContext(ctx, a.orders, a.cookieID, m)
	if err != nil {
		a.logger.WarnContext(ctx, "读取买家订单阶段失败，本次请求不携带订单阶段", "err", err)
	}
	if !available {
		return nil
	}
	// orders 保存转换后的协议订单列表；保持非 nil 以序列化为空数组。
	orders := make([]agentclient.OrderInfo, 0, len(stages))
	// stage 表示当前遍历的订单阶段摘要。
	for _, stage := range stages {
		orders = append(orders, agentclient.OrderInfo{Stage: stage.Stage, Quantity: stage.Quantity, SpecName: stage.SpecName, SpecValue: stage.SpecValue})
	}
	return &agentclient.OrderContext{Orders: orders}
}

// deliverReplies 按序发送响应中的回复并执行条数与图片数上限；返回 true 表示本轮必须立即结束
// （发送失败、结果不确定或上下文取消）。
func (a *AgentReplier) deliverReplies(ctx context.Context, m ChatMessage, replies []agentclient.Reply, state *agentTurnState) bool {
	// reply 表示当前待发送的 Agent 回复。
	for _, reply := range replies {
		if ctx.Err() != nil {
			return true
		}
		if state.sent >= agentMaxRepliesPerTurn {
			a.logger.WarnContext(ctx, "Agent 回复条数超过单轮上限，多余部分已丢弃", "max_replies", agentMaxRepliesPerTurn)
			return false
		}
		// message 是交给聊天应用的完整回复；非 http(s) 图片地址直接忽略图片部分。
		message := ReplyMessage{AccountID: a.cookieID, ChatID: m.ChatID, ToUserID: m.SenderUserID, Text: reply.Text}
		if isHTTPURL(reply.ImageURL) {
			if state.images >= agentMaxImagesPerTurn {
				a.logger.WarnContext(ctx, "Agent 图片数量超过单轮上限，该图片已丢弃", "max_images", agentMaxImagesPerTurn)
			} else {
				message.ImageURL = reply.ImageURL
			}
		}
		if message.Text == "" && message.ImageURL == "" {
			continue
		}
		// sent、err 保存聊天应用的分段确认；任何失败或不确定结果都不重试。
		sent, err := a.delivery.SendReply(ctx, message)
		if sent.ImageSent {
			state.images++
		}
		if sent.ImageSent || sent.TextSent {
			state.sent++
		}
		if err != nil || sent.Uncertain {
			a.logger.ErrorContext(ctx, "Agent 回复发送失败，已结束本轮", "err", err, "uncertain", sent.Uncertain)
			return true
		}
	}
	return false
}

// clampPollInterval 把 Agent 建议的轮询间隔夹取到允许区间；未指定时使用默认间隔。
func clampPollInterval(suggested time.Duration) time.Duration {
	switch {
	case suggested <= 0:
		return agentDefaultPollInterval
	case suggested < agentMinPollInterval:
		return agentMinPollInterval
	case suggested > agentMaxPollInterval:
		return agentMaxPollInterval
	}
	return suggested
}

// isHTTPURL 判断图片地址是否为 http(s) 绝对地址；其他协议不会交给下载器。
func isHTTPURL(raw string) bool {
	raw = strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://")
}

var _ APIReplier = (*AgentReplier)(nil)
