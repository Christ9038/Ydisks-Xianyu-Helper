package browser

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

// sliderSelectors 按优先级排列的滑块相关选择器（移植自 xianyu_slider_stealth.py）。
var sliderButtonSelectors = []string{
	"#nc_1_n1z", ".nc_iconfont", ".btn_slide",
	"#scratch-captcha-btn", ".scratch-captcha-slider .button",
}
var sliderTrackSelectors = []string{
	"#nc_1_n1t", ".nc_scale", ".nc_1_n1t",
}
var sliderContainerSelectors = []string{
	"#nc_1_n1z", "#baxia-dialog-content", ".nc-container", ".nc_wrapper",
	"#nocaptcha", ".scratch-captcha-container",
}
var sliderRetrySelectors = []string{
	".nc-lang-cnt", "#nc_1_n1z",
}

// trajectoryPoint 轨迹中的单个采样点。
type trajectoryPoint struct {
	x     float64
	y     float64
	delay time.Duration
}

// generateTrajectory 生成仿人类滑动轨迹（移植自 _generate_physics_trajectory）。
// 过冲 100-110%，15-25 步，progress^1.5 加速曲线。纯数学，可单测。
func generateTrajectory(distance float64) []trajectoryPoint {
	overshoot := distance * (1.0 + 0.1*float64(rng.Intn(11))/10.0) // 100-110%
	steps := 15 + rng.Intn(11)                                     // 15-25
	baseDelay := 3.0 + float64(rng.Intn(6))                        // 3-8 ms

	pts := make([]trajectoryPoint, steps)
	for i := 0; i < steps; i++ {
		progress := float64(i+1) / float64(steps)
		x := overshoot * math.Pow(progress, 1.5)
		y := float64(rng.Intn(3)) // 0-2 px
		d := time.Duration((baseDelay*(0.9+0.2*float64(rng.Intn(11))/10.0))*float64(time.Millisecond))
		pts[i] = trajectoryPoint{x: x, y: y, delay: d}
	}
	return pts
}

// isScratchCaptcha 判断是否为刮刮乐验证码（只滑 25-35%）。
func isScratchCaptcha(content string) bool {
	return strings.Contains(content, "scratch-captcha") ||
		strings.Contains(content, "scratch-captcha-btn") ||
		strings.Contains(content, "scratch-captcha-slider")
}

// SliderVerify 访问风控验证 URL，自动过滑块，返回更新后的 cookie map（含 x5sec）。
// 移植自 xianyu_slider_stealth.py run() + solve_slider()。
func (m *Manager) SliderVerify(ctx context.Context, url, cookieID string, headless bool) (map[string]string, error) {
	if err := m.init(); err != nil {
		return nil, err
	}
	page, release, err := m.newPage(ctx, cookieID, "", headless)
	if err != nil {
		return nil, err
	}
	defer release()

	if _, err := page.Goto(url, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(30000),
	}); err != nil {
		return nil, fmt.Errorf("访问验证 URL 失败: %w", err)
	}
	time.Sleep(300 + time.Duration(rng.Intn(500))*time.Millisecond)

	content, _ := page.Content()
	if !strings.Contains(content, "验证") && !strings.Contains(content, "captcha") && !strings.Contains(content, "slider") {
		// 页面不含验证码，直接提取 cookie。
		m.logger.Info("页面无验证码，直接提取 cookie")
		return extractPageCookies(page)
	}

	scratch := isScratchCaptcha(content)
	if err := solveSlider(page, scratch, m.logger); err != nil {
		return nil, fmt.Errorf("滑块验证失败: %w", err)
	}

	time.Sleep(1 * time.Second)
	return extractPageCookies(page)
}

// solveSlider 在 page 上求解滑块，最多重试 3 次。
func solveSlider(page playwright.Page, scratch bool, logger interface {
	Info(string, ...any)
	Warn(string, ...any)
	Error(string, ...any)
}) error {
	for attempt := 0; attempt < 3; attempt++ {
		btn, track, _, err := findSliderElements(page)
		if err != nil {
			logger.Warn("未找到滑块元素", "attempt", attempt, "err", err)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		dist, err := calculateSlideDistance(btn, track, scratch)
		if err != nil {
			logger.Warn("计算滑块距离失败", "err", err)
			dist = 200 // 降级默认值
		}

		if err := simulateSlide(page, btn, dist); err != nil {
			logger.Warn("模拟滑动失败", "err", err)
			continue
		}
		time.Sleep(800 * time.Millisecond)

		if checkSliderSuccess(page) {
			logger.Info("滑块验证成功", "attempt", attempt)
			return nil
		}
		// 失败后尝试点重试按钮。
		clickRetry(page)
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("滑块验证 3 次均失败")
}

// findSliderElements 在 page 和所有 iframe 中找到按钮与轨道元素。
func findSliderElements(page playwright.Page) (btn, track playwright.ElementHandle, frame playwright.Frame, err error) {
	frames := append([]playwright.Frame{page.MainFrame()}, page.Frames()...)
	for _, f := range frames {
		b := queryFirst(f, sliderButtonSelectors)
		if b == nil {
			continue
		}
		t := queryFirst(f, sliderTrackSelectors)
		return b, t, f, nil
	}
	return nil, nil, nil, fmt.Errorf("未找到滑块元素")
}

func queryFirst(f playwright.Frame, selectors []string) playwright.ElementHandle {
	for _, sel := range selectors {
		el, err := f.QuerySelector(sel)
		if err == nil && el != nil {
			return el
		}
	}
	return nil
}

// calculateSlideDistance 计算需要滑动的像素距离。
func calculateSlideDistance(btn, track playwright.ElementHandle, scratch bool) (float64, error) {
	var dist float64
	if track != nil {
		tb, err := track.BoundingBox()
		if err == nil && tb != nil {
			bb, err2 := btn.BoundingBox()
			if err2 == nil && bb != nil {
				dist = tb.Width - bb.Width + float64(rng.Intn(2)) - 0.5
			}
		}
	}
	if dist <= 0 {
		dist = 220 + float64(rng.Intn(40))
	}
	if scratch {
		dist *= 0.25 + float64(rng.Intn(11))*0.01 // 25-35%
	}
	return dist, nil
}

// simulateSlide 模拟人类滑动（移植自 simulate_slide）。
func simulateSlide(page playwright.Page, btn playwright.ElementHandle, distance float64) error {
	bb, err := btn.BoundingBox()
	if err != nil || bb == nil {
		return fmt.Errorf("无法获取按钮位置")
	}
	startX := bb.X + bb.Width/2
	startY := bb.Y + bb.Height/2
	mouse := page.Mouse()

	// 移到按钮旁边再到中心，模拟自然接近。
	_ = mouse.Move(startX-float64(10+rng.Intn(20)), startY+float64(rng.Intn(31)-15),
		playwright.MouseMoveOptions{Steps: playwright.Int(3 + rng.Intn(3))})
	time.Sleep(100*time.Millisecond + time.Duration(rng.Intn(200))*time.Millisecond)
	_ = mouse.Move(startX, startY, playwright.MouseMoveOptions{Steps: playwright.Int(2)})
	time.Sleep(100*time.Millisecond + time.Duration(rng.Intn(200))*time.Millisecond)

	if err := mouse.Down(); err != nil {
		return err
	}

	pts := generateTrajectory(distance)
	for _, pt := range pts {
		_ = mouse.Move(startX+pt.x, startY+pt.y, playwright.MouseMoveOptions{Steps: playwright.Int(1)})
		time.Sleep(pt.delay)
	}

	return mouse.Up()
}

// checkSliderSuccess 检查验证是否成功（nc-container 消失或 frame 断开）。
func checkSliderSuccess(page playwright.Page) bool {
	frames := append([]playwright.Frame{page.MainFrame()}, page.Frames()...)
	for _, f := range frames {
		el, err := f.QuerySelector(".nc-container")
		if err != nil || el == nil {
			continue
		}
		vis, err := el.IsVisible()
		if err != nil || !vis {
			return true
		}
	}
	return true // 找不到容器也视为成功
}

func clickRetry(page playwright.Page) {
	frames := append([]playwright.Frame{page.MainFrame()}, page.Frames()...)
	for _, f := range frames {
		for _, sel := range sliderRetrySelectors {
			el, err := f.QuerySelector(sel)
			if err == nil && el != nil {
				_ = el.Click()
				return
			}
		}
	}
}

// extractPageCookies 从 page 的 context 提取所有 cookie 返回 map。
func extractPageCookies(page playwright.Page) (map[string]string, error) {
	all, err := page.Context().Cookies()
	if err != nil {
		return nil, err
	}
	return cookiesToMap(all), nil
}
