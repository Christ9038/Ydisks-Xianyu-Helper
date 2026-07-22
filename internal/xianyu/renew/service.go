// Package renew 实现闲鱼登录 Cookie 续期链路。
//
// 这层只负责低成本 HTTP 续期：hasLogin.do -> silentHasLogin.do ->
// setLoginSettings.do。浏览器续期和密码登录属于更重的恢复手段，应该由上层在
// 本包返回失败后再决定是否执行。
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
	"xianyu-go/internal/xianyu/protocol"
)

const (
	HasLoginURL           = "https://passport.goofish.com/newlogin/hasLogin.do"
	SilentHasLoginURL     = "https://passport.goofish.com/newlogin/silentHasLogin.do"
	SetLoginSettingsURL   = "https://passport.goofish.com/ac/account/setLoginSettings.do"
	QueryLoginSettingsURL = "https://passport.goofish.com/ac/account/queryLoginSettings.do"
	defaultRequestTimout  = 2 * time.Second
	maxRenewBodyBytes     = 2 << 20
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
}

type LongLoginSettings struct {
	CanOpenLongLogin bool     `json:"can_open_long_login"`
	Enabled          bool     `json:"enabled"`
	NewCookies       string   `json:"-"`
	SetCookies       []string `json:"-"`
}

// QueryLongLoginSettings 对齐官网个人信息弹窗的“保存登录信息”查询。
func (s Service) QueryLongLoginSettings(ctx context.Context, cookiesStr string) (*LongLoginSettings, error) {
	return s.longLoginRequest(ctx, cookiesStr, nil)
}

// SetLongLoginSettings 中 status=0 表示开启，status=1 表示关闭。
func (s Service) SetLongLoginSettings(ctx context.Context, cookiesStr string, enabled bool) (*LongLoginSettings, error) {
	status := "1"
	if enabled {
		status = "0"
	}
	return s.longLoginRequest(ctx, cookiesStr, &status)
}

func (s Service) longLoginRequest(ctx context.Context, cookiesStr string, status *string) (*LongLoginSettings, error) {
	target := s.urlOrDefault(s.QueryLoginSettingsURL, QueryLoginSettingsURL)
	var body io.Reader
	if status != nil {
		target = s.urlOrDefault(s.SetLoginSettingsURL, SetLoginSettingsURL)
		body = strings.NewReader("status=" + url.QueryEscape(*status))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, body)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Set("fromSite", "77")
	q.Set("appName", "xianyu")
	q.Set("bizEntrance", "web")
	req.URL.RawQuery = q.Encode()
	setSilentHasLoginHeaders(req, cookiesStr, s.documentReferer())
	if status != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	hc := s.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: defaultRequestTimout}
	}
	requestCtx, cancel := context.WithTimeout(req.Context(), defaultRequestTimout)
	defer cancel()
	resp, err := hc.Do(req.Clone(requestCtx))
	if err != nil {
		return nil, fmt.Errorf("保存登录信息请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := readRenewBody(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("保存登录信息 HTTP 状态异常: %d", resp.StatusCode)
	}
	value, ok := findReturnValue(raw)
	if !ok {
		return nil, fmt.Errorf("保存登录信息响应缺少 returnValue")
	}
	canOpen, _ := value["canOpenLongLogin"].(bool)
	enabledValue, hasEnabled := value["hasLongTokenLogin"].(bool)
	if status != nil && !hasEnabled {
		enabledValue = *status == "0"
	}
	setCookies := filterValidSetCookies(resp.Header.Values("Set-Cookie"))
	return &LongLoginSettings{
		CanOpenLongLogin: canOpen,
		Enabled:          enabledValue,
		NewCookies:       MergeSetCookies(cookiesStr, setCookies),
		SetCookies:       setCookies,
	}, nil
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
	Success            bool
	Skipped            bool
	SkipReason         string
	RenewMethod        string
	NewCookies         string
	UpdatedCookieNames []string
	SetCookies         []string
	StepDetails        []StepResult
	Message            string
	ResponseText       string
	NeedPasswordLogin  bool
	RequestCount       int
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
func (s Service) RenewAPIFirst(ctx context.Context, cookiesStr string) (*Result, error) {
	cookiesStr = strings.TrimSpace(cookiesStr)
	if cookiesStr == "" {
		return &Result{RenewMethod: "none", Message: "Cookie为空，无法续期", NeedPasswordLogin: true}, nil
	}
	mode, skipReason := autoLoginMode(protocol.TransCookies(cookiesStr), time.Now())
	if skipReason != "" {
		return &Result{
			Skipped:     true,
			SkipReason:  skipReason,
			RenewMethod: "auto_login_plugin",
			NewCookies:  cookiesStr,
			Message:     autoLoginSkipMessage(skipReason),
		}, nil
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
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	call, err := s.callAutoLogin(ctx, cookiesStr, mode)
	if err != nil {
		return nil, err
	}
	newCookies := cookiesStr
	if len(call.SetCookies) > 0 {
		newCookies = MergeSetCookies(cookiesStr, call.SetCookies)
	}
	message := "静默续期成功"
	if !call.Step.BusinessOK {
		message = firstNonEmpty(call.Step.Message, "静默续期未通过")
	}
	return &Result{
		Success:            call.Step.BusinessOK,
		RenewMethod:        "auto_login_plugin",
		NewCookies:         newCookies,
		UpdatedCookieNames: ChangedCookieNames(cookiesStr, newCookies),
		SetCookies:         append([]string(nil), call.SetCookies...),
		StepDetails:        []StepResult{call.Step},
		Message:            message,
		ResponseText:       string(call.Body),
		NeedPasswordLogin:  !call.Step.BusinessOK,
		RequestCount:       1,
	}, nil
}

func autoLoginMode(cookies map[string]string, now time.Time) (mode, skipReason string) {
	if cookieTimeAfter(cookies["sdkSilent"], now) {
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
}

func (s Service) callAutoLogin(ctx context.Context, cookiesStr, mode string) (callResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.urlOrDefault(s.SilentHasLoginURL, SilentHasLoginURL), nil)
	if err != nil {
		return callResult{}, err
	}
	q := req.URL.Query()
	q.Set("documentReferer", s.documentReferer())
	q.Set("appName", "xianyu")
	q.Set("appEntrance", "xianyu_sdkSilent")
	q.Set("fromSite", "0")
	switch mode {
	case autoLoginModeHavana:
		q.Set("ltl", "true")
	case autoLoginModeCookie3:
		q.Set("skipSessionFilter", "true")
		q.Set("c2r", "true")
	default:
		return callResult{}, fmt.Errorf("未知静默续期模式: %s", mode)
	}
	req.URL.RawQuery = q.Encode()
	setSilentHasLoginHeaders(req, cookiesStr, s.documentReferer())
	return s.doRenewRequest(req, "silentHasLogin")
}

func (s Service) doRenewRequest(req *http.Request, name string) (callResult, error) {
	hc := s.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: defaultRequestTimout}
	}
	requestCtx, cancel := context.WithTimeout(req.Context(), defaultRequestTimout)
	defer cancel()
	req = req.Clone(requestCtx)
	resp, err := hc.Do(req)
	if err != nil {
		return callResult{}, fmt.Errorf("%s 请求失败: %w", name, err)
	}
	defer resp.Body.Close()
	body, err := readRenewBody(resp.Body)
	if err != nil {
		return callResult{}, fmt.Errorf("%s 响应读取失败: %w", name, err)
	}
	setCookies := filterValidSetCookies(resp.Header.Values("Set-Cookie"))
	step := StepResult{
		Name:           name,
		HTTPStatus:     resp.StatusCode,
		SetCookieCount: len(setCookies),
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		step.Message = fmt.Sprintf("HTTP状态异常: %d", resp.StatusCode)
		return callResult{Step: step, SetCookies: setCookies, Body: body}, nil
	}
	step.BusinessOK = renewBusinessOK(body)
	if step.BusinessOK {
		step.Message = "业务成功"
	} else {
		step.Message = "业务结果未确认成功"
	}
	return callResult{Step: step, SetCookies: setCookies, Body: body}, nil
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
		if finished && mtopInt(data["resultCode"]) == 100 {
			return true
		}
	}
	return false
}

func mtopInt(v any) int {
	switch value := v.(type) {
	case float64:
		return int(value)
	case json.Number:
		n, _ := strconv.Atoi(value.String())
		return n
	case string:
		n, _ := strconv.Atoi(value)
		return n
	default:
		return 0
	}
}

func setSilentHasLoginHeaders(req *http.Request, cookiesStr, documentReferer string) {
	xianyu.ApplyBrowserFingerprint(req.Header)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Priority", "u=1, i")
	req.Header.Set("Origin", "https://www.goofish.com")
	req.Header.Set("Referer", documentReferer)
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "same-site")
	req.Header.Set("Cookie", strings.ReplaceAll(strings.ReplaceAll(cookiesStr, "\n", ""), "\r", ""))
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
		if parsed.MaxAge < 0 || (!parsed.Expires.IsZero() && !parsed.Expires.After(time.Now())) {
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
