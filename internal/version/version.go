// Package version exposes build metadata injected by release builds.
package version

import "strings"

// Version 保存Version，供当前处理流程使用
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// ShortCommit returns a compact commit identifier suitable for UI display.
// ShortCommit 负责ShortCommit相关处理。
func ShortCommit() string {
	// commit 保存commit，供当前处理流程使用
	commit := strings.TrimSpace(Commit)
	if len(commit) > 12 {
		return commit[:12]
	}
	if commit == "" {
		return "unknown"
	}
	return commit
}
