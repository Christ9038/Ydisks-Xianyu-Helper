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
	Commit = "   "
	if // got 是空提交标识的稳定回退值。
	got := ShortCommit(); got != "unknown" {
		t.Fatalf("空提交标识回退值=%q", got)
	}
	Commit = "abc123"
	if // got 是短提交标识的原样返回值。
	got := ShortCommit(); got != "abc123" {
		t.Fatalf("短提交标识=%q", got)
	}
}
