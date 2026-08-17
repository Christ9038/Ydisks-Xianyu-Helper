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
	// sliderContinuousEventMaxInterval 是主引擎按住期间相邻微移动事件的最大间隔，保证提速后仍呈连续轨迹。
	sliderContinuousEventMaxInterval = 15 * time.Millisecond
	// sliderMovementMinDuration、sliderMovementMaxDuration 是主引擎按下到释放的目标墙钟范围，较上一版提速一倍。
	sliderMovementMinDuration = 550 * time.Millisecond
	sliderMovementMaxDuration = 1050 * time.Millisecond
)

// trajectoryPoint 轨迹中的单个采样点。
type trajectoryPoint struct {
	// x、y 是相对滑块中心的曲线锚点偏移；x 保持单调前进并在末点落到受控超调终点。
	x float64
	y float64
	// delay 是该锚点对应的相对时长权重；连续微移动会按该权重分配时间，不能形成锚点后的静止停顿。
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

// generateTrajectory 根据 distance 和 verticalSpan 生成标准 NC 的单调三阶段曲线锚点。
// distance 是按钮可用移动距离；verticalSpan 是 Y 轴峰峰值，锚点之间由连续微移动连接，末点会受控超出可用距离。
func generateTrajectory(distance, verticalSpan float64) []trajectoryPoint {
	// requestedSteps 是本次高层鼠标移动采样数，范围避免固定轨迹特征，同时保持在 Chromium CDP 的低开销区间。
	requestedSteps := 8 + rng.Intn(5)
	// averageDelay 是各采样点的基础等待秒数；实际总时长由 simulateSlide 的墙钟预算补偿。
	averageDelay := randomFloat(0.045, 0.085)
	// accelRatio、decelRatio 分别限定加速和减速阶段所占轨迹比例，余量构成中速前进阶段。
	accelRatio := randomFloat(0.26, 0.34)
	decelRatio := randomFloat(0.24, 0.32)
	// accelSteps、decelSteps、constantSteps 是三段分别包含的采样点数，至少各保留两个点以避免变成直线移动。
	accelSteps := max(2, int(math.Round(float64(requestedSteps)*accelRatio)))
	decelSteps := max(2, int(math.Round(float64(requestedSteps)*decelRatio)))
	constantSteps := max(2, requestedSteps-accelSteps-decelSteps)

	// overshoot 是鼠标终点超出可用滑轨的像素量；页面控件自身会在轨道边界钳位，因此不会被拖出滑轨。
	overshoot := randomFloat(4, 20)
	// targetDistance 是鼠标实际行进的水平目标，包含受控超调；三段距离相加后在末点落到该目标。
	targetDistance := distance + overshoot
	// accelDistance、constantDistance、decelDistance 是三段覆盖的水平距离；三者相加始终等于带超调的鼠标目标距离。
	accelDistance := targetDistance * randomFloat(0.24, 0.34)
	constantDistance := targetDistance * randomFloat(0.46, 0.56)
	decelDistance := targetDistance - accelDistance - constantDistance
	// points 保存按加速、中速、减速依次生成的高层鼠标坐标。
	points := make([]trajectoryPoint, 0, accelSteps+constantSteps+decelSteps)

	// step 表示当前加速段采样点序号。
	for step := 1; step <= accelSteps; step++ {
		// progress 是当前采样点在加速段中的归一化进度。
		progress := float64(step) / float64(accelSteps)
		points = append(points, trajectoryPoint{
			x:     accelDistance * progress * progress,
			delay: secondsDuration(averageDelay * randomFloat(0.70, 1.20)),
		})
	}
	// step 表示当前中速段采样点序号。
	for step := 1; step <= constantSteps; step++ {
		// progress 是当前采样点在中速段中的归一化进度。
		progress := float64(step) / float64(constantSteps)
		// delay 是中速段本点后的基础等待，刻意保留差异而非固定间隔。
		delay := averageDelay * randomFloat(0.65, 1.25)
		points = append(points, trajectoryPoint{
			x:     accelDistance + constantDistance*progress,
			delay: secondsDuration(delay),
		})
	}
	// step 表示当前减速段采样点序号。
	for step := 1; step <= decelSteps; step++ {
		// progress 是当前采样点在减速段中的归一化进度。
		progress := float64(step) / float64(decelSteps)
		points = append(points, trajectoryPoint{
			x:     accelDistance + constantDistance + decelDistance*(1-math.Pow(1-progress, 2)),
			delay: secondsDuration(averageDelay * randomFloat(0.90, 1.55)),
		})
	}
	applyContinuousVerticalDrift(points, verticalSpan)
	if len(points) > 0 {
		points[len(points)-1].x = targetDistance
	}
	return points
}

// expandContinuousTrajectory 把稀疏曲线锚点展开为连续事件流，避免鼠标在每个锚点后肉眼可见地停住。
// points 是调用方生成的有序锚点；target 是按下到释放的目标总时长；返回点中的 delay 不超过连续事件允许的最大间隔。
func expandContinuousTrajectory(points []trajectoryPoint, target time.Duration) []trajectoryPoint {
	// totalWeight 汇总各锚点的相对时长权重，以便保持加速、中速、减速三段的时间比例。
	var totalWeight time.Duration
	for _, point := range points {
		totalWeight += point.delay
	}
	if len(points) == 0 || target <= 0 || totalWeight <= 0 {
		return nil
	}
	// expanded 保存连接每一对锚点后的连续微移动事件。
	expanded := make([]trajectoryPoint, 0, len(points)*8)
	// previous 是本段的起点；首次从按钮中心开始，后续从前一个锚点继续。
	previous := trajectoryPoint{}
	for _, point := range points {
		// segmentDuration 是本段在总拖动窗口中所占时长，按生成轨迹的非均匀权重分配。
		segmentDuration := time.Duration(int64(target) * int64(point.delay) / int64(totalWeight))
		if segmentDuration <= 0 {
			segmentDuration = time.Nanosecond
		}
		// eventCount 是本段需要发出的微移动数量，预留节奏变化空间且至少两个事件以避免跨锚点跳跃。
		eventCount := max(2, int(math.Ceil(float64(segmentDuration)/float64(sliderContinuousEventMaxInterval)*1.34)))
		// rhythmPhase、rhythmRate 为本段节奏曲线的随机相位和周期，产生平滑的快慢变化而不是逐点抖动。
		rhythmPhase := randomFloat(0, 2*math.Pi)
		rhythmRate := randomFloat(0.8, 1.6)
		// eventWeights 保存每个微移动的相对时长，归一化后总和仍等于本段预算。
		eventWeights := make([]float64, eventCount)
		var weightTotal float64
		for eventIndex := range eventWeights {
			// progress 是本段节奏曲线的归一化位置；较低权重代表更快移动，较高权重代表更慢移动。
			progress := float64(eventIndex) / float64(max(1, eventCount-1))
			weight := 1 + 0.22*math.Sin(2*math.Pi*rhythmRate*progress+rhythmPhase)
			eventWeights[eventIndex] = weight
			weightTotal += weight
		}
		for eventIndex := 1; eventIndex <= eventCount; eventIndex++ {
			// progress 是当前微移动在本段中的线性进度，使拖动一直前进而不在锚点处驻留。
			progress := float64(eventIndex) / float64(eventCount)
			// eventDelay 是归一化后的本次微移动间隔，确保节奏变化不突破连续事件上限。
			eventDelay := time.Duration(float64(segmentDuration) * eventWeights[eventIndex-1] / weightTotal)
			if eventDelay <= 0 {
				eventDelay = time.Nanosecond
			}
			expanded = append(expanded, trajectoryPoint{
				x:     previous.x + (point.x-previous.x)*progress,
				y:     previous.y + (point.y-previous.y)*progress,
				delay: eventDelay,
			})
		}
		previous = point
	}
	return expanded
}

// applyContinuousVerticalDrift 把 Y 坐标改为连续的正弦形曲线。
// points 由调用方拥有；verticalSpan 是轨迹实际达到的 Y 轴峰峰值，非正值或不足三个点时保留水平路径作为降级行为。
func applyContinuousVerticalDrift(points []trajectoryPoint, verticalSpan float64) {
	if len(points) < 3 || verticalSpan <= 0 {
		return
	}
	// direction 决定本次先向上还是先向下偏移，避免每次都从相同方向开始。
	direction := 1.0
	if rng.Intn(2) == 0 {
		direction = -1
	}
	// rawY 保存未缩放的完整正弦周期；minY、maxY 用于把离散采样后的实际峰峰值精确映射到滑轨高度的一半。
	rawY := make([]float64, len(points))
	minY, maxY := math.Inf(1), math.Inf(-1)
	// pointIndex 是当前轨迹点下标；pointCount 是轨迹总点数减一，用于得到闭区间的归一化进度。
	pointCount := len(points) - 1
	for pointIndex := range points {
		// progress 是从按下到释放的归一化进度，正弦曲线让纵向移动连续且不呈线性变化。
		progress := float64(pointIndex) / float64(pointCount)
		rawY[pointIndex] = math.Sin(2 * math.Pi * progress)
		minY = min(minY, rawY[pointIndex])
		maxY = max(maxY, rawY[pointIndex])
	}
	// scale 把当前离散采样的最大纵向范围精确缩放到请求的峰峰值。
	scale := verticalSpan / (maxY - minY)
	// centre 让曲线围绕滑块中心上下摆动，而不是单向贴近轨道边缘。
	centre := (minY + maxY) / 2
	for pointIndex := range points {
		points[pointIndex].y = direction * (rawY[pointIndex] - centre) * scale
	}
	points[0].y = 0
	points[len(points)-1].y = 0
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

		motion, err := simulateSlide(page, btn, track, dist)
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
			// reset 是已完成拖动后点击页面失败按钮得到的恢复结果；该路径禁止 reload，避免丢失平台重新生成的滑块状态。
			reset := retrySliderAfterFailedDrag(context.Background(), page, deadline)
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
// btn 和 track 必须来自同一 frame；distance 是精确水平距离，纵向范围优先使用 track 的实际高度。
func simulateSlide(page playwright.Page, btn, track playwright.ElementHandle, distance float64) (slideMotionMetrics, error) {
	metrics := slideMotionMetrics{}
	totalStarted := time.Now()
	time.Sleep(secondsDuration(randomFloat(0.1, 0.3)))
	bb, err := btn.BoundingBox()
	if err != nil || bb == nil {
		return metrics, fmt.Errorf("无法获取按钮位置")
	}
	startX := bb.X + bb.Width/2
	startY := bb.Y + bb.Height/2
	// verticalSpan 是拖动 Y 坐标的峰峰值，默认使用按钮高度的一半；track 可测量时改用实际滑轨高度的一半。
	verticalSpan := bb.Height / 2
	// trackBox、trackErr 保存同一滑轨的几何信息及其读取错误；读取失败时保持按钮高度的安全降级值。
	trackBox, trackErr := track.BoundingBox()
	if trackErr == nil && trackBox != nil && trackBox.Height > 0 {
		verticalSpan = trackBox.Height / 2
	}
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

	pts := generateTrajectory(distance, verticalSpan)
	metrics.points = len(pts)
	// point 是当前曲线锚点；plannedDelay 汇总锚点相对时长权重，便于诊断曲线规划。
	for _, point := range pts {
		metrics.plannedDelay += point.delay
	}
	// targetMovement 是拖动阶段的目标墙钟时长，保持连续且非匀速的前提下按用户要求提速一倍。
	metrics.targetMovement = secondsDuration(randomFloat(
		float64(sliderMovementMinDuration)/float64(time.Second),
		float64(sliderMovementMaxDuration)/float64(time.Second),
	))
	// continuousPoints 是在相邻曲线锚点间持续分发的微移动事件；按下后至释放前不允许插入独立的静止停顿。
	continuousPoints := expandContinuousTrajectory(pts, metrics.targetMovement)
	movementStarted := time.Now()
	currentX, currentY := startX, startY
	// scheduledElapsed 是当前微移动完成后的目标累计时间；浏览器调用本身消耗的时间会从后续等待中扣除。
	var scheduledElapsed time.Duration
	for _, pt := range continuousPoints {
		currentX, currentY = startX+pt.x, startY+pt.y
		if err := mouse.Move(currentX, currentY, playwright.MouseMoveOptions{Steps: playwright.Int(1)}); err != nil {
			_ = mouse.Up()
			return metrics, err
		}
		// scheduledElapsed 按平滑节奏曲线推进，保证整段的快慢变化不会被浏览器调用开销累积拉长。
		scheduledElapsed += pt.delay
		// delay 是到下一计划时间点的剩余等待；非正值表示浏览器调用已耗尽本次等待预算，应立即继续移动。
		delay := scheduledElapsed - time.Since(movementStarted)
		if delay > 0 {
			time.Sleep(delay)
		}
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

// retrySliderAfterFailedDrag 在一次拖动已经完成且页面明确失败后，点击平台提供的失败按钮重新生成滑块。
// ctx 控制等待；page 是当前验证页；deadline 是浏览器总期限；该函数绝不 reload 页面，调用方应保留本次失败现场。
func retrySliderAfterFailedDrag(ctx context.Context, page playwright.Page, deadline time.Time) sliderResetResult {
	return clickSliderRetry(ctx, page, deadline)
}

// clickSliderRetry 等待并点击可见的失败重试控件，然后确认新滑块已回到轨道起点。
// ctx 控制轮询；page 是当前验证页；deadline 限制等待；返回结果只描述点击恢复，绝不包含页面刷新。
func clickSliderRetry(ctx context.Context, page playwright.Page, deadline time.Time) sliderResetResult {
	// result 保存重试按钮点击与新滑块归位的观察结果，供调用方决定是否允许后续刷新恢复。
	result := sliderResetResult{}
	// selector、clickErr 分别保存命中的失败按钮选择器和首次点击结果。
	selector, clickErr := clickRetry(page)
	if clickErr != nil {
		// waitDeadline 给平台渲染失败按钮一个短暂窗口；轮询期间不刷新当前验证页。
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
	if clickErr != nil {
		result.err = fmt.Errorf("未找到可点击的滑块失败重试控件: %w", clickErr)
		return result
	}
	result.method = "click"
	result.selector = selector
	result.ready = waitForSliderReady(ctx, page, boundedDeadline(deadline, 4*time.Second))
	if !result.ready {
		result.err = fmt.Errorf("点击滑块失败重试控件后未重新归位")
	}
	return result
}

func resetSliderForRetry(ctx context.Context, page playwright.Page, deadline time.Time) sliderResetResult {
	// result 保存先尝试点击恢复后的结果；只有非拖动失败的调用方才允许在其失败时回退到 reload。
	result := clickSliderRetry(ctx, page, deadline)
	if result.err == nil {
		return result
	}

	// reloadResult 清除点击失败的细节，记录初始页面恢复场景允许使用的页面重载结果。
	reloadResult := sliderResetResult{method: "reload"}
	reloadTimeout := boundedDuration(deadline, 8*time.Second)
	if reloadTimeout <= 0 {
		reloadResult.err = fmt.Errorf("滑块恢复前已超过总超时")
		return reloadResult
	}
	_, reloadErr := page.Reload(playwright.PageReloadOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(float64(reloadTimeout.Milliseconds())),
	})
	if reloadErr != nil {
		reloadResult.err = fmt.Errorf("重载滑块验证页: %w", reloadErr)
		return reloadResult
	}
	reloadResult.ready = waitForSliderReady(ctx, page, boundedDeadline(deadline, 5*time.Second))
	if !reloadResult.ready {
		reloadResult.err = fmt.Errorf("重载后滑块未重新出现")
	}
	return reloadResult
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
