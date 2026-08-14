package automation

import (
	"context"
	"reflect"
	"testing"

	"xianyu-go/internal/db"
)

// TestActionPlannerPaidEventKeepsCardBeforeShipment 验证付款事件只生成匹配卡密动作并保持发货顺序。
func TestActionPlannerPaidEventKeepsCardBeforeShipment(t *testing.T) {
	// task 是包含规格事实的付款事件。
	task := Task{TriggerType: TriggerOrderPaid, SpecName: "颜色", SpecValue: "蓝", Quantity: "2"}
	// actions 是待匹配的规则动作，故意包含一个规格不匹配的卡密动作。
	actions := []db.AutomationAction{
		{ID: 1, ActionType: ActionConfirmShipment, Enabled: true},
		{ID: 2, ActionType: ActionSendCard, Enabled: true, ConfigJSON: `{"spec_name":"颜色","spec_value":"蓝"}`},
		{ID: 3, ActionType: ActionSendCard, Enabled: true, ConfigJSON: `{"spec_name":"颜色","spec_value":"红"}`},
	}
	// original 用于确认规划过程不会修改规则动作输入。
	original := append([]db.AutomationAction(nil), actions...)
	// planner 是不执行外部 I/O 的纯动作计划组件。
	planner := actionPlanner{}
	// plan 是按发卡优先规则生成的不可变动作快照。
	plan := planner.plan(task, actions)
	if got := []int64{plan[0].ID, plan[1].ID}; !reflect.DeepEqual(got, []int64{2, 1}) {
		t.Fatalf("付款事件动作顺序=%v，want [2 1]", got)
	}
	if !reflect.DeepEqual(actions, original) {
		t.Fatal("动作计划不应修改规则动作输入")
	}
}

// TestEventFactRecorderWithoutOrderIsNoOp 验证没有订单事实时记录组件不执行任何持久化动作。
func TestEventFactRecorderWithoutOrderIsNoOp(t *testing.T) {
	// recorder 未注入数据库时应对无订单任务安全忽略。
	recorder := newEventFactRecorder(nil)
	if err := recorder.record(context.Background(), Task{AccountID: "cid", TriggerType: TriggerBuyerReviewed}); err != nil {
		t.Fatalf("无订单事实应安全忽略，err=%v", err)
	}
}
