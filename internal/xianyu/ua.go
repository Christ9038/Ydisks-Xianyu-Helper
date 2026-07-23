package xianyu

import (
	"net/http"
	"strings"
	"sync"
)

// BrowserFingerprint is populated from Playwright's bundled Chromium at
// runtime. It is the sole source for browser-identifying HTTP headers.
type BrowserFingerprint struct {
	UserAgent string
	SecChUA   string
	Platform  string
	Mobile    string
}

var browserFingerprint struct {
	sync.RWMutex
	value BrowserFingerprint
}

func SetBrowserFingerprint(value BrowserFingerprint) {
	value.UserAgent = strings.TrimSpace(value.UserAgent)
	value.SecChUA = strings.TrimSpace(value.SecChUA)
	value.Platform = strings.TrimSpace(value.Platform)
	value.Mobile = strings.TrimSpace(value.Mobile)
	browserFingerprint.Lock()
	browserFingerprint.value = value
	browserFingerprint.Unlock()
}

func CurrentBrowserFingerprint() BrowserFingerprint {
	browserFingerprint.RLock()
	defer browserFingerprint.RUnlock()
	return browserFingerprint.value
}

// ApplyBrowserFingerprint applies only headers observed from Chromium. Before
// browser initialization it intentionally adds no synthetic browser identity.
func ApplyBrowserFingerprint(h http.Header) {
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
