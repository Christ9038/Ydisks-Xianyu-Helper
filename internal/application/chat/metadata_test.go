package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// metadataTestRepository 是快捷回复和买家备注用例的内存持久化替身。
type metadataTestRepository struct {
	// fakeRepository 复用聊天查询和账号归属所需的最小端口实现。
	*fakeRepository
	// replies 保存测试中的账号快捷回复列表。
	replies []QuickReply
	// note 保存测试中的买家备注；空值由 found 控制是否已持久化。
	note BuyerNote
	// found 表示测试买家备注是否已经存在。
	found bool
	// listErr、createErr、deleteErr 保存快捷回复仓储操作错误。
	listErr, createErr, deleteErr error
	// noteErr、saveErr 保存买家备注仓储操作错误。
	noteErr, saveErr error
}

// ListQuickReplies 返回当前测试替身保存的快捷回复集合。
func (r *metadataTestRepository) ListQuickReplies(context.Context, string) ([]QuickReply, error) {
	if r.listErr != nil {
		return nil, r.listErr
	}
	return append([]QuickReply(nil), r.replies...), nil
}

// CreateQuickReply 将测试回复追加到内存集合，并模拟已达上限错误。
func (r *metadataTestRepository) CreateQuickReply(_ context.Context, accountID, content string) (QuickReply, error) {
	if r.createErr != nil {
		return QuickReply{}, r.createErr
	}
	if len(r.replies) >= quickReplyLimit {
		return QuickReply{}, ErrQuickReplyLimitReached
	}
	// reply 保存当前调用构造的确定性快捷回复。
	reply := QuickReply{ID: int64(len(r.replies) + 1), AccountID: accountID, Content: content, CreatedAt: 1}
	r.replies = append(r.replies, reply)
	return reply, nil
}

// DeleteQuickReply 按标识移除测试快捷回复并返回是否命中。
func (r *metadataTestRepository) DeleteQuickReply(_ context.Context, _ string, quickReplyID int64) (bool, error) {
	if r.deleteErr != nil {
		return false, r.deleteErr
	}
	// replyIndex 保存与待删除标识匹配的内存快捷回复下标。
	for replyIndex, reply := range r.replies {
		if reply.ID == quickReplyID {
			r.replies = append(r.replies[:replyIndex], r.replies[replyIndex+1:]...)
			return true, nil
		}
	}
	return false, nil
}

// GetBuyerNote 返回内存备注及其持久化存在状态。
func (r *metadataTestRepository) GetBuyerNote(context.Context, string, string) (BuyerNote, bool, error) {
	if r.noteErr != nil {
		return BuyerNote{}, false, r.noteErr
	}
	return r.note, r.found, nil
}

// SaveBuyerNote 保存或清除内存备注，并复现空内容的逻辑空备注语义。
func (r *metadataTestRepository) SaveBuyerNote(_ context.Context, note BuyerNote) (BuyerNote, error) {
	if r.saveErr != nil {
		return BuyerNote{}, r.saveErr
	}
	if note.Content == "" {
		r.note = BuyerNote{AccountID: note.AccountID, BuyerID: note.BuyerID}
		r.found = false
		return r.note, nil
	}
	note.UpdatedAt = 1
	r.note = note
	r.found = true
	return note, nil
}

// TestChatMetadataValidatesOwnershipAndContent 验证聊天元数据用例执行账号隔离、输入校验和空备注默认语义。
func TestChatMetadataValidatesOwnershipAndContent(t *testing.T) {
	// repository 保存具备账号归属和元数据能力的内存替身。
	repository := &metadataTestRepository{fakeRepository: &fakeRepository{owned: true}}
	// service 保存使用该替身的聊天应用服务。
	service := New(repository)
	// reply 和 createErr 保存创建快捷回复的结果及错误。
	reply, createErr := service.CreateQuickReply(context.Background(), 7, " account ", "  您好\n可以直接拍  ")
	if createErr != nil || reply.Content != "您好\n可以直接拍" || len(repository.replies) != 1 {
		t.Fatalf("创建快捷回复 reply=%+v err=%v", reply, createErr)
	}
	// invalidErr 保存删除不存在快捷回复时应用服务返回的业务错误。
	if invalidErr := service.DeleteQuickReply(context.Background(), 7, "account", 99); !errors.Is(invalidErr, ErrQuickReplyNotFound) {
		t.Fatalf("删除不存在快捷回复 error=%v", invalidErr)
	}
	// emptyNote 和 noteErr 保存尚未持久化买家备注的逻辑默认值。
	emptyNote, noteErr := service.GetBuyerNote(context.Background(), 7, "account", "buyer")
	if noteErr != nil || emptyNote.Content != "" || emptyNote.BuyerID != "buyer" {
		t.Fatalf("空备注 note=%+v err=%v", emptyNote, noteErr)
	}
	// savedNote 和 saveErr 保存买家备注写入结果及错误。
	savedNote, saveErr := service.SaveBuyerNote(context.Background(), 7, "account", "buyer", "  重点客户  ")
	if saveErr != nil || savedNote.Content != "重点客户" || savedNote.UpdatedAt != 1 {
		t.Fatalf("保存备注 note=%+v err=%v", savedNote, saveErr)
	}
	// clearedNote 和 clearErr 保存清除备注后的逻辑空结果。
	clearedNote, clearErr := service.SaveBuyerNote(context.Background(), 7, "account", "buyer", "")
	if clearErr != nil || clearedNote.Content != "" || repository.found {
		t.Fatalf("清除备注 note=%+v found=%v err=%v", clearedNote, repository.found, clearErr)
	}
	// invalidErr 保存尝试创建空快捷回复时应用服务返回的输入校验错误。
	if _, invalidErr := service.CreateQuickReply(context.Background(), 7, "account", ""); !errors.Is(invalidErr, ErrInvalidInput) {
		t.Fatalf("空快捷回复 error=%v", invalidErr)
	}
	// deniedRepository 保存模拟无权访问账号的元数据替身。
	deniedRepository := &metadataTestRepository{fakeRepository: &fakeRepository{owned: false}}
	// deniedErr 保存无账号归属时读取快捷回复返回的授权错误。
	if _, deniedErr := New(deniedRepository).ListQuickReplies(context.Background(), 7, "account"); !errors.Is(deniedErr, ErrMetadataForbidden) {
		t.Fatalf("越权读取 error=%v", deniedErr)
	}
}

// TestChatMetadataPropagatesRepositoryErrors 验证聊天元数据各仓储操作的错误不会被应用层吞掉。
func TestChatMetadataPropagatesRepositoryErrors(t *testing.T) {
	// wantErr 是测试仓储统一返回的确定性错误。
	wantErr := errors.New("metadata repository failed")
	// repository 是具备账号归属和全部元数据能力的错误替身。
	repository := &metadataTestRepository{fakeRepository: &fakeRepository{owned: true}, listErr: wantErr, createErr: wantErr, deleteErr: wantErr, noteErr: wantErr, saveErr: wantErr}
	// service 是使用错误替身的聊天服务。
	service := New(repository)
	// listErr 保存快捷回复列表读取错误。
	if _, listErr := service.ListQuickReplies(context.Background(), 7, "account"); !errors.Is(listErr, wantErr) {
		t.Fatalf("list error=%v", listErr)
	}
	// createErr 保存快捷回复创建错误。
	if _, createErr := service.CreateQuickReply(context.Background(), 7, "account", "content"); !errors.Is(createErr, wantErr) {
		t.Fatalf("create error=%v", createErr)
	}
	// deleteErr 保存快捷回复删除错误。
	if deleteErr := service.DeleteQuickReply(context.Background(), 7, "account", 1); !errors.Is(deleteErr, wantErr) {
		t.Fatalf("delete error=%v", deleteErr)
	}
	// getNoteErr 保存买家备注读取错误。
	if _, getNoteErr := service.GetBuyerNote(context.Background(), 7, "account", "buyer"); !errors.Is(getNoteErr, wantErr) {
		t.Fatalf("get note error=%v", getNoteErr)
	}
	// saveNoteErr 保存买家备注写入错误。
	if _, saveNoteErr := service.SaveBuyerNote(context.Background(), 7, "account", "buyer", "content"); !errors.Is(saveNoteErr, wantErr) {
		t.Fatalf("save note error=%v", saveNoteErr)
	}
	// invalidService 是没有聊天仓储的服务，用于验证元数据入口的空依赖边界。
	invalidService := New(nil)
	// invalidListErr 保存未初始化服务读取快捷回复时的输入错误。
	if _, invalidListErr := invalidService.ListQuickReplies(context.Background(), 7, "account"); !errors.Is(invalidListErr, ErrInvalidInput) {
		t.Fatalf("nil list error=%v", invalidListErr)
	}
	// longReplyErr 保存超长快捷回复的输入错误。
	if _, longReplyErr := service.CreateQuickReply(context.Background(), 7, "account", strings.Repeat("中", chatMetadataMaxCharacters+1)); !errors.Is(longReplyErr, ErrInvalidInput) {
		t.Fatalf("long quick reply error=%v", longReplyErr)
	}
	// invalidDeleteErr 保存非法快捷回复标识的输入错误。
	if invalidDeleteErr := service.DeleteQuickReply(context.Background(), 7, "account", 0); !errors.Is(invalidDeleteErr, ErrInvalidInput) {
		t.Fatalf("invalid quick reply ID error=%v", invalidDeleteErr)
	}
	// emptyBuyerErr 保存空买家标识读取备注时的输入错误。
	if _, emptyBuyerErr := service.GetBuyerNote(context.Background(), 7, "account", ""); !errors.Is(emptyBuyerErr, ErrInvalidInput) {
		t.Fatalf("empty buyer ID error=%v", emptyBuyerErr)
	}
	// emptySaveErr 保存空买家标识写入备注时的输入错误。
	if _, emptySaveErr := service.SaveBuyerNote(context.Background(), 7, "account", "", "content"); !errors.Is(emptySaveErr, ErrInvalidInput) {
		t.Fatalf("empty buyer ID save error=%v", emptySaveErr)
	}
}
