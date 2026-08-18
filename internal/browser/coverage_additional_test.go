package browser

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mxschmitt/playwright-go"

	"xianyu-go/internal/xianyu/cookierefresh"
)

// TestSnapshotToOptionalCookiesPreservesBrowserAttributes 验证完整 Cookie 快照到 Playwright 参数的属性映射。
func TestSnapshotToOptionalCookiesPreservesBrowserAttributes(t *testing.T) {
	// expires 用于本次流程后续判断的过期时间。
	expires := float64(1700000000)
	// partitionKey 用于本次流程后续判断的分区键。
	partitionKey := "https://shop.example"
	// snapshot 保存待转换的浏览器 Cookie 快照。
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "strict", Value: "1", Expires: expires, HTTPOnly: true, Secure: true, SameSite: "Strict", PartitionKey: partitionKey},
		{Name: "lax", Value: "2", Domain: "example.com", Path: "/im", SameSite: "Lax"},
		{Name: "none", Value: "3", SameSite: "None"},
		{Name: "default", Value: "4"},
	}
	// cookies 保存转换后的 Playwright Cookie 参数。
	cookies := snapshotToOptionalCookies(snapshot)
	if len(cookies) != len(snapshot) {
		t.Fatalf("转换数量=%d，期望=%d", len(cookies), len(snapshot))
	}
	if cookies[0].Domain == nil || *cookies[0].Domain != goofishDot || cookies[0].Path == nil || *cookies[0].Path != "/" {
		t.Fatalf("默认作用域错误：%+v", cookies[0])
	}
	if cookies[0].Expires == nil || *cookies[0].Expires != expires || cookies[0].PartitionKey == nil || *cookies[0].PartitionKey != partitionKey {
		t.Fatalf("完整属性丢失：%+v", cookies[0])
	}
	if cookies[0].SameSite == nil || *cookies[0].SameSite != *playwright.SameSiteAttributeStrict {
		t.Fatalf("Strict 属性错误：%+v", cookies[0].SameSite)
	}
	if cookies[1].Domain == nil || *cookies[1].Domain != "example.com" || cookies[1].Path == nil || *cookies[1].Path != "/im" || cookies[1].SameSite == nil || *cookies[1].SameSite != *playwright.SameSiteAttributeLax {
		t.Fatalf("Lax 属性错误：%+v", cookies[1])
	}
	if cookies[2].SameSite == nil || *cookies[2].SameSite != *playwright.SameSiteAttributeNone {
		t.Fatalf("None 属性错误：%+v", cookies[2].SameSite)
	}
	if cookies[3].Domain == nil || *cookies[3].Domain != goofishDot || cookies[3].Path == nil || *cookies[3].Path != "/" {
		t.Fatalf("默认属性错误：%+v", cookies[3])
	}
}

// TestCookieSnapshotFromPlaywrightNormalizesFields 验证 Playwright Cookie 可逆转换并补齐默认路径。
func TestCookieSnapshotFromPlaywrightNormalizesFields(t *testing.T) {
	// sameSite 用于本次流程后续判断的 SameSite 属性。
	sameSite := playwright.SameSiteAttributeLax
	// partitionKey 用于本次流程后续判断的分区键。
	partitionKey := "https://top.example"
	// cookies 保存 Playwright 返回的 Cookie 列表。
	cookies := []playwright.Cookie{
		{Name: "session", Value: "fresh", Domain: ".goofish.com", Expires: 123, HttpOnly: true, Secure: true, SameSite: sameSite, PartitionKey: &partitionKey},
		{Name: "path-default", Value: "value", Domain: ".goofish.com"},
		{Name: "", Value: "ignored"},
	}
	// snapshot 保存转换后的浏览器快照。
	snapshot := cookieSnapshotFromPlaywright(cookies)
	if len(snapshot) != 2 {
		t.Fatalf("快照数量=%d，期望=2：%+v", len(snapshot), snapshot)
	}
	if snapshot[0].SameSite != "Lax" || snapshot[0].PartitionKey != partitionKey || !snapshot[0].HTTPOnly || !snapshot[0].Secure {
		t.Fatalf("属性转换错误：%+v", snapshot[0])
	}
	if snapshot[1].Path != "/" {
		t.Fatalf("默认路径=%q，期望=/", snapshot[1].Path)
	}
}

// TestCurrentCookieHeaderRespectsScope 验证当前 Cookie 头按 URL 作用域筛选。
func TestCurrentCookieHeaderRespectsScope(t *testing.T) {
	// snapshot 保存不同路径和安全属性的 Cookie。
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "root", Value: "1", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "im", Value: "2", Domain: "www.goofish.com", Path: "/im", Secure: true},
		{Name: "other", Value: "3", Domain: ".goofish.com", Path: "/other", Secure: true},
	}
	// header 保存消息页 Cookie 头。
	header := currentCookieHeader(snapshot, goofishIMURL)
	if header != "im=2; root=1" {
		t.Fatalf("消息页 Cookie 头=%q，期望 im=2; root=1", header)
	}
	if // got 保存 HTTP 请求生成的 Cookie 头。
	got := currentCookieHeader(snapshot, "http://www.goofish.com/im"); got != "" {
		t.Fatalf("HTTP 请求不应发送 Secure Cookie：%q", got)
	}
	if // got 保存空快照生成的 Cookie 头。
	got := currentCookieHeader(nil, goofishIMURL); got != "" {
		t.Fatalf("nil 快照应返回空 Cookie 头：%q", got)
	}
}

// TestCleanSingletonFilesRemovesChromiumLocks 验证持久化 Profile 锁文件清理。
func TestCleanSingletonFilesRemovesChromiumLocks(t *testing.T) {
	// userDataDir 保存临时 Profile 目录。
	userDataDir := t.TempDir()
	for // name 表示当前待创建的锁文件名称。
	_, name := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
		// path 保存锁文件路径。
		path := filepath.Join(userDataDir, name)
		if // err 保存锁文件创建错误。
		err := os.WriteFile(path, []byte("lock"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cleanSingletonFiles(userDataDir)
	for // name 表示当前待检查的锁文件名称。
	_, name := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
		if // err 保存锁文件检查错误。
		_, err := os.Stat(filepath.Join(userDataDir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("锁文件 %s 未清理，err=%v", name, err)
		}
	}
	// 清理不存在文件必须保持幂等。
	cleanSingletonFiles(userDataDir)
}

// TestManagerRenewLockAndSlotCancellation 验证账号锁复用及续期槽位取消。
func TestManagerRenewLockAndSlotCancellation(t *testing.T) {
	// manager 保存待测试的浏览器管理器。
	manager := &Manager{}
	// firstLock 保存第一次获取的账号锁。
	firstLock := manager.accountRenewLock("account")
	// secondLock 保存第二次获取的账号锁。
	secondLock := manager.accountRenewLock("account")
	if firstLock != secondLock {
		t.Fatal("同一账号必须复用同一把续期锁")
	}
	// canceled 保存已取消的上下文。
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	manager.renewSlots = make(chan struct{}, 1)
	manager.renewSlots <- struct{}{}
	if // err 保存取消上下文获取槽位的错误。
	_, err := manager.acquireRenewSlot(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("槽位取消错误=%v，期望 context.Canceled", err)
	}
	<-manager.renewSlots
	// release、err 保存成功获取槽位后的释放函数和错误。
	release, err := manager.acquireRenewSlot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	release()
}

// TestPasswordDetectorHandlesHTMLAndURLs 验证登录错误与验证 URL 的 HTML 清洗分支。
func TestPasswordDetectorHandlesHTMLAndURLs(t *testing.T) {
	if // got 保存页面错误文本识别结果。
	got := detectPasswordLoginErrorHTML(`<script>账号密码错误</script><div>暂时无法登录</div>`); got != "暂时无法登录" {
		t.Fatalf("脚本内容不应被识别，页面错误=%q", got)
	}
	// htmlText 保存包含验证链接和二维码的页面片段。
	htmlText := `<div>身份验证</div><a href="https://passport.goofish.com/verify?id=1">继续</a><img src="data:image/png;base64,QUJD">`
	// event、ok 保存检测结果。
	event, ok := detectPasswordVerificationHTML(htmlText)
	if !ok || event.VerificationURL == "" || event.QRCodeURL == "" {
		t.Fatalf("验证页面解析失败：ok=%v event=%+v", ok, event)
	}
	if // ok 保存普通页面是否被误判为验证页面。
	_, ok := detectPasswordVerificationHTML("<div>普通登录页</div>"); ok {
		t.Fatal("普通登录页不应误判为人工验证")
	}
	if // got 保存 HTML 清洗结果。
	got := normalizeHTMLText(`<style>账号密码错误</style><p>登录&nbsp;失败</p>`); got != "登录 失败" {
		t.Fatalf("HTML 清洗结果=%q", got)
	}
	if !containsAny("abc", "", "bc") || containsAny("abc", "xyz") {
		t.Fatal("containsAny 边界结果错误")
	}
	// manager 保存只用于验证空参数保护的浏览器管理器。
	manager := &Manager{}
	if // err 保存空账号密码登录错误。
	_, err := manager.PasswordLoginWithEvents(context.Background(), "", "password", "id", "", true, nil); err == nil {
		t.Fatal("空账号必须在启动浏览器前失败")
	}
	if // err 保存空 Cookie 续期错误。
	_, err := manager.CookieRenew(context.Background(), "id", "", true); err == nil {
		t.Fatal("空 Cookie 必须在启动浏览器前失败")
	}
	if // err 保存空快照续期错误。
	_, _, _, err := manager.CookiesRefreshSnapshot(context.Background(), "id", "", nil, true); err == nil {
		t.Fatal("空 Cookie 快照必须在启动浏览器前失败")
	}
	if // err 保存兼容续期入口错误。
	_, _, err := manager.CookieRenewSnapshot(context.Background(), "id", "", nil, true); err == nil {
		t.Fatal("兼容续期入口必须转发空 Cookie 错误")
	}
	if // err 保存空扫码临时 Cookie 错误。
	_, _, err := manager.QRCookieRefresh(context.Background(), "", "", nil); err == nil {
		t.Fatal("空扫码临时 Cookie 必须在启动浏览器前失败")
	}
	// canceled、cancel 保存用于验证浏览器上下文槽位取消的上下文。
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	manager.renewSlots = make(chan struct{}, 1)
	manager.renewSlots <- struct{}{}
	if // err 保存密码登录上下文取消错误。
	_, _, err := manager.newPersistentPasswordContext(canceled, "id", "profile", true); !errors.Is(err, context.Canceled) {
		t.Fatalf("密码登录上下文取消错误=%v", err)
	}
	if // err 保存续期上下文取消错误。
	_, _, err := manager.newPersistentRenewContext(canceled, "id", "", nil, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("续期上下文取消错误=%v", err)
	}
	if // err 保存 Token Cookie 快照取消错误。
	_, _, err := manager.TokenCaptchaCookieSnapshot(canceled, "id", true); !errors.Is(err, context.Canceled) {
		t.Fatalf("Token Cookie 快照取消错误=%v", err)
	}
	// failedManager 保存初始化函数失败的浏览器管理器。
	failedManager := NewManager(nil)
	failedManager.installFn = func(context.Context) error { return errors.New("测试安装失败") }
	if // err 保存初始化失败错误。
	err := failedManager.Initialize(); err == nil {
		t.Fatal("初始化失败应向调用方返回错误")
	}
}

// TestBrowserPageHelpers 使用本地 Chromium 覆盖页面相关的确定性辅助函数。
func TestBrowserPageHelpers(t *testing.T) {
	if os.Getenv("RUN_BROWSER_INTEGRATION") != "1" {
		t.Skip("设置 RUN_BROWSER_INTEGRATION=1 后运行本地 Chromium 页面覆盖测试")
	}
	// manager、browser、context、page 保存本地 Chromium 测试资源。
	manager := NewManager(nil)
	if // err 保存 Playwright 初始化错误。
	err := manager.init(); err != nil {
		t.Skipf("本机没有可用 Chromium runtime：%v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	// browser、err 保存本地 Chromium 实例及启动错误。
	browser, err := manager.pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(true), Args: chromiumLaunchArgs()})
	if err != nil {
		t.Skipf("启动本地 Chromium 失败：%v", err)
	}
	t.Cleanup(func() { _ = browser.Close() })
	// bctx、err 保存浏览器上下文及创建错误。
	bctx, err := browser.NewContext()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bctx.Close() })
	// page、err 保存测试页面及创建错误。
	page, err := bctx.NewPage()
	if err != nil {
		t.Fatal(err)
	}

	if // err 保存个人中心页面写入错误。
	err := page.SetContent(`<a href="https://www.goofish.com/personal">个人中心</a><button style="display:none">快速进入</button><button>快速进入</button>`); err != nil {
		t.Fatal(err)
	}
	if !clickQuickEnter(page) {
		t.Fatal("应点击可见的快速进入按钮")
	}
	if // err 保存登录状态验证错误。
	err := verifyHomeLoginState(page); err != nil {
		t.Fatalf("个人中心页面应判定为已登录：%v", err)
	}
	if // err 保存登录页面写入错误。
	err := page.SetContent(`<button>登录</button>`); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(verifyHomeLoginState(page), ErrInteractiveLoginRequired) {
		t.Fatal("登录按钮页面应要求交互式登录")
	}
	if // err 保存安全验证页面写入错误。
	err := page.SetContent(`<div>请拖动下方滑块完成验证</div>`); err != nil {
		t.Fatal(err)
	}
	if !pageHasSecurityVerification(page) {
		t.Fatal("安全验证文本应被识别")
	}

	if // err 保存订单页面写入错误。
	err := page.SetContent(`<span class="boldNum--JgEOXfA3">¥0.88</span><span class="sku--u_ddZval">颜色: 蓝色</span><span class="sku--u_ddZval">x2</span>`); err != nil {
		t.Fatal(err)
	}
	// order 保存待解析的订单详情。
	order := &OrderDetail{}
	parseDOMContent(page, order)
	if order.Amount != "¥0.88" || order.SpecName != "颜色" || order.SpecValue != "蓝色" || order.Quantity != "2" {
		t.Fatalf("DOM 订单解析结果错误：%+v", order)
	}
	if // err 保存金额回退页面写入错误。
	err := page.SetContent(`<body><div>实付款</div><div>12.50</div></body>`); err != nil {
		t.Fatal(err)
	}
	// fallbackOrder 保存金额回退解析的订单详情。
	fallbackOrder := &OrderDetail{}
	parseDOMContent(page, fallbackOrder)
	if fallbackOrder.Amount != "12.50" || fallbackOrder.Quantity != "1" {
		t.Fatalf("文本回退解析结果错误：%+v", fallbackOrder)
	}
	if // err 保存扁平 Cookie 同步错误。
	err := syncCredentialCookies(bctx, "session=fresh"); err != nil {
		t.Fatal(err)
	}
	// cookies、err 保存同步后的 Cookie 列表及读取错误。
	cookies, err := bctx.Cookies()
	if err != nil {
		t.Fatal(err)
	}
	if len(cookies) != 1 || cookies[0].Name != "session" || cookies[0].Value != "fresh" {
		t.Fatalf("Cookie 同步结果错误：%+v", cookies)
	}
	if // err 保存完整快照 Cookie 同步错误。
	err := syncCredentialCookies(bctx, "", []cookierefresh.BrowserCookie{{Name: "snapshot", Value: "ok", Domain: ".goofish.com", Path: "/", Secure: true}}); err != nil {
		t.Fatal(err)
	}
	// oldWorkingDirectory 保存测试前的工作目录，避免持久化 Profile 污染仓库。
	oldWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if // err 保存切换临时工作目录的错误。
	err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWorkingDirectory) })
	// persistentContext、release 保存持久化续期上下文及释放函数。
	persistentContext, release, err := manager.newPersistentRenewContext(context.Background(), "coverage-profile", "session=renewed", nil, true)
	if err != nil {
		t.Fatalf("启动持久化续期上下文失败：%v", err)
	}
	// jarHeader、jarSnapshot、err 保存持久化上下文 Cookie 读取结果。
	jarHeader, jarSnapshot, err := readAuthoritativeCookieJar(persistentContext, goofishIMURL)
	release()
	if err != nil || !strings.Contains(jarHeader, "session=renewed") || len(jarSnapshot) == 0 {
		t.Fatalf("持久化上下文 Cookie 读取失败：header=%q snapshot=%+v err=%v", jarHeader, jarSnapshot, err)
	}
	// pooledPage、pooledRelease、err 保存共享浏览器池页面及租约释放函数。
	pooledPage, pooledRelease, err := manager.newPage(context.Background(), "coverage-pool", "pool=1", true)
	if err != nil {
		t.Fatalf("创建浏览器池页面失败：%v", err)
	}
	pooledRelease()
	_ = pooledPage
	if // err 保存滑块页面写入错误。
	err := page.SetContent(`<div class="nc-container" style="width:300px;height:34px"><div id="nc_1_n1t" style="position:relative;width:300px;height:34px"><span id="nc_1_n1z" style="display:block;position:absolute;left:0;width:42px;height:34px"></span></div><button id="nc_1_refresh1">刷新</button></div>`); err != nil {
		t.Fatal(err)
	}
	// button、track、frame、err 保存滑块元素查找结果。
	button, track, frame, err := findSliderElements(page)
	if err != nil || button == nil || track == nil || frame == nil {
		t.Fatalf("滑块元素查找失败：button=%v track=%v frame=%v err=%v", button, track, frame, err)
	}
	if !sliderAtOrigin(button, track) || readSliderLeft(button) != "0px" || isScratchCaptchaFromPage(page) {
		t.Fatalf("滑块初始状态识别错误：origin=%v left=%q scratch=%v", sliderAtOrigin(button, track), readSliderLeft(button), isScratchCaptchaFromPage(page))
	}
	if // selector、err 保存重试控件选择器及点击错误。
	selector, err := clickRetry(page); err != nil || selector != "#nc_1_refresh1" {
		t.Fatalf("滑块重试控件点击失败：selector=%q err=%v", selector, err)
	}
	if !hasDefinitiveSliderFailure(page) {
		t.Fatal("显式刷新按钮应被识别为确定失败")
	}
	if // err 保存隐藏滑块容器的脚本错误。
	_, err := page.Evaluate(`() => document.querySelector('.nc-container').style.display = 'none'`); err != nil {
		t.Fatal(err)
	}
	if !checkSliderSuccess(page) {
		t.Fatal("隐藏滑块容器应判定为成功")
	}
	// pageCookies、err 保存从页面上下文提取的 Cookie。
	pageCookies, err := extractPageCookies(page)
	if err != nil || pageCookies == nil {
		// 该分支仅验证函数可调用；具体 Cookie 快照已在上方同步测试。
		t.Fatalf("页面 Cookie 提取失败：cookies=%v err=%v", pageCookies, err)
	}
	if // err 保存登录表单页面写入错误。
	err := page.SetContent(`<a class="password-login-tab-item">密码登录</a><input id="fm-login-id"><input id="fm-login-password" type="password"><button type="submit">登录</button>`); err != nil {
		t.Fatal(err)
	}
	if !clickPasswordLoginTab(page) {
		t.Fatal("密码登录页签应可点击")
	}
	// idElement、passwordElement、submitElement 保存登录表单元素。
	idElement, passwordElement, submitElement := findLoginForm(page)
	if idElement == nil || passwordElement == nil || submitElement == nil || findPasswordElement(page, loginIDSelectors) == nil {
		t.Fatal("登录表单元素查找失败")
	}
	if detectAndHandlePasswordSlider(page, manager.logger) != true {
		t.Fatal("没有滑块时检测处理应视为成功")
	}
	if // err 保存登录成功页面写入错误。
	err := page.SetContent(`<div class="rc-virtual-list-holder-inner"><span>账号</span></div>`); err != nil {
		t.Fatal(err)
	}
	if !checkLoginSuccess(page) {
		t.Fatal("带侧边栏子元素的页面应判定为登录成功")
	}
	if // err 保存登录错误页面写入错误。
	err := page.SetContent(`<div class="login-error-msg">账号密码错误</div>`); err != nil {
		t.Fatal(err)
	}
	if passwordLoginErrorFromPage(page) != "账号密码错误" {
		t.Fatal("登录错误提示提取失败")
	}
	if // event、ok 保存普通页面 Baxia 事件检测结果。
	event, ok := passwordBaxiaEventFromPage(page); ok || event.Message != "" {
		t.Fatal("普通错误页不应产生 Baxia 事件")
	}
	if // err 保存人工验证页面写入错误。
	err := page.SetContent(`<div>身份验证</div>`); err != nil {
		t.Fatal(err)
	}
	if // event、ok 保存人工验证事件检测结果。
	event, ok := passwordVerificationEventFromPage(page); !ok || event.Status != PasswordLoginStatusVerificationRequired || !strings.HasPrefix(event.ScreenshotPath, "data:image/png;base64,") {
		t.Fatalf("验证事件或截图提取失败：ok=%v event=%+v", ok, event)
	}
	if !looksLikeVerificationURL("https://passport.goofish.com/verify") || looksLikeVerificationURL("https://www.goofish.com/item/1") || firstNonEmptyString("", "  ", "selected") != "selected" {
		t.Fatal("验证 URL 或首个非空字符串判断错误")
	}
}

// TestQuickEnterCookiesUsableCoversEmptyAndValidValues 验证快速进入 Cookie 的有效性判断。
func TestQuickEnterCookiesUsableCoversEmptyAndValidValues(t *testing.T) {
	// cases 保存输入 Cookie 与期望结果。
	cases := []struct {
		cookies map[string]string
		valid   bool
	}{
		{cookies: nil, valid: false},
		{cookies: map[string]string{"unb": ""}, valid: false},
		{cookies: map[string]string{"unb": "  "}, valid: false},
		{cookies: map[string]string{"unb": "1"}, valid: true},
	}
	for // item 表示当前快速进入 Cookie 测试用例。
	_, item := range cases {
		if // got 保存当前用例的有效性判断结果。
		got := quickEnterCookiesUsable(item.cookies); got != item.valid {
			t.Errorf("quickEnterCookiesUsable(%v)=%v，期望=%v", item.cookies, got, item.valid)
		}
	}
}
