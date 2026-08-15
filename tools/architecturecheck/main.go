// architecturecheck 检查 Go 依赖方向、应用 Port 边界和 Server 裸事务入口。
package main

import (
	"fmt"
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
	syntax, err := parser.ParseFile(fset, filePath, source, parser.ImportsOnly)
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
	if importedPath != "internal/db" && !strings.HasPrefix(importedPath, "internal/db/") &&
		importedPath != "internal/xianyu" && !strings.HasPrefix(importedPath, "internal/xianyu/") &&
		importedPath != "internal/browser" && !strings.HasPrefix(importedPath, "internal/browser/") {
		return false
	}
	return !temporaryServerLowLevelAllowlist[filePath]
}

// temporaryServerLowLevelAllowlist 是审计重开前已存在的 Server 低层依赖临时白名单。
// 每迁移一个业务域，必须删除对应文件条目；新文件不得加入白名单代替迁移。
var temporaryServerLowLevelAllowlist = map[string]bool{
	"internal/server/account_login_repository.go":    true,
	"internal/server/account_login_service.go":       true,
	"internal/server/account_task_handlers.go":       true,
	"internal/server/analytics_handlers.go":          true,
	"internal/server/analytics_repository.go":        true,
	"internal/server/analytics_service.go":           true,
	"internal/server/api_contract.go":                true,
	"internal/server/auth_handlers.go":               true,
	"internal/server/automation_handlers.go":         true,
	"internal/server/card_batch_handlers.go":         true,
	"internal/server/card_handlers.go":               true,
	"internal/server/chat_handlers.go":               true,
	"internal/server/communication_repository.go":    true,
	"internal/server/communication_service.go":       true,
	"internal/server/cookie_handlers.go":             true,
	"internal/server/default_reply_handlers.go":      true,
	"internal/server/item_handlers.go":               true,
	"internal/server/item_publish_batch_handlers.go": true,
	"internal/server/item_publish_images.go":         true,
	"internal/server/item_publish_repository.go":     true,
	"internal/server/item_publish_service.go":        true,
	"internal/server/keyword_handlers.go":            true,
	"internal/server/login_audit.go":                 true,
	"internal/server/mtop_cookie_session.go":         true,
	"internal/server/notification_handlers.go":       true,
	"internal/server/order_handlers.go":              true,
	"internal/server/order_repository.go":            true,
	"internal/server/order_service.go":               true,
	"internal/server/ownership_helpers.go":           true,
	"internal/server/platform_runtime.go":            true,
	"internal/server/server.go":                      true,
	"internal/server/settings_handlers.go":           true,
	"internal/server/success_contract.go":            true,
	"internal/server/transaction_repository.go":      true,
	"internal/server/qrlogin_handlers.go":            true,
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
