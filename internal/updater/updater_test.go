package updater

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fixedReleaseSource 为 updater 测试返回固定正式版本。
type fixedReleaseSource struct {
	// manifest 是测试使用的已验证正式版本清单。
	manifest ReleaseManifest
}

// LatestStable 返回固定正式版本清单。
func (source fixedReleaseSource) LatestStable(context.Context) (ReleaseManifest, error) {
	return source.manifest, nil
}

// recordingRunner 记录 updater 构造的固定宿主机命令并返回可编程结果。
type recordingRunner struct {
	// mu 保护并发测试中的命令记录。
	mu sync.Mutex
	// calls 保存 executable 和参数拼接后的命令记录。
	calls []string
	// run 根据命令内容返回测试输出。
	run func(string, ...string) ([]byte, error)
}

// Run 记录命令后调用测试实现。
func (runner *recordingRunner) Run(ctx context.Context, executable string, args ...string) ([]byte, error) {
	// contextErr 表示测试命令在记录前已被调用方取消。
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, contextErr
	}
	runner.mu.Lock()
	runner.calls = append(runner.calls, executable+" "+strings.Join(args, " "))
	runner.mu.Unlock()
	return runner.run(executable, args...)
}

// stableManifest 构造完整且支持当前平台的正式版 manifest。
func stableManifest(version string) ReleaseManifest {
	return ReleaseManifest{
		Schema: 1, Version: version, Tag: "v" + version, Commit: strings.Repeat("a", 40), Image: OfficialImage,
		ManifestDigest: "sha256:" + strings.Repeat("b", 64), PublishedAt: time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC), Platforms: []string{"linux/arm64"},
	}
}

// stableDeployment 构造满足更新条件的 SQLite 官方部署。
func stableDeployment() CurrentDeployment {
	return CurrentDeployment{
		ImageReference: OfficialImage + ":v1.0.0", ImageID: "sha256:" + strings.Repeat("c", 64), RepositoryDigests: []string{OfficialImage + "@sha256:" + strings.Repeat("d", 64)}, ContainerHealth: "healthy",
		Build: BuildInfo{Status: "ok", Database: "ok", Version: "1.0.0", Commit: strings.Repeat("c", 12), BuildTime: "2026-09-21T00:00:00Z"},
		Kind:  DeploymentStable, DatabaseKind: "sqlite", DataMountSupported: true,
	}
}

// TestBuildCheckResultBlocksUnsafeDeployments 验证数据库、存储、健康和镜像来源均在执行前阻断。
func TestBuildCheckResultBlocksUnsafeDeployments(t *testing.T) {
	// latest 是所有阻断样例共用的新正式版本。
	latest := stableManifest("1.0.1")
	// cases 保存单一不安全条件及其稳定阻断原因。
	cases := []struct {
		// name 是子测试名称。
		name string
		// mutate 只修改当前样例关联的部署条件。
		mutate func(*CurrentDeployment)
		// reason 是预期机器可读阻断原因。
		reason string
	}{
		{name: "postgres", mutate: func(current *CurrentDeployment) { current.DatabaseKind = "postgres" }, reason: "unsupported_database"},
		{name: "named volume", mutate: func(current *CurrentDeployment) { current.DataMountSupported = false }, reason: "unsupported_storage"},
		{name: "unhealthy", mutate: func(current *CurrentDeployment) { current.ContainerHealth = "starting" }, reason: "current_unhealthy"},
		{name: "custom", mutate: func(current *CurrentDeployment) { current.Kind = DeploymentCustom }, reason: "custom_image"},
	}
	for _, testCase := range cases { // testCase 是当前执行的不安全部署样例。
		t.Run(testCase.name, func(t *testing.T) {
			// current 是当前样例修改后的部署快照。
			current := stableDeployment()
			testCase.mutate(&current)
			// result、checkErr 分别是更新决策和版本比较错误。
			result, checkErr := buildCheckResult(current, latest)
			if checkErr != nil || result.CanUpdate || result.BlockedReason != testCase.reason {
				t.Fatalf("result=%+v err=%v", result, checkErr)
			}
		})
	}
	// allowed、allowedErr 验证完整安全条件允许更新。
	allowed, allowedErr := buildCheckResult(stableDeployment(), latest)
	if allowedErr != nil || !allowed.Available || !allowed.CanUpdate || allowed.BlockedReason != "" {
		t.Fatalf("allowed=%+v err=%v", allowed, allowedErr)
	}
}

// TestReleaseManifestValidation 验证正式版清单拒绝可变仓库、错误摘要和缺失平台。
func TestReleaseManifestValidation(t *testing.T) {
	// manifest 是后续逐项破坏的有效清单。
	manifest := stableManifest("1.2.3")
	// validationErr 表示有效正式版清单未通过基础校验。
	if validationErr := validateReleaseManifest("v1.2.3", "linux/arm64", manifest); validationErr != nil {
		t.Fatalf("valid manifest: %v", validationErr)
	}
	manifest.Image = "example.com/custom/image"
	// validationErr 表示自定义镜像被正式版清单校验拒绝。
	if validationErr := validateReleaseManifest("v1.2.3", "linux/arm64", manifest); validationErr == nil {
		t.Fatal("custom image should be rejected")
	}
	manifest = stableManifest("1.2.3")
	manifest.ManifestDigest = "latest"
	// validationErr 表示可变镜像引用被正式版清单校验拒绝。
	if validationErr := validateReleaseManifest("v1.2.3", "linux/arm64", manifest); validationErr == nil {
		t.Fatal("mutable digest should be rejected")
	}
	manifest = stableManifest("1.2.3")
	// validationErr 表示缺少当前平台的清单被校验拒绝。
	if validationErr := validateReleaseManifest("v1.2.3", "linux/amd64", manifest); validationErr == nil {
		t.Fatal("missing platform should be rejected")
	}
}

// TestClientUsesFixedUnixRoutes 验证客户端只发送三个固定路径且更新请求没有参数或正文。
func TestClientUsesFixedUnixRoutes(t *testing.T) {
	// socketPath 是当前测试独占的 Unix Socket。
	socketPath := filepath.Join(t.TempDir(), "updater.sock")
	// listener 是测试 HTTP 服务的 Unix 监听器。
	listener, listenErr := net.Listen("unix", socketPath)
	if listenErr != nil {
		t.Fatalf("listen unix: %v", listenErr)
	}
	defer listener.Close()
	// requests 保存服务端收到的方法、路径、查询和正文长度。
	requests := make(chan string, 3)
	// server 返回三个客户端方法各自要求的成功模型。
	server := &http.Server{Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests <- request.Method + " " + request.URL.RequestURI() + " " + request.Header.Get("Content-Length")
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/status":
			_ = json.NewEncoder(writer).Encode(StatusResponse{Operation: OperationState{Status: OperationIdle}})
		case "/check":
			_ = json.NewEncoder(writer).Encode(CheckResult{})
		case "/apply_latest_stable":
			writer.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(writer).Encode(ApplyResponse{Accepted: true, Operation: OperationState{Status: OperationQueued}})
		}
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	// client 只在包内测试中替换 Socket 路径，生产构造始终使用固定默认路径。
	client := newClient(socketPath)
	// statusErr 表示固定状态请求未按契约完成。
	if _, statusErr := client.Status(context.Background()); statusErr != nil {
		t.Fatalf("status: %v", statusErr)
	}
	// checkErr 表示固定检查请求未按契约完成。
	if _, checkErr := client.Check(context.Background()); checkErr != nil {
		t.Fatalf("check: %v", checkErr)
	}
	// applyErr 表示无参数更新请求未按契约完成。
	if _, applyErr := client.ApplyLatestStable(context.Background()); applyErr != nil {
		t.Fatalf("apply: %v", applyErr)
	}
	// expected 是三个固定无参数请求的稳定顺序。
	expected := []string{"GET /status ", "GET /check ", "POST /apply_latest_stable 0"}
	for _, want := range expected { // want 是当前预期的固定方法、路径和正文长度。
		// got 是 Unix 测试服务实际收到的请求摘要。
		if got := <-requests; got != want {
			t.Fatalf("request=%q want=%q", got, want)
		}
	}
}

// TestDaemonRejectsDynamicParameters 验证 Unix API 在进入 Docker 或文件逻辑前拒绝动态输入。
func TestDaemonRejectsDynamicParameters(t *testing.T) {
	// daemon 不需要 engine，因为所有样例都应在参数边界提前返回。
	daemon := &Daemon{}
	// cases 保存动态查询、请求体和未知路径样例。
	cases := []struct {
		// method、path 和 body 描述当前协议攻击面样例。
		method string
		path   string
		body   string
		// chunked 表示请求使用未知长度传输编码，验证不能绕过正文限制。
		chunked bool
		// status 是预期拒绝状态。
		status int
	}{
		{method: http.MethodGet, path: "/status?image=custom", status: http.StatusBadRequest},
		{method: http.MethodGet, path: "/check?version=1.2.3", status: http.StatusBadRequest},
		{method: http.MethodPost, path: "/apply_latest_stable", body: `{"image":"custom"}`, status: http.StatusBadRequest},
		{method: http.MethodPost, path: "/apply_latest_stable", body: `x`, chunked: true, status: http.StatusBadRequest},
		{method: http.MethodPost, path: "/shell", status: http.StatusNotFound},
	}
	for _, testCase := range cases { // testCase 是当前协议边界样例。
		// request 是当前动态参数请求。
		request, requestErr := http.NewRequest(testCase.method, testCase.path, strings.NewReader(testCase.body))
		if requestErr != nil {
			t.Fatalf("request: %v", requestErr)
		}
		if testCase.chunked {
			request.ContentLength = -1
			request.TransferEncoding = []string{"chunked"}
		}
		// recorder 捕获 daemon 拒绝响应。
		recorder := &responseRecorder{header: make(http.Header)}
		daemon.handler().ServeHTTP(recorder, request)
		if recorder.status != testCase.status {
			t.Fatalf("%s %s status=%d want=%d", testCase.method, testCase.path, recorder.status, testCase.status)
		}
	}
}

// responseRecorder 是不依赖 httptest 的最小 HTTP 响应记录器。
type responseRecorder struct {
	// header 保存 handler 写入的响应头。
	header http.Header
	// status 保存 handler 写入的状态码。
	status int
}

// Header 返回可写响应头。
func (recorder *responseRecorder) Header() http.Header { return recorder.header }

// Write 丢弃测试响应体并补齐默认成功状态。
func (recorder *responseRecorder) Write(payload []byte) (int, error) {
	if recorder.status == 0 {
		recorder.status = http.StatusOK
	}
	return len(payload), nil
}

// WriteHeader 保存首个响应状态码。
func (recorder *responseRecorder) WriteHeader(status int) {
	if recorder.status == 0 {
		recorder.status = status
	}
}

// TestEnvironmentImageReplacementPreservesSecrets 验证镜像切换不会重排或回显其他环境配置。
func TestEnvironmentImageReplacementPreservesSecrets(t *testing.T) {
	// original 包含需要原样保留的密钥和注释。
	original := []byte("# production\nXIANYU_DATA_KEY=secret-value\nXIANYU_IMAGE=old-image\nDATABASE_URL=sqlite://data/xianyu_data.db\n")
	// updatedBytes、replacementErr 分别是只替换镜像引用后的环境文件和歧义检查错误。
	updatedBytes, replacementErr := replaceEnvironmentImage(original, OfficialImage+"@sha256:"+strings.Repeat("d", 64))
	if replacementErr != nil {
		t.Fatalf("replace image: %v", replacementErr)
	}
	// updated 是便于断言的环境文件文本。
	updated := string(updatedBytes)
	if !strings.Contains(updated, "XIANYU_DATA_KEY=secret-value\n") || !strings.Contains(updated, "DATABASE_URL=sqlite://data/xianyu_data.db\n") {
		t.Fatalf("sensitive lines changed: %q", updated)
	}
	if strings.Count(updated, "XIANYU_IMAGE=") != 1 || strings.Contains(updated, "XIANYU_IMAGE=old-image") {
		t.Fatalf("image line not replaced exactly once: %q", updated)
	}
	// 临时文件变量保留 os 导入并验证测试工作区可写权限。
	if writeErr := os.WriteFile(filepath.Join(t.TempDir(), "env"), []byte(updated), 0o600); writeErr != nil {
		t.Fatalf("write updated env: %v", writeErr)
	}
	// duplicateErr 表示重复镜像声明被 fail closed 拒绝。
	if _, duplicateErr := replaceEnvironmentImage([]byte("XIANYU_IMAGE=one\nXIANYU_IMAGE=two\n"), OfficialImage+":v1.0.0"); duplicateErr == nil {
		t.Fatal("duplicate XIANYU_IMAGE declarations should be rejected")
	}
}

// TestApplyRollbackPreservesWritesBeforeQuiesce 验证最终回滚使用停止 app 后的备份并恢复原始环境配置。
func TestApplyRollbackPreservesWritesBeforeQuiesce(t *testing.T) {
	// root 是当前事务测试的固定部署根目录。
	root := t.TempDir()
	// dataDirectory 是模拟 `/app/data` 绑定挂载的宿主机目录。
	dataDirectory := filepath.Join(root, "data")
	// mkdirErr 表示模拟数据目录无法创建。
	if mkdirErr := os.MkdirAll(dataDirectory, 0o700); mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	// databaseFile 是模拟生产 SQLite 主文件。
	databaseFile := filepath.Join(dataDirectory, "xianyu_data.db")
	// writeErr 表示模拟初始数据库无法写入。
	if writeErr := os.WriteFile(databaseFile, []byte("initial"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	// environmentFile 是 updater 唯一允许修改的部署环境文件。
	environmentFile := filepath.Join(root, ".env")
	// originalEnvironment 保存更新前必须在回滚后逐字恢复的环境配置。
	originalEnvironment := []byte("XIANYU_IMAGE=" + OfficialImage + ":v1.0.0\nDATABASE_URL=sqlite:///app/data/xianyu_data.db\n")
	// writeErr 表示模拟环境配置无法写入。
	if writeErr := os.WriteFile(environmentFile, originalEnvironment, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	// composeFile 是备份流程要求存在的固定 Compose 文件。
	composeFile := filepath.Join(root, "compose.yml")
	// writeErr 表示模拟 Compose 文件无法写入。
	if writeErr := os.WriteFile(composeFile, []byte("services: {}\n"), 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	// currentBuild 是更新前和回滚后必须一致的构建身份。
	currentBuild := BuildInfo{Status: "ok", Database: "ok", Version: "1.0.0", Commit: strings.Repeat("c", 12), BuildTime: "before"}
	// phaseMu 保护健康服务器读取的模拟容器阶段。
	var phaseMu sync.RWMutex
	// phase 是 old、target 中的当前模拟运行镜像。
	phase := "old"
	// healthServer 按模拟容器阶段返回旧版本或故意不匹配的候选版本。
	healthServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		phaseMu.RLock()
		// currentPhase 是当前请求观察到的稳定模拟阶段。
		currentPhase := phase
		phaseMu.RUnlock()
		// build 是当前阶段上报的健康构建身份。
		build := currentBuild
		if currentPhase == "target" {
			build = BuildInfo{Status: "ok", Database: "ok", Version: "1.0.1", Commit: "deadbeef", BuildTime: "target"}
		}
		_ = json.NewEncoder(writer).Encode(build)
	}))
	defer healthServer.Close()
	// config 使用临时路径和短健康超时执行完整更新与回滚事务。
	config := runtimeConfig{
		projectDir: root, composeFile: composeFile, envFile: environmentFile, databaseFile: databaseFile,
		backupRoot: filepath.Join(root, "backups"), lockFile: filepath.Join(root, "update.lock"), socketPath: filepath.Join(root, "updater.sock"),
		healthURL: healthServer.URL, composeService: "app", composeProject: "test-project",
		updateTimeout: 2 * time.Second, healthTimeout: 20 * time.Millisecond, healthInterval: time.Millisecond,
	}
	// manifest 是事务准备切换但最终健康身份不匹配的正式版本。
	manifest := stableManifest("1.0.1")
	// runner 记录命令并模拟 Docker、Compose 与 sqlite3 的确定性结果。
	runner := &recordingRunner{}
	runner.run = func(executable string, args ...string) ([]byte, error) {
		if executable == sqliteExecutable {
			if len(args) > 0 && args[0] == "-readonly" {
				return []byte("ok\n"), nil
			}
			// destination 是 `.backup` 元命令中由 updater 生成的固定恢复点路径。
			destination := strings.TrimSuffix(strings.TrimPrefix(args[1], ".backup '"), "'")
			// databaseContent 是执行备份时生产数据库的精确内容。
			databaseContent, readErr := os.ReadFile(databaseFile)
			if readErr != nil {
				return nil, readErr
			}
			return nil, os.WriteFile(destination, databaseContent, 0o600)
		}
		if executable != dockerExecutable {
			return nil, nil
		}
		// hasArg 判断当前固定 Docker 命令是否包含目标操作参数。
		hasArg := func(want string) bool {
			for _, argument := range args { // argument 是当前 Docker 命令参数。
				if argument == want {
					return true
				}
			}
			return false
		}
		switch {
		case len(args) > 0 && args[0] == "compose" && hasArg("config"):
			// rendered 是固定 app 服务的最小 Compose JSON。
			rendered := composeConfig{Services: map[string]composeServiceConfig{"app": {
				Image: OfficialImage + ":v1.0.0", Environment: map[string]string{"DATABASE_URL": "sqlite:///app/data/xianyu_data.db"},
				Volumes: []composeVolumeConfig{{Type: "bind", Source: dataDirectory, Target: "/app/data"}},
			}}}
			return json.Marshal(rendered)
		case len(args) > 0 && args[0] == "compose" && hasArg("ps"):
			return []byte("container-1\n"), nil
		case len(args) > 0 && args[0] == "compose" && hasArg("stop"):
			phaseMu.Lock()
			if phase == "old" {
				// writeErr 模拟旧 app 在真正停止前完成的最后一笔持久化写入。
				if writeErr := os.WriteFile(databaseFile, []byte("latest-write"), 0o600); writeErr != nil {
					phaseMu.Unlock()
					return nil, writeErr
				}
			}
			phaseMu.Unlock()
			return nil, nil
		case len(args) > 0 && args[0] == "compose" && hasArg("up"):
			// environmentContent 是 Compose 本次启动读取的镜像引用。
			environmentContent, readErr := os.ReadFile(environmentFile)
			if readErr != nil {
				return nil, readErr
			}
			phaseMu.Lock()
			if strings.Contains(string(environmentContent), manifest.ImageReference()) {
				phase = "target"
				// writeErr 模拟候选版本迁移生产数据库后健康身份校验失败。
				if writeErr := os.WriteFile(databaseFile, []byte("target-mutated"), 0o600); writeErr != nil {
					phaseMu.Unlock()
					return nil, writeErr
				}
			} else {
				phase = "old"
			}
			phaseMu.Unlock()
			return nil, nil
		case len(args) > 0 && args[0] == "inspect" && hasArg("{{.Image}}"):
			return []byte("sha256:" + strings.Repeat("c", 64) + "\n"), nil
		case len(args) > 0 && args[0] == "inspect":
			return []byte("healthy\n"), nil
		case len(args) > 1 && args[0] == "image" && args[1] == "inspect":
			return json.Marshal([]imageInspectResult{{ID: "sha256:" + strings.Repeat("c", 64), RepoDigests: []string{OfficialImage + "@sha256:" + strings.Repeat("d", 64)}}})
		default:
			return nil, nil
		}
	}
	// engine 是使用固定 Release 和模拟宿主机命令的完整更新核心。
	engine := newUpdateEngine(config, runner, fixedReleaseSource{manifest: manifest}, newHTTPHealthClient())
	// result、applyErr 分别是预期成功回滚的事务结果和候选健康失败。
	result, applyErr := engine.ApplyLatestStable(context.Background(), strings.Repeat("1", 32), func(string, string) {})
	if applyErr == nil || !result.rolledBack {
		t.Fatalf("result=%+v err=%v", result, applyErr)
	}
	// restoredDatabase 是回滚后必须包含停止前最后写入的生产数据库。
	restoredDatabase, readErr := os.ReadFile(databaseFile)
	if readErr != nil || string(restoredDatabase) != "latest-write" {
		t.Fatalf("database=%q err=%v", restoredDatabase, readErr)
	}
	// restoredEnvironment 是回滚健康后必须恢复的原始 Compose 配置。
	restoredEnvironment, readErr := os.ReadFile(environmentFile)
	if readErr != nil || string(restoredEnvironment) != string(originalEnvironment) {
		t.Fatalf("environment=%q err=%v", restoredEnvironment, readErr)
	}
}
