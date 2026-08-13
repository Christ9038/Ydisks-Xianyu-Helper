package version

import "testing"

func TestShortCommit(t *testing.T) {
	original := Commit
	t.Cleanup(func() { Commit = original })

	Commit = "0123456789abcdef"
	if got := ShortCommit(); got != "0123456789ab" {
		t.Fatalf("ShortCommit() = %q", got)
	}
}
