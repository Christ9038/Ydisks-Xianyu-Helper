package browser

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// sliderSelectors 按优先级排列的滑块相关选择器（移植自 xianyu_slider_stealth.py）。
var sliderButtonSelectors = []string{
	"#nc_1_n1z", ".nc_iconfont", ".btn_slide",
	"#scratch-captcha-btn", ".scratch-captcha-slider .button",
}
var sliderTrackSelectors = []string{
	"#nc_1_n1t", ".nc_scale", ".nc_1_n1t",
}
var sliderRetrySelectors = []string{
	"#nc_1_refresh1", ".nc_iconfont.btn_refresh", ".errloading",
	"[class*='refresh']", ".nc-container",
}
var sliderExplicitRetrySelectors = sliderRetrySelectors[:4]
var sliderSuccessSelectors = []string{
	".nc_ok_icon", ".nc-lang-cnt .nc_ok", "#nc_1_n1z.nc_ok",
}

const (
	sliderSuccessCheckAttempts = 4
	sliderSuccessCheckInterval = 400 * time.Millisecond
)

// trajectoryPoint 轨迹中的单个采样点。
type trajectoryPoint struct {
	x     float64
	y     float64
	delay time.Duration
}

type slideMotionMetrics struct {
	points          int
	plannedDelay    time.Duration
	targetMovement  time.Duration
	movementElapsed time.Duration
	totalElapsed    time.Duration
	finalLeft       string
	finalClass      string
}

type sliderResetResult struct {
	method   string
	selector string
	ready    bool
	err      error
}

// generateTrajectory mirrors the reference Playwright engine's short
// acceleration/constant/deceleration trajectory. Each point is a high-level
// browser protocol call, so generating dozens of points makes the drag unlike
// the reference implementation even when the wall-clock duration is similar.
func generateTrajectory(distance float64) []trajectoryPoint {
	requestedSteps := 5 + rng.Intn(2)
	totalDuration := randomFloat(0.010, 0.020)
	avgDelay := totalDuration / float64(requestedSteps)

	accelRatio := 0.35 + randomFloat(-0.05, 0.05)
	decelRatio := 0.30 + randomFloat(-0.05, 0.05)
	accelSteps := max(2, int(math.Round(float64(requestedSteps)*accelRatio)))
	decelSteps := max(2, int(math.Round(float64(requestedSteps)*decelRatio)))
	constantSteps := max(2, requestedSteps-accelSteps-decelSteps)

	accelDistance := distance * randomFloat(0.25, 0.35)
	constantDistance := distance * randomFloat(0.50, 0.60)
	decelDistance := distance - accelDistance - constantDistance
	pts := make([]trajectoryPoint, 0, accelSteps+constantSteps+decelSteps)

	for i := 1; i <= accelSteps; i++ {
		progress := float64(i) / float64(accelSteps)
		pts = append(pts, trajectoryPoint{
			x:     accelDistance * progress * progress,
			y:     randomFloat(-1, 1),
			delay: secondsDuration(avgDelay * randomFloat(1, 1.3)),
		})
	}
	for i := 1; i <= constantSteps; i++ {
		progress := float64(i) / float64(constantSteps)
		delay := avgDelay * randomFloat(0.85, 1.15)
		if randomFloat(0, 1) < 0.03 {
			delay *= randomFloat(1.1, 1.3)
		}
		pts = append(pts, trajectoryPoint{
			x:     accelDistance + constantDistance*progress,
			y:     randomFloat(-1, 1) * 0.6,
			delay: secondsDuration(delay),
		})
	}
	for i := 1; i <= decelSteps; i++ {
		progress := float64(i) / float64(decelSteps)
		pts = append(pts, trajectoryPoint{
			x:     accelDistance + constantDistance + decelDistance*(1-math.Pow(1-progress, 2)),
			y:     randomFloat(-1, 1) * 0.4,
			delay: secondsDuration(avgDelay * randomFloat(1.1, 1.5)),
		})
	}
	if len(pts) > 0 {
		pts[len(pts)-1].x = distance
	}
	return pts
}

func randomFloat(minValue, maxValue float64) float64 {
	return minValue + rng.Float64()*(maxValue-minValue)
}

func secondsDuration(seconds float64) time.Duration {
	return time.Duration(seconds * float64(time.Second))
}

// isScratchCaptcha 判断是否为刮刮乐验证码（只滑 25-35%）。
func isScratchCaptcha(content string) bool {
	return strings.Contains(content, "scratch-captcha") ||
		strings.Contains(content, "scratch-captcha-btn") ||
		strings.Contains(content, "scratch-captcha-slider")
}

type sliderLogger interface {
	Info(string, ...any)
	Warn(string, ...any)
	Error(string, ...any)
}

// solveSlider 在 page 上求解滑块，最多重试 3 次。
func solveSlider(page playwright.Page, scratch bool, logger sliderLogger) error {
	return solveSliderStrict(page, scratch, logger, nil, time.Time{})
}

func solveSliderStrict(page playwright.Page, scratch bool, logger sliderLogger, previousX5Sec map[string]struct{}, deadline time.Time) error {
	for attempt := 0; attempt < 3; attempt++ {
		if !deadline.IsZero() && time.Now().After(deadline) {
			return fmt.Errorf("滑块验证超过浏览器总超时")
		}
		btn, track, frame, err := findSliderElements(page)
		if err != nil {
			logger.Warn("未找到滑块元素", "attempt", attempt, "err", err)
			var verified bool
			if previousX5Sec != nil {
				verified = waitForStrictSliderSuccess(page, previousX5Sec, 600*time.Millisecond, 100*time.Millisecond)
			} else {
				verified = checkSliderSuccess(page)
			}
			if verified {
				logger.Info("滑块元素消失且严格成功条件成立", "attempt", attempt+1)
				return nil
			}
			if attempt < 2 {
				reset := resetSliderForRetry(context.Background(), page, deadline)
				logSliderReset(logger, attempt+1, reset)
				if reset.err != nil {
					return fmt.Errorf("未找到滑块且无法恢复: %w", reset.err)
				}
			}
			continue
		}

		dist, err := calculateSlideDistance(btn, track, scratch)
		if err != nil {
			logger.Warn("计算滑块距离失败", "err", err)
			dist = 200 // 降级默认值
		}
		logSliderAttemptStart(logger, page, frame, btn, track, attempt+1, dist)

		motion, err := simulateSlide(page, btn, dist)
		if err != nil {
			logger.Warn("模拟滑动失败", "err", err)
			if attempt < 2 {
				reset := resetSliderForRetry(context.Background(), page, deadline)
				logSliderReset(logger, attempt+1, reset)
				if reset.err != nil {
					return fmt.Errorf("滑块执行失败后无法恢复: %w", reset.err)
				}
			}
			continue
		}
		logger.Info("滑块拖动已释放",
			"attempt", attempt+1,
			"points", motion.points,
			"planned_delay", motion.plannedDelay,
			"target_movement", motion.targetMovement,
			"movement_elapsed", motion.movementElapsed,
			"total_elapsed", motion.totalElapsed,
			"final_left", motion.finalLeft,
			"final_class", motion.finalClass,
		)
		time.Sleep(800 * time.Millisecond)

		var verified bool
		if previousX5Sec != nil {
			confirmTimeout := 5 * time.Second
			if !deadline.IsZero() {
				confirmTimeout = min(confirmTimeout, time.Until(deadline.Add(-2*time.Second)))
				if confirmTimeout < time.Second {
					confirmTimeout = time.Second
				}
			}
			verified = waitForStrictSliderSuccess(page, previousX5Sec, confirmTimeout, 300*time.Millisecond)
		} else {
			verified = checkSliderSuccess(page)
		}
		if verified {
			logger.Info("滑块验证成功", "attempt", attempt+1)
			return nil
		}
		logSliderFailureState(logger, page, attempt+1)
		if attempt < 2 {
			reset := resetSliderForRetry(context.Background(), page, deadline)
			logSliderReset(logger, attempt+1, reset)
			if reset.err != nil {
				return fmt.Errorf("滑块第 %d 次失败后无法恢复: %w", attempt+1, reset.err)
			}
			if err := sleepUntil(context.Background(), deadline, secondsDuration(randomFloat(1, 2))); err != nil {
				return err
			}
		}
	}
	return fmt.Errorf("滑块验证 3 次均失败")
}

func waitForStrictSliderSuccess(page playwright.Page, previousValues map[string]struct{}, timeout, interval time.Duration) bool {
	if timeout <= 0 || interval <= 0 {
		return false
	}
	deadline := time.Now().Add(timeout)
	for {
		cookies, err := page.Context().Cookies()
		if err == nil {
			_, fresh := freshX5Cookies(cookies, previousValues)
			if fresh && !isPunishURL(page.URL()) {
				return true
			}
		}
		if hasDefinitiveSliderFailure(page) {
			return false
		}
		if time.Now().Add(interval).After(deadline) {
			return false
		}
		time.Sleep(interval)
	}
}

func isPunishURL(rawURL string) bool {
	lower := strings.ToLower(rawURL)
	for _, marker := range []string{"punish", "x5step=2", "action=captcha", "purecaptcha", "/captcha"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// findSliderElements 在 page 和所有 iframe 中找到按钮与轨道元素。
func findSliderElements(page playwright.Page) (btn, track playwright.ElementHandle, frame playwright.Frame, err error) {
	frames := append([]playwright.Frame{page.MainFrame()}, page.Frames()...)
	// The current NC page normally exposes this exact pair. Require both
	// elements to be visible in the same frame before trying broad fallbacks.
	for _, f := range frames {
		b := queryVisible(f, "#nc_1_n1z")
		t := queryVisible(f, "#nc_1_n1t")
		if b != nil && t != nil {
			return b, t, f, nil
		}
	}
	for _, f := range frames {
		b := queryFirstVisible(f, sliderButtonSelectors)
		if b == nil {
			continue
		}
		t := queryFirstVisible(f, sliderTrackSelectors)
		if t != nil {
			return b, t, f, nil
		}
	}
	return nil, nil, nil, fmt.Errorf("未找到同一 frame 内可见的滑块按钮和轨道")
}

func queryVisible(f playwright.Frame, selector string) playwright.ElementHandle {
	el, err := f.QuerySelector(selector)
	if err != nil || el == nil || !elementVisible(el) {
		return nil
	}
	return el
}

// queryFirst is shared by non-slider browser flows that intentionally handle
// visibility after locating an element. Slider code uses queryFirstVisible.
func queryFirst(f playwright.Frame, selectors []string) playwright.ElementHandle {
	for _, selector := range selectors {
		el, err := f.QuerySelector(selector)
		if err == nil && el != nil {
			return el
		}
	}
	return nil
}

func queryFirstVisible(f playwright.Frame, selectors []string) playwright.ElementHandle {
	for _, sel := range selectors {
		if el := queryVisible(f, sel); el != nil {
			return el
		}
	}
	return nil
}

// calculateSlideDistance 计算需要滑动的像素距离。
func calculateSlideDistance(btn, track playwright.ElementHandle, scratch bool) (float64, error) {
	var dist float64
	if track != nil {
		// Use the exact usable rail width. Overshoot is interpreted as a failed
		// drag by the current captcha implementation.
		if precise, err := btn.Evaluate(`(button, track) => {
			const buttonRect = button.getBoundingClientRect();
			const trackRect = track.getBoundingClientRect();
			return trackRect.width - buttonRect.width;
		}`, track); err == nil {
			if value, ok := precise.(float64); ok && value > 0 {
				dist = value
			}
		}
		if dist <= 0 {
			tb, trackErr := track.BoundingBox()
			bb, buttonErr := btn.BoundingBox()
			if trackErr == nil && buttonErr == nil && tb != nil && bb != nil {
				dist = tb.Width - bb.Width
			}
		}
	}
	if dist <= 0 {
		dist = 220 + float64(rng.Intn(40))
	}
	if scratch {
		dist *= randomFloat(0.25, 0.35)
	}
	return dist, nil
}

// simulateSlide 模拟人类滑动，并返回协议调用后的真实墙钟耗时。
func simulateSlide(page playwright.Page, btn playwright.ElementHandle, distance float64) (slideMotionMetrics, error) {
	metrics := slideMotionMetrics{}
	totalStarted := time.Now()
	time.Sleep(secondsDuration(randomFloat(0.1, 0.3)))
	bb, err := btn.BoundingBox()
	if err != nil || bb == nil {
		return metrics, fmt.Errorf("无法获取按钮位置")
	}
	startX := bb.X + bb.Width/2
	startY := bb.Y + bb.Height/2
	mouse := page.Mouse()

	// 第一阶段：从左侧附近自然接近滑块。
	_ = mouse.Move(startX+randomFloat(-30, -10), startY+randomFloat(-15, 15),
		playwright.MouseMoveOptions{Steps: playwright.Int(5 + rng.Intn(6))})
	time.Sleep(secondsDuration(randomFloat(0.15, 0.30)))
	_ = mouse.Move(startX, startY, playwright.MouseMoveOptions{Steps: playwright.Int(3 + rng.Intn(4))})
	time.Sleep(secondsDuration(randomFloat(0.10, 0.25)))

	// 第二阶段：悬停与按下前停顿。
	_ = btn.Hover(playwright.ElementHandleHoverOptions{Timeout: playwright.Float(2000)})
	time.Sleep(secondsDuration(randomFloat(0.10, 0.30)))
	_ = mouse.Move(startX, startY)
	time.Sleep(secondsDuration(randomFloat(0.05, 0.15)))

	if err := mouse.Down(); err != nil {
		return metrics, err
	}
	time.Sleep(secondsDuration(randomFloat(0.05, 0.15)))

	pts := generateTrajectory(distance)
	metrics.points = len(pts)
	for _, pt := range pts {
		metrics.plannedDelay += pt.delay
	}
	// The reference implementation gets roughly 80-150ms of CDP round-trip
	// latency per high-level point. The Go driver is often much faster, so keep
	// the same six-point shape while compensating to the observed 480-900ms
	// movement window using wall-clock time.
	metrics.targetMovement = secondsDuration(randomFloat(0.48, 0.90))
	movementStarted := time.Now()
	currentX, currentY := startX, startY
	for index, pt := range pts {
		currentX, currentY = startX+pt.x, startY+pt.y
		if err := mouse.Move(currentX, currentY, playwright.MouseMoveOptions{Steps: playwright.Int(1 + rng.Intn(3))}); err != nil {
			_ = mouse.Up()
			return metrics, err
		}
		planned := time.Duration(float64(pt.delay) * randomFloat(0.9, 1.1))
		remainingPoints := len(pts) - index
		delay := compensatedTrajectoryDelay(planned, metrics.targetMovement, time.Since(movementStarted), remainingPoints)
		time.Sleep(delay)
	}
	metrics.movementElapsed = time.Since(movementStarted)
	if isScratchCaptchaFromPage(page) {
		time.Sleep(secondsDuration(randomFloat(0.3, 0.5)))
	}
	time.Sleep(secondsDuration(randomFloat(0.02, 0.05)))
	if err := mouse.Up(); err != nil {
		return metrics, err
	}
	time.Sleep(secondsDuration(randomFloat(0.01, 0.03)))
	_, _ = btn.Evaluate(`(slider, point) => {
		const event = new MouseEvent('click', {
			bubbles: true,
			cancelable: true,
			view: window,
			clientX: point.x,
			clientY: point.y,
			button: 0,
		});
		slider.dispatchEvent(event);
	}`, map[string]any{"x": currentX, "y": currentY})
	metrics.finalLeft = readSliderLeft(btn)
	metrics.finalClass, _ = btn.GetAttribute("class")
	metrics.totalElapsed = time.Since(totalStarted)
	return metrics, nil
}

func compensatedTrajectoryDelay(planned, target, elapsed time.Duration, remainingPoints int) time.Duration {
	if remainingPoints <= 0 || target <= elapsed {
		return planned
	}
	compensated := (target - elapsed) / time.Duration(remainingPoints)
	if compensated > planned {
		return compensated
	}
	return planned
}

func readSliderLeft(button playwright.ElementHandle) string {
	value, err := button.Evaluate(`button => {
		if (button.style && button.style.left) return button.style.left;
		const parent = button.parentElement;
		if (!parent) return null;
		return (button.getBoundingClientRect().left - parent.getBoundingClientRect().left) + 'px';
	}`)
	if err != nil || value == nil {
		return "<unavailable>"
	}
	return fmt.Sprint(value)
}

func isScratchCaptchaFromPage(page playwright.Page) bool {
	content, err := page.Content()
	return err == nil && isScratchCaptcha(content)
}

type sliderContainerState struct {
	found           bool
	visible         bool
	visibilityKnown bool
}

// checkSliderSuccess 检查验证是否成功（nc-container 消失或 frame 断开）。
func checkSliderSuccess(page playwright.Page) bool {
	for attempt := 0; attempt < sliderSuccessCheckAttempts; attempt++ {
		if sliderContainerStatesSucceeded(readSliderContainerStates(page)) || hasVisibleSliderSuccessMarker(page) {
			return true
		}
		if attempt+1 < sliderSuccessCheckAttempts {
			time.Sleep(sliderSuccessCheckInterval)
		}
	}
	return false
}

func hasVisibleSliderSuccessMarker(page playwright.Page) bool {
	frames := append([]playwright.Frame{page.MainFrame()}, page.Frames()...)
	for _, frame := range frames {
		if queryFirstVisible(frame, sliderSuccessSelectors) != nil {
			return true
		}
	}
	return false
}

func readSliderContainerStates(page playwright.Page) []sliderContainerState {
	frames := append([]playwright.Frame{page.MainFrame()}, page.Frames()...)
	states := make([]sliderContainerState, 0, len(frames))
	for _, f := range frames {
		el, err := f.QuerySelector(".nc-container")
		if err != nil || el == nil {
			continue
		}
		vis, err := el.IsVisible()
		states = append(states, sliderContainerState{
			found:           true,
			visible:         vis,
			visibilityKnown: err == nil,
		})
	}
	return states
}

func sliderContainerStatesSucceeded(states []sliderContainerState) bool {
	for _, state := range states {
		if state.found && (!state.visibilityKnown || state.visible) {
			return false
		}
	}
	return true
}

func clickRetry(page playwright.Page) (string, error) {
	frames := append([]playwright.Frame{page.MainFrame()}, page.Frames()...)
	for _, f := range frames {
		for _, sel := range sliderExplicitRetrySelectors {
			if el := queryVisible(f, sel); el != nil {
				if err := el.Click(playwright.ElementHandleClickOptions{
					Timeout: playwright.Float(2000),
				}); err != nil {
					return sel, err
				}
				return sel, nil
			}
		}
		// .nc-container is only safe to click once it actually contains a
		// failure/retry prompt. Clicking the initial container can start a drag.
		if el := queryVisible(f, ".nc-container"); el != nil {
			text, _ := el.InnerText()
			if sliderRetryText(text) {
				if err := el.Click(playwright.ElementHandleClickOptions{Timeout: playwright.Float(2000)}); err != nil {
					return ".nc-container", err
				}
				return ".nc-container", nil
			}
		}
	}
	return "", fmt.Errorf("未找到可见的滑块失败重试控件")
}

func sliderRetryText(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, marker := range []string{"重试", "刷新", "失败", "retry", "refresh", "failed"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func resetSliderForRetry(ctx context.Context, page playwright.Page, deadline time.Time) sliderResetResult {
	result := sliderResetResult{}
	selector, clickErr := clickRetry(page)
	if clickErr != nil {
		// AWSC can publish the failure control shortly after its verification
		// request completes. Give it a brief chance before reloading the page.
		waitDeadline := boundedDeadline(deadline, 800*time.Millisecond)
		for time.Now().Before(waitDeadline) {
			if err := sleepUntil(ctx, waitDeadline, 100*time.Millisecond); err != nil {
				break
			}
			selector, clickErr = clickRetry(page)
			if clickErr == nil {
				break
			}
		}
	}
	if clickErr == nil {
		result.method = "click"
		result.selector = selector
		result.ready = waitForSliderReady(ctx, page, boundedDeadline(deadline, 4*time.Second))
		if result.ready {
			return result
		}
	}

	result.method = "reload"
	reloadTimeout := boundedDuration(deadline, 8*time.Second)
	if reloadTimeout <= 0 {
		result.err = fmt.Errorf("滑块恢复前已超过总超时")
		return result
	}
	_, reloadErr := page.Reload(playwright.PageReloadOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(float64(reloadTimeout.Milliseconds())),
	})
	if reloadErr != nil {
		result.err = fmt.Errorf("重载滑块验证页: %w", reloadErr)
		return result
	}
	result.ready = waitForSliderReady(ctx, page, boundedDeadline(deadline, 5*time.Second))
	if !result.ready {
		result.err = fmt.Errorf("重载后滑块未重新出现")
	}
	return result
}

func waitForSliderReady(ctx context.Context, page playwright.Page, deadline time.Time) bool {
	for {
		btn, track, _, err := findSliderElements(page)
		if err == nil && sliderAtOrigin(btn, track) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		if err := sleepUntil(ctx, deadline, 100*time.Millisecond); err != nil {
			return false
		}
	}
}

func sliderAtOrigin(btn, track playwright.ElementHandle) bool {
	buttonBox, buttonErr := btn.BoundingBox()
	trackBox, trackErr := track.BoundingBox()
	if buttonErr != nil || trackErr != nil || buttonBox == nil || trackBox == nil {
		return false
	}
	return math.Abs(buttonBox.X-trackBox.X) <= 3
}

func boundedDeadline(deadline time.Time, limit time.Duration) time.Time {
	bounded := time.Now().Add(limit)
	if !deadline.IsZero() && deadline.Before(bounded) {
		return deadline
	}
	return bounded
}

func boundedDuration(deadline time.Time, limit time.Duration) time.Duration {
	if deadline.IsZero() {
		return limit
	}
	remaining := time.Until(deadline)
	if remaining < limit {
		return remaining
	}
	return limit
}

func logSliderAttemptStart(logger sliderLogger, page playwright.Page, frame playwright.Frame, btn, track playwright.ElementHandle, attempt int, distance float64) {
	buttonBox, _ := btn.BoundingBox()
	trackBox, _ := track.BoundingBox()
	buttonClass, _ := btn.GetAttribute("class")
	trackClass, _ := track.GetAttribute("class")
	interference := detectInjectedPageTools(page)
	logger.Info("滑块拖动准备",
		"attempt", attempt,
		"page", redactedPageURL(page.URL()),
		"frame", redactedPageURL(frame.URL()),
		"distance_px", fmt.Sprintf("%.2f", distance),
		"button_box", formatBoundingBox(buttonBox),
		"track_box", formatBoundingBox(trackBox),
		"button_class", buttonClass,
		"track_class", trackClass,
		"injected_tools", strings.Join(interference, ","),
	)
}

func logSliderFailureState(logger sliderLogger, page playwright.Page, attempt int) {
	button, track, _, _ := findSliderElements(page)
	buttonStyle, buttonClass, trackClass := "<missing>", "<missing>", "<missing>"
	if button != nil {
		buttonStyle, _ = button.GetAttribute("style")
		buttonClass, _ = button.GetAttribute("class")
	}
	if track != nil {
		trackClass, _ = track.GetAttribute("class")
	}
	retrySelector, retryText := visibleSliderRetryState(page)
	logger.Warn("滑块验证失败态",
		"attempt", attempt,
		"page", redactedPageURL(page.URL()),
		"button_style", buttonStyle,
		"button_class", buttonClass,
		"track_class", trackClass,
		"retry_selector", retrySelector,
		"retry_text", retryText,
	)
}

func logSliderReset(logger sliderLogger, attempt int, reset sliderResetResult) {
	if reset.err != nil {
		logger.Warn("滑块失败后恢复失败", "attempt", attempt, "method", reset.method, "selector", reset.selector, "err", reset.err)
		return
	}
	logger.Info("滑块失败后已恢复", "attempt", attempt, "method", reset.method, "selector", reset.selector, "ready", reset.ready)
}

func visibleSliderRetryState(page playwright.Page) (string, string) {
	frames := append([]playwright.Frame{page.MainFrame()}, page.Frames()...)
	for _, frame := range frames {
		for _, selector := range sliderExplicitRetrySelectors {
			if el := queryVisible(frame, selector); el != nil {
				text, _ := el.InnerText()
				return selector, truncateSliderText(text)
			}
		}
	}
	return "", ""
}

func hasDefinitiveSliderFailure(page playwright.Page) bool {
	frames := append([]playwright.Frame{page.MainFrame()}, page.Frames()...)
	for _, frame := range frames {
		for _, selector := range sliderRetrySelectors[:3] {
			if queryVisible(frame, selector) != nil {
				return true
			}
		}
	}
	return false
}

func detectInjectedPageTools(page playwright.Page) []string {
	markers := []struct {
		name     string
		selector string
	}{
		{name: "requestly", selector: "rq-implicit-test-rule-widget"},
		{name: "pikpak", selector: "#__PIKPAK_EXTENSION__"},
		{name: "deepl", selector: "deepl-input-controller"},
		{name: "immersive-translate", selector: "#immersive-translate-browser-popup"},
	}
	found := make([]string, 0, len(markers))
	for _, marker := range markers {
		if el, err := page.MainFrame().QuerySelector(marker.selector); err == nil && el != nil {
			found = append(found, marker.name)
		}
	}
	return found
}

func redactedPageURL(rawURL string) string {
	if index := strings.IndexAny(rawURL, "?#"); index >= 0 {
		return rawURL[:index]
	}
	return rawURL
}

func formatBoundingBox(box *playwright.Rect) string {
	if box == nil {
		return "<missing>"
	}
	return fmt.Sprintf("x=%.1f y=%.1f w=%.1f h=%.1f", box.X, box.Y, box.Width, box.Height)
}

func truncateSliderText(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) > 160 {
		return text[:160]
	}
	return text
}

// extractPageCookies 从 page 的 context 提取所有 cookie 返回 map。
func extractPageCookies(page playwright.Page) (map[string]string, error) {
	all, err := page.Context().Cookies()
	if err != nil {
		return nil, err
	}
	return cookiesToMap(all), nil
}
