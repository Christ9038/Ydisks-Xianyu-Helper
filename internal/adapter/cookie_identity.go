package adapter

import (
	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/protocol"
)

// AccountIDFromCookie 从平台 Cookie 中提取扫码结果使用的账号标识；解析失败返回空字符串。
func AccountIDFromCookie(cookies string) string {
	return protocol.TransCookies(cookies)["unb"]
}

// CookieSnapshotsFromResult 将扫码服务结果中的浏览器快照转换为应用层模型，避免 Server 依赖 Cookie 实现类型。
func CookieSnapshotsFromResult(result map[string]any) ([]accountapp.CookieSnapshot, bool) {
	// raw 保存扫码结果中的原始 Cookie 快照；exists 表示结果是否提供该字段。
	raw, exists := result["cookie_snapshot"]
	if !exists {
		return nil, false
	}
	// snapshot 保存平台 Cookie 快照；ok 表示其类型符合浏览器适配器约定。
	snapshot, ok := raw.([]cookierefresh.BrowserCookie)
	if !ok || snapshot == nil {
		return nil, false
	}
	// normalized 保存去重、归一化后的 Cookie 快照。
	normalized := cookierefresh.NormalizeSnapshot(snapshot)
	if normalized == nil {
		normalized = []cookierefresh.BrowserCookie{}
	}
	// converted 保存脱离浏览器实现类型的应用层快照。
	converted := make([]accountapp.CookieSnapshot, 0, len(normalized))
	// cookie 表示当前待转换的浏览器 Cookie，明文只在适配器转换期间存在。
	for _, cookie := range normalized {
		converted = append(converted, accountapp.CookieSnapshot{
			Name: cookie.Name, Value: cookie.Value, Domain: cookie.Domain, Path: cookie.Path,
			Expires: cookie.Expires, HTTPOnly: cookie.HTTPOnly, Secure: cookie.Secure,
			SameSite: cookie.SameSite, PartitionKey: cookie.PartitionKey,
		})
	}
	return converted, true
}
