package items

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	automationapp "xianyu-go/internal/application/automation"
)

// sheetTemplateOwnershipFake 在账号与卡券归属替身之上，按模板主键返回预置的模板变量需求。
type sheetTemplateOwnershipFake struct {
	batchPreviewOwnershipFake
	// templates 是模板主键到变量需求的映射；不在表中的模板视为不存在。
	templates map[int64]BatchTemplateRequirements
	// unbindable 是归属当前用户但不能被模板绑定（停用或类型不支持）的卡密组。
	unbindable int64
	// requirementsErr 模拟模板需求查询故障。
	requirementsErr error
}

// DeliveryTemplateRequirements 返回预置的模板需求，模拟查询故障时返回错误。
func (fake sheetTemplateOwnershipFake) DeliveryTemplateRequirements(_ context.Context, _ int64, templateID int64) (BatchTemplateRequirements, error) {
	if fake.requirementsErr != nil {
		return BatchTemplateRequirements{}, fake.requirementsErr
	}
	return fake.templates[templateID], nil
}

// TemplateCardBindable 认为除 unbindable 外的卡密组都可以绑定。
func (fake sheetTemplateOwnershipFake) TemplateCardBindable(_ context.Context, _ int64, cardID int64) (bool, error) {
	return cardID != fake.unbindable, nil
}

// newSheetTemplateService 创建带模板需求替身的预检服务：模板 1 需要 link/code 变量和 main 卡密，模板 2 只需要 gift 变量，模板 3 已停用。
func newSheetTemplateService(t *testing.T) *BatchPreviewService {
	t.Helper()
	// ownership 是包含三个模板需求的归属替身；卡密组 9 归属当前用户，10 归属但不可绑定。
	ownership := sheetTemplateOwnershipFake{
		batchPreviewOwnershipFake: batchPreviewOwnershipFake{cookieOwned: "acc"},
		templates: map[int64]BatchTemplateRequirements{
			1: {Usable: true, CardKeys: []string{"main"}, CustomKeys: []string{"link", "code"}},
			2: {Usable: true, CustomKeys: []string{"gift"}},
			3: {Usable: false, CustomKeys: []string{"link"}},
		},
		unbindable: 10,
	}
	ownership.cardOwned = 9
	// service、err 保存创建的预检服务及失败原因。
	service, err := NewBatchPreviewService(ownership, batchPreviewImageFake{})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

// TestParseCardActionsParsesTemplateTokensAndKeepsLegacyFormat 覆盖模板令牌的各种写法、与卡密动作混写的顺序以及非法令牌。
func TestParseCardActionsParsesTemplateTokensAndKeepsLegacyFormat(t *testing.T) {
	// legacy、legacyErr 保存旧版卡密动作的解析结果，必须与改动前一致。
	legacy, legacyErr := parseCardActions("101:1;102:2:5")
	// legacyWant 是旧版卡密动作的期望结果。
	legacyWant := []BatchPreviewCardAction{{CardID: 101, DeliveryCount: 1}, {CardID: 102, DeliveryCount: 2, DelaySeconds: 5}}
	if legacyErr != "" || !reflect.DeepEqual(legacy, legacyWant) {
		t.Fatalf("旧格式解析结果变化: %+v %q", legacy, legacyErr)
	}
	// mixed、mixedErr 保存混写动作的解析结果。
	mixed, mixedErr := parseCardActions("101:1；T12\n模板7：30;t8:")
	if mixedErr != "" || len(mixed) != 4 {
		t.Fatalf("混写解析失败: %+v %q", mixed, mixedErr)
	}
	if mixed[0].ActionType != "" || mixed[0].CardID != 101 {
		t.Fatalf("首项应为卡密动作: %+v", mixed[0])
	}
	// wants 是三条模板动作的模板主键和延迟期望。
	wants := []struct{ id, delay int }{{12, 0}, {7, 30}, {8, 0}}
	// index、want 表示当前模板动作序号及期望值。
	for index, want := range wants {
		// action 是混写结果中对应的模板动作。
		action := mixed[index+1]
		if action.ActionType != BatchActionSendTemplate || action.DeliveryTemplateID != int64(want.id) || action.DelaySeconds != want.delay || action.DeliveryCount != 1 || action.CardID != 0 {
			t.Fatalf("模板动作 %d 解析异常: %+v", index, action)
		}
	}
	// invalidCases 是非法模板令牌及期望的错误片段。
	invalidCases := map[string]string{"T0": "发货模板ID无效", "Tx": "发货模板ID无效", "模板": "发货模板ID无效", "T12:-1": "延迟秒不能小于0", "T12:a": "延迟秒不能小于0", "T12:1:2": "模板动作格式错误", "101;T": "第2项发货模板ID无效"}
	// raw、want 表示当前非法文本及期望错误。
	for raw, want := range invalidCases {
		// actions、parseErr 保存非法文本的解析结果。
		actions, parseErr := parseCardActions(raw)
		if actions != nil || !strings.Contains(parseErr, want) {
			t.Errorf("非法令牌 %q: actions=%+v err=%q want=%q", raw, actions, parseErr, want)
		}
	}
	// formatErr 保存卡密动作格式错误，提示中需同时给出两种写法。
	if _, formatErr := parseCardActions("9:1:2:3"); !strings.Contains(formatErr, "卡密组ID:每件份数:延迟秒") || !strings.Contains(formatErr, "T模板ID") {
		t.Fatalf("格式错误提示缺少写法说明: %q", formatErr)
	}
}

// TestParseSheetTemplateInputsCollectsColumns 覆盖变量列别名、包含分隔符的取值、空值、冲突、非法键和绑定列格式。
func TestParseSheetTemplateInputsCollectsColumns(t *testing.T) {
	// inputs 保存只含普通列的行解析结果，应为 nil。
	if inputs :=parseSheetTemplateInputs(map[string]any{"title": "商品", "付款发货内容": "101"}); inputs != nil {
		t.Fatalf("没有模板列时必须返回 nil: %+v", inputs)
	}
	// link 是包含冒号、分号和查询参数的网盘链接，必须完整保留。
	link := "https://pan.example.com/s/abc?a=1;b=2:3"
	// inputs 保存合法模板列的解析结果。
	inputs := parseSheetTemplateInputs(map[string]any{"变量.link": link, "模板变量.code": " x7k2 ", "CUSTOM.extra": "e", "变量.empty": "", "模板卡密.main": "9：2", "模板卡密.alt": "11", "模板卡密.none": ""})
	if inputs == nil || inputs.ParseError != "" {
		t.Fatalf("合法模板列解析失败: %+v", inputs)
	}
	if !reflect.DeepEqual(inputs.Variables, map[string]string{"link": link, "code": "x7k2", "extra": "e"}) {
		t.Fatalf("变量解析异常: %+v", inputs.Variables)
	}
	// bindingsWant 是按列名字典序排列的期望绑定。
	bindingsWant := []BatchPreviewTemplateBinding{{VariableKey: "alt", CardID: 11, DeliveryCount: 1}, {VariableKey: "main", CardID: 9, DeliveryCount: 2}}
	if !reflect.DeepEqual(inputs.Bindings, bindingsWant) {
		t.Fatalf("绑定解析异常: %+v", inputs.Bindings)
	}
	// same 保存两个别名列给出相同取值时的解析结果，不应报冲突。
	if same := parseSheetTemplateInputs(map[string]any{"变量.link": "a", "custom.link": "a"}); same.ParseError != "" {
		t.Fatalf("相同取值不应冲突: %q", same.ParseError)
	}
	// errorCases 是非法模板列及期望错误片段。
	errorCases := []struct {
		fields map[string]any
		want   string
	}{
		{map[string]any{"变量.link": "a", "custom.link": "b"}, "模板变量 link 在多列中取值不一致"},
		{map[string]any{"变量.网盘": "a"}, "模板变量列「变量.网盘」名称只能包含"},
		{map[string]any{"模板卡密.主卡": "9"}, "模板卡密列「模板卡密.主卡」名称只能包含"},
		{map[string]any{"模板卡密.main": "abc"}, "模板卡密 main 格式应为"},
		{map[string]any{"模板卡密.main": "0"}, "模板卡密 main 格式应为"},
		{map[string]any{"模板卡密.main": "9:0"}, "模板卡密 main 格式应为"},
		{map[string]any{"模板卡密.main": "9:x"}, "模板卡密 main 格式应为"},
		{map[string]any{"模板卡密.main": "9:1:2"}, "模板卡密 main 格式应为"},
		{map[string]any{"模板卡密.a": "x", "模板卡密.b": "y"}, "模板卡密 a 格式应为"},
	}
	// item 是当前非法模板列样例。
	for _, item := range errorCases {
		// got 保存当前样例的解析结果。
		got := parseSheetTemplateInputs(item.fields)
		if got == nil || !strings.Contains(got.ParseError, item.want) {
			t.Errorf("fields=%v 期望错误 %q 实际 %+v", item.fields, item.want, got)
			continue
		}
		if strings.Contains(got.ParseError, "abc") {
			t.Errorf("错误提示不应回显单元格取值: %q", got.ParseError)
		}
	}
}

// TestParseAutomationReportsTemplateInputErrorsOnlyForTemplateActions 验证模板列错误只在使用模板动作时报告，且不覆盖动作文本自身的错误。
func TestParseAutomationReportsTemplateInputErrorsOnlyForTemplateActions(t *testing.T) {
	// fields 中变量列非法，付款发货使用模板，评价赠品只发卡密。
	fields := map[string]any{"paid_delivery_enabled": "是", "paid_delivery_contents": "T1", "review_gift_enabled": "是", "review_gift_contents": "9", "变量.网盘": "a"}
	// automation 保存解析结果。
	automation := parseAutomation(fields)
	if !strings.Contains(automation.PaidDelivery.ParseError, "名称只能包含") || automation.ReviewGift.ParseError != "" {
		t.Fatalf("模板列错误归属异常: paid=%q review=%q", automation.PaidDelivery.ParseError, automation.ReviewGift.ParseError)
	}
	fields["paid_delivery_contents"] = "T0"
	// got 保存动作文本与模板列同时出错时的提示，应优先报告动作文本错误。
	if got := parseAutomation(fields).PaidDelivery.ParseError; !strings.Contains(got, "发货模板ID无效") {
		t.Fatalf("动作文本错误应优先: %q", got)
	}
	// legacy 保存旧格式行的解析结果，不应带表格模板输入。
	if legacy :=parseAutomation(map[string]any{"paid_delivery_contents": "9"}); legacy.sheetTemplateInputs != nil {
		t.Fatalf("旧表格不应产生模板输入: %+v", legacy.sheetTemplateInputs)
	}
}

// TestBatchPreviewSheetTemplateActionsEndToEnd 从 CSV 解析到预检再到规则动作，验证变量按模板裁剪、缺失项拦截且错误不含变量值。
func TestBatchPreviewSheetTemplateActionsEndToEnd(t *testing.T) {
	// link 是第一行的网盘链接，包含会被旧解析器拆坏的分隔符。
	link := "https://pan.example.com/s/abc?a=1;b=2:3"
	// csvText 包含：合法行（两个模板共享变量）、缺变量与缺卡密的行、停用模板行、不可绑定卡密行、旧格式行。
	csvText := "账号ID,标题,价格,图片,邮费模式,付款发货启用,付款发货内容,评价赠品启用,评价赠品内容,变量.link,变量.code,变量.gift,模板卡密.main,模板卡密.unused\n" +
		"acc,网盘资源A,9.9,a.jpg,无需邮寄,是,T1,是,T2:60,\"" + link + "\",x7k2,礼物,9:2,9\n" +
		"acc,网盘资源B,9.9,a.jpg,none,是,T1,否,,\"" + link + "\",,,,\n" +
		"acc,网盘资源C,9.9,a.jpg,none,是,T3,否,,\"" + link + "\",,,,\n" +
		"acc,网盘资源D,9.9,a.jpg,none,是,T1,否,,\"" + link + "\",x,,10,\n" +
		"acc,旧格式商品,9.9,a.jpg,free,是,9:1,否,,,,,,\n"
	// fields、parseErr 保存 CSV 解析结果。
	fields, parseErr := ParseSheet([]byte(csvText), "batch.csv", 50)
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	// rows、previewErr 保存预检结果。
	rows, previewErr := newSheetTemplateService(t).Preview(context.Background(), BatchPreviewInput{UserID: 1, Rows: fields})
	if previewErr != nil || len(rows) != 5 {
		t.Fatalf("预检失败: rows=%d err=%v", len(rows), previewErr)
	}
	if len(rows[0].Errors) != 0 || rows[0].PostageMode != "none" {
		t.Fatalf("合法行不应报错: %+v", rows[0].Errors)
	}
	// paid 是第一行付款发货模板动作，只应携带模板 1 需要的变量与绑定。
	paid := rows[0].Automation.PaidDelivery.Actions[0]
	if !reflect.DeepEqual(paid.CustomVariables, map[string]string{"link": link, "code": "x7k2"}) || !reflect.DeepEqual(paid.TemplateBindings, []BatchPreviewTemplateBinding{{VariableKey: "main", CardID: 9, DeliveryCount: 2}}) {
		t.Fatalf("付款发货模板输入裁剪异常: %+v", paid)
	}
	// gift 是第一行评价赠品模板动作，只应携带 gift 变量且没有绑定。
	gift := rows[0].Automation.ReviewGift.Actions[0]
	if !reflect.DeepEqual(gift.CustomVariables, map[string]string{"gift": "礼物"}) || gift.TemplateBindings != nil || gift.DelaySeconds != 60 {
		t.Fatalf("评价赠品模板输入裁剪异常: %+v", gift)
	}
	// missing 是第二行的错误合集，应同时报告缺卡密和缺变量，且不含链接。
	missing := strings.Join(rows[1].Errors, "|")
	if !strings.Contains(missing, "付款发货第1项缺少模板卡密 main") || !strings.Contains(missing, "付款发货第1项缺少模板变量 code") || strings.Contains(missing, "缺少模板变量 link") || strings.Contains(missing, "pan.example.com") {
		t.Fatalf("缺失项报错异常: %s", missing)
	}
	// joined 是停用模板行的错误合集，不应继续报告变量缺失。
	if joined := strings.Join(rows[2].Errors, "|"); !strings.Contains(joined, "发货模板不存在或已停用") || strings.Contains(joined, "缺少模板变量") {
		t.Fatalf("停用模板应只报告模板不可用: %s", joined)
	}
	// joined 是绑定他人卡密组行的错误合集。
	if joined := strings.Join(rows[3].Errors, "|"); !strings.Contains(joined, "卡密组不存在或不属于当前用户") {
		t.Fatalf("他人卡密组应被拦截: %s", joined)
	}
	// legacyRow 是旧格式行，结果应与改动前一致。
	legacyRow := rows[4]
	if len(legacyRow.Errors) != 0 || legacyRow.Automation.sheetTemplateInputs == nil || !reflect.DeepEqual(legacyRow.Automation.PaidDelivery.Actions, []BatchPreviewCardAction{{CardID: 9, DeliveryCount: 1}}) {
		t.Fatalf("旧格式卡密行异常: %+v", legacyRow)
	}
	// automationJSON 是第一行持久化到批次明细的自动化配置，不能包含未被模板使用的变量。
	automationJSON, _ := json.Marshal(rows[0].Automation)
	if strings.Contains(string(automationJSON), "unused") || strings.Contains(string(automationJSON), "sheetTemplateInputs") {
		t.Fatalf("持久化配置包含多余输入: %s", automationJSON)
	}
	// config 是发布阶段读取的自动化配置。
	var config batchPublishAutomationConfig
	// err 保存持久化配置的反序列化错误。
	if err := json.Unmarshal(automationJSON, &config); err != nil {
		t.Fatal(err)
	}
	// actions 是付款发货规则的动作链。
	actions := batchDeliveryActions(config.PaidDelivery.Actions)
	if len(actions) != 1 || actions[0].ActionType != automationapp.ActionSendTemplate || actions[0].DeliveryTemplateID != 1 || actions[0].CustomVariables["link"] != link || len(actions[0].TemplateBindings) != 1 {
		t.Fatalf("规则动作转换异常: %+v", actions)
	}
}

// TestBatchPreviewSheetTemplateUnbindableCard 验证归属当前用户但停用或类型不支持的卡密组不能绑定到模板。
func TestBatchPreviewSheetTemplateUnbindableCard(t *testing.T) {
	// ownership 声明卡密组 10 归属但不可绑定。
	ownership := sheetTemplateOwnershipFake{batchPreviewOwnershipFake: batchPreviewOwnershipFake{cookieOwned: "acc", cardOwned: 10}, templates: map[int64]BatchTemplateRequirements{1: {Usable: true, CardKeys: []string{"main"}}}, unbindable: 10}
	// service、err 保存预检服务及失败原因。
	service, err := NewBatchPreviewService(ownership, batchPreviewImageFake{})
	if err != nil {
		t.Fatal(err)
	}
	// rows、previewErr 保存预检结果。
	rows, previewErr := service.Preview(context.Background(), BatchPreviewInput{UserID: 1, Rows: []map[string]any{{"cookie_id": "acc", "title": "t", "price": "1", "images": "a.jpg", "paid_delivery_enabled": "是", "paid_delivery_contents": "T1", "模板卡密.main": "10"}}})
	if previewErr != nil {
		t.Fatal(previewErr)
	}
	// joined 是该行的错误合集，应只报告不可绑定而非缺少绑定。
	if joined := strings.Join(rows[0].Errors, "|"); !strings.Contains(joined, "模板卡密 main 绑定的卡密组已停用或类型不支持") || strings.Contains(joined, "缺少模板卡密") {
		t.Fatalf("不可绑定卡密组报错异常: %s", joined)
	}
}

// TestPreviewRowsTemplateActionRequiresCustomVariables 覆盖资源库模板铺货等结构化行：模板动作缺少变量或绑定不符时在预检阶段拦截。
func TestPreviewRowsTemplateActionRequiresCustomVariables(t *testing.T) {
	// service 使用模板需求替身。
	service := newSheetTemplateService(t)
	// base 是合法结构化行的模板，没有表格模板输入。
	base := BatchPreviewRow{CookieID: "acc", Title: "商品", Price: "1", Quantity: 1, PostageMode: "none", Images: []string{"a.jpg"}}
	// complete 携带模板 1 需要的全部输入。
	complete := base
	complete.Automation.PaidDelivery = BatchPreviewCardAutomation{Enabled: true, Actions: []BatchPreviewCardAction{{ActionType: BatchActionSendTemplate, DeliveryTemplateID: 1, CustomVariables: map[string]string{"link": "l", "code": "c"}, TemplateBindings: []BatchPreviewTemplateBinding{{VariableKey: "main", CardID: 9, DeliveryCount: 1}}}}}
	// incomplete 变量为空白、卡密重复绑定并多出一个未使用的绑定。
	incomplete := base
	incomplete.Automation.PaidDelivery = BatchPreviewCardAutomation{Enabled: true, Actions: []BatchPreviewCardAction{{ActionType: BatchActionSendTemplate, DeliveryTemplateID: 1, CustomVariables: map[string]string{"link": " "}, TemplateBindings: []BatchPreviewTemplateBinding{{VariableKey: "main", CardID: 9}, {VariableKey: "main", CardID: 9}, {VariableKey: "other", CardID: 9}}}}}
	// rows、err 保存预检结果。
	rows, err := service.PreviewRows(context.Background(), BatchPreviewInput{UserID: 1}, []BatchPreviewRow{complete, incomplete})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows[0].Errors) != 0 {
		t.Fatalf("完整模板动作不应报错: %v", rows[0].Errors)
	}
	// joined 是不完整行的错误合集。
	joined := strings.Join(rows[1].Errors, "|")
	for /* want 是期望出现的错误片段。 */ _, want := range []string{"缺少模板变量 link", "缺少模板变量 code", "模板卡密 main 重复绑定", "模板未使用卡密变量 other"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("错误 %q 缺少 %q", joined, want)
		}
	}
}

// TestResolveSheetTemplateActionsGuards 覆盖没有表格输入、归属实现不支持模板、模板查询失败与空动作时的原样返回。
func TestResolveSheetTemplateActionsGuards(t *testing.T) {
	// automation 是带表格输入的模板动作配置。
	automation := BatchPreviewAutomation{PaidDelivery: BatchPreviewCardAutomation{Actions: []BatchPreviewCardAction{{ActionType: BatchActionSendTemplate, DeliveryTemplateID: 1}, {CardID: 9, DeliveryCount: 1}}}, sheetTemplateInputs: &sheetTemplateInputs{Variables: map[string]string{"link": "l"}}}
	// plain 是不支持模板端口的预检服务。
	plain, err := NewBatchPreviewService(batchPreviewOwnershipFake{}, batchPreviewImageFake{})
	if err != nil {
		t.Fatal(err)
	}
	// got 保存不支持模板端口时的解析结果，模板动作应保持空输入。
	if got := plain.resolveSheetTemplateActions(context.Background(), 1, automation); got.PaidDelivery.Actions[0].CustomVariables != nil {
		t.Fatalf("不支持模板端口时不应填充: %+v", got.PaidDelivery.Actions[0])
	}
	// failing 是模板需求查询失败的预检服务。
	failing, err := NewBatchPreviewService(sheetTemplateOwnershipFake{requirementsErr: errors.New("db down")}, batchPreviewImageFake{})
	if err != nil {
		t.Fatal(err)
	}
	// got 保存查询失败时的解析结果。
	got := failing.resolveSheetTemplateActions(context.Background(), 1, automation)
	if got.PaidDelivery.Actions[0].CustomVariables != nil || got.ReviewGift.Actions != nil {
		t.Fatalf("查询失败时应保持空输入: %+v", got)
	}
	// noInputs 是没有表格输入的配置，应原样返回。
	noInputs := automation
	noInputs.sheetTemplateInputs = nil
	if !reflect.DeepEqual(failing.resolveSheetTemplateActions(context.Background(), 1, noInputs), noInputs) {
		t.Fatal("没有表格输入时应原样返回")
	}
	if automation.PaidDelivery.Actions[0].CustomVariables != nil {
		t.Fatal("解析不得修改调用方的动作切片")
	}
	// resolved 保存模板需求可用时的填充结果，模板未使用的变量不出现。
	resolved := newSheetTemplateService(t).resolveSheetTemplateActions(context.Background(), 1, automation)
	if !reflect.DeepEqual(resolved.PaidDelivery.Actions[0].CustomVariables, map[string]string{"link": "l"}) || resolved.PaidDelivery.Actions[0].TemplateBindings != nil || resolved.PaidDelivery.Actions[1].CustomVariables != nil {
		t.Fatalf("模板输入填充异常: %+v", resolved.PaidDelivery.Actions)
	}
	if automation.PaidDelivery.Actions[0].CustomVariables != nil {
		t.Fatal("解析不得修改调用方的动作切片")
	}
}
