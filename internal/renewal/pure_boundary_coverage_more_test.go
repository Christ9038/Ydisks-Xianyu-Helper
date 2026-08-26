package renewal

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestSchedulerSettingsNilStoreDefaults 覆盖续期设置仓储未装配时的安全默认值。
func TestSchedulerSettingsNilStoreDefaults(t *testing.T) {
	// scheduler 用于验证无数据库依赖时的配置读取边界。
	scheduler := NewScheduler(nil, nil, nil, nil)
	// ctx 用于控制本次设置读取的生命周期。
	ctx := context.Background()
	if scheduler.settingConfigured(ctx, "missing") {
		t.Fatal("nil store must not report a configured setting")
	}
	if !scheduler.settingEnabled(ctx, "missing", true) || scheduler.settingEnabled(ctx, "missing", false) {
		t.Fatal("settingEnabled must preserve caller default")
	}
	if scheduler.settingInterval(ctx, "missing", 3*time.Second) != 3*time.Second {
		t.Fatal("settingInterval must preserve caller default")
	}
	if scheduler.settingInt(ctx, "missing", 7) != 7 {
		t.Fatal("settingInt must preserve caller default")
	}
	if !scheduler.apiRenewEnabled(ctx) {
		t.Fatal("API renewal must remain enabled by safe default")
	}
	if scheduler.apiRenewInterval(ctx) != apiCookieRenewInterval {
		t.Fatal("API renewal interval must preserve safe default")
	}
}

// TestNewBatchIDProducesOpaqueUniqueValue 覆盖续期批次标识的基本格式与随机性边界。
func TestNewBatchIDProducesOpaqueUniqueValue(t *testing.T) {
	// first、second 保存两次批次标识生成结果。
	first, second := newBatchID(), newBatchID()
	if len(first) != 32 || len(second) != 32 || first == second || strings.TrimSpace(first) == "" || strings.TrimSpace(second) == "" {
		t.Fatalf("batch IDs first=%q second=%q", first, second)
	}
}
