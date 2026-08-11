//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
)

func serviceAction(action string) error {
	label := envOr("XIANYU_SERVICE_NAME", "com.christ.ydisks-xianyu-helper")
	uid := fmt.Sprint(os.Getuid())
	target := "gui/" + uid + "/" + label
	switch action {
	case "start":
		return exec.Command("launchctl", "kickstart", target).Run()
	case "stop":
		return exec.Command("launchctl", "kill", "SIGTERM", target).Run()
	case "restart":
		return exec.Command("launchctl", "kickstart", "-k", target).Run()
	default:
		return nil
	}
}
