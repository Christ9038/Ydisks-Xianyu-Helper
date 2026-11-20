package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// feishuRegistrarStub 是飞书扫码注册端口的可控测试替身。
type feishuRegistrarStub struct {
	// mu 保护轮询计数、请求记录和轮询结果序列，支持并发状态查询测试。
	mu sync.Mutex
	// beginResult 是 BeginRegistration 返回的预置结果。
	beginResult FeishuDeviceCode
	// beginErr 是 BeginRegistration 返回的预置错误。
	beginErr error
	// pollResults 是按顺序返回的轮询结果。
	pollResults []FeishuPollResult
	// pollErr 是 PollRegistration 返回的预置错误。
	pollErr error
	// pollCalls 是已执行的轮询次数。
	pollCalls int
	// pollRequests 保存每次轮询请求，便于验证域名与设备码。
	pollRequests []FeishuPollRequest
	// pollStarted 在轮询进入阻塞点前发出信号；nil 表示不通知。
	pollStarted chan struct{}
	// pollGate 在非 nil 时阻塞轮询，直到测试关闭通道。
	pollGate chan struct{}
}

// BeginRegistration 返回预置注册结果或错误；未配置结果时返回可直接建立会话的默认扫码结果。
func (r *feishuRegistrarStub) BeginRegistration(context.Context) (FeishuDeviceCode, error) {
	if r.beginErr != nil {
		return FeishuDeviceCode{}, r.beginErr
	}
	if strings.TrimSpace(r.beginResult.DeviceCode) == "" {
		return FeishuDeviceCode{DeviceCode: "device-stub", QRCodeURL: "https://example.com/qr", ExpiresIn: 5 * time.Minute, Interval: 5 * time.Second}, nil
	}
	return r.beginResult, nil
}

// PollRegistration 依次返回预置轮询结果，并可阻塞以验证并发轮询抑制。
func (r *feishuRegistrarStub) PollRegistration(_ context.Context, request FeishuPollRequest) (FeishuPollResult, error) {
	r.mu.Lock()
	r.pollCalls++
	r.pollRequests = append(r.pollRequests, request)
	// index 是本次轮询应返回的结果下标。
	index := r.pollCalls - 1
	r.mu.Unlock()
	if r.pollStarted != nil {
		r.pollStarted <- struct{}{}
	}
	if r.pollGate != nil {
		<-r.pollGate
	}
	if r.pollErr != nil {
		return FeishuPollResult{}, r.pollErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.pollResults) == 0 {
		return FeishuPollResult{Status: FeishuPollPending, Domain: request.Domain}, nil
	}
	if index >= len(r.pollResults) {
		index = len(r.pollResults) - 1
	}
	return r.pollResults[index], nil
}

// pollCallCount 返回线程安全的轮询次数。
func (r *feishuRegistrarStub) pollCallCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pollCalls
}

// fixedFeishuService 构造使用固定时间和固定会话标识的飞书渠道服务，便于确定状态断言。
func fixedFeishuService(repository ChannelRepository, registrar FeishuRegistrar, currentTime func() time.Time) *ChannelService {
	// service 是注入测试端口与固定时钟的渠道应用服务。
	service := NewChannelServiceWithFeishu(repository, nil, registrar)
	service.now = currentTime
	service.newSessionID = func() (string, error) { return "feishu-session-1", nil }
	return service
}

// TestFeishuBindingStartSchedulesPollAndReturnsStableSession 验证开始绑定返回扫码地址并等待轮询间隔。
func TestFeishuBindingStartSchedulesPollAndReturnsStableSession(t *testing.T) {
	// baseTime 是测试会话的固定创建时间。
	baseTime := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	// currentTime 保存可推进的测试时钟值。
	currentTime := baseTime
	// repository 是记录渠道与绑定写入的端口替身。
	repository := &channelRepositoryStub{}
	// registrar 是返回固定 begin 结果的协议替身。
	registrar := &feishuRegistrarStub{beginResult: FeishuDeviceCode{DeviceCode: "device-1", QRCodeURL: "https://accounts.feishu.cn/qr?code=abc", ExpiresIn: 10 * time.Minute, Interval: 30 * time.Second}}
	// service 是待验证的飞书渠道应用服务。
	service := fixedFeishuService(repository, registrar, func() time.Time { return currentTime })
	// start、startErr 保存开始绑定结果及错误。
	start, startErr := service.StartFeishuAppBinding(context.Background(), 7, FeishuBindingOptions{BindAllAccounts: true})
	if startErr != nil {
		t.Fatalf("StartFeishuAppBinding: %v", startErr)
	}
	if start.SessionID != "feishu-session-1" || start.QRCodeURL != "https://accounts.feishu.cn/qr?code=abc" || start.ExpiresIn != 600 {
		t.Fatalf("开始绑定结果异常: %+v", start)
	}
	// immediate、immediateErr 保存尚未到达轮询时间的状态查询结果。
	immediate, immediateErr := service.GetFeishuAppBindingStatus(context.Background(), 7, start.SessionID)
	if immediateErr != nil || immediate.Status != FeishuBindingStatusWaiting {
		t.Fatalf("初次状态异常: status=%+v err=%v", immediate, immediateErr)
	}
	if registrar.pollCallCount() != 0 {
		t.Fatalf("轮询过早: calls=%d", registrar.pollCallCount())
	}
	// currentTime 推进到允许下一次轮询的时间。
	currentTime = baseTime.Add(31 * time.Second)
	// waited、waitedErr 保存轮询后的等待状态。
	waited, waitedErr := service.GetFeishuAppBindingStatus(context.Background(), 7, start.SessionID)
	if waitedErr != nil || waited.Status != FeishuBindingStatusWaiting {
		t.Fatalf("轮询后状态异常: status=%+v err=%v", waited, waitedErr)
	}
	if registrar.pollCallCount() != 1 || registrar.pollRequests[0].Domain != "feishu" || registrar.pollRequests[0].DeviceCode != "device-1" {
		t.Fatalf("轮询请求异常: calls=%d requests=%+v", registrar.pollCallCount(), registrar.pollRequests)
	}
}

// TestFeishuBindingSuccessCreatesChannelAndBindsOwnedAccounts 验证成功结果保存加密配置、默认事件类型并绑定全部账号。
func TestFeishuBindingSuccessCreatesChannelAndBindsOwnedAccounts(t *testing.T) {
	// baseTime 是测试会话的固定创建时间。
	baseTime := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	// currentTime 保存可推进的测试时钟值。
	currentTime := baseTime
	// repository 是记录渠道创建、更新与账号绑定的端口替身。
	repository := &channelRepositoryStub{ownedAccountIDs: []string{"cid-1", "cid-2"}}
	// registrar 是返回成功结果的协议替身，租户品牌为 lark。
	registrar := &feishuRegistrarStub{pollResults: []FeishuPollResult{{Status: FeishuPollSuccess, Domain: "lark", AppID: "cli-1", AppSecret: "secret-1", OpenID: "ou-1"}}}
	// service 是待验证的飞书渠道应用服务。
	service := fixedFeishuService(repository, registrar, func() time.Time { return currentTime })
	// start、startErr 保存开始绑定结果。
	start, startErr := service.StartFeishuAppBinding(context.Background(), 7, FeishuBindingOptions{BindAllAccounts: true, NotifyWarning: false, NotifyInfo: true})
	if startErr != nil {
		t.Fatalf("StartFeishuAppBinding: %v", startErr)
	}
	// currentTime 推进到允许轮询的时间。
	currentTime = baseTime.Add(6 * time.Second)
	// status、statusErr 保存成功状态结果。
	status, statusErr := service.GetFeishuAppBindingStatus(context.Background(), 7, start.SessionID)
	if statusErr != nil || status.Status != FeishuBindingStatusSuccess || status.ChannelID != 41 {
		t.Fatalf("成功状态异常: status=%+v err=%v", status, statusErr)
	}
	if repository.createdInput.Type != feishuAppChannelType || repository.createdInput.Name != feishuAppChannelName || repository.createdInput.EventTypes != feishuAppDefaultEventTypes || !repository.createdInput.Enabled {
		t.Fatalf("渠道创建输入异常: %+v", repository.createdInput)
	}
	// config 保存解析后的渠道配置，仅用于测试断言。
	var config feishuAppChannelConfig
	// unmarshalErr 是渠道配置 JSON 解析失败原因；nil 表示配置可解析。
	if unmarshalErr := json.Unmarshal([]byte(repository.createdInput.Config), &config); unmarshalErr != nil {
		t.Fatalf("解析渠道配置失败: %v", unmarshalErr)
	}
	if config.AppID != "cli-1" || config.AppSecret != "secret-1" || config.OpenID != "ou-1" || config.Domain != "lark" {
		t.Fatalf("渠道配置异常: %+v", config)
	}
	if config.NotifyWarning || !config.NotifyInfo {
		t.Fatalf("分类开关未按扫码会话落库: warning=%v info=%v", config.NotifyWarning, config.NotifyInfo)
	}
	if repository.createdInput.Config == "" || strings.Contains(status.Message, "secret-1") {
		t.Fatalf("配置或状态泄漏凭据: config=%q message=%q", repository.createdInput.Config, status.Message)
	}
	if len(repository.singleBindings) != 2 || repository.singleBindings[0].CookieID != "cid-1" || repository.singleBindings[1].CookieID != "cid-2" || repository.singleBindings[0].ChannelID != 41 || !repository.singleBindings[0].Enabled {
		t.Fatalf("账号绑定记录异常: %+v", repository.singleBindings)
	}
	// repeated、repeatedErr 保存终态会话再次查询的结果。
	repeated, repeatedErr := service.GetFeishuAppBindingStatus(context.Background(), 7, start.SessionID)
	if repeatedErr != nil || repeated.Status != FeishuBindingStatusSuccess || repeated.ChannelID != 41 {
		t.Fatalf("终态状态查询异常: status=%+v err=%v", repeated, repeatedErr)
	}
	if registrar.pollCallCount() != 1 {
		t.Fatalf("终态查询不应再次轮询: calls=%d", registrar.pollCallCount())
	}
}

// TestFeishuBindingUpdatesExistingChannelWithoutDuplicate 验证已有 feishu_app 渠道只更新配置，不重复创建。
func TestFeishuBindingUpdatesExistingChannelWithoutDuplicate(t *testing.T) {
	// baseTime 是测试会话的固定创建时间。
	baseTime := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	// currentTime 保存可推进的测试时钟值。
	currentTime := baseTime
	// repository 是预置已有飞书渠道且禁止账号绑定的端口替身。
	repository := &channelRepositoryStub{
		summaries: []ChannelSummary{{ID: 7, Name: "自定义飞书", Type: feishuAppChannelType}},
		record:    &ChannelRecord{ID: 7, Name: "自定义飞书", Type: feishuAppChannelType, Config: `{"old":true}`, EventTypes: "", Enabled: false},
	}
	// registrar 是返回成功结果的协议替身。
	registrar := &feishuRegistrarStub{pollResults: []FeishuPollResult{{Status: FeishuPollSuccess, Domain: "feishu", AppID: "cli-2", AppSecret: "secret-2", OpenID: "ou-2"}}}
	// service 是待验证的飞书渠道应用服务。
	service := fixedFeishuService(repository, registrar, func() time.Time { return currentTime })
	// start、startErr 保存开始绑定结果。
	start, startErr := service.StartFeishuAppBinding(context.Background(), 7, FeishuBindingOptions{BindAllAccounts: false})
	if startErr != nil {
		t.Fatalf("StartFeishuAppBinding: %v", startErr)
	}
	// currentTime 推进到允许轮询的时间。
	currentTime = baseTime.Add(6 * time.Second)
	// status、statusErr 保存更新成功状态。
	status, statusErr := service.GetFeishuAppBindingStatus(context.Background(), 7, start.SessionID)
	if statusErr != nil || status.Status != FeishuBindingStatusSuccess || status.ChannelID != 7 {
		t.Fatalf("更新状态异常: status=%+v err=%v", status, statusErr)
	}
	if repository.createdInput.Type != "" {
		t.Fatalf("不应重复创建渠道: %+v", repository.createdInput)
	}
	if repository.updatedRecord == nil || repository.updatedRecord.Name != "自定义飞书" || repository.updatedRecord.EventTypes != feishuAppDefaultEventTypes || !repository.updatedRecord.Enabled {
		t.Fatalf("渠道更新记录异常: %+v", repository.updatedRecord)
	}
	if len(repository.singleBindings) != 0 {
		t.Fatalf("bind_all_accounts=false 不应绑定账号: %+v", repository.singleBindings)
	}
}

// TestFeishuBindingTerminalPollStates 验证 slow_down、拒绝、过期、未知状态和轮询错误的终态映射。
func TestFeishuBindingTerminalPollStates(t *testing.T) {
	// baseTime 是测试会话的固定创建时间。
	baseTime := time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)
	// cases 保存轮询结果与期望状态的测试样例。
	cases := []struct {
		// name 是测试样例名称。
		name string
		// result 是协议替身返回的轮询结果。
		result FeishuPollResult
		// pollErr 是协议替身返回的轮询错误。
		pollErr error
		// wantStatus 是期望的绑定状态。
		wantStatus string
	}{
		{name: "降低频率", result: FeishuPollResult{Status: FeishuPollSlowDown, Interval: 20 * time.Second}, wantStatus: FeishuBindingStatusWaiting},
		{name: "用户拒绝", result: FeishuPollResult{Status: FeishuPollDenied}, wantStatus: FeishuBindingStatusError},
		{name: "设备码过期", result: FeishuPollResult{Status: FeishuPollExpired}, wantStatus: FeishuBindingStatusExpired},
		{name: "未知结果", result: FeishuPollResult{Status: FeishuPollError, Message: "平台结果无法识别"}, wantStatus: FeishuBindingStatusError},
		{name: "轮询错误", pollErr: errors.New("network broken"), wantStatus: FeishuBindingStatusError},
	}
	// testCase 表示当前终态样例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// currentTime 保存可推进的测试时钟值。
			currentTime := baseTime
			// repository 是记录渠道写入的端口替身。
			repository := &channelRepositoryStub{}
			// registrar 是返回当前样例结果的协议替身。
			registrar := &feishuRegistrarStub{pollResults: []FeishuPollResult{testCase.result}, pollErr: testCase.pollErr}
			// service 是待验证的飞书渠道应用服务。
			service := fixedFeishuService(repository, registrar, func() time.Time { return currentTime })
			// start、startErr 保存开始绑定结果。
			start, startErr := service.StartFeishuAppBinding(context.Background(), 7, FeishuBindingOptions{BindAllAccounts: false})
			if startErr != nil {
				t.Fatalf("StartFeishuAppBinding: %v", startErr)
			}
			// currentTime 推进到允许轮询的时间。
			currentTime = baseTime.Add(6 * time.Second)
			// status、statusErr 保存终态查询结果。
			status, statusErr := service.GetFeishuAppBindingStatus(context.Background(), 7, start.SessionID)
			if statusErr != nil || status.Status != testCase.wantStatus {
				t.Fatalf("状态异常: status=%+v err=%v", status, statusErr)
			}
			if strings.Contains(status.Message, "secret") || strings.Contains(status.Message, "network broken") {
				t.Fatalf("状态消息泄漏内部细节: %q", status.Message)
			}
			if testCase.name == "降低频率" {
				// next 是 slow_down 后登记的下一次轮询时间。
				if next := service.feishuSessions[start.SessionID].NextPollAt; next != baseTime.Add(6*time.Second).Add(20*time.Second) {
					t.Fatalf("slow_down 下一次轮询时间=%s", next)
				}
			}
		})
	}
}

// TestFeishuBindingPersistFailureReturnsErrorStatus 验证渠道写入或账号绑定失败时返回可展示错误状态。
func TestFeishuBindingPersistFailureReturnsErrorStatus(t *testing.T) {
	// baseTime 是测试会话的固定创建时间。
	baseTime := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	// cases 保存渠道保存或账号绑定失败的测试样例。
	cases := []struct {
		// name 是测试样例名称。
		name string
		// repository 是预置失败条件的端口替身。
		repository *channelRepositoryStub
		// bindAll 表示是否请求绑定全部账号。
		bindAll bool
	}{
		{name: "创建渠道失败", repository: &channelRepositoryStub{createErr: errors.New("database down")}, bindAll: false},
		{name: "渠道列表失败", repository: &channelRepositoryStub{listErr: errors.New("list down")}, bindAll: false},
		{name: "渠道详情失败", repository: &channelRepositoryStub{summaries: []ChannelSummary{{ID: 7, Type: feishuAppChannelType}}, recordErr: errors.New("record down")}, bindAll: false},
		{name: "渠道更新失败", repository: &channelRepositoryStub{summaries: []ChannelSummary{{ID: 7, Type: feishuAppChannelType}}, record: &ChannelRecord{ID: 7, Type: feishuAppChannelType, Name: "已有"}, updateErr: errors.New("update down")}, bindAll: false},
		{name: "绑定账号失败", repository: &channelRepositoryStub{ownedAccountIDs: []string{"cid-1"}, singleBindingErr: errors.New("binding down")}, bindAll: true},
		{name: "账号列表失败", repository: &channelRepositoryStub{ownedAccountsErr: errors.New("account list down")}, bindAll: true},
	}
	// testCase 表示当前保存失败样例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// currentTime 保存可推进的测试时钟值。
			currentTime := baseTime
			// registrar 是返回成功结果的协议替身。
			registrar := &feishuRegistrarStub{pollResults: []FeishuPollResult{{Status: FeishuPollSuccess, Domain: "feishu", AppID: "cli-3", AppSecret: "secret-3", OpenID: "ou-3"}}}
			// service 是待验证的飞书渠道应用服务。
			service := fixedFeishuService(testCase.repository, registrar, func() time.Time { return currentTime })
			// start、startErr 保存开始绑定结果。
			start, startErr := service.StartFeishuAppBinding(context.Background(), 7, FeishuBindingOptions{BindAllAccounts: testCase.bindAll})
			if startErr != nil {
				t.Fatalf("StartFeishuAppBinding: %v", startErr)
			}
			// currentTime 推进到允许轮询的时间。
			currentTime = baseTime.Add(6 * time.Second)
			// status、statusErr 保存保存失败后的状态。
			status, statusErr := service.GetFeishuAppBindingStatus(context.Background(), 7, start.SessionID)
			if statusErr != nil || status.Status != FeishuBindingStatusError {
				t.Fatalf("失败状态异常: status=%+v err=%v", status, statusErr)
			}
			if strings.Contains(status.Message, "database down") || strings.Contains(status.Message, "secret-3") {
				t.Fatalf("状态消息泄漏内部细节: %q", status.Message)
			}
		})
	}
}

// TestFeishuBindingSessionOwnershipExpiryAndCleanup 验证会话归属隔离、本地过期清理与开始绑定时清理旧会话。
func TestFeishuBindingSessionOwnershipExpiryAndCleanup(t *testing.T) {
	// baseTime 是测试会话的固定创建时间。
	baseTime := time.Date(2026, 6, 7, 8, 9, 10, 0, time.UTC)
	// currentTime 保存可推进的测试时钟值。
	currentTime := baseTime
	// repository 是记录渠道写入的端口替身。
	repository := &channelRepositoryStub{}
	// registrar 是返回不同会话标识的协议替身。
	registrar := &feishuRegistrarStub{beginResult: FeishuDeviceCode{DeviceCode: "device-1", QRCodeURL: "https://example.com/qr", ExpiresIn: time.Minute, Interval: 5 * time.Second}}
	// service 是使用固定会话标识生成器的飞书渠道应用服务。
	service := NewChannelServiceWithFeishu(repository, nil, registrar)
	service.now = func() time.Time { return currentTime }
	// generated 保存下一次生成的会话标识，测试可切换以建立第二个会话。
	generated := "session-1"
	service.newSessionID = func() (string, error) { return generated, nil }
	// start、startErr 保存第一个会话。
	start, startErr := service.StartFeishuAppBinding(context.Background(), 7, FeishuBindingOptions{BindAllAccounts: false})
	if startErr != nil {
		t.Fatalf("StartFeishuAppBinding: %v", startErr)
	}
	// otherErr 是其他用户查询会话的归属错误，必须与会话不存在使用同一稳定错误。
	if _, otherErr := service.GetFeishuAppBindingStatus(context.Background(), 8, start.SessionID); !errors.Is(otherErr, ErrFeishuSessionNotFound) {
		t.Fatalf("其他用户查询应返回会话不存在: %v", otherErr)
	}
	// currentTime 推进到超过本地会话有效期。
	currentTime = baseTime.Add(2 * time.Minute)
	// expiredErr 是本地过期会话查询错误，必须返回会话不存在。
	if _, expiredErr := service.GetFeishuAppBindingStatus(context.Background(), 7, start.SessionID); !errors.Is(expiredErr, ErrFeishuSessionNotFound) {
		t.Fatalf("过期会话应返回会话不存在: %v", expiredErr)
	}
	// generated 切换到第二个会话标识，验证 begin 时清理已过期会话。
	generated = "session-2"
	// second、secondErr 保存第二个会话。
	second, secondErr := service.StartFeishuAppBinding(context.Background(), 7, FeishuBindingOptions{BindAllAccounts: false})
	if secondErr != nil {
		t.Fatalf("第二次 StartFeishuAppBinding: %v", secondErr)
	}
	// exists 表示已过期会话是否仍留在会话表中；false 表示 begin 清理已生效。
	if _, exists := service.feishuSessions["session-1"]; exists {
		t.Fatal("已过期会话应在 begin 时清理")
	}
	// unknownErr 表示未知会话查询错误。
	if _, unknownErr := service.GetFeishuAppBindingStatus(context.Background(), 7, "missing"); !errors.Is(unknownErr, ErrFeishuSessionNotFound) {
		t.Fatalf("未知会话应返回会话不存在: %v", unknownErr)
	}
	if second.SessionID != "session-2" {
		t.Fatalf("第二会话标识=%q", second.SessionID)
	}
}

// TestFeishuBindingConcurrentStatusPollsOnce 验证同一会话在轮询期间不会被并发请求重复调用平台。
func TestFeishuBindingConcurrentStatusPollsOnce(t *testing.T) {
	// baseTime 是测试会话的固定创建时间。
	baseTime := time.Date(2026, 7, 8, 9, 10, 11, 0, time.UTC)
	// currentTime 保存可推进的测试时钟值。
	currentTime := baseTime
	// pollStarted 在协议替身进入轮询后通知测试。
	pollStarted := make(chan struct{}, 1)
	// pollGate 阻塞第一个轮询，直到测试确认第二个状态请求已返回。
	pollGate := make(chan struct{})
	// repository 是记录渠道写入的端口替身。
	repository := &channelRepositoryStub{}
	// registrar 是带阻塞点的协议替身。
	registrar := &feishuRegistrarStub{pollStarted: pollStarted, pollGate: pollGate, pollResults: []FeishuPollResult{{Status: FeishuPollPending, Domain: "feishu"}}}
	// service 是待验证的飞书渠道应用服务。
	service := fixedFeishuService(repository, registrar, func() time.Time { return currentTime })
	// start、startErr 保存开始绑定结果。
	start, startErr := service.StartFeishuAppBinding(context.Background(), 7, FeishuBindingOptions{BindAllAccounts: false})
	if startErr != nil {
		t.Fatalf("StartFeishuAppBinding: %v", startErr)
	}
	// currentTime 推进到允许轮询的时间。
	currentTime = baseTime.Add(6 * time.Second)
	// firstDone 在第一个状态请求结束后关闭，承载其结果。
	firstDone := make(chan struct{})
	// firstStatus 保存第一个状态请求的结果。
	var firstStatus FeishuBindingStatus
	// firstErr 保存第一个状态请求的错误。
	var firstErr error
	go func() {
		defer close(firstDone)
		firstStatus, firstErr = service.GetFeishuAppBindingStatus(context.Background(), 7, start.SessionID)
	}()
	<-pollStarted
	// concurrentStatus、concurrentErr 保存轮询期间的第二个状态请求结果。
	concurrentStatus, concurrentErr := service.GetFeishuAppBindingStatus(context.Background(), 7, start.SessionID)
	if concurrentErr != nil || concurrentStatus.Status != FeishuBindingStatusWaiting {
		t.Fatalf("并发状态异常: status=%+v err=%v", concurrentStatus, concurrentErr)
	}
	if registrar.pollCallCount() != 1 {
		t.Fatalf("并发请求重复轮询: calls=%d", registrar.pollCallCount())
	}
	close(pollGate)
	<-firstDone
	if firstErr != nil || firstStatus.Status != FeishuBindingStatusWaiting {
		t.Fatalf("首个状态异常: status=%+v err=%v", firstStatus, firstErr)
	}
}

// TestFeishuBindingRejectsUnavailableAndInvalidInput 验证未装配协议端口、nil 服务和非法输入的错误映射。
func TestFeishuBindingRejectsUnavailableAndInvalidInput(t *testing.T) {
	// repository 是空渠道端口替身。
	repository := &channelRepositoryStub{}
	// plainService 是未装配飞书协议端口的普通渠道服务。
	plainService := NewChannelService(repository, nil)
	// startErr 是未装配端口服务的开始绑定错误，必须映射为飞书绑定不可用。
	if _, startErr := plainService.StartFeishuAppBinding(context.Background(), 7, FeishuBindingOptions{BindAllAccounts: false}); !errors.Is(startErr, ErrFeishuUnavailable) {
		t.Fatalf("未装配协议端口应返回不可用: %v", startErr)
	}
	// statusErr 是未装配端口服务的状态查询错误，必须映射为飞书绑定不可用。
	if _, statusErr := plainService.GetFeishuAppBindingStatus(context.Background(), 7, "session"); !errors.Is(statusErr, ErrFeishuUnavailable) {
		t.Fatalf("未装配协议端口应返回不可用: %v", statusErr)
	}
	// nilService 是零值服务，必须拒绝所有调用。
	var nilService *ChannelService
	// startErr 是 nil 服务的开始绑定错误，必须映射为输入无效。
	if _, startErr := nilService.StartFeishuAppBinding(context.Background(), 7, FeishuBindingOptions{BindAllAccounts: false}); !errors.Is(startErr, ErrChannelInvalidInput) {
		t.Fatalf("nil 服务应返回输入错误: %v", startErr)
	}
	// service 是已装配端口的服务，用于验证用户与会话标识校验。
	service := NewChannelServiceWithFeishu(repository, nil, &feishuRegistrarStub{})
	// startErr 是非法用户标识的开始绑定错误，必须在调用协议端口前失败。
	if _, startErr := service.StartFeishuAppBinding(context.Background(), 0, FeishuBindingOptions{BindAllAccounts: false}); !errors.Is(startErr, ErrChannelInvalidInput) {
		t.Fatalf("非法用户应返回输入错误: %v", startErr)
	}
	// statusErr 是空会话标识的状态查询错误，必须映射为输入无效。
	if _, statusErr := service.GetFeishuAppBindingStatus(context.Background(), 7, " "); !errors.Is(statusErr, ErrChannelInvalidInput) {
		t.Fatalf("空会话标识应返回输入错误: %v", statusErr)
	}
	// beginFailure 是注册端口预置错误。
	beginFailure := errors.New("platform unavailable")
	// failingService 是 begin 阶段失败的渠道服务。
	failingService := NewChannelServiceWithFeishu(repository, nil, &feishuRegistrarStub{beginErr: beginFailure})
	// startErr 是注册端口 begin 失败错误，必须原样透传。
	if _, startErr := failingService.StartFeishuAppBinding(context.Background(), 7, FeishuBindingOptions{BindAllAccounts: false}); !errors.Is(startErr, beginFailure) {
		t.Fatalf("begin 错误应透传: %v", startErr)
	}
}

// TestFeishuBindingStartDefaultsAndGenerationFailure 验证 begin 缺省时间兜底与会话标识生成失败。
func TestFeishuBindingStartDefaultsAndGenerationFailure(t *testing.T) {
	// repository 是空渠道端口替身。
	repository := &channelRepositoryStub{}
	// registrar 返回缺少有效期和间隔的合法扫码结果。
	registrar := &feishuRegistrarStub{beginResult: FeishuDeviceCode{DeviceCode: "device-default", QRCodeURL: "https://example.com/qr"}}
	// service 是使用默认时间与固定生成器的渠道服务。
	service := NewChannelServiceWithFeishu(repository, nil, registrar)
	// start、startErr 保存使用默认时间参数的结果。
	start, startErr := service.StartFeishuAppBinding(context.Background(), 7, FeishuBindingOptions{BindAllAccounts: false})
	if startErr != nil {
		t.Fatalf("StartFeishuAppBinding: %v", startErr)
	}
	if start.ExpiresIn != int(feishuAppDefaultExpiresIn.Seconds()) {
		t.Fatalf("默认有效期=%d", start.ExpiresIn)
	}
	// session 是登记后的内部会话，用于校验默认轮询间隔。
	session := service.feishuSessions[start.SessionID]
	if session == nil || session.Interval != feishuAppDefaultPollInterval {
		t.Fatalf("默认轮询间隔异常: %+v", session)
	}
	// generationFailure 是会话标识生成器预置错误。
	generationFailure := errors.New("rand unavailable")
	// failingGeneratorService 使用失败生成器，必须拒绝创建无法索引的会话。
	failingGeneratorService := NewChannelServiceWithFeishu(repository, nil, registrar)
	failingGeneratorService.newSessionID = func() (string, error) { return "", generationFailure }
	// failingErr 是会话标识生成失败时的开始绑定错误，必须阻止登记无标识会话。
	if _, failingErr := failingGeneratorService.StartFeishuAppBinding(context.Background(), 7, FeishuBindingOptions{BindAllAccounts: false}); failingErr == nil {
		t.Fatal("会话标识生成失败应返回错误")
	}
}

// TestFeishuBindingHelpers 验证轮询间隔、域名合并与会话状态复制的边界行为。
func TestFeishuBindingHelpers(t *testing.T) {
	// intervalCases 保存轮询间隔归一化样例及期望值。
	intervalCases := []struct {
		// input 是原始间隔。
		input time.Duration
		// want 是归一后的间隔。
		want time.Duration
	}{
		{input: 0, want: feishuAppDefaultPollInterval},
		{input: time.Second, want: feishuAppDefaultPollInterval},
		{input: 20 * time.Second, want: 20 * time.Second},
		{input: 2 * time.Minute, want: feishuAppMaxPollInterval},
	}
	// intervalCase 表示当前轮询间隔样例。
	for _, intervalCase := range intervalCases {
		// got 是当前间隔样例归一化后的实际间隔。
		if got := normalizePollInterval(intervalCase.input); got != intervalCase.want {
			t.Errorf("normalizePollInterval(%s)=%s want=%s", intervalCase.input, got, intervalCase.want)
		}
	}
	// domainCases 保存域名合并样例及期望值。
	domainCases := []struct {
		// current 是当前域名。
		current string
		// next 是轮询结果域名。
		next string
		// want 是合并后的域名。
		domainWant string
	}{
		{current: "feishu", next: "feishu", domainWant: "feishu"},
		{current: "feishu", next: "lark", domainWant: "lark"},
		{current: "lark", next: "feishu", domainWant: "lark"},
		{current: "", next: "", domainWant: "feishu"},
	}
	// domainCase 表示当前域名合并样例。
	for _, domainCase := range domainCases {
		// got 是当前域名样例合并后的实际域名。
		if got := mergePollDomain(domainCase.current, domainCase.next); got != domainCase.domainWant {
			t.Errorf("mergePollDomain(%q,%q)=%q want=%q", domainCase.current, domainCase.next, got, domainCase.domainWant)
		}
	}
	// emptyStatus 保存 nil 会话状态复制结果。
	emptyStatus := feishuBindingStatusFromSession(nil)
	if emptyStatus.Status != FeishuBindingStatusError || emptyStatus.Message == "" {
		t.Fatalf("nil 会话状态异常: %+v", emptyStatus)
	}
	// session 是用于状态复制的会话实例。
	session := &feishuBindingSession{Status: FeishuBindingStatusSuccess, Message: "成功", ChannelID: 9}
	// copied 是复制后的状态快照。
	copied := feishuBindingStatusFromSession(session)
	if copied.Status != FeishuBindingStatusSuccess || copied.ChannelID != 9 || copied.Message != "成功" {
		t.Fatalf("状态复制异常: %+v", copied)
	}
	// normalizeFeishuDomain 验证未知域名按飞书中国站处理。
	if normalizeFeishuDomain("LARK") != "lark" || normalizeFeishuDomain("unknown") != "feishu" {
		t.Fatal("域名归一化异常")
	}
}
