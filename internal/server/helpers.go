package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

const maxJSONRequestBytes = 1 << 20

// writeJSON 写 JSON 响应。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// unifiedErrorPayload 构造统一 code/message 错误体，供无请求上下文的内部出口使用。
func unifiedErrorPayload(msg string) map[string]any {
	return map[string]any{"code": "internal_error", "message": msg}
}

// writeErr 写统一错误响应。
func writeErr(w http.ResponseWriter, status int, msg string) {
	writeErrCode(w, status, "", msg, "")
}

// decodeJSON 解析请求体 JSON。
func decodeJSON(r *http.Request, v any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxJSONRequestBytes+1))
	if err != nil {
		return err
	}
	if len(body) > maxJSONRequestBytes {
		return fmt.Errorf("JSON 请求体超过 %d 字节", maxJSONRequestBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(v); err != nil {
		return err
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON 请求体只能包含一个值")
		}
		return err
	}
	return nil
}
