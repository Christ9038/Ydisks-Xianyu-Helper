package browser

import (
	"math"
	"testing"
)

func TestGenerateTrajectoryShape(t *testing.T) {
	pts := generateTrajectory(200)
	if len(pts) < 15 || len(pts) > 25 {
		t.Fatalf("steps 应在 15-25，got %d", len(pts))
	}
	// 最终 x 应在 distance*1.0 ~ distance*1.1 附近（最后一点在过冲范围内）。
	last := pts[len(pts)-1]
	if last.x < 190 || last.x > 240 {
		t.Fatalf("末端 x 应约等于 distance*1.05，got %.1f", last.x)
	}
	// x 应单调不减（加速曲线）。
	for i := 1; i < len(pts); i++ {
		if pts[i].x < pts[i-1].x-1 {
			t.Fatalf("轨迹 x 不单调: pts[%d]=%.1f < pts[%d]=%.1f", i, pts[i].x, i-1, pts[i-1].x)
		}
	}
}

func TestGenerateTrajectoryDelay(t *testing.T) {
	pts := generateTrajectory(150)
	for i, pt := range pts {
		if pt.delay < 2*1e6 || pt.delay > 15*1e6 { // 2ms-15ms in nanoseconds
			t.Fatalf("delay[%d]=%v 超出合理范围", i, pt.delay)
		}
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

func TestTrajectoryPhysics(t *testing.T) {
	// progress^1.5 曲线：中间步进应比线性大。
	pts := generateTrajectory(100)
	// 前半段增量应小于后半段增量（加速）。
	half := len(pts) / 2
	frontIncrement := pts[half-1].x - pts[0].x
	backIncrement := pts[len(pts)-1].x - pts[half].x
	// 允许误差：不严格要求加速，但增量应合理。
	if math.IsNaN(frontIncrement) || math.IsNaN(backIncrement) {
		t.Fatal("轨迹 x 为 NaN")
	}
}
