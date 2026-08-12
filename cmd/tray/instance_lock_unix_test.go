//go:build !windows

package main

import (
	"path/filepath"
	"testing"
)

func TestAcquireTrayInstanceRejectsSecondInstance(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "tray.lock")
	releaseFirst, acquired, err := acquireTrayFileLock(lockPath)
	if err != nil {
		t.Fatalf("acquire first tray instance: %v", err)
	}
	if !acquired {
		t.Fatal("first tray instance should acquire lock")
	}
	defer releaseFirst()

	releaseSecond, acquired, err := acquireTrayFileLock(lockPath)
	if err != nil {
		t.Fatalf("acquire second tray instance: %v", err)
	}
	defer releaseSecond()
	if acquired {
		t.Fatal("second tray instance must be rejected")
	}
}
