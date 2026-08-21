// Package settings 提供系统、用户和账号 AI 设置的应用层用例。
// 本包只依赖消费者定义的 Port，不感知 HTTP、数据库模型或具体网络实现。
package settings

import (
	"context"
	"errors"
	"sort"
	"strings"
)

// ErrInvalidUser 表示调用方没有提供有效的本地用户标识。
var ErrInvalidUser = errors.New("设置用户标识无效")

// ErrAccountNotFound 表示目标账号不存在。
var ErrAccountNotFound = errors.New("设置账号不存在")

// ErrForbidden 表示目标账号属于其他用户。
var ErrForbidden = errors.New("无权操作该账号设置")

// ErrConfigNotFound 表示账号存在但尚未保存 AI 回复设置。
var ErrConfigNotFound = errors.New("AI 回复设置不存在")

// ErrPricingModeConflict 表示 AI 议价与固定自动改价规则不能同时启用。
var ErrPricingModeConflict = errors.New("AI 议价与自动化规则改价不能同时启用，请先关闭另一种改价方式")

// SecretChange 描述敏感系统设置的显式三态变更命令。
type SecretChange struct {
	// Action 是 retain、replace 或 clear 之一。
	Action string
	// Value 是 replace 操作要保存的新秘密。
	Value string
}

// AIReplySettings 是不携带模型地址、API 密钥等系统秘密的账号级 AI 设置。
type AIReplySettings struct {
	// CookieID 是设置所属账号的稳定标识。
	CookieID string
	// AIEnabled 表示账号 AI 回复是否启用。
	AIEnabled bool
	// AutoAdjustPriceEnabled 表示是否把有效 AI 报价自动应用到买家新拍订单。
	AutoAdjustPriceEnabled bool
	// MaxDiscountPercent 是允许的最大折扣比例。
	MaxDiscountPercent int
	// MaxDiscountAmount 是允许的最大折扣金额。
	MaxDiscountAmount int
	// MaxBargainRounds 是允许的最大砍价轮次。
	MaxBargainRounds int
	// CustomPrompts 是账号自定义提示词。
	CustomPrompts string
}

// AuditRecord 是敏感设置访问审计的非敏感应用模型。
type AuditRecord struct {
	// UserID 是执行敏感操作的本地用户标识。
	UserID int64
	// Action 是敏感操作类型。
	Action string
	// Resource 是被访问的业务资源。
	Resource string
	// Keys 是涉及的敏感设置键名，不包含键值。
	Keys []string
	// Outcome 是审计结果，例如 accepted。
	Outcome string
}

// Repository 定义设置用例所需的最小持久化和审计能力。
type Repository interface {
	// IsSensitiveSettingKey 判断设置键是否属于敏感白名单。
	IsSensitiveSettingKey(key string) bool
	// SensitiveSettingKeys 返回敏感设置键名，不返回任何秘密值。
	SensitiveSettingKeys() []string
	// PublicSystem 返回无需认证展示的系统设置。
	PublicSystem(ctx context.Context) (map[string]string, error)
	// RedactedSystem 返回管理员可见但已脱敏的系统设置。
	RedactedSystem(ctx context.Context) (map[string]string, error)
	// GetSystem 读取指定系统设置；仅应用服务内部用于受控业务操作。
	GetSystem(ctx context.Context, key string) (string, error)
	// ReadSensitiveSystem 审计后读取指定敏感系统设置。
	ReadSensitiveSystem(ctx context.Context, userID int64, key, action, resource string) (string, error)
	// ApplySystemChanges 原子保存普通设置和敏感变更命令。
	ApplySystemChanges(ctx context.Context, values map[string]string, secrets map[string]SecretChange) error
	// SetSystem 保存一项普通系统设置。
	SetSystem(ctx context.Context, key, value string) error
	// AddAudit 写入不包含秘密值的访问审计。
	AddAudit(ctx context.Context, record AuditRecord) error
	// ListUser 返回指定用户的全部用户设置。
	ListUser(ctx context.Context, userID int64) (map[string]string, error)
	// GetUser 返回指定用户的一项设置；不存在时返回空值。
	GetUser(ctx context.Context, userID int64, key string) (string, error)
	// SetUser 保存指定用户的一项设置。
	SetUser(ctx context.Context, userID int64, key, value string) error
	// CheckOwnership 返回账号的非敏感所有者标识。
	CheckOwnership(ctx context.Context, userID int64, cookieID string) (int64, error)
	// ListAIReply 返回用户范围内的账号 AI 设置摘要。
	ListAIReply(ctx context.Context, userID int64) ([]AIReplySettings, error)
	// GetAIReply 返回指定用户账号的 AI 设置摘要。
	GetAIReply(ctx context.Context, userID int64, cookieID string) (AIReplySettings, error)
	// UpsertAIReply 保存指定账号的 AI 设置摘要。
	UpsertAIReply(ctx context.Context, cookieID string, settings AIReplySettings) error
	// HasEnabledAdjustPriceRule 判断账号是否已有启用的固定自动改价规则。
	HasEnabledAdjustPriceRule(ctx context.Context, cookieID string) (bool, error)
}

// ModelClient 定义读取远端 AI 模型列表所需的最小网络能力。
type ModelClient interface {
	// Fetch 获取指定 AI 服务地址的模型名称，调用方不得把 API 密钥写入日志或响应。
	Fetch(ctx context.Context, baseURL, apiKey string) ([]string, error)
}

// OutboundPolicy 定义系统设置切换用户可配置 HTTP 出站策略所需的最小运行时 Port。
type OutboundPolicy interface {
	// SetPublicOnly 立即切换公网地址限制，不负责持久化设置值。
	SetPublicOnly(publicOnly bool)
}

// Service 编排设置读取、校验、审计和持久化，不依赖 HTTP 或数据库类型。
type Service struct {
	// repository 提供设置持久化与敏感访问审计能力。
	repository Repository
	// modelClient 提供远端模型目录读取能力。
	modelClient ModelClient
	// outboundPolicy 保存运行时 HTTP 策略端口，数据库仓储不直接依赖基础设施实现。
	outboundPolicy OutboundPolicy
}

// IsSensitiveSettingKey 判断 HTTP 兼容层中的设置键是否属于敏感白名单。
func (s *Service) IsSensitiveSettingKey(key string) bool {
	return s != nil && s.repository != nil && s.repository.IsSensitiveSettingKey(key)
}

// NewService 构造设置应用服务。
func NewService(repository Repository, modelClient ModelClient, policies ...OutboundPolicy) *Service {
	// service 保存调用方提供的设置 Port 与模型客户端。
	var policy OutboundPolicy
	if len(policies) > 0 {
		policy = policies[0]
	}
	// service 保存调用方提供的设置 Port、模型客户端和运行时策略端口。
	service := &Service{repository: repository, modelClient: modelClient, outboundPolicy: policy}
	return service
}

// PublicSystem 读取公开系统设置。
func (s *Service) PublicSystem(ctx context.Context) (map[string]string, error) {
	// err 表示设置 Port 未装配或公开设置读取失败。
	if err := s.validateRepository(); err != nil {
		return nil, err
	}
	return s.repository.PublicSystem(ctx)
}

// GetSystem 返回管理员可见的系统设置，并先记录完整敏感键白名单的读取审计。
func (s *Service) GetSystem(ctx context.Context, userID int64) (map[string]string, error) {
	// err 表示设置 Port 未装配或用户身份无效。
	if err := s.validateUser(userID); err != nil {
		return nil, err
	}
	// keys 是本次管理员读取需要审计的敏感键名集合。
	keys := s.repository.SensitiveSettingKeys()
	// err 表示敏感设置读取审计写入失败。
	if err := s.audit(ctx, AuditRecord{UserID: userID, Action: "settings.read", Resource: "system_settings", Keys: keys}); err != nil {
		return nil, err
	}
	return s.repository.RedactedSystem(ctx)
}

// ApplySystemChanges 校验并原子保存系统设置，敏感命令写入前必须完成审计。
func (s *Service) ApplySystemChanges(ctx context.Context, userID int64, values map[string]string, secrets map[string]SecretChange) error {
	// err 表示设置 Port 未装配或用户身份无效。
	if err := s.validateUser(userID); err != nil {
		return err
	}
	if len(values) == 0 && len(secrets) == 0 {
		return errors.New("设置不能为空")
	}
	// err 表示普通设置值违反敏感键或键名约束。
	if err := s.validateValues(values); err != nil {
		return err
	}
	// keys 保存待写入审计记录的敏感设置键名。
	keys := make([]string, 0, len(secrets))
	// key 是待审计的敏感设置键；change 是对应的三态命令。
	for key, change := range secrets {
		if !s.repository.IsSensitiveSettingKey(key) || !validSecretAction(change) {
			return errors.New("敏感设置命令无效")
		}
		if change.Action == "replace" && strings.TrimSpace(change.Value) == "" {
			return errors.New("敏感设置命令无效")
		}
		keys = append(keys, key)
	}
	if len(keys) > 0 {
		// err 表示敏感设置写入审计失败。
		if err := s.audit(ctx, AuditRecord{UserID: userID, Action: "settings.write", Resource: "system_settings", Keys: keys}); err != nil {
			return err
		}
	}
	// err 表示普通设置和敏感设置原子保存失败。
	if err := s.repository.ApplySystemChanges(ctx, values, secrets); err != nil {
		return err
	}
	s.applyOutboundPolicy(values)
	return nil
}

// SetSystem 保存单项系统设置，并把敏感设置的三态命令统一交给应用层校验。
func (s *Service) SetSystem(ctx context.Context, userID int64, key, value, action string) error {
	// err 表示设置 Port 未装配或用户身份无效。
	if err := s.validateUser(userID); err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 100 {
		return errors.New("设置键无效")
	}
	if s.repository.IsSensitiveSettingKey(key) {
		// change 是敏感设置的显式三态命令。
		change := SecretChange{Action: action, Value: value}
		if !validSecretAction(change) || change.Action == "replace" && strings.TrimSpace(change.Value) == "" {
			return errors.New("敏感设置命令无效")
		}
		// err 表示敏感设置写入审计失败。
		if err := s.audit(ctx, AuditRecord{UserID: userID, Action: "settings.write", Resource: "system_settings", Keys: []string{key}}); err != nil {
			return err
		}
		return s.repository.ApplySystemChanges(ctx, nil, map[string]SecretChange{key: change})
	}
	// err 表示单项普通设置的业务值校验错误。
	if err := validateSystemValue(key, value); err != nil {
		return err
	}
	// err 表示单项普通设置持久化失败。
	if err := s.repository.SetSystem(ctx, key, value); err != nil {
		return err
	}
	s.applyOutboundPolicy(map[string]string{key: value})
	return nil
}

// ListUser 读取当前用户的全部偏好设置。
func (s *Service) ListUser(ctx context.Context, userID int64) (map[string]string, error) {
	// err 表示设置 Port 未装配或用户身份无效。
	if err := s.validateUser(userID); err != nil {
		return nil, err
	}
	return s.repository.ListUser(ctx, userID)
}

// GetUser 读取当前用户的一项偏好设置。
func (s *Service) GetUser(ctx context.Context, userID int64, key string) (string, error) {
	// err 表示设置 Port 未装配或用户身份无效。
	if err := s.validateUser(userID); err != nil {
		return "", err
	}
	return s.repository.GetUser(ctx, userID, strings.TrimSpace(key))
}

// SetUser 保存当前用户的一项偏好设置。
func (s *Service) SetUser(ctx context.Context, userID int64, key, value string) error {
	// err 表示设置 Port 未装配或用户身份无效。
	if err := s.validateUser(userID); err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 100 {
		return errors.New("设置键无效")
	}
	return s.repository.SetUser(ctx, userID, key, value)
}

// ListAIReply 查询当前用户账号的 AI 设置摘要。
func (s *Service) ListAIReply(ctx context.Context, userID int64) ([]AIReplySettings, error) {
	// err 表示设置 Port 未装配或用户身份无效。
	if err := s.validateUser(userID); err != nil {
		return nil, err
	}
	return s.repository.ListAIReply(ctx, userID)
}

// GetAIReply 查询账号 AI 设置，并区分账号不存在和未配置设置。
func (s *Service) GetAIReply(ctx context.Context, userID int64, cookieID string) (AIReplySettings, error) {
	// err 表示账号归属校验失败。
	if err := s.ensureOwned(ctx, userID, cookieID); err != nil {
		return AIReplySettings{}, err
	}
	return s.repository.GetAIReply(ctx, userID, strings.TrimSpace(cookieID))
}

// UpsertAIReply 校验账号归属和数值边界后保存 AI 设置。
func (s *Service) UpsertAIReply(ctx context.Context, userID int64, cookieID string, settings AIReplySettings) error {
	// err 表示账号归属校验失败。
	if err := s.ensureOwned(ctx, userID, cookieID); err != nil {
		return err
	}
	if settings.MaxDiscountPercent < 0 || settings.MaxDiscountPercent > 100 {
		return errors.New("最大折扣比例必须在 0 到 100 之间")
	}
	if settings.MaxDiscountAmount < 0 {
		return errors.New("最大折扣金额不能小于 0")
	}
	if settings.MaxBargainRounds < 1 || settings.MaxBargainRounds > 10 {
		return errors.New("最大砍价轮次必须在 1 到 10 之间")
	}
	if settings.AutoAdjustPriceEnabled && !settings.AIEnabled {
		return errors.New("开启 AI 自动改价前必须先启用 AI 议价")
	}
	if settings.AIEnabled {
		// conflict 表示当前账号是否已有启用的固定价格规则；conflictErr 是查询错误。
		conflict, conflictErr := s.repository.HasEnabledAdjustPriceRule(ctx, strings.TrimSpace(cookieID))
		if conflictErr != nil {
			return conflictErr
		}
		if conflict {
			return ErrPricingModeConflict
		}
	}
	settings.CookieID = strings.TrimSpace(cookieID)
	return s.repository.UpsertAIReply(ctx, settings.CookieID, settings)
}

// ListAIModels 读取远端模型目录，API 密钥使用前必须经过受控读取或审计。
func (s *Service) ListAIModels(ctx context.Context, userID int64, baseURL, apiKey string) ([]string, error) {
	// err 表示设置 Port 未装配或用户身份无效。
	if err := s.validateUser(userID); err != nil {
		return nil, err
	}
	if s.modelClient == nil {
		return nil, errors.New("AI 模型客户端未初始化")
	}
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		// err 表示系统 AI 地址读取失败。
		var err error
		baseURL, err = s.repository.GetSystem(ctx, "ai_api_url")
		if err != nil {
			return nil, err
		}
	}
	if baseURL == "" {
		baseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		// err 表示敏感 API 密钥读取或审计失败。
		var err error
		apiKey, err = s.repository.ReadSensitiveSystem(ctx, userID, "ai_api_key", "settings.use", "ai_models")
		if err != nil {
			return nil, err
		}
	} else if // err 表示外部传入 API 密钥的使用审计失败。
	err := s.audit(ctx, AuditRecord{UserID: userID, Action: "settings.use", Resource: "ai_models", Keys: []string{"ai_api_key"}}); err != nil {
		return nil, err
	}
	return s.modelClient.Fetch(ctx, baseURL, apiKey)
}

// validateRepository 检查应用服务的基础设施 Port 是否已装配。
func (s *Service) validateRepository() error {
	if s == nil || s.repository == nil {
		return errors.New("设置 repository 未初始化")
	}
	return nil
}

// validateUser 检查设置服务及用户身份，避免无效身份进入数据库 Port。
func (s *Service) validateUser(userID int64) error {
	// err 表示设置 Port 未装配。
	if err := s.validateRepository(); err != nil {
		return err
	}
	if userID <= 0 {
		return ErrInvalidUser
	}
	return nil
}

// ensureOwned 统一执行用户身份和非敏感账号所有权校验。
func (s *Service) ensureOwned(ctx context.Context, userID int64, cookieID string) error {
	// err 表示设置服务未装配或用户身份无效。
	if err := s.validateUser(userID); err != nil {
		return err
	}
	cookieID = strings.TrimSpace(cookieID)
	if cookieID == "" {
		return ErrAccountNotFound
	}
	// ownerID、err 保存非敏感账号所有者标识及查询错误。
	ownerID, err := s.repository.CheckOwnership(ctx, userID, cookieID)
	if err != nil {
		return err
	}
	if ownerID != userID {
		return ErrForbidden
	}
	return nil
}

// validateValues 校验批量普通设置键值，阻止敏感值进入普通参数。
func (s *Service) validateValues(values map[string]string) error {
	// key 是待校验的普通设置键名。
	for key := range values {
		key = strings.TrimSpace(key)
		if key == "" || len(key) > 100 {
			return errors.New("设置键无效")
		}
		if s.repository.IsSensitiveSettingKey(key) {
			return errors.New("敏感设置必须放入 secrets 命令")
		}
		// err 表示批量普通设置的业务值校验错误。
		if err := validateSystemValue(key, values[key]); err != nil {
			return err
		}
	}
	return nil
}

// validateSystemValue 校验具有运行时语义的普通系统设置，避免非法值落库或切换策略。
func validateSystemValue(key, value string) error {
	if key == "outbound_http_public_only" && !strings.EqualFold(strings.TrimSpace(value), "true") && !strings.EqualFold(strings.TrimSpace(value), "false") {
		return errors.New("outbound_http_public_only 必须是布尔值")
	}
	return nil
}

// applyOutboundPolicy 在系统设置成功持久化后立即同步运行时策略。
func (s *Service) applyOutboundPolicy(values map[string]string) {
	if s == nil || s.outboundPolicy == nil {
		return
	}
	// raw、ok 保存公网限制值及本次变更是否包含该设置。
	if raw, ok := values["outbound_http_public_only"]; ok {
		s.outboundPolicy.SetPublicOnly(strings.EqualFold(strings.TrimSpace(raw), "true"))
	}
}

// validSecretAction 判断敏感设置命令是否具有受支持的三态语义。
func validSecretAction(change SecretChange) bool {
	switch change.Action {
	case "retain", "replace", "clear":
		return true
	default:
		return false
	}
}

// audit 写入排序去重后的敏感设置键名，确保审计内容不包含秘密值。
func (s *Service) audit(ctx context.Context, record AuditRecord) error {
	if len(record.Keys) == 0 {
		return nil
	}
	// normalizedKeys 保存排序去重后的键名。
	normalizedKeys := make([]string, 0, len(record.Keys))
	// seen 保存已经加入审计记录的键名。
	seen := make(map[string]struct{}, len(record.Keys))
	// key 是当前待规范化的敏感设置键名。
	for _, key := range record.Keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		// exists 表示键名是否已经加入当前审计记录。
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalizedKeys = append(normalizedKeys, key)
	}
	sort.Strings(normalizedKeys)
	record.Keys = normalizedKeys
	if record.Outcome == "" {
		record.Outcome = "accepted"
	}
	return s.repository.AddAudit(ctx, record)
}
