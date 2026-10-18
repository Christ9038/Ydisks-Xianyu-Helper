package defaultreply

import (
	"context"
	"errors"
	"testing"
)

// Update 用 f 的当前配置运行 build，仅成功后替换内存快照以模拟事务回滚。
func (f *fakeRepository) Update(_ context.Context, cookieID string, build func(Reply) (Reply, error)) error {
	// reply、err 是应用合并后的快照和输入校验结果。
	reply, err := build(f.reply)
	if err != nil {
		return err
	}
	if f.mutationErr != nil {
		return f.mutationErr
	}
	f.reply, f.mutationCookieID = reply, cookieID
	return nil
}

// TestUpdatePreservesLocalImageAndRejectsConflicts 验证旧客户端保存不会清空本地图，冲突与非法路径失败不写入。
func TestUpdatePreservesLocalImageAndRejectsConflicts(t *testing.T) {
	// repository、service 分别是受控配置快照及应用服务。
	repository := &fakeRepository{ownership: AccountOwnership{OwnerID: 1}, reply: Reply{Enabled: true, ReplyImagePath: "原图.png"}}
	// service 在内存配置仓储上执行归属与图片兼容校验。
	service := NewService(repository)
	// ctx 是本地服务调用的非取消上下文。
	ctx := context.Background()
	if err := service.Update(ctx, 1, "account", Draft{Enabled: true, ReplyContent: "新正文"}); err != nil { // err 是兼容保存结果。
		t.Fatal(err)
	}
	if repository.reply.ReplyImagePath != "原图.png" || repository.reply.ReplyContent != "新正文" {
		t.Fatalf("缺省字段被覆盖: %+v", repository.reply)
	}
	// remote、empty、invalid 分别表示来源切换、明确清空和非法相对引用。
	remote, empty, invalid := "https://example.test/new.png", "", "../outside.png"
	if err := service.Update(ctx, 1, "account", Draft{ReplyImageURL: &remote}); !errors.Is(err, ErrInvalidReply) { // err 必须拒绝与保留本地图冲突的旧客户端写入。
		t.Fatalf("冲突错误=%v", err)
	}
	if err := service.Update(ctx, 1, "account", Draft{ReplyImageURL: &empty, ReplyImagePath: &invalid}); !errors.Is(err, ErrInvalidReply) { // err 必须拒绝越界引用。
		t.Fatalf("路径错误=%v", err)
	}
	if repository.reply.ReplyImagePath != "原图.png" || repository.reply.ReplyContent != "新正文" {
		t.Fatal("失败校验修改了当前配置")
	}
	if err := service.Update(ctx, 1, "account", Draft{ReplyImageURL: &remote, ReplyImagePath: &empty}); err != nil { // err 不应阻止明确清空本地图后的来源切换。
		t.Fatal(err)
	}
	if repository.reply.ReplyImageURL != remote || repository.reply.ReplyImagePath != "" {
		t.Fatalf("来源切换失败: %+v", repository.reply)
	}
	if err := service.Update(ctx, 2, "account", Draft{}); !errors.Is(err, ErrForbidden) { // err 应先拒绝跨用户写入。
		t.Fatalf("越权结果=%v", err)
	}
	if err := service.Upsert(ctx, 1, "account", Reply{ReplyImagePath: invalid}); !errors.Is(err, ErrInvalidReply) { // err 应阻止内部完整写入绕过路径校验。
		t.Fatalf("完整写入绕过校验: %v", err)
	}
}
