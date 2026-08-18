package browser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolvePersistentUserDataDir prepares the profile directory required by
// Playwright's LaunchPersistentContext. playwright-go rejects relative paths.
// resolvePersistentUserDataDir 封装resolvePersistent用户数据Dir业务协调。
func resolvePersistentUserDataDir(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("持久化浏览器用户数据目录不能为空")
	}

	// absPath、err 用于本次流程后续判断的absPath、err
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("解析持久化浏览器用户数据目录失败: %w", err)
	}
	absPath = filepath.Clean(absPath)
	if !filepath.IsAbs(absPath) {
		return "", fmt.Errorf("持久化浏览器用户数据目录不是绝对路径: %s", absPath)
	}
	if // err 用于本次流程后续判断的err
	err := os.MkdirAll(absPath, 0o700); err != nil {
		return "", fmt.Errorf("创建持久化浏览器用户数据目录失败: %w", err)
	}
	return absPath, nil
}
