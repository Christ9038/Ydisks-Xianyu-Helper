package main

import (
	_ "embed"
	"encoding/binary"
	"runtime"
)

// icon.png 是从根目录 icon/windows/icon.png 同步的彩色产品图标，并直接嵌入托盘二进制。
//
// productIconPNG 用于本次流程后续判断的productIconPNG
//
//go:embed icon.png
var productIconPNG []byte

// productIconGrayPNG 用于本次流程后续判断的productIconGrayPNG
//
//go:embed icon-gray.png
var productIconGrayPNG []byte

// trayIconBytes 返回当前服务状态对应的图标，避免运行时依赖外部图标文件。
// Windows 的 Shell_NotifyIcon 需要 ICO；macOS/Linux 可以直接使用 PNG。
// trayIconBytes 封装trayIconBytes业务协调。
func trayIconBytes(running bool) []byte {
	// data 用于本次流程后续判断的数据
	data := productIconGrayPNG
	if running {
		data = productIconPNG
	}
	if runtime.GOOS != "windows" {
		return data
	}
	return pngToICO(data, 256)
}

// pngToICO 封装pngToICO业务协调。
func pngToICO(data []byte, size int) []byte {
	// headerSize 用于本次流程后续判断的header数量
	const headerSize = 6
	// entrySize 用于本次流程后续判断的entry数量
	const entrySize = 16
	// result 用于本次流程后续判断的结果
	result := make([]byte, headerSize+entrySize+len(data))
	// ICONDIR: reserved, type=icon, image count=1.
	binary.LittleEndian.PutUint16(result[0:2], 0)
	binary.LittleEndian.PutUint16(result[2:4], 1)
	binary.LittleEndian.PutUint16(result[4:6], 1)
	// entry 用于本次流程后续判断的entry
	entry := result[headerSize : headerSize+entrySize]
	if size >= 256 {
		entry[0] = 0
		entry[1] = 0
	} else {
		entry[0] = byte(size)
		entry[1] = byte(size)
	}
	entry[2] = 0
	entry[3] = 0
	binary.LittleEndian.PutUint16(entry[4:6], 1)
	binary.LittleEndian.PutUint16(entry[6:8], 32)
	binary.LittleEndian.PutUint32(entry[8:12], uint32(len(data)))
	binary.LittleEndian.PutUint32(entry[12:16], headerSize+entrySize)
	copy(result[headerSize+entrySize:], data)
	return result
}
