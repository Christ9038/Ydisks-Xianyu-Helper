package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/xianyu/protocol"
	"xianyu-go/internal/xianyu/ws"
)

// outgoingMessageCoordinator 拥有当前连接上的出站消息、回显确认、聊天历史和会话查询边界。
// 它只在锁内读取 WebSocket 与账号身份快照，任何发送、回显等待或查询 I/O 都在锁外执行。
type outgoingMessageCoordinator struct {
	// account 是构造完成后固定的账号 facade，提供连接状态和出站旁路观察器。
	account *Account
	// echoTracker 保存当前账号自动化消息的自身回显等待项；它不参与数据库写入或平台 I/O。
	echoTracker *outgoingEchoTracker
	// echoWaitTimeout 是测试可覆盖的回显确认预算；生产默认使用固定的有限等待时间。
	echoWaitTimeout time.Duration
}

// sendText 使用当前已注册 WebSocket 发送文本，并在平台接受后通知可选的聊天旁路。
// ctx 是调用方取消边界；chatID、toUserID 与 text 共同确定一次出站消息；错误保持原调用方语义。
func (c *outgoingMessageCoordinator) sendText(ctx context.Context, chatID, toUserID, text string) error {
	// a 是当前协调器绑定的账号 facade；它在 New 中写入且之后不可替换。
	a := c.account
	if a == nil {
		return errors.New("账号出站消息协调器未初始化")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	// conn、myID、err 保存锁外发送所需的连接与账号身份快照，以及读取失败原因。
	conn, myID, err := c.currentSenderState()
	if err != nil {
		return err
	}
	// sendCtx、cancel 限制单次文本发送的最长等待，并在函数返回时释放计时器。
	sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// requestID 是本次平台请求的 mid；自动化回显确认必须沿发送上下文复用它。
	requestID := protocol.GenerateMid()
	// echoWaiter 必须在平台写入前登记，防止闲鱼回显先到而错过确认窗口。
	echoWaiter := c.registerOutgoingEcho(ctx, chatID, toUserID, "text", text, requestID)
	if echoWaiter != nil {
		sendCtx = ws.WithOutgoingRequestID(sendCtx, requestID)
	}
	// err 是平台文本发送失败原因；此时调用方按是否确定未发送决定重试或人工核对。
	if err := conn.SendText(sendCtx, myID, chatID, toUserID, text); err != nil {
		// classified 保存按平台结果分类后的发送错误，供回显保护策略判断。
		classified := classifyPlatformSendError(err)
		if errors.Is(classified, automation.ErrMessageNotSent) {
			echoWaiter.cancelWithoutLateProtection()
		} else {
			echoWaiter.cancel()
		}
		return classified
	}
	// err 是自身回显确认失败原因；失败时必须把发送结果交给上层人工核对。
	if err := c.confirmOutgoingEcho(ctx, echoWaiter, chatID); err != nil {
		return err
	}
	// observer、ok 是可选出站旁路观察器及其接口匹配结果；旁路失败不能改变平台发送成功结果。
	if observer, ok := a.handler.(outgoingChatHandler); ok {
		// key 是 UI 创建的待发送消息关联键，避免旁路重复插入同一文本。
		key, _ := ctx.Value(outgoingMessageKeyContextKey{}).(string)
		// err 是旁路持久化或广播失败原因，仅记录脱敏告警。
		if err := observer.HandleOutgoingChatMessage(ctx, OutgoingChatMessage{
			AccountID: a.CookieID, ChatID: chatID, BuyerID: toUserID, Text: text, MessageKey: key, ObservedAt: time.Now().UTC().UnixMilli(),
		}); err != nil {
			a.logger.Warn("保存出站聊天旁路失败", "account", a.CookieID, "chat_id", chatID, "err", err)
		}
	}
	return nil
}

// sendImage 使用当前已注册 WebSocket 发送可直接访问的远程图片。
// ctx 是调用方取消边界；cardID 仅用于兼容 MessageSender 契约，当前协议发送不直接使用它；width/height 为图片像素尺寸。
func (c *outgoingMessageCoordinator) sendImage(ctx context.Context, chatID, toUserID, imageURL string, cardID int64, width, height int) error {
	// a 是当前协调器绑定的账号 facade；它用于维持与文本发送一致的初始化检查。
	a := c.account
	if a == nil {
		return errors.New("账号出站消息协调器未初始化")
	}
	imageURL = strings.TrimSpace(imageURL)
	if imageURL == "" {
		return nil
	}
	if strings.HasPrefix(imageURL, "/static/") || strings.HasPrefix(imageURL, "static/") {
		return fmt.Errorf("当前运行时暂不支持本地图片自动上传到闲鱼 CDN: %s", imageURL)
	}
	// conn、myID、err 保存锁外图片发送所需的连接与账号身份快照，以及读取失败原因。
	conn, myID, err := c.currentSenderState()
	if err != nil {
		return err
	}
	// sendCtx、cancel 限制单次图片发送的最长等待，并在函数返回时释放计时器。
	sendCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	// requestID 是本次平台图片请求的 mid；自动化回显确认必须沿发送上下文复用它。
	requestID := protocol.GenerateMid()
	// echoWaiter 必须在图片写入前登记；图片回显使用平台返回的同类媒体正文进行匹配。
	echoWaiter := c.registerOutgoingEcho(ctx, chatID, toUserID, "image", imageURL, requestID)
	if echoWaiter != nil {
		sendCtx = ws.WithOutgoingRequestID(sendCtx, requestID)
	}
	_ = cardID // cardID 由上层动作检查点持久化，协议图片发送本身不携带该字段。
	if err := conn.SendImage(sendCtx, myID, chatID, toUserID, imageURL, width, height); err != nil {
		// classified 保存按平台结果分类后的图片发送错误，供回显保护策略判断。
		classified := classifyPlatformSendError(err)
		if errors.Is(classified, automation.ErrMessageNotSent) {
			echoWaiter.cancelWithoutLateProtection()
		} else {
			echoWaiter.cancel()
		}
		return classified
	}
	return c.confirmOutgoingEcho(ctx, echoWaiter, chatID)
}

// registerOutgoingEcho 按调用上下文决定是否登记自动化出站回显确认；普通人工聊天保持原有非阻塞旁路。
func (c *outgoingMessageCoordinator) registerOutgoingEcho(ctx context.Context, chatID, buyerID, messageType, content, requestID string) *outgoingEchoWaiter {
	if c == nil || c.echoTracker == nil || !wantsOutgoingEchoConfirmation(ctx) {
		return nil
	}
	return c.echoTracker.register(chatID, buyerID, messageType, content, requestID)
}

// observePlatformSendResponse 接收 WebSocket 发送成功响应中经过正文与身份核验的自身消息。
// 它只唤醒同账号的自动化确认等待项，不持久化、不广播且不执行外部 I/O；正常同步推送仍会沿既有分发路径处理。
func (c *outgoingMessageCoordinator) observePlatformSendResponse(echo ws.OutgoingEcho) {
	if c == nil || c.echoTracker == nil {
		return
	}
	// observed 保存适配为引擎内部等待键的非敏感回显摘要；响应已由 ws 层验证消息正文与发送者归属。
	observed := OutgoingChatMessage{
		ChatID:      echo.ChatID,
		BuyerID:     echo.BuyerID,
		RequestID:   echo.RequestID,
		MessageKey:  echo.MessageKey,
		MessageType: echo.MessageType,
		Text:        echo.Text,
		Content:     echo.Content,
	}
	c.echoTracker.observePlatform(observed)
}

// confirmOutgoingEcho 等待有限时间的自身回显；超时必须返回不确定错误，禁止自动化安全重试。
func (c *outgoingMessageCoordinator) confirmOutgoingEcho(ctx context.Context, waiter *outgoingEchoWaiter, chatID string) error {
	if waiter == nil {
		return nil
	}
	// timeout 是当前协调器的回显等待预算；测试可缩短它，生产默认保持五秒。
	timeout := c.echoWaitTimeout
	if timeout <= 0 {
		timeout = outgoingEchoConfirmationTimeout
	}
	// waitErr 是回显等待的退出原因；超时、取消和账号关闭都视为结果不确定。
	if waitErr := waiter.wait(ctx, timeout); waitErr != nil {
		// pushDelay、pushSeen 表示确认窗口内是否到达过正文匹配、但因本次发送绑定 mid 而被忽略的推送回显及其延迟；仅用于诊断。
		pushDelay, pushSeen := waiter.ignoredPushDelay()
		waiter.cancel()
		if c != nil && c.account != nil && c.account.logger != nil {
			c.account.logger.Warn("自动化出站消息等待闲鱼回显超时", "chat_id", chatID, "wait_timeout_ms", timeout.Milliseconds(),
				"request_mid", waiter.requestID, "push_echo_seen", pushSeen, "push_echo_after_ms", pushDelay.Milliseconds())
		}
		return fmt.Errorf("%w: %v", errOutgoingEchoUnconfirmed, waitErr)
	}
	return nil
}

// sendItemCard 使用当前已注册 WebSocket 发送商品卡片，并将本地幂等键交给出站观察。
func (c *outgoingMessageCoordinator) sendItemCard(ctx context.Context, chatID, toUserID, itemID, title, imageURL, price string) error {
	// account 是当前协调器绑定的账号 facade。
	account := c.account
	if account == nil {
		return errors.New("账号出站消息协调器未初始化")
	}
	// conn、myID 和 stateErr 是锁外发送所需的连接、当前账号身份和状态错误。
	conn, myID, stateErr := c.currentSenderState()
	if stateErr != nil {
		return stateErr
	}
	// itemSender 和 supported 表示当前连接是否支持商品卡片扩展协议。
	itemSender, supported := conn.(interface {
		SendItemCard(context.Context, string, string, string, string, string, string, string) error
	})
	if !supported {
		return fmt.Errorf("%w: 当前 WebSocket 不支持商品卡片", automation.ErrMessageNotSent)
	}
	// sendCtx 和 cancel 将单次卡片发送最长等待限制为八秒。
	sendCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	// sendErr 表示底层 WebSocket 商品卡片投递是否失败。
	if sendErr := itemSender.SendItemCard(sendCtx, myID, chatID, toUserID, itemID, title, imageURL, price); sendErr != nil {
		return classifyPlatformSendError(sendErr)
	}
	// contentBytes 是与本地 item 消息格式一致的规范出站正文。
	contentBytes, marshalErr := json.Marshal(struct {
		ItemID   string `json:"item_id"`
		Title    string `json:"title"`
		ImageURL string `json:"image_url"`
		Price    string `json:"price"`
	}{ItemID: strings.TrimSpace(itemID), Title: strings.TrimSpace(title), ImageURL: strings.TrimSpace(imageURL), Price: strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(price), "¥"))})
	if marshalErr != nil {
		account.logger.Warn("构建商品卡片出站观察失败", "account", account.CookieID, "chat_id", chatID, "err", marshalErr)
		return nil
	}
	// observer 和 ok 表示账号事件处理器是否支持出站聊天旁路观察。
	if observer, ok := account.handler.(outgoingChatHandler); ok {
		// messageKey 是 UI 创建的待发送消息关联键。
		messageKey, _ := ctx.Value(outgoingMessageKeyContextKey{}).(string)
		// observeErr 表示商品卡片旁路消息是否完成本地观察。
		if observeErr := observer.HandleOutgoingChatMessage(ctx, OutgoingChatMessage{AccountID: account.CookieID, ChatID: chatID, BuyerID: toUserID, MessageKey: messageKey, MessageType: "item", Content: string(contentBytes), ObservedAt: time.Now().UTC().UnixMilli()}); observeErr != nil {
			account.logger.Warn("保存出站商品卡片旁路失败", "account", account.CookieID, "chat_id", chatID, "err", observeErr)
		}
	}
	return nil
}

// classifyPlatformSendError 将协议层确定未发送或明确拒绝转换为自动化可安全处理的错误；未知结果保留原错误链，禁止自动重放。
func classifyPlatformSendError(err error) error {
	if err == nil {
		return nil
	}
	// kind 保存协议层已经判定的发送结果分类。
	kind := ws.SendResultKind(err)
	if kind == ws.SendNotSent || kind == ws.SendRejected {
		return fmt.Errorf("%w: %v", automation.ErrMessageNotSent, err)
	}
	return err
}

// currentSenderState 返回可用 WebSocket 与账号 unb 身份快照；持锁范围只覆盖快照读取。
func (c *outgoingMessageCoordinator) currentSenderState() (WSConn, string, error) {
	// a 是当前协调器绑定的账号 facade；未初始化时不能安全读取连接状态。
	a := c.account
	if a == nil {
		return nil, "", errors.New("账号出站消息协调器未初始化")
	}
	a.runtimeMu.Lock()
	// conn 是当前连接快照；后续读取账号身份字段使用 Account 自身的凭证锁。
	conn := a.conn
	a.runtimeMu.Unlock()
	if conn == nil {
		return nil, "", fmt.Errorf("%w: 账号 %s 当前没有可用 WebSocket 连接", automation.ErrMessageNotSent, a.CookieID)
	}
	a.mu.Lock()
	// myID 是发送协议所需的当前账号 unb 身份快照。
	myID := strings.TrimSpace(a.UserID)
	if myID == "" {
		myID = protocol.TransCookies(a.CookieStr)["unb"]
	}
	a.mu.Unlock()
	if myID == "" {
		return nil, "", fmt.Errorf("%w: 账号 %s 缺少 unb，无法发送消息", automation.ErrMessageNotSent, a.CookieID)
	}
	return conn, myID, nil
}

// fetchChatHistory 使用当前已注册连接查询指定聊天的历史消息。
// ctx、chatID、cursor 与 limit 直接传给平台连接；返回值保留账号身份快照与原始平台正文。
func (c *outgoingMessageCoordinator) fetchChatHistory(ctx context.Context, chatID string, cursor int64, limit int) (map[string]any, string, error) {
	// conn、myID、err 保存历史查询所需连接、账号身份快照与读取失败原因。
	conn, myID, err := c.currentSenderState()
	if err != nil {
		return nil, "", err
	}
	// history、ok 保存连接是否支持历史查询的可选能力及其类型判断结果。
	history, ok := conn.(interface {
		ListUserMessages(context.Context, string, int64, int) (map[string]any, error)
	})
	if !ok {
		return nil, "", errors.New("当前 WebSocket 连接不支持聊天历史")
	}
	// body、err 保存平台返回的历史正文与查询错误。
	body, err := history.ListUserMessages(ctx, chatID, cursor, limit)
	return body, myID, err
}

// fetchChatConversations 使用当前已注册连接查询历史会话。
// ctx、cursor 与 limit 直接传给平台连接；返回值保留账号身份快照与原始平台正文。
func (c *outgoingMessageCoordinator) fetchChatConversations(ctx context.Context, cursor int64, limit int) (map[string]any, string, error) {
	// conn、myID、err 保存会话查询所需连接、账号身份快照与读取失败原因。
	conn, myID, err := c.currentSenderState()
	if err != nil {
		return nil, "", err
	}
	// fetcher、ok 保存连接是否支持会话查询的可选能力及其类型判断结果。
	fetcher, ok := conn.(interface {
		ListConversations(context.Context, int64, int) (map[string]any, error)
	})
	if !ok {
		return nil, "", errors.New("当前 WebSocket 连接不支持历史会话")
	}
	// body、err 保存平台返回的会话正文与查询错误。
	body, err := fetcher.ListConversations(ctx, cursor, limit)
	return body, myID, err
}

// automationReady 返回当前 WebSocket 是否已进入 online 状态，供 Automation 在发送前做无 I/O 门禁。
func (c *outgoingMessageCoordinator) automationReady() bool {
	// a 是当前协调器绑定的账号 facade；未初始化协调器不可能提供在线发送能力。
	a := c.account
	if a == nil {
		return false
	}
	a.runtimeMu.Lock()
	// ready 表示连接存在且状态已进入 online 的瞬时快照。
	ready := a.conn != nil && a.runtimeState == RuntimeOnline
	a.runtimeMu.Unlock()
	return ready
}
