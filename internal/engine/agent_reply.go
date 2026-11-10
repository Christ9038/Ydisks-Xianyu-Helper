package engine

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"xianyu-go/internal/agentclient"
	"xianyu-go/internal/db"
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
// 并发：turns 保证同一会话单飞；配置每次从数据库读取，修改后无需重启账号。
type AgentReplier struct {
	// cookieID 是所属账号标识。
	cookieID string
	// store 提供 Agent 配置；只读取，不写入。
	store *db.Store
	// delivery 是完整消息投递端口，与普通自动回复共用图片上传和文字发送规则。
	delivery ReplyDelivery
	// logger 带账号字段；不得记录 Agent 地址、令牌和消息正文。
	logger *slog.Logger
	// turns 保证同一会话同一时刻只有一个对话轮次。
	turns agentTurnRegistry
	// pollSleep 是轮询等待函数，遵守 ctx 取消；测试可替换以免真实等待。
	pollSleep func(ctx context.Context, d time.Duration) error
}

// NewAgentReplier 构造账号的 Agent 回复器；delivery 为空时回复器始终不接手。
func NewAgentReplier(cookieID string, store *db.Store, delivery ReplyDelivery, logger *slog.Logger) *AgentReplier {
	if logger == nil {
		logger = slog.Default()
	}
	return &AgentReplier{
		cookieID: cookieID, store: store, delivery: delivery,
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
	// turnCtx、finish、beginErr 是本轮次的上下文（含总时长上限）、结束函数及登记错误。
	turnCtx, finish, beginErr := a.turns.begin(ctx, m.ChatID, cfg.MaxSession)
	if beginErr != nil {
		// 等待旧轮次退出时账号被停止：抑制后续回复。
		return &ReplyResult{Skip: true, Source: agentReplySource}, nil
	}
	defer finish()
	return a.runTurn(turnCtx, m, cfg)
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
	// endpoint 是本轮复用的出站目标，令牌随它传递但不会写入日志。
	endpoint := agentclient.Endpoint{URL: cfg.EndpointURL, Token: cfg.Token, Timeout: cfg.RequestTimeout}
	// request 是即将发送的请求，首个为 message 事件，之后由 pollRequest 派生 poll 事件。
	request := a.messageRequest(m)
	// state 记录本轮副作用计数，用于执行请求数、回复数和图片数上限。
	state := &agentTurnState{}
	// started 是对话开始时刻，用于输出耗时摘要。
	started := time.Now()
	defer func() {
		if state.engaged {
			a.logger.InfoContext(ctx, "Agent 回复轮次结束", "requests", state.requests, "replies_sent", state.sent, "duration", time.Since(started).Round(time.Millisecond))
		}
	}()
	for state.requests < agentMaxRequestsPerTurn {
		state.requests++
		request.Sequence = state.requests
		// response、err 保存 Agent 响应及调用错误。
		response, err := agentclient.Send(ctx, endpoint, request)
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
		// stop 为 true 表示发送失败、结果不确定或上下文被取消，本轮必须立即结束。
		if stop := a.deliverReplies(ctx, m, response.Replies, state); stop {
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
// 接手后的失败只记录日志并结束本轮，避免重复回复。
func (a *AgentReplier) failTurn(ctx context.Context, state *agentTurnState, err error) (*ReplyResult, error) {
	if ctx.Err() != nil {
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

// messageRequest 构造首个 message 事件；买家图片消息携带公网图片地址。
func (a *AgentReplier) messageRequest(m ChatMessage) agentclient.Request {
	// message 是转发给 Agent 的买家消息，默认按文字处理。
	message := agentclient.Message{ID: m.MessageID, Type: agentclient.MessageTypeText, Text: m.Text}
	if strings.TrimSpace(m.Text) == agentImagePlaceholder {
		// imageURL 是从买家图片消息中提取的公网图片地址；提取不到时按文字转发。
		if imageURL := extractImageObservationContent(m.Raw); imageURL != "" {
			message.Type, message.ImageURL = agentclient.MessageTypeImage, imageURL
		}
	}
	return agentclient.Request{
		Event: agentclient.EventMessage, TurnID: agentclient.NewTurnID(),
		AccountID: m.AccountID, ChatID: m.ChatID, ItemID: m.ItemID,
		Buyer:   &agentclient.Buyer{UserID: m.SenderUserID, Name: m.SenderName},
		Message: &message,
	}
}

// pollRequest 由上一个请求派生 poll 事件：保留标识，不再携带买家与消息。
func (a *AgentReplier) pollRequest(previous agentclient.Request) agentclient.Request {
	previous.Event, previous.Buyer, previous.Message = agentclient.EventPoll, nil, nil
	return previous
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
