// Package mtop: 虚拟发货域 — mtop.taobao.idle.logistic.consign.dummy 调用与重试。
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

// Consign 调用 mtop.taobao.idle.logistic.consign.dummy 确认发货（虚拟发货）。
// data_val 形如 {"orderId":"...","tradeText":"","picList":[],"newUnconsign":true}。
// 返回成功标志、响应 ret 列表、可能更新后的 cookie。
// 移植自 secure_confirm_decrypted.auto_confirm。
func (c *ClientImpl) Consign(cookiesStr, orderID string) (ok bool, ret []string, updatedCookies string, err error) {
	return c.ConsignContext(context.Background(), cookiesStr, orderID)
}

// ConsignContext 确认发货；签名 token 过期时使用响应下发的新 Cookie 重签并重试。
func (c *ClientImpl) ConsignContext(ctx context.Context, cookiesStr, orderID string) (ok bool, ret []string, updatedCookies string, err error) {
	currentCookies := cookiesStr
	var lastRet []string
	for attempt := 0; attempt < 4; attempt++ {
		previousCookies := currentCookies
		ok, ret, updated, requestErr := c.consignOnce(ctx, currentCookies, orderID)
		if requestErr != nil {
			return false, ret, currentCookies, requestErr
		}
		lastRet = ret
		if updated != "" {
			currentCookies = updated
		}
		if ok {
			return true, ret, currentCookies, nil
		}
		if !isTokenExpiredRet(ret) {
			return false, ret, currentCookies, nil
		}
		if attempt == 3 {
			break
		}

		// MTop 通常会在 token 过期响应中通过 Set-Cookie 下发新签名 token。
		// 若没有下发，则主动调用 token API 尝试刷新一次。
		if currentCookies == previousCookies {
			refreshed, refreshErr := c.RefreshTokenContext(ctx, currentCookies)
			if refreshErr != nil {
				return false, ret, currentCookies, fmt.Errorf("consign token 过期且刷新失败: %w", refreshErr)
			}
			if refreshed.UpdatedCookies != "" {
				currentCookies = refreshed.UpdatedCookies
			}
		}
		if err := sleepCtx(ctx, MTopRetryGap); err != nil {
			return false, ret, currentCookies, err
		}
	}
	return false, lastRet, currentCookies, nil
}

func (c *ClientImpl) consignOnce(ctx context.Context, cookiesStr, orderID string) (ok bool, ret []string, updatedCookies string, err error) {
	hc := c.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	cookies := protocol.TransCookies(cookiesStr)
	token := protocol.SignToken(cookiesStr)
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	dataVal := `{"orderId":"` + orderID + `", "tradeText":"","picList":[],"newUnconsign":true}`
	sign := protocol.GenerateSign(t, token, dataVal)

	query := buildConsignQuery(t, sign)
	body := "data=" + url.QueryEscape(dataVal)
	consignURL := c.ConsignURL
	if consignURL == "" {
		consignURL = ConsignAPI
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		consignURL+"?"+query,
		strings.NewReader(body))
	if err != nil {
		return false, nil, cookiesStr, err
	}
	setCommonHeaders(req, cookiesStr)
	resp, err := hc.Do(req)
	if err != nil {
		return false, nil, cookiesStr, fmt.Errorf("consign 请求失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := readMTopBody(resp)
	if err != nil {
		return false, nil, cookiesStr, err
	}
	updated := mergeSetCookie(cookiesStr, cookies, resp)
	var res struct {
		Ret []string `json:"ret"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return false, nil, updated, fmt.Errorf("解析 consign 响应失败: %w (body=%s)", err, truncate(string(raw), 300))
	}
	for _, r := range res.Ret {
		if strings.Contains(r, "SUCCESS::调用成功") {
			return true, res.Ret, updated, nil
		}
	}
	return false, res.Ret, updated, nil
}

func buildConsignQuery(t, sign string) string {
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
		{"api", "mtop.taobao.idle.logistic.consign.dummy"},
		{"sessionOption", "AutoLoginOnly"},
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
