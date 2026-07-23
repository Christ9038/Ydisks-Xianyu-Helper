package xianyu

import (
	"net/http"
	"testing"
)

func TestApplyBrowserFingerprintUsesRuntimeValues(t *testing.T) {
	SetBrowserFingerprint(BrowserFingerprint{
		UserAgent: "runtime-chromium-ua",
		SecChUA:   `"Chromium";v="999"`,
		Platform:  "macOS",
		Mobile:    "?0",
	})
	h := http.Header{}
	ApplyBrowserFingerprint(h)
	if h.Get("User-Agent") != "runtime-chromium-ua" ||
		h.Get("sec-ch-ua") != `"Chromium";v="999"` ||
		h.Get("sec-ch-ua-platform") != `"macOS"` || h.Get("sec-ch-ua-mobile") != "?0" {
		t.Fatalf("runtime browser fingerprint not applied: %v", h)
	}
}
