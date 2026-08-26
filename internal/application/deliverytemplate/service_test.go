package deliverytemplate

import (
	"context"
	"errors"
	"testing"
)

// templateRepositoryStub 保存测试服务调用收到的最后一份草稿。
type templateRepositoryStub struct {
	// draft 是仓储最近收到的模板输入。
	draft Draft
	// listResult 是列表查询返回的模板集合。
	listResult []Template
	// listErr 是列表查询模拟的持久化错误。
	listErr error
	// getResult 是单模板查询返回的模板。
	getResult Template
	// getErr 是单模板查询模拟的持久化错误。
	getErr error
	// createErr 是创建操作模拟的持久化错误。
	createErr error
	// updateErr 是更新操作模拟的持久化错误。
	updateErr error
	// deleteErr 是删除操作模拟的持久化错误。
	deleteErr error
	// lastUserID 是仓储最近收到的用户标识。
	lastUserID int64
	// lastTemplateID 是仓储最近收到的模板标识。
	lastTemplateID int64
}

// ListForUser 返回空列表以满足模板服务测试仓储接口。
func (s *templateRepositoryStub) ListForUser(_ context.Context, userID int64) ([]Template, error) {
	// lastUserID 记录列表调用的用户归属。
	s.lastUserID = userID
	return s.listResult, s.listErr
}

// GetForUser 返回固定模板以满足模板服务测试仓储接口。
func (s *templateRepositoryStub) GetForUser(_ context.Context, userID, templateID int64) (Template, error) {
	// lastUserID、lastTemplateID 记录单模板调用的所有权参数。
	s.lastUserID, s.lastTemplateID = userID, templateID
	return s.getResult, s.getErr
}

// Create 记录模板输入并返回固定标识。
func (s *templateRepositoryStub) Create(_ context.Context, userID int64, draft Draft) (int64, error) {
	// lastUserID 记录创建调用的用户归属。
	s.lastUserID = userID
	s.draft = draft
	return 1, s.createErr
}

// Update 返回空错误以满足模板服务测试仓储接口。
func (s *templateRepositoryStub) Update(_ context.Context, userID, templateID int64, draft Draft) error {
	// lastUserID、lastTemplateID、draft 保存更新调用的业务参数。
	s.lastUserID, s.lastTemplateID, s.draft = userID, templateID, draft
	return s.updateErr
}

// Delete 返回空错误以满足模板服务测试仓储接口。
func (s *templateRepositoryStub) Delete(_ context.Context, userID, templateID int64) error {
	// lastUserID、lastTemplateID 保存删除调用的所有权参数。
	s.lastUserID, s.lastTemplateID = userID, templateID
	return s.deleteErr
}

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

// TestServiceDelegatesValidOperations 验证服务会校验参数、规范化草稿并委托五类仓储操作。
func TestServiceDelegatesValidOperations(t *testing.T) {
	// repository 保存本测试使用的可观察仓储替身。
	repository := &templateRepositoryStub{listResult: []Template{{ID: 9}}, getResult: Template{ID: 8}}
	// service 是待验证的模板应用服务。
	service := NewService(repository)
	// ctx 是本测试所有用例共用的非取消上下文。
	ctx := context.Background()

	// list、listErr 保存列表委托结果。
	list, listErr := service.List(ctx, 7)
	if listErr != nil || len(list) != 1 || repository.lastUserID != 7 {
		t.Fatalf("List result=%v err=%v user=%d", list, listErr, repository.lastUserID)
	}
	// got、getErr 保存单模板委托结果。
	got, getErr := service.Get(ctx, 7, 8)
	if getErr != nil || got.ID != 8 || repository.lastTemplateID != 8 {
		t.Fatalf("Get result=%+v err=%v", got, getErr)
	}
	// createdID、createErr 保存创建委托结果。
	createdID, createErr := service.Create(ctx, 7, Draft{Name: " 新模板 ", Messages: []string{"正文"}})
	if createErr != nil || createdID != 1 || repository.draft.Name != "新模板" {
		t.Fatalf("Create id=%d err=%v draft=%+v", createdID, createErr, repository.draft)
	}
	// updateErr 保存更新委托结果。
	updateErr := service.Update(ctx, 7, 8, Draft{Name: "更新", Messages: []string{"消息"}})
	if updateErr != nil || repository.lastTemplateID != 8 || repository.draft.Name != "更新" {
		t.Fatalf("Update err=%v draft=%+v", updateErr, repository.draft)
	}
	// deleteErr 保存删除委托结果。
	deleteErr := service.Delete(ctx, 7, 8)
	if deleteErr != nil || repository.lastTemplateID != 8 {
		t.Fatalf("Delete err=%v template=%d", deleteErr, repository.lastTemplateID)
	}
}

// TestServiceRejectsInvalidInput 验证服务拒绝未初始化、非法标识和非法模板草稿。
func TestServiceRejectsInvalidInput(t *testing.T) {
	// ctx 是本测试所有调用共用的非取消上下文。
	ctx := context.Background()
	// validDraft 是能够通过名称和模板语法校验的最小草稿。
	validDraft := Draft{Name: "模板", Messages: []string{"{{order_id}}"}}
	// cases 描述不同入口的无效参数及预期错误。
	cases := []struct {
		// name 是子测试名称。
		name string
		// service 是待调用的模板服务。
		service *Service
		// userID 是请求携带的用户标识。
		userID int64
		// templateID 是请求携带的模板标识。
		templateID int64
		// draft 是请求携带的模板草稿。
		draft Draft
		// operation 执行当前场景对应的服务入口。
		operation func(*Service) error
	}{
		{name: "nil service", service: nil, operation: func(_ *Service) error {
			// service 表示未初始化的模板服务指针，用于验证 nil receiver 防护。
			var service *Service
			return service.Delete(ctx, 1, 1)
		}},
		{name: "nil repository", service: NewService(nil), operation: func(service *Service) error { return service.Delete(ctx, 1, 1) }},
		{name: "invalid user", service: NewService(&templateRepositoryStub{}), userID: 0, operation: func(service *Service) error {
			// err 保存非法用户标识触发的列表校验错误。
			_, err := service.List(ctx, 0)
			return err
		}},
		{name: "invalid get user", service: NewService(&templateRepositoryStub{}), operation: func(service *Service) error {
			// err 保存单模板查询的非法用户标识错误。
			_, err := service.Get(ctx, 0, 1)
			return err
		}},
		{name: "invalid create user", service: NewService(&templateRepositoryStub{}), operation: func(service *Service) error {
			// err 保存模板创建的非法用户标识错误。
			_, err := service.Create(ctx, 0, validDraft)
			return err
		}},
		{name: "invalid get id", service: NewService(&templateRepositoryStub{}), userID: 1, templateID: 0, operation: func(service *Service) error {
			// err 保存非法模板标识触发的查询校验错误。
			_, err := service.Get(ctx, 1, 0)
			return err
		}},
		{name: "invalid update id", service: NewService(&templateRepositoryStub{}), userID: 1, templateID: 0, operation: func(service *Service) error { return service.Update(ctx, 1, 0, validDraft) }},
		{name: "invalid update user", service: NewService(&templateRepositoryStub{}), operation: func(service *Service) error {
			// err 保存模板更新的非法用户标识错误。
			return service.Update(ctx, 0, 1, validDraft)
		}},
		{name: "invalid update draft", service: NewService(&templateRepositoryStub{}), operation: func(service *Service) error {
			// err 保存模板更新的非法草稿错误。
			return service.Update(ctx, 1, 1, Draft{Name: "模板"})
		}},
		{name: "invalid delete id", service: NewService(&templateRepositoryStub{}), userID: 1, templateID: 0, operation: func(service *Service) error { return service.Delete(ctx, 1, 0) }},
		{name: "empty name", service: NewService(&templateRepositoryStub{}), operation: func(service *Service) error {
			// err 保存空名称草稿触发的创建校验错误。
			_, err := service.Create(ctx, 1, Draft{Messages: []string{"正文"}})
			return err
		}},
		{name: "empty messages", service: NewService(&templateRepositoryStub{}), operation: func(service *Service) error {
			// err 保存空消息草稿触发的创建校验错误。
			_, err := service.Create(ctx, 1, Draft{Name: "模板"})
			return err
		}},
		{name: "invalid variable", service: NewService(&templateRepositoryStub{}), operation: func(service *Service) error {
			// err 保存非法变量草稿触发的创建校验错误。
			_, err := service.Create(ctx, 1, Draft{Name: "模板", Messages: []string{"{{unknown}}"}})
			return err
		}},
	}
	for /* item 表示当前待验证的无效输入场景。 */ _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			// service 保存当前场景需要调用的模板服务。
			service := item.service
			// err 保存当前无效输入的业务错误。
			err := item.operation(service)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error=%v, want ErrInvalidInput", err)
			}
		})
	}
}

// TestServicePropagatesRepositoryErrors 验证服务不吞掉仓储层的业务错误和基础设施错误。
func TestServicePropagatesRepositoryErrors(t *testing.T) {
	// ctx 是本测试所有调用共用的非取消上下文。
	ctx := context.Background()
	// repository 保存可注入错误的仓储替身。
	repository := &templateRepositoryStub{listErr: errors.New("list"), getErr: errors.New("get"), createErr: errors.New("create"), updateErr: errors.New("update"), deleteErr: errors.New("delete")}
	// service 是待验证的模板应用服务。
	service := NewService(repository)
	// list、listErr 保存列表错误传播结果。
	_, listErr := service.List(ctx, 1)
	// got、getErr 保存单模板错误传播结果。
	_, getErr := service.Get(ctx, 1, 1)
	// createdID、createErr 保存创建错误传播结果。
	createdID, createErr := service.Create(ctx, 1, Draft{Name: "模板", Messages: []string{"正文"}})
	// updateErr 保存更新错误传播结果。
	updateErr := service.Update(ctx, 1, 1, Draft{Name: "模板", Messages: []string{"正文"}})
	// deleteErr 保存删除错误传播结果。
	deleteErr := service.Delete(ctx, 1, 1)
	if listErr == nil || getErr == nil || createErr == nil || updateErr == nil || deleteErr == nil || createdID != 1 {
		t.Fatalf("repository errors were not propagated: list=%v get=%v create=%v update=%v delete=%v id=%d", listErr, getErr, createErr, updateErr, deleteErr, createdID)
	}
}
