package updater

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	// rollbackImageRepository 是失败切换后保留旧镜像的本地专用仓库前缀。
	rollbackImageRepository = "ydisks-xianyu-helper-rollback"
	// preflightContainerPrefix 是无网络迁移预检容器的固定名称前缀。
	preflightContainerPrefix = "ydisks-updater-preflight-"
)

var (
	// errUpdateBusy 表示另一进程或本 daemon 已经持有更新事务。
	errUpdateBusy = errors.New("已有更新正在执行")
	// errCustomDeployment 表示当前部署不是可验证的官方稳定镜像。
	errCustomDeployment = errors.New("当前部署不是可自动更新的官方稳定镜像")
	// errNoUpdate 表示当前正式版本已经不低于 GitHub latest。
	errNoUpdate = errors.New("当前已是最新稳定版本")
)

// progressReporter 接收不包含秘密的事务阶段和状态说明。
type progressReporter func(phase, message string)

// updateResult 描述事务失败时是否已完整恢复旧镜像与数据库。
type updateResult struct {
	// rolledBack 表示生产切换失败后旧版本健康恢复成功。
	rolledBack bool
}

// updateEngine 聚合固定部署探测、正式版本检查和更新事务依赖。
type updateEngine struct {
	// config 保存不可由 HTTP 请求覆盖的固定生产路径和服务名。
	config runtimeConfig
	// runner 只执行 updater 内部构造的绝对路径命令。
	runner commandRunner
	// releases 读取并验证 GitHub 最新稳定 Release manifest。
	releases ReleaseSource
	// health 读取固定 app 健康端点。
	health *httpHealthClient
	// now 提供可测试的当前时间。
	now func() time.Time
}

// newUpdateEngine 创建生产 updater 核心，所有可变测试依赖都留在包内构造函数。
func newUpdateEngine(config runtimeConfig, runner commandRunner, releases ReleaseSource, health *httpHealthClient) *updateEngine {
	return &updateEngine{config: config, runner: runner, releases: releases, health: health, now: time.Now}
}

// Current 返回固定生产部署的当前镜像和构建身份。
func (engine *updateEngine) Current(ctx context.Context) (CurrentDeployment, error) {
	return inspectDeployment(ctx, engine.config, engine.runner, engine.health)
}

// Check 同时读取当前部署和 GitHub latest，并给出不执行副作用的更新结论。
func (engine *updateEngine) Check(ctx context.Context) (CheckResult, error) {
	// current 保存固定 Compose app 当前部署快照。
	current, currentErr := engine.Current(ctx)
	if currentErr != nil {
		return CheckResult{}, currentErr
	}
	// latest 保存已验证的 GitHub 最新稳定 manifest。
	latest, latestErr := engine.releases.LatestStable(ctx)
	if latestErr != nil {
		return CheckResult{}, latestErr
	}
	return buildCheckResult(current, latest)
}

// buildCheckResult 将部署类型和语义版本转换为稳定更新决策。
func buildCheckResult(current CurrentDeployment, latest ReleaseManifest) (CheckResult, error) {
	// result 保存默认不可自动更新的完整检查结果。
	result := CheckResult{Current: current, Latest: latest}
	// currentVersion 是去除可选 v 前缀后的当前构建版本。
	currentVersion := strings.TrimPrefix(strings.TrimSpace(current.Build.Version), "v")
	// comparison 保存当前版本相对 latest 的语义版本顺序。
	comparison := -1
	// compareErr 表示当前构建版本不能作为正式版本比较。
	var compareErr error
	if stableVersionPattern.MatchString(currentVersion) {
		comparison, compareErr = compareStableVersions(currentVersion, latest.Version)
	}
	result.Available = compareErr != nil || comparison < 0 || !healthMatchesRelease(current.Build, latest)
	switch current.Kind {
	case DeploymentCustom:
		result.BlockedReason = "custom_image"
		return result, nil
	case DeploymentDevelopment:
		result.BlockedReason = "development_image"
		return result, nil
	case DeploymentUnknown:
		result.BlockedReason = "unknown_image"
		return result, nil
	}
	if current.DatabaseKind != "sqlite" {
		result.BlockedReason = "unsupported_database"
		return result, nil
	}
	if !current.DataMountSupported {
		result.BlockedReason = "unsupported_storage"
		return result, nil
	}
	if current.ContainerHealth != "healthy" {
		result.BlockedReason = "current_unhealthy"
		return result, nil
	}
	switch current.Kind {
	case DeploymentStable, DeploymentStableUnpinned:
		if compareErr != nil {
			result.BlockedReason = "invalid_current_version"
			return result, nil
		}
		if comparison > 0 {
			result.BlockedReason = "current_version_newer"
			result.Available = false
			return result, nil
		}
		if comparison == 0 && healthMatchesRelease(current.Build, latest) {
			result.BlockedReason = "already_latest"
			result.Available = false
			return result, nil
		}
		result.CanUpdate = true
		return result, nil
	default:
		result.BlockedReason = "unknown_image"
		return result, nil
	}
}

// ApplyLatestStable 在跨进程文件锁内重新检查版本，备份、预检、切换并在失败时回滚。
func (engine *updateEngine) ApplyLatestStable(ctx context.Context, requestID string, report progressReporter) (updateResult, error) {
	// transactionContext 限制完整更新事务最长运行时间。
	transactionContext, cancel := context.WithTimeout(ctx, engine.config.updateTimeout)
	defer cancel()
	// lock 保存跨 daemon 进程的排他更新锁文件。
	lock, lockErr := acquireUpdateLock(engine.config.lockFile)
	if lockErr != nil {
		return updateResult{}, lockErr
	}
	defer releaseUpdateLock(lock)
	report("checking", "正在重新确认当前部署和最新稳定版本")
	// check 保存取得文件锁后的最终更新决策，避免排队期间状态变化。
	check, checkErr := engine.Check(transactionContext)
	if checkErr != nil {
		return updateResult{}, checkErr
	}
	if !check.CanUpdate {
		if check.BlockedReason == "already_latest" {
			return updateResult{}, errNoUpdate
		}
		return updateResult{}, fmt.Errorf("%w: %s", errCustomDeployment, check.BlockedReason)
	}
	// backup 保存迁移预检副本、环境文件和 Compose 文件的更新前恢复点。
	report("backup", "正在创建 SQLite 迁移预检副本")
	// backupErr 表示预检恢复点未完整创建，事务必须在切换前终止。
	backup, backupErr := engine.createBackup(transactionContext, requestID)
	if backupErr != nil {
		return updateResult{}, backupErr
	}
	// targetReference 是固定官方仓库与 Release 不可变摘要组成的镜像引用。
	targetReference := check.Latest.ImageReference()
	report("pull", "正在拉取最新稳定镜像")
	// pullErr 表示不可变目标镜像未能拉取到本机。
	if _, pullErr := engine.runner.Run(transactionContext, dockerExecutable, "pull", targetReference); pullErr != nil {
		return updateResult{}, fmt.Errorf("拉取稳定镜像失败: %w", pullErr)
	}
	report("preflight", "正在使用数据库副本执行离线迁移预检")
	// preflightErr 表示候选镜像迁移或核心 CRUD 预检失败。
	if preflightErr := engine.runMigrationPreflight(transactionContext, requestID, targetReference, backup.preflightDatabase); preflightErr != nil {
		return updateResult{}, preflightErr
	}
	// rollbackReference 是指向当前镜像 ID 的本地恢复标签，不依赖远端 mutable tag。
	rollbackReference := rollbackImageRepository + ":" + requestID
	// tagErr 表示旧镜像未能保存为本地恢复标签，不能开始切换。
	if _, tagErr := engine.runner.Run(transactionContext, dockerExecutable, "image", "tag", check.Current.ImageID, rollbackReference); tagErr != nil {
		return updateResult{}, fmt.Errorf("保留旧镜像失败: %w", tagErr)
	}
	report("quiescing", "正在停止 app 并创建最终回滚恢复点")
	// stopErr 表示旧 app 无法在最终备份前停止，生产尚未发生配置切换。
	if _, stopErr := engine.runner.Run(transactionContext, dockerExecutable, composeArgs(engine.config, "stop", engine.config.composeService)...); stopErr != nil {
		return updateResult{}, fmt.Errorf("停止 app 以创建最终恢复点失败: %w", stopErr)
	}
	// rollbackBackupErr 表示停止写入后仍无法生成最终一致性回滚备份。
	if rollbackBackupErr := engine.createStoppedRollbackBackup(transactionContext, &backup); rollbackBackupErr != nil {
		// restartErr 保存恢复原 app 的独立结果，不能让已取消的更新 Context 把生产留在停止状态。
		restartErr := engine.restartOriginalAfterPreparationFailure(check.Current.Build)
		if restartErr != nil {
			return updateResult{}, fmt.Errorf("创建最终回滚恢复点失败且旧 app 重启失败: %v: %w", rollbackBackupErr, restartErr)
		}
		return updateResult{}, fmt.Errorf("创建最终回滚恢复点失败，旧 app 已恢复: %w", rollbackBackupErr)
	}
	report("switching", "正在切换 app 到最新稳定镜像")
	// envErr 表示目标镜像引用未能原子写入固定部署环境文件。
	if envErr := engine.writeImageReference(backup.environmentContent, backup.environmentMode, targetReference); envErr != nil {
		// rollbackErr 保存配置写入失败后的独立恢复结果。
		rollbackErr := engine.rollbackAfterFailure(requestID, rollbackReference, backup, check.Current.Build, report)
		if rollbackErr != nil {
			return updateResult{}, fmt.Errorf("更新镜像配置失败且回滚失败: %v: %w", envErr, rollbackErr)
		}
		return updateResult{rolledBack: true}, fmt.Errorf("更新镜像配置失败，已恢复旧版本: %w", envErr)
	}
	// composeErr 表示 app-only Compose 重建失败，失败时必须回滚。
	if _, composeErr := engine.runner.Run(transactionContext, dockerExecutable, composeArgs(engine.config, "up", "-d", "--no-deps", "--pull", "never", engine.config.composeService)...); composeErr == nil {
		// build 保存新容器健康后上报的目标构建身份。
		if _, healthErr := waitForHealth(transactionContext, engine.config, engine.runner, engine.health, &check.Latest); healthErr == nil {
			return updateResult{}, nil
		} else {
			composeErr = healthErr
		}
		// rollbackErr 保存新容器不健康后的独立超时恢复结果，不复用可能已取消的更新 Context。
		rollbackErr := engine.rollbackAfterFailure(requestID, rollbackReference, backup, check.Current.Build, report)
		if rollbackErr != nil {
			return updateResult{}, fmt.Errorf("新版本健康检查失败且回滚失败: %w", rollbackErr)
		}
		return updateResult{rolledBack: true}, fmt.Errorf("新版本健康检查失败，已恢复旧版本: %w", composeErr)
	} else {
		// rollbackErr 保存 Compose 重建失败后的独立超时恢复结果。
		rollbackErr := engine.rollbackAfterFailure(requestID, rollbackReference, backup, check.Current.Build, report)
		if rollbackErr != nil {
			return updateResult{}, fmt.Errorf("重建 app 失败且回滚失败: %w", rollbackErr)
		}
		return updateResult{rolledBack: true}, fmt.Errorf("重建 app 失败，已恢复旧版本: %w", composeErr)
	}
}

// rollbackAfterFailure 使用独立 Context 恢复生产，确保 daemon 停止信号不会令已激活事务跳过回滚。
func (engine *updateEngine) rollbackAfterFailure(requestID, rollbackReference string, backup backupSet, expected BuildInfo, report progressReporter) error {
	// rollbackContext 为停止失败容器、恢复文件、重建旧容器和健康检查保留独立预算。
	rollbackContext, rollbackCancel := context.WithTimeout(context.Background(), engine.config.healthTimeout+2*time.Minute)
	defer rollbackCancel()
	return engine.rollback(rollbackContext, requestID, rollbackReference, backup, expected, report)
}

// backupSet 保存更新前恢复点位置及原始环境文件内容，秘密不得离开事务作用域。
type backupSet struct {
	// directory 是本次更新专用恢复点目录。
	directory string
	// preflightDatabase 是 app 运行期间创建、仅供候选镜像迁移预检的数据库副本。
	preflightDatabase string
	// rollbackDatabase 是停止 app 后创建、允许写回生产的最终一致性恢复点。
	rollbackDatabase string
	// environmentContent 是更新前 .env 原始字节，仅用于切换或回滚。
	environmentContent []byte
	// environmentMode 是敏感 .env 原有权限。
	environmentMode os.FileMode
	// databaseMode 是生产数据库原有权限。
	databaseMode os.FileMode
	// databaseUID 和 databaseGID 是生产数据库原有宿主机属主。
	databaseUID int
	// databaseGID 是生产数据库原有宿主机属组。
	databaseGID int
}

// createBackup 使用 SQLite 在线备份 API 创建一致性副本，并备份固定配置文件。
func (engine *updateEngine) createBackup(ctx context.Context, requestID string) (backupSet, error) {
	// directory 是只由 daemon 随机 requestID 派生的恢复点目录。
	directory := filepath.Join(engine.config.backupRoot, requestID)
	// mkdirErr 表示恢复点目录无法创建，后续不得触碰生产配置。
	if mkdirErr := os.MkdirAll(directory, 0o700); mkdirErr != nil {
		return backupSet{}, fmt.Errorf("创建恢复点目录失败: %w", mkdirErr)
	}
	// environmentContent 和 environmentMode 保存更新前敏感环境文件及权限。
	environmentContent, environmentMode, environmentErr := readEnvironmentFile(engine.config.envFile)
	if environmentErr != nil {
		return backupSet{}, fmt.Errorf("读取环境配置失败: %w", environmentErr)
	}
	// databaseInfo 保存生产 SQLite 权限和属主，回滚时必须恢复。
	databaseInfo, databaseStatErr := os.Stat(engine.config.databaseFile)
	if databaseStatErr != nil {
		return backupSet{}, fmt.Errorf("读取 SQLite 元数据失败: %w", databaseStatErr)
	}
	// databaseStat 保存 Unix UID/GID；非 Unix 元数据视为不受支持。
	databaseStat, statOK := databaseInfo.Sys().(*syscall.Stat_t)
	if !statOK {
		return backupSet{}, errors.New("无法读取 SQLite Unix 属主信息")
	}
	// databaseBackup 是 sqlite3 .backup 命令生成的迁移预检副本。
	databaseBackup := filepath.Join(directory, "preflight-xianyu_data.db")
	// backupCommand 是路径完全由固定目录和随机十六进制 ID 组成的 sqlite3 元命令。
	backupCommand := ".backup '" + databaseBackup + "'"
	// backupErr 表示 SQLite 在线备份未能生成一致性副本。
	if _, backupErr := engine.runner.Run(ctx, sqliteExecutable, engine.config.databaseFile, backupCommand); backupErr != nil {
		return backupSet{}, fmt.Errorf("SQLite 在线备份失败: %w", backupErr)
	}
	// integrityErr 表示恢复点未通过 SQLite 完整性或外键检查。
	if integrityErr := engine.validateSQLite(ctx, databaseBackup); integrityErr != nil {
		return backupSet{}, integrityErr
	}
	// writeErr 表示敏感环境配置恢复点未能持久化。
	if writeErr := os.WriteFile(filepath.Join(directory, "deployment.env"), environmentContent, 0o600); writeErr != nil {
		return backupSet{}, fmt.Errorf("备份环境配置失败: %w", writeErr)
	}
	// copyErr 表示 Compose 配置恢复点未能复制完成。
	if copyErr := copyFile(engine.config.composeFile, filepath.Join(directory, "compose.yml"), 0o600); copyErr != nil {
		return backupSet{}, fmt.Errorf("备份 Compose 配置失败: %w", copyErr)
	}
	return backupSet{
		directory:          directory,
		preflightDatabase:  databaseBackup,
		environmentContent: environmentContent,
		environmentMode:    environmentMode,
		databaseMode:       databaseInfo.Mode().Perm(),
		databaseUID:        int(databaseStat.Uid),
		databaseGID:        int(databaseStat.Gid),
	}, nil
}

// createStoppedRollbackBackup 在 app 已停止写入后创建最终回滚数据库，不复用较早的迁移预检副本。
func (engine *updateEngine) createStoppedRollbackBackup(ctx context.Context, backup *backupSet) error {
	if backup == nil || backup.directory == "" {
		return errors.New("最终回滚恢复点未初始化")
	}
	// rollbackDatabase 是停止 app 后生成的最终一致性 SQLite 副本。
	rollbackDatabase := filepath.Join(backup.directory, "rollback-xianyu_data.db")
	// backupCommand 只包含固定恢复点目录派生的 SQLite 元命令。
	backupCommand := ".backup '" + rollbackDatabase + "'"
	// backupErr 表示最终回滚副本未能创建。
	if _, backupErr := engine.runner.Run(ctx, sqliteExecutable, engine.config.databaseFile, backupCommand); backupErr != nil {
		return fmt.Errorf("创建停止写入后的 SQLite 回滚备份失败: %w", backupErr)
	}
	// integrityErr 表示最终回滚副本未通过 SQLite 完整性检查。
	if integrityErr := engine.validateSQLite(ctx, rollbackDatabase); integrityErr != nil {
		return fmt.Errorf("验证最终 SQLite 回滚备份失败: %w", integrityErr)
	}
	backup.rollbackDatabase = rollbackDatabase
	return nil
}

// validateSQLite 要求恢复点 integrity_check 为 ok 且 foreign_key_check 没有异常行。
func (engine *updateEngine) validateSQLite(ctx context.Context, databasePath string) error {
	// output 保存两个只读 PRAGMA 的逐行结果；正常结果只能是 integrity_check 的 ok。
	output, validationErr := engine.runner.Run(ctx, sqliteExecutable, "-readonly", databasePath, "PRAGMA integrity_check; PRAGMA foreign_key_check;")
	if validationErr != nil {
		return fmt.Errorf("验证 SQLite 恢复点失败: %w", validationErr)
	}
	if strings.TrimSpace(string(output)) != "ok" {
		return errors.New("SQLite 恢复点完整性或外键检查失败")
	}
	return nil
}

// runMigrationPreflight 在禁网临时容器中对生产备份副本执行候选镜像迁移和核心 CRUD 验证。
func (engine *updateEngine) runMigrationPreflight(ctx context.Context, requestID, imageReference, databaseBackup string) error {
	// preflightData 是候选容器唯一可写的临时数据目录。
	preflightData := filepath.Join(filepath.Dir(databaseBackup), "preflight-data")
	// mkdirErr 表示迁移预检工作目录无法创建。
	if mkdirErr := os.MkdirAll(preflightData, 0o700); mkdirErr != nil {
		return fmt.Errorf("创建迁移预检目录失败: %w", mkdirErr)
	}
	// preflightDatabase 是候选迁移可修改的数据库副本。
	preflightDatabase := filepath.Join(preflightData, "xianyu_data.db")
	// copyErr 表示迁移预检数据库副本无法准备。
	if copyErr := copyFile(databaseBackup, preflightDatabase, 0o600); copyErr != nil {
		return fmt.Errorf("准备迁移预检数据库失败: %w", copyErr)
	}
	// containerName 是由随机十六进制 requestID 派生的固定前缀临时容器名。
	containerName := preflightContainerPrefix + requestID
	// volumeMount 只将本次恢复点副本挂载给候选容器，不暴露生产目录或 Docker Socket。
	volumeMount := preflightData + ":/app/data:rw"
	// runErr 表示禁网候选容器未能完成迁移和核心 CRUD 验证。
	if _, runErr := engine.runner.Run(ctx, dockerExecutable,
		"run", "--rm", "--network", "none", "--name", containerName,
		"--entrypoint", "/app/xianyu-dbverify", "-v", volumeMount,
		imageReference, "sqlite:///app/data/xianyu_data.db"); runErr != nil {
		return fmt.Errorf("候选镜像迁移预检失败: %w", runErr)
	}
	// integrityErr 表示迁移后的数据库副本未通过完整性检查。
	if integrityErr := engine.validateSQLite(ctx, preflightDatabase); integrityErr != nil {
		return fmt.Errorf("候选迁移后数据库验证失败: %w", integrityErr)
	}
	return nil
}

// writeImageReference 将固定 .env 的 XIANYU_IMAGE 原子改为受验证的内部镜像引用。
func (engine *updateEngine) writeImageReference(original []byte, mode os.FileMode, imageReference string) error {
	// updated 保存仅替换 XIANYU_IMAGE 后的完整环境文件字节。
	updated, replacementErr := replaceEnvironmentImage(original, imageReference)
	if replacementErr != nil {
		return replacementErr
	}
	// writeErr 表示目标环境文件未能以原权限原子替换。
	if writeErr := writeFileAtomic(engine.config.envFile, updated, mode); writeErr != nil {
		return fmt.Errorf("更新镜像配置失败: %w", writeErr)
	}
	return nil
}

// restartOriginalAfterPreparationFailure 在配置尚未切换时用独立 Context 恢复刚停止的旧 app。
func (engine *updateEngine) restartOriginalAfterPreparationFailure(expected BuildInfo) error {
	// restartContext 为旧 app 重建和健康确认保留不受更新取消影响的独立预算。
	restartContext, restartCancel := context.WithTimeout(context.Background(), engine.config.healthTimeout+2*time.Minute)
	defer restartCancel()
	// composeErr 表示原配置下的 app 未能重新启动。
	if _, composeErr := engine.runner.Run(restartContext, dockerExecutable, composeArgs(engine.config, "up", "-d", "--no-deps", "--pull", "never", engine.config.composeService)...); composeErr != nil {
		return fmt.Errorf("重启旧 app 失败: %w", composeErr)
	}
	// restoredBuild 是重新启动后的旧应用构建身份。
	restoredBuild, healthErr := waitForHealth(restartContext, engine.config, engine.runner, engine.health, nil)
	if healthErr != nil {
		return fmt.Errorf("重启旧 app 后健康检查失败: %w", healthErr)
	}
	if restoredBuild.Version != expected.Version || restoredBuild.Commit != expected.Commit {
		return errors.New("重启后的旧 app 构建身份与停止前不一致")
	}
	return nil
}

// rollback 停止失败 app、保存失败现场、恢复数据库并使用本地旧镜像标签重建。
func (engine *updateEngine) rollback(ctx context.Context, requestID, rollbackReference string, backup backupSet, expected BuildInfo, report progressReporter) error {
	report("rollback", "新版本未通过验证，正在恢复旧镜像和数据库")
	// stopErr 保存停止固定 app 服务的结果，失败时仍继续保存现场和恢复数据库。
	_, stopErr := engine.runner.Run(ctx, dockerExecutable, composeArgs(engine.config, "stop", engine.config.composeService)...)
	if stopErr != nil {
		return fmt.Errorf("停止失败版本 app 失败: %w", stopErr)
	}
	// preserveErr 表示失败版本数据库现场未能完整保留。
	if preserveErr := preserveFailedDatabase(engine.config.databaseFile, filepath.Join(backup.directory, "failed-runtime")); preserveErr != nil {
		return fmt.Errorf("保存失败数据库现场失败: %w", preserveErr)
	}
	if backup.rollbackDatabase == "" {
		return errors.New("缺少停止写入后的最终 SQLite 回滚备份")
	}
	// restoreErr 表示停止写入后的最终 SQLite 恢复点无法写回生产路径。
	if restoreErr := copyFile(backup.rollbackDatabase, engine.config.databaseFile, backup.databaseMode); restoreErr != nil {
		return fmt.Errorf("恢复 SQLite 数据库失败: %w", restoreErr)
	}
	// chownErr 表示恢复数据库后无法恢复原宿主机属主。
	if chownErr := os.Chown(engine.config.databaseFile, backup.databaseUID, backup.databaseGID); chownErr != nil {
		return fmt.Errorf("恢复 SQLite 属主失败: %w", chownErr)
	}
	// envErr 表示旧镜像恢复引用未能写回固定环境文件。
	if envErr := engine.writeImageReference(backup.environmentContent, backup.environmentMode, rollbackReference); envErr != nil {
		return envErr
	}
	// composeErr 表示旧镜像 app-only Compose 重建失败。
	if _, composeErr := engine.runner.Run(ctx, dockerExecutable, composeArgs(engine.config, "up", "-d", "--no-deps", "--pull", "never", engine.config.composeService)...); composeErr != nil {
		return fmt.Errorf("重建旧版本 app 失败: %w", composeErr)
	}
	// restoredBuild 保存回滚后健康应用的构建身份。
	restoredBuild, healthErr := waitForHealth(ctx, engine.config, engine.runner, engine.health, nil)
	if healthErr != nil {
		return fmt.Errorf("旧版本健康检查失败: %w", healthErr)
	}
	if restoredBuild.Version != expected.Version || restoredBuild.Commit != expected.Commit {
		return errors.New("回滚后的构建身份与更新前不一致")
	}
	// envRestoreErr 表示旧容器健康后无法把 Compose 配置恢复为更新前的原始镜像引用。
	if envRestoreErr := writeFileAtomic(engine.config.envFile, backup.environmentContent, backup.environmentMode); envRestoreErr != nil {
		return fmt.Errorf("恢复更新前环境配置失败: %w", envRestoreErr)
	}
	return nil
}

// preserveFailedDatabase 将失败版本可能迁移过的 SQLite 主文件、WAL 和 SHM 移入恢复点。
func preserveFailedDatabase(databasePath, destinationDirectory string) error {
	// mkdirErr 表示失败现场目录无法创建，不能继续移动任何数据库文件。
	if mkdirErr := os.MkdirAll(destinationDirectory, 0o700); mkdirErr != nil {
		return mkdirErr
	}
	// suffix 是当前保存的 SQLite 主文件或日志文件后缀。
	for _, suffix := range []string{"", "-wal", "-shm"} {
		// source 是生产 SQLite 当前文件路径。
		source := databasePath + suffix
		// info 用于区分文件不存在和真实读取错误。
		_, statErr := os.Stat(source)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return statErr
		}
		// destination 是恢复点内保留原文件名的失败现场路径。
		destination := filepath.Join(destinationDirectory, filepath.Base(source))
		// renameErr 表示失败数据库现场无法移入受限恢复点目录。
		if renameErr := os.Rename(source, destination); renameErr != nil {
			return renameErr
		}
	}
	return nil
}

// copyFile 以指定权限复制普通文件，目标内容会先截断并在返回前同步。
func copyFile(sourcePath, destinationPath string, mode os.FileMode) error {
	// source 是仅读取的源文件。
	source, sourceErr := os.Open(sourcePath)
	if sourceErr != nil {
		return sourceErr
	}
	defer source.Close()
	// destination 是按调用方权限创建或截断的目标文件。
	destination, destinationErr := os.OpenFile(destinationPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if destinationErr != nil {
		return destinationErr
	}
	// closed 表示目标文件是否已由成功路径关闭。
	closed := false
	defer func() {
		if !closed {
			_ = destination.Close()
		}
	}()
	// copyErr 表示普通文件内容未能完整复制到恢复点。
	if _, copyErr := io.Copy(destination, source); copyErr != nil {
		return copyErr
	}
	// syncErr 表示恢复点文件内容未能同步到底层存储。
	if syncErr := destination.Sync(); syncErr != nil {
		return syncErr
	}
	// closeErr 表示恢复点文件关闭失败，不能确认副本已完成。
	if closeErr := destination.Close(); closeErr != nil {
		return closeErr
	}
	closed = true
	return nil
}

// acquireUpdateLock 创建固定锁目录并以非阻塞 flock 阻止并发更新进程。
func acquireUpdateLock(lockPath string) (*os.File, error) {
	// mkdirErr 表示更新锁目录无法创建。
	if mkdirErr := os.MkdirAll(filepath.Dir(lockPath), 0o750); mkdirErr != nil {
		return nil, fmt.Errorf("创建更新锁目录失败: %w", mkdirErr)
	}
	// lock 是 daemon 持有到事务结束的文件描述符。
	lock, openErr := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o640)
	if openErr != nil {
		return nil, fmt.Errorf("打开更新锁失败: %w", openErr)
	}
	// flockErr 表示更新锁被占用或系统拒绝加锁。
	if flockErr := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); flockErr != nil {
		_ = lock.Close()
		if errors.Is(flockErr, unix.EWOULDBLOCK) {
			return nil, errUpdateBusy
		}
		return nil, fmt.Errorf("取得更新锁失败: %w", flockErr)
	}
	return lock, nil
}

// releaseUpdateLock 解锁并关闭事务文件描述符；释放失败不覆盖主事务结果。
func releaseUpdateLock(lock *os.File) {
	if lock == nil {
		return
	}
	_ = unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	_ = lock.Close()
}

// newRequestID 生成只含十六进制字符的不可预测操作标识，可安全用于固定目录和容器后缀。
func newRequestID() (string, error) {
	// randomBytes 保存 128 位随机操作标识原始字节。
	var randomBytes [16]byte
	// readErr 表示系统随机源不可用，不能生成安全操作标识。
	if _, readErr := rand.Read(randomBytes[:]); readErr != nil {
		return "", readErr
	}
	return hex.EncodeToString(randomBytes[:]), nil
}
