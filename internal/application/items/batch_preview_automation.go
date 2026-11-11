package items

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// sheetTemplateVariablePrefixes 是表格中声明发货模板自定义变量列的表头前缀，列名其余部分即 {{custom.<key>}} 的 key。
var sheetTemplateVariablePrefixes = []string{"变量.", "模板变量.", "custom."}

// sheetTemplateBindingPrefix 是表格中声明发货模板卡密变量绑定列的表头前缀，列名其余部分即 {{cards.<key>}} 的 key。
const sheetTemplateBindingPrefix = "模板卡密."

// sheetTemplateKeyPattern 与发货模板变量令牌允许的键字符保持一致，避免表格列名永远无法匹配模板变量。
var sheetTemplateKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// sheetTemplateInputs 保存一行表格为发货模板提供的全部输入；同一行的多个模板动作共享这些值，
// 预检时再按每个模板实际引用的键裁剪，保证写入规则的变量与绑定恰好覆盖模板需要。
type sheetTemplateInputs struct {
	// Variables 是自定义变量键到该行取值的映射，取值可能是网盘链接等不得写入日志或错误提示的业务内容。
	Variables map[string]string
	// Bindings 是卡密变量键到卡密组的绑定，按键名排序以保证预检结果稳定。
	Bindings []BatchPreviewTemplateBinding
	// ParseError 是变量列或绑定列的格式错误；只在该行确实使用模板动作时才报告。
	ParseError string
}

// parseAutomation 解析表格中的自动化配置文本，并收集该行供发货模板使用的变量列与绑定列。
func parseAutomation(fields map[string]any) BatchPreviewAutomation {
	// inputs 是该行全部模板输入，供付款发货与评价赠品两处模板动作共享。
	inputs := parseSheetTemplateInputs(fields)
	// paidActions 和 paidError 表示付款发货动作及解析错误。
	paidActions, paidError := parseCardActions(firstString(fields, "paid_delivery_contents", "付款发货内容"))
	// reviewActions 和 reviewError 表示评价赠品动作及解析错误。
	reviewActions, reviewError := parseCardActions(firstString(fields, "review_gift_contents", "评价赠品内容"))
	return BatchPreviewAutomation{
		PaidDelivery:        BatchPreviewCardAutomation{Enabled: parseBool(firstString(fields, "paid_delivery_enabled", "付款发货启用")), Actions: paidActions, ParseError: withTemplateInputError(paidError, paidActions, inputs)},
		ReviewGift:          BatchPreviewCardAutomation{Enabled: parseBool(firstString(fields, "review_gift_enabled", "评价赠品启用")), Actions: reviewActions, ParseError: withTemplateInputError(reviewError, reviewActions, inputs)},
		ReviewRequest:       BatchPreviewReviewRequest{Enabled: parseBool(firstString(fields, "review_request_enabled", "求评价启用")), AfterShippedHours: parseIntDefault(firstString(fields, "review_request_after_hours", "求评价等待小时"), 72), Message: firstString(fields, "review_request_message", "求评价文案"), MaxAttempts: parseIntDefault(firstString(fields, "review_request_max_attempts", "求评价最多次数"), 1), DelaySeconds: parseIntDefault(firstString(fields, "review_request_delay_seconds", "求评价延迟秒"), 0)},
		sheetTemplateInputs: inputs,
	}
}

// withTemplateInputError 在动作文本本身无误且包含模板动作时，返回模板输入列的格式错误；
// parseError 是动作文本解析错误，actions 是解析出的动作，inputs 是该行模板输入。返回值为空表示无错误。
func withTemplateInputError(parseError string, actions []BatchPreviewCardAction, inputs *sheetTemplateInputs) string {
	if parseError != "" || inputs == nil || inputs.ParseError == "" {
		return parseError
	}
	// action 是当前检查是否引用发货模板的动作。
	for _, action := range actions {
		if action.ActionType == BatchActionSendTemplate {
			return inputs.ParseError
		}
	}
	return ""
}

// parseCardActions 解析由卡密动作「卡密组ID:每件份数:延迟秒」和模板动作「T模板ID:延迟秒」混写的动作文本。
// 返回按书写顺序排列的动作和首个格式错误；旧版只含卡密动作的文本解析结果保持不变。
func parseCardActions(raw string) ([]BatchPreviewCardAction, string) {
	// entries 保存按分隔符拆出的动作文本。
	entries := strings.FieldsFunc(strings.TrimSpace(raw), func(char rune) bool { return char == ';' || char == '；' || char == '\n' || char == '\r' })
	if len(entries) == 0 {
		return nil, ""
	}
	// actions 保存解析后的动作。
	actions := make([]BatchPreviewCardAction, 0, len(entries))
	// index 和 entry 表示动作序号及原始动作文本。
	for index, entry := range entries {
		// normalized 是统一全角冒号并去除首尾空白后的动作文本。
		normalized := strings.ReplaceAll(strings.TrimSpace(entry), "：", ":")
		// templateAction、isTemplate、templateErr 保存模板动作解析结果；isTemplate 为假时按卡密动作解析。
		if templateAction, isTemplate, templateErr := parseTemplateActionEntry(index, normalized); isTemplate {
			if templateErr != "" {
				return nil, templateErr
			}
			actions = append(actions, templateAction)
			continue
		}
		// parts 保存动作的 ID、数量和延迟字段。
		parts := strings.Split(normalized, ":")
		if len(parts) < 1 || len(parts) > 3 || strings.TrimSpace(parts[0]) == "" {
			return nil, fmt.Sprintf("第%d项格式错误，应为 卡密组ID:每件份数:延迟秒 或 T模板ID:延迟秒", index+1)
		}
		// cardID 和 err 表示卡券组标识及解析错误。
		cardID, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
		if err != nil || cardID <= 0 {
			return nil, fmt.Sprintf("第%d项卡密组ID无效", index+1)
		}
		// count 和 delay 保存默认的每件数量及延迟秒数。
		count, delay := 1, 0
		if len(parts) >= 2 && strings.TrimSpace(parts[1]) != "" {
			count, err = strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil || count <= 0 {
				return nil, fmt.Sprintf("第%d项每件份数必须大于0", index+1)
			}
		}
		if len(parts) == 3 && strings.TrimSpace(parts[2]) != "" {
			delay, err = strconv.Atoi(strings.TrimSpace(parts[2]))
			if err != nil || delay < 0 {
				return nil, fmt.Sprintf("第%d项延迟秒不能小于0", index+1)
			}
		}
		actions = append(actions, BatchPreviewCardAction{CardID: cardID, DeliveryCount: count, DelaySeconds: delay})
	}
	return actions, ""
}

// parseTemplateActionEntry 解析以 T 或「模板」开头的发货模板动作文本，例如 T12 或 模板12:30。
// index 是动作序号（从 0 开始），entry 是已统一冒号的动作文本。返回模板动作、是否为模板动作以及格式错误；
// 卡密组 ID 只能是数字，因此以 T/模板 开头的文本不会与旧卡密动作冲突。
func parseTemplateActionEntry(index int, entry string) (BatchPreviewCardAction, bool, string) {
	// body 是去掉模板前缀后的「模板ID:延迟秒」文本。
	var body string
	switch {
	case strings.HasPrefix(entry, "模板"):
		body = strings.TrimPrefix(entry, "模板")
	case strings.HasPrefix(entry, "T"), strings.HasPrefix(entry, "t"):
		body = entry[1:]
	default:
		return BatchPreviewCardAction{}, false, ""
	}
	// parts 保存模板 ID 和可选延迟秒。
	parts := strings.Split(body, ":")
	if len(parts) > 2 {
		return BatchPreviewCardAction{}, true, fmt.Sprintf("第%d项模板动作格式错误，应为 T模板ID:延迟秒", index+1)
	}
	// templateID、err 保存发货模板主键及解析错误。
	templateID, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil || templateID <= 0 {
		return BatchPreviewCardAction{}, true, fmt.Sprintf("第%d项发货模板ID无效", index+1)
	}
	// delay 是模板消息发送前的延迟秒数，留空为 0。
	delay := 0
	if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
		delay, err = strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || delay < 0 {
			return BatchPreviewCardAction{}, true, fmt.Sprintf("第%d项延迟秒不能小于0", index+1)
		}
	}
	return BatchPreviewCardAction{ActionType: BatchActionSendTemplate, DeliveryTemplateID: templateID, DeliveryCount: 1, DelaySeconds: delay}, true, ""
}

// parseSheetTemplateInputs 从原始行字段收集「变量.xxx」与「模板卡密.xxx」列；没有任何此类列时返回 nil，
// 使旧表格的预检结果与改动前完全一致。fields 是归一化表头到单元格值的映射。
func parseSheetTemplateInputs(fields map[string]any) *sheetTemplateInputs {
	// headers 是按字典序排列的表头，保证报错和绑定顺序与 map 遍历顺序无关。
	headers := make([]string, 0, len(fields))
	// header 是当前收集的表头名。
	for header := range fields {
		headers = append(headers, header)
	}
	sort.Strings(headers)
	// inputs 是待返回的模板输入；found 表示是否出现过任一模板输入列。
	inputs, found := &sheetTemplateInputs{Variables: map[string]string{}}, false
	// header 是当前检查的表头名。
	for _, header := range headers {
		// value 是去除首尾空白的单元格文本。
		value := strings.TrimSpace(stringValue(fields[header]))
		// key、isVariable 是变量列的键及是否为变量列。
		if key, isVariable := templateVariableKey(header); isVariable {
			found = true
			inputs.addVariable(header, key, value)
			continue
		}
		if strings.HasPrefix(header, sheetTemplateBindingPrefix) {
			found = true
			inputs.addBinding(header, strings.TrimPrefix(header, sheetTemplateBindingPrefix), value)
		}
	}
	if !found {
		return nil
	}
	return inputs
}

// templateVariableKey 判断表头是否为模板变量列并返回变量键；custom. 前缀不区分大小写，中文前缀需完全匹配。
func templateVariableKey(header string) (string, bool) {
	// prefix 是当前尝试匹配的变量列前缀。
	for _, prefix := range sheetTemplateVariablePrefixes {
		if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
			return header[len(prefix):], true
		}
	}
	return "", false
}

// addVariable 记录一个自定义变量列；header 是原始列名（仅用于提示），key 是变量键，value 是单元格值。
// 空值视为未提供；多个别名列给同一键不同的非空值时记录首个冲突错误，错误中不包含取值本身。
func (inputs *sheetTemplateInputs) addVariable(header, key, value string) {
	if !sheetTemplateKeyPattern.MatchString(key) {
		inputs.setError(fmt.Sprintf("模板变量列「%s」名称只能包含字母、数字、下划线或短横线", header))
		return
	}
	if value == "" {
		return
	}
	// existing、exists 是该键已记录的取值及是否存在。
	if existing, exists := inputs.Variables[key]; exists && existing != value {
		inputs.setError(fmt.Sprintf("模板变量 %s 在多列中取值不一致", key))
		return
	}
	inputs.Variables[key] = value
}

// addBinding 记录一个模板卡密绑定列；header 是原始列名，key 是卡密变量键，value 是「卡密组ID:每件份数」文本。
func (inputs *sheetTemplateInputs) addBinding(header, key, value string) {
	if !sheetTemplateKeyPattern.MatchString(key) {
		inputs.setError(fmt.Sprintf("模板卡密列「%s」名称只能包含字母、数字、下划线或短横线", header))
		return
	}
	if value == "" {
		return
	}
	// parts 保存卡密组 ID 和可选每件份数。
	parts := strings.Split(strings.ReplaceAll(value, "：", ":"), ":")
	// cardID、cardErr 保存卡密组主键及解析错误。
	cardID, cardErr := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	// count 是每件商品准备的卡密份数，留空为 1。
	count := 1
	// countErr 保存每件份数的解析错误。
	var countErr error
	if len(parts) == 2 && strings.TrimSpace(parts[1]) != "" {
		count, countErr = strconv.Atoi(strings.TrimSpace(parts[1]))
	}
	if len(parts) > 2 || cardErr != nil || cardID <= 0 || countErr != nil || count <= 0 {
		inputs.setError(fmt.Sprintf("模板卡密 %s 格式应为 卡密组ID:每件份数", key))
		return
	}
	inputs.Bindings = append(inputs.Bindings, BatchPreviewTemplateBinding{VariableKey: key, CardID: cardID, DeliveryCount: count})
}

// setError 只保留首个模板输入错误，避免一行出现大量重复提示。
func (inputs *sheetTemplateInputs) setError(message string) {
	if inputs.ParseError == "" {
		inputs.ParseError = message
	}
}

// resolveSheetTemplateActions 把表格行的模板输入按每个模板实际引用的键分发给模板动作：
// 只保留模板 {{custom.*}} 用到的变量和 {{cards.*}} 用到的绑定，缺失项留给 validateTemplateAction 报错。
// 非表格来源（如资源库模板铺货）的行没有表格输入，原样返回。该方法只读取数据库摘要，不访问平台接口。
func (service *BatchPreviewService) resolveSheetTemplateActions(ctx context.Context, userID int64, automation BatchPreviewAutomation) BatchPreviewAutomation {
	// inputs 是当前行的表格模板输入。
	inputs := automation.sheetTemplateInputs
	if inputs == nil {
		return automation
	}
	// port、ok 保存归属实现是否支持模板核对；不支持时由校验阶段报告模板不可用。
	port, ok := service.ownership.(BatchPreviewTemplatePort)
	if !ok {
		return automation
	}
	automation.PaidDelivery.Actions = resolveTemplateActionInputs(ctx, port, userID, automation.PaidDelivery.Actions, inputs)
	automation.ReviewGift.Actions = resolveTemplateActionInputs(ctx, port, userID, automation.ReviewGift.Actions, inputs)
	return automation
}

// resolveTemplateActionInputs 为一组动作中的模板动作填入裁剪后的变量与绑定，返回新切片而不修改调用方数据。
func resolveTemplateActionInputs(ctx context.Context, port BatchPreviewTemplatePort, userID int64, actions []BatchPreviewCardAction, inputs *sheetTemplateInputs) []BatchPreviewCardAction {
	if len(actions) == 0 {
		return actions
	}
	// resolved 保存填充后的动作副本。
	resolved := make([]BatchPreviewCardAction, len(actions))
	copy(resolved, actions)
	// index 是当前动作下标。
	for index := range resolved {
		if resolved[index].ActionType != BatchActionSendTemplate {
			continue
		}
		// requirements、err 保存模板需要的变量键；模板不可用时保持空输入，由校验报告模板错误。
		requirements, err := port.DeliveryTemplateRequirements(ctx, userID, resolved[index].DeliveryTemplateID)
		if err != nil || !requirements.Usable {
			continue
		}
		resolved[index].CustomVariables = pickTemplateVariables(inputs.Variables, requirements.CustomKeys)
		resolved[index].TemplateBindings = pickTemplateBindings(inputs.Bindings, requirements.CardKeys)
	}
	return resolved
}

// pickTemplateVariables 按模板自定义变量键挑出该行已填写的取值；没有任何匹配时返回 nil。
func pickTemplateVariables(values map[string]string, keys []string) map[string]string {
	// picked 保存模板用到且已填写的变量。
	var picked map[string]string
	// key 是模板引用的自定义变量键。
	for _, key := range keys {
		// value、exists 是该行对应列的取值及是否填写。
		if value, exists := values[key]; exists {
			if picked == nil {
				picked = make(map[string]string, len(keys))
			}
			picked[key] = value
		}
	}
	return picked
}

// pickTemplateBindings 按模板卡密变量键的顺序挑出该行提供的绑定；没有任何匹配时返回 nil。
func pickTemplateBindings(bindings []BatchPreviewTemplateBinding, keys []string) []BatchPreviewTemplateBinding {
	// picked 保存模板用到的绑定。
	var picked []BatchPreviewTemplateBinding
	// key 是模板引用的卡密变量键。
	for _, key := range keys {
		// binding 是该行提供的候选绑定。
		for _, binding := range bindings {
			if binding.VariableKey == key {
				picked = append(picked, binding)
				break
			}
		}
	}
	return picked
}
