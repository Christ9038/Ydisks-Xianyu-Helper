package server

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"xianyu-go/internal/xianyu/mtop"
)

// mtopResp 描述一次 mock mtop HTTP 响应。
type mtopResp struct {
	ret            []string
	body           string // 非 empty 时直接返回该 body（优先于 ret）
	updatedCookies string // 通过 Set-Cookie 头下发
	statusCode     int
}

// withMTopTransport 用自定义 transport 构造 *mtop.ClientImpl（按 URL 分发用闭包）。
func withMTopTransport(rt roundTripFunc) *mtop.ClientImpl {
	// cli 用于本次流程后续判断的cli
	cli := mtop.NewClient()
	cli.HTTPClient = &http.Client{Transport: rt}
	return cli
}

// 默认对未匹配 URL 返回 SUCCESS。
func newMockMTop(t *testing.T, resp mtopResp) *mtop.ClientImpl {
	t.Helper()
	// cli 用于本次流程后续判断的cli
	cli := mtop.NewClient()
	cli.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// body 用于本次流程后续判断的请求体
		body := resp.body
		if body == "" {
			body = `{"ret":["` + strings.Join(resp.ret, "\",\"") + `"]}`
		}
		// status 用于本次流程后续判断的状态
		status := resp.statusCode
		if status == 0 {
			status = http.StatusOK
		}
		// r 用于本次流程后续判断的r
		r := &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}
		if resp.updatedCookies != "" {
			r.Header.Set("Set-Cookie", "_m_h5_tk="+resp.updatedCookies+"; Path=/")
		}
		return r, nil
	})}
	return cli
}
