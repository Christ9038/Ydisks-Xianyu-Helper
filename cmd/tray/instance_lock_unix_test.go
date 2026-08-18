//go:build !windows

package main

import (
	"path/filepath"
	"testing"
)

// TestAcquireTrayInstanceRejectsSecondInstance 封装TestAcquireTrayInstanceRejectsSecondInstance业务协调。
func TestAcquireTrayInstanceRejectsSecondInstance(t *testing.T) {
	// lockPath 用于本次流程后续判断的锁路径
	lockPath := filepath.Join(t.TempDir(), "tray.lock")
	// releaseFirst、acquired、err 用于本次流程后续判断的releaseFirst、acquired、err
	releaseFirst, acquired, err := acquireTrayFileLock(lockPath)
	if err != nil {
		t.Fatalf("acquire first tray instance: %v", err)
	}
	if !acquired {
		t.Fatal("first tray instance should acquire lock")
	}
	defer releaseFirst()

	// releaseSecond、acquired、err 用于本次流程后续判断的releaseSecond、acquired、err
	releaseSecond, acquired, err := acquireTrayFileLock(lockPath)
	if err != nil {
		t.Fatalf("acquire second tray instance: %v", err)
	}
	defer releaseSecond()
	if acquired {
		t.Fatal("second tray instance must be rejected")
	}
}
