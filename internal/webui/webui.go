// Package webui 提供嵌入式前端构建产物。
package webui

import (
	"embed"
	"io/fs"
)

// files 保存文件列表，供当前处理流程使用
//
//go:embed static/*
var files embed.FS

// Static returns the embedded built frontend rooted at static/.
// Static 负责Static相关处理。
func Static() (fs.FS, error) {
	return fs.Sub(files, "static")
}
