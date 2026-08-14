package mtop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/xianyu/protocol"
)

// LoginUserAPI 保存登录用户API，供当前处理流程使用
const LoginUserAPI = "https://h5api.m.goofish.com/h5/mtop.taobao.idlemessage.pc.loginuser.get/1.0/"

// LoginStatusSuccess 保存登录状态Success，供当前处理流程使用
const (
	LoginStatusSuccess        = "success"
	LoginStatusTokenRefreshed = "token_refreshed"
	LoginStatusSessionExpired = "session_expired"
	LoginStatusTokenEmpty     = "token_empty"
	LoginStatusRiskRequired   = "risk_required"
	LoginStatusFailed         = "failed"
)

// LoginStatusResult 是 loginuser.get 的登录态检查结果。
type LoginStatusResult struct {
	Status          string
	Ret             []string
	UpdatedCookies  string
	VerificationURL string
	Message         string
}

// CheckLoginStatusContext 调用 loginuser.get 做低成本登录态检查。
// 它不会做浏览器动作；只负责分类响应和合并 Set-Cookie。
// CheckLoginStatusContext 检查登录状态上下文。
func (c *ClientImpl) CheckLoginStatusContext(ctx context.Context, cookiesStr string) (*LoginStatusResult, error) {
	if // session 保存会话，供当前处理流程使用
	session := cookieSessionFromContext(ctx); session != nil {
		cookiesStr, _, _ = session.State()
	}
	// hc 保存hc，供当前处理流程使用
	hc := c.httpClientWithTimeout(20 * time.Second)
	// loginURL 保存登录URL，供当前处理流程使用
	loginURL := c.LoginUserURL
	if loginURL == "" {
		loginURL = LoginUserAPI
	}
	// signingCookies、requestCookies 保存signingCookies、requestCookies，供当前处理流程使用
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookiesStr, mtopDocumentURL, loginURL)
	// t 保存t，供当前处理流程使用
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	// dataVal 保存数据Val，供当前处理流程使用
	dataVal := `{}`
	// query 保存查询，供当前处理流程使用
	query := buildLoginStatusQuery(t, protocol.GenerateSign(t, protocol.SignToken(signingCookies), dataVal))

	// req、err 保存req、err，供当前处理流程使用
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, loginURL+"?"+query, strings.NewReader("data=%7B%7D"))
	if err != nil {
		return nil, err
	}
	setCommonHeaders(req, requestCookies)
	req.Header.Set("Referer", mtopDocumentURL)

	// resp、err 保存resp、err，供当前处理流程使用
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("loginuser.get 请求失败: %w", err)
	}
	defer resp.Body.Close()
	// updated 保存updated，供当前处理流程使用
	updated := absorbMTopResponseCookies(ctx, cookiesStr, resp)
	// raw、err 保存raw、err，供当前处理流程使用
	raw, err := readMTopBody(resp)
	if err != nil {
		return nil, err
	}

	// payload 保存请求载荷，供当前处理流程使用
	var payload struct {
		Ret  []string `json:"ret"`
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("解析 loginuser.get 响应失败: %w (body=%s)", err, truncate(string(raw), 300))
	}
	// status、msg 保存status、msg，供当前处理流程使用
	status, msg := classifyLoginStatus(payload.Ret, updated != cookiesStr)
	return &LoginStatusResult{
		Status:          status,
		Ret:             payload.Ret,
		UpdatedCookies:  updated,
		VerificationURL: payload.Data.URL,
		Message:         msg,
	}, nil
}

// buildLoginStatusQuery 负责build登录状态查询相关处理。
func buildLoginStatusQuery(t, sign string) string {
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
		{"api", "mtop.taobao.idlemessage.pc.loginuser.get"},
		{"sessionOption", "AutoLoginOnly"},
		{"spm_cnt", "a21ybx.im.0.0"},
		{"spm_pre", "a21ybx.item.want.1.12523da6waCtUp"},
		{"log_id", "12523da6waCtUp"},
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

// classifyLoginStatus 负责classify登录状态相关处理。
func classifyLoginStatus(ret []string, cookieUpdated bool) (string, string) {
	if hasMTopSuccess(ret) {
		return LoginStatusSuccess, "登录状态正常"
	}
	if isRiskVerificationRet(ret) {
		return LoginStatusRiskRequired, "闲鱼要求安全验证"
	}
	// retStr 保存retStr，供当前处理流程使用
	retStr := strings.Join(ret, " ")
	switch {
	case strings.Contains(retStr, "TOKEN_EMPTY") || strings.Contains(retStr, "令牌为空"):
		return LoginStatusTokenEmpty, "令牌为空，需要重新登录"
	case strings.Contains(retStr, "SESSION_EXPIRED") || strings.Contains(retStr, "Session过期"):
		return LoginStatusSessionExpired, "Session过期，需要重新登录"
	case strings.Contains(retStr, "TOKEN_EXOIRED") || strings.Contains(retStr, "TOKEN_EXPIRED"):
		if cookieUpdated {
			return LoginStatusTokenRefreshed, "令牌已刷新"
		}
		return LoginStatusFailed, "令牌过期但未获取到新Cookie"
	default:
		return LoginStatusFailed, retStr
	}
}
