//go:build windows

package main

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// fakeWindowsServiceController 用于本次流程后续判断的fakeWindowsService请求取消控制器
type fakeWindowsServiceController struct {
	states  []uint32
	actions []string
}

// state 封装状态业务协调。
func (controller *fakeWindowsServiceController) state() (uint32, error) {
	if len(controller.states) == 0 {
		return 0, fmt.Errorf("测试状态序列为空")
	}
	// state 用于本次流程后续判断的状态
	state := controller.states[0]
	if len(controller.states) > 1 {
		controller.states = controller.states[1:]
	}
	return state, nil
}

// start 封装开始业务协调。
func (controller *fakeWindowsServiceController) start() error {
	controller.actions = append(controller.actions, "start")
	return nil
}

// stop 封装stop业务协调。
func (controller *fakeWindowsServiceController) stop() error {
	controller.actions = append(controller.actions, "stop")
	return nil
}

// close 封装close业务协调。
func (controller *fakeWindowsServiceController) close() {}

// TestWindowsRestartWaitsForStoppedBeforeStarting 封装TestWindowsRestartWaitsForStoppedBeforeStarting业务协调。
func TestWindowsRestartWaitsForStoppedBeforeStarting(t *testing.T) {
	// controller 用于本次流程后续判断的请求取消控制器
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
	if // err 用于本次流程后续判断的err
	err := controlWindowsService(controller, "restart", time.Second, time.Millisecond); err != nil {
		t.Fatalf("restart Windows service: %v", err)
	}
	if // want 用于本次流程后续判断的want
	want := []string{"stop", "start"}; !reflect.DeepEqual(controller.actions, want) {
		t.Fatalf("actions = %v, want %v", controller.actions, want)
	}
}

// TestWindowsStartWaitsForPreviousStop 封装TestWindows开始WaitsForPreviousStop业务协调。
func TestWindowsStartWaitsForPreviousStop(t *testing.T) {
	// controller 用于本次流程后续判断的请求取消控制器
	controller := &fakeWindowsServiceController{
		states: []uint32{
			windows.SERVICE_STOP_PENDING,
			windows.SERVICE_STOPPED,
			windows.SERVICE_START_PENDING,
			windows.SERVICE_RUNNING,
		},
	}
	if // err 用于本次流程后续判断的err
	err := controlWindowsService(controller, "start", time.Second, time.Millisecond); err != nil {
		t.Fatalf("start Windows service: %v", err)
	}
	if // want 用于本次流程后续判断的want
	want := []string{"start"}; !reflect.DeepEqual(controller.actions, want) {
		t.Fatalf("actions = %v, want %v", controller.actions, want)
	}
}

// TestWindowsServiceAccessIsLimitedToStatusStartStop 封装TestWindowsServiceAccessIsLimitedTo状态开始Stop业务协调。
func TestWindowsServiceAccessIsLimitedToStatusStartStop(t *testing.T) {
	// want 用于本次流程后续判断的want
	want := uint32(windows.SERVICE_QUERY_STATUS | windows.SERVICE_START | windows.SERVICE_STOP)
	if windowsServiceAccess != want {
		t.Fatalf("service access = %#x, want %#x", windowsServiceAccess, want)
	}
}
