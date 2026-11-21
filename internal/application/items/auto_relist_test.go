package items

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// relistRepoStub 是自动上架仓储替身，字段控制各读取结果。
type relistRepoStub struct {
	// enabled、enabledErr 是账号开关及读取错误。
	enabled    bool
	enabledErr error
	// owned、ownedErr 是商品归属及读取错误。
	owned    bool
	ownedErr error
	// userID、userErr 是账号归属用户及读取错误。
	userID  int64
	userErr error
}

// AutoRelistEnabled 返回预设开关。
func (r relistRepoStub) AutoRelistEnabled(context.Context, string) (bool, error) {
	return r.enabled, r.enabledErr
}

// AccountUserID 返回预设归属用户。
func (r relistRepoStub) AccountUserID(context.Context, string) (int64, error) {
	return r.userID, r.userErr
}

// OwnsItem 返回预设商品归属。
func (r relistRepoStub) OwnsItem(context.Context, string, string) (bool, error) {
	return r.owned, r.ownedErr
}

// relistPlatformStub 记录平台调用次数并返回预设结果。
type relistPlatformStub struct {
	// calls 是被调用次数。
	calls int
	// outcome、err 是预设返回。
	outcome RelistOutcome
	err     error
}

// RelistItem 记录调用并返回预设结果。
func (p *relistPlatformStub) RelistItem(context.Context, int64, string, string) (RelistOutcome, error) {
	p.calls++
	return p.outcome, p.err
}

// runRelist 构造服务并处理一次付款，返回平台调用次数与日志文本。
func runRelist(t *testing.T, repo relistRepoStub, platform *relistPlatformStub, orders ...string) (*AutoRelistService, string) {
	t.Helper()
	// buffer 收集日志，用于断言跳过原因被打印。
	var buffer bytes.Buffer
	// err 是本次调用返回的错误，用于断言错误映射或失败分类；service 是被测的自动上架应用服务。
	service, err := NewAutoRelistService(repo, platform, slog.New(slog.NewTextHandler(&buffer, nil)))
	if err != nil {
		t.Fatal(err)
	}
	// order 是本次付款事件对应的订单号。
	for _, order := range orders {
		service.HandlePaid(context.Background(), "cid", "item-1", order)
	}
	return service, buffer.String()
}

// TestAutoRelistDisabledOrNotOwnedMakesNoPlatformCall 验证开关关闭、读取失败、商品不属于账号时不请求平台。
func TestAutoRelistDisabledOrNotOwnedMakesNoPlatformCall(t *testing.T) {
	// cases 是各种应当跳过的前置状态。
	cases := map[string]relistRepoStub{
		"开关关闭":   {enabled: false, owned: true, userID: 1},
		"开关读取失败": {enabledErr: errors.New("db"), owned: true, userID: 1},
		"商品不属于":  {enabled: true, owned: false, userID: 1},
		"归属读取失败": {enabled: true, ownedErr: errors.New("db"), userID: 1},
		"用户读取失败": {enabled: true, owned: true, userErr: errors.New("db")},
	}
	// name、repo 是用例名与仓储替身。
	for name, repo := range cases {
		// platform 是记录平台调用次数的平台端口替身。
		platform := &relistPlatformStub{}
		runRelist(t, repo, platform, "o1")
		if platform.calls != 0 {
			t.Fatalf("%s 不应请求平台: %d", name, platform.calls)
		}
	}
}

// TestAutoRelistCallsOncePerOrderAndLogsOutcomes 验证成功只调用一次、重复订单被忽略，以及各结果的日志。
func TestAutoRelistCallsOncePerOrderAndLogsOutcomes(t *testing.T) {
	// repo 是开关开启且商品属于账号的仓储。
	repo := relistRepoStub{enabled: true, owned: true, userID: 1}
	// platform 是记录平台调用次数的平台端口替身。
	platform := &relistPlatformStub{outcome: RelistOutcome{RestoredQuantity: true}}
	// logs 是本次处理产生的日志文本，用于断言跳过原因。
	_, logs := runRelist(t, repo, platform, "o1", "o1")
	if platform.calls != 1 || !strings.Contains(logs, "已自动重新上架") {
		t.Fatalf("重复订单应只调用一次: calls=%d logs=%s", platform.calls, logs)
	}
	platform = &relistPlatformStub{outcome: RelistOutcome{AlreadyOnSale: true}}
	if _, logs = runRelist(t, repo, platform, "o2"); !strings.Contains(logs, "仍在售") {
		t.Fatalf("已在售应记录原因: %s", logs)
	}
	// 空订单号不去重但仍只调用一次平台。
	platform = &relistPlatformStub{}
	runRelist(t, repo, platform, "")
	if platform.calls != 1 {
		t.Fatalf("空订单号应处理一次: %d", platform.calls)
	}
}

// TestAutoRelistFailuresOnlyLogAndNeverRetry 验证不支持类目打印原因，风控、会话失效和普通错误都只调用一次且不重试。
func TestAutoRelistFailuresOnlyLogAndNeverRetry(t *testing.T) {
	// repo 是开关开启且商品属于账号的仓储。
	repo := relistRepoStub{enabled: true, owned: true, userID: 1}
	// cases 是平台错误与期望日志片段。
	cases := map[string]struct {
		err  error
		want string
	}{
		"不支持类目":  {ErrRelistEditUnsupported, "类目不支持网页端编辑"},
		"风控":     {ErrRelistRiskVerification, "安全验证"},
		"会话失效":   {ErrRelistSessionExpired, "登录已失效"},
		"凭证变化":   {ErrRelistCredentialChanged, "凭证已变化"},
		"客户端不支持": {ErrRelistUnsupported, "不支持商品重新上架"},
		"其他错误":   {errors.New("网络错误"), "本次不重试"},
	}
	// name、c 是用例名与错误场景。
	for name, c := range cases {
		// platform 是记录平台调用次数的平台端口替身。
		platform := &relistPlatformStub{err: c.err}
		// logs 是本次处理产生的日志文本，用于断言跳过原因。
		_, logs := runRelist(t, repo, platform, "o1")
		if platform.calls != 1 || !strings.Contains(logs, c.want) {
			t.Fatalf("%s: calls=%d logs=%s", name, platform.calls, logs)
		}
	}
}

// TestAutoRelistConstructionAndGuards 验证依赖校验、空标识忽略、nil 服务安全和去重表容量回收。
func TestAutoRelistConstructionAndGuards(t *testing.T) {
	// err 是本次调用返回的错误，用于断言错误映射或失败分类。
	if _, err := NewAutoRelistService(nil, &relistPlatformStub{}, nil); err == nil {
		t.Fatal("缺少仓储应报错")
	}
	// err 是本次调用返回的错误，用于断言错误映射或失败分类。
	if _, err := NewAutoRelistService(relistRepoStub{}, nil, nil); err == nil {
		t.Fatal("缺少平台端口应报错")
	}
	// nilService 是空服务指针，用于验证 nil 接收者安全。
	var nilService *AutoRelistService
	nilService.HandlePaid(context.Background(), "cid", "item", "o")
	// platform 是记录平台调用次数的平台端口替身。
	platform := &relistPlatformStub{}
	// service 是被测的自动上架应用服务。
	service, _ := runRelist(t, relistRepoStub{enabled: true, owned: true, userID: 1}, platform)
	service.HandlePaid(context.Background(), " ", "item", "o")
	service.HandlePaid(context.Background(), "cid", " ", "o")
	if platform.calls != 0 {
		t.Fatal("空账号或商品不应请求平台")
	}
	// i 是填满去重表的订单序号，超过上限后表被整体回收。
	for i := 0; i <= autoRelistSeenLimit+1; i++ {
		service.claim(strings.Repeat("x", 1) + string(rune('a'+i%26)) + strings.Repeat("y", i/26))
	}
	if len(service.seen) > autoRelistSeenLimit {
		t.Fatalf("去重表应有容量上限: %d", len(service.seen))
	}
}
