package deliverytemplate

import (
	"context"
	"testing"
)

// templateRepositoryStub 保存测试服务调用收到的最后一份草稿。
type templateRepositoryStub struct {
	// draft 是仓储最近收到的模板输入。
	draft Draft
}

// ListForUser 返回空列表以满足模板服务测试仓储接口。
func (s *templateRepositoryStub) ListForUser(context.Context, int64) ([]Template, error) {
	return nil, nil
}

// GetForUser 返回固定模板以满足模板服务测试仓储接口。
func (s *templateRepositoryStub) GetForUser(context.Context, int64, int64) (Template, error) {
	return Template{}, nil
}

// Create 记录模板输入并返回固定标识。
func (s *templateRepositoryStub) Create(_ context.Context, _ int64, draft Draft) (int64, error) {
	s.draft = draft
	return 1, nil
}

// Update 返回空错误以满足模板服务测试仓储接口。
func (s *templateRepositoryStub) Update(context.Context, int64, int64, Draft) error { return nil }

// Delete 返回空错误以满足模板服务测试仓储接口。
func (s *templateRepositoryStub) Delete(context.Context, int64, int64) error { return nil }

// TestCreateNormalizesDraft 验证应用服务会清理名称但保留消息正文。
func TestCreateNormalizesDraft(t *testing.T) {
	// repository 是记录输入的测试仓储。
	repository := &templateRepositoryStub{}
	// service 是待验证的模板应用服务。
	service := NewService(repository)
	// err 保存模板创建测试失败原因。
	if _, err := service.Create(context.Background(), 1, Draft{Name: " 模板 ", Messages: []string{"  内容  "}}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if repository.draft.Name != "模板" || repository.draft.Messages[0] != "  内容  " {
		t.Fatalf("normalized draft=%+v", repository.draft)
	}
}
