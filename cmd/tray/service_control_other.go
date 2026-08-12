//go:build !windows && !darwin

package main

import (
	"fmt"
	"path/filepath"
)

func serviceAction(action string) error {
	return fmt.Errorf("当前平台不支持托盘服务操作: %s", action)
}

func quitTray() error { return nil }

func logDirectoryPath() (string, error) {
	return filepath.Join("/var", "log", "ydisks-xianyu-helper"), nil
}
