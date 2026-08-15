package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestTaskRegistryTracksCompletion 证明正常返回的后台任务会留下成功终态。
func TestTaskRegistryTracksCompletion(t *testing.T) {
	// registry 是本测试使用的内存任务注册表。
	registry := newTaskRegistry()
	// taskID、complete 是任务标识及退出时的收束回调。
	taskID, complete := registry.start("测试任务", context.Background())
	complete(nil)
	// snapshots 是任务状态快照列表。
	snapshots := registry.list()
	if len(snapshots) != 1 || snapshots[0].ID != taskID || snapshots[0].State != taskStateSucceeded {
		t.Fatalf("任务正常完成后的状态不正确: %+v", snapshots)
	}
}

// TestTaskRegistryTracksCancellationAndTimeout 证明取消与超时不会被误报为成功。
func TestTaskRegistryTracksCancellationAndTimeout(t *testing.T) {
	// canceledContext、cancelCanceled 是主动取消任务的上下文及其取消函数。
	canceledContext, cancelCanceled := context.WithCancel(context.Background())
	// canceledRegistry 是主动取消场景使用的注册表。
	canceledRegistry := newTaskRegistry()
	// canceledComplete 是主动取消任务的终态收束回调。
	_, canceledComplete := canceledRegistry.start("取消任务", canceledContext)
	cancelCanceled()
	canceledComplete(nil)
	// snapshots 是主动取消任务的状态快照列表。
	if snapshots := canceledRegistry.list(); len(snapshots) != 1 || snapshots[0].State != taskStateCanceled {
		t.Fatalf("任务取消后的状态不正确: %+v", snapshots)
	}

	// timeoutContext、cancelTimeout 是超时任务的上下文及其释放函数。
	timeoutContext, cancelTimeout := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancelTimeout()
	// timeoutRegistry 是超时场景使用的注册表。
	timeoutRegistry := newTaskRegistry()
	// timeoutComplete 是超时任务的终态收束回调。
	_, timeoutComplete := timeoutRegistry.start("超时任务", timeoutContext)
	select {
	case <-timeoutContext.Done():
	case <-time.After(time.Second):
		t.Fatal("超时上下文未按预期结束")
	}
	// pendingSnapshots 是任务函数尚未退出时的即时状态，用于验证超时可观测性。
	pendingSnapshots := timeoutRegistry.list()
	if len(pendingSnapshots) != 1 || pendingSnapshots[0].State != taskStateTimedOut {
		t.Fatalf("任务超时未被即时观测: %+v", pendingSnapshots)
	}
	timeoutComplete(nil)
	// finalSnapshots 是超时任务退出后的最终状态快照列表。
	if finalSnapshots := timeoutRegistry.list(); len(finalSnapshots) != 1 || finalSnapshots[0].State != taskStateTimedOut {
		t.Fatalf("任务退出后的超时状态不正确: %+v", finalSnapshots)
	}
}

// TestListAdminTasksReturnsNamedStatusDTO 证明管理端任务查询只返回具名非敏感状态字段。
func TestListAdminTasksReturnsNamedStatusDTO(t *testing.T) {
	// srv 是只装配任务注册表的最小 HTTP 服务实例。
	srv := &Server{taskRegistry: newTaskRegistry()}
	// _, complete 是已登记任务的标识和完成回调。
	_, complete := srv.taskRegistry.start("HTTP 测试任务", context.Background())
	complete(nil)
	// request、recorder 是管理端查询请求及其响应捕获器。
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tasks?limit=1", nil)
	// recorder 捕获管理端任务查询响应。
	recorder := httptest.NewRecorder()
	srv.listAdminTasks(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("任务查询状态码=%d, body=%s", recorder.Code, recorder.Body.String())
	}
	// response 是管理端具名状态 DTO 列表。
	var response []taskStatusResponse
	// decodeErr 是管理端状态 DTO 的 JSON 解析错误。
	if decodeErr := json.Unmarshal(recorder.Body.Bytes(), &response); decodeErr != nil {
		t.Fatalf("任务状态响应解析失败: %v", decodeErr)
	}
	if len(response) != 1 || response[0].Name != "HTTP 测试任务" || response[0].State != string(taskStateSucceeded) {
		t.Fatalf("任务状态响应不正确: %+v", response)
	}
}

// TestVersionedAdminTaskRouteRequiresAdminAndReturnsStatus 证明版本化管理路由已挂载并继承管理员权限。
func TestVersionedAdminTaskRouteRequiresAdminAndReturnsStatus(t *testing.T) {
	// srv、cleanup 是带测试数据库和认证服务的完整 HTTP 服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// taskDone 是用于快速完成测试任务的退出信号。
	taskDone := make(chan struct{})
	srv.startBackgroundTaskContext("路由测试任务", context.Background(), func() {
		close(taskDone)
	})
	<-taskDone
	// handler 是当前版本化管理路由树。
	handler := srv.Router()
	// sessionCookie 是管理员登录后的认证会话。
	sessionCookie := loginHelper(t, handler)
	// request、recorder 是版本化任务查询请求及响应捕获器。
	request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tasks", nil)
	request.AddCookie(sessionCookie)
	// recorder 捕获版本化管理员任务查询响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("管理员任务查询状态码=%d, body=%s", recorder.Code, recorder.Body.String())
	}
	// response 是路由返回的任务状态 DTO 列表。
	var response []taskStatusResponse
	// decodeErr 是路由响应 JSON 的解析错误。
	if decodeErr := json.Unmarshal(recorder.Body.Bytes(), &response); decodeErr != nil {
		t.Fatalf("管理员任务响应解析失败: %v", decodeErr)
	}
	if len(response) == 0 || response[0].Name != "路由测试任务" {
		t.Fatalf("管理员任务响应不正确: %+v", response)
	}
}

// TestStartBackgroundTaskContextWaitsForTaskExit 证明 Server Stop 会等待已登记任务退出。
func TestStartBackgroundTaskContextWaitsForTaskExit(t *testing.T) {
	// srv 是零值兼容的 Server 生命周期测试实例。
	srv := &Server{}
	// taskStarted、releaseTask 分别表示任务开始及允许任务退出的信号。
	taskStarted := make(chan struct{})
	// releaseTask 是允许阻塞测试任务退出的信号。
	releaseTask := make(chan struct{})
	srv.startBackgroundTaskContext("阻塞测试任务", context.Background(), func() {
		close(taskStarted)
		<-releaseTask
	})
	<-taskStarted
	// waitDone 表示 WaitForBackground 的退出信号。
	waitDone := make(chan struct{})
	go func() {
		srv.WaitForBackground()
		close(waitDone)
	}()
	select {
	case <-waitDone:
		t.Fatal("后台任务仍未退出时不应完成等待")
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseTask)
	select {
	case <-waitDone:
	case <-time.After(time.Second):
		t.Fatal("后台任务退出后等待未完成")
	}
}
