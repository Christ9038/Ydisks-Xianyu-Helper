package deliverytemplate

import (
	"context"
	"errors"
	"testing"

	domain "xianyu-go/internal/deliverytemplate"
)

// TestMessageItemsDraft 验证应用层优先完整消息、复制草稿、保留旧格式标识和拒绝非法图片。
func TestMessageItemsDraft(t *testing.T) {
	// repository 记录交给持久化边界的完整草稿。
	repository := &templateRepositoryStub{}
	// service 是实际模板应用服务，不注入任何外部平台依赖。
	service := NewService(repository)
	// draft 的旧正文故意非法，以证明新消息字段优先。
	draft := Draft{Name: " 图片 ", Messages: []string{"{{bad}}"}, MessageItems: []domain.Message{{Type: "image", ImagePath: "a.png"}}}
	if _, err := service.Create(context.Background(), 7, draft); err != nil { // err 是纯图片草稿创建错误。
		t.Fatal(err)
	}
	draft.MessageItems[0].ImagePath = "changed.png"
	if repository.draft.MessageItems[0].ImagePath != "a.png" || repository.draft.Name != "图片" {
		t.Fatal("草稿规范化共享了调用方切片")
	}
	if err := service.Update(context.Background(), 7, 1, Draft{Name: "旧文本", Messages: []string{"正文"}}); err != nil || repository.draft.MessageItems != nil { // err 验证旧输入保持 nil 标志供事务防丢图使用。
		t.Fatalf("旧草稿兼容错误=%v", err)
	}
	if _, err := service.Create(context.Background(), 7, Draft{Name: "空新字段", Messages: []string{"正文"}, MessageItems: []domain.Message{}}); !errors.Is(err, ErrInvalidInput) { // err 验证显式空新字段不能回退旧列表。
		t.Fatalf("空新字段错误=%v", err)
	}
	if err := service.Update(context.Background(), 7, 1, Draft{Name: "非法图", MessageItems: []domain.Message{{Type: "image", ImagePath: "../bad.png"}}}); !errors.Is(err, ErrInvalidInput) { // err 是非法路径的应用层稳定分类。
		t.Fatalf("非法路径错误=%v", err)
	}
}
