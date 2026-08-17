package account

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// TestCredentialRefreshCoordinatorSerializesSameAccount 验证同一账号只允许一个恢复回调运行。
func TestCredentialRefreshCoordinatorSerializesSameAccount(t *testing.T) {
	// coordinator 保存待测试的账号恢复协调器。
	coordinator := NewCredentialRefreshCoordinator()
	// entered 用于让首个恢复回调保持执行状态。
	entered := make(chan struct{})
	// release 用于控制首个恢复回调何时完成。
	release := make(chan struct{})
	// firstDone 保存首个恢复调用的结果。
	firstDone := make(chan error, 1)
	go func() {
		// accepted、renewed、runErr 保存首个恢复回调的协调结果。
		// accepted、renewed、runErr 保存首个恢复回调的登记结果、凭证结果和错误。
		accepted, renewed, runErr := coordinator.Run(context.Background(), "account-1", func(context.Context) (bool, error) {
			close(entered)
			<-release
			return true, nil
		})
		if !accepted || !renewed {
			firstDone <- errors.New("首个恢复调用未成功登记")
			return
		}
		firstDone <- runErr
	}()
	<-entered
	// accepted、renewed、runErr 保存重复恢复调用的协调结果。
	accepted, renewed, runErr := coordinator.Run(context.Background(), "account-1", func(context.Context) (bool, error) {
		return true, nil
	})
	if accepted || renewed || !errors.Is(runErr, ErrCredentialRefreshInFlight) {
		t.Fatalf("重复恢复结果 accepted=%v renewed=%v err=%v", accepted, renewed, runErr)
	}
	close(release)
	// runErr 保存首个恢复回调完成后的错误。
	if runErr := <-firstDone; runErr != nil {
		t.Fatalf("首个恢复错误: %v", runErr)
	}
	// accepted、renewed、runErr 保存首个调用结束后再次恢复的结果。
	accepted, renewed, runErr = coordinator.Run(context.Background(), "account-1", func(context.Context) (bool, error) {
		return true, nil
	})
	if !accepted || !renewed || runErr != nil {
		t.Fatalf("完成后恢复结果 accepted=%v renewed=%v err=%v", accepted, renewed, runErr)
	}
}

// TestCredentialRefreshCoordinatorReleasesOnErrorAndCancellation 验证错误与取消均不会遗留账号占用状态。
func TestCredentialRefreshCoordinatorReleasesOnErrorAndCancellation(t *testing.T) {
	// coordinator 保存待测试的账号恢复协调器。
	coordinator := NewCredentialRefreshCoordinator()
	// expectedErr 保存恢复回调预置的业务错误。
	expectedErr := errors.New("renew failed")
	// accepted、renewed、runErr 保存错误恢复回调的协调结果。
	accepted, renewed, runErr := coordinator.Run(context.Background(), "account-2", func(context.Context) (bool, error) {
		return false, expectedErr
	})
	if !accepted || renewed || !errors.Is(runErr, expectedErr) {
		t.Fatalf("错误恢复结果 accepted=%v renewed=%v err=%v", accepted, renewed, runErr)
	}
	// canceled、cancel 保存已取消的恢复上下文及其释放函数。
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	// accepted、renewed、runErr 保存取消恢复回调的协调结果。
	accepted, renewed, runErr = coordinator.Run(canceled, "account-2", func(ctx context.Context) (bool, error) {
		return false, ctx.Err()
	})
	if !accepted || renewed || !errors.Is(runErr, context.Canceled) {
		t.Fatalf("取消恢复结果 accepted=%v renewed=%v err=%v", accepted, renewed, runErr)
	}
}

// TestCredentialRefreshCoordinatorReleasesAfterPanic 验证回调 panic 仍会释放账号登记状态。
func TestCredentialRefreshCoordinatorReleasesAfterPanic(t *testing.T) {
	// coordinator 保存待测试的账号恢复协调器。
	coordinator := NewCredentialRefreshCoordinator()
	// panicked 标记是否捕获到预期 panic。
	panicked := false
	func() {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		coordinator.Run(context.Background(), "account-3", func(context.Context) (bool, error) {
			panic("test panic")
		})
	}()
	if !panicked {
		t.Fatal("恢复回调 panic 应向调用方传播")
	}
	// accepted、renewed、runErr 保存 panic 后重新执行的结果。
	accepted, renewed, runErr := coordinator.Run(context.Background(), "account-3", func(context.Context) (bool, error) {
		return true, nil
	})
	if !accepted || !renewed || runErr != nil {
		t.Fatalf("panic 后恢复结果 accepted=%v renewed=%v err=%v", accepted, renewed, runErr)
	}
}

// TestCredentialRefreshCoordinatorSupportsConcurrentDifferentAccounts 验证不同账号可以并行恢复且不共享占用状态。
func TestCredentialRefreshCoordinatorSupportsConcurrentDifferentAccounts(t *testing.T) {
	// coordinator 保存待测试的账号恢复协调器。
	coordinator := NewCredentialRefreshCoordinator()
	// start 用于同步两个账号的回调开始时机。
	start := make(chan struct{})
	// results 保存两个账号恢复调用的结果。
	results := make(chan error, 2)
	// waitGroup 等待两个独立账号的恢复调用结束。
	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	// accountID 表示当前并行恢复测试正在处理的账号标识。
	for _, accountID := range []string{"account-4", "account-5"} {
		// currentAccount 保存当前并行恢复的账号标识。
		currentAccount := accountID
		go func() {
			defer waitGroup.Done()
			// accepted、renewed、runErr 保存当前账号恢复结果。
			accepted, renewed, runErr := coordinator.Run(context.Background(), currentAccount, func(context.Context) (bool, error) {
				<-start
				return true, nil
			})
			if !accepted || !renewed {
				results <- errors.New("独立账号未成功恢复")
				return
			}
			results <- runErr
		}()
	}
	close(start)
	waitGroup.Wait()
	for range 2 {
		// runErr 保存并行账号恢复回调的错误。
		if runErr := <-results; runErr != nil {
			t.Fatalf("并行恢复错误: %v", runErr)
		}
	}
}
