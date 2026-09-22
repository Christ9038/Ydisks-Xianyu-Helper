package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	// githubLatestReleaseURL 是 daemon 唯一允许访问的正式版本元数据入口。
	githubLatestReleaseURL = "https://api.github.com/repos/Christ9038/Ydisks-Xianyu-Helper/releases/latest"
	// releaseManifestAssetName 是正式 Release 必须携带的 updater manifest 文件名。
	releaseManifestAssetName = "docker-manifest.json"
	// maximumReleaseBodyBytes 限制远端 JSON 大小，避免异常响应消耗宿主机内存。
	maximumReleaseBodyBytes = 1 << 20
)

var (
	// stableVersionPattern 只接受不含预发布后缀的 X.Y.Z 正式版本。
	stableVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	// digestPattern 只接受完整 sha256 OCI 摘要。
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	// commitPattern 只接受 Git 提交的 40 位十六进制标识。
	commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// ReleaseSource 定义服务检查最新稳定版本所需的最小外部能力。
type ReleaseSource interface {
	// LatestStable 返回已经验证 GitHub Release 与 manifest 一致性的最新稳定版本。
	LatestStable(context.Context) (ReleaseManifest, error)
}

// githubReleaseSource 从固定 GitHub 仓库读取 latest Release 和 manifest 附件。
type githubReleaseSource struct {
	// client 为两个只读 GitHub 请求提供超时和取消能力。
	client *http.Client
	// latestURL 在生产中固定为 githubLatestReleaseURL，测试可替换为本地服务器。
	latestURL string
	// platform 是当前 daemon 所在的 GOOS/GOARCH 组合。
	platform string
}

// githubRelease 保存 GitHub latest API 中与更新判断有关的最小字段。
type githubRelease struct {
	// TagName 是 Release 对应的 Git 标签。
	TagName string `json:"tag_name"`
	// HTMLURL 是供 UI 打开的正式发布页面。
	HTMLURL string `json:"html_url"`
	// Draft 表示 Release 尚未公开，updater 必须拒绝。
	Draft bool `json:"draft"`
	// Prerelease 表示预发布版本，稳定通道必须拒绝。
	Prerelease bool `json:"prerelease"`
	// Assets 保存正式发布附件列表。
	Assets []githubReleaseAsset `json:"assets"`
}

// githubReleaseAsset 保存单个 Release 附件的名称与下载地址。
type githubReleaseAsset struct {
	// Name 是发布附件文件名。
	Name string `json:"name"`
	// BrowserDownloadURL 是 GitHub 提供的附件下载地址。
	BrowserDownloadURL string `json:"browser_download_url"`
}

// newGitHubReleaseSource 创建只访问固定官方仓库的生产 Release 源。
func newGitHubReleaseSource() ReleaseSource {
	return &githubReleaseSource{
		client:    &http.Client{Timeout: 20 * time.Second},
		latestURL: githubLatestReleaseURL,
		platform:  runtime.GOOS + "/" + runtime.GOARCH,
	}
}

// LatestStable 读取 GitHub latest Release，再验证其 manifest 的版本、仓库、摘要、提交和平台。
func (source *githubReleaseSource) LatestStable(ctx context.Context) (ReleaseManifest, error) {
	// release 保存 GitHub latest API 返回的最小稳定版本元数据。
	var release githubRelease
	// requestErr 表示 latest Release 元数据请求或解码失败。
	if requestErr := source.getJSON(ctx, source.latestURL, &release); requestErr != nil {
		return ReleaseManifest{}, fmt.Errorf("读取 GitHub 最新正式版本失败: %w", requestErr)
	}
	if release.Draft || release.Prerelease {
		return ReleaseManifest{}, errors.New("GitHub latest 不是公开稳定版本")
	}
	// manifestURL 保存名称精确匹配的正式更新清单下载地址。
	manifestURL := ""
	// asset 是当前检查的 Release 附件。
	for _, asset := range release.Assets {
		if asset.Name == releaseManifestAssetName {
			manifestURL = asset.BrowserDownloadURL
			break
		}
	}
	if manifestURL == "" {
		return ReleaseManifest{}, errors.New("最新正式版本缺少 docker-manifest.json")
	}
	// manifest 保存 Release 附件声明的不可变镜像目标。
	var manifest ReleaseManifest
	// manifestErr 表示正式更新清单下载或 JSON 解码失败。
	if manifestErr := source.getJSON(ctx, manifestURL, &manifest); manifestErr != nil {
		return ReleaseManifest{}, fmt.Errorf("读取正式版本 manifest 失败: %w", manifestErr)
	}
	manifest.ReleaseURL = release.HTMLURL
	// validationErr 表示 Release 与 manifest 的版本、仓库、摘要或平台不一致。
	if validationErr := validateReleaseManifest(release.TagName, source.platform, manifest); validationErr != nil {
		return ReleaseManifest{}, validationErr
	}
	return manifest, nil
}

// getJSON 执行受 Context 控制的只读 JSON 请求，并限制成功响应体积。
func (source *githubReleaseSource) getJSON(ctx context.Context, targetURL string, destination any) error {
	// request 是带固定 User-Agent 的 GitHub 只读请求。
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if requestErr != nil {
		return requestErr
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "Ydisks-Xianyu-Helper-Updater")
	// response 是 GitHub 或其 Release 附件 CDN 返回的 HTTP 响应。
	response, responseErr := source.client.Do(request)
	if responseErr != nil {
		return responseErr
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("远端返回 HTTP %d", response.StatusCode)
	}
	// limitedReader 防止远端返回超出 manifest 合理范围的数据。
	limitedReader := io.LimitReader(response.Body, maximumReleaseBodyBytes+1)
	// payload 保存受大小限制的完整 JSON 数据。
	payload, readErr := io.ReadAll(limitedReader)
	if readErr != nil {
		return readErr
	}
	if len(payload) > maximumReleaseBodyBytes {
		return errors.New("远端版本元数据超过大小限制")
	}
	// decodeErr 表示远端成功响应不是目标契约要求的 JSON。
	if decodeErr := json.Unmarshal(payload, destination); decodeErr != nil {
		return fmt.Errorf("解析远端版本元数据失败: %w", decodeErr)
	}
	return nil
}

// validateReleaseManifest 拒绝任何不属于固定官方稳定通道的 manifest。
func validateReleaseManifest(releaseTag, platform string, manifest ReleaseManifest) error {
	if manifest.Schema != 1 {
		return fmt.Errorf("不支持的 release manifest schema: %d", manifest.Schema)
	}
	if !stableVersionPattern.MatchString(manifest.Version) || manifest.Tag != "v"+manifest.Version || releaseTag != manifest.Tag {
		return errors.New("release 标签与稳定版本不一致")
	}
	if manifest.Image != OfficialImage {
		return errors.New("release manifest 镜像仓库不受信任")
	}
	if !digestPattern.MatchString(manifest.ManifestDigest) {
		return errors.New("release manifest 镜像摘要无效")
	}
	if !commitPattern.MatchString(manifest.Commit) {
		return errors.New("release manifest 提交号无效")
	}
	if manifest.PublishedAt.IsZero() {
		return errors.New("release manifest 缺少发布时间")
	}
	// supported 表示 manifest 是否声明当前宿主机平台。
	supported := false
	// declaredPlatform 是当前检查的发布平台字符串。
	for _, declaredPlatform := range manifest.Platforms {
		if declaredPlatform == platform {
			supported = true
			break
		}
	}
	if !supported {
		return fmt.Errorf("正式镜像不支持当前平台 %s", platform)
	}
	return nil
}

// compareStableVersions 比较两个严格 X.Y.Z 版本，返回 -1、0 或 1。
func compareStableVersions(left, right string) (int, error) {
	// leftParts 保存左侧版本的三个十进制分段。
	leftParts, leftErr := parseStableVersion(left)
	if leftErr != nil {
		return 0, leftErr
	}
	// rightParts 保存右侧版本的三个十进制分段。
	rightParts, rightErr := parseStableVersion(right)
	if rightErr != nil {
		return 0, rightErr
	}
	// partIndex 是当前比较的 major、minor 或 patch 下标。
	for partIndex := range leftParts {
		if leftParts[partIndex] < rightParts[partIndex] {
			return -1, nil
		}
		if leftParts[partIndex] > rightParts[partIndex] {
			return 1, nil
		}
	}
	return 0, nil
}

// parseStableVersion 将严格 X.Y.Z 版本转换为可逐段比较的整数数组。
func parseStableVersion(value string) ([3]int, error) {
	// parsed 保存 major、minor 和 patch 的十进制值。
	var parsed [3]int
	if !stableVersionPattern.MatchString(value) {
		return parsed, fmt.Errorf("不是稳定语义版本: %s", value)
	}
	// rawParts 保存按点分隔的三个版本字段。
	rawParts := strings.Split(value, ".")
	// partIndex 是当前转换的版本字段下标；rawPart 是对应十进制文本。
	for partIndex, rawPart := range rawParts {
		// numericPart 是当前版本字段的整数值。
		numericPart, conversionErr := strconv.Atoi(rawPart)
		if conversionErr != nil {
			return parsed, fmt.Errorf("解析稳定版本失败: %w", conversionErr)
		}
		parsed[partIndex] = numericPart
	}
	return parsed, nil
}
