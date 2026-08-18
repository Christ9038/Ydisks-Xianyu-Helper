package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFrontendProductionRequestsUseVersionedAPI 清点 React 生产源码，确保业务请求不再调用旧路径。
func TestFrontendProductionRequestsUseVersionedAPI(t *testing.T) {
	// root 是当前仓库根目录，测试从任意 Go 包工作目录向上解析它。
	root := repositoryRootForContractTest(t)
	// frontendRoot 是需要审计的前端源码目录。
	frontendRoot := filepath.Join(root, "frontend")
	// legacyFragments 是旧 HTTP 入口在字符串字面量中的稳定前缀集合。
	legacyFragments := []string{
		"'/login", `"/login`, "`/login",
		"'/verify", `"/verify`, "`/verify",
		"'/logout", `"/logout`, "`/logout",
		"'/change-password", `"/change-password`, "`/change-password",
		"'/account/credentials", `"/account/credentials`, "`/account/credentials",
		"'/cookies", `"/cookies`, "`/cookies",
		"'/items", `"/items`, "`/items",
		"'/automation-rules", `"/automation-rules`, "`/automation-rules",
		"'/automation-issues", `"/automation-issues`, "`/automation-issues",
		"'/automation-runs", `"/automation-runs`, "`/automation-runs",
		"'/automation-pending-tasks", `"/automation-pending-tasks`, "`/automation-pending-tasks",
		"'/api/orders", `"/api/orders`, "`/api/orders",
		"'/system-settings", `"/system-settings`, "`/system-settings",
		"'/user-settings", `"/user-settings`, "`/user-settings",
		"'/ai-reply-settings", `"/ai-reply-settings`, "`/ai-reply-settings",
		"'/ai-models", `"/ai-models`, "`/ai-models",
		"'/cards", `"/cards`, "`/cards",
		"'/notification-channels", `"/notification-channels`, "`/notification-channels",
		"'/message-notifications", `"/message-notifications`, "`/message-notifications",
		"'/api/chat", `"/api/chat`, "`/api/chat",
		"'/keywords", `"/keywords`, "`/keywords",
		"'/keywords-with-item-id", `"/keywords-with-item-id`, "`/keywords-with-item-id",
		"'/keywords-with-type", `"/keywords-with-type`, "`/keywords-with-type",
		"'/item-reply", `"/item-reply`, "`/item-reply",
		"'/itemReplays", `"/itemReplays`, "`/itemReplays",
		"'/default-replies", `"/default-replies`, "`/default-replies",
		"'/api/default-reply", `"/api/default-reply`, "`/api/default-reply",
		"'/api/account-tasks", `"/api/account-tasks`, "`/api/account-tasks",
		"'/admin/", `"/admin/`, "`/admin/",
		"'/change-admin-password", `"/change-admin-password`, "`/change-admin-password",
		"'/dashboard/stats", `"/dashboard/stats`, "`/dashboard/stats",
		"'/analytics/", `"/analytics/`, "`/analytics/",
		"'/qr-login", `"/qr-login`, "`/qr-login",
		"'/password-login", `"/password-login`, "`/password-login",
	}
	// findings 保存命中的文件、行号和旧路径片段。
	var findings []string
	// walkErr 表示遍历前端生产源码时遇到的文件系统错误。
	walkErr := filepath.WalkDir(frontendRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "node_modules" || entry.Name() == "coverage" || entry.Name() == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		if !isFrontendProductionSource(path) {
			return nil
		}
		// file 是当前待扫描的前端生产源码文件；openErr 表示打开失败原因。
		file, openErr := os.Open(path)
		if openErr != nil {
			return openErr
		}
		defer file.Close()
		// lineNumber 是当前源码行的 1-based 行号。
		lineNumber := 0
		// scanner 按行读取当前前端源码，供旧路径静态清点使用。
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			lineNumber++
			// line 是当前源码文本，用于检查是否包含历史 API 路径。
			line := scanner.Text()
			// fragment 是当前待检查的历史 API 路径片段。
			for _, fragment := range legacyFragments {
				if strings.Contains(line, fragment) {
					findings = append(findings, fmt.Sprintf("%s:%d 命中 %s", filepath.ToSlash(path), lineNumber, fragment))
				}
			}
		}
		return scanner.Err()
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}
	if len(findings) != 0 {
		t.Fatalf("前端生产源码存在旧 API 调用:\n%s", strings.Join(findings, "\n"))
	}
}

// TestCompatibilityMatrixDocumentsSunsetConditions 验证兼容清单保留调用方证据和删除条件。
func TestCompatibilityMatrixDocumentsSunsetConditions(t *testing.T) {
	// root 是当前仓库根目录，供读取架构契约文档使用。
	root := repositoryRootForContractTest(t)
	// matrixPath 是 API 兼容边界清单的绝对路径。
	matrixPath := filepath.Join(root, "docs", "architecture", "api-compatibility-matrix.md")
	// raw 是兼容清单原文，测试只检查治理字段是否仍被记录。
	raw, err := os.ReadFile(matrixPath)
	if err != nil {
		t.Fatal(err)
	}
	// requiredPhrases 是 API 调用方审计必须保留的证据和删除条件短语；正式阶段编号只由总计划定义。
	requiredPhrases := []string{
		"API 调用方审计",
		"TestFrontendProductionRequestsUseVersionedAPI",
		"外部调用方",
		"Sunset",
		"连续两个发布周期",
		"回滚",
	}
	for _, phrase := range requiredPhrases { // phrase 是当前待核对的治理短语。
		if !strings.Contains(string(raw), phrase) {
			t.Errorf("兼容清单缺少治理短语 %q", phrase)
		}
	}
}

// isFrontendProductionSource 判断文件是否属于需要审计的前端生产源码。
func isFrontendProductionSource(path string) bool {
	// ext 是当前文件扩展名，用于排除非 TypeScript 源码。
	ext := filepath.Ext(path)
	if ext != ".ts" && ext != ".tsx" {
		return false
	}
	// base 是当前文件名，用于排除测试与规格文件。
	base := filepath.Base(path)
	return !strings.Contains(base, ".test.") && !strings.Contains(base, ".spec.")
}

// repositoryRootForContractTest 从当前测试目录向上寻找包含 go.mod 的仓库根目录。
func repositoryRootForContractTest(t *testing.T) string {
	t.Helper()
	// current 是当前测试进程的工作目录绝对路径。
	current, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		// statErr 表示当前目录是否存在模块声明文件。
		if _, statErr := os.Stat(filepath.Join(current, "go.mod")); statErr == nil {
			return current
		}
		// parent 是向上搜索仓库根目录时的父目录。
		parent := filepath.Dir(current)
		if parent == current {
			t.Fatal("无法定位仓库根目录")
		}
		current = parent
	}
}
