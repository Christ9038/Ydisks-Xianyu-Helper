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
	// currentCookies 保存currentCookies，供当前处理流程使用
	currentCookies := cookiesStr
	if // session 保存会话，供当前处理流程使用
	session := cookieSessionFromContext(ctx); session != nil {
		currentCookies, _, _ = session.State()
	}
	// lastRet 保存lastRet，供当前处理流程使用
	var lastRet []string
	for // attempt 保存尝试次数，供当前处理流程使用
	attempt := 0; attempt < 4; attempt++ {
		// res、ret、updatedCookies、err 保存res、ret、updatedCookies、err，供当前处理流程使用
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
			if // err 保存err，供当前处理流程使用
			err := sleepCtx(ctx, MTopRetryGap); err != nil {
				return nil, err
			}
			continue
		}
		if // err 保存err，供当前处理流程使用
		err := sleepCtx(ctx, MTopRetryGap); err != nil {
			return nil, err
		}
		// refreshed、err 保存refreshed、err，供当前处理流程使用
		refreshed, err := c.RefreshTokenContext(ctx, currentCookies)
		if err != nil {
			return nil, fmt.Errorf("刷新 mtop token 失败: %w", err)
		}
		currentCookies = refreshed.UpdatedCookies
	}
	return nil, fmt.Errorf("账号资料接口 token 重试失败: ret=%v", lastRet)
}

// fetchUserProfileOnce 负责fetch用户ProfileOnce相关处理。
func (c *ClientImpl) fetchUserProfileOnce(ctx context.Context, cookiesStr string) (*UserProfileResult, []string, string, error) {
	// hc 保存hc，供当前处理流程使用
	hc := c.httpClient()

	// dataVal 保存数据Val，供当前处理流程使用
	dataVal := "{}"
	// signingCookies、requestCookies 保存signingCookies、requestCookies，供当前处理流程使用
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookiesStr, "https://www.goofish.com/", UserPageNavAPI)
	// t 保存t，供当前处理流程使用
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	// sign 保存sign，供当前处理流程使用
	sign := protocol.GenerateSign(t, protocol.SignToken(signingCookies), dataVal)
	// query 保存查询，供当前处理流程使用
	query := buildUserPageNavQuery(t, sign)
	// body 保存请求体，供当前处理流程使用
	body := "data=" + url.QueryEscape(dataVal)

	// req、err 保存req、err，供当前处理流程使用
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, UserPageNavAPI+"?"+query, strings.NewReader(body))
	if err != nil {
		return nil, nil, cookiesStr, err
	}
	setCommonHeaders(req, requestCookies)

	// resp、err 保存resp、err，供当前处理流程使用
	resp, err := hc.Do(req)
	if err != nil {
		return nil, nil, cookiesStr, fmt.Errorf("账号资料请求失败: %w", err)
	}
	defer resp.Body.Close()
	// updated 保存updated，供当前处理流程使用
	updated := absorbMTopResponseCookies(ctx, cookiesStr, resp)
	// raw、err 保存raw、err，供当前处理流程使用
	raw, err := readMTopBody(resp)
	if err != nil {
		return nil, nil, updated, err
	}

	// decoded 保存decoded，供当前处理流程使用
	var decoded struct {
		Ret  []string       `json:"ret"`
		Data map[string]any `json:"data"`
	}
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, nil, updated, fmt.Errorf("解析账号资料响应失败: %w (body=%s)", err, truncate(string(raw), 300))
	}
	if !hasMTopSuccess(decoded.Ret) {
		return nil, decoded.Ret, updated, nil
	}

	// profile 保存profile，供当前处理流程使用
	profile := parseUserProfile(decoded.Data)
	profile.UpdatedCookies = updated
	return profile, decoded.Ret, updated, nil
}

// buildUserPageNavQuery 负责build用户页码Nav查询相关处理。
func buildUserPageNavQuery(t, sign string) string {
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
		{"api", "mtop.idle.web.user.page.nav"},
		{"sessionOption", "AutoLoginOnly"},
		{"ecode", "0"},
		{"spm_cnt", "a21ybx.home.0.0"},
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
		b.WriteString(url.QueryEscape(p[1]))
	}
	return b.String()
}

// parseUserProfile 负责parse用户Profile相关处理。
func parseUserProfile(data map[string]any) *UserProfileResult {
	// module 保存module，供当前处理流程使用
	module, _ := data["module"].(map[string]any)
	// base 保存base，供当前处理流程使用
	base, _ := module["base"].(map[string]any)
	if base == nil {
		return &UserProfileResult{}
	}
	// nickname 保存nickname，供当前处理流程使用
	nickname := strings.TrimSpace(mtopString(base["displayName"]))
	// displayNick 保存displayNick，供当前处理流程使用
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
