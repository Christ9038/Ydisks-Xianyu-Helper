package xianyu

import (
	"net/http"
	"testing"
)

// TestApplyBrowserFingerprintUsesRuntimeValues 封装TestApply浏览器FingerprintUsesRuntimeValues业务协调。
func TestApplyBrowserFingerprintUsesRuntimeValues(t *testing.T) {
	SetBrowserFingerprint(BrowserFingerprint{
		UserAgent: "runtime-chromium-ua",
		SecChUA:   `"Chromium";v="999"`,
		Platform:  "macOS",
		Mobile:    "?0",
	})
	// h 用于本次流程后续判断的h
	h := http.Header{}
	ApplyBrowserFingerprint(h)
	if h.Get("User-Agent") != "runtime-chromium-ua" ||
		h.Get("sec-ch-ua") != `"Chromium";v="999"` ||
		h.Get("sec-ch-ua-platform") != `"macOS"` || h.Get("sec-ch-ua-mobile") != "?0" {
		t.Fatalf("runtime browser fingerprint not applied: %v", h)
	}
}
