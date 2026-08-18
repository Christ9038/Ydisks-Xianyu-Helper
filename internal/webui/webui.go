// Package webui 提供嵌入式前端构建产物。
package webui

import (
	"embed"
	"io/fs"
)

// files 用于本次流程后续判断的文件列表
//
//go:embed static/*
var files embed.FS

// Static returns the embedded built frontend rooted at static/.
// Static 封装Static业务协调。
func Static() (fs.FS, error) {
	return fs.Sub(files, "static")
}
