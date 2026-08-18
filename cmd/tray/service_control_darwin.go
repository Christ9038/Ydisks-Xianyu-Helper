//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// serviceAction 封装service动作业务协调。
func serviceAction(action string) error {
	// label 用于本次流程后续判断的label
	label := envOr("XIANYU_SERVICE_NAME", "com.ydisks.xianyu-helper.server")
	// uid 用于本次流程后续判断的uid
	uid := fmt.Sprint(os.Getuid())
	// domain 用于本次流程后续判断的domain
	domain := "gui/" + uid
	// target 用于本次流程后续判断的target
	target := domain + "/" + label
	// home、err 用于本次流程后续判断的home、err
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("获取用户目录失败: %w", err)
	}
	// plistPath 用于本次流程后续判断的plist路径
	plistPath := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
	switch action {
	case "start":
		if // err 用于本次流程后续判断的err
		err := launchctl("print", target); err == nil {
			if // err 用于本次流程后续判断的err
			err := launchctl("kickstart", target); err == nil {
				return nil
			}
			// launchd 可能仍保留一个正在退出的旧 job；先完整卸载，
			// 等它消失后再 bootstrap，避免第二次启动报 Input/output error。
			_ = launchctl("bootout", target)
			if // err 用于本次流程后续判断的err
			err := waitForLaunchctlGone(target, 10*time.Second); err != nil {
				return err
			}
		}
		if // err 用于本次流程后续判断的err
		err := launchctl("bootstrap", domain, plistPath); err != nil {
			return err
		}
		return launchctl("kickstart", target)
	case "stop":
		if // err 用于本次流程后续判断的err
		err := launchctl("print", target); err != nil {
			return nil
		}
		if // err 用于本次流程后续判断的err
		err := launchctl("bootout", target); err != nil {
			return err
		}
		return waitForLaunchctlGone(target, 10*time.Second)
	case "restart":
		_ = launchctl("bootout", target)
		if // err 用于本次流程后续判断的err
		err := waitForLaunchctlGone(target, 10*time.Second); err != nil {
			return err
		}
		if // err 用于本次流程后续判断的err
		err := launchctl("bootstrap", domain, plistPath); err != nil {
			return err
		}
		return launchctl("kickstart", target)
	default:
		return fmt.Errorf("未知服务操作: %s", action)
	}
}

// waitForLaunchctlGone 封装waitForLaunchctlGone业务协调。
func waitForLaunchctlGone(target string, timeout time.Duration) error {
	// deadline 用于本次流程后续判断的deadline
	deadline := time.Now().Add(timeout)
	for {
		if // err 用于本次流程后续判断的err
		err := launchctl("print", target); err != nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("等待 LaunchAgent 退出超时: %s", target)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// quitTray 封装quitTray业务协调。
func quitTray() error {
	if // err 用于本次流程后续判断的err
	err := serviceAction("stop"); err != nil {
		return fmt.Errorf("停止后台服务失败: %w", err)
	}
	// 不要从托盘进程内部 bootout 自己的 LaunchAgent。launchctl 可能会先
	// 卸载 job 再留下当前进程，导致旧托盘残留而新托盘无法正确接管。
	// KeepAlive=false，随后由 systray.Quit() 正常退出进程即可。
	return nil
}

// logDirectoryPath 封装logDirectory路径业务协调。
func logDirectoryPath() (string, error) {
	// home、err 用于本次流程后续判断的home、err
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("获取用户目录失败: %w", err)
	}
	return filepath.Join(home, "Library", "Logs", "YdisksXianyuHelper"), nil
}

// launchctl 封装launchctl业务协调。
func launchctl(args ...string) error {
	// cmd 用于本次流程后续判断的cmd
	cmd := exec.Command("launchctl", args...)
	// output、err 用于本次流程后续判断的output、err
	output, err := cmd.CombinedOutput()
	if err != nil {
		// message 用于本次流程后续判断的消息
		message := strings.TrimSpace(string(output))
		if message == "" {
			return fmt.Errorf("launchctl %s 失败: %w", strings.Join(args, " "), err)
		}
		return fmt.Errorf("launchctl %s 失败: %s", strings.Join(args, " "), message)
	}
	return nil
}
