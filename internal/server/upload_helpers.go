package server

import "io"

// readLimitedBytes 负责readLimitedBytes相关处理。
func readLimitedBytes(r io.Reader, maxBytes int64) ([]byte, bool, error) {
	// data、err 保存data、err，供当前处理流程使用
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > maxBytes {
		return nil, true, nil
	}
	return data, false, nil
}
