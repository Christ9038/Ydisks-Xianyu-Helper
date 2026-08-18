package version

import "testing"

// TestShortCommit 封装TestShortCommit业务协调。
func TestShortCommit(t *testing.T) {
	// original 用于本次流程后续判断的original
	original := Commit
	t.Cleanup(func() { Commit = original })

	Commit = "0123456789abcdef"
	if // got 用于本次流程后续判断的got
	got := ShortCommit(); got != "0123456789ab" {
		t.Fatalf("ShortCommit() = %q", got)
	}
}
