package account

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestValidateConsignProofInput 覆盖凭证模式枚举、固定文字长度与“固定文字模式显式空文字”三类拒绝，以及合法输入放行。
func TestValidateConsignProofInput(t *testing.T) {
	// ptr 返回字符串指针，用于构造可选字段。
	ptr := func(value string) *string { return &value }
	// cases 是输入组合与是否应被拒绝。
	cases := []struct {
		// name 是子测试名称。
		name string
		// mode、text 是本次请求中可选的凭证模式与固定文字。
		mode, text *string
		// wantErr 表示是否应返回 ErrInvalidConsignProof。
		wantErr bool
	}{
		{name: "均未提供"},
		{name: "卡密全文", mode: ptr(ConsignProofModeDelivery)},
		{name: "不携带", mode: ptr(ConsignProofModeNone)},
		{name: "固定文字带内容", mode: ptr(ConsignProofModeFixed), text: ptr("已通过聊天发送")},
		{name: "只切换固定模式由数据库复核", mode: ptr(ConsignProofModeFixed)},
		{name: "固定文字恰好上限", text: ptr(strings.Repeat("字", ConsignProofTextMaxRunes))},
		{name: "未知模式", mode: ptr("all"), wantErr: true},
		{name: "固定文字超长", text: ptr(strings.Repeat("字", ConsignProofTextMaxRunes+1)), wantErr: true},
		{name: "固定模式显式空白文字", mode: ptr(ConsignProofModeFixed), text: ptr("  "), wantErr: true},
	}
	// tc 是当前子测试用例。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// err 是校验结果。
			err := validateConsignProofInput(tc.mode, tc.text)
			if tc.wantErr != errors.Is(err, ErrInvalidConsignProof) || (!tc.wantErr && err != nil) {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

// TestUpdateSettingsRejectsInvalidConsignProofBeforeLocking 验证非法凭证输入在取凭证锁、写库之前被拒绝。
func TestUpdateSettingsRejectsInvalidConsignProofBeforeLocking(t *testing.T) {
	// repository 记录是否取锁。
	repository := &fakeSettingsRepository{}
	// service 是被测账号设置服务。
	service, err := NewSettingsService(repository, nil)
	if err != nil {
		t.Fatal(err)
	}
	// mode 是非法的凭证模式。
	mode := "bogus"
	// updateErr 必须是凭证校验错误。
	_, updateErr := service.UpdateSettings(context.Background(), SettingsUpdateInput{UserID: 1, AccountID: "acc", ConsignProofMode: &mode})
	if !errors.Is(updateErr, ErrInvalidConsignProof) || repository.lockCalls != 0 {
		t.Fatalf("err=%v lockCalls=%d", updateErr, repository.lockCalls)
	}
}
