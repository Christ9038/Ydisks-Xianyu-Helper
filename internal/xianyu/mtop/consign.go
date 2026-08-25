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
// 返回成功标志、响应 ret 列表、可能更新后的 cookie；不带额外凭证内容时保持历史请求语义。
// 移植自 secure_confirm_decrypted.auto_confirm。
func (c *ClientImpl) Consign(cookiesStr, orderID string) (ok bool, ret []string, updatedCookies string, err error) {
	return c.ConsignContextWithDelivery(context.Background(), cookiesStr, orderID, "", nil)
}

// ConsignContext 确认发货；签名 token 过期时使用响应下发的新 Cookie 重签并重试。
func (c *ClientImpl) ConsignContext(ctx context.Context, cookiesStr, orderID string) (ok bool, ret []string, updatedCookies string, err error) {
	return c.ConsignContextWithDelivery(ctx, cookiesStr, orderID, "", nil)
}

// ConsignContextWithDelivery 确认虚拟发货并携带已发送给买家的文本或图片凭证；凭证只存在于本次请求内，不写入日志或任务快照。
func (c *ClientImpl) ConsignContextWithDelivery(ctx context.Context, cookiesStr, orderID, tradeText string, picList []string) (ok bool, ret []string, updatedCookies string, err error) {
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
		// previousCookies 用于本次流程后续判断的previousCookies
		previousCookies := currentCookies
		// ok、ret、updated、requestErr 用于本次流程后续判断的ok、ret、updated、requestErr
		ok, ret, updated, requestErr := c.consignOnce(ctx, currentCookies, orderID, tradeText, picList)
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
		if isSessionExpiredRet(ret) {
			return false, ret, currentCookies, sessionExpiredError("确认发货接口", ret)
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
			// refreshed、refreshErr 用于本次流程后续判断的refreshed、refreshErr
			refreshed, refreshErr := c.RefreshTokenContext(ctx, currentCookies)
			if refreshErr != nil {
				return false, ret, currentCookies, fmt.Errorf("consign token 过期且刷新失败: %w", refreshErr)
			}
			if refreshed.UpdatedCookies != "" {
				currentCookies = refreshed.UpdatedCookies
			}
		}
		if // err 用于本次流程后续判断的err
		err := sleepCtx(ctx, MTopRetryGap); err != nil {
			return false, ret, currentCookies, err
		}
	}
	return false, lastRet, currentCookies, nil
}

// consignOnce 封装consignOnce业务协调。
// consignOnce 发送一次带发货凭证的确认发货请求；调用方负责在 token 过期时重试。
func (c *ClientImpl) consignOnce(ctx context.Context, cookiesStr, orderID, tradeText string, picList []string) (ok bool, ret []string, updatedCookies string, err error) {
	// hc 用于本次流程后续判断的hc
	hc := c.httpClient()
	// consignURL 用于本次流程后续判断的consignURL
	consignURL := c.ConsignURL
	if consignURL == "" {
		consignURL = ConsignAPI
	}
	// signingCookies、requestCookies 用于本次流程后续判断的signingCookies、requestCookies
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookiesStr, "https://www.goofish.com/", consignURL)
	// token 用于本次流程后续判断的令牌
	token := protocol.SignToken(signingCookies)
	// t 用于本次流程后续判断的t
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	// payload 保存确认发货接口需要的订单号、发货文本和图片凭证；图片切片只在当前请求中使用。
	payload := struct {
		// OrderID 是平台确认发货使用的订单号。
		OrderID string `json:"orderId"`
		// TradeText 是买家实际收到的文本发货凭证。
		TradeText string `json:"tradeText"`
		// PictureList 是买家实际收到的图片发货凭证地址。
		PictureList []string `json:"picList"`
		// NewUnconsign 表示本次请求按虚拟商品确认发货处理。
		NewUnconsign bool `json:"newUnconsign"`
	}{OrderID: orderID, TradeText: tradeText, PictureList: append([]string(nil), picList...), NewUnconsign: true}
	if payload.PictureList == nil {
		payload.PictureList = []string{}
	}
	// dataVal 是签名和请求体共同使用的 JSON；序列化确保订单号和凭证中的特殊字符不会破坏请求结构。
	dataBytes, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return false, nil, cookiesStr, fmt.Errorf("构造 consign 请求数据失败: %w", marshalErr)
	}
	// dataVal 是序列化后的确认发货数据，参与签名并作为表单值提交。
	dataVal := string(dataBytes)
	// sign 用于本次流程后续判断的sign
	sign := protocol.GenerateSign(t, token, dataVal)

	// query 用于本次流程后续判断的查询
	query := buildConsignQuery(t, sign)
	// body 用于本次流程后续判断的请求体
	body := "data=" + url.QueryEscape(dataVal)
	// req、err 用于本次流程后续判断的req、err
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		consignURL+"?"+query,
		strings.NewReader(body))
	if err != nil {
		return false, nil, cookiesStr, err
	}
	setCommonHeaders(req, requestCookies)
	// resp、err 用于本次流程后续判断的resp、err
	resp, err := hc.Do(req)
	if err != nil {
		return false, nil, cookiesStr, fmt.Errorf("consign 请求失败: %w", err)
	}
	defer resp.Body.Close()
	// updated 用于本次流程后续判断的updated
	updated := absorbMTopResponseCookies(ctx, cookiesStr, resp)
	// raw、err 用于本次流程后续判断的raw、err
	raw, err := readMTopBody(resp)
	if err != nil {
		return false, nil, updated, err
	}
	// res 用于本次流程后续判断的响应
	var res struct {
		Ret []string `json:"ret"`
	}
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal(raw, &res); err != nil {
		return false, nil, updated, fmt.Errorf("解析 consign 响应失败: %w (body=%s)", err, truncate(string(raw), 300))
	}
	// r 表示当前遍历过程中的r
	for _, r := range res.Ret {
		if strings.Contains(r, "SUCCESS::调用成功") {
			return true, res.Ret, updated, nil
		}
	}
	return false, res.Ret, updated, nil
}

// buildConsignQuery 封装buildConsign查询业务协调。
func buildConsignQuery(t, sign string) string {
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
		{"api", "mtop.taobao.idle.logistic.consign.dummy"},
		{"sessionOption", "AutoLoginOnly"},
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
		b.WriteString(p[1])
	}
	return b.String()
}
