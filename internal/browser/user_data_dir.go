package browser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolvePersistentUserDataDir prepares the profile directory required by
// Playwright's LaunchPersistentContext. playwright-go rejects relative paths.
// resolvePersistentUserDataDir 负责resolvePersistent用户数据Dir相关处理。
func resolvePersistentUserDataDir(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("持久化浏览器用户数据目录不能为空")
	}

	// absPath、err 保存absPath、err，供当前处理流程使用
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("解析持久化浏览器用户数据目录失败: %w", err)
	}
	absPath = filepath.Clean(absPath)
	if !filepath.IsAbs(absPath) {
		return "", fmt.Errorf("持久化浏览器用户数据目录不是绝对路径: %s", absPath)
	}
	if // err 保存err，供当前处理流程使用
	err := os.MkdirAll(absPath, 0o700); err != nil {
		return "", fmt.Errorf("创建持久化浏览器用户数据目录失败: %w", err)
	}
	return absPath, nil
}
