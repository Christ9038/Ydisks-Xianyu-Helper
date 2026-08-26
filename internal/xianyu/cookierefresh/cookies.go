package cookierefresh

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

// metadataSnapshotKey 用于本次流程后续判断的metadataSnapshotKey
const (
	metadataSnapshotKey    = "cookies_refresh_snapshot"
	legacyMetadataSnapshot = "cookie_refresh_snapshot"
)

// BrowserCookie 保存浏览器返回的完整 Cookie 属性。
type BrowserCookie struct {
	Name         string  `json:"name"`
	Value        string  `json:"value"`
	Domain       string  `json:"domain,omitempty"`
	Path         string  `json:"path,omitempty"`
	Expires      float64 `json:"expires,omitempty"`
	HTTPOnly     bool    `json:"httpOnly,omitempty"`
	Secure       bool    `json:"secure,omitempty"`
	SameSite     string  `json:"sameSite,omitempty"`
	PartitionKey string  `json:"partitionKey,omitempty"`
}

// ParseCookieString 把 Cookie 头解析为 name -> value。
func ParseCookieString(s string) map[string]string {
	// out 用于本次流程后续判断的out
	out := make(map[string]string)
	// part 表示当前遍历过程中的part
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// eq 用于本次流程后续判断的eq
		eq := strings.Index(part, "=")
		if eq <= 0 {
			continue
		}
		out[strings.TrimSpace(part[:eq])] = strings.TrimSpace(part[eq+1:])
	}
	return out
}

// MarshalCookieString 以稳定顺序把 Cookie map 拼回 Cookie 头。
func MarshalCookieString(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	// keys 用于本次流程后续判断的keys
	keys := make([]string, 0, len(m))
	// k 表示当前遍历过程中的k
	for k := range m {
		if strings.TrimSpace(k) != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	// parts 用于本次流程后续判断的parts
	parts := make([]string, 0, len(keys))
	// k 表示当前遍历过程中的k
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, "; ")
}

// MergeSetCookies 将 Set-Cookie 响应头合并进原 Cookie 字符串。
func MergeSetCookies(original string, setCookies []string) string {
	// m 用于本次流程后续判断的m
	m := ParseCookieString(original)
	// raw 表示当前遍历过程中的原始
	for _, raw := range setCookies {
		// first 用于本次流程后续判断的first
		first := strings.TrimSpace(strings.Split(raw, ";")[0])
		// eq 用于本次流程后续判断的eq
		eq := strings.Index(first, "=")
		if eq <= 0 {
			continue
		}
		// name 用于本次流程后续判断的名称
		name := strings.TrimSpace(first[:eq])
		m[name] = strings.TrimSpace(first[eq+1:])
	}
	return MarshalCookieString(m)
}

// SnapshotFromCookieString 为只有扁平 Cookie 的历史账号建立兼容快照。浏览器刷新后
// 应使用真实快照覆盖它，避免长期依赖推断出的 Domain/Path。
// SnapshotFromCookieString 封装SnapshotFrom登录凭证String业务协调。
func SnapshotFromCookieString(cookieString, domain string) []BrowserCookie {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		domain = ".goofish.com"
	}
	// values 用于本次流程后续判断的values
	values := ParseCookieString(cookieString)
	// names 用于本次流程后续判断的names
	names := make([]string, 0, len(values))
	// name 表示当前遍历过程中的名称
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	// out 用于本次流程后续判断的out
	out := make([]BrowserCookie, 0, len(names))
	// name 表示当前遍历过程中的名称
	for _, name := range names {
		// value 用于本次流程后续判断的值
		value := values[name]
		out = append(out, BrowserCookie{Name: name, Value: value, Domain: domain, Path: "/", Secure: true})
	}
	return NormalizeSnapshot(out)
}

// ReconcileSnapshotWithCookieString 在调用方暂时只能获得扁平 Cookie 结果时保留
// 已知属性，并同步值与删除项。新增字段使用 .goofish.com 根路径作为兼容作用域。
// ReconcileSnapshotWithCookieString 封装ReconcileSnapshotWith登录凭证String业务协调。
func ReconcileSnapshotWithCookieString(snapshot []BrowserCookie, cookieString string) []BrowserCookie {
	// values 用于本次流程后续判断的values
	values := ParseCookieString(cookieString)
	// counts 用于本次流程后续判断的counts
	counts := make(map[string]int, len(snapshot))
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range NormalizeSnapshot(snapshot) {
		counts[cookie.Name]++
	}
	// seen 用于本次流程后续判断的seen
	seen := make(map[string]struct{})
	// out 用于本次流程后续判断的out
	out := make([]BrowserCookie, 0, len(snapshot)+len(values))
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range NormalizeSnapshot(snapshot) {
		// value、exists 用于本次流程后续判断的value、exists
		value, exists := values[cookie.Name]
		if !exists {
			continue
		}
		// 扁平 Cookie 字符串无法表达同名 Cookie 的 Domain/Path/PartitionKey。
		// 只有名称唯一时才可确定其新值；否则保留各作用域原值，避免把一个值
		// 错误覆盖到完整 Jar 的所有同名项。
		if counts[cookie.Name] == 1 {
			cookie.Value = value
		}
		out = append(out, cookie)
		seen[cookie.Name] = struct{}{}
	}
	// names 用于本次流程后续判断的names
	names := make([]string, 0, len(values))
	// name 表示当前遍历过程中的名称
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	// name 表示当前遍历过程中的名称
	for _, name := range names {
		if // exists 用于本次流程后续判断的exists
		_, exists := seen[name]; exists {
			continue
		}
		// value 用于本次流程后续判断的值
		value := values[name]
		out = append(out, BrowserCookie{Name: name, Value: value, Domain: ".goofish.com", Path: "/", Secure: true})
	}
	return NormalizeSnapshot(out)
}

// CookieHeaderForURL 按浏览器 Domain/Path/Secure/Expires 规则生成指定 URL 的
// Cookie header，并保留不同路径下的同名 Cookie。
// CookieHeaderForURL 封装登录凭证HeaderForURL业务协调。
func CookieHeaderForURL(snapshot []BrowserCookie, rawURL string, now time.Time) string {
	// header 用于本次流程后续判断的header
	header, _ := ScopedCookieHeaderForURL(snapshot, rawURL, now)
	return header
}

// ScopedCookieHeaderForURL 按浏览器匹配规则生成 Cookie header。第二个返回值
// 表示调用方提供了权威快照且 URL 有效；即使没有任何 Cookie 匹配，也会返回
// ("", true)，让调用方能区分“明确为空”和“没有快照，需要兼容回退”。
//
// 此简化入口不具备顶级站点上下文，因此不会发送分区 Cookie。需要处理 CHIPS
// 时应使用 ScopedCookieHeaderForRequest 并传入浏览器提供的 PartitionKey。
// ScopedCookieHeaderForURL 封装Scoped登录凭证HeaderForURL业务协调。
func ScopedCookieHeaderForURL(snapshot []BrowserCookie, rawURL string, now time.Time) (string, bool) {
	return ScopedCookieHeaderForRequest(snapshot, rawURL, "", now)
}

// ScopedCookieHeaderForRequest 为带顶级站点 PartitionKey 的请求生成 Cookie header。
func ScopedCookieHeaderForRequest(snapshot []BrowserCookie, rawURL, partitionKey string, now time.Time) (string, bool) {
	if snapshot == nil {
		return "", false
	}
	// target、err 用于本次流程后续判断的target、err
	target, err := url.Parse(rawURL)
	if err != nil || target.Hostname() == "" {
		return "", false
	}
	// matchedCookie 用于本次流程后续判断的matched登录凭证
	type matchedCookie struct {
		cookie BrowserCookie
		index  int
	}
	// matched 用于本次流程后续判断的matched
	matched := make([]matchedCookie, 0, len(snapshot))
	// index、cookie 表示当前遍历过程中的index、cookie
	for index, cookie := range NormalizeSnapshot(snapshot) {
		// Unpartitioned cookies are always eligible. Partitioned cookies are
		// additionally sent when their key matches the current top-level site.
		if cookie.PartitionKey != "" && cookie.PartitionKey != partitionKey {
			continue
		}
		if cookie.Expires > 0 && cookie.Expires <= float64(now.Unix()) {
			continue
		}
		if cookie.Secure && target.Scheme != "https" && target.Scheme != "wss" {
			continue
		}
		if !cookieDomainMatches(target.Hostname(), cookie.Domain) || !cookiePathMatches(target.EscapedPath(), cookie.Path) {
			continue
		}
		matched = append(matched, matchedCookie{cookie: cookie, index: index})
	}
	sort.SliceStable(matched, func(i, j int) bool {
		if len(matched[i].cookie.Path) != len(matched[j].cookie.Path) {
			return len(matched[i].cookie.Path) > len(matched[j].cookie.Path)
		}
		return matched[i].index < matched[j].index
	})
	// parts 用于本次流程后续判断的parts
	parts := make([]string, 0, len(matched))
	// item 表示当前遍历过程中的商品
	for _, item := range matched {
		parts = append(parts, item.cookie.Name+"="+item.cookie.Value)
	}
	return strings.Join(parts, "; "), true
}

// ApplySetCookies 把某次请求响应的 Set-Cookie 应用到完整快照。删除操作只删除
// 相同 name/domain/path 的 Cookie，不会误删其他作用域下的同名项。
// ApplySetCookies 封装ApplySetCookies业务协调。
func ApplySetCookies(snapshot []BrowserCookie, requestURL string, setCookies []string, now time.Time, partitionKeys ...string) []BrowserCookie {
	// target、err 用于本次流程后续判断的target、err
	target, err := url.Parse(requestURL)
	if err != nil || target.Hostname() == "" {
		return NormalizeSnapshot(snapshot)
	}
	// Cookie identity is name/domain/path/partition key, while header ordering for
	// equal paths follows creation time. Keep the input position when replacing an
	// existing cookie; if it is deleted and later recreated, append it as a new
	// creation, just like Chromium's cookie store.
	// state 用于本次流程后续判断的状态
	state := make([]BrowserCookie, 0, len(snapshot)+len(setCookies))
	// positions 用于本次流程后续判断的positions
	positions := make(map[string]int, len(snapshot)+len(setCookies))
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range NormalizeSnapshot(snapshot) {
		// key 用于本次流程后续判断的key
		key := snapshotKey(cookie)
		if // index、exists 用于本次流程后续判断的index、exists
		index, exists := positions[key]; exists {
			state[index] = cookie
			continue
		}
		positions[key] = len(state)
		state = append(state, cookie)
	}
	// raw 表示当前遍历过程中的原始
	for _, raw := range setCookies {
		// parsed、err 用于本次流程后续判断的parsed、err
		parsed, err := http.ParseSetCookie(raw)
		if err != nil || strings.TrimSpace(parsed.Name) == "" {
			continue
		}
		// rawDomain 用于本次流程后续判断的原始Domain
		rawDomain := strings.TrimSpace(parsed.Domain)
		// domain 用于本次流程后续判断的domain
		domain := strings.ToLower(rawDomain)
		if domain == "" {
			domain = strings.ToLower(target.Hostname())
		} else {
			// Domain 属性无论是否带前导点都表示域 Cookie；前导点按
			// RFC 6265 应被忽略。不能复用已存 Cookie 的 host-only
			// 匹配规则，否则 Domain=goofish.com 会被错误拒绝。
			if !cookieDomainAttributeMatches(strings.ToLower(target.Hostname()), domain) {
				// Chromium rejects a Domain attribute unrelated to the response host.
				continue
			}
			if !strings.HasPrefix(domain, ".") {
				domain = "." + domain
			}
		}
		// cookiePath 用于本次流程后续判断的登录凭证路径
		cookiePath := parsed.Path
		if cookiePath == "" {
			cookiePath = defaultCookiePath(target.Path)
		}
		if parsed.SameSite == http.SameSiteNoneMode && !parsed.Secure {
			// Chromium rejects SameSite=None cookies unless they are also Secure.
			continue
		}
		if strings.HasPrefix(parsed.Name, "__Secure-") && !parsed.Secure {
			continue
		}
		if strings.HasPrefix(parsed.Name, "__Host-") && (!parsed.Secure || rawDomain != "" || parsed.Path != "/") {
			// __Host- requires Secure, an explicit Path=/, and no Domain attribute.
			continue
		}
		// partitionKey 用于本次流程后续判断的partitionKey
		partitionKey := ""
		if parsed.Partitioned {
			if !parsed.Secure || len(partitionKeys) == 0 || strings.TrimSpace(partitionKeys[0]) == "" {
				// Without the top-level site key this cookie cannot be represented
				// faithfully; never turn it into an unpartitioned cookie.
				continue
			}
			partitionKey = strings.TrimSpace(partitionKeys[0])
		}
		// cookie 用于本次流程后续判断的登录凭证
		cookie := BrowserCookie{
			Name: parsed.Name, Value: parsed.Value, Domain: domain, Path: cookiePath,
			HTTPOnly: parsed.HttpOnly, Secure: parsed.Secure, SameSite: sameSiteLabel(parsed.SameSite), PartitionKey: partitionKey,
		}
		if parsed.MaxAge > 0 {
			// RFC 6265: Max-Age 优先于 Expires，并从收到响应的时刻计算。
			cookie.Expires = float64(now.Add(time.Duration(parsed.MaxAge) * time.Second).Unix())
		} else if !parsed.Expires.IsZero() {
			cookie.Expires = float64(parsed.Expires.Unix())
		}
		// key 用于本次流程后续判断的key
		key := snapshotKey(cookie)
		if parsed.MaxAge < 0 || (parsed.MaxAge == 0 && !parsed.Expires.IsZero() && !parsed.Expires.After(now)) {
			if // index、exists 用于本次流程后续判断的index、exists
			index, exists := positions[key]; exists {
				state = append(state[:index], state[index+1:]...)
				delete(positions, key)
				for // i 用于本次流程后续判断的i
				i := index; i < len(state); i++ {
					positions[snapshotKey(state[i])] = i
				}
			}
			continue
		}
		if // index、exists 用于本次流程后续判断的index、exists
		index, exists := positions[key]; exists {
			state[index] = cookie
			continue
		}
		positions[key] = len(state)
		state = append(state, cookie)
	}
	return NormalizeSnapshot(state)
}

// snapshotKey 封装snapshotKey业务协调。
func snapshotKey(cookie BrowserCookie) string {
	return cookie.Name + "\x00" + strings.ToLower(cookie.Domain) + "\x00" + cookie.Path + "\x00" + cookie.PartitionKey
}

// cookieDomainAttributeMatches 封装登录凭证DomainAttributeMatches业务协调。
func cookieDomainAttributeMatches(host, domain string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	// base 用于本次流程后续判断的base
	base := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), ".")
	return base != "" && (host == base || strings.HasSuffix(host, "."+base))
}

// cookieDomainMatches 封装登录凭证DomainMatches业务协调。
func cookieDomainMatches(host, domain string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return false
	}
	if strings.HasPrefix(domain, ".") {
		// base 用于本次流程后续判断的base
		base := strings.TrimPrefix(domain, ".")
		return host == base || strings.HasSuffix(host, "."+base)
	}
	return host == domain
}

// cookiePathMatches 封装登录凭证路径Matches业务协调。
func cookiePathMatches(requestPath, cookiePath string) bool {
	if requestPath == "" {
		requestPath = "/"
	}
	if cookiePath == "" {
		cookiePath = "/"
	}
	if requestPath == cookiePath {
		return true
	}
	if !strings.HasPrefix(requestPath, cookiePath) {
		return false
	}
	return strings.HasSuffix(cookiePath, "/") || (len(requestPath) > len(cookiePath) && requestPath[len(cookiePath)] == '/')
}

// defaultCookiePath 封装default登录凭证路径业务协调。
func defaultCookiePath(requestPath string) string {
	if requestPath == "" || requestPath[0] != '/' || requestPath == "/" {
		return "/"
	}
	// dir 用于本次流程后续判断的dir
	dir := path.Dir(requestPath)
	if dir == "." || dir == "/" {
		return "/"
	}
	return dir
}

// sameSiteLabel 封装sameSiteLabel业务协调。
func sameSiteLabel(value http.SameSite) string {
	switch value {
	case http.SameSiteStrictMode:
		return "Strict"
	case http.SameSiteLaxMode:
		return "Lax"
	case http.SameSiteNoneMode:
		return "None"
	default:
		return ""
	}
}

// ChangedCookieNames 返回两个 Cookie 字符串之间发生变化的字段名。
func ChangedCookieNames(before, after string) []string {
	// a 用于本次流程后续判断的a
	a := ParseCookieString(before)
	// b 用于本次流程后续判断的b
	b := ParseCookieString(after)
	// seen 用于本次流程后续判断的seen
	seen := make(map[string]struct{}, len(a)+len(b))
	// k 表示当前遍历过程中的k
	for k := range a {
		seen[k] = struct{}{}
	}
	// k 表示当前遍历过程中的k
	for k := range b {
		seen[k] = struct{}{}
	}
	// names 用于本次流程后续判断的names
	names := make([]string, 0, len(seen))
	// k 表示当前遍历过程中的k
	for k := range seen {
		if a[k] != b[k] {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	return names
}

// ChangedSnapshotLabels 返回完整浏览器 Cookie 快照变化标签，格式 name@domain/path。
func ChangedSnapshotLabels(before, after []BrowserCookie) []string {
	// key 用于本次流程后续判断的key
	key := func(c BrowserCookie) string {
		// path 用于本次流程后续判断的路径
		path := c.Path
		return c.Name + "|" + c.Domain + "|" + path + "|" + c.PartitionKey
	}
	// label 用于本次流程后续判断的label
	label := func(c BrowserCookie) string {
		// path 用于本次流程后续判断的路径
		path := c.Path
		if c.Domain != "" {
			// out 用于本次流程后续判断的out
			out := c.Name + "@" + c.Domain + path
			if c.PartitionKey != "" {
				out += "#" + c.PartitionKey
			}
			return out
		}
		return c.Name
	}
	// oldMap 用于本次流程后续判断的oldMap
	oldMap := make(map[string]BrowserCookie)
	// newMap 用于本次流程后续判断的newMap
	newMap := make(map[string]BrowserCookie)
	// c 表示当前遍历过程中的c
	for _, c := range NormalizeSnapshot(before) {
		oldMap[key(c)] = c
	}
	// c 表示当前遍历过程中的c
	for _, c := range NormalizeSnapshot(after) {
		newMap[key(c)] = c
	}
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
	// labels 用于本次流程后续判断的labels
	labels := make([]string, 0, len(seen))
	// k 表示当前遍历过程中的k
	for k := range seen {
		// old 用于本次流程后续判断的old
		old := oldMap[k]
		// newCookie 用于本次流程后续判断的new登录凭证
		newCookie := newMap[k]
		if old == newCookie {
			continue
		}
		if newCookie.Name != "" {
			labels = append(labels, label(newCookie))
		} else if old.Name != "" {
			labels = append(labels, label(old))
		}
	}
	sort.Strings(labels)
	return labels
}

// CookieStringFromSnapshot 将浏览器 Cookie 快照压成请求 Cookie 字符串。
func CookieStringFromSnapshot(cookies []BrowserCookie) string {
	// parts 用于本次流程后续判断的parts
	parts := make([]string, 0, len(cookies))
	// c 表示当前遍历过程中的c
	for _, c := range NormalizeSnapshot(cookies) {
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

// MergeOriginalFields 补回浏览器未返回但原 Cookie 中存在的字段。
func MergeOriginalFields(original, browserCookieString string) string {
	// m 用于本次流程后续判断的m
	m := ParseCookieString(original)
	// k、v 表示当前遍历过程中的k、v
	for k, v := range ParseCookieString(browserCookieString) {
		m[k] = v
	}
	return MarshalCookieString(m)
}

// NormalizeSnapshot 补齐默认 path 并保留浏览器返回的创建顺序。Cookie
// header 在 Path 长度相同时必须沿用该顺序，因此不能在这里按名称重排。
// NormalizeSnapshot 归一化Snapshot。
func NormalizeSnapshot(cookies []BrowserCookie) []BrowserCookie {
	if cookies == nil {
		return nil
	}
	// out 用于本次流程后续判断的out
	out := make([]BrowserCookie, 0, len(cookies))
	// c 表示当前遍历过程中的c
	for _, c := range cookies {
		if c.Name == "" {
			continue
		}
		if c.Path == "" {
			c.Path = "/"
		}
		out = append(out, c)
	}
	return out
}

// SnapshotFromMetadata 从 cookies.metadata_json 中读取浏览器 Cookie 快照。
func SnapshotFromMetadata(metadata string) []BrowserCookie {
	// out 用于本次流程后续判断的out
	out, _ := SnapshotFromMetadataOK(metadata)
	return out
}

// SnapshotFromMetadataOK 读取完整 Cookie 快照，并报告 metadata 中是否真的
// 存在快照键。存在空数组时返回非 nil 空切片和 true，避免被误当成历史账号
// 而回退到扁平 Cookie。
// SnapshotFromMetadataOK 封装SnapshotFromMetadataOK业务协调。
func SnapshotFromMetadataOK(metadata string) ([]BrowserCookie, bool) {
	// m 用于本次流程后续判断的m
	var m map[string]json.RawMessage
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal([]byte(strings.TrimSpace(metadata)), &m); err != nil {
		return nil, false
	}
	// out 用于本次流程后续判断的out
	var out []BrowserCookie
	if // raw 用于本次流程后续判断的原始
	raw := m[metadataSnapshotKey]; len(raw) > 0 {
		if // err 用于本次流程后续判断的err
		err := json.Unmarshal(raw, &out); err != nil {
			return nil, false
		}
	} else if // raw 用于本次流程后续判断的原始
	raw := m[legacyMetadataSnapshot]; len(raw) > 0 {
		if // err 用于本次流程后续判断的err
		err := json.Unmarshal(raw, &out); err != nil {
			return nil, false
		}
	} else {
		return nil, false
	}
	if out == nil {
		out = []BrowserCookie{}
	}
	return NormalizeSnapshot(out), true
}

// MetadataWithSnapshot 写入浏览器 Cookie 快照，保留 metadata 中的其他键。
func MetadataWithSnapshot(metadata string, cookies []BrowserCookie) string {
	// m 用于本次流程后续判断的m
	m := make(map[string]any)
	if strings.TrimSpace(metadata) != "" {
		_ = json.Unmarshal([]byte(metadata), &m)
	}
	delete(m, legacyMetadataSnapshot)
	m[metadataSnapshotKey] = NormalizeSnapshot(cookies)
	// b 保存已合并 Cookie 快照的 metadata JSON；m 来自 JSON 或固定可序列化字段，编码不会失败。
	b, _ := json.Marshal(m)
	return string(b)
}

// MetadataWithoutSnapshot 清除浏览器 Cookie 快照。
func MetadataWithoutSnapshot(metadata string) string {
	if strings.TrimSpace(metadata) == "" {
		return ""
	}
	// m 用于本次流程后续判断的m
	m := make(map[string]any)
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal([]byte(metadata), &m); err != nil {
		return metadata
	}
	delete(m, metadataSnapshotKey)
	delete(m, legacyMetadataSnapshot)
	// b 保存移除 Cookie 快照后的 metadata JSON；m 只包含 JSON 可表达的值。
	b, _ := json.Marshal(m)
	return string(b)
}
