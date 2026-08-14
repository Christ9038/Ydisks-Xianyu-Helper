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

// metadataSnapshotKey 保存metadataSnapshotKey，供当前处理流程使用
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
	// out 保存out，供当前处理流程使用
	out := make(map[string]string)
	// part 表示当前遍历过程中的part
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// eq 保存eq，供当前处理流程使用
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
	// keys 保存keys，供当前处理流程使用
	keys := make([]string, 0, len(m))
	// k 表示当前遍历过程中的k
	for k := range m {
		if strings.TrimSpace(k) != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	// parts 保存parts，供当前处理流程使用
	parts := make([]string, 0, len(keys))
	// k 表示当前遍历过程中的k
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, "; ")
}

// MergeSetCookies 将 Set-Cookie 响应头合并进原 Cookie 字符串。
func MergeSetCookies(original string, setCookies []string) string {
	// m 保存m，供当前处理流程使用
	m := ParseCookieString(original)
	// raw 表示当前遍历过程中的原始
	for _, raw := range setCookies {
		// first 保存first，供当前处理流程使用
		first := strings.TrimSpace(strings.Split(raw, ";")[0])
		// eq 保存eq，供当前处理流程使用
		eq := strings.Index(first, "=")
		if eq <= 0 {
			continue
		}
		// name 保存名称，供当前处理流程使用
		name := strings.TrimSpace(first[:eq])
		if name == "" {
			continue
		}
		m[name] = strings.TrimSpace(first[eq+1:])
	}
	return MarshalCookieString(m)
}

// SnapshotFromCookieString 为只有扁平 Cookie 的历史账号建立兼容快照。浏览器刷新后
// 应使用真实快照覆盖它，避免长期依赖推断出的 Domain/Path。
// SnapshotFromCookieString 负责SnapshotFrom登录凭证String相关处理。
func SnapshotFromCookieString(cookieString, domain string) []BrowserCookie {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		domain = ".goofish.com"
	}
	// values 保存values，供当前处理流程使用
	values := ParseCookieString(cookieString)
	// names 保存names，供当前处理流程使用
	names := make([]string, 0, len(values))
	// name 表示当前遍历过程中的名称
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	// out 保存out，供当前处理流程使用
	out := make([]BrowserCookie, 0, len(names))
	// name 表示当前遍历过程中的名称
	for _, name := range names {
		// value 保存值，供当前处理流程使用
		value := values[name]
		out = append(out, BrowserCookie{Name: name, Value: value, Domain: domain, Path: "/", Secure: true})
	}
	return NormalizeSnapshot(out)
}

// ReconcileSnapshotWithCookieString 在调用方暂时只能获得扁平 Cookie 结果时保留
// 已知属性，并同步值与删除项。新增字段使用 .goofish.com 根路径作为兼容作用域。
// ReconcileSnapshotWithCookieString 负责ReconcileSnapshotWith登录凭证String相关处理。
func ReconcileSnapshotWithCookieString(snapshot []BrowserCookie, cookieString string) []BrowserCookie {
	// values 保存values，供当前处理流程使用
	values := ParseCookieString(cookieString)
	// counts 保存counts，供当前处理流程使用
	counts := make(map[string]int, len(snapshot))
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range NormalizeSnapshot(snapshot) {
		counts[cookie.Name]++
	}
	// seen 保存seen，供当前处理流程使用
	seen := make(map[string]struct{})
	// out 保存out，供当前处理流程使用
	out := make([]BrowserCookie, 0, len(snapshot)+len(values))
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range NormalizeSnapshot(snapshot) {
		// value、exists 保存value、exists，供当前处理流程使用
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
	// names 保存names，供当前处理流程使用
	names := make([]string, 0, len(values))
	// name 表示当前遍历过程中的名称
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	// name 表示当前遍历过程中的名称
	for _, name := range names {
		if // exists 保存exists，供当前处理流程使用
		_, exists := seen[name]; exists {
			continue
		}
		// value 保存值，供当前处理流程使用
		value := values[name]
		out = append(out, BrowserCookie{Name: name, Value: value, Domain: ".goofish.com", Path: "/", Secure: true})
	}
	return NormalizeSnapshot(out)
}

// CookieHeaderForURL 按浏览器 Domain/Path/Secure/Expires 规则生成指定 URL 的
// Cookie header，并保留不同路径下的同名 Cookie。
// CookieHeaderForURL 负责登录凭证HeaderForURL相关处理。
func CookieHeaderForURL(snapshot []BrowserCookie, rawURL string, now time.Time) string {
	// header 保存header，供当前处理流程使用
	header, _ := ScopedCookieHeaderForURL(snapshot, rawURL, now)
	return header
}

// ScopedCookieHeaderForURL 按浏览器匹配规则生成 Cookie header。第二个返回值
// 表示调用方提供了权威快照且 URL 有效；即使没有任何 Cookie 匹配，也会返回
// ("", true)，让调用方能区分“明确为空”和“没有快照，需要兼容回退”。
//
// 此简化入口不具备顶级站点上下文，因此不会发送分区 Cookie。需要处理 CHIPS
// 时应使用 ScopedCookieHeaderForRequest 并传入浏览器提供的 PartitionKey。
// ScopedCookieHeaderForURL 负责Scoped登录凭证HeaderForURL相关处理。
func ScopedCookieHeaderForURL(snapshot []BrowserCookie, rawURL string, now time.Time) (string, bool) {
	return ScopedCookieHeaderForRequest(snapshot, rawURL, "", now)
}

// ScopedCookieHeaderForRequest 为带顶级站点 PartitionKey 的请求生成 Cookie header。
func ScopedCookieHeaderForRequest(snapshot []BrowserCookie, rawURL, partitionKey string, now time.Time) (string, bool) {
	if snapshot == nil {
		return "", false
	}
	// target、err 保存target、err，供当前处理流程使用
	target, err := url.Parse(rawURL)
	if err != nil || target.Hostname() == "" {
		return "", false
	}
	// matchedCookie 保存matched登录凭证，供当前处理流程使用
	type matchedCookie struct {
		cookie BrowserCookie
		index  int
	}
	// matched 保存matched，供当前处理流程使用
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
	// parts 保存parts，供当前处理流程使用
	parts := make([]string, 0, len(matched))
	// item 表示当前遍历过程中的商品
	for _, item := range matched {
		parts = append(parts, item.cookie.Name+"="+item.cookie.Value)
	}
	return strings.Join(parts, "; "), true
}

// ApplySetCookies 把某次请求响应的 Set-Cookie 应用到完整快照。删除操作只删除
// 相同 name/domain/path 的 Cookie，不会误删其他作用域下的同名项。
// ApplySetCookies 负责ApplySetCookies相关处理。
func ApplySetCookies(snapshot []BrowserCookie, requestURL string, setCookies []string, now time.Time, partitionKeys ...string) []BrowserCookie {
	// target、err 保存target、err，供当前处理流程使用
	target, err := url.Parse(requestURL)
	if err != nil || target.Hostname() == "" {
		return NormalizeSnapshot(snapshot)
	}
	// Cookie identity is name/domain/path/partition key, while header ordering for
	// equal paths follows creation time. Keep the input position when replacing an
	// existing cookie; if it is deleted and later recreated, append it as a new
	// creation, just like Chromium's cookie store.
	// state 保存状态，供当前处理流程使用
	state := make([]BrowserCookie, 0, len(snapshot)+len(setCookies))
	// positions 保存positions，供当前处理流程使用
	positions := make(map[string]int, len(snapshot)+len(setCookies))
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range NormalizeSnapshot(snapshot) {
		// key 保存key，供当前处理流程使用
		key := snapshotKey(cookie)
		if // index、exists 保存index、exists，供当前处理流程使用
		index, exists := positions[key]; exists {
			state[index] = cookie
			continue
		}
		positions[key] = len(state)
		state = append(state, cookie)
	}
	// raw 表示当前遍历过程中的原始
	for _, raw := range setCookies {
		// parsed、err 保存parsed、err，供当前处理流程使用
		parsed, err := http.ParseSetCookie(raw)
		if err != nil || strings.TrimSpace(parsed.Name) == "" {
			continue
		}
		// rawDomain 保存原始Domain，供当前处理流程使用
		rawDomain := strings.TrimSpace(parsed.Domain)
		// domain 保存domain，供当前处理流程使用
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
		// cookiePath 保存登录凭证路径，供当前处理流程使用
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
		// partitionKey 保存partitionKey，供当前处理流程使用
		partitionKey := ""
		if parsed.Partitioned {
			if !parsed.Secure || len(partitionKeys) == 0 || strings.TrimSpace(partitionKeys[0]) == "" {
				// Without the top-level site key this cookie cannot be represented
				// faithfully; never turn it into an unpartitioned cookie.
				continue
			}
			partitionKey = strings.TrimSpace(partitionKeys[0])
		}
		// cookie 保存登录凭证，供当前处理流程使用
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
		// key 保存key，供当前处理流程使用
		key := snapshotKey(cookie)
		if parsed.MaxAge < 0 || (parsed.MaxAge == 0 && !parsed.Expires.IsZero() && !parsed.Expires.After(now)) {
			if // index、exists 保存index、exists，供当前处理流程使用
			index, exists := positions[key]; exists {
				state = append(state[:index], state[index+1:]...)
				delete(positions, key)
				for // i 保存i，供当前处理流程使用
				i := index; i < len(state); i++ {
					positions[snapshotKey(state[i])] = i
				}
			}
			continue
		}
		if // index、exists 保存index、exists，供当前处理流程使用
		index, exists := positions[key]; exists {
			state[index] = cookie
			continue
		}
		positions[key] = len(state)
		state = append(state, cookie)
	}
	return NormalizeSnapshot(state)
}

// snapshotKey 负责snapshotKey相关处理。
func snapshotKey(cookie BrowserCookie) string {
	return cookie.Name + "\x00" + strings.ToLower(cookie.Domain) + "\x00" + cookie.Path + "\x00" + cookie.PartitionKey
}

// cookieDomainAttributeMatches 负责登录凭证DomainAttributeMatches相关处理。
func cookieDomainAttributeMatches(host, domain string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	// base 保存base，供当前处理流程使用
	base := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), ".")
	return base != "" && (host == base || strings.HasSuffix(host, "."+base))
}

// cookieDomainMatches 负责登录凭证DomainMatches相关处理。
func cookieDomainMatches(host, domain string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return false
	}
	if strings.HasPrefix(domain, ".") {
		// base 保存base，供当前处理流程使用
		base := strings.TrimPrefix(domain, ".")
		return host == base || strings.HasSuffix(host, "."+base)
	}
	return host == domain
}

// cookiePathMatches 负责登录凭证路径Matches相关处理。
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

// defaultCookiePath 负责default登录凭证路径相关处理。
func defaultCookiePath(requestPath string) string {
	if requestPath == "" || requestPath[0] != '/' || requestPath == "/" {
		return "/"
	}
	// dir 保存dir，供当前处理流程使用
	dir := path.Dir(requestPath)
	if dir == "." || dir == "/" {
		return "/"
	}
	return dir
}

// sameSiteLabel 负责sameSiteLabel相关处理。
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
	// a 保存a，供当前处理流程使用
	a := ParseCookieString(before)
	// b 保存b，供当前处理流程使用
	b := ParseCookieString(after)
	// seen 保存seen，供当前处理流程使用
	seen := make(map[string]struct{}, len(a)+len(b))
	// k 表示当前遍历过程中的k
	for k := range a {
		seen[k] = struct{}{}
	}
	// k 表示当前遍历过程中的k
	for k := range b {
		seen[k] = struct{}{}
	}
	// names 保存names，供当前处理流程使用
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
	// key 保存key，供当前处理流程使用
	key := func(c BrowserCookie) string {
		// path 保存路径，供当前处理流程使用
		path := c.Path
		if path == "" {
			path = "/"
		}
		return c.Name + "|" + c.Domain + "|" + path + "|" + c.PartitionKey
	}
	// label 保存label，供当前处理流程使用
	label := func(c BrowserCookie) string {
		// path 保存路径，供当前处理流程使用
		path := c.Path
		if path == "" {
			path = "/"
		}
		if c.Domain != "" {
			// out 保存out，供当前处理流程使用
			out := c.Name + "@" + c.Domain + path
			if c.PartitionKey != "" {
				out += "#" + c.PartitionKey
			}
			return out
		}
		return c.Name
	}
	// oldMap 保存oldMap，供当前处理流程使用
	oldMap := make(map[string]BrowserCookie)
	// newMap 保存newMap，供当前处理流程使用
	newMap := make(map[string]BrowserCookie)
	// c 表示当前遍历过程中的c
	for _, c := range NormalizeSnapshot(before) {
		oldMap[key(c)] = c
	}
	// c 表示当前遍历过程中的c
	for _, c := range NormalizeSnapshot(after) {
		newMap[key(c)] = c
	}
	// seen 保存seen，供当前处理流程使用
	seen := make(map[string]struct{}, len(oldMap)+len(newMap))
	// k 表示当前遍历过程中的k
	for k := range oldMap {
		seen[k] = struct{}{}
	}
	// k 表示当前遍历过程中的k
	for k := range newMap {
		seen[k] = struct{}{}
	}
	// labels 保存labels，供当前处理流程使用
	labels := make([]string, 0, len(seen))
	// k 表示当前遍历过程中的k
	for k := range seen {
		// old 保存old，供当前处理流程使用
		old := oldMap[k]
		// newCookie 保存new登录凭证，供当前处理流程使用
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
	// parts 保存parts，供当前处理流程使用
	parts := make([]string, 0, len(cookies))
	// c 表示当前遍历过程中的c
	for _, c := range NormalizeSnapshot(cookies) {
		if c.Name == "" {
			continue
		}
		parts = append(parts, c.Name+"="+c.Value)
	}
	return strings.Join(parts, "; ")
}

// MergeOriginalFields 补回浏览器未返回但原 Cookie 中存在的字段。
func MergeOriginalFields(original, browserCookieString string) string {
	// m 保存m，供当前处理流程使用
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
	// out 保存out，供当前处理流程使用
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
	// out 保存out，供当前处理流程使用
	out, _ := SnapshotFromMetadataOK(metadata)
	return out
}

// SnapshotFromMetadataOK 读取完整 Cookie 快照，并报告 metadata 中是否真的
// 存在快照键。存在空数组时返回非 nil 空切片和 true，避免被误当成历史账号
// 而回退到扁平 Cookie。
// SnapshotFromMetadataOK 负责SnapshotFromMetadataOK相关处理。
func SnapshotFromMetadataOK(metadata string) ([]BrowserCookie, bool) {
	// m 保存m，供当前处理流程使用
	var m map[string]json.RawMessage
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal([]byte(strings.TrimSpace(metadata)), &m); err != nil {
		return nil, false
	}
	// out 保存out，供当前处理流程使用
	var out []BrowserCookie
	if // raw 保存原始，供当前处理流程使用
	raw := m[metadataSnapshotKey]; len(raw) > 0 {
		if // err 保存err，供当前处理流程使用
		err := json.Unmarshal(raw, &out); err != nil {
			return nil, false
		}
	} else if // raw 保存原始，供当前处理流程使用
	raw := m[legacyMetadataSnapshot]; len(raw) > 0 {
		if // err 保存err，供当前处理流程使用
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
	// m 保存m，供当前处理流程使用
	m := make(map[string]any)
	if strings.TrimSpace(metadata) != "" {
		_ = json.Unmarshal([]byte(metadata), &m)
	}
	delete(m, legacyMetadataSnapshot)
	m[metadataSnapshotKey] = NormalizeSnapshot(cookies)
	// b、err 保存b、err，供当前处理流程使用
	b, err := json.Marshal(m)
	if err != nil {
		return metadata
	}
	return string(b)
}

// MetadataWithoutSnapshot 清除浏览器 Cookie 快照。
func MetadataWithoutSnapshot(metadata string) string {
	if strings.TrimSpace(metadata) == "" {
		return ""
	}
	// m 保存m，供当前处理流程使用
	m := make(map[string]any)
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal([]byte(metadata), &m); err != nil {
		return metadata
	}
	delete(m, metadataSnapshotKey)
	delete(m, legacyMetadataSnapshot)
	// b、err 保存b、err，供当前处理流程使用
	b, err := json.Marshal(m)
	if err != nil {
		return metadata
	}
	return string(b)
}
