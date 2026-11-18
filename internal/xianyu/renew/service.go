// Package renew 实现闲鱼登录 Cookie 续期与“保存登录信息”接口。
// 主动续期严格复用 auto-login plugin 的单次 silentHasLogin 流程；长登录开关
// 独立执行 setLoginSettings -> queryLoginSettings，不混入主动续期链。
package renew

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/xianyu"
	"xianyu-go/internal/xianyu/cookierefresh"
)

// HasLoginURL 用于本次流程后续判断的Has登录URL
const (
	HasLoginURL            = "https://passport.goofish.com/newlogin/hasLogin.do"
	SilentHasLoginURL      = "https://passport.goofish.com/newlogin/silentHasLogin.do"
	SetLoginSettingsURL    = "https://passport.goofish.com/ac/account/setLoginSettings.do"
	QueryLoginSettingsURL  = "https://passport.goofish.com/ac/account/queryLoginSettings.do"
	defaultRequestTimout   = 2 * time.Second
	backgroundFetchTimeout = 30 * time.Second
	longLoginRequestTimout = 30 * time.Second
	maxRenewBodyBytes      = 2 << 20
	goofishTopSite         = "https://goofish.com"
	goofishIMDocumentURL   = "https://www.goofish.com/im"
)

// Service 是 Cookie 接口续期服务。零值可用；测试可覆盖 URL 和 HTTPClient。
type Service struct {
	HTTPClient            *http.Client
	HasLoginURL           string
	SilentHasLoginURL     string
	QueryLoginSettingsURL string
	SetLoginSettingsURL   string
	DocumentReferer       string
	RetryDelay            time.Duration
	// PromiseTimeout 仅供测试缩短官网固定的 2 秒 Promise.race 窗口；
	// 生产零值始终使用 defaultRequestTimout。
	PromiseTimeout time.Duration
}

// LongLoginSettings 用于本次流程后续判断的Long登录设置
type LongLoginSettings struct {
	CanOpenLongLogin       bool                          `json:"can_open_long_login"`
	Enabled                bool                          `json:"enabled"`
	NewCookies             string                        `json:"-"`
	SetCookies             []string                      `json:"-"`
	CookieSnapshot         []cookierefresh.BrowserCookie `json:"-"`
	CookieSnapshotComplete bool                          `json:"-"`
}

// Result 描述一次接口续期的完整结果。
type Result struct {
	Success                bool
	Skipped                bool
	SkipReason             string
	RenewMethod            string
	NewCookies             string
	UpdatedCookieNames     []string
	SetCookies             []string
	CookieSnapshot         []cookierefresh.BrowserCookie
	CookieSnapshotComplete bool
	StepDetails            []StepResult
	Message                string
	ResponseText           string
	NeedPasswordLogin      bool
	RequestCount           int
	// VerificationRequired 表示平台返回 iframeRedirect=true，续期停在需要人工完成的安全验证；
	// 此时 Success 仍为 false，响应 Cookie 照常接收。
	VerificationRequired bool
	// verificationURL 是平台给出的一次性验证地址，仅允许通过 VerificationURL 读取并放入内存登记，
	// 不得出现在日志、通知或持久化字段中。
	verificationURL   string
	pending           <-chan pendingRenewResult
	responseCookieURL string
	// responseReceivedAt 固定响应头的接收时刻，迟到正文或持久化重放不得延长 Max-Age。
	responseReceivedAt time.Time
	// responseCookies 保留重定向响应链的精确作用域，不能只保存最终响应头。
	responseCookies []responseCookieBatch
}

// pendingRenewResult 用于本次流程后续判断的pendingRenew结果
type pendingRenewResult struct {
	result *Result
	err    error
}

// VerificationURL 返回 r 记录的安全验证跳转地址；没有验证要求或 r 为空时返回空串。
// 返回值带一次性令牌，调用方只能交给内存登记表，禁止写入日志、通知或数据库。
func (r *Result) VerificationURL() string {
	if r == nil {
		return ""
	}
	return r.verificationURL
}

// HasPending 表示官网 2 秒 Promise 已结束，但底层 fetch 仍在接收响应。
func (r *Result) HasPending() bool { return r != nil && r.pending != nil }

// AwaitPending 等待底层 fetch 的最终响应。只能由一个持久化调用方消费一次。
func (r *Result) AwaitPending(ctx context.Context) (*Result, error) {
	if r == nil || r.pending == nil {
		return nil, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case // outcome、ok 用于本次流程后续判断的outcome、ok
	outcome, ok := <-r.pending:
		if !ok {
			return nil, nil
		}
		return outcome.result, outcome.err
	}
}

// RebaseResponseCookies 把续期响应的 Set-Cookie 应用到“当前最新”凭证状态。
// 后台 fetch 可能晚于其他 MTOP 请求返回，必须重放响应头而不是覆盖成请求发起
// 时计算出的旧快照。
// RebaseResponseCookies 封装Rebase响应Cookies业务协调。
func RebaseResponseCookies(currentCookies, currentMetadata string, result *Result) (string, string, bool) {
	if result == nil || len(result.SetCookies) == 0 {
		return currentCookies, currentMetadata, false
	}
	if // snapshot、complete 用于本次流程后续判断的snapshot、complete
	snapshot, complete := cookierefresh.SnapshotFromMetadataOK(currentMetadata); complete {
		// updated 用于本次流程后续判断的updated
		updated := result.applyResponseCookies(snapshot)
		if updated == nil {
			updated = []cookierefresh.BrowserCookie{}
		}
		// value 用于本次流程后续判断的值
		value, _ := cookierefresh.ScopedCookieHeaderForRequest(updated, goofishIMDocumentURL, goofishTopSite, time.Now())
		// metadata 用于本次流程后续判断的metadata
		metadata := cookierefresh.MetadataWithSnapshot(currentMetadata, updated)
		return value, metadata, value != currentCookies || metadata != currentMetadata
	}
	// value 用于本次流程后续判断的值
	value := MergeSetCookies(currentCookies, result.SetCookies)
	// metadata 用于本次流程后续判断的metadata
	metadata := cookierefresh.MetadataWithoutSnapshot(currentMetadata)
	return value, metadata, value != currentCookies || metadata != currentMetadata
}

// StepResult 是单个续期接口的执行结果，便于上层记录日志和定位失败点。
type StepResult struct {
	Name           string
	HTTPStatus     int
	BusinessOK     bool
	SetCookieCount int
	Message        string
	// VerificationRequired 表示本步骤响应为 iframeRedirect 中间状态，需要用户人工完成安全验证。
	VerificationRequired bool
}

// autoLoginModeHavana 用于本次流程后续判断的auto登录模式Havana
const (
	autoLoginModeHavana  = "havana"
	autoLoginModeCookie3 = "cookie3"
)

// RenewAPIFirst 使用 s 的请求配置对 cookiesStr 或权威 snapshots 执行主动静默续期。
// 先检查 sdkSilent，再等待两秒并按当时时钟选择长登录分支，只发起一次 silentHasLogin。
// ctx 约束等待与调用方生命周期；返回结果、可等待的迟到 Cookie 及网络错误，不串联交互登录。
func (s Service) RenewAPIFirst(ctx context.Context, cookiesStr string, snapshots ...[]cookierefresh.BrowserCookie) (*Result, error) {
	return s.renewAPIFirst(ctx, false, cookiesStr, snapshots...)
}

// RenewAfterSessionExpired 用于服务端已经明确返回 SESSION_EXPIRED 的恢复路径。
// sdkSilent 只用于限制健康账号的主动静默续期；它不能阻止一次已经被业务接口
// 证实为失效的凭证恢复。长登录凭证本身过期时仍拒绝请求并要求重新扫码。
// RenewAfterSessionExpired 封装RenewAfter会话Expired业务协调。
func (s Service) RenewAfterSessionExpired(ctx context.Context, cookiesStr string, snapshots ...[]cookierefresh.BrowserCookie) (*Result, error) {
	return s.renewAPIFirst(ctx, true, cookiesStr, snapshots...)
}

// renewAPIFirst 编排 s 的一次官方续期：cookiesStr 是降级明文请求头，snapshots 是可选权威 Jar。
// sessionExpired 只为用户保留的立即恢复需求绕过疲劳窗口；ctx 取消可中止等待。
// 返回 SDK 成功/跳过/超时语义和响应 Cookie，错误不代表可以丢弃已收到的 Cookie。
func (s Service) renewAPIFirst(ctx context.Context, sessionExpired bool, cookiesStr string, snapshots ...[]cookierefresh.BrowserCookie) (*Result, error) {
	cookiesStr = strings.TrimSpace(cookiesStr)
	// authoritativeSnapshot 用于本次流程后续判断的authoritativeSnapshot
	authoritativeSnapshot := len(snapshots) > 0 && snapshots[0] != nil
	// documentURL 用于本次流程后续判断的documentURL
	documentURL := s.documentReferer()
	// requestURL 用于本次流程后续判断的请求URL
	requestURL := s.urlOrDefault(s.SilentHasLoginURL, SilentHasLoginURL)
	// now 用于本次流程后续判断的now
	now := time.Now()
	// decisionCookies 用于本次流程后续判断的decisionCookies
	decisionCookies := cookiesStr
	// requestCookies 用于本次流程后续判断的请求Cookies
	requestCookies := cookiesStr
	// newCookies 用于本次流程后续判断的newCookies
	newCookies := cookiesStr
	// snapshot 用于本次流程后续判断的snapshot
	var snapshot []cookierefresh.BrowserCookie
	if authoritativeSnapshot {
		snapshot = cookierefresh.NormalizeSnapshot(snapshots[0])
		// visibleSnapshot 仅供 SDK 的 document.cookie 分支判断；HttpOnly 凭证
		// 仍保留在完整 snapshot 中，允许符合作用域的 HTTP 请求携带。
		visibleSnapshot := make([]cookierefresh.BrowserCookie, 0, len(snapshot))
		// cookie 是当前检查脚本可见性的 Cookie，不输出明文值。
		for _, cookie := range snapshot {
			if !cookie.HTTPOnly {
				visibleSnapshot = append(visibleSnapshot, cookie)
			}
		}
		if // scoped、authoritative 用于本次流程后续判断的scoped、authoritative
		scoped, authoritative := cookierefresh.ScopedCookieHeaderForRequest(visibleSnapshot, documentURL, goofishTopSite, now); authoritative {
			decisionCookies = scoped
		}
		if // scoped、authoritative 用于本次流程后续判断的scoped、authoritative
		scoped, authoritative := cookierefresh.ScopedCookieHeaderForRequest(snapshot, requestURL, goofishTopSite, now); authoritative {
			requestCookies = scoped
		}
		if // scoped、authoritative 用于本次流程后续判断的scoped、authoritative
		scoped, authoritative := cookierefresh.ScopedCookieHeaderForRequest(snapshot, goofishIMDocumentURL, goofishTopSite, now); authoritative {
			newCookies = scoped
		}
	}
	// result 用于本次流程后续判断的结果
	result := &Result{
		RenewMethod:       "auto_login_plugin",
		NewCookies:        newCookies,
		responseCookieURL: requestURL,
	}
	if authoritativeSnapshot {
		result.CookieSnapshot = make([]cookierefresh.BrowserCookie, len(snapshot))
		copy(result.CookieSnapshot, snapshot)
		result.CookieSnapshotComplete = true
	}
	if cookiesStr == "" && !authoritativeSnapshot {
		result.RenewMethod = "none"
		result.Message = "Cookie为空，无法续期"
		result.NeedPasswordLogin = true
		return result, nil
	}
	// decisionValues 保存 SDK 在等待前读取一次的脚本可见 Cookie；后续不重读。
	decisionValues := autoLoginCookieValues(decisionCookies)
	if !sessionExpired && strictCookieTimeAfter(decisionValues["sdkSilent"], now) {
		result.Skipped = true
		result.SkipReason = "fatigue"
		result.Message = autoLoginSkipMessage(result.SkipReason)
		return result, nil
	}
	// waitErr 保存官网请求前延迟被调用方取消的原因。
	if waitErr := s.waitAutoLoginDelay(ctx); waitErr != nil {
		result.Message = waitErr.Error()
		result.NeedPasswordLogin = true
		return result, waitErr
	}
	// mode、skipReason 按等待结束时的时钟判断原始 Cookie 值，避免跨过期边界后
	// 仍选择 Havana。立即恢复仅绕过疲劳窗口，不绕过长登录到期判断。
	mode, skipReason := autoLoginModeWithoutFatigue(decisionValues, time.Now())
	if skipReason != "" {
		result.Skipped = true
		result.SkipReason = skipReason
		result.Message = autoLoginSkipMessage(skipReason)
		return result, nil
	}
	if authoritativeSnapshot {
		// 请求发送时重新筛除已过期 Cookie，但不改变等待前捕获的 SDK 判断值。
		requestCookies, _ = cookierefresh.ScopedCookieHeaderForRequest(snapshot, requestURL, goofishTopSite, time.Now())
	}
	// call、err 用于本次流程后续判断的call、err
	call, err := s.callAutoLogin(ctx, requestCookies, mode, snapshot)
	// populate 用于本次流程后续判断的populate
	populate := func(target *Result, finished callResult, callErr error, promiseTimedOut bool) (*Result, error) {
		target.RequestCount = 1
		target.SetCookies = append([]string(nil), finished.SetCookies...)
		target.responseReceivedAt = finished.receivedAt
		target.responseCookies = finished.responses
		target.ResponseText = string(finished.Body)
		target.VerificationRequired = finished.Step.VerificationRequired
		target.verificationURL = finished.verificationURL
		if strings.TrimSpace(finished.Step.Name) != "" {
			target.StepDetails = []StepResult{finished.Step}
		}
		if authoritativeSnapshot {
			// updatedSnapshot 用于本次流程后续判断的updatedSnapshot
			updatedSnapshot := target.applyResponseCookies(snapshot)
			target.CookieSnapshot = updatedSnapshot
			target.CookieSnapshotComplete = true
			if // scoped、authoritative 用于本次流程后续判断的scoped、authoritative
			scoped, authoritative := cookierefresh.ScopedCookieHeaderForRequest(updatedSnapshot, goofishIMDocumentURL, goofishTopSite, time.Now()); authoritative {
				target.NewCookies = scoped
			}
			target.UpdatedCookieNames = cookierefresh.ChangedSnapshotLabels(snapshot, updatedSnapshot)
		} else {
			target.NewCookies = MergeSetCookies(cookiesStr, finished.SetCookies)
			target.UpdatedCookieNames = ChangedCookieNames(cookiesStr, target.NewCookies)
		}
		if promiseTimedOut {
			target.Success = false
			target.NeedPasswordLogin = false
			target.Message = "官网静默续期 Promise 已超时"
			if len(finished.SetCookies) > 0 {
				target.Message += "；底层响应 Cookie 已接收"
			}
			return target, callErr
		}
		if callErr != nil {
			target.Message = callErr.Error()
			target.NeedPasswordLogin = true
			return target, callErr
		}
		// message 用于本次流程后续判断的消息
		message := "静默续期成功"
		if !finished.Step.BusinessOK {
			message = firstNonEmpty(finished.Step.Message, "静默续期未通过")
		}
		target.Success = finished.Step.BusinessOK
		target.Message = message
		target.NeedPasswordLogin = !finished.Step.BusinessOK
		return target, nil
	}
	if call.pending != nil {
		result.RequestCount = 1
		result.StepDetails = []StepResult{call.Step}
		result.Message = call.Step.Message
		result.NeedPasswordLogin = false
		// pending 用于本次流程后续判断的pending
		pending := make(chan pendingRenewResult, 1)
		result.pending = pending
		go func() {
			// outcome、ok 用于本次流程后续判断的outcome、ok
			outcome, ok := <-call.pending
			if !ok {
				close(pending)
				return
			}
			// late 用于本次流程后续判断的late
			late := &Result{
				RenewMethod:       "auto_login_plugin",
				NewCookies:        newCookies,
				responseCookieURL: requestURL,
			}
			if authoritativeSnapshot {
				late.CookieSnapshot = append([]cookierefresh.BrowserCookie(nil), snapshot...)
				late.CookieSnapshotComplete = true
			}
			// late、lateErr 接收迟到 Cookie，但 Promise 一旦超时就不能重新
			// resolve；上层不得据此模拟官网 reload 或宣布本轮续期成功。
			late, lateErr := populate(late, outcome.call, outcome.err, true)
			pending <- pendingRenewResult{result: late, err: lateErr}
			close(pending)
		}()
		return result, nil
	}
	return populate(result, call, err, false)
}

// autoLoginModeWithoutFatigue 封装auto登录模式WithoutFatigue业务协调。
func autoLoginModeWithoutFatigue(cookies map[string]string, now time.Time) (mode, skipReason string) {
	if cookieTimeAfter(cookies["havana_lgc_exp"], now) {
		return autoLoginModeHavana, ""
	}
	if cookieTimeAfter(cookies["cookie3_bak_exp"], now) {
		return autoLoginModeCookie3, ""
	}
	return "", "long_login_expired"
}

// autoLoginMode 封装auto登录模式业务协调。
func autoLoginMode(cookies map[string]string, now time.Time) (mode, skipReason string) {
	if strictCookieTimeAfter(cookies["sdkSilent"], now) {
		return "", "fatigue"
	}
	if cookieTimeAfter(cookies["havana_lgc_exp"], now) {
		return autoLoginModeHavana, ""
	}
	if cookieTimeAfter(cookies["cookie3_bak_exp"], now) {
		return autoLoginModeCookie3, ""
	}
	return "", "long_login_expired"
}

// autoLoginSkipMessage 封装auto登录Skip消息业务协调。
func autoLoginSkipMessage(reason string) string {
	switch reason {
	case "fatigue":
		return "sdkSilent 疲劳窗口内，跳过静默续期"
	case "long_login_expired":
		return "长登录凭证已过期，静默续期不应发起请求"
	default:
		return "无需静默续期"
	}
}

// callResult 用于本次流程后续判断的call结果
type callResult struct {
	Step       StepResult
	SetCookies []string
	Body       []byte
	// verificationURL 是响应中的一次性安全验证地址，只在内存中向上传递。
	verificationURL string
	pending         <-chan callOutcome
	// receivedAt 是响应头接收时间，仅用于响应 Cookie 的有效期计算。
	receivedAt time.Time
	// responses 是包括重定向在内的完整 Cookie 响应链。
	responses []responseCookieBatch
}

// callOutcome 用于本次流程后续判断的callOutcome
type callOutcome struct {
	call callResult
	err  error
}

// callAutoLogin 按 mode 选择官方查询参数，以 s 的配置和 ctx 发起单次静默续期。
// cookiesStr 是只在 HTTP 内使用的明文降级凭证，snapshots 为可选权威 Jar；返回响应及请求错误。
func (s Service) callAutoLogin(ctx context.Context, cookiesStr, mode string, snapshots ...[]cookierefresh.BrowserCookie) (callResult, error) {
	// partial 用于本次流程后续判断的partial
	partial := callResult{Step: StepResult{Name: "silentHasLogin"}}
	// req、err 用于本次流程后续判断的req、err
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.urlOrDefault(s.SilentHasLoginURL, SilentHasLoginURL), nil)
	if err != nil {
		return partial, err
	}
	// query 用于本次流程后续判断的查询
	query := [][2]string{
		{"documentReferer", s.documentReferer()},
		{"appName", "xianyu"},
		{"appEntrance", "xianyu_sdkSilent"},
		{"fromSite", "0"},
	}
	switch mode {
	case autoLoginModeHavana:
		query = append(query, [2]string{"ltl", "true"})
	case autoLoginModeCookie3:
		query = append(query, [2]string{"skipSessionFilter", "true"}, [2]string{"c2r", "true"})
	default:
		return partial, fmt.Errorf("未知静默续期模式: %s", mode)
	}
	appendOrderedQuery(req.URL, query)
	setSilentHasLoginHeaders(req, cookiesStr, s.documentReferer())
	return s.doRenewRequest(req, "silentHasLogin", snapshots...)
}

// doRenewRequest 以 s 的网络配置执行 req，name 用作步骤标签，snapshots 用于每跳 Cookie 作用域。
// 本函数拥有请求协程：响应头与两秒 Promise 窗口竞速，正文不在竞速内；请求有三十秒硬上限。
// 窗口超时后返回单消费者 pending 通道，协程通过该通道交付 Cookie 并结束，不改变超时终态。
func (s Service) doRenewRequest(req *http.Request, name string, snapshots ...[]cookierefresh.BrowserCookie) (callResult, error) {
	// hc 用于本次流程后续判断的hc
	hc := s.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: backgroundFetchTimeout}
	}
	// snapshot 为可选的完整 Cookie Jar，仅传给本请求独占的传输状态。
	var snapshot []cookierefresh.BrowserCookie
	if len(snapshots) > 0 {
		snapshot = snapshots[0]
	}
	// client、cookieTransport 使每跳重定向都使用前一响应更新后的 Cookie，不修改共享 hc。
	client, cookieTransport := requestClient(hc, req, snapshot)
	// Promise.race 的计时器不能取消底层 fetch。使用 WithoutCancel 让 2 秒
	// 窗口结束后请求继续，但仍以 30 秒硬上限防止后台泄漏。
	// requestCtx、cancel 用于本次流程后续判断的请求Ctx、cancel
	requestCtx, cancel := context.WithTimeout(context.WithoutCancel(req.Context()), backgroundFetchTimeout)
	// backgroundReq 用于本次流程后续判断的backgroundReq
	backgroundReq := req.Clone(requestCtx)
	// done 用于本次流程后续判断的done
	done := make(chan callOutcome, 1)
	// headersReceived 由请求协程在响应头到达时关闭；Promise 只与此阶段竞速。
	headersReceived := make(chan struct{})
	// signalHeaders 是请求协程持有的固定发送端，不随选择器禁用接收而改变。
	signalHeaders := headersReceived
	// 请求协程由本次续期拥有，硬超时或调用方取消终止，通过 done 交付唯一结果。
	go func() {
		defer cancel()
		// call、err 用于本次流程后续判断的call、err
		call, err := executeRenewRequest(client, backgroundReq, name, signalHeaders)
		call.responses = cookieTransport.responses
		call.SetCookies = nil
		// batch 是本次请求链中的各跳响应；旧字段保留合并后的响应头供兼容调用方使用。
		for _, batch := range call.responses {
			call.SetCookies = append(call.SetCookies, batch.headers...)
		}
		call.Step.SetCookieCount = len(call.SetCookies)
		done <- callOutcome{call: call, err: err}
	}()
	// timer 用于本次流程后续判断的定时器
	timer := time.NewTimer(s.promiseTimeout())
	defer timer.Stop()
	// timeout 只在等待响应头时启用；收到头后等待正文，不再受 Promise 计时器影响。
	timeout := timer.C
	for {
		select {
		case <-req.Context().Done():
			cancel()
			// result 用于本次流程后续判断的结果
			result := callResult{Step: StepResult{Name: name, Message: req.Context().Err().Error()}}
			return result, req.Context().Err()
		case // outcome 用于本次流程后续判断的outcome
		outcome := <-done:
			return outcome.call, outcome.err
		case <-headersReceived:
			headersReceived = nil
			timeout = nil
		case <-timeout:
			// 响应头已先到但本协程尚未被调度时，仍按 fetch 已兑现处理。
			select {
			case <-headersReceived:
				headersReceived = nil
				timeout = nil
				continue
			default:
			}
			return callResult{
				Step:    StepResult{Name: name, Message: "官网静默续期 Promise 已超时；底层 fetch 继续接收 Cookie"},
				pending: done,
			}, nil
		}
	}
}

// promiseTimeout 封装promiseTimeout业务协调。
func (s Service) promiseTimeout() time.Duration {
	if s.PromiseTimeout > 0 {
		return s.PromiseTimeout
	}
	return defaultRequestTimout
}

// executeRenewRequest 使用 hc 执行 req，按 name 标记结果，并在响应头到达时关闭
// headersReceived。返回响应 Cookie、正文及读取错误；正文读取受 req 的硬超时限制。
func executeRenewRequest(hc *http.Client, req *http.Request, name string, headersReceived chan<- struct{}) (callResult, error) {
	// result 用于本次流程后续判断的结果
	result := callResult{Step: StepResult{Name: name}}
	// resp、err 用于本次流程后续判断的resp、err
	resp, err := hc.Do(req)
	if err != nil {
		result.Step.Message = fmt.Sprintf("请求失败: %v", err)
		return result, fmt.Errorf("%s 请求失败: %w", name, err)
	}
	defer resp.Body.Close()
	result.receivedAt = time.Now()
	close(headersReceived)
	// 与浏览器 Cookie Jar 一样，在响应头到达后立即接收 Set-Cookie。后续响应体
	// 读取失败也不能丢弃服务端已经完成的凭证轮换。
	result.SetCookies = filterValidSetCookies(resp.Header.Values("Set-Cookie"))
	result.Step.HTTPStatus = resp.StatusCode
	result.Step.SetCookieCount = len(result.SetCookies)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Step.Message = fmt.Sprintf("HTTP状态异常: %d", resp.StatusCode)
		return result, nil
	}
	// body、err 用于本次流程后续判断的body、err
	body, err := readRenewBody(resp.Body)
	if err != nil {
		result.Step.Message = fmt.Sprintf("响应读取失败: %v", err)
		return result, fmt.Errorf("%s 响应读取失败: %w", name, err)
	}
	result.Body = body
	result.Step.BusinessOK = renewBusinessOK(body)
	switch {
	case result.Step.BusinessOK:
		result.Step.Message = "业务成功"
	default:
		// verificationURL、required 表示响应是否为 iframeRedirect 中间状态；它仍不是成功。
		verificationURL, required := renewVerificationURL(body)
		if required {
			result.Step.VerificationRequired = true
			result.verificationURL = verificationURL
			result.Step.Message = "平台要求安全验证，需人工完成"
			// 响应正文会进入续期日志，验证地址带一次性令牌，必须先脱敏。
			result.Body = redactVerificationBody(body)
		} else {
			result.Step.Message = "业务结果未确认成功"
		}
	}
	return result, nil
}

// renewResponseData 从 silentHasLogin 响应正文 body 中取出官方插件读取的 content.data 对象。
// 顶层 content 与嵌套 data.content 的优先级遵循官方 JavaScript 真值语义；
// 正文为空、不是 JSON 或缺少 content.data 时返回 nil，调用方一律按“未确认成功”处理。
func renewResponseData(body []byte) map[string]any {
	if len(body) == 0 {
		return nil
	}
	// payload 保存解码后的响应顶层对象。
	var payload map[string]any
	// err 是 json.Unmarshal 返回的失败原因，用于断言成功或被拒绝的路径。
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil
	}
	// content 保存官方插件选定的 content 对象。
	content, _ := payload["content"].(map[string]any)
	if // data 保存嵌套的 data 对象，仅用于判断是否覆盖顶层 content。
	data, _ := payload["data"].(map[string]any); data != nil {
		if javascriptTruthy(data["content"]) {
			content, _ = data["content"].(map[string]any)
		}
	}
	if content == nil {
		return nil
	}
	// data 是 content 下承载业务结果的对象。
	data, _ := content["data"].(map[string]any)
	return data
}

// renewBusinessOK 判断 body 是否满足官方插件的成功条件：processFinished 为布尔 true 且 resultCode 为 100。
func renewBusinessOK(body []byte) bool {
	// data 为响应中的业务结果对象。
	data := renewResponseData(body)
	if data == nil {
		return false
	}
	// finished 表示平台是否声明流程已结束；非布尔类型一律视为未结束。
	finished, _ := data["processFinished"].(bool)
	return finished && numericResultCode(data["resultCode"]) == 100
}

// numericResultCode 从 v 读取整数业务码；小数不能截断为成功码，字符串不隐式转换。
func numericResultCode(v any) int {
	switch // value 用于本次流程后续判断的值
	value := v.(type) {
	case float64:
		if math.Trunc(value) != value || math.IsInf(value, 0) {
			return 0
		}
		return int(value)
	case json.Number:
		// n 用于本次流程后续判断的n
		n, _ := strconv.Atoi(value.String())
		return n
	case int:
		return value
	default:
		return 0
	}
}

// javascriptTruthy 判断 JSON 值 value 的 JavaScript 真值，返回是否应覆盖顶层 content。
func javascriptTruthy(value any) bool {
	// scalar 是当前 JSON 标量；对象和数组即使为空也为真。
	switch scalar := value.(type) {
	case nil:
		return false
	case bool:
		return scalar
	case string:
		return scalar != ""
	case float64:
		return scalar != 0 && !math.IsNaN(scalar)
	default:
		return true
	}
}

// setSilentHasLoginHeaders 封装setSilentHas登录Headers业务协调。
func setSilentHasLoginHeaders(req *http.Request, cookiesStr, documentReferer string) {
	xianyu.ApplyBrowserFingerprint(req.Header)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Origin", "https://www.goofish.com")
	req.Header.Set("Referer", documentReferer)
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "same-site")
	req.Header.Set("Cookie", strings.ReplaceAll(strings.ReplaceAll(cookiesStr, "\n", ""), "\r", ""))
}

// appendOrderedQuery 封装appendOrdered查询业务协调。
func appendOrderedQuery(target *url.URL, values [][2]string) {
	// parts 用于本次流程后续判断的parts
	parts := make([]string, 0, len(values)+1)
	if strings.TrimSpace(target.RawQuery) != "" {
		parts = append(parts, target.RawQuery)
	}
	// item 表示当前遍历过程中的商品
	for _, item := range values {
		parts = append(parts, encodeURIComponent(item[0])+"="+encodeURIComponent(item[1]))
	}
	target.RawQuery = strings.Join(parts, "&")
}

// encodeURIComponent 将 value 编码成官方查询串；空格使用 %20，并保留 JS 的五个额外安全符号。
func encodeURIComponent(value string) string {
	return strings.NewReplacer("+", "%20", "%21", "!", "%27", "'", "%28", "(", "%29", ")", "%2A", "*").Replace(url.QueryEscape(value))
}

// urlOrDefault 封装地址OrDefault业务协调。
func (s Service) urlOrDefault(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

// documentReferer 封装documentReferer业务协调。
func (s Service) documentReferer() string {
	if strings.TrimSpace(s.DocumentReferer) != "" {
		// 官方捕获 location.href.split("?")[0]，查询参数不进入 documentReferer。
		return strings.SplitN(strings.TrimSpace(s.DocumentReferer), "?", 2)[0]
	}
	return "https://www.goofish.com/im"
}

// readRenewBody 封装readRenew请求体业务协调。
func readRenewBody(r io.Reader) ([]byte, error) {
	// body、err 用于本次流程后续判断的body、err
	body, err := io.ReadAll(io.LimitReader(r, maxRenewBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxRenewBodyBytes {
		return nil, fmt.Errorf("续期响应体超过 %d MiB", maxRenewBodyBytes>>20)
	}
	return body, nil
}

// firstNonEmpty 封装firstNonEmpty业务协调。
func firstNonEmpty(values ...string) string {
	// v 表示当前遍历过程中的v
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// filterValidSetCookies 封装filter有效SetCookies业务协调。
func filterValidSetCookies(setCookies []string) []string {
	if len(setCookies) == 0 {
		return nil
	}
	// out 用于本次流程后续判断的out
	out := make([]string, 0, len(setCookies))
	// sc 表示当前遍历过程中的sc
	for _, sc := range setCookies {
		if strings.TrimSpace(sc) == "" {
			continue
		}
		out = append(out, sc)
	}
	return out
}
