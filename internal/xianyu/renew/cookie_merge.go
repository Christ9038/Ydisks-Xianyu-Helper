package renew

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"xianyu-go/internal/xianyu/protocol"
)

// MergeSetCookies 将 Set-Cookie 头合并到 Cookie 头字符串。只保留 name=value，
// 忽略 Path/Domain/Expires 等属性，因为后续出站请求只需要 Cookie header。
// MergeSetCookies 封装MergeSetCookies业务协调。
func MergeSetCookies(original string, setCookies []string) string {
	// cookies 用于本次流程后续判断的cookies
	cookies := protocol.TransCookies(original)
	// sc 表示当前遍历过程中的sc
	for _, sc := range setCookies {
		// parsed、err 用于本次流程后续判断的parsed、err
		parsed, err := http.ParseSetCookie(sc)
		if err != nil || strings.TrimSpace(parsed.Name) == "" {
			continue
		}
		if parsed.MaxAge < 0 || (parsed.MaxAge == 0 && !parsed.Expires.IsZero() && !parsed.Expires.After(time.Now())) {
			delete(cookies, parsed.Name)
		} else {
			cookies[parsed.Name] = parsed.Value
		}
	}
	return marshalCookies(cookies)
}

// ChangedCookieNames 返回 newCookies 相对 original 变化过的字段名，按字典序排序。
func ChangedCookieNames(original, newCookies string) []string {
	// oldMap 用于本次流程后续判断的oldMap
	oldMap := protocol.TransCookies(original)
	// newMap 用于本次流程后续判断的newMap
	newMap := protocol.TransCookies(newCookies)
	// changed 用于本次流程后续判断的changed
	changed := make([]string, 0)
	// seen 用于本次流程后续判断的seen
	seen := make(map[string]struct{}, len(oldMap)+len(newMap))
	// k 表示当前遍历过程中的k
	for k := range oldMap {
		seen[k] = struct{}{}
	}
	// k 表示当前遍历过程中的k
	for k := range newMap {
		seen[k] = struct{}{}
	}
	// k 表示当前遍历过程中的k
	for k := range seen {
		if oldMap[k] != newMap[k] {
			changed = append(changed, k)
		}
	}
	sort.Strings(changed)
	return changed
}

// marshalCookies 封装marshalCookies业务协调。
func marshalCookies(cookies map[string]string) string {
	// keys 用于本次流程后续判断的keys
	keys := make([]string, 0, len(cookies))
	// k 表示当前遍历过程中的k
	for k := range cookies {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	// parts 用于本次流程后续判断的parts
	parts := make([]string, 0, len(keys))
	// k 表示当前遍历过程中的k
	for _, k := range keys {
		parts = append(parts, k+"="+cookies[k])
	}
	return strings.Join(parts, "; ")
}
