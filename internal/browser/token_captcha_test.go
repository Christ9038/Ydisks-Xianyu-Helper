package browser

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
)

type cookieReadResult struct {
	cookies []playwright.Cookie
	err     error
}

type sequenceCookieReader struct {
	results []cookieReadResult
	calls   int
}

func (r *sequenceCookieReader) Cookies(_ ...string) ([]playwright.Cookie, error) {
	index := r.calls
	r.calls++
	if index >= len(r.results) {
		index = len(r.results) - 1
	}
	return r.results[index].cookies, r.results[index].err
}

func TestWaitForFreshX5SecCookieWaitsForChangedValue(t *testing.T) {
	reader := &sequenceCookieReader{results: []cookieReadResult{
		{cookies: []playwright.Cookie{{Name: "x5sec", Value: "old"}}},
		{cookies: []playwright.Cookie{
			{Name: "x5sec", Value: "new"},
			{Name: "x5sectag", Value: "tag"},
		}},
	}}

	got, err := waitForFreshX5SecCookie(context.Background(), reader, map[string]struct{}{"old": {}}, 100*time.Millisecond, time.Millisecond)
	if err != nil {
		t.Fatalf("waitForFreshX5SecCookie: %v", err)
	}
	if got["x5sec"] != "new" || got["x5sectag"] != "tag" {
		t.Fatalf("未返回完整的新 x5 Cookie，got %#v", got)
	}
	if reader.calls < 2 {
		t.Fatalf("旧值不应被立即接受，calls=%d", reader.calls)
	}
}

func TestWaitForFreshX5SecCookieAcceptsFirstValueWithoutBaseline(t *testing.T) {
	reader := &sequenceCookieReader{results: []cookieReadResult{{cookies: []playwright.Cookie{{Name: "X5SEC", Value: "fresh"}}}}}
	got, err := waitForFreshX5SecCookie(context.Background(), reader, nil, 100*time.Millisecond, time.Millisecond)
	if err != nil {
		t.Fatalf("waitForFreshX5SecCookie: %v", err)
	}
	if got["X5SEC"] != "fresh" {
		t.Fatalf("x5sec 名称匹配应不区分大小写，got %#v", got)
	}
}

func TestWaitForFreshX5SecCookieTimesOutOnStaleValue(t *testing.T) {
	reader := &sequenceCookieReader{results: []cookieReadResult{{cookies: []playwright.Cookie{{Name: "x5sec", Value: "old"}}}}}
	_, err := waitForFreshX5SecCookie(context.Background(), reader, map[string]struct{}{"old": {}}, 5*time.Millisecond, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "未获取到新的") {
		t.Fatalf("持续旧值应超时，got %v", err)
	}
}

func TestWaitForFreshX5SecCookieHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := &sequenceCookieReader{results: []cookieReadResult{{}}}
	_, err := waitForFreshX5SecCookie(ctx, reader, nil, time.Second, time.Millisecond)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("应返回 context.Canceled，got %v", err)
	}
	if reader.calls != 0 {
		t.Fatalf("上下文已取消时不应再读取 Cookie，calls=%d", reader.calls)
	}
}

func TestWaitForFreshX5SecCookieReturnsReadError(t *testing.T) {
	want := errors.New("read failed")
	reader := &sequenceCookieReader{results: []cookieReadResult{{err: want}}}
	_, err := waitForFreshX5SecCookie(context.Background(), reader, nil, time.Second, time.Millisecond)
	if !errors.Is(err, want) {
		t.Fatalf("应保留底层读取错误，got %v", err)
	}
}

func TestWaitForFreshX5SecCookieRejectsInvalidTiming(t *testing.T) {
	reader := &sequenceCookieReader{results: []cookieReadResult{{}}}
	_, err := waitForFreshX5SecCookie(context.Background(), reader, nil, 0, time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "必须大于 0") {
		t.Fatalf("无效超时应被拒绝，got %v", err)
	}
	if reader.calls != 0 {
		t.Fatalf("参数无效时不应读取 Cookie，calls=%d", reader.calls)
	}
}

func TestFreshX5CookiesFiltersRelatedNames(t *testing.T) {
	got, fresh := freshX5Cookies([]playwright.Cookie{
		{Name: "x5sec", Value: "value"},
		{Name: "x5sectag", Value: "tag"},
		{Name: "other", Value: "ignored"},
	}, nil)
	if !fresh || len(got) != 2 || got["x5sec"] != "value" || got["x5sectag"] != "tag" {
		t.Fatalf("freshX5Cookies got=%#v fresh=%v", got, fresh)
	}
}

func TestFreshX5CookiesChoosesNewValueAcrossDomains(t *testing.T) {
	got, fresh := freshX5Cookies([]playwright.Cookie{
		{Name: "x5sec", Value: "old", Domain: ".taobao.com"},
		{Name: "x5sec", Value: "new", Domain: ".goofish.com"},
		{Name: "x5sec", Value: "old", Domain: ".alipay.com"},
	}, map[string]struct{}{"old": {}})
	if !fresh || got["x5sec"] != "new" {
		t.Fatalf("应选择新域名 Cookie 值，got=%#v fresh=%v", got, fresh)
	}
}

func TestX5SecValuesCollectsAllDomains(t *testing.T) {
	got := x5SecValues([]playwright.Cookie{
		{Name: "x5sec", Value: "one", Domain: ".goofish.com"},
		{Name: "X5SEC", Value: "two", Domain: ".taobao.com"},
		{Name: "x5sectag", Value: "ignored"},
	})
	if len(got) != 2 {
		t.Fatalf("应收集所有域名的旧 x5sec 值，got %#v", got)
	}
	if _, ok := got["one"]; !ok {
		t.Fatal("缺少 one")
	}
	if _, ok := got["two"]; !ok {
		t.Fatal("缺少 two")
	}
}
