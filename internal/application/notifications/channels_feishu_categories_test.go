package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestFeishuChannelCategoriesReadDefaultsTrueWhenMissing 验证缺少分类字段的飞书渠道按 true 返回。
func TestFeishuChannelCategoriesReadDefaultsTrueWhenMissing(t *testing.T) {
	// repository 是保存飞书渠道完整配置的端口替身。
	repository := &channelRepositoryStub{record: &ChannelRecord{ID: 7, Type: feishuAppChannelType, Name: "飞书应用", Config: `{"app_id":"cli","app_secret":"secret-read","open_id":"ou","domain":"feishu"}`}}
	// service 是待验证的渠道应用服务。
	service := NewChannelService(repository, nil)
	// categories、readErr 保存分类开关读取结果及错误。
	categories, readErr := service.GetFeishuChannelCategories(context.Background(), 7, 7)
	if readErr != nil || !categories.Warning || !categories.Info {
		t.Fatalf("缺省分类开关异常: categories=%+v err=%v", categories, readErr)
	}
}

// TestFeishuChannelCategoriesReadExplicitFalse 验证显式 false 的开关按配置返回。
func TestFeishuChannelCategoriesReadExplicitFalse(t *testing.T) {
	// repository 是保存两个开关均为 false 的飞书渠道端口替身。
	repository := &channelRepositoryStub{record: &ChannelRecord{ID: 7, Type: feishuAppChannelType, Config: `{"notify_warning":false,"notify_info":false}`}}
	// service 是待验证的渠道应用服务。
	service := NewChannelService(repository, nil)
	// categories、readErr 保存显式开关读取结果。
	categories, readErr := service.GetFeishuChannelCategories(context.Background(), 7, 7)
	if readErr != nil || categories.Warning || categories.Info {
		t.Fatalf("显式开关异常: categories=%+v err=%v", categories, readErr)
	}
}

// TestFeishuChannelCategoriesReadRejectsUnsupportedResources 验证读取拒绝非飞书渠道、缺失渠道和非法配置。
func TestFeishuChannelCategoriesReadRejectsUnsupportedResources(t *testing.T) {
	// cases 保存读取错误场景。
	cases := []struct {
		// name 是测试样例名称。
		name string
		// repository 是预置渠道状态的端口替身。
		repository *channelRepositoryStub
		// wantErr 是期望的错误类型。
		wantErr error
	}{
		{name: "非飞书渠道", repository: &channelRepositoryStub{record: &ChannelRecord{ID: 7, Type: "webhook", Config: `{}`}}, wantErr: ErrChannelInvalidInput},
		{name: "渠道不存在", repository: &channelRepositoryStub{}, wantErr: ErrChannelNotFound},
		{name: "归属错误", repository: &channelRepositoryStub{recordErr: ErrChannelForbidden}, wantErr: ErrChannelForbidden},
		{name: "非法配置", repository: &channelRepositoryStub{record: &ChannelRecord{ID: 7, Type: feishuAppChannelType, Config: `not-json`}}},
	}
	// testCase 表示当前读取错误场景。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// categories、readErr 保存当前样例读取结果。
			categories, readErr := (&ChannelService{repository: testCase.repository}).GetFeishuChannelCategories(context.Background(), 7, 7)
			if readErr == nil {
				t.Fatalf("错误场景应返回错误: categories=%+v", categories)
			}
			if testCase.wantErr != nil && !errors.Is(readErr, testCase.wantErr) {
				t.Fatalf("错误类型=%v want=%v", readErr, testCase.wantErr)
			}
			if strings.Contains(readErr.Error(), "secret-read") {
				t.Fatalf("读取错误泄漏配置值: %v", readErr)
			}
		})
	}
}

// TestFeishuChannelCategoriesSetPreservesSensitiveConfig 验证写回只修改两个开关并保留应用凭据和未知字段。
func TestFeishuChannelCategoriesSetPreservesSensitiveConfig(t *testing.T) {
	// repository 是保存完整飞书配置并记录更新记录的端口替身。
	repository := &channelRepositoryStub{record: &ChannelRecord{ID: 7, Type: feishuAppChannelType, Name: "飞书应用", EventTypes: `["chat_message_received"]`, Enabled: true, Config: `{"app_id":"cli-set","app_secret":"secret-set","open_id":"ou-set","domain":"lark","extra":"keep-me"}`}}
	// service 是待验证的渠道应用服务。
	service := NewChannelService(repository, nil)
	// setErr 保存分类开关写回结果。
	setErr := service.SetFeishuChannelCategories(context.Background(), 7, 7, FeishuChannelCategories{Warning: false, Info: true})
	if setErr != nil {
		t.Fatalf("SetFeishuChannelCategories: %v", setErr)
	}
	if repository.updatedRecord == nil {
		t.Fatal("分类开关更新未写入端口")
	}
	// fields 保存写回配置的原始字段，用于确认敏感值和未知字段未被覆盖。
	var fields map[string]json.RawMessage
	// unmarshalErr 是写回配置解析失败原因；nil 表示配置是 JSON 对象。
	if unmarshalErr := json.Unmarshal([]byte(repository.updatedRecord.Config), &fields); unmarshalErr != nil {
		t.Fatalf("解析写回配置失败: %v", unmarshalErr)
	}
	// expectedStrings 保存必须保持原值的字符串字段。
	expectedStrings := map[string]string{"app_id": "cli-set", "app_secret": "secret-set", "open_id": "ou-set", "domain": "lark", "extra": "keep-me"}
	// key、want 表示当前待校验的字符串字段与原值。
	for key, want := range expectedStrings {
		// got 是写回配置中当前字段的字符串值。
		var got string
		// unmarshalErr 是当前字段解析失败原因；nil 表示字段类型仍为字符串。
		if unmarshalErr := json.Unmarshal(fields[key], &got); unmarshalErr != nil || got != want {
			t.Fatalf("字段 %s=%q want=%q err=%v", key, got, want, unmarshalErr)
		}
	}
	// warning、warningErr 保存写回配置中的警告类开关。
	var warning bool
	// warningErr 是写回配置中警告类开关解析失败原因；nil 且值为 false 表示写回成功。
	if warningErr := json.Unmarshal(fields["notify_warning"], &warning); warningErr != nil || warning {
		t.Fatalf("notify_warning=%v err=%v", warning, warningErr)
	}
	// info、infoErr 保存写回配置中的普通提示类开关。
	var info bool
	// infoErr 是写回配置中普通提示类开关解析失败原因；nil 且值为 true 表示写回成功。
	if infoErr := json.Unmarshal(fields["notify_info"], &info); infoErr != nil || !info {
		t.Fatalf("notify_info=%v err=%v", info, infoErr)
	}
	if repository.updatedRecord.Name != "飞书应用" || repository.updatedRecord.Enabled != true || repository.updatedRecord.EventTypes != `["chat_message_received"]` {
		t.Fatalf("非配置字段被意外修改: %+v", repository.updatedRecord)
	}
}

// TestFeishuChannelCategoriesSetRejectsUnsupportedResources 验证写入拒绝非飞书渠道、缺失渠道、非法配置和存储错误。
func TestFeishuChannelCategoriesSetRejectsUnsupportedResources(t *testing.T) {
	// cases 保存写入错误场景。
	cases := []struct {
		// name 是测试样例名称。
		name string
		// repository 是预置渠道状态的端口替身。
		repository *channelRepositoryStub
		// wantErr 是期望的错误类型；nil 表示只要求返回错误。
		wantErr error
	}{
		{name: "非飞书渠道", repository: &channelRepositoryStub{record: &ChannelRecord{ID: 7, Type: "webhook", Config: `{}`}}, wantErr: ErrChannelInvalidInput},
		{name: "渠道不存在", repository: &channelRepositoryStub{}, wantErr: ErrChannelNotFound},
		{name: "归属错误", repository: &channelRepositoryStub{recordErr: ErrChannelForbidden}, wantErr: ErrChannelForbidden},
		{name: "非法配置", repository: &channelRepositoryStub{record: &ChannelRecord{ID: 7, Type: feishuAppChannelType, Config: `not-json`}}},
		{name: "更新失败", repository: &channelRepositoryStub{record: &ChannelRecord{ID: 7, Type: feishuAppChannelType, Config: `{"app_secret":"secret-write"}`}, updateErr: errors.New("update failed")}},
	}
	// testCase 表示当前写入错误场景。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// setErr 保存当前样例写回结果。
			setErr := (&ChannelService{repository: testCase.repository}).SetFeishuChannelCategories(context.Background(), 7, 7, FeishuChannelCategories{Warning: true, Info: false})
			if setErr == nil {
				t.Fatal("错误场景应返回错误")
			}
			if testCase.wantErr != nil && !errors.Is(setErr, testCase.wantErr) {
				t.Fatalf("错误类型=%v want=%v", setErr, testCase.wantErr)
			}
			if strings.Contains(setErr.Error(), "secret-write") {
				t.Fatalf("写入错误泄漏配置值: %v", setErr)
			}
		})
	}
}

// TestFeishuChannelCategoriesRejectsInvalidInput 验证零值服务、非法用户和非法渠道标识直接失败。
func TestFeishuChannelCategoriesRejectsInvalidInput(t *testing.T) {
	// nilService 是零值渠道服务。
	var nilService *ChannelService
	// categories、readErr 保存 nil 服务读取结果。
	categories, readErr := nilService.GetFeishuChannelCategories(context.Background(), 7, 7)
	if readErr == nil || !errors.Is(readErr, ErrChannelInvalidInput) || categories.Warning || categories.Info {
		t.Fatalf("nil 服务读取异常: categories=%+v err=%v", categories, readErr)
	}
	// repository 是空渠道端口替身。
	repository := &channelRepositoryStub{}
	// service 是只注入端口的最小服务。
	service := NewChannelService(repository, nil)
	// invalidCases 保存非法输入样例。
	invalidCases := []struct {
		// userID 是用户标识。
		userID int64
		// channelID 是渠道标识。
		channelID int64
	}{
		{userID: 0, channelID: 7},
		{userID: 7, channelID: 0},
	}
	// invalidCase 表示当前非法输入样例。
	for _, invalidCase := range invalidCases {
		// err 是当前非法输入样例的读取结果，必须为输入错误。
		if _, err := service.GetFeishuChannelCategories(context.Background(), invalidCase.userID, invalidCase.channelID); !errors.Is(err, ErrChannelInvalidInput) {
			t.Fatalf("非法读取输入应返回输入错误: user=%d channel=%d err=%v", invalidCase.userID, invalidCase.channelID, err)
		}
		// err 是当前非法输入样例的写入结果，必须为输入错误。
		if err := service.SetFeishuChannelCategories(context.Background(), invalidCase.userID, invalidCase.channelID, FeishuChannelCategories{}); !errors.Is(err, ErrChannelInvalidInput) {
			t.Fatalf("非法写入输入应返回输入错误: user=%d channel=%d err=%v", invalidCase.userID, invalidCase.channelID, err)
		}
	}
}
