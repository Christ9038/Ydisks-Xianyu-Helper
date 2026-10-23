package mtop

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestPacer 构造使用固定时钟、零抖动并记录睡眠时长的节拍器；now 与 slept 由测试持有以便推进时间和断言。
func newTestPacer(now *time.Time, slept *[]time.Duration) *Pacer {
	return &Pacer{
		next:   map[string]time.Time{},
		now:    func() time.Time { return *now },
		jitter: func() time.Duration { return 0 },
		sleep: func(_ context.Context, d time.Duration) error {
			*slept = append(*slept, d)
			return nil
		},
	}
}

// TestPacerSpacesRequestsPerAccount 验证同账号请求被依次排开，不同账号互不影响。
func TestPacerSpacesRequestsPerAccount(t *testing.T) {
	// now 固定为同一时刻，模拟同一瞬间到达的并发请求；slept 记录每次需要等待的时长。
	now, slept := time.Unix(1000, 0), []time.Duration{}
	// pacer 是确定性节拍器。
	pacer := newTestPacer(&now, &slept)
	// keys 依次是账号 a 的三次请求和账号 b 的一次请求。
	for _, key := range []string{"a", "a", "a", "b"} {
		// waitErr 保存本次预约等待的错误。
		if waitErr := pacer.Wait(context.Background(), key, 2*time.Second); waitErr != nil {
			t.Fatal(waitErr)
		}
	}
	// want 依次为 a 的 0/2/4 秒与 b 的 0 秒。
	want := []time.Duration{0, 2 * time.Second, 4 * time.Second, 0}
	// index 遍历期望等待序列。
	for index := range want {
		if slept[index] != want[index] {
			t.Fatalf("第 %d 次等待=%v，期望 %v", index, slept[index], want[index])
		}
	}
}

// TestPacerIdleAccountDoesNotWait 验证空闲超过间隔后请求立即放行。
func TestPacerIdleAccountDoesNotWait(t *testing.T) {
	// now 是可推进的固定时钟，slept 记录等待时长。
	now, slept := time.Unix(1000, 0), []time.Duration{}
	// pacer 是确定性节拍器。
	pacer := newTestPacer(&now, &slept)
	_ = pacer.Wait(context.Background(), "a", 2*time.Second)
	now = now.Add(10 * time.Second)
	_ = pacer.Wait(context.Background(), "a", 2*time.Second)
	if slept[1] != 0 {
		t.Fatalf("空闲后不应等待，实际 %v", slept[1])
	}
}

// TestPacerSweepsIdleAccounts 验证长期空闲账号的记录被清理，避免内存无限增长。
func TestPacerSweepsIdleAccounts(t *testing.T) {
	// now 是可推进的固定时钟，slept 记录等待时长。
	now, slept := time.Unix(1000, 0), []time.Duration{}
	// pacer 是确定性节拍器。
	pacer := newTestPacer(&now, &slept)
	_ = pacer.Wait(context.Background(), "old", time.Second)
	now = now.Add(pacerIdleRetention + time.Minute)
	_ = pacer.Wait(context.Background(), "new", time.Second)
	// exists 表示过期账号记录是否仍在。
	if _, exists := pacer.next["old"]; exists {
		t.Fatal("过期账号记录应被清理")
	}
}

// TestPacerCancelledWaitReturnsError 验证等待被取消时返回错误且时隙保持占用。
func TestPacerCancelledWaitReturnsError(t *testing.T) {
	// now 是固定时钟，slept 仅为构造节拍器所需。
	now, slept := time.Unix(1000, 0), []time.Duration{}
	// pacer 是确定性节拍器，其睡眠被替换为返回取消错误。
	pacer := newTestPacer(&now, &slept)
	// cancelErr 是模拟的取消错误。
	cancelErr := errors.New("取消")
	pacer.sleep = func(context.Context, time.Duration) error { return cancelErr }
	// waitErr 保存等待被取消时返回的错误。
	waitErr := pacer.Wait(context.Background(), "a", time.Second)
	if !errors.Is(waitErr, cancelErr) {
		t.Fatalf("err=%v", waitErr)
	}
	if !pacer.next["a"].After(now) {
		t.Fatal("取消后时隙仍应占用")
	}
}

// TestSleepContext 验证真实睡眠的完成、取消与零时长分支。
func TestSleepContext(t *testing.T) {
	// zeroErr、shortErr 分别保存零时长和短时长等待的错误。
	zeroErr, shortErr := sleepContext(context.Background(), 0), sleepContext(context.Background(), time.Millisecond)
	if zeroErr != nil || shortErr != nil {
		t.Fatalf("zero=%v short=%v", zeroErr, shortErr)
	}
	// ctx、cancel 是已取消的上下文。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// canceledErr 保存取消后等待返回的错误。
	canceledErr := sleepContext(ctx, time.Hour)
	if !errors.Is(canceledErr, context.Canceled) {
		t.Fatalf("err=%v", canceledErr)
	}
}

// TestNewPacerJitterWithinBounds 验证生产节拍器的抖动落在上限内。
func TestNewPacerJitterWithinBounds(t *testing.T) {
	// pacer 是生产节拍器。
	pacer := NewPacer()
	// round 是采样序号。
	for round := 0; round < 100; round++ {
		// jitter 是本次采样的抖动值。
		jitter := pacer.jitter()
		if jitter < 0 || jitter >= pacerJitterMax {
			t.Fatalf("抖动越界: %v", jitter)
		}
	}
}

// TestPacerAccountKey 验证账号键从 Cookie 的 unb 提取，缺失时落入匿名键。
func TestPacerAccountKey(t *testing.T) {
	// cases 把 Cookie 头映射到期望的账号键。
	cases := map[string]string{"a=1; unb=42; b=2": "42", "a=1; unb=": "", "": ""}
	// cookie、want 分别是 Cookie 头和期望账号键。
	for cookie, want := range cases {
		// header 携带待解析的 Cookie。
		header := http.Header{}
		header.Set("Cookie", cookie)
		// got 是实际解析出的账号键。
		if got := pacerAccountKey(header); got != want {
			t.Fatalf("cookie=%q got %q want %q", cookie, got, want)
		}
	}
}

// TestIsLoopbackHost 验证回环与远端主机的判定。
func TestIsLoopbackHost(t *testing.T) {
	// cases 把主机映射到是否回环。
	cases := map[string]bool{"127.0.0.1:8080": true, "localhost:1": true, "localhost": true, "[::1]:80": true, "h5api.m.goofish.com": false, "h5api.m.goofish.com:443": false}
	// host、want 分别是待判定主机和期望结果。
	for host, want := range cases {
		// got 是实际判定结果。
		if got := isLoopbackHost(host); got != want {
			t.Fatalf("%s: got %v want %v", host, got, want)
		}
	}
}

// roundTripperFunc 把函数适配为 http.RoundTripper。
type roundTripperFunc func(*http.Request) (*http.Response, error)

// RoundTrip 调用函数本身。
func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// roundTripVia 通过 pacingTransport 发送一次请求，下层传输直接返回 200，不会真正联网。
func roundTripVia(pacer *Pacer, url string, ctx context.Context) error {
	// req 携带账号 unb，用于节拍键。
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Cookie", "unb=7")
	// transport 是被测的限速传输。
	transport := pacingTransport{base: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: http.NoBody, Request: r}, nil
	}), pacer: pacer}
	// resp、err 保存传输结果。
	resp, err := transport.RoundTrip(req)
	if resp != nil {
		resp.Body.Close()
	}
	return err
}

// TestPacingTransport 验证传输层按账号限速、后台请求使用更长间隔、回环地址跳过、取消时不发请求。
func TestPacingTransport(t *testing.T) {
	// server 是本地回环服务，仅用于获得回环地址。
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	// now、slept 是固定时钟与等待记录。
	now, slept := time.Unix(1000, 0), []time.Duration{}
	// pacer 是确定性节拍器。
	pacer := newTestPacer(&now, &slept)
	// remote 是非回环平台地址。
	const remote = "https://h5api.m.goofish.com/x"
	// ctxs 依次是默认、后台、默认请求上下文。
	ctxs := []context.Context{context.Background(), WithBackgroundPacing(context.Background()), context.Background()}
	// ctx 遍历三次请求的上下文。
	for _, ctx := range ctxs {
		// sendErr 保存本次请求错误。
		sendErr := roundTripVia(pacer, remote, ctx)
		if sendErr != nil {
			t.Fatal(sendErr)
		}
	}
	// 第二次等待默认间隔，第三次等待“默认 + 后台”两个间隔之和。
	if len(slept) != 3 || slept[1] != DefaultPacerGap || slept[2] != DefaultPacerGap+BackgroundPacerGap {
		t.Fatalf("slept=%v", slept)
	}
	// loopSlept 记录回环节拍器的等待。
	loopSlept := []time.Duration{}
	// loop 是开启回环跳过的节拍器。
	loop := newTestPacer(&now, &loopSlept)
	loop.skipLoopback = true
	// loopErr 保存回环请求错误。
	loopErr := roundTripVia(loop, server.URL, context.Background())
	if loopErr != nil || len(loopSlept) != 0 || len(loop.next) != 0 {
		t.Fatalf("回环请求不应进入节拍器: err=%v", loopErr)
	}
	// cancelled 的睡眠总是返回取消，请求不应发出。
	cancelled := newTestPacer(&now, &loopSlept)
	cancelled.sleep = func(context.Context, time.Duration) error { return context.Canceled }
	// cancelErr 保存取消时的错误。
	cancelErr := roundTripVia(cancelled, remote, context.Background())
	if !errors.Is(cancelErr, context.Canceled) {
		t.Fatalf("err=%v", cancelErr)
	}
	// nilErr 保存空节拍器透传时的错误。
	nilErr := roundTripVia(nil, remote, context.Background())
	if nilErr != nil {
		t.Fatal(nilErr)
	}
}

// TestHTTPClientUsesSharedPacer 验证客户端出站传输默认经过共享节拍器，可被注入替换，测试替身传输不限速。
func TestHTTPClientUsesSharedPacer(t *testing.T) {
	// custom 是注入的节拍器。
	custom := NewPacer()
	// injected 是注入节拍器的客户端传输。
	injected := (&ClientImpl{Pacer: custom}).httpClient().Transport.(pacingTransport)
	// shared 是默认配置的客户端传输。
	shared := (&ClientImpl{}).httpClient().Transport.(pacingTransport)
	if injected.pacer != custom || shared.pacer != defaultPacer {
		t.Fatal("节拍器选择不符合预期")
	}
	// _, paced 判断测试替身传输是否被限速包装。
	_, paced := (&ClientImpl{HTTPClient: &http.Client{}}).httpClient().Transport.(pacingTransport)
	if paced {
		t.Fatal("测试替身传输不应限速")
	}
}

// TestProductionNeverInjectsHTTPClient 守护限速不被绕开：生产代码不得在 mtop 包外构造 ClientImpl 字面量。
func TestProductionNeverInjectsHTTPClient(t *testing.T) {
	// root 是需要扫描的仓库目录。
	for _, root := range []string{"../../../internal", "../../../cmd"} {
		// walkErr 保存目录遍历错误。
		walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			if strings.Contains(filepath.ToSlash(path), "/internal/xianyu/mtop/") {
				return nil
			}
			// raw、readErr 保存源码内容与读取错误。
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			if strings.Contains(string(raw), "mtop.ClientImpl{") {
				t.Errorf("%s 直接构造 mtop.ClientImpl 字面量，必须使用 mtop.NewClient()", path)
			}
			return nil
		})
		if walkErr != nil {
			t.Fatal(walkErr)
		}
	}
}
