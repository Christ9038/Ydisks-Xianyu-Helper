package chat

import (
	"context"
	"errors"
	"testing"
)

// TestChatMetadataCoversSuccessfulReadsAndMissingPorts 覆盖聊天元数据的命中读取、删除成功及端口缺失分支。
func TestChatMetadataCoversSuccessfulReadsAndMissingPorts(t *testing.T) {
	// repository 保存具备元数据能力且已经存在买家备注的内存替身。
	repository := &metadataTestRepository{
		fakeRepository: &fakeRepository{owned: true},
		note:           BuyerNote{AccountID: "account", BuyerID: "buyer", Content: "已确认", UpdatedAt: 2},
		found:          true,
		replies:        []QuickReply{{ID: 1, AccountID: "account", Content: "您好"}},
	}
	// service 保存待验证的聊天元数据服务。
	service := New(repository)
	// note 保存命中持久化记录后的买家备注。
	note, noteErr := service.GetBuyerNote(context.Background(), 7, " account ", " buyer ")
	if noteErr != nil || note.Content != "已确认" || note.UpdatedAt != 2 {
		t.Fatalf("命中备注异常: note=%+v err=%v", note, noteErr)
	}
	// replies 保存账号下已有的快捷回复集合。
	replies, repliesErr := service.ListQuickReplies(context.Background(), 7, " account ")
	if repliesErr != nil || len(replies) != 1 || replies[0].Content != "您好" {
		t.Fatalf("快捷回复读取异常: replies=%+v err=%v", replies, repliesErr)
	}
	// deleteErr 保存删除已存在快捷回复后的结果错误。
	if deleteErr := service.DeleteQuickReply(context.Background(), 7, " account ", 1); deleteErr != nil {
		t.Fatalf("删除已存在快捷回复失败: %v", deleteErr)
	}
	// unavailableErr 保存基础仓储未实现元数据端口时的错误。
	if _, unavailableErr := New(&fakeRepository{owned: true}).ListQuickReplies(context.Background(), 7, "account"); !errors.Is(unavailableErr, ErrMetadataUnavailable) {
		t.Fatalf("缺少元数据端口错误=%v", unavailableErr)
	}
	// invalidAccountService 是具备元数据端口但收到非法用户或账号标识的服务。
	invalidAccountService := New(repository)
	// invalidListErr 保存非法用户 ID 导致的快捷回复读取错误。
	if _, invalidListErr := invalidAccountService.ListQuickReplies(context.Background(), 0, "account"); !errors.Is(invalidListErr, ErrInvalidInput) {
		t.Fatalf("非法用户读取错误=%v", invalidListErr)
	}
	// emptyCreateErr 保存空账号创建快捷回复时的输入错误。
	if _, emptyCreateErr := invalidAccountService.CreateQuickReply(context.Background(), 7, "", "内容"); !errors.Is(emptyCreateErr, ErrInvalidInput) {
		t.Fatalf("空账号创建错误=%v", emptyCreateErr)
	}
	// emptyDeleteErr 保存空账号删除快捷回复时的输入错误。
	if emptyDeleteErr := invalidAccountService.DeleteQuickReply(context.Background(), 7, "", 1); !errors.Is(emptyDeleteErr, ErrInvalidInput) {
		t.Fatalf("空账号删除错误=%v", emptyDeleteErr)
	}
	// emptyGetErr 保存空账号读取买家备注时的输入错误。
	if _, emptyGetErr := invalidAccountService.GetBuyerNote(context.Background(), 7, "", "buyer"); !errors.Is(emptyGetErr, ErrInvalidInput) {
		t.Fatalf("空账号读取备注错误=%v", emptyGetErr)
	}
	// emptySaveErr 保存空账号写入买家备注时的输入错误。
	if _, emptySaveErr := invalidAccountService.SaveBuyerNote(context.Background(), 7, "", "buyer", "内容"); !errors.Is(emptySaveErr, ErrInvalidInput) {
		t.Fatalf("空账号写入备注错误=%v", emptySaveErr)
	}
	// nilService 表示未初始化的聊天服务指针，用于覆盖元数据入口的空接收者分支。
	var nilService *Service
	// nilServiceErr 保存空聊天服务读取快捷回复时的错误。
	if _, nilServiceErr := nilService.ListQuickReplies(context.Background(), 7, "account"); !errors.Is(nilServiceErr, ErrInvalidInput) {
		t.Fatalf("空聊天服务错误=%v", nilServiceErr)
	}
}

// TestChatMetadataCoversOwnershipFailure 覆盖元数据归属查询失败的错误透传分支。
func TestChatMetadataCoversOwnershipFailure(t *testing.T) {
	// wantErr 是账号归属查询需要返回的稳定错误。
	wantErr := errors.New("ownership lookup failed")
	// repository 保存会返回归属错误的元数据仓储替身。
	repository := &metadataTestRepository{fakeRepository: &fakeRepository{ownedErr: wantErr}}
	// service 保存使用错误替身的聊天元数据服务。
	service := New(repository)
	// actualErr 保存元数据入口向调用方透传的归属错误。
	if _, actualErr := service.ListQuickReplies(context.Background(), 7, "account"); !errors.Is(actualErr, wantErr) {
		t.Fatalf("归属查询错误未透传: %v", actualErr)
	}
}
