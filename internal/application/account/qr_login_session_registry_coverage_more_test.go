package account

import (
	"errors"
	"testing"
	"time"
)

// TestQRLoginSessionRegistryCoversZeroValueAndGuards 覆盖扫码会话注册表的空接收者、零值实例和非法输入保护。
func TestQRLoginSessionRegistryCoversZeroValueAndGuards(t *testing.T) {
	// nilRegistry 表示未初始化的扫码会话注册表。
	var nilRegistry *QRLoginSessionRegistry
	nilRegistry.Register("session", 7, time.Time{})
	if !errors.Is(nilRegistry.Authorize("session", 7), ErrQRLoginSessionNotFound) {
		t.Fatal("空注册表授权应返回会话不存在")
	}
	// got 保存空注册表清理返回的会话标识集合。
	if got := nilRegistry.Cleanup(time.Time{}); got != nil {
		t.Fatalf("空注册表清理结果=%v", got)
	}
	nilRegistry.Delete("session")
	// persistErr 保存空注册表执行幂等持久化时的初始化错误。
	if _, persistErr := nilRegistry.PersistOnce("session", 7, func() (QRLoginSessionPersistence, error) {
		return QRLoginSessionPersistence{}, nil
	}); persistErr == nil {
		t.Fatal("空注册表持久化应失败")
	}
	// zeroRegistry 是依赖字段均未预初始化的零值注册表。
	zeroRegistry := &QRLoginSessionRegistry{}
	zeroRegistry.ownerLifetime = time.Hour
	zeroRegistry.Register("zero-session", 7, time.Time{})
	// persisted、persistErr 保存零值注册表首次持久化的幂等结果及错误。
	persisted, persistErr := zeroRegistry.PersistOnce("zero-session", 7, func() (QRLoginSessionPersistence, error) {
		return QRLoginSessionPersistence{AccountID: "account"}, nil
	})
	if persistErr != nil || persisted.AccountID != "account" || persisted.UserID != 7 || persisted.CreatedAt.IsZero() {
		t.Fatalf("零值注册表持久化结果=%+v err=%v", persisted, persistErr)
	}
	// err 保存错误用户访问零值注册表时的会话归属错误。
	if err := zeroRegistry.Authorize("zero-session", 8); !errors.Is(err, ErrQRLoginSessionForbidden) {
		t.Fatalf("错误用户授权错误=%v", err)
	}
	// invalidWorkErr 保存空工作函数导致的输入错误。
	if _, invalidWorkErr := zeroRegistry.PersistOnce("zero-session-2", 7, nil); invalidWorkErr == nil {
		t.Fatal("空工作函数应失败")
	}
	zeroRegistry.Register("", 7, time.Time{})
	zeroRegistry.Delete("")
}

// TestQRLoginSessionRegistryCoversCleanupLockReclamation 验证清理过期会话时会一并回收空闲持久化锁。
func TestQRLoginSessionRegistryCoversCleanupLockReclamation(t *testing.T) {
	// now 是清理测试使用的固定 UTC 时间。
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	// registry 是带固定时钟和显式空闲锁的扫码会话注册表。
	registry := NewQRLoginSessionRegistry()
	registry.now = func() time.Time { return now }
	registry.Register("expired-lock", 7, now.Add(-31*time.Minute))
	registry.mu.Lock()
	registry.persistLocks["expired-lock"] = &qrLoginPersistenceLock{}
	registry.mu.Unlock()
	// expired 保存清理返回的外部平台会话标识。
	expired := registry.Cleanup(time.Time{})
	if len(expired) != 1 || expired[0] != "expired-lock" {
		t.Fatalf("过期会话清理结果=%v", expired)
	}
	// authorizeErr 保存清理后再次授权已删除会话时的错误。
	if authorizeErr := registry.Authorize("expired-lock", 7); !errors.Is(authorizeErr, ErrQRLoginSessionNotFound) {
		t.Fatalf("清理后授权错误=%v", authorizeErr)
	}
}
