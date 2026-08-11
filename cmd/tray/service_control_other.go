//go:build !windows && !darwin

package main

import "fmt"

func serviceAction(action string) error {
	return fmt.Errorf("当前平台不支持托盘服务操作: %s", action)
}

func quitTray() error { return nil }
