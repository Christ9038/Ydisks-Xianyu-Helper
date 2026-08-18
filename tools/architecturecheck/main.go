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

// controlledDynamicResponses 是阶段 9 允许保留的最小动态响应登记表。
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
		fileViolations, err := checkGoFile(root, relativePath, fset)
		if err != nil {
			return err
		}
		violations = append(violations, fileViolations...)
		return nil
	})
	violations = append(violations, checkCompatibilityGovernance(root)...)
	return violations, walkErr
}

// checkGoFile 检查单个 Go 文件的导入方向和事务调用位置。
func checkGoFile(root, relativePath string, fset *token.FileSet) ([]violation, error) {
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
				message: fmt.Sprintf("Server 新增低层依赖必须先迁移到应用 Port 或登记临时白名单 %q", importedPath),
			})
		}
	}
	violations = append(violations, checkApplicationTypeLeaks(relativePath, syntax, fset)...)
	violations = append(violations, checkHTTPResponseContracts(relativePath, syntax, fset)...)
	violations = append(violations, checkHTTPRequestContracts(relativePath, syntax, fset)...)
	violations = append(violations, checkRuntimeSetterCalls(relativePath, syntax, fset)...)
	violations = append(violations, checkServerCompositionCalls(relativePath, syntax, fset)...)
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
		// message 是命中构造函数后返回的组合根迁移提示。
		message, forbidden := forbiddenConstructors[selector.Sel.Name]
		if !forbidden {
			return true
		}
		violations = append(violations, violation{
			file:    normalizedPath,
			line:    fset.Position(call.Pos()).Line,
			message: message,
		})
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

// isControlledDynamicResponseType 判断已登记的动态键兼容响应，避免本阶段改变旧客户端 JSON 形状。
func isControlledDynamicResponseType(name string) bool {
	// ok 表示响应类型是否已在阶段 9 的兼容登记表中备案。
	_, ok := controlledDynamicResponses[name]
	return ok
}

// isForbiddenHiddenDependencyImport 禁止应用与 Server 通过反射、插件机制或动态加载隐藏必需依赖。
func isForbiddenHiddenDependencyImport(filePath, importedPath string) bool {
	// productionLayer 表示阶段 9 需要封死隐式装配旁路的生产层。
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

// checkCompatibilityGovernance 校验动态响应白名单、Sunset 版本与运行时遥测保持同步。
func checkCompatibilityGovernance(root string) []violation {
	// matrixPath 是记录外部调用方、删除条件和 Sunset 版本的兼容矩阵路径。
	matrixPath := filepath.Join(root, "docs", "architecture", "api-compatibility-matrix.md")
	// matrixBytes 是兼容矩阵原文，用于避免白名单脱离文档治理。
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
