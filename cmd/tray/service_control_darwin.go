//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func serviceAction(action string) error {
	label := envOr("XIANYU_SERVICE_NAME", "com.ydisks.xianyu-helper.server")
	uid := fmt.Sprint(os.Getuid())
	domain := "gui/" + uid
	target := domain + "/" + label
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("获取用户目录失败: %w", err)
	}
	plistPath := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	switch action {
	case "start":
		if err := launchctl("print", target); err != nil {
			if err := launchctl("bootstrap", domain, plistPath); err != nil {
				return err
			}
		}
		return launchctl("kickstart", target)
	case "stop":
		if err := launchctl("print", target); err != nil {
			return nil
		}
		return launchctl("bootout", target)
	case "restart":
		_ = launchctl("bootout", target)
		if err := launchctl("bootstrap", domain, plistPath); err != nil {
			return err
		}
		return launchctl("kickstart", target)
	default:
		return fmt.Errorf("未知服务操作: %s", action)
	}
}

func launchctl(args ...string) error {
	cmd := exec.Command("launchctl", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return fmt.Errorf("launchctl %s 失败: %w", strings.Join(args, " "), err)
		}
		return fmt.Errorf("launchctl %s 失败: %s", strings.Join(args, " "), message)
	}
	return nil
}
