package cards

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// cardRepositoryStub 记录应用服务对卡券持久化 Port 的调用，并允许测试注入各阶段结果。
type cardRepositoryStub struct {
	// cards 是列表查询返回的卡券组。
	cards []Card
	// card 是单卡查询返回的卡券组。
	card Card
	// listErr、getErr、createErr、updateErr、deleteErr 和 appendErr 分别控制各持久化阶段的失败结果。
	listErr, getErr, createErr, updateErr, deleteErr, appendErr error
	// createdID 是创建成功时返回的稳定标识。
	createdID int64
	// listedUserID 是列表查询收到的用户标识。
	listedUserID int64
	// gotCardID 是单卡查询收到的卡券标识。
	gotCardID int64
	// createdCard 和 updatedCard 是创建、更新阶段收到的应用模型。
	createdCard, updatedCard Card
	// deletedCardID 是删除阶段收到的卡券标识。
	deletedCardID int64
	// appendedCardID 和 appendedContent 是追加库存阶段收到的标识与内容。
	appendedCardID  int64
	appendedContent string
	// appendedCount 是追加成功时返回的有效库存行数。
	appendedCount int
}

// ListForUser 返回预设列表并记录用户隔离条件。
func (r *cardRepositoryStub) ListForUser(_ context.Context, userID int64) ([]Card, error) {
	r.listedUserID = userID
	return r.cards, r.listErr
}

// Get 返回预设卡券并记录查询标识。
func (r *cardRepositoryStub) Get(_ context.Context, cardID int64) (Card, error) {
	r.gotCardID = cardID
	return r.card, r.getErr
}

// Create 记录待创建卡券并返回预设标识或错误。
func (r *cardRepositoryStub) Create(_ context.Context, card Card) (int64, error) {
	r.createdCard = card
	return r.createdID, r.createErr
}

// Update 记录待更新卡券并返回预设错误。
func (r *cardRepositoryStub) Update(_ context.Context, card Card) error {
	r.updatedCard = card
	return r.updateErr
}

// Delete 记录待删除卡券标识并返回预设错误。
func (r *cardRepositoryStub) Delete(_ context.Context, cardID int64) error {
	r.deletedCardID = cardID
	return r.deleteErr
}

// AppendData 记录追加库存请求，并返回预设行数或错误。
func (r *cardRepositoryStub) AppendData(_ context.Context, cardID int64, content string) (int, error) {
	r.appendedCardID = cardID
	r.appendedContent = content
	return r.appendedCount, r.appendErr
}

// TestServiceListAndGet 验证列表隔离、详情归属和查询错误边界。
func TestServiceListAndGet(t *testing.T) {
	// repository 是本测试共享的可观测持久化替身。
	repository := &cardRepositoryStub{cards: []Card{{ID: 3, UserID: 7}}, card: Card{ID: 3, UserID: 7}}
	// service 是绑定替身仓储的卡券应用服务。
	service := NewService(repository)
	// ctx 是本测试使用的非取消上下文。
	ctx := context.Background()
	// cards、listErr 保存列表用例结果。
	cards, listErr := service.List(ctx, 7)
	if listErr != nil || repository.listedUserID != 7 || !reflect.DeepEqual(cards, repository.cards) {
		t.Fatalf("列表结果异常 cards=%+v user=%d err=%v", cards, repository.listedUserID, listErr)
	}
	// card、getErr 保存详情用例结果。
	card, getErr := service.Get(ctx, 7, 3)
	if getErr != nil || card.ID != 3 || repository.gotCardID != 3 {
		t.Fatalf("详情结果异常 card=%+v queried=%d err=%v", card, repository.gotCardID, getErr)
	}
	// infraErr 是用于验证数据库故障不会伪装成资源缺失的错误。
	infraErr := errors.New("database unavailable")
	repository.getErr = infraErr
	// err 表示详情查询透传的基础设施错误。
	if _, err := service.Get(ctx, 7, 3); !errors.Is(err, infraErr) {
		t.Fatalf("基础设施错误应原样返回，err=%v", err)
	}
	repository.getErr = nil
	repository.card.UserID = 8
	// err 表示跨用户详情查询返回的所有权错误。
	if _, err := service.Get(ctx, 7, 3); !errors.Is(err, ErrForbidden) {
		t.Fatalf("跨用户详情应拒绝，err=%v", err)
	}
}

// TestServiceCreateValidation 验证创建输入规则、API 类型限制和所有者写入。
func TestServiceCreateValidation(t *testing.T) {
	// repository 是记录创建输入的持久化替身。
	repository := &cardRepositoryStub{createdID: 19}
	// service 是待验证的卡券应用服务。
	service := NewService(repository)
	// valid 是覆盖全部可编辑字段的合法文本卡券输入。
	valid := Draft{Name: "文本卡", Type: "text", TextContent: "CODE", Description: "说明", Enabled: true, DelaySeconds: 9, IsMultiSpec: true, SpecName: "颜色", SpecValue: "蓝"}
	// createdID、createErr 保存创建结果。
	createdID, createErr := service.Create(context.Background(), 7, valid)
	if createErr != nil || createdID != 19 {
		t.Fatalf("创建结果异常 id=%d err=%v", createdID, createErr)
	}
	if repository.createdCard.UserID != 7 || repository.createdCard.Name != valid.Name || repository.createdCard.ID != 0 {
		t.Fatalf("创建模型未正确绑定所有者：%+v", repository.createdCard)
	}
	// testCase 是当前待验证的非法输入及期望错误。
	for _, testCase := range []struct {
		// name 是失败场景名称。
		name string
		// draft 是当前场景提交的卡券输入。
		draft Draft
		// want 是期望出现的稳定错误文本。
		want string
	}{
		{name: "missing-name", draft: Draft{Type: "text", TextContent: "x"}, want: "名称和类型不能为空"},
		{name: "invalid-type", draft: Draft{Name: "x", Type: "unknown"}, want: "类型必须为 text、data、image 或 api"},
		{name: "invalid-delay", draft: Draft{Name: "x", Type: "text", TextContent: "x", DelaySeconds: 3601}, want: "延时发货必须在 0 到 3600 秒之间"},
		{name: "empty-text", draft: Draft{Name: "x", Type: "text", TextContent: "  "}, want: "文本卡密内容不能为空"},
		{name: "empty-data", draft: Draft{Name: "x", Type: "data", DataContent: "\n"}, want: "数据卡密内容不能为空"},
		{name: "empty-image", draft: Draft{Name: "x", Type: "image", ImageURL: ""}, want: "图片卡密 URL 不能为空"},
		{name: "non-http-image", draft: Draft{Name: "x", Type: "image", ImageURL: "file:///tmp/card.png"}, want: "图片卡密 URL 必须是 HTTP(S) 地址"},
		{name: "credential-image", draft: Draft{Name: "x", Type: "image", ImageURL: "https://user:pass@example.com/card.png"}, want: "图片卡密 URL 必须是 HTTP(S) 地址"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			// err 是当前非法输入返回的业务校验结果。
			_, err := service.Create(context.Background(), 7, testCase.draft)
			// validationError 用于确认错误保留业务校验类型。
			var validationError *ValidationError
			if !errors.As(err, &validationError) || err.Error() != testCase.want {
				t.Fatalf("校验错误不匹配 err=%v want=%q", err, testCase.want)
			}
		})
	}
	// err 表示创建 API 卡券时的业务限制错误。
	if _, err := service.Create(context.Background(), 7, Draft{Name: "API", Type: "api"}); !errors.Is(err, ErrUnsupportedAPIType) {
		t.Fatalf("新建 API 卡券应拒绝，err=%v", err)
	}
}

// TestServiceUpdateAndDeleteOwnership 验证更新、删除不会接受请求覆盖所有者，并覆盖 API 转换限制与错误阶段。
func TestServiceUpdateAndDeleteOwnership(t *testing.T) {
	// updateErr 是更新持久化失败的预设错误。
	updateErr := errors.New("update failed")
	// repository 是当前测试的持久化替身，卡券归属于用户 7。
	repository := &cardRepositoryStub{card: Card{ID: 5, UserID: 7, Type: "text"}, updateErr: updateErr}
	// service 是待验证的卡券应用服务。
	service := NewService(repository)
	// draft 是合法的文本卡券更新输入。
	draft := Draft{Name: "new", Type: "text", TextContent: "value", Enabled: true}
	// err 表示更新卡券组时透传的持久化错误。
	if err := service.Update(context.Background(), 7, 5, draft); !errors.Is(err, updateErr) {
		t.Fatalf("更新错误应原样返回，err=%v", err)
	}
	if repository.updatedCard.ID != 5 || repository.updatedCard.UserID != 7 || repository.updatedCard.Name != "new" {
		t.Fatalf("更新模型未保留标识和所有者：%+v", repository.updatedCard)
	}
	// err 表示非 API 卡券转换为 API 类型时的业务错误。
	if err := service.Update(context.Background(), 7, 5, Draft{Name: "api", Type: "api"}); !errors.Is(err, ErrUnsupportedAPIType) {
		t.Fatalf("非 API 卡券转换为 API 应拒绝，err=%v", err)
	}
	repository.updateErr = nil
	repository.card.Type = "api"
	// err 表示既有 API 卡券继续编辑时的更新结果。
	if err := service.Update(context.Background(), 7, 5, Draft{Name: "legacy", Type: "api"}); err != nil {
		t.Fatalf("既有 API 卡券应允许继续编辑，err=%v", err)
	}
	repository.card.UserID = 8
	// err 表示跨用户删除卡券时的所有权错误。
	if err := service.Delete(context.Background(), 7, 5); !errors.Is(err, ErrForbidden) || repository.deletedCardID != 0 {
		t.Fatalf("跨用户删除应在持久化前拒绝，deleted=%d err=%v", repository.deletedCardID, err)
	}
	repository.card.UserID = 7
	// deleteErr 是删除阶段持久化失败的预设错误。
	deleteErr := errors.New("delete failed")
	repository.deleteErr = deleteErr
	// err 表示删除卡券组时透传的持久化错误。
	if err := service.Delete(context.Background(), 7, 5); !errors.Is(err, deleteErr) || repository.deletedCardID != 5 {
		t.Fatalf("删除错误或标识不匹配，deleted=%d err=%v", repository.deletedCardID, err)
	}
}

// TestServiceRejectsInvalidDependenciesAndIdentifiers 验证空仓储、无效用户和无效卡券标识均在持久化前失败。
func TestServiceRejectsInvalidDependenciesAndIdentifiers(t *testing.T) {
	// err 表示空仓储依赖导致的应用服务装配错误。
	if _, err := NewService(nil).List(context.Background(), 1); err == nil {
		t.Fatal("空仓储应返回装配错误")
	}
	// repository 是用于确认非法参数不会产生有效查询的持久化替身。
	repository := &cardRepositoryStub{}
	// service 是绑定替身仓储的卡券应用服务。
	service := NewService(repository)
	// err 表示无效用户身份导致的业务参数错误。
	if _, err := service.Get(context.Background(), 0, 1); !errors.Is(err, ErrInvalidUser) {
		t.Fatalf("无效用户应拒绝，err=%v", err)
	}
	// err 表示无效卡券标识导致的业务参数错误。
	if _, err := service.Get(context.Background(), 1, 0); !errors.Is(err, ErrInvalidCardID) {
		t.Fatalf("无效卡券标识应拒绝，err=%v", err)
	}
}

// TestServiceExistsOwned 验证批量发布使用的卡券归属查询在成功、越权、缺失和基础设施故障时保持可区分错误。
func TestServiceExistsOwned(t *testing.T) {
	// repository 是返回指定卡券详情的持久化替身。
	repository := &cardRepositoryStub{card: Card{ID: 8, UserID: 7}}
	// service 是绑定归属查询替身仓储的卡券应用服务。
	service := NewService(repository)
	// exists、err 保存当前用户成功查询卡券归属的结果。
	exists, err := service.ExistsOwned(context.Background(), 7, 8)
	if err != nil || !exists {
		t.Fatalf("归属查询应成功，exists=%t err=%v", exists, err)
	}
	// repository.card 切换为其他用户，验证越权不会被误报为存在。
	repository.card.UserID = 9
	// exists、err 保存越权查询的结果。
	exists, err = service.ExistsOwned(context.Background(), 7, 8)
	if exists || !errors.Is(err, ErrForbidden) {
		t.Fatalf("越权卡券应拒绝，exists=%t err=%v", exists, err)
	}
	// repository.card 恢复当前用户，随后注入资源缺失错误。
	repository.card.UserID = 7
	repository.getErr = ErrNotFound
	// exists、err 保存资源缺失查询的结果。
	exists, err = service.ExistsOwned(context.Background(), 7, 8)
	if exists || !errors.Is(err, ErrNotFound) {
		t.Fatalf("缺失卡券应返回未找到，exists=%t err=%v", exists, err)
	}
	// infraErr 是用于验证数据库故障不会被归一化为资源缺失的错误。
	infraErr := errors.New("card ownership unavailable")
	repository.getErr = infraErr
	// exists、err 保存基础设施故障查询的结果。
	exists, err = service.ExistsOwned(context.Background(), 7, 8)
	if exists || !errors.Is(err, infraErr) {
		t.Fatalf("基础设施错误应原样返回，exists=%t err=%v", exists, err)
	}
}

// TestServiceAppendData 验证追加库存的输入、归属、类型和持久化错误边界。
func TestServiceAppendData(t *testing.T) {
	// repository 是记录追加请求并提供卡券归属的持久化替身。
	repository := &cardRepositoryStub{
		card:          Card{ID: 8, UserID: 7, Type: "data"},
		appendedCount: 2,
	}
	// service 是绑定追加库存替身仓储的应用服务。
	service := NewService(repository)
	// added、addErr 保存成功追加的有效行数和错误。
	added, addErr := service.AppendData(context.Background(), 7, 8, " A\nB ")
	if addErr != nil || added != 2 || repository.appendedCardID != 8 || repository.appendedContent != "A\nB" {
		t.Fatalf("追加结果异常 added=%d err=%v id=%d content=%q", added, addErr, repository.appendedCardID, repository.appendedContent)
	}
	// err 表示空内容在查询卡券前返回的稳定校验错误。
	if _, err := service.AppendData(context.Background(), 7, 8, " \n "); !errors.As(err, new(*ValidationError)) {
		t.Fatalf("空内容应返回校验错误，err=%v", err)
	}
	// repository.card.UserID 切换为其他用户，验证所有权边界不会调用追加仓储。
	repository.card.UserID = 9
	repository.appendedCardID = 0
	// err 表示跨用户库存追加被所有权检查拒绝的业务错误。
	if _, err := service.AppendData(context.Background(), 7, 8, "C"); !errors.Is(err, ErrForbidden) || repository.appendedCardID != 0 {
		t.Fatalf("跨用户追加应拒绝且不写入，id=%d err=%v", repository.appendedCardID, err)
	}
	// repository.card 恢复为当前用户但标记为非 data 类型，验证类型约束。
	repository.card.UserID = 7
	repository.card.Type = "text"
	// err 表示非 data 卡券无法追加逐行库存的类型错误。
	if _, err := service.AppendData(context.Background(), 7, 8, "C"); !errors.Is(err, ErrNotDataType) {
		t.Fatalf("非 data 类型应拒绝，err=%v", err)
	}
	// getErr 是资源读取阶段返回的缺失错误，追加不得触发写入。
	repository.card.Type = "data"
	repository.getErr = ErrNotFound
	repository.appendedCardID = 0
	// err 表示资源读取阶段发现卡券不存在时返回的稳定错误。
	if _, err := service.AppendData(context.Background(), 7, 8, "C"); !errors.Is(err, ErrNotFound) || repository.appendedCardID != 0 {
		t.Fatalf("不存在卡券应返回未找到且不写入，id=%d err=%v", repository.appendedCardID, err)
	}
	repository.getErr = nil
	// infraErr 是追加阶段的基础设施故障，服务应原样透传。
	infraErr := errors.New("append unavailable")
	repository.appendErr = infraErr
	// err 表示库存持久化端口返回的基础设施错误。
	if _, err := service.AppendData(context.Background(), 7, 8, "C"); !errors.Is(err, infraErr) {
		t.Fatalf("追加基础设施错误应透传，err=%v", err)
	}
}

var _ Repository = (*cardRepositoryStub)(nil)
