// Package mtop: token 域 — mtop.taobao.idlemessage.pc.login.token 调用与重试。
package mtop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/protocol"
)

const officialMTopMaxAttempts = 5

// RefreshToken 调用 mtop.taobao.idlemessage.pc.login.token 获取 accessToken。
// 遇到 mtop 签名 token 过期时，按官网 lib-mtop 2.7.3 的 H5 流程最多执行
// 5 次请求（含首次），每次先吸收 Go Cookie Jar 再重新签名。
func (c *ClientImpl) RefreshToken(cookiesStr string) (*RefreshResult, error) {
	return c.RefreshTokenContext(context.Background(), cookiesStr)
}

// RefreshTokenContext 是支持取消的 RefreshToken 版本。
func (c *ClientImpl) RefreshTokenContext(ctx context.Context, cookiesStr string) (*RefreshResult, error) {
	return c.RefreshTokenWithDeviceIDContext(ctx, cookiesStr, "")
}

// RefreshTokenWithDeviceIDContext 使用指定 deviceId 获取 accessToken。
// 闲鱼 IM token 和 WS /reg 的 did 是绑定校验关系：token 请求里的 deviceId
// 必须与 /reg.headers.did 完全一致，否则 /reg 会返回
// "device id or appkey is not equal"。
func (c *ClientImpl) RefreshTokenWithDeviceIDContext(ctx context.Context, cookiesStr, deviceID string) (*RefreshResult, error) {
	return c.RefreshTokenWithCredentialContext(ctx, cookiesStr, deviceID, nil)
}

// RefreshTokenWithCredentialContext 使用完整 Cookie 快照执行纯 Go HTTP 请求，
// 避免把不同 Domain/Path 的同名 Cookie 压成一个值。
func (c *ClientImpl) RefreshTokenWithCredentialContext(ctx context.Context, cookiesStr, deviceID string, cookieSnapshot []cookierefresh.BrowserCookie) (*RefreshResult, error) {
	currentCookies := cookiesStr
	currentSnapshot := cookierefresh.NormalizeSnapshot(cookieSnapshot)
	currentSnapshotComplete := cookieSnapshot != nil
	cookieStateChanged := false
	if session := cookieSessionFromContext(ctx); session != nil {
		currentCookies, currentSnapshot, cookieStateChanged = session.State()
		currentSnapshotComplete = currentSnapshot != nil
	}
	for attempt := 0; attempt < officialMTopMaxAttempts; attempt++ {
		accessToken, expireAt, ret, updatedCookies, snapshot, verificationURL, status, snapshotComplete, attemptChanged, err := c.refreshTokenOnce(ctx, currentCookies, deviceID, currentSnapshot)
		if updatedCookies != "" || snapshot != nil || attemptChanged {
			currentCookies = updatedCookies
		}
		if snapshotComplete {
			currentSnapshot = snapshot
			currentSnapshotComplete = true
		} else if attemptChanged {
			// 只有扁平更新时无法把变化安全映射回既有 Domain/Path Jar；
			// 必须降级为非权威状态。
			currentSnapshot = nil
			currentSnapshotComplete = false
		}
		cookieStateChanged = cookieStateChanged || attemptChanged
		if err != nil {
			return refreshResultFromContext(ctx, currentCookies, currentSnapshot, currentSnapshotComplete, cookieStateChanged), err
		}
		if accessToken != "" {
			result := refreshResultFromContext(ctx, currentCookies, currentSnapshot, currentSnapshotComplete, cookieStateChanged)
			result.AccessToken = accessToken
			result.AccessTokenExpireAt = expireAt
			return result, nil
		}
		if isRiskVerificationRet(ret) {
			return refreshResultFromContext(ctx, currentCookies, currentSnapshot, currentSnapshotComplete, cookieStateChanged), &RiskVerificationError{Ret: ret, VerificationURL: verificationURL}
		}
		if !isOfficialTokenRetryRet(ret) {
			return refreshResultFromContext(ctx, currentCookies, currentSnapshot, currentSnapshotComplete, cookieStateChanged), fmt.Errorf("token API 返回非成功: ret=%v (status=%d)", ret, status)
		}
		if attempt == officialMTopMaxAttempts-1 {
			var snapshotForClear []cookierefresh.BrowserCookie
			if currentSnapshotComplete {
				snapshotForClear = currentSnapshot
			}
			cleanedCookies, cleanedSnapshot := clearOfficialMTopTokenCookies(currentCookies, snapshotForClear)
			cookieStateChanged = cookieStateChanged || cleanedCookies != currentCookies || !slices.Equal(cleanedSnapshot, currentSnapshot)
			currentCookies, currentSnapshot = cleanedCookies, cleanedSnapshot
			if session := cookieSessionFromContext(ctx); session != nil {
				if currentSnapshot != nil {
					session.replace(currentSnapshot)
				} else {
					session.replaceFlat(currentCookies)
				}
			}
			return refreshResultFromContext(ctx, currentCookies, currentSnapshot, currentSnapshotComplete, cookieStateChanged), fmt.Errorf("token API 登录凭证已失效: ret=%v (status=%d)", ret, status)
		}
	}
	return refreshResultFromContext(ctx, currentCookies, currentSnapshot, currentSnapshotComplete, cookieStateChanged), fmt.Errorf("token API 登录凭证已失效")
}

func refreshResult(updatedCookies string, snapshot []cookierefresh.BrowserCookie, complete, changed bool) *RefreshResult {
	if !complete {
		snapshot = nil
	}
	return &RefreshResult{
		UpdatedCookies:         updatedCookies,
		CookieSnapshot:         snapshot,
		CookieSnapshotComplete: complete,
		CookieStateChanged:     changed,
	}
}

func refreshResultFromContext(ctx context.Context, updatedCookies string, snapshot []cookierefresh.BrowserCookie, complete, changed bool) *RefreshResult {
	if session := cookieSessionFromContext(ctx); session != nil {
		var sessionChanged bool
		updatedCookies, snapshot, sessionChanged = session.State()
		complete = snapshot != nil
		changed = changed || sessionChanged
	}
	return refreshResult(updatedCookies, snapshot, complete, changed)
}

// RequestFreshCaptchaURLContext 重新请求 token API，用于浏览器风控验证前获取新鲜验证链接。
// 如果风控已解除并直接返回 accessToken，则 TokenOK=true。
func (c *ClientImpl) RequestFreshCaptchaURLContext(ctx context.Context, cookiesStr, deviceID string) (*FreshCaptchaResult, error) {
	var snapshot []cookierefresh.BrowserCookie
	if session := cookieSessionFromContext(ctx); session != nil {
		cookiesStr, snapshot, _ = session.State()
	}
	accessToken, expireAt, ret, updatedCookies, _, verificationURL, _, _, _, err := c.refreshTokenOnce(ctx, cookiesStr, deviceID, snapshot)
	if session := cookieSessionFromContext(ctx); session != nil {
		updatedCookies, _, _ = session.State()
	}
	if err != nil {
		return &FreshCaptchaResult{UpdatedCookies: updatedCookies}, err
	}
	if accessToken != "" {
		return &FreshCaptchaResult{
			TokenOK:             true,
			AccessToken:         accessToken,
			AccessTokenExpireAt: expireAt,
			UpdatedCookies:      updatedCookies,
			Ret:                 ret,
		}, nil
	}
	return &FreshCaptchaResult{
		UpdatedCookies:  updatedCookies,
		VerificationURL: verificationURL,
		Ret:             ret,
	}, nil
}

func (c *ClientImpl) refreshTokenOnce(ctx context.Context, cookiesStr, deviceID string, cookieSnapshot []cookierefresh.BrowserCookie) (string, int64, []string, string, []cookierefresh.BrowserCookie, string, int, bool, bool, error) {
	hc := c.httpClientWithTimeout(20 * time.Second)

	tokenURL := c.TokenURL
	if tokenURL == "" {
		tokenURL = TokenAPI
	}
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookiesStr, mtopDocumentURL, tokenURL)
	if cookieSessionFromContext(ctx) == nil && cookieSnapshot != nil {
		signingCookies, requestCookies = snapshotRequestCookies(cookieSnapshot, cookiesStr, tokenURL)
	}
	myid := protocol.TransCookies(signingCookies)["unb"]
	if myid == "" {
		return "", 0, nil, cookiesStr, nil, "", 0, false, false, fmt.Errorf("cookie 缺少 unb 字段，无法生成 deviceId")
	}
	if strings.TrimSpace(deviceID) == "" {
		deviceID = protocol.GenerateDeviceID(myid)
	}
	token := protocol.SignToken(signingCookies)

	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	dataVal := `{"appKey":"` + RegAppKey + `","deviceId":"` + deviceID + `"}`

	// 签名不覆盖 query，因此 query 的编码细节不影响验签。
	query := buildTokenQuery(t, protocol.GenerateSign(t, token, dataVal))

	body := "data=" + url.QueryEscape(dataVal)

	requestURL := tokenURL + "?" + query
	var raw []byte
	var status int
	var updated string
	var snapshot []cookierefresh.BrowserCookie
	snapshotComplete := false
	stateChanged := false
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, strings.NewReader(body))
	if reqErr != nil {
		return "", 0, nil, cookiesStr, nil, "", 0, false, false, reqErr
	}
	setCommonHeaders(req, requestCookies)
	req.Header.Set("Referer", "https://www.goofish.com/im")
	resp, reqErr := hc.Do(req)
	if reqErr != nil {
		return "", 0, nil, cookiesStr, nil, "", 0, false, false, fmt.Errorf("token API 请求失败: %w", reqErr)
	}
	defer resp.Body.Close()
	// Go CookieSession 在读取响应体前应用 Set-Cookie，避免解析失败时丢掉
	// 服务端已经下发的凭证轮换或删除。
	if session := cookieSessionFromContext(ctx); session != nil {
		updated = absorbMTopResponseCookies(ctx, cookiesStr, resp)
		var sessionChanged bool
		_, snapshot, sessionChanged = session.State()
		stateChanged = sessionChanged
		snapshotComplete = snapshot != nil
	} else if cookieSnapshot != nil {
		snapshot = cookierefresh.ApplySetCookies(cookieSnapshot, requestURL, resp.Header.Values("Set-Cookie"), time.Now(), goofishTopSite)
		updated, _ = cookierefresh.ScopedCookieHeaderForRequest(snapshot, mtopDocumentURL, goofishTopSite, time.Now())
		stateChanged = !slices.Equal(snapshot, cookierefresh.NormalizeSnapshot(cookieSnapshot))
		snapshotComplete = true
	} else {
		updated = absorbMTopResponseCookies(ctx, cookiesStr, resp)
		stateChanged = updated != cookiesStr
	}
	raw, reqErr = readMTopBody(resp)
	if reqErr != nil {
		return "", 0, nil, updated, snapshot, "", resp.StatusCode, snapshotComplete, stateChanged, reqErr
	}
	status = resp.StatusCode

	var res struct {
		Ret  []string `json:"ret"`
		Data struct {
			AccessToken            string          `json:"accessToken"`
			AccessTokenExpiredTime json.RawMessage `json:"accessTokenExpiredTime"`
			URL                    string          `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", 0, nil, updated, snapshot, "", status, snapshotComplete, stateChanged, fmt.Errorf("解析 token 响应失败: %w (body=%s)", err, truncate(string(raw), 300))
	}

	ok := false
	for _, r := range res.Ret {
		if strings.Contains(r, "SUCCESS") {
			ok = true
			break
		}
	}
	if !ok {
		return "", 0, res.Ret, updated, snapshot, res.Data.URL, status, snapshotComplete, stateChanged, nil
	}
	if res.Data.AccessToken == "" {
		return "", 0, res.Ret, updated, snapshot, "", status, snapshotComplete, stateChanged, fmt.Errorf("token API 成功但 accessToken 为空 (body=%s)", truncate(string(raw), 300))
	}
	return res.Data.AccessToken, parseAccessTokenExpireAt(res.Data.AccessTokenExpiredTime, time.Now()), res.Ret, updated, snapshot, "", status, snapshotComplete, stateChanged, nil
}

func snapshotRequestCookies(snapshot []cookierefresh.BrowserCookie, fallback, requestURL string) (string, string) {
	if snapshot == nil {
		return fallback, fallback
	}
	documentCookies := make([]cookierefresh.BrowserCookie, 0, len(snapshot))
	for _, cookie := range snapshot {
		if !cookie.HTTPOnly {
			documentCookies = append(documentCookies, cookie)
		}
	}
	signing, _ := cookierefresh.ScopedCookieHeaderForRequest(documentCookies, mtopDocumentURL, goofishTopSite, time.Now())
	requestCookies, _ := cookierefresh.ScopedCookieHeaderForRequest(snapshot, requestURL, goofishTopSite, time.Now())
	return signing, requestCookies
}

func isOfficialTokenRetryRet(ret []string) bool {
	for _, value := range ret {
		if strings.Contains(value, "TOKEN_EMPTY") || strings.Contains(value, "TOKEN_EXOIRED") {
			return true
		}
	}
	return false
}

func clearOfficialMTopTokenCookies(cookieStr string, snapshot []cookierefresh.BrowserCookie) (string, []cookierefresh.BrowserCookie) {
	values := protocol.TransCookies(cookieStr)
	for _, name := range []string{"_m_h5_c", "_m_h5_tk", "_m_h5_tk_enc"} {
		delete(values, name)
	}
	var cleaned []cookierefresh.BrowserCookie
	if snapshot != nil {
		cleaned = make([]cookierefresh.BrowserCookie, 0, len(snapshot))
	}
	for _, cookie := range snapshot {
		domain := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(cookie.Domain), "."))
		cookiePath := cookie.Path
		if cookiePath == "" {
			cookiePath = "/"
		}
		remove := cookiePath == "/" && domain == "goofish.com" &&
			(cookie.Name == "_m_h5_c" || cookie.Name == "_m_h5_tk" || cookie.Name == "_m_h5_tk_enc")
		remove = remove || (cookiePath == "/" && domain == "m.goofish.com" &&
			(cookie.Name == "_m_h5_tk" || cookie.Name == "_m_h5_tk_enc"))
		if !remove {
			cleaned = append(cleaned, cookie)
		}
	}
	cleaned = cookierefresh.NormalizeSnapshot(cleaned)
	if snapshot != nil {
		// 完整 Jar 存在时，扁平兼容值也必须重新按 /im scope 生成；不能用
		// name map 把仍有效的 Path=/im 同名凭证一并抹掉。
		canonical, _ := cookierefresh.ScopedCookieHeaderForRequest(cleaned, mtopDocumentURL, goofishTopSite, time.Now())
		return canonical, cleaned
	}
	return marshalTokenCookies(values), cleaned
}

func marshalTokenCookies(cookies map[string]string) string {
	keys := make([]string, 0, len(cookies))
	for key := range cookies {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+cookies[key])
	}
	return strings.Join(parts, "; ")
}

func parseAccessTokenExpireAt(raw json.RawMessage, now time.Time) int64 {
	value := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if value == "" || value == "null" {
		return 0
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.Unix()
	}
	n, err := strconv.ParseFloat(value, 64)
	if err != nil || n <= 0 {
		return 0
	}
	switch {
	case n >= float64(now.UnixMilli()/2):
		return int64(n / 1000)
	case n >= float64(now.Unix()/2):
		return int64(n)
	case n >= 1_000_000:
		return now.Add(time.Duration(n) * time.Millisecond).Unix()
	default:
		return now.Add(time.Duration(n) * time.Second).Unix()
	}
}

// buildTokenQuery 构造 token API 的 query string。
// 值按原样拼接（dangerouslySetWindvaneParams 已是单次编码），不做二次编码。
func buildTokenQuery(t, sign string) string {
	parts := [][2]string{
		{"jsv", "2.7.2"},
		{"appKey", protocol.SignAppKey},
		{"t", t},
		{"sign", sign},
		{"v", "1.0"},
		{"type", "originaljson"},
		{"accountSite", "xianyu"},
		{"dataType", "json"},
		{"timeout", "20000"},
		{"needLoginPC", "false"},
		{"showErrorToast", "false"},
		{"api", "mtop.taobao.idlemessage.pc.login.token"},
		{"needLogin", "false"},
		{"sessionOption", "AutoLoginOnly"},
		{"ecode", "0"},
		{"dangerouslySetWindvaneParams", "%5Bobject%20Object%5D"},
		{"spm_cnt", "a21ybx.im.0.0"},
		{"spm_pre", ""},
		{"log_id", ""},
	}
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(p[0])
		b.WriteByte('=')
		b.WriteString(p[1])
	}
	return b.String()
}
