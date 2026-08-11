package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"runtime"
)

// trayIconBytes 返回一个不依赖外部文件的图标，避免安装器还要额外管理图标路径。
// Windows 的 Shell_NotifyIcon 需要 ICO；macOS/Linux 可以直接使用 PNG。
func trayIconBytes() []byte {
	const size = 32
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := x-size/2, y-size/2
			if dx*dx+dy*dy <= 14*14 {
				img.SetRGBA(x, y, color.RGBA{R: 37, G: 99, B: 235, A: 255})
			}
			if abs(x-y) <= 2 || abs(x+y-(size-1)) <= 2 {
				if dx*dx+dy*dy <= 10*10 {
					img.SetRGBA(x, y, color.RGBA{R: 255, G: 255, B: 255, A: 255})
				}
			}
		}
	}

	var pngData bytes.Buffer
	_ = png.Encode(&pngData, img)
	if runtime.GOOS != "windows" {
		return pngData.Bytes()
	}
	return pngToICO(pngData.Bytes(), size)
}

func pngToICO(data []byte, size int) []byte {
	const headerSize = 6
	const entrySize = 16
	result := make([]byte, headerSize+entrySize+len(data))
	// ICONDIR: reserved, type=icon, image count=1.
	binary.LittleEndian.PutUint16(result[0:2], 0)
	binary.LittleEndian.PutUint16(result[2:4], 1)
	binary.LittleEndian.PutUint16(result[4:6], 1)
	entry := result[headerSize : headerSize+entrySize]
	entry[0] = byte(size)
	entry[1] = byte(size)
	entry[2] = 0
	entry[3] = 0
	binary.LittleEndian.PutUint16(entry[4:6], 1)
	binary.LittleEndian.PutUint16(entry[6:8], 32)
	binary.LittleEndian.PutUint32(entry[8:12], uint32(len(data)))
	binary.LittleEndian.PutUint32(entry[12:16], headerSize+entrySize)
	copy(result[headerSize+entrySize:], data)
	return result
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
