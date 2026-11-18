package renewal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"xianyu-go/internal/db"
	apirenew "xianyu-go/internal/xianyu/renew"
)

// verificationNotice 记录一次发给通知器的事件，便于核对类别与正文。
type verificationNotice struct {
	// accountID 是被通知的账号标识。
	accountID string
	// eventType 是通知类别。
	eventType string
	// title、body 是通知标题与正文。
	title, body string
}

// recordingNotifier 记录全部账号事件，与既有只计数的替身不同，它保留事件类别。
type recordingNotifier struct {
	// mu 保护 notices；调度器的 watcher 协程会并发调用。
	mu sync.Mutex
	// notices 按到达顺序保存收到的事件。
	notices []verificationNotice
}

// NotifyAccountEvent 记录一次事件。
func (n *recordingNotifier) NotifyAccountEvent(accountID, eventType, _ string, title, body string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.notices = append(n.notices, verificationNotice{accountID: accountID, eventType: eventType, title: title, body: body})
}

// snapshot 返回当前已记录事件的副本。
func (n *recordingNotifier) snapshot() []verificationNotice {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]verificationNotice(nil), n.notices...)
}

// fakeVerificationRegistry 是待验证登记表替身，按账号去重并记录登记内容。
type fakeVerificationRegistry struct {
	// mu 保护 urls。
	mu sync.Mutex
	// urls 保存各账号最近一次登记的验证地址。
	urls map[string]string
}

// Register 登记账号；此前不存在登记时返回 true。
func (f *fakeVerificationRegistry) Register(accountID, verificationURL string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.urls == nil {
		f.urls = make(map[string]string)
	}
	// existed 标记 该表达式 取得的条目是否存在或调用是否成功。
	_, existed := f.urls[accountID]
	f.urls[accountID] = verificationURL
	return !existed
}

// iframeRedirectServer 创建返回 iframeRedirect 中间状态并下发新 Cookie 的模拟平台；token 是伪造的验证令牌。
func iframeRedirectServer(delay time.Duration) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(delay)
		http.SetCookie(w, &http.Cookie{Name: "_m_h5_tk", Value: "rotated_1", Path: "/"})
		_, _ = w.Write([]byte(`{"content":{"data":{"iframeRedirect":true,"iframeRedirectUrl":"https://passport.example.invalid/iv/identity_verify.htm?token=fixture-secret","resultCode":100}},"success":true}`))
	}))
}

// healthyLongLoginCookie 返回长登录未过期、未处于疲劳窗口的伪造 Cookie 头。
func healthyLongLoginCookie() string {
	return "unb=1; havana_lgc_exp=" + strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
}

// TestAPICookieRenewVerificationRequiredNotifiesOnFirstFailure 验证遇到 iframeRedirect 时：
// 响应 Cookie 照常保存、日志标记且不含令牌、首次即发出 security_verification 通知、通知不含验证地址，
// 并把地址登记给验证通道；同一要求再次出现时不重复通知；t 管理断言。
func TestAPICookieRenewVerificationRequiredNotifiesOnFirstFailure(t *testing.T) {
	// cleanup 保存 newSchedulerTestStore 的结果，供后续分支和断言使用；store 保存 newSchedulerTestStore 的结果，供后续分支和断言使用。
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	// account 保存 createSchedulerAccount 的结果，供后续分支和断言使用。
	account := createSchedulerAccount(t, store, "cid-verify", healthyLongLoginCookie())
	// srv 保存 iframeRedirectServer 的结果，供后续分支和断言使用。
	srv := iframeRedirectServer(0)
	defer srv.Close()
	// notifier、registry 是被注入的通知器与登记表替身。
	notifier, registry := &recordingNotifier{}, &fakeVerificationRegistry{}
	// s 保存 NewScheduler 的结果，供后续分支和断言使用。
	s := NewScheduler(store, nil, nil, nil, notifier)
	s.SetVerificationRegistry(registry)
	s.api = apirenew.Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1}

	s.apiCookieRenewOne(context.Background(), "batch-verify", account)

	// log 是写入的续期日志；状态仍为 failed，且带验证标记。
	log := lastAPIRenewLog(t, store, account.ID)
	if log.status != "failed" || !strings.Contains(log.stepDetails, "verification_required=true") {
		t.Fatalf("日志应为 failed 且带验证标记: %+v", log)
	}
	if strings.Contains(log.responseContent, "fixture-secret") || strings.Contains(log.stepDetails, "fixture-secret") || strings.Contains(log.message, "fixture-secret") {
		t.Fatalf("验证令牌不得落库: %+v", log)
	}
	// detail 是重新读取的账号凭证，响应 Cookie 应已保存（保持原有行为）。
	detail, err := store.Cookies.GetDetails(context.Background(), account.ID)
	if err != nil || !strings.Contains(detail.Value, "_m_h5_tk=rotated_1") {
		t.Fatalf("响应 Cookie 应照常保存: %v %q", err, detail.Value)
	}
	// notices 是首次失败后收到的通知，只能有一条 security_verification。
	notices := notifier.snapshot()
	if len(notices) != 1 || notices[0].eventType != "security_verification" || notices[0].accountID != account.ID {
		t.Fatalf("首次失败即应发出一条 security_verification 通知: %+v", notices)
	}
	if strings.Contains(notices[0].body, "fixture-secret") || strings.Contains(notices[0].body, "passport.example.invalid") || !strings.Contains(notices[0].body, "「完成安全验证」") {
		t.Fatalf("通知不得含验证地址且应指引验证通道: %q", notices[0].body)
	}
	// got 保存 strings.Contains 的结果，供后续分支和断言使用。
	if got := registry.urls[account.ID]; !strings.Contains(got, "/iv/identity_verify.htm") {
		t.Fatalf("验证地址应登记给验证通道: %q", got)
	}
	// 同一要求仍在有效期内再次出现：不重复通知。
	s.apiCookieRenewOne(context.Background(), "batch-verify-2", account)
	// got 保存 len 的结果，供后续分支和断言使用。
	if got := len(notifier.snapshot()); got != 1 {
		t.Fatalf("重复的验证要求不应再次通知: %d", got)
	}
}

// TestAPICookieRenewVerificationRequiredWithoutRegistryFallsBackToRescan 验证没有登记表时仍通知，但提示重新扫码；t 管理断言。
func TestAPICookieRenewVerificationRequiredWithoutRegistryFallsBackToRescan(t *testing.T) {
	// cleanup 保存 newSchedulerTestStore 的结果，供后续分支和断言使用；store 保存 newSchedulerTestStore 的结果，供后续分支和断言使用。
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	// account 保存 createSchedulerAccount 的结果，供后续分支和断言使用。
	account := createSchedulerAccount(t, store, "cid-verify-norg", healthyLongLoginCookie())
	// srv 保存 iframeRedirectServer 的结果，供后续分支和断言使用。
	srv := iframeRedirectServer(0)
	defer srv.Close()
	// notifier 保存 该表达式 的结果，供后续分支和断言使用。
	notifier := &recordingNotifier{}
	// s 保存 NewScheduler 的结果，供后续分支和断言使用。
	s := NewScheduler(store, nil, nil, nil, notifier)
	s.api = apirenew.Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1}

	s.apiCookieRenewOne(context.Background(), "batch-verify-norg", account)

	// notices 保存 notifier.snapshot 的结果，供后续分支和断言使用。
	notices := notifier.snapshot()
	if len(notices) != 1 || !strings.Contains(notices[0].body, "重新扫码") || strings.Contains(notices[0].body, "「完成安全验证」") {
		t.Fatalf("没有验证通道时应提示重新扫码: %+v", notices)
	}
}

// TestAPICookieRenewVerificationRequiredWithoutNotifier 验证没有通知器时不 panic 且仍登记验证地址；t 管理断言。
func TestAPICookieRenewVerificationRequiredWithoutNotifier(t *testing.T) {
	// cleanup 保存 newSchedulerTestStore 的结果，供后续分支和断言使用；store 保存 newSchedulerTestStore 的结果，供后续分支和断言使用。
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	// account 保存 createSchedulerAccount 的结果，供后续分支和断言使用。
	account := createSchedulerAccount(t, store, "cid-verify-nonotify", healthyLongLoginCookie())
	// srv 保存 iframeRedirectServer 的结果，供后续分支和断言使用。
	srv := iframeRedirectServer(0)
	defer srv.Close()
	// registry 保存 该表达式 的结果，供后续分支和断言使用。
	registry := &fakeVerificationRegistry{}
	// s 保存 NewScheduler 的结果，供后续分支和断言使用。
	s := NewScheduler(store, nil, nil, nil)
	s.SetVerificationRegistry(registry)
	s.api = apirenew.Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1}

	s.apiCookieRenewOne(context.Background(), "batch-verify-nonotify", account)

	if registry.urls[account.ID] == "" {
		t.Fatal("没有通知器时仍应登记验证地址")
	}
	// nilScheduler 是未初始化的调度器，相关入口应安全返回。
	var nilScheduler *Scheduler
	nilScheduler.SetVerificationRegistry(registry)
	nilScheduler.noteVerificationRequired("x", &apirenew.Result{VerificationRequired: true})
}

// TestPendingAPIRenewVerificationRequiredNotifies 验证迟到响应是 iframeRedirect 时同样通知并标记日志；t 管理断言。
func TestPendingAPIRenewVerificationRequiredNotifies(t *testing.T) {
	// cleanup 保存 newSchedulerTestStore 的结果，供后续分支和断言使用；store 保存 newSchedulerTestStore 的结果，供后续分支和断言使用。
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	// account 保存 createSchedulerAccount 的结果，供后续分支和断言使用。
	account := createSchedulerAccount(t, store, "cid-verify-late", healthyLongLoginCookie())
	// srv 先拖过 2 秒 Promise 窗口（测试中缩短为 10 毫秒）再返回验证中间态。
	srv := iframeRedirectServer(60 * time.Millisecond)
	defer srv.Close()
	// notifier 保存 该表达式 的结果，供后续分支和断言使用；registry 保存 该表达式 的结果，供后续分支和断言使用。
	notifier, registry := &recordingNotifier{}, &fakeVerificationRegistry{}
	// s 保存 NewScheduler 的结果，供后续分支和断言使用。
	s := NewScheduler(store, nil, nil, nil, notifier)
	s.SetVerificationRegistry(registry)
	s.api = apirenew.Service{HTTPClient: srv.Client(), SilentHasLoginURL: srv.URL, RetryDelay: -1, PromiseTimeout: 10 * time.Millisecond}

	s.apiCookieRenewOne(context.Background(), "batch-verify-late", account)
	s.watchers.Wait()

	// log 保存 lastAPIRenewLog 的结果，供后续分支和断言使用。
	if log := lastAPIRenewLog(t, store, account.ID); log.status != "failed" || !strings.Contains(log.stepDetails, "verification_required=true") || strings.Contains(log.responseContent, "fixture-secret") {
		t.Fatalf("迟到响应日志应带验证标记且不含令牌: %+v", log)
	}
	// notices 保存 notifier.snapshot 的结果，供后续分支和断言使用。
	if notices := notifier.snapshot(); len(notices) != 1 || notices[0].eventType != "security_verification" {
		t.Fatalf("迟到的验证要求也应通知一次: %+v", notices)
	}
	if registry.urls[account.ID] == "" {
		t.Fatal("迟到响应也应登记验证地址")
	}
}

// TestAddAPILogConsecutiveFailuresIgnoreSkipped 验证夹在失败之间的 skipped 与 pending 不再让“连续 3 次失败”通知失效，
// 而成功仍会打断连续计数；t 管理断言。
func TestAddAPILogConsecutiveFailuresIgnoreSkipped(t *testing.T) {
	// cleanup 保存 newSchedulerTestStore 的结果，供后续分支和断言使用；store 保存 newSchedulerTestStore 的结果，供后续分支和断言使用。
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	// notifier 保存 该表达式 的结果，供后续分支和断言使用。
	notifier := &recordingNotifier{}
	// s 保存 NewScheduler 的结果，供后续分支和断言使用。
	s := NewScheduler(store, nil, nil, nil, notifier)
	// ctx 保存 context.Background 的结果，供后续分支和断言使用。
	ctx := context.Background()
	// add 写入一条指定状态的续期日志。
	add := func(cookieID, status string) {
		s.addAPILog(ctx, db.RenewalLog{BatchID: "b", CookieID: cookieID, Status: status, ErrorMessage: "接口超时"})
	}
	// 失败、跳过（疲劳窗口）、失败、等待、失败：三次终态失败应通知一次。
	for _, status := range []string{"failed", "skipped", "failed", "pending", "failed"} {
		add("cid-skip", status)
	}
	// got 保存 len 的结果，供后续分支和断言使用。
	if got := len(notifier.snapshot()); got != 1 {
		t.Fatalf("夹杂 skipped/pending 的三次终态失败应通知 1 次: %d", got)
	}
	// 第四次终态失败不应重复通知。
	add("cid-skip", "failed")
	// got 保存 len 的结果，供后续分支和断言使用。
	if got := len(notifier.snapshot()); got != 1 {
		t.Fatalf("第四次失败不应重复通知: %d", got)
	}
	// 中间出现成功时计数被打断：失败、失败、成功、失败不应通知。
	for _, status := range []string{"failed", "failed", "success", "failed"} {
		add("cid-broken", status)
	}
	// got 保存 len 的结果，供后续分支和断言使用。
	if got := len(notifier.snapshot()); got != 1 {
		t.Fatalf("被成功打断的失败序列不应通知: %d", got)
	}
}
