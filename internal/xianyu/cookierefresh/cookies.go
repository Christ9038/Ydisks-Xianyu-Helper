package cookierefresh

import (
	"encoding/json"
	"sort"
	"strings"
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
