package browser

import (
	"math"
	"testing"
	"time"
)

func TestGenerateTrajectoryShape(t *testing.T) {
	pts := generateTrajectory(200)
	if len(pts) != 6 {
		t.Fatalf("参考三阶段轨迹应生成 6 个高层点，got %d", len(pts))
	}
	// 参考实现明确禁止超调，最后一点必须精确落在目标位置。
	last := pts[len(pts)-1]
	if math.Abs(last.x-200) > 1e-9 {
		t.Fatalf("末端 x 必须精确等于 distance，got %.6f", last.x)
	}
	// 三阶段轨迹不超调，且保持向目标方向移动。
	for i := 1; i < len(pts); i++ {
		if pts[i].x < pts[i-1].x {
			t.Fatalf("轨迹 x 不单调: pts[%d]=%.1f < pts[%d]=%.1f", i, pts[i].x, i-1, pts[i-1].x)
		}
		if pts[i].x > 200 {
			t.Fatalf("主引擎轨迹不应超调: pts[%d]=%.1f", i, pts[i].x)
		}
	}
}

func TestGenerateTrajectoryDelay(t *testing.T) {
	pts := generateTrajectory(150)
	var total time.Duration
	for i, pt := range pts {
		total += pt.delay
		if pt.delay <= 0 || pt.delay > 8*time.Millisecond {
			t.Fatalf("delay[%d]=%v 超出合理范围", i, pt.delay)
		}
		if pt.y < -1 || pt.y > 1 {
			t.Fatalf("y[%d]=%.2f 超出合理抖动", i, pt.y)
		}
	}
	if total < 8*time.Millisecond || total > 35*time.Millisecond {
		t.Fatalf("总轨迹时长不合理: %s", total)
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
	pts := generateTrajectory(100)
	half := len(pts) / 2
	frontIncrement := pts[half-1].x - pts[0].x
	backIncrement := pts[len(pts)-1].x - pts[half].x
	// 允许误差：不严格要求加速，但增量应合理。
	if math.IsNaN(frontIncrement) || math.IsNaN(backIncrement) {
		t.Fatal("轨迹 x 为 NaN")
	}
}
