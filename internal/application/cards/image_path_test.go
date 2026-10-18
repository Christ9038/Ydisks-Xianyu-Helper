package cards

import (
	"context"
	"errors"
	"testing"
)

// TestCardImagePathMutation 验证本地图创建、省略保留、显式清空切换及用户归属，图片不绑定保存时账号。
func TestCardImagePathMutation(t *testing.T) {
	// repository 记录图片卡写入，service 使用真实应用校验。
	repository := &cardRepositoryStub{createdID: 1}
	// service 负责图片来源和所有权业务约束。
	service := NewService(repository)
	// ctx 是本地确定性测试上下文。
	ctx := context.Background()
	if _, err := service.Create(ctx, 7, Draft{Name: "图片", Type: "image", ImagePath: "材料/图.png"}); err != nil { // err 是合法本地图创建错误。
		t.Fatal(err)
	}
	if repository.createdCard.ImagePath != "材料/图.png" || repository.createdCard.UserID != 7 {
		t.Fatalf("创建字段丢失: %+v", repository.createdCard)
	}
	repository.card = repository.createdCard
	repository.card.ID = 1
	if err := service.Update(ctx, 7, 1, Draft{Name: "改名", Type: "image"}); err != nil { // err 是省略来源更新错误。
		t.Fatal(err)
	}
	if repository.updatedCard.ImagePath != "材料/图.png" {
		t.Fatal("旧客户端更新丢失本地图")
	}
	if err := service.Update(ctx, 7, 1, Draft{Name: "冲突", Type: "image", ImageURL: "https://example.test/a.png"}); err == nil { // err 必须提示旧本地图未显式清空。
		t.Fatal("切换 URL 未清空路径却被接受")
	}
	if err := service.Update(ctx, 7, 1, Draft{Name: "远程", Type: "image", ImageURL: "https://example.test/a.png", ImagePathSet: true}); err != nil { // err 是显式清空本地图后的 URL 切换错误。
		t.Fatal(err)
	}
	if repository.updatedCard.ImagePath != "" || repository.updatedCard.ImageURL == "" {
		t.Fatal("URL 切换未清空路径")
	}
	repository.card = repository.updatedCard
	if err := service.Update(ctx, 7, 1, Draft{Name: "保留远程", Type: "image"}); err != nil || repository.updatedCard.ImageURL == "" { // err 是省略 URL 的保留更新错误。
		t.Fatalf("省略 URL 未保留: %v", err)
	}
	if err := service.Update(ctx, 7, 1, Draft{Name: "本地", Type: "image", ImagePath: "b.png", ImageURLSet: true}); err != nil { // err 是显式清空 URL 后切换本地图的错误。
		t.Fatal(err)
	}
	if repository.updatedCard.ImageURL != "" || repository.updatedCard.ImagePath != "b.png" {
		t.Fatal("本地图切换未清空 URL")
	}
	if err := service.Update(ctx, 8, 1, Draft{Name: "越权", Type: "image", ImagePath: "other.png"}); !errors.Is(err, ErrForbidden) { // err 必须在写入前拒绝跨用户。
		t.Fatalf("跨用户修改错误=%v", err)
	}
	if err := service.Update(ctx, 7, 1, Draft{Name: "文字", Type: "text", TextContent: "正文", ImagePath: "old.png"}); err != nil || repository.updatedCard.ImagePath != "" || repository.updatedCard.ImageURL != "" { // err 验证切换非图片类型清除残留来源。
		t.Fatalf("切换类型残留图片: %v", err)
	}
}

// TestCardLegacyImageURLWhitespace 验证历史带空白 URL 原样回显或省略更新时仍可编辑，不要求迁移旧数据。
func TestCardLegacyImageURLWhitespace(t *testing.T) {
	// repository 保存旧校验允许写入的原始 URL 文本。
	repository := &cardRepositoryStub{card: Card{ID: 1, UserID: 7, Type: "image", ImageURL: " https://example.test/old.png "}}
	// service 使用当前校验规则处理历史图片记录。
	service := NewService(repository)
	// draft 分别模拟旧表单原样提交 URL 与仅改名时完全省略来源。
	for _, draft := range []Draft{{Name: "原样编辑", Type: "image", ImageURL: repository.card.ImageURL, ImageURLSet: true}, {Name: "省略更新", Type: "image"}} {
		if err := service.Update(context.Background(), 7, 1, draft); err != nil || repository.updatedCard.ImageURL != repository.card.ImageURL { // err 是历史 URL 兼容更新错误。
			t.Fatalf("历史 URL 更新失败: %v", err)
		}
	}
}

// TestCardImagePathValidation 验证无图片、双来源、非法相对路径和空串显式清除都不会写入无效 image 卡。
func TestCardImagePathValidation(t *testing.T) {
	// service 使用不执行外部 I/O 的仓储替身。
	service := NewService(&cardRepositoryStub{card: Card{ID: 1, UserID: 7, Type: "image", ImagePath: "old.png"}})
	// draft 是当前非法配置，不应进入仓储写入。
	for _, draft := range []Draft{{Name: "图", Type: "image", ImagePath: "../a.png"}, {Name: "图", Type: "image", ImagePath: "/a.png"}, {Name: "图", Type: "image", ImageURL: "https://example.test/a.png", ImagePath: "a.png"}} {
		if _, err := service.Create(context.Background(), 7, draft); err == nil { // err 是路径或互斥规则错误。
			t.Fatal("非法图片配置被接受")
		}
	}
	if err := service.Update(context.Background(), 7, 1, Draft{Name: "图", Type: "image", ImagePathSet: true}); err == nil { // err 验证仅清空唯一来源不能留下空图片卡。
		t.Fatal("空图片卡被保存")
	}
}
