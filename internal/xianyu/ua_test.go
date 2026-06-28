package xianyu

import (
	"strings"
	"testing"
)

// TestUAConsistency 保证所有出站请求的 Chrome 版本号完全一致。
// 这是反风控的关键：真实浏览器 HTTP 请求、WS 握手、/reg 注册消息的 UA 必须同版本号，
// sec-ch-ua 的主版本也必须匹配。任何一处不一致都是伪造特征。
func TestUAConsistency(t *testing.T) {
	// 1. BrowserUA 含 ChromeVer。
	if !strings.Contains(BrowserUA, "Chrome/"+ChromeVer) {
		t.Errorf("BrowserUA 未含 Chrome/%s: %s", ChromeVer, BrowserUA)
	}

	// 2. RegUA 必须以 BrowserUA 开头（同一浏览器 UA + DingTalk 后缀）。
	if !strings.HasPrefix(RegUA, BrowserUA) {
		t.Errorf("RegUA 必须以 BrowserUA 开头（保证 Chrome 版本一致）\n RegUA: %s\n BrowserUA: %s", RegUA, BrowserUA)
	}
	// RegUA 的 Browser(Chrome/x) 后缀版本也要一致。
	if !strings.Contains(RegUA, "Browser(Chrome/"+ChromeVer+")") {
		t.Errorf("RegUA 的 Browser(Chrome/x) 版本与 ChromeVer 不一致: %s", RegUA)
	}

	// 3. SecChUA 的主版本号必须等于 ChromeMajor。
	if !strings.Contains(SecChUA, `"Google Chrome";v="`+ChromeMajor+`"`) {
		t.Errorf("SecChUA 的 Google Chrome 版本与 ChromeMajor 不一致: %s", SecChUA)
	}
	if !strings.Contains(SecChUA, `"Chromium";v="`+ChromeMajor+`"`) {
		t.Errorf("SecChUA 的 Chromium 版本与 ChromeMajor 不一致: %s", SecChUA)
	}

	// 4. ChromeMajor 必须是 ChromeVer 的主版本号前缀。
	if !strings.HasPrefix(ChromeVer, ChromeMajor+".") {
		t.Errorf("ChromeMajor(%s) 应是 ChromeVer(%s) 的主版本前缀", ChromeMajor, ChromeVer)
	}
}
