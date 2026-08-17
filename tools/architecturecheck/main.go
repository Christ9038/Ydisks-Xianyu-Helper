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
	// 这些类型的键来自账号 ID、触发类型或设置名称；只有完成外部调用方审计后才允许在后续阶段删除。
	switch name {
	case "settingsResponse", "notificationBindingListResponse", "automationRulePageResponse":
		return true
	default:
		return false
	}
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
