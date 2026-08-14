//go:build !windows && !darwin

package main

import (
	"fmt"
	"path/filepath"
)

// serviceAction 负责service动作相关处理。
func serviceAction(action string) error {
	return fmt.Errorf("当前平台不支持托盘服务操作: %s", action)
}

// quitTray 负责quitTray相关处理。
func quitTray() error { return nil }

// logDirectoryPath 负责logDirectory路径相关处理。
func logDirectoryPath() (string, error) {
	return filepath.Join("/var", "log", "ydisks-xianyu-helper"), nil
}
