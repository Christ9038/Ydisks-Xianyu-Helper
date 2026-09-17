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
	// addedDrafts 保存本次流程中的全部创建输入，用于断言多选只落一条规则。
	addedDrafts []Draft
	// listRows 是列表操作返回的规则。
	listRows []Keyword
	// listErr 是列表操作返回的错误。
	listErr error
	// replaceErr 是批量替换返回的错误。
	replaceErr error
	// updateErr 是更新返回的错误。
	updateErr error
	// updatedDraft 保存最近一次更新输入。
	updatedDraft Draft
	// deleteErr 是删除返回的错误。
	deleteErr error
	// deletedIDs 保存按标识删除过的规则标识。
	deletedIDs []int64
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
	f.addedDrafts = append(f.addedDrafts, draft)
	return 9, f.addErr
}

// Replace 实现测试仓储的关键词批量替换端口。
func (f *keywordRepositoryFake) Replace(context.Context, int64, string, []Draft) error {
	return f.replaceErr
}

// Update 实现测试仓储的关键词更新端口。
func (f *keywordRepositoryFake) Update(_ context.Context, _ int64, _ string, _ int64, draft Draft) error {
	f.updatedDraft = draft
	return f.updateErr
}

// DeleteByID 实现测试仓储的关键词 ID 删除端口。
func (f *keywordRepositoryFake) DeleteByID(_ context.Context, _ int64, _ string, id int64) error {
	f.deletedIDs = append(f.deletedIDs, id)
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
		{name: "missing text reply", call: func(service *Service) error {
			// err 表示文字规则缺少回复正文的校验错误。
			_, err := service.Add(context.Background(), 1, "account", Draft{Keyword: "k"})
			return err
		}},
		{name: "invalid batch draft", call: func(service *Service) error {
			// err 表示批量替换中非法规则的校验错误。
			return service.Replace(context.Background(), 1, "account", []Draft{{Keyword: "k"}})
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

// TestServiceDelegatesKeywordAndItemReplyOperations 验证关键词和指定商品回复的成功路径均委托给仓储。
func TestServiceDelegatesKeywordAndItemReplyOperations(t *testing.T) {
	// ctx 是本测试所有服务调用共用的非取消上下文。
	ctx := context.Background()
	// repository 是返回固定数据的关键词仓储替身。
	repository := &keywordRepositoryFake{listRows: []Keyword{{ID: 1, Keyword: "价格"}}, itemRows: []ItemReply{{ItemID: "item-1", CookieID: "account-1", ReplyContent: "欢迎"}}}
	// service 是待验证的关键词应用服务。
	service := NewService(repository)
	// rows、listErr 保存关键词列表结果。
	rows, listErr := service.List(ctx, 1, "account-1")
	if listErr != nil || len(rows) != 1 {
		t.Fatalf("List rows=%+v err=%v", rows, listErr)
	}
	// replaceErr 保存批量替换结果。
	replaceErr := service.Replace(ctx, 1, "account-1", []Draft{{Keyword: " 图片 ", Type: "image", ImageURL: " https://example.invalid/a.png "}, {Keyword: " 文本 ", Reply: " 回复 "}})
	if replaceErr != nil {
		t.Fatalf("Replace: %v", replaceErr)
	}
	// updateErr 保存关键词更新结果。
	updateErr := service.Update(ctx, 1, "account-1", 1, Draft{Keyword: "价格", Reply: "50"})
	if updateErr != nil {
		t.Fatalf("Update: %v", updateErr)
	}
	// deleteIDErr、deleteIndexErr 保存两种关键词删除入口的结果。
	deleteIDErr := service.DeleteByID(ctx, 1, "account-1", 1)
	// deleteIndexErr 保存按索引删除关键词的结果。
	deleteIndexErr := service.DeleteByIndex(ctx, 1, "account-1", 0)
	if deleteIDErr != nil || deleteIndexErr != nil {
		t.Fatalf("DeleteByID=%v DeleteByIndex=%v", deleteIDErr, deleteIndexErr)
	}
	// itemRows、itemListErr 保存商品回复列表结果。
	itemRows, itemListErr := service.ListItemReplies(ctx, 1)
	if itemListErr != nil || len(itemRows) != 1 {
		t.Fatalf("ListItemReplies rows=%+v err=%v", itemRows, itemListErr)
	}
	// itemReply、getItemErr 保存指定商品回复读取结果。
	itemReply, getItemErr := service.GetItemReply(ctx, 1, "account-1", "item-1")
	if getItemErr != nil || itemReply.ReplyContent != "欢迎" {
		t.Fatalf("GetItemReply result=%+v err=%v", itemReply, getItemErr)
	}
	// setItemErr、deleteItemErr 保存指定商品回复写入和删除结果。
	setItemErr := service.SetItemReply(ctx, 1, "account-1", "item-1", "更新")
	// deleteItemErr 保存指定商品回复删除结果。
	deleteItemErr := service.DeleteItemReply(ctx, 1, "account-1", "item-1")
	if setItemErr != nil || deleteItemErr != nil {
		t.Fatalf("SetItemReply=%v DeleteItemReply=%v", setItemErr, deleteItemErr)
	}
}

// TestServiceRejectsInvalidOperationIdentifiers 验证更新、删除和商品回复入口拒绝非法标识。
func TestServiceRejectsInvalidOperationIdentifiers(t *testing.T) {
	// ctx 是本测试所有服务调用共用的非取消上下文。
	ctx := context.Background()
	// service 是使用空仓储替身的关键词应用服务。
	service := NewService(&keywordRepositoryFake{})
	// cases 描述每个非法标识入口及预期错误。
	cases := []struct {
		// name 是子测试名称。
		name string
		// call 执行当前非法参数场景。
		call func() error
	}{
		{name: "update id", call: func() error { return service.Update(ctx, 1, "account", 0, Draft{Keyword: "k", Reply: "r"}) }},
		{name: "update draft", call: func() error { return service.Update(ctx, 1, "account", 1, Draft{Keyword: "k"}) }},
		{name: "delete id", call: func() error { return service.DeleteByID(ctx, 1, "account", 0) }},
		{name: "delete index", call: func() error { return service.DeleteByIndex(ctx, 1, "account", -1) }},
		{name: "get item", call: func() error {
			// err 保存空商品标识触发的查询校验错误。
			_, err := service.GetItemReply(ctx, 1, "account", "")
			return err
		}},
		{name: "set item", call: func() error { return service.SetItemReply(ctx, 1, "account", "", "reply") }},
		{name: "delete item", call: func() error { return service.DeleteItemReply(ctx, 1, "account", "") }},
	}
	for /* item 表示当前非法标识场景。 */ _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			// err 保存当前非法标识返回的校验错误。
			err := item.call()
			if err == nil {
				t.Fatal("invalid identifier should fail")
			}
		})
	}
}

// TestServiceRejectsInvalidUsersAcrossOperations 验证所有公开入口都拒绝非法用户或未初始化服务。
func TestServiceRejectsInvalidUsersAcrossOperations(t *testing.T) {
	// ctx 是本测试所有服务调用共用的非取消上下文。
	ctx := context.Background()
	// cases 描述需要执行用户身份校验的入口。
	cases := []struct {
		// name 是子测试名称。
		name string
		// call 执行当前入口。
		call func(*Service) error
	}{
		{name: "list", call: func(service *Service) error {
			// err 保存列表入口的非法用户错误。
			_, err := service.List(ctx, 0, "account")
			return err
		}},
		{name: "replace", call: func(service *Service) error { return service.Replace(ctx, 0, "account", nil) }},
		{name: "update", call: func(service *Service) error {
			return service.Update(ctx, 0, "account", 1, Draft{Keyword: "k", Reply: "r"})
		}},
		{name: "delete id", call: func(service *Service) error { return service.DeleteByID(ctx, 0, "account", 1) }},
		{name: "delete index", call: func(service *Service) error { return service.DeleteByIndex(ctx, 0, "account", 0) }},
		{name: "item list", call: func(service *Service) error {
			// err 保存商品回复列表入口的非法用户错误。
			_, err := service.ListItemReplies(ctx, 0)
			return err
		}},
		{name: "item get", call: func(service *Service) error {
			// err 保存商品回复读取入口的非法用户错误。
			_, err := service.GetItemReply(ctx, 0, "account", "item")
			return err
		}},
		{name: "item set", call: func(service *Service) error { return service.SetItemReply(ctx, 0, "account", "item", "reply") }},
		{name: "item delete", call: func(service *Service) error { return service.DeleteItemReply(ctx, 0, "account", "item") }},
	}
	for /* item 表示当前用户身份校验场景。 */ _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			// err 保存非法用户触发的服务错误。
			err := item.call(NewService(&keywordRepositoryFake{}))
			if !errors.Is(err, ErrInvalidUser) {
				t.Fatalf("error=%v want ErrInvalidUser", err)
			}
		})
	}
	// nilServiceErr 保存 nil receiver 触发的服务初始化错误。
	var nilService *Service
	// nilServiceErr 保存 nil receiver 触发的服务初始化错误。
	_, nilServiceErr := nilService.List(ctx, 1, "account")
	if !errors.Is(nilServiceErr, ErrInvalidInput) {
		t.Fatalf("nil service error=%v", nilServiceErr)
	}
}

// TestValidationErrorText 验证输入错误的空值和自定义提示保持稳定。
func TestValidationErrorText(t *testing.T) {
	// emptyError 保存空验证错误的默认提示。
	var emptyError *ValidationError
	if emptyError.Error() != "关键词回复输入无效" {
		t.Fatalf("empty validation error=%q", emptyError.Error())
	}
	// customError 保存自定义验证提示。
	customError := (&ValidationError{Message: "自定义错误"}).Error()
	if customError != "自定义错误" {
		t.Fatalf("custom validation error=%q", customError)
	}
}
// TestSplitItemIDs 验证商品范围字段拆分、去重、去空白并保持首次出现顺序。
func TestSplitItemIDs(t *testing.T) {
	// cases 覆盖单值、多值、重复与空白商品范围的拆分边界。
	cases := []struct {
		// name 是子测试名称。
		name string
		// raw 是持久化的商品范围字段。
		raw string
		// want 是期望的拆分结果。
		want []string
	}{
		{name: "empty is account level", raw: "", want: []string{}},
		{name: "legacy single value", raw: "item-1", want: []string{"item-1"}},
		{name: "multiple values", raw: "item-1,item-2", want: []string{"item-1", "item-2"}},
		{name: "trim and dedupe", raw: " item-1 , item-2 ,item-1,", want: []string{"item-1", "item-2"}},
		{name: "blank only", raw: " , ,", want: []string{}},
	}
	for /* scenario 表示当前商品范围拆分场景。 */ _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			// got 是拆分后的商品标识集合。
			got := SplitItemIDs(scenario.raw)
			if len(got) != len(scenario.want) {
				t.Fatalf("got=%v want=%v", got, scenario.want)
			}
			// index、itemID 分别表示结果下标与期望项，用于逐项比对顺序。
			for index, itemID := range scenario.want {
				if got[index] != itemID {
					t.Fatalf("got=%v want=%v", got, scenario.want)
				}
			}
		})
	}
}

// TestJoinItemIDs 验证商品标识集合合并为稳定的逗号分隔字段。
func TestJoinItemIDs(t *testing.T) {
	// cases 覆盖空集合、单元素、多元素与重复元素的合并边界。
	cases := []struct {
		// name 是子测试名称。
		name string
		// itemIDs 是待合并的商品标识集合。
		itemIDs []string
		// want 是期望的持久化字段。
		want string
	}{
		{name: "empty becomes account level", itemIDs: nil, want: ""},
		{name: "single value stays raw", itemIDs: []string{"item-1"}, want: "item-1"},
		{name: "multiple values joined", itemIDs: []string{"item-1", "item-2"}, want: "item-1,item-2"},
		{name: "dedupe and trim", itemIDs: []string{" item-1 ", "item-1", "", "item-2"}, want: "item-1,item-2"},
	}
	for /* scenario 表示当前商品范围合并场景。 */ _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			if /* got 是合并后的持久化商品范围字段。 */ got := JoinItemIDs(scenario.itemIDs); got != scenario.want {
				t.Fatalf("got=%q want=%q", got, scenario.want)
			}
		})
	}
}

// TestServiceJoinsMultipleItemIDsIntoSingleRule 验证多选保存只落一条规则并写入逗号分隔字段。
func TestServiceJoinsMultipleItemIDsIntoSingleRule(t *testing.T) {
	// repository 是本测试使用的可控关键词仓储。
	repository := &keywordRepositoryFake{}
	// service 是待验证的关键词应用服务。
	service := NewService(repository)
	// id、err 保存多选规则的创建结果。
	id, err := service.Add(context.Background(), 7, "account-1", Draft{Keyword: " 价格 ", Reply: " 50元 ", ItemIDs: []string{"item-1", "item-2"}})
	if err != nil || id != 9 {
		t.Fatalf("多选保存失败 id=%d err=%v", id, err)
	}
	if repository.addedDraft.ItemID != "item-1,item-2" {
		t.Fatalf("商品范围未合并为单条规则: %q", repository.addedDraft.ItemID)
	}
	if len(repository.addedDrafts) != 1 {
		t.Fatalf("多选保存只应写入一条规则，实际 %d 条", len(repository.addedDrafts))
	}
	// accountLevel、accountLevelErr 保存无商品范围的账号级退化结果。
	accountLevelID, accountLevelErr := service.Add(context.Background(), 7, "account-1", Draft{Keyword: "k", Reply: "r"})
	if accountLevelErr != nil || accountLevelID != 9 {
		t.Fatalf("账号级保存失败 id=%d err=%v", accountLevelID, accountLevelErr)
	}
	if repository.addedDraft.ItemID != "" {
		t.Fatalf("账号级规则不应带商品范围: %q", repository.addedDraft.ItemID)
	}
}

// TestServiceAcceptsLegacySingleItemID 验证历史单值 ItemID 输入仍按单条规则保存。
func TestServiceAcceptsLegacySingleItemID(t *testing.T) {
	// repository 是本测试使用的可控关键词仓储。
	repository := &keywordRepositoryFake{}
	// service 是待验证的关键词应用服务。
	service := NewService(repository)
	// err 保存兼容单值输入的创建结果。
	err := func() error {
		// addErr 是兼容单值输入的创建错误。
		_, addErr := service.Add(context.Background(), 7, "account-1", Draft{Keyword: "k", Reply: "r", ItemID: "item-9"})
		return addErr
	}()
	if err != nil {
		t.Fatalf("兼容单值保存失败: %v", err)
	}
	// updateErr 保存兼容单值输入的更新结果。
	updateErr := service.Update(context.Background(), 7, "account-1", 5, Draft{Keyword: "k", Reply: "r", ItemID: "item-9"})
	if updateErr != nil {
		t.Fatalf("兼容单值更新失败: %v", updateErr)
	}
	if repository.updatedDraft.ItemID != "item-9" || len(repository.updatedDraft.ItemIDs) != 1 {
		t.Fatalf("兼容单值未被规范化: %+v", repository.updatedDraft)
	}
}

// TestServiceUpdateRewritesItemRange 验证编辑规则只更新同一行的商品范围字段。
func TestServiceUpdateRewritesItemRange(t *testing.T) {
	// repository 是本测试使用的可控关键词仓储。
	repository := &keywordRepositoryFake{}
	// service 是待验证的关键词应用服务。
	service := NewService(repository)
	// err 保存把商品范围改为多选的更新结果。
	err := service.Update(context.Background(), 7, "account-1", 5, Draft{Keyword: "新词", Reply: "新回复", ItemIDs: []string{"item-2", "item-3"}})
	if err != nil {
		t.Fatalf("多选更新失败: %v", err)
	}
	if repository.updatedDraft.ItemID != "item-2,item-3" {
		t.Fatalf("商品范围未写回同一行: %q", repository.updatedDraft.ItemID)
	}
	if len(repository.addedDrafts) != 0 || len(repository.deletedIDs) != 0 {
		t.Fatalf("编辑不应增删规则行 added=%+v deleted=%v", repository.addedDrafts, repository.deletedIDs)
	}
}

// TestServiceRejectsInvalidItemScopeInput 验证缺少关键词或回复内容时多选输入同样被拒绝。
func TestServiceRejectsInvalidItemScopeInput(t *testing.T) {
	// service 是待验证的关键词应用服务。
	service := NewService(&keywordRepositoryFake{})
	// cases 覆盖多选输入下的关键校验边界。
	cases := []struct {
		// name 是子测试名称。
		name string
		// draft 是当前待校验的规则输入。
		draft Draft
	}{
		{name: "missing keyword", draft: Draft{Reply: "r", ItemIDs: []string{"item-1"}}},
		{name: "missing text reply", draft: Draft{Keyword: "k", ItemIDs: []string{"item-1"}}},
		{name: "missing image url", draft: Draft{Keyword: "k", Type: "image", ItemIDs: []string{"item-1"}}},
	}
	for /* scenario 表示当前非法多选输入场景。 */ _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			if /* err 表示非法输入返回的校验错误。 */ err := service.Update(context.Background(), 7, "account-1", 5, scenario.draft); err == nil {
				t.Fatal("非法输入应被拒绝")
			}
		})
	}
}

var _ Repository = (*keywordRepositoryFake)(nil)
