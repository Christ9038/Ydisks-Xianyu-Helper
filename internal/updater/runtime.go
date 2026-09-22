package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	// dockerExecutable 是 updater 唯一允许调用的 Docker CLI 绝对路径。
	dockerExecutable = "/usr/bin/docker"
	// sqliteExecutable 是 updater 执行在线备份与完整性检查的固定 SQLite CLI。
	sqliteExecutable = "/usr/bin/sqlite3"
)

// runtimeConfig 保存生产固定路径；字段不从 HTTP 请求、环境变量或命令行参数读取。
type runtimeConfig struct {
	// projectDir 是固定 Compose 项目目录。
	projectDir string
	// composeFile 是固定 Compose 配置文件。
	composeFile string
	// envFile 是固定部署环境文件，更新只允许修改 XIANYU_IMAGE。
	envFile string
	// databaseFile 是 SQLite 生产数据库固定路径。
	databaseFile string
	// backupRoot 是 updater 专用恢复点根目录。
	backupRoot string
	// lockFile 是跨 daemon 进程排他更新锁文件。
	lockFile string
	// socketPath 是 Unix HTTP 服务监听路径。
	socketPath string
	// healthURL 是固定 app 本机健康检查地址。
	healthURL string
	// composeService 是唯一允许重建的 Compose 服务名。
	composeService string
	// composeProject 是用于定位固定生产容器的 Compose 项目名。
	composeProject string
	// updateTimeout 限制完整更新事务执行时间。
	updateTimeout time.Duration
	// healthTimeout 限制每次切换后的健康等待时间。
	healthTimeout time.Duration
	// healthInterval 是健康检查轮询间隔。
	healthInterval time.Duration
}

// productionConfig 返回不可由外部输入覆盖的正式部署配置。
func productionConfig() runtimeConfig {
	return runtimeConfig{
		projectDir:     "/opt/ydisks-xianyu-helper",
		composeFile:    "/opt/ydisks-xianyu-helper/compose.yml",
		envFile:        "/opt/ydisks-xianyu-helper/.env",
		databaseFile:   "/opt/ydisks-xianyu-helper/data/xianyu_data.db",
		backupRoot:     "/opt/ydisks-xianyu-helper/backups/updater",
		lockFile:       "/run/ydisks-xianyu-helper/update.lock",
		socketPath:     DefaultSocketPath,
		healthURL:      "http://127.0.0.1:59188/health",
		composeService: "app",
		composeProject: "ydisks-xianyu-helper",
		updateTimeout:  10 * time.Minute,
		healthTimeout:  3 * time.Minute,
		healthInterval: 2 * time.Second,
	}
}

// commandRunner 定义 updater 运行固定宿主机命令所需的最小同步能力。
type commandRunner interface {
	// Run 执行由 updater 内部构造的固定程序和参数，返回标准输出；错误不携带命令输出。
	Run(context.Context, string, ...string) ([]byte, error)
}

// osCommandRunner 使用绝对程序路径执行生产命令，不通过 shell 解释参数。
type osCommandRunner struct{}

// Run 执行 executable 及 args；ctx 取消时终止子进程，返回值不会包含可能敏感的标准错误输出。
func (osCommandRunner) Run(ctx context.Context, executable string, args ...string) ([]byte, error) {
	// command 是不经过 shell 的固定宿主机子进程。
	command := exec.CommandContext(ctx, executable, args...)
	// output 仅保存成功命令的标准输出；stderr 不进入 API 或状态消息。
	output, commandErr := command.Output()
	if commandErr != nil {
		return nil, fmt.Errorf("%s 执行失败: %w", filepath.Base(executable), commandErr)
	}
	return output, nil
}

// composeArgs 为固定 Compose 文件、项目目录和项目名生成公共参数前缀。
func composeArgs(config runtimeConfig, operationArgs ...string) []string {
	// args 保存不会被外部请求修改的 Compose 项目边界和具体操作。
	args := []string{"compose", "--project-name", config.composeProject, "--project-directory", config.projectDir, "-f", config.composeFile}
	return append(args, operationArgs...)
}

// composeConfig 是 docker compose config JSON 中只读取 app 镜像所需的最小结构。
type composeConfig struct {
	// Services 保存按固定服务名索引的渲染结果。
	Services map[string]composeServiceConfig `json:"services"`
}

// composeServiceConfig 保存 Compose 服务渲染后的镜像引用。
type composeServiceConfig struct {
	// Image 是解析 .env 后的完整镜像引用。
	Image string `json:"image"`
	// Environment 是 Compose 已解析的应用环境变量；只读取 DATABASE_URL 协议部分。
	Environment map[string]string `json:"environment"`
	// Volumes 是应用容器已解析的数据挂载，不读取挂载目录内文件。
	Volumes []composeVolumeConfig `json:"volumes"`
}

// composeVolumeConfig 是 Compose JSON 中识别 SQLite 固定绑定目录所需的最小字段。
type composeVolumeConfig struct {
	// Type 是 bind、volume 等 Compose 挂载类型。
	Type string `json:"type"`
	// Source 是宿主机绑定目录或命名卷名称。
	Source string `json:"source"`
	// Target 是容器内挂载目标。
	Target string `json:"target"`
}

// imageInspectResult 是 docker image inspect 输出的最小镜像来源信息。
type imageInspectResult struct {
	// ID 是本地镜像内容标识。
	ID string `json:"Id"`
	// RepoDigests 是镜像已知的远端仓库摘要。
	RepoDigests []string `json:"RepoDigests"`
}

// inspectDeployment 使用固定 Compose 项目、容器和健康地址构建当前部署快照。
func inspectDeployment(ctx context.Context, config runtimeConfig, runner commandRunner, healthClient *httpHealthClient) (CurrentDeployment, error) {
	// renderedJSON 保存固定 Compose 文件解析后的 JSON。
	renderedJSON, configErr := runner.Run(ctx, dockerExecutable, composeArgs(config, "config", "--format", "json")...)
	if configErr != nil {
		return CurrentDeployment{}, fmt.Errorf("读取 Compose 配置失败: %w", configErr)
	}
	// rendered 保存只包含服务镜像的 Compose 配置模型。
	var rendered composeConfig
	// decodeErr 表示 Docker Compose 输出不是预期 JSON 结构。
	if decodeErr := json.Unmarshal(renderedJSON, &rendered); decodeErr != nil {
		return CurrentDeployment{}, fmt.Errorf("解析 Compose 配置失败: %w", decodeErr)
	}
	// appConfig 保存固定 app 服务的渲染配置。
	appConfig, appExists := rendered.Services[config.composeService]
	if !appExists || strings.TrimSpace(appConfig.Image) == "" {
		return CurrentDeployment{}, errors.New("compose 缺少固定 app 镜像")
	}
	// containerOutput 保存固定 app 服务当前容器 ID。
	containerOutput, containerErr := runner.Run(ctx, dockerExecutable, composeArgs(config, "ps", "-q", config.composeService)...)
	if containerErr != nil {
		return CurrentDeployment{}, fmt.Errorf("读取 app 容器失败: %w", containerErr)
	}
	// containerID 是当前 app 容器的 Docker 标识。
	containerID := strings.TrimSpace(string(containerOutput))
	if containerID == "" || strings.Contains(containerID, "\n") {
		return CurrentDeployment{}, errors.New("固定 app 服务没有唯一运行容器")
	}
	// imageIDOutput 保存容器实际运行镜像的内容 ID。
	imageIDOutput, imageIDErr := runner.Run(ctx, dockerExecutable, "inspect", "--format", "{{.Image}}", containerID)
	if imageIDErr != nil {
		return CurrentDeployment{}, fmt.Errorf("读取 app 镜像 ID 失败: %w", imageIDErr)
	}
	// imageID 是去除空白后的本地镜像 ID。
	imageID := strings.TrimSpace(string(imageIDOutput))
	// healthOutput 保存 Docker 容器健康或运行状态。
	healthOutput, dockerHealthErr := runner.Run(ctx, dockerExecutable, "inspect", "--format", "{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}", containerID)
	if dockerHealthErr != nil {
		return CurrentDeployment{}, fmt.Errorf("读取 app 容器健康状态失败: %w", dockerHealthErr)
	}
	// inspectJSON 保存当前镜像的仓库摘要元数据。
	inspectJSON, imageInspectErr := runner.Run(ctx, dockerExecutable, "image", "inspect", imageID)
	if imageInspectErr != nil {
		return CurrentDeployment{}, fmt.Errorf("读取 app 镜像来源失败: %w", imageInspectErr)
	}
	// inspectedImages 保存 Docker 数组形状的镜像检查结果。
	var inspectedImages []imageInspectResult
	// decodeErr 表示 Docker 镜像检查输出无法解析或不是唯一镜像。
	if decodeErr := json.Unmarshal(inspectJSON, &inspectedImages); decodeErr != nil || len(inspectedImages) != 1 {
		return CurrentDeployment{}, errors.New("解析 app 镜像来源失败")
	}
	// build 保存应用健康接口返回的版本与数据库状态。
	build, buildErr := healthClient.Read(ctx, config.healthURL)
	if buildErr != nil {
		return CurrentDeployment{}, fmt.Errorf("读取 app 健康信息失败: %w", buildErr)
	}
	// deployment 汇总镜像、Docker 健康和应用构建标识。
	deployment := CurrentDeployment{
		ImageReference:     appConfig.Image,
		ImageID:            imageID,
		RepositoryDigests:  append([]string(nil), inspectedImages[0].RepoDigests...),
		ContainerHealth:    strings.TrimSpace(string(healthOutput)),
		Build:              build,
		DatabaseKind:       databaseKind(appConfig.Environment["DATABASE_URL"]),
		DataMountSupported: supportsFixedSQLiteMount(appConfig.Volumes, filepath.Dir(config.databaseFile)),
	}
	deployment.Kind = classifyDeployment(deployment)
	return deployment, nil
}

// databaseKind 只解析 DATABASE_URL 的协议名称，不保留或返回连接凭据。
func databaseKind(databaseURL string) string {
	// normalized 是仅保留协议判断所需的规范化连接类型，不包含凭据。
	normalized := strings.ToLower(strings.TrimSpace(databaseURL))
	switch {
	case strings.HasPrefix(normalized, "sqlite://"):
		return "sqlite"
	case strings.HasPrefix(normalized, "mysql://"):
		return "mysql"
	case strings.HasPrefix(normalized, "postgres://"), strings.HasPrefix(normalized, "postgresql://"):
		return "postgres"
	default:
		return "unknown"
	}
}

// supportsFixedSQLiteMount 验证 `/app/data` 来自 updater 固定生产数据目录的 bind 挂载。
func supportsFixedSQLiteMount(volumes []composeVolumeConfig, expectedSource string) bool {
	// volume 是当前检查的 Compose app 挂载。
	for _, volume := range volumes {
		if volume.Type != "bind" || filepath.Clean(volume.Target) != "/app/data" {
			continue
		}
		// sourceAbs 是规范化后的宿主机绑定目录，必须与固定数据目录完全一致。
		sourceAbs, sourceErr := filepath.Abs(volume.Source)
		if sourceErr == nil && filepath.Clean(sourceAbs) == filepath.Clean(expectedSource) {
			return true
		}
	}
	return false
}

// classifyDeployment 根据官方仓库引用、开发标签和健康版本判断自动更新通道。
func classifyDeployment(deployment CurrentDeployment) DeploymentKind {
	// imageReference 是去除首尾空白的 Compose 镜像引用。
	imageReference := strings.TrimSpace(deployment.ImageReference)
	if !strings.HasPrefix(imageReference, OfficialImage+":") && !strings.HasPrefix(imageReference, OfficialImage+"@") {
		return DeploymentCustom
	}
	// version 是去除可选 v 前缀后的应用构建版本。
	version := strings.TrimPrefix(strings.TrimSpace(deployment.Build.Version), "v")
	// suffix 是官方仓库名之后的标签或摘要部分。
	suffix := strings.TrimPrefix(imageReference, OfficialImage)
	if suffix == ":dev" || suffix == ":main" || strings.HasPrefix(suffix, ":sha-") {
		return DeploymentDevelopment
	}
	if !stableVersionPattern.MatchString(version) {
		return DeploymentDevelopment
	}
	if !hasOfficialRepositoryDigest(deployment.RepositoryDigests) {
		return DeploymentUnknown
	}
	if suffix == ":latest" {
		return DeploymentStableUnpinned
	}
	if strings.HasPrefix(suffix, "@sha256:") || suffix == ":"+version || suffix == ":v"+version {
		return DeploymentStable
	}
	return DeploymentUnknown
}

// hasOfficialRepositoryDigest 要求运行镜像至少保留一个固定官方仓库的不可变拉取摘要。
func hasOfficialRepositoryDigest(repositoryDigests []string) bool {
	// repositoryDigest 是 Docker 为当前运行镜像记录的单个仓库摘要。
	for _, repositoryDigest := range repositoryDigests {
		// normalized 是去除首尾空白后的仓库摘要。
		normalized := strings.TrimSpace(repositoryDigest)
		if strings.HasPrefix(normalized, OfficialImage+"@") && digestPattern.MatchString(strings.TrimPrefix(normalized, OfficialImage+"@")) {
			return true
		}
	}
	return false
}

// readEnvironmentFile 读取固定 .env 并保留原始字节和权限，值不得写入日志或 API。
func readEnvironmentFile(path string) ([]byte, os.FileMode, error) {
	// info 保存环境文件权限，更新时必须原样保留敏感文件访问边界。
	info, statErr := os.Stat(path)
	if statErr != nil {
		return nil, 0, statErr
	}
	// content 保存环境文件原始字节，仅在更新事务的最小作用域内存在。
	content, readErr := os.ReadFile(path)
	if readErr != nil {
		return nil, 0, readErr
	}
	return content, info.Mode().Perm(), nil
}

// replaceEnvironmentImage 只替换或追加唯一 XIANYU_IMAGE，重复声明因语义歧义而拒绝更新。
func replaceEnvironmentImage(content []byte, imageReference string) ([]byte, error) {
	// lines 保存原环境文件按换行拆分的各行。
	lines := strings.Split(string(content), "\n")
	// replaced 表示是否已经替换现有 XIANYU_IMAGE 行。
	replaced := false
	// declarations 统计有效 XIANYU_IMAGE 声明，重复配置必须 fail closed。
	declarations := 0
	// lineIndex 是当前环境文件行下标；line 是不记录到日志的原始行内容。
	for lineIndex, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "XIANYU_IMAGE=") {
			declarations++
			if declarations > 1 {
				return nil, errors.New("环境配置包含重复 XIANYU_IMAGE 声明")
			}
			lines[lineIndex] = "XIANYU_IMAGE=" + imageReference
			replaced = true
		}
	}
	if !replaced {
		if len(lines) > 0 && lines[len(lines)-1] != "" {
			lines = append(lines, "")
		}
		lines = append(lines, "XIANYU_IMAGE="+imageReference)
	}
	return []byte(strings.Join(lines, "\n")), nil
}

// writeFileAtomic 在目标目录创建临时文件、同步并重命名，避免中断留下半个 .env。
func writeFileAtomic(path string, content []byte, mode os.FileMode) error {
	// directory 是目标文件所在目录，临时文件必须位于同一文件系统。
	directory := filepath.Dir(path)
	// existingInfo 保存目标文件属主，root updater 原子替换时不能静默改成 root:root。
	existingInfo, statErr := os.Stat(path)
	if statErr != nil {
		return statErr
	}
	// existingStat 保存 Linux UID/GID；updater 只支持 Linux 宿主机。
	existingStat, statOK := existingInfo.Sys().(*syscall.Stat_t)
	if !statOK {
		return errors.New("无法读取目标文件 Unix 属主信息")
	}
	// temporaryFile 是本次原子替换独占的临时文件。
	temporaryFile, createErr := os.CreateTemp(directory, ".updater-env-*")
	if createErr != nil {
		return createErr
	}
	// temporaryPath 用于失败清理尚未重命名的临时文件。
	temporaryPath := temporaryFile.Name()
	// committed 表示临时文件是否已经成功替换目标。
	committed := false
	defer func() {
		_ = temporaryFile.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	// chmodErr 表示临时文件无法继承敏感目标文件权限。
	if chmodErr := temporaryFile.Chmod(mode); chmodErr != nil {
		return chmodErr
	}
	if int(existingStat.Uid) != os.Geteuid() || int(existingStat.Gid) != os.Getegid() {
		// chownErr 表示临时文件无法继承目标配置属主。
		if chownErr := temporaryFile.Chown(int(existingStat.Uid), int(existingStat.Gid)); chownErr != nil {
			return chownErr
		}
	}
	// writeErr 表示完整目标内容未能写入同目录临时文件。
	if _, writeErr := temporaryFile.Write(content); writeErr != nil {
		return writeErr
	}
	// syncErr 表示临时文件内容未能在重命名前同步到底层存储。
	if syncErr := temporaryFile.Sync(); syncErr != nil {
		return syncErr
	}
	// closeErr 表示临时文件在原子替换前关闭失败。
	if closeErr := temporaryFile.Close(); closeErr != nil {
		return closeErr
	}
	// renameErr 表示同文件系统原子替换目标文件失败。
	if renameErr := os.Rename(temporaryPath, path); renameErr != nil {
		return renameErr
	}
	committed = true
	// directoryHandle 用于同步重命名目录项，避免掉电后只持久化文件内容而丢失替换结果。
	directoryHandle, openErr := os.Open(directory)
	if openErr != nil {
		return openErr
	}
	defer directoryHandle.Close()
	return directoryHandle.Sync()
}
