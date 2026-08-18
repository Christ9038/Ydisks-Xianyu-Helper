package xianyu

import (
	"net/http"
	"strings"
	"sync"
)

// BrowserFingerprint is populated from Playwright's bundled Chromium at
// runtime. It is the sole source for browser-identifying HTTP headers.
// BrowserFingerprint 用于本次流程后续判断的浏览器Fingerprint
type BrowserFingerprint struct {
	UserAgent string
	SecChUA   string
	Platform  string
	Mobile    string
}

// browserFingerprint 用于本次流程后续判断的浏览器Fingerprint
var browserFingerprint struct {
	sync.RWMutex
	value BrowserFingerprint
}

// SetBrowserFingerprint 设置浏览器Fingerprint。
func SetBrowserFingerprint(value BrowserFingerprint) {
	value.UserAgent = strings.TrimSpace(value.UserAgent)
	value.SecChUA = strings.TrimSpace(value.SecChUA)
	value.Platform = strings.TrimSpace(value.Platform)
	value.Mobile = strings.TrimSpace(value.Mobile)
	browserFingerprint.Lock()
	browserFingerprint.value = value
	browserFingerprint.Unlock()
}

// CurrentBrowserFingerprint 封装Current浏览器Fingerprint业务协调。
func CurrentBrowserFingerprint() BrowserFingerprint {
	browserFingerprint.RLock()
	defer browserFingerprint.RUnlock()
	return browserFingerprint.value
}

// ApplyBrowserFingerprint applies only headers observed from Chromium. Before
// browser initialization it intentionally adds no synthetic browser identity.
// ApplyBrowserFingerprint 封装Apply浏览器Fingerprint业务协调。
func ApplyBrowserFingerprint(h http.Header) {
	// fp 用于本次流程后续判断的fp
	fp := CurrentBrowserFingerprint()
	if fp.UserAgent != "" {
		h.Set("user-agent", fp.UserAgent)
	}
	if fp.SecChUA != "" {
		h.Set("sec-ch-ua", fp.SecChUA)
	}
	if fp.Platform != "" {
		h.Set("sec-ch-ua-platform", `"`+strings.ReplaceAll(fp.Platform, `"`, ``)+`"`)
	}
	if fp.Mobile != "" {
		h.Set("sec-ch-ua-mobile", fp.Mobile)
	}
}
