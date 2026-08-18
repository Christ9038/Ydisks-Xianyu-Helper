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
