package account

import (
	"context"
	"errors"
	"testing"
)

// platformCredentialPortFake 保存测试用平台凭证窄视图，不模拟数据库或加密实现。
type platformCredentialPortFake struct {
	// detail 是端口返回的最小平台凭证视图。
	detail *CredentialDetail
	// err 是端口返回的读取错误。
	err error
}

// TestNewPlatformCredentialServiceRejectsMissingPort 验证凭证读取服务不会接受半初始化端口。
func TestNewPlatformCredentialServiceRejectsMissingPort(t *testing.T) {
	// service、serviceErr 保存缺失端口时的构造结果。
	service, serviceErr := NewPlatformCredentialService(nil)
	if service != nil || serviceErr == nil {
		t.Fatalf("缺失凭证端口不应构造成功: service=%v err=%v", service, serviceErr)
	}
}

// LoadPlatformDetail 返回固定平台凭证视图，验证应用服务不要求数据库具体类型。
func (f platformCredentialPortFake) LoadPlatformDetail(context.Context, string) (*CredentialDetail, error) {
	return f.detail, f.err
}

// TestPlatformCredentialServiceValidatesOwnedWithoutReturningCookie 验证归属校验只返回用户标识。
func TestPlatformCredentialServiceValidatesOwnedWithoutReturningCookie(t *testing.T) {
	// service 是使用最小凭证端口构造的平台凭证应用服务。
	service, serviceErr := NewPlatformCredentialService(platformCredentialPortFake{detail: &CredentialDetail{ID: "acc1", UserID: 7, Value: "sid=masked"}})
	if serviceErr != nil {
		t.Fatalf("构造凭证服务失败: %v", serviceErr)
	}
	// ownerID、validationErr 保存归属校验的非敏感结果及错误。
	ownerID, validationErr := service.ValidateOwned(context.Background(), 7, "acc1")
	if validationErr != nil || ownerID != 7 {
		t.Fatalf("归属校验结果异常: owner=%d err=%v", ownerID, validationErr)
	}
}

// TestPlatformCredentialServiceRejectsForeignAndEmptyCredentials 验证越权和空 Cookie 均 fail closed。
func TestPlatformCredentialServiceRejectsForeignAndEmptyCredentials(t *testing.T) {
	// cases 描述不同平台凭证状态及期望的稳定应用错误。
	cases := []struct {
		name   string
		detail *CredentialDetail
		want   error
	}{
		{name: "foreign", detail: &CredentialDetail{ID: "acc1", UserID: 8, Value: "sid=masked"}, want: ErrForbidden},
		{name: "empty", detail: &CredentialDetail{ID: "acc1", UserID: 7}, want: ErrCredentialEmpty},
	}
	// testCase 表示当前遍历的平台凭证状态用例。
	// testCase 表示当前遍历的凭证状态与预期错误。
	for _, testCase := range cases {
		// testCase 是当前凭证状态用例，防止闭包捕获后续迭代变量。
		t.Run(testCase.name, func(t *testing.T) {
			// service 是当前状态用例使用的平台凭证应用服务。
			service, serviceErr := NewPlatformCredentialService(platformCredentialPortFake{detail: testCase.detail})
			if serviceErr != nil {
				t.Fatalf("构造凭证服务失败: %v", serviceErr)
			}
			// _, validationErr 保存归属校验错误；明文结果不会被测试输出。
			_, validationErr := service.ValidateOwned(context.Background(), 7, "acc1")
			if !errors.Is(validationErr, testCase.want) {
				t.Fatalf("错误类型=%v，期望=%v", validationErr, testCase.want)
			}
		})
	}
}

// TestPlatformCredentialServicePropagatesPortErrors 验证基础设施错误不会被伪装成成功。
func TestPlatformCredentialServicePropagatesPortErrors(t *testing.T) {
	// portErr 是模拟凭证存储不可用的脱敏错误。
	portErr := errors.New("credential store unavailable")
	// service 是绑定故障端口的平台凭证应用服务。
	service, serviceErr := NewPlatformCredentialService(platformCredentialPortFake{err: portErr})
	if serviceErr != nil {
		t.Fatalf("构造凭证服务失败: %v", serviceErr)
	}
	// _, loadErr 保存平台凭证读取错误。
	_, loadErr := service.LoadPlatformDetail(context.Background(), "acc1")
	if !errors.Is(loadErr, portErr) {
		t.Fatalf("错误未透传: %v", loadErr)
	}
}
