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

// officialMTopMaxAttempts 保存officialMTopMax尝试次数，供当前处理流程使用
const officialMTopMaxAttempts = 5

// RefreshToken 调用 mtop.taobao.idlemessage.pc.login.token 获取 accessToken。
// 遇到 mtop 签名 token 过期时，按官网 lib-mtop 2.7.3 的 H5 流程最多执行
// 5 次请求（含首次），每次先吸收 Go Cookie Jar 再重新签名。
// RefreshToken 刷新令牌。
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
// RefreshTokenWithDeviceIDContext 刷新令牌WithDeviceID上下文。
func (c *ClientImpl) RefreshTokenWithDeviceIDContext(ctx context.Context, cookiesStr, deviceID string) (*RefreshResult, error) {
	return c.RefreshTokenWithCredentialContext(ctx, cookiesStr, deviceID, nil)
}

// RefreshTokenWithCredentialContext 使用完整 Cookie 快照执行纯 Go HTTP 请求，
// 避免把不同 Domain/Path 的同名 Cookie 压成一个值。
// RefreshTokenWithCredentialContext 刷新令牌WithCredential上下文。
func (c *ClientImpl) RefreshTokenWithCredentialContext(ctx context.Context, cookiesStr, deviceID string, cookieSnapshot []cookierefresh.BrowserCookie) (*RefreshResult, error) {
	// currentCookies 保存currentCookies，供当前处理流程使用
	currentCookies := cookiesStr
	// currentSnapshot 保存currentSnapshot，供当前处理流程使用
	currentSnapshot := cookierefresh.NormalizeSnapshot(cookieSnapshot)
	// currentSnapshotComplete 保存currentSnapshotComplete，供当前处理流程使用
	currentSnapshotComplete := cookieSnapshot != nil
	// cookieStateChanged 保存登录凭证状态Changed，供当前处理流程使用
	cookieStateChanged := false
	if // session 保存会话，供当前处理流程使用
	session := cookieSessionFromContext(ctx); session != nil {
		currentCookies, currentSnapshot, cookieStateChanged = session.State()
		currentSnapshotComplete = currentSnapshot != nil
	}
	for // attempt 保存尝试次数，供当前处理流程使用
	attempt := 0; attempt < officialMTopMaxAttempts; attempt++ {
		// accessToken、expireAt、ret、updatedCookies、snapshot、verificationURL、status、snapshotComplete、attemptChanged、err 保存accessToken、expireAt、ret、updatedCookies、snapshot、verificationURL、status、snapshotComplete、attemptChanged、err，供当前处理流程使用
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
			// result 保存结果，供当前处理流程使用
			result := refreshResultFromContext(ctx, currentCookies, currentSnapshot, currentSnapshotComplete, cookieStateChanged)
			result.AccessToken = accessToken
			result.AccessTokenExpireAt = expireAt
			return result, nil
		}
		if isRiskVerificationRet(ret) {
			return refreshResultFromContext(ctx, currentCookies, currentSnapshot, currentSnapshotComplete, cookieStateChanged), &RiskVerificationError{Ret: ret, VerificationURL: verificationURL}
		}
		if isSessionExpiredRet(ret) {
			return refreshResultFromContext(ctx, currentCookies, currentSnapshot, currentSnapshotComplete, cookieStateChanged), sessionExpiredError("token API", ret)
		}
		if !isOfficialTokenRetryRet(ret) {
			return refreshResultFromContext(ctx, currentCookies, currentSnapshot, currentSnapshotComplete, cookieStateChanged), fmt.Errorf("token API 返回非成功: ret=%v (status=%d)", ret, status)
		}
		if attempt == officialMTopMaxAttempts-1 {
			// snapshotForClear 保存snapshotForClear，供当前处理流程使用
			var snapshotForClear []cookierefresh.BrowserCookie
			if currentSnapshotComplete {
				snapshotForClear = currentSnapshot
			}
			// cleanedCookies、cleanedSnapshot 保存cleanedCookies、cleanedSnapshot，供当前处理流程使用
			cleanedCookies, cleanedSnapshot := clearOfficialMTopTokenCookies(currentCookies, snapshotForClear)
			cookieStateChanged = cookieStateChanged || cleanedCookies != currentCookies || !slices.Equal(cleanedSnapshot, currentSnapshot)
			currentCookies, currentSnapshot = cleanedCookies, cleanedSnapshot
			if // session 保存会话，供当前处理流程使用
			session := cookieSessionFromContext(ctx); session != nil {
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

// refreshResult 负责refresh结果相关处理。
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

// refreshResultFromContext 负责refresh结果From上下文相关处理。
func refreshResultFromContext(ctx context.Context, updatedCookies string, snapshot []cookierefresh.BrowserCookie, complete, changed bool) *RefreshResult {
	if // session 保存会话，供当前处理流程使用
	session := cookieSessionFromContext(ctx); session != nil {
		// sessionChanged 保存会话Changed，供当前处理流程使用
		var sessionChanged bool
		updatedCookies, snapshot, sessionChanged = session.State()
		complete = snapshot != nil
		changed = changed || sessionChanged
	}
	return refreshResult(updatedCookies, snapshot, complete, changed)
}

// RequestFreshCaptchaURLContext 重新请求 token API，用于浏览器风控验证前获取新鲜验证链接。
// 如果风控已解除并直接返回 accessToken，则 TokenOK=true。
// RequestFreshCaptchaURLContext 负责请求FreshCaptchaURL上下文相关处理。
func (c *ClientImpl) RequestFreshCaptchaURLContext(ctx context.Context, cookiesStr, deviceID string) (*FreshCaptchaResult, error) {
	// snapshot 保存snapshot，供当前处理流程使用
	var snapshot []cookierefresh.BrowserCookie
	if // session 保存会话，供当前处理流程使用
	session := cookieSessionFromContext(ctx); session != nil {
		cookiesStr, snapshot, _ = session.State()
	}
	// accessToken、expireAt、ret、updatedCookies、verificationURL、err 保存accessToken、expireAt、ret、updatedCookies、verificationURL、err，供当前处理流程使用
	accessToken, expireAt, ret, updatedCookies, _, verificationURL, _, _, _, err := c.refreshTokenOnce(ctx, cookiesStr, deviceID, snapshot)
	if // session 保存会话，供当前处理流程使用
	session := cookieSessionFromContext(ctx); session != nil {
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

// refreshTokenOnce 负责refresh令牌Once相关处理。
func (c *ClientImpl) refreshTokenOnce(ctx context.Context, cookiesStr, deviceID string, cookieSnapshot []cookierefresh.BrowserCookie) (string, int64, []string, string, []cookierefresh.BrowserCookie, string, int, bool, bool, error) {
	// hc 保存hc，供当前处理流程使用
	hc := c.httpClientWithTimeout(20 * time.Second)

	// tokenURL 保存令牌URL，供当前处理流程使用
	tokenURL := c.TokenURL
	if tokenURL == "" {
		tokenURL = TokenAPI
	}
	// signingCookies、requestCookies 保存signingCookies、requestCookies，供当前处理流程使用
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookiesStr, mtopDocumentURL, tokenURL)
	if cookieSessionFromContext(ctx) == nil && cookieSnapshot != nil {
		signingCookies, requestCookies = snapshotRequestCookies(cookieSnapshot, cookiesStr, tokenURL)
	}
	// myid 保存myid，供当前处理流程使用
	myid := protocol.TransCookies(signingCookies)["unb"]
	if myid == "" {
		return "", 0, nil, cookiesStr, nil, "", 0, false, false, fmt.Errorf("cookie 缺少 unb 字段，无法生成 deviceId")
	}
	if strings.TrimSpace(deviceID) == "" {
		deviceID = protocol.GenerateDeviceID(myid)
	}
	// token 保存令牌，供当前处理流程使用
	token := protocol.SignToken(signingCookies)

	// t 保存t，供当前处理流程使用
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	// dataVal 保存数据Val，供当前处理流程使用
	dataVal := `{"appKey":"` + RegAppKey + `","deviceId":"` + deviceID + `"}`

	// 签名不覆盖 query，因此 query 的编码细节不影响验签。
	query := buildTokenQuery(t, protocol.GenerateSign(t, token, dataVal))

	// body 保存请求体，供当前处理流程使用
	body := "data=" + url.QueryEscape(dataVal)

	// requestURL 保存请求URL，供当前处理流程使用
	requestURL := tokenURL + "?" + query
	// raw 保存原始，供当前处理流程使用
	var raw []byte
	// status 保存状态，供当前处理流程使用
	var status int
	// updated 保存updated，供当前处理流程使用
	var updated string
	// snapshot 保存snapshot，供当前处理流程使用
	var snapshot []cookierefresh.BrowserCookie
	// snapshotComplete 保存snapshotComplete，供当前处理流程使用
	snapshotComplete := false
	// stateChanged 保存状态Changed，供当前处理流程使用
	stateChanged := false
	// req、reqErr 保存req、reqErr，供当前处理流程使用
	req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, strings.NewReader(body))
	if reqErr != nil {
		return "", 0, nil, cookiesStr, nil, "", 0, false, false, reqErr
	}
	setCommonHeaders(req, requestCookies)
	req.Header.Set("Referer", "https://www.goofish.com/im")
	// resp、reqErr 保存resp、reqErr，供当前处理流程使用
	resp, reqErr := hc.Do(req)
	if reqErr != nil {
		return "", 0, nil, cookiesStr, nil, "", 0, false, false, fmt.Errorf("token API 请求失败: %w", reqErr)
	}
	defer resp.Body.Close()
	// Go CookieSession 在读取响应体前应用 Set-Cookie，避免解析失败时丢掉
	// 服务端已经下发的凭证轮换或删除。
	if // session 保存会话，供当前处理流程使用
	session := cookieSessionFromContext(ctx); session != nil {
		updated = absorbMTopResponseCookies(ctx, cookiesStr, resp)
		// sessionChanged 保存会话Changed，供当前处理流程使用
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

	// res 保存响应，供当前处理流程使用
	var res struct {
		Ret  []string `json:"ret"`
		Data struct {
			AccessToken            string          `json:"accessToken"`
			AccessTokenExpiredTime json.RawMessage `json:"accessTokenExpiredTime"`
			URL                    string          `json:"url"`
		} `json:"data"`
	}
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(raw, &res); err != nil {
		return "", 0, nil, updated, snapshot, "", status, snapshotComplete, stateChanged, fmt.Errorf("解析 token 响应失败: %w (body=%s)", err, truncate(string(raw), 300))
	}

	// ok 保存ok，供当前处理流程使用
	ok := false
	// r 表示当前遍历过程中的r
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

// snapshotRequestCookies 负责snapshot请求Cookies相关处理。
func snapshotRequestCookies(snapshot []cookierefresh.BrowserCookie, fallback, requestURL string) (string, string) {
	if snapshot == nil {
		return fallback, fallback
	}
	// documentCookies 保存documentCookies，供当前处理流程使用
	documentCookies := make([]cookierefresh.BrowserCookie, 0, len(snapshot))
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range snapshot {
		if !cookie.HTTPOnly {
			documentCookies = append(documentCookies, cookie)
		}
	}
	// signing 保存signing，供当前处理流程使用
	signing, _ := cookierefresh.ScopedCookieHeaderForRequest(documentCookies, mtopDocumentURL, goofishTopSite, time.Now())
	// requestCookies 保存请求Cookies，供当前处理流程使用
	requestCookies, _ := cookierefresh.ScopedCookieHeaderForRequest(snapshot, requestURL, goofishTopSite, time.Now())
	return signing, requestCookies
}

// isOfficialTokenRetryRet 负责isOfficial令牌重试Ret相关处理。
func isOfficialTokenRetryRet(ret []string) bool {
	// value 表示当前遍历过程中的值
	for _, value := range ret {
		if strings.Contains(value, "TOKEN_EMPTY") || strings.Contains(value, "TOKEN_EXOIRED") {
			return true
		}
	}
	return false
}

// clearOfficialMTopTokenCookies 负责clearOfficialMTop令牌Cookies相关处理。
func clearOfficialMTopTokenCookies(cookieStr string, snapshot []cookierefresh.BrowserCookie) (string, []cookierefresh.BrowserCookie) {
	// values 保存values，供当前处理流程使用
	values := protocol.TransCookies(cookieStr)
	// name 表示当前遍历过程中的名称
	for _, name := range []string{"_m_h5_c", "_m_h5_tk", "_m_h5_tk_enc"} {
		delete(values, name)
	}
	// cleaned 保存cleaned，供当前处理流程使用
	var cleaned []cookierefresh.BrowserCookie
	if snapshot != nil {
		cleaned = make([]cookierefresh.BrowserCookie, 0, len(snapshot))
	}
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range snapshot {
		// domain 保存domain，供当前处理流程使用
		domain := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(cookie.Domain), "."))
		// cookiePath 保存登录凭证路径，供当前处理流程使用
		cookiePath := cookie.Path
		if cookiePath == "" {
			cookiePath = "/"
		}
		// remove 保存remove，供当前处理流程使用
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
		// canonical 保存canonical，供当前处理流程使用
		canonical, _ := cookierefresh.ScopedCookieHeaderForRequest(cleaned, mtopDocumentURL, goofishTopSite, time.Now())
		return canonical, cleaned
	}
	return marshalTokenCookies(values), cleaned
}

// marshalTokenCookies 负责marshal令牌Cookies相关处理。
func marshalTokenCookies(cookies map[string]string) string {
	// keys 保存keys，供当前处理流程使用
	keys := make([]string, 0, len(cookies))
	// key 表示当前遍历过程中的key
	for key := range cookies {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	// parts 保存parts，供当前处理流程使用
	parts := make([]string, 0, len(keys))
	// key 表示当前遍历过程中的key
	for _, key := range keys {
		parts = append(parts, key+"="+cookies[key])
	}
	return strings.Join(parts, "; ")
}

// parseAccessTokenExpireAt 负责parseAccess令牌ExpireAt相关处理。
func parseAccessTokenExpireAt(raw json.RawMessage, now time.Time) int64 {
	// value 保存值，供当前处理流程使用
	value := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if value == "" || value == "null" {
		return 0
	}
	if // parsed、err 保存parsed、err，供当前处理流程使用
	parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.Unix()
	}
	// n、err 保存n、err，供当前处理流程使用
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
// buildTokenQuery 负责build令牌查询相关处理。
func buildTokenQuery(t, sign string) string {
	// parts 保存parts，供当前处理流程使用
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
	// b 保存b，供当前处理流程使用
	var b strings.Builder
	// i、p 表示当前遍历过程中的i、p
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
