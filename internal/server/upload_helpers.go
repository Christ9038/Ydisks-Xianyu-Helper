package server

import "io"

// readLimitedBytes 封装readLimitedBytes业务协调。
func readLimitedBytes(r io.Reader, maxBytes int64) ([]byte, bool, error) {
	// data、err 用于本次流程后续判断的data、err
	data, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > maxBytes {
		return nil, true, nil
	}
	return data, false, nil
}
