package adapter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	libraryapp "xianyu-go/internal/application/library"
	"xianyu-go/internal/netguard"
)

// libraryRemoteImageTimeout 是单张网址图片的下载时限；导入服务据此保证单行最坏耗时低于服务端写超时。
const libraryRemoteImageTimeout = 10 * time.Second

// LibraryRemoteImageFetcher 通过 netguard 出站策略下载 Excel 导入中的网址图片，供资源库写入图片库。
// 它只做下载与大小限制，图片内容是否受支持由图片库的内容嗅探判断；无可变状态，可并发使用。
type LibraryRemoteImageFetcher struct {
	// client 是受出站策略约束的 HTTP 客户端，开启“仅公网”时拒绝连接本机与内网地址。
	client *http.Client
}

// NewLibraryRemoteImageFetcher 创建使用系统出站策略与 10 秒时限的下载器，策略与批量铺货下载网址图片一致。
func NewLibraryRemoteImageFetcher() *LibraryRemoteImageFetcher {
	return &LibraryRemoteImageFetcher{client: netguard.ConfiguredHTTPClient(libraryRemoteImageTimeout)}
}

// Fetch 下载一张网址图片：只接受 http/https，非 2xx 状态、超过 10 MiB 或读取失败都返回错误。
// 错误信息只描述原因，不重复网址，调用方会自行标注是哪张图片。
func (f *LibraryRemoteImageFetcher) Fetch(ctx context.Context, rawURL string) ([]byte, error) {
	if f == nil || f.client == nil {
		return nil, errors.New("图片下载器未初始化")
	}
	// parsed、parseErr 保存网址解析结果。
	parsed, parseErr := url.Parse(rawURL)
	if parseErr != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" {
		return nil, errors.New("网址无效")
	}
	// request 是携带调用方取消信号的请求；网址已解析成功、方法为常量，构造不会失败。
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	// response、err 保存响应及连接错误；被出站策略拒绝的地址同样在此返回。
	response, err := f.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("无法连接图片地址或连接超时")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	// data、readErr 保存最多 10 MiB 加一字节的响应体。
	data, readErr := io.ReadAll(io.LimitReader(response.Body, libraryapp.MaxImageBytes+1))
	if readErr != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("读取图片失败")
	}
	if len(data) > libraryapp.MaxImageBytes {
		return nil, errors.New("图片不能超过 10 MiB")
	}
	return data, nil
}

// 编译期确认下载器满足资源库导入端口。
var _ libraryapp.RemoteImageFetcher = (*LibraryRemoteImageFetcher)(nil)
