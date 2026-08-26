package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Store 聚合各 repository，供上层（HTTP server、account supervisor 等）统一持有。
type Store struct {
	DB                *sql.DB
	Dialect           Dialect
	Users             *Users
	Sessions          *Sessions
	Cookies           *Cookies
	Items             *Items
	Cards             *Cards
	Automation        *AutomationRules
	DeliveryTemplates *DeliveryTemplateStore
	Orders            *Orders
	// OrderWrites 保存订单与商品基础信息跨仓储原子写入的窄 Unit of Work。
	OrderWrites      *OrderWriteUnitOfWork
	Reconciliations  *OrderReconciliations
	OrderRefreshJobs *OrderRefreshJobs
	Keywords         *Keywords
	DefaultReps      *DefaultReplies
	ItemReps         *ItemReplies
	AIReply          *AIReply
	Notifications    *Notifications
	Settings         *SystemSettings
	UserSettings     *UserSettings
	WSMessages       *WSMessageStore
	PublishBatches   *ItemPublishBatches
	Tokens           *AccountTokens
	Renewal          *RenewalStore
	LoginLogs        *AccountLoginLogs
	RiskLogs         *RiskControlLogs
	// SecurityAudit 保存敏感配置访问审计记录。
	SecurityAudit *SecurityAuditLogs
	Chats         *ChatStore
	AccountTasks  *AccountTaskStore
	Admin         *AdminQueries
	Analytics     *AnalyticsQueries

	credentialMu    sync.Mutex
	credentialLocks map[string]*credentialLockEntry
	// pricingModeMu 串行化 AI 议价与固定规则改价的互斥配置写入；锁内只允许短数据库事务，禁止外部 I/O。
	pricingModeMu sync.Mutex
}

// LockPricingMode 串行化当前进程内的互斥改价模式检查与写入。
func (s *Store) LockPricingMode() func() {
	if s == nil {
		return func() {}
	}
	s.pricingModeMu.Lock()
	return s.pricingModeMu.Unlock
}

// credentialLockEntry 保存单个账号凭证锁及当前排队/持有者数量。
type credentialLockEntry struct {
	// mu 串行化该账号的 Cookie、token 和 metadata 状态变更。
	mu sync.Mutex
	// refs 记录仍可能使用该 entry 的调用方数量，用于安全回收空闲锁。
	refs int
}

// NewStore 基于 *sql.DB 构造聚合 store。dialect 用于业务 SQL 方言分支。
func NewStore(db *sql.DB, dialect Dialect) *Store {
	// codec 保存用于敏感字段静态加密的编解码器，只传给允许处理秘密的仓储。
	codec := secretCodecFromEnvironment()
	// items、orders 保存 Store 内共享的商品和订单仓储；Unit of Work 必须复用它们以保持方言与连接池一致。
	items, orders := &Items{DB: db, Dialect: dialect}, &Orders{DB: db, Dialect: dialect}
	return &Store{
		DB:                db,
		Dialect:           dialect,
		Users:             &Users{DB: db},
		Sessions:          &Sessions{DB: db},
		Cookies:           &Cookies{DB: db, Dialect: dialect, codec: codec},
		Items:             items,
		Cards:             &Cards{DB: db, Dialect: dialect, codec: codec},
		Automation:        &AutomationRules{DB: db, Dialect: dialect, codec: codec},
		DeliveryTemplates: &DeliveryTemplateStore{DB: db, Dialect: dialect},
		Orders:            orders,
		OrderWrites:       newOrderWriteUnitOfWork(db, orders, items),
		Reconciliations:   &OrderReconciliations{DB: db, Dialect: dialect},
		OrderRefreshJobs:  &OrderRefreshJobs{DB: db},
		Keywords:          &Keywords{DB: db, Dialect: dialect},
		DefaultReps:       &DefaultReplies{DB: db, Dialect: dialect},
		ItemReps:          &ItemReplies{DB: db, Dialect: dialect},
		AIReply:           &AIReply{DB: db, Dialect: dialect, codec: codec},
		Notifications:     &Notifications{DB: db, Dialect: dialect, codec: codec},
		Settings:          &SystemSettings{DB: db, Dialect: dialect, codec: codec},
		UserSettings:      &UserSettings{DB: db, Dialect: dialect},
		WSMessages:        &WSMessageStore{DB: db},
		PublishBatches:    &ItemPublishBatches{DB: db},
		Tokens:            &AccountTokens{DB: db, Dialect: dialect, codec: codec},
		Renewal:           &RenewalStore{DB: db, Dialect: dialect},
		LoginLogs:         &AccountLoginLogs{DB: db},
		RiskLogs:          &RiskControlLogs{DB: db, Dialect: dialect},
		SecurityAudit:     &SecurityAuditLogs{DB: db},
		Chats:             &ChatStore{DB: db, Dialect: dialect},
		AccountTasks:      &AccountTaskStore{DB: db, Dialect: dialect},
		Admin:             &AdminQueries{DB: db},
		Analytics:         &AnalyticsQueries{DB: db},
		credentialLocks:   make(map[string]*credentialLockEntry),
	}
}

// ReadSensitiveSetting 读取指定用户可用的敏感系统设置，并在解密前写入不含秘密值的访问审计。
// userID 必须是正数；key 必须属于敏感设置白名单；action 和 resource 用于区分调用场景。
// 审计存储不可用或参数不合法时拒绝读取，避免未记录的秘密访问继续执行。
func (s *Store) ReadSensitiveSetting(ctx context.Context, userID int64, key, action, resource string) (string, error) {
	if s == nil || s.Settings == nil {
		return "", errors.New("敏感设置存储未初始化")
	}
	if userID <= 0 {
		return "", ErrInvalidUserID
	}
	key = strings.TrimSpace(key)
	if !IsSensitiveSettingKey(key) {
		return "", fmt.Errorf("设置 %q 不属于敏感设置白名单", key)
	}
	if strings.TrimSpace(action) == "" || strings.TrimSpace(resource) == "" {
		return "", errors.New("敏感设置审计上下文无效")
	}
	if s.SecurityAudit == nil {
		return "", errors.New("敏感设置访问审计未初始化")
	}
	// auditErr 表示敏感设置读取前写入访问审计时发生的错误。
	auditErr := s.SecurityAudit.Add(ctx, SecurityAuditLog{
		UserID: userID, Action: strings.TrimSpace(action), Resource: strings.TrimSpace(resource),
		Keys: []string{key}, Outcome: "accepted",
	})
	if auditErr != nil {
		return "", fmt.Errorf("记录敏感设置访问审计失败: %w", auditErr)
	}
	// value、readErr 保存审计成功后解密得到的设置值及读取错误；value 只返回给受控调用方。
	value, readErr := s.Settings.Get(ctx, key)
	if readErr != nil {
		return "", readErr
	}
	return value, nil
}

// ReadSensitiveSettingForAccount 按账号所有者读取敏感系统设置并记录访问审计。
// 账号所有者通过非敏感账号 ID 查询得到，调用方无需持有或传递登录 Cookie、密码等凭证字段。
func (s *Store) ReadSensitiveSettingForAccount(ctx context.Context, cookieID, key, action, resource string) (string, error) {
	if s == nil || s.Cookies == nil {
		return "", errors.New("账号凭证存储未初始化")
	}
	// ownerID、ownerErr 保存账号所有者查询结果；该查询只读取 user_id，不解密任何凭证。
	ownerID, ownerErr := s.Cookies.GetOwnerID(ctx, strings.TrimSpace(cookieID))
	if ownerErr != nil {
		return "", fmt.Errorf("读取敏感设置所属账号失败: %w", ownerErr)
	}
	return s.ReadSensitiveSetting(ctx, ownerID, key, action, resource)
}

// LockAccountCredentials serializes Cookie/token state transitions for one
// account across the IM runtime and renewal scheduler. The returned function
// must be called exactly once.
// LockAccountCredentials 封装锁账号Credentials业务协调。
func (s *Store) LockAccountCredentials(cookieID string) func() {
	if s == nil {
		return func() {}
	}
	s.credentialMu.Lock()
	if s.credentialLocks == nil {
		s.credentialLocks = make(map[string]*credentialLockEntry)
	}
	// entry 保存账号锁及其引用计数。
	entry := s.credentialLocks[cookieID]
	if entry == nil {
		entry = &credentialLockEntry{}
		s.credentialLocks[cookieID] = entry
	}
	entry.refs++
	s.credentialMu.Unlock()
	entry.mu.Lock()
	// unlocked 防止异常重复调用释放函数破坏锁和引用计数。
	unlocked := false
	// unlockMu 保护释放函数的幂等状态。
	var unlockMu sync.Mutex
	return func() {
		unlockMu.Lock()
		if unlocked {
			unlockMu.Unlock()
			return
		}
		unlocked = true
		unlockMu.Unlock()
		entry.mu.Unlock()
		s.credentialMu.Lock()
		entry.refs--
		if entry.refs == 0 && s.credentialLocks[cookieID] == entry {
			delete(s.credentialLocks, cookieID)
		}
		s.credentialMu.Unlock()
	}
}
