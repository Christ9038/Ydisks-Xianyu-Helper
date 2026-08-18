package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	// architectureStageBaseline 表示始终启用的依赖方向、API 契约和隐藏依赖基础门禁。
	architectureStageBaseline = 1
	// architectureStageServerComposition 在阶段二启用 Server 组合根与平台旁路门禁。
	architectureStageServerComposition = 2
	// architectureStageLifecycle 在阶段三启用后台任务 Context 与生命周期所有权门禁。
	architectureStageLifecycle = 3
	// architectureStageReact 在阶段四启用 React feature、契约与网络访问边界门禁。
	architectureStageReact = 4
	// architectureStageDatabase 在阶段五启用上层裸数据库与事务泄露门禁。
	architectureStageDatabase = 5
	// architectureStageClosure 在阶段六启用复杂度、超大文件与最终兼容收口门禁。
	architectureStageClosure = 6
)

// architectureGateDefinition 描述一组预先建立的架构规则及其最早激活阶段。
type architectureGateDefinition struct {
	// name 是稳定的门禁标识，供测试和失败诊断使用。
	name string
	// activationStage 是该门禁开始阻断合并的正式阶段编号。
	activationStage int
	// description 是该门禁保护的架构边界摘要。
	description string
}

// architectureGateCatalog 是六阶段重构使用的完整门禁目录；进入阶段只改变激活范围，不临时发明规则。
var architectureGateCatalog = []architectureGateDefinition{
	{name: "baseline-boundaries", activationStage: architectureStageBaseline, description: "依赖方向、API 契约、隐藏依赖与兼容登记"},
	{name: "server-composition", activationStage: architectureStageServerComposition, description: "Server 组合根、平台旁路与生命周期反转"},
	{name: "worker-lifecycle", activationStage: architectureStageLifecycle, description: "后台任务 Context、取消与静态所有权清单"},
	{name: "react-feature-boundary", activationStage: architectureStageReact, description: "React feature、传输契约、网络与动态导入边界"},
	{name: "database-boundary", activationStage: architectureStageDatabase, description: "上层裸数据库、事务与持久化模型泄露"},
	{name: "quality-closure", activationStage: architectureStageClosure, description: "复杂度、超大文件、兼容登记与最终架构收口"},
}

// architectureStageEnabled 判断指定门禁是否已随当前阶段开启；前序阶段门禁不会再次关闭。
func architectureStageEnabled(activeStage, activationStage int) bool {
	return activeStage >= activationStage
}

// readActiveArchitectureStage 从唯一总计划状态表读取当前阶段，拒绝缺失、重复或越界声明。
func readActiveArchitectureStage(root string) (int, error) {
	// planPath 是唯一允许声明当前阶段的总计划文件。
	planPath := filepath.Join(root, "docs", "architecture", "refactoring-master-plan.md")
	// raw、readErr 分别是总计划原文及读取失败原因。
	raw, readErr := os.ReadFile(planPath)
	if readErr != nil {
		return 0, fmt.Errorf("读取重构总计划失败: %w", readErr)
	}
	// currentPattern 匹配状态表中唯一标记为当前阶段的阶段编号。
	currentPattern := regexp.MustCompile(`(?m)^\|\s*([1-6])\.[^|]*\|\s*当前阶段\s*\|`)
	// matches 保存状态表中的全部当前阶段匹配，必须且只能存在一项。
	matches := currentPattern.FindAllSubmatch(raw, -1)
	if len(matches) == 0 {
		// completedPattern 只允许在阶段六已经明确完成后保持全量门禁永久开启。
		completedPattern := regexp.MustCompile(`(?m)^\|\s*6\.[^|]*\|\s*已完成\s*\|`)
		if completedPattern.Match(raw) {
			return architectureStageClosure, nil
		}
	}
	if len(matches) != 1 {
		return 0, fmt.Errorf("重构总计划必须且只能声明一个当前阶段，实际匹配 %d 项", len(matches))
	}
	// stage、parseErr 分别是当前阶段编号及数字解析失败原因。
	stage, parseErr := strconv.Atoi(string(matches[0][1]))
	if parseErr != nil || stage < architectureStageBaseline || stage > architectureStageClosure {
		return 0, fmt.Errorf("无效的当前架构阶段 %q", matches[0][1])
	}
	return stage, nil
}

// checkActivatedRepositoryGates 执行当前阶段及全部前序阶段已经激活的仓库级规则。
func checkActivatedRepositoryGates(root string, activeStage int) []violation {
	// violations 保存后续阶段规则在当前激活范围内发现的全部违规。
	var violations []violation
	if architectureStageEnabled(activeStage, architectureStageLifecycle) {
		violations = append(violations, checkLifecycleArchitecture(root)...)
	}
	if architectureStageEnabled(activeStage, architectureStageReact) {
		violations = append(violations, checkReactArchitecture(root)...)
	}
	if architectureStageEnabled(activeStage, architectureStageDatabase) {
		violations = append(violations, checkDatabaseArchitecture(root)...)
	}
	if architectureStageEnabled(activeStage, architectureStageClosure) {
		violations = append(violations, checkQualityArchitecture(root)...)
	}
	return violations
}

// checkLifecycleArchitecture 禁止后台组件使用脱离所有者的根 Context，并要求静态清单保留完整边界字段。
func checkLifecycleArchitecture(root string) []violation {
	// violations 保存阶段三生命周期边界违规。
	var violations []violation
	// lifecycleRoots 是拥有后台 worker 或慢外部调用的生产包前缀。
	lifecycleRoots := []string{"internal/account/", "internal/automation/", "internal/browser/", "internal/engine/", "internal/notify/", "internal/renewal/"}
	violations = append(violations, scanGoProductionFiles(root, func(relativePath string, source []byte) []violation {
		// frozen 表示受冻结 CAPTCHA 规范保护的实现；阶段三不得以生命周期迁移修改其调用链。
		// frozen 表示当前文件是否受冻结 CAPTCHA 规范保护，阶段三不允许修改其调用链。
		if frozen := isFrozenCaptchaLifecyclePath(relativePath); frozen {
			return nil
		}
		// inLifecyclePackage 表示当前文件是否属于需要显式继承所有者 Context 的生产包。
		inLifecyclePackage := false
		// lifecycleRoot 是当前待匹配的后台组件包前缀。
		for _, lifecycleRoot := range lifecycleRoots {
			if strings.HasPrefix(relativePath, lifecycleRoot) {
				inLifecyclePackage = true
				break
			}
		}
		if !inLifecyclePackage {
			return nil
		}
		// fset 是将 AST 位置转换为稳定源码行号的文件集合。
		fset := token.NewFileSet()
		// syntax、parseErr 分别是当前后台源码 AST 及其解析错误。
		syntax, parseErr := parser.ParseFile(fset, relativePath, source, 0)
		if parseErr != nil {
			return []violation{{file: relativePath, line: 1, message: fmt.Sprintf("生命周期门禁无法解析生产源码: %v", parseErr)}}
		}
		return checkUnboundedRootContexts(relativePath, syntax, fset)
	})...)
	// inventoryPath 是必须描述 owner、Context、Cancel 与 Wait/Join 的生命周期清单。
	inventoryPath := filepath.Join(root, "docs", "architecture", "lifecycle-inventory.md")
	// inventory、readErr 分别是生命周期清单原文及读取失败原因。
	inventory, readErr := os.ReadFile(inventoryPath)
	if readErr != nil {
		return append(violations, violation{file: "docs/architecture/lifecycle-inventory.md", line: 1, message: fmt.Sprintf("无法读取生命周期清单: %v", readErr)})
	}
	// requiredTerm 是生命周期清单不可删除的所有权与收束语义。
	for _, requiredTerm := range []string{"所有者", "Context 来源", "停止/关闭", "等待/观测", "Wait/Join", "锁顺序"} {
		if !strings.Contains(string(inventory), requiredTerm) {
			violations = append(violations, violation{file: "docs/architecture/lifecycle-inventory.md", line: 1, message: fmt.Sprintf("生命周期清单缺少必填边界 %q", requiredTerm)})
		}
	}
	return violations
}

// isFrozenCaptchaLifecyclePath 判断文件是否属于冻结 CAPTCHA 实现；它们只能由冻结规范授权修改。
func isFrozenCaptchaLifecyclePath(relativePath string) bool {
	// frozenPaths 是规范直接保护的生产实现文件，不包含可自由重构的 browser 生命周期组件。
	frozenPaths := map[string]struct{}{
		"internal/browser/slider.go":                 {},
		"internal/browser/token_captcha.go":          {},
		"internal/browser/token_captcha_fallback.go": {},
	}
	// frozen 表示当前路径是否命中冻结 CAPTCHA 的直接保护范围。
	_, frozen := frozenPaths[relativePath]
	return frozen
}

// checkUnboundedRootContexts 拒绝后台源码把 Background 或 TODO 直接传入异步、I/O 或关闭链。
// 唯一允许的根 Context 形态是 context.WithTimeout/WithDeadline 创建的显式有限收口预算。
func checkUnboundedRootContexts(relativePath string, syntax *ast.File, fset *token.FileSet) []violation {
	// boundedRoots 保存已经作为有限超时或截止时间父 Context 的根 Context 调用位置。
	boundedRoots := make(map[token.Pos]struct{})
	ast.Inspect(syntax, func(node ast.Node) bool {
		// call、ok 分别是当前节点是否为函数调用及其断言结果。
		call, ok := node.(*ast.CallExpr)
		if !ok || !isBoundedContextConstructor(call) || len(call.Args) == 0 {
			return true
		}
		// rootCall、rootContext 分别是有限 Context 构造函数的父 Context 调用及其是否为根 Context。
		rootCall, rootContext := call.Args[0].(*ast.CallExpr)
		if rootContext && isRootContextCall(rootCall) {
			boundedRoots[rootCall.Pos()] = struct{}{}
		}
		return true
	})
	// violations 保存未继承 owner 且未声明有限预算的根 Context 位置。
	var violations []violation
	ast.Inspect(syntax, func(node ast.Node) bool {
		// call、ok 分别是当前节点是否为函数调用及其断言结果。
		call, ok := node.(*ast.CallExpr)
		if !ok || !isRootContextCall(call) {
			return true
		}
		// bounded 表示当前根 Context 已作为 WithTimeout 或 WithDeadline 的有限父 Context。
		if _, bounded := boundedRoots[call.Pos()]; bounded {
			return true
		}
		violations = append(violations, violation{file: relativePath, line: fset.Position(call.Pos()).Line, message: "后台组件根 Context 必须继承 owner；仅显式 WithTimeout/WithDeadline 的有限收口预算可使用 Background"})
		return true
	})
	return violations
}

// isRootContextCall 判断调用是否为标准库的 Background 或 TODO 根 Context 构造。
func isRootContextCall(call *ast.CallExpr) bool {
	// selector、ok 分别是调用函数的包选择器及其断言结果。
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	// packageName、ok 分别是选择器所属包标识符及其断言结果。
	packageName, ok := selector.X.(*ast.Ident)
	return ok && packageName.Name == "context" && (selector.Sel.Name == "Background" || selector.Sel.Name == "TODO")
}

// isBoundedContextConstructor 判断调用是否显式创建可证明带截止时间的 Context。
func isBoundedContextConstructor(call *ast.CallExpr) bool {
	// selector、ok 分别是调用函数的包选择器及其断言结果。
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	// packageName、ok 分别是选择器所属包标识符及其断言结果。
	packageName, ok := selector.X.(*ast.Ident)
	return ok && packageName.Name == "context" && (selector.Sel.Name == "WithTimeout" || selector.Sel.Name == "WithDeadline")
}

// checkReactArchitecture 检查 feature 物理边界、契约直连、网络旁路、动态导入和严格类型选项。
func checkReactArchitecture(root string) []violation {
	// violations 保存阶段四前端架构违规。
	var violations []violation
	// frontendRoot 是 React/Vite 源码根目录。
	frontendRoot := filepath.Join(root, "frontend")
	// forbiddenFiles 是阶段四必须物理删除或迁入 feature 的集中入口。
	forbiddenFiles := []string{"services/amapLocation.ts", "shared/api-contract/index.ts"}
	// forbiddenFile 是当前待确认已删除的前端集中入口。
	for _, forbiddenFile := range forbiddenFiles {
		// statErr 表示当前集中入口是否已经完成物理迁移或删除。
		if _, statErr := os.Stat(filepath.Join(frontendRoot, filepath.FromSlash(forbiddenFile))); statErr == nil {
			violations = append(violations, violation{file: "frontend/" + forbiddenFile, line: 1, message: "阶段四禁止根级服务或大型契约 barrel；必须迁入所属 feature 或领域契约模块"})
		}
	}
	// modulePattern 提取静态 import、re-export 和动态 import 的模块路径。
	modulePattern := regexp.MustCompile(`(?m)(?:from\s+|import\s*\()\s*["']([^"']+)["']`)
	// walkErr 是遍历前端生产源码时的文件系统错误。
	walkErr := filepath.WalkDir(frontendRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == "node_modules" || entry.Name() == "coverage" || entry.Name() == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		if !isFrontendProductionPath(path) {
			return nil
		}
		// relativePath 是当前源码相对于 frontend 的稳定斜杠路径。
		relativePath, relativeErr := filepath.Rel(frontendRoot, path)
		if relativeErr != nil {
			return relativeErr
		}
		relativePath = filepath.ToSlash(relativePath)
		// source、readErr 分别是当前前端源码及读取失败原因。
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		// sourceText 是用于模块和网络边界匹配的源码文本。
		sourceText := string(source)
		if relativePath != "shared/http/client.ts" && (strings.Contains(sourceText, "fetch(") || regexp.MustCompile(`\baxios\b`).MatchString(sourceText)) {
			violations = append(violations, violation{file: "frontend/" + relativePath, line: 1, message: "前端生产代码不得绕过 shared/http/client 直接请求网络"})
		}
		if relativePath != "app/shell/AuthenticatedShell.tsx" && strings.Contains(sourceText, "import(") {
			violations = append(violations, violation{file: "frontend/" + relativePath, line: 1, message: "动态 import 只能用于路由级页面加载，禁止隐藏 feature 依赖"})
		}
		// matches 是当前源码中全部模块路径声明。
		matches := modulePattern.FindAllStringSubmatch(sourceText, -1)
		// match 是当前待检查的模块路径匹配结果。
		for _, match := range matches {
			// specifier 是 import/export 声明引用的模块路径。
			specifier := match[1]
			if strings.HasSuffix(specifier, "shared/api-contract") {
				violations = append(violations, violation{file: "frontend/" + relativePath, line: sourceLineAt(source, strings.Index(sourceText, match[0])), message: "feature 必须直接导入领域契约模块，禁止依赖 shared/api-contract barrel"})
			}
			violations = append(violations, checkCrossFeatureImport(relativePath, specifier)...)
		}
		return nil
	})
	if walkErr != nil {
		violations = append(violations, violation{file: "frontend", line: 1, message: fmt.Sprintf("扫描前端架构失败: %v", walkErr)})
	}
	// tsconfigPath 是必须启用未使用符号检查的前端 TypeScript 配置。
	tsconfigPath := filepath.Join(frontendRoot, "tsconfig.json")
	// tsconfigBytes、readErr 分别是 TypeScript 配置原文及读取失败原因。
	tsconfigBytes, readErr := os.ReadFile(tsconfigPath)
	if readErr != nil {
		return append(violations, violation{file: "frontend/tsconfig.json", line: 1, message: fmt.Sprintf("无法读取 TypeScript 配置: %v", readErr)})
	}
	// tsconfig 是用于读取 compilerOptions 的最小结构化配置。
	var tsconfig struct {
		CompilerOptions map[string]any `json:"compilerOptions"`
	}
	// parseErr 表示 TypeScript JSON 配置无法结构化解析的原因。
	if parseErr := json.Unmarshal(tsconfigBytes, &tsconfig); parseErr != nil {
		return append(violations, violation{file: "frontend/tsconfig.json", line: 1, message: fmt.Sprintf("无法解析 TypeScript 配置: %v", parseErr)})
	}
	// optionName 是当前必须开启的 TypeScript 未使用符号检查选项。
	for _, optionName := range []string{"noUnusedLocals", "noUnusedParameters"} {
		// enabled、ok 分别是配置值及其是否为布尔类型。
		if enabled, ok := tsconfig.CompilerOptions[optionName].(bool); !ok || !enabled {
			violations = append(violations, violation{file: "frontend/tsconfig.json", line: 1, message: fmt.Sprintf("阶段四必须启用 compilerOptions.%s", optionName)})
		}
	}
	return violations
}

// checkCrossFeatureImport 拒绝 feature 直接导入另一个 feature 的内部实现。
func checkCrossFeatureImport(sourcePath, specifier string) []violation {
	// sourceFeature 是当前源码所属的直属 feature 名称。
	sourceFeature := featureName(sourcePath)
	if sourceFeature == "" || !strings.HasPrefix(specifier, ".") {
		return nil
	}
	// resolved 是相对导入解析到 frontend 根下的稳定路径。
	resolved := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(sourcePath), specifier)))
	// targetFeature 是导入目标所属的直属 feature 名称。
	targetFeature := featureName(resolved)
	if targetFeature != "" && targetFeature != sourceFeature {
		return []violation{{file: "frontend/" + sourcePath, line: 1, message: fmt.Sprintf("feature %s 禁止导入 feature %s 的内部实现", sourceFeature, targetFeature)}}
	}
	return nil
}

// featureName 返回 app/features 路径下的直属 feature 名称。
func featureName(path string) string {
	// parts 是使用斜杠切分后的源码路径片段。
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) >= 3 && parts[0] == "app" && parts[1] == "features" {
		return parts[2]
	}
	return ""
}

// checkDatabaseArchitecture 禁止应用、Server 和业务运行时泄露裸 SQL 连接、事务或 Store.DB。
func checkDatabaseArchitecture(root string) []violation {
	// upperRoots 是阶段五不得操作裸数据库的上层生产包。
	upperRoots := []string{"internal/account/", "internal/application/", "internal/automation/", "internal/chat/", "internal/engine/", "internal/notify/", "internal/server/"}
	// rawDatabasePattern 匹配裸 SQL 类型、事务入口和 Store.DB 旁路。
	rawDatabasePattern := regexp.MustCompile(`\*sql\.(?:DB|Tx|Row|Rows|Stmt)\b|\.BeginTx\s*\(|\.DB\b`)
	return scanGoProductionFiles(root, func(relativePath string, source []byte) []violation {
		// isUpperLayer 表示当前文件是否属于禁止裸数据库访问的生产层。
		isUpperLayer := false
		// upperRoot 是当前待匹配的上层包前缀。
		for _, upperRoot := range upperRoots {
			if strings.HasPrefix(relativePath, upperRoot) {
				isUpperLayer = true
				break
			}
		}
		if !isUpperLayer {
			return nil
		}
		// match 是当前文件首次命中的裸数据库访问位置。
		match := rawDatabasePattern.FindIndex(source)
		if match == nil {
			return nil
		}
		return []violation{{file: relativePath, line: sourceLineAt(source, match[0]), message: "上层生产代码禁止暴露 sql.DB/sql.Tx、BeginTx 或 Store.DB；必须使用窄 repository 或应用 Unit of Work"}}
	})
}

// checkQualityArchitecture 对非冻结生产源码执行超大文件、超长函数和高分支复杂度收口。
func checkQualityArchitecture(root string) []violation {
	// violations 保存阶段六质量与可维护性违规。
	var violations []violation
	// frozenPaths 是冻结 CAPTCHA 规范明确保护、禁止借质量收口修改的精确文件集合。
	frozenPaths := map[string]struct{}{
		"internal/browser/slider.go":                 {},
		"internal/browser/token_captcha.go":          {},
		"internal/browser/token_captcha_fallback.go": {},
	}
	violations = append(violations, scanGoProductionFiles(root, func(relativePath string, source []byte) []violation {
		// frozen 表示当前文件是否受 CAPTCHA 冻结规范保护，禁止因质量阈值修改。
		if _, frozen := frozenPaths[relativePath]; frozen || isGeneratedSource(source) {
			return nil
		}
		// findings 保存当前生产 Go 文件的尺寸和函数复杂度违规。
		var findings []violation
		// lineCount 是当前生产文件的物理行数。
		lineCount := 1 + strings.Count(string(source), "\n")
		if lineCount > 800 {
			findings = append(findings, violation{file: relativePath, line: 1, message: fmt.Sprintf("生产 Go 文件超过 800 行（当前 %d 行），必须按业务职责拆分", lineCount)})
		}
		// fset 是当前文件函数位置和跨度的源码位置集合。
		fset := token.NewFileSet()
		// syntax、parseErr 分别是当前文件 AST 及解析失败原因。
		syntax, parseErr := parser.ParseFile(fset, relativePath, source, 0)
		if parseErr != nil {
			return append(findings, violation{file: relativePath, line: 1, message: fmt.Sprintf("复杂度扫描解析失败: %v", parseErr)})
		}
		// declaration 是当前待检查的顶层函数或方法声明。
		for _, declaration := range syntax.Decls {
			// function、ok 分别是当前声明是否为函数及断言结果。
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			// startLine 是函数声明起始行号。
			startLine := fset.Position(function.Pos()).Line
			// endLine 是函数声明结束行号，用于计算单函数物理跨度。
			endLine := fset.Position(function.End()).Line
			// branches 是函数内条件、循环、分支 case 与 select 的近似复杂度计数。
			branches := 1
			ast.Inspect(function.Body, func(node ast.Node) bool {
				switch node.(type) {
				case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.CaseClause, *ast.CommClause:
					branches++
				}
				return true
			})
			if endLine-startLine+1 > 180 || branches > 40 {
				findings = append(findings, violation{file: relativePath, line: startLine, message: fmt.Sprintf("函数 %s 过大或分支过多（%d 行，复杂度计数 %d），必须按业务责任拆分", function.Name.Name, endLine-startLine+1, branches)})
			}
		}
		return findings
	})...)
	return violations
}

// scanGoProductionFiles 遍历全部非测试 Go 生产源码，并把每个文件交给指定规则检查。
func scanGoProductionFiles(root string, inspect func(relativePath string, source []byte) []violation) []violation {
	// violations 保存遍历、读取与调用方规则产生的全部违规。
	var violations []violation
	// walkErr 是生产 Go 源码遍历失败原因。
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "node_modules" || entry.Name() == "coverage" || entry.Name() == "static" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// relativePath 是当前 Go 文件相对于仓库根目录的稳定斜杠路径。
		relativePath, relativeErr := filepath.Rel(root, path)
		if relativeErr != nil {
			return relativeErr
		}
		relativePath = filepath.ToSlash(relativePath)
		// source、readErr 分别是当前生产 Go 源码及读取失败原因。
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		violations = append(violations, inspect(relativePath, source)...)
		return nil
	})
	if walkErr != nil {
		violations = append(violations, violation{file: ".", line: 1, message: fmt.Sprintf("遍历生产 Go 源码失败: %v", walkErr)})
	}
	return violations
}

// isFrontendProductionPath 判断路径是否为需要架构扫描的 TypeScript/TSX 生产源码。
func isFrontendProductionPath(path string) bool {
	// extension 是当前前端源码扩展名。
	extension := filepath.Ext(path)
	if extension != ".ts" && extension != ".tsx" {
		return false
	}
	// base 是当前源码文件名，用于排除测试和规格文件。
	base := filepath.Base(path)
	return !strings.Contains(base, ".test.") && !strings.Contains(base, ".spec.")
}

// sourceLineAt 将源码字节偏移转换为一开始的行号，负偏移安全回退到首行。
func sourceLineAt(source []byte, offset int) int {
	if offset <= 0 {
		return 1
	}
	if offset > len(source) {
		offset = len(source)
	}
	return 1 + strings.Count(string(source[:offset]), "\n")
}

// isGeneratedSource 判断 Go 文件是否由工具生成，生成源码不接受人工复杂度重构。
func isGeneratedSource(source []byte) bool {
	// firstLines 是生成标记通常出现的文件头区域。
	firstLines := string(source)
	if len(firstLines) > 512 {
		firstLines = firstLines[:512]
	}
	return strings.Contains(firstLines, "Code generated") && strings.Contains(firstLines, "DO NOT EDIT")
}
