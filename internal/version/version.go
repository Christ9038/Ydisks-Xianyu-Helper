// Package version exposes build metadata injected by release builds.
package version

import "strings"

// Version 用于本次流程后续判断的Version
var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// ShortCommit returns a compact commit identifier suitable for UI display.
// ShortCommit 封装ShortCommit业务协调。
func ShortCommit() string {
	// commit 用于本次流程后续判断的commit
	commit := strings.TrimSpace(Commit)
	if len(commit) > 12 {
		return commit[:12]
	}
	if commit == "" {
		return "unknown"
	}
	return commit
}
