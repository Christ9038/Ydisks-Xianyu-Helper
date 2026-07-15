// Package mtop: token 域 — mtop.taobao.idlemessage.pc.login.token 调用与重试。
package mtop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/xianyu/protocol"
)

const tokenRetryGap = 500 * time.Millisecond

// RefreshToken 调用 mtop.taobao.idlemessage.pc.login.token 获取 accessToken。
// 遇到 mtop 签名 token 过期时，仅在响应下发了新 Cookie 后低频重试一次。
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
	currentCookies := cookiesStr
	for attempt := 0; attempt < 2; attempt++ {
		accessToken, expireAt, ret, updatedCookies, verificationURL, status, err := c.refreshTokenOnce(ctx, currentCookies, deviceID)
		if err != nil {
			return &RefreshResult{UpdatedCookies: currentCookies}, err
		}
		if accessToken != "" {
			return &RefreshResult{AccessToken: accessToken, AccessTokenExpireAt: expireAt, UpdatedCookies: updatedCookies}, nil
		}
		if isRiskVerificationRet(ret) {
			return &RefreshResult{UpdatedCookies: updatedCookies}, &RiskVerificationError{Ret: ret, VerificationURL: verificationURL}
		}
		if !isTokenExpiredRet(ret) {
			return &RefreshResult{UpdatedCookies: updatedCookies}, fmt.Errorf("token API 返回非成功: ret=%v (status=%d)", ret, status)
		}
		// 参考实现对 FAIL_SYS_TOKEN_EXOIRED/EXPIRED 固定等待 0.5 秒重试
		// 一次；是否收到 Set-Cookie 不改变重试次数。
		if attempt == 1 {
			return &RefreshResult{UpdatedCookies: updatedCookies}, fmt.Errorf("token API 登录凭证已失效: ret=%v (status=%d)", ret, status)
		}
		if updatedCookies != "" {
			currentCookies = updatedCookies
		}
		if err := sleepCtx(ctx, tokenRetryGap); err != nil {
			return &RefreshResult{UpdatedCookies: currentCookies}, err
		}
	}
	return &RefreshResult{UpdatedCookies: currentCookies}, fmt.Errorf("token API 登录凭证已失效")
}

// RequestFreshCaptchaURLContext 重新请求 token API，用于浏览器风控验证前获取新鲜验证链接。
// 如果风控已解除并直接返回 accessToken，则 TokenOK=true。
func (c *ClientImpl) RequestFreshCaptchaURLContext(ctx context.Context, cookiesStr, deviceID string) (*FreshCaptchaResult, error) {
	accessToken, expireAt, ret, updatedCookies, verificationURL, _, err := c.refreshTokenOnce(ctx, cookiesStr, deviceID)
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

func (c *ClientImpl) refreshTokenOnce(ctx context.Context, cookiesStr, deviceID string) (string, int64, []string, string, string, int, error) {
	hc := c.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}

	cookies := protocol.TransCookies(cookiesStr)
	myid := cookies["unb"]
	if myid == "" {
		return "", 0, nil, cookiesStr, "", 0, fmt.Errorf("cookie 缺少 unb 字段，无法生成 deviceId")
	}
	if strings.TrimSpace(deviceID) == "" {
		deviceID = protocol.GenerateDeviceID(myid)
	}
	token := protocol.SignToken(cookiesStr)

	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	dataVal := `{"appKey":"` + RegAppKey + `","deviceId":"` + deviceID + `"}`

	// 签名不覆盖 query，因此 query 的编码细节不影响验签。
	query := buildTokenQuery(t, protocol.GenerateSign(t, token, dataVal))

	body := "data=" + url.QueryEscape(dataVal)

	tokenURL := c.TokenURL
	if tokenURL == "" {
		tokenURL = TokenAPI
	}
	requestURL := tokenURL + "?" + query
	var raw []byte
	var status int
	updated := cookiesStr
	if c.TokenExecutor != nil {
		browserResp, execErr := c.TokenExecutor.ExecuteTokenRequest(ctx, TokenBrowserRequest{
			URL: requestURL, Body: body, Cookies: cookiesStr,
		})
		if execErr != nil {
			return "", 0, nil, cookiesStr, "", 0, fmt.Errorf("浏览器 token API 请求失败: %w", execErr)
		}
		if browserResp == nil {
			return "", 0, nil, cookiesStr, "", 0, fmt.Errorf("浏览器 token API 返回空响应")
		}
		raw, status = browserResp.Body, browserResp.Status
		if strings.TrimSpace(browserResp.UpdatedCookies) != "" {
			updated = browserResp.UpdatedCookies
		}
	} else {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, strings.NewReader(body))
		if reqErr != nil {
			return "", 0, nil, cookiesStr, "", 0, reqErr
		}
		setCommonHeaders(req, cookiesStr)
		resp, reqErr := hc.Do(req)
		if reqErr != nil {
			return "", 0, nil, cookiesStr, "", 0, fmt.Errorf("token API 请求失败: %w", reqErr)
		}
		defer resp.Body.Close()
		raw, reqErr = readMTopBody(resp)
		if reqErr != nil {
			return "", 0, nil, cookiesStr, "", resp.StatusCode, reqErr
		}
		status = resp.StatusCode
		// 即使业务返回 token 过期，也要保留响应下发的新签名 Cookie。
		updated = mergeSetCookie(cookiesStr, cookies, resp)
	}

	var res struct {
		Ret  []string `json:"ret"`
		Data struct {
			AccessToken            string          `json:"accessToken"`
			AccessTokenExpiredTime json.RawMessage `json:"accessTokenExpiredTime"`
			URL                    string          `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", 0, nil, updated, "", status, fmt.Errorf("解析 token 响应失败: %w (body=%s)", err, truncate(string(raw), 300))
	}

	ok := false
	for _, r := range res.Ret {
		if strings.Contains(r, "SUCCESS::调用成功") {
			ok = true
			break
		}
	}
	if !ok {
		return "", 0, res.Ret, updated, res.Data.URL, status, nil
	}
	if res.Data.AccessToken == "" {
		return "", 0, res.Ret, updated, "", status, fmt.Errorf("token API 成功但 accessToken 为空 (body=%s)", truncate(string(raw), 300))
	}
	return res.Data.AccessToken, parseAccessTokenExpireAt(res.Data.AccessTokenExpiredTime, time.Now()), res.Ret, updated, "", status, nil
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
		{"api", "mtop.taobao.idlemessage.pc.login.token"},
		{"sessionOption", "AutoLoginOnly"},
		{"dangerouslySetWindvaneParams", "%5Bobject%20Object%5D"},
		{"smToken", "token"},
		{"queryToken", "sm"},
		{"sm", "sm"},
		{"spm_cnt", "a21ybx.im.0.0"},
		{"spm_pre", "a21ybx.home.sidebar.1.4c053da6vYwnmf"},
		{"log_id", "4c053da6vYwnmf"},
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
