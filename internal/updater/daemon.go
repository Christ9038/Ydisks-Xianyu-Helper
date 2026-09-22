package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// daemonShutdownTimeout 限制 Unix HTTP 请求排空时间。
	daemonShutdownTimeout = 10 * time.Second
	// daemonProbeTimeout 限制 status 和 check 的单次外部探测时间。
	daemonProbeTimeout = 30 * time.Second
)

// Daemon 拥有 Unix HTTP 服务、异步更新事务及其关闭等待责任。
//
// operationMu 只保护 operation、running 和 runContext；持锁期间不执行网络、Docker 或文件 I/O。
// updateWait 只由受理更新的 goroutine 增加和完成，Run 在退出前等待其收束。
type Daemon struct {
	// config 保存固定生产路径和 Unix Socket 地址。
	config runtimeConfig
	// engine 执行部署探测、检查和更新事务。
	engine *updateEngine
	// operationMu 保护最近操作状态、运行标记和 daemon 生命周期 Context。
	operationMu sync.RWMutex
	// operation 是最近一次异步更新的公开状态。
	operation OperationState
	// latest 保存最近一次成功检查的正式版本结论，供状态轮询复用。
	latest *CheckResult
	// running 表示本 daemon 已有更新 goroutine，防止重复受理。
	running bool
	// runContext 在 Run 期间控制后台更新取消；未启动时为 nil。
	runContext context.Context
	// updateWait 等待 daemon 拥有的更新 goroutine 完成回滚或成功收口。
	updateWait sync.WaitGroup
}

// NewDaemon 创建使用固定生产配置、官方 Release 源和宿主机命令执行器的 updater daemon。
func NewDaemon() *Daemon {
	// config 是不接受外部覆盖的生产运行配置。
	config := productionConfig()
	// engine 是生产部署探测和更新事务实现。
	engine := newUpdateEngine(config, osCommandRunner{}, newGitHubReleaseSource(), newHTTPHealthClient())
	return newDaemon(config, engine)
}

// newDaemon 创建包内可测试 daemon，config 和 engine 不能由 HTTP 调用方替换。
func newDaemon(config runtimeConfig, engine *updateEngine) *Daemon {
	return &Daemon{
		config:    config,
		engine:    engine,
		operation: OperationState{Status: OperationIdle},
	}
}

// Run 在固定 Unix Socket 上服务，ctx 取消后停止受理并等待后台更新事务收口。
func (daemon *Daemon) Run(ctx context.Context) error {
	if daemon == nil || daemon.engine == nil {
		return errors.New("updater daemon 未初始化")
	}
	// socketDirectory 是固定 Unix Socket 的父目录。
	socketDirectory := filepath.Dir(daemon.config.socketPath)
	// mkdirErr 表示 Socket 父目录无法创建或权限不足。
	if mkdirErr := os.MkdirAll(socketDirectory, 0o750); mkdirErr != nil {
		return fmt.Errorf("创建 updater socket 目录失败: %w", mkdirErr)
	}
	// staleErr 表示既有 Socket 无法安全清理，避免覆盖普通文件。
	if staleErr := removeStaleSocket(daemon.config.socketPath); staleErr != nil {
		return staleErr
	}
	// listener 是 daemon 独占并负责关闭的 Unix Socket 监听器。
	listener, listenErr := net.Listen("unix", daemon.config.socketPath)
	if listenErr != nil {
		return fmt.Errorf("监听 updater socket 失败: %w", listenErr)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(daemon.config.socketPath)
	}()
	// chmodErr 表示 Socket 权限无法收紧到 systemd 组访问边界。
	if chmodErr := os.Chmod(daemon.config.socketPath, 0o660); chmodErr != nil {
		return fmt.Errorf("设置 updater socket 权限失败: %w", chmodErr)
	}
	// runContext 在 daemon 停止时取消尚未完成的更新，并允许其执行受控回滚。
	runContext, runCancel := context.WithCancel(ctx)
	defer runCancel()
	daemon.operationMu.Lock()
	daemon.runContext = runContext
	daemon.operationMu.Unlock()
	// httpServer 只在 Unix Socket 上暴露无参数的固定操作。
	httpServer := &http.Server{Handler: daemon.handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 35 * time.Second, MaxHeaderBytes: 16 << 10}
	// serveResult 接收唯一 HTTP Serve goroutine 的退出结果，由 Run 负责读取和关闭。
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- httpServer.Serve(listener)
	}()
	select {
	case <-ctx.Done():
		// shutdownContext 独立限制 HTTP 请求排空，不复用已取消的父 Context。
		shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), daemonShutdownTimeout)
		// shutdownErr 保存 HTTP 服务停止结果。
		shutdownErr := httpServer.Shutdown(shutdownContext)
		shutdownCancel()
		runCancel()
		daemon.updateWait.Wait()
		// serveErr 是 Shutdown 后 Serve goroutine 的终态。
		serveErr := <-serveResult
		if shutdownErr != nil {
			return shutdownErr
		}
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return serveErr
		}
		return nil
	case serveErr := <-serveResult: // serveErr 表示 HTTP 服务意外退出或完成正常关闭。
		runCancel()
		daemon.updateWait.Wait()
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			return serveErr
		}
		return nil
	}
}

// handler 构建不接受动态路径参数的 Unix HTTP 路由。
func (daemon *Daemon) handler() http.Handler {
	// mux 是只登记三个固定 updater 操作的私有路由器。
	mux := http.NewServeMux()
	mux.HandleFunc("/status", daemon.handleStatus)
	mux.HandleFunc("/check", daemon.handleCheck)
	mux.HandleFunc("/apply_latest_stable", daemon.handleApplyLatestStable)
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(writer, request)
	})
}

// handleStatus 返回当前部署探测和最近异步操作状态，只允许无参数 GET。
func (daemon *Daemon) handleStatus(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeDaemonError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "status 只支持 GET")
		return
	}
	if requestHasDynamicInput(request) {
		writeDaemonError(writer, http.StatusBadRequest, "parameters_not_allowed", "updater API 不接受查询参数或请求体")
		return
	}
	// probeContext 限制 Docker 和健康接口探测时长。
	probeContext, probeCancel := context.WithTimeout(request.Context(), daemonProbeTimeout)
	defer probeCancel()
	// current 保存当前固定 app 部署快照。
	current, currentErr := daemon.engine.Current(probeContext)
	// response 即使探测失败也返回最近操作，便于 UI 展示 daemon 状态。
	response := StatusResponse{Operation: daemon.operationSnapshot()}
	if currentErr != nil {
		response.Error = "当前部署探测失败"
	} else {
		response.Current = &current
		daemon.operationMu.RLock()
		if daemon.latest != nil {
			// latest 保存最近检查结果的独立副本，避免序列化期间读写竞争。
			latest := *daemon.latest
			latest.Current.RepositoryDigests = append([]string(nil), daemon.latest.Current.RepositoryDigests...)
			latest.Latest.Platforms = append([]string(nil), daemon.latest.Latest.Platforms...)
			response.Latest = &latest.Latest
			response.Available = latest.Available
			response.CanUpdate = latest.CanUpdate
			response.BlockedReason = latest.BlockedReason
		}
		daemon.operationMu.RUnlock()
	}
	writeDaemonJSON(writer, http.StatusOK, response)
}

// handleCheck 返回 GitHub latest 与当前部署的只读更新决策，只允许无参数 GET。
func (daemon *Daemon) handleCheck(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writeDaemonError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "check 只支持 GET")
		return
	}
	if requestHasDynamicInput(request) {
		writeDaemonError(writer, http.StatusBadRequest, "parameters_not_allowed", "updater API 不接受查询参数或请求体")
		return
	}
	// checkContext 限制 GitHub、Docker 和健康接口检查时长。
	checkContext, checkCancel := context.WithTimeout(request.Context(), daemonProbeTimeout)
	defer checkCancel()
	// result 保存不执行副作用的稳定版本检查结论。
	result, checkErr := daemon.engine.Check(checkContext)
	if checkErr != nil {
		writeDaemonError(writer, http.StatusBadGateway, "update_check_failed", "检查最新稳定版本失败")
		return
	}
	daemon.operationMu.Lock()
	daemon.latest = &result
	daemon.operationMu.Unlock()
	writeDaemonJSON(writer, http.StatusOK, result)
}

// handleApplyLatestStable 受理唯一固定更新动作，不解析请求体、版本、镜像或路径。
func (daemon *Daemon) handleApplyLatestStable(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeDaemonError(writer, http.StatusMethodNotAllowed, "method_not_allowed", "apply_latest_stable 只支持 POST")
		return
	}
	if requestHasDynamicInput(request) {
		writeDaemonError(writer, http.StatusBadRequest, "parameters_not_allowed", "updater API 不接受请求参数或请求体")
		return
	}
	// operation 保存受理后立即返回的异步操作状态。
	operation, applyErr := daemon.startApply(request.Context())
	if applyErr != nil {
		switch {
		case errors.Is(applyErr, errUpdateBusy):
			writeDaemonError(writer, http.StatusConflict, "update_in_progress", "已有更新正在执行")
		case errors.Is(applyErr, errNoUpdate):
			writeDaemonError(writer, http.StatusConflict, "already_latest", "当前已是最新稳定版本")
		case errors.Is(applyErr, errCustomDeployment):
			writeDaemonError(writer, http.StatusConflict, "deployment_blocked", "当前部署不允许自动切换到官方稳定版")
		default:
			writeDaemonError(writer, http.StatusBadGateway, "update_check_failed", "更新前检查失败")
		}
		return
	}
	writeDaemonJSON(writer, http.StatusAccepted, ApplyResponse{Accepted: true, Operation: operation})
}

// requestHasDynamicInput 读取至多一个正文字节，确保 chunked 请求也不能绕过无参数协议。
func requestHasDynamicInput(request *http.Request) bool {
	if request.URL.RawQuery != "" {
		return true
	}
	if request.Body == nil || request.Body == http.NoBody {
		return false
	}
	// payload、readErr 分别是最多一个正文字节和读取异常；任一存在都按动态输入拒绝。
	payload, readErr := io.ReadAll(io.LimitReader(request.Body, 1))
	return len(payload) > 0 || readErr != nil
}

// startApply 先同步确认可更新，再创建 daemon 拥有的后台事务。
func (daemon *Daemon) startApply(requestContext context.Context) (OperationState, error) {
	daemon.operationMu.RLock()
	// alreadyRunning 表示无需执行外部检查即可拒绝重复请求。
	alreadyRunning := daemon.running
	// runContext 是 daemon 生命周期 Context，后台事务不能继承短暂 HTTP 请求取消。
	runContext := daemon.runContext
	daemon.operationMu.RUnlock()
	if alreadyRunning {
		return OperationState{}, errUpdateBusy
	}
	if runContext == nil {
		return OperationState{}, errors.New("updater daemon 尚未运行")
	}
	// checkContext 只用于受理前检查，客户端断开会取消该阶段。
	checkContext, checkCancel := context.WithTimeout(requestContext, daemonProbeTimeout)
	defer checkCancel()
	// check 保存当前可更新目标，后台事务仍会在 flock 内重新检查。
	check, checkErr := daemon.engine.Check(checkContext)
	if checkErr != nil {
		return OperationState{}, checkErr
	}
	if !check.CanUpdate {
		if check.BlockedReason == "already_latest" {
			return OperationState{}, errNoUpdate
		}
		return OperationState{}, fmt.Errorf("%w: %s", errCustomDeployment, check.BlockedReason)
	}
	// latest 保存受理前确认过的版本结论，状态页可在后台更新期间继续展示目标版本。
	daemon.operationMu.Lock()
	daemon.latest = &check
	daemon.operationMu.Unlock()
	// requestID 是 daemon 生成且不含用户输入的操作标识。
	requestID, requestIDErr := newRequestID()
	if requestIDErr != nil {
		return OperationState{}, requestIDErr
	}
	// startedAt 是受理操作的 UTC 时间。
	startedAt := time.Now().UTC()
	// operation 是向调用方返回并写入 daemon 状态的排队记录。
	operation := OperationState{
		RequestID:     requestID,
		Status:        OperationQueued,
		Phase:         "queued",
		TargetVersion: check.Latest.Version,
		Message:       "更新请求已受理",
		StartedAt:     &startedAt,
	}
	daemon.operationMu.Lock()
	if daemon.running {
		daemon.operationMu.Unlock()
		return OperationState{}, errUpdateBusy
	}
	daemon.running = true
	daemon.operation = operation
	daemon.updateWait.Add(1)
	daemon.operationMu.Unlock()
	go daemon.runApply(runContext, requestID)
	return operation, nil
}

// runApply 执行 daemon 拥有的异步事务，并将最终状态写回内存。
func (daemon *Daemon) runApply(ctx context.Context, requestID string) {
	defer daemon.updateWait.Done()
	// report 把事务阶段转换为可查询状态，不持锁执行任何外部 I/O。
	report := func(phase, message string) {
		daemon.operationMu.Lock()
		daemon.operation.Status = OperationRunning
		daemon.operation.Phase = phase
		daemon.operation.Message = message
		daemon.operationMu.Unlock()
	}
	// result 和 applyErr 保存更新事务是否已回滚及最终错误。
	result, applyErr := daemon.engine.ApplyLatestStable(ctx, requestID, report)
	// finishedAt 是事务进入稳定终态的 UTC 时间。
	finishedAt := time.Now().UTC()
	daemon.operationMu.Lock()
	daemon.running = false
	daemon.operation.FinishedAt = &finishedAt
	if applyErr == nil {
		daemon.operation.Status = OperationSucceeded
		daemon.operation.Phase = "completed"
		daemon.operation.Message = "更新完成并通过健康检查"
	} else if result.rolledBack {
		daemon.operation.Status = OperationRolledBack
		daemon.operation.Phase = "rolled_back"
		daemon.operation.Message = "新版本未通过验证，已恢复旧版本"
	} else {
		daemon.operation.Status = OperationFailed
		daemon.operation.Phase = "failed"
		daemon.operation.Message = "更新失败，请检查 updater daemon 日志"
	}
	daemon.operationMu.Unlock()
	if applyErr != nil {
		slog.Error("闲鱼助手宿主机更新失败", "request_id", requestID, "rolled_back", result.rolledBack, "err", applyErr)
	}
}

// operationSnapshot 在线程安全边界内复制最近操作状态。
func (daemon *Daemon) operationSnapshot() OperationState {
	daemon.operationMu.RLock()
	defer daemon.operationMu.RUnlock()
	return daemon.operation
}

// removeStaleSocket 只删除遗留 Unix Socket；同路径若是普通文件则拒绝覆盖。
func removeStaleSocket(socketPath string) error {
	// info 保存既有路径类型；不存在时无需处理。
	info, statErr := os.Lstat(socketPath)
	if errors.Is(statErr, os.ErrNotExist) {
		return nil
	}
	if statErr != nil {
		return fmt.Errorf("检查 updater socket 失败: %w", statErr)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return errors.New("updater socket 路径已被非 socket 文件占用")
	}
	// removeErr 表示遗留 Socket 无法删除，daemon 不应继续覆盖该路径。
	if removeErr := os.Remove(socketPath); removeErr != nil {
		return fmt.Errorf("删除遗留 updater socket 失败: %w", removeErr)
	}
	return nil
}

// writeDaemonJSON 写入具名 updater API 响应；编码失败时连接由 net/http 收束。
func writeDaemonJSON(writer http.ResponseWriter, status int, payload any) {
	writer.WriteHeader(status)
	// encodeErr 表示响应模型无法序列化；当前全部模型均为确定性 JSON 类型。
	if encodeErr := json.NewEncoder(writer).Encode(payload); encodeErr != nil {
		slog.Error("写入 updater API 响应失败", "err", encodeErr)
	}
}

// writeDaemonError 写入不包含宿主机路径、命令输出或敏感配置的稳定错误响应。
func writeDaemonError(writer http.ResponseWriter, status int, code, message string) {
	writeDaemonJSON(writer, status, ErrorResponse{Code: code, Message: message})
}
