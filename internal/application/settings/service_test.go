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
	// systemErr 保存系统设置读取失败原因。
	systemErr error
	// readSecretErr 保存敏感系统设置读取失败原因。
	readSecretErr error
	// applyErr 保存系统设置原子写入失败原因。
	applyErr error
	// setSystemErr 保存单项系统设置写入失败原因。
	setSystemErr error
	// conflictErr 保存固定改价规则查询失败原因。
	conflictErr error
	// adjustRuleEnabled 表示测试账号是否已有启用的固定自动改价规则。
	adjustRuleEnabled bool
}

// outboundPolicyFake 记录应用服务要求立即生效的公网限制状态。
type outboundPolicyFake struct {
	// publicOnly 保存最近一次运行时公网限制状态。
	publicOnly bool
	// calls 保存策略切换次数，用于确认数据库保存成功后才切换。
	calls int
}

// SetPublicOnly 记录测试中的运行时策略切换。
func (p *outboundPolicyFake) SetPublicOnly(publicOnly bool) {
	p.publicOnly = publicOnly
	p.calls++
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
	if r.systemErr != nil {
		return "", r.systemErr
	}
	return r.systemValues[key], nil
}

// ReadSensitiveSystem 返回测试敏感设置值并模拟数据库已完成审计。
func (r *settingsRepositoryFake) ReadSensitiveSystem(_ context.Context, _ int64, _, _, _ string) (string, error) {
	if r.readSecretErr != nil {
		return "", r.readSecretErr
	}
	return "stored-secret", nil
}

// ApplySystemChanges 保存测试系统设置变更。
func (r *settingsRepositoryFake) ApplySystemChanges(_ context.Context, values map[string]string, secrets map[string]SecretChange) error {
	if r.applyErr != nil {
		return r.applyErr
	}
	r.values = values
	r.secrets = secrets
	return nil
}

// SetSystem 保存测试单项系统设置。
func (r *settingsRepositoryFake) SetSystem(_ context.Context, key, value string) error {
	if r.setSystemErr != nil {
		return r.setSystemErr
	}
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
	if r.conflictErr != nil {
		return false, r.conflictErr
	}
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

// TestServiceAppliesOutboundPolicyAfterValidation 验证公网限制设置校验和运行时即时生效边界。
func TestServiceAppliesOutboundPolicyAfterValidation(t *testing.T) {
	// repository 是保存系统设置的内存 Port。
	repository := &settingsRepositoryFake{}
	// policy 是记录运行时开关状态的测试 Port。
	policy := &outboundPolicyFake{}
	// service 是注入策略 Port 的设置应用服务。
	service := NewService(repository, nil, policy)
	// err 表示合法公网限制设置的保存错误。
	if err := service.ApplySystemChanges(context.Background(), 7, map[string]string{"outbound_http_public_only": "true"}, nil); err != nil {
		t.Fatalf("保存公网限制失败: %v", err)
	}
	if !policy.publicOnly || policy.calls != 1 {
		t.Fatalf("公网限制未即时生效 policy=%+v", policy)
	}
	// err 表示非法公网限制设置的保存结果。
	if err := service.ApplySystemChanges(context.Background(), 7, map[string]string{"outbound_http_public_only": "maybe"}, nil); err == nil {
		t.Fatal("非法公网限制值必须拒绝")
	}
	if policy.calls != 1 {
		t.Fatalf("非法值不应切换运行时策略 policy=%+v", policy)
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

// TestServiceCoversSettingsAndAIReadWrite 验证系统、用户和账号 AI 设置的成功读写路径。
func TestServiceCoversSettingsAndAIReadWrite(t *testing.T) {
	// ctx 是设置成功路径共用的请求上下文。
	ctx := context.Background()
	// repository 是带有敏感键、系统值和账号所有者的内存设置端口。
	repository := &settingsRepositoryFake{
		sensitiveKeys: []string{"ai_api_key", "smtp_password"},
		systemValues:  map[string]string{"ai_api_url": "https://configured.test/v1"},
		ownerID:       7,
		aiSettings:    map[string]AIReplySettings{"acc-1": {CookieID: "acc-1", AIEnabled: true}},
	}
	// client 是返回固定模型目录的本地模型客户端。
	client := &modelClientFake{}
	// policy 是记录公网策略切换的运行时端口。
	policy := &outboundPolicyFake{}
	// service 是待验证的设置应用服务。
	service := NewService(repository, client, policy)
	if !service.IsSensitiveSettingKey("ai_api_key") || service.IsSensitiveSettingKey("theme_color") || (*Service)(nil).IsSensitiveSettingKey("ai_api_key") {
		t.Fatal("敏感键判断异常")
	}
	if // err 是公开系统设置读取错误。
	_, err := service.PublicSystem(ctx); err != nil {
		t.Fatalf("公开系统设置读取失败: %v", err)
	}
	if // err 是管理员系统设置读取及审计错误。
	_, err := service.GetSystem(ctx, 7); err != nil || len(repository.audits) != 1 {
		t.Fatalf("管理员系统设置读取失败: err=%v audits=%+v", err, repository.audits)
	}
	if // err 是普通系统设置写入错误。
	err := service.SetSystem(ctx, 7, "theme_color", "dark", ""); err != nil {
		t.Fatalf("普通系统设置写入失败: %v", err)
	}
	if // err 是敏感设置 retain 命令写入错误。
	err := service.SetSystem(ctx, 7, "ai_api_key", "", "retain"); err != nil {
		t.Fatalf("敏感 retain 写入失败: %v", err)
	}
	if // err 是敏感设置 clear 命令写入错误。
	err := service.SetSystem(ctx, 7, "ai_api_key", "", "clear"); err != nil {
		t.Fatalf("敏感 clear 写入失败: %v", err)
	}
	if // err 是敏感设置 replace 命令写入错误。
	err := service.SetSystem(ctx, 7, "ai_api_key", "new-secret", "replace"); err != nil {
		t.Fatalf("敏感 replace 写入失败: %v", err)
	}
	if // err 是用户设置列表读取错误。
	_, err := service.ListUser(ctx, 7); err != nil {
		t.Fatalf("用户设置列表读取失败: %v", err)
	}
	if // err 是用户设置单项读取错误。
	_, err := service.GetUser(ctx, 7, " theme "); err != nil {
		t.Fatalf("用户设置读取失败: %v", err)
	}
	if // err 是用户设置单项写入错误。
	err := service.SetUser(ctx, 7, " theme ", "light"); err != nil {
		t.Fatalf("用户设置写入失败: %v", err)
	}
	if // err 是账号 AI 设置列表读取错误。
	_, err := service.ListAIReply(ctx, 7); err != nil {
		t.Fatalf("账号 AI 设置列表读取失败: %v", err)
	}
	if // err 是账号 AI 设置读取错误。
	_, err := service.GetAIReply(ctx, 7, "acc-1"); err != nil {
		t.Fatalf("账号 AI 设置读取失败: %v", err)
	}
	if // err 是有效账号 AI 设置写入错误。
	err := service.UpsertAIReply(ctx, 7, "acc-1", AIReplySettings{AIEnabled: true, MaxDiscountPercent: 10, MaxDiscountAmount: 20, MaxBargainRounds: 3}); err != nil {
		t.Fatalf("账号 AI 设置写入失败: %v", err)
	}
	if // err 是显式地址和密钥读取模型目录的错误。
	_, err := service.ListAIModels(ctx, 7, "https://models.test/v1", "provided-key"); err != nil {
		t.Fatalf("显式模型目录读取失败: %v", err)
	}
	if // err 是从系统地址和受控密钥读取模型目录的错误。
	_, err := service.ListAIModels(ctx, 7, "", ""); err != nil {
		t.Fatalf("受控模型目录读取失败: %v", err)
	}
	if client.calls != 2 {
		t.Fatalf("模型客户端调用次数=%d，期望=2", client.calls)
	}
}

// TestServiceRejectsInvalidSettingsInputs 验证所有设置公开用例的身份、键名、秘密命令和数值边界校验。
func TestServiceRejectsInvalidSettingsInputs(t *testing.T) {
	// ctx 是输入校验共用的请求上下文。
	ctx := context.Background()
	// repository 是允许账号 7 访问但不保存秘密值的测试端口。
	repository := &settingsRepositoryFake{sensitiveKeys: []string{"secret_key"}, ownerID: 7, aiSettings: map[string]AIReplySettings{}}
	// service 是待验证的设置应用服务。
	service := NewService(repository, nil)
	// longKey 是超过设置键名上限的无效键。
	longKey := strings.Repeat("x", 101)
	// invalidCases 汇总公开设置用例的无效输入。
	invalidCases := []struct {
		// name 是当前无效输入场景名称。
		name string
		// run 执行当前场景并返回业务错误。
		run func() error
	}{
		{name: "public nil service", run: func() error {
			// err 是空服务公开设置查询的装配错误。
			_, err := (*Service)(nil).PublicSystem(ctx)
			return err
		}},
		{name: "get invalid user", run: func() error {
			// err 是无效用户管理员设置查询的身份错误。
			_, err := service.GetSystem(ctx, 0)
			return err
		}},
		{name: "apply invalid user", run: func() error { return service.ApplySystemChanges(ctx, 0, map[string]string{"theme": "dark"}, nil) }},
		{name: "apply empty", run: func() error { return service.ApplySystemChanges(ctx, 7, nil, nil) }},
		{name: "apply secret key", run: func() error { return service.ApplySystemChanges(ctx, 7, map[string]string{"secret_key": "x"}, nil) }},
		{name: "apply unknown secret", run: func() error {
			return service.ApplySystemChanges(ctx, 7, nil, map[string]SecretChange{"unknown": {Action: "clear"}})
		}},
		{name: "apply invalid action", run: func() error {
			return service.ApplySystemChanges(ctx, 7, nil, map[string]SecretChange{"secret_key": {Action: "invalid"}})
		}},
		{name: "apply blank replace", run: func() error {
			return service.ApplySystemChanges(ctx, 7, nil, map[string]SecretChange{"secret_key": {Action: "replace"}})
		}},
		{name: "set invalid user", run: func() error { return service.SetSystem(ctx, 0, "key", "value", "") }},
		{name: "set empty key", run: func() error { return service.SetSystem(ctx, 7, " ", "value", "") }},
		{name: "set long key", run: func() error { return service.SetSystem(ctx, 7, longKey, "value", "") }},
		{name: "set secret action", run: func() error { return service.SetSystem(ctx, 7, "secret_key", "", "invalid") }},
		{name: "set secret blank", run: func() error { return service.SetSystem(ctx, 7, "secret_key", "", "replace") }},
		{name: "set outbound value", run: func() error { return service.SetSystem(ctx, 7, "outbound_http_public_only", "maybe", "") }},
		{name: "list user invalid", run: func() error {
			// err 是无效用户偏好列表查询的身份错误。
			_, err := service.ListUser(ctx, 0)
			return err
		}},
		{name: "get user invalid", run: func() error {
			// err 是无效用户偏好查询的身份错误。
			_, err := service.GetUser(ctx, 0, "key")
			return err
		}},
		{name: "set user invalid", run: func() error { return service.SetUser(ctx, 0, "key", "value") }},
		{name: "set user empty key", run: func() error { return service.SetUser(ctx, 7, " ", "value") }},
		{name: "set user long key", run: func() error { return service.SetUser(ctx, 7, longKey, "value") }},
		{name: "list AI invalid", run: func() error {
			// err 是无效用户 AI 设置列表的身份错误。
			_, err := service.ListAIReply(ctx, 0)
			return err
		}},
		{name: "get AI empty account", run: func() error {
			// err 是空账号标识的归属错误。
			_, err := service.GetAIReply(ctx, 7, " ")
			return err
		}},
		{name: "get AI missing config", run: func() error {
			// err 是账号存在但 AI 设置缺失的配置错误。
			_, err := service.GetAIReply(ctx, 7, "missing")
			return err
		}},
		{name: "upsert invalid user", run: func() error { return service.UpsertAIReply(ctx, 0, "acc", AIReplySettings{}) }},
		{name: "upsert negative percent", run: func() error {
			return service.UpsertAIReply(ctx, 7, "acc", AIReplySettings{MaxDiscountPercent: -1, MaxBargainRounds: 1})
		}},
		{name: "upsert large percent", run: func() error {
			return service.UpsertAIReply(ctx, 7, "acc", AIReplySettings{MaxDiscountPercent: 101, MaxBargainRounds: 1})
		}},
		{name: "upsert negative amount", run: func() error {
			return service.UpsertAIReply(ctx, 7, "acc", AIReplySettings{MaxDiscountAmount: -1, MaxBargainRounds: 1})
		}},
		{name: "upsert zero rounds", run: func() error { return service.UpsertAIReply(ctx, 7, "acc", AIReplySettings{MaxBargainRounds: 0}) }},
		{name: "upsert large rounds", run: func() error { return service.UpsertAIReply(ctx, 7, "acc", AIReplySettings{MaxBargainRounds: 11}) }},
		{name: "upsert auto without AI", run: func() error {
			return service.UpsertAIReply(ctx, 7, "acc", AIReplySettings{AutoAdjustPriceEnabled: true, MaxBargainRounds: 1})
		}},
		{name: "models invalid user", run: func() error {
			// err 是无效用户模型目录查询的身份错误。
			_, err := service.ListAIModels(ctx, 0, "", "")
			return err
		}},
		{name: "models missing client", run: func() error {
			// err 是模型客户端未装配的初始化错误。
			_, err := NewService(repository, nil).ListAIModels(ctx, 7, "", "")
			return err
		}},
		{name: "user list nil service", run: func() error {
			// err 是空服务用户设置查询的装配错误。
			_, err := (*Service)(nil).ListUser(ctx, 1)
			return err
		}},
		{name: "apply empty key", run: func() error { return service.ApplySystemChanges(ctx, 7, map[string]string{" ": "dark"}, nil) }},
		{name: "apply long key", run: func() error { return service.ApplySystemChanges(ctx, 7, map[string]string{longKey: "dark"}, nil) }},
	}
	// testCase 是当前待执行的设置输入场景。
	for _, testCase := range invalidCases {
		t.Run(testCase.name, func(t *testing.T) {
			// err 是当前无效设置场景返回的业务错误。
			err := testCase.run()
			if err == nil {
				t.Fatal("无效设置输入应返回错误")
			}
		})
	}
	// auditErr 是空敏感键审计无需访问端口的结果。
	auditErr := service.audit(ctx, AuditRecord{})
	if auditErr != nil {
		t.Fatalf("空审计记录不应失败: %v", auditErr)
	}
	// ownershipService 是账号所有者查询返回账号不存在的设置服务。
	ownershipService := NewService(&settingsRepositoryFake{}, nil)
	if // err 是账号所有者查询失败错误。
	_, err := ownershipService.GetAIReply(ctx, 7, "account"); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("账号所有者查询错误异常: %v", err)
	}
	// auditKeys 是包含空白、重复和未排序键名的审计输入。
	auditKeys := AuditRecord{Keys: []string{" ", "z_key", "a_key", "z_key"}}
	if // err 是审计键名规范化后的写入结果。
	err := service.audit(ctx, auditKeys); err != nil || len(repository.audits) == 0 || strings.Join(repository.audits[len(repository.audits)-1].Keys, ",") != "a_key,z_key" {
		t.Fatalf("审计键名规范化异常: err=%v audits=%+v", err, repository.audits)
	}
}

// TestServicePropagatesSettingsPortErrors 验证审计、系统写入、规则查询和敏感读取错误原样传播。
func TestServicePropagatesSettingsPortErrors(t *testing.T) {
	// ctx 是错误传播测试上下文。
	ctx := context.Background()
	// auditFailure 是审计端口返回的错误。
	auditFailure := errors.New("audit failed")
	// auditRepository 是注入审计错误的设置端口。
	auditRepository := &settingsRepositoryFake{auditErr: auditFailure, sensitiveKeys: []string{"secret"}, ownerID: 7}
	// auditService 是注入审计错误的应用服务。
	auditService := NewService(auditRepository, nil)
	if // err 是 GetSystem 审计失败错误。
	_, err := auditService.GetSystem(ctx, 7); !errors.Is(err, auditFailure) {
		t.Fatalf("GetSystem 审计错误未透传: %v", err)
	}
	if // err 是 ApplySystemChanges 审计失败错误。
	err := auditService.ApplySystemChanges(ctx, 7, nil, map[string]SecretChange{"secret": {Action: "clear"}}); !errors.Is(err, auditFailure) {
		t.Fatalf("ApplySystemChanges 审计错误未透传: %v", err)
	}
	if // err 是 SetSystem 审计失败错误。
	err := auditService.SetSystem(ctx, 7, "secret", "", "clear"); !errors.Is(err, auditFailure) {
		t.Fatalf("SetSystem 审计错误未透传: %v", err)
	}
	// applyFailure 是系统设置原子写入错误。
	applyFailure := errors.New("apply failed")
	// applyService 是注入系统原子写入错误的应用服务。
	applyService := NewService(&settingsRepositoryFake{applyErr: applyFailure}, nil)
	if // err 是普通系统设置原子写入错误。
	err := applyService.ApplySystemChanges(ctx, 7, map[string]string{"theme": "dark"}, nil); !errors.Is(err, applyFailure) {
		t.Fatalf("ApplySystemChanges 写入错误未透传: %v", err)
	}
	// setFailure 是单项系统设置写入错误。
	setFailure := errors.New("set failed")
	// setService 是注入单项系统设置写入错误的应用服务。
	setService := NewService(&settingsRepositoryFake{setSystemErr: setFailure}, nil)
	if // err 是单项系统设置写入错误。
	err := setService.SetSystem(ctx, 7, "theme", "dark", ""); !errors.Is(err, setFailure) {
		t.Fatalf("SetSystem 写入错误未透传: %v", err)
	}
	// conflictFailure 是固定自动改价规则查询错误。
	conflictFailure := errors.New("rule lookup failed")
	// conflictService 是注入规则查询错误的应用服务。
	conflictService := NewService(&settingsRepositoryFake{ownerID: 7, conflictErr: conflictFailure}, nil)
	if // err 是 AI 议价规则查询错误。
	err := conflictService.UpsertAIReply(ctx, 7, "acc", AIReplySettings{AIEnabled: true, MaxBargainRounds: 1}); !errors.Is(err, conflictFailure) {
		t.Fatalf("规则查询错误未透传: %v", err)
	}
	// systemFailure 是系统 AI 地址读取错误。
	systemFailure := errors.New("system lookup failed")
	// systemService 是注入系统地址读取错误的应用服务。
	systemService := NewService(&settingsRepositoryFake{systemErr: systemFailure}, &modelClientFake{})
	if // err 是系统 AI 地址读取错误。
	_, err := systemService.ListAIModels(ctx, 7, "", "provided"); !errors.Is(err, systemFailure) {
		t.Fatalf("系统地址读取错误未透传: %v", err)
	}
	// secretFailure 是受控读取敏感 API 密钥错误。
	secretFailure := errors.New("secret lookup failed")
	// secretService 是注入敏感密钥读取错误的应用服务。
	secretService := NewService(&settingsRepositoryFake{readSecretErr: secretFailure}, &modelClientFake{})
	if // err 是敏感 API 密钥读取错误。
	_, err := secretService.ListAIModels(ctx, 7, "https://models.test", ""); !errors.Is(err, secretFailure) {
		t.Fatalf("敏感密钥读取错误未透传: %v", err)
	}
	// blankSystemService 是系统地址为空时使用默认地址的应用服务。
	blankSystemService := NewService(&settingsRepositoryFake{systemValues: map[string]string{"ai_api_url": ""}, readSecretErr: nil}, &modelClientFake{})
	if // err 是默认 AI 地址和受控密钥读取的成功结果。
	_, err := blankSystemService.ListAIModels(ctx, 7, "", ""); err != nil {
		t.Fatalf("默认 AI 地址读取失败: %v", err)
	}
}

var _ ModelClient = (*modelClientFake)(nil)
