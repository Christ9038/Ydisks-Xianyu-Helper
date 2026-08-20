package adapter

import (
	"context"
	"errors"
	"strings"

	settingsapp "xianyu-go/internal/application/settings"
	"xianyu-go/internal/db"
)

// SettingsRepository 将系统、用户和账号 AI 设置数据库模型适配为应用层 Port。
type SettingsRepository struct {
	// store 保存数据库聚合入口，仅由适配器访问数据库模型。
	store *db.Store
}

// NewSettingsRepository 构造设置数据库适配器。
func NewSettingsRepository(store *db.Store) *SettingsRepository {
	// repository 保存设置用例所需的数据库入口。
	repository := &SettingsRepository{store: store}
	return repository
}

// IsSensitiveSettingKey 判断设置键是否属于数据库维护的敏感白名单。
func (r *SettingsRepository) IsSensitiveSettingKey(key string) bool {
	if r == nil {
		return false
	}
	return db.IsSensitiveSettingKey(key)
}

// SensitiveSettingKeys 返回敏感设置键名，不返回任何敏感值。
func (r *SettingsRepository) SensitiveSettingKeys() []string {
	if r == nil {
		return nil
	}
	return db.SensitiveSettingKeys()
}

// PublicSystem 读取公开系统设置。
func (r *SettingsRepository) PublicSystem(ctx context.Context) (map[string]string, error) {
	// err 表示设置数据库适配器未装配。
	if err := r.validate(); err != nil {
		return nil, err
	}
	return r.store.Settings.Public(ctx)
}

// RedactedSystem 读取已脱敏系统设置，敏感值只转换为 configured 标记。
func (r *SettingsRepository) RedactedSystem(ctx context.Context) (map[string]string, error) {
	// err 表示设置数据库适配器未装配。
	if err := r.validate(); err != nil {
		return nil, err
	}
	return r.store.Settings.Redacted(ctx)
}

// GetSystem 读取一项系统设置，仅供应用层受控业务使用。
func (r *SettingsRepository) GetSystem(ctx context.Context, key string) (string, error) {
	// err 表示设置数据库适配器未装配。
	if err := r.validate(); err != nil {
		return "", err
	}
	return r.store.Settings.Get(ctx, key)
}

// ReadSensitiveSystem 通过数据库现有 fail-closed 审计入口读取系统秘密。
func (r *SettingsRepository) ReadSensitiveSystem(ctx context.Context, userID int64, key, action, resource string) (string, error) {
	// err 表示设置数据库适配器未装配。
	if err := r.validate(); err != nil {
		return "", err
	}
	return r.store.ReadSensitiveSetting(ctx, userID, key, action, resource)
}

// ApplySystemChanges 将应用层敏感变更命令转换为数据库模型并原子保存。
func (r *SettingsRepository) ApplySystemChanges(ctx context.Context, values map[string]string, secrets map[string]settingsapp.SecretChange) error {
	// err 表示设置数据库适配器未装配。
	if err := r.validate(); err != nil {
		return err
	}
	// dbSecrets 保存转换后的数据库敏感设置命令。
	dbSecrets := make(map[string]db.SensitiveSettingChange, len(secrets))
	// key 是敏感设置键名；change 是应用层三态命令。
	for key, change := range secrets {
		dbSecrets[key] = db.SensitiveSettingChange{Action: change.Action, Value: change.Value}
	}
	return r.store.Settings.ApplyChanges(ctx, values, dbSecrets)
}

// SetSystem 保存一项普通系统设置。
func (r *SettingsRepository) SetSystem(ctx context.Context, key, value string) error {
	// err 表示设置数据库适配器未装配。
	if err := r.validate(); err != nil {
		return err
	}
	return r.store.Settings.Set(ctx, key, value)
}

// AddAudit 将应用层非敏感审计模型转换为数据库记录。
func (r *SettingsRepository) AddAudit(ctx context.Context, record settingsapp.AuditRecord) error {
	// err 表示审计存储未装配。
	if err := r.validateAudit(); err != nil {
		return err
	}
	return r.store.SecurityAudit.Add(ctx, db.SecurityAuditLog{
		UserID: record.UserID, Action: record.Action, Resource: record.Resource,
		Keys: record.Keys, Outcome: record.Outcome,
	})
}

// ListUser 读取指定用户的全部偏好设置。
func (r *SettingsRepository) ListUser(ctx context.Context, userID int64) (map[string]string, error) {
	// err 表示设置数据库适配器未装配。
	if err := r.validate(); err != nil {
		return nil, err
	}
	return r.store.UserSettings.AllForUser(ctx, userID)
}

// GetUser 读取指定用户的一项偏好设置。
func (r *SettingsRepository) GetUser(ctx context.Context, userID int64, key string) (string, error) {
	// err 表示设置数据库适配器未装配。
	if err := r.validate(); err != nil {
		return "", err
	}
	return r.store.UserSettings.GetForUser(ctx, userID, key)
}

// SetUser 保存指定用户的一项偏好设置。
func (r *SettingsRepository) SetUser(ctx context.Context, userID int64, key, value string) error {
	// err 表示设置数据库适配器未装配。
	if err := r.validate(); err != nil {
		return err
	}
	return r.store.UserSettings.SetForUser(ctx, userID, key, value)
}

// CheckOwnership 查询账号所属用户，不读取或解密任何账号凭证。
func (r *SettingsRepository) CheckOwnership(ctx context.Context, userID int64, cookieID string) (int64, error) {
	// err 表示设置数据库适配器未装配。
	if err := r.validate(); err != nil {
		return 0, err
	}
	// owned、err 保存非敏感账号归属结果及查询错误。
	owned, err := r.store.Cookies.ExistsOwned(ctx, userID, strings.TrimSpace(cookieID))
	if err != nil {
		return 0, err
	}
	if owned {
		return userID, nil
	}
	// ownerID、err 保存跨用户账号所有者标识及查询错误。
	ownerID, err := r.store.Cookies.GetOwnerID(ctx, strings.TrimSpace(cookieID))
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return 0, settingsapp.ErrAccountNotFound
		}
		return 0, err
	}
	return ownerID, nil
}

// ListAIReply 查询用户范围内的账号 AI 设置摘要，数据库层不会读取 API 密钥。
func (r *SettingsRepository) ListAIReply(ctx context.Context, userID int64) ([]settingsapp.AIReplySettings, error) {
	// err 表示设置数据库适配器未装配。
	if err := r.validate(); err != nil {
		return nil, err
	}
	// records、err 保存数据库 AI 设置摘要及查询错误。
	records, err := r.store.AIReply.ListForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	// result 保存转换后的应用 AI 设置摘要。
	result := make([]settingsapp.AIReplySettings, 0, len(records))
	// record 是当前待转换的数据库 AI 设置摘要。
	for _, record := range records {
		result = append(result, aiReplyModel(record))
	}
	return result, nil
}

// GetAIReply 从用户范围摘要中读取单个账号，避免解密或传递账号 API 密钥。
func (r *SettingsRepository) GetAIReply(ctx context.Context, userID int64, cookieID string) (settingsapp.AIReplySettings, error) {
	// err 表示设置数据库适配器未装配。
	if err := r.validate(); err != nil {
		return settingsapp.AIReplySettings{}, err
	}
	// records、err 保存数据库 AI 设置摘要及查询错误。
	records, err := r.store.AIReply.ListForUser(ctx, userID)
	if err != nil {
		return settingsapp.AIReplySettings{}, err
	}
	// record 是当前待匹配的账号 AI 设置摘要。
	for _, record := range records {
		if record.CookieID == cookieID {
			return aiReplyModel(record), nil
		}
	}
	return settingsapp.AIReplySettings{}, settingsapp.ErrConfigNotFound
}

// UpsertAIReply 将账号 AI 设置转换为数据库模型并保存。
func (r *SettingsRepository) UpsertAIReply(ctx context.Context, cookieID string, settings settingsapp.AIReplySettings) error {
	// err 表示设置数据库适配器未装配。
	if err := r.validate(); err != nil {
		return err
	}
	// unlock 串行化 AI 议价与固定规则改价的最终冲突检查和写入。
	unlock := r.store.LockPricingMode()
	defer unlock()
	if settings.AIEnabled {
		// conflict 表示最终写入时账号是否已有启用的固定改价规则；conflictErr 是查询错误。
		conflict, conflictErr := r.store.Automation.HasEnabledAdjustPriceRule(ctx, cookieID)
		if conflictErr != nil {
			return conflictErr
		}
		if conflict {
			return settingsapp.ErrPricingModeConflict
		}
	}
	return r.store.AIReply.UpsertSettings(ctx, cookieID, db.AIReplySettings{
		CookieID: cookieID, AIEnabled: settings.AIEnabled, AutoAdjustPriceEnabled: settings.AutoAdjustPriceEnabled,
		MaxDiscountPercent: settings.MaxDiscountPercent, MaxDiscountAmount: settings.MaxDiscountAmount,
		MaxBargainRounds: settings.MaxBargainRounds, CustomPrompts: settings.CustomPrompts,
	})
}

// HasEnabledAdjustPriceRule 判断账号是否存在启用的固定自动改价规则。
func (r *SettingsRepository) HasEnabledAdjustPriceRule(ctx context.Context, cookieID string) (bool, error) {
	// err 表示设置数据库适配器未装配。
	if err := r.validate(); err != nil {
		return false, err
	}
	if r.store.Automation == nil {
		return false, errors.New("自动化规则存储未初始化")
	}
	return r.store.Automation.HasEnabledAdjustPriceRule(ctx, strings.TrimSpace(cookieID))
}

// validate 检查设置适配器及其数据库子仓储是否完整装配。
func (r *SettingsRepository) validate() error {
	if r == nil || r.store == nil || r.store.Settings == nil || r.store.UserSettings == nil || r.store.AIReply == nil || r.store.Cookies == nil {
		return errors.New("设置数据库适配器未初始化")
	}
	return nil
}

// validateAudit 检查敏感设置审计所需的独立存储，避免普通设置被无关依赖阻断。
func (r *SettingsRepository) validateAudit() error {
	// err 表示设置数据库适配器未装配。
	if err := r.validate(); err != nil {
		return err
	}
	if r.store.SecurityAudit == nil {
		return errors.New("敏感访问审计未初始化")
	}
	return nil
}

// aiReplyModel 将不含 API 密钥的数据库摘要转换为应用模型。
func aiReplyModel(record db.AIReplySettings) settingsapp.AIReplySettings {
	return settingsapp.AIReplySettings{
		CookieID: record.CookieID, AIEnabled: record.AIEnabled, AutoAdjustPriceEnabled: record.AutoAdjustPriceEnabled,
		MaxDiscountPercent: record.MaxDiscountPercent, MaxDiscountAmount: record.MaxDiscountAmount,
		MaxBargainRounds: record.MaxBargainRounds, CustomPrompts: record.CustomPrompts,
	}
}

var _ settingsapp.Repository = (*SettingsRepository)(nil)
