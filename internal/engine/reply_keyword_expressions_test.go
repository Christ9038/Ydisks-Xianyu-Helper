package engine

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestMatchKeywordExpressionsTable 覆盖多表达式 OR、历史模式、Unicode/英文大小写及 Go/RE2 语法。
func TestMatchKeywordExpressionsTable(t *testing.T) {
	// cases 是纯匹配函数的确定性输入与期望结果表。
	cases := []struct {
		// name 是子测试名称。
		name string
		// expressions 是同一条规则按 OR 语义执行的表达式集合。
		expressions []string
		// matchType 是规则使用的匹配模式。
		matchType string
		// text 是待匹配的聊天消息。
		text string
		// wantMatched 表示是否至少有一个表达式命中。
		wantMatched bool
		// wantInvalidIndexes 是被容错跳过的非法正则零基位置。
		wantInvalidIndexes []int
		// wantUnknown 表示匹配模式是否未知。
		wantUnknown bool
	}{
		{name: "contains OR", expressions: []string{"价格", "询价"}, matchType: "contains", text: "可以询价吗", wantMatched: true},
		{name: "unicode contains", expressions: []string{"你好"}, matchType: "contains", text: "老板你好", wantMatched: true},
		{name: "english case", expressions: []string{"hello"}, matchType: "contains", text: "Say HELLO", wantMatched: true},
		{name: "default contains", expressions: []string{"Welcome"}, matchType: "", text: "welcome home", wantMatched: true},
		{name: "fuzzy history", expressions: []string{"Welcome"}, matchType: "fuzzy", text: "WELCOME", wantMatched: true},
		{name: "exact history", expressions: []string{"Welcome"}, matchType: "exact", text: "welcome", wantMatched: true},
		{name: "regexp anchor", expressions: []string{`^订单$`}, matchType: "regexp", text: "订单", wantMatched: true},
		{name: "regexp OR", expressions: []string{`^(foo|bar)$`}, matchType: "regexp", text: "BAR", wantMatched: true},
		{name: "regexp quantifier", expressions: []string{`^a{2,3}$`}, matchType: "regexp", text: "AAA", wantMatched: true},
		{name: "regexp inline multiline", expressions: []string{`(?m)^foo$`}, matchType: "regexp", text: "first\nFOO\nlast", wantMatched: true},
		{name: "regexp inline disable case", expressions: []string{`(?-i:foo)`}, matchType: "regexp", text: "FOO", wantMatched: false},
		{name: "invalid regexp does not block later expression", expressions: []string{"[private", "可用"}, matchType: "regexp", text: "可用", wantMatched: true, wantInvalidIndexes: []int{0}},
		{name: "invalid regexp alone", expressions: []string{"[private"}, matchType: "regexp", text: "可用", wantMatched: false, wantInvalidIndexes: []int{0}},
		{name: "unknown mode", expressions: []string{"可用"}, matchType: "regex", text: "可用", wantMatched: false, wantUnknown: true},
	}
	// testCase 表示当前待执行的纯匹配场景。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// matched、invalidIndexes、unknownMode 保存纯匹配函数的实际结果。
			matched, invalidIndexes, unknownMode := matchKeywordExpressions(testCase.expressions, testCase.matchType, testCase.text)
			if matched != testCase.wantMatched || unknownMode != testCase.wantUnknown {
				t.Fatalf("matched=%v unknown=%v want matched=%v unknown=%v", matched, unknownMode, testCase.wantMatched, testCase.wantUnknown)
			}
			if len(invalidIndexes) != len(testCase.wantInvalidIndexes) {
				t.Fatalf("invalid indexes=%v want=%v", invalidIndexes, testCase.wantInvalidIndexes)
			}
			// index 表示非法正则位置的逐项比较下标。
			for index, invalidIndex := range testCase.wantInvalidIndexes {
				if invalidIndexes[index] != invalidIndex {
					t.Fatalf("invalid indexes=%v want=%v", invalidIndexes, testCase.wantInvalidIndexes)
				}
			}
		})
	}
}

// TestKeywordMatchesPrefersExpressionsAndFallsBackToLegacyKeyword 验证新表达式集合优先于旧单关键词字段，缺少集合时回退旧字段。
func TestKeywordMatchesPrefersExpressionsAndFallsBackToLegacyKeyword(t *testing.T) {
	// service 只使用纯匹配方法，不访问数据库或外部服务。
	service := &ReplyService{}
	// legacyKeyword 是只有历史单关键词字段的规则。
	legacyKeyword := db.Keyword{Keyword: "历史词"}
	if !service.keywordMatches(legacyKeyword, "消息包含历史词") {
		t.Fatal("缺少 Expressions 时应回退历史 Keyword")
	}
	// expressionKeyword 是同时带有旧字段和新表达式集合的规则。
	expressionKeyword := db.Keyword{Keyword: "历史词", Expressions: []string{"新词"}}
	if !service.keywordMatches(expressionKeyword, "消息包含新词") {
		t.Fatal("新表达式集合应按自身 OR 语义命中")
	}
	if service.keywordMatches(expressionKeyword, "消息只包含历史词") {
		t.Fatal("存在新表达式集合时不应回退旧 Keyword")
	}
}

// TestKeywordMatchesRedactsInvalidRegexpDiagnostics 验证坏正则运行时只记录账号、位置和稳定错误类别。
func TestKeywordMatchesRedactsInvalidRegexpDiagnostics(t *testing.T) {
	// logBuffer 收集本次坏正则匹配产生的诊断文本。
	var logBuffer bytes.Buffer
	// logger 使用文本处理器模拟生产日志并允许检查脱敏边界。
	logger := newTestLogger(&logBuffer).With("account", "account-diagnostic", "subsys", "reply")
	// service 注入账号上下文和测试日志端口。
	service := &ReplyService{cookieID: "account-diagnostic", logger: logger}
	// keyword 是包含敏感正文片段和非法语法的历史坏规则。
	keyword := db.Keyword{Expressions: []string{"[private-secret"}, MatchType: "regexp"}
	if service.keywordMatches(keyword, "无关消息") {
		t.Fatal("非法正则不应命中")
	}
	// output 保存脱敏后的诊断日志。
	output := logBuffer.String()
	if strings.Contains(output, "private-secret") || strings.Contains(output, "error parsing regexp") {
		t.Fatalf("日志泄露表达式或底层编译错误: %q", output)
	}
	if !strings.Contains(output, "account-diagnostic") || !strings.Contains(output, "expression_index=0") || !strings.Contains(output, "regexp_compile") {
		t.Fatalf("日志缺少稳定诊断字段: %q", output)
	}
}

// TestReplyKeywordExpressionsSQLiteResolve 验证 SQLite 真实 resolve 的多表达式、正则、优先级、账号商品隔离和降级链路。
func TestReplyKeywordExpressionsSQLiteResolve(t *testing.T) {
	// store、cleanup 提供本地 SQLite 真实仓储及其关闭责任。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 是本测试所有本地数据库操作共用的上下文。
	ctx := context.Background()

	// _, addErr 保存账号级多表达式规则的写入结果。
	_, addErr := store.Keywords.AddWithExpressions(ctx, "cid", []string{"价格", "询价"}, "商品价格回复", "", "text", "contains", "")
	if addErr != nil {
		t.Fatalf("写入多表达式规则: %v", addErr)
	}
	// _, regexpErr 保存正则锚点、OR 和量词规则的写入结果。
	_, regexpErr := store.Keywords.AddWithExpressions(ctx, "cid", []string{`^订单$`, `^(foo|bar)$`, `^a{2,3}$`}, "正则回复", "", "text", "regexp", "")
	if regexpErr != nil {
		t.Fatalf("写入正则规则: %v", regexpErr)
	}
	// _, inlineErr 保存 RE2 内联标志规则的写入结果。
	_, inlineErr := store.Keywords.AddWithExpressions(ctx, "cid", []string{`(?m)^foo$`}, "内联标志回复", "", "text", "regexp", "")
	if inlineErr != nil {
		t.Fatalf("写入内联标志规则: %v", inlineErr)
	}
	// _, itemErr 保存商品级规则的写入结果。
	_, itemErr := store.Keywords.AddWithExpressions(ctx, "cid", []string{"价格"}, "商品专属回复", "item-1", "text", "contains", "")
	if itemErr != nil {
		t.Fatalf("写入商品级规则: %v", itemErr)
	}
	// _, silentErr 保存空回复规则的写入结果。
	_, silentErr := store.Keywords.AddWithExpressions(ctx, "cid", []string{"静默"}, "", "", "text", "contains", "")
	if silentErr != nil {
		t.Fatalf("写入空回复规则: %v", silentErr)
	}
	// _, imageErr 保存图片规则的写入结果。
	_, imageErr := store.Keywords.AddWithExpressions(ctx, "cid", []string{"图片"}, "", "", "image", "contains", "https://example.invalid/image.png")
	if imageErr != nil {
		t.Fatalf("写入图片规则: %v", imageErr)
	}

	// reply 使用真实 SQLite 读取路径解析关键词，未装配 API/AI 以验证默认降级。
	reply := NewReplyService("cid", store, nil, nil, nil, nil)
	// containsResult 保存多表达式 OR 的真实解析结果。
	containsResult := reply.resolve(ctx, chatMsg("请问可以询价吗", "item-2", "chat-contains"))
	if containsResult == nil || containsResult.Source != "关键词" || containsResult.Text != "商品价格回复" {
		t.Fatalf("多表达式 OR 解析错误: %+v", containsResult)
	}
	// regexpCases 覆盖真实数据库读取后的正则锚点、OR、量词和内联标志消息。
	regexpCases := []struct {
		// name 是正则回归子测试名称。
		name string
		// text 是触发正则规则的消息文本。
		text string
		// want 是规则应生成的回复正文。
		want string
	}{
		{name: "anchor", text: "订单", want: "正则回复"},
		{name: "or", text: "BAR", want: "正则回复"},
		{name: "quantifier", text: "AAA", want: "正则回复"},
		{name: "inline flags", text: "第一行\nFOO\n最后", want: "内联标志回复"},
	}
	// testCase 表示当前真实 SQLite 正则场景。
	for _, testCase := range regexpCases {
		t.Run(testCase.name, func(t *testing.T) {
			// result 保存当前消息的真实解析结果。
			result := reply.resolve(ctx, chatMsg(testCase.text, "item-2", "chat-regexp-"+testCase.name))
			if result == nil || result.Source != "关键词" || result.Text != testCase.want {
				t.Fatalf("正则解析 text=%q result=%+v", testCase.text, result)
			}
		})
	}
	// silentResult 保存空回复规则的 Skip 结果。
	silentResult := reply.resolve(ctx, chatMsg("请静默", "item-2", "chat-silent"))
	if silentResult == nil || !silentResult.Skip || silentResult.Source != "关键词" {
		t.Fatalf("空回复应 Skip: %+v", silentResult)
	}
	// imageResult 保存图片规则的图片回复结果。
	imageResult := reply.resolve(ctx, chatMsg("请发图片", "item-2", "chat-image"))
	if imageResult == nil || imageResult.Source != "关键词" || imageResult.ImageURL != "https://example.invalid/image.png" || imageResult.Text != "" {
		t.Fatalf("图片关键词语义错误: %+v", imageResult)
	}
	// itemResult 保存商品级规则结果，验证商品级优先于账号级规则。
	itemResult := reply.resolve(ctx, chatMsg("商品价格", "item-1", "chat-item"))
	if itemResult == nil || itemResult.Text != "商品专属回复" {
		t.Fatalf("商品级规则未优先: %+v", itemResult)
	}

	// apiReply 提供最高优先级 API 回复，验证它覆盖已命中的关键词。
	apiReply := &fakeAPIReplier{result: &ReplyResult{Text: "API优先"}}
	// apiService 使用真实 SQLite 规则和 API 替身验证完整优先级链第一段。
	apiService := NewReplyService("cid", store, nil, apiReply, nil, nil)
	// apiResult 保存 API 优先级解析结果。
	apiResult := apiService.resolve(ctx, chatMsg("价格", "item-2", "chat-api"))
	if apiResult == nil || apiResult.Source != "API" || apiResult.Text != "API优先" {
		t.Fatalf("API 未优先于关键词: %+v", apiResult)
	}

	// aiReply 提供无关键词命中时的 AI 回复。
	aiReply := &fakeAIReplier{result: &ReplyResult{Text: "AI兜底"}}
	// aiService 使用真实 SQLite 规则验证关键词未命中后进入 AI。
	aiService := NewReplyService("cid", store, nil, nil, aiReply, nil)
	// aiResult 保存 AI 降级结果。
	aiResult := aiService.resolve(ctx, chatMsg("没有任何规则命中", "item-2", "chat-ai"))
	if aiResult == nil || aiResult.Source != "AI" || aiResult.Text != "AI兜底" {
		t.Fatalf("无关键词命中未进入 AI: %+v", aiResult)
	}

	// defaultSetupErr 保存默认回复的真实 SQLite 配置结果。
	defaultSetupErr := store.DefaultReps.Upsert(ctx, "cid", db.DefaultReply{Enabled: true, ReplyContent: "默认{send_user_name}:{send_message}"})
	if defaultSetupErr != nil {
		t.Fatalf("写入默认回复: %v", defaultSetupErr)
	}
	// defaultService 验证无关键词且无 AI 时的默认模板替换。
	defaultService := NewReplyService("cid", store, nil, nil, nil, nil)
	// defaultResult 保存默认回复解析结果。
	defaultResult := defaultService.resolve(ctx, chatMsg("未命中", "item-2", "chat-default"))
	if defaultResult == nil || defaultResult.Source != "默认" || defaultResult.Text != "默认买家:未命中" {
		t.Fatalf("无关键词命中未进入默认回复: %+v", defaultResult)
	}

	// admin、adminErr 读取测试账号，用于创建第二个隔离账号。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatalf("读取测试管理员: %v", adminErr)
	}
	// otherCookieErr 保存第二个账号的本地凭证创建结果。
	otherCookieErr := store.Cookies.Save(ctx, "other-cid", "unb=456; _m_h5_tk=other;", admin.ID)
	if otherCookieErr != nil {
		t.Fatalf("创建第二账号: %v", otherCookieErr)
	}
	// _, otherKeywordErr 保存第二账号的同文本规则写入结果。
	_, otherKeywordErr := store.Keywords.AddWithExpressions(ctx, "other-cid", []string{"账号隔离"}, "其他账号回复", "", "text", "contains", "")
	if otherKeywordErr != nil {
		t.Fatalf("写入第二账号规则: %v", otherKeywordErr)
	}
	// isolatedResult 使用当前账号解析第二账号规则文本，必须保持无命中。
	isolatedResult := reply.resolve(ctx, chatMsg("账号隔离", "item-2", "chat-isolation"))
	if isolatedResult == nil || isolatedResult.Source != "默认" || isolatedResult.Text != "默认买家:账号隔离" {
		t.Fatalf("关键词不应串账号: %+v", isolatedResult)
	}
	// otherReply 使用第二账号服务确认其自身规则仍可命中。
	otherReply := NewReplyService("other-cid", store, nil, nil, nil, nil)
	// otherResult 保存第二账号真实命中结果。
	otherResult := otherReply.resolve(ctx, chatMsg("账号隔离", "item-2", "chat-other"))
	if otherResult == nil || otherResult.Text != "其他账号回复" {
		t.Fatalf("第二账号规则未命中: %+v", otherResult)
	}
}

// TestReplyKeywordExpressionsSQLiteLegacyRows 验证没有新字段的历史文本和图片规则仍按旧语义解析。
func TestReplyKeywordExpressionsSQLiteLegacyRows(t *testing.T) {
	// store、cleanup 提供历史数据回归所需的本地 SQLite 仓储。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 是历史规则写入和解析共用的上下文。
	ctx := context.Background()
	// _, textErr 写入缺少 keyword_expressions 的历史文本规则。
	_, textErr := store.DB.ExecContext(ctx,
		`INSERT INTO keywords (cookie_id,keyword,reply,item_id,type,image_url) VALUES (?,?,?,?,?,?)`,
		"cid", "旧文本", "旧{send_user_name}:{send_message}", "", "text", "")
	if textErr != nil {
		t.Fatalf("写入历史文本规则: %v", textErr)
	}
	// _, imageErr 写入缺少 keyword_expressions 的历史图片规则。
	_, imageErr := store.DB.ExecContext(ctx,
		`INSERT INTO keywords (cookie_id,keyword,reply,item_id,type,image_url) VALUES (?,?,?,?,?,?)`,
		"cid", "旧图片", "", "", "image", "https://example.invalid/legacy.png")
	if imageErr != nil {
		t.Fatalf("写入历史图片规则: %v", imageErr)
	}
	// reply 使用真实 SQLite 读取旧字段并执行回复模板和图片语义。
	reply := NewReplyService("cid", store, nil, nil, nil, nil)
	// textResult 保存历史文本规则解析结果。
	textResult := reply.resolve(ctx, chatMsg("请问旧文本", "item-legacy", "chat-legacy-text"))
	if textResult == nil || textResult.Source != "关键词" || textResult.Text != "旧买家:请问旧文本" {
		t.Fatalf("历史文本规则解析错误: %+v", textResult)
	}
	// imageResult 保存历史图片规则解析结果。
	imageResult := reply.resolve(ctx, chatMsg("请看旧图片", "item-legacy", "chat-legacy-image"))
	if imageResult == nil || imageResult.Source != "关键词" || imageResult.ImageURL != "https://example.invalid/legacy.png" || imageResult.Text != "" {
		t.Fatalf("历史图片规则解析错误: %+v", imageResult)
	}
}

// newTestLogger 创建不会向测试标准输出写入的结构化日志端口；buffer 接收结构化日志，返回值供运行时注入。
func newTestLogger(buffer *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buffer, nil))
}
