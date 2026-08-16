package automation

import (
	"context"
	"errors"
	"testing"
)

// credentialWakeRepositoryFake 是凭证恢复唤醒应用服务测试用的可控端口替身。
type credentialWakeRepositoryFake struct {
	// accountID 保存最近一次收到的账号标识。
	accountID string
	// wakeErr 保存模拟的持久化唤醒错误。
	wakeErr error
}

// WakeCredentialBlocked 记录唤醒请求并返回预置错误。
func (f *credentialWakeRepositoryFake) WakeCredentialBlocked(_ context.Context, accountID string) error {
	f.accountID = accountID
	return f.wakeErr
}

// TestCredentialWakeServiceValidatesAndForwards 验证凭证恢复唤醒的输入、转发和错误传播。
func TestCredentialWakeServiceValidatesAndForwards(t *testing.T) {
	// repository 是接收应用层唤醒请求的测试端口。
	repository := &credentialWakeRepositoryFake{}
	// service 是绑定测试端口的凭证恢复唤醒服务。
	// err 保存应用服务构造错误。
	service, err := NewCredentialWakeService(repository)
	if err != nil {
		t.Fatalf("构造唤醒服务失败: %v", err)
	}
	// err 表示归一化账号标识后的唤醒调用错误。
	if err := service.WakeCredentialBlocked(context.Background(), "  acc1 "); err != nil {
		t.Fatalf("唤醒账号失败: %v", err)
	}
	if repository.accountID != "acc1" {
		t.Fatalf("账号标识未归一化: %q", repository.accountID)
	}
	// err 表示空账号标识被应用层拒绝的错误结果。
	if err := service.WakeCredentialBlocked(context.Background(), " "); err == nil {
		t.Fatal("空账号标识不应调用唤醒端口")
	}
	// wakeErr 是底层任务状态写入故障。
	wakeErr := errors.New("唤醒写入失败")
	repository.wakeErr = wakeErr
	// err 表示底层任务状态写入故障向应用层传播的结果。
	if err := service.WakeCredentialBlocked(context.Background(), "acc1"); !errors.Is(err, wakeErr) {
		t.Fatalf("唤醒错误未传播: %v", err)
	}
}

// TestNewCredentialWakeServiceRequiresRepository 验证凭证恢复唤醒服务拒绝缺失端口。
func TestNewCredentialWakeServiceRequiresRepository(t *testing.T) {
	// _, err 保存缺少持久化端口时的构造错误。
	if _, err := NewCredentialWakeService(nil); err == nil {
		t.Fatal("缺少唤醒 repository 时不应构造成功")
	}
}
