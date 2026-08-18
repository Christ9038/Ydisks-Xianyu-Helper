package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// blockingHTTPStopper 模拟不响应关闭的 HTTP transport，用于验证其预算不会吞掉应用 worker 的 Join 时间。
type blockingHTTPStopper struct {
	// calls 记录 Stop 调用次数，确保关闭阶段只调用一次。
	calls int
}

// Stop 等待 HTTP 关闭 Context 到期，模拟无法在预算内排空的请求。
func (stopper *blockingHTTPStopper) Stop(ctx context.Context) error {
	stopper.calls++
	<-ctx.Done()
	return ctx.Err()
}

// observingApplicationCloser 记录应用关闭 Context，并返回预置 worker 收束错误。
type observingApplicationCloser struct {
	// activeContexts 接收 Close 调用瞬间的 Context 活跃状态，避免函数返回后的 defer 取消影响断言。
	activeContexts chan bool
	// closeErr 是模拟未完成 worker 时要保留的聚合错误。
	closeErr error
}

// startupRollbackCloser 记录 HTTP 启动失败后的应用回滚 Context，并返回预置错误。
type startupRollbackCloser struct {
	// closeErr 是回滚阶段要保留的底层组件错误。
	closeErr error
}

// Close 记录回滚请求并返回预置错误，模拟 worker 无法在预算内收束。
func (closer *startupRollbackCloser) Close(context.Context) error {
	return closer.closeErr
}

// Close 记录独立的应用 worker 关闭 Context，并立即返回预置收束错误。
func (closer *observingApplicationCloser) Close(ctx context.Context) error {
	closer.activeContexts <- ctx.Err() == nil
	return closer.closeErr
}

// TestParseOptionsReadsAllOperationalFlags 验证服务入口的命令行参数完整映射。
func TestParseOptionsReadsAllOperationalFlags(t *testing.T) {
	// oldArgs、oldCommandLine 保存测试前的全局命令行状态。
	oldArgs, oldCommandLine := os.Args, flag.CommandLine
	t.Cleanup(func() {
		os.Args = oldArgs
		flag.CommandLine = oldCommandLine
	})
	os.Args = []string{"server", "-db", "custom.db", "-db-url", "sqlite://override.db", "-addr", "127.0.0.1:1", "-web", "web", "-workdir", "data", "-playwright-runtime-root", "runtime", "-playwright-driver-dir", "driver", "-playwright-browser-dir", "browsers", "-data-key-file", "key", "-secure", "-no-browser", "-v", "-log-level", "debug", "-log-format", "json", "-init-admin", "-ensure-admin", "-admin-email", "a@example.com", "-admin-password", "secret", "-service", "-version"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	flag.CommandLine.SetOutput(io.Discard)
	// opts 保存解析后的服务启动选项。
	opts := parseOptions()
	if opts.dbPath != "custom.db" || opts.dbURL != "sqlite://override.db" || opts.addr != "127.0.0.1:1" || opts.webDir != "web" || opts.workDir != "data" || opts.playwrightRuntimeRoot != "runtime" || opts.playwrightDriverDir != "driver" || opts.playwrightBrowserDir != "browsers" || opts.dataKeyFile != "key" {
		t.Fatalf("路径参数解析错误：%+v", opts)
	}
	if !opts.secure || !opts.noBrowser || !opts.verbose || !opts.initAdmin || !opts.ensureAdmin || !opts.service || !opts.showVersion || opts.logLevel != "debug" || opts.logFormat != "json" || opts.adminEmail != "a@example.com" || opts.adminPassword != "secret" {
		t.Fatalf("布尔或日志参数解析错误：%+v", opts)
	}
}

// TestRunPlatformServiceReportsUnsupportedPlatform 验证非 Windows 服务入口给出明确错误。
func TestRunPlatformServiceReportsUnsupportedPlatform(t *testing.T) {
	// err 保存平台服务调用结果。
	err := runPlatformService("test", func(context.Context) error { return nil })
	if err == nil {
		t.Fatal("当前平台应报告不支持服务模式")
	}
}

// TestCloseServerRuntimePreservesIndependentWorkerBudget 验证 HTTP 预算耗尽后，应用 worker 仍获得独立关闭预算并且两类失败都会返回。
func TestCloseServerRuntimePreservesIndependentWorkerBudget(t *testing.T) {
	// httpStopper 模拟 HTTP 排空持续到自身关闭 Context 到期。
	httpStopper := &blockingHTTPStopper{}
	// workerErr 是应用 worker 未完成时用于确认聚合结果的稳定错误。
	workerErr := errors.New("worker join incomplete")
	// applicationCloser 记录应用关闭 Context，验证其不会继承已截止的 HTTP Context。
	applicationCloser := &observingApplicationCloser{activeContexts: make(chan bool, 1), closeErr: workerErr}
	// logger 使用丢弃输出，避免本测试把预期关闭告警写入测试日志。
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// startedAt 记录关闭阶段开始时间，确保 HTTP 预算实际生效且没有无限等待。
	startedAt := time.Now()
	// shutdownErr 是 HTTP 超时和应用 worker 未完成组成的聚合关闭错误。
	shutdownErr := closeServerRuntime(httpStopper, applicationCloser, logger, 20*time.Millisecond, time.Second)
	if !errors.Is(shutdownErr, context.DeadlineExceeded) {
		t.Fatalf("关闭错误未保留 HTTP 截止时间: %v", shutdownErr)
	}
	if !errors.Is(shutdownErr, workerErr) {
		t.Fatalf("关闭错误未保留应用 worker 诊断: %v", shutdownErr)
	}
	if httpStopper.calls != 1 {
		t.Fatalf("HTTP Stop 调用次数=%d，期望=1", httpStopper.calls)
	}
	// workerContextActive 表示应用组件 Close 调用瞬间收到的 Context 是否仍可用。
	workerContextActive := <-applicationCloser.activeContexts
	if !workerContextActive {
		t.Fatal("应用 worker 不应继承已截止的 HTTP Context")
	}
	// elapsed 是本次关闭总耗时，HTTP 预算到期后应立刻继续应用收束而非无限阻塞。
	elapsed := time.Since(startedAt)
	if elapsed < 15*time.Millisecond || elapsed > time.Second {
		t.Fatalf("关闭耗时=%s，未按 HTTP 预算收束", elapsed)
	}
}

// TestWrapShutdownErrorKeepsNilAndCause 验证关闭错误包装既不制造空错误，也保留底层可分类原因。
func TestWrapShutdownErrorKeepsNilAndCause(t *testing.T) {
	// emptyWrappedErr 是空关闭原因的包装结果，应该保持 nil。
	emptyWrappedErr := wrapShutdownError("HTTP 服务关闭", nil)
	if emptyWrappedErr != nil {
		t.Fatalf("空关闭错误不应被包装: %v", emptyWrappedErr)
	}
	// cause 是应用关闭失败的底层可分类错误。
	cause := errors.New("component still running")
	// wrappedErr 是附带稳定关闭阶段名称的错误。
	wrappedErr := wrapShutdownError("应用组件关闭", cause)
	if !errors.Is(wrappedErr, cause) {
		t.Fatalf("包装错误未保留底层原因: %v", wrappedErr)
	}
}

// TestStartupRollbackErrorKeepsWorkerDiagnostic 验证 HTTP 启动失败后的应用回滚错误不会被静默丢弃。
func TestStartupRollbackErrorKeepsWorkerDiagnostic(t *testing.T) {
	// workerErr 是应用组件回滚失败的可分类底层原因。
	workerErr := errors.New("worker rollback incomplete")
	// closer 是返回回滚失败的应用组件关闭替身。
	closer := &startupRollbackCloser{closeErr: workerErr}
	// wrappedErr 是启动失败后统一包装应用回滚阶段的结果。
	wrappedErr := errors.Join(errors.New("HTTP 启动失败"), rollbackApplicationRuntime(closer, time.Second))
	if !errors.Is(wrappedErr, workerErr) {
		t.Fatalf("启动回滚错误未保留 worker 原因: %v", wrappedErr)
	}
}

// TestEnsureAdminIfMissingCreatesOnlyOnce 封装TestEnsureAdminIfMissingCreatesOnlyOnce业务协调。
func TestEnsureAdminIfMissingCreatesOnlyOnce(t *testing.T) {
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// database、dialect、err 用于本次流程后续判断的database、dialect、err
	database, dialect, err := db.Open(ctx, "sqlite://"+filepath.Join(t.TempDir(), "server.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	// store 用于本次流程后续判断的store
	store := db.NewStore(database, dialect)

	// created、err 用于本次流程后续判断的created、err
	created, err := ensureAdminIfMissing(ctx, store, "admin@example.com", "first-password")
	if err != nil || !created {
		t.Fatalf("first ensure: created=%v err=%v", created, err)
	}
	created, err = ensureAdminIfMissing(ctx, store, "admin@example.com", "second-password")
	if err != nil || created {
		t.Fatalf("second ensure: created=%v err=%v", created, err)
	}
	if // ok、err 用于本次流程后续判断的ok、err
	_, ok, err := store.Users.VerifyAndUpgrade(ctx, "admin", "first-password"); err != nil || !ok {
		t.Fatalf("original password should remain valid: ok=%v err=%v", ok, err)
	}
	if // ok、err 用于本次流程后续判断的ok、err
	_, ok, err := store.Users.VerifyAndUpgrade(ctx, "admin", "second-password"); err == nil || ok {
		t.Fatalf("later password must not reset admin: ok=%v err=%v", ok, err)
	}
}

// TestRunServerInitAdminStopsBeforeRuntimeStartup 验证 -init-admin 仅初始化管理员并在 HTTP、浏览器和后台任务启动前退出。
func TestRunServerInitAdminStopsBeforeRuntimeStartup(t *testing.T) {
	// databasePath 是本测试隔离的 SQLite 文件，避免命令入口读取或修改默认数据目录。
	databasePath := filepath.Join(t.TempDir(), "init-admin.db")
	// opts 使用无浏览器启动参数，仅覆盖管理员初始化分支。
	opts := serverOptions{dbPath: databasePath, noBrowser: true, initAdmin: true, adminEmail: "admin@example.com", adminPassword: "initial-password"}
	// runErr 表示管理员初始化命令的执行结果，成功时服务不应继续监听 HTTP 端口。
	runErr := runServer(context.Background(), opts)
	if runErr != nil {
		t.Fatalf("run init-admin server: %v", runErr)
	}
	// verifyDatabase 和 dialect 用于重新打开数据库并验证管理员账户已经被写入。
	verifyDatabase, dialect, openErr := db.Open(context.Background(), "sqlite://"+databasePath)
	if openErr != nil {
		t.Fatalf("open initialized database: %v", openErr)
	}
	t.Cleanup(func() { _ = verifyDatabase.Close() })
	// store 是验证管理员认证结果的仓储集合。
	store := db.NewStore(verifyDatabase, dialect)
	// user、matched、verifyErr 分别是管理员记录、密码验证结果和认证查询失败。
	user, matched, verifyErr := store.Users.VerifyAndUpgrade(context.Background(), "admin", "initial-password")
	if verifyErr != nil || !matched || user == nil {
		t.Fatalf("admin initialization user=%v matched=%t err=%v", user, matched, verifyErr)
	}
}

// TestLoadOrCreateDataKeyPersists 封装TestLoadOrCreate数据KeyPersists业务协调。
func TestLoadOrCreateDataKeyPersists(t *testing.T) {
	// path 用于本次流程后续判断的路径
	path := filepath.Join(t.TempDir(), "data-key")
	// first、err 用于本次流程后续判断的first、err
	first, err := loadOrCreateDataKey(path)
	if err != nil {
		t.Fatalf("create data key: %v", err)
	}
	if first == "" {
		t.Fatal("created data key is empty")
	}
	// second、err 用于本次流程后续判断的second、err
	second, err := loadOrCreateDataKey(path)
	if err != nil {
		t.Fatalf("load data key: %v", err)
	}
	if first != second {
		t.Fatalf("data key changed between loads")
	}
	if // raw、err 用于本次流程后续判断的raw、err
	raw, err := os.ReadFile(path); err != nil || string(raw) == "" {
		t.Fatalf("data key file was not written: err=%v", err)
	}
}

// TestOpenServerLogWriterUsesConfiguredDirectory 封装TestOpenServerLogWriterUsesConfiguredDirectory业务协调。
func TestOpenServerLogWriterUsesConfiguredDirectory(t *testing.T) {
	// logDir 用于本次流程后续判断的logDir
	logDir := t.TempDir()
	t.Setenv("XIANYU_LOG_DIR", logDir)

	// writer、closeLog、err 用于本次流程后续判断的writer、closeLog、err
	writer, closeLog, err := openServerLogWriter("")
	if err != nil {
		t.Fatalf("open log writer: %v", err)
	}
	if // err 用于本次流程后续判断的err
	_, err := io.WriteString(writer, "test log\n"); err != nil {
		t.Fatalf("write log: %v", err)
	}
	closeLog()

	// content、err 用于本次流程后续判断的content、err
	content, err := os.ReadFile(filepath.Join(logDir, "server.log"))
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if string(content) != "test log\n" {
		t.Fatalf("unexpected log content: %q", content)
	}
}

// TestResolveDataDirKeepsExplicitDirectory 封装TestResolve数据DirKeepsExplicitDirectory业务协调。
func TestResolveDataDirKeepsExplicitDirectory(t *testing.T) {
	// explicit 用于本次流程后续判断的explicit
	explicit := filepath.Join(t.TempDir(), "ydisks-data")
	// got、err 用于本次流程后续判断的got、err
	got, err := resolveDataDir(explicit)
	if err != nil {
		t.Fatalf("resolve explicit data directory: %v", err)
	}
	if got != explicit {
		t.Fatalf("explicit data directory changed: got %q want %q", got, explicit)
	}
}

// TestUserDataDirName 封装Test用户数据Dir名称业务协调。
func TestUserDataDirName(t *testing.T) {
	// base 用于本次流程后续判断的base
	base := filepath.Join(t.TempDir(), "Application Support")
	// got 用于本次流程后续判断的got
	got := filepath.Join(base, userDataDirName)
	// want 用于本次流程后续判断的want
	want := filepath.Join(base, "YdisksXianyuHelper")
	if got != want {
		t.Fatalf("unexpected user data directory: got %q want %q", got, want)
	}
}

// TestResolveDBPathUsesDataDirectoryForDefault 封装TestResolveDB路径Uses数据DirectoryForDefault业务协调。
func TestResolveDBPathUsesDataDirectoryForDefault(t *testing.T) {
	// dataDir 用于本次流程后续判断的数据Dir
	dataDir := filepath.Join(t.TempDir(), "YdisksXianyuHelper")
	// got 用于本次流程后续判断的got
	got := resolveDBPath(dataDir, defaultDBPath)
	// want 用于本次流程后续判断的want
	want := filepath.Join(dataDir, "data", "xianyu_data.db")
	if got != want {
		t.Fatalf("unexpected default database path: got %q want %q", got, want)
	}
}

// TestResolveDBPathPreservesCustomPath 封装TestResolveDB路径PreservesCustom路径业务协调。
func TestResolveDBPathPreservesCustomPath(t *testing.T) {
	// dataDir 用于本次流程后续判断的数据Dir
	dataDir := filepath.Join(t.TempDir(), "YdisksXianyuHelper")
	// custom 用于本次流程后续判断的custom
	custom := filepath.Join(t.TempDir(), "custom.db")
	if // got 用于本次流程后续判断的got
	got := resolveDBPath(dataDir, custom); got != custom {
		t.Fatalf("custom database path changed: got %q want %q", got, custom)
	}
}

// TestPlaywrightRuntimeRootUsesProcessArchitecture 封装TestPlaywrightRuntimeRootUsesProcessArchitecture业务协调。
func TestPlaywrightRuntimeRootUsesProcessArchitecture(t *testing.T) {
	// opts 用于本次流程后续判断的opts
	opts := serverOptions{playwrightRuntimeRoot: filepath.Join(t.TempDir(), "playwright-runtime")}
	applyPlaywrightRuntimeRoot(&opts)
	// wantRoot 用于本次流程后续判断的wantRoot
	wantRoot := filepath.Join(opts.playwrightRuntimeRoot, runtime.GOARCH)
	if opts.playwrightDriverDir != filepath.Join(wantRoot, "playwright-driver") {
		t.Fatalf("driver 目录=%q", opts.playwrightDriverDir)
	}
	if opts.playwrightBrowserDir != filepath.Join(wantRoot, "playwright-browsers") {
		t.Fatalf("browser 目录=%q", opts.playwrightBrowserDir)
	}
}
