package account

import (
	"context"
	"errors"
	"testing"
)

// TestValidateManualTakeoverInput 覆盖分钟数边界、未知范围与 nil 字段保持原值。
func TestValidateManualTakeoverInput(t *testing.T) {
	// intValue、strValue 构造可选字段指针。
	intValue := func(value int) *int { return &value }
	// strValue 构造可选字符串指针。
	strValue := func(value string) *string { return &value }
	// cases 是表驱动用例。
	cases := []struct {
		// name 是用例名称。
		name string
		// minutes、scope 是待校验输入。
		minutes *int
		scope   *string
		// wantErr 表示是否应返回校验错误。
		wantErr bool
	}{
		{name: "全部省略", wantErr: false},
		{name: "关闭", minutes: intValue(0), wantErr: false},
		{name: "上限", minutes: intValue(ManualTakeoverMaxMinutes), scope: strValue(ManualTakeoverScopeAll), wantErr: false},
		{name: "负数", minutes: intValue(-1), wantErr: true},
		{name: "超上限", minutes: intValue(ManualTakeoverMaxMinutes + 1), wantErr: true},
		{name: "只暂停 AI", scope: strValue(ManualTakeoverScopeAI), wantErr: false},
		{name: "未知范围", scope: strValue("agent"), wantErr: true},
	}
	// tc 是当前用例。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// err 是校验结果。
			err := validateManualTakeoverInput(tc.minutes, tc.scope)
			if tc.wantErr != (err != nil) || (err != nil && !errors.Is(err, ErrInvalidManualTakeover)) {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

// TestUpdateSettingsRejectsInvalidManualTakeoverBeforeLocking 验证非法人工介入输入在取凭证锁、写库之前被拒绝。
func TestUpdateSettingsRejectsInvalidManualTakeoverBeforeLocking(t *testing.T) {
	// repository 记录是否取锁。
	repository := &fakeSettingsRepository{}
	// service 是被测账号设置服务。
	service, err := NewSettingsService(repository, nil)
	if err != nil {
		t.Fatal(err)
	}
	// minutes 是越界分钟数。
	minutes := 5000
	// updateErr 必须是人工介入校验错误。
	_, updateErr := service.UpdateSettings(context.Background(), SettingsUpdateInput{UserID: 1, AccountID: "acc", ManualTakeoverMinutes: &minutes})
	if !errors.Is(updateErr, ErrInvalidManualTakeover) || repository.lockCalls != 0 {
		t.Fatalf("err=%v lockCalls=%d", updateErr, repository.lockCalls)
	}
}
