// architecturecheck 检查 Go 依赖方向、应用 Port 边界和 Server 裸事务入口。
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// violation 表示一条架构依赖违规记录。
type violation struct {
	// file 是违规文件的仓库相对路径。
	file string
	// line 是违规代码所在行号。
	line int
	// message 是面向开发者的修复提示。
	message string
}

// controlledDynamicResponse 描述一个暂时保留的动态响应及其外部兼容治理条件。
type controlledDynamicResponse struct {
	// matrixName 是兼容矩阵中必须出现的响应类型登记名。
	matrixName string
	// sunsetVersion 是该兼容响应计划移除的版本，必须与服务端遥测版本一致。
	sunsetVersion string
}

// controlledDynamicResponses 是兼容矩阵允许保留的最小动态响应登记表。
var controlledDynamicResponses = map[string]controlledDynamicResponse{
	"settingsResponse": {
		matrixName:    "settingsResponse",
		sunsetVersion: "v2.0",
	},
	"notificationBindingListResponse": {
		matrixName:    "notificationBindingListResponse",
		sunsetVersion: "v2.0",
	},
	"automationRulePageResponse": {
		matrixName:    "automationRulePageResponse",
		sunsetVersion: "v2.0",
	},
}

// main 执行架构依赖检查并在发现违规时返回失败状态。
func main() {
	// root 是待检查的仓库根目录。
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	// violations 保存全部架构违规，便于一次修复完整问题集。
	violations, err := checkRepository(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "architecturecheck: %v\n", err)
		os.Exit(1)
	}
	// violation 是待输出的单条架构违规记录。
	for _, violation := range violations {
		fmt.Fprintf(os.Stderr, "%s:%d: %s\n", violation.file, violation.line, violation.message)
	}
	if len(violations) > 0 {
		os.Exit(1)
	}
	fmt.Println("architecturecheck: 通过")
}

// checkRepository 扫描 Go 源码并汇总依赖方向与事务边界问题。
func checkRepository(root string) ([]violation, error) {
	// activeStage、stageErr 分别是总计划声明的唯一当前阶段及其解析失败原因。
	activeStage, stageErr := readActiveArchitectureStage(root)
	if stageErr != nil {
		return nil, stageErr
	}
	// violations 保存扫描过程中发现的全部违规。
	var violations []violation
	// fset 为 AST 节点提供统一文件与行号映射。
	fset := token.NewFileSet()
	// walkErr 是目录遍历或单文件检查的错误。
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		// relativePath 是当前文件相对于仓库根目录的路径。
		relativePath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		// fileViolations 保存当前文件的架构检查结果。
		fileViolations, err := checkGoFile(root, relativePath, fset, activeStage)
		if err != nil {
			return err
		}
		violations = append(violations, fileViolations...)
		return nil
	})
	if architectureStageEnabled(activeStage, architectureStageServerComposition) {
		// stageTwoViolations 是阶段二启用的组合根门禁；该阶段未完成前不得降级为告警或白名单。
		violations = append(violations, checkStageTwoCompositionRoot(root)...)
	}
	// phasedViolations 是已达到激活阶段的生命周期、前端、数据库与质量门禁结果。
	violations = append(violations, checkActivatedRepositoryGates(root, activeStage)...)
	violations = append(violations, checkCompatibilityGovernance(root)...)
	return violations, walkErr
}

// checkGoFile 检查单个 Go 文件的导入方向和事务调用位置。
func checkGoFile(root, relativePath string, fset *token.FileSet, activeStage int) ([]violation, error) {
	// filePath 是当前 Go 文件的绝对或仓库相对路径。
	filePath := filepath.Join(root, relativePath)
	// source 是当前文件原文，用于识别事务调用的精确文本边界。
	source, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	// syntax 是当前文件的 AST。
	syntax, err := parser.ParseFile(fset, filePath, source, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	// violations 保存当前文件发现的违规。
	var violations []violation
	// importPath 是当前文件所属包的导入路径前缀。
	importPath := filepath.ToSlash(relativePath)
	// imp 是当前 Go 文件的一条导入声明。
	for _, imp := range syntax.Imports {
		// importedPath 是导入声明去除 Go 字符串引号后的路径。
		importedPath, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return nil, err
		}
		// normalizedImport 是去除模块前缀后的内部导入路径。
		normalizedImport := normalizeImportPath(importedPath)
		if isForbiddenLowLevelImport(importPath, normalizedImport) {
			// line 是低层包反向依赖所在的源码行号。
			line := fset.Position(imp.Pos()).Line
			violations = append(violations, violation{
				file:    filepath.ToSlash(relativePath),
				line:    line,
				message: fmt.Sprintf("低层包禁止依赖上层应用包 %q", importedPath),
			})
		}
		if isForbiddenApplicationImport(importPath, normalizedImport) {
			// line 是应用层导入基础设施所在的源码行号。
			line := fset.Position(imp.Pos()).Line
			violations = append(violations, violation{
				file:    filepath.ToSlash(relativePath),
				line:    line,
				message: fmt.Sprintf("应用层禁止依赖基础设施或 HTTP 层 %q", importedPath),
			})
		}
		if isForbiddenHiddenDependencyImport(importPath, normalizedImport) {
			// line 是隐藏依赖导入所在的源码行号。
			line := fset.Position(imp.Pos()).Line
			violations = append(violations, violation{
				file:    filepath.ToSlash(relativePath),
				line:    line,
				message: fmt.Sprintf("业务与传输层禁止使用反射、插件或动态依赖隐藏必需装配 %q", importedPath),
			})
		}
		if isForbiddenServerLowLevelImport(importPath, normalizedImport) {
			// line 是 Server 新增低层依赖所在的源码行号。
			line := fset.Position(imp.Pos()).Line
			violations = append(violations, violation{
				file:    filepath.ToSlash(relativePath),
				line:    line,
				message: fmt.Sprintf("Server 新增低层依赖必须先迁移到应用 Port，禁止使用临时例外 %q", importedPath),
			})
		}
	}
	violations = append(violations, checkApplicationTypeLeaks(relativePath, syntax, fset)...)
	violations = append(violations, checkHTTPResponseContracts(relativePath, syntax, fset)...)
	violations = append(violations, checkHTTPRequestContracts(relativePath, syntax, fset)...)
	violations = append(violations, checkRuntimeSetterCalls(relativePath, syntax, fset)...)
	violations = append(violations, checkServerCompositionCalls(relativePath, syntax, fset)...)
	violations = append(violations, checkServerInfrastructureFields(relativePath, syntax, fset)...)
	if architectureStageEnabled(activeStage, architectureStageServerComposition) {
		violations = append(violations, checkStageTwoTransportBoundary(relativePath, syntax, fset)...)
	}
	if strings.HasPrefix(filepath.ToSlash(relativePath), "internal/server/") && !strings.HasSuffix(relativePath, "_repository.go") {
		// sourceLine 是裸 BeginTx 调用首次出现的源码行号。
		sourceLine := firstLineContaining(string(source), ".DB.BeginTx(")
		if sourceLine > 0 {
			violations = append(violations, violation{
				file:    filepath.ToSlash(relativePath),
				line:    sourceLine,
				message: "Server 业务层禁止直接创建数据库事务，请通过 repository 执行",
			})
		}
	}
	return violations, nil
}

// checkStageTwoCompositionRoot 强制阶段二使用独立组合层，并禁止 cmd 重新承担应用服务装配职责。
func checkStageTwoCompositionRoot(root string) []violation {
	// compositionPath 是阶段二唯一允许承载跨层生产装配的目录。
	compositionPath := filepath.Join(root, "internal", "composition")
	// entries、readErr 分别是组合层目录成员和读取失败原因。
	entries, readErr := os.ReadDir(compositionPath)
	if readErr != nil {
		return []violation{{file: "internal/composition", line: 1, message: "阶段二要求独立 composition 包承载应用服务和 worker 装配，禁止留在 Server 或 cmd/server"}}
	}
	// hasProductionGo 表示组合层至少包含一个非测试 Go 源文件。
	hasProductionGo := false
	// entry 是当前检查的组合层目录成员；只有生产 Go 文件能够证明存在真实装配实现。
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go") {
			hasProductionGo = true
			break
		}
	}
	if !hasProductionGo {
		return []violation{{file: "internal/composition", line: 1, message: "阶段二 composition 包必须包含生产装配代码，测试文件或空目录不能替代组合根"}}
	}
	// mainPath 是必须显式调用组合层的 Server 入口文件。
	mainPath := filepath.Join(root, "cmd", "server", "main.go")
	// syntax、parseErr 分别是入口源码 AST 及解析失败原因。
	syntax, parseErr := parser.ParseFile(token.NewFileSet(), mainPath, nil, parser.ImportsOnly)
	if parseErr != nil {
		return []violation{{file: "cmd/server/main.go", line: 1, message: fmt.Sprintf("无法解析 cmd/server 组合根导入: %v", parseErr)}}
	}
	if !importsCompositionPath(syntax) {
		return []violation{{file: "cmd/server/main.go", line: 1, message: "阶段二要求 cmd/server 显式调用 internal/composition，禁止自行装配应用服务和 worker"}}
	}
	return nil
}

// importsCompositionPath 判断 cmd 是否直接调用独立组合层或其明确的 runtime 子层。
func importsCompositionPath(syntax *ast.File) bool {
	// imported 是当前 cmd 文件声明的导入项，逐项判断是否显式依赖组合层。
	for _, imported := range syntax.Imports {
		// rawPath、unquoteErr 分别是导入路径的去引号文本及其解析错误。
		rawPath, unquoteErr := strconv.Unquote(imported.Path.Value)
		if unquoteErr != nil {
			continue
		}
		// importPath 是标准化后的仓库内导入路径。
		importPath := normalizeImportPath(rawPath)
		if importPath == "internal/composition" || strings.HasPrefix(importPath, "internal/composition/") {
			return true
		}
	}
	return false
}

// checkStageTwoTransportBoundary 以 fail-closed 规则阻止组合根、平台实现和生命周期所有权重新进入 Server。
func checkStageTwoTransportBoundary(relativePath string, syntax *ast.File, fset *token.FileSet) []violation {
	// normalizedPath 是统一使用斜杠的仓库相对路径。
	normalizedPath := filepath.ToSlash(relativePath)
	if strings.HasSuffix(normalizedPath, "_test.go") {
		return nil
	}
	// isServer 表示当前文件是否属于 HTTP transport 包。
	isServer := strings.HasPrefix(normalizedPath, "internal/server/")
	// isServerCommand 表示当前文件是否属于 Server 进程入口包。
	isServerCommand := strings.HasPrefix(normalizedPath, "cmd/server/")
	if !isServer && !isServerCommand {
		return nil
	}
	// violations 保存当前源码文件违反阶段二边界的全部位置。
	var violations []violation
	if isServer && filepath.Base(normalizedPath) == "application_services.go" {
		violations = append(violations, violation{file: normalizedPath, line: 1, message: "阶段二禁止 internal/server/application_services.go；应用服务、runner 和 coordinator 必须迁入 composition 与应用层"})
	}
	// forbiddenImports 是当前阶段不允许出现在 transport 或 cmd 组合入口的跨层依赖。
	var forbiddenImports map[string]string
	if isServer {
		forbiddenImports = map[string]string{
			"internal/account":    "Server 不得直接依赖 account.Manager，必须消费应用 Port",
			"internal/adapter":    "Server 不得直接依赖 adapter 实现或 factory，必须消费应用 Port",
			"internal/automation": "Server 不得直接依赖 automation.Center，必须消费应用 Port",
			"internal/browser":    "Server 不得直接依赖 browser 实现，必须消费应用 Port",
			"internal/chat":       "Server 不得直接依赖 chat.Service，必须消费聊天应用 Port",
			"internal/db":         "Server 不得直接依赖 db.Store 或 repository，实现必须留在 adapter",
			"internal/notify":     "Server 不得直接依赖 notifier，实现必须留在通知应用 Port",
			"internal/xianyu":     "Server 不得直接依赖平台协议，实现必须留在 adapter",
		}
	} else {
		forbiddenImports = map[string]string{
			"internal/adapter":      "cmd/server 不得直接装配 adapter 服务；必须委托 internal/composition",
			"internal/account":      "cmd/server 不得直接装配账号运行时；必须委托 internal/composition",
			"internal/automation":   "cmd/server 不得直接装配自动化 worker；必须委托 internal/composition",
			"internal/chat":         "cmd/server 不得直接装配聊天服务；必须委托 internal/composition",
			"internal/notify":       "cmd/server 不得直接装配通知 worker；必须委托 internal/composition",
			"internal/application/": "cmd/server 不得直接构造应用服务或 worker；必须委托 internal/composition",
		}
	}
	// imported 是当前源码声明的内部或标准库导入，必须逐项检查其是否越过当前层边界。
	for _, imported := range syntax.Imports {
		// rawPath、unquoteErr 分别是当前导入路径及其语法解析错误。
		rawPath, unquoteErr := strconv.Unquote(imported.Path.Value)
		if unquoteErr != nil {
			continue
		}
		// importPath 是去除模块前缀后的稳定内部导入路径。
		importPath := normalizeImportPath(rawPath)
		// forbiddenPath 是禁止导入的内部路径前缀；message 是命中后返回给迁移者的具体修复方向。
		for forbiddenPath, message := range forbiddenImports {
			if importPath == forbiddenPath || strings.HasPrefix(importPath, forbiddenPath) {
				violations = append(violations, violation{file: normalizedPath, line: fset.Position(imported.Pos()).Line, message: message})
				break
			}
		}
	}
	if isServer {
		violations = append(violations, checkStageTwoServerDeclarations(normalizedPath, syntax, fset)...)
		violations = append(violations, checkStageTwoServerConstruction(normalizedPath, syntax, fset)...)
		violations = append(violations, checkStageTwoApplicationPortDeclarations(normalizedPath, syntax, fset)...)
	}
	violations = append(violations, checkStageTwoCompositionProjection(normalizedPath, syntax, fset)...)
	if isServerCommand {
		violations = append(violations, checkStageTwoCmdCalls(normalizedPath, syntax, fset)...)
	}
	return violations
}

// checkStageTwoApplicationPortDeclarations 拒绝 HTTP Port 容器重新持有具体应用服务实现。
func checkStageTwoApplicationPortDeclarations(relativePath string, syntax *ast.File, fset *token.FileSet) []violation {
	if filepath.Base(relativePath) != "application_ports.go" {
		return nil
	}
	// applicationAliases 保存当前文件中应用包的本地导入别名。
	applicationAliases := applicationImportAliases(syntax)
	// violations 保存具体应用服务指针泄露到 transport Port 容器的全部位置。
	var violations []violation
	// declaration 是当前文件的顶级声明，只有类型声明可能定义 Port 容器。
	for _, declaration := range syntax.Decls {
		// generalDeclaration、ok 分别是转换后的通用声明及其类型匹配状态。
		generalDeclaration, ok := declaration.(*ast.GenDecl)
		if !ok || generalDeclaration.Tok != token.TYPE {
			continue
		}
		// specification 是该通用声明中的单个类型规格。
		for _, specification := range generalDeclaration.Specs {
			// typeSpecification、ok 分别是转换后的类型规格及其匹配状态。
			typeSpecification, ok := specification.(*ast.TypeSpec)
			if !ok || (typeSpecification.Name.Name != "ApplicationPorts" && typeSpecification.Name.Name != "ApplicationPortsInput") {
				continue
			}
			// structure、ok 分别是 Port 容器的结构体定义及其类型匹配状态。
			structure, ok := typeSpecification.Type.(*ast.StructType)
			if !ok {
				continue
			}
			// field 是当前 Port 容器声明的字段，用于检查是否泄露具体应用服务。
			for _, field := range structure.Fields.List {
				if !containsConcreteApplicationServicePointer(field.Type, applicationAliases) {
					continue
				}
				violations = append(violations, violation{file: relativePath, line: fset.Position(field.Pos()).Line, message: "阶段二 HTTP 应用 Port 容器不得持有具体 application Service 指针；请在 internal/server 定义消费者接口并由 composition 投影实现"})
			}
		}
	}
	return violations
}

// applicationImportAliases 收集 internal/application 子包的本地别名。
func applicationImportAliases(syntax *ast.File) map[string]struct{} {
	// aliases 保存应用包在当前文件中的可见名称。
	aliases := make(map[string]struct{})
	// imported 是当前 transport 文件的导入项，用于识别应用包的本地别名。
	for _, imported := range syntax.Imports {
		// rawPath、unquoteErr 分别是导入路径的去引号文本及其解析错误。
		rawPath, unquoteErr := strconv.Unquote(imported.Path.Value)
		if unquoteErr != nil {
			continue
		}
		// importPath 是标准化后的仓库内导入路径。
		importPath := normalizeImportPath(rawPath)
		if !strings.HasPrefix(importPath, "internal/application/") {
			continue
		}
		// alias 是当前文件使用的应用包本地名称，默认取导入路径末段。
		alias := filepath.Base(importPath)
		if imported.Name != nil {
			alias = imported.Name.Name
		}
		if alias != "_" && alias != "." {
			aliases[alias] = struct{}{}
		}
	}
	return aliases
}

// containsConcreteApplicationServicePointer 递归识别字段类型中隐藏的具体应用服务指针。
func containsConcreteApplicationServicePointer(expression ast.Expr, applicationAliases map[string]struct{}) bool {
	// pointer 表示当前类型是否直接持有应用层对象；只有具体 Service/ServiceSet 才违反 Port 边界。
	if pointer, ok := expression.(*ast.StarExpr); ok {
		// selector、ok 分别是指针元素的限定类型选择器及其匹配状态。
		if selector, ok := pointer.X.(*ast.SelectorExpr); ok {
			// packageName、ok 分别是限定类型所属包标识符及其匹配状态。
			if packageName, ok := selector.X.(*ast.Ident); ok {
				// imported 表示该包名是否来自当前文件导入的应用层包。
				if _, imported := applicationAliases[packageName.Name]; imported && (strings.HasSuffix(selector.Sel.Name, "Service") || strings.HasSuffix(selector.Sel.Name, "ServiceSet")) {
					return true
				}
			}
		}
		return containsConcreteApplicationServicePointer(pointer.X, applicationAliases)
	}
	// array 表示切片或数组字段；递归避免以集合形式隐藏具体服务。
	if array, ok := expression.(*ast.ArrayType); ok {
		return containsConcreteApplicationServicePointer(array.Elt, applicationAliases)
	}
	// mapping 表示 map 字段；键和值都不能用于隐藏服务实例。
	if mapping, ok := expression.(*ast.MapType); ok {
		return containsConcreteApplicationServicePointer(mapping.Key, applicationAliases) || containsConcreteApplicationServicePointer(mapping.Value, applicationAliases)
	}
	return false
}

// checkStageTwoCompositionProjection 确保 composition 核心不反向依赖 Server，只有 runtime 子层可以投影 Server 依赖。
func checkStageTwoCompositionProjection(relativePath string, syntax *ast.File, fset *token.FileSet) []violation {
	if !strings.HasPrefix(relativePath, "internal/composition/") || strings.HasPrefix(relativePath, "internal/composition/runtime/") {
		return nil
	}
	// imported 是 composition 核心文件的导入项，核心层不得反向导入 Server。
	for _, imported := range syntax.Imports {
		// rawPath、unquoteErr 分别是导入路径的去引号文本及其解析错误。
		rawPath, unquoteErr := strconv.Unquote(imported.Path.Value)
		if unquoteErr != nil || normalizeImportPath(rawPath) != "internal/server" {
			continue
		}
		return []violation{{file: relativePath, line: fset.Position(imported.Pos()).Line, message: "阶段二 composition 核心不得依赖 Server；只能由 internal/composition/runtime 投影 server.Dependencies"}}
	}
	return nil
}

// checkStageTwoServerDeclarations 禁止 Server 继续声明服务集合、平台 Port 或反向生命周期 API。
func checkStageTwoServerDeclarations(relativePath string, syntax *ast.File, fset *token.FileSet) []violation {
	// forbiddenTypes 是必须从 internal/server 消失的组合根和平台类型。
	forbiddenTypes := map[string]string{
		"ApplicationServices":            "Server 不能拥有应用服务集合；请迁入 composition 并向 handler 注入最小应用 Port",
		"ApplicationServiceDependencies": "Server 不能声明应用服务装配依赖；请迁入 composition",
		"PlatformPort":                   "Server 不能持有 MTOP、长登录或二维码平台 Port；请迁入对应应用用例",
	}
	// forbiddenMethods 是必须从 Server 消失的反向访问器和 session callback。
	forbiddenMethods := map[string]string{
		"ApplicationServices":       "Server 不得向外返还应用服务集合",
		"LifecycleComponents":       "Server 不得生成或返还应用 worker 生命周期组件",
		"mtopClient":                "Server 不得获取 MTOP 客户端",
		"longLoginClient":           "Server 不得获取长登录客户端",
		"qrLoginService":            "Server 不得获取二维码平台客户端",
		"sessionRecoveryCallback":   "Server 不得创建 session recovery callback",
		"recoverExpiredMTOPSession": "Server 不得承载 MTOP session 恢复",
	}
	// violations 保存声明层违反阶段二边界的位置。
	var violations []violation
	// declaration 是当前 Server 文件的顶层声明，可能声明禁止的组合根类型或访问器。
	for _, declaration := range syntax.Decls {
		// typedDeclaration 是顶层声明的具体语法类型，用于分开处理类型和函数声明。
		switch typedDeclaration := declaration.(type) {
		case *ast.GenDecl:
			if typedDeclaration.Tok != token.TYPE {
				continue
			}
			// specification 是当前类型声明块中的单个类型定义。
			for _, specification := range typedDeclaration.Specs {
				// typeSpecification、ok 分别是当前语法规格是否为具名类型及其断言结果。
				typeSpecification, ok := specification.(*ast.TypeSpec)
				if !ok {
					continue
				}
				// message、forbidden 分别是命中禁止类型时的修复提示及其匹配结果。
				if message, forbidden := forbiddenTypes[typeSpecification.Name.Name]; forbidden {
					violations = append(violations, violation{file: relativePath, line: fset.Position(typeSpecification.Pos()).Line, message: message})
				}
			}
		case *ast.FuncDecl:
			// message、forbidden 分别是命中禁止 Server 访问器时的修复提示及其匹配结果。
			if message, forbidden := forbiddenMethods[typedDeclaration.Name.Name]; forbidden {
				violations = append(violations, violation{file: relativePath, line: fset.Position(typedDeclaration.Pos()).Line, message: message})
			}
		}
	}
	return violations
}

// checkStageTwoServerConstruction 拒绝 Server 直接构造应用服务、runner、coordinator 或 adapter factory。
func checkStageTwoServerConstruction(relativePath string, syntax *ast.File, fset *token.FileSet) []violation {
	// internalAliases 保存当前文件中指向 application 或 adapter 的导入别名，供构造调用精确判定。
	internalAliases := internalConstructionImportAliases(syntax)
	// violations 保存调用层违反阶段二边界的位置。
	var violations []violation
	ast.Inspect(syntax, func(node ast.Node) bool {
		// call 是当前待检查的函数或方法调用。
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		// selector 表示当前调用的接收者与方法名称。
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		// name 是当前被调用的方法或构造函数名称。
		name := selector.Sel.Name
		if name == "NewApplicationServices" || name == "LifecycleComponents" || name == "ApplicationServices" || name == "sessionRecoveryCallback" || name == "recoverExpiredMTOPSession" || (strings.HasPrefix(name, "New") && isInternalConstructionCall(selector.X, internalAliases)) {
			violations = append(violations, violation{file: relativePath, line: fset.Position(call.Pos()).Line, message: fmt.Sprintf("Server 禁止调用 %s；应用服务、runner、coordinator 和 adapter factory 必须由 composition 构造", name)})
		}
		return true
	})
	return violations
}

// internalConstructionImportAliases 收集 application 与 adapter 导入的本地别名，避免误判标准库 New 调用。
func internalConstructionImportAliases(syntax *ast.File) map[string]struct{} {
	// aliases 保存需要受阶段二构造门禁保护的本地导入名。
	aliases := make(map[string]struct{})
	// imported 是当前待解析别名的导入声明。
	for _, imported := range syntax.Imports {
		// rawPath、unquoteErr 分别是当前导入路径及其语法解析错误。
		rawPath, unquoteErr := strconv.Unquote(imported.Path.Value)
		if unquoteErr != nil {
			continue
		}
		// importPath 是去除模块前缀后的内部导入路径。
		importPath := normalizeImportPath(rawPath)
		if !strings.HasPrefix(importPath, "internal/application/") && !strings.HasPrefix(importPath, "internal/adapter") {
			continue
		}
		// alias 是源码调用选择器使用的包名；显式别名优先于路径末段。
		alias := filepath.Base(importPath)
		if imported.Name != nil {
			alias = imported.Name.Name
		}
		if alias != "_" && alias != "." {
			aliases[alias] = struct{}{}
		}
	}
	return aliases
}

// isInternalConstructionCall 判断构造调用是否来自应用/adapter 包或依赖 factory 链。
func isInternalConstructionCall(receiver ast.Expr, internalAliases map[string]struct{}) bool {
	// identifier 是直接包名调用，例如 orderapp.NewRefreshJobRunner。
	if identifier, ok := receiver.(*ast.Ident); ok {
		// found 表示该直接接收者是否是 application 或 adapter 的本地导入别名。
		_, found := internalAliases[identifier.Name]
		return found
	}
	// selector 表示依赖 factory 链，例如 dependencies.ItemDependencies.NewItemBatchRepository。
	_, isFactoryChain := receiver.(*ast.SelectorExpr)
	return isFactoryChain
}

// checkStageTwoCmdCalls 拒绝 cmd/server 通过 Server API 或 factory 间接重新承担业务组合根职责。
func checkStageTwoCmdCalls(relativePath string, syntax *ast.File, fset *token.FileSet) []violation {
	// violations 保存入口层违反阶段二边界的位置。
	var violations []violation
	ast.Inspect(syntax, func(node ast.Node) bool {
		// call 是当前待检查的函数或方法调用。
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		// selector 表示当前调用的接收者与名称。
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		// name 是被调用的构造或生命周期访问器名称。
		name := selector.Sel.Name
		if name == "NewApplicationServices" || name == "LifecycleComponents" || name == "ApplicationServices" {
			violations = append(violations, violation{file: relativePath, line: fset.Position(call.Pos()).Line, message: fmt.Sprintf("cmd/server 禁止通过 %s 使用 Server 组合根；必须调用 internal/composition", name)})
		}
		return true
	})
	return violations
}

// checkServerInfrastructureFields 禁止 HTTP Server 保存业务运行时或具体平台实现，避免 transport 成为组合根。
func checkServerInfrastructureFields(relativePath string, syntax *ast.File, fset *token.FileSet) []violation {
	// normalizedPath 是统一使用斜杠的仓库相对路径。
	normalizedPath := filepath.ToSlash(relativePath)
	if !strings.HasPrefix(normalizedPath, "internal/server/") || strings.HasSuffix(normalizedPath, "_test.go") {
		return nil
	}
	// forbiddenTypes 记录 Server 不得作为字段持有者的业务运行时和平台实现类型。
	forbiddenTypes := map[string]string{
		"account.Manager":              "账号 Manager 必须由进程组合根和应用 Port 持有，Server 只能调用应用服务",
		"automation.Center":            "自动化 Center 必须由应用层持有，Server 不能保存业务 worker",
		"notify.Notifier":              "通知器必须通过通知应用 Port 使用，Server 不能保存基础设施实现",
		"adapter.PlatformDependencies": "具体平台依赖必须在组合根封装为消费者定义的 Port",
		"adapter.MTOPClient":           "MTOP 客户端必须通过应用 Port 或不可变平台 Port 提供",
		"adapter.LongLoginClient":      "长登录客户端必须通过应用 Port 或不可变平台 Port 提供",
		"adapter.QRLoginService":       "二维码客户端必须通过应用 Port 或不可变平台 Port 提供",
	}
	// violations 保存 Server 结构体字段违反基础设施持有边界的扫描结果。
	var violations []violation
	// declaration 是当前文件中的类型声明，只有 Server 结构体需要本规则检查。
	for _, declaration := range syntax.Decls {
		// generalDeclaration 表示可能包含类型定义的声明。
		generalDeclaration, ok := declaration.(*ast.GenDecl)
		if !ok || generalDeclaration.Tok != token.TYPE {
			continue
		}
		// specification 是当前声明中的一个类型定义。
		for _, specification := range generalDeclaration.Specs {
			// typeSpecification 表示具体类型名及其语法树。
			typeSpecification, ok := specification.(*ast.TypeSpec)
			if !ok || typeSpecification.Name.Name != "Server" {
				continue
			}
			// structure 表示 Server 的字段集合；非结构体类型不适用本规则。
			structure, ok := typeSpecification.Type.(*ast.StructType)
			if !ok {
				continue
			}
			// field 是当前待检查的 Server 字段。
			for _, field := range structure.Fields.List {
				// typeName 是去掉指针层后的限定类型名；未知表达式保持空字符串。
				typeName := qualifiedTypeName(field.Type)
				// message、forbidden 分别表示字段类型的禁止原因及其是否命中。
				message, forbidden := forbiddenTypes[typeName]
				if !forbidden {
					continue
				}
				violations = append(violations, violation{file: normalizedPath, line: fset.Position(field.Pos()).Line, message: message})
			}
		}
	}
	return violations
}

// qualifiedTypeName 返回指针、选择器或标识符类型的稳定文本名，供结构体字段边界规则使用。
func qualifiedTypeName(expression ast.Expr) string {
	// pointer 表示字段是否以指针形式声明；边界规则不区分值与指针持有。
	if pointer, ok := expression.(*ast.StarExpr); ok {
		return qualifiedTypeName(pointer.X)
	}
	// selector 表示包限定类型，例如 adapter.MTOPClient。
	if selector, ok := expression.(*ast.SelectorExpr); ok {
		// packageName、ok 分别表示选择器左侧是否为可解析的包标识符。
		packageName, ok := selector.X.(*ast.Ident)
		if ok {
			return packageName.Name + "." + selector.Sel.Name
		}
	}
	// identifier 表示当前字段是否为未限定类型；本规则当前不禁止未限定类型。
	if identifier, ok := expression.(*ast.Ident); ok {
		return identifier.Name
	}
	return ""
}

// checkServerCompositionCalls 禁止已迁出的应用 worker 组合逻辑回流到 Server transport 包。
func checkServerCompositionCalls(relativePath string, syntax *ast.File, fset *token.FileSet) []violation {
	// normalizedPath 是统一使用斜杠的仓库相对路径。
	normalizedPath := filepath.ToSlash(relativePath)
	if !strings.HasPrefix(normalizedPath, "internal/server/") || strings.HasSuffix(normalizedPath, "_test.go") {
		return nil
	}
	// forbiddenConstructors 记录必须由进程组合根创建的应用 worker 构造函数及修复提示。
	forbiddenConstructors := map[string]string{
		"NewReconciliationRecoveryCoordinator": "订单补偿恢复协调器必须由 cmd 组合根构造后注入 Server",
		"NewDatabaseHealth":                    "数据库健康检查端口必须由 cmd 组合根构造后注入 Server",
	}
	// forbiddenMethods 记录 Server 不得再作为应用 worker 生命周期反向提供者的遗留方法。
	forbiddenMethods := map[string]string{
		"ApplicationLifecycleComponents": "应用 worker 生命周期组件必须由组合根的应用服务集合登记，Server 不得反向返还组件",
	}
	// violations 保存当前 Server 文件中发现的组合根回流问题。
	var violations []violation
	ast.Inspect(syntax, func(node ast.Node) bool {
		// call 是当前待检查的函数调用节点。
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		// selector 是包函数或对象方法调用；仅包级构造函数可能违反本规则。
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		// message 是命中构造函数或遗留生命周期方法后返回的组合根迁移提示。
		message, forbidden := forbiddenConstructors[selector.Sel.Name]
		if !forbidden {
			message, forbidden = forbiddenMethods[selector.Sel.Name]
		}
		if forbidden {
			violations = append(violations, violation{
				file:    normalizedPath,
				line:    fset.Position(call.Pos()).Line,
				message: message,
			})
		}
		return true
	})
	return violations
}

// checkHTTPRequestContracts 禁止 Server handler 使用匿名请求结构，保证请求契约能够被复用、审计和版本化。
func checkHTTPRequestContracts(relativePath string, syntax *ast.File, fset *token.FileSet) []violation {
	// normalizedPath 是统一使用斜杠的仓库相对路径。
	normalizedPath := filepath.ToSlash(relativePath)
	if !strings.HasPrefix(normalizedPath, "internal/server/") || strings.HasSuffix(normalizedPath, "_test.go") || !strings.HasSuffix(normalizedPath, "_handlers.go") {
		return nil
	}
	// violations 保存当前 handler 文件中发现的匿名请求结构。
	var violations []violation
	ast.Inspect(syntax, func(node ast.Node) bool {
		// declaration 是函数体内可能包含请求变量的声明语句。
		declaration, ok := node.(*ast.DeclStmt)
		if !ok {
			return true
		}
		// generalDeclaration 是具体的 var 声明；短变量声明不会承载匿名 struct 类型。
		generalDeclaration, ok := declaration.Decl.(*ast.GenDecl)
		if !ok || generalDeclaration.Tok != token.VAR {
			return true
		}
		// specification 是当前 var 声明中的单个语法规格。
		for _, specification := range generalDeclaration.Specs {
			// valueSpecification 是当前 var 声明的名称、类型和初始值组合。
			valueSpecification, ok := specification.(*ast.ValueSpec)
			if !ok {
				continue
			}
			// anonymousStruct 表示该变量是否直接声明为匿名 struct 类型。
			_, anonymousStruct := valueSpecification.Type.(*ast.StructType)
			if !anonymousStruct {
				continue
			}
			// name 是当前匿名结构声明中的变量名。
			for _, name := range valueSpecification.Names {
				if name.Name != "req" && name.Name != "input" {
					continue
				}
				violations = append(violations, violation{
					file:    normalizedPath,
					line:    fset.Position(valueSpecification.Pos()).Line,
					message: fmt.Sprintf("HTTP 请求变量 %s 禁止使用匿名 struct，请定义具名 DTO", name.Name),
				})
			}
		}
		return true
	})
	return violations
}

// checkRuntimeSetterCalls 禁止生产代码调用仅为测试替身保留的 Adapter 运行时 setter。
func checkRuntimeSetterCalls(relativePath string, syntax *ast.File, fset *token.FileSet) []violation {
	// normalizedPath 是统一使用斜杠的仓库相对路径。
	normalizedPath := filepath.ToSlash(relativePath)
	if strings.HasSuffix(normalizedPath, "_test.go") {
		return nil
	}
	// testOnlySetters 是已登记为测试隔离入口的 Adapter setter 名称。
	testOnlySetters := map[string]struct{}{
		"SetAutomation": {}, "SetNotifier": {}, "SetChatService": {}, "SetCredentialWakeService": {},
		"SetBrowser": {}, "SetRenewService": {}, "SetTokenCaptchaRequester": {}, "SetOrderDetailClient": {},
	}
	// violations 保存生产代码绕过构造期依赖固定的调用位置。
	var violations []violation
	ast.Inspect(syntax, func(node ast.Node) bool {
		// call 是当前待判断的函数调用节点。
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		// selector 是当前调用的选择器表达式，只有明确的 Adapter setter 名称才属于本规则。
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		// registered 表示当前调用是否属于明确登记的测试兼容 setter。
		if _, registered := testOnlySetters[selector.Sel.Name]; !registered {
			return true
		}
		violations = append(violations, violation{
			file:    normalizedPath,
			line:    fset.Position(call.Pos()).Line,
			message: fmt.Sprintf("生产代码禁止调用测试兼容 setter %s，请通过构造期 RuntimeBundle 注入", selector.Sel.Name),
		})
		return true
	})
	return violations
}

// checkHTTPResponseContracts 检查 Server 对外响应是否使用具名 DTO，并阻止动态 map 绕过契约边界。
func checkHTTPResponseContracts(relativePath string, syntax *ast.File, fset *token.FileSet) []violation {
	// normalizedPath 是统一使用斜杠的仓库相对路径。
	normalizedPath := filepath.ToSlash(relativePath)
	if !strings.HasPrefix(normalizedPath, "internal/server/") || strings.HasSuffix(normalizedPath, "_test.go") {
		return nil
	}
	// violations 保存当前 Server 文件发现的 HTTP 契约问题。
	var violations []violation
	ast.Inspect(syntax, func(node ast.Node) bool {
		// typedNode 是当前遍历到的 AST 节点，用于识别 HTTP 契约声明或调用。
		switch typedNode := node.(type) {
		case *ast.TypeSpec:
			if !isHTTPContractTypeName(typedNode.Name.Name) {
				return true
			}
			if (typedNode.Name.Name == "settingsResponse" || containsDynamicMapType(typedNode.Type)) &&
				!isControlledDynamicResponseType(typedNode.Name.Name) {
				violations = append(violations, violation{
					file:    normalizedPath,
					line:    fset.Position(typedNode.Pos()).Line,
					message: fmt.Sprintf("HTTP 契约类型 %s 禁止使用动态 map，请定义具名 DTO 字段", typedNode.Name.Name),
				})
			}
		case *ast.CallExpr:
			if !isWriteJSONCall(typedNode) || len(typedNode.Args) < 3 {
				return true
			}
			// responseArg 是 writeJSON 的响应值参数，必须是具名 DTO 或受控类型。
			responseArg := typedNode.Args[2]
			if isDynamicMapLiteral(responseArg) {
				violations = append(violations, violation{
					file:    normalizedPath,
					line:    fset.Position(responseArg.Pos()).Line,
					message: "HTTP 响应禁止直接写入动态 map，请使用具名 DTO",
				})
			}
		}
		return true
	})
	return violations
}

// isControlledDynamicResponseType 判断已登记的动态键兼容响应，避免在未满足 Sunset 条件前改变旧客户端 JSON 形状。
func isControlledDynamicResponseType(name string) bool {
	// ok 表示响应类型是否已在带 Sunset 条件的兼容登记表中备案。
	_, ok := controlledDynamicResponses[name]
	return ok
}

// isForbiddenHiddenDependencyImport 禁止应用与 Server 通过反射、插件机制或动态加载隐藏必需依赖。
func isForbiddenHiddenDependencyImport(filePath, importedPath string) bool {
	// productionLayer 表示必须封死隐式装配旁路的生产层。
	productionLayer := (strings.HasPrefix(filePath, "internal/application/") && !strings.HasSuffix(filePath, "_test.go")) ||
		(strings.HasPrefix(filePath, "internal/server/") && !strings.HasSuffix(filePath, "_test.go"))
	if !productionLayer {
		return false
	}
	for _, forbidden /* forbidden 是禁止隐藏依赖实现的标准库或运行时包名。 */ := range []string{"reflect", "plugin", "unsafe"} {
		if importedPath == forbidden || strings.HasPrefix(importedPath, forbidden+"/") {
			return true
		}
	}
	return false
}

// checkCompatibilityGovernance 校验受控动态响应登记、Sunset 版本与运行时遥测保持同步。
func checkCompatibilityGovernance(root string) []violation {
	// matrixPath 是记录外部调用方、删除条件和 Sunset 版本的兼容矩阵路径。
	matrixPath := filepath.Join(root, "docs", "architecture", "api-compatibility-matrix.md")
	// matrixBytes 是兼容矩阵原文，用于避免受控兼容响应脱离文档治理。
	matrixBytes, err := os.ReadFile(matrixPath)
	if err != nil {
		return []violation{{file: filepath.ToSlash(filepath.Join("docs", "architecture", "api-compatibility-matrix.md")), line: 1, message: fmt.Sprintf("无法读取兼容矩阵: %v", err)}}
	}
	// matrix 是兼容矩阵文本，统一使用字符串匹配保留文档格式独立性。
	matrix := string(matrixBytes)
	// serverPath 是定义历史 API 遥测版本的服务端文件路径。
	serverPath := filepath.Join(root, "internal", "server", "server.go")
	// serverBytes 是服务端源码，用于确认每个兼容响应共用实际遥测版本。
	serverBytes, err := os.ReadFile(serverPath)
	if err != nil {
		return []violation{{file: filepath.ToSlash(filepath.Join("internal", "server", "server.go")), line: 1, message: fmt.Sprintf("无法读取历史 API 遥测实现: %v", err)}}
	}
	// serverSource 是服务端源码文本，供版本与弃用头检查使用。
	serverSource := string(serverBytes)
	// violations 保存兼容治理缺失或版本漂移问题。
	var violations []violation
	for name /* name 是当前受控动态响应的 Go 类型名。 */, registration /* registration 是该响应的矩阵登记与退场版本。 */ := range controlledDynamicResponses {
		if !strings.Contains(matrix, "`"+registration.matrixName+"`") && !strings.Contains(matrix, registration.matrixName) {
			violations = append(violations, violation{file: "docs/architecture/api-compatibility-matrix.md", line: 1, message: fmt.Sprintf("动态响应 %s 未登记在兼容矩阵", name)})
		}
		if !strings.Contains(matrix, registration.sunsetVersion) {
			violations = append(violations, violation{file: "docs/architecture/api-compatibility-matrix.md", line: 1, message: fmt.Sprintf("动态响应 %s 缺少 Sunset 版本 %s", name, registration.sunsetVersion)})
		}
	}
	if !strings.Contains(serverSource, `legacyAPISuccessorLink = "</api/v1>; rel=\"successor-version\"; title=\"v2.0\""`) {
		violations = append(violations, violation{file: "internal/server/server.go", line: 1, message: "历史 API successor Link 必须声明与兼容矩阵一致的版本 v2.0"})
	}
	if !strings.Contains(serverSource, `legacyAPISunsetDate = `) || !strings.Contains(serverSource, `Header().Set("Deprecation", "true")`) || !strings.Contains(serverSource, `Header().Set("Sunset", legacyAPISunsetDate)`) {
		violations = append(violations, violation{file: "internal/server/server.go", line: 1, message: "历史 API 必须写入 Deprecation 与 Sunset 遥测头"})
	}
	return violations
}

// isHTTPContractTypeName 判断类型名称是否属于 Server 的 HTTP 响应契约。
func isHTTPContractTypeName(name string) bool {
	// lowerName 统一大小写，兼容 Response、DTO、Envelope 和 Result 的命名习惯。
	lowerName := strings.ToLower(name)
	return strings.HasSuffix(lowerName, "response") || strings.HasSuffix(lowerName, "dto") ||
		strings.HasSuffix(lowerName, "envelope") || strings.HasSuffix(lowerName, "result")
}

// containsDynamicMapType 递归识别以 any/interface{} 为值的动态 map，保留已有键值兼容契约。
func containsDynamicMapType(expr ast.Expr) bool {
	// typedExpr 是当前递归检查的 AST 类型表达式。
	switch typedExpr := expr.(type) {
	case *ast.MapType:
		return isAnyType(typedExpr.Value)
	case *ast.ArrayType:
		return containsDynamicMapType(typedExpr.Elt)
	case *ast.StarExpr:
		return containsDynamicMapType(typedExpr.X)
	case *ast.StructType:
		// field 是当前结构体字段，需继续检查其元素类型。
		for _, field := range typedExpr.Fields.List {
			if containsDynamicMapType(field.Type) {
				return true
			}
		}
	}
	return false
}

// isAnyType 判断表达式是否为 Go 的 any 或空接口，用于识别无稳定字段边界的响应 map。
func isAnyType(expr ast.Expr) bool {
	// ident 是表达式的标识符形式；ok 表示断言成功。
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name == "any" || ident.Name == "interface{}"
	}
	// interfaceType 是表达式的接口类型形式；ok 表示断言成功。
	if interfaceType, ok := expr.(*ast.InterfaceType); ok {
		return interfaceType.Methods != nil && len(interfaceType.Methods.List) == 0
	}
	return false
}

// isWriteJSONCall 判断调用是否为 Server 的统一 JSON 响应写入函数。
func isWriteJSONCall(call *ast.CallExpr) bool {
	// functionName 是调用目标的简单函数名。
	functionName, ok := call.Fun.(*ast.Ident)
	return ok && functionName.Name == "writeJSON"
}

// isDynamicMapLiteral 判断表达式是否直接构造 map 响应，避免匿名契约逃逸静态检查。
func isDynamicMapLiteral(expr ast.Expr) bool {
	// composite 是表达式的复合字面量；ok 表示断言成功。
	composite, ok := expr.(*ast.CompositeLit)
	if !ok {
		return false
	}
	// isMap 表示复合字面量是否直接构造动态 map。
	_, isMap := composite.Type.(*ast.MapType)
	return isMap
}

// checkApplicationTypeLeaks 检查应用 Port 是否泄露数据库、事务或 Server 类型。
func checkApplicationTypeLeaks(relativePath string, syntax *ast.File, fset *token.FileSet) []violation {
	if !strings.HasPrefix(filepath.ToSlash(relativePath), "internal/application/") {
		return nil
	}
	// violations 保存当前应用文件发现的类型泄露。
	var violations []violation
	ast.Inspect(syntax, func(node ast.Node) bool {
		switch typedNode /* typedNode 是当前应用声明中的 AST 类型节点。 */ := node.(type) {
		case *ast.SelectorExpr:
			// packageName、typeName 保存选择器左侧包名和右侧类型名。
			packageName, ok := typedNode.X.(*ast.Ident)
			if !ok {
				return true
			}
			// typeName 是选择器右侧的类型或字段名称。
			typeName := typedNode.Sel.Name
			if (packageName.Name == "sql" && typeName == "Tx") || packageName.Name == "db" {
				violations = append(violations, violation{
					file: filepath.ToSlash(relativePath), line: fset.Position(typedNode.Pos()).Line,
					message: fmt.Sprintf("应用 Port 禁止暴露基础设施类型 %s.%s", packageName.Name, typeName),
				})
			}
		case *ast.StarExpr:
			// ident 是指针类型的目标标识符。
			ident, ok := typedNode.X.(*ast.Ident)
			if ok && ident.Name == "Server" {
				violations = append(violations, violation{
					file: filepath.ToSlash(relativePath), line: fset.Position(typedNode.Pos()).Line,
					message: "应用 Port 禁止暴露 *Server 类型",
				})
			}
		}
		return true
	})
	return violations
}

// normalizeImportPath 去除当前模块前缀，统一架构规则使用的内部包路径。
func normalizeImportPath(importedPath string) string {
	return strings.TrimPrefix(importedPath, "xianyu-go/")
}

// isForbiddenApplicationImport 判断应用层是否依赖了基础设施或 HTTP 层。
func isForbiddenApplicationImport(filePath, importedPath string) bool {
	if !strings.HasPrefix(filePath, "internal/application/") {
		return false
	}
	for _, forbidden /* forbidden 是应用层禁止依赖的包前缀。 */ := range []string{
		"internal/db", "internal/server", "internal/xianyu", "internal/browser",
		"database/sql", "net/http", "github.com/go-chi/chi",
	} {
		if importedPath == forbidden || strings.HasPrefix(importedPath, forbidden+"/") {
			return true
		}
	}
	return false
}

// isForbiddenServerLowLevelImport 判断 Server 是否新增了未登记的基础设施依赖。
func isForbiddenServerLowLevelImport(filePath, importedPath string) bool {
	if !strings.HasPrefix(filePath, "internal/server/") || strings.HasSuffix(filePath, "_test.go") {
		return false
	}
	if importedPath != "database/sql" && importedPath != "internal/db" && !strings.HasPrefix(importedPath, "internal/db/") &&
		importedPath != "internal/xianyu" && !strings.HasPrefix(importedPath, "internal/xianyu/") &&
		importedPath != "internal/browser" && !strings.HasPrefix(importedPath, "internal/browser/") {
		return false
	}
	return true
}

// isForbiddenLowLevelImport 判断低层包是否依赖了上层应用包。
func isForbiddenLowLevelImport(filePath, importedPath string) bool {
	// lowLevelPackage 标识当前文件所属的低层包。
	lowLevelPackage := ""
	switch {
	case strings.HasPrefix(filePath, "internal/db/"):
		lowLevelPackage = "internal/db/"
	case strings.HasPrefix(filePath, "internal/xianyu/"):
		lowLevelPackage = "internal/xianyu/"
	case strings.HasPrefix(filePath, "internal/browser/"):
		lowLevelPackage = "internal/browser/"
	}
	if lowLevelPackage == "" || strings.HasPrefix(importedPath, lowLevelPackage) {
		return false
	}
	// upperPackage 是禁止被低层包依赖的上层包路径。
	for _, upperPackage := range []string{
		"internal/server",
		"internal/adapter",
		"internal/account",
		"internal/automation",
		"internal/engine",
		"internal/chat",
		"internal/notify",
		"internal/auth",
	} {
		if importedPath == upperPackage || strings.HasPrefix(importedPath, upperPackage+"/") {
			return true
		}
	}
	return false
}

// firstLineContaining 返回文本中首次出现目标字符串的行号。
func firstLineContaining(source, target string) int {
	// offset 是目标字符串在原文中的字节偏移。
	offset := strings.Index(source, target)
	if offset < 0 {
		return 0
	}
	return 1 + strings.Count(source[:offset], "\n")
}
