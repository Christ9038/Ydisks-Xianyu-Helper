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

const (
	metadataSnapshotKey    = "cookies_refresh_snapshot"
	legacyMetadataSnapshot = "cookie_refresh_snapshot"
)

// BrowserCookie 保存浏览器返回的完整 Cookie 属性。
type BrowserCookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain,omitempty"`
	Path     string  `json:"path,omitempty"`
	Expires  float64 `json:"expires,omitempty"`
	HTTPOnly bool    `json:"httpOnly,omitempty"`
	Secure   bool    `json:"secure,omitempty"`
	SameSite string  `json:"sameSite,omitempty"`
}

// ParseCookieString 把 Cookie 头解析为 name -> value。
func ParseCookieString(s string) map[string]string {
	out := make(map[string]string)
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
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
	keys := make([]string, 0, len(m))
	for k := range m {
		if strings.TrimSpace(k) != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, "; ")
}

// MergeSetCookies 将 Set-Cookie 响应头合并进原 Cookie 字符串。
func MergeSetCookies(original string, setCookies []string) string {
	m := ParseCookieString(original)
	for _, raw := range setCookies {
		first := strings.TrimSpace(strings.Split(raw, ";")[0])
		eq := strings.Index(first, "=")
		if eq <= 0 {
			continue
		}
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
func SnapshotFromCookieString(cookieString, domain string) []BrowserCookie {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		domain = ".goofish.com"
	}
	out := make([]BrowserCookie, 0)
	for name, value := range ParseCookieString(cookieString) {
		out = append(out, BrowserCookie{Name: name, Value: value, Domain: domain, Path: "/", Secure: true})
	}
	return NormalizeSnapshot(out)
}

// ReconcileSnapshotWithCookieString 在调用方暂时只能获得扁平 Cookie 结果时保留
// 已知属性，并同步值与删除项。新增字段使用 .goofish.com 根路径作为兼容作用域。
func ReconcileSnapshotWithCookieString(snapshot []BrowserCookie, cookieString string) []BrowserCookie {
	values := ParseCookieString(cookieString)
	seen := make(map[string]struct{})
	out := make([]BrowserCookie, 0, len(snapshot)+len(values))
	for _, cookie := range NormalizeSnapshot(snapshot) {
		value, exists := values[cookie.Name]
		if !exists {
			continue
		}
		cookie.Value = value
		out = append(out, cookie)
		seen[cookie.Name] = struct{}{}
	}
	for name, value := range values {
		if _, exists := seen[name]; exists {
			continue
		}
		out = append(out, BrowserCookie{Name: name, Value: value, Domain: ".goofish.com", Path: "/", Secure: true})
	}
	return NormalizeSnapshot(out)
}

// CookieHeaderForURL 按浏览器 Domain/Path/Secure/Expires 规则生成指定 URL 的
// Cookie header，并保留不同路径下的同名 Cookie。
func CookieHeaderForURL(snapshot []BrowserCookie, rawURL string, now time.Time) string {
	target, err := url.Parse(rawURL)
	if err != nil || target.Hostname() == "" {
		return ""
	}
	type matchedCookie struct {
		cookie BrowserCookie
		index  int
	}
	matched := make([]matchedCookie, 0, len(snapshot))
	for index, cookie := range NormalizeSnapshot(snapshot) {
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
	parts := make([]string, 0, len(matched))
	for _, item := range matched {
		parts = append(parts, item.cookie.Name+"="+item.cookie.Value)
	}
	return strings.Join(parts, "; ")
}

// ApplySetCookies 把某次请求响应的 Set-Cookie 应用到完整快照。删除操作只删除
// 相同 name/domain/path 的 Cookie，不会误删其他作用域下的同名项。
func ApplySetCookies(snapshot []BrowserCookie, requestURL string, setCookies []string, now time.Time) []BrowserCookie {
	target, err := url.Parse(requestURL)
	if err != nil || target.Hostname() == "" {
		return NormalizeSnapshot(snapshot)
	}
	state := make(map[string]BrowserCookie)
	for _, cookie := range NormalizeSnapshot(snapshot) {
		state[snapshotKey(cookie)] = cookie
	}
	for _, raw := range setCookies {
		parsed, err := http.ParseSetCookie(raw)
		if err != nil || strings.TrimSpace(parsed.Name) == "" {
			continue
		}
		domain := strings.ToLower(strings.TrimSpace(parsed.Domain))
		if domain == "" {
			domain = strings.ToLower(target.Hostname())
		} else if !strings.HasPrefix(domain, ".") {
			domain = "." + domain
		}
		cookiePath := parsed.Path
		if cookiePath == "" {
			cookiePath = defaultCookiePath(target.Path)
		}
		cookie := BrowserCookie{
			Name: parsed.Name, Value: parsed.Value, Domain: domain, Path: cookiePath,
			HTTPOnly: parsed.HttpOnly, Secure: parsed.Secure, SameSite: sameSiteLabel(parsed.SameSite),
		}
		if !parsed.Expires.IsZero() {
			cookie.Expires = float64(parsed.Expires.Unix())
		}
		key := snapshotKey(cookie)
		if parsed.MaxAge < 0 || (!parsed.Expires.IsZero() && !parsed.Expires.After(now)) {
			delete(state, key)
			continue
		}
		state[key] = cookie
	}
	out := make([]BrowserCookie, 0, len(state))
	for _, cookie := range state {
		out = append(out, cookie)
	}
	return NormalizeSnapshot(out)
}

func snapshotKey(cookie BrowserCookie) string {
	return cookie.Name + "\x00" + strings.ToLower(cookie.Domain) + "\x00" + cookie.Path
}

func cookieDomainMatches(host, domain string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return false
	}
	if strings.HasPrefix(domain, ".") {
		base := strings.TrimPrefix(domain, ".")
		return host == base || strings.HasSuffix(host, "."+base)
	}
	return host == domain
}

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

func defaultCookiePath(requestPath string) string {
	if requestPath == "" || requestPath[0] != '/' || requestPath == "/" {
		return "/"
	}
	dir := path.Dir(requestPath)
	if dir == "." || dir == "/" {
		return "/"
	}
	return dir
}

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
	a := ParseCookieString(before)
	b := ParseCookieString(after)
	seen := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	names := make([]string, 0, len(seen))
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
	key := func(c BrowserCookie) string {
		path := c.Path
		if path == "" {
			path = "/"
		}
		return c.Name + "|" + c.Domain + "|" + path
	}
	label := func(c BrowserCookie) string {
		path := c.Path
		if path == "" {
			path = "/"
		}
		if c.Domain != "" {
			return c.Name + "@" + c.Domain + path
		}
		return c.Name
	}
	oldMap := make(map[string]BrowserCookie)
	newMap := make(map[string]BrowserCookie)
	for _, c := range NormalizeSnapshot(before) {
		oldMap[key(c)] = c
	}
	for _, c := range NormalizeSnapshot(after) {
		newMap[key(c)] = c
	}
	seen := make(map[string]struct{}, len(oldMap)+len(newMap))
	for k := range oldMap {
		seen[k] = struct{}{}
	}
	for k := range newMap {
		seen[k] = struct{}{}
	}
	labels := make([]string, 0, len(seen))
	for k := range seen {
		old := oldMap[k]
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
	m := make(map[string]string, len(cookies))
	for _, c := range cookies {
		if c.Name == "" {
			continue
		}
		m[c.Name] = c.Value
	}
	return MarshalCookieString(m)
}

// MergeOriginalFields 补回浏览器未返回但原 Cookie 中存在的字段。
func MergeOriginalFields(original, browserCookieString string) string {
	m := ParseCookieString(original)
	for k, v := range ParseCookieString(browserCookieString) {
		m[k] = v
	}
	return MarshalCookieString(m)
}

// NormalizeSnapshot 排序并补齐默认 path，保证 metadata 稳定。
func NormalizeSnapshot(cookies []BrowserCookie) []BrowserCookie {
	out := make([]BrowserCookie, 0, len(cookies))
	for _, c := range cookies {
		if c.Name == "" {
			continue
		}
		if c.Path == "" {
			c.Path = "/"
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Domain != out[j].Domain {
			return out[i].Domain < out[j].Domain
		}
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// SnapshotFromMetadata 从 cookies.metadata_json 中读取浏览器 Cookie 快照。
func SnapshotFromMetadata(metadata string) []BrowserCookie {
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(strings.TrimSpace(metadata)), &m); err != nil {
		return nil
	}
	var out []BrowserCookie
	if raw := m[metadataSnapshotKey]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	} else if raw := m[legacyMetadataSnapshot]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return NormalizeSnapshot(out)
}

// MetadataWithSnapshot 写入浏览器 Cookie 快照，保留 metadata 中的其他键。
func MetadataWithSnapshot(metadata string, cookies []BrowserCookie) string {
	m := make(map[string]any)
	if strings.TrimSpace(metadata) != "" {
		_ = json.Unmarshal([]byte(metadata), &m)
	}
	delete(m, legacyMetadataSnapshot)
	m[metadataSnapshotKey] = NormalizeSnapshot(cookies)
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
	m := make(map[string]any)
	if err := json.Unmarshal([]byte(metadata), &m); err != nil {
		return metadata
	}
	delete(m, metadataSnapshotKey)
	delete(m, legacyMetadataSnapshot)
	b, err := json.Marshal(m)
	if err != nil {
		return metadata
	}
	return string(b)
}
