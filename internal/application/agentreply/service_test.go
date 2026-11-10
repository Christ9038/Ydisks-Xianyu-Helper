package agentreply

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeRepository 是内存 Repository；owner 为账号所有者，saved 记录最近一次保存的草稿。
type fakeRepository struct {
	owner    int64
	ownerErr error
	settings *Settings
	// getErr 非空时 Get 返回该错误，用于模拟配置读取失败。
	getErr   error
	saved    *Draft
	token    string
	tokenErr error
	saveErr  error
}

// CheckOwnership 返回预设所有者或错误。
func (f *fakeRepository) CheckOwnership(context.Context, int64, string) (AccountOwnership, error) {
	return AccountOwnership{OwnerID: f.owner}, f.ownerErr
}

// Get 返回预设配置，未设置时返回 ErrConfigNotFound。
func (f *fakeRepository) Get(context.Context, string) (Settings, error) {
	if f.getErr != nil {
		return Settings{}, f.getErr
	}
	if f.settings == nil {
		return Settings{}, ErrConfigNotFound
	}
	return *f.settings, nil
}

// Save 记录草稿并让后续 Get 反映保存结果。
func (f *fakeRepository) Save(_ context.Context, cookieID string, draft Draft) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = &draft
	f.settings = &Settings{CookieID: cookieID, Enabled: draft.Enabled, EndpointURL: draft.EndpointURL,
		RequestTimeoutSeconds: draft.RequestTimeoutSeconds, MaxSessionSeconds: draft.MaxSessionSeconds}
	return nil
}

// StoredToken 返回预设令牌。
func (f *fakeRepository) StoredToken(context.Context, string) (string, error) {
	return f.token, f.tokenErr
}

// fakeTester 记录收到的出站目标并返回预设结果。
type fakeTester struct {
	got    Endpoint
	result TestResult
	err    error
}

// Ping 记录 endpoint。
func (f *fakeTester) Ping(_ context.Context, endpoint Endpoint) (TestResult, error) {
	f.got = endpoint
	return f.result, f.err
}

// TestGetDefaultsAndOwnership 验证未配置返回默认关闭值，以及用户身份、账号标识、归属错误。
func TestGetDefaultsAndOwnership(t *testing.T) {
	// repo 是账号所有者为 7 的内存仓储。
	repo := &fakeRepository{owner: 7}
	// service 是仅装配仓储、未装配测试器的被测服务。
	service := NewService(repo, nil)
	// settings、err 保存未配置时读取到的默认配置及错误。
	settings, err := service.Get(context.Background(), 7, "acc")
	if err != nil || settings.Enabled || settings.RequestTimeoutSeconds != DefaultRequestTimeoutSeconds || settings.MaxSessionSeconds != DefaultMaxSessionSeconds || settings.CookieID != "acc" {
		t.Fatalf("默认配置异常: %+v err=%v", settings, err)
	}
	// err 保存无效用户身份的拒绝错误。
	if _, err := service.Get(context.Background(), 0, "acc"); !errors.Is(err, ErrInvalidUser) {
		t.Fatalf("无效用户: %v", err)
	}
	// err 保存空账号标识的拒绝错误。
	if _, err := service.Get(context.Background(), 7, ""); !errors.Is(err, ErrInvalidCookieID) {
		t.Fatalf("无效账号: %v", err)
	}
	// err 保存非所有者访问的越权错误。
	if _, err := service.Get(context.Background(), 8, "acc"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("越权: %v", err)
	}
	repo.ownerErr = ErrAccountNotFound
	// err 保存账号不存在时仓储返回的错误。
	if _, err := service.Get(context.Background(), 7, "acc"); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("账号不存在: %v", err)
	}
	// err 保存未装配服务的防御性错误。
	if _, err := (*Service)(nil).Get(context.Background(), 7, "acc"); err == nil {
		t.Fatal("未装配服务应报错")
	}
}

// TestUpdateNormalizesAndValidates 验证默认值补齐、令牌命令与各类校验失败。
func TestUpdateNormalizesAndValidates(t *testing.T) {
	// repo 是账号所有者为 1 的内存仓储，用于记录保存的草稿。
	repo := &fakeRepository{owner: 1}
	// service 是仅装配仓储的被测服务。
	service := NewService(repo, nil)
	// settings、err 保存保存后读回的配置摘要及错误。
	settings, err := service.Update(context.Background(), 1, "acc", Draft{Enabled: true, EndpointURL: " https://agent.example.com/v1 ", TokenAction: TokenReplace, Token: " tok "})
	if err != nil || !settings.Enabled || settings.EndpointURL != "https://agent.example.com/v1" ||
		repo.saved.Token != "tok" || repo.saved.RequestTimeoutSeconds != 15 || repo.saved.MaxSessionSeconds != 120 {
		t.Fatalf("保存结果异常: %+v saved=%+v err=%v", settings, repo.saved, err)
	}
	// 空命令按 retain 处理。
	if _, err := service.Update(context.Background(), 1, "acc", Draft{EndpointURL: "http://127.0.0.1:9000"}); err != nil || repo.saved.TokenAction != TokenRetain {
		t.Fatalf("空命令应为 retain: %+v err=%v", repo.saved, err)
	}
	// invalid 覆盖各个校验分支。
	invalid := map[string]Draft{
		"未知命令":    {TokenAction: "x"},
		"超时过小":    {RequestTimeoutSeconds: 4},
		"超时过大":    {RequestTimeoutSeconds: 31},
		"总时长过小":   {MaxSessionSeconds: 9, RequestTimeoutSeconds: 5},
		"总时长过大":   {MaxSessionSeconds: 301},
		"总时长小于超时": {RequestTimeoutSeconds: 30, MaxSessionSeconds: 20},
		"地址过长":    {EndpointURL: "https://a.example.com/" + strings.Repeat("x", MaxEndpointURLRunes)},
		"令牌过长":    {TokenAction: TokenReplace, Token: strings.Repeat("t", MaxTokenRunes+1)},
		"替换空令牌":   {TokenAction: TokenReplace},
		"启用无地址":   {Enabled: true},
		"协议非法":    {EndpointURL: "ftp://agent.example.com"},
		"无主机":     {EndpointURL: "https://"},
		"含账号密码":   {EndpointURL: "https://u:p@agent.example.com"},
	}
	// name、draft 是当前非法草稿的场景名与内容。
	for name, draft := range invalid {
		// err 保存非法草稿的校验错误，期望匹配 ErrInvalidInput。
		if _, err := service.Update(context.Background(), 1, "acc", draft); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s 应返回 ErrInvalidInput: %v", name, err)
		}
	}
	// invalidErr 用于取出可展示给用户的参数错误原因。
	var invalidErr *InvalidInputError
	// err 保存启用但缺地址时的校验错误。
	if _, err := service.Update(context.Background(), 1, "acc", Draft{Enabled: true}); !errors.As(err, &invalidErr) || invalidErr.Reason == "" {
		t.Fatalf("应携带可展示原因: %v", err)
	}
	// 越权与存储错误。
	if _, err := service.Update(context.Background(), 2, "acc", Draft{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("越权: %v", err)
	}
	repo.saveErr = errors.New("db down")
	// err 保存仓储写入失败时透传的错误。
	if _, err := service.Update(context.Background(), 1, "acc", Draft{}); err == nil {
		t.Fatal("保存错误应透传")
	}
}

// TestTestUsesDraftAndTokenCommand 验证连接测试对三种令牌命令的取值、超时换算以及错误分支。
func TestTestUsesDraftAndTokenCommand(t *testing.T) {
	// repo 是账号所有者为 1 且已保存令牌 stored 的内存仓储。
	repo := &fakeRepository{owner: 1, token: "stored"}
	// tester 是记录出站目标并返回预设结果的测试器。
	tester := &fakeTester{result: TestResult{Status: TestStatusOK, Handled: true}}
	// service 是同时装配仓储与测试器的被测服务。
	service := NewService(repo, tester)
	// cases 依次验证 retain 用已保存令牌、replace 用草稿令牌、clear 不带令牌。
	for _, item := range []struct{ action, token, want string }{
		{"", "", "stored"}, {TokenRetain, "", "stored"}, {TokenReplace, "draft", "draft"}, {TokenClear, "", ""},
	} {
		// result、err 保存连接测试的诊断结果及错误。
		result, err := service.Test(context.Background(), 1, "acc", TestInput{EndpointURL: "https://agent.example.com", TokenAction: item.action, Token: item.token, RequestTimeoutSeconds: 8})
		if err != nil || result.Status != TestStatusOK || tester.got.Token != item.want || tester.got.Timeout != 8*time.Second || tester.got.URL != "https://agent.example.com" {
			t.Fatalf("%q: got=%+v result=%+v err=%v", item.action, tester.got, result, err)
		}
	}
	// err 保存缺少地址时的校验错误。
	if _, err := service.Test(context.Background(), 1, "acc", TestInput{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("测试必须有地址: %v", err)
	}
	// err 保存越权测试的拒绝错误。
	if _, err := service.Test(context.Background(), 2, "acc", TestInput{EndpointURL: "https://a.example.com"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("越权: %v", err)
	}
	repo.tokenErr = errors.New("decrypt failed")
	// err 保存读取已保存令牌失败时透传的错误。
	if _, err := service.Test(context.Background(), 1, "acc", TestInput{EndpointURL: "https://a.example.com"}); err == nil {
		t.Fatal("读取已保存令牌失败应透传")
	}
	// err 保存未装配测试器时返回的不可用错误。
	if _, err := NewService(repo, nil).Test(context.Background(), 1, "acc", TestInput{EndpointURL: "https://a.example.com"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("未装配 tester: %v", err)
	}
}

// TestTokenReentryRequiredWhenEndpointOriginChanges 验证已保存令牌不会随新的地址来源被沿用：
// 保存与连接测试在 retain 命令下更换协议、主机或端口都必须被拒绝，而替换、清除、仅改路径和未保存令牌的账号不受影响。
func TestTokenReentryRequiredWhenEndpointOriginChanges(t *testing.T) {
	// repo 是已为 https://agent.example.com 保存令牌的内存仓储。
	repo := &fakeRepository{owner: 1, token: "stored", settings: &Settings{CookieID: "acc", EndpointURL: "https://agent.example.com/v1", TokenConfigured: true, RequestTimeoutSeconds: 15, MaxSessionSeconds: 120}}
	// tester 记录连接测试实际发出的出站目标，用于断言被拒绝时没有发出任何请求。
	tester := &fakeTester{result: TestResult{Status: TestStatusOK}}
	// service 是同时装配仓储与测试器的被测服务。
	service := NewService(repo, tester)
	// changed 列出来源发生变化的地址：主机、协议、端口各一种，另含大小写与默认端口等价的写法用于对照。
	changed := []string{"https://evil.example.net/v1", "http://agent.example.com/v1", "https://agent.example.com:8443/v1"}
	// target 是当前被测的新地址。
	for _, target := range changed {
		// err 保存沿用令牌保存到新来源的结果，期望被拒绝且携带可展示原因。
		_, err := service.Update(context.Background(), 1, "acc", Draft{Enabled: true, EndpointURL: target, TokenAction: TokenRetain})
		// invalidErr 用于确认拒绝原因可直接展示给用户。
		var invalidErr *InvalidInputError
		if !errors.As(err, &invalidErr) || !strings.Contains(invalidErr.Reason, "重新输入访问令牌") {
			t.Fatalf("保存 %s 应要求重新输入令牌: %v", target, err)
		}
		if repo.saved != nil {
			t.Fatalf("被拒绝时不得保存: %+v", repo.saved)
		}
		// testErr 保存沿用令牌测试新来源的结果，期望被拒绝且令牌不会被读取或发出。
		_, testErr := service.Test(context.Background(), 1, "acc", TestInput{EndpointURL: target, TokenAction: TokenRetain})
		if !errors.Is(testErr, ErrInvalidInput) || tester.got.URL != "" || tester.got.Token != "" {
			t.Fatalf("测试 %s 不得发出已保存令牌: err=%v got=%+v", target, testErr, tester.got)
		}
	}
	// allowed 列出不应被拒绝的场景：同来源改路径、大小写与默认端口等价、替换令牌、清除令牌。
	allowed := []Draft{
		{Enabled: true, EndpointURL: "https://agent.example.com/other?x=1", TokenAction: TokenRetain},
		{Enabled: true, EndpointURL: "HTTPS://AGENT.example.com:443/v2", TokenAction: TokenRetain},
		{Enabled: true, EndpointURL: "https://evil.example.net/v1", TokenAction: TokenReplace, Token: "new"},
		{Enabled: true, EndpointURL: "https://evil.example.net/v1", TokenAction: TokenClear},
		{EndpointURL: "", TokenAction: TokenRetain},
	}
	// draft 是当前被测的允许场景。
	for _, draft := range allowed {
		// err 保存允许场景的保存结果，期望成功。
		if _, err := service.Update(context.Background(), 1, "acc", draft); err != nil {
			t.Fatalf("%+v 应允许: %v", draft, err)
		}
		// 每次成功保存都会改写 fake 的已保存地址，这里恢复到初始来源以便独立验证下一个场景。
		repo.settings = &Settings{CookieID: "acc", EndpointURL: "https://agent.example.com/v1", TokenConfigured: true, RequestTimeoutSeconds: 15, MaxSessionSeconds: 120}
	}
	// replaceResult、replaceErr 保存替换令牌后测试新来源的结果，期望使用草稿令牌并成功发出。
	if _, replaceErr := service.Test(context.Background(), 1, "acc", TestInput{EndpointURL: "https://evil.example.net", TokenAction: TokenReplace, Token: "draft"}); replaceErr != nil || tester.got.Token != "draft" {
		t.Fatalf("替换令牌后应允许测试新来源: err=%v got=%+v", replaceErr, tester.got)
	}
}

// TestTokenReentryNotRequiredWithoutStoredToken 验证未保存令牌或没有配置的账号更换地址不受限制，且配置读取失败会透传。
func TestTokenReentryNotRequiredWithoutStoredToken(t *testing.T) {
	// repo 是已有配置但没有保存令牌的内存仓储。
	repo := &fakeRepository{owner: 1, settings: &Settings{CookieID: "acc", EndpointURL: "https://agent.example.com", TokenConfigured: false}}
	// service 是仅装配仓储的被测服务。
	service := NewService(repo, nil)
	// err 保存未保存令牌时更换来源的保存结果，期望成功。
	if _, err := service.Update(context.Background(), 1, "acc", Draft{Enabled: true, EndpointURL: "https://other.example.net", TokenAction: TokenRetain}); err != nil {
		t.Fatalf("未保存令牌不应限制换地址: %v", err)
	}
	// fresh 是完全没有配置的账号，Get 返回 ErrConfigNotFound。
	fresh := NewService(&fakeRepository{owner: 1}, nil)
	// err 保存没有配置时的保存结果，期望成功。
	if _, err := fresh.Update(context.Background(), 1, "acc", Draft{Enabled: true, EndpointURL: "https://a.example.net"}); err != nil {
		t.Fatalf("没有配置不应限制: %v", err)
	}
	// readFailure 是模拟的配置读取失败，必须透传而不能当作“无令牌”放行。
	readFailure := errors.New("db down")
	repo.getErr = readFailure
	// err 保存配置读取失败时沿用令牌保存的结果，期望透传读取错误而不是放行。
	if _, err := service.Update(context.Background(), 1, "acc", Draft{Enabled: true, EndpointURL: "https://other.example.net", TokenAction: TokenRetain}); !errors.Is(err, readFailure) {
		t.Fatalf("读取失败应透传: %v", err)
	}
	if endpointOrigin("not a url") != "" || endpointOrigin("https://") != "" {
		t.Fatal("无法解析的地址来源应为空")
	}
	if endpointOrigin("http://Agent.Example.com") != "http://agent.example.com:80" || endpointOrigin("https://a.com:8443/x") != "https://a.com:8443" {
		t.Fatal("来源应补全默认端口并统一小写")
	}
}
