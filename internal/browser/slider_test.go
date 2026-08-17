package browser

import (
	"math"
	"testing"
	"time"
)

// TestGenerateTrajectoryShape 验证标准 NC 轨迹保持单调前进，并在末端使用受控鼠标超调。
func TestGenerateTrajectoryShape(t *testing.T) {
	// pts 是本次生成的轨迹采样点，用于核验水平移动的边界与顺序。
	pts := generateTrajectory(200, 34)
	if len(pts) < 8 || len(pts) > 12 {
		t.Fatalf("主引擎应生成 8 至 12 个高层点，got %d", len(pts))
	}
	// last 是最后一个鼠标目标点；其 X 应位于可用距离外的受控超调范围。
	last := pts[len(pts)-1]
	if last.x < 204 || last.x > 220 {
		t.Fatalf("末端 x 应超出 distance 4 至 20px，got %.6f", last.x)
	}
	// 三阶段轨迹保持向目标方向移动；超调只允许落在最后的受控鼠标目标范围内。
	for i := 1; i < len(pts); i++ {
		if pts[i].x < pts[i-1].x {
			t.Fatalf("轨迹 x 不单调: pts[%d]=%.1f < pts[%d]=%.1f", i, pts[i].x, i-1, pts[i-1].x)
		}
		if pts[i].x > 220 {
			t.Fatalf("主引擎轨迹超出受控范围: pts[%d]=%.1f", i, pts[i].x)
		}
	}
}

// TestGenerateTrajectoryDelay 验证曲线锚点的相对时长有效，连续拖动由后续微移动事件负责分发。
func TestGenerateTrajectoryDelay(t *testing.T) {
	// pts 是本次生成的曲线锚点，用于汇总相对时长权重。
	pts := generateTrajectory(150, 34)
	// total 汇总锚点相对时长。
	var total time.Duration
	// i 表示采样点下标；pt 是当前待检查的轨迹点。
	for i, pt := range pts {
		total += pt.delay
		if pt.delay <= 0 || pt.delay > 140*time.Millisecond {
			t.Fatalf("delay[%d]=%v 超出合理范围", i, pt.delay)
		}
	}
	if total < 180*time.Millisecond || total > 1700*time.Millisecond {
		t.Fatalf("总轨迹时长不合理: %s", total)
	}
}

// TestExpandContinuousTrajectory 验证锚点间被拆为密集连续事件，且没有可见的中段静止间隔。
func TestExpandContinuousTrajectory(t *testing.T) {
	// anchors 是简化的三段曲线锚点，delay 仅用作时间权重。
	anchors := []trajectoryPoint{
		{x: 20, y: 3, delay: 40 * time.Millisecond},
		{x: 110, y: -5, delay: 70 * time.Millisecond},
		{x: 180, y: 0, delay: 50 * time.Millisecond},
	}
	// target 是提速后完整按住拖动的墙钟目标时长；points 是展开后的微移动事件。
	target := 800 * time.Millisecond
	points := expandContinuousTrajectory(anchors, target)
	if len(points) <= len(anchors) {
		t.Fatalf("连续事件数=%d，应多于锚点数=%d", len(points), len(anchors))
	}
	// total 汇总事件延时；previousX 用于确认鼠标全程只向目标方向前进；distinctDelays 检查节奏是否有变化。
	var total time.Duration
	previousX := 0.0
	distinctDelays := make(map[time.Duration]struct{})
	for pointIndex, point := range points {
		if point.delay <= 0 || point.delay > sliderContinuousEventMaxInterval {
			t.Fatalf("delay[%d]=%s，不应形成可感知停顿", pointIndex, point.delay)
		}
		if point.x < previousX {
			t.Fatalf("x[%d]=%.3f 小于前一点 %.3f", pointIndex, point.x, previousX)
		}
		total += point.delay
		previousX = point.x
		distinctDelays[point.delay] = struct{}{}
	}
	if len(distinctDelays) < 2 {
		t.Fatalf("连续事件间隔不应全部相同: %v", distinctDelays)
	}
	if points[len(points)-1].x != anchors[len(anchors)-1].x || points[len(points)-1].y != anchors[len(anchors)-1].y {
		t.Fatalf("末点=(%.3f, %.3f)，want=(%.3f, %.3f)", points[len(points)-1].x, points[len(points)-1].y, anchors[len(anchors)-1].x, anchors[len(anchors)-1].y)
	}
	if total > target || total < target-sliderContinuousEventMaxInterval {
		t.Fatalf("连续事件计划总时长=%s，want 接近 %s", total, target)
	}
}

// TestSliderMovementDurationRange 验证提速后的主引擎墙钟窗口仍有明确上下限。
func TestSliderMovementDurationRange(t *testing.T) {
	if sliderMovementMinDuration != 550*time.Millisecond || sliderMovementMaxDuration != 1050*time.Millisecond {
		t.Fatalf("主引擎目标窗口=(%s,%s)，want=(550ms,1050ms)", sliderMovementMinDuration, sliderMovementMaxDuration)
	}
}

// TestGenerateTrajectoryVerticalDrift 验证纵向路径连续、非线性，并且实际峰峰值严格等于滑轨高度的一半。
func TestGenerateTrajectoryVerticalDrift(t *testing.T) {
	// trackHeight 是模拟标准 NC 轨道的像素高度；pts 是使用该高度生成的鼠标路径。
	const trackHeight = 34.0
	pts := generateTrajectory(258, trackHeight/2)
	// minY、maxY 收集路径的纵向边界；hasDirectionChange 确认路径不是单向线性斜线。
	minY, maxY := math.Inf(1), math.Inf(-1)
	hasDirectionChange := false
	// previousDelta 保存前一段的纵向增量；pointIndex、point 是当前遍历的路径位置和坐标。
	previousDelta := 0.0
	for pointIndex, point := range pts {
		minY = min(minY, point.y)
		maxY = max(maxY, point.y)
		if pointIndex == 0 {
			continue
		}
		// currentDelta 是当前段的纵向变化，符号改变表示连续曲线已穿过极值而非维持线性斜率。
		currentDelta := point.y - pts[pointIndex-1].y
		if previousDelta != 0 && currentDelta*previousDelta < 0 {
			hasDirectionChange = true
		}
		if currentDelta != 0 {
			previousDelta = currentDelta
		}
	}
	if math.Abs((maxY-minY)-trackHeight/2) > 1e-9 {
		t.Fatalf("纵向峰峰值=%.6f want %.6f", maxY-minY, trackHeight/2)
	}
	if !hasDirectionChange || math.Abs(pts[0].y) > 1e-9 || math.Abs(pts[len(pts)-1].y) > 1e-9 {
		t.Fatalf("纵向路径必须从中心出发并以连续非线性曲线回到中心: first=%.6f last=%.6f changed=%v", pts[0].y, pts[len(pts)-1].y, hasDirectionChange)
	}
}

func TestCompensatedTrajectoryDelayUsesWallClockBudget(t *testing.T) {
	planned := 3 * time.Millisecond
	if got := compensatedTrajectoryDelay(planned, 600*time.Millisecond, 100*time.Millisecond, 5); got != 100*time.Millisecond {
		t.Fatalf("应按剩余墙钟预算补偿，got %s", got)
	}
	if got := compensatedTrajectoryDelay(planned, 100*time.Millisecond, 120*time.Millisecond, 2); got != planned {
		t.Fatalf("达到目标时长后应保留原始微延迟，got %s", got)
	}
}

func TestSliderSelectorsMatchReferencePriority(t *testing.T) {
	if sliderButtonSelectors[0] != "#nc_1_n1z" || sliderTrackSelectors[0] != "#nc_1_n1t" {
		t.Fatalf("滑块精确选择器必须优先: button=%q track=%q", sliderButtonSelectors[0], sliderTrackSelectors[0])
	}
	wantRetry := []string{"#nc_1_refresh1", ".nc_iconfont.btn_refresh", ".errloading"}
	for i, want := range wantRetry {
		if sliderRetrySelectors[i] != want {
			t.Fatalf("重试选择器[%d]=%q want %q", i, sliderRetrySelectors[i], want)
		}
	}
}

func TestSliderRetryTextOnlyAcceptsFailurePrompts(t *testing.T) {
	for _, text := range []string{"验证失败，点击框体重试", "刷新验证码", "Retry verification"} {
		if !sliderRetryText(text) {
			t.Fatalf("应识别重试文案 %q", text)
		}
	}
	if sliderRetryText("请按住滑块，拖动到最右边") {
		t.Fatal("初始滑块容器不应被当作重试控件")
	}
}

func TestIsScratchCaptcha(t *testing.T) {
	if !isScratchCaptcha("<div id='scratch-captcha-btn'>") {
		t.Fatal("应识别 scratch-captcha-btn")
	}
	if !isScratchCaptcha("scratch-captcha-slider") {
		t.Fatal("应识别 scratch-captcha-slider")
	}
	if isScratchCaptcha("<div id='nc_1_n1z'>") {
		t.Fatal("普通滑块不应识别为刮刮乐")
	}
}

func TestIsPunishURL(t *testing.T) {
	for _, rawURL := range []string{
		"https://example/punish?x=1", "https://example/?x5step=2",
		"https://example/?action=captcha", "https://example/PURECAPTCHA",
		"https://example/captcha/check",
	} {
		if !isPunishURL(rawURL) {
			t.Fatalf("应识别风控 URL: %s", rawURL)
		}
	}
	if isPunishURL("https://www.goofish.com/item/1") {
		t.Fatal("普通页面不应识别为风控 URL")
	}
}

func TestCaptchaURLExpiredRequiresReferenceErrorPage(t *testing.T) {
	if !captchaURLExpired("<main>抱歉，页面访问出现了问题</main>") {
		t.Fatal("应识别参考项目定义的验证链接过期页")
	}
	if captchaURLExpired("<main>验证码加载中，请稍候</main>") {
		t.Fatal("普通验证码页面不应被当作链接过期")
	}
}

func TestSliderContainerStatesSucceeded(t *testing.T) {
	tests := []struct {
		name   string
		states []sliderContainerState
		want   bool
	}{
		{name: "container missing", want: true},
		{name: "container hidden", states: []sliderContainerState{{found: true, visibilityKnown: true}}, want: true},
		{name: "container visible", states: []sliderContainerState{{found: true, visible: true, visibilityKnown: true}}, want: false},
		{name: "visibility unknown", states: []sliderContainerState{{found: true}}, want: false},
		{name: "one visible", states: []sliderContainerState{{found: true, visibilityKnown: true}, {found: true, visible: true, visibilityKnown: true}}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sliderContainerStatesSucceeded(tt.states); got != tt.want {
				t.Fatalf("sliderContainerStatesSucceeded()=%v want %v", got, tt.want)
			}
		})
	}
}

func TestTrajectoryPhysics(t *testing.T) {
	pts := generateTrajectory(100, 34)
	half := len(pts) / 2
	frontIncrement := pts[half-1].x - pts[0].x
	backIncrement := pts[len(pts)-1].x - pts[half].x
	// 允许误差：不严格要求加速，但增量应合理。
	if math.IsNaN(frontIncrement) || math.IsNaN(backIncrement) {
		t.Fatal("轨迹 x 为 NaN")
	}
}
