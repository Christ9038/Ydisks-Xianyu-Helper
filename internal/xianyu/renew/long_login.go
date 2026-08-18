package renew

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"xianyu-go/internal/xianyu/cookierefresh"
)

// longLoginCookieState 用于本次流程后续判断的long登录登录凭证状态
type longLoginCookieState struct {
	flat          string
	snapshot      []cookierefresh.BrowserCookie
	authoritative bool
}

// newLongLoginCookieState 封装newLong登录登录凭证状态业务协调。
func newLongLoginCookieState(cookiesStr string, snapshots [][]cookierefresh.BrowserCookie) *longLoginCookieState {
	// state 用于本次流程后续判断的状态
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

// requestCookies 封装请求Cookies业务协调。
func (state *longLoginCookieState) requestCookies(requestURL string) string {
	if state == nil || !state.authoritative {
		if state == nil {
			return ""
		}
		return state.flat
	}
	// value 用于本次流程后续判断的值
	value, _ := cookierefresh.ScopedCookieHeaderForRequest(state.snapshot, requestURL, goofishTopSite, time.Now())
	return value
}

// apply 封装apply业务协调。
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

// refreshCanonical 封装refreshCanonical业务协调。
func (state *longLoginCookieState) refreshCanonical() {
	state.flat, _ = cookierefresh.ScopedCookieHeaderForRequest(state.snapshot, goofishIMDocumentURL, goofishTopSite, time.Now())
}

// populate 封装populate业务协调。
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
	// status 用于本次流程后续判断的状态
	status := "1"
	if enabled {
		status = "0"
	}
	// state 用于本次流程后续判断的状态
	state := newLongLoginCookieState(cookiesStr, snapshots)
	// setResult、err 用于本次流程后续判断的setResult、err
	setResult, err := s.longLoginRequest(ctx, state, &status)
	if err != nil {
		return setResult, err
	}
	// 官网 SET 成功后触发 LONG_LOGIN_SWITCH，再通过 QUERY 获取最终状态；SET
	// 本身只要求 data.success，不要求携带 returnValue。
	// queried、err 用于本次流程后续判断的queried、err
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

// longLoginRequest 封装long登录请求业务协调。
func (s Service) longLoginRequest(ctx context.Context, cookieState *longLoginCookieState, status *string) (*LongLoginSettings, error) {
	// partial 用于本次流程后续判断的partial
	partial := &LongLoginSettings{}
	cookieState.populate(partial)
	if status != nil {
		partial.Enabled = *status == "0"
	}
	// cookieURL 用于本次流程后续判断的登录凭证URL
	cookieURL := QueryLoginSettingsURL
	// target 用于本次流程后续判断的target
	target := s.urlOrDefault(s.QueryLoginSettingsURL, QueryLoginSettingsURL)
	// body 用于本次流程后续判断的请求体
	var body io.Reader
	if status != nil {
		cookieURL = SetLoginSettingsURL
		target = s.urlOrDefault(s.SetLoginSettingsURL, SetLoginSettingsURL)
		body = strings.NewReader("status=" + url.QueryEscape(*status))
	}
	// req、err 用于本次流程后续判断的req、err
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, body)
	if err != nil {
		return partial, err
	}
	appendOrderedQuery(req.URL, [][2]string{{"fromSite", "77"}, {"appName", "xianyu"}, {"bizEntrance", "web"}})
	setSilentHasLoginHeaders(req, cookieState.requestCookies(cookieURL), s.documentReferer())
	if status != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	// hc 用于本次流程后续判断的hc
	hc := s.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: longLoginRequestTimout}
	}
	// requestCtx、cancel 用于本次流程后续判断的请求Ctx、cancel
	requestCtx, cancel := context.WithTimeout(req.Context(), longLoginRequestTimout)
	defer cancel()
	// resp、err 用于本次流程后续判断的resp、err
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
	// raw、err 用于本次流程后续判断的raw、err
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
	// value、ok 用于本次流程后续判断的value、ok
	value, ok := findReturnValue(raw)
	if !ok {
		return partial, fmt.Errorf("保存登录信息响应缺少 returnValue")
	}
	partial.CanOpenLongLogin, _ = value["canOpenLongLogin"].(bool)
	partial.Enabled, _ = value["hasLongTokenLogin"].(bool)
	return partial, nil
}

// longLoginSetBusinessOK 封装long登录SetBusinessOK业务协调。
func longLoginSetBusinessOK(raw []byte) bool {
	// payload 用于本次流程后续判断的请求载荷
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return false
	}
	if // success 用于本次流程后续判断的success
	success, _ := payload["success"].(bool); success {
		return true
	}
	if // data 用于本次流程后续判断的数据
	data, _ := payload["data"].(map[string]any); data != nil {
		// success 用于本次流程后续判断的success
		success, _ := data["success"].(bool)
		return success
	}
	return false
}

// findReturnValue 封装findReturn值业务协调。
func findReturnValue(raw []byte) (map[string]any, bool) {
	// payload 用于本次流程后续判断的请求载荷
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil {
		return nil, false
	}
	return findMapChild(payload, "returnValue", 0)
}

// findMapChild 封装findMapChild业务协调。
func findMapChild(parent map[string]any, key string, depth int) (map[string]any, bool) {
	if parent == nil || depth > 6 {
		return nil, false
	}
	if // value、ok 用于本次流程后续判断的value、ok
	value, ok := parent[key].(map[string]any); ok {
		return value, true
	}
	// child 表示当前遍历过程中的child
	for _, child := range parent {
		if // nested、ok 用于本次流程后续判断的nested、ok
		nested, ok := child.(map[string]any); ok {
			if // value、found 用于本次流程后续判断的value、found
			value, found := findMapChild(nested, key, depth+1); found {
				return value, true
			}
		}
	}
	return nil, false
}
