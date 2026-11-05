// docscheck 校验 docs/wiki 手册与代码事实的一致性：
// 命令行参数、环境变量、模板变量、邮费别名、数值上限、打包用户名，以及中英文页面的语言无关事实。
// 代码是事实来源，文档只能跟随；规则表见 rules.go，新增“代码里的枚举必须写进文档”的约束时在那里追加。
package main

import (
	"fmt"
	"os"
	"sort"
)

// main 解析可选的仓库根目录参数，执行全部检查并输出违规；存在违规或执行失败时以非零状态退出。
func main() {
	// root 是待检查仓库根目录，默认使用当前工作目录。
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	// violations、err 分别是全部违规和执行失败原因。
	violations, err := check(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "docs-check: %v\n", err)
		os.Exit(1)
	}
	for _, violation := range violations { // violation 是待输出的单条文档漂移。
		fmt.Fprintln(os.Stderr, violation)
	}
	if len(violations) > 0 {
		fmt.Fprintf(os.Stderr, "docs-check: 发现 %d 处文档与代码不一致\n", len(violations))
		os.Exit(1)
	}
	fmt.Println("docs-check: 通过")
}

// check 依次执行令牌、数值、回归守卫、模板变量反向校验、中英文对齐与 Linux 打包一致性检查。
// root 是仓库根目录；返回全部违规（已排序）或执行失败原因。
func check(root string) ([]string, error) {
	// violations 汇总全部检查产生的违规。
	var violations []string
	for _, rule := range tokenRules { // rule 是一条令牌规则。
		// found、err 分别是该规则的违规和执行失败原因。
		found, err := checkTokenRule(root, rule)
		if err != nil {
			return nil, err
		}
		violations = append(violations, found...)
	}
	for _, rule := range numberRules { // rule 是一条数值规则。
		// found、err 分别是该规则的违规和执行失败原因。
		found, err := checkNumberRule(root, rule)
		if err != nil {
			return nil, err
		}
		violations = append(violations, found...)
	}
	// forbidden、err 分别是回归守卫的违规和执行失败原因。
	forbidden, err := checkForbidden(root, forbiddenRules)
	if err != nil {
		return nil, err
	}
	violations = append(violations, forbidden...)
	// templateVars、templateErr 分别是模板变量反向校验的违规和执行失败原因。
	templateVars, templateErr := checkTemplateVariablesAreSupported(root,
		[]string{"发货模板.zh-CN.md", "Delivery-Templates.md"},
		map[string]bool{"order_id": true, "buyer_id": true, "buyer_nickname": true, "card_name": true})
	if templateErr != nil {
		return nil, templateErr
	}
	violations = append(violations, templateVars...)
	// parity、parityErr 分别是中英文对齐检查的违规和执行失败原因。
	parity, parityErr := checkParity(root, pagePairs)
	if parityErr != nil {
		return nil, parityErr
	}
	violations = append(violations, parity...)
	// linux、linuxErr 分别是 Linux 打包一致性检查的违规和执行失败原因。
	linux, linuxErr := checkLinuxPackaging(root)
	if linuxErr != nil {
		return nil, linuxErr
	}
	violations = append(violations, linux...)
	sort.Strings(violations)
	return violations, nil
}
