package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// maximumHealthBodyBytes 限制本机健康响应大小。
	maximumHealthBodyBytes = 64 << 10
)

// httpHealthClient 读取固定本机健康接口并校验 JSON 状态。
type httpHealthClient struct {
	// client 提供请求超时；每次调用仍受上层 Context 控制。
	client *http.Client
}

// newHTTPHealthClient 创建短超时本机健康检查客户端。
func newHTTPHealthClient() *httpHealthClient {
	return &httpHealthClient{client: &http.Client{Timeout: 5 * time.Second}}
}

// Read 请求 targetURL 并要求 status/database 均为 ok，返回公开构建信息。
func (client *httpHealthClient) Read(ctx context.Context, targetURL string) (BuildInfo, error) {
	// request 是只读本机健康检查请求。
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if requestErr != nil {
		return BuildInfo{}, requestErr
	}
	// response 是 app 健康端点返回结果。
	response, responseErr := client.client.Do(request)
	if responseErr != nil {
		return BuildInfo{}, responseErr
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return BuildInfo{}, fmt.Errorf("健康接口返回 HTTP %d", response.StatusCode)
	}
	// payload 保存受大小限制的健康响应字节。
	payload, readErr := io.ReadAll(io.LimitReader(response.Body, maximumHealthBodyBytes+1))
	if readErr != nil {
		return BuildInfo{}, readErr
	}
	if len(payload) > maximumHealthBodyBytes {
		return BuildInfo{}, errors.New("健康响应超过大小限制")
	}
	// build 保存解码后的健康与构建信息。
	var build BuildInfo
	// decodeErr 表示健康响应不符合公开构建信息 JSON 契约。
	if decodeErr := json.Unmarshal(payload, &build); decodeErr != nil {
		return BuildInfo{}, fmt.Errorf("解析健康响应失败: %w", decodeErr)
	}
	if build.Status != "ok" || build.Database != "ok" {
		return BuildInfo{}, errors.New("应用或数据库健康状态异常")
	}
	return build, nil
}

// waitForHealth 在 deadline 内等待 Docker 和应用健康，同时核对目标版本与提交。
func waitForHealth(ctx context.Context, config runtimeConfig, runner commandRunner, client *httpHealthClient, expected *ReleaseManifest) (BuildInfo, error) {
	// waitContext 限制目标容器切换后的最长健康等待时间。
	waitContext, cancel := context.WithTimeout(ctx, config.healthTimeout)
	defer cancel()
	// ticker 控制轮询频率，避免持续轰击 Docker 和应用。
	ticker := time.NewTicker(config.healthInterval)
	defer ticker.Stop()
	// lastErr 保存最近一次健康失败原因，超时时返回稳定摘要。
	var lastErr error
	for {
		// containerOutput 保存固定 app 服务当前容器 ID。
		containerOutput, containerErr := runner.Run(waitContext, dockerExecutable, composeArgs(config, "ps", "-q", config.composeService)...)
		if containerErr == nil {
			// containerID 是 app 当前容器 ID。
			containerID := strings.TrimSpace(string(containerOutput))
			if containerID != "" && !strings.Contains(containerID, "\n") {
				// dockerHealthOutput 保存 Docker 健康检查状态。
				dockerHealthOutput, dockerHealthErr := runner.Run(waitContext, dockerExecutable, "inspect", "--format", "{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}", containerID)
				if dockerHealthErr == nil && strings.TrimSpace(string(dockerHealthOutput)) == "healthy" {
					// build 保存应用健康接口的构建身份。
					build, buildErr := client.Read(waitContext, config.healthURL)
					if buildErr == nil {
						if expected == nil || healthMatchesRelease(build, *expected) {
							return build, nil
						}
						lastErr = errors.New("健康应用的版本与目标 Release 不一致")
					} else {
						lastErr = buildErr
					}
				} else if dockerHealthErr != nil {
					lastErr = dockerHealthErr
				} else {
					lastErr = errors.New("docker 健康检查尚未通过")
				}
			}
		} else {
			lastErr = containerErr
		}
		select {
		case <-waitContext.Done():
			if lastErr == nil {
				lastErr = waitContext.Err()
			}
			return BuildInfo{}, fmt.Errorf("等待 app 健康超时: %w", lastErr)
		case <-ticker.C:
		}
	}
}

// healthMatchesRelease 要求健康版本和提交均能证明正在运行目标正式镜像。
func healthMatchesRelease(build BuildInfo, manifest ReleaseManifest) bool {
	// version 是去除兼容 v 前缀后的健康版本。
	version := strings.TrimPrefix(strings.TrimSpace(build.Version), "v")
	// commit 是应用可能缩短到 12 位的提交标识。
	commit := strings.TrimSpace(build.Commit)
	return version == manifest.Version && len(commit) >= 7 && strings.HasPrefix(manifest.Commit, commit)
}
