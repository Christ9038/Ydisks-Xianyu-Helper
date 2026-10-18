package engine

import (
	"context"
	"errors"
	"testing"

	"xianyu-go/internal/db"
)

// localReplyDelivery 记录引擎的完整回复，并按测试设定模拟图片已确认但文字确定失败。
type localReplyDelivery struct {
	// messages 保存依次收到的分段内容，用于检查 once 重试不会重新读图。
	messages []ReplyMessage
	// failTextFirst 控制首次发送的文字分段是否失败。
	failTextFirst bool
}

// SendReply 接收 message 并返回分段确认，不访问真实平台或文件系统。
func (d *localReplyDelivery) SendReply(_ context.Context, message ReplyMessage) (ReplySendResult, error) {
	d.messages = append(d.messages, message)
	if d.failTextFirst && len(d.messages) == 1 {
		return ReplySendResult{ImageSent: true}, errors.New("文字确定失败")
	}
	return ReplySendResult{ImageSent: message.ImagePath != "" || message.ImageURL != "", TextSent: message.Text != ""}, nil
}

// TestDefaultReplyLocalImageSelection 验证商品图片覆盖、纯图片回复、空商品回退和账号隔离。
func TestDefaultReplyLocalImageSelection(t *testing.T) {
	// store 和 cleanup 保存临时 SQLite 及其关闭责任。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 是所有本地数据库操作的测试上下文。
	ctx := context.Background()
	// upsertErr 保存带本地图片的账号默认配置写入结果。
	if upsertErr := store.DefaultReps.Upsert(ctx, "cid", db.DefaultReply{Enabled: true, ReplyContent: "账号默认", ReplyImagePath: "account.png", ReplyOnce: true}); upsertErr != nil {
		t.Fatal(upsertErr)
	}
	// insertErr 设置分别使用本地图片、URL 图片和空配置的三个商品。
	if _, insertErr := store.DB.ExecContext(ctx, `INSERT INTO item_replay (cookie_id,item_id,reply_content,reply_image_url,reply_image_path) VALUES
		('cid','local','商品{item_id}','','product.png'),('cid','url','','https://example.test/image.png',''),('cid','empty','','','')`); insertErr != nil {
		t.Fatal(insertErr)
	}
	// service 使用真实配置查询但不发送消息。
	service := NewReplyService("cid", store, nil, nil, nil, nil)
	// cases 描述商品覆盖与未配置商品的完整解析预期。
	cases := []struct {
		// itemID 是当前聊天关联的商品。
		itemID string
		// text、imageURL 和 imagePath 是本场景应使用的内容。
		text, imageURL, imagePath string
		// once 只在账号默认分支沿用配置。
		once bool
	}{
		{itemID: "local", text: "商品local", imagePath: "product.png"},
		{itemID: "url", imageURL: "https://example.test/image.png"},
		{itemID: "empty", text: "账号默认", imagePath: "account.png", once: true},
		{itemID: "missing", text: "账号默认", imagePath: "account.png", once: true},
	}
	// testCase 是当前检查的默认回复场景。
	for _, testCase := range cases {
		// result 保存完整默认解析结果，不应混入账号图片或次数配置。
		result := service.resolve(ctx, chatMsg("咨询", testCase.itemID, "chat"))
		if result == nil || result.Skip || result.Text != testCase.text || result.ImageURL != testCase.imageURL || result.ImagePath != testCase.imagePath || result.ReplyOnce != testCase.once {
			t.Fatalf("商品 %s 解析错误：%+v", testCase.itemID, result)
		}
	}
	// other 使用没有配置的另一个账号，不能命中当前账号的商品默认回复。
	other := NewReplyService("other", store, nil, nil, nil, nil)
	if other.resolve(ctx, chatMsg("咨询", "local", "chat")) != nil {
		t.Fatal("商品默认回复跨账号泄露")
	}
}

// TestDefaultReplyLocalImageOnceResumesOnlyText 验证本地图片也遵循原有分段 once，不在重试时重新读取发送图片。
func TestDefaultReplyLocalImageOnceResumesOnlyText(t *testing.T) {
	// store 和 cleanup 保存临时 SQLite 及其关闭责任。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 用于当前测试的本地数据库和发送调用。
	ctx := context.Background()
	// upsertErr 保存账号图文 once 配置的写入结果。
	if upsertErr := store.DefaultReps.Upsert(ctx, "cid", db.DefaultReply{Enabled: true, ReplyContent: "回复", ReplyImagePath: "reply.png", ReplyOnce: true}); upsertErr != nil {
		t.Fatal(upsertErr)
	}
	// delivery 首次确认图片但文字确定失败，第二次只允许补发文字。
	delivery := &localReplyDelivery{failTextFirst: true}
	// service 使用持久化分段状态和可观察的完整回复发送端口。
	service := NewReplyService("cid", store, delivery, nil, nil, nil)
	if service.Handle(ctx, chatMsg("咨询", "missing", "chat")) == nil {
		t.Fatal("首次文字失败应返回错误")
	}
	// retryErr 应只发送未成功的文字部分。
	if retryErr := service.Handle(ctx, chatMsg("继续咨询", "missing", "chat")); retryErr != nil {
		t.Fatal(retryErr)
	}
	// finalErr 再次触发应被已经完成的 once 记录阻止。
	if finalErr := service.Handle(ctx, chatMsg("再次咨询", "missing", "chat")); finalErr != nil {
		t.Fatal(finalErr)
	}
	if len(delivery.messages) != 2 || delivery.messages[0].ImagePath != "reply.png" || delivery.messages[1].ImagePath != "" || delivery.messages[1].Text != "回复" {
		t.Fatalf("本地图片 once 重试错误：%+v", delivery.messages)
	}
}
