package chat

import (
	"context"
	"crypto/rand"
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

type HistoryPage struct {
	Messages   []db.ChatMessage
	HasMore    bool
	NextCursor int64
}

type ConversationPage struct {
	HasMore    bool
	NextCursor int64
}

const xianxiaomiAvatar = "https://img.alicdn.com/imgextra/i2/O1CN01rxBFRr1II3BU0as29_!!6000000000869-2-tps-144-144.png_110x10000.jpg_.webp"

func (s *Service) RecordConversationPage(ctx context.Context, accountID, myID string, body map[string]any) (ConversationPage, error) {
	page := ConversationPage{HasMore: boolValue(body["hasMore"]), NextCursor: int64Value(body["nextCursor"])}
	items, _ := body["userConvs"].([]any)
	for _, item := range items {
		wrapper, _ := item.(map[string]any)
		conv := wrapper
		if nested, ok := wrapper["singleChatUserConversation"].(map[string]any); ok {
			conv = nested
		}
		single, _ := conv["singleChatConversation"].(map[string]any)
		cid := strings.Split(strings.TrimSpace(fmt.Sprint(single["cid"])), "@")[0]
		if visible, exists := conv["visible"]; exists && int64Value(visible) == 0 {
			if cid != "" && cid != "<nil>" {
				_ = s.store.Chats.DeleteSession(ctx, accountID, cid)
			}
			continue
		}
		first := strings.Split(strings.TrimSpace(fmt.Sprint(single["pairFirst"])), "@")[0]
		second := strings.Split(strings.TrimSpace(fmt.Sprint(single["pairSecond"])), "@")[0]
		ext := mapValue(single["extension"])
		if second == "0" && cleanNilString(ext["extUserId"]) != "1400" {
			_ = s.store.Chats.DeleteSession(ctx, accountID, cid)
			continue
		}
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
		lastWrap, _ := conv["lastMessage"].(map[string]any)
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
		peerName := ""
		custom := map[string]any{}
		extension := mapValue(last["extension"])
		if content, ok := last["content"].(map[string]any); ok {
			custom, _ = content["custom"].(map[string]any)
		}
		summary := cleanNilString(custom["summary"])
		if summary == "" {
			summary = cleanNilString(extension["reminderContent"])
		}
		if summary == "" {
			summary = cleanNilString(extension["detailNotice"])
		}
		if summary == "" {
			if encoded := cleanNilString(custom["data"]); encoded != "" {
				var decoded map[string]any
				if raw, err := base64.StdEncoding.DecodeString(encoded); err == nil && json.Unmarshal(raw, &decoded) == nil {
					fallback := ""
					if textBlock, ok := decoded["text"].(map[string]any); ok {
						fallback = cleanNilString(textBlock["text"])
					}
					_, summary = extractMessageContent(decoded, fallback)
				}
			}
		}
		if summary == "" {
			summary = "暂无消息"
		}
		avatar := ""
		if peerID == "1400" {
			peerName, avatar = "闲小蜜", xianxiaomiAvatar
		}
		modifyTime := int64Value(conv["modifyTime"])
		lastMessageAt := int64Value(last["createAt"])
		if lastMessageAt <= 0 {
			lastMessageAt = modifyTime
		}
		session := db.ChatSession{CookieID: accountID, ChatID: cid, BuyerID: peerID, BuyerName: peerName, BuyerAvatar: avatar,
			ItemID: cleanNilString(ext["itemId"]), ItemTitle: cleanNilString(ext["itemTitle"]), LastMessage: summary,
			LastMessageAt: lastMessageAt, UnreadCount: int(int64Value(conv["redPoint"]))}
		if err := s.store.Chats.UpsertSession(ctx, session); err != nil {
			return page, err
		}
		if err := s.store.Chats.SyncSessionSummary(ctx, accountID, cid, summary, lastMessageAt, modifyTime, session.UnreadCount); err != nil {
			return page, err
		}
	}
	return page, nil
}

var invalidNicknames = map[string]struct{}{
	"交易消息": {}, "系统消息": {}, "卡片消息": {}, "我完成了评价": {}, "对方完成了评价": {},
	"快给ta一个评价吧～": {}, "卖家已发货": {}, "买家已付款": {}, "买家已确认收货": {},
	"等待您发货": {}, "超时未付款，系统关闭了订单": {}, "邀您填写售后问卷": {},
}

func ValidNickname(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if strings.Contains(value, "***") {
		return false
	}
	if _, err := strconv.ParseUint(value, 10, 64); err == nil {
		return false
	}
	trimmed := strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
	if _, invalid := invalidNicknames[trimmed]; invalid {
		return false
	}
	return !strings.Contains(value, "发来一条新消息")
}

func cleanNilString(value any) string {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "<nil>" {
		return ""
	}
	return text
}

type Incoming struct {
	AccountID string
	ChatID    string
	BuyerID   string
	BuyerName string
	Text      string
	ItemID    string
	Raw       map[string]any
}

type Event struct {
	Type    string          `json:"type"`
	Message *db.ChatMessage `json:"message,omitempty"`
	Session *db.ChatSession `json:"session,omitempty"`
}

type subscriber struct {
	accounts map[string]struct{}
	ch       chan Event
}

type Service struct {
	store *db.Store
	mu    sync.RWMutex
	next  uint64
	subs  map[uint64]subscriber
}

func New(store *db.Store) *Service {
	return &Service{store: store, subs: make(map[uint64]subscriber)}
}

func (s *Service) Subscribe(ctx context.Context, userID int64) (<-chan Event, func(), error) {
	accounts, err := s.store.Cookies.AllForUser(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	allowed := make(map[string]struct{}, len(accounts))
	for accountID := range accounts {
		allowed[accountID] = struct{}{}
	}
	s.mu.Lock()
	s.next++
	id := s.next
	ch := make(chan Event, 128)
	s.subs[id] = subscriber{accounts: allowed, ch: ch}
	s.mu.Unlock()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			s.mu.Lock()
			if sub, ok := s.subs[id]; ok {
				delete(s.subs, id)
				close(sub.ch)
			}
			s.mu.Unlock()
		})
	}
	return ch, cancel, nil
}

func (s *Service) Publish(accountID string, event Event) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sub := range s.subs {
		if _, ok := sub.accounts[accountID]; !ok {
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

func (s *Service) RecordIncoming(ctx context.Context, in Incoming) (*db.ChatMessage, bool, error) {
	if s == nil || s.store == nil || s.store.Chats == nil {
		return nil, false, fmt.Errorf("聊天服务未初始化")
	}
	sentAt := extractUnixMilli(in.Raw)
	if sentAt == 0 {
		sentAt = time.Now().UTC().UnixMilli()
	}
	key := extractString(in.Raw, "messageId", "message_id", "msgId", "mid", "uuid")
	if key == "" {
		raw, _ := json.Marshal(in.Raw)
		digest := sha256.Sum256([]byte(in.AccountID + "\x00" + in.ChatID + "\x00" + in.BuyerID + "\x00" + in.Text + "\x00" + string(raw)))
		key = "in-" + hex.EncodeToString(digest[:16])
	}
	session := db.ChatSession{CookieID: in.AccountID, ChatID: in.ChatID, BuyerID: in.BuyerID,
		BuyerName: in.BuyerName, BuyerAvatar: extractString(in.Raw, "avatar", "avatarUrl", "senderAvatar"),
		ItemID: in.ItemID, ItemTitle: extractString(in.Raw, "itemTitle", "title")}
	messageType, content := extractMessageContent(in.Raw, in.Text)
	if isOfficialSystemMessage(in.Raw, in.BuyerID, in.Text) {
		messageType = "system"
		if strings.TrimSuffix(strings.TrimSpace(in.BuyerID), "@goofish") == "1400" {
			in.BuyerName = "闲小蜜"
			session.BuyerName = "闲小蜜"
			session.BuyerAvatar = xianxiaomiAvatar
		}
	}
	message := db.ChatMessage{MessageKey: key, Direction: "incoming", SenderID: in.BuyerID,
		SenderName: in.BuyerName, MessageType: messageType, Content: content, Status: "received", SentAt: sentAt}
	stored, inserted, err := s.store.Chats.SaveMessage(ctx, session, message, true)
	if err == nil && inserted {
		s.Publish(in.AccountID, Event{Type: "message.created", Message: stored, Session: &session})
	}
	return stored, inserted, err
}

// RecordHistoryPage normalizes official IM history and stores it idempotently.
func (s *Service) RecordHistoryPage(ctx context.Context, accountID, chatID, myID string, session db.ChatSession, body map[string]any) (HistoryPage, error) {
	page := HistoryPage{HasMore: boolValue(body["hasMore"]), NextCursor: int64Value(body["nextCursor"])}
	models, _ := body["userMessageModels"].([]any)
	for i := len(models) - 1; i >= 0; i-- { // official API returns newest first
		model, _ := models[i].(map[string]any)
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
		stored, _, err := s.store.Chats.SaveMessage(ctx, session, message, false)
		if err != nil {
			return page, err
		}
		if message.MessageType == "system" {
			if err := s.store.Chats.UpdateMessageType(ctx, accountID, message.MessageKey, "system"); err != nil {
				return page, err
			}
			stored.MessageType = "system"
		}
		page.Messages = append(page.Messages, *stored)
	}
	return page, nil
}

func parseHistoryMessage(accountID, chatID, myID string, model map[string]any) (db.ChatMessage, bool) {
	message, _ := model["message"].(map[string]any)
	if message == nil {
		return db.ChatMessage{}, false
	}
	extension := mapValue(message["extension"])
	senderID := strings.Split(strings.TrimSpace(fmt.Sprint(extension["senderUserId"])), "@")[0]
	senderName := strings.TrimSpace(fmt.Sprint(extension["reminderTitle"]))
	if senderName == "<nil>" {
		senderName = ""
	}
	key := strings.TrimSpace(fmt.Sprint(message["messageId"]))
	if key == "" || key == "<nil>" {
		return db.ChatMessage{}, false
	}
	contentMap, _ := message["content"].(map[string]any)
	custom, _ := contentMap["custom"].(map[string]any)
	rawContent := map[string]any{}
	if encoded := strings.TrimSpace(fmt.Sprint(custom["data"])); encoded != "" && encoded != "<nil>" {
		if decoded, err := base64.StdEncoding.DecodeString(encoded); err == nil {
			_ = json.Unmarshal(decoded, &rawContent)
		}
	}
	fallback := strings.TrimSpace(fmt.Sprint(custom["summary"]))
	if fallback == "<nil>" {
		fallback = ""
	}
	if textBlock, ok := rawContent["text"].(map[string]any); ok {
		if text := strings.TrimSpace(fmt.Sprint(textBlock["text"])); text != "" && text != "<nil>" {
			fallback = text
		}
	}
	messageType, content := extractMessageContent(rawContent, fallback)
	if isOfficialSystemMessage(rawContent, senderID, fallback) {
		messageType = "system"
		if senderID == "1400" {
			senderName = "闲小蜜"
		}
	}
	if content == "" {
		content = "[系统消息]"
	}
	direction, status := "incoming", "received"
	if senderID != "" && senderID == strings.TrimSpace(myID) {
		direction, status = "outgoing", "sent"
	}
	return db.ChatMessage{CookieID: accountID, ChatID: chatID, MessageKey: key, Direction: direction,
		SenderID: senderID, SenderName: senderName, MessageType: messageType, Content: content,
		Status: status, SentAt: int64Value(message["createAt"])}, true
}

func mapValue(value any) map[string]any {
	if result, ok := value.(map[string]any); ok {
		return result
	}
	if text, ok := value.(string); ok {
		var result map[string]any
		if json.Unmarshal([]byte(text), &result) == nil {
			return result
		}
	}
	return map[string]any{}
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	case json.Number:
		result, _ := typed.Int64()
		return result
	case string:
		var result int64
		_, _ = fmt.Sscan(strings.TrimSpace(typed), &result)
		return result
	}
	return 0
}

func boolValue(value any) bool {
	switch typed := value.(type) {
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

func extractMessageContent(raw map[string]any, fallback string) (string, string) {
	var inspect func(any) (string, string)
	inspect = func(value any) (string, string) {
		switch typed := value.(type) {
		case string:
			var nested any
			if json.Unmarshal([]byte(typed), &nested) == nil {
				return inspect(nested)
			}
		case map[string]any:
			contentType := strings.TrimSpace(fmt.Sprint(typed["contentType"]))
			if contentType == "2" {
				if mediaURL := extractString(typed["image"], "url"); mediaURL != "" {
					return "image", mediaURL
				}
			}
			if contentType == "4" || typed["video"] != nil {
				if mediaURL := extractString(typed["video"], "url", "videoUrl", "playUrl"); mediaURL != "" {
					return "video", mediaURL
				}
			}
			for _, child := range typed {
				if kind, mediaURL := inspect(child); mediaURL != "" {
					return kind, mediaURL
				}
			}
		case []any:
			for _, child := range typed {
				if kind, mediaURL := inspect(child); mediaURL != "" {
					return kind, mediaURL
				}
			}
		}
		return "", ""
	}
	if kind, mediaURL := inspect(raw); mediaURL != "" {
		return kind, mediaURL
	}
	return "text", strings.TrimSpace(fallback)
}

// isOfficialSystemMessage recognizes platform-generated IM content using the
// protocol metadata, rather than matching a growing list of Chinese prompts.
// contentType=14 is a platform notice and contentType=26 is an official trade
// card.  User 1400 is 闲小蜜, whose messages are also not peer chat.
func isOfficialSystemMessage(raw map[string]any, senderID, fallback string) bool {
	if strings.TrimSuffix(strings.TrimSpace(senderID), "@goofish") == "1400" {
		return true
	}
	if contentType := findOfficialContentType(raw); contentType == "14" || contentType == "26" {
		return true
	}
	return strings.TrimSpace(fallback) == "发来一条新消息"
}

// findOfficialContentType walks decoded history content as well as live WS
// envelopes. History stores the inner content JSON under custom.data, while
// live messages expose it under 1.6.3 or 1.10.extJson.
func findOfficialContentType(value any) string {
	var found string
	var walk func(any)
	walk = func(current any) {
		if found != "" {
			return
		}
		switch typed := current.(type) {
		case string:
			var nested any
			if json.Unmarshal([]byte(typed), &nested) == nil {
				walk(nested)
			}
		case map[string]any:
			if candidate := strings.TrimSpace(fmt.Sprint(typed["contentType"])); candidate == "14" || candidate == "26" {
				found = candidate
				return
			}
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(value)
	return found
}

func (s *Service) CreateOutgoing(ctx context.Context, session db.ChatSession, text string) (*db.ChatMessage, error) {
	return s.CreateOutgoingMedia(ctx, session, "text", strings.TrimSpace(text))
}

func (s *Service) CreateOutgoingMedia(ctx context.Context, session db.ChatSession, messageType, content string) (*db.ChatMessage, error) {
	key := "local-" + randomID()
	message := db.ChatMessage{MessageKey: key, Direction: "outgoing", SenderID: session.CookieID,
		SenderName: "我", MessageType: messageType, Content: strings.TrimSpace(content), Status: "sending",
		SentAt: time.Now().UTC().UnixMilli()}
	stored, _, err := s.store.Chats.SaveMessage(ctx, session, message, false)
	if err == nil {
		s.Publish(session.CookieID, Event{Type: "message.created", Message: stored, Session: &session})
	}
	return stored, err
}

// RecordOutgoingSent captures automatic replies and automation messages. A
// supplied key correlates a UI pending message and only updates its status.
func (s *Service) RecordOutgoingSent(ctx context.Context, session db.ChatSession, key, text string) (*db.ChatMessage, error) {
	if strings.TrimSpace(key) != "" {
		return s.SetOutgoingStatus(ctx, session.CookieID, key, "sent")
	}
	message := db.ChatMessage{MessageKey: "sent-" + randomID(), Direction: "outgoing", SenderID: session.CookieID,
		SenderName: "我", MessageType: "text", Content: strings.TrimSpace(text), Status: "sent",
		SentAt: time.Now().UTC().UnixMilli()}
	stored, _, err := s.store.Chats.SaveMessage(ctx, session, message, false)
	if err == nil {
		s.Publish(session.CookieID, Event{Type: "message.created", Message: stored, Session: &session})
	}
	return stored, err
}

func (s *Service) SetOutgoingStatus(ctx context.Context, accountID, key, status string) (*db.ChatMessage, error) {
	message, err := s.store.Chats.UpdateMessageStatus(ctx, accountID, key, status)
	if err == nil {
		s.Publish(accountID, Event{Type: "message.updated", Message: message})
	}
	return message, err
}

func randomID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err == nil {
		return hex.EncodeToString(value[:])
	}
	return fmt.Sprintf("%d", time.Now().UTC().UnixNano())
}

func extractString(value any, keys ...string) string {
	wanted := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		wanted[strings.ToLower(key)] = struct{}{}
	}
	var walk func(any) string
	walk = func(v any) string {
		switch x := v.(type) {
		case map[string]any:
			for key, child := range x {
				if _, ok := wanted[strings.ToLower(key)]; ok {
					if text := strings.TrimSpace(fmt.Sprint(child)); text != "" && text != "<nil>" {
						return text
					}
				}
			}
			for _, child := range x {
				if text := walk(child); text != "" {
					return text
				}
			}
		case []any:
			for _, child := range x {
				if text := walk(child); text != "" {
					return text
				}
			}
		}
		return ""
	}
	return walk(value)
}

func extractUnixMilli(raw map[string]any) int64 {
	text := extractString(raw, "sendTime", "timestamp", "time", "createdAt")
	var value int64
	_, _ = fmt.Sscan(text, &value)
	if value > 0 && value < 10_000_000_000 {
		value *= 1000
	}
	return value
}
