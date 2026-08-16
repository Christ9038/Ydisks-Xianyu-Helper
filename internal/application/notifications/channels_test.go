package notifications

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// channelRepositoryStub 是通知渠道应用服务测试使用的可控端口替身。
type channelRepositoryStub struct {
	// summaries 保存列表查询返回的非敏感渠道摘要。
	summaries []ChannelSummary
	// record 保存更新查询返回的渠道记录。
	record *ChannelRecord
	// bindings 保存绑定列表查询结果。
	bindings []BindingSummary
	// bindingIDs 保存账号启用的渠道 ID。
	bindingIDs []int64
	// ownedChannel 表示渠道归属校验结果。
	ownedChannel bool
	// ownedAccount 表示账号归属校验结果。
	ownedAccount bool
	// err 保存端口操作失败原因。
	err error
	// createdInput 保存最近一次创建输入，便于验证敏感值只进入端口。
	createdInput ChannelInput
	// updatedRecord 保存最近一次更新记录，便于验证部分更新合并。
	updatedRecord *ChannelRecord
	// sentBindings 保存最近一次覆盖绑定请求。
	sentBindings []int64
}

// ListChannels 返回预置渠道摘要。
func (r *channelRepositoryStub) ListChannels(context.Context, int64) ([]ChannelSummary, error) {
	return r.summaries, r.err
}

// GetChannelForUpdate 返回预置渠道完整记录。
func (r *channelRepositoryStub) GetChannelForUpdate(context.Context, int64, int64) (*ChannelRecord, error) {
	return r.record, r.err
}

// CreateChannel 记录创建输入并返回固定渠道 ID。
func (r *channelRepositoryStub) CreateChannel(_ context.Context, _ int64, input ChannelInput) (int64, error) {
	r.createdInput = input
	if r.err != nil {
		return 0, r.err
	}
	return 41, nil
}

// UpdateChannel 记录更新后的完整渠道记录。
func (r *channelRepositoryStub) UpdateChannel(_ context.Context, _ int64, record ChannelRecord) error {
	r.updatedRecord = &record
	return r.err
}

// DeleteChannel 返回预置端口错误。
func (r *channelRepositoryStub) DeleteChannel(context.Context, int64, int64) error { return r.err }

// OwnsChannel 返回预置渠道归属结果。
func (r *channelRepositoryStub) OwnsChannel(context.Context, int64, int64) (bool, error) {
	return r.ownedChannel, r.err
}

// OwnsAccount 返回预置账号归属结果。
func (r *channelRepositoryStub) OwnsAccount(context.Context, int64, string) (bool, error) {
	return r.ownedAccount, r.err
}

// ListBindings 返回预置绑定摘要。
func (r *channelRepositoryStub) ListBindings(context.Context, int64) ([]BindingSummary, error) {
	return r.bindings, r.err
}

// GetBindingIDs 返回预置账号绑定 ID。
func (r *channelRepositoryStub) GetBindingIDs(context.Context, string) ([]int64, error) {
	return r.bindingIDs, r.err
}

// SetBindings 记录覆盖绑定请求。
func (r *channelRepositoryStub) SetBindings(_ context.Context, _ string, channelIDs []int64) error {
	r.sentBindings = append([]int64(nil), channelIDs...)
	return r.err
}

// SetSingleBinding 返回预置端口错误。
func (r *channelRepositoryStub) SetSingleBinding(context.Context, string, int64, bool) error {
	return r.err
}

// DeleteBinding 返回预置端口错误。
func (r *channelRepositoryStub) DeleteBinding(context.Context, int64, int64) error { return r.err }

// DeleteAccountBindings 返回预置端口错误。
func (r *channelRepositoryStub) DeleteAccountBindings(context.Context, int64, string) error {
	return r.err
}

// channelSenderStub 是测试发送端口的替身。
type channelSenderStub struct {
	// body 保存最近一次发送的正文。
	body string
	// err 保存发送失败原因。
	err error
}

// SendToChannel 记录发送正文并返回预置错误。
func (s *channelSenderStub) SendToChannel(_ int64, body string) error {
	s.body = body
	return s.err
}

// TestChannelServiceCreatesAndUpdatesWithoutReturningConfig 验证渠道创建更新成功且配置不进入展示模型。
func TestChannelServiceCreatesAndUpdatesWithoutReturningConfig(t *testing.T) {
	// repository 是保存敏感配置但不把它放入摘要的端口替身。
	repository := &channelRepositoryStub{summaries: []ChannelSummary{{ID: 1, Name: "邮件", Type: "email"}}, record: &ChannelRecord{ID: 1, Name: "旧名", Type: "email", Config: `{"password":"secret"}`, Enabled: true}}
	// service 是待验证的通知渠道应用服务。
	service := NewChannelService(repository, nil)
	// channelID、createErr 保存创建结果。
	channelID, createErr := service.CreateChannel(context.Background(), 7, ChannelInput{Name: "邮件", Type: "email", Config: `{"password":"secret"}`})
	if createErr != nil || channelID != 41 || repository.createdInput.Config == "" {
		t.Fatalf("创建结果异常: id=%d err=%v", channelID, createErr)
	}
	// name 保存部分更新后的渠道名称。
	name := "新名"
	// updateErr 保存更新结果。
	updateErr := service.UpdateChannel(context.Background(), 7, 1, ChannelPatch{Name: &name})
	if updateErr != nil || repository.updatedRecord == nil || repository.updatedRecord.Name != "新名" || repository.updatedRecord.Config != `{"password":"secret"}` {
		t.Fatalf("更新结果异常: record=%+v err=%v", repository.updatedRecord, updateErr)
	}
	// summaries、listErr 保存非敏感渠道列表结果。
	summaries, listErr := service.ListChannels(context.Background(), 7)
	if listErr != nil || len(summaries) != 1 || summaries[0].Name != "邮件" {
		t.Fatalf("列表结果异常: summaries=%+v err=%v", summaries, listErr)
	}
}

// TestChannelServiceRejectsOwnershipAndStorageFailures 验证归属失败和存储错误不会继续写入。
func TestChannelServiceRejectsOwnershipAndStorageFailures(t *testing.T) {
	// forbiddenRepository 是拒绝渠道归属的端口替身。
	forbiddenRepository := &channelRepositoryStub{ownedChannel: false, ownedAccount: false}
	// forbiddenService 是使用拒绝归属端口的应用服务。
	forbiddenService := NewChannelService(forbiddenRepository, &channelSenderStub{})
	// forbiddenErr 保存测试发送的归属错误。
	forbiddenErr := forbiddenService.TestChannel(context.Background(), 7, 1, time.Unix(0, 0))
	if !errors.Is(forbiddenErr, ErrChannelForbidden) {
		t.Fatalf("渠道归属失败错误异常: %v", forbiddenErr)
	}
	// bindingErr 保存账号绑定的归属错误。
	bindingErr := forbiddenService.SetBindings(context.Background(), 7, "acc", []int64{1})
	if !errors.Is(bindingErr, ErrAccountForbidden) {
		t.Fatalf("账号归属失败错误异常: %v", bindingErr)
	}
	// storageError 是持久化端口失败原因。
	storageError := errors.New("storage failed")
	// storageRepository 是返回存储错误的端口替身。
	storageRepository := &channelRepositoryStub{ownedChannel: true, ownedAccount: true, err: storageError, record: &ChannelRecord{Name: "x", Type: "webhook"}}
	// storageService 是使用存储错误端口的应用服务。
	storageService := NewChannelService(storageRepository, &channelSenderStub{})
	// updateErr 保存存储错误传播结果。
	updateErr := storageService.UpdateChannel(context.Background(), 7, 1, ChannelPatch{})
	if !errors.Is(updateErr, storageError) {
		t.Fatalf("存储错误未透传: %v", updateErr)
	}
}

// TestChannelServiceOwnsChannelKeepsRepositoryBoundary 验证渠道归属查询只透传非敏感结论和存储错误。
func TestChannelServiceOwnsChannelKeepsRepositoryBoundary(t *testing.T) {
	// cases 描述成功、越权、存储失败和非法输入四类归属结果。
	cases := []struct {
		name       string
		repository *channelRepositoryStub
		userID     int64
		channelID  int64
		want       bool
		wantErr    error
	}{
		{name: "owned", repository: &channelRepositoryStub{ownedChannel: true}, userID: 7, channelID: 1, want: true},
		{name: "not owned", repository: &channelRepositoryStub{ownedChannel: false}, userID: 7, channelID: 1},
		{name: "storage error", repository: &channelRepositoryStub{err: errors.New("storage failed")}, userID: 7, channelID: 1, wantErr: errors.New("storage failed")},
		{name: "invalid user", repository: &channelRepositoryStub{ownedChannel: true}, userID: 0, channelID: 1, wantErr: ErrChannelInvalidInput},
	}
	// testCase 表示当前验证的渠道归属边界场景。
	for _, testCase := range cases {
		// service 是使用当前场景持久化替身的通知渠道服务。
		service := NewChannelService(testCase.repository, nil)
		// exists、ownershipErr 保存应用层归属结果。
		exists, ownershipErr := service.OwnsChannel(context.Background(), testCase.userID, testCase.channelID)
		if exists != testCase.want {
			t.Fatalf("%s: 归属结果=%v，期望=%v", testCase.name, exists, testCase.want)
		}
		if testCase.wantErr != nil {
			if ownershipErr == nil || ownershipErr.Error() != testCase.wantErr.Error() {
				t.Fatalf("%s: 错误=%v，期望=%v", testCase.name, ownershipErr, testCase.wantErr)
			}
			continue
		}
		if ownershipErr != nil {
			t.Fatalf("%s: 意外错误=%v", testCase.name, ownershipErr)
		}
	}
}

// TestChannelServiceTestSendUsesSafeBody 验证测试发送只生成固定正文，不携带渠道配置。
func TestChannelServiceTestSendUsesSafeBody(t *testing.T) {
	// repository 是允许渠道归属且不返回敏感配置的端口替身。
	repository := &channelRepositoryStub{ownedChannel: true}
	// sender 是记录测试正文的通知发送替身。
	sender := &channelSenderStub{}
	// service 是待验证的测试发送应用服务。
	service := NewChannelService(repository, sender)
	// sendErr 保存测试发送结果。
	sendErr := service.TestChannel(context.Background(), 7, 1, time.Unix(0, 0).UTC())
	if sendErr != nil || !strings.Contains(sender.body, "通知渠道测试") || strings.Contains(sender.body, "password") {
		t.Fatalf("测试正文异常: body=%q err=%v", sender.body, sendErr)
	}
}
