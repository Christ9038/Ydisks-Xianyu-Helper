//go:build windows

package main

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

type fakeWindowsServiceController struct {
	states  []uint32
	actions []string
}

func (controller *fakeWindowsServiceController) state() (uint32, error) {
	if len(controller.states) == 0 {
		return 0, fmt.Errorf("测试状态序列为空")
	}
	state := controller.states[0]
	if len(controller.states) > 1 {
		controller.states = controller.states[1:]
	}
	return state, nil
}

func (controller *fakeWindowsServiceController) start() error {
	controller.actions = append(controller.actions, "start")
	return nil
}

func (controller *fakeWindowsServiceController) stop() error {
	controller.actions = append(controller.actions, "stop")
	return nil
}

func (controller *fakeWindowsServiceController) close() {}

func TestWindowsRestartWaitsForStoppedBeforeStarting(t *testing.T) {
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
	if err := controlWindowsService(controller, "restart", time.Second, time.Millisecond); err != nil {
		t.Fatalf("restart Windows service: %v", err)
	}
	if want := []string{"stop", "start"}; !reflect.DeepEqual(controller.actions, want) {
		t.Fatalf("actions = %v, want %v", controller.actions, want)
	}
}

func TestWindowsStartWaitsForPreviousStop(t *testing.T) {
	controller := &fakeWindowsServiceController{
		states: []uint32{
			windows.SERVICE_STOP_PENDING,
			windows.SERVICE_STOPPED,
			windows.SERVICE_START_PENDING,
			windows.SERVICE_RUNNING,
		},
	}
	if err := controlWindowsService(controller, "start", time.Second, time.Millisecond); err != nil {
		t.Fatalf("start Windows service: %v", err)
	}
	if want := []string{"start"}; !reflect.DeepEqual(controller.actions, want) {
		t.Fatalf("actions = %v, want %v", controller.actions, want)
	}
}

func TestWindowsServiceAccessIsLimitedToStatusStartStop(t *testing.T) {
	want := uint32(windows.SERVICE_QUERY_STATUS | windows.SERVICE_START | windows.SERVICE_STOP)
	if windowsServiceAccess != want {
		t.Fatalf("service access = %#x, want %#x", windowsServiceAccess, want)
	}
}
