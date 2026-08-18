package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/xianyu/protocol"
)

// outgoingMessageCoordinator 拥有当前连接上的出站消息、聊天历史和会话查询边界。
// 它只在锁内读取 WebSocket 与账号身份快照，任何发送或查询 I/O 都在锁外执行。
type outgoingMessageCoordinator struct {
	// account 是构造完成后固定的账号 facade，提供连接状态和出站旁路观察器。
	account *Account
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
	// err 是平台文本发送失败原因；此时调用方按是否确定未发送决定重试或人工核对。
	if err := conn.SendText(sendCtx, myID, chatID, toUserID, text); err != nil {
		return err
	}
	// observer、ok 是可选出站旁路观察器及其接口匹配结果；旁路失败不能改变平台发送成功结果。
	if observer, ok := a.handler.(outgoingChatHandler); ok {
		// key 是 UI 创建的待发送消息关联键，避免旁路重复插入同一文本。
		key, _ := ctx.Value(outgoingMessageKeyContextKey{}).(string)
		// err 是旁路持久化或广播失败原因，仅记录脱敏告警。
		if err := observer.HandleOutgoingChatMessage(ctx, OutgoingChatMessage{
			AccountID: a.CookieID, ChatID: chatID, BuyerID: toUserID, Text: text, MessageKey: key,
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
	_ = cardID // cardID 由上层动作检查点持久化，协议图片发送本身不携带该字段。
	return conn.SendImage(sendCtx, myID, chatID, toUserID, imageURL, width, height)
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
