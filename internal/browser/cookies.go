package browser

import (
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/mxschmitt/playwright-go"

	"xianyu-go/internal/xianyu/cookierefresh"
)

// parseCookieStr 把 "k=v; k2=v2" 解析为 map。
func parseCookieStr(s string) map[string]string {
	// m 用于本次流程后续判断的m
	m := make(map[string]string)
	// part 表示当前遍历过程中的part
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if // eq 用于本次流程后续判断的eq
		eq := strings.Index(part, "="); eq >= 0 {
			m[part[:eq]] = part[eq+1:]
		}
	}
	return m
}

// cookieMarshal 把 map 拼成 "k=v; k2=v2"。
func cookieMarshal(m map[string]string) string {
	// parts 用于本次流程后续判断的parts
	parts := make([]string, 0, len(m))
	// k、v 表示当前遍历过程中的k、v
	for k, v := range m {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "; ")
}

// MarshalCookies 把 cookie map 拼成标准 Cookie 头字符串。
func MarshalCookies(m map[string]string) string {
	return cookieMarshal(m)
}

// parseCookieStrToPlaywright 把 cookie 字符串转成 playwright OptionalCookie。
func parseCookieStrToPlaywright(s string) []playwright.OptionalCookie {
	// cookies 用于本次流程后续判断的cookies
	var cookies []playwright.OptionalCookie
	// part 表示当前遍历过程中的part
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if // eq 用于本次流程后续判断的eq
		eq := strings.Index(part, "="); eq >= 0 {
			// name 用于本次流程后续判断的名称
			name := part[:eq]
			// value 用于本次流程后续判断的值
			value := part[eq+1:]
			if name == "" {
				continue
			}
			cookies = append(cookies, playwright.OptionalCookie{
				Name:   name,
				Value:  value,
				Domain: playwright.String(goofishDot),
				Path:   playwright.String("/"),
			})
		}
	}
	return cookies
}

// snapshotToOptionalCookies 封装snapshotToOptionalCookies业务协调。
func snapshotToOptionalCookies(snapshot []cookierefresh.BrowserCookie) []playwright.OptionalCookie {
	// out 用于本次流程后续判断的out
	var out []playwright.OptionalCookie
	// c 表示当前遍历过程中的c
	for _, c := range cookierefresh.NormalizeSnapshot(snapshot) {
		// domain 用于本次流程后续判断的domain
		domain := c.Domain
		if domain == "" {
			domain = goofishDot
		}
		// path 用于本次流程后续判断的路径
		path := c.Path
		if path == "" {
			path = "/"
		}
		// oc 用于本次流程后续判断的oc
		oc := playwright.OptionalCookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   playwright.String(domain),
			Path:     playwright.String(path),
			HttpOnly: playwright.Bool(c.HTTPOnly),
			Secure:   playwright.Bool(c.Secure),
		}
		if c.Expires > 0 {
			oc.Expires = playwright.Float(c.Expires)
		}
		if c.PartitionKey != "" {
			oc.PartitionKey = playwright.String(c.PartitionKey)
		}
		switch c.SameSite {
		case "Strict":
			oc.SameSite = playwright.SameSiteAttributeStrict
		case "Lax":
			oc.SameSite = playwright.SameSiteAttributeLax
		case "None":
			oc.SameSite = playwright.SameSiteAttributeNone
		}
		out = append(out, oc)
	}
	return out
}

// cookieSnapshotFromPlaywright 封装登录凭证SnapshotFromPlaywright业务协调。
func cookieSnapshotFromPlaywright(cs []playwright.Cookie) []cookierefresh.BrowserCookie {
	// out 用于本次流程后续判断的out
	out := make([]cookierefresh.BrowserCookie, 0, len(cs))
	// c 表示当前遍历过程中的c
	for _, c := range cs {
		// bc 用于本次流程后续判断的bc
		bc := cookierefresh.BrowserCookie{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  c.Expires,
			HTTPOnly: c.HttpOnly,
			Secure:   c.Secure,
		}
		if c.SameSite != nil {
			bc.SameSite = string(*c.SameSite)
		}
		if c.PartitionKey != nil {
			bc.PartitionKey = *c.PartitionKey
		}
		out = append(out, bc)
	}
	return cookierefresh.NormalizeSnapshot(out)
}

// cookiesToMap 把 playwright Cookie 切片转成 map。
func cookiesToMap(cs []playwright.Cookie) map[string]string {
	// m 用于本次流程后续判断的m
	m := make(map[string]string, len(cs))
	// c 表示当前遍历过程中的c
	for _, c := range cs {
		m[c.Name] = c.Value
	}
	return m
}

// rng is used only for human-like pointer timing in interactive captcha
// handling. It must not be used to alter the browser/device fingerprint.
// #nosec G404 -- non-cryptographic interaction jitter only.
// rng 用于本次流程后续判断的rng
var rng = &lockedRand{value: rand.New(rand.NewSource(time.Now().UnixNano()))}

// lockedRand 用于本次流程后续判断的lockedRand
type lockedRand struct {
	mu    sync.Mutex
	value *rand.Rand
}

// Intn 封装Intn业务协调。
func (r *lockedRand) Intn(n int) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.value.Intn(n)
}

// Float64 封装Float64业务协调。
func (r *lockedRand) Float64() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.value.Float64()
}

// stealthScript is intentionally stable. Randomly overriding canvas, WebGL,
// hardware, platform, or timing APIs makes one account present a different
// device fingerprint on every renewal and is itself a strong risk signal.
// stealthScript 封装stealthScript业务协调。
func stealthScript() string {
	return stealthTemplate
}
