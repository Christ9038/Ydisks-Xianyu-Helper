package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"xianyu-go/internal/agentclient"
	"xianyu-go/internal/agentplatform"
	"xianyu-go/internal/db"
	"xianyu-go/internal/netguard"
)

// platformSessionKey 由账号与买家会话号派生稳定且不暴露平台会话号的标识：
// Dify 用作终端用户标识 user，FastGPT 用作 chatId。同一账号同一买家会话始终得到同一个值，不同账号互不冲突。
func platformSessionKey(cookieID, chatID string) string {
	// sum 是“账号 + 会话号”的 SHA-256 摘要，中间用 NUL 分隔避免拼接歧义。
	sum := sha256.Sum256([]byte(cookieID + "\x00" + chatID))
	return hex.EncodeToString(sum[:16])
}

// replyViaPlatform 处理 dify/fastgpt 协议：把（合并后的）买家输入交给工作流 Agent 平台，并把文字回复发给买家。
//
// 与 replyViaModel 一样是单轮请求：不轮询、不产生改价提案、最多发送一条文字回复；
// 与之不同的是对话记忆由平台自己维护（Dify 用 conversation_id，FastGPT 用 chatId），所以不发送系统提示词、模型名和订单阶段，
// 也不带聊天历史。平台出错、拒绝或没有文字时返回 nil 让后续 AI 与默认回复接管；回复一旦开始发送就视为已接手。
// 买家连发被合并时，被中断的那次请求平台可能已记录了前半段，合并后的请求会再次包含它，平台历史里可能出现重复，不影响回复。
func (a *AgentReplier) replyViaPlatform(ctx context.Context, m ChatMessage, cfg db.AgentReplyRuntime) (*ReplyResult, error) {
	// client、clientErr 是按用户配置地址构造的受限 HTTP 客户端；一次请求就是整轮对话，超时取单轮总时长上限。
	client, clientErr := netguard.ConfiguredEndpointHTTPClient(cfg.EndpointURL, cfg.MaxSession)
	if clientErr != nil {
		return nil, fmt.Errorf("平台地址无效（Agent）: %w", clientErr)
	}
	// endpoint 是本次调用的目标平台；key 是稳定的会话派生标识。
	endpoint := agentplatform.Endpoint{BaseURL: cfg.EndpointURL, Token: cfg.Token, HTTPClient: client}
	// key 是由账号和会话号派生的稳定标识。
	key := platformSessionKey(a.cookieID, m.ChatID)
	// sessionID 是本次请求使用的平台会话标识：FastGPT 的 chatId 直接取 key；Dify 的 conversation_id 由平台生成，需读取已保存的映射。
	sessionID := key
	if cfg.Protocol == db.AgentProtocolDify {
		sessionID = ""
		if a.store.AgentRemoteSessions != nil {
			// stored、storedErr 保存已保存的会话标识及读取错误；读取失败只降级为开启新会话。
			stored, storedErr := a.store.AgentRemoteSessions.Get(ctx, a.cookieID, m.ChatID)
			if storedErr != nil {
				a.logger.WarnContext(ctx, "读取 Dify 会话标识失败，按新会话继续", "err", storedErr)
			}
			sessionID = stored
		}
	}
	// userText、images 是本轮买家输入（合并批次时是合并后的文字与最近的图片）。
	userText, images := modelUserInput(m)
	// call 用给定会话标识调用平台一次。
	call := func(session string) (agentplatform.Result, error) {
		return agentplatform.Call(ctx, cfg.Protocol, endpoint, agentplatform.Request{Text: userText, ImageURLs: images, User: key, SessionID: session})
	}
	// result、err 保存平台结果；会话标识失效（Dify 会话被删除或已结束）时清除映射并用新会话重试一次。
	result, err := call(sessionID)
	if errors.Is(err, agentplatform.ErrSessionGone) {
		if a.store.AgentRemoteSessions != nil {
			// deleteErr 是清除会话映射的错误。
			if deleteErr := a.store.AgentRemoteSessions.Delete(ctx, a.cookieID, m.ChatID); deleteErr != nil {
				a.logger.WarnContext(ctx, "清除失效的 Dify 会话标识失败", "err", deleteErr)
			}
		}
		result, err = call("")
	}
	if errors.Is(err, agentplatform.ErrEmptyReply) {
		return nil, nil
	}
	if err != nil {
		return a.failTurn(ctx, &agentTurnState{}, err)
	}
	if cfg.Protocol == db.AgentProtocolDify && result.SessionID != "" && result.SessionID != sessionID && a.store.AgentRemoteSessions != nil {
		// 会话在平台侧已经存在，即使本轮随后被新消息顶替也要保存，下一轮才能续用。
		if putErr := a.store.AgentRemoteSessions.Put(ctx, a.cookieID, m.ChatID, result.SessionID); putErr != nil {
			a.logger.WarnContext(ctx, "保存 Dify 会话标识失败，下一轮将开启新会话", "err", putErr)
		}
	}
	if result.ImageDropped {
		a.logger.WarnContext(ctx, "Agent 平台不接受图片输入，已降级为纯文字", "protocol", cfg.Protocol)
	}
	// sendCtx、committed 是提交后的发送上下文及提交是否成功；本轮已被买家新消息顶替时放弃发送，消息留给下一轮合并重发。
	sendCtx, committed := m.Turn.Commit(ctx, false)
	if !committed {
		return a.finishedResult(), nil
	}
	// state 记录本轮副作用计数；发送失败或结果不确定时 deliverReplies 已记录日志，本轮直接结束。
	state := &agentTurnState{engaged: true}
	a.deliverReplies(sendCtx, m, []agentclient.Reply{{Text: result.Text}}, state)
	a.logger.InfoContext(ctx, "Agent 平台回复轮次结束", "protocol", cfg.Protocol, "replies_sent", state.sent)
	return a.finishedResult(), nil
}
