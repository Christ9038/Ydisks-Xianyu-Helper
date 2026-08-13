// Package version exposes build metadata injected by release builds.
package version

import "strings"

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

// ShortCommit returns a compact commit identifier suitable for UI display.
func ShortCommit() string {
	commit := strings.TrimSpace(Commit)
	if len(commit) > 12 {
		return commit[:12]
	}
	if commit == "" {
		return "unknown"
	}
	return commit
}
