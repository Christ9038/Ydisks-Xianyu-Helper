//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"strings"
)

func serviceAction(action string) error {
	name := envOr("XIANYU_SERVICE_NAME", "YdisksXianyuHelper")
	if action != "start" && action != "stop" && action != "restart" {
		return fmt.Errorf("未知服务操作: %s", action)
	}

	quotedName := strings.ReplaceAll(name, "'", "''")
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
function Invoke-Sc([string] $verb) {
  $process = Start-Process -FilePath "$env:SystemRoot\System32\sc.exe" -ArgumentList @($verb, '%s') -Verb RunAs -WindowStyle Hidden -Wait -PassThru
  if ($null -eq $process) { exit 1 }
  return $process.ExitCode
}
$exitCode = 0
switch ('%s') {
  'start' { $exitCode = Invoke-Sc 'start' }
  'stop' { $exitCode = Invoke-Sc 'stop' }
  'restart' {
    $stopCode = Invoke-Sc 'stop'
    if ($stopCode -ne 0 -and $stopCode -ne 1062) { exit $stopCode }
    $exitCode = Invoke-Sc 'start'
  }
}
exit $exitCode`, quotedName, action)

	cmd := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return fmt.Errorf("Windows 服务操作 %s 失败: %w", action, err)
		}
		return fmt.Errorf("Windows 服务操作 %s 失败: %s", action, message)
	}
	return nil
}
