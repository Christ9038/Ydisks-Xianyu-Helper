package adapter

import (
	"errors"
	"strings"
	"testing"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/db"
)

// TestAdapterPureHelpersCoverBoundaryInputs 验证适配层金额、路径、类目、XML 标签和日志截断纯函数边界。
func TestAdapterPureHelpersCoverBoundaryInputs(t *testing.T) {
	// truncateCases 保存消息截断长度的边界样例。
	truncateCases := []struct {
		// raw 是待截断的日志消息。
		raw string
		// limit 是允许保留的最大字节数。
		limit int
		// want 是截断后的预期消息。
		want string
	}{
		{raw: "hello", limit: 0, want: "hello"},
		{raw: "hello", limit: 5, want: "hello"},
		{raw: "hello", limit: 3, want: "hel"},
	}
	// truncateCase 表示当前消息截断样例。
	for _, truncateCase := range truncateCases {
		// got 保存当前消息截断结果。
		if got := truncateMessage(truncateCase.raw, truncateCase.limit); got != truncateCase.want {
			t.Errorf("消息截断=%q want=%q", got, truncateCase.want)
		}
	}
	// moneyCases 保存平台金额文本的合法、空值和非法格式样例。
	moneyCases := []struct {
		// raw 是用户输入的元金额文本。
		raw string
		// want 是转换后的分值。
		want int64
		// bad 表示该输入是否应返回格式错误。
		bad bool
	}{
		{raw: "", want: 0},
		{raw: "¥12.30", want: 1230},
		{raw: "+1", want: 100},
		{raw: "-0.01", want: -1},
		{raw: "12.345", bad: true},
		{raw: "1.2.3", bad: true},
		{raw: "abc", bad: true},
	}
	// moneyCase 表示当前金额转换样例。
	for _, moneyCase := range moneyCases {
		// got、err 保存金额转换结果及格式错误。
		got, err := parseBatchMoneyCents(moneyCase.raw)
		if moneyCase.bad {
			if err == nil {
				t.Errorf("非法金额=%q 应返回错误", moneyCase.raw)
			}
			continue
		}
		if err != nil || got != moneyCase.want {
			t.Errorf("金额=%q got=%d err=%v want=%d", moneyCase.raw, got, err, moneyCase.want)
		}
	}
	// categoryCases 保存批次默认类目的空值、完整值和损坏值样例。
	categoryCases := []struct {
		// raw 是数据库中保存的类目 JSON。
		raw string
		// wantNil 表示空配置是否应被视为未设置。
		wantNil bool
		// bad 表示配置是否应被拒绝。
		bad bool
	}{
		{raw: "", wantNil: true},
		{raw: "{}", wantNil: true},
		{raw: `{"cat_id":"1","cat_name":"食品","channel_cat_id":"2"}`},
		{raw: `{"cat_id":"1"}`, bad: true},
		{raw: "not-json", bad: true},
	}
	// categoryCase 表示当前批次类目解析样例。
	for _, categoryCase := range categoryCases {
		// category、err 保存类目解析结果及错误。
		category, err := batchPublishCategory(categoryCase.raw)
		if categoryCase.bad {
			if err == nil {
				t.Errorf("损坏类目=%q 应返回错误", categoryCase.raw)
			}
			continue
		}
		if err != nil || (categoryCase.wantNil && category != nil) || (!categoryCase.wantNil && category == nil) {
			t.Errorf("类目=%q category=%v err=%v", categoryCase.raw, category, err)
		}
	}
	// pathCases 保存普通字段、数组下标和非法路径段样例。
	pathCases := []struct {
		// raw 是待拆分的 API 路径段。
		raw string
		// wantName 是字段名称。
		wantName string
		// wantIndex 是数组下标。
		wantIndex int
		// wantArray 表示是否包含数组下标。
		wantArray bool
		// wantOK 表示路径段是否合法。
		wantOK bool
	}{
		{raw: "buyer", wantName: "buyer", wantOK: true},
		{raw: "items[2]", wantName: "items", wantIndex: 2, wantArray: true, wantOK: true},
		{raw: "items[-1]", wantOK: false},
		{raw: "items[x]", wantOK: false},
		{raw: "items[", wantOK: false},
	}
	// pathCase 表示当前 API 路径解析样例。
	for _, pathCase := range pathCases {
		// name、index、hasIndex、ok 保存路径拆分结果。
		name, index, hasIndex, ok := parseAPIPathSegment(pathCase.raw)
		if name != pathCase.wantName || index != pathCase.wantIndex || hasIndex != pathCase.wantArray || ok != pathCase.wantOK {
			t.Errorf("路径=%q got=(%q,%d,%v,%v)", pathCase.raw, name, index, hasIndex, ok)
		}
	}
	// tagCases 保存 XML 标签名校验的合法和非法字符。
	tagCases := []struct {
		// raw 是待校验的 XML 标签名。
		raw string
		// want 表示标签名是否可安全使用。
		want bool
	}{
		{raw: "field_1", want: true},
		{raw: "field-name", want: true},
		{raw: "1field", want: false},
		{raw: "field name", want: false},
		{raw: "", want: false},
	}
	// tagCase 表示当前 XML 标签名校验样例。
	for _, tagCase := range tagCases {
		// got 保存当前标签名是否合法的判断结果。
		if got := isXMLTagName(tagCase.raw); got != tagCase.want {
			t.Errorf("标签=%q got=%v want=%v", tagCase.raw, got, tagCase.want)
		}
	}
	// dialectCases 保存金额表达式在三种数据库方言下的关键语法。
	dialectCases := []struct {
		// dialect 是数据库方言。
		dialect db.Dialect
		// marker 是表达式中应出现的方言标记。
		marker string
	}{
		{dialect: db.DialectSQLite, marker: "GLOB"},
		{dialect: db.DialectMySQL, marker: "REGEXP"},
		{dialect: db.DialectPostgres, marker: "DOUBLE PRECISION"},
	}
	// dialectCase 表示当前金额表达式方言样例。
	for _, dialectCase := range dialectCases {
		// expression 保存当前方言生成的金额清洗表达式。
		if expression := amountExpression(dialectCase.dialect, "orders.amount"); expression == "" || !strings.Contains(expression, dialectCase.marker) {
			t.Errorf("方言=%v 金额表达式=%q", dialectCase.dialect, expression)
		}
	}
}

// TestAPIJSONTemplateReplacementCoversNestedArraysAndScalarValues 验证 API 卡券模板的对象、数组、字符串和标量递归替换。
func TestAPIJSONTemplateReplacementCoversNestedArraysAndScalarValues(t *testing.T) {
	// variables 保存订单上下文中的模板变量替换值。
	variables := map[string]string{"{order_id}": "order-1", "{buyer_id}": "buyer-1"}
	// input 保存包含对象、数组和非字符串标量的 API 模板。
	input := map[string]any{
		"order":  "{order_id}",
		"nested": map[string]any{"buyer": "{buyer_id}"},
		"items":  []any{"{order_id}", 7, nil},
	}
	// output 保存复制并递归替换后的 API 模板对象。
	output := replaceAPIJSONMap(input, variables)
	if output["order"] != "order-1" {
		t.Fatalf("订单模板替换=%v", output["order"])
	}
	// nested 保存替换后的嵌套对象。
	nested, nestedOK := output["nested"].(map[string]any)
	if !nestedOK || nested["buyer"] != "buyer-1" {
		t.Fatalf("嵌套模板替换=%v", output["nested"])
	}
	// items 保存替换后的数组模板，验证数组元素顺序和标量类型不变。
	items, itemsOK := output["items"].([]any)
	if !itemsOK || len(items) != 3 || items[0] != "order-1" || items[1] != 7 || items[2] != nil {
		t.Fatalf("数组模板替换=%v", output["items"])
	}
	if input["order"] != "{order_id}" {
		t.Fatal("模板替换不应修改原始对象")
	}
}

// TestNormalizeAccountSettingsErrorCoversStableMappings 验证数据库所有权错误到应用层错误的稳定映射。
func TestNormalizeAccountSettingsErrorCoversStableMappings(t *testing.T) {
	// customErr 保存不属于所有权错误的底层错误，适配器应保持原错误链。
	customErr := errors.New("database unavailable")
	// cases 保存数据库错误和应用层预期错误的映射样例。
	cases := []struct {
		// input 是数据库仓储返回的原始错误。
		input error
		// want 是适配器应该返回的应用层错误。
		want error
	}{
		{input: nil, want: nil},
		{input: db.ErrNotFound, want: accountapp.ErrNotFound},
		{input: db.ErrForbidden, want: accountapp.ErrForbidden},
		{input: customErr, want: customErr},
	}
	// testCase 表示当前数据库错误映射样例。
	for _, testCase := range cases {
		// got 保存适配器转换后的应用层错误。
		got := normalizeAccountSettingsError(testCase.input)
		if !errors.Is(got, testCase.want) {
			t.Errorf("输入错误=%v got=%v want=%v", testCase.input, got, testCase.want)
		}
	}
}
