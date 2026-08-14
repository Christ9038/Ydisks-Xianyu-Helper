//go:build windows

package main

import (
	"context"
	"fmt"

	"golang.org/x/sys/windows/svc"
)

// windowsServiceHandler 保存windowsServiceHandler，供当前处理流程使用
type windowsServiceHandler struct {
	run func(context.Context) error
}

// runPlatformService 负责运行PlatformService相关处理。
func runPlatformService(name string, run func(context.Context) error) error {
	if // err 保存err，供当前处理流程使用
	err := svc.Run(name, windowsServiceHandler{run: run}); err != nil {
		return fmt.Errorf("Windows Service %q: %w", name, err)
	}
	return nil
}

// Execute 负责Execute相关处理。
func (h windowsServiceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	// accepted 保存accepted，供当前处理流程使用
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	status <- svc.Status{State: svc.StartPending}

	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithCancel(context.Background())
	// done 保存done，供当前处理流程使用
	done := make(chan error, 1)
	go func() { done <- h.run(ctx) }()
	status <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case // request 保存请求，供当前处理流程使用
		request := <-requests:
			switch request.Cmd {
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending, Accepts: accepted}
				cancel()
			}
		case // err 保存err，供当前处理流程使用
		err := <-done:
			cancel()
			if err != nil {
				return true, 1
			}
			return false, 0
		}
	}
}
