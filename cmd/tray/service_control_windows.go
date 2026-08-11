//go:build windows

package main

import "os/exec"

func serviceAction(action string) error {
	name := envOr("XIANYU_SERVICE_NAME", "YdisksXianyuHelper")
	switch action {
	case "start", "stop":
		return exec.Command("sc.exe", action, name).Run()
	case "restart":
		if err := exec.Command("sc.exe", "stop", name).Run(); err != nil {
			return err
		}
		return exec.Command("sc.exe", "start", name).Run()
	default:
		return nil
	}
}
