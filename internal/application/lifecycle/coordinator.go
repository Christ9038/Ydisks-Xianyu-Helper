// Package lifecycle 提供应用组件统一的启动、取消、关闭和等待协调。
// 组件由 cmd 在进程边界装配；协调器不依赖 HTTP、数据库或平台实现。
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrStopped 表示生命周期协调器已经关闭，不能再次启动。
var ErrStopped = errors.New("应用生命周期协调器已关闭")

// Component 定义一个可被进程生命周期拥有者启动和关闭的组件。
type Component interface {
	// Start 在给定进程 Context 下启动组件；返回错误时协调器会回滚已启动组件。
	Start(context.Context) error
	// Close 在给定关闭 Context 下停止组件并等待其后台任务收束。
	Close(context.Context) error
}

// NamedComponent 将稳定名称与生命周期组件绑定，便于失败日志定位。
type NamedComponent struct {
	// Name 是组件的稳定诊断名称，不承载敏感数据。
	Name string
	// Component 是实际拥有后台任务的生命周期组件。
	Component Component
}

// FuncComponent 将两个函数适配为生命周期组件；函数内部必须自行保证重复关闭幂等。
type FuncComponent struct {
	// StartFunc 执行组件启动；为空时视为无需启动。
	StartFunc func(context.Context) error
	// CloseFunc 执行组件关闭；为空时视为无需关闭。
	CloseFunc func(context.Context) error
}

// Start 启动函数适配组件。
func (component FuncComponent) Start(ctx context.Context) error {
	if component.StartFunc == nil {
		return nil
	}
	return component.StartFunc(ctx)
}

// Close 关闭函数适配组件。
func (component FuncComponent) Close(ctx context.Context) error {
	if component.CloseFunc == nil {
		return nil
	}
	return component.CloseFunc(ctx)
}

// Coordinator 统一拥有进程级 Context、组件启动顺序和反向关闭顺序。
// mu 只保护状态与取消函数；组件 Start/Close 始终在锁外执行，避免持锁等待外部 I/O。
type Coordinator struct {
	// mu 保护 started、closing、closed、parentCancel 和关闭结果。
	mu sync.Mutex
	// components 保存按启动顺序登记的组件。
	components []NamedComponent
	// startedCount 表示已经成功或正在启动的组件数量；失败回滚时包含当前组件。
	startedCount int
	// lifecycleCtx 是所有后台组件共享的进程生命周期 Context。
	lifecycleCtx context.Context
	// parentCancel 取消所有组件继承的生命周期 Context。
	parentCancel context.CancelFunc
	// started 表示协调器是否已接受启动请求。
	started bool
	// starting 表示组件启动回调正在执行；关闭调用必须等待该阶段结束。
	starting bool
	// startDone 在启动阶段结束后关闭，供并发 Close 等待而不持锁阻塞。
	startDone chan struct{}
	// closing 表示当前是否已有关闭调用在执行。
	closing bool
	// closed 表示协调器已完成关闭且不再允许启动。
	closed bool
	// done 在关闭完成后关闭，供并发 Close/Wait 调用等待。
	done chan struct{}
	// closeErr 保存首次关闭或启动回滚的聚合错误。
	closeErr error
}

// NewCoordinator 创建空的生命周期协调器；组件必须在 Start 前通过 Add 登记。
func NewCoordinator() *Coordinator {
	return &Coordinator{done: make(chan struct{})}
}

// Add 登记一个生命周期组件；启动后或关闭后禁止修改组件集合。
func (coordinator *Coordinator) Add(component NamedComponent) error {
	if coordinator == nil {
		return errors.New("生命周期协调器未初始化")
	}
	if component.Component == nil {
		return errors.New("生命周期组件不能为空")
	}
	if component.Name == "" {
		return errors.New("生命周期组件名称不能为空")
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if coordinator.started || coordinator.closing || coordinator.closed {
		return errors.New("生命周期协调器已开始运行，不能追加组件")
	}
	coordinator.components = append(coordinator.components, component)
	return nil
}

// Context 返回应用组件共享的生命周期 Context；未启动时返回可取消前的空闲 Context。
func (coordinator *Coordinator) Context() context.Context {
	if coordinator == nil {
		return context.Background()
	}
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	if coordinator.lifecycleCtx == nil {
		return context.Background()
	}
	return coordinator.lifecycleCtx
}

// Start 按登记顺序启动全部组件；任一组件失败时按逆序关闭已启动组件并拒绝再次启动。
func (coordinator *Coordinator) Start(parent context.Context) error {
	if coordinator == nil {
		return errors.New("生命周期协调器未初始化")
	}
	if parent == nil {
		return errors.New("生命周期父 Context 不能为空")
	}
	coordinator.mu.Lock()
	if coordinator.closed || coordinator.closing {
		coordinator.mu.Unlock()
		return ErrStopped
	}
	if coordinator.started {
		coordinator.mu.Unlock()
		return nil
	}
	coordinator.starting = true
	coordinator.startDone = make(chan struct{})
	// lifecycleCtx、cancel 保存全部组件共享的生命周期上下文和取消函数。
	lifecycleCtx, cancel := context.WithCancel(parent)
	coordinator.lifecycleCtx = lifecycleCtx
	coordinator.parentCancel = cancel
	coordinator.started = true
	// components 是启动阶段的组件快照，避免组件回调期间持有协调器锁。
	components := append([]NamedComponent(nil), coordinator.components...)
	coordinator.mu.Unlock()

	// index、component 分别表示当前启动组件在登记快照中的序号和定义。
	for index, component := range components {
		coordinator.mu.Lock()
		coordinator.startedCount = index + 1
		coordinator.mu.Unlock()
		// err 表示当前组件启动失败；失败会触发包含当前组件的逆序回滚。
		if err := component.Component.Start(lifecycleCtx); err != nil {
			// rollbackErr 表示回滚已启动组件时追加产生的关闭错误。
			rollbackErr := coordinator.rollback(components[:index+1], cancel)
			return fmt.Errorf("启动生命周期组件 %q 失败: %w", component.Name, errors.Join(err, rollbackErr))
		}
	}
	coordinator.mu.Lock()
	coordinator.starting = false
	close(coordinator.startDone)
	coordinator.mu.Unlock()
	return nil
}

// rollback 在启动失败时逆序关闭已开始的组件，并永久结束协调器。
func (coordinator *Coordinator) rollback(components []NamedComponent, cancel context.CancelFunc) error {
	if cancel != nil {
		cancel()
	}
	// rollbackContext 限制启动失败后的组件关闭时间；rollbackCancel 负责释放定时器。
	rollbackContext, rollbackCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer rollbackCancel()
	// rollbackErr 聚合回滚期间各组件的关闭错误。
	var rollbackErr error
	// index 表示当前逆序回滚的组件下标。
	for index := len(components) - 1; index >= 0; index-- {
		// err 表示当前组件回滚关闭失败。
		if err := components[index].Component.Close(rollbackContext); err != nil {
			rollbackErr = errors.Join(rollbackErr, fmt.Errorf("回滚生命周期组件 %q 失败: %w", components[index].Name, err))
		}
	}
	coordinator.mu.Lock()
	coordinator.closed = true
	coordinator.starting = false
	if coordinator.startDone != nil {
		close(coordinator.startDone)
	}
	coordinator.closing = false
	coordinator.closeErr = rollbackErr
	close(coordinator.done)
	coordinator.mu.Unlock()
	return rollbackErr
}

// Close 取消进程 Context，并按登记逆序关闭所有组件；重复调用保持幂等。
func (coordinator *Coordinator) Close(ctx context.Context) error {
	if coordinator == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("生命周期关闭 Context 不能为空")
	}
	for {
		coordinator.mu.Lock()
		if !coordinator.starting {
			break
		}
		// startDone 是当前启动阶段的结束信号；等待时不持有协调器锁。
		startDone := coordinator.startDone
		coordinator.mu.Unlock()
		select {
		case <-startDone:
			continue
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if coordinator.closed {
		// err 保存已完成关闭的历史错误，重复关闭直接复用该结果。
		err := coordinator.closeErr
		coordinator.mu.Unlock()
		return err
	}
	if coordinator.closing {
		// done 是首个关闭调用完成时关闭的信号。
		done := coordinator.done
		coordinator.mu.Unlock()
		select {
		case <-done:
			coordinator.mu.Lock()
			// err 保存首个关闭调用的聚合结果。
			err := coordinator.closeErr
			coordinator.mu.Unlock()
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	coordinator.closing = true
	// components 保存已经成功启动的组件快照，关闭时按其登记顺序逆序处理。
	components := append([]NamedComponent(nil), coordinator.components[:coordinator.startedCount]...)
	// cancel 是协调器共享 Context 的取消函数；组件关闭前先广播取消。
	cancel := coordinator.parentCancel
	coordinator.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	// closeErr 聚合全部组件关闭错误，确保后续组件仍有机会收束。
	var closeErr error
	// index 表示当前逆序关闭的组件下标。
	for index := len(components) - 1; index >= 0; index-- {
		// err 表示当前组件关闭失败。
		if err := components[index].Component.Close(ctx); err != nil {
			closeErr = errors.Join(closeErr, fmt.Errorf("关闭生命周期组件 %q 失败: %w", components[index].Name, err))
		}
	}
	coordinator.mu.Lock()
	coordinator.closed = true
	coordinator.closing = false
	coordinator.closeErr = closeErr
	close(coordinator.done)
	coordinator.mu.Unlock()
	return closeErr
}

// Wait 等待协调器关闭；未启动且未关闭的协调器会阻塞，调用者应先调用 Start 或 Close。
func (coordinator *Coordinator) Wait() {
	if coordinator == nil {
		return
	}
	<-coordinator.done
}

// WaitContext 在 Context 截止前等待协调器完成关闭。
func (coordinator *Coordinator) WaitContext(ctx context.Context) error {
	if coordinator == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("生命周期等待 Context 不能为空")
	}
	select {
	case <-coordinator.done:
		coordinator.mu.Lock()
		// err 保存协调器关闭时记录的聚合错误。
		err := coordinator.closeErr
		coordinator.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
