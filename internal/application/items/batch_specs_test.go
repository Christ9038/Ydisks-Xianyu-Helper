package items

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	automationapp "xianyu-go/internal/application/automation"
)

// testBatchSpecs 构造 颜色(红/蓝) × 尺码(S/M) 的合法四组合快照，红色带规格图。
func testBatchSpecs() *BatchSpecs {
	// property 简化单个 SKU 规格对的构造。
	property := func(color, size string) []PublishSKUProperty {
		return []PublishSKUProperty{{PropertyText: "颜色", ValueText: color}, {PropertyText: "尺码", ValueText: size}}
	}
	return &BatchSpecs{
		Specs: []PublishSpec{
			{PropertyName: "颜色", SupportImage: true, Values: []PublishSpecValue{{Value: "红", ImageIndex: 0}, {Value: "蓝", ImageIndex: -1}}},
			{PropertyName: "尺码", Values: []PublishSpecValue{{Value: "S", ImageIndex: -1}, {Value: "M", ImageIndex: -1}}},
		},
		SKUs: []PublishSKU{
			{PriceCents: 1000, Quantity: 1, PropertyList: property("红", "S")},
			{PriceCents: 1100, Quantity: 2, PropertyList: property("红", "M")},
			{PriceCents: 1200, Quantity: 3, PropertyList: property("蓝", "S")},
			{PriceCents: 1300, Quantity: 4, PropertyList: property("蓝", "M")},
		},
		SpecImages: []string{"library/1.png"},
	}
}

// TestValidateBatchSpecs 覆盖单规格、合法多规格和各类非法规格分支。
func TestValidateBatchSpecs(t *testing.T) {
	// err 保存 ValidateBatchSpecs 调用的失败原因。
	if err := ValidateBatchSpecs(nil); err != nil {
		t.Fatalf("nil 应视为单规格: %v", err)
	}
	// err 保存 ValidateBatchSpecs 调用的失败原因。
	if err := ValidateBatchSpecs(&BatchSpecs{SKUs: []PublishSKU{{}}}); err == nil {
		t.Fatal("单规格不能携带 SKU")
	}
	// err 保存 ValidateBatchSpecs 调用的失败原因。
	if err := ValidateBatchSpecs(testBatchSpecs()); err != nil {
		t.Fatalf("合法规格被拒绝: %v", err)
	}
	// cases 保存对合法快照做单点破坏的变体及期望的错误片段。
	cases := []struct {
		// name 是用例名。
		name string
		// mutate 对快照做单点破坏。
		mutate func(*BatchSpecs)
		// want 是期望出现在错误中的片段。
		want string
	}{
		{"三个规格类型", func(s *BatchSpecs) { s.Specs = append(s.Specs, PublishSpec{PropertyName: "材质"}) }, "最多支持 2"},
		{"规格名为空", func(s *BatchSpecs) { s.Specs[0].PropertyName = " " }, "不能为空"},
		{"规格名含分号", func(s *BatchSpecs) { s.Specs[0].PropertyName = "颜;色" }, "分号"},
		{"规格名重复", func(s *BatchSpecs) { s.Specs[1].PropertyName = "颜色" }, "不能重复"},
		{"规格值为空", func(s *BatchSpecs) { s.Specs[0].Values[0].Value = "" }, "空规格值"},
		{"规格值含分号", func(s *BatchSpecs) { s.Specs[0].Values[0].Value = "红；绿" }, "分号"},
		{"规格值重复", func(s *BatchSpecs) { s.Specs[0].Values[1].Value = "红" }, "不能重复"},
		{"图片索引越界", func(s *BatchSpecs) { s.Specs[0].Values[0].ImageIndex = 5 }, "图片索引"},
		{"规格值不足两个", func(s *BatchSpecs) { s.Specs[1].Values = s.Specs[1].Values[:1] }, "至少需要 2"},
		{"SKU 数量不符", func(s *BatchSpecs) { s.SKUs = s.SKUs[:3] }, "数量与规格值不匹配"},
		{"SKU 价格为零", func(s *BatchSpecs) { s.SKUs[0].PriceCents = 0 }, "价格"},
		{"SKU 库存为零", func(s *BatchSpecs) { s.SKUs[0].Quantity = 0 }, "库存"},
		{"SKU 规格对缺失", func(s *BatchSpecs) { s.SKUs[0].PropertyList = s.SKUs[0].PropertyList[:1] }, "不完整"},
		{"SKU 引用未知值", func(s *BatchSpecs) { s.SKUs[0].PropertyList[0].ValueText = "绿" }, "不匹配"},
		{"SKU 规格名不符", func(s *BatchSpecs) { s.SKUs[0].PropertyList[0].PropertyText = "款式" }, "不匹配"},
		{"SKU 组合重复", func(s *BatchSpecs) { s.SKUs[1].PropertyList = s.SKUs[0].PropertyList }, "不能重复"},
	}
	// tc 是当前破坏用例。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// specs 是被破坏的深拷贝快照。
			specs := cloneBatchSpecs(t, testBatchSpecs())
			tc.mutate(specs)
			// err 保存校验错误。
			err := ValidateBatchSpecs(specs)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v want contains %q", err, tc.want)
			}
		})
	}
	// tooMany 构造超过 1500 组合的规格。
	tooMany := &BatchSpecs{Specs: []PublishSpec{{PropertyName: "a"}, {PropertyName: "b"}}}
	// index 是构造超限规格值时的序号。
	for index := 0; index < 40; index++ {
		tooMany.Specs[0].Values = append(tooMany.Specs[0].Values, PublishSpecValue{Value: fmt.Sprintf("x%d", index), ImageIndex: -1})
		tooMany.Specs[1].Values = append(tooMany.Specs[1].Values, PublishSpecValue{Value: fmt.Sprintf("y%d", index), ImageIndex: -1})
	}
	// err 保存 ValidateBatchSpecs 调用的失败原因。
	if err := ValidateBatchSpecs(tooMany); err == nil || !strings.Contains(err.Error(), "1500") {
		t.Fatalf("组合上限未生效: %v", err)
	}
}

// cloneBatchSpecs 通过 JSON 深拷贝快照，避免用例互相污染。
func cloneBatchSpecs(t *testing.T, specs *BatchSpecs) *BatchSpecs {
	t.Helper()
	// raw、err 保存序列化结果。
	raw, err := json.Marshal(specs)
	if err != nil {
		t.Fatal(err)
	}
	// clone 保存反序列化出的副本。
	var clone BatchSpecs
	// err 保存 Unmarshal 调用的失败原因。
	if err := json.Unmarshal(raw, &clone); err != nil {
		t.Fatal(err)
	}
	return &clone
}

// TestBatchSpecsCodecAndCombinations 覆盖快照编解码、库存汇总和规格元组规范化。
func TestBatchSpecsCodecAndCombinations(t *testing.T) {
	// encoded、err 保存多规格快照编码结果。
	encoded, err := EncodeBatchSpecs(testBatchSpecs())
	if err != nil || encoded == "" {
		t.Fatalf("encoded=%q err=%v", encoded, err)
	}
	// decoded 保存回读结果。
	decoded, err := DecodeBatchSpecs(encoded)
	if err != nil || !decoded.IsMultiSpec() || decoded.TotalQuantity() != 10 {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}
	// combinations 验证规格元组使用中文分号规范化格式。
	combinations, err := decoded.Combinations()
	if err != nil || len(combinations) != 4 || combinations[0] != (SKUCombination{SpecName: "颜色；尺码", SpecValue: "红；S"}) {
		t.Fatalf("combinations=%+v err=%v", combinations, err)
	}
	// raw 是遍历 []string 时的当前元素。
	for _, raw := range []string{"", "  ", "{}", "null", `{"specs":[]}`} {
		// got 保存 DecodeBatchSpecs 的返回结果，decodeErr 保存其失败原因。
		if got, decodeErr := DecodeBatchSpecs(raw); got != nil || decodeErr != nil {
			t.Fatalf("raw=%q 应解码为单规格: %v %v", raw, got, decodeErr)
		}
	}
	// decodeErr 保存 DecodeBatchSpecs 调用的失败原因。
	if _, decodeErr := DecodeBatchSpecs("{bad"); decodeErr == nil {
		t.Fatal("损坏快照必须报错")
	}
	// single 保存 EncodeBatchSpecs 的返回结果，encodeErr 保存其失败原因。
	if single, encodeErr := EncodeBatchSpecs(nil); single != "" || encodeErr != nil {
		t.Fatalf("单规格编码应为空串: %q %v", single, encodeErr)
	}
	if (*BatchSpecs)(nil).TotalQuantity() != 0 {
		t.Fatal("nil 快照库存应为 0")
	}
	// combos、comboErr 是空规格组合展开的结果及失败原因。
	if combos, comboErr := (*BatchSpecs)(nil).Combinations(); combos != nil || comboErr != nil {
		t.Fatal("单规格没有规格元组")
	}
	// broken 构造规格名为空的快照，规范化应失败。
	broken := cloneBatchSpecs(t, testBatchSpecs())
	broken.SKUs[0].PropertyList[0].PropertyText = ""
	// comboErr 保存 Combinations 调用的失败原因。
	if _, comboErr := broken.Combinations(); comboErr == nil {
		t.Fatal("空规格名应导致规范化失败")
	}
}

// templatePreviewOwnershipFake 在卡券归属替身之上增加发货模板可用性判断。
type templatePreviewOwnershipFake struct {
	batchPreviewOwnershipFake
	// usableTemplate 是唯一可用的发货模板标识。
	usableTemplate int64
}

// DeliveryTemplateUsable 只承认预置的模板。
func (fake templatePreviewOwnershipFake) DeliveryTemplateUsable(_ context.Context, _ int64, templateID int64) (bool, error) {
	return templateID == fake.usableTemplate, fake.err
}

// previewRowForSpecs 构造带多规格快照和按 SKU 覆盖发货动作的合法预检行。
func previewRowForSpecs() BatchPreviewRow {
	// specs 是合法多规格快照。
	specs := testBatchSpecs()
	// actions 为每个 SKU 配置一条发卡动作。
	var actions []BatchPreviewCardAction
	// combination 是当前 SKU 的规格元组。
	combinations, _ := specs.Combinations()
	// combination 是遍历 combinations 时的当前元素。
	for _, combination := range combinations {
		actions = append(actions, BatchPreviewCardAction{CardID: 7, DeliveryCount: 1, SpecName: combination.SpecName, SpecValue: combination.SpecValue})
	}
	return BatchPreviewRow{CookieID: "acc", Title: "商品", Price: "10.00", Quantity: 10, PostageMode: "free", Images: []string{"a.png"},
		Specs: specs, Automation: BatchPreviewAutomation{PaidDelivery: BatchPreviewCardAutomation{Enabled: true, Actions: actions}}}
}

// TestPreviewRowsValidatesSpecsAndActions 覆盖结构化行的多规格、规格图、SKU 覆盖与模板动作校验。
func TestPreviewRowsValidatesSpecsAndActions(t *testing.T) {
	// service 使用支持模板核对的归属替身。
	service, err := NewBatchPreviewService(templatePreviewOwnershipFake{batchPreviewOwnershipFake: batchPreviewOwnershipFake{cookieOwned: "acc", cardOwned: 7}, usableTemplate: 3}, batchPreviewImageFake{invalid: "bad.png"})
	if err != nil {
		t.Fatal(err)
	}
	// input 是预检上下文。
	input := BatchPreviewInput{UserID: 1, DefaultCookieID: "acc"}
	// rowsErr 保存 PreviewRows 调用的失败原因。
	if _, rowsErr := service.PreviewRows(context.Background(), input, nil); rowsErr != ErrBatchPreviewNoRows {
		t.Fatalf("空输入应返回 ErrBatchPreviewNoRows: %v", rowsErr)
	}
	// good 是完全合法的行。
	good := previewRowForSpecs()
	good.CookieID = ""
	// badImage 的规格图被拒绝。
	badImage := previewRowForSpecs()
	badImage.Specs.SpecImages = []string{"bad.png"}
	// missingSKU 少覆盖一个 SKU。
	missingSKU := previewRowForSpecs()
	missingSKU.Automation.PaidDelivery.Actions = missingSKU.Automation.PaidDelivery.Actions[:3]
	// stray 含有不属于任何 SKU 的规格动作。
	stray := previewRowForSpecs()
	stray.Automation.PaidDelivery.Actions[0].SpecValue = "红；XL"
	// brokenSpecs 的规格本身非法。
	brokenSpecs := previewRowForSpecs()
	brokenSpecs.Specs.SKUs = brokenSpecs.Specs.SKUs[:2]
	// template 使用模板动作并绑定有效卡密组。
	template := previewRowForSpecs()
	template.Specs = nil
	template.Automation.PaidDelivery.Actions = []BatchPreviewCardAction{{ActionType: BatchActionSendTemplate, DeliveryTemplateID: 3, TemplateBindings: []BatchPreviewTemplateBinding{{VariableKey: "k", CardID: 7, DeliveryCount: 1}}}}
	// badTemplate 引用不可用模板，且绑定他人卡密组。
	badTemplate := previewRowForSpecs()
	badTemplate.Specs = nil
	badTemplate.Automation.PaidDelivery.Actions = []BatchPreviewCardAction{{ActionType: BatchActionSendTemplate, DeliveryTemplateID: 99, TemplateBindings: []BatchPreviewTemplateBinding{{VariableKey: "k", CardID: 8}}}}
	// unknownType 使用不支持的动作类型和未规范化的规格绑定。
	unknownType := previewRowForSpecs()
	unknownType.Specs = nil
	unknownType.Automation.PaidDelivery.Actions = []BatchPreviewCardAction{{ActionType: "weird", DeliveryCount: 1, SpecName: "颜色", SpecValue: "红;"}}
	// rows 与期望错误片段一一对应，空串表示该行应无错误。
	rows := []BatchPreviewRow{good, badImage, missingSKU, stray, brokenSpecs, template, badTemplate, unknownType}
	// wants 是与各校验用例一一对应的期望错误片段，空串表示应通过。
	wants := []string{"", "图片文件不存在", "每个 SKU", "有效 SKU", "数量与规格值不匹配", "", "发货模板不存在", "动作类型不受支持"}
	// result 保存校验后的行。
	result, err := service.PreviewRows(context.Background(), input, rows)
	if err != nil || len(result) != len(rows) {
		t.Fatalf("result=%d err=%v", len(result), err)
	}
	// index 是当前行下标。
	for index := range result {
		// joined 是该行所有错误拼接，便于子串断言。
		joined := strings.Join(result[index].Errors, "|")
		if wants[index] == "" {
			if joined != "" {
				t.Fatalf("row %d 不应有错误: %s", index, joined)
			}
			continue
		}
		if !strings.Contains(joined, wants[index]) {
			t.Fatalf("row %d 错误=%q 缺少 %q", index, joined, wants[index])
		}
	}
	if result[0].CookieID != "acc" || result[0].RowNo != 2 || result[0].Description != "商品" {
		t.Fatalf("默认值未补齐: %+v", result[0])
	}
	if !strings.Contains(strings.Join(result[6].Errors, "|"), "绑定的卡密组") || !strings.Contains(strings.Join(result[7].Errors, "|"), "规格绑定格式错误") {
		t.Fatalf("模板绑定/规格格式错误缺失: %v %v", result[6].Errors, result[7].Errors)
	}
	// 未实现模板端口的归属替身不能使用模板动作。
	plain, _ := NewBatchPreviewService(batchPreviewOwnershipFake{cookieOwned: "acc", cardOwned: 7}, batchPreviewImageFake{})
	// plainRows 保存 PreviewRows 的返回结果。
	plainRows, _ := plain.PreviewRows(context.Background(), input, []BatchPreviewRow{template})
	if !strings.Contains(strings.Join(plainRows[0].Errors, "|"), "发货模板不可用") {
		t.Fatalf("缺少模板端口应拒绝模板动作: %v", plainRows[0].Errors)
	}
	// 未装配服务返回错误。
	if _, nilErr := (*BatchPreviewService)(nil).PreviewRows(context.Background(), input, rows); nilErr == nil {
		t.Fatal("nil 服务必须报错")
	}
}

// TestBatchLocalPublishCreatesSKUBoundAndTemplateRules 验证多规格商品标记、SKU 绑定动作和发货模板动作的规则转换。
func TestBatchLocalPublishCreatesSKUBoundAndTemplateRules(t *testing.T) {
	// completionRepository、itemRepository、ruleRepository 是收口服务的替身依赖。
	completionRepository := &batchCompletionRepositoryFake{batch: BatchInfo{Status: "running", WorkerToken: "worker"}}
	// itemRepository 记录本地商品写入。
	itemRepository := &batchPublishedItemRepositoryFake{}
	// ruleRepository 记录自动化规则写入。
	ruleRepository := &batchPublishRuleRepositoryFake{}
	// service 保存 NewBatchLocalPublishService 的返回结果，err 保存其失败原因。
	service, err := NewBatchLocalPublishService(completionRepository, itemRepository, ruleRepository)
	if err != nil {
		t.Fatal(err)
	}
	// specsJSON 是多规格快照文本。
	specsJSON, _ := EncodeBatchSpecs(testBatchSpecs())
	// automation 同时包含 SKU 发卡动作和发货模板动作。
	automation := BatchPreviewAutomation{PaidDelivery: BatchPreviewCardAutomation{Enabled: true, Actions: []BatchPreviewCardAction{
		{CardID: 7, DeliveryCount: 2, SpecName: "颜色；尺码", SpecValue: "红；S"},
		{ActionType: BatchActionSendTemplate, DeliveryTemplateID: 3, CustomVariables: map[string]string{"k": "v"},
			TemplateBindings: []BatchPreviewTemplateBinding{{VariableKey: "key", CardID: 9, DeliveryCount: 1}}, SpecName: "颜色；尺码", SpecValue: "蓝；M"},
	}}}
	// automationJSON 是持久化的自动化配置。
	automationJSON, _ := json.Marshal(automation)
	// row 是带多规格快照的批量行，顶层库存为 1，仍应因 SKU 汇总库存启用多数量交付。
	row := BatchRow{ID: 1, BatchID: "b", CookieID: "c", Title: "t", Quantity: 1, SpecsJSON: specsJSON, AutomationJSON: string(automationJSON)}
	// completeErr 保存 Complete 调用的失败原因。
	if completeErr := service.Complete(context.Background(), 9, row, "worker", &BatchPublishResult{ItemID: "i1"}); completeErr != nil {
		t.Fatal(completeErr)
	}
	if !itemRepository.item.IsMultiSpec || !itemRepository.item.MultiQuantityDelivery {
		t.Fatalf("多规格商品标记异常: %+v", itemRepository.item)
	}
	// actions 是付款发货规则的动作链。
	actions := ruleRepository.inputs[0].Actions
	if len(actions) != 3 || actions[0].ActionType != automationapp.ActionSendCard || actions[1].ActionType != automationapp.ActionSendTemplate || actions[2].ActionType != automationapp.ActionConfirmShipment {
		t.Fatalf("动作链异常: %+v", actions)
	}
	if !strings.Contains(actions[0].ConfigJSON, `"spec_name":"颜色；尺码"`) || !strings.Contains(actions[0].ConfigJSON, `"spec_value":"红；S"`) || !strings.Contains(actions[0].ConfigJSON, `"delay_override":true`) {
		t.Fatalf("SKU 绑定配置缺失: %s", actions[0].ConfigJSON)
	}
	if actions[1].DeliveryTemplateID != 3 || actions[1].CustomVariables["k"] != "v" || len(actions[1].TemplateBindings) != 1 || actions[1].TemplateBindings[0].CardID != 9 || actions[1].CardID != 0 {
		t.Fatalf("模板动作转换异常: %+v", actions[1])
	}
	// 损坏的规格快照在远端成功后按后置错误处理，不写入商品。
	itemRepository.item = BatchPublishedItem{}
	// bad 是规格快照损坏的批次行副本。
	bad := row
	bad.SpecsJSON = "{bad"
	// postErr 保存后置本地收口错误的类型化视图。
	var postErr *PostPublishError
	// completeErr 保存 Complete 调用的失败原因。
	if completeErr := service.Complete(context.Background(), 9, bad, "worker", &BatchPublishResult{ItemID: "i1"}); !errors.As(completeErr, &postErr) || itemRepository.item.ItemID != "" {
		t.Fatalf("损坏快照处理异常: %v", completeErr)
	}
}

// TestNormalizePostageModeAcceptsAllPublishModes 验证批量导入接受与单品发布一致的四种邮费模式及其中文别名。
func TestNormalizePostageModeAcceptsAllPublishModes(t *testing.T) {
	// cases 保存用户输入及其归一化后的稳定模式。
	cases := map[string]string{"包邮": "free", "按距离计费": "distance", "一口价邮费": "fixed", "固定邮费": "fixed", "无需邮寄": "none", "NONE": "none"}
	// input、want 是当前输入与期望模式。
	for input, want := range cases {
		// got 是实际归一化结果。
		if got := normalizePostageMode(input); got != want {
			t.Fatalf("normalizePostageMode(%q)=%q want %q", input, got, want)
		}
	}
	// service 是用于校验邮费模式的预检服务。
	service, err := NewBatchPreviewService(templatePreviewOwnershipFake{batchPreviewOwnershipFake: batchPreviewOwnershipFake{cookieOwned: "acc", cardOwned: 7}}, batchPreviewImageFake{})
	if err != nil {
		t.Fatal(err)
	}
	// mode 是待验证的邮费模式。
	for _, mode := range []string{"free", "distance", "none"} {
		// row 使用该模式且不填邮费，应通过校验。
		row := previewRowForSpecs()
		row.Specs, row.Automation, row.PostageMode, row.Postage = nil, BatchPreviewAutomation{}, mode, ""
		// rows、rowsErr 保存预检结果及失败原因。
		rows, rowsErr := service.PreviewRows(context.Background(), BatchPreviewInput{UserID: 1, DefaultCookieID: "acc"}, []BatchPreviewRow{row})
		if rowsErr != nil || len(rows[0].Errors) != 0 {
			t.Fatalf("%s 应通过校验: err=%v errors=%v", mode, rowsErr, rows[0].Errors)
		}
	}
}
