package lifecycle

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// lifecycleComponentFake 记录组件启动、关闭和顺序，用于验证协调器的所有权语义。
type lifecycleComponentFake struct {
	// name 是测试替身的诊断名称。
	name string
	// events 记录启动和关闭事件。
	events *[]string
	// mu 保护共享事件切片。
	mu *sync.Mutex
	// startErr 是预置的启动失败。
	startErr error
	// closeErr 是预置的关闭失败。
	closeErr error
}

// Start 记录组件启动并按预置结果返回。
func (fake *lifecycleComponentFake) Start(context.Context) error {
	fake.mu.Lock()
	*fake.events = append(*fake.events, "start:"+fake.name)
	fake.mu.Unlock()
	return fake.startErr
}

// Close 记录组件关闭并按预置结果返回。
func (fake *lifecycleComponentFake) Close(context.Context) error {
	fake.mu.Lock()
	*fake.events = append(*fake.events, "close:"+fake.name)
	fake.mu.Unlock()
	return fake.closeErr
}

// retryCloseComponent 首次关闭等待 Context 取消，后续关闭模拟外部任务最终收束。
type retryCloseComponent struct {
	// mu 保护 closeCalls，确保并发测试读取关闭次数时无数据竞争。
	mu sync.Mutex
	// closeCalls 记录协调器实际调用 Close 的次数。
	closeCalls int
}

// Start 让可重试关闭组件立即进入运行状态。
func (*retryCloseComponent) Start(context.Context) error {
	return nil
}

// Close 首次等待调用方截止，第二次模拟使用更长 Context 成功收束。
func (component *retryCloseComponent) Close(ctx context.Context) error {
	component.mu.Lock()
	component.closeCalls++
	// callCount 保存本次关闭调用序号，用于区分首次超时和后续重试。
	callCount := component.closeCalls
	component.mu.Unlock()
	if callCount == 1 {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

// TestCoordinatorStartsAndClosesInDeterministicOrder 验证正常启动、重复调用和逆序关闭。
func TestCoordinatorStartsAndClosesInDeterministicOrder(t *testing.T) {
	// events 保存生命周期事件顺序；mu 保护并发回调写入。
	events := make([]string, 0, 4)
	// mu 串行化测试组件对事件切片的并发追加。
	mu := &sync.Mutex{}
	// first、second 保存按顺序登记的两个组件。
	first := &lifecycleComponentFake{name: "first", events: &events, mu: mu}
	// second 保存第二个按顺序启动、逆序关闭的测试组件。
	second := &lifecycleComponentFake{name: "second", events: &events, mu: mu}
	// coordinator 保存待验证的生命周期协调器。
	coordinator := NewCoordinator()
	// err 表示登记第一个组件时的参数校验错误。
	if err := coordinator.Add(NamedComponent{Name: "first", Component: first}); err != nil {
		t.Fatalf("登记第一个组件失败: %v", err)
	}
	// err 表示登记第二个组件时的参数校验错误。
	if err := coordinator.Add(NamedComponent{Name: "second", Component: second}); err != nil {
		t.Fatalf("登记第二个组件失败: %v", err)
	}
	// parent 保存进程级生命周期 Context。
	parent := context.Background()
	// err 表示首次启动协调器的失败原因。
	if err := coordinator.Start(parent); err != nil {
		t.Fatalf("启动协调器失败: %v", err)
	}
	// err 表示重复启动协调器的非预期错误。
	if err := coordinator.Start(parent); err != nil {
		t.Fatalf("重复启动应幂等: %v", err)
	}
	if coordinator.Context() == context.Background() {
		t.Fatal("启动后应返回专用生命周期 Context")
	}
	// err 表示首次关闭协调器的聚合错误。
	if err := coordinator.Close(context.Background()); err != nil {
		t.Fatalf("关闭协调器失败: %v", err)
	}
	// err 表示重复关闭协调器的幂等结果。
	if err := coordinator.Close(context.Background()); err != nil {
		t.Fatalf("重复关闭应幂等: %v", err)
	}
	// want 是生命周期回调必须产生的确定性顺序。
	want := []string{"start:first", "start:second", "close:second", "close:first"}
	if !equalStrings(events, want) {
		t.Fatalf("生命周期顺序=%v，期望=%v", events, want)
	}
}

// TestCoordinatorRollsBackOnStartFailure 验证启动失败会取消 Context、逆序回滚并拒绝后续启动。
func TestCoordinatorRollsBackOnStartFailure(t *testing.T) {
	// events 保存启动失败回滚过程中的事件顺序；mu 保护共享切片。
	events := make([]string, 0, 4)
	// mu 串行化测试组件对事件切片的并发追加。
	mu := &sync.Mutex{}
	// first、second 保存成功组件和失败组件。
	first := &lifecycleComponentFake{name: "first", events: &events, mu: mu}
	// second 保存会在启动阶段返回错误的测试组件。
	second := &lifecycleComponentFake{name: "second", events: &events, mu: mu, startErr: errors.New("start failed")}
	// coordinator 保存待验证的生命周期协调器。
	coordinator := NewCoordinator()
	_ = coordinator.Add(NamedComponent{Name: "first", Component: first})
	_ = coordinator.Add(NamedComponent{Name: "second", Component: second})
	// err 表示启动失败回滚返回的聚合错误。
	if err := coordinator.Start(context.Background()); err == nil {
		t.Fatal("组件启动失败应返回错误")
	}
	// err 表示回滚后再次启动收到的停止错误。
	if err := coordinator.Start(context.Background()); !errors.Is(err, ErrStopped) {
		t.Fatalf("失败回滚后应拒绝再次启动: %v", err)
	}
	// want 是启动失败后包含失败组件在内的逆序回滚顺序。
	want := []string{"start:first", "start:second", "close:second", "close:first"}
	if !equalStrings(events, want) {
		t.Fatalf("回滚顺序=%v，期望=%v", events, want)
	}
}

// TestCoordinatorValidatesInputsAndRollbackErrors 验证协调器参数校验和回滚错误聚合。
func TestCoordinatorValidatesInputsAndRollbackErrors(t *testing.T) {
	if // err 是 nil 协调器追加组件时的初始化错误。
	err := (*Coordinator)(nil).Add(NamedComponent{}); err == nil {
		t.Fatal("nil 协调器应拒绝追加组件")
	}
	// coordinator 保存用于参数校验的空协调器。
	coordinator := NewCoordinator()
	// nilContext 是专门验证协调器 nil Context 防护的空接口值。
	var nilContext context.Context
	if // err 是空组件实现被拒绝的参数错误。
	err := coordinator.Add(NamedComponent{Name: "empty"}); err == nil {
		t.Fatal("空组件实现应被拒绝")
	}
	if // err 是空组件名称被拒绝的参数错误。
	err := coordinator.Add(NamedComponent{Component: FuncComponent{}}); err == nil {
		t.Fatal("空组件名称应被拒绝")
	}
	if // err 是未启动协调器返回的父 Context。
	err := coordinator.Context(); err == nil {
		t.Fatal("Context 返回值不应为 nil")
	}
	if // err 是 nil 父 Context 被拒绝的启动错误。
	err := coordinator.Start(nilContext); err == nil {
		t.Fatal("nil 父 Context 应被拒绝")
	}
	// closeFailure 保存失败组件关闭时产生的回滚错误。
	closeFailure := errors.New("rollback close failed")
	// events 保存启动失败回滚的组件事件。
	events := make([]string, 0, 2)
	// mu 保护回滚测试组件的事件切片。
	mu := &sync.Mutex{}
	// component 保存启动失败且回滚关闭失败的组件。
	component := &lifecycleComponentFake{name: "failed", events: &events, mu: mu, startErr: errors.New("start failed"), closeErr: closeFailure}
	// rollbackCoordinator 保存待验证的回滚错误协调器。
	rollbackCoordinator := NewCoordinator()
	_ = rollbackCoordinator.Add(NamedComponent{Name: "failed", Component: component})
	// err 是同时包含启动错误和回滚错误的聚合结果。
	err := rollbackCoordinator.Start(context.Background())
	if !errors.Is(err, closeFailure) || !errors.Is(err, component.startErr) {
		t.Fatalf("回滚错误未聚合: %v", err)
	}
}

// TestCoordinatorRejectsAddAfterStart 验证启动后不能追加组件，防止部分构造状态被观察。
func TestCoordinatorRejectsAddAfterStart(t *testing.T) {
	// coordinator 保存待验证的生命周期协调器。
	coordinator := NewCoordinator()
	_ = coordinator.Add(NamedComponent{Name: "one", Component: FuncComponent{}})
	// err 表示单组件协调器启动错误。
	if err := coordinator.Start(context.Background()); err != nil {
		t.Fatalf("启动协调器失败: %v", err)
	}
	// err 表示启动后追加组件返回的拒绝错误。
	if err := coordinator.Add(NamedComponent{Name: "late", Component: FuncComponent{}}); err == nil {
		t.Fatal("启动后追加组件应失败")
	}
	// err 表示单组件协调器关闭错误。
	if err := coordinator.Close(context.Background()); err != nil {
		t.Fatalf("关闭协调器失败: %v", err)
	}
}

// TestCoordinatorCloseContextBoundsConcurrentClose 验证并发关闭等待遵守调用方 Context。
func TestCoordinatorCloseContextBoundsConcurrentClose(t *testing.T) {
	// entered、release 同步第一个关闭调用的阻塞窗口。
	entered := make(chan struct{})
	// release 允许首个关闭回调结束，完成并发关闭测试。
	release := make(chan struct{})
	// component 保存阻塞关闭的测试组件。
	component := FuncComponent{
		CloseFunc: func(context.Context) error {
			close(entered)
			<-release
			return nil
		},
	}
	// coordinator 保存待验证的生命周期协调器。
	coordinator := NewCoordinator()
	_ = coordinator.Add(NamedComponent{Name: "blocking", Component: component})
	_ = coordinator.Start(context.Background())
	// firstDone 接收首个关闭调用的最终错误。
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- coordinator.Close(context.Background())
	}()
	<-entered
	// waitingDone 接收第二个关闭调用等待首轮关闭结果后的错误。
	waitingDone := make(chan error, 1)
	go func() {
		waitingDone <- coordinator.Close(context.Background())
	}()
	time.Sleep(5 * time.Millisecond)
	// timeoutCtx、cancel 控制第二个关闭调用的等待时限。
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	// err 表示第二个关闭调用在等待首个关闭时收到的超时错误。
	if err := coordinator.Close(timeoutCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("并发关闭应返回等待超时: %v", err)
	}
	close(release)
	// err 表示首个关闭调用在释放阻塞后返回的最终结果。
	if err := <-firstDone; err != nil {
		t.Fatalf("首个关闭失败: %v", err)
	}
	// err 表示第二个关闭调用复用首轮关闭结果后的错误。
	if err := <-waitingDone; err != nil {
		t.Fatalf("等待首轮关闭的调用失败: %v", err)
	}
}

// TestCoordinatorCloseRetainsIncompleteComponentsForRetry 验证关闭超时后保留未完成组件并支持再次 Join。
func TestCoordinatorCloseRetainsIncompleteComponentsForRetry(t *testing.T) {
	// component 保存首次超时、第二次成功的生命周期组件。
	component := &retryCloseComponent{}
	// coordinator 保存待验证的关闭重试协调器。
	coordinator := NewCoordinator()
	// err 表示登记可重试组件时的参数校验错误。
	if err := coordinator.Add(NamedComponent{Name: "retryable", Component: component}); err != nil {
		t.Fatalf("登记可重试组件失败: %v", err)
	}
	// err 表示启动可重试组件协调器的失败原因。
	if err := coordinator.Start(context.Background()); err != nil {
		t.Fatalf("启动协调器失败: %v", err)
	}
	// timeoutCtx 限制首次关闭，模拟组件无法在本轮预算内完成。
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	// firstErr 保存首次关闭的组件诊断错误。
	firstErr := coordinator.Close(timeoutCtx)
	if !errors.Is(firstErr, context.DeadlineExceeded) {
		t.Fatalf("首次关闭应返回截止错误: %v", firstErr)
	}
	// err 表示关闭诊断中是否包含未完成组件名称。
	if !strings.Contains(firstErr.Error(), "retryable") {
		t.Fatalf("关闭错误应包含未完成组件名称: %v", firstErr)
	}
	// waitCtx 确认首次失败不能伪造协调器已完成关闭。
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer waitCancel()
	// err 表示未完成关闭在短等待 Context 下的结果。
	if err := coordinator.WaitContext(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("关闭未完成时 Wait 应返回截止错误: %v", err)
	}
	// err 表示使用更长 Context 重试关闭的结果。
	if err := coordinator.Close(context.Background()); err != nil {
		t.Fatalf("使用更长 Context 重试关闭失败: %v", err)
	}
	component.mu.Lock()
	// closeCalls 保存组件被协调器调用的总次数，必须包含一次失败和一次重试。
	closeCalls := component.closeCalls
	component.mu.Unlock()
	if closeCalls != 2 {
		t.Fatalf("组件关闭调用次数=%d，期望=2", closeCalls)
	}
	// err 表示所有组件重试收束后等待协调器完成的结果。
	if err := coordinator.WaitContext(context.Background()); err != nil {
		t.Fatalf("重试成功后 Wait 应完成: %v", err)
	}
}

// TestCoordinatorCloseWaitsForStart 验证关闭不会越过仍在执行的组件启动回调。
func TestCoordinatorCloseWaitsForStart(t *testing.T) {
	// entered、release 控制组件启动回调的阻塞窗口。
	entered := make(chan struct{})
	// release 允许阻塞的启动回调继续完成。
	release := make(chan struct{})
	// component 保存会阻塞启动、随后允许正常关闭的测试组件。
	component := FuncComponent{
		StartFunc: func(context.Context) error {
			close(entered)
			<-release
			return nil
		},
	}
	// coordinator 保存待验证的启动与关闭协调器。
	coordinator := NewCoordinator()
	// err 表示登记阻塞组件时的参数校验错误。
	if err := coordinator.Add(NamedComponent{Name: "starting", Component: component}); err != nil {
		t.Fatalf("登记阻塞组件失败: %v", err)
	}
	// startDone 接收组件启动结果。
	startDone := make(chan error, 1)
	go func() {
		startDone <- coordinator.Start(context.Background())
	}()
	<-entered
	// timeoutCtx、timeoutCancel 限制启动阶段的关闭等待时间。
	timeoutCtx, timeoutCancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	// err 表示启动未完成时关闭等待收到的截止错误。
	err := coordinator.Close(timeoutCtx)
	timeoutCancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("启动阶段关闭应返回截止错误: %v", err)
	}
	// closeDone 接收并发关闭结果；关闭必须等待启动回调结束。
	closeDone := make(chan error, 1)
	go func() {
		closeDone <- coordinator.Close(context.Background())
	}()
	select {
	case <-closeDone:
		t.Fatal("组件启动未结束时关闭不应提前返回")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	// err 表示组件启动完成后的最终启动结果。
	if err := <-startDone; err != nil {
		t.Fatalf("组件启动失败: %v", err)
	}
	// err 表示等待启动完成后执行的关闭结果。
	if err := <-closeDone; err != nil {
		t.Fatalf("等待启动完成后的关闭失败: %v", err)
	}
}

// TestCoordinatorRetrySkipsClosedComponents 验证关闭重试不会重复关闭已成功收束的组件。
func TestCoordinatorRetrySkipsClosedComponents(t *testing.T) {
	// firstCloseCalls 保存第一个组件的关闭调用次数。
	firstCloseCalls := 0
	// first 是首轮成功关闭、重试时应被跳过的组件。
	first := FuncComponent{CloseFunc: func(context.Context) error {
		firstCloseCalls++
		return nil
	}}
	// secondCloseCalls 保存第二个组件的关闭调用次数。
	secondCloseCalls := 0
	// closeFailure 保存第二个组件首轮关闭失败的原因。
	closeFailure := errors.New("close failed")
	// second 是首轮失败、重试时成功关闭的组件。
	second := FuncComponent{CloseFunc: func(context.Context) error {
		secondCloseCalls++
		if secondCloseCalls == 1 {
			return closeFailure
		}
		return nil
	}}
	// coordinator 保存两个可分别收束的组件。
	coordinator := NewCoordinator()
	_ = coordinator.Add(NamedComponent{Name: "first", Component: first})
	_ = coordinator.Add(NamedComponent{Name: "second", Component: second})
	if // err 是启动两个测试组件的结果。
	err := coordinator.Start(context.Background()); err != nil {
		t.Fatalf("启动协调器失败: %v", err)
	}
	if // err 是首轮关闭聚合的组件错误。
	err := coordinator.Close(context.Background()); !errors.Is(err, closeFailure) {
		t.Fatalf("首轮关闭应保留组件错误: %v", err)
	}
	if // err 是重试关闭剩余组件的结果。
	err := coordinator.Close(context.Background()); err != nil {
		t.Fatalf("重试关闭失败: %v", err)
	}
	if firstCloseCalls != 1 || secondCloseCalls != 2 {
		t.Fatalf("关闭调用次数异常: first=%d second=%d", firstCloseCalls, secondCloseCalls)
	}
}

// TestCoordinatorWaitAndCloseNilSemantics 验证空指针和 nil Context 的等待、关闭语义。
func TestCoordinatorWaitAndCloseNilSemantics(t *testing.T) {
	// nilCoordinator 保存空协调器指针。
	var nilCoordinator *Coordinator
	// nilContext 是专门验证关闭和等待 nil Context 防护的空接口值。
	var nilContext context.Context
	nilCoordinator.Wait()
	if // lifecycleContext 是空协调器的默认生命周期 Context。
	lifecycleContext := nilCoordinator.Context(); lifecycleContext == nil {
		t.Fatal("nil 协调器应返回默认 Context")
	}
	if // startErr 是空协调器的初始化错误。
	startErr := nilCoordinator.Start(context.Background()); startErr == nil {
		t.Fatal("nil 协调器应拒绝启动")
	}
	if // err 是空协调器等待的结果。
	err := nilCoordinator.WaitContext(context.Background()); err != nil {
		t.Fatalf("nil 协调器等待不应失败: %v", err)
	}
	if // closeErr 是空协调器的关闭结果。
	closeErr := nilCoordinator.Close(context.Background()); closeErr != nil {
		t.Fatalf("nil 协调器关闭不应失败: %v", closeErr)
	}
	if // err 是 nil 关闭 Context 的参数错误。
	err := NewCoordinator().Close(nilContext); err == nil {
		t.Fatal("nil 关闭 Context 应被拒绝")
	}
	if // err 是 nil 等待 Context 的参数错误。
	err := NewCoordinator().WaitContext(nilContext); err == nil {
		t.Fatal("nil 等待 Context 应被拒绝")
	}
	// coordinator 保存已完成关闭的协调器。
	coordinator := NewCoordinator()
	if // err 是空组件协调器的关闭结果。
	err := coordinator.Close(context.Background()); err != nil {
		t.Fatalf("空协调器关闭失败: %v", err)
	}
	coordinator.Wait()
}

// equalStrings 比较两个生命周期事件列表。
func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	// index 表示当前比较的生命周期事件下标。
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
