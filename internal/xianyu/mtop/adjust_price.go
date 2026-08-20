// Package mtop: 订单改价域 — mtop.taobao.idle.trade.user.adjust.price 调用与重试。
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

// AdjustPriceAPI 是卖家对待付款订单改价的 MTOP 端点。
const AdjustPriceAPI = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.trade.user.adjust.price/1.0/"

// AdjustOrderPriceContext 调用 mtop.taobao.idle.trade.user.adjust.price 修改待付款订单价格。
// priceCents 是改价后的订单总价，单位为分；平台只接受买家尚未付款的订单。
// 签名 token 过期时使用响应下发的新 Cookie 重签并重试，与确认发货保持同一恢复策略。
func (c *ClientImpl) AdjustOrderPriceContext(ctx context.Context, cookiesStr, orderID string, priceCents int64) (ok bool, ret []string, updatedCookies string, err error) {
	// currentCookies 是本轮请求实际使用的扁平 Cookie，优先取上下文中的会话状态。
	currentCookies := cookiesStr
	if // session 是上下文携带的响应 Cookie 会话，存在时以其合并状态为准。
	session := cookieSessionFromContext(ctx); session != nil {
		currentCookies, _, _ = session.State()
	}
	// lastRet 保存最后一次业务返回，供重试耗尽后回传调用方。
	var lastRet []string
	for // attempt 是含首次在内的 token 重签尝试序号。
	attempt := 0; attempt < 4; attempt++ {
		// previousCookies 记录本次请求前的 Cookie，用于判断响应是否下发了新 token。
		previousCookies := currentCookies
		// ok、ret、updated、requestErr 分别是单次调用的业务成功标志、业务返回、Cookie 更新和传输错误。
		ok, ret, updated, requestErr := c.adjustOrderPriceOnce(ctx, currentCookies, orderID, priceCents)
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
			return false, ret, currentCookies, sessionExpiredError("订单改价接口", ret)
		}
		if !isTokenExpiredRet(ret) {
			return false, ret, currentCookies, nil
		}
		if attempt == 3 {
			break
		}

		// MTop 通常会在 token 过期响应中通过 Set-Cookie 下发新签名 token；
		// 未下发时主动刷新一次 token 再重试。
		if currentCookies == previousCookies {
			// refreshed、refreshErr 分别是 token 主动刷新结果和刷新失败原因。
			refreshed, refreshErr := c.RefreshTokenContext(ctx, currentCookies)
			if refreshErr != nil {
				return false, ret, currentCookies, fmt.Errorf("订单改价 token 过期且刷新失败: %w", refreshErr)
			}
			if refreshed.UpdatedCookies != "" {
				currentCookies = refreshed.UpdatedCookies
			}
		}
		if // err 是重试间隔等待期间的上下文取消错误。
		err := sleepCtx(ctx, MTopRetryGap); err != nil {
			return false, ret, currentCookies, err
		}
	}
	return false, lastRet, currentCookies, nil
}

// adjustOrderPriceOnce 执行一次订单改价请求；业务成功要求 ret 为 SUCCESS 且 data.success 为 true。
func (c *ClientImpl) adjustOrderPriceOnce(ctx context.Context, cookiesStr, orderID string, priceCents int64) (ok bool, ret []string, updatedCookies string, err error) {
	// hc 是带统一日志的 HTTP 客户端。
	hc := c.httpClient()
	// adjustURL 是实际请求端点，测试可通过 AdjustPriceURL 覆盖。
	adjustURL := c.AdjustPriceURL
	if adjustURL == "" {
		adjustURL = AdjustPriceAPI
	}
	// signingCookies、requestCookies 分别是参与签名的 Cookie 和随请求发送的 Cookie。
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookiesStr, "https://www.goofish.com/", adjustURL)
	// token 是从 Cookie 提取的 MTOP 签名 token。
	token := protocol.SignToken(signingCookies)
	// t 是签名使用的毫秒时间戳。
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	// dataVal 是改价请求体：modifyFee 为整数分，运费固定为 0。
	dataVal := `{"modifyFee":` + strconv.FormatInt(priceCents, 10) + `,"newTransportFee":"0","orderId":"` + orderID + `"}`
	// sign 是本次请求的 MTOP 签名。
	sign := protocol.GenerateSign(t, token, dataVal)

	// query 是改价请求的 URL 查询参数。
	query := buildAdjustPriceQuery(t, sign)
	// body 是 URL 编码后的请求体。
	body := "data=" + url.QueryEscape(dataVal)
	// req、err 分别是构造的 HTTP 请求和构造错误。
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		adjustURL+"?"+query,
		strings.NewReader(body))
	if err != nil {
		return false, nil, cookiesStr, err
	}
	setCommonHeaders(req, requestCookies)
	// resp、err 分别是 HTTP 响应和传输错误。
	resp, err := hc.Do(req)
	if err != nil {
		return false, nil, cookiesStr, fmt.Errorf("订单改价请求失败: %w", err)
	}
	defer resp.Body.Close()
	// updated 是合并响应 Set-Cookie 后的最新扁平 Cookie。
	updated := absorbMTopResponseCookies(ctx, cookiesStr, resp)
	// raw、err 分别是响应正文和读取错误。
	raw, err := readMTopBody(resp)
	if err != nil {
		return false, nil, updated, err
	}
	// res 是改价响应的最小解析结构；data.success 是平台的业务成功标志。
	var res struct {
		Ret  []string `json:"ret"`
		Data struct {
			Success bool `json:"success"`
		} `json:"data"`
	}
	if // err 是响应 JSON 解析错误。
	err := json.Unmarshal(raw, &res); err != nil {
		return false, nil, updated, fmt.Errorf("解析订单改价响应失败: %w (body=%s)", err, truncate(string(raw), 300))
	}
	if hasMTopSuccess(res.Ret) && res.Data.Success {
		return true, res.Ret, updated, nil
	}
	return false, res.Ret, updated, nil
}

// buildAdjustPriceQuery 构造订单改价请求的固定查询参数。
func buildAdjustPriceQuery(t, sign string) string {
	// parts 是按顺序拼接的查询键值对。
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
		{"api", "mtop.taobao.idle.trade.user.adjust.price"},
		{"sessionOption", "AutoLoginOnly"},
	}
	// b 是查询字符串构造器。
	var b strings.Builder
	// i、p 分别是当前键值对的下标和内容。
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
