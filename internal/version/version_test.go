package version

import "testing"

// TestShortCommit 负责TestShortCommit相关处理。
func TestShortCommit(t *testing.T) {
	// original 保存original，供当前处理流程使用
	original := Commit
	t.Cleanup(func() { Commit = original })

	Commit = "0123456789abcdef"
	if // got 保存got，供当前处理流程使用
	got := ShortCommit(); got != "0123456789ab" {
		t.Fatalf("ShortCommit() = %q", got)
	}
}
