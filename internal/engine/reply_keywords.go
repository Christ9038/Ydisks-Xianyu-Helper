// reply_keywords.go 关键词规则的表达式匹配与结果构造；从 reply.go 拆出以保持单文件职责与行数上限。

package engine

import (
	"log/slog"
	"strings"

	"xianyu-go/internal/db"
	"xianyu-go/internal/keywordmatch"
)

// keywordMatches 判断一条规则的多个表达式是否命中消息文本。
// contains 模式保持历史大小写不敏感的字面包含语义；regexp 模式使用 Go/RE2，
// 单个遗留非法表达式只记录并跳过，不影响同一规则的其他表达式或后续降级。
func (r *ReplyService) keywordMatches(kw db.Keyword, text string) bool {
	// expressions 保存兼容数据库行转换出的匹配表达式集合。
	expressions := kw.Expressions
	if len(expressions) == 0 && strings.TrimSpace(kw.Keyword) != "" {
		expressions = []string{kw.Keyword}
	}
	// matched、invalidIndexes、unknownMode 保存纯匹配结果和需要对外记录的稳定诊断。
	matched, invalidIndexes, unknownMode := matchKeywordExpressions(expressions, kw.MatchType, text)
	// logger 保存带账号上下文的诊断日志端口；正常构造路径总会提供该端口。
	logger := r.logger
	if logger == nil {
		logger = slog.Default().With("account", r.cookieID, "subsys", "reply")
	}
	// expressionIndex 表示发生编译失败的表达式零基位置。
	for _, expressionIndex := range invalidIndexes {
		// errorType 只记录稳定的诊断类别，避免把表达式正文和底层编译文本写入日志。
		errorType := "regexp_compile"
		logger.Warn("关键词正则编译失败，已跳过", "expression_index", expressionIndex, "error_type", errorType)
	}
	if unknownMode {
		// errorType 只标识未知匹配模式，不记录调用方传入的原始模式文本。
		errorType := "unknown_match_type"
		logger.Warn("关键词匹配模式未知，已跳过", "error_type", errorType)
	}
	return matched
}

// matchKeywordExpressions 对表达式集合执行无副作用的 OR 匹配。
// expressions 是规则表达式，matchType 是规范或历史匹配模式，text 是待匹配消息；返回命中结果、非法正则的零基位置和未知模式标记。
func matchKeywordExpressions(expressions []string, matchType, text string) (bool, []int, bool) {
	// normalizedMatchType 保存兼容历史模式后的匹配模式。
	normalizedMatchType := strings.ToLower(strings.TrimSpace(matchType))
	if normalizedMatchType == "" || normalizedMatchType == "fuzzy" || normalizedMatchType == "exact" {
		normalizedMatchType = "contains"
	}
	if normalizedMatchType != "contains" && normalizedMatchType != "regexp" {
		return false, nil, true
	}
	// invalidIndexes 保存编译失败但不阻断同规则其他表达式的零基位置。
	invalidIndexes := make([]int, 0)
	// expressionIndex、rawExpression 表示当前遍历的表达式位置和原始文本。
	for expressionIndex, rawExpression := range expressions {
		// expression 是去除边界空白后的匹配表达式。
		expression := strings.TrimSpace(rawExpression)
		if expression == "" {
			continue
		}
		if normalizedMatchType == "regexp" {
			// compiled、compileErr 保存与保存时校验共用的默认忽略大小写正则及编译错误，已编译结果带缓存。
			compiled, compileErr := keywordmatch.CompileExpression(expression)
			if compileErr != nil {
				invalidIndexes = append(invalidIndexes, expressionIndex)
				continue
			}
			if compiled.MatchString(text) {
				return true, invalidIndexes, false
			}
			continue
		}
		if keywordMatches(strings.ToLower(text), expression) {
			return true, invalidIndexes, false
		}
	}
	return false, invalidIndexes, false
}

// keywordMatches 判断已小写化的买家消息 messageLower 是否包含 contains 模式表达式 rawKeyword 中的任一候选词。
// 表达式可使用 |、中英文逗号、顿号或换行分隔候选词；空片段会被忽略，以保持历史单关键词规则不变。
// regexp 模式不经过此函数：其中的 | 是正则分支，不做拆分。
func keywordMatches(messageLower, rawKeyword string) bool {
	// term 表示由规则分隔符拆出的单个候选关键词。
	for _, term := range strings.FieldsFunc(rawKeyword, isKeywordTermSeparator) {
		// normalizedTerm 去除候选词首尾空白后参与不区分大小写的包含匹配。
		normalizedTerm := strings.TrimSpace(term)
		if normalizedTerm != "" && strings.Contains(messageLower, strings.ToLower(normalizedTerm)) {
			return true
		}
	}
	return false
}

// isKeywordTermSeparator 判断字符 separator 是否是关键词规则中表达 OR 语义的分隔符。
func isKeywordTermSeparator(separator rune) bool {
	return separator == '|' || separator == ',' || separator == '，' || separator == '、' || separator == '\n' || separator == '\r'
}

// containsItemID 判断规则的商品范围字段是否覆盖目标商品标识。
// 字段为逗号分隔的商品集合时命中任一元素即视为覆盖；空字段不会匹配任何商品。
// 引擎直接读取持久化字段，故分隔符必须与关键词应用服务的存储约定保持一致。
func containsItemID(rawItemIDs string, target string) bool {
	// itemID 表示规则商品范围中的单个商品标识。
	for _, itemID := range strings.Split(rawItemIDs, ",") {
		if strings.TrimSpace(itemID) == target {
			return true
		}
	}
	return false
}

// keywordResult 封装关键词结果业务协调。
func (r *ReplyService) keywordResult(kw db.Keyword, m ChatMessage) *ReplyResult {
	if kw.Type == "image" && (kw.ImageURL != "" || kw.LibraryImageID > 0) {
		return &ReplyResult{ImageURL: kw.ImageURL, LibraryImageID: kw.LibraryImageID, Source: "关键词", NotifyManual: kw.NotifyManual}
	}
	if strings.TrimSpace(kw.Reply) == "" {
		return &ReplyResult{Skip: true, Source: "关键词", NotifyManual: kw.NotifyManual} // EMPTY_REPLY
	}
	return &ReplyResult{Text: formatReply(kw.Reply, m), Source: "关键词", NotifyManual: kw.NotifyManual}
}
