package db

import (
	"sync"
	"testing"
	"time"
)

// TestLockAccountCredentialsReclaimsIdleEntries 验证账号凭证锁在最后一个持有者释放后会回收。
func TestLockAccountCredentialsReclaimsIdleEntries(t *testing.T) {
	// store 是只初始化凭证锁表的最小 Store。
	store := &Store{credentialLocks: make(map[string]*credentialLockEntry)}
	// unlock 是当前账号凭证锁的释放函数。
	unlock := store.LockAccountCredentials("cookie-1")
	store.credentialMu.Lock()
	// entryCount 是当前凭证锁表中的账号数量。
	entryCount := len(store.credentialLocks)
	store.credentialMu.Unlock()
	if entryCount != 1 {
		t.Fatalf("获取锁后应有一个 entry，got=%d", entryCount)
	}
	unlock()
	unlock()
	store.credentialMu.Lock()
	// remainingCount 是释放最后一个锁后的账号数量。
	remainingCount := len(store.credentialLocks)
	store.credentialMu.Unlock()
	if remainingCount != 0 {
		t.Fatalf("最后一个锁释放后应回收 entry，got=%d", remainingCount)
	}
}

// TestLockAccountCredentialsKeepsEntryWhileWaiterExists 验证等待者存在时不会提前回收共享锁。
func TestLockAccountCredentialsKeepsEntryWhileWaiterExists(t *testing.T) {
	// store 是并发凭证锁测试使用的最小 Store。
	store := &Store{credentialLocks: make(map[string]*credentialLockEntry)}
	// firstUnlock 是第一个持有者的释放函数。
	firstUnlock := store.LockAccountCredentials("cookie-2")
	// waiterAcquired 表示第二个调用方已经获得并释放了账号锁。
	waiterAcquired := make(chan struct{})
	go func() {
		// secondUnlock 是等待者获得的释放函数。
		secondUnlock := store.LockAccountCredentials("cookie-2")
		secondUnlock()
		close(waiterAcquired)
	}()
	// waiterStarted 是确认等待者已经登记引用后再释放第一个锁的同步信号。
	waiterStarted := false
	// deadline 限制等待者登记引用的最长时间。
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for !waiterStarted {
		store.credentialMu.Lock()
		// entry 是当前账号的共享锁 entry。
		entry := store.credentialLocks["cookie-2"]
		if entry != nil && entry.refs == 2 {
			waiterStarted = true
		}
		store.credentialMu.Unlock()
		if waiterStarted {
			break
		}
		select {
		case <-deadline.C:
			t.Fatal("等待者未登记凭证锁引用")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	firstUnlock()
	select {
	case <-waiterAcquired:
	case <-time.After(time.Second):
		t.Fatal("等待者未获得凭证锁")
	}
	store.credentialMu.Lock()
	// remainingCount 是等待者释放后凭证锁表中的账号数量。
	remainingCount := len(store.credentialLocks)
	store.credentialMu.Unlock()
	if remainingCount != 0 {
		t.Fatalf("等待者释放后应回收 entry，got=%d", remainingCount)
	}
}

// TestLockAccountCredentialsSerializesConcurrentUpdates 验证同一账号的临界区仍然互斥。
func TestLockAccountCredentialsSerializesConcurrentUpdates(t *testing.T) {
	// store 是并发临界区测试使用的最小 Store。
	store := &Store{credentialLocks: make(map[string]*credentialLockEntry)}
	// activeCount 是同时进入临界区的调用方数量。
	activeCount := 0
	// maxActiveCount 是测试期间观察到的最大并发数量。
	maxActiveCount := 0
	// countMu 保护测试计数器。
	var countMu sync.Mutex
	// done 是两个并发调用完成的同步信号。
	done := make(chan struct{}, 2)
	for // i 表示当前并发锁测试调用序号。
	i := 0; i < 2; i++ {
		go func() {
			// unlock 是当前调用方的账号锁释放函数。
			unlock := store.LockAccountCredentials("cookie-3")
			countMu.Lock()
			activeCount++
			if activeCount > maxActiveCount {
				maxActiveCount = activeCount
			}
			countMu.Unlock()
			time.Sleep(time.Millisecond)
			countMu.Lock()
			activeCount--
			countMu.Unlock()
			unlock()
			done <- struct{}{}
		}()
	}
	for // i 表示当前完成信号序号。
	i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("并发凭证锁测试超时")
		}
	}
	if maxActiveCount != 1 {
		t.Fatalf("同一账号临界区应互斥，最大并发数=%d", maxActiveCount)
	}
}
