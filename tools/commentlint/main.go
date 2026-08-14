// commentlint 检查 Go 源码是否为新增或修改的声明提供了准确的中文注释。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// Finding 描述一个缺少中文注释的 Go 声明。
type Finding struct {
	// File 是相对于检查根目录的源码路径。
	File string `json:"file"`
	// Line 是声明在源码中的起始行号。
	Line int `json:"line"`
	// Kind 是声明类别，例如 function、variable 或 field。
	Kind string `json:"kind"`
	// Name 是声明名称，匿名声明使用 anonymous。
	Name string `json:"name"`
}

// main 根据命令行模式生成历史基线或检查当前源码。
func main() {
	// modeFlag 表示本次运行是生成基线还是执行门禁检查。
	modeFlag := flag.String("mode", "check", "运行模式：check 或 baseline")
	// rootFlag 指定需要递归扫描的仓库根目录。
	rootFlag := flag.String("root", ".", "源码扫描根目录")
	// baselineFlag 指定保存或读取历史问题基线的 JSON 文件。
	baselineFlag := flag.String("baseline", ".commentlint/go-baseline.json", "历史问题基线文件")
	flag.Parse()

	// findings 保存本次扫描发现的全部缺少中文注释项。
	findings, err := collectFindings(*rootFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "commentlint: %v\n", err)
		os.Exit(1)
	}

	// sortFindings 保证基线内容和门禁输出在不同机器上保持稳定顺序。
	sortFindings(findings)
	if *modeFlag == "baseline" {
		// writeErr 保存写入历史基线时的文件系统错误。
		if err := writeBaseline(*baselineFlag, findings); err != nil {
			fmt.Fprintf(os.Stderr, "commentlint: 写入基线失败：%v\n", err)
			os.Exit(1)
		}
		fmt.Printf("commentlint: 已记录 %d 个历史问题到 %s\n", len(findings), *baselineFlag)
		return
	}
	if *modeFlag != "check" {
		fmt.Fprintf(os.Stderr, "commentlint: 不支持的模式 %q\n", *modeFlag)
		os.Exit(2)
	}

	// baseline 保存允许继续存在的历史问题，不允许新增问题混入主干。
	baseline, err := readBaseline(*baselineFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "commentlint: 读取基线失败：%v\n", err)
		os.Exit(1)
	}
	// newFindings 只保留不在历史基线中的新增问题。
	newFindings := filterNewFindings(findings, baseline)
	// finding 表示当前待输出的一条新增注释问题。
	for _, finding := range newFindings {
		fmt.Printf("%s:%d: [%s] %s 缺少中文注释\n", finding.File, finding.Line, finding.Kind, finding.Name)
	}
	if len(newFindings) > 0 {
		fmt.Fprintf(os.Stderr, "commentlint: 发现 %d 个新增问题\n", len(newFindings))
		os.Exit(1)
	}
	fmt.Printf("commentlint: 通过（扫描 %d 个历史问题）\n", len(findings))
}

// collectFindings 递归解析根目录下所有 Go 文件并收集缺少中文注释的声明。
func collectFindings(root string) ([]Finding, error) {
	// findings 汇总所有文件的检查结果。
	findings := make([]Finding, 0)
	// walkErr 保存遍历过程中遇到的第一个错误。
	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if shouldSkipDirectory(path, root) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".gen.go") {
			return nil
		}
		// fileFindings 是当前文件的问题；parseErr 表示 AST 解析失败的原因。
		fileFindings, parseErr := inspectGoFile(root, path)
		if parseErr != nil {
			return parseErr
		}
		findings = append(findings, fileFindings...)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return findings, nil
}

// shouldSkipDirectory 判断目录是否为依赖、构建产物或运行时数据目录。
func shouldSkipDirectory(path, root string) bool {
	// relativePath 用统一的分隔符表达目录，避免跨平台判断差异。
	relativePath, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	// ignoredNames 是不属于业务源码、不能进入注释门禁的目录集合。
	ignoredNames := map[string]bool{
		".git": true, "vendor": true, "node_modules": true, "dist": true,
		"browser_data": true, "data": true, "internal/webui/static": true,
	}
	if ignoredNames[relativePath] {
		return true
	}
	// ignoredPath 表示当前检查的被忽略目录前缀。
	for ignoredPath := range ignoredNames {
		if strings.HasPrefix(relativePath, ignoredPath+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// inspectGoFile 解析单个 Go 文件并返回其中缺少中文注释的声明。
func inspectGoFile(root, path string) ([]Finding, error) {
	// fileSet 保存 AST 位置到源码行号的映射。
	fileSet := token.NewFileSet()
	// fileNode 是当前文件的语法树和注释信息。
	fileNode, err := parser.ParseFile(fileSet, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("解析 %s 失败：%w", path, err)
	}
	// relativePath 让基线不依赖开发者本机的绝对路径。
	relativePath, err := filepath.Rel(root, path)
	if err != nil {
		return nil, err
	}
	// findings 保存当前文件的检查结果。
	findings := make([]Finding, 0)
	// nodeStack 用于判断字段属于结构体/接口，还是函数参数/返回值。
	nodeStack := make([]ast.Node, 0)
	ast.Inspect(fileNode, func(node ast.Node) bool {
		if node == nil {
			nodeStack = nodeStack[:len(nodeStack)-1]
			return true
		}
		// parent 是当前 AST 节点的直接父节点，用于识别结构体或接口字段。
		var parent ast.Node
		if len(nodeStack) > 0 {
			parent = nodeStack[len(nodeStack)-1]
		}
		// current 是经过类型断言后的具体 AST 节点。
		switch current := node.(type) {
		case *ast.FuncDecl:
			if !hasChineseComment(fileNode, fileSet, current) {
				findings = append(findings, newFinding(relativePath, fileSet, current, "function", funcName(current.Name)))
			}
		case *ast.GenDecl:
			// spec 表示当前声明块中的一条常量、变量或类型规范。
			for _, spec := range current.Specs {
				checkSpec(fileNode, fileSet, relativePath, current, spec, &findings)
			}
		case *ast.Field:
			// isStruct 表示字段是否属于结构体定义。
			if _, isStruct := parent.(*ast.StructType); isStruct {
				checkField(fileNode, fileSet, relativePath, current, &findings)
			}
			// isInterface 表示字段是否属于接口定义。
			if _, isInterface := parent.(*ast.InterfaceType); isInterface {
				checkField(fileNode, fileSet, relativePath, current, &findings)
			}
		case *ast.AssignStmt:
			if current.Tok == token.DEFINE {
				checkIdentifiers(fileNode, fileSet, relativePath, current, current.Lhs, "local-variable", &findings)
			}
		case *ast.RangeStmt:
			checkIdentifiers(fileNode, fileSet, relativePath, current, []ast.Expr{current.Key, current.Value}, "range-variable", &findings)
		}
		nodeStack = append(nodeStack, node)
		return true
	})
	return findings, nil
}

// checkSpec 检查常量、变量、类型声明及其名称是否有中文注释。
func checkSpec(fileNode *ast.File, fileSet *token.FileSet, relativePath string, declaration *ast.GenDecl, spec ast.Spec, findings *[]Finding) {
	// kind 用于把 Go 声明映射到可读的门禁错误类别。
	kind := strings.ToLower(declaration.Tok.String())
	// current 是当前规范的具体 AST 类型。
	switch current := spec.(type) {
	case *ast.ValueSpec:
		// name 表示常量或变量规范中的一个绑定名称。
		for _, name := range current.Names {
			// declaration 允许分组 const/var 声明共用紧邻声明块的职责说明。
			if name.Name == "_" || hasChineseComment(fileNode, fileSet, declaration) || hasChineseComment(fileNode, fileSet, current) {
				continue
			}
			*findings = append(*findings, newFinding(relativePath, fileSet, current, kind, name.Name))
		}
	case *ast.TypeSpec:
		if !hasChineseComment(fileNode, fileSet, current) {
			*findings = append(*findings, newFinding(relativePath, fileSet, current, "type", current.Name.Name))
		}
	}
}

// checkField 检查结构体和接口字段，匿名字段以其类型名作为声明名称。
func checkField(fileNode *ast.File, fileSet *token.FileSet, relativePath string, field *ast.Field, findings *[]Finding) {
	if hasChineseComment(fileNode, fileSet, field) {
		return
	}
	// names 保存显式字段名；匿名字段没有 Names，需要使用类型文本兜底。
	names := make([]string, 0, len(field.Names))
	// name 表示结构体或接口字段中的一个显式名称。
	for _, name := range field.Names {
		names = append(names, name.Name)
	}
	if len(names) == 0 {
		names = append(names, "anonymous")
	}
	// name 表示已经归一化后的字段名称。
	for _, name := range names {
		*findings = append(*findings, newFinding(relativePath, fileSet, field, "field", name))
	}
}

// checkIdentifiers 检查短变量声明或范围变量中的每个有效标识符。
func checkIdentifiers(fileNode *ast.File, fileSet *token.FileSet, relativePath string, node ast.Node, expressions []ast.Expr, kind string, findings *[]Finding) {
	if hasChineseComment(fileNode, fileSet, node) {
		return
	}
	// expression 表示短变量或范围变量绑定列表中的一个表达式。
	for _, expression := range expressions {
		// identifier 是表达式对应的变量名；ok 表示表达式是否确实是标识符。
		identifier, ok := expression.(*ast.Ident)
		if !ok || identifier.Name == "_" {
			continue
		}
		*findings = append(*findings, newFinding(relativePath, fileSet, node, kind, identifier.Name))
	}
}

// hasChineseComment 判断声明附近是否存在包含汉字的文档或行注释。
func hasChineseComment(fileNode *ast.File, fileSet *token.FileSet, node ast.Node) bool {
	// startLine 和 endLine 限制注释必须紧邻声明，避免误认远处说明。
	startLine := fileSet.Position(node.Pos()).Line
	// endLine 是声明末尾所在的行，用于允许同一声明范围内的尾部注释。
	endLine := fileSet.Position(node.End()).Line
	// group 表示当前文件中的一组连续注释。
	for _, group := range fileNode.Comments {
		// commentStart 和 commentEnd 是注释组的首尾行。
		commentStart := fileSet.Position(group.Pos()).Line
		// commentEnd 是注释组末尾所在的行。
		commentEnd := fileSet.Position(group.End()).Line
		// commentEnd 必须紧邻声明；同一文档块可以跨越多行说明完整职责。
		if commentEnd > startLine || commentEnd < startLine-1 {
			continue
		}
		if commentStart == startLine || commentEnd == startLine-1 || commentStart <= endLine && commentEnd >= startLine {
			if containsChinese(group.Text()) {
				return true
			}
		}
	}
	return false
}

// containsChinese 判断文本中是否至少包含一个汉字。
func containsChinese(text string) bool {
	// character 是待检查文本中的一个 Unicode 字符。
	for _, character := range text {
		if unicode.Is(unicode.Han, character) {
			return true
		}
	}
	return false
}

// newFinding 将 AST 节点转换成稳定的基线记录。
func newFinding(relativePath string, fileSet *token.FileSet, node ast.Node, kind, name string) Finding {
	return Finding{File: filepath.ToSlash(relativePath), Line: fileSet.Position(node.Pos()).Line, Kind: kind, Name: name}
}

// funcName 返回函数名称，匿名函数统一使用 anonymous。
func funcName(identifier *ast.Ident) string {
	if identifier == nil || identifier.Name == "" {
		return "anonymous"
	}
	return identifier.Name
}

// sortFindings 按文件、行号、类别和名称对检查结果排序。
func sortFindings(findings []Finding) {
	sort.Slice(findings, func(left, right int) bool {
		if findings[left].File != findings[right].File {
			return findings[left].File < findings[right].File
		}
		if findings[left].Line != findings[right].Line {
			return findings[left].Line < findings[right].Line
		}
		if findings[left].Kind != findings[right].Kind {
			return findings[left].Kind < findings[right].Kind
		}
		return findings[left].Name < findings[right].Name
	})
}

// findingKey 生成基线记录的稳定键。
func findingKey(finding Finding) string {
	return fmt.Sprintf("%s:%d:%s:%s", finding.File, finding.Line, finding.Kind, finding.Name)
}

// writeBaseline 将当前发现写成可审查、可版本控制的 JSON 文件。
func writeBaseline(path string, findings []Finding) error {
	// mkdirErr 表示创建基线目录时的文件系统错误。
	if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o755); mkdirErr != nil {
		return mkdirErr
	}
	// file 是基线输出文件，使用缩进格式方便代码审查。
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	// encoder 负责以稳定缩进格式输出基线 JSON。
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(findings)
}

// readBaseline 读取历史基线并转换为快速查询集合。
func readBaseline(path string) (map[string]struct{}, error) {
	// data 是基线文件的 JSON 内容。
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// findings 是反序列化后的历史问题列表。
	var findings []Finding
	// unmarshalErr 表示基线 JSON 无法解析的原因。
	if unmarshalErr := json.Unmarshal(data, &findings); unmarshalErr != nil {
		return nil, unmarshalErr
	}
	// baseline 是稳定键集合，避免检查阶段重复遍历历史列表。
	baseline := make(map[string]struct{}, len(findings))
	// finding 表示历史基线中的一条问题记录。
	for _, finding := range findings {
		baseline[findingKey(finding)] = struct{}{}
	}
	return baseline, nil
}

// filterNewFindings 过滤掉已记录在历史基线中的问题。
func filterNewFindings(findings []Finding, baseline map[string]struct{}) []Finding {
	// newFindings 保存真正需要开发者处理的新增问题。
	newFindings := make([]Finding, 0)
	// finding 表示本次扫描发现的一条问题记录。
	for _, finding := range findings {
		// exists 表示该问题是否已经被历史基线允许。
		if _, exists := baseline[findingKey(finding)]; !exists {
			newFindings = append(newFindings, finding)
		}
	}
	return newFindings
}
