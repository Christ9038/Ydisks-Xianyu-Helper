package account

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// PendingVerificationTTL 是续期安全验证地址在内存中的保留时长；平台验证令牌是一次性且短命的，
// 过期后必须让用户回退到重新扫码，而不是继续使用可能已失效的地址。
const PendingVerificationTTL = 10 * time.Minute

// maxPendingVerifications 是登记表的条目上限；账号数量有限，达到上限时淘汰最早登记的条目防止无界增长。
const maxPendingVerifications = 256

// ErrVerificationNotPending 表示账号当前没有可用的续期安全验证登记（从未登记、已被取走或已过期）；
// 调用方应回退到重新扫码授权。
var ErrVerificationNotPending = errors.New("账号当前没有待完成的安全验证")

// pendingVerification 是单个账号的待验证登记；只保存验证地址与登记时间，不保存任何 Cookie。
type pendingVerification struct {
	// url 是平台给出的一次性验证地址，带令牌，禁止写日志、通知或数据库；可为空，仅用于通知去重。
	url string
	// registeredAt 是登记时间，用于 TTL 与淘汰顺序。
	registeredAt time.Time
}

// PendingVerificationRegistry 在内存中登记“定时续期被平台要求安全验证”的账号，
// 同时向调度器提供去重依据、向 HTTP 用例提供一次性取走验证地址的能力。
//
// 所有者：组合根创建并同时注入调度器与验证服务。并发：mu 保护 entries 与 now 之外的全部字段，
// 锁内只做 map 读写，无 I/O、无 goroutine；进程重启后清空，不持久化。
type PendingVerificationRegistry struct {
	// mu 保护 entries；不得在持锁时调用外部依赖。
	mu sync.Mutex
	// entries 按账号标识保存待验证登记，容量受 maxPendingVerifications 约束。
	entries map[string]pendingVerification
	// now 返回当前时间；生产为 time.Now，测试注入固定时钟。
	now func() time.Time
}

// NewPendingVerificationRegistry 创建空登记表，使用系统时钟。
func NewPendingVerificationRegistry() *PendingVerificationRegistry {
	return &PendingVerificationRegistry{entries: make(map[string]pendingVerification), now: time.Now}
}

// pruneLocked 删除已过期条目并在超出上限时淘汰最早的条目；调用方必须持有 r.mu。
func (r *PendingVerificationRegistry) pruneLocked(now time.Time) {
	// accountID、entry 是当前检查的登记项。
	for accountID, entry := range r.entries {
		if now.Sub(entry.registeredAt) > PendingVerificationTTL {
			delete(r.entries, accountID)
		}
	}
	for len(r.entries) > maxPendingVerifications {
		// oldestID、oldestAt 记录当前最早登记的账号及其时间。
		oldestID, oldestAt := "", time.Time{}
		// accountID 是遍历登记表时的账号标识；entry 是遍历登记表时的待验证登记项。
		for accountID, entry := range r.entries {
			if oldestID == "" || entry.registeredAt.Before(oldestAt) {
				oldestID, oldestAt = accountID, entry.registeredAt
			}
		}
		delete(r.entries, oldestID)
	}
}

// Register 登记 accountID 需要人工完成安全验证，verificationURL 为平台给出的一次性地址（可为空）。
// 返回 true 表示此前没有未过期登记，即这是一次新的验证要求，调用方应发出通知；
// 返回 false 表示同一要求仍在有效期内，只刷新地址与时间，不应重复通知。
func (r *PendingVerificationRegistry) Register(accountID, verificationURL string) bool {
	accountID = strings.TrimSpace(accountID)
	if r == nil || accountID == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// now 是本次登记的统一时间点。
	now := r.now()
	r.pruneLocked(now)
	// existing、exists 表示该账号此前是否已有未过期登记。
	_, exists := r.entries[accountID]
	r.entries[accountID] = pendingVerification{url: strings.TrimSpace(verificationURL), registeredAt: now}
	r.pruneLocked(now)
	return !exists
}

// Pending 报告 accountID 当前是否存在未过期的待验证登记；只读，不改变登记。
func (r *PendingVerificationRegistry) Pending(accountID string) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked(r.now())
	// exists 标记 strings.TrimSpace 取得的条目是否存在或调用是否成功。
	_, exists := r.entries[strings.TrimSpace(accountID)]
	return exists
}

// take 取走 accountID 的待验证登记并返回其内容；没有未过期登记或登记没有地址时返回 false，
// 没有地址的登记保持原样（仅用于通知去重）。取走后同一地址不会被第二次使用。
func (r *PendingVerificationRegistry) take(accountID string) (pendingVerification, bool) {
	if r == nil {
		return pendingVerification{}, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked(r.now())
	// key 是去除空白后的账号标识。
	key := strings.TrimSpace(accountID)
	// entry 保存 该表达式 的结果，供后续分支和断言使用；exists 标记 该表达式 取得的条目是否存在或调用是否成功。
	entry, exists := r.entries[key]
	if !exists || entry.url == "" {
		return pendingVerification{}, false
	}
	delete(r.entries, key)
	return entry, true
}

// restore 在启动失败时放回被取走的登记，保留原登记时间使 TTL 不被延长；
// 若期间已出现更新的登记则保留新的，不覆盖。
func (r *PendingVerificationRegistry) restore(accountID string, entry pendingVerification) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// key 是去除空白后的账号标识。
	key := strings.TrimSpace(accountID)
	// exists 标记 该表达式 取得的条目是否存在或调用是否成功。
	if _, exists := r.entries[key]; exists {
		return
	}
	r.entries[key] = entry
	r.pruneLocked(r.now())
}

// CredentialVerificationPort 定义启动人工验证会话所需的平台适配端口。
// 实现方负责读取账号当前凭证并创建验证会话；凭证明文只在实现内部短暂存在。
type CredentialVerificationPort interface {
	// StartCredentialVerification 用账号当前凭证与验证地址创建验证会话，返回会话标识。
	StartCredentialVerification(ctx context.Context, accountID, verificationURL string) (string, error)
}

// CredentialVerificationService 编排“续期需人工验证”的用例：归属校验、一次性取走验证地址、创建验证会话。
type CredentialVerificationService struct {
	// repository 提供不解密凭证的账号归属查询。
	repository ProfileSummaryRepository
	// registry 保存待验证登记，由组合根与调度器共享。
	registry *PendingVerificationRegistry
	// port 承担平台会话创建与凭证读取。
	port CredentialVerificationPort
}

// NewCredentialVerificationService 构造验证服务并校验全部必需依赖。
func NewCredentialVerificationService(repository ProfileSummaryRepository, registry *PendingVerificationRegistry, port CredentialVerificationPort) (*CredentialVerificationService, error) {
	if repository == nil {
		return nil, errors.New("凭证验证账号摘要 repository 未初始化")
	}
	if registry == nil {
		return nil, errors.New("凭证验证登记表未初始化")
	}
	if port == nil {
		return nil, errors.New("凭证验证平台端口未初始化")
	}
	return &CredentialVerificationService{repository: repository, registry: registry, port: port}, nil
}

// PendingAmong 返回 accountIDs 中当前有待完成安全验证的账号集合。
// 调用方必须只传入已按用户归属过滤的账号标识（运行状态接口已经如此）；本方法只读内存，不查询数据库也不读取凭证。
func (s *CredentialVerificationService) PendingAmong(accountIDs []string) map[string]bool {
	// pending 是结果集合；服务未初始化时返回空集合而不是 nil，调用方可直接读取。
	pending := make(map[string]bool)
	if s == nil || s.registry == nil {
		return pending
	}
	// accountID 是当前检查的账号标识。
	for _, accountID := range accountIDs {
		if s.registry.Pending(accountID) {
			pending[accountID] = true
		}
	}
	return pending
}

// Start 为当前用户拥有的账号启动人工验证会话并返回会话标识。
// 没有可用登记时返回 ErrVerificationNotPending；平台会话创建失败时放回登记，允许用户重试。
func (s *CredentialVerificationService) Start(ctx context.Context, userID int64, accountID string) (string, error) {
	if s == nil || s.repository == nil || s.registry == nil || s.port == nil {
		return "", errors.New("凭证验证服务未初始化")
	}
	// err 是 s.repository.GetOwnedSummary 返回的失败原因，用于断言成功或被拒绝的路径。
	if _, err := s.repository.GetOwnedSummary(ctx, userID, accountID); err != nil {
		return "", err
	}
	// entry、ok 是一次性取走的待验证登记。
	entry, ok := s.registry.take(accountID)
	if !ok {
		return "", ErrVerificationNotPending
	}
	// sessionID、err 是平台适配端口创建的验证会话标识及错误。
	sessionID, err := s.port.StartCredentialVerification(ctx, accountID, entry.url)
	if err != nil {
		s.registry.restore(accountID, entry)
		return "", err
	}
	return sessionID, nil
}
