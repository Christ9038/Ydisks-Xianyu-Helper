package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestArchitectureGateCatalogActivatesMonotonically 验证所有阶段门禁均已预注册且只会随阶段递增开启。
func TestArchitectureGateCatalogActivatesMonotonically(t *testing.T) {
	// expectedStages 是六阶段计划中必须存在的门禁激活顺序。
	expectedStages := []int{architectureStageBaseline, architectureStageServerComposition, architectureStageLifecycle, architectureStageReact, architectureStageDatabase, architectureStageClosure}
	if len(architectureGateCatalog) != len(expectedStages) {
		t.Fatalf("门禁目录数量=%d，期望=%d", len(architectureGateCatalog), len(expectedStages))
	}
	// index 是当前门禁目录位置；expectedStage 是该位置必须对应的激活阶段。
	for index, expectedStage := range expectedStages {
		// gate 是当前顺序位置的完整门禁定义。
		gate := architectureGateCatalog[index]
		if gate.activationStage != expectedStage || gate.name == "" || gate.description == "" {
			t.Fatalf("门禁目录项=%+v，期望阶段=%d", gate, expectedStage)
		}
		if !architectureStageEnabled(expectedStage, gate.activationStage) || architectureStageEnabled(expectedStage-1, gate.activationStage) {
			t.Fatalf("门禁 %s 未遵守阶段激活边界", gate.name)
		}
	}
}

// TestReadActiveArchitectureStage 验证总计划缺失或重复当前阶段时门禁直接失败。
func TestReadActiveArchitectureStage(t *testing.T) {
	// root 是存放临时总计划的独立目录。
	root := t.TempDir()
	// planPath 是门禁读取的唯一状态文件位置。
	planPath := filepath.Join(root, "docs", "architecture", "refactoring-master-plan.md")
	// mkdirErr 表示创建临时总计划目录失败的文件系统原因。
	if mkdirErr := os.MkdirAll(filepath.Dir(planPath), 0o755); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	// validPlan 是只声明阶段四为当前阶段的最小合法状态表。
	validPlan := []byte("| 阶段 | 状态 | 说明 |\n| --- | --- | --- |\n| 4. React | 当前阶段 | x |\n")
	// writeErr 表示写入合法临时总计划失败的文件系统原因。
	if writeErr := os.WriteFile(planPath, validPlan, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	// stage、readErr 分别是合法状态表解析出的阶段号及错误。
	stage, readErr := readActiveArchitectureStage(root)
	if readErr != nil || stage != architectureStageReact {
		t.Fatalf("stage=%d err=%v", stage, readErr)
	}
	// duplicatePlan 是包含两个当前阶段标记的非法状态表。
	duplicatePlan := append(validPlan, []byte("| 5. DB | 当前阶段 | y |\n")...)
	// writeErr 表示写入重复阶段样例失败的文件系统原因。
	if writeErr := os.WriteFile(planPath, duplicatePlan, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	// duplicateErr 表示重复当前阶段是否被状态解析器明确拒绝。
	if _, duplicateErr := readActiveArchitectureStage(root); duplicateErr == nil {
		t.Fatal("重复当前阶段未被拒绝")
	}
	// completedPlan 是六阶段全部完成后必须继续保持全量门禁的最终状态表。
	completedPlan := []byte("| 阶段 | 状态 | 说明 |\n| --- | --- | --- |\n| 6. Closure | 已完成 | z |\n")
	// writeErr 表示写入最终完成状态样例失败的文件系统原因。
	if writeErr := os.WriteFile(planPath, completedPlan, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	// completedStage、completedErr 分别是最终状态解析结果及错误。
	completedStage, completedErr := readActiveArchitectureStage(root)
	if completedErr != nil || completedStage != architectureStageClosure {
		t.Fatalf("completed stage=%d err=%v", completedStage, completedErr)
	}
}

// TestLifecycleArchitectureGate 验证生命周期门禁会拒绝脱离 owner 的根 Context，并要求静态清单字段完整。
func TestLifecycleArchitectureGate(t *testing.T) {
	// root 是包含最小生命周期违规样例的临时仓库。
	root := t.TempDir()
	// workerPath 是模拟后台 worker 源码位置。
	workerPath := filepath.Join(root, "internal", "engine", "worker.go")
	// mkdirErr 表示创建临时 worker 目录失败的文件系统原因。
	if mkdirErr := os.MkdirAll(filepath.Dir(workerPath), 0o755); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	// writeErr 表示写入根 Context 违规样例失败的文件系统原因。
	if writeErr := os.WriteFile(workerPath, []byte("package engine\nimport (\"context\"; \"time\")\nvar _ = context.Background()\nvar _ = context.WithTimeout(context.Background(), time.Second)\n"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	// inventoryPath 是模拟生命周期清单位置。
	inventoryPath := filepath.Join(root, "docs", "architecture", "lifecycle-inventory.md")
	// mkdirErr 表示创建临时生命周期清单目录失败的文件系统原因。
	if mkdirErr := os.MkdirAll(filepath.Dir(inventoryPath), 0o755); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	// inventory 是包含所有强制生命周期字段的最小清单文本。
	inventory := []byte("所有者 Context 来源 停止/关闭 等待/观测 Wait/Join 锁顺序")
	// writeErr 表示写入临时生命周期清单失败的文件系统原因。
	if writeErr := os.WriteFile(inventoryPath, inventory, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	// violations 是生命周期门禁对根 Context 的阻断结果。
	violations := checkLifecycleArchitecture(root)
	if len(violations) != 1 || !strings.Contains(violations[0].message, "根 Context") {
		t.Fatalf("violations=%+v", violations)
	}
}

// TestReactArchitectureGate 验证 React 阶段门禁会拒绝集中契约、根级地图服务和未启用严格类型选项。
func TestReactArchitectureGate(t *testing.T) {
	// root 是包含最小前端边界违规样例的临时仓库。
	root := t.TempDir()
	// frontendRoot 是临时 React 源码根目录。
	frontendRoot := filepath.Join(root, "frontend")
	// mkdirErr 表示创建根级 services 目录失败的文件系统原因。
	if mkdirErr := os.MkdirAll(filepath.Join(frontendRoot, "services"), 0o755); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	// writeErr 表示写入根级地图服务违规样例失败的文件系统原因。
	if writeErr := os.WriteFile(filepath.Join(frontendRoot, "services", "amapLocation.ts"), []byte("export const x = 1;\n"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	// mkdirErr 表示创建集中契约目录失败的文件系统原因。
	if mkdirErr := os.MkdirAll(filepath.Join(frontendRoot, "shared", "api-contract"), 0o755); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	// writeErr 表示写入大型契约 barrel 违规样例失败的文件系统原因。
	if writeErr := os.WriteFile(filepath.Join(frontendRoot, "shared", "api-contract", "index.ts"), []byte("export {};\n"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	// mkdirErr 表示创建临时 items feature 目录失败的文件系统原因。
	if mkdirErr := os.MkdirAll(filepath.Join(frontendRoot, "app", "features", "items"), 0o755); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	// writeErr 表示写入契约和网络旁路违规样例失败的文件系统原因。
	if writeErr := os.WriteFile(filepath.Join(frontendRoot, "app", "features", "items", "bad.ts"), []byte("import { x } from '../../../shared/api-contract';\nvoid fetch('/x');\n"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	// writeErr 表示写入绕过 feature API adapter 的契约依赖样例失败的文件系统原因。
	if writeErr := os.WriteFile(filepath.Join(frontendRoot, "app", "features", "items", "ui.ts"), []byte("import type { Item } from '../../../shared/api-contract/items';\nexport type Row = Item;\n"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	// writeErr 表示写入缺少严格选项的 TypeScript 配置失败的文件系统原因。
	if writeErr := os.WriteFile(filepath.Join(frontendRoot, "tsconfig.json"), []byte("{\"compilerOptions\":{}}\n"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	// violations 是 React 阶段门禁发现的集中入口、网络旁路和类型配置违规。
	violations := checkReactArchitecture(root)
	if len(violations) < 5 {
		t.Fatalf("violations=%+v", violations)
	}
}

// TestDatabaseArchitectureGate 验证数据库阶段门禁会拒绝别名 SQL、Store.DB、事务入口和语法绕过。
func TestDatabaseArchitectureGate(t *testing.T) {
	// root 是包含上层裸 SQL 违规和合法 repository 样例的临时仓库。
	root := t.TempDir()
	// samples 保存各类上层裸 SQL 泄露、合法 repository 与无法解析源码的最小样例。
	samples := map[string]string{
		"internal/application/orders/sql_alias.go":   "package orders\nimport storeSQL \"database/sql\"\nfunc run(database *storeSQL.DB) { _, _ = database.BeginTx(nil, nil) }\n",
		"internal/server/store.go":                   "package server\nimport persistence \"xianyu-go/internal/db\"\nfunc run(store *persistence.Store) { _ = store.DB }\n",
		"internal/application/orders/transaction.go": "package orders\ntype unit struct{}\nfunc (unit) BeginTx() {}\nfunc run(transaction unit) { transaction.BeginTx() }\n",
		"internal/application/orders/broken.go":      "package orders\nfunc broken( {\n",
		"internal/adapter/store.go":                  "package adapter\nimport persistence \"xianyu-go/internal/db\"\ntype repository struct { store *persistence.Store }\n",
	}
	// relativePath、source 分别是当前样例的仓库相对路径和源代码。
	for relativePath, source := range samples {
		// samplePath 是当前样例在临时仓库中的完整路径。
		samplePath := filepath.Join(root, filepath.FromSlash(relativePath))
		// mkdirErr 表示创建当前样例目录失败的文件系统原因。
		if mkdirErr := os.MkdirAll(filepath.Dir(samplePath), 0o755); mkdirErr != nil {
			t.Fatal(mkdirErr)
		}
		// writeErr 表示写入当前边界样例失败的文件系统原因。
		if writeErr := os.WriteFile(samplePath, []byte(source), 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	// violations 是数据库阶段门禁发现的全部上层裸数据库泄露。
	violations := checkDatabaseArchitecture(root)
	// messages 保存违规文件到诊断文本的映射，便于同时断言覆盖范围与合法 adapter 例外边界。
	messages := make(map[string]string, len(violations))
	// finding 是当前待记录的数据库边界违规。
	for _, finding := range violations {
		messages[finding.file] = finding.message
	}
	if len(messages) != 4 || !strings.Contains(messages["internal/application/orders/sql_alias.go"], "上层生产代码") || !strings.Contains(messages["internal/server/store.go"], "Store.DB") || !strings.Contains(messages["internal/application/orders/transaction.go"], "事务") || !strings.Contains(messages["internal/application/orders/broken.go"], "无法解析") {
		t.Fatalf("violations=%+v", violations)
	}
	// adapterViolation 表示合法 db adapter 是否被数据库门禁错误阻断。
	if _, adapterViolation := messages["internal/adapter/store.go"]; adapterViolation {
		t.Fatalf("合法 db adapter 被错误阻断: %+v", violations)
	}
}

// TestQualityArchitectureGate 验证质量阶段门禁会拒绝超过阈值的非冻结生产文件。
func TestQualityArchitectureGate(t *testing.T) {
	// root 是包含超大生产源码样例的临时仓库。
	root := t.TempDir()
	// sourcePath 是模拟超大 Go 文件位置。
	sourcePath := filepath.Join(root, "internal", "application", "items", "large.go")
	// mkdirErr 表示创建临时生产源码目录失败的文件系统原因。
	if mkdirErr := os.MkdirAll(filepath.Dir(sourcePath), 0o755); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	// source 是超过 800 行但仍保持合法 Go 语法的生产源码。
	source := []byte("package items\n" + strings.Repeat("// 业务说明\n", 801))
	// writeErr 表示写入超大生产文件样例失败的文件系统原因。
	if writeErr := os.WriteFile(sourcePath, source, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	// violations 是质量阶段门禁发现的超大文件结果。
	violations := checkQualityArchitecture(root)
	if len(violations) != 1 || !strings.Contains(violations[0].message, "超过 800 行") {
		t.Fatalf("violations=%+v", violations)
	}
}
