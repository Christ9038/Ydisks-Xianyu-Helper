package browser

import (
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"

	"xianyu-go/internal/xianyu/cookierefresh"
)

var cookieDomains = []string{".goofish.com", ".taobao.com", ".alipay.com"}

// parseCookieStr 把 "k=v; k2=v2" 解析为 map。
func parseCookieStr(s string) map[string]string {
	m := make(map[string]string)
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if eq := strings.Index(part, "="); eq >= 0 {
			m[part[:eq]] = part[eq+1:]
		}
	}
	return m
}

// cookieMarshal 把 map 拼成 "k=v; k2=v2"。
func cookieMarshal(m map[string]string) string {
	parts := make([]string, 0, len(m))
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
	var cookies []playwright.OptionalCookie
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if eq := strings.Index(part, "="); eq >= 0 {
			name := part[:eq]
			value := part[eq+1:]
			if name == "" {
				continue
			}
			for _, domain := range cookieDomains {
				cookies = append(cookies, playwright.OptionalCookie{
					Name:   name,
					Value:  value,
					Domain: playwright.String(domain),
					Path:   playwright.String("/"),
				})
			}
		}
	}
	return cookies
}

func snapshotToOptionalCookies(snapshot []cookierefresh.BrowserCookie) []playwright.OptionalCookie {
	var out []playwright.OptionalCookie
	for _, c := range cookierefresh.NormalizeSnapshot(snapshot) {
		domain := c.Domain
		if domain == "" {
			domain = goofishDot
		}
		path := c.Path
		if path == "" {
			path = "/"
		}
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

func cookieSnapshotFromPlaywright(cs []playwright.Cookie) []cookierefresh.BrowserCookie {
	out := make([]cookierefresh.BrowserCookie, 0, len(cs))
	for _, c := range cs {
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
		out = append(out, bc)
	}
	return cookierefresh.NormalizeSnapshot(out)
}

// cookiesToMap 把 playwright Cookie 切片转成 map。
func cookiesToMap(cs []playwright.Cookie) map[string]string {
	m := make(map[string]string, len(cs))
	for _, c := range cs {
		m[c.Name] = c.Value
	}
	return m
}

// rng 只用于模拟浏览器轨迹和指纹扰动，不用于凭证或安全令牌。
// #nosec G404 -- 此处需要可重复的非密码学伪随机序列。
var rng = rand.New(rand.NewSource(time.Now().UnixNano()))

// stealthScript 生成隐身 JS（移植自 xianyu_slider_stealth._get_stealth_script）。
// 使用占位符并在 Go 运行时替换随机值。
func stealthScript() string {
	pluginCount := rng.Intn(6) + 3 // 3-8
	hwCores := []int{2, 4, 6, 8}[rng.Intn(4)]
	mem := []int{4, 8, 16}[rng.Intn(3)]
	effType := []string{"3g", "4g", "5g"}[rng.Intn(3)]
	rtt := rng.Intn(81) + 20                       // 20-100
	downlink := float64(rng.Intn(901)+100) / 100.0 // 1.00-10.00
	maxTouch := []int{0, 1, 5, 10}[rng.Intn(4)]
	battCharging := []string{"true", "false"}[rng.Intn(2)]
	battLevel := float64(rng.Intn(66)+30) / 100.0 // 0.30-0.95

	repl := strings.NewReplacer(
		"{{PLUGIN_COUNT}}", strconv.Itoa(pluginCount),
		"{{LOCALE}}", defaultLang,
		"{{VW}}", strconv.Itoa(defaultW),
		"{{VH}}", strconv.Itoa(defaultH),
		"{{HW_CORES}}", strconv.Itoa(hwCores),
		"{{MEM}}", strconv.Itoa(mem),
		"{{TZ}}", defaultTZ,
		"{{EFF_TYPE}}", effType,
		"{{RTT}}", strconv.Itoa(rtt),
		"{{DOWNLINK}}", strconv.FormatFloat(downlink, 'f', 2, 64),
		"{{MAX_TOUCH}}", strconv.Itoa(maxTouch),
		"{{BATT_CHARGE}}", battCharging,
		"{{BATT_LEVEL}}", strconv.FormatFloat(battLevel, 'f', 2, 64),
		"{{UA}}", defaultUA,
	)
	return repl.Replace(stealthTemplate)
}
