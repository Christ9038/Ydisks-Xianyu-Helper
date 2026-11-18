package qrlogin

import (
	"context"
	"errors"
	"testing"
	"time"

	"xianyu-go/internal/xianyu/cookierefresh"
)

// TestStartCredentialVerificationEntersVerificationState 验证续期验证会话直接进入 verification_required，
// 登记验证地址、临时 Cookie 与完整快照，并重置有效期；t 管理断言。
func TestStartCredentialVerificationEntersVerificationState(t *testing.T) {
	// manager 是被测二维码管理器；测试结束时关闭并等待全部后台任务。
	manager := NewManager(nil)
	defer func() { _ = manager.CloseContext(context.Background()) }()
	// snapshot 是账号当前完整 Cookie 快照的伪造样本。
	snapshot := []cookierefresh.BrowserCookie{{Name: "unb", Value: "fixture-unb", Domain: ".goofish.com", Path: "/"}}
	// verificationURL 使用不属于已支持人脸流程的路径，避免后台任务发起任何网络请求。
	const verificationURL = "https://passport.example.invalid/other/page.htm?token=fixture"

	// sessionID、err 是新建验证会话标识及错误。
	sessionID, err := manager.StartCredentialVerification(context.Background(), "unb=fixture-unb; _m_h5_tk=a_1", snapshot, "  "+verificationURL+"  ")
	if err != nil || sessionID == "" {
		t.Fatalf("sessionID=%q err=%v", sessionID, err)
	}
	// status 是对外可见的会话状态。
	status := manager.GetSessionStatus(sessionID)
	if status["status"] != "verification_required" {
		t.Fatalf("会话应直接进入 verification_required: %+v", status)
	}
	// state 是内部会话快照，用于核对凭证与地址是否登记。
	manager.mu.Lock()
	// sess 保存 该表达式 的结果，供后续分支和断言使用。
	sess := manager.sessions[sessionID]
	manager.mu.Unlock()
	// state 保存 sess.snapshot 的结果，供后续分支和断言使用。
	state := sess.snapshot()
	if state.verificationURL != verificationURL {
		t.Fatalf("验证地址应去除空白后登记: %q", state.verificationURL)
	}
	if state.cookies["_m_h5_tk"] != "a_1" || len(state.cookieSnapshot) != 1 || state.cookieSnapshot[0].Name != "unb" {
		t.Fatalf("临时 Cookie 与快照未正确登记: %+v", state)
	}
	if state.expireTime != 5*time.Minute || time.Since(state.createdTime) > time.Minute {
		t.Fatalf("有效期应从现在起重新计时: %+v", state)
	}
	// 修改调用方持有的快照不得影响会话内部状态。
	snapshot[0].Name = "mutated"
	if sess.snapshot().cookieSnapshot[0].Name != "unb" {
		t.Fatal("会话必须持有快照的独立副本")
	}
}

// TestStartCredentialVerificationWithoutSnapshotUsesFlatCookies 验证没有完整快照时使用扁平 Cookie 播种；t 管理断言。
func TestStartCredentialVerificationWithoutSnapshotUsesFlatCookies(t *testing.T) {
	// manager 是被测二维码管理器。
	manager := NewManager(nil)
	defer func() { _ = manager.CloseContext(context.Background()) }()
	// sessionID、err 是新建验证会话标识及错误。
	sessionID, err := manager.StartCredentialVerification(context.Background(), "unb=1", nil, "https://passport.example.invalid/other")
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	manager.mu.Lock()
	// sess 保存 该表达式 的结果，供后续分支和断言使用。
	sess := manager.sessions[sessionID]
	manager.mu.Unlock()
	// state 保存 sess.snapshot 的结果，供后续分支和断言使用。
	if state := sess.snapshot(); state.cookieSnapshot != nil || state.cookies["unb"] != "1" {
		t.Fatalf("无快照时应只保留扁平 Cookie: %+v", state)
	}
}

// TestStartCredentialVerificationRejectsInvalidInput 验证非 https 地址、空凭证、已取消 Context 与关闭中的管理器都被拒绝；t 管理断言。
func TestStartCredentialVerificationRejectsInvalidInput(t *testing.T) {
	// manager 是被测二维码管理器。
	manager := NewManager(nil)
	defer func() { _ = manager.CloseContext(context.Background()) }()
	// canceled 是已经取消的调用方 Context。
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	// cases 列出非法输入及期望的错误类别。
	cases := []struct {
		// name 是子用例名称。
		name string
		// ctx 是调用方 Context。
		ctx context.Context
		// cookies 是扁平 Cookie 头。
		cookies string
		// url 是验证地址。
		url string
		// wantInvalid 表示期望得到 ErrCredentialVerificationInvalid。
		wantInvalid bool
	}{
		{"http 地址", context.Background(), "unb=1", "http://passport.example.invalid/iv/x", true},
		{"无主机名", context.Background(), "unb=1", "https:///iv/x", true},
		{"空地址", context.Background(), "unb=1", "  ", true},
		{"非法地址", context.Background(), "unb=1", "://bad", true},
		{"空凭证", context.Background(), "  ", "https://passport.example.invalid/iv/x", true},
		{"已取消", canceled, "unb=1", "https://passport.example.invalid/iv/x", false},
	}
	// sample 是当前被验证的非法输入。
	for _, sample := range cases {
		t.Run(sample.name, func(t *testing.T) {
			// sessionID、err 是应被拒绝的创建结果。
			sessionID, err := manager.StartCredentialVerification(sample.ctx, sample.cookies, nil, sample.url)
			if err == nil || sessionID != "" {
				t.Fatalf("应拒绝非法输入: sessionID=%q err=%v", sessionID, err)
			}
			if sample.wantInvalid && !errors.Is(err, ErrCredentialVerificationInvalid) {
				t.Fatalf("应为输入无效错误: %v", err)
			}
		})
	}
	// 非法输入不得遗留任何会话。
	manager.mu.Lock()
	// remaining 保存 len 的结果，供后续分支和断言使用。
	remaining := len(manager.sessions)
	manager.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("拒绝后不应登记会话: %d", remaining)
	}
	// nilManager 是未初始化的管理器。
	var nilManager *Manager
	// err 是 nilManager.StartCredentialVerification 返回的失败原因，用于断言成功或被拒绝的路径。
	if _, err := nilManager.StartCredentialVerification(context.Background(), "unb=1", nil, "https://x.invalid/iv/a"); err == nil {
		t.Fatal("空管理器应返回错误")
	}
}

// TestStartCredentialVerificationRejectsClosingManager 验证管理器进入关闭阶段后不再接受新会话；t 管理断言。
func TestStartCredentialVerificationRejectsClosingManager(t *testing.T) {
	// manager 是被测二维码管理器，先关闭再尝试创建。
	manager := NewManager(nil)
	// err 是 manager.CloseContext 返回的失败原因，用于断言成功或被拒绝的路径。
	if err := manager.CloseContext(context.Background()); err != nil {
		t.Fatalf("关闭失败: %v", err)
	}
	// err 是 manager.StartCredentialVerification 返回的失败原因，用于断言成功或被拒绝的路径。
	if _, err := manager.StartCredentialVerification(context.Background(), "unb=1", nil, "https://x.invalid/other"); err == nil {
		t.Fatal("关闭中的管理器不应创建会话")
	}
}
