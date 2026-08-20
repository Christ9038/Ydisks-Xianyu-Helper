package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
	xrenew "xianyu-go/internal/xianyu/renew"
	"xianyu-go/internal/xianyu/ws"
)

// fakeRunMtop 返回成功 token，不触网。
type fakeRunMtop struct{ token string }

// FetchUserProfile 封装Fetch用户Profile业务协调。
func (f *fakeRunMtop) FetchUserProfile(context.Context, string) (*mtop.UserProfileResult, error) {
	return nil, nil
}

// shortTokenMtop 用于本次流程后续判断的short令牌Mtop
type shortTokenMtop struct {
	fakeRunMtop
	calls atomic.Int32
}

// RefreshTokenWithDeviceIDContext 刷新令牌WithDeviceID上下文。
func (s *shortTokenMtop) RefreshTokenWithDeviceIDContext(context.Context, string, string) (*mtop.RefreshResult, error) {
	// call 用于本次流程后续判断的call
	call := s.calls.Add(1)
	return &mtop.RefreshResult{AccessToken: fmt.Sprintf("short-%d", call), AccessTokenExpireAt: time.Now().Add(2 * time.Second).Unix()}, nil
}

// ConsignContext 封装Consign上下文业务协调。
func (f *fakeRunMtop) ConsignContext(context.Context, string, string) (bool, []string, string, error) {
	return true, nil, "", nil
}

// AdjustOrderPriceContext 满足 MTOP 客户端接口，引擎测试不关心订单改价。
func (f *fakeRunMtop) AdjustOrderPriceContext(context.Context, string, string, int64) (bool, []string, string, error) {
	return true, nil, "", nil
}

// FetchItemsPage 封装Fetch商品列表页码业务协调。
func (f *fakeRunMtop) FetchItemsPage(context.Context, string, int, int) (*mtop.ItemListResult, error) {
	return nil, nil
}

// FetchAllItems 封装FetchAll商品列表业务协调。
func (f *fakeRunMtop) FetchAllItems(context.Context, string, int, int) (*mtop.ItemListResult, error) {
	return nil, nil
}

// PublishItem 封装发布商品业务协调。
func (f *fakeRunMtop) PublishItem(context.Context, string, mtop.PublishItemRequest) (*mtop.PublishItemResult, error) {
	return nil, nil
}

// RefreshTokenWithDeviceIDContext 刷新令牌WithDeviceID上下文。
func (f *fakeRunMtop) RefreshTokenWithDeviceIDContext(_ context.Context, _ string, _ string) (*mtop.RefreshResult, error) {
	return &mtop.RefreshResult{AccessToken: f.token, AccessTokenExpireAt: time.Now().Add(time.Hour).Unix()}, nil
}

// fakeFailTokenMtop 用于本次流程后续判断的fakeFail令牌Mtop
type fakeFailTokenMtop struct{ err error }

// FetchUserProfile 封装Fetch用户Profile业务协调。
func (f *fakeFailTokenMtop) FetchUserProfile(context.Context, string) (*mtop.UserProfileResult, error) {
	return nil, nil
}

// ConsignContext 封装Consign上下文业务协调。
func (f *fakeFailTokenMtop) ConsignContext(context.Context, string, string) (bool, []string, string, error) {
	return true, nil, "", nil
}

// AdjustOrderPriceContext 满足 MTOP 客户端接口，token 失败测试不关心订单改价。
func (f *fakeFailTokenMtop) AdjustOrderPriceContext(context.Context, string, string, int64) (bool, []string, string, error) {
	return true, nil, "", nil
}

// FetchItemsPage 封装Fetch商品列表页码业务协调。
func (f *fakeFailTokenMtop) FetchItemsPage(context.Context, string, int, int) (*mtop.ItemListResult, error) {
	return nil, nil
}

// FetchAllItems 封装FetchAll商品列表业务协调。
func (f *fakeFailTokenMtop) FetchAllItems(context.Context, string, int, int) (*mtop.ItemListResult, error) {
	return nil, nil
}

// PublishItem 封装发布商品业务协调。
func (f *fakeFailTokenMtop) PublishItem(context.Context, string, mtop.PublishItemRequest) (*mtop.PublishItemResult, error) {
	return nil, nil
}

// RefreshTokenWithDeviceIDContext 刷新令牌WithDeviceID上下文。
func (f *fakeFailTokenMtop) RefreshTokenWithDeviceIDContext(context.Context, string, string) (*mtop.RefreshResult, error) {
	return nil, f.err
}

// sequencedTokenMtop 用于本次流程后续判断的sequenced令牌Mtop
type sequencedTokenMtop struct {
	fakeRunMtop
	mu      sync.Mutex
	devices []string
	cookies []string
	calls   int
}

// RefreshTokenWithDeviceIDContext 刷新令牌WithDeviceID上下文。
func (s *sequencedTokenMtop) RefreshTokenWithDeviceIDContext(_ context.Context, cookieStr, deviceID string) (*mtop.RefreshResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.devices = append(s.devices, deviceID)
	s.cookies = append(s.cookies, cookieStr)
	return &mtop.RefreshResult{
		AccessToken:         fmt.Sprintf("fresh-%d", s.calls),
		AccessTokenExpireAt: time.Now().Add(time.Hour).Unix(),
	}, nil
}

// fakeWSConn 实现 WSConn，可控地投递消息并阻塞到 ctx 取消。
type fakeWSConn struct {
	mu            sync.Mutex
	closed        bool
	registerErr   error
	registeredDID string
	registeredTok string
	sentTexts     []string
	sentImages    []string
	imageWidths   []int
	imageHeights  []int
	heartbeatDone chan struct{}
	closeCh       chan struct{}
	closeOnce     sync.Once
	// onReceive 在 ReceiveLoop 启动时被调用，参数是 onMessage 回调，便于测试投递消息。
	onReceive func(onMessage func(map[string]any))
	// recvBlock 控制 ReceiveLoop 是否阻塞到 ctx 取消（默认 true）。
	recvBlock bool
	recvErr   error
}

// Register 封装Register业务协调。
func (f *fakeWSConn) Register(_ context.Context, deviceID, accessToken string) error {
	f.mu.Lock()
	f.registeredDID = deviceID
	f.registeredTok = accessToken
	// err 用于本次流程后续判断的err
	err := f.registerErr
	f.mu.Unlock()
	return err
}

// HeartbeatLoop 封装HeartbeatLoop业务协调。
func (f *fakeWSConn) HeartbeatLoop(ctx context.Context, _ time.Duration) error {
	<-ctx.Done()
	if f.heartbeatDone != nil {
		close(f.heartbeatDone)
	}
	return ctx.Err()
}

// ReceiveLoop 封装ReceiveLoop业务协调。
func (f *fakeWSConn) ReceiveLoop(ctx context.Context, onMessage func(map[string]any)) error {
	if f.onReceive != nil {
		f.onReceive(onMessage)
	}
	if f.recvBlock {
		if f.closeCh != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-f.closeCh:
				return nil
			}
		}
		<-ctx.Done()
		return ctx.Err()
	}
	return f.recvErr
}

// Close 关闭当前值。
func (f *fakeWSConn) Close() error {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
	if f.closeCh != nil {
		f.closeOnce.Do(func() { close(f.closeCh) })
	}
	return nil
}

// TestRunRotatesTokenBeforeExpiry 封装Test运行Rotates令牌BeforeExpiry业务协调。
func TestRunRotatesTokenBeforeExpiry(t *testing.T) {
	// tokenClient 用于本次流程后续判断的令牌Client
	tokenClient := &shortTokenMtop{}
	// acc、cleanup 用于本次流程后续判断的acc、cleanup
	acc, _, _, cleanup := newRunAccount(t, tokenClient)
	defer cleanup()
	// first 用于本次流程后续判断的first
	first := &fakeWSConn{recvBlock: true, closeCh: make(chan struct{})}
	// second 用于本次流程后续判断的second
	second := &fakeWSConn{recvBlock: true, closeCh: make(chan struct{})}
	acc.wsDialer = &fakeDialer{results: []dialResult{{conn: first}, {conn: second}}}
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithCancel(context.Background())
	// done 用于本次流程后续判断的done
	done := make(chan error, 1)
	go func() { done <- acc.Run(ctx) }()
	// deadline 用于本次流程后续判断的deadline
	deadline := time.Now().Add(4 * time.Second)
	for tokenClient.calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if // got 用于本次流程后续判断的got
	got := tokenClient.calls.Load(); got < 2 {
		cancel()
		t.Fatalf("Token 到期前未主动轮换，calls=%d status=%+v", got, acc.RuntimeStatus())
	}
	// status 用于本次流程后续判断的状态
	status := acc.RuntimeStatus()
	if status.TokenExpiresAt.IsZero() || status.TokenRefreshAt.IsZero() || !status.TokenRefreshAt.Before(status.TokenExpiresAt) {
		t.Fatalf("Token 有效期状态异常: %+v", status)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("账号在 Token 轮换测试后未退出")
	}
}

// SendText 封装Send文本业务协调。
func (f *fakeWSConn) SendText(_ context.Context, _, _, _, text string) error {
	f.mu.Lock()
	f.sentTexts = append(f.sentTexts, text)
	f.mu.Unlock()
	return nil
}

// SendImage 封装Send图片业务协调。
func (f *fakeWSConn) SendImage(_ context.Context, _, _, _, url string, width, height int) error {
	f.mu.Lock()
	f.sentImages = append(f.sentImages, url)
	f.imageWidths = append(f.imageWidths, width)
	f.imageHeights = append(f.imageHeights, height)
	f.mu.Unlock()
	return nil
}

// TestAccountSendImagePreservesDimensions 验证账号运行时将调用方提供的真实图片尺寸交给 WebSocket。
func TestAccountSendImagePreservesDimensions(t *testing.T) {
	// account 是未启动但已具备账号身份的运行时对象。
	account := New(Config{CookieID: "image-size-test", CookieStr: "unb=me"})
	// conn 是记录图片 URL 和像素尺寸的 WebSocket 测试替身。
	conn := &fakeWSConn{}
	account.runtimeMu.Lock()
	account.conn = conn
	account.runtimeMu.Unlock()
	// sendErr 表示账号运行时向 WebSocket 发送图片的结果。
	sendErr := account.SendImage(context.Background(), "chat", "buyer", "https://cdn.example/image.jpg", 0, 1920, 1080)
	if sendErr != nil {
		t.Fatalf("SendImage() error=%v", sendErr)
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if len(conn.imageWidths) != 1 || conn.imageWidths[0] != 1920 || len(conn.imageHeights) != 1 || conn.imageHeights[0] != 1080 {
		t.Fatalf("图片尺寸未透传 widths=%v heights=%v", conn.imageWidths, conn.imageHeights)
	}
}

// fakeDialer 按预设序列返回连接或错误，第 N 次（1-based）调用返回 dialResults[N-1]。
type fakeDialer struct {
	mu      sync.Mutex
	results []dialResult // 每次.Dial 的结果
	calls   int
	conns   []*fakeWSConn
	configs []ws.Config
	lastCfg ws.Config // 记录最后一次 Dial 的配置（含 AccessToken）
}

// dialResult 用于本次流程后续判断的dial结果
type dialResult struct {
	conn *fakeWSConn
	err  error
}

// Dial 封装Dial业务协调。
func (d *fakeDialer) Dial(_ context.Context, cfg ws.Config, _ *slog.Logger) (WSConn, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lastCfg = cfg
	d.configs = append(d.configs, cfg)
	// idx 用于本次流程后续判断的idx
	idx := d.calls
	d.calls++
	if idx >= len(d.results) {
		// 超出预设：返回最后一个 conn，避免无限重连耗尽测试。
		if len(d.results) > 0 {
			// last 用于本次流程后续判断的last
			last := d.results[len(d.results)-1]
			return last.conn, last.err
		}
		return nil, nil
	}
	// r 用于本次流程后续判断的r
	r := d.results[idx]
	if r.conn != nil {
		d.conns = append(d.conns, r.conn)
	}
	return r.conn, r.err
}

// newRunAccount 构造一个用 fakeMtop + fakeDialer 的 Account，不触网。
func newRunAccount(t *testing.T, mtopClient mtop.Client) (*Account, *recordingHandler, *db.Store, func()) {
	t.Helper()
	// dbPath 用于本次流程后续判断的db路径
	dbPath := filepath.Join(t.TempDir(), "run.db")
	// d、err 用于本次流程后续判断的d、err
	d, _, err := db.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// store 用于本次流程后续判断的store
	store := db.NewStore(d, db.DialectSQLite)
	store.Users.Create(context.Background(), "admin", "a@e.com", "pw")
	// admin 用于本次流程后续判断的admin
	admin, _ := store.Users.GetByUsername(context.Background(), "admin")
	store.Cookies.Save(context.Background(), "cid", "unb=123; _m_h5_tk=tk_1;", admin.ID)
	store.Cookies.SetStatus(context.Background(), "cid", true)

	// h 用于本次流程后续判断的h
	h := &recordingHandler{}
	// acc 用于本次流程后续判断的acc
	acc := New(Config{
		CookieID:  "cid",
		CookieStr: "unb=123; _m_h5_tk=tk_1;",
		Store:     store,
		Handler:   h,
		MTop:      mtopClient,
	})
	return acc, h, store, func() { d.Close() }
}

// TestRun_ConnectsAndDispatchesMessage 验证 Run 主循环：
// 刷新 token → 拨号 WS → ReceiveLoop 投递消息 → dispatch 进 handler 防抖链 → ctx 取消后优雅退出。
// TestRun_ConnectsAndDispatchesMessage 封装Test运行ConnectsAndDispatches消息业务协调。
func TestRun_ConnectsAndDispatchesMessage(t *testing.T) {
	// acc、h、cleanup 用于本次流程后续判断的acc、h、cleanup
	acc, h, _, cleanup := newRunAccount(t, &fakeRunMtop{token: "tok-1"})
	defer cleanup()

	// conn 用于本次流程后续判断的conn
	conn := &fakeWSConn{
		recvBlock: true,
		onReceive: func(onMessage func(map[string]any)) {
			// 投递一条普通聊天消息。
			onMessage(map[string]any{
				"1": map[string]any{
					"2": "chat-1@goofish",
					"10": map[string]any{
						"reminderContent": "你好",
						"senderUserId":    "buyer-1",
						"senderNick":      "买家",
						"reminderUrl":     "fleamarket://message_chat?itemId=item-1&peerUserId=buyer-1",
					},
				},
			})
		},
	}
	acc.wsDialer = &fakeDialer{results: []dialResult{{conn: conn}}}

	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithCancel(context.Background())
	// runDone 用于本次流程后续判断的运行Done
	runDone := make(chan error, 1)
	go func() { runDone <- acc.Run(ctx) }()

	// 等待防抖延迟后消息进入 handler。
	deadline := time.After(3 * time.Second)
	for {
		h.mu.Lock()
		// n 用于本次流程后续判断的n
		n := len(h.chats)
		h.mu.Unlock()
		if n >= 1 {
			break
		}
		select {
		case <-deadline:
			cancel()
			t.Fatalf("Run 未在 3s 内投递消息到 handler，chats=%d", n)
		default:
		}
		time.Sleep(50 * time.Millisecond)
	}

	// token 必须在握手成功后通过 Register 发送，而不是在 Dial 前获得。
	conn.mu.Lock()
	// gotToken 用于本次流程后续判断的got令牌
	gotToken := conn.registeredTok == "tok-1"
	conn.mu.Unlock()
	if !gotToken {
		t.Fatal("Register 未收到 token=tok-1")
	}

	// 取消 ctx → Run 应退出。
	cancel()
	select {
	case // err 用于本次流程后续判断的err
	err := <-runDone:
		if err != nil && err != context.Canceled {
			t.Fatalf("Run 退出 err=%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run 未在 ctx 取消后 3s 内退出")
	}
}

// TestRun_DialFailureRequiresRelogin mirrors the web page: native
// CONNECT_FAILED enters CONN_ERROR and does not auto-reconnect.
// TestRun_DialFailureRequiresRelogin 封装Test运行DialFailureRequiresRelogin业务协调。
func TestRun_DialFailureRequiresRelogin(t *testing.T) {
	// acc、cleanup 用于本次流程后续判断的acc、cleanup
	acc, _, _, cleanup := newRunAccount(t, &fakeRunMtop{token: "tok-1"})
	defer cleanup()

	// 拨号始终失败。
	acc.wsDialer = &fakeDialer{results: []dialResult{{err: errFakeDial}}}

	// runDone 用于本次流程后续判断的运行Done
	runDone := make(chan error, 1)
	go func() { runDone <- acc.Run(context.Background()) }()
	select {
	case // err 用于本次流程后续判断的err
	err := <-runDone:
		if !errors.Is(err, errFakeDial) {
			t.Fatalf("Run error=%v，期望拨号错误", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("拨号失败后 Run 未退出")
	}
	if // status 用于本次流程后续判断的状态
	status := acc.RuntimeStatus(); status.State != RuntimeAuthExpired {
		t.Fatalf("runtime state=%q，期望 %q", status.State, RuntimeAuthExpired)
	}
}

// TestRun_ReceiveLoopEndsTriggersReconnect 正常结束后直接重连，并重新获取 token。
func TestRun_ReceiveLoopEndsTriggersReconnect(t *testing.T) {
	// mtopClient 用于本次流程后续判断的mtopClient
	mtopClient := &countingMtop{fakeRunMtop: fakeRunMtop{token: "tok-1"}}
	// acc、cleanup 用于本次流程后续判断的acc、cleanup
	acc, _, _, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()

	conn1 := &fakeWSConn{recvBlock: false} // 立即返回，模拟断线
	conn2 := &fakeWSConn{recvBlock: true}  // 第二次连上后阻塞
	d := &fakeDialer{results: []dialResult{{conn: conn1}, {conn: conn2}}}
	acc.wsDialer = d

	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// runDone 用于本次流程后续判断的运行Done
	runDone := make(chan error, 1)
	go func() { runDone <- acc.Run(ctx) }()

	// 正常 close 不计失败、也不退避。
	deadline := time.After(2 * time.Second)
	for {
		d.mu.Lock()
		// calls 用于本次流程后续判断的calls
		calls := d.calls
		d.mu.Unlock()
		if calls >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("Run 未在 2s 内重连，calls=%d", calls)
		default:
		}
		time.Sleep(100 * time.Millisecond)
	}

	cancel()
	select {
	case <-runDone:
	case <-time.After(3 * time.Second):
		t.Fatal("重连后 ctx 取消 Run 未退出")
	}
	if // calls 用于本次流程后续判断的calls
	calls := atomic.LoadInt32(&mtopClient.calls); calls != 2 {
		t.Fatalf("正常重连应重新请求 mtop: calls=%d", calls)
	}
}

// TestRun_ReconnectUsesFreshTokenAndStableDeviceID 封装Test运行ReconnectUsesFresh令牌AndStableDeviceID业务协调。
func TestRun_ReconnectUsesFreshTokenAndStableDeviceID(t *testing.T) {
	// mtopClient 用于本次流程后续判断的mtopClient
	mtopClient := &sequencedTokenMtop{fakeRunMtop: fakeRunMtop{token: "unused"}}
	// acc、cleanup 用于本次流程后续判断的acc、cleanup
	acc, _, _, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()
	// first 用于本次流程后续判断的first
	first := &fakeWSConn{recvBlock: false}
	// second 用于本次流程后续判断的second
	second := &fakeWSConn{recvBlock: true}
	// dialer 用于本次流程后续判断的dialer
	dialer := &fakeDialer{results: []dialResult{
		{conn: first},
		{conn: second},
	}}
	acc.wsDialer = dialer

	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithCancel(context.Background())
	// done 用于本次流程后续判断的done
	done := make(chan error, 1)
	go func() { done <- acc.Run(ctx) }()
	waitForDialCalls(t, dialer, 2)
	waitForRegisteredToken(t, second, "fresh-2")
	cancel()
	<-done

	dialer.mu.Lock()
	// conns 用于本次流程后续判断的conns
	conns := append([]*fakeWSConn(nil), dialer.conns...)
	dialer.mu.Unlock()
	if len(conns) < 2 {
		t.Fatalf("dial conns=%d want>=2", len(conns))
	}
	conns[0].mu.Lock()
	// firstToken 用于本次流程后续判断的first令牌
	firstToken := conns[0].registeredTok
	conns[0].mu.Unlock()
	conns[1].mu.Lock()
	// secondToken 用于本次流程后续判断的second令牌
	secondToken := conns[1].registeredTok
	conns[1].mu.Unlock()
	if firstToken != "fresh-1" || secondToken != "fresh-2" {
		t.Fatalf("reconnect tokens=%v", []string{firstToken, secondToken})
	}
	mtopClient.mu.Lock()
	// devices 用于本次流程后续判断的devices
	devices := append([]string(nil), mtopClient.devices...)
	mtopClient.mu.Unlock()
	if len(devices) != 2 || devices[0] == "" || devices[0] != devices[1] {
		t.Fatalf("device IDs must remain stable: %v", devices)
	}
}

// TestRun_EstablishedNetworkErrorReconnectsWithFreshToken 封装Test运行EstablishedNetwork错误ReconnectsWithFresh令牌业务协调。
func TestRun_EstablishedNetworkErrorReconnectsWithFreshToken(t *testing.T) {
	// mtopClient 用于本次流程后续判断的mtopClient
	mtopClient := &countingMtop{fakeRunMtop: fakeRunMtop{token: "tok-1"}}
	// acc、store、cleanup 用于本次流程后续判断的acc、store、cleanup
	acc, _, store, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()
	// first 用于本次流程后续判断的first
	first := &fakeWSConn{recvErr: errors.New("connection reset by peer")}
	// second 用于本次流程后续判断的second
	second := &fakeWSConn{recvBlock: true}
	// dialer 用于本次流程后续判断的dialer
	dialer := &fakeDialer{results: []dialResult{{conn: first}, {conn: second}}}
	acc.wsDialer = dialer

	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithCancel(context.Background())
	// done 用于本次流程后续判断的done
	done := make(chan error, 1)
	go func() { done <- acc.Run(ctx) }()
	waitForDialCalls(t, dialer, 2)
	waitForRegisteredToken(t, second, "tok-1")
	acc.mu.Lock()
	// currentToken 用于本次流程后续判断的current令牌
	currentToken := acc.currentToken
	// networkFailures 用于本次流程后续判断的networkFailures
	networkFailures := acc.networkFailures
	acc.mu.Unlock()
	if networkFailures != 0 || currentToken != "tok-1" {
		cancel()
		<-done
		t.Fatalf("网络断线后应立即用新凭证恢复连接: failures=%d token=%q", networkFailures, currentToken)
	}
	if // cached、err 用于本次流程后续判断的cached、err
	cached, err := store.Tokens.Get(context.Background(), "cid"); err != nil || cached.AccessToken != "tok-1" {
		cancel()
		<-done
		t.Fatalf("重连成功后应缓存新连接凭证: cached=%+v err=%v", cached, err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("取消后 Run 未退出")
	}
	if // calls 用于本次流程后续判断的calls
	calls := atomic.LoadInt32(&mtopClient.calls); calls != 2 {
		t.Fatalf("官网重连会重新获取连接凭证: calls=%d", calls)
	}
}

// TestRun_InvalidRegTokenRequiresRelogin 封装Test运行InvalidReg令牌RequiresRelogin业务协调。
func TestRun_InvalidRegTokenRequiresRelogin(t *testing.T) {
	// mtopClient 用于本次流程后续判断的mtopClient
	mtopClient := &countingMtop{fakeRunMtop: fakeRunMtop{token: "fresh-token"}}
	// acc、cleanup 用于本次流程后续判断的acc、cleanup
	acc, _, _, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()
	// dialer 用于本次流程后续判断的dialer
	dialer := &fakeDialer{results: []dialResult{
		{conn: &fakeWSConn{registerErr: &ws.RegError{Kind: ws.RegErrorInvalidToken, Code: 401, Reason: "invalid token"}}},
	}}
	acc.wsDialer = dialer

	// done 用于本次流程后续判断的done
	done := make(chan error, 1)
	go func() { done <- acc.Run(context.Background()) }()
	select {
	case // err 用于本次流程后续判断的err
	err := <-done:
		if !ws.IsInvalidTokenError(err) {
			t.Fatalf("Run error=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("invalid /reg 后 Run 未退出")
	}

	if // calls 用于本次流程后续判断的calls
	calls := atomic.LoadInt32(&mtopClient.calls); calls != 1 {
		t.Fatalf("invalid /reg 不应在页面要求重新登录时静默重试: calls=%d", calls)
	}
	if dialer.calls != 1 {
		t.Fatalf("invalid /reg dial calls=%d want 1", dialer.calls)
	}
	if // status 用于本次流程后续判断的状态
	status := acc.RuntimeStatus(); status.State != RuntimeAuthExpired {
		t.Fatalf("runtime state=%q want %q", status.State, RuntimeAuthExpired)
	}
}

// TestRun_ReloadsDatabaseCookieBeforeNaturalReconnect 封装Test运行ReloadsDatabase登录凭证BeforeNaturalReconnect业务协调。
func TestRun_ReloadsDatabaseCookieBeforeNaturalReconnect(t *testing.T) {
	// mtopClient 用于本次流程后续判断的mtopClient
	mtopClient := &sequencedTokenMtop{fakeRunMtop: fakeRunMtop{token: "token"}}
	// acc、store、cleanup 用于本次流程后续判断的acc、store、cleanup
	acc, _, store, cleanup := newRunAccount(t, mtopClient)
	defer cleanup()
	// newCookie 用于本次流程后续判断的new登录凭证
	newCookie := "unb=123; _m_h5_tk=db-renewed_2; cookie2=new"
	// first 用于本次流程后续判断的first
	first := &fakeWSConn{recvBlock: false, onReceive: func(func(map[string]any)) {
		if // err 用于本次流程后续判断的err
		err := store.Cookies.UpdateValueExisting(context.Background(), "cid", newCookie); err != nil {
			t.Errorf("update Cookie: %v", err)
		}
	}}
	// dialer 用于本次流程后续判断的dialer
	dialer := &fakeDialer{results: []dialResult{
		{conn: first},
		{conn: &fakeWSConn{recvBlock: true}},
	}}
	acc.wsDialer = dialer

	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithCancel(context.Background())
	// done 用于本次流程后续判断的done
	done := make(chan error, 1)
	go func() { done <- acc.Run(ctx) }()
	waitForDialCalls(t, dialer, 2)
	cancel()
	<-done

	mtopClient.mu.Lock()
	// cookies 用于本次流程后续判断的cookies
	cookies := append([]string(nil), mtopClient.cookies...)
	mtopClient.mu.Unlock()
	if len(cookies) < 2 || cookies[1] != newCookie {
		t.Fatalf("second token Cookie=%v want=%q", cookies, newCookie)
	}
	if // calls 用于本次流程后续判断的calls
	calls := mtopClient.calls; calls != 2 {
		t.Fatalf("Cookie change should invalidate token for reconnect, calls=%d", calls)
	}
}

// waitForDialCalls 封装waitForDialCalls业务协调。
func waitForDialCalls(t *testing.T, dialer *fakeDialer, want int) {
	t.Helper()
	// deadline 用于本次流程后续判断的deadline
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		dialer.mu.Lock()
		// calls 用于本次流程后续判断的calls
		calls := dialer.calls
		dialer.mu.Unlock()
		if calls >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	dialer.mu.Lock()
	// calls 用于本次流程后续判断的calls
	calls := dialer.calls
	dialer.mu.Unlock()
	t.Fatalf("dial calls=%d want>=%d", calls, want)
}

// waitForRegisteredToken 封装waitForRegistered令牌业务协调。
func waitForRegisteredToken(t *testing.T, conn *fakeWSConn, want string) {
	t.Helper()
	// deadline 用于本次流程后续判断的deadline
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn.mu.Lock()
		// token 用于本次流程后续判断的令牌
		token := conn.registeredTok
		conn.mu.Unlock()
		if token == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	conn.mu.Lock()
	// token 用于本次流程后续判断的令牌
	token := conn.registeredTok
	conn.mu.Unlock()
	t.Fatalf("registered token=%q want=%q", token, want)
}

// TestRun_DisabledAccountExits 账号被禁用时 Run 立即退出。
func TestRun_DisabledAccountExits(t *testing.T) {
	// acc、store、cleanup 用于本次流程后续判断的acc、store、cleanup
	acc, _, store, cleanup := newRunAccount(t, &fakeRunMtop{token: "tok-1"})
	defer cleanup()
	// 禁用账号。
	store.Cookies.SetStatus(context.Background(), "cid", false)

	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// done 用于本次流程后续判断的done
	done := make(chan error, 1)
	go func() { done <- acc.Run(ctx) }()
	select {
	case // err 用于本次流程后续判断的err
	err := <-done:
		if err != nil {
			t.Fatalf("禁用账号 Run 应返回 nil，got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("禁用账号 Run 应立即退出")
	}
}

// TestRun_APIRenewFailureDoesNotBlockTokenAndWebSocket 封装Test运行APIRenewFailureDoesNotBlock令牌AndWebSocket业务协调。
func TestRun_APIRenewFailureDoesNotBlockTokenAndWebSocket(t *testing.T) {
	// acc、cleanup 用于本次流程后续判断的acc、cleanup
	acc, _, _, cleanup := newRunAccount(t, &fakeRunMtop{token: "tok-after-renew-error"})
	defer cleanup()

	// renewer 用于本次流程后续判断的renewer
	renewer := &stubCookieRenewer{err: errors.New("startup API renewal failed")}
	acc.renewer = renewer
	// conn 用于本次流程后续判断的conn
	conn := &fakeWSConn{recvBlock: true}
	acc.wsDialer = &fakeDialer{results: []dialResult{{conn: conn}}}

	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithCancel(context.Background())
	// done 用于本次流程后续判断的done
	done := make(chan error, 1)
	go func() { done <- acc.Run(ctx) }()

	waitForRegisteredToken(t, conn, "tok-after-renew-error")
	if renewer.calls != 1 {
		t.Fatalf("启动时协议续期调用次数=%d want=1", renewer.calls)
	}
	cancel()
	select {
	case // err 用于本次流程后续判断的err
	err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error=%v want context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
}

// TestRun_APIRenewSuccessRebuildsOfficialPageDeviceID 封装Test运行APIRenewSuccessRebuildsOfficial页码DeviceID业务协调。
func TestRun_APIRenewSuccessRebuildsOfficialPageDeviceID(t *testing.T) {
	// tokenClient 用于本次流程后续判断的令牌Client
	tokenClient := &sequencedTokenMtop{}
	// acc、cleanup 用于本次流程后续判断的acc、cleanup
	acc, _, _, cleanup := newRunAccount(t, tokenClient)
	defer cleanup()

	acc.mu.Lock()
	// initialDeviceID 用于本次流程后续判断的initialDeviceID
	initialDeviceID := acc.deviceID
	acc.mu.Unlock()
	acc.renewer = &stubCookieRenewer{result: &xrenew.Result{
		Success:     true,
		RenewMethod: "auto_login_plugin",
		NewCookies:  "unb=123; _m_h5_tk=tk_1;",
	}}
	// conn 用于本次流程后续判断的conn
	conn := &fakeWSConn{recvBlock: true}
	acc.wsDialer = &fakeDialer{results: []dialResult{{conn: conn}}}

	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithCancel(context.Background())
	// done 用于本次流程后续判断的done
	done := make(chan error, 1)
	go func() { done <- acc.Run(ctx) }()

	waitForRegisteredToken(t, conn, "fresh-1")
	conn.mu.Lock()
	// registeredDeviceID 用于本次流程后续判断的registeredDeviceID
	registeredDeviceID := conn.registeredDID
	conn.mu.Unlock()
	if registeredDeviceID == initialDeviceID {
		t.Fatal("官网 auto-login 成功后的逻辑 reload 必须重建页面级 device ID")
	}
	tokenClient.mu.Lock()
	if len(tokenClient.devices) != 1 || tokenClient.devices[0] != registeredDeviceID {
		t.Fatalf("token 与 /reg 必须绑定同一新 device ID: token=%v reg=%q", tokenClient.devices, registeredDeviceID)
	}
	tokenClient.mu.Unlock()

	cancel()
	select {
	case // err 用于本次流程后续判断的err
	err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error=%v want context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
}

// TestRun_TokenFetchFailureRetriesWithoutDisablingAccount 封装Test运行令牌FetchFailureRetriesWithoutDisabling账号业务协调。
func TestRun_TokenFetchFailureRetriesWithoutDisablingAccount(t *testing.T) {
	// acc、store、cleanup 用于本次流程后续判断的acc、store、cleanup
	acc, _, store, cleanup := newRunAccount(t, &fakeFailTokenMtop{err: errFakeDial})
	defer cleanup()
	acc.wsDialer = &fakeDialer{results: []dialResult{{conn: &fakeWSConn{recvBlock: true}}}}
	// h 用于本次流程后续判断的h
	h := &failingRefreshHandler{}
	acc.handler = h
	acc.mu.Lock()
	acc.tokenFetchFailures = TokenFetchDisableThreshold - 1
	acc.mu.Unlock()

	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithCancel(context.Background())
	// done 用于本次流程后续判断的done
	done := make(chan error, 1)
	go func() { done <- acc.Run(ctx) }()
	// deadline 用于本次流程后续判断的deadline
	deadline := time.Now().Add(2 * time.Second)
	for acc.RuntimeStatus().State != RuntimeReconnecting && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if // status 用于本次流程后续判断的状态
	status := acc.RuntimeStatus(); status.State != RuntimeReconnecting {
		t.Fatalf("runtime state=%q want %q", status.State, RuntimeReconnecting)
	}
	if !store.Cookies.GetStatus(context.Background(), "cid") {
		t.Fatal("token failure threshold must not disable account")
	}
	// event 表示当前遍历过程中的event
	for _, event := range h.events {
		if event == EventAccountDisabled {
			t.Fatalf("unexpected disable event: events=%+v alerts=%+v", h.events, h.alerts)
		}
	}
	cancel()
	select {
	case // err 用于本次流程后续判断的err
	err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error=%v want context canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
}

// errFakeDial 用于本次流程后续判断的errFakeDial
var errFakeDial = fakeDialErr{}

// fakeDialErr 用于本次流程后续判断的fakeDialErr
type fakeDialErr struct{}

// Error 封装错误业务协调。
func (fakeDialErr) Error() string { return "fake dial failure" }
