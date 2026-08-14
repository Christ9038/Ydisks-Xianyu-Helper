//go:build windows

package main

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// fakeWindowsServiceController 保存fakeWindowsService请求取消控制器，供当前处理流程使用
type fakeWindowsServiceController struct {
	states  []uint32
	actions []string
}

// state 负责状态相关处理。
func (controller *fakeWindowsServiceController) state() (uint32, error) {
	if len(controller.states) == 0 {
		return 0, fmt.Errorf("测试状态序列为空")
	}
	// state 保存状态，供当前处理流程使用
	state := controller.states[0]
	if len(controller.states) > 1 {
		controller.states = controller.states[1:]
	}
	return state, nil
}

// start 负责开始相关处理。
func (controller *fakeWindowsServiceController) start() error {
	controller.actions = append(controller.actions, "start")
	return nil
}

// stop 负责stop相关处理。
func (controller *fakeWindowsServiceController) stop() error {
	controller.actions = append(controller.actions, "stop")
	return nil
}

// close 负责close相关处理。
func (controller *fakeWindowsServiceController) close() {}

// TestWindowsRestartWaitsForStoppedBeforeStarting 负责TestWindowsRestartWaitsForStoppedBeforeStarting相关处理。
func TestWindowsRestartWaitsForStoppedBeforeStarting(t *testing.T) {
	// controller 保存请求取消控制器，供当前处理流程使用
	controller := &fakeWindowsServiceController{
		states: []uint32{
			windows.SERVICE_RUNNING,
			windows.SERVICE_STOP_PENDING,
			windows.SERVICE_STOPPED,
			windows.SERVICE_STOPPED,
			windows.SERVICE_START_PENDING,
			windows.SERVICE_RUNNING,
		},
	}
	if // err 保存err，供当前处理流程使用
	err := controlWindowsService(controller, "restart", time.Second, time.Millisecond); err != nil {
		t.Fatalf("restart Windows service: %v", err)
	}
	if // want 保存want，供当前处理流程使用
	want := []string{"stop", "start"}; !reflect.DeepEqual(controller.actions, want) {
		t.Fatalf("actions = %v, want %v", controller.actions, want)
	}
}

// TestWindowsStartWaitsForPreviousStop 负责TestWindows开始WaitsForPreviousStop相关处理。
func TestWindowsStartWaitsForPreviousStop(t *testing.T) {
	// controller 保存请求取消控制器，供当前处理流程使用
	controller := &fakeWindowsServiceController{
		states: []uint32{
			windows.SERVICE_STOP_PENDING,
			windows.SERVICE_STOPPED,
			windows.SERVICE_START_PENDING,
			windows.SERVICE_RUNNING,
		},
	}
	if // err 保存err，供当前处理流程使用
	err := controlWindowsService(controller, "start", time.Second, time.Millisecond); err != nil {
		t.Fatalf("start Windows service: %v", err)
	}
	if // want 保存want，供当前处理流程使用
	want := []string{"start"}; !reflect.DeepEqual(controller.actions, want) {
		t.Fatalf("actions = %v, want %v", controller.actions, want)
	}
}

// TestWindowsServiceAccessIsLimitedToStatusStartStop 负责TestWindowsServiceAccessIsLimitedTo状态开始Stop相关处理。
func TestWindowsServiceAccessIsLimitedToStatusStartStop(t *testing.T) {
	// want 保存want，供当前处理流程使用
	want := uint32(windows.SERVICE_QUERY_STATUS | windows.SERVICE_START | windows.SERVICE_STOP)
	if windowsServiceAccess != want {
		t.Fatalf("service access = %#x, want %#x", windowsServiceAccess, want)
	}
}
