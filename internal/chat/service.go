package chat

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"xianyu-go/internal/db"
)

// HistoryPage 用于本次流程后续判断的History页码
type HistoryPage struct {
	Messages   []db.ChatMessage
	HasMore    bool
	NextCursor int64
}

// ConversationPage 用于本次流程后续判断的Conversation页码
type ConversationPage struct {
	HasMore    bool
	NextCursor int64
}

// xianxiaomiAvatar 用于本次流程后续判断的xianxiaomiAvatar
const xianxiaomiAvatar = "https://img.alicdn.com/imgextra/i2/O1CN01rxBFRr1II3BU0as29_!!6000000000869-2-tps-144-144.png_110x10000.jpg_.webp"

// RecordConversationPage 封装RecordConversation页码业务协调。
func (s *Service) RecordConversationPage(ctx context.Context, accountID, myID string, body map[string]any) (ConversationPage, error) {
	// page 用于本次流程后续判断的页码
	page := ConversationPage{HasMore: boolValue(body["hasMore"]), NextCursor: int64Value(body["nextCursor"])}
	// items 用于本次流程后续判断的商品列表
	items, _ := body["userConvs"].([]any)
	// item 表示当前遍历过程中的商品
	for _, item := range items {
		// wrapper 用于本次流程后续判断的wrapper
		wrapper, _ := item.(map[string]any)
		// conv 用于本次流程后续判断的conv
		conv := wrapper
		if // nested、ok 用于本次流程后续判断的nested、ok
		nested, ok := wrapper["singleChatUserConversation"].(map[string]any); ok {
			conv = nested
		}
		// single 用于本次流程后续判断的single
		single, _ := conv["singleChatConversation"].(map[string]any)
		// cid 用于本次流程后续判断的cid
		cid := strings.Split(strings.TrimSpace(fmt.Sprint(single["cid"])), "@")[0]
		if // visible、exists 用于本次流程后续判断的visible、exists
		visible, exists := conv["visible"]; exists && int64Value(visible) == 0 {
			if cid != "" && cid != "<nil>" {
				_ = s.repository.DeleteSession(ctx, accountID, cid)
			}
			continue
		}
		// first 用于本次流程后续判断的first
		first := strings.Split(strings.TrimSpace(fmt.Sprint(single["pairFirst"])), "@")[0]
		// second 用于本次流程后续判断的second
		second := strings.Split(strings.TrimSpace(fmt.Sprint(single["pairSecond"])), "@")[0]
		// ext 用于本次流程后续判断的ext
		ext := mapValue(single["extension"])
		if second == "0" && cleanNilString(ext["extUserId"]) != "1400" {
			_ = s.repository.DeleteSession(ctx, accountID, cid)
			continue
		}
		// peerID 用于本次流程后续判断的peerID
		peerID := first
		if first == myID {
			peerID = second
		}
		if peerID == "0" {
			peerID = cleanNilString(ext["extUserId"])
		}
		if cid == "" || cid == "<nil>" || peerID == "" || peerID == "0" || peerID == "<nil>" {
			continue
		}
		// lastWrap 用于本次流程后续判断的lastWrap
		lastWrap, _ := conv["lastMessage"].(map[string]any)
		// last 用于本次流程后续判断的last
		last, _ := lastWrap["message"].(map[string]any)
		// The conversation endpoint can return empty shells for notification
		// recipients. The official web client does not render these as contacts;
		// importing them creates fake users such as numeric IDs with “暂无消息”.
		if len(last) == 0 {
			continue
		}
		// reminderTitle belongs to the message presentation layer. Depending on
		// the message type it may contain a nickname, an order-state prompt, or
		// another card title. The official web client resolves conversation
		// identity separately through pc.user.query, so never persist this field
		// as the session nickname.
		// peerName 用于本次流程后续判断的peer名称
		peerName := ""
		// custom 用于本次流程后续判断的custom
		custom := map[string]any{}
		// extension 用于本次流程后续判断的extension
		extension := mapValue(last["extension"])
		if // content、ok 用于本次流程后续判断的content、ok
		content, ok := last["content"].(map[string]any); ok {
			custom, _ = content["custom"].(map[string]any)
		}
		// summary 用于本次流程后续判断的summary
		summary := cleanNilString(custom["summary"])
		if summary == "" {
			summary = cleanNilString(extension["reminderContent"])
		}
		if summary == "" {
			summary = cleanNilString(extension["detailNotice"])
		}
		if summary == "" {
			if // encoded 用于本次流程后续判断的encoded
			encoded := cleanNilString(custom["data"]); encoded != "" {
				// decoded 用于本次流程后续判断的decoded
				var decoded map[string]any
				if // raw、err 用于本次流程后续判断的raw、err
				raw, err := base64.StdEncoding.DecodeString(encoded); err == nil && json.Unmarshal(raw, &decoded) == nil {
					// fallback 用于本次流程后续判断的fallback
					fallback := ""
					if // textBlock、ok 用于本次流程后续判断的文本Block、ok
					textBlock, ok := decoded["text"].(map[string]any); ok {
						fallback = cleanNilString(textBlock["text"])
					}
					_, summary = extractMessageContent(decoded, fallback)
				}
			}
		}
		if summary == "" {
			summary = "暂无消息"
		}
		// avatar 用于本次流程后续判断的avatar
		avatar := ""
		if peerID == "1400" {
			peerName, avatar = "闲小蜜", xianxiaomiAvatar
		}
		// modifyTime 用于本次流程后续判断的modify时间
		modifyTime := int64Value(conv["modifyTime"])
		// lastMessageAt 用于本次流程后续判断的last消息At
		lastMessageAt := int64Value(last["createAt"])
		if lastMessageAt <= 0 {
			lastMessageAt = modifyTime
		}
		// unreadCount 是平台红点与本地真实用户消息状态归一后的展示数量。
		unreadCount := s.conversationUnreadCount(ctx, accountID, cid, peerID, conv, last, summary)
		// session 保存待写入的非敏感会话摘要及归一后的未读数量。
		session := db.ChatSession{CookieID: accountID, ChatID: cid, BuyerID: peerID, BuyerName: peerName, BuyerAvatar: avatar,
			ItemID: cleanNilString(ext["itemId"]), ItemTitle: cleanNilString(ext["itemTitle"]), ItemImageURL: cleanNilString(ext["itemMainPic"]), LastMessage: summary,
			LastMessageAt: lastMessageAt, UnreadCount: unreadCount}
		if // err 用于本次流程后续判断的err
		err := s.repository.UpsertSession(ctx, session); err != nil {
			return page, err
		}
		if // err 用于本次流程后续判断的err
		err := s.repository.SyncSessionSummary(ctx, accountID, cid, summary, lastMessageAt, modifyTime, session.UnreadCount); err != nil {
			return page, err
		}
	}
	return page, nil
}

// conversationUnreadCount 以平台红点为上限，并用本地真实用户消息已读状态排除系统卡片。
func (s *Service) conversationUnreadCount(ctx context.Context, accountID, chatID, peerID string, conv, last map[string]any, summary string) int {
	// official 保存平台会话红点；负数响应按零处理。
	official := int(int64Value(conv["redPoint"]))
	if official < 0 {
		official = 0
	}
	if official == 0 {
		return 0
	}

	// local、err 保存本地真实用户未读数及查询错误；查询失败时回退平台红点。
	if local, err := s.repository.CountUnreadUserMessages(ctx, accountID, chatID); err == nil && local > 0 {
		// A stale local event must not make the badge exceed the official
		// conversation signal. In normal operation these values are equal.
		if local > official {
			return official
		}
		return local
	}

	if !historyMessageIsSystem(last, summary) {
		// No message-level row exists yet (for example immediately after a
		// fresh login), so retain the official count as a safe fallback.
		return official
	}

	// 闲小蜜会话只包含平台消息，永远不能制造用户红点。
	if strings.TrimSuffix(strings.TrimSpace(peerID), "@goofish") == "1400" {
		return 0
	}

	// The official last-message envelope exposes unreadCount/readStatus for
	// that item. Subtract that system portion from redPoint; when older server
	// responses omit the fields, conservatively remove one system item.
	// systemUnread 保存最后一条系统消息在官方会话红点中的未读份额。
	systemUnread := int(int64Value(last["unreadCount"]))
	// readStatus 保存最后一条系统消息的协议已读状态，缺少未读数时用于保守扣除。
	readStatus := int64Value(last["readStatus"])
	if systemUnread <= 0 && readStatus != 2 {
		systemUnread = 1
	}
	if systemUnread > official {
		systemUnread = official
	}
	return official - systemUnread
}

// historyMessageIsSystem 判断平台会话摘要是否代表系统消息，避免系统卡片制造用户未读。
func historyMessageIsSystem(last map[string]any, summary string) bool {
	// extension 保存会话末条消息的扩展字段，包含协议发送者身份。
	extension := mapValue(last["extension"])
	// senderID 保存协议发送者标识，供系统账号和系统卡片分类使用。
	senderID := cleanNilString(extension["senderUserId"])
	// content 保存解码后的系统卡片内容；解码失败时保留空对象供后续摘要规则判断。
	content := map[string]any{}
	// contentMap、ok 保存消息内容对象及其类型断言结果，非对象响应无需继续读取卡片字段。
	if contentMap, ok := last["content"].(map[string]any); ok {
		// custom、ok 保存自定义消息容器及其类型断言结果，卡片编码仅存在于该容器。
		if custom, ok := contentMap["custom"].(map[string]any); ok {
			// encoded 保存 Base64 卡片载荷，空载荷代表没有可解码的系统协议内容。
			if encoded := cleanNilString(custom["data"]); encoded != "" {
				// raw、err 保存解码后的卡片 JSON 与解码错误；错误时退回摘要分类，且不传播载荷。
				if raw, err := base64.StdEncoding.DecodeString(encoded); err == nil {
					_ = json.Unmarshal(raw, &content)
				}
			}
		}
	}
	return isOfficialSystemMessage(content, senderID, summary)
}

// invalidNicknames 保存不能作为买家真实昵称持久化的系统展示文本。
var invalidNicknames = map[string]struct{}{
	"交易消息": {}, "系统消息": {}, "卡片消息": {}, "我完成了评价": {}, "对方完成了评价": {},
	"快给ta一个评价吧～": {}, "卖家已发货": {}, "买家已付款": {}, "买家已确认收货": {},
	"等待您发货": {}, "超时未付款，系统关闭了订单": {}, "邀您填写售后问卷": {},
}

// ValidNickname 封装有效Nickname业务协调。
func ValidNickname(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if strings.Contains(value, "***") {
		return false
	}
	if // err 用于本次流程后续判断的err
	_, err := strconv.ParseUint(value, 10, 64); err == nil {
		return false
	}
	// trimmed 用于本次流程后续判断的trimmed
	trimmed := strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
	if // invalid 用于本次流程后续判断的invalid
	_, invalid := invalidNicknames[trimmed]; invalid {
		return false
	}
	return !strings.Contains(value, "发来一条新消息")
}

// cleanNilString 封装cleanNilString业务协调。
func cleanNilString(value any) string {
	// text 用于本次流程后续判断的文本
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "<nil>" {
		return ""
	}
	return text
}

// Incoming 用于本次流程后续判断的Incoming
type Incoming struct {
	AccountID string
	ChatID    string
	BuyerID   string
	BuyerName string
	Text      string
	MessageID string
	ItemID    string
	Raw       map[string]any
}

// Event 用于本次流程后续判断的Event
type Event struct {
	Type    string          `json:"type"`
	Message *db.ChatMessage `json:"message,omitempty"`
	Session *db.ChatSession `json:"session,omitempty"`
}

// subscriber 用于本次流程后续判断的subscriber
type subscriber struct {
	accounts map[string]struct{}
	ch       chan Event
}

// Service 用于本次流程后续判断的Service
type Service struct {
	// repository 提供聊天服务所需的最小持久化能力。
	repository Repository
	mu         sync.RWMutex
	next       uint64
	subs       map[uint64]subscriber
}

// New 封装New业务协调。
func New(store *db.Store) *Service {
	return NewWithRepository(newStoreRepository(store))
}

// NewWithRepository 使用窄 repository 构造聊天服务，便于应用层和测试隔离数据库聚合器。
func NewWithRepository(repository Repository) *Service {
	return &Service{repository: repository, subs: make(map[uint64]subscriber)}
}

// Subscribe 封装Subscribe业务协调。
func (s *Service) Subscribe(ctx context.Context, userID int64) (<-chan Event, func(), error) {
	accountIDs, err := s.repository.ListOwnedIDs(ctx, userID) // accountIDs 和 err 是用户账号 ID 列表及查询错误。
	if err != nil {
		return nil, nil, err
	}
	// allowed 用于本次流程后续判断的allowed
	allowed := make(map[string]struct{}, len(accountIDs))
	for _, accountID := range accountIDs { // accountID 是当前订阅允许接收事件的账号。
		allowed[accountID] = struct{}{}
	}
	s.mu.Lock()
	s.next++
	// id 用于本次流程后续判断的标识
	id := s.next
	// ch 用于本次流程后续判断的ch
	ch := make(chan Event, 128)
	s.subs[id] = subscriber{accounts: allowed, ch: ch}
	s.mu.Unlock()
	// once 用于本次流程后续判断的once
	var once sync.Once
	// cancel 用于本次流程后续判断的取消
	cancel := func() {
		once.Do(func() {
			s.mu.Lock()
			if // sub、ok 用于本次流程后续判断的sub、ok
			sub, ok := s.subs[id]; ok {
				delete(s.subs, id)
				close(sub.ch)
			}
			s.mu.Unlock()
		})
	}
	return ch, cancel, nil
}

// Publish 封装发布业务协调。
func (s *Service) Publish(accountID string, event Event) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// sub 表示当前遍历过程中的sub
	for _, sub := range s.subs {
		if // ok 用于本次流程后续判断的ok
		_, ok := sub.accounts[accountID]; !ok {
			continue
		}
		select {
		case sub.ch <- event:
		default:
			// A slow browser must not block the account receive loop. The client
			// reconnects and reloads authoritative history when its buffer fills.
		}
	}
}

// RecordIncoming 封装RecordIncoming业务协调。
func (s *Service) RecordIncoming(ctx context.Context, in Incoming) (*db.ChatMessage, bool, error) {
	if s == nil || s.repository == nil {
		return nil, false, fmt.Errorf("聊天服务未初始化")
	}
	// sentAt 用于本次流程后续判断的sentAt
	sentAt := extractUnixMilli(in.Raw)
	if sentAt == 0 {
		sentAt = time.Now().UTC().UnixMilli()
	}
	// key 优先使用引擎提供的稳定平台消息 ID，缺失时再从原始消息递归提取。
	key := strings.TrimSpace(in.MessageID)
	if key == "" {
		key = extractString(in.Raw, "messageId", "message_id", "msgId", "mid", "uuid")
	}
	if key == "" {
		// raw 用于本次流程后续判断的原始
		raw, _ := json.Marshal(in.Raw)
		// digest 用于本次流程后续判断的digest
		digest := sha256.Sum256([]byte(in.AccountID + "\x00" + in.ChatID + "\x00" + in.BuyerID + "\x00" + in.Text + "\x00" + string(raw)))
		key = "in-" + hex.EncodeToString(digest[:16])
	}
	// session 用于本次流程后续判断的会话
	session := db.ChatSession{CookieID: in.AccountID, ChatID: in.ChatID, BuyerID: in.BuyerID,
		BuyerName: in.BuyerName, BuyerAvatar: extractString(in.Raw, "avatar", "avatarUrl", "senderAvatar"),
		ItemID: in.ItemID, ItemTitle: extractString(in.Raw, "itemTitle", "title"), ItemImageURL: extractString(in.Raw, "itemMainPic", "itemImage", "itemImageUrl")}
	// messageType、content 用于本次流程后续判断的消息Type、content
	messageType, content := extractMessageContent(in.Raw, in.Text)
	// mediaDuration 保存平台语音载荷的秒级时长；非语音或缺失时保持零值。
	mediaDuration := extractMediaDuration(in.Raw, messageType)
	if isOfficialSystemMessage(in.Raw, in.BuyerID, in.Text) {
		messageType = "system"
		if strings.TrimSuffix(strings.TrimSpace(in.BuyerID), "@goofish") == "1400" {
			in.BuyerName = "闲小蜜"
			session.BuyerName = "闲小蜜"
			session.BuyerAvatar = xianxiaomiAvatar
		}
	}
	// message 用于本次流程后续判断的消息
	message := db.ChatMessage{MessageKey: key, Direction: "incoming", SenderID: in.BuyerID,
		SenderName: in.BuyerName, MessageType: messageType, Content: content, MediaDuration: mediaDuration, Status: "received", SentAt: sentAt}
	// stored、inserted、err 保存落库消息、首次插入标识及错误；系统消息永不增加用户红点。
	stored, inserted, err := s.repository.SaveMessage(ctx, session, message, messageType != "system")
	if err == nil && inserted {
		s.Publish(in.AccountID, Event{Type: "message.created", Message: stored, Session: &session})
	}
	return stored, inserted, err
}

// RecordHistoryPage normalizes official IM history and stores it idempotently.
// RecordHistoryPage 封装RecordHistory页码业务协调。
func (s *Service) RecordHistoryPage(ctx context.Context, accountID, chatID, myID string, session db.ChatSession, body map[string]any) (HistoryPage, error) {
	// page 用于本次流程后续判断的页码
	page := HistoryPage{HasMore: boolValue(body["hasMore"]), NextCursor: int64Value(body["nextCursor"])}
	// models 用于本次流程后续判断的模型列表
	models, _ := body["userMessageModels"].([]any)
	for                                 // i 用于本次流程后续判断的i
	i := len(models) - 1; i >= 0; i-- { // official API returns newest first
		// model 用于本次流程后续判断的模型
		model, _ := models[i].(map[string]any)
		// message、ok 用于本次流程后续判断的message、ok
		message, ok := parseHistoryMessage(accountID, chatID, myID, model)
		if !ok {
			continue
		}
		session.CookieID, session.ChatID = accountID, chatID
		if message.Direction == "incoming" && message.MessageType != "system" {
			if session.BuyerID == "" {
				session.BuyerID = message.SenderID
			}
		}
		// stored、err 用于本次流程后续判断的stored、err
		stored, _, err := s.repository.SaveMessage(ctx, session, message, false)
		if err != nil {
			return page, err
		}
		if message.MessageType != "text" && (stored.MessageType != message.MessageType || stored.Content != message.Content) {
			// updateErr 保存历史接口用真实媒体地址纠正已有占位消息时的持久化错误。
			if updateErr := s.repository.UpdateMessageContent(ctx, accountID, message.MessageKey, message.MessageType, message.Content); updateErr != nil {
				return page, updateErr
			}
			stored.MessageType = message.MessageType
			stored.Content = message.Content
		}
		if message.MediaDuration > 0 && stored.MediaDuration != message.MediaDuration {
			// durationErr 保存把历史语音时长补齐到既有消息行时的持久化错误。
			if durationErr := s.repository.UpdateMessageMediaDuration(ctx, accountID, message.MessageKey, message.MediaDuration); durationErr != nil {
				return page, durationErr
			}
			stored.MediaDuration = message.MediaDuration
		}
		page.Messages = append(page.Messages, *stored)
	}
	return page, nil
}

// parseHistoryMessage 封装parseHistory消息业务协调。
func parseHistoryMessage(accountID, chatID, myID string, model map[string]any) (db.ChatMessage, bool) {
	// message 用于本次流程后续判断的消息
	message, _ := model["message"].(map[string]any)
	if message == nil {
		return db.ChatMessage{}, false
	}
	// extension 用于本次流程后续判断的extension
	extension := mapValue(message["extension"])
	// senderID 用于本次流程后续判断的senderID
	senderID := strings.Split(strings.TrimSpace(fmt.Sprint(extension["senderUserId"])), "@")[0]
	// senderName 用于本次流程后续判断的sender名称
	senderName := strings.TrimSpace(fmt.Sprint(extension["reminderTitle"]))
	if senderName == "<nil>" {
		senderName = ""
	}
	// key 用于本次流程后续判断的key
	key := strings.TrimSpace(fmt.Sprint(message["messageId"]))
	if key == "" || key == "<nil>" {
		return db.ChatMessage{}, false
	}
	// contentMap 用于本次流程后续判断的内容Map
	contentMap, _ := message["content"].(map[string]any)
	// custom 用于本次流程后续判断的custom
	custom, _ := contentMap["custom"].(map[string]any)
	// rawContent 用于本次流程后续判断的原始内容
	rawContent := map[string]any{}
	if // encoded 用于本次流程后续判断的encoded
	encoded := strings.TrimSpace(fmt.Sprint(custom["data"])); encoded != "" && encoded != "<nil>" {
		if // decoded、err 用于本次流程后续判断的decoded、err
		decoded, err := base64.StdEncoding.DecodeString(encoded); err == nil {
			_ = json.Unmarshal(decoded, &rawContent)
		}
	}
	// fallback 用于本次流程后续判断的fallback
	fallback := strings.TrimSpace(fmt.Sprint(custom["summary"]))
	if fallback == "<nil>" {
		fallback = ""
	}
	if // textBlock、ok 用于本次流程后续判断的文本Block、ok
	textBlock, ok := rawContent["text"].(map[string]any); ok {
		if // text 用于本次流程后续判断的文本
		text := strings.TrimSpace(fmt.Sprint(textBlock["text"])); text != "" && text != "<nil>" {
			fallback = text
		}
	}
	// messageType、content 用于本次流程后续判断的消息Type、content
	messageType, content := extractMessageContent(rawContent, fallback)
	// mediaDuration 保存历史自定义载荷提供的语音时长，单位为秒。
	mediaDuration := extractMediaDuration(rawContent, messageType)
	if isOfficialSystemMessage(rawContent, senderID, fallback) {
		messageType = "system"
		if senderID == "1400" {
			senderName = "闲小蜜"
		}
	}
	if content == "" {
		content = "[系统消息]"
	}
	// direction、status 用于本次流程后续判断的direction、status
	direction, status := "incoming", "received"
	if senderID != "" && senderID == strings.TrimSpace(myID) {
		direction, status = "outgoing", "sent"
	}
	return db.ChatMessage{CookieID: accountID, ChatID: chatID, MessageKey: key, Direction: direction,
		SenderID: senderID, SenderName: senderName, MessageType: messageType, Content: content,
		MediaDuration: mediaDuration, Status: status, SentAt: int64Value(message["createAt"])}, true
}

// mapValue 封装map值业务协调。
func mapValue(value any) map[string]any {
	if // result、ok 用于本次流程后续判断的result、ok
	result, ok := value.(map[string]any); ok {
		return result
	}
	if // text、ok 用于本次流程后续判断的text、ok
	text, ok := value.(string); ok {
		// result 用于本次流程后续判断的结果
		var result map[string]any
		if json.Unmarshal([]byte(text), &result) == nil {
			return result
		}
	}
	return map[string]any{}
}

// int64Value 封装int64值业务协调。
func int64Value(value any) int64 {
	switch // typed 用于本次流程后续判断的typed
	typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	case json.Number:
		// result 用于本次流程后续判断的结果
		result, _ := typed.Int64()
		return result
	case string:
		// result 用于本次流程后续判断的结果
		var result int64
		_, _ = fmt.Sscan(strings.TrimSpace(typed), &result)
		return result
	}
	return 0
}

// boolValue 封装bool值业务协调。
func boolValue(value any) bool {
	switch // typed 用于本次流程后续判断的typed
	typed := value.(type) {
	case bool:
		return typed
	case float64:
		return typed == 1
	case int:
		return typed == 1
	case string:
		return typed == "1" || strings.EqualFold(typed, "true")
	}
	return false
}

// extractMessageContent 封装extract消息内容业务协调。
func extractMessageContent(raw map[string]any, fallback string) (string, string) {
	// inspect 用于本次流程后续判断的inspect
	var inspect func(any) (string, string)
	inspect = func(value any) (string, string) {
		switch // typed 用于本次流程后续判断的typed
		typed := value.(type) {
		case string:
			// nested 用于本次流程后续判断的nested
			var nested any
			if json.Unmarshal([]byte(typed), &nested) == nil {
				return inspect(nested)
			}
		case map[string]any:
			// contentType 用于本次流程后续判断的内容类型
			contentType := strings.TrimSpace(fmt.Sprint(typed["contentType"]))
			if contentType == "2" {
				if // mediaURL 用于本次流程后续判断的mediaURL
				mediaURL := extractString(typed["image"], "url"); mediaURL != "" {
					return "image", mediaURL
				}
			}
			if contentType == "4" || typed["video"] != nil {
				if // mediaURL 用于本次流程后续判断的mediaURL
				mediaURL := extractString(typed["video"], "url", "videoUrl", "playUrl"); mediaURL != "" {
					return "video", mediaURL
				}
			}
			if contentType == "3" || typed["audio"] != nil {
				// mediaURL 保存闲鱼语音载荷中的 AMR 地址；同一载荷的秒级时长由独立解析器持久化。
				if mediaURL := extractString(typed["audio"], "url", "audioUrl", "playUrl"); mediaURL != "" {
					return "audio", mediaURL
				}
			}
			// child 表示当前遍历过程中的child
			for _, child := range typed {
				if // kind、mediaURL 用于本次流程后续判断的kind、mediaURL
				kind, mediaURL := inspect(child); mediaURL != "" {
					return kind, mediaURL
				}
			}
		case []any:
			// child 表示当前遍历过程中的child
			for _, child := range typed {
				if // kind、mediaURL 用于本次流程后续判断的kind、mediaURL
				kind, mediaURL := inspect(child); mediaURL != "" {
					return kind, mediaURL
				}
			}
		}
		return "", ""
	}
	if // kind、mediaURL 用于本次流程后续判断的kind、mediaURL
	kind, mediaURL := inspect(raw); mediaURL != "" {
		return kind, mediaURL
	}
	return "text", strings.TrimSpace(fallback)
}

// isOfficialSystemMessage recognizes platform-generated IM content using the
// protocol metadata, rather than matching a growing list of Chinese prompts.
// contentType=14 is a platform notice and contentType=26 is an official trade
// card.  User 1400 is 闲小蜜, whose messages are also not peer chat.
// isOfficialSystemMessage 封装isOfficial系统消息业务协调。
func isOfficialSystemMessage(raw map[string]any, senderID, fallback string) bool {
	if strings.TrimSuffix(strings.TrimSpace(senderID), "@goofish") == "1400" {
		return true
	}
	if // contentType 用于本次流程后续判断的内容类型
	contentType := findOfficialContentType(raw); contentType == "14" || contentType == "26" {
		return true
	}
	return strings.TrimSpace(fallback) == "发来一条新消息"
}

// findOfficialContentType walks decoded history content as well as live WS
// envelopes. History stores the inner content JSON under custom.data, while
// live messages expose it under 1.6.3 or 1.10.extJson.
// findOfficialContentType 封装findOfficial内容类型业务协调。
func findOfficialContentType(value any) string {
	// found 用于本次流程后续判断的found
	var found string
	// walk 用于本次流程后续判断的walk
	var walk func(any)
	walk = func(current any) {
		if found != "" {
			return
		}
		switch // typed 用于本次流程后续判断的typed
		typed := current.(type) {
		case string:
			// nested 用于本次流程后续判断的nested
			var nested any
			if json.Unmarshal([]byte(typed), &nested) == nil {
				walk(nested)
			}
		case map[string]any:
			if // candidate 用于本次流程后续判断的candidate
			candidate := strings.TrimSpace(fmt.Sprint(typed["contentType"])); candidate == "14" || candidate == "26" {
				found = candidate
				return
			}
			// child 表示当前遍历过程中的child
			for _, child := range typed {
				walk(child)
			}
		case []any:
			// child 表示当前遍历过程中的child
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	return found
}

// CreateOutgoing 创建Outgoing。
func (s *Service) CreateOutgoing(ctx context.Context, session db.ChatSession, text string) (*db.ChatMessage, error) {
	return s.CreateOutgoingMedia(ctx, session, "text", strings.TrimSpace(text))
}

// CreateOutgoingMedia 创建OutgoingMedia。
func (s *Service) CreateOutgoingMedia(ctx context.Context, session db.ChatSession, messageType, content string) (*db.ChatMessage, error) {
	// key 用于本次流程后续判断的key
	key := "local-" + randomID()
	// message 用于本次流程后续判断的消息
	message := db.ChatMessage{MessageKey: key, Direction: "outgoing", SenderID: session.CookieID,
		SenderName: "我", MessageType: messageType, Content: strings.TrimSpace(content), Status: "sending",
		SentAt: time.Now().UTC().UnixMilli()}
	// stored、err 用于本次流程后续判断的stored、err
	stored, _, err := s.repository.SaveMessage(ctx, session, message, false)
	if err == nil {
		s.Publish(session.CookieID, Event{Type: "message.created", Message: stored, Session: &session})
	}
	return stored, err
}

// RecordOutgoingSent captures automatic replies and automation messages. A
// supplied key correlates a UI pending message and only updates its status.
// RecordOutgoingSent 封装RecordOutgoingSent业务协调。
func (s *Service) RecordOutgoingSent(ctx context.Context, session db.ChatSession, key, text string) (*db.ChatMessage, error) {
	if strings.TrimSpace(key) != "" {
		return s.SetOutgoingStatus(ctx, session.CookieID, key, "sent")
	}
	// message 用于本次流程后续判断的消息
	message := db.ChatMessage{MessageKey: "sent-" + randomID(), Direction: "outgoing", SenderID: session.CookieID,
		SenderName: "我", MessageType: "text", Content: strings.TrimSpace(text), Status: "sent",
		SentAt: time.Now().UTC().UnixMilli()}
	// stored、err 用于本次流程后续判断的stored、err
	stored, _, err := s.repository.SaveMessage(ctx, session, message, false)
	if err == nil {
		s.Publish(session.CookieID, Event{Type: "message.created", Message: stored, Session: &session})
	}
	return stored, err
}

// SetOutgoingStatus 设置Outgoing状态。
func (s *Service) SetOutgoingStatus(ctx context.Context, accountID, key, status string) (*db.ChatMessage, error) {
	// message、err 用于本次流程后续判断的message、err
	message, err := s.repository.UpdateMessageStatus(ctx, accountID, key, status)
	if err == nil {
		s.Publish(accountID, Event{Type: "message.updated", Message: message})
	}
	return message, err
}

// MarkOutgoingRead 根据平台回执把指定出站消息标记已读并广播增量事件。
func (s *Service) MarkOutgoingRead(ctx context.Context, accountID, key string, readAt int64) (*db.ChatMessage, error) {
	// message、err 保存已读更新后的消息及持久化错误。
	message, err := s.repository.MarkMessageRead(ctx, accountID, key, readAt)
	if err != nil {
		return nil, err
	}
	if message == nil || message.Direction != "outgoing" {
		return nil, nil
	}
	s.Publish(accountID, Event{Type: "message.updated", Message: message})
	return message, nil
}

// MarkLatestOutgoingRead 在回执未带幂等键时回退标记会话最近待确认的出站消息。
func (s *Service) MarkLatestOutgoingRead(ctx context.Context, accountID, chatID string, readAt int64) (*db.ChatMessage, error) {
	// message、err 保存回退更新后的消息及持久化错误。
	message, err := s.repository.MarkLatestOutgoingRead(ctx, accountID, chatID, readAt)
	if err == nil {
		s.Publish(accountID, Event{Type: "message.updated", Message: message})
	}
	return message, err
}
