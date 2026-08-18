package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

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
