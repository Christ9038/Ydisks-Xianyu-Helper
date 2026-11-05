package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// wikiDir 是中英文手册源文件相对仓库根目录的位置。
const wikiDir = "docs/wiki"

// readWiki 读取 docs/wiki 下的一份文档；root 是仓库根目录，name 是文档文件名。
// 返回文档全文；文档缺失视为错误，因为规则表引用了不存在的页面。
func readWiki(root, name string) (string, error) {
	// raw 是文档原始字节，err 是读取失败原因。
	raw, err := os.ReadFile(filepath.Join(root, wikiDir, name))
	if err != nil {
		return "", fmt.Errorf("读取文档 %s 失败: %w", name, err)
	}
	return string(raw), nil
}

// extractTokens 按规则从源码文本提取全部令牌并去重排序，text 是源码全文。
// scope 非空时先把文本限定到其第一次匹配；split 非空时把令牌再拆分。
func extractTokens(rule tokenRule, text string) []string {
	if rule.scope != nil {
		// scoped 是范围正则的第一次匹配；没有匹配时按空文本处理，让调用方报“未提取到令牌”。
		scoped := rule.scope.FindString(text)
		text = scoped
	}
	// seen 用于令牌去重，保证同一令牌只校验一次。
	seen := make(map[string]bool)
	// tokens 保存去重后的令牌。
	var tokens []string
	for _, match := range rule.pattern.FindAllStringSubmatch(text, -1) { // match 是一次正则匹配及其全部捕获组。
		for _, group := range match[1:] { // group 是该匹配的一个捕获组文本。
			if group == "" {
				continue
			}
			// parts 是按分隔符拆分后的令牌；没有分隔符时就是整个捕获组。
			parts := []string{group}
			if rule.split != "" {
				parts = strings.Split(group, rule.split)
			}
			for _, part := range parts { // part 是一个候选令牌。
				if part != "" && !seen[part] {
					seen[part] = true
					tokens = append(tokens, part)
				}
			}
		}
	}
	sort.Strings(tokens)
	return tokens
}

// expectedText 返回令牌在文档中应出现的文本：有格式串时套用格式，否则原样。
func expectedText(rule tokenRule, token string) string {
	if rule.format == "" {
		return token
	}
	return fmt.Sprintf(rule.format, token)
}

// isPrefixOnly 判断令牌是否只要求文档出现其前缀（POSTGRES_ 这类合并书写的变量）。
func isPrefixOnly(rule tokenRule, token string) bool {
	for _, name := range rule.prefixOnly { // name 是只要求前缀的令牌名。
		if name == token {
			return true
		}
	}
	return false
}

// checkTokenRule 执行一条令牌规则，返回全部违规；root 是仓库根目录。
func checkTokenRule(root string, rule tokenRule) ([]string, error) {
	// tokens 汇总全部源码文件提取到的令牌，键为令牌、值为来源文件。
	tokens := make(map[string]string)
	for _, pattern := range rule.sources { // pattern 是源码文件路径或 glob。
		// files、err 分别是 glob 展开的文件列表和展开失败原因。
		files, err := filepath.Glob(filepath.Join(root, pattern))
		if err != nil {
			return nil, fmt.Errorf("展开 %s 失败: %w", pattern, err)
		}
		for _, file := range files { // file 是一个实际源码文件。
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			// raw、readErr 分别是源码字节和读取失败原因。
			raw, readErr := os.ReadFile(file)
			if readErr != nil {
				return nil, fmt.Errorf("读取源码 %s 失败: %w", file, readErr)
			}
			// relative 是相对仓库根目录的来源路径，用于违规信息。
			relative, relErr := filepath.Rel(root, file)
			if relErr != nil {
				relative = file
			}
			for _, token := range extractTokens(rule, string(raw)) { // token 是源码里提取到的一个令牌。
				if _, ok := tokens[token]; !ok {
					tokens[token] = relative
				}
			}
		}
	}
	if len(tokens) == 0 {
		return []string{fmt.Sprintf("[%s] 未从源码提取到任何令牌，规则的正则或来源路径已失效", rule.name)}, nil
	}
	// names 是排序后的令牌名，保证输出顺序稳定。
	names := make([]string, 0, len(tokens))
	for token := range tokens { // token 是待排序的令牌名。
		names = append(names, token)
	}
	sort.Strings(names)
	// violations 保存全部缺失令牌。
	var violations []string
	for _, doc := range rule.docs { // doc 是必须包含令牌的文档文件名。
		// text 是文档全文，readErr 是读取失败原因。
		text, readErr := readWiki(root, doc)
		if readErr != nil {
			return nil, readErr
		}
		for _, token := range names { // token 是待核对的令牌。
			if rule.exempt[token] {
				continue
			}
			// want 是该令牌在文档中应出现的文本。
			want := expectedText(rule, token)
			if isPrefixOnly(rule, token) {
				want = token[:strings.Index(token, "_")+1]
			}
			if !strings.Contains(text, want) {
				violations = append(violations, fmt.Sprintf("[%s] %s 缺少 %q（来源 %s）", rule.name, doc, want, tokens[token]))
			}
		}
	}
	return violations, nil
}

// numberAtWordBoundary 构造“数值作为独立数字出现”的正则，避免 20 误匹配 200 或 0.20。
func numberAtWordBoundary(number string) *regexp.Regexp {
	return regexp.MustCompile(`(^|[^0-9.])` + regexp.QuoteMeta(number) + `([^0-9]|$)`)
}

// thousandsSeparator 匹配数字之间的千分位逗号。
var thousandsSeparator = regexp.MustCompile(`(\d),(\d{3})`)

// lineHasNumber 判断文档是否存在同时包含关键词（忽略大小写）和该独立数值的一行。
func lineHasNumber(text, keyword, number string) bool {
	// boundary 是该数值的独立数字匹配器。
	boundary := numberAtWordBoundary(number)
	// lowerKeyword 是小写化的关键词，用于忽略大小写比较。
	lowerKeyword := strings.ToLower(keyword)
	for _, line := range strings.Split(text, "\n") { // line 是文档的一行。
		// plain 是去掉千分位逗号（12,000 → 12000）后的行，使英文写法也能与代码常量比较。
		plain := thousandsSeparator.ReplaceAllString(line, "$1$2")
		if strings.Contains(strings.ToLower(plain), lowerKeyword) && boundary.MatchString(plain) {
			return true
		}
	}
	return false
}

// checkNumberRule 执行一条数值规则，返回全部违规；root 是仓库根目录。
func checkNumberRule(root string, rule numberRule) ([]string, error) {
	// raw、err 分别是源码字节和读取失败原因。
	raw, err := os.ReadFile(filepath.Join(root, rule.source))
	if err != nil {
		return nil, fmt.Errorf("读取源码 %s 失败: %w", rule.source, err)
	}
	// match 是数值正则的第一次匹配；没有匹配说明源码常量已被改名或移动。
	match := rule.pattern.FindStringSubmatch(string(raw))
	if match == nil {
		return []string{fmt.Sprintf("[%s] 未能从 %s 提取数值，规则的正则已失效", rule.name, rule.source)}, nil
	}
	// violations 保存全部缺失数值。
	var violations []string
	for _, doc := range rule.docs { // doc 是需要出现数值的文档及关键词。
		// text、readErr 分别是文档全文和读取失败原因。
		text, readErr := readWiki(root, doc.file)
		if readErr != nil {
			return nil, readErr
		}
		// checked 记录已核对的数值，避免同一数值重复报告。
		checked := make(map[string]bool)
		for _, number := range match[1:] { // number 是代码里提取到的一个数值文本。
			if checked[number] {
				continue
			}
			checked[number] = true
			if !lineHasNumber(text, doc.keyword, number) {
				violations = append(violations, fmt.Sprintf("[%s] %s 没有同时含“%s”和数值 %s 的行（来源 %s）", rule.name, doc.file, doc.keyword, number, rule.source))
			}
		}
	}
	return violations, nil
}

// checkForbidden 执行回归守卫：文档不得再出现已被证实错误的说法。
func checkForbidden(root string, rules []forbiddenRule) ([]string, error) {
	// violations 保存仍出现的错误说法。
	var violations []string
	for _, rule := range rules { // rule 是一条回归守卫。
		// text、err 分别是被守卫文档全文和读取失败原因。
		text, err := readWiki(root, rule.file)
		if err != nil {
			return nil, err
		}
		if strings.Contains(strings.ToLower(text), strings.ToLower(rule.phrase)) {
			violations = append(violations, fmt.Sprintf("[回归守卫] %s 仍含 %q：%s", rule.file, rule.phrase, rule.reason))
		}
	}
	return violations, nil
}

// templateVariablePattern 匹配文档里写出的 {{变量}} 令牌。
var templateVariablePattern = regexp.MustCompile(`\{\{([A-Za-z0-9_.:<>-]+)\}\}`)

// checkTemplateVariablesAreSupported 反向校验：发货模板文档里出现的每个 {{变量}} 都必须被代码支持。
// supported 是代码支持的内置变量集合；cards.、custom.、delivery.、library_image: 前缀的变量按命名空间放行。
func checkTemplateVariablesAreSupported(root string, docs []string, supported map[string]bool) ([]string, error) {
	// violations 保存文档里出现的未知变量。
	var violations []string
	for _, doc := range docs { // doc 是被反向校验的文档。
		// text、err 分别是文档全文和读取失败原因。
		text, err := readWiki(root, doc)
		if err != nil {
			return nil, err
		}
		for _, match := range templateVariablePattern.FindAllStringSubmatch(text, -1) { // match 是文档里的一个 {{变量}}。
			// name 是去掉大括号后的变量名。
			name := match[1]
			if supported[name] || strings.HasPrefix(name, "cards.") || strings.HasPrefix(name, "custom.") ||
				strings.HasPrefix(name, "delivery.") || strings.HasPrefix(name, "library_image:") {
				continue
			}
			violations = append(violations, fmt.Sprintf("[模板变量反向校验] %s 出现了代码不支持的变量 {{%s}}", doc, name))
		}
	}
	return violations, nil
}

// codeSpanPattern 匹配 Markdown 行内代码。
var codeSpanPattern = regexp.MustCompile("`([^`\n]+)`")

// languageNeutral 判断行内代码是否语言无关（纯 ASCII 且含数字或符号），这类令牌中英文版必须一致。
func languageNeutral(span string) bool {
	// 带 <占位符>、引号或 URL 的片段含自然语言文字，不同语言写法天然不同，不参与对齐。
	if strings.ContainsAny(span, "<>\"'") || strings.Contains(span, "://") {
		return false
	}
	// hasSignal 表示片段含数字或标识符符号，排除普通英文单词。
	hasSignal := false
	for _, r := range span { // r 是片段中的一个字符。
		if r > 127 {
			return false
		}
		if (r >= '0' && r <= '9') || strings.ContainsRune("_-./{}:=$[]", r) {
			hasSignal = true
		}
	}
	// 纯数字片段（如 `0`）只是取值而非标识，不参与对齐。
	if strings.Trim(span, "0123456789") == "" {
		return false
	}
	return hasSignal
}

// neutralSpans 返回文档里全部语言无关的行内代码集合。
func neutralSpans(text string) map[string]bool {
	// spans 是去重后的语言无关片段集合。
	spans := make(map[string]bool)
	for _, match := range codeSpanPattern.FindAllStringSubmatch(text, -1) { // match 是一个行内代码匹配。
		if languageNeutral(match[1]) {
			spans[match[1]] = true
		}
	}
	return spans
}

// checkParity 校验中英文文档的语言无关行内代码集合一致，防止只改一种语言。
func checkParity(root string, pairs []zhEnPair) ([]string, error) {
	// violations 保存两侧不一致的片段。
	var violations []string
	for _, pair := range pairs { // pair 是一对中英文页面。
		// zhText、zhErr 分别是中文文档全文和读取失败原因。
		zhText, zhErr := readWiki(root, pair.zh)
		if zhErr != nil {
			return nil, zhErr
		}
		// enText、enErr 分别是英文文档全文和读取失败原因。
		enText, enErr := readWiki(root, pair.en)
		if enErr != nil {
			return nil, enErr
		}
		// zhSpans、enSpans 分别是两种语言的语言无关片段集合。
		zhSpans, enSpans := neutralSpans(zhText), neutralSpans(enText)
		for _, span := range sortedKeys(zhSpans) { // span 是中文版里出现的片段。
			if !enSpans[span] {
				violations = append(violations, fmt.Sprintf("[中英文对齐] %s 有 `%s`，但 %s 没有", pair.zh, span, pair.en))
			}
		}
		for _, span := range sortedKeys(enSpans) { // span 是英文版里出现的片段。
			if !zhSpans[span] {
				violations = append(violations, fmt.Sprintf("[中英文对齐] %s 有 `%s`，但 %s 没有", pair.en, span, pair.zh))
			}
		}
	}
	return violations, nil
}

// sortedKeys 返回集合的排序键，保证输出顺序稳定。
func sortedKeys(set map[string]bool) []string {
	// keys 保存排序前的键。
	keys := make([]string, 0, len(set))
	for key := range set { // key 是集合中的一个键。
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// userLinePattern 匹配 systemd 单元里的 User= / Group= 行。
var userLinePattern = regexp.MustCompile(`(?m)^(User|Group)=(.+)$`)

// appNamePattern 匹配 install.sh 的 APP_NAME 定义，该名称同时是服务用户名、用户组名和目录名。
var appNamePattern = regexp.MustCompile(`(?m)^APP_NAME="([^"]+)"`)

// checkLinuxPackaging 校验 Linux 安装脚本与 systemd 单元一致：
// 服务运行用户/组必须是脚本创建并 chown 的用户，目录与环境文件路径必须以同一名称为准。
func checkLinuxPackaging(root string) ([]string, error) {
	// installRaw、err 分别是安装脚本字节和读取失败原因。
	installRaw, err := os.ReadFile(filepath.Join(root, "packaging/linux/install.sh"))
	if err != nil {
		return nil, fmt.Errorf("读取 install.sh 失败: %w", err)
	}
	// unitRaw、unitErr 分别是 systemd 单元字节和读取失败原因。
	unitRaw, unitErr := os.ReadFile(filepath.Join(root, "packaging/linux/ydisks-xianyu-helper.service"))
	if unitErr != nil {
		return nil, fmt.Errorf("读取 systemd 单元失败: %w", unitErr)
	}
	// nameMatch 是 APP_NAME 的匹配结果。
	nameMatch := appNamePattern.FindStringSubmatch(string(installRaw))
	if nameMatch == nil {
		return []string{"[Linux 打包] install.sh 未找到 APP_NAME 定义"}, nil
	}
	// app 是安装脚本使用的统一应用名。
	app := nameMatch[1]
	// unit 是 systemd 单元全文。
	unit := string(unitRaw)
	// violations 保存全部不一致。
	var violations []string
	// seen 记录单元里出现的 User/Group 行数，缺失同样是违规。
	seen := 0
	for _, match := range userLinePattern.FindAllStringSubmatch(unit, -1) { // match 是一条 User=/Group= 声明。
		seen++
		if strings.TrimSpace(match[2]) != app {
			violations = append(violations, fmt.Sprintf("[Linux 打包] 单元 %s=%s 与 install.sh 创建的用户 %s 不一致，服务将无法启动", match[1], strings.TrimSpace(match[2]), app))
		}
	}
	if seen < 2 {
		violations = append(violations, "[Linux 打包] systemd 单元必须同时声明 User= 与 Group=")
	}
	// wantPaths 是单元里必须以统一应用名为准的路径片段。
	wantPaths := []string{"/var/lib/" + app, "/etc/" + app + "/config.env", "/var/log/" + app, "/opt/" + app + "/xianyu-server"}
	for _, want := range wantPaths { // want 是必须出现在单元中的路径。
		if !strings.Contains(unit, want) {
			violations = append(violations, fmt.Sprintf("[Linux 打包] systemd 单元缺少与 install.sh 一致的路径 %s", want))
		}
	}
	return violations, nil
}
