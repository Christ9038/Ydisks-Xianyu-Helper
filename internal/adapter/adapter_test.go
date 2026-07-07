package adapter

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/browser"
	"xianyu-go/internal/db"
)

// fakeNotifier 记录告警调用，供 OnAccountAlert 断言。
type fakeNotifier struct {
	alerts []struct{ cookieID, level, title, body string }
}

func (f *fakeNotifier) NotifyAccountAlert(cookieID, level, title, body string) {
	f.alerts = append(f.alerts, struct{ cookieID, level, title, body string }{cookieID, level, title, body})
}

// fakeBrowser 桩实现 browserManager 接口，记录调用并返回可控结果。
type fakeBrowser struct {
	fetchErr     error
	fetchDetail  *browser.OrderDetail
	loginErr     error
	loginCookies map[string]string
	loginCalls   int
}

func (f *fakeBrowser) FetchOrderDetail(_ context.Context, _, _, _ string, _ ...bool) (*browser.OrderDetail, error) {
	return f.fetchDetail, f.fetchErr
}
func (f *fakeBrowser) PasswordLogin(_ context.Context, _, _, _, _ string, _ bool) (map[string]string, error) {
	f.loginCalls++
	return f.loginCookies, f.loginErr
}

func newAdapterTestStore(t *testing.T) (*db.Store, func()) {
	t.Helper()
	d, _, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "adapt.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	s := db.NewStore(d, db.DialectSQLite)
	ctx := context.Background()
	s.Users.Create(ctx, "admin", "a@e.com", "pw")
	admin, _ := s.Users.GetByUsername(ctx, "admin")
	s.Cookies.Save(ctx, "cid", "unb=1; _m_h5_tk=tk;", admin.ID)
	return s, func() { d.Close() }
}

// TestOnAccountAlert_ForwardedToNotifier 注入 notifier 后告警被转发；未注入时不 panic。
func TestOnAccountAlert_ForwardedToNotifier(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	a := New(store, nil, nil)

	// 未注入 notifier：不应 panic，仅记录日志。
	a.OnAccountAlert(context.Background(), "cid", "warn", "t", "b")

	n := &fakeNotifier{}
	a.SetNotifier(n)
	a.OnAccountAlert(context.Background(), "cid", "warn", "token 失效", "请重新登录")
	if len(n.alerts) != 1 || n.alerts[0].cookieID != "cid" || n.alerts[0].title != "token 失效" {
		t.Fatalf("告警未转发: %+v", n.alerts)
	}
}

// TestHandleSystemEvent_UninjectedSafe 未注入 automation 时安全返回 nil。
func TestHandleSystemEvent_UninjectedSafe(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	a := New(store, nil, nil)
	if err := a.HandleSystemEvent(context.Background(), automation.Task{AccountID: "cid"}); err != nil {
		t.Fatalf("未注入 automation 应返回 nil: %v", err)
	}
}

// TestFetchOrderDetail_LocalHitShortCircuits 本地订单字段齐全时不启动浏览器。
func TestFetchOrderDetail_LocalHitShortCircuits(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO orders (order_id, cookie_id, spec_name, spec_value, quantity, amount) VALUES ('o-local','cid','套餐','30天','1','9.9')`)

	// browser=nil，本地命中应短路；若误走浏览器分支会 panic。
	a := New(store, nil, nil)
	detail, err := a.FetchOrderDetail(ctx, "cid", "o-local", "item-1", "buyer-1", "cookie")
	if err != nil {
		t.Fatalf("本地命中不应报错: %v", err)
	}
	if detail.SpecName != "套餐" || detail.Amount != "9.9" {
		t.Fatalf("detail=%+v", detail)
	}
}

// TestFetchOrderDetail_BrowserNilReturnsError 本地缺字段且浏览器未启用时返回明确错误。
func TestFetchOrderDetail_BrowserNilReturnsError(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	a := New(store, nil, nil)
	_, err := a.FetchOrderDetail(context.Background(), "cid", "o-missing", "item-1", "buyer-1", "cookie")
	if err == nil {
		t.Fatal("browser=nil 且本地缺失应返回错误")
	}
}

// TestFetchOrderDetail_BrowserFallback 本地缺失但有浏览器时调用浏览器抓取并保存刷新的 cookie。
func TestFetchOrderDetail_BrowserFallback(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	ctx := context.Background()
	a := New(store, nil, nil)
	a.SetBrowser(&fakeBrowser{
		fetchDetail: &browser.OrderDetail{
			Quantity: "2", SpecName: "套餐", SpecValue: "30天", Amount: "19.8",
			UpdatedCookies: "unb=1; _m_h5_tk=newtoken;",
		},
	})
	detail, err := a.FetchOrderDetail(ctx, "cid", "o-fallback", "item-1", "buyer-1", "old-cookie")
	if err != nil {
		t.Fatalf("浏览器兜底应成功: %v", err)
	}
	if detail.SpecValue != "30天" || detail.Amount != "19.8" {
		t.Fatalf("detail=%+v", detail)
	}
	// UpdatedCookies 与入参不同时应保存。
	saved, _ := store.Cookies.GetValue(ctx, "cid")
	if saved != "unb=1; _m_h5_tk=newtoken;" {
		t.Fatalf("刷新的 cookie 未保存: %q", saved)
	}
}

// TestOnPasswordLoginRefresh_BrowserNilReturnsFalse 浏览器未启用时直接返回 false。
func TestOnPasswordLoginRefresh_BrowserNilReturnsFalse(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	a := New(store, nil, nil)
	if a.OnPasswordLoginRefresh(context.Background(), "cid") {
		t.Fatal("browser=nil 应返回 false")
	}
}

// TestOnPasswordLoginRefresh_NoCredentialsReturnsFalse 有浏览器但账号未配密码时返回 false。
func TestOnPasswordLoginRefresh_NoCredentialsReturnsFalse(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	a := New(store, nil, nil)
	a.SetBrowser(&fakeBrowser{})
	if a.OnPasswordLoginRefresh(context.Background(), "cid") {
		t.Fatal("账号未配用户名/密码应返回 false")
	}
}

// TestOnPasswordLoginRefresh_Success 配好凭据后浏览器登录成功，cookie 被保存。
func TestOnPasswordLoginRefresh_Success(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	ctx := context.Background()
	// 配置账号用户名/密码。
	store.DB.ExecContext(ctx, `UPDATE cookies SET username='u', password='p' WHERE id='cid'`)

	fb := &fakeBrowser{loginCookies: map[string]string{"unb": "1", "_m_h5_tk": "fresh"}}
	a := New(store, nil, nil)
	a.SetBrowser(fb)
	if !a.OnPasswordLoginRefresh(ctx, "cid") {
		t.Fatal("登录成功应返回 true")
	}
	if fb.loginCalls != 1 {
		t.Fatalf("PasswordLogin 应调用一次，got %d", fb.loginCalls)
	}
	saved, _ := store.Cookies.GetValue(ctx, "cid")
	if saved == "" {
		t.Fatal("刷新的 cookie 应已保存")
	}
}

// TestOnPasswordLoginRefresh_LoginError 浏览器登录返回错误时返回 false 且不保存 cookie。
func TestOnPasswordLoginRefresh_LoginError(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	ctx := context.Background()
	store.DB.ExecContext(ctx, `UPDATE cookies SET username='u', password='p' WHERE id='cid'`)

	fb := &fakeBrowser{loginErr: errors.New("captcha required")}
	a := New(store, nil, nil)
	a.SetBrowser(fb)
	if a.OnPasswordLoginRefresh(ctx, "cid") {
		t.Fatal("登录失败应返回 false")
	}
}

// TestOnPasswordLoginRefresh_Cooldown 同一账号短时间内第二次刷新被冷却拒绝。
func TestOnPasswordLoginRefresh_Cooldown(t *testing.T) {
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	ctx := context.Background()
	store.DB.ExecContext(ctx, `UPDATE cookies SET username='u', password='p' WHERE id='cid'`)

	fb := &fakeBrowser{loginCookies: map[string]string{"unb": "1"}}
	a := New(store, nil, nil)
	a.SetBrowser(fb)
	if !a.OnPasswordLoginRefresh(ctx, "cid") {
		t.Fatal("首次应成功")
	}
	// 第二次在冷却期内，应被拒绝且不调用浏览器。
	if a.OnPasswordLoginRefresh(ctx, "cid") {
		t.Fatal("冷却期内应返回 false")
	}
	if fb.loginCalls != 1 {
		t.Fatalf("冷却期内不应调用浏览器，got calls=%d", fb.loginCalls)
	}
}
