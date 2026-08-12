package browser

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"
)

var fallbackTrackSelectors = []string{
	"#nc_1_n1t", ".nc_scale",
	"#nc_1__scale_text", ".nc-lang-cnt", "#nc_1_wrapper", ".nc_wrapper",
}

// tokenCaptchaCDPFallback 直接启动 Chromium 并通过 CDP 连接，复现参考项目的
// DrissionPage 第二引擎：它不复用 Playwright launch 协议，使用同一持久化目录和另一套轨迹。
func (m *Manager) tokenCaptchaCDPFallback(ctx context.Context, cookieID, cookieStr, verificationURL string, headless bool, _ TokenCaptchaURLProvider) (string, error) {
	if strings.TrimSpace(cookieStr) == "" || strings.TrimSpace(verificationURL) == "" {
		return "", fmt.Errorf("备用滑块引擎参数不完整")
	}
	releaseSlot, err := m.acquireRenewSlot(ctx)
	if err != nil {
		return "", err
	}
	defer releaseSlot()
	lock := m.accountRenewLock(cookieID)
	lock.Lock()
	defer lock.Unlock()
	if err := m.init(); err != nil {
		return "", err
	}

	profileDir, err := resolvePersistentUserDataDir(filepath.Join("browser_data", "user_"+pureUserID(cookieID)))
	if err != nil {
		return "", err
	}
	cleanSingletonFiles(profileDir)
	_ = os.Remove(filepath.Join(profileDir, "DevToolsActivePort"))

	executable := m.pw.Chromium.ExecutablePath()
	if configured := chromiumExecutablePath(); configured != nil {
		executable = *configured
	}
	if strings.TrimSpace(executable) == "" {
		return "", fmt.Errorf("备用滑块引擎未找到 Chromium")
	}
	args := fallbackChromiumArgs(profileDir, headless)
	cmd := exec.Command(executable, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("备用滑块引擎启动 Chromium: %w", err)
	}
	processDone := make(chan error, 1)
	go func() { processDone <- cmd.Wait() }()
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		select {
		case <-processDone:
		case <-time.After(2 * time.Second):
		}
		cleanSingletonFiles(profileDir)
	}()

	endpoint, err := waitForDevToolsEndpoint(ctx, profileDir, processDone, 10*time.Second)
	if err != nil {
		return "", err
	}
	connected, err := m.pw.Chromium.ConnectOverCDP(endpoint, playwright.BrowserTypeConnectOverCDPOptions{
		Timeout: playwright.Float(10000),
	})
	if err != nil {
		return "", fmt.Errorf("备用滑块引擎连接 Chromium: %w", err)
	}
	defer func() { _ = connected.Close() }()
	contexts := connected.Contexts()
	if len(contexts) == 0 {
		return "", fmt.Errorf("备用滑块引擎未取得默认浏览器上下文")
	}
	bctx := contexts[0]
	if err := addCookieStr(bctx, cookieStr); err != nil {
		return "", fmt.Errorf("备用滑块引擎注入 Cookie: %w", err)
	}
	before, err := bctx.Cookies()
	if err != nil {
		return "", fmt.Errorf("备用滑块引擎读取旧 Cookie: %w", err)
	}
	previousX5 := x5SecValues(before)
	page, err := bctx.NewPage()
	if err != nil {
		return "", fmt.Errorf("备用滑块引擎创建页面: %w", err)
	}
	defer func() { _ = page.Close() }()

	deadline := time.Now().Add(drissionFallbackTimeout())
	if _, err := page.Goto(verificationURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(15000),
	}); err != nil {
		m.logger.Warn("备用滑块引擎访问验证页异常，继续检测页面", "cookieID", cookieID, "err", err)
	}
	if err := sleepUntil(ctx, deadline, secondsDuration(randomFloat(1, 3))); err != nil {
		return "", err
	}

	for attempt := 0; attempt < 3; attempt++ {
		if err := contextDeadlineError(ctx, deadline); err != nil {
			return "", err
		}
		if attempt > 0 {
			if err := sleepUntil(ctx, deadline, secondsDuration(randomFloat(1, 2))); err != nil {
				return "", err
			}
		}

		button, track, err := waitForFallbackSlider(ctx, page, boundedDeadline(deadline, 5*time.Second))
		if err != nil {
			m.logger.Warn("备用滑块引擎未找到滑块", "cookieID", cookieID, "attempt", attempt+1, "err", err)
			if attempt < 2 {
				reset := resetSliderForRetry(ctx, page, deadline)
				logSliderReset(m.logger, attempt+1, reset)
				if reset.err != nil {
					return "", fmt.Errorf("备用滑块第 %d 次前无法恢复: %w", attempt+1, reset.err)
				}
			}
			continue
		}
		distance := fallbackSlideDistance(page, button, track)
		frame, _ := button.OwnerFrame()
		if frame == nil {
			frame = page.MainFrame()
		}
		logSliderAttemptStart(m.logger, page, frame, button, track, attempt+1, distance)
		mode := fallbackMotionForAttempt(attempt)
		motion, err := simulateFallbackSlide(ctx, page, button, distance, mode, deadline)
		if err != nil {
			m.logger.Warn("备用滑块引擎滑动失败", "cookieID", cookieID, "attempt", attempt+1, "err", err)
			if attempt < 2 {
				reset := resetSliderForRetry(ctx, page, deadline)
				logSliderReset(m.logger, attempt+1, reset)
				if reset.err != nil {
					return "", fmt.Errorf("备用滑块第 %d 次失败后无法恢复: %w", attempt+1, reset.err)
				}
			}
			continue
		}
		m.logger.Info("备用滑块拖动已释放",
			"cookieID", cookieID,
			"attempt", attempt+1,
			"points", motion.points,
			"planned_duration", motion.plannedDuration,
			"movement_elapsed", motion.movementElapsed,
			"total_elapsed", motion.totalElapsed,
			"final_offset_x", fmt.Sprintf("%.1f", motion.finalOffsetX),
			"final_left", motion.finalLeft,
		)
		x5, fresh := waitForFallbackSuccess(ctx, bctx, page, previousX5, boundedDeadline(deadline, 3*time.Second))
		if fresh {
			merged := parseCookieStr(cookieStr)
			for name, value := range x5 {
				merged[name] = value
			}
			m.logger.Info("备用滑块引擎验证成功", "cookieID", cookieID, "attempt", attempt+1)
			return cookieMarshal(merged), nil
		}
		logSliderFailureState(m.logger, page, attempt+1)
		if attempt < 2 {
			reset := resetSliderForRetry(ctx, page, deadline)
			logSliderReset(m.logger, attempt+1, reset)
			if reset.err != nil {
				return "", fmt.Errorf("备用滑块第 %d 次失败后无法恢复: %w", attempt+1, reset.err)
			}
		}
	}
	return "", fmt.Errorf("备用滑块引擎 3 次均失败")
}

func fallbackChromiumArgs(profileDir string, headless bool) []string {
	args := []string{
		"--user-data-dir=" + profileDir,
		"--remote-debugging-port=0",
		"--no-sandbox",
		"--disable-setuid-sandbox",
		"--disable-dev-shm-usage",
		"--disable-blink-features=AutomationControlled",
		"--no-first-run",
		"--window-size=1920,1080",
		"--lang=zh-CN",
		"about:blank",
	}
	if headless {
		args = append([]string{"--headless=new"}, args...)
	}
	return args
}

func waitForDevToolsEndpoint(ctx context.Context, profileDir string, processDone <-chan error, timeout time.Duration) (string, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	path := filepath.Join(profileDir, "DevToolsActivePort")
	for {
		if raw, err := os.ReadFile(path); err == nil {
			lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
			if len(lines) > 0 {
				if port, err := strconv.Atoi(strings.TrimSpace(lines[0])); err == nil && port > 0 {
					return "http://127.0.0.1:" + strconv.Itoa(port), nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case err := <-processDone:
			return "", fmt.Errorf("备用 Chromium 提前退出: %w", err)
		case <-timer.C:
			return "", fmt.Errorf("等待备用 Chromium 调试端口超时")
		case <-ticker.C:
		}
	}
}

type fallbackMotion struct {
	totalTime time.Duration
	points    int
	impatient bool
}

type fallbackSlideMetrics struct {
	points          int
	plannedDuration time.Duration
	movementElapsed time.Duration
	totalElapsed    time.Duration
	finalOffsetX    float64
	finalLeft       string
}

func fallbackMotionForAttempt(attempt int) fallbackMotion {
	switch attempt {
	case 0:
		return fallbackMotion{totalTime: secondsDuration(randomFloat(1.5, 4)), points: 60 + rng.Intn(91)}
	case 1:
		return fallbackMotion{totalTime: secondsDuration(randomFloat(0.9, 1.3)), points: 30 + rng.Intn(31), impatient: true}
	default:
		return fallbackMotion{totalTime: secondsDuration(randomFloat(1, 2)), points: 50 + rng.Intn(41)}
	}
}

func fallbackSlideDistance(page playwright.Page, button, track playwright.ElementHandle) float64 {
	if track != nil {
		if box, err := track.BoundingBox(); err == nil && box != nil && box.Width > 0 {
			if button != nil {
				if buttonBox, buttonErr := button.BoundingBox(); buttonErr == nil && buttonBox != nil {
					if distance := fallbackUsableDistance(box.Width, buttonBox.Width); distance > 0 {
						return distance
					}
				}
			}
			return fallbackDistanceFromTrackWidth(box.Width)
		}
	}
	width := 1920.0
	if value, err := page.Evaluate(`() => window.innerWidth || 1920`); err == nil {
		if parsed, ok := value.(float64); ok {
			width = parsed
		}
	}
	if width <= 1366 {
		return 250 + float64(rng.Intn(71))
	}
	if width <= 1920 {
		return 300 + float64(rng.Intn(101))
	}
	return 350 + float64(rng.Intn(131))
}

func fallbackUsableDistance(trackWidth, buttonWidth float64) float64 {
	if trackWidth <= 0 || buttonWidth <= 0 || buttonWidth >= trackWidth {
		return 0
	}
	return trackWidth - buttonWidth
}

func fallbackDistanceFromTrackWidth(width float64) float64 {
	distance := width*randomFloat(0.70, 0.90) + float64(rng.Intn(41)-20)
	return math.Max(200, math.Min(600, distance))
}

func waitForFallbackSlider(ctx context.Context, page playwright.Page, deadline time.Time) (playwright.ElementHandle, playwright.ElementHandle, error) {
	for {
		frames := append([]playwright.Frame{page.MainFrame()}, page.Frames()...)
		for _, frame := range frames {
			button := queryVisible(frame, "#nc_1_n1z")
			if button == nil {
				continue
			}
			track := queryFirstVisible(frame, fallbackTrackSelectors)
			return button, track, nil
		}
		if err := contextDeadlineError(ctx, deadline); err != nil {
			if ctx.Err() != nil {
				return nil, nil, ctx.Err()
			}
			return nil, nil, fmt.Errorf("等待备用滑块元素超时")
		}
		if err := sleepUntil(ctx, deadline, 100*time.Millisecond); err != nil {
			return nil, nil, err
		}
	}
}

func simulateFallbackSlide(ctx context.Context, page playwright.Page, button playwright.ElementHandle, distance float64, mode fallbackMotion, deadline time.Time) (fallbackSlideMetrics, error) {
	metrics := fallbackSlideMetrics{plannedDuration: mode.totalTime}
	totalStarted := time.Now()
	box, err := button.BoundingBox()
	if err != nil || box == nil {
		return metrics, fmt.Errorf("备用引擎无法获取滑块位置")
	}
	waitBefore := secondsDuration(randomFloat(0.8, 2))
	if mode.impatient {
		waitBefore = secondsDuration(randomFloat(0.1, 0.5))
	}
	if err := sleepUntil(ctx, deadline, waitBefore); err != nil {
		return metrics, err
	}
	_ = button.Hover()
	if err := sleepUntil(ctx, deadline, secondsDuration(randomFloat(0.05, 0.3))); err != nil {
		return metrics, err
	}
	startX, startY := box.X+box.Width/2, box.Y+box.Height/2
	mouse := page.Mouse()
	if err := mouse.Move(startX, startY); err != nil {
		return metrics, err
	}
	if err := mouse.Down(); err != nil {
		return metrics, err
	}
	mouseDown := true
	defer func() {
		if mouseDown {
			_ = mouse.Up()
		}
	}()
	if err := sleepUntil(ctx, deadline, secondsDuration(randomFloat(0.05, 0.3))); err != nil {
		return metrics, err
	}
	tracks := generateFallbackTracks(distance, mode.points)
	if len(tracks) == 0 {
		return metrics, fmt.Errorf("备用引擎轨迹为空")
	}
	metrics.points = len(tracks)
	started := time.Now()
	lastX := 0.0
	currentY := startY
	direction := 1.0
	if rng.Intn(2) == 0 {
		direction = -1
	}
	yTrend := randomFloat(-3, 3)
	for i, absoluteX := range tracks {
		offsetX := absoluteX - lastX
		lastX = absoluteX
		if math.Abs(offsetX) < 0.1 {
			continue
		}
		progress := float64(i) / float64(len(tracks))
		yShake := randomFloat(-1.5, 1.5)
		if math.Abs(offsetX) > 8 {
			yShake *= randomFloat(1.2, 1.8)
		}
		offsetY := math.Max(-8, math.Min(8, yTrend*math.Pow(progress, 0.7)+yShake+direction*randomFloat(0.2, 1)))
		currentY += offsetY
		if err := mouse.Move(startX+absoluteX, currentY); err != nil {
			return metrics, err
		}
		remaining := mode.totalTime - time.Since(started)
		steps := len(tracks) - i
		stepDelay := time.Millisecond
		if steps > 0 && remaining > 0 {
			stepDelay = remaining / time.Duration(steps)
		}
		stepDelay = time.Duration(float64(stepDelay) * randomFloat(0.7, 1.3))
		if stepDelay < time.Millisecond {
			stepDelay = time.Millisecond
		}
		if stepDelay > 150*time.Millisecond {
			stepDelay = 150 * time.Millisecond
		}
		if err := sleepUntil(ctx, deadline, stepDelay); err != nil {
			return metrics, err
		}
	}
	metrics.movementElapsed = time.Since(started)
	metrics.finalOffsetX = lastX
	releaseWait := secondsDuration(randomFloat(0.2, 0.8))
	if mode.impatient {
		releaseWait = secondsDuration(randomFloat(0.05, 0.2))
	}
	if err := sleepUntil(ctx, deadline, releaseWait); err != nil {
		return metrics, err
	}
	if err := mouse.Up(); err != nil {
		return metrics, err
	}
	mouseDown = false
	metrics.finalLeft = readSliderLeft(button)
	metrics.totalElapsed = time.Since(totalStarted)
	return metrics, nil
}

func waitForFallbackSuccess(ctx context.Context, bctx playwright.BrowserContext, page playwright.Page, previousX5 map[string]struct{}, deadline time.Time) (map[string]string, bool) {
	var latest map[string]string
	for {
		all, err := bctx.Cookies()
		if err == nil {
			x5, fresh := freshX5Cookies(all, previousX5)
			latest = x5
			if fresh && !isPunishURL(page.URL()) {
				return x5, true
			}
		}
		if hasDefinitiveSliderFailure(page) {
			return latest, false
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return latest, false
		}
		if err := sleepUntil(ctx, deadline, 100*time.Millisecond); err != nil {
			return latest, false
		}
	}
}

// generateFallbackTracks 移植 DrissionPage fallback 的加速/匀速/减速、犹豫、超调和重采样。
func generateFallbackTracks(distance float64, targetPoints int) []float64 {
	if distance <= 0 || targetPoints <= 1 {
		return nil
	}
	tracks := []float64{0}
	current, velocity := 0.0, 0.0
	maxVelocity := randomFloat(80, 150)
	accelerationPhase := distance * randomFloat(0.3, 0.6)
	decelerationStart := distance * randomFloat(0.6, 0.85)
	dt := math.Max(0.01, math.Min(0.2, distance/(float64(targetPoints)*maxVelocity*0.5)*randomFloat(0.8, 1.2)))
	hesitationCounter := 0
	for step := 1; current < distance && step <= 10000; step++ {
		var acceleration float64
		switch {
		case current < accelerationPhase:
			acceleration = randomFloat(15, 35)
			if step%(3+rng.Intn(6)) == 0 {
				acceleration *= randomFloat(0.7, 1.4)
			}
		case current < decelerationStart:
			acceleration = randomFloat(-2, 2)
			if randomFloat(0, 1) < 0.2 {
				acceleration = randomFloat(-8, 8)
			}
		case distance-current > 20:
			acceleration = randomFloat(-25, -8)
		default:
			acceleration = randomFloat(-15, -3)
		}
		if randomFloat(0, 1) < 0.15 && current > accelerationPhase {
			hesitationCounter++
			if hesitationCounter < 3 {
				if randomFloat(0, 1) < 0.4 {
					acceleration = randomFloat(-8, -2)
				} else {
					acceleration = randomFloat(-2, 2)
				}
			} else {
				hesitationCounter = 0
			}
		}
		velocity = math.Max(0, math.Min(maxVelocity, velocity*0.95+acceleration*dt))
		old := current
		current += velocity * dt
		if len(tracks) > 5 {
			current += randomFloat(-0.3, 0.3) * (velocity / maxVelocity)
		}
		if randomFloat(0, 1) < 0.12 && current > 50 {
			switch correction := randomFloat(0, 1); {
			case correction < 0.6:
				current -= randomFloat(1, 4)
			case correction >= 0.8:
				current += randomFloat(0.2, 1)
			}
		}
		if current < old {
			current = old + randomFloat(0.1, 0.8)
		}
		if current-old > 15 {
			current = old + randomFloat(8, 15)
		}
		tracks = append(tracks, math.Round(current*10)/10)
	}
	if randomFloat(0, 1) < 0.3 {
		overshoot := randomFloat(2, 8)
		tracks = append(tracks, math.Round((distance+overshoot)*10)/10)
		correctionSteps := 2 + rng.Intn(4)
		for i := 0; i < correctionSteps; i++ {
			correction := overshoot * (1 - float64(i+1)/float64(correctionSteps))
			tracks = append(tracks, math.Round((distance+correction+randomFloat(-0.3, 0.3))*10)/10)
		}
	}
	finalTarget := distance + randomFloat(-1, 2)
	for i := 0; i < 1+rng.Intn(3); i++ {
		finalTarget += randomFloat(-0.5, 0.5)
		tracks = append(tracks, math.Round(finalTarget*10)/10)
	}
	result := resampleFallbackTracks(cleanFallbackTracks(tracks), targetPoints)
	return result
}

func cleanFallbackTracks(tracks []float64) []float64 {
	if len(tracks) == 0 {
		return nil
	}
	cleaned := []float64{tracks[0]}
	last := tracks[0]
	for _, current := range tracks[1:] {
		if math.Abs(current-last) < 1.5 {
			continue
		}
		if current < last && last-current >= 3 {
			current = last + randomFloat(0.1, 1)
		}
		cleaned = append(cleaned, current)
		last = current
	}
	return cleaned
}

func resampleFallbackTracks(tracks []float64, target int) []float64 {
	if len(tracks) <= 1 || target <= 1 || len(tracks) == target {
		return tracks
	}
	if len(tracks) > target {
		step := float64(len(tracks)) / float64(target)
		out := make([]float64, 0, target)
		out = append(out, tracks[0])
		for i := 1; i < target-1; i++ {
			index := min(int(float64(i)*step), len(tracks)-1)
			out = append(out, tracks[index])
		}
		return append(out, tracks[len(tracks)-1])
	}
	for len(tracks) < target && len(tracks) > 1 {
		out := make([]float64, 0, min(target, len(tracks)*2))
		out = append(out, tracks[0])
		for i := 0; i < len(tracks)-1 && len(out) < target; i++ {
			if i > 0 {
				out = append(out, tracks[i])
			}
			if len(out) < target {
				out = append(out, (tracks[i]+tracks[i+1])/2+randomFloat(-0.5, 0.5))
			}
		}
		if len(out) < target {
			out = append(out, tracks[len(tracks)-1])
		}
		tracks = out
	}
	if len(tracks) > target {
		tracks = tracks[:target]
	}
	return tracks
}

func contextDeadlineError(ctx context.Context, deadline time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !deadline.IsZero() && time.Now().After(deadline) {
		return fmt.Errorf("备用滑块引擎超过 %s", drissionFallbackTimeout())
	}
	return nil
}

func sleepUntil(ctx context.Context, deadline time.Time, duration time.Duration) error {
	if duration <= 0 {
		return contextDeadlineError(ctx, deadline)
	}
	if remaining := time.Until(deadline); !deadline.IsZero() && duration > remaining {
		duration = remaining
	}
	if duration <= 0 {
		return contextDeadlineError(ctx, deadline)
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return contextDeadlineError(ctx, deadline)
	}
}
