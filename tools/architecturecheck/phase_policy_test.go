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

// TestDatabaseArchitectureGate 验证数据库阶段门禁会拒绝上层裸事务与 Store.DB 访问。
func TestDatabaseArchitectureGate(t *testing.T) {
	// root 是包含最小裸数据库违规样例的临时仓库。
	root := t.TempDir()
	// servicePath 是模拟上层应用服务源码位置。
	servicePath := filepath.Join(root, "internal", "application", "orders", "service.go")
	// mkdirErr 表示创建临时应用服务目录失败的文件系统原因。
	if mkdirErr := os.MkdirAll(filepath.Dir(servicePath), 0o755); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	// writeErr 表示写入裸数据库违规样例失败的文件系统原因。
	if writeErr := os.WriteFile(servicePath, []byte("package orders\nimport \"database/sql\"\nfunc run(db *sql.DB) { db.BeginTx(nil, nil) }\n"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	// violations 是数据库阶段门禁发现的裸数据库访问结果。
	violations := checkDatabaseArchitecture(root)
	if len(violations) != 1 || !strings.Contains(violations[0].message, "上层生产代码") {
		t.Fatalf("violations=%+v", violations)
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
