package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestTemplateFindingsInGoFile 验证模板审计器会定位阶段 10 禁止的占位注释，并忽略正常业务说明。
func TestTemplateFindingsInGoFile(t *testing.T) {
	// root 是承载独立 Go 源码夹具的临时目录。
	root := t.TempDir()
	// sourcePath 是写入模板与非模板注释混合夹具的路径。
	sourcePath := filepath.Join(root, "fixture.go")
	// source 是用于覆盖全部模板规则的最小 Go 源码文本。
	source := `package fixture
// value 保存 value，供当前处理流程使用
var value string
// doWork 负责 doWork 相关处理。
func doWork() {}
// err 表示错误
var err error
// count 表示数量
var count int
// callback 回调函数负责当前业务流程。
var callback = func() {}
// accountID 是所有权校验通过后的非敏感账号标识。
var accountID string
`
	// err 是创建临时夹具时的文件系统错误。
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		t.Fatalf("写入模板注释夹具失败: %v", err)
	}
	// findings、err 是夹具扫描后的模板记录及文件读取错误。
	findings, err := templateFindingsInGoFile(root, sourcePath)
	if err != nil {
		t.Fatalf("templateFindingsInGoFile error: %v", err)
	}
	if len(findings) != 5 {
		t.Fatalf("模板记录数=%d，want 5: %+v", len(findings), findings)
	}
	// expectedKinds 是每种模板规则应至少命中一次的类别集合。
	expectedKinds := map[string]bool{
		"保存当前处理流程": false,
		"负责相关处理":   false,
		"泛化错误说明":   false,
		"泛化数量说明":   false,
		"泛化回调职责":   false,
	}
	// finding 是当前待核验的模板扫描记录。
	for _, finding := range findings {
		// exists 表示当前扫描记录是否属于本测试登记的模板类别。
		if _, exists := expectedKinds[finding.Kind]; !exists {
			t.Fatalf("出现未登记模板类别: %+v", finding)
		}
		expectedKinds[finding.Kind] = true
	}
	// kind、found 表示当前模板类别及其实际命中状态。
	for kind, found := range expectedKinds {
		if !found {
			t.Errorf("未命中模板类别 %q", kind)
		}
	}
}
