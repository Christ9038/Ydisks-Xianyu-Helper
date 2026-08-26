package chat

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestOutgoingServiceOperations 覆盖聊天服务的本地出站创建、状态更新和已读广播路径。
func TestOutgoingServiceOperations(t *testing.T) {
	// store、cleanup 提供真实聊天数据库及关闭责任。
	store, cleanup := chatTestStore(t)
	defer cleanup()
	// service 保存绑定真实数据库的聊天服务。
	service := New(store)
	// ctx 是本测试共用的数据库上下文。
	ctx := context.Background()
	// session 保存出站消息所属的非敏感会话摘要。
	session := db.ChatSession{CookieID: "account-1", ChatID: "outgoing", BuyerID: "buyer", BuyerName: "买家"}
	// message、createErr 保存本地出站消息创建结果。
	message, createErr := service.CreateOutgoing(ctx, session, "  你好  ")
	if createErr != nil || message == nil || message.Direction != "outgoing" || message.Content != "你好" {
		t.Fatalf("message=%+v err=%v", message, createErr)
	}
	// updated、updateErr 保存出站状态更新结果。
	updated, updateErr := service.SetOutgoingStatus(ctx, session.CookieID, message.MessageKey, "sent")
	if updateErr != nil || updated == nil || updated.Status != "sent" {
		t.Fatalf("updated=%+v err=%v", updated, updateErr)
	}
	// read, readErr 保存指定消息已读回执结果。
	read, readErr := service.MarkOutgoingRead(ctx, session.CookieID, message.MessageKey, 123)
	if readErr != nil || read == nil || read.ReadStatus != 2 || read.ReadAt != 123 {
		t.Fatalf("read=%+v err=%v", read, readErr)
	}
	// second、secondErr 保存另一条待回执出站消息，供会话级回退读取。
	second, secondErr := service.CreateOutgoing(ctx, session, "第二条")
	if secondErr != nil || second == nil {
		t.Fatalf("second=%+v err=%v", second, secondErr)
	}
	// err 表示第二条出站消息进入平台已发送状态的数据库错误。
	if _, err := service.SetOutgoingStatus(ctx, session.CookieID, second.MessageKey, "sent"); err != nil {
		t.Fatal(err)
	}
	// latest、latestErr 保存缺少消息键时的会话级已读回退结果。
	latest, latestErr := service.MarkLatestOutgoingRead(ctx, session.CookieID, session.ChatID, 456)
	if latestErr != nil || latest == nil || latest.ReadStatus != 2 {
		t.Fatalf("latest=%+v err=%v", latest, latestErr)
	}
	// generated 保存随机出站键，验证随机 ID 不为空。
	generated := randomID()
	if generated == "" {
		t.Fatal("randomID should not be empty")
	}
	// nilRepository 验证空 Store 不会构造聊天 repository。
	if newStoreRepository(nil) != nil {
		t.Fatal("nil store repository should be nil")
	}
	// generatedMessage、generatedErr 保存缺少平台消息键时生成本地幂等键的出站消息。
	generatedMessage, generatedErr := NewWithRepository(&fakeRepository{}).RecordOutgoingSent(ctx, session, "", "自动发送")
	if generatedErr != nil || generatedMessage == nil || !strings.HasPrefix(generatedMessage.MessageKey, "sent-") {
		t.Fatalf("缺少消息键的出站记录异常 message=%+v err=%v", generatedMessage, generatedErr)
	}
	// statusErr 保存非 NotFound 状态更新错误，必须直接返回而不能创建官方回显消息。
	statusErr := errors.New("outgoing status failed")
	// failedMessage、failedErr 保存未知状态错误的出站记录结果。
	failedMessage, failedErr := NewWithRepository(&outgoingErrorRepository{statusErr: statusErr}).RecordOutgoingSent(ctx, session, "platform-key", "自动发送")
	if failedMessage != nil || !errors.Is(failedErr, statusErr) {
		t.Fatalf("非 NotFound 出站状态错误异常 message=%+v err=%v", failedMessage, failedErr)
	}
}
