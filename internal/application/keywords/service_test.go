package keywords

import (
	"context"
	"errors"
	"testing"
)

// keywordRepositoryFake 保存关键词服务测试所需的可控端口行为。
type keywordRepositoryFake struct {
	// addErr 是创建操作返回的错误。
	addErr error
	// addedDraft 保存最近一次创建输入。
	addedDraft Draft
	// listRows 是列表操作返回的规则。
	listRows []Keyword
	// listErr 是列表操作返回的错误。
	listErr error
	// replaceErr 是批量替换返回的错误。
	replaceErr error
	// updateErr 是更新返回的错误。
	updateErr error
	// deleteErr 是删除返回的错误。
	deleteErr error
	// itemRows 是商品回复列表。
	itemRows []ItemReply
	// itemErr 是商品回复操作返回的错误。
	itemErr error
}

// List 实现测试仓储的关键词列表端口。
func (f *keywordRepositoryFake) List(context.Context, int64, string) ([]Keyword, error) {
	return f.listRows, f.listErr
}

// Add 实现测试仓储的关键词创建端口。
func (f *keywordRepositoryFake) Add(_ context.Context, _ int64, _ string, draft Draft) (int64, error) {
	f.addedDraft = draft
	return 9, f.addErr
}

// Replace 实现测试仓储的关键词批量替换端口。
func (f *keywordRepositoryFake) Replace(context.Context, int64, string, []Draft) error {
	return f.replaceErr
}

// Update 实现测试仓储的关键词更新端口。
func (f *keywordRepositoryFake) Update(context.Context, int64, string, int64, Draft) error {
	return f.updateErr
}

// DeleteByID 实现测试仓储的关键词 ID 删除端口。
func (f *keywordRepositoryFake) DeleteByID(context.Context, int64, string, int64) error {
	return f.deleteErr
}

// DeleteByIndex 实现测试仓储的关键词索引删除端口。
func (f *keywordRepositoryFake) DeleteByIndex(context.Context, int64, string, int) error {
	return f.deleteErr
}

// ListItemReplies 实现测试仓储的商品回复列表端口。
func (f *keywordRepositoryFake) ListItemReplies(context.Context, int64) ([]ItemReply, error) {
	return f.itemRows, f.itemErr
}

// GetItemReply 实现测试仓储的商品回复读取端口。
func (f *keywordRepositoryFake) GetItemReply(context.Context, int64, string, string) (ItemReply, error) {
	if f.itemErr != nil {
		return ItemReply{}, f.itemErr
	}
	if len(f.itemRows) == 0 {
		return ItemReply{}, ErrNotFound
	}
	return f.itemRows[0], nil
}

// SetItemReply 实现测试仓储的商品回复写入端口。
func (f *keywordRepositoryFake) SetItemReply(context.Context, int64, string, string, string) error {
	return f.itemErr
}

// DeleteItemReply 实现测试仓储的商品回复删除端口。
func (f *keywordRepositoryFake) DeleteItemReply(context.Context, int64, string, string) error {
	return f.itemErr
}

// TestServiceNormalizesAndCreatesKeyword 验证成功创建会规范化输入并传给仓储。
func TestServiceNormalizesAndCreatesKeyword(t *testing.T) {
	// repository 是本测试使用的可控关键词仓储。
	repository := &keywordRepositoryFake{}
	// service 是待验证的关键词应用服务。
	service := NewService(repository)
	// id、err 保存创建结果。
	id, err := service.Add(context.Background(), 7, "account-1", Draft{Keyword: "  价格 ", Reply: "  50元 ", Type: "TEXT"})
	if err != nil || id != 9 {
		t.Fatalf("创建失败 id=%d err=%v", id, err)
	}
	if repository.addedDraft.Keyword != "价格" || repository.addedDraft.Reply != "50元" || repository.addedDraft.Type != "text" {
		t.Fatalf("输入未规范化: %+v", repository.addedDraft)
	}
}

// TestServiceRejectsInvalidInput 验证用户、账号、关键词和类型参数在仓储调用前被拒绝。
func TestServiceRejectsInvalidInput(t *testing.T) {
	// cases 是覆盖关键参数边界的服务调用集合。
	cases := []struct {
		// name 是子测试名称。
		name string
		// call 是待验证的服务调用。
		call func(*Service) error
	}{
		{name: "invalid user", call: func(service *Service) error {
			// err 表示无效用户调用返回的参数错误。
			_, err := service.Add(context.Background(), 0, "account", Draft{Keyword: "k", Reply: "r"})
			return err
		}},
		{name: "invalid account", call: func(service *Service) error {
			// err 表示空账号标识调用返回的参数错误。
			_, err := service.Add(context.Background(), 1, "", Draft{Keyword: "k", Reply: "r"})
			return err
		}},
		{name: "missing keyword", call: func(service *Service) error {
			// err 表示缺少关键词调用返回的校验错误。
			_, err := service.Add(context.Background(), 1, "account", Draft{Reply: "r"})
			return err
		}},
		{name: "missing image", call: func(service *Service) error {
			// err 表示图片规则缺少图片地址的校验错误。
			_, err := service.Add(context.Background(), 1, "account", Draft{Keyword: "k", Type: "image"})
			return err
		}},
		{name: "unsupported type", call: func(service *Service) error {
			// err 表示不支持的回复类型校验错误。
			_, err := service.Add(context.Background(), 1, "account", Draft{Keyword: "k", Type: "api", Reply: "r"})
			return err
		}},
	}
	// testCase 表示当前待验证的参数边界。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// err 是当前参数调用返回的错误。
			err := testCase.call(NewService(&keywordRepositoryFake{}))
			if err == nil {
				t.Fatal("无效参数应返回错误")
			}
		})
	}
}

// TestServicePreservesOwnershipAndInfrastructureErrors 验证仓储返回的跨用户和基础设施错误不被吞掉。
func TestServicePreservesOwnershipAndInfrastructureErrors(t *testing.T) {
	// backendErr 是模拟数据库故障的哨兵错误。
	backendErr := errors.New("database unavailable")
	// cases 是不同底层错误阶段的服务调用集合。
	cases := []struct {
		// name 是子测试名称。
		name string
		// repository 是返回当前错误的仓储。
		repository *keywordRepositoryFake
		// want 是期望的错误。
		want error
	}{
		{name: "forbidden", repository: &keywordRepositoryFake{listErr: ErrForbidden}, want: ErrForbidden},
		{name: "infrastructure", repository: &keywordRepositoryFake{listErr: backendErr}, want: backendErr},
	}
	// testCase 表示当前待验证的错误边界。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// err 是列表服务返回的错误。
			_, err := NewService(testCase.repository).List(context.Background(), 1, "account")
			if !errors.Is(err, testCase.want) {
				t.Fatalf("err=%v want=%v", err, testCase.want)
			}
		})
	}
}

// TestServiceItemReplyValidationAndPropagation 验证指定商品回复参数校验及底层错误传播。
func TestServiceItemReplyValidationAndPropagation(t *testing.T) {
	// backendErr 是模拟商品回复数据库故障的哨兵错误。
	backendErr := errors.New("item reply unavailable")
	// repository 是返回底层故障的测试仓储。
	repository := &keywordRepositoryFake{itemErr: backendErr}
	// service 是待验证的关键词应用服务。
	service := NewService(repository)
	// err 表示空商品标识返回的校验错误。
	if err := service.SetItemReply(context.Background(), 1, "account", "", "reply"); err == nil {
		t.Fatal("空商品 ID 应被拒绝")
	}
	// err 表示商品回复持久化阶段返回的基础设施错误。
	if err := service.SetItemReply(context.Background(), 1, "account", "item", "reply"); !errors.Is(err, backendErr) {
		t.Fatalf("基础设施错误未透传: %v", err)
	}
}

var _ Repository = (*keywordRepositoryFake)(nil)
