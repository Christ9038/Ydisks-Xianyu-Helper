package settings

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// settingsRepositoryFake 是设置应用服务测试使用的内存 Port。
type settingsRepositoryFake struct {
	// sensitiveKeys 保存测试环境允许的敏感设置键。
	sensitiveKeys []string
	// audits 保存收到的非敏感审计记录。
	audits []AuditRecord
	// auditErr 模拟敏感审计存储故障。
	auditErr error
	// values 保存最近一次系统设置普通值。
	values map[string]string
	// secrets 保存最近一次系统设置敏感命令。
	secrets map[string]SecretChange
	// ownerID 保存账号所有者标识。
	ownerID int64
	// aiSettings 保存账号 AI 设置摘要。
	aiSettings map[string]AIReplySettings
	// systemValues 保存系统设置读取值。
	systemValues map[string]string
	// adjustRuleEnabled 表示测试账号是否已有启用的固定自动改价规则。
	adjustRuleEnabled bool
}

// IsSensitiveSettingKey 判断测试设置键是否属于敏感集合。
func (r *settingsRepositoryFake) IsSensitiveSettingKey(key string) bool {
	// candidate 是测试敏感键集合中的当前候选值。
	for _, candidate := range r.sensitiveKeys {
		if candidate == key {
			return true
		}
	}
	return false
}

// SensitiveSettingKeys 返回测试敏感设置键名。
func (r *settingsRepositoryFake) SensitiveSettingKeys() []string {
	return append([]string(nil), r.sensitiveKeys...)
}

// PublicSystem 返回测试公开设置。
func (r *settingsRepositoryFake) PublicSystem(context.Context) (map[string]string, error) {
	return map[string]string{"theme_color": "blue"}, nil
}

// RedactedSystem 返回测试脱敏设置。
func (r *settingsRepositoryFake) RedactedSystem(context.Context) (map[string]string, error) {
	return map[string]string{"ai_api_key_configured": "true"}, nil
}

// GetSystem 返回测试系统设置。
func (r *settingsRepositoryFake) GetSystem(_ context.Context, key string) (string, error) {
	return r.systemValues[key], nil
}

// ReadSensitiveSystem 返回测试敏感设置值并模拟数据库已完成审计。
func (r *settingsRepositoryFake) ReadSensitiveSystem(_ context.Context, _ int64, _, _, _ string) (string, error) {
	return "stored-secret", nil
}

// ApplySystemChanges 保存测试系统设置变更。
func (r *settingsRepositoryFake) ApplySystemChanges(_ context.Context, values map[string]string, secrets map[string]SecretChange) error {
	r.values = values
	r.secrets = secrets
	return nil
}

// SetSystem 保存测试单项系统设置。
func (r *settingsRepositoryFake) SetSystem(_ context.Context, key, value string) error {
	if r.systemValues == nil {
		r.systemValues = make(map[string]string)
	}
	r.systemValues[key] = value
	return nil
}

// AddAudit 保存测试审计记录。
func (r *settingsRepositoryFake) AddAudit(_ context.Context, record AuditRecord) error {
	if r.auditErr != nil {
		return r.auditErr
	}
	r.audits = append(r.audits, record)
	return nil
}

// ListUser 返回测试用户设置。
func (r *settingsRepositoryFake) ListUser(context.Context, int64) (map[string]string, error) {
	return map[string]string{"theme": "dark"}, nil
}

// GetUser 返回测试用户单项设置。
func (r *settingsRepositoryFake) GetUser(context.Context, int64, string) (string, error) {
	return "dark", nil
}

// SetUser 保存测试用户单项设置。
func (r *settingsRepositoryFake) SetUser(context.Context, int64, string, string) error { return nil }

// CheckOwnership 返回测试账号所有者。
func (r *settingsRepositoryFake) CheckOwnership(context.Context, int64, string) (int64, error) {
	if r.ownerID == 0 {
		return 0, ErrAccountNotFound
	}
	return r.ownerID, nil
}

// ListAIReply 返回测试账号 AI 设置列表。
func (r *settingsRepositoryFake) ListAIReply(context.Context, int64) ([]AIReplySettings, error) {
	// result 保存测试账号 AI 设置列表。
	result := make([]AIReplySettings, 0, len(r.aiSettings))
	// setting 是当前待复制的测试账号 AI 设置。
	for _, setting := range r.aiSettings {
		result = append(result, setting)
	}
	return result, nil
}

// GetAIReply 返回测试账号 AI 设置。
func (r *settingsRepositoryFake) GetAIReply(_ context.Context, _ int64, cookieID string) (AIReplySettings, error) {
	// setting、ok 保存测试账号 AI 设置及是否存在。
	setting, ok := r.aiSettings[cookieID]
	if !ok {
		return AIReplySettings{}, ErrConfigNotFound
	}
	return setting, nil
}

// UpsertAIReply 保存测试账号 AI 设置。
func (r *settingsRepositoryFake) UpsertAIReply(_ context.Context, cookieID string, setting AIReplySettings) error {
	if r.aiSettings == nil {
		r.aiSettings = make(map[string]AIReplySettings)
	}
	r.aiSettings[cookieID] = setting
	return nil
}

// HasEnabledAdjustPriceRule 返回测试配置的固定自动改价规则状态。
func (r *settingsRepositoryFake) HasEnabledAdjustPriceRule(context.Context, string) (bool, error) {
	return r.adjustRuleEnabled, nil
}

// modelClientFake 是 AI 模型客户端测试替身。
type modelClientFake struct {
	// calls 保存收到的端点和密钥，测试只比较是否传递，不输出秘密。
	calls int
}

// Fetch 返回固定模型列表并记录调用次数。
func (c *modelClientFake) Fetch(context.Context, string, string) ([]string, error) {
	c.calls++
	return []string{"qwen-plus"}, nil
}

// TestServiceApplySystemChangesAuditsSecrets 验证敏感系统设置写入先审计再进入 Port。
func TestServiceApplySystemChangesAuditsSecrets(t *testing.T) {
	// repository 保存测试设置 Port。
	repository := &settingsRepositoryFake{sensitiveKeys: []string{"ai_api_key"}}
	// service 保存待测试的设置应用服务。
	service := NewService(repository, nil)
	// err 表示敏感系统设置写入结果。
	err := service.ApplySystemChanges(context.Background(), 7, map[string]string{"theme_color": "blue"}, map[string]SecretChange{"ai_api_key": {Action: "replace", Value: "secret"}})
	if err != nil {
		t.Fatalf("apply settings: %v", err)
	}
	if len(repository.audits) != 1 || repository.audits[0].Action != "settings.write" || repository.audits[0].Keys[0] != "ai_api_key" {
		t.Fatalf("unexpected audit records: %+v", repository.audits)
	}
	// forwarded 表示敏感命令是否完整传递到 Port；断言失败时不输出秘密值。
	forwarded, ok := repository.secrets["ai_api_key"]
	if !ok || forwarded.Action != "replace" || forwarded.Value != "secret" {
		t.Fatalf("secret command was not forwarded: action=%q present=%t", forwarded.Action, ok)
	}
}

// TestServiceRejectsSensitivePlainValue 验证敏感键不能通过普通设置值写入。
func TestServiceRejectsSensitivePlainValue(t *testing.T) {
	// repository 保存测试设置 Port。
	repository := &settingsRepositoryFake{sensitiveKeys: []string{"ai_api_key"}}
	// service 保存待测试的设置应用服务。
	service := NewService(repository, nil)
	// err 记录当前操作失败原因的普通敏感值写入结果。
	err := service.ApplySystemChanges(context.Background(), 7, map[string]string{"ai_api_key": "secret"}, nil)
	if err == nil || !strings.Contains(err.Error(), "敏感设置") {
		t.Fatalf("sensitive plain value should fail: %v", err)
	}
}

// TestServiceAIReplyOwnershipAndValidation 验证账号 AI 设置的归属和数值约束。
func TestServiceAIReplyOwnershipAndValidation(t *testing.T) {
	// repository 保存测试账号所有者及配置。
	repository := &settingsRepositoryFake{ownerID: 7, aiSettings: make(map[string]AIReplySettings)}
	// service 保存待测试的设置应用服务。
	service := NewService(repository, nil)
	// forbiddenErr 保存跨用户更新返回的权限错误。
	if forbiddenErr := service.UpsertAIReply(context.Background(), 8, "acc1", AIReplySettings{MaxDiscountPercent: 1, MaxDiscountAmount: 1, MaxBargainRounds: 1}); !errors.Is(forbiddenErr, ErrForbidden) {
		t.Fatalf("cross-user update error=%v", forbiddenErr)
	}
	// invalidErr 保存非法折扣边界返回的校验错误。
	if invalidErr := service.UpsertAIReply(context.Background(), 7, "acc1", AIReplySettings{MaxDiscountPercent: 101, MaxDiscountAmount: 1, MaxBargainRounds: 1}); invalidErr == nil {
		t.Fatal("invalid discount should fail")
	}
}

// TestServiceListAIModelsAuditsExplicitKey 验证外部传入 API 密钥也必须留下使用审计。
func TestServiceListAIModelsAuditsExplicitKey(t *testing.T) {
	// repository 保存测试系统设置 Port。
	repository := &settingsRepositoryFake{systemValues: map[string]string{"ai_api_url": "https://example.test/v1"}}
	// client 保存模型目录测试客户端。
	client := &modelClientFake{}
	// service 保存待测试的设置应用服务。
	service := NewService(repository, client)
	// models、err 保存模型目录结果和调用错误。
	models, err := service.ListAIModels(context.Background(), 7, "https://example.test/v1", "provided-secret")
	if err != nil || len(models) != 1 || client.calls != 1 {
		t.Fatalf("models=%v err=%v calls=%d", models, err, client.calls)
	}
	if len(repository.audits) != 1 || repository.audits[0].Resource != "ai_models" {
		t.Fatalf("missing model access audit: %+v", repository.audits)
	}
}

// TestServiceListAIModelsFailsClosedWhenAuditUnavailable 验证模型请求不会绕过敏感密钥审计。
func TestServiceListAIModelsFailsClosedWhenAuditUnavailable(t *testing.T) {
	// repository 保存会返回审计故障的设置 Port。
	repository := &settingsRepositoryFake{auditErr: errors.New("audit unavailable")}
	// client 保存模型目录测试客户端。
	client := &modelClientFake{}
	// service 保存待测试的设置应用服务。
	service := NewService(repository, client)
	// models、err 保存审计失败时的模型结果和错误。
	models, err := service.ListAIModels(context.Background(), 7, "https://example.test/v1", "provided-secret")
	if err == nil || models != nil || client.calls != 0 {
		t.Fatalf("audit failure should stop model request: models=%v err=%v calls=%d", models, err, client.calls)
	}
}

var _ Repository = (*settingsRepositoryFake)(nil)

// TestServiceRejectsConflictingPricingModes 验证自动改价依赖 AI 议价，且 AI 议价不能与固定改价规则同时启用。
func TestServiceRejectsConflictingPricingModes(t *testing.T) {
	// ctx 是 AI 设置互斥校验测试上下文。
	ctx := context.Background()
	// repository 模拟账号已存在启用的固定自动改价规则。
	repository := &settingsRepositoryFake{ownerID: 7, adjustRuleEnabled: true}
	// service 是待验证的设置应用服务。
	service := NewService(repository, nil)
	// settings 是开启 AI 议价的有效数值配置。
	settings := AIReplySettings{AIEnabled: true, MaxDiscountPercent: 10, MaxDiscountAmount: 100, MaxBargainRounds: 3}
	// err 是启用冲突 AI 模式时返回的互斥错误。
	if err := service.UpsertAIReply(ctx, 7, "account-1", settings); !errors.Is(err, ErrPricingModeConflict) {
		t.Fatalf("固定规则冲突应被拒绝: %v", err)
	}
	repository.adjustRuleEnabled = false
	settings.AIEnabled = false
	settings.AutoAdjustPriceEnabled = true
	// err 是脱离 AI 议价单独启用真实改价时返回的依赖错误。
	if err := service.UpsertAIReply(ctx, 7, "account-1", settings); err == nil || !strings.Contains(err.Error(), "必须先启用 AI 议价") {
		t.Fatalf("独立开启 AI 自动改价应被拒绝: %v", err)
	}
}

var _ ModelClient = (*modelClientFake)(nil)
