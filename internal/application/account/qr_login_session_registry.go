package account

import (
	"errors"
	"sync"
	"time"
)

// ErrQRLoginSessionNotFound 表示扫码会话不存在或已经超过服务端保留期限。
var ErrQRLoginSessionNotFound = errors.New("扫码会话不存在或已过期")

// ErrQRLoginSessionForbidden 表示当前用户不是扫码会话的创建者。
var ErrQRLoginSessionForbidden = errors.New("扫码会话不属于当前用户")

// QRLoginSessionPersistence 是扫码成功后缓存的非敏感持久化结果，避免重复写入账号凭证。
type QRLoginSessionPersistence struct {
	// AccountID 是扫码结果最终写入的本地账号标识。
	AccountID string
	// IsNew 表示本次扫码是否创建了新的本地账号。
	IsNew bool
	// UserID 是创建该扫码会话的本地用户标识，用于阻止跨用户复用幂等结果。
	UserID int64
	// CreatedAt 是结果写入缓存的时间，仅用于诊断和生命周期观测。
	CreatedAt time.Time
}

// QRLoginSessionRegistry 持有扫码会话所有权和幂等结果，Server 只通过方法访问这些状态。
// mu 保护 owners 与 persisted；persistLocks 为每个会话串行化慢速凭证写入，锁内禁止持有 mu 调用外部 I/O。
type QRLoginSessionRegistry struct {
	mu            sync.Mutex
	owners        map[string]qrLoginSessionOwner
	persisted     map[string]QRLoginSessionPersistence
	persistLocks  map[string]*qrLoginPersistenceLock
	ownerLifetime time.Duration
	now           func() time.Time
}

// qrLoginSessionOwner 保存扫码会话创建者和创建时间，不包含 Cookie 或平台 Token。
type qrLoginSessionOwner struct {
	// userID 是创建扫码会话的本地用户标识。
	userID int64
	// createdAt 是会话创建时间，用于服务端过期回收。
	createdAt time.Time
}

// qrLoginPersistenceLock 保存单个扫码会话的串行锁及等待者计数，用于安全回收锁对象。
type qrLoginPersistenceLock struct {
	// mu 串行化该会话的凭证持久化回调；不与注册表主锁同时持有。
	mu sync.Mutex
	// refs 统计已经取得该锁或正在等待该锁的调用数，避免回收时制造第二把锁。
	refs int
}

// NewQRLoginSessionRegistry 创建扫码会话状态注册表，并设置默认三十分钟的 HTTP 所有权期限。
func NewQRLoginSessionRegistry() *QRLoginSessionRegistry {
	return &QRLoginSessionRegistry{
		owners:        make(map[string]qrLoginSessionOwner),
		persisted:     make(map[string]QRLoginSessionPersistence),
		persistLocks:  make(map[string]*qrLoginPersistenceLock),
		ownerLifetime: 30 * time.Minute,
		now:           time.Now,
	}
}

// Register 记录新扫码会话的创建者；重复会话标识会覆盖旧所有权，平台层负责保证标识唯一。
func (r *QRLoginSessionRegistry) Register(sessionID string, userID int64, createdAt time.Time) {
	if r == nil || sessionID == "" {
		return
	}
	if createdAt.IsZero() {
		createdAt = r.currentTime()
	}
	r.mu.Lock()
	if r.owners == nil {
		r.owners = make(map[string]qrLoginSessionOwner)
	}
	r.owners[sessionID] = qrLoginSessionOwner{userID: userID, createdAt: createdAt}
	r.mu.Unlock()
}

// Authorize 校验扫码会话归属；过期或不存在统一返回 NotFound，归属不符返回 Forbidden。
func (r *QRLoginSessionRegistry) Authorize(sessionID string, userID int64) error {
	if r == nil || sessionID == "" {
		return ErrQRLoginSessionNotFound
	}
	r.mu.Lock()
	// owner 保存会话创建者；ok 表示注册表中存在该会话。
	owner, ok := r.owners[sessionID]
	// expired 表示会话创建时间已经超过 HTTP 所有权期限。
	expired := ok && owner.createdAt.Before(r.currentTime().Add(-r.ownerLifetime))
	if expired {
		delete(r.owners, sessionID)
		r.deletePersistedLocked(sessionID)
		r.deletePersistLockIfIdleLocked(sessionID)
	}
	r.mu.Unlock()
	if !ok || expired {
		return ErrQRLoginSessionNotFound
	}
	if owner.userID != userID {
		return ErrQRLoginSessionForbidden
	}
	return nil
}

// Cleanup 删除超过所有权期限的会话并返回应同步释放的平台会话标识。
func (r *QRLoginSessionRegistry) Cleanup(now time.Time) []string {
	if r == nil {
		return nil
	}
	if now.IsZero() {
		now = r.currentTime()
	}
	// cutoff 是本次清理允许保留的最早会话创建时间。
	cutoff := now.Add(-r.ownerLifetime)
	// expired 收集需要同步释放的平台扫码会话标识。
	expired := make([]string, 0)
	r.mu.Lock()
	// sessionID、owner 分别表示当前遍历的扫码会话标识及其创建者记录。
	for sessionID, owner := range r.owners {
		if owner.createdAt.Before(cutoff) {
			delete(r.owners, sessionID)
			r.deletePersistedLocked(sessionID)
			r.deletePersistLockIfIdleLocked(sessionID)
			expired = append(expired, sessionID)
		}
	}
	r.mu.Unlock()
	return expired
}

// Delete 移除指定扫码会话的所有权和幂等结果；平台会话释放由调用方按返回标识执行。
func (r *QRLoginSessionRegistry) Delete(sessionID string) {
	if r == nil || sessionID == "" {
		return
	}
	r.mu.Lock()
	delete(r.owners, sessionID)
	r.deletePersistedLocked(sessionID)
	r.deletePersistLockIfIdleLocked(sessionID)
	r.mu.Unlock()
}

// PersistOnce 以会话为粒度串行执行凭证持久化，并缓存成功后的非敏感结果。
// work 仅在该会话锁内执行，不能把 Cookie 写入注册表或日志；失败不会留下幂等结果。
func (r *QRLoginSessionRegistry) PersistOnce(sessionID string, userID int64, work func() (QRLoginSessionPersistence, error)) (QRLoginSessionPersistence, error) {
	if r == nil || sessionID == "" || work == nil {
		return QRLoginSessionPersistence{}, errors.New("扫码会话注册表未初始化")
	}
	// persistLock 串行化同一扫码会话的慢速凭证写入，避免重复创建账号。
	persistLock := r.acquirePersistLock(sessionID)
	persistLock.mu.Lock()
	defer r.releasePersistLock(sessionID, persistLock)

	r.mu.Lock()
	// persisted、ok 保存已完成的幂等结果及其存在性。
	if persisted, ok := r.persisted[sessionID]; ok {
		r.mu.Unlock()
		if persisted.UserID != userID {
			return QRLoginSessionPersistence{}, ErrQRLoginSessionForbidden
		}
		return persisted, nil
	}
	r.mu.Unlock()

	// persisted 保存工作函数返回的非敏感结果；Cookie 明文只在 work 的调用链内短暂存在。
	persisted, err := work()
	if err != nil {
		return QRLoginSessionPersistence{}, err
	}
	persisted.UserID = userID
	if persisted.CreatedAt.IsZero() {
		persisted.CreatedAt = r.currentTime()
	}
	r.mu.Lock()
	if r.persisted == nil {
		r.persisted = make(map[string]QRLoginSessionPersistence)
	}
	r.persisted[sessionID] = persisted
	r.mu.Unlock()
	return persisted, nil
}

// currentTime 返回可测试的当前时间；测试可在包内替换 now 而不影响生产时钟。
func (r *QRLoginSessionRegistry) currentTime() time.Time {
	if r != nil && r.now != nil {
		return r.now().UTC()
	}
	return time.Now().UTC()
}

// acquirePersistLock 获取会话级持久化锁，并在注册表主锁内登记等待者数量。
func (r *QRLoginSessionRegistry) acquirePersistLock(sessionID string) *qrLoginPersistenceLock {
	r.mu.Lock()
	if r.persistLocks == nil {
		r.persistLocks = make(map[string]*qrLoginPersistenceLock)
	}
	// persistLock 保存该会话共享的锁对象，所有等待者都复用同一实例。
	persistLock := r.persistLocks[sessionID]
	if persistLock == nil {
		persistLock = &qrLoginPersistenceLock{}
		r.persistLocks[sessionID] = persistLock
	}
	persistLock.refs++
	r.mu.Unlock()
	return persistLock
}

// releasePersistLock 释放会话级持久化锁，并在没有等待者时回收锁对象。
func (r *QRLoginSessionRegistry) releasePersistLock(sessionID string, persistLock *qrLoginPersistenceLock) {
	persistLock.mu.Unlock()
	r.mu.Lock()
	persistLock.refs--
	if persistLock.refs == 0 && r.persistLocks[sessionID] == persistLock {
		delete(r.persistLocks, sessionID)
	}
	r.mu.Unlock()
}

// deletePersistLockIfIdleLocked 在调用方持有注册表主锁时回收没有并发等待者的锁对象。
func (r *QRLoginSessionRegistry) deletePersistLockIfIdleLocked(sessionID string) {
	// persistLock 保存待回收的会话级锁对象；调用方已持有注册表主锁。
	persistLock := r.persistLocks[sessionID]
	if persistLock != nil && persistLock.refs == 0 {
		delete(r.persistLocks, sessionID)
	}
}

// deletePersistedLocked 删除会话缓存；调用方必须已经持有 mu。
func (r *QRLoginSessionRegistry) deletePersistedLocked(sessionID string) {
	delete(r.persisted, sessionID)
}
