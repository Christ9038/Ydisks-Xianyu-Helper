package server

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// taskState 表示 Server 后台任务对外可观测的生命周期状态。
type taskState string

const (
	// taskStateRunning 表示任务已启动但尚未完成。
	taskStateRunning taskState = "running"
	// taskStateSucceeded 表示任务函数正常返回且上下文未取消。
	taskStateSucceeded taskState = "succeeded"
	// taskStateFailed 表示任务函数返回时记录了错误。
	taskStateFailed taskState = "failed"
	// taskStateCanceled 表示任务因父 Context 被主动取消而收束。
	taskStateCanceled taskState = "canceled"
	// taskStateTimedOut 表示任务因带截止时间的 Context 超时而收束。
	taskStateTimedOut taskState = "timed_out"
)

// taskStatusSnapshot 是后台任务的非敏感状态快照，供管理端 DTO 转换使用。
type taskStatusSnapshot struct {
	// ID 是当前 Server 进程内唯一的任务标识，不代表数据库主键。
	ID string
	// Name 是任务的稳定业务名称，用于运维定位任务来源。
	Name string
	// State 是任务当前生命周期状态。
	State taskState
	// StartedAt 是任务开始执行的 UTC 时间。
	StartedAt time.Time
	// FinishedAt 是任务完成或取消的 UTC 时间；仍运行时为空。
	FinishedAt *time.Time
	// DeadlineAt 是任务 Context 的截止时间；没有截止时间时为空。
	DeadlineAt *time.Time
}

// trackedTask 保存单个后台任务的内部状态及其取消上下文。
type trackedTask struct {
	// snapshot 保存任务的可查询字段；修改时由 taskRegistry.mu 保护。
	snapshot taskStatusSnapshot
	// ctx 用于在查询时识别尚未写入终态的取消或超时任务。
	ctx context.Context
}

// taskRegistry 管理 Server 后台任务的有限状态历史。
// registry 只保存任务名称、时间和状态，不保存请求体、Cookie、Token 或错误正文。
type taskRegistry struct {
	// mu 保护 tasks、order 和任务快照；nextID 由原子操作单独保护。
	mu sync.RWMutex
	// nextID 生成当前进程内单调递增的任务序号。
	nextID uint64
	// tasks 按任务 ID 保存运行中和有限历史任务。
	tasks map[string]*trackedTask
	// order 按启动顺序保存任务 ID，用于限制历史容量和稳定返回顺序。
	order []string
	// maxHistory 限制内存中保留的任务数量，避免恢复扫描器长期运行导致无界增长。
	maxHistory int
}

// newTaskRegistry 创建一个保留有限历史的后台任务注册表。
func newTaskRegistry() *taskRegistry {
	return &taskRegistry{
		tasks:      make(map[string]*trackedTask),
		maxHistory: 128,
	}
}

// start 登记任务并返回任务 ID 与幂等收束回调。
// ctx 是任务的取消和超时来源；complete 必须在任务函数退出时调用，重复调用不会覆盖首个终态。
func (r *taskRegistry) start(name string, ctx context.Context) (string, func(error)) {
	if r == nil {
		r = newTaskRegistry()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// taskID 是进程内唯一标识，便于日志与管理端状态查询关联。
	taskID := fmt.Sprintf("srv-task-%d", atomic.AddUint64(&r.nextID, 1))
	// startedAt 记录任务实际进入 goroutine 前的登记时间。
	startedAt := time.Now().UTC()
	// snapshot 是新任务的初始状态快照。
	snapshot := taskStatusSnapshot{ID: taskID, Name: name, State: taskStateRunning, StartedAt: startedAt}
	// deadline、hasDeadline 分别表示 Context 的截止时间及其是否存在截止时间。
	if deadline, hasDeadline := ctx.Deadline(); hasDeadline {
		// deadlineAt 保存 Context 的截止时间，帮助运维识别尚未收束的超时任务。
		deadlineAt := deadline.UTC()
		snapshot.DeadlineAt = &deadlineAt
	}
	r.mu.Lock()
	r.tasks[taskID] = &trackedTask{snapshot: snapshot, ctx: ctx}
	r.order = append(r.order, taskID)
	r.pruneLocked()
	r.mu.Unlock()
	// complete 负责把任务函数的退出结果转换为统一状态；它可被 defer 安全调用。
	complete := func(taskErr error) {
		r.finish(taskID, taskErr)
	}
	return taskID, complete
}

// finish 将任务写入首个终态；错误正文不会进入注册表，避免敏感信息通过管理端泄露。
func (r *taskRegistry) finish(taskID string, taskErr error) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// task 保存待收束的任务记录。
	task, ok := r.tasks[taskID]
	if !ok || task.snapshot.State != taskStateRunning {
		return
	}
	// finishedAt 记录任务函数退出的 UTC 时间。
	finishedAt := time.Now().UTC()
	task.snapshot.FinishedAt = &finishedAt
	if taskErr != nil {
		task.snapshot.State = taskStateFailed
		return
	}
	task.snapshot.State = stateForContext(task.ctx)
}

// stateForContext 将已取消的 Context 分类为主动取消或截止时间超时。
func stateForContext(ctx context.Context) taskState {
	if ctx == nil {
		return taskStateSucceeded
	}
	// contextErr 是任务退出时 Context 的最终错误分类。
	contextErr := ctx.Err()
	switch contextErr {
	case context.DeadlineExceeded:
		return taskStateTimedOut
	case context.Canceled:
		return taskStateCanceled
	default:
		return taskStateSucceeded
	}
}

// list 返回按最近启动时间倒序排列的状态快照，并即时反映尚未收束任务的 Context 超时。
func (r *taskRegistry) list() []taskStatusSnapshot {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	// snapshots 是脱离锁后返回的快照副本，避免调用方持有注册表读锁。
	snapshots := make([]taskStatusSnapshot, 0, len(r.tasks))
	// task 表示当前遍历的内部任务记录。
	for _, task := range r.tasks {
		// snapshot 是当前任务的值副本，指针时间字段在复制前后均不再修改。
		snapshot := task.snapshot
		// contextState 表示任务 Context 当前是否已经取消或超时。
		contextState := stateForContext(task.ctx)
		if snapshot.State == taskStateRunning {
			if contextState != taskStateSucceeded {
				snapshot.State = contextState
			}
		}
		snapshots = append(snapshots, snapshot)
	}
	r.mu.RUnlock()
	sort.SliceStable(snapshots, func(left, right int) bool {
		return snapshots[left].StartedAt.After(snapshots[right].StartedAt)
	})
	return snapshots
}

// pruneLocked 删除最早的已完成历史，调用方必须持有 r.mu 写锁。
func (r *taskRegistry) pruneLocked() {
	for len(r.order) > r.maxHistory {
		// oldestID 是按启动顺序最早的任务标识。
		oldestID := r.order[0]
		r.order = r.order[1:]
		// oldestTask 是待删除的历史记录。
		oldestTask, exists := r.tasks[oldestID]
		if !exists || oldestTask.snapshot.State != taskStateRunning {
			delete(r.tasks, oldestID)
			continue
		}
		// 运行中的任务不能被历史容量清理，放回队尾等待完成后再清理。
		r.order = append(r.order, oldestID)
		return
	}
}
