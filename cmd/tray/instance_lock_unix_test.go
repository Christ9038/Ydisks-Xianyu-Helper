//go:build !windows

package main

import (
	"path/filepath"
	"testing"
)

// TestAcquireTrayInstanceRejectsSecondInstance 负责TestAcquireTrayInstanceRejectsSecondInstance相关处理。
func TestAcquireTrayInstanceRejectsSecondInstance(t *testing.T) {
	// lockPath 保存锁路径，供当前处理流程使用
	lockPath := filepath.Join(t.TempDir(), "tray.lock")
	// releaseFirst、acquired、err 保存releaseFirst、acquired、err，供当前处理流程使用
	releaseFirst, acquired, err := acquireTrayFileLock(lockPath)
	if err != nil {
		t.Fatalf("acquire first tray instance: %v", err)
	}
	if !acquired {
		t.Fatal("first tray instance should acquire lock")
	}
	defer releaseFirst()

	// releaseSecond、acquired、err 保存releaseSecond、acquired、err，供当前处理流程使用
	releaseSecond, acquired, err := acquireTrayFileLock(lockPath)
	if err != nil {
		t.Fatalf("acquire second tray instance: %v", err)
	}
	defer releaseSecond()
	if acquired {
		t.Fatal("second tray instance must be rejected")
	}
}
