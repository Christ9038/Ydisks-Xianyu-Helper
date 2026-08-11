// Command iconconv wraps a PNG image in the Windows ICO container format.
// It is used to generate the checked-in installer icon from the product asset.
package main

import (
	"encoding/binary"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: iconconv input.png output.ico")
		os.Exit(2)
	}

	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		panic(err)
	}

	// A PNG-compressed 256x256 image is valid in an ICO file and keeps the
	// alpha channel of the original product icon.
	const headerSize = 6
	const entrySize = 16
	result := make([]byte, headerSize+entrySize+len(data))
	binary.LittleEndian.PutUint16(result[0:2], 0)
	binary.LittleEndian.PutUint16(result[2:4], 1)
	binary.LittleEndian.PutUint16(result[4:6], 1)
	entry := result[headerSize : headerSize+entrySize]
	entry[0] = 0 // 0 means 256 in the ICO format.
	entry[1] = 0
	binary.LittleEndian.PutUint16(entry[4:6], 1)
	binary.LittleEndian.PutUint16(entry[6:8], 32)
	binary.LittleEndian.PutUint32(entry[8:12], uint32(len(data)))
	binary.LittleEndian.PutUint32(entry[12:16], headerSize+entrySize)
	copy(result[headerSize+entrySize:], data)

	if err := os.WriteFile(os.Args[2], result, 0644); err != nil {
		panic(err)
	}
}
