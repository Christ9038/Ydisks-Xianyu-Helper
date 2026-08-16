package account

import (
	"context"
	"errors"
	"testing"
)

// fakeSummaryRepository 是资料刷新应用服务测试用的摘要 repository 替身。
type fakeSummaryRepository struct {
	// summary 是待返回的非敏感账号摘要。
	summary Summary
	// err 是摘要查询需要模拟的错误。
	err error
}

// GetOwnedSummary 返回测试预置的摘要或查询错误。
func (r fakeSummaryRepository) GetOwnedSummary(context.Context, int64, string) (Summary, error) {
	return r.summary, r.err
}

// fakeProfilePort 是资料刷新应用服务测试用的平台端口替身。
type fakeProfilePort struct {
	// result 是待返回的资料刷新结果。
	result ProfileResult
	// err 是平台资料刷新需要模拟的错误。
	err error
	// input 保存最近一次收到的端口输入，供测试验证归属摘要传递。
	input ProfileInput
}

// RefreshProfile 返回测试预置结果并记录输入。
func (p *fakeProfilePort) RefreshProfile(_ context.Context, input ProfileInput) (ProfileResult, error) {
	p.input = input
	return p.result, p.err
}

// TestNewProfileServiceRequiresPorts 验证构造函数拒绝缺失依赖。
func TestNewProfileServiceRequiresPorts(t *testing.T) {
	// repository 是满足摘要查询能力的测试端口。
	repository := fakeSummaryRepository{}
	// profilePort 是满足平台资料刷新能力的测试端口。
	profilePort := &fakeProfilePort{}
	// cases 是构造阶段需要拒绝的缺失依赖场景。
	cases := []struct {
		// name 是当前构造失败场景名称。
		name string
		// repository 是当前场景使用的摘要端口。
		repository ProfileSummaryRepository
		// profilePort 是当前场景使用的平台端口。
		profilePort ProfilePort
	}{
		{name: "缺少摘要repository", repository: nil, profilePort: profilePort},
		{name: "缺少资料端口", repository: repository, profilePort: nil},
	}
	// testCase 表示当前正在验证的构造失败场景。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// err 保存当前场景的构造结果错误。
			_, err := NewProfileService(testCase.repository, testCase.profilePort)
			if err == nil {
				t.Fatal("缺少必需端口时应构造失败")
			}
		})
	}
}

// TestRefreshProfileVerifiesOwnershipAndPassesNonSensitiveSummary 验证成功路径只向端口传递已归属摘要。
func TestRefreshProfileVerifiesOwnershipAndPassesNonSensitiveSummary(t *testing.T) {
	// repository 是返回当前用户账号摘要的测试端口。
	repository := fakeSummaryRepository{summary: Summary{ID: "acc1", UserID: 7, Nickname: "旧昵称"}}
	// profilePort 是返回平台资料结果的测试端口。
	profilePort := &fakeProfilePort{result: ProfileResult{Nickname: "新昵称", AvatarURL: "https://img.example/avatar.jpg"}}
	// service 是待验证的账号资料应用服务。
	service, err := NewProfileService(repository, profilePort)
	if err != nil {
		t.Fatalf("构造资料服务失败: %v", err)
	}
	// result 保存资料刷新成功结果。
	result, err := service.RefreshProfile(context.Background(), 7, "acc1")
	if err != nil {
		t.Fatalf("资料刷新失败: %v", err)
	}
	if result.AccountID != "acc1" || result.Nickname != "新昵称" {
		t.Fatalf("资料刷新结果异常: %+v", result)
	}
	if profilePort.input.UserID != 7 || profilePort.input.Summary.ID != "acc1" {
		t.Fatalf("资料端口未收到归属摘要: %+v", profilePort.input)
	}
}

// TestRefreshProfilePropagatesOwnershipAndPlatformFailures 验证归属查询和平台端口错误不会被吞掉。
func TestRefreshProfilePropagatesOwnershipAndPlatformFailures(t *testing.T) {
	// ownershipErr 是账号不属于当前用户时 repository 返回的错误。
	ownershipErr := errors.New("账号不属于当前用户")
	// ownershipService 是使用归属错误替身构造的资料服务。
	ownershipService, err := NewProfileService(fakeSummaryRepository{err: ownershipErr}, &fakeProfilePort{})
	if err != nil {
		t.Fatalf("构造归属错误服务失败: %v", err)
	}
	// ownershipResult、ownershipCallErr 保存归属失败结果。
	_, ownershipCallErr := ownershipService.RefreshProfile(context.Background(), 7, "other")
	if !errors.Is(ownershipCallErr, ownershipErr) {
		t.Fatalf("应保留归属错误，got %v", ownershipCallErr)
	}

	// platformErr 是平台资料端口返回的故障。
	platformErr := errors.New("平台不可用")
	// platformService 是使用平台错误替身构造的资料服务。
	platformService, err := NewProfileService(fakeSummaryRepository{summary: Summary{ID: "acc1"}}, &fakeProfilePort{err: platformErr})
	if err != nil {
		t.Fatalf("构造平台错误服务失败: %v", err)
	}
	// platformResult、platformCallErr 保存平台失败结果。
	_, platformCallErr := platformService.RefreshProfile(context.Background(), 7, "acc1")
	if !errors.Is(platformCallErr, platformErr) {
		t.Fatalf("应保留平台错误，got %v", platformCallErr)
	}
}
