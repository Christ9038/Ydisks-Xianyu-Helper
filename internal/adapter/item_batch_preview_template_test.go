package adapter

import (
	"context"
	"reflect"
	"testing"

	"xianyu-go/internal/db"
)

// TestItemBatchPreviewPortTemplateRequirements 用真实 SQLite 验证模板需求：变量键提取、停用、跨用户与不存在。
func TestItemBatchPreviewPortTemplateRequirements(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// port 是绑定测试存储的批量预检适配器。
	port := NewItemBatchPreviewPort(store)
	// enabledID、enabledErr 保存引用卡密变量与自定义变量的启用模板。
	enabledID, enabledErr := store.DeliveryTemplates.Create(ctx, db.DeliveryTemplateInput{UserID: 1, Name: "网盘模板", Enabled: true, Messages: []string{"资源：{{custom.link}} 卡密：{{cards.main}}", "提取码：{{custom.code}}"}})
	if enabledErr != nil {
		t.Fatal(enabledErr)
	}
	// requirements、requirementsErr 保存启用模板的需求。
	requirements, requirementsErr := port.DeliveryTemplateRequirements(ctx, 1, enabledID)
	if requirementsErr != nil || !requirements.Usable || !reflect.DeepEqual(requirements.CardKeys, []string{"main"}) || !reflect.DeepEqual(requirements.CustomKeys, []string{"link", "code"}) {
		t.Fatalf("启用模板需求异常: %+v err=%v", requirements, requirementsErr)
	}
	// foreign、foreignErr 保存跨用户读取结果，应视为不存在且无错误。
	if foreign, foreignErr := port.DeliveryTemplateRequirements(ctx, 2, enabledID); foreignErr != nil || foreign.Usable {
		t.Fatalf("跨用户模板应不可用: %+v err=%v", foreign, foreignErr)
	}
	// missing、missingErr 保存不存在模板的读取结果。
	if missing, missingErr := port.DeliveryTemplateRequirements(ctx, 1, enabledID+100); missingErr != nil || missing.Usable {
		t.Fatalf("不存在模板应不可用: %+v err=%v", missing, missingErr)
	}
	// disabledID、disabledErr 保存停用模板的创建结果。
	disabledID, disabledErr := store.DeliveryTemplates.Create(ctx, db.DeliveryTemplateInput{UserID: 1, Name: "停用模板", Enabled: false, Messages: []string{"{{custom.link}}"}})
	if disabledErr != nil {
		t.Fatal(disabledErr)
	}
	// disabled、disabledLookupErr 保存停用模板的需求。
	if disabled, disabledLookupErr := port.DeliveryTemplateRequirements(ctx, 1, disabledID); disabledLookupErr != nil || disabled.Usable {
		t.Fatalf("停用模板应不可用: %+v err=%v", disabled, disabledLookupErr)
	}
	// nilTemplates 是只装配账号与卡密仓储、缺少发货模板仓储的存储。
	nilTemplates := &db.Store{Cookies: store.Cookies, Cards: store.Cards}
	// nilErr 保存缺少模板仓储时的初始化错误。
	if _, nilErr := NewItemBatchPreviewPort(nilTemplates).DeliveryTemplateRequirements(ctx, 1, enabledID); nilErr == nil {
		t.Fatal("缺少模板仓储时应返回错误")
	}
	// uninitializedErr 保存未装配数据库时的初始化错误。
	if _, uninitializedErr := NewItemBatchPreviewPort(nil).DeliveryTemplateRequirements(ctx, 1, enabledID); uninitializedErr == nil {
		t.Fatal("未装配数据库时应返回错误")
	}
	cleanup()
	// closedErr 保存数据库关闭后的读取错误，应向上传递而不是视为不存在。
	if _, closedErr := port.DeliveryTemplateRequirements(ctx, 1, enabledID); closedErr == nil {
		t.Fatal("数据库故障应返回错误")
	}
}

// TestItemBatchPreviewPortTemplateCardBindable 验证模板可绑定卡密组的类型、启用状态与归属判断。
func TestItemBatchPreviewPortTemplateCardBindable(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// port 是绑定测试存储的批量预检适配器。
	port := NewItemBatchPreviewPort(store)
	// cases 是卡密组定义与期望的可绑定结果。
	cases := []struct {
		card db.CardFull
		want bool
	}{
		{db.CardFull{UserID: 1, Name: "文本", Type: "text", TextContent: "v", Enabled: true}, true},
		{db.CardFull{UserID: 1, Name: "批量", Type: "data", DataContent: "a\nb", Enabled: true}, true},
		{db.CardFull{UserID: 1, Name: "停用文本", Type: "text", TextContent: "v", Enabled: false}, false},
		{db.CardFull{UserID: 1, Name: "图片", Type: "image", ImageURL: "https://example.com/a.png", Enabled: true}, false},
		{db.CardFull{UserID: 1, Name: "未配置API", Type: "api", Enabled: true}, false},
	}
	// lastID 保存最后创建的卡密组主键，用于跨用户与不存在场景。
	var lastID int64
	// item 是当前卡密组样例。
	for _, item := range cases {
		// card 是当前样例的可写副本。
		card := item.card
		// cardID、createErr 保存卡密组创建结果。
		cardID, createErr := store.Cards.Create(ctx, &card)
		if createErr != nil {
			t.Fatalf("创建 %s 失败: %v", card.Name, createErr)
		}
		lastID = cardID
		// bindable、bindErr 保存可绑定判断结果。
		if bindable, bindErr := port.TemplateCardBindable(ctx, 1, cardID); bindErr != nil || bindable != item.want {
			t.Fatalf("%s 可绑定=%v err=%v want=%v", card.Name, bindable, bindErr, item.want)
		}
	}
	// foreign、foreignErr 保存跨用户判断结果。
	if foreign, foreignErr := port.TemplateCardBindable(ctx, 2, lastID-4); foreignErr != nil || foreign {
		t.Fatalf("跨用户卡密组不应可绑定: %v %v", foreign, foreignErr)
	}
	// missing、missingErr 保存不存在卡密组的判断结果。
	if missing, missingErr := port.TemplateCardBindable(ctx, 1, lastID+100); missingErr != nil || missing {
		t.Fatalf("不存在卡密组不应可绑定: %v %v", missing, missingErr)
	}
	// uninitializedErr 保存未装配数据库时的初始化错误。
	if _, uninitializedErr := NewItemBatchPreviewPort(nil).TemplateCardBindable(ctx, 1, lastID); uninitializedErr == nil {
		t.Fatal("未装配数据库时应返回错误")
	}
	cleanup()
	// closedErr 保存数据库关闭后的读取错误。
	if _, closedErr := port.TemplateCardBindable(ctx, 1, lastID); closedErr == nil {
		t.Fatal("数据库故障应返回错误")
	}
}
