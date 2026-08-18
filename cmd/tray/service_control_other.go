//go:build !windows && !darwin

package main

import (
	"fmt"
	"path/filepath"
)

// serviceAction 封装service动作业务协调。
func serviceAction(action string) error {
	return fmt.Errorf("当前平台不支持托盘服务操作: %s", action)
}

// quitTray 封装quitTray业务协调。
func quitTray() error { return nil }

// logDirectoryPath 封装logDirectory路径业务协调。
func logDirectoryPath() (string, error) {
	return filepath.Join("/var", "log", "ydisks-xianyu-helper"), nil
}
