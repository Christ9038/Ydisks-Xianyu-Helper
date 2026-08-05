// Package renew 实现闲鱼登录 Cookie 续期与“保存登录信息”接口。
// 主动续期严格复用 auto-login plugin 的单次 silentHasLogin 流程；长登录开关
// 独立执行 setLoginSettings -> queryLoginSettings，不混入主动续期链。
package renew

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/xianyu"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/protocol"
)

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

type LongLoginSettings struct {
	CanOpenLongLogin       bool                          `json:"can_open_long_login"`
	Enabled                bool                          `json:"enabled"`
	NewCookies             string                        `json:"-"`
	SetCookies             []string                      `json:"-"`
	CookieSnapshot         []cookierefresh.BrowserCookie `json:"-"`
	CookieSnapshotComplete bool                          `json:"-"`
}

type longLoginCookieState struct {
	flat          string
	snapshot      []cookierefresh.BrowserCookie
	authoritative bool
}

func newLongLoginCookieState(cookiesStr string, snapshots [][]cookierefresh.BrowserCookie) *longLoginCookieState {
	state := &longLoginCookieState{flat: cookiesStr, authoritative: len(snapshots) > 0}
	if state.authoritative {
		state.snapshot = cookierefresh.NormalizeSnapshot(snapshots[0])
		if state.snapshot == nil {
			state.snapshot = []cookierefresh.BrowserCookie{}
		}
		state.refreshCanonical()
	}
	return state
}

func (state *longLoginCookieState) requestCookies(requestURL string) string {
	if state == nil || !state.authoritative {
		if state == nil {
			return ""
		}
		return state.flat
	}
	value, _ := cookierefresh.ScopedCookieHeaderForRequest(state.snapshot, requestURL, goofishTopSite, time.Now())
	return value
}

func (state *longLoginCookieState) apply(requestURL string, setCookies []string) {
	if state == nil {
		return
	}
	if state.authoritative {
		state.snapshot = cookierefresh.ApplySetCookies(state.snapshot, requestURL, setCookies, time.Now(), goofishTopSite)
		if state.snapshot == nil {
			state.snapshot = []cookierefresh.BrowserCookie{}
		}
		state.refreshCanonical()
		return
	}
	state.flat = MergeSetCookies(state.flat, setCookies)
}

func (state *longLoginCookieState) refreshCanonical() {
	state.flat, _ = cookierefresh.ScopedCookieHeaderForRequest(state.snapshot, goofishIMDocumentURL, goofishTopSite, time.Now())
}

func (state *longLoginCookieState) populate(result *LongLoginSettings) {
	if state == nil || result == nil {
		return
	}
	result.NewCookies = state.flat
	result.CookieSnapshotComplete = state.authoritative
	if state.authoritative {
		result.CookieSnapshot = cookierefresh.NormalizeSnapshot(state.snapshot)
		if result.CookieSnapshot == nil {
			result.CookieSnapshot = []cookierefresh.BrowserCookie{}
		}
	}
}

// QueryLongLoginSettings 对齐官网个人信息弹窗的“保存登录信息”查询。
func (s Service) QueryLongLoginSettings(ctx context.Context, cookiesStr string, snapshots ...[]cookierefresh.BrowserCookie) (*LongLoginSettings, error) {
	return s.longLoginRequest(ctx, newLongLoginCookieState(cookiesStr, snapshots), nil)
}

// SetLongLoginSettings 中 status=0 表示开启，status=1 表示关闭。
func (s Service) SetLongLoginSettings(ctx context.Context, cookiesStr string, enabled bool, snapshots ...[]cookierefresh.BrowserCookie) (*LongLoginSettings, error) {
	status := "1"
	if enabled {
		status = "0"
	}
	state := newLongLoginCookieState(cookiesStr, snapshots)
	setResult, err := s.longLoginRequest(ctx, state, &status)
	if err != nil {
		return setResult, err
	}
	// 官网 SET 成功后触发 LONG_LOGIN_SWITCH，再通过 QUERY 获取最终状态；SET
	// 本身只要求 data.success，不要求携带 returnValue。
	queried, err := s.longLoginRequest(ctx, state, nil)
	if queried == nil {
		queried = &LongLoginSettings{
			Enabled: enabled,
		}
		state.populate(queried)
	}
	queried.SetCookies = append(append([]string(nil), setResult.SetCookies...), queried.SetCookies...)
	if err != nil {
		// QUERY 失败时仍返回 SET 请求及 QUERY 响应头已经刷新的 Cookie；最终
		// 开关状态尚未确认，只能保留调用方本次请求的目标值。
		queried.Enabled = enabled
		return queried, err
	}
	return queried, nil
}

func (s Service) longLoginRequest(ctx context.Context, cookieState *longLoginCookieState, status *string) (*LongLoginSettings, error) {
	partial := &LongLoginSettings{}
	cookieState.populate(partial)
	if status != nil {
		partial.Enabled = *status == "0"
	}
	cookieURL := QueryLoginSettingsURL
	target := s.urlOrDefault(s.QueryLoginSettingsURL, QueryLoginSettingsURL)
	var body io.Reader
	if status != nil {
		cookieURL = SetLoginSettingsURL
		target = s.urlOrDefault(s.SetLoginSettingsURL, SetLoginSettingsURL)
		body = strings.NewReader("status=" + url.QueryEscape(*status))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, body)
	if err != nil {
		return partial, err
	}
	appendOrderedQuery(req.URL, [][2]string{{"fromSite", "77"}, {"appName", "xianyu"}, {"bizEntrance", "web"}})
	setSilentHasLoginHeaders(req, cookieState.requestCookies(cookieURL), s.documentReferer())
	if status != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	hc := s.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: longLoginRequestTimout}
	}
	requestCtx, cancel := context.WithTimeout(req.Context(), longLoginRequestTimout)
	defer cancel()
	resp, err := hc.Do(req.Clone(requestCtx))
	if err != nil {
		return partial, fmt.Errorf("保存登录信息请求失败: %w", err)
	}
	defer resp.Body.Close()
	// 浏览器在响应头到达时就会更新 Cookie Jar；因此必须先捕获 Set-Cookie，
	// 即使随后读取响应体、校验 HTTP 状态或解析业务 JSON 失败也要返回给调用方。
	partial.SetCookies = filterValidSetCookies(resp.Header.Values("Set-Cookie"))
	cookieState.apply(cookieURL, partial.SetCookies)
	cookieState.populate(partial)
	raw, err := readRenewBody(resp.Body)
	if err != nil {
		return partial, fmt.Errorf("保存登录信息响应读取失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return partial, fmt.Errorf("保存登录信息 HTTP 状态异常: %d", resp.StatusCode)
	}
	if status != nil {
		if !longLoginSetBusinessOK(raw) {
			return partial, fmt.Errorf("保存登录信息业务结果未确认成功")
		}
		return partial, nil
	}
	value, ok := findReturnValue(raw)
	if !ok {
		return partial, fmt.Errorf("保存登录信息响应缺少 returnValue")
	}
	partial.CanOpenLongLogin, _ = value["canOpenLongLogin"].(bool)
	partial.Enabled, _ = value["hasLongTokenLogin"].(bool)
	return partial, nil
}

func longLoginSetBusinessOK(raw []byte) bool {
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return false
	}
	if success, _ := payload["success"].(bool); success {
		return true
	}
	if data, _ := payload["data"].(map[string]any); data != nil {
		success, _ := data["success"].(bool)
		return success
	}
	return false
}

func findReturnValue(raw []byte) (map[string]any, bool) {
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return nil, false
	}
	return findMapChild(payload, "returnValue", 0)
}

func findMapChild(parent map[string]any, key string, depth int) (map[string]any, bool) {
	if parent == nil || depth > 6 {
		return nil, false
	}
	if value, ok := parent[key].(map[string]any); ok {
		return value, true
	}
	for _, child := range parent {
		if nested, ok := child.(map[string]any); ok {
			if value, found := findMapChild(nested, key, depth+1); found {
				return value, true
			}
		}
	}
	return nil, false
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
	pending                <-chan pendingRenewResult
	responseCookieURL      string
}

type pendingRenewResult struct {
	result *Result
	err    error
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
	case outcome, ok := <-r.pending:
		if !ok {
			return nil, nil
		}
		return outcome.result, outcome.err
	}
}

// RebaseResponseCookies 把续期响应的 Set-Cookie 应用到“当前最新”凭证状态。
// 后台 fetch 可能晚于其他 MTOP 请求返回，必须重放响应头而不是覆盖成请求发起
// 时计算出的旧快照。
func RebaseResponseCookies(currentCookies, currentMetadata string, result *Result) (string, string, bool) {
	if result == nil || len(result.SetCookies) == 0 {
		return currentCookies, currentMetadata, false
	}
	requestURL := strings.TrimSpace(result.responseCookieURL)
	if requestURL == "" {
		requestURL = SilentHasLoginURL
	}
	if snapshot, complete := cookierefresh.SnapshotFromMetadataOK(currentMetadata); complete {
		updated := cookierefresh.ApplySetCookies(snapshot, requestURL, result.SetCookies, time.Now(), goofishTopSite)
		if updated == nil {
			updated = []cookierefresh.BrowserCookie{}
		}
		value, _ := cookierefresh.ScopedCookieHeaderForRequest(updated, goofishIMDocumentURL, goofishTopSite, time.Now())
		metadata := cookierefresh.MetadataWithSnapshot(currentMetadata, updated)
		return value, metadata, value != currentCookies || metadata != currentMetadata
	}
	value := MergeSetCookies(currentCookies, result.SetCookies)
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
}

const (
	autoLoginModeHavana  = "havana"
	autoLoginModeCookie3 = "cookie3"
)

// RenewAPIFirst mirrors goofish-auto-login/plugin.js. The web client first
// honors the sdkSilent fatigue cookie, chooses the still-valid long-login
// branch, waits briefly, and sends exactly one silentHasLogin request.
// It never chains hasLogin/setLoginSettings or escalates to an interactive
// login from this proactive renewal path.
func (s Service) RenewAPIFirst(ctx context.Context, cookiesStr string, snapshots ...[]cookierefresh.BrowserCookie) (*Result, error) {
	return s.renewAPIFirst(ctx, false, cookiesStr, snapshots...)
}

// RenewAfterSessionExpired 用于服务端已经明确返回 SESSION_EXPIRED 的恢复路径。
// sdkSilent 只用于限制健康账号的主动静默续期；它不能阻止一次已经被业务接口
// 证实为失效的凭证恢复。长登录凭证本身过期时仍拒绝请求并要求重新扫码。
func (s Service) RenewAfterSessionExpired(ctx context.Context, cookiesStr string, snapshots ...[]cookierefresh.BrowserCookie) (*Result, error) {
	return s.renewAPIFirst(ctx, true, cookiesStr, snapshots...)
}

func (s Service) renewAPIFirst(ctx context.Context, sessionExpired bool, cookiesStr string, snapshots ...[]cookierefresh.BrowserCookie) (*Result, error) {
	cookiesStr = strings.TrimSpace(cookiesStr)
	authoritativeSnapshot := len(snapshots) > 0 && snapshots[0] != nil
	documentURL := s.documentReferer()
	requestURL := s.urlOrDefault(s.SilentHasLoginURL, SilentHasLoginURL)
	now := time.Now()
	decisionCookies := cookiesStr
	requestCookies := cookiesStr
	newCookies := cookiesStr
	var snapshot []cookierefresh.BrowserCookie
	if authoritativeSnapshot {
		snapshot = cookierefresh.NormalizeSnapshot(snapshots[0])
		// havana_lgc_exp 由官网以 HttpOnly Cookie 下发。续期服务保存的是
		// 浏览器完整 Cookie Jar，不能按 document.cookie 过滤，否则刚登录的
		// 有效长登录凭证会被误判为不存在。
		if scoped, authoritative := cookierefresh.ScopedCookieHeaderForRequest(snapshot, documentURL, goofishTopSite, now); authoritative {
			decisionCookies = scoped
		}
		if scoped, authoritative := cookierefresh.ScopedCookieHeaderForRequest(snapshot, requestURL, goofishTopSite, now); authoritative {
			requestCookies = scoped
		}
		if scoped, authoritative := cookierefresh.ScopedCookieHeaderForRequest(snapshot, goofishIMDocumentURL, goofishTopSite, now); authoritative {
			newCookies = scoped
		}
	}
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
	mode, skipReason := autoLoginMode(firstCookieValues(decisionCookies), now)
	if sessionExpired && skipReason == "fatigue" {
		// 业务接口已明确证明 Session 失效，忽略主动续期疲劳标记，但仍只调用
		// 一次 silentHasLogin，且继续要求长登录凭证有效。
		mode, skipReason = autoLoginModeWithoutFatigue(firstCookieValues(decisionCookies), now)
	}
	if skipReason != "" {
		result.Skipped = true
		result.SkipReason = skipReason
		result.Message = autoLoginSkipMessage(skipReason)
		return result, nil
	}
	delay := 2 * time.Second
	if s.RetryDelay < 0 {
		delay = 0
	} else if s.RetryDelay > 0 {
		delay = s.RetryDelay
	}
	if delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			result.Message = ctx.Err().Error()
			result.NeedPasswordLogin = true
			return result, ctx.Err()
		case <-timer.C:
		}
	}
	call, err := s.callAutoLogin(ctx, requestCookies, mode)
	populate := func(target *Result, finished callResult, callErr error, promiseTimedOut bool) (*Result, error) {
		target.RequestCount = 1
		target.SetCookies = append([]string(nil), finished.SetCookies...)
		target.ResponseText = string(finished.Body)
		if strings.TrimSpace(finished.Step.Name) != "" {
			target.StepDetails = []StepResult{finished.Step}
		}
		if authoritativeSnapshot {
			updatedSnapshot := cookierefresh.ApplySetCookies(snapshot, requestURL, finished.SetCookies, time.Now(), goofishTopSite)
			target.CookieSnapshot = updatedSnapshot
			target.CookieSnapshotComplete = true
			if scoped, authoritative := cookierefresh.ScopedCookieHeaderForRequest(updatedSnapshot, goofishIMDocumentURL, goofishTopSite, time.Now()); authoritative {
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
		pending := make(chan pendingRenewResult, 1)
		result.pending = pending
		go func() {
			outcome, ok := <-call.pending
			if !ok {
				close(pending)
				return
			}
			late := &Result{
				RenewMethod:       "auto_login_plugin",
				NewCookies:        newCookies,
				responseCookieURL: requestURL,
			}
			if authoritativeSnapshot {
				late.CookieSnapshot = append([]cookierefresh.BrowserCookie(nil), snapshot...)
				late.CookieSnapshotComplete = true
			}
			// Promise 超时只描述前端等待窗口；底层响应到达后必须按真实
			// HTTP/业务结果生成终态，不能永久标记为失败。
			late, lateErr := populate(late, outcome.call, outcome.err, false)
			pending <- pendingRenewResult{result: late, err: lateErr}
			close(pending)
		}()
		return result, nil
	}
	return populate(result, call, err, false)
}

func autoLoginModeWithoutFatigue(cookies map[string]string, now time.Time) (mode, skipReason string) {
	if cookieTimeAfter(cookies["havana_lgc_exp"], now) {
		return autoLoginModeHavana, ""
	}
	if cookieTimeAfter(cookies["cookie3_bak_exp"], now) {
		return autoLoginModeCookie3, ""
	}
	return "", "long_login_expired"
}

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

// firstCookieValues 对齐浏览器 document.cookie getter：同名 Cookie 按浏览器
// header 顺序读取首个值。ScopedCookieHeaderForRequest 已把更长 Path 排在前面。
func firstCookieValues(cookieHeader string) map[string]string {
	values := make(map[string]string)
	for _, part := range strings.Split(cookieHeader, ";") {
		name, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			continue
		}
		if _, exists := values[name]; exists {
			continue
		}
		values[name] = strings.TrimSpace(value)
	}
	return values
}

func strictCookieTimeAfter(raw string, now time.Time) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	millis, err := strconv.ParseInt(raw, 10, 64)
	return err == nil && millis > now.UnixMilli()
}

func cookieTimeAfter(raw string, now time.Time) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	millis, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		// plugin.js 使用 Invalid Date <= now；结果为 false，因此非空异常值
		// 会继续进入对应续期分支。
		return true
	}
	return millis > now.UnixMilli()
}

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

type callResult struct {
	Step       StepResult
	SetCookies []string
	Body       []byte
	pending    <-chan callOutcome
}

type callOutcome struct {
	call callResult
	err  error
}

func (s Service) callAutoLogin(ctx context.Context, cookiesStr, mode string) (callResult, error) {
	partial := callResult{Step: StepResult{Name: "silentHasLogin"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.urlOrDefault(s.SilentHasLoginURL, SilentHasLoginURL), nil)
	if err != nil {
		return partial, err
	}
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
	return s.doRenewRequest(req, "silentHasLogin")
}

func (s Service) doRenewRequest(req *http.Request, name string) (callResult, error) {
	hc := s.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: backgroundFetchTimeout}
	}
	// Promise.race 的计时器不能取消底层 fetch。使用 WithoutCancel 让 2 秒
	// 窗口结束后请求继续，但仍以 30 秒硬上限防止后台泄漏。
	requestCtx, cancel := context.WithTimeout(context.WithoutCancel(req.Context()), backgroundFetchTimeout)
	backgroundReq := req.Clone(requestCtx)
	done := make(chan callOutcome, 1)
	go func() {
		defer cancel()
		call, err := executeRenewRequest(hc, backgroundReq, name)
		done <- callOutcome{call: call, err: err}
	}()
	timer := time.NewTimer(s.promiseTimeout())
	defer timer.Stop()
	select {
	case <-req.Context().Done():
		cancel()
		result := callResult{Step: StepResult{Name: name, Message: req.Context().Err().Error()}}
		return result, req.Context().Err()
	case outcome := <-done:
		return outcome.call, outcome.err
	case <-timer.C:
		return callResult{
			Step:    StepResult{Name: name, Message: "官网静默续期 Promise 已超时；底层 fetch 继续接收 Cookie"},
			pending: done,
		}, nil
	}
}

func (s Service) promiseTimeout() time.Duration {
	if s.PromiseTimeout > 0 {
		return s.PromiseTimeout
	}
	return defaultRequestTimout
}

func executeRenewRequest(hc *http.Client, req *http.Request, name string) (callResult, error) {
	result := callResult{Step: StepResult{Name: name}}
	resp, err := hc.Do(req)
	if err != nil {
		result.Step.Message = fmt.Sprintf("请求失败: %v", err)
		return result, fmt.Errorf("%s 请求失败: %w", name, err)
	}
	defer resp.Body.Close()
	// 与浏览器 Cookie Jar 一样，在响应头到达后立即接收 Set-Cookie。后续响应体
	// 读取失败也不能丢弃服务端已经完成的凭证轮换。
	result.SetCookies = filterValidSetCookies(resp.Header.Values("Set-Cookie"))
	result.Step.HTTPStatus = resp.StatusCode
	result.Step.SetCookieCount = len(result.SetCookies)
	body, err := readRenewBody(resp.Body)
	if err != nil {
		result.Step.Message = fmt.Sprintf("响应读取失败: %v", err)
		return result, fmt.Errorf("%s 响应读取失败: %w", name, err)
	}
	result.Body = body
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Step.Message = fmt.Sprintf("HTTP状态异常: %d", resp.StatusCode)
		return result, nil
	}
	result.Step.BusinessOK = renewBusinessOK(body)
	if result.Step.BusinessOK {
		result.Step.Message = "业务成功"
	} else {
		result.Step.Message = "业务结果未确认成功"
	}
	return result, nil
}

func renewBusinessOK(body []byte) bool {
	if len(body) == 0 {
		return false
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return false
	}
	content, _ := payload["content"].(map[string]any)
	if data, _ := payload["data"].(map[string]any); data != nil {
		if nested, _ := data["content"].(map[string]any); nested != nil {
			content = nested
		}
	}
	if content == nil {
		return false
	}
	data, _ := content["data"].(map[string]any)
	if data != nil {
		finished, _ := data["processFinished"].(bool)
		if finished && numericResultCode(data["resultCode"]) == 100 {
			return true
		}
	}
	return false
}

func numericResultCode(v any) int {
	switch value := v.(type) {
	case float64:
		return int(value)
	case json.Number:
		n, _ := strconv.Atoi(value.String())
		return n
	case int:
		return value
	default:
		return 0
	}
}

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

func appendOrderedQuery(target *url.URL, values [][2]string) {
	parts := make([]string, 0, len(values)+1)
	if strings.TrimSpace(target.RawQuery) != "" {
		parts = append(parts, target.RawQuery)
	}
	for _, item := range values {
		parts = append(parts, url.QueryEscape(item[0])+"="+url.QueryEscape(item[1]))
	}
	target.RawQuery = strings.Join(parts, "&")
}

func (s Service) urlOrDefault(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

func (s Service) documentReferer() string {
	if strings.TrimSpace(s.DocumentReferer) != "" {
		return strings.TrimSpace(s.DocumentReferer)
	}
	return "https://www.goofish.com/im"
}

func readRenewBody(r io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r, maxRenewBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxRenewBodyBytes {
		return nil, fmt.Errorf("续期响应体超过 %d MiB", maxRenewBodyBytes>>20)
	}
	return body, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func filterValidSetCookies(setCookies []string) []string {
	if len(setCookies) == 0 {
		return nil
	}
	out := make([]string, 0, len(setCookies))
	for _, sc := range setCookies {
		if strings.TrimSpace(sc) == "" {
			continue
		}
		out = append(out, sc)
	}
	return out
}

// MergeSetCookies 将 Set-Cookie 头合并到 Cookie 头字符串。只保留 name=value，
// 忽略 Path/Domain/Expires 等属性，因为后续出站请求只需要 Cookie header。
func MergeSetCookies(original string, setCookies []string) string {
	cookies := protocol.TransCookies(original)
	for _, sc := range setCookies {
		parsed, err := http.ParseSetCookie(sc)
		if err != nil || strings.TrimSpace(parsed.Name) == "" {
			continue
		}
		if parsed.MaxAge < 0 || (parsed.MaxAge == 0 && !parsed.Expires.IsZero() && !parsed.Expires.After(time.Now())) {
			delete(cookies, parsed.Name)
		} else {
			cookies[parsed.Name] = parsed.Value
		}
	}
	return marshalCookies(cookies)
}

// ChangedCookieNames 返回 newCookies 相对 original 变化过的字段名，按字典序排序。
func ChangedCookieNames(original, newCookies string) []string {
	oldMap := protocol.TransCookies(original)
	newMap := protocol.TransCookies(newCookies)
	changed := make([]string, 0)
	seen := make(map[string]struct{}, len(oldMap)+len(newMap))
	for k := range oldMap {
		seen[k] = struct{}{}
	}
	for k := range newMap {
		seen[k] = struct{}{}
	}
	for k := range seen {
		if oldMap[k] != newMap[k] {
			changed = append(changed, k)
		}
	}
	sort.Strings(changed)
	return changed
}

func marshalCookies(cookies map[string]string) string {
	keys := make([]string, 0, len(cookies))
	for k := range cookies {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+cookies[k])
	}
	return strings.Join(parts, "; ")
}
