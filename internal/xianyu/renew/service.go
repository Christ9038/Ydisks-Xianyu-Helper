// Package renew 实现闲鱼登录 Cookie 续期链路。
//
// 这层只负责低成本 HTTP 续期：hasLogin.do -> silentHasLogin.do ->
// setLoginSettings.do。浏览器续期和密码登录属于更重的恢复手段，应该由上层在
// 本包返回失败后再决定是否执行。
package renew

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/xianyu/protocol"
)

const (
	HasLoginURL          = "https://passport.goofish.com/newlogin/hasLogin.do"
	SilentHasLoginURL    = "https://passport.goofish.com/newlogin/silentHasLogin.do"
	SetLoginSettingsURL  = "https://passport.goofish.com/ac/account/setLoginSettings.do"
	defaultRequestTimout = 20 * time.Second
	maxRenewBodyBytes    = 2 << 20
	renewUA              = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"
	hasLoginSecChUA      = `"Chromium";v="145", "Not:A-Brand";v="99"`
	settingSecChUA       = `"Google Chrome";v="146", "Not=A?Brand";v="8"`
)

// Service 是 Cookie 接口续期服务。零值可用；测试可覆盖 URL 和 HTTPClient。
type Service struct {
	HTTPClient          *http.Client
	HasLoginURL         string
	SilentHasLoginURL   string
	SetLoginSettingsURL string
	RetryDelay          time.Duration
}

// Result 描述一次接口续期的完整结果。
type Result struct {
	Success            bool
	RenewMethod        string
	NewCookies         string
	UpdatedCookieNames []string
	StepDetails        []StepResult
	Message            string
	ResponseText       string
	NeedPasswordLogin  bool
}

// StepResult 是单个续期接口的执行结果，便于上层记录日志和定位失败点。
type StepResult struct {
	Name           string
	HTTPStatus     int
	BusinessOK     bool
	SetCookieCount int
	Message        string
}

// RenewAPIFirst 按对方项目的“接口续期优先”思路执行三段续期。
// 只有 setLoginSettings.do 返回 Set-Cookie，才认为长登录续期真正成功；
// 前两步下发的 Cookie 即使最终失败也会保留在 NewCookies 中，避免丢失服务端刷新字段。
func (s Service) RenewAPIFirst(ctx context.Context, cookiesStr string) (*Result, error) {
	cookiesStr = strings.TrimSpace(cookiesStr)
	if cookiesStr == "" {
		return &Result{RenewMethod: "none", Message: "Cookie为空，无法续期", NeedPasswordLogin: true}, nil
	}
	res, err := s.renewOnce(ctx, cookiesStr)
	if err != nil || res == nil || res.Success {
		return res, err
	}
	if s.RetryDelay < 0 {
		return res, nil
	}
	delay := s.RetryDelay
	if delay == 0 {
		delay = 2 * time.Second
	}
	if delay > 0 {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return res, ctx.Err()
		case <-timer.C:
		}
	}
	retry, err := s.renewOnce(ctx, res.NewCookies)
	if err != nil {
		return res, err
	}
	if retry == nil {
		return res, nil
	}
	retry.StepDetails = append(res.StepDetails, retry.StepDetails...)
	retry.UpdatedCookieNames = ChangedCookieNames(cookiesStr, retry.NewCookies)
	return retry, nil
}

func (s Service) renewOnce(ctx context.Context, original string) (*Result, error) {
	current := original
	steps := make([]StepResult, 0, 3)
	allSetCookies := make([]string, 0, 8)

	hasLogin, err := s.callHasLogin(ctx, current)
	if err != nil {
		return nil, err
	}
	steps = append(steps, hasLogin.Step)
	if len(hasLogin.SetCookies) > 0 {
		allSetCookies = append(allSetCookies, hasLogin.SetCookies...)
		current = MergeSetCookies(current, hasLogin.SetCookies)
	}

	silent, err := s.callSilentHasLogin(ctx, current)
	if err != nil {
		return nil, err
	}
	steps = append(steps, silent.Step)
	if len(silent.SetCookies) > 0 {
		allSetCookies = append(allSetCookies, silent.SetCookies...)
		current = MergeSetCookies(current, silent.SetCookies)
	}

	settings, err := s.callSetLoginSettings(ctx, current)
	if err != nil {
		return nil, err
	}
	steps = append(steps, settings.Step)
	if len(settings.SetCookies) > 0 {
		allSetCookies = append(allSetCookies, settings.SetCookies...)
	}

	newCookies := original
	if len(allSetCookies) > 0 {
		newCookies = MergeSetCookies(original, allSetCookies)
	}
	updated := ChangedCookieNames(original, newCookies)
	success := len(settings.SetCookies) > 0
	method := "none"
	msg := "setLoginSettings 未返回 Set-Cookie，需要浏览器续期或密码登录"
	if success {
		method = "api"
		msg = "接口续期成功"
	}
	return &Result{
		Success:            success,
		RenewMethod:        method,
		NewCookies:         newCookies,
		UpdatedCookieNames: updated,
		StepDetails:        steps,
		Message:            msg,
		ResponseText:       string(silent.Body),
		NeedPasswordLogin:  !success,
	}, nil
}

type callResult struct {
	Step       StepResult
	SetCookies []string
	Body       []byte
}

func (s Service) callHasLogin(ctx context.Context, cookiesStr string) (callResult, error) {
	cookies := protocol.TransCookies(cookiesStr)
	unb := cookies["unb"]
	if unb == "" {
		return callResult{Step: StepResult{Name: "hasLogin", Message: "Cookie缺少unb，跳过"}}, nil
	}
	form := url.Values{}
	form.Set("hid", unb)
	form.Set("ltl", "true")
	form.Set("appName", "xianyu")
	form.Set("appEntrance", "web")
	form.Set("_csrf_token", cookies["_tb_token_"])
	form.Set("umidToken", firstNonEmpty(cookies["_uab_collina"], cookies["cna"]))
	form.Set("hsiz", cookies["cookie2"])
	form.Set("bizParams", "taobaoBizLoginFrom=web&renderRefer=https%3A%2F%2Fwww.goofish.com%2F")
	form.Set("mainPage", "false")
	form.Set("isMobile", "false")
	form.Set("lang", "zh_CN")
	form.Set("fromSite", "77")
	form.Set("isIframe", "true")
	form.Set("documentReferer", "https://www.goofish.com/")
	form.Set("defaultView", "hasLogin")
	form.Set("umidTag", "SERVER")
	form.Set("returnUrl", "")
	form.Set("deviceId", "")
	form.Set("pageTraceId", "21504"+strconv.FormatInt(time.Now().UnixMilli(), 10)+randomDigits(6))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.urlOrDefault(s.HasLoginURL, HasLoginURL), strings.NewReader(form.Encode()))
	if err != nil {
		return callResult{}, err
	}
	setHasLoginHeaders(req, cookiesStr, miniLoginReferer())
	if xsrf := cookies["XSRF-TOKEN"]; xsrf != "" {
		req.Header.Set("x-xsrf-token", xsrf)
	}
	q := req.URL.Query()
	q.Set("appName", "xianyu")
	q.Set("fromSite", "77")
	req.URL.RawQuery = q.Encode()
	return s.doRenewRequest(req, "hasLogin")
}

func (s Service) callSilentHasLogin(ctx context.Context, cookiesStr string) (callResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.urlOrDefault(s.SilentHasLoginURL, SilentHasLoginURL), nil)
	if err != nil {
		return callResult{}, err
	}
	q := req.URL.Query()
	q.Set("documentReferer", "https://www.goofish.com/")
	q.Set("appName", "xianyu")
	q.Set("appEntrance", "xianyu_sdkSilent")
	q.Set("fromSite", "0")
	q.Set("ltl", "true")
	req.URL.RawQuery = q.Encode()
	setSilentHasLoginHeaders(req, cookiesStr)
	return s.doRenewRequest(req, "silentHasLogin")
}

func (s Service) callSetLoginSettings(ctx context.Context, cookiesStr string) (callResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.urlOrDefault(s.SetLoginSettingsURL, SetLoginSettingsURL), strings.NewReader("status=0"))
	if err != nil {
		return callResult{}, err
	}
	q := req.URL.Query()
	q.Set("fromSite", "77")
	q.Set("appName", "xianyu")
	q.Set("bizEntrance", "web")
	req.URL.RawQuery = q.Encode()
	setLoginSettingsHeaders(req, cookiesStr)
	return s.doRenewRequest(req, "setLoginSettings")
}

func (s Service) doRenewRequest(req *http.Request, name string) (callResult, error) {
	hc := s.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: defaultRequestTimout}
	}
	client := *hc
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := client.Do(req)
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
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
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
	if content == nil {
		return false
	}
	ok, _ := content["success"].(bool)
	return ok
}

func setHasLoginHeaders(req *http.Request, cookiesStr, referer string) {
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", referer)
	req.Header.Set("User-Agent", renewUA)
	req.Header.Set("bx-v", "2.5.31")
	req.Header.Set("sec-ch-ua", hasLoginSecChUA)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"Windows"`)
	req.Header.Set("Cookie", strings.ReplaceAll(strings.ReplaceAll(cookiesStr, "\n", ""), "\r", ""))
}

func setSilentHasLoginHeaders(req *http.Request, cookiesStr string) {
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en,zh-CN;q=0.9,zh;q=0.8,ru;q=0.7")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Priority", "u=1, i")
	req.Header.Set("Referer", "https://www.goofish.com/")
	req.Header.Set("User-Agent", renewUA)
	req.Header.Set("sec-ch-ua", `"Google Chrome";v="146", "Not=A?Brand";v="8", "Not/A)Brand";v="146"`)
	req.Header.Set("sec-ch-ua-arch", `"x86"`)
	req.Header.Set("sec-ch-ua-bitness", `"64"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"Win32"`)
	req.Header.Set("sec-ch-ua-platform-version", `"10.0.0"`)
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "same-site")
	req.Header.Set("Cookie", strings.ReplaceAll(strings.ReplaceAll(cookiesStr, "\n", ""), "\r", ""))
}

func setLoginSettingsHeaders(req *http.Request, cookiesStr string) {
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "https://www.goofish.com/")
	req.Header.Set("User-Agent", renewUA)
	req.Header.Set("sec-ch-ua", settingSecChUA)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"Windows"`)
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

func miniLoginReferer() string {
	return "https://passport.goofish.com/mini_login.htm?lang=zh_cn&appName=xianyu&appEntrance=web&styleType=vertical&bizParams=&notLoadSsoView=false&notKeepLogin=false&isMobile=false&qrCodeFirst=false&stie=77&rnd=" + randomFraction()
}

func randomDigits(n int) string {
	if n <= 0 {
		return ""
	}
	min := 1
	for i := 1; i < n; i++ {
		min *= 10
	}
	max := min * 9
	v, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return strings.Repeat("0", n)
	}
	return strconv.Itoa(min + int(v.Int64()))
}

func randomFraction() string {
	v, err := cryptorand.Int(cryptorand.Reader, big.NewInt(1_000_000_000))
	if err != nil {
		return "0.0"
	}
	return "0." + fmt.Sprintf("%09d", v.Int64())
}

func filterValidSetCookies(setCookies []string) []string {
	if len(setCookies) == 0 {
		return nil
	}
	out := make([]string, 0, len(setCookies))
	for _, sc := range setCookies {
		if strings.Contains(sc, "Max-Age=0") || strings.Contains(sc, "1970") {
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
		pair := sc
		if i := strings.Index(pair, ";"); i >= 0 {
			pair = pair[:i]
		}
		name, val, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		cookies[name] = strings.TrimSpace(val)
	}
	return marshalCookies(cookies)
}

// ChangedCookieNames 返回 newCookies 相对 original 变化过的字段名，按字典序排序。
func ChangedCookieNames(original, newCookies string) []string {
	oldMap := protocol.TransCookies(original)
	newMap := protocol.TransCookies(newCookies)
	changed := make([]string, 0)
	for k, v := range newMap {
		if oldMap[k] != v {
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
