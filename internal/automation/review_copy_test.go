package automation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// fixedIntn 返回始终取固定值的随机源，便于让随机分支可复现。
func fixedIntn(value int) func(int) int {
	return func(n int) int { return min(value, n-1) }
}

// TestParseReviewCopyConfig 覆盖默认随机、空白模板过滤和字数缺省。
func TestParseReviewCopyConfig(t *testing.T) {
	// cfg 是解析结果：未配置 random 时默认随机，空白条目被丢弃。
	cfg := parseReviewCopyConfig(`{"source":"pool","pool":["好用"," ","不错"]}`)
	if !cfg.Random || len(cfg.Pool) != 2 || cfg.AIMaxChars != reviewCopyDefaultMaxChars {
		t.Fatalf("解析结果不符合预期: %+v", cfg)
	}
	// off 显式关闭随机。
	if off := parseReviewCopyConfig(`{"random":false}`); off.Random {
		t.Fatal("显式 random=false 必须生效")
	}
	// bad 是无法解析的配置对应的解析结果，模板池应为空。
	if bad := parseReviewCopyConfig(`not json`); len(bad.Pool) != 0 {
		t.Fatal("非法 JSON 应得到空配置")
	}
}

// TestSanitizeReviewCopy 覆盖去引号、按字符裁剪和空串。
func TestSanitizeReviewCopy(t *testing.T) {
	// got 是清理后的文案。
	if got := sanitizeReviewCopy(" “东西很好，推荐” \n", 0); got != "东西很好，推荐" {
		t.Fatalf("引号未清理: %q", got)
	}
	// got 是按字符裁剪后的文案。
	if got := sanitizeReviewCopy("一二三四五六", 3); got != "一二三" {
		t.Fatalf("应按字符裁剪: %q", got)
	}
	// got 是只含引号的输入清理后的结果，应为空串。
	if got := sanitizeReviewCopy(" \"\" ", 10); got != "" {
		t.Fatalf("空内容应为空串: %q", got)
	}
}

// TestPickReviewCopyFromPool 覆盖空池、随机与顺序循环（含负位置）。
func TestPickReviewCopyFromPool(t *testing.T) {
	// cfg 是三条模板的顺序池。
	cfg := reviewCopyConfig{Pool: []string{"a", "b", "c"}}
	// ok 表示空模板池是否取到了模板，应为 false。
	if _, ok := pickReviewCopyFromPool(reviewCopyConfig{}, 0, fixedIntn(0)); ok {
		t.Fatal("空池不应有结果")
	}
	for /* position 与 want 是顺序循环位置和期望模板。 */ position, want := range map[int]string{0: "a", 1: "b", 3: "a", -2: "a"} {
		if got, _ := pickReviewCopyFromPool(cfg, position, fixedIntn(0)); got != want {
			t.Fatalf("位置 %d 期望 %s 得到 %s", position, want, got)
		}
	}
	cfg.Random = true
	// got 是随机模式下由注入随机源选出的模板。
	if got, _ := pickReviewCopyFromPool(cfg, 0, fixedIntn(2)); got != "c" {
		t.Fatalf("随机模式应使用随机源: %s", got)
	}
}

// TestResolveReviewCopyText 覆盖三种来源的成功、回退和失败分支。
func TestResolveReviewCopyText(t *testing.T) {
	// ctx 是测试用上下文。
	ctx := context.Background()
	// aiOK 与 aiFail 分别模拟 AI 成功与失败。
	aiOK := func(context.Context) (string, error) { return "“AI 好评”", nil }
	// aiFail 模拟 AI 调用失败。
	aiFail := func(context.Context) (string, error) { return "", errors.New("超时") }
	// aiEmpty 模拟 AI 只返回引号等空内容。
	aiEmpty := func(context.Context) (string, error) { return " \"\" ", nil }
	// pool 是带模板池的基础配置。
	pool := reviewCopyConfig{Pool: []string{"模板"}, AIMaxChars: 50}

	// cases 保存每个来源场景的配置、AI 桩、随机源、期望文案和是否应报错。
	cases := []struct {
		name     string
		cfg      reviewCopyConfig
		generate func(context.Context) (string, error)
		intn     func(int) int
		want     string
		wantErr  bool
	}{
		{"池成功", withSource(pool, reviewCopySourcePool), nil, fixedIntn(0), "模板", false},
		{"池为空", reviewCopyConfig{Source: reviewCopySourcePool}, nil, fixedIntn(0), "", true},
		{"AI成功", withSource(pool, reviewCopySourceAI), aiOK, fixedIntn(0), "AI 好评", false},
		{"AI失败不回退", withSource(pool, reviewCopySourceAI), aiFail, fixedIntn(0), "", true},
		{"AI为空", withSource(pool, reviewCopySourceAI), aiEmpty, fixedIntn(0), "", true},
		{"AI未装配", withSource(pool, reviewCopySourceAI), nil, fixedIntn(0), "", true},
		{"混合命中AI", withRatio(withSource(pool, reviewCopySourceMixed), 100), aiOK, fixedIntn(0), "AI 好评", false},
		{"混合AI失败回退池", withRatio(withSource(pool, reviewCopySourceMixed), 100), aiFail, fixedIntn(0), "模板", false},
		{"混合未命中AI", withRatio(withSource(pool, reviewCopySourceMixed), 0), aiOK, fixedIntn(0), "模板", false},
		{"混合池空走AI", withRatio(reviewCopyConfig{Source: reviewCopySourceMixed, AIMaxChars: 50}, 0), aiOK, fixedIntn(0), "AI 好评", false},
		{"混合池空AI失败", withRatio(reviewCopyConfig{Source: reviewCopySourceMixed, AIMaxChars: 50}, 100), aiFail, fixedIntn(0), "", true},
		{"混合池空且未命中AI失败", withRatio(reviewCopyConfig{Source: reviewCopySourceMixed, AIMaxChars: 50}, 0), aiFail, fixedIntn(0), "", true},
		{"未知来源", reviewCopyConfig{Source: "x"}, nil, fixedIntn(0), "", true},
	}
	for /* tc 是一个来源场景。 */ _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// got 是解析出的文案；err 是解析失败原因。
			got, err := resolveReviewCopyText(ctx, tc.cfg, 0, tc.intn, tc.generate)
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Fatalf("got=%q err=%v", got, err)
			}
			if err != nil && !errors.Is(err, ErrReviewCopyUnavailable) {
				t.Fatalf("错误应包装 ErrReviewCopyUnavailable: %v", err)
			}
		})
	}
}

// withSource 返回设置了来源的配置副本。
func withSource(cfg reviewCopyConfig, source string) reviewCopyConfig {
	cfg.Source = source
	return cfg
}

// withRatio 返回设置了 AI 比例的配置副本。
func withRatio(cfg reviewCopyConfig, ratio int) reviewCopyConfig {
	cfg.AIRatio = ratio
	return cfg
}

// TestActionJitterSeconds 覆盖非文本动作、缺省、越界截断和正常取值。
func TestActionJitterSeconds(t *testing.T) {
	// text 构造带抖动配置的文本动作。
	text := func(kind, config string) db.AutomationAction {
		return db.AutomationAction{ActionType: kind, ConfigJSON: config}
	}
	// got 是非文本动作得到的抖动秒数，应为 0。
	if got := actionJitterSeconds(text(ActionSendCard, `{"delay_jitter_seconds":5}`), fixedIntn(5)); got != 0 {
		t.Fatalf("非文本动作不应抖动: %d", got)
	}
	// got 是未配置抖动时的秒数，应为 0。
	if got := actionJitterSeconds(text(ActionSendText, `{}`), fixedIntn(5)); got != 0 {
		t.Fatalf("缺省不抖动: %d", got)
	}
	// got 是配置非法时的抖动秒数，应为 0。
	if got := actionJitterSeconds(text(ActionSendText, `bad`), fixedIntn(5)); got != 0 {
		t.Fatalf("非法配置不抖动: %d", got)
	}
	// got 是好评文案动作在上限 5 内取得的抖动秒数。
	if got := actionJitterSeconds(text(ActionSendReviewCopy, `{"delay_jitter_seconds":5}`), fixedIntn(99)); got != 5 {
		t.Fatalf("应在 [0,5] 内取值: %d", got)
	}
	// got 是超出上限的配置被截断后取得的最大抖动秒数。
	if got := actionJitterSeconds(text(ActionSendText, `{"delay_jitter_seconds":9999}`), func(n int) int { return n - 1 }); got != maxActionJitterSeconds {
		t.Fatalf("应截断到上限: %d", got)
	}
}

// TestEnsureReviewCopy 覆盖：无该动作、已确定、成功写入快照、解析失败零发送、未装配。
func TestEnsureReviewCopy(t *testing.T) {
	// ctx 与 run 是协调器入参。
	ctx := context.Background()
	// run 是游标在第一个动作的运行记录。
	run := &db.AutomationRun{ActionCursor: 0}
	// copyAction 是启用的好评文案动作。
	copyAction := db.AutomationAction{ActionType: ActionSendReviewCopy, Enabled: true}
	// calls 统计解析次数。
	calls := 0
	// coordinator 是注入成功解析桩的运行协调器；每次解析会累加 calls。
	coordinator := automationRunCoordinator{resolveReviewCopy: func(context.Context, int64, Task, db.AutomationAction) (string, error) {
		calls++
		return "好评正文", nil
	}}

	// task 是尚未确定好评文案的任务快照。
	task := Task{}
	// err 是计划中没有好评文案动作时的解析错误，应为空。
	if err := coordinator.ensureReviewCopy(ctx, &task, 1, run, []db.AutomationAction{{ActionType: ActionSendText, Enabled: true}}); err != nil || calls != 0 {
		t.Fatalf("没有好评文案动作时不应解析: %v %d", err, calls)
	}
	// err 是计划含好评文案动作时的解析错误，应为空且文案写入快照。
	if err := coordinator.ensureReviewCopy(ctx, &task, 1, run, []db.AutomationAction{{ActionType: ActionSendText, Enabled: true}, copyAction}); err != nil || rawReviewCopy(task) != "好评正文" {
		t.Fatalf("应写入任务快照: %v %q", err, rawReviewCopy(task))
	}
	// 已确定的文案在延迟恢复或重试时必须沿用，不得再次解析。
	if err := coordinator.ensureReviewCopy(ctx, &task, 1, run, []db.AutomationAction{copyAction}); err != nil || calls != 1 {
		t.Fatalf("已确定的文案不应再次解析: %v %d", err, calls)
	}
	// 已经执行过的游标之前的动作与停用动作都不触发解析。
	skipTask := Task{}
	// err 是游标已越过文案动作时的解析错误，应为空且不解析。
	if err := coordinator.ensureReviewCopy(ctx, &skipTask, 1, &db.AutomationRun{ActionCursor: 1}, []db.AutomationAction{copyAction}); err != nil || rawReviewCopy(skipTask) != "" {
		t.Fatalf("游标之后没有文案动作: %v", err)
	}
	// disabled 是停用的好评文案动作副本。
	disabled := copyAction
	disabled.Enabled = false
	// err 是只有停用动作时的解析错误，应为空且不解析。
	if err := coordinator.ensureReviewCopy(ctx, &skipTask, 1, run, []db.AutomationAction{disabled}); err != nil || rawReviewCopy(skipTask) != "" {
		t.Fatalf("停用动作不应解析: %v", err)
	}

	// failing 是解析总是失败的运行协调器。
	failing := automationRunCoordinator{resolveReviewCopy: func(context.Context, int64, Task, db.AutomationAction) (string, error) {
		return "", ErrReviewCopyUnavailable
	}}
	// failTask 是用于验证失败时不写快照的任务。
	failTask := Task{}
	// err 是解析失败时返回的错误，应标记为确定未发送。
	if err := failing.ensureReviewCopy(ctx, &failTask, 1, run, []db.AutomationAction{copyAction}); !errors.Is(err, ErrMessageNotSent) || rawReviewCopy(failTask) != "" {
		t.Fatalf("解析失败应标记为确定未发送且不写快照: %v", err)
	}
	// err 是未装配解析能力时的错误，应标记为确定未发送。
	if err := (automationRunCoordinator{}).ensureReviewCopy(ctx, &Task{}, 1, run, []db.AutomationAction{copyAction}); !errors.Is(err, ErrMessageNotSent) {
		t.Fatalf("未装配应明确失败: %v", err)
	}
}

// TestReviewRequestRuleDueBasis 覆盖按确认收货计时和按发货计时的互不串扰。
func TestReviewRequestRuleDueBasis(t *testing.T) {
	// old 是已过 24 小时等待期、仍在回溯窗口（48 小时）内的时间点。
	old := time.Now().UTC().Add(-30 * time.Hour).Format(time.RFC3339)
	// completedRule 与 shippedRule 是两种计时起点的规则。
	completedRule := db.AutomationRule{ConfigJSON: `{"basis":"completed","after_shipped_hours":24,"max_attempts":1}`}
	// shippedRule 是按发货计时的历史规则，不含 basis。
	shippedRule := db.AutomationRule{ConfigJSON: `{"after_shipped_hours":24,"max_attempts":1}`}

	if reviewRequestRuleDue(db.Order{ShippedAt: old}, completedRule) {
		t.Fatal("尚未确认收货的订单不应按确认收货规则求评价")
	}
	if !reviewRequestRuleDue(db.Order{ShippedAt: old, CompletedAt: old, SystemShipped: true}, completedRule) {
		t.Fatal("确认收货超过等待期应到期")
	}
	if reviewRequestRuleDue(db.Order{ShippedAt: old, CompletedAt: time.Now().UTC().Format(time.RFC3339)}, completedRule) {
		t.Fatal("刚确认收货不应到期")
	}
	if !reviewRequestRuleDue(db.Order{ShippedAt: old, SystemShipped: true}, shippedRule) {
		t.Fatal("发货计时规则行为保持不变")
	}
	if reviewRequestRuleDue(db.Order{CompletedAt: old}, shippedRule) {
		t.Fatal("非系统发货但已确认收货的订单不应被发货计时规则处理")
	}
}

// TestActionDelaySecondsAddsJitter 验证 Center 把随机上浮叠加到固定延迟上。
func TestActionDelaySecondsAddsJitter(t *testing.T) {
	// center 只需空依赖：文本动作不读取卡密。
	center := &Center{}
	// action 的固定延迟为 3 秒，抖动上限为 4 秒。
	action := db.AutomationAction{ActionType: ActionSendText, DelaySeconds: 3, ConfigJSON: `{"delay_jitter_seconds":4}`}
	// seen 记录 200 次抽样里出现过的延迟取值；随机上浮必须真正产生多种取值而不是恒为固定延迟。
	seen := map[int]bool{}
	for range 200 {
		// got 是固定延迟叠加随机上浮后的秒数；err 是延迟计算错误。
		got, err := center.actionDelaySeconds(context.Background(), action)
		if err != nil || got < 3 || got > 7 {
			t.Fatalf("延迟应在 [3,7]: %d %v", got, err)
		}
		seen[got] = true
	}
	if len(seen) < 3 || !seen[3] && !seen[4] {
		t.Fatalf("随机上浮应覆盖多个取值，实际只出现 %v", seen)
	}
	if !seen[7] && !seen[6] {
		t.Fatalf("随机上浮应能取到区间上端，实际只出现 %v", seen)
	}
}

// TestReviewCopyExecutorRequiresResolvedText 验证执行器只发送已确定的文案，缺失时明确未发送。
func TestReviewCopyExecutorRequiresResolvedText(t *testing.T) {
	// executor 未装配发送器：缺文案分支在发送前返回。
	executor := &automationActionExecutor{}
	// err 是缺少已确定文案时执行动作返回的错误，应标记为确定未发送。
	_, err := executor.executeActionWithProof(context.Background(), Task{}, db.AutomationAction{ActionType: ActionSendReviewCopy}, shipmentDeliveryProof{})
	if !errors.Is(err, ErrMessageNotSent) || !strings.Contains(err.Error(), "好评文案") {
		t.Fatalf("缺少文案应明确未发送: %v", err)
	}
}

// stubReviewCopyGenerator 返回固定文案，并记录收到的提示词，用于验证隐私字段不外发。
type stubReviewCopyGenerator struct {
	// text 与 err 是桩的固定返回值。
	text string
	err  error
	// got 记录最近一次提示词。
	got ReviewCopyPrompt
}

// Generate 记录提示词并返回预设结果。
func (s *stubReviewCopyGenerator) Generate(_ context.Context, prompt ReviewCopyPrompt) (string, error) {
	s.got = prompt
	return s.text, s.err
}

// TestCenterResolveReviewCopy 验证顺序循环按规则运行数推算位置，AI 来源只传递账号与商品标识。
func TestCenterResolveReviewCopy(t *testing.T) {
	// store、cleanup 是临时测试库。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是测试上下文。
	ctx := context.Background()
	// center 未注入生成器：纯模板池规则不受影响。
	center := New(store, nil, nil)
	// action 是顺序循环的模板池动作；ruleID 不存在时运行数为 0。
	action := db.AutomationAction{ActionType: ActionSendReviewCopy, ConfigJSON: `{"source":"pool","random":false,"pool":["第一条","第二条"]}`}
	// got 是没有运行记录时顺序循环取到的文案；err 是解析错误。
	got, err := center.resolveReviewCopy(ctx, 987654, Task{}, action)
	if err != nil || got != "第一条" {
		t.Fatalf("没有运行记录时应取第一条: %q %v", got, err)
	}
	// 随机来源不读库，AI 来源未装配生成器时明确失败。
	if _, aiErr := center.resolveReviewCopy(ctx, 1, Task{}, db.AutomationAction{ConfigJSON: `{"source":"ai"}`}); !errors.Is(aiErr, ErrReviewCopyUnavailable) {
		t.Fatalf("未装配生成器应失败: %v", aiErr)
	}

	// 注入生成器后，提示词只包含账号、商品标识与配置项。
	stub := &stubReviewCopyGenerator{text: "AI 写的好评"}
	// withAI 是注入了 AI 生成器桩的自动化中心。
	withAI := NewWithDependencies(store, nil, nil, CenterDependencies{ReviewCopyGenerator: stub})
	// aiAction 是 AI 来源且带风格、字数和附加要求的好评文案动作。
	aiAction := db.AutomationAction{ConfigJSON: `{"source":"ai","ai_style":"warm","ai_min_chars":10,"ai_max_chars":30,"ai_prompt":"提包装"}`}
	// text 是 AI 生成并清理后的文案；err 是解析错误。
	text, err := withAI.resolveReviewCopy(ctx, 1, Task{AccountID: "acc", ItemID: "item", BuyerID: "buyer-secret", OrderID: "order-secret"}, aiAction)
	if err != nil || text != "AI 写的好评" {
		t.Fatalf("AI 来源: %q %v", text, err)
	}
	// want 是期望交给生成器的提示词，只含账号、商品标识和配置项。
	if want := (ReviewCopyPrompt{AccountID: "acc", ItemID: "item", Style: "warm", MinChars: 10, MaxChars: 30, Extra: "提包装"}); stub.got != want {
		t.Fatalf("提示词内容不符: %+v", stub.got)
	}
}
