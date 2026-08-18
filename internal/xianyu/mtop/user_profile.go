// Package mtop: 账号资料域 — mtop.idle.web.user.page.nav 调用与解析。
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

// FetchUserProfile 获取当前 cookie 对应账号的实时昵称和头像。
func (c *ClientImpl) FetchUserProfile(ctx context.Context, cookiesStr string) (*UserProfileResult, error) {
	// currentCookies 用于本次流程后续判断的currentCookies
	currentCookies := cookiesStr
	if // session 用于本次流程后续判断的会话
	session := cookieSessionFromContext(ctx); session != nil {
		currentCookies, _, _ = session.State()
	}
	// lastRet 用于本次流程后续判断的lastRet
	var lastRet []string
	for // attempt 用于本次流程后续判断的尝试次数
	attempt := 0; attempt < 4; attempt++ {
		// res、ret、updatedCookies、err 用于本次流程后续判断的res、ret、updatedCookies、err
		res, ret, updatedCookies, err := c.fetchUserProfileOnce(ctx, currentCookies)
		if err != nil {
			return nil, err
		}
		lastRet = ret
		if res != nil {
			return res, nil
		}
		if isSessionExpiredRet(ret) {
			return nil, sessionExpiredError("账号资料接口", ret)
		}
		if !isTokenExpiredRet(ret) {
			return nil, fmt.Errorf("账号资料接口返回非成功: ret=%v", ret)
		}
		if updatedCookies != "" && updatedCookies != currentCookies {
			currentCookies = updatedCookies
			if // err 用于本次流程后续判断的err
			err := sleepCtx(ctx, MTopRetryGap); err != nil {
				return nil, err
			}
			continue
		}
		if // err 用于本次流程后续判断的err
		err := sleepCtx(ctx, MTopRetryGap); err != nil {
			return nil, err
		}
		// refreshed、err 用于本次流程后续判断的refreshed、err
		refreshed, err := c.RefreshTokenContext(ctx, currentCookies)
		if err != nil {
			return nil, fmt.Errorf("刷新 mtop token 失败: %w", err)
		}
		currentCookies = refreshed.UpdatedCookies
	}
	return nil, fmt.Errorf("账号资料接口 token 重试失败: ret=%v", lastRet)
}

// fetchUserProfileOnce 封装fetch用户ProfileOnce业务协调。
func (c *ClientImpl) fetchUserProfileOnce(ctx context.Context, cookiesStr string) (*UserProfileResult, []string, string, error) {
	// hc 用于本次流程后续判断的hc
	hc := c.httpClient()

	// dataVal 用于本次流程后续判断的数据Val
	dataVal := "{}"
	// signingCookies、requestCookies 用于本次流程后续判断的signingCookies、requestCookies
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookiesStr, "https://www.goofish.com/", UserPageNavAPI)
	// t 用于本次流程后续判断的t
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	// sign 用于本次流程后续判断的sign
	sign := protocol.GenerateSign(t, protocol.SignToken(signingCookies), dataVal)
	// query 用于本次流程后续判断的查询
	query := buildUserPageNavQuery(t, sign)
	// body 用于本次流程后续判断的请求体
	body := "data=" + url.QueryEscape(dataVal)

	// req、err 用于本次流程后续判断的req、err
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, UserPageNavAPI+"?"+query, strings.NewReader(body))
	if err != nil {
		return nil, nil, cookiesStr, err
	}
	setCommonHeaders(req, requestCookies)

	// resp、err 用于本次流程后续判断的resp、err
	resp, err := hc.Do(req)
	if err != nil {
		return nil, nil, cookiesStr, fmt.Errorf("账号资料请求失败: %w", err)
	}
	defer resp.Body.Close()
	// updated 用于本次流程后续判断的updated
	updated := absorbMTopResponseCookies(ctx, cookiesStr, resp)
	// raw、err 用于本次流程后续判断的raw、err
	raw, err := readMTopBody(resp)
	if err != nil {
		return nil, nil, updated, err
	}

	// decoded 用于本次流程后续判断的decoded
	var decoded struct {
		Ret  []string       `json:"ret"`
		Data map[string]any `json:"data"`
	}
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, nil, updated, fmt.Errorf("解析账号资料响应失败: %w (body=%s)", err, truncate(string(raw), 300))
	}
	if !hasMTopSuccess(decoded.Ret) {
		return nil, decoded.Ret, updated, nil
	}

	// profile 用于本次流程后续判断的profile
	profile := parseUserProfile(decoded.Data)
	profile.UpdatedCookies = updated
	return profile, decoded.Ret, updated, nil
}

// buildUserPageNavQuery 封装build用户页码Nav查询业务协调。
func buildUserPageNavQuery(t, sign string) string {
	// parts 用于本次流程后续判断的parts
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
		{"api", "mtop.idle.web.user.page.nav"},
		{"sessionOption", "AutoLoginOnly"},
		{"ecode", "0"},
		{"spm_cnt", "a21ybx.home.0.0"},
	}
	// b 用于本次流程后续判断的b
	var b strings.Builder
	// i、p 表示当前遍历过程中的i、p
	for i, p := range parts {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(p[0])
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(p[1]))
	}
	return b.String()
}

// parseUserProfile 封装parse用户Profile业务协调。
func parseUserProfile(data map[string]any) *UserProfileResult {
	// module 用于本次流程后续判断的module
	module, _ := data["module"].(map[string]any)
	// base 用于本次流程后续判断的base
	base, _ := module["base"].(map[string]any)
	if base == nil {
		return &UserProfileResult{}
	}
	// nickname 用于本次流程后续判断的nickname
	nickname := strings.TrimSpace(mtopString(base["displayName"]))
	// displayNick 用于本次流程后续判断的displayNick
	displayNick := strings.TrimSpace(mtopString(base["displayNick"]))
	if nickname == "" {
		nickname = displayNick
	}
	return &UserProfileResult{
		Nickname:    nickname,
		DisplayNick: displayNick,
		AvatarURL:   strings.TrimSpace(mtopString(base["avatar"])),
	}
}
