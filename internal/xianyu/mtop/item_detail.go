// Package mtop: 商品详情域 — 补充商品列表未返回的多规格信息。
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

// ItemDetailFetcher 是商品同步使用的可选详情能力。
type ItemDetailFetcher interface {
	DetectItemMultiSpec(ctx context.Context, cookies, itemID string) (bool, error)
}

var _ ItemDetailFetcher = (*ClientImpl)(nil)

// DetectItemMultiSpec 查询商品详情并识别多规格结构。该调用不主动刷新 token；
// 当 ctx 携带 CookieSession 时会像浏览器一样吸收响应 Cookie。
func (c *ClientImpl) DetectItemMultiSpec(ctx context.Context, cookies, itemID string) (bool, error) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return false, fmt.Errorf("item_id 不能为空")
	}
	endpoint := c.ItemDetailURL
	if endpoint == "" {
		endpoint = ItemDetailAPI
	}
	documentURL := "https://www.goofish.com/item?id=" + url.QueryEscape(itemID)
	signingCookies, requestCookies := mtopRequestCookies(ctx, cookies, documentURL, endpoint)
	token := protocol.SignToken(signingCookies)
	if token == "" {
		return false, fmt.Errorf("cookie 缺少 _m_h5_tk，无法获取商品详情")
	}
	dataVal := `{"itemId":` + strconv.Quote(itemID) + `}`
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	sign := protocol.GenerateSign(timestamp, token, dataVal)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"?"+buildItemDetailQuery(timestamp, sign), strings.NewReader("data="+url.QueryEscape(dataVal)))
	if err != nil {
		return false, err
	}
	setCommonHeaders(req, requestCookies)
	req.Header.Set("Origin", "https://www.goofish.com")
	req.Header.Set("Referer", documentURL)

	hc := c.httpClient()
	resp, err := hc.Do(req)
	if err != nil {
		return false, fmt.Errorf("商品详情请求失败: %w", err)
	}
	defer resp.Body.Close()
	absorbMTopResponseCookies(ctx, cookies, resp)
	raw, err := readMTopBody(resp)
	if err != nil {
		return false, err
	}
	var decoded struct {
		Ret  []string       `json:"ret"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return false, fmt.Errorf("解析商品详情响应失败: %w (body=%s)", err, truncate(string(raw), 300))
	}
	if !hasMTopSuccess(decoded.Ret) {
		return false, fmt.Errorf("商品详情接口返回非成功: ret=%v", decoded.Ret)
	}
	return detectItemMultiSpec(decoded.Data), nil
}

func buildItemDetailQuery(timestamp, sign string) string {
	values := url.Values{
		"jsv":           {"2.7.2"},
		"appKey":        {protocol.SignAppKey},
		"t":             {timestamp},
		"sign":          {sign},
		"v":             {"1.0"},
		"type":          {"originaljson"},
		"accountSite":   {"xianyu"},
		"dataType":      {"json"},
		"timeout":       {"20000"},
		"api":           {"mtop.taobao.idle.pc.detail"},
		"sessionOption": {"AutoLoginOnly"},
		"spm_cnt":       {"a21ybx.item.0.0"},
	}
	return values.Encode()
}

func detectItemMultiSpec(value any) bool {
	return detectMultiSpecValue(value, false, 0)
}

func detectMultiSpecValue(value any, skuContext bool, depth int) bool {
	if depth > 16 {
		return false
	}
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
			switch normalized {
			case "multisku", "ismultisku", "ismultispec", "multiplesku":
				if mtopBool(child) {
					return true
				}
			case "skulist", "skus":
				if list, ok := child.([]any); ok && len(list) > 1 {
					return true
				}
			case "skuprops", "skuproperties", "specprops", "specifications":
				if list, ok := child.([]any); ok && len(list) > 0 {
					return true
				}
			case "props", "properties":
				if skuContext {
					if list, ok := child.([]any); ok && len(list) > 0 {
						return true
					}
				}
			}
			nextSKUContext := skuContext || normalized == "skudo" || normalized == "skubase" || normalized == "skumodel"
			if detectMultiSpecValue(child, nextSKUContext, depth+1) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if detectMultiSpecValue(child, skuContext, depth+1) {
				return true
			}
		}
	}
	return false
}
