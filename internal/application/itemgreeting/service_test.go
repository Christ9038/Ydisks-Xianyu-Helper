package itemgreeting

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeRepository 是内存持久化替身，记录调用并可注入错误。
type fakeRepository struct {
	// owners 保存账号到所有者的映射；缺失账号返回 ErrAccountNotFound。
	owners map[string]int64
	// rules 保存按主键索引的规则。
	rules map[int64]Rule
	// nextID 是下一条规则主键。
	nextID int64
	// createErr、updateErr 是注入的写入错误。
	createErr, updateErr error
	// cleared 记录被清理记录的账号。
	cleared []string
}

// CheckOwnership 返回预设所有者。
func (f *fakeRepository) CheckOwnership(_ context.Context, _ int64, cookieID string) (AccountOwnership, error) {
	// ok、owner 保存按键读取替身数据的结果及是否存在。
	owner, ok := f.owners[cookieID]
	if !ok {
		return AccountOwnership{}, ErrAccountNotFound
	}
	return AccountOwnership{OwnerID: owner}, nil
}

// Get 返回规则或 ErrRuleNotFound。
func (f *fakeRepository) Get(_ context.Context, id int64) (Rule, error) {
	// ok、rule 保存按键读取替身数据的结果及是否存在。
	rule, ok := f.rules[id]
	if !ok {
		return Rule{}, ErrRuleNotFound
	}
	return rule, nil
}

// ListByAccount 返回账号规则。
func (f *fakeRepository) ListByAccount(_ context.Context, cookieID string) ([]Rule, error) {
	// out 保存按遍历顺序收集的规则。
	var out []Rule
	// rule 是当前处理的规则。
	for _, rule := range f.rules {
		if rule.CookieID == cookieID {
			out = append(out, rule)
		}
	}
	return out, nil
}

// ListForUser 返回全部规则。
func (f *fakeRepository) ListForUser(_ context.Context, _ int64) ([]Rule, error) {
	// out 保存按遍历顺序收集的规则。
	var out []Rule
	// rule 是当前处理的规则。
	for _, rule := range f.rules {
		out = append(out, rule)
	}
	return out, nil
}

// Create 保存规则。
func (f *fakeRepository) Create(_ context.Context, rule Rule) (int64, error) {
	if f.createErr != nil {
		return 0, f.createErr
	}
	f.nextID++
	rule.ID = f.nextID
	f.rules[rule.ID] = rule
	return rule.ID, nil
}

// Update 覆盖规则。
func (f *fakeRepository) Update(_ context.Context, rule Rule) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.rules[rule.ID] = rule
	return nil
}

// Delete 删除规则。
func (f *fakeRepository) Delete(_ context.Context, id int64) error {
	delete(f.rules, id)
	return nil
}

// ClearRecords 记录清理账号。
func (f *fakeRepository) ClearRecords(_ context.Context, cookieID string) error {
	f.cleared = append(f.cleared, cookieID)
	return nil
}

// newFake 构造 acc1 属于用户 1、foreign 属于用户 2 的替身。
func newFake() *fakeRepository {
	return &fakeRepository{owners: map[string]int64{"acc1": 1, "foreign": 2}, rules: map[int64]Rule{}}
}

// TestServiceCreateNormalizesAndValidates 覆盖规整、去重和各类参数校验。
func TestServiceCreateNormalizesAndValidates(t *testing.T) {
	// repo 是内存持久化替身，用于隔离验证服务行为。
	repo := newFake()
	// service 是被测的服务实例，绑定本用例的测试依赖。
	service := NewService(repo)
	// ctx 是本用例共用的请求上下文。
	ctx := context.Background()
	// err、rule 保存 Create 的返回值及错误。
	rule, err := service.Create(ctx, 1, Draft{CookieID: "acc1", Name: " n ", ReplyContent: " hi ", ItemIDs: []string{" a ", "a", "", "b"}})
	if err != nil || rule.ID == 0 || rule.Name != "n" || rule.ReplyContent != "hi" || len(rule.ItemIDs) != 2 {
		t.Fatalf("rule=%+v err=%v", rule, err)
	}
	// long 是超过文字长度上限的回复内容。
	long := strings.Repeat("长", MaxContentRunes+1)
	// name、draft 是当前样例的名称和提交的规则草稿。
	for name, draft := range map[string]Draft{
		"no-items":     {CookieID: "acc1", ReplyContent: "x"},
		"no-content":   {CookieID: "acc1", ItemIDs: []string{"a"}},
		"long-content": {CookieID: "acc1", ReplyContent: long, ItemIDs: []string{"a"}},
		"long-name":    {CookieID: "acc1", Name: strings.Repeat("n", MaxNameRunes+1), ReplyContent: "x", ItemIDs: []string{"a"}},
		"long-image":   {CookieID: "acc1", ReplyImageURL: strings.Repeat("u", MaxImageURLRunes+1), ItemIDs: []string{"a"}},
		"too-many":     {CookieID: "acc1", ReplyContent: "x", ItemIDs: make([]string, 0)},
	} {
		if name == "too-many" {
			// i 是当前循环序号。
			for i := 0; i <= MaxItemsPerRule; i++ {
				draft.ItemIDs = append(draft.ItemIDs, strings.Repeat("i", 1)+string(rune('a'+i%26))+strings.Repeat("0", i/26))
			}
		}
		// err 保存 Create 的返回错误，非空时按该分支处理。
		if _, err := service.Create(ctx, 1, draft); !errors.Is(err, ErrInvalidInput) {
			t.Errorf("%s err=%v", name, err)
		}
	}
}

// TestServiceOwnershipAndErrors 覆盖未初始化、无效身份、越权、账号缺失、冲突透传、更新迁移账号和清理。
func TestServiceOwnershipAndErrors(t *testing.T) {
	// repo 是内存持久化替身，用于隔离验证服务行为。
	repo := newFake()
	// service 是被测的服务实例，绑定本用例的测试依赖。
	service := NewService(repo)
	// ctx 是本用例共用的请求上下文。
	ctx := context.Background()
	// draft 是当前样例提交的规则草稿。
	draft := Draft{CookieID: "acc1", ReplyContent: "x", ItemIDs: []string{"a"}}
	// err 保存 List 的返回错误，非空时按该分支处理。
	if _, err := (*Service)(nil).List(ctx, 1, ""); err == nil {
		t.Error("nil service should fail")
	}
	// err 保存 List 的返回错误，非空时按该分支处理。
	if _, err := service.List(ctx, 0, ""); !errors.Is(err, ErrInvalidUser) {
		t.Errorf("invalid user err=%v", err)
	}
	// err 保存 Create 的返回错误，非空时按该分支处理。
	if _, err := service.Create(ctx, 1, Draft{ReplyContent: "x", ItemIDs: []string{"a"}}); !errors.Is(err, ErrInvalidCookieID) {
		t.Errorf("empty cookie err=%v", err)
	}
	// err 保存 Create 的返回错误，非空时按该分支处理。
	if _, err := service.Create(ctx, 1, Draft{CookieID: "foreign", ReplyContent: "x", ItemIDs: []string{"a"}}); !errors.Is(err, ErrForbidden) {
		t.Errorf("forbidden err=%v", err)
	}
	// err 保存 Create 的返回错误，非空时按该分支处理。
	if _, err := service.Create(ctx, 1, Draft{CookieID: "nope", ReplyContent: "x", ItemIDs: []string{"a"}}); !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("missing account err=%v", err)
	}
	repo.createErr = &ItemConflictError{ItemIDs: []string{"a"}}
	// err 保存 Create 的返回错误，非空时按该分支处理。
	if _, err := service.Create(ctx, 1, draft); err == nil || !strings.Contains(err.Error(), "a") {
		t.Errorf("conflict err=%v", err)
	}
	repo.createErr = nil
	// created 保存 Create 的返回值。
	created, _ := service.Create(ctx, 1, draft)
	// 列表：账号过滤与全部。
	if rules, err := service.List(ctx, 1, "acc1"); err != nil || len(rules) != 1 {
		t.Errorf("list by account=%v err=%v", rules, err)
	}
	// err、rules 保存 List 的返回值及错误。
	if rules, err := service.List(ctx, 1, ""); err != nil || len(rules) != 1 {
		t.Errorf("list all=%v err=%v", rules, err)
	}
	// err 保存 List 的返回错误，非空时按该分支处理。
	if _, err := service.List(ctx, 1, "foreign"); !errors.Is(err, ErrForbidden) {
		t.Errorf("list foreign err=%v", err)
	}
	// 更新：迁移账号被拒，错误透传，正常更新保持账号。
	if _, err := service.Update(ctx, 1, created.ID, Draft{CookieID: "foreign", ReplyContent: "x", ItemIDs: []string{"a"}}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("move err=%v", err)
	}
	// err 保存 Update 的返回错误，非空时按该分支处理。
	if _, err := service.Update(ctx, 1, created.ID, Draft{ReplyContent: "", ItemIDs: []string{"a"}}); !errors.Is(err, ErrInvalidInput) {
		t.Errorf("update invalid err=%v", err)
	}
	repo.updateErr = errors.New("db")
	// err 保存 Update 的返回错误，非空时按该分支处理。
	if _, err := service.Update(ctx, 1, created.ID, Draft{ReplyContent: "y", ItemIDs: []string{"a"}}); err == nil {
		t.Error("update error should propagate")
	}
	repo.updateErr = nil
	// err、rule 保存 Update 的返回值及错误。
	if rule, err := service.Update(ctx, 1, created.ID, Draft{ReplyContent: "y", ItemIDs: []string{"a"}}); err != nil || rule.CookieID != "acc1" || rule.ReplyContent != "y" {
		t.Errorf("update=%+v err=%v", rule, err)
	}
	// err 保存 Update 的返回错误，非空时按该分支处理。
	if _, err := service.Update(ctx, 1, 999, draft); !errors.Is(err, ErrRuleNotFound) {
		t.Errorf("update missing err=%v", err)
	}
	// 他人规则不可改删。
	repo.rules[50] = Rule{ID: 50, CookieID: "foreign"}
	// err 保存 Delete 的返回错误，非空时按该分支处理。
	if err := service.Delete(ctx, 1, 50); !errors.Is(err, ErrForbidden) {
		t.Errorf("delete foreign err=%v", err)
	}
	// err 保存 Delete 的返回错误，非空时按该分支处理。
	if err := service.Delete(ctx, 1, created.ID); err != nil {
		t.Errorf("delete err=%v", err)
	}
	// err 保存 ClearRecords 的返回错误，非空时按该分支处理。
	if err := service.ClearRecords(ctx, 1, "acc1"); err != nil || len(repo.cleared) != 1 {
		t.Errorf("clear err=%v cleared=%v", err, repo.cleared)
	}
	// err 保存 ClearRecords 的返回错误，非空时按该分支处理。
	if err := service.ClearRecords(ctx, 1, "foreign"); !errors.Is(err, ErrForbidden) {
		t.Errorf("clear foreign err=%v", err)
	}
}

// TestServiceGreetingLibraryImageSatisfiesContentAndExcludesURL 验证首次回复可只用图片库图片，且与 URL 互斥。
func TestServiceGreetingLibraryImageSatisfiesContentAndExcludesURL(t *testing.T) {
	// service 是绑定内存仓储的被测服务。
	service := NewService(newFake())
	// rule、err 保存仅含图片库图片的创建结果。
	rule, err := service.Create(context.Background(), 1, Draft{CookieID: "acc1", ReplyLibraryImageID: 9, ItemIDs: []string{"a"}})
	if err != nil || rule.ReplyLibraryImageID != 9 {
		t.Fatalf("rule=%+v err=%v", rule, err)
	}
	// draft 是待校验的一份草稿输入；name 是当前场景的名称，用于失败信息定位。
	for name, draft := range map[string]Draft{
		"both":     {CookieID: "acc1", ReplyImageURL: "https://a.example/x.png", ReplyLibraryImageID: 9, ItemIDs: []string{"a"}},
		"negative": {CookieID: "acc1", ReplyLibraryImageID: -1, ItemIDs: []string{"a"}},
	} {
		// invalid 保存非法图片来源的输入错误。
		var invalid *InvalidInputError
		// createErr 保存创建调用的失败原因，应命中图片来源校验错误。
		if _, createErr := service.Create(context.Background(), 1, draft); !errors.As(createErr, &invalid) {
			t.Fatalf("%s 应为输入错误: %v", name, createErr)
		}
	}
}
