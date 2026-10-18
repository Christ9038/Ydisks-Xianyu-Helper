package webui

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/mxschmitt/playwright-go"
)

// TestDeliveryTemplateEditorBrowserIntegration 用 t 验证真实嵌入页面的滚动、固定操作区和新增焦点；显式开启时缺少 Chromium 必须失败。
func TestDeliveryTemplateEditorBrowserIntegration(t *testing.T) {
	if os.Getenv("RUN_BROWSER_INTEGRATION") != "1" {
		t.Skip("设置 RUN_BROWSER_INTEGRATION=1 执行本地 Chromium 布局回归")
	}
	// runtime、err 是测试独占的 Playwright 驱动及启动结果；不安装或改写应用的浏览器配置。
	runtime, err := playwright.Run()
	templateBrowserCheck(t, err, "启动 Playwright")
	t.Cleanup(func() { // 测试结束后等待驱动退出，不遗留后台进程。
		templateBrowserCheck(t, runtime.Stop(), "关闭 Playwright")
	})
	// browser 是仅访问本地夹具的 Chromium，在驱动之前关闭。
	browser, err := runtime.Chromium.Launch(playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(true)})
	templateBrowserCheck(t, err, "启动 Chromium")
	t.Cleanup(func() { // 浏览器拥有所有测试页面，关闭时同步释放。
		templateBrowserCheck(t, browser.Close(), "关闭 Chromium")
	})
	// server 提供实际构建资源和无凭据模板接口；关闭责任属于当前测试。
	server := templateBrowserServer(t)
	// viewport 是桌面、窄屏或小屏的 CSS 像素大小，不通过设备仿真跳过布局。
	for _, viewport := range []playwright.Size{{Width: 1366, Height: 768}, {Width: 1920, Height: 1080}, {Width: 390, Height: 844}, {Width: 320, Height: 568}} {
		// count 是打开编辑器时的消息条数，覆盖已知阈值和长列表。
		for _, count := range []int{3, 5, 10} {
			// mixed 决定是否交替放入本地图片配置，以覆盖不同消息高度。
			for _, mixed := range []bool{false, true} {
				t.Run(fmt.Sprintf("%dx%d/%d/mixed=%t", viewport.Width, viewport.Height, count, mixed), func(t *testing.T) { // t 收集当前独立视口和消息组合的失败。
					// page、err 是该场景独占的页面及创建结果，关闭后丢弃全部草稿。
					page, err := browser.NewPage(playwright.BrowserNewPageOptions{Viewport: &viewport})
					templateBrowserCheck(t, err, "创建页面")
					t.Cleanup(func() { // 场景结束立即关闭独立上下文。
						templateBrowserCheck(t, page.Close(), "关闭页面")
					})
					page.SetDefaultTimeout(5000)
					// 页面只允许访问本地服务，任何意外的外部图片或请求立即失败。
					templateBrowserCheck(t, page.Route("**/*", func(route playwright.Route) { // route 是浏览器的单次出站请求，不携带真实账号数据。
						if strings.HasPrefix(route.Request().URL(), server.URL+"/") {
							_ = route.Continue()
							return
						}
						_ = route.Abort()
					}), "限制本地请求")
					_, err = page.Goto(fmt.Sprintf("%s/?count=%d&mixed=%t", server.URL, count, mixed))
					templateBrowserCheck(t, err, "打开模板夹具")
					templateBrowserCheck(t, page.Locator("button:has-text('编辑')").Click(), "编辑现有模板")
					templateBrowserActionsVisible(t, page)
					// body 是实际可滚动区域，检测滚轮而不是仅断言 overflow 样式存在。
					body := page.Locator(".delivery-template-editor__body")
					// box 是正文视口几何，用边缘空白避免滚轮被 textarea 消费。
					box, err := body.BoundingBox()
					templateBrowserCheck(t, err, "读取正文视口")
					templateBrowserCheck(t, page.Mouse().Move(box.X+8, box.Y+8), "移动到正文")
					templateBrowserCheck(t, page.Mouse().Wheel(0, 5000), "滚动长消息列表")
					_, err = page.WaitForFunction("() => document.querySelector('.delivery-template-editor__body').scrollTop > 0", nil)
					templateBrowserCheck(t, err, "正文必须真正滚动")
					templateBrowserActionsVisible(t, page)
					// number 是连续两次新增后的序号；正常点击不得用 force 或直接派发事件绕过遮挡。
					for number := count + 1; number <= count+2; number++ {
						templateBrowserCheck(t, page.Locator("button:has-text('添加消息')").Click(), "点击固定添加入口")
						_, err = page.WaitForFunction(`number => {
							// input 是新增文本消息，焦点中心必须位于正文内部且未被页脚遮挡。
							const input = document.querySelector('[aria-label="第 ' + number + ' 条消息正文"]');
							if (!input || document.activeElement !== input) return false;
							// rect、body 分别是输入框和滚动视口，坐标单位为 CSS 像素。
							const rect = input.getBoundingClientRect(), body = document.querySelector('.delivery-template-editor__body').getBoundingClientRect();
							return rect.top >= body.top && rect.bottom <= body.bottom;
						}`, number)
						if err != nil {
							// geometry 仅输出测试页面几何和焦点状态，帮助区分遮挡、滚动与焦点回归。
							geometry, _ := page.Evaluate(`() => ({active:document.activeElement?.getAttribute('aria-label'),rect:document.activeElement?.getBoundingClientRect().toJSON(),body:document.querySelector('.delivery-template-editor__body').getBoundingClientRect().toJSON(),scrollTop:document.querySelector('.delivery-template-editor__body').scrollTop})`)
							t.Logf("新增焦点布局：%v", geometry)
						}
						templateBrowserCheck(t, err, "新增消息必须聚焦且滚入视口")
						templateBrowserCheck(t, page.Locator(fmt.Sprintf("textarea[aria-label='第 %d 条消息正文']", number)).Fill("新增内容"), "填写新消息")
						templateBrowserActionsVisible(t, page)
					}
					templateBrowserCheck(t, page.Locator("button:has-text('保存模板')").Click(), "保存长模板")
					templateBrowserCheck(t, page.Locator("[role='dialog']").WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateDetached}), "保存后关闭编辑器")
					templateBrowserCheck(t, page.Locator("button:has-text('编辑')").Click(), "重新打开模板")
					templateBrowserCheck(t, page.Locator("button:has-text('取消')").Click(), "取消仍然可点击")
					templateBrowserCheck(t, page.Locator("[role='dialog']").WaitFor(playwright.LocatorWaitForOptions{State: playwright.WaitForSelectorStateDetached}), "取消后关闭编辑器")
				})
			}
		}
	}
}

// templateBrowserCheck 将 err 对应的 operation 失败报告给调用测试 t，不输出平台或凭据信息。
func templateBrowserCheck(t *testing.T, err error, operation string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s：%v", operation, err)
	}
}

// templateBrowserActionsVisible 用 page 的真实命中测试确保三个操作都在固定页脚和视口内，失败由 t 报告。
func templateBrowserActionsVisible(t *testing.T, page playwright.Page) {
	t.Helper()
	// visible、err 保存几何与命中检查的布尔结果及脚本执行错误。
	visible, err := page.Evaluate(`() => {
		// footer 是不随正文滚动的操作区，所有按钮都必须属于它。
		const footer = document.querySelector('.delivery-template-editor__footer');
		return ['添加消息', '取消', '保存模板'].every(name => {
			// button 是当前操作；rect 为实际矩形，hit 为中心点击会收到事件的元素。
			const button = Array.from(document.querySelectorAll('button')).find(item => item.textContent.trim() === name);
			if (!button || !footer.contains(button)) return false;
			const rect = button.getBoundingClientRect(), hit = document.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2);
			return rect.x >= 0 && rect.y >= 0 && rect.right <= innerWidth && rect.bottom <= innerHeight && (hit === button || button.contains(hit));
		});
	}`)
	templateBrowserCheck(t, err, "检查固定操作区")
	if visible != true {
		t.Fatal("添加、取消和保存按钮必须全部可见、可命中且位于固定页脚")
	}
}

// templateBrowserServer 为 t 提供真实嵌入前端；模板请求使用本地固定数据，服务由 Cleanup 关闭。
func templateBrowserServer(t *testing.T) *httptest.Server {
	t.Helper()
	// assets、err 是应用实际嵌入的构建输出，缺失时不能回退到手写仿制页面。
	assets, err := Static()
	templateBrowserCheck(t, err, "读取嵌入资源")
	// templates、vendors、styles 是 Vite 的页面、React 和样式分片路径。
	templates, err := fs.Glob(assets, "assets/DeliveryTemplates-*.js")
	templateBrowserCheck(t, err, "查找模板页面分片")
	// vendors、err 是唯一 React 共享分片路径和文件系统匹配结果。
	vendors, err := fs.Glob(assets, "assets/react-vendor-*.js")
	templateBrowserCheck(t, err, "查找 React 分片")
	// styles、err 是所有构建样式路径和文件系统匹配结果。
	styles, err := fs.Glob(assets, "assets/*.css")
	templateBrowserCheck(t, err, "查找构建样式")
	if len(templates) != 1 || len(vendors) != 1 || len(styles) == 0 {
		t.Fatal("需要先运行 npm --prefix frontend run build 生成唯一且完整的嵌入资源")
	}
	// links 包含实际构建样式，不注入任何修复 CSS 或变更生产 DOM。
	var links strings.Builder
	// stylesheet 是本次需要加载的构建样式路径。
	for _, stylesheet := range styles {
		fmt.Fprintf(&links, `<link rel="stylesheet" href="/%s">`, stylesheet)
	}
	// html 只挂载生产组件；React 导出按能力解析，避免依赖压缩后的导出别名。
	html := fmt.Sprintf(`<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1">%s</head><body><div id="root"></div><script type="module">
	import * as vendor from '/%s'; import Page from '/%s';
	// React、ReactDOM 是构建产物中的真实模块，不复制组件或替换浏览器布局。
	const React = Object.values(vendor).find(value => value?.createElement), ReactDOM = Object.values(vendor).find(value => value?.createRoot);
	ReactDOM.createRoot(document.getElementById('root')).render(React.createElement(Page));
	</script></body></html>`, links.String(), vendors[0], templates[0])
	// mux 只提供本地模板接口、夹具入口和嵌入文件，不接触应用真实数据库。
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/delivery-templates", func(w http.ResponseWriter, r *http.Request) { // w 输出固定列表；r 的同源 Referer 决定本场景的条数与类型。
		w.Header().Set("Content-Type", "application/json")
		// count、mixed 是已知场景参数，来自本地页面地址而非真实账号。
		count, mixed := 3, false
		_, _ = fmt.Sscanf(r.Referer()[strings.LastIndex(r.Referer(), "?")+1:], "count=%d&mixed=%t", &count, &mixed)
		// messages 保持测试规定的发送顺序，图片仅用相对路径且不加载外部素材。
		messages := make([]string, 0, count)
		// index 是当前消息从零开始的发送位置。
		for index := 0; index < count; index++ {
			if mixed && index%2 == 1 {
				messages = append(messages, fmt.Sprintf(`{"id":%d,"sort_order":%d,"type":"image","content":"","image_path":"test.png"}`, index+1, index))
			} else {
				messages = append(messages, fmt.Sprintf(`{"id":%d,"sort_order":%d,"type":"text","content":"测试消息"}`, index+1, index))
			}
		}
		_, _ = fmt.Fprintf(w, `{"data":[{"id":1,"name":"滚动回归夹具","enabled":true,"keys":[],"custom_keys":[],"created_at":"","updated_at":"","messages":[%s]}]}`, strings.Join(messages, ","))
	})
	mux.HandleFunc("/api/v1/delivery-templates/1", func(w http.ResponseWriter, r *http.Request) { // w 返回保存成功；r 只允许使用生产 adapter 的 PUT 方法。
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	})
	mux.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) { // w 返回只挂载生产页面的夹具；r 无需认证或账号状态。
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, html)
	})
	mux.Handle("/assets/", http.FileServer(http.FS(assets)))
	// server 的监听、处理 goroutine 均由 httptest 拥有，Cleanup 关闭并等待请求完成。
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}
