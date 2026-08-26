package chat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"xianyu-go/internal/db"
)

// markReadRepository 是只替换指定消息已读结果的聊天仓储测试替身。
type markReadRepository struct {
	fakeRepository
	// message 保存本次已读更新返回的消息，可用于切换出站、入站和空结果分支。
	message *db.ChatMessage
	// err 保存仓储模拟返回的持久化错误。
	err error
}

// unreadRepository 是只替换本地未读统计结果的聊天仓储测试替身。
type unreadRepository struct {
	fakeRepository
	// unread 保存本地用户消息未读数。
	unread int
	// err 保存本地未读统计查询错误。
	err error
}

// CountUnreadUserMessages 返回测试预置的本地未读统计结果。
func (repository *unreadRepository) CountUnreadUserMessages(_ context.Context, _ string, _ string) (int, error) {
	return repository.unread, repository.err
}

// MarkMessageRead 返回测试预置的消息和错误。
func (repository *markReadRepository) MarkMessageRead(_ context.Context, _ string, _ string, _ int64) (*db.ChatMessage, error) {
	return repository.message, repository.err
}

// TestMarkOutgoingReadCoversDirectionAndErrorBranches 覆盖指定出站消息已读回执的方向、空值和错误分支。
func TestMarkOutgoingReadCoversDirectionAndErrorBranches(t *testing.T) {
	// outgoingService 保存返回出站消息的聊天服务。
	outgoingService := NewWithRepository(&markReadRepository{message: &db.ChatMessage{Direction: "outgoing", MessageKey: "outgoing"}})
	// outgoingMessage、outgoingErr 保存出站消息已读结果。
	outgoingMessage, outgoingErr := outgoingService.MarkOutgoingRead(context.Background(), "account", "outgoing", 1)
	if outgoingErr != nil || outgoingMessage == nil || outgoingMessage.Direction != "outgoing" {
		t.Fatalf("outgoing message=%+v err=%v", outgoingMessage, outgoingErr)
	}
	// incomingService 保存返回入站消息的聊天服务。
	incomingService := NewWithRepository(&markReadRepository{message: &db.ChatMessage{Direction: "incoming", MessageKey: "incoming"}})
	// incomingMessage、incomingErr 保存入站消息被忽略后的结果。
	incomingMessage, incomingErr := incomingService.MarkOutgoingRead(context.Background(), "account", "incoming", 1)
	if incomingErr != nil || incomingMessage != nil {
		t.Fatalf("incoming message=%+v err=%v", incomingMessage, incomingErr)
	}
	// emptyService 保存返回空消息的聊天服务。
	emptyService := NewWithRepository(&markReadRepository{})
	// emptyMessage、emptyErr 保存空仓储结果。
	emptyMessage, emptyErr := emptyService.MarkOutgoingRead(context.Background(), "account", "missing", 1)
	if emptyErr != nil || emptyMessage != nil {
		t.Fatalf("empty message=%+v err=%v", emptyMessage, emptyErr)
	}
	// expectedErr 保存仓储失败时应原样传播的错误。
	expectedErr := errors.New("mark read failed")
	// errorService 保存返回持久化错误的聊天服务。
	errorService := NewWithRepository(&markReadRepository{err: expectedErr})
	// errorMessage、actualErr 保存错误路径的返回结果。
	errorMessage, actualErr := errorService.MarkOutgoingRead(context.Background(), "account", "error", 1)
	if errorMessage != nil || !errors.Is(actualErr, expectedErr) {
		t.Fatalf("error message=%+v err=%v", errorMessage, actualErr)
	}
}

// TestConversationUnreadCountCoversFallbackRules 覆盖平台红点、本地未读和系统消息扣除规则。
func TestConversationUnreadCountCoversFallbackRules(t *testing.T) {
	// officialZeroService 保存平台没有红点的聊天服务。
	officialZeroService := NewWithRepository(&unreadRepository{unread: 9})
	// got 表示负数平台红点归一后的未读数量。
	if got := officialZeroService.conversationUnreadCount(context.Background(), "account", "chat", "peer", map[string]any{"redPoint": -1}, nil, ""); got != 0 {
		t.Fatalf("negative red point=%d", got)
	}
	// got 表示零平台红点的未读数量。
	if got := officialZeroService.conversationUnreadCount(context.Background(), "account", "chat", "peer", map[string]any{"redPoint": 0}, nil, ""); got != 0 {
		t.Fatalf("zero red point=%d", got)
	}
	// cappedService 保存本地未读数超过官方红点的聊天服务。
	cappedService := NewWithRepository(&unreadRepository{unread: 9})
	// got 表示本地未读数超过官方红点时的封顶结果。
	if got := cappedService.conversationUnreadCount(context.Background(), "account", "chat", "peer", map[string]any{"redPoint": 3}, nil, "普通消息"); got != 3 {
		t.Fatalf("capped local unread=%d", got)
	}
	// localService 保存本地未读数小于官方红点的聊天服务。
	localService := NewWithRepository(&unreadRepository{unread: 2})
	// got 表示本地未读数有效时的展示结果。
	if got := localService.conversationUnreadCount(context.Background(), "account", "chat", "peer", map[string]any{"redPoint": 3}, nil, "普通消息"); got != 2 {
		t.Fatalf("local unread=%d", got)
	}
	// fallbackService 保存本地统计失败时回退官方红点的聊天服务。
	fallbackService := NewWithRepository(&unreadRepository{err: errors.New("unread query failed")})
	// got 表示本地统计失败后的官方红点回退结果。
	if got := fallbackService.conversationUnreadCount(context.Background(), "account", "chat", "peer", map[string]any{"redPoint": 3}, map[string]any{}, "普通消息"); got != 3 {
		t.Fatalf("fallback unread=%d", got)
	}
	// systemService 保存末条消息为系统消息的聊天服务。
	systemService := NewWithRepository(&unreadRepository{err: errors.New("unread query failed")})
	// systemLast 保存带一条未读系统消息的末条消息载荷。
	systemLast := map[string]any{"extension": map[string]any{"senderUserId": "100"}, "unreadCount": 1}
	// got 表示系统消息从官方红点中扣除后的结果。
	if got := systemService.conversationUnreadCount(context.Background(), "account", "chat", "peer", map[string]any{"redPoint": 3}, systemLast, "发来一条新消息"); got != 2 {
		t.Fatalf("system unread=%d", got)
	}
	// got 表示闲小蜜系统会话被完全排除后的结果。
	if got := systemService.conversationUnreadCount(context.Background(), "account", "chat", "1400@goofish", map[string]any{"redPoint": 3}, systemLast, "发来一条新消息"); got != 0 {
		t.Fatalf("xiaomi unread=%d", got)
	}
	// readSystemLast 保存已读系统消息，缺少 unreadCount 时不应扣除一条。
	readSystemLast := map[string]any{"extension": map[string]any{"senderUserId": "100"}, "readStatus": 2}
	// got 表示已读系统消息保持官方红点的结果。
	if got := systemService.conversationUnreadCount(context.Background(), "account", "chat", "peer", map[string]any{"redPoint": 3}, readSystemLast, "发来一条新消息"); got != 3 {
		t.Fatalf("read system unread=%d", got)
	}
}

// historyRepository 是只替换历史消息写入和媒体修复结果的聊天仓储测试替身。
type historyRepository struct {
	fakeRepository
	// saveErr 保存历史消息首次写入错误。
	saveErr error
	// storedMessage 保存仓储返回的既有消息快照。
	storedMessage *db.ChatMessage
	// contentErr 保存媒体内容修复错误。
	contentErr error
	// durationErr 保存语音时长修复错误。
	durationErr error
	// upsertErr、syncErr 保存会话摘要写入和同步错误。
	upsertErr error
	syncErr   error
	// upsertCalls、syncCalls 保存会话写入和同步的调用次数。
	upsertCalls int
	syncCalls   int
}

// UpsertSession 返回预置的会话写入错误。
func (repository *historyRepository) UpsertSession(context.Context, db.ChatSession) error {
	repository.upsertCalls++
	return repository.upsertErr
}

// SyncSessionSummary 返回预置的会话摘要同步错误。
func (repository *historyRepository) SyncSessionSummary(context.Context, string, string, string, int64, int64, int) error {
	repository.syncCalls++
	return repository.syncErr
}

// SaveMessage 返回预置的历史消息写入结果。
func (repository *historyRepository) SaveMessage(_ context.Context, _ db.ChatSession, message db.ChatMessage, _ bool) (*db.ChatMessage, bool, error) {
	if repository.saveErr != nil {
		return nil, false, repository.saveErr
	}
	if repository.storedMessage != nil {
		// stored 保存仓储预置的消息副本，避免测试修改调用方传入的对象。
		stored := *repository.storedMessage
		return &stored, true, nil
	}
	return &message, true, nil
}

// UpdateMessageContent 返回预置的媒体内容修复错误。
func (repository *historyRepository) UpdateMessageContent(context.Context, string, string, string, string) error {
	return repository.contentErr
}

// UpdateMessageMediaDuration 返回预置的语音时长修复错误。
func (repository *historyRepository) UpdateMessageMediaDuration(context.Context, string, string, int64) error {
	return repository.durationErr
}

// historyMessageModel 创建带自定义 JSON 内容的历史消息模型。
func historyMessageModel(messageID, rawContent string) map[string]any {
	// rawBytes 保存待嵌入历史消息的内容 JSON 字节。
	rawBytes, _ := json.Marshal(json.RawMessage(rawContent))
	// encodedContent 保存平台历史协议使用的 Base64 内容。
	encodedContent := base64.StdEncoding.EncodeToString(rawBytes)
	return map[string]any{"message": map[string]any{
		"messageId": messageID,
		"extension": map[string]any{"senderUserId": "peer@goofish"},
		"content":   map[string]any{"custom": map[string]any{"data": encodedContent}},
	}}
}

// TestRecordHistoryPageCoversPersistenceAndMediaErrors 覆盖历史消息写入、媒体内容和语音时长修复错误路径。
func TestRecordHistoryPageCoversPersistenceAndMediaErrors(t *testing.T) {
	// saveErr 保存历史消息首次落库错误。
	saveErr := errors.New("history save failed")
	// saveService 保存历史写入失败的聊天服务。
	saveService := NewWithRepository(&historyRepository{saveErr: saveErr})
	// _, saveResultErr 接收历史消息写入错误。
	_, saveResultErr := saveService.RecordHistoryPage(context.Background(), "account", "chat", "me", db.ChatSession{}, map[string]any{"userMessageModels": []any{historyMessageModel("save", `{"contentType":1,"text":"hello"}`)}})
	if !errors.Is(saveResultErr, saveErr) {
		t.Fatalf("history save error=%v", saveResultErr)
	}
	// contentErr 保存媒体内容修复错误。
	contentErr := errors.New("content repair failed")
	// contentService 保存媒体内容需要修复且修复失败的聊天服务。
	contentService := NewWithRepository(&historyRepository{storedMessage: &db.ChatMessage{MessageType: "text", Content: "placeholder"}, contentErr: contentErr})
	// _, contentResultErr 接收媒体内容修复错误。
	_, contentResultErr := contentService.RecordHistoryPage(context.Background(), "account", "chat", "me", db.ChatSession{}, map[string]any{"userMessageModels": []any{historyMessageModel("image", `{"contentType":2,"image":{"url":"https://image.example/a.png"}}`)}})
	if !errors.Is(contentResultErr, contentErr) {
		t.Fatalf("content repair error=%v", contentResultErr)
	}
	// durationErr 保存语音时长修复错误。
	durationErr := errors.New("duration repair failed")
	// durationService 保存语音时长需要补齐且补齐失败的聊天服务。
	durationService := NewWithRepository(&historyRepository{storedMessage: &db.ChatMessage{MessageType: "audio", Content: "https://audio.example/a.amr"}, durationErr: durationErr})
	// _, durationResultErr 接收语音时长修复错误。
	_, durationResultErr := durationService.RecordHistoryPage(context.Background(), "account", "chat", "me", db.ChatSession{}, map[string]any{"userMessageModels": []any{historyMessageModel("audio", `{"contentType":3,"audio":{"url":"https://audio.example/a.amr","duration":3}}`)}})
	if !errors.Is(durationResultErr, durationErr) {
		t.Fatalf("duration repair error=%v", durationResultErr)
	}
}

// TestRecordConversationPageCoversSessionPersistenceErrors 验证会话写入和摘要同步错误按原样返回。
func TestRecordConversationPageCoversSessionPersistenceErrors(t *testing.T) {
	// body 保存能够进入会话写入分支的最小平台联系人载荷。
	body := map[string]any{"userConvs": []any{map[string]any{
		"singleChatUserConversation": map[string]any{
			"singleChatConversation": map[string]any{"cid": "chat-error@goofish", "pairFirst": "buyer", "pairSecond": "self"},
			"lastMessage":            map[string]any{"message": map[string]any{"createAt": 1, "content": map[string]any{"custom": map[string]any{"summary": "你好"}}}},
		},
	}}}
	// upsertErr 保存会话首次写入错误。
	upsertErr := errors.New("session upsert failed")
	// _, upsertResultErr 保存会话写入错误传播结果。
	upsertRepository := &historyRepository{upsertErr: upsertErr}
	// _, upsertResultErr 保存会话写入错误传播结果。
	_, upsertResultErr := NewWithRepository(upsertRepository).RecordConversationPage(context.Background(), "account", "self", body)
	if upsertRepository.upsertCalls != 1 {
		t.Fatalf("会话写入未调用，调用次数=%d", upsertRepository.upsertCalls)
	}
	if !errors.Is(upsertResultErr, upsertErr) {
		t.Fatalf("会话写入错误=%v", upsertResultErr)
	}
	// syncErr 保存会话摘要同步错误。
	syncErr := errors.New("session sync failed")
	// _, syncResultErr 保存摘要同步错误传播结果。
	syncRepository := &historyRepository{syncErr: syncErr}
	// _, syncResultErr 保存摘要同步错误传播结果。
	_, syncResultErr := NewWithRepository(syncRepository).RecordConversationPage(context.Background(), "account", "self", body)
	if syncRepository.syncCalls != 1 {
		t.Fatalf("会话摘要同步未调用，调用次数=%d", syncRepository.syncCalls)
	}
	if !errors.Is(syncResultErr, syncErr) {
		t.Fatalf("会话摘要同步错误=%v", syncResultErr)
	}
}
