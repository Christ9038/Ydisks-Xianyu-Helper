//go:build windows

package main

import (
	"context"
	"fmt"

	"golang.org/x/sys/windows/svc"
)

// windowsServiceHandler 用于本次流程后续判断的windowsServiceHandler
type windowsServiceHandler struct {
	run func(context.Context) error
}

// runPlatformService 封装运行PlatformService业务协调。
func runPlatformService(name string, run func(context.Context) error) error {
	if // err 用于本次流程后续判断的err
	err := svc.Run(name, windowsServiceHandler{run: run}); err != nil {
		return fmt.Errorf("Windows Service %q: %w", name, err)
	}
	return nil
}

// Execute 封装Execute业务协调。
func (h windowsServiceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	// accepted 用于本次流程后续判断的accepted
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	status <- svc.Status{State: svc.StartPending}

	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithCancel(context.Background())
	// done 用于本次流程后续判断的done
	done := make(chan error, 1)
	go func() { done <- h.run(ctx) }()
	status <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case // request 用于本次流程后续判断的请求
		request := <-requests:
			switch request.Cmd {
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending, Accepts: accepted}
				cancel()
			}
		case // err 用于本次流程后续判断的err
		err := <-done:
			cancel()
			if err != nil {
				return true, 1
			}
			return false, 0
		}
	}
}
