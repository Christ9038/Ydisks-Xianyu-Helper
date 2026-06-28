package protocol

import (
	_ "embed"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

//go:embed testdata/sample.b64
var sampleB64 string

//go:embed testdata/expected_decrypt.json
var expectedDecrypt string

// TestGenerateSign_Golden 与 Python hashlib.md5 结果逐字节对比。
func TestGenerateSign_Golden(t *testing.T) {
	got := GenerateSign("1700000000000", "abc_token", `{"appKey":"x"}`)
	want := "497ff18ef9c6d4792ba5aeef0e99929a"
	if got != want {
		t.Fatalf("sign mismatch:\n got %s\nwant %s", got, want)
	}
}

// TestDecrypt_Golden 用真实抓包样本对比 Python decrypt 的输出。
// 比较方式：两侧都按 JSON 解析（UseNumber 保留整数精度），reflect.DeepEqual 结构相等。
func TestDecrypt_Golden(t *testing.T) {
	got, err := Decrypt(strings.TrimSpace(sampleB64))
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	gotV := mustParseJSONUseNumber(t, got)
	wantV := mustParseJSONUseNumber(t, strings.TrimSpace(expectedDecrypt))
	if !reflect.DeepEqual(gotV, wantV) {
		gj, _ := json.MarshalIndent(gotV, "", "  ")
		wj, _ := json.MarshalIndent(wantV, "", "  ")
		t.Fatalf("decrypt mismatch:\n--- got ---\n%s\n--- want ---\n%s", gj, wj)
	}
}

func mustParseJSONUseNumber(t *testing.T, s string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("parse json: %v", err)
	}
	return v
}

// TestTransCookies 基本解析。
func TestTransCookies(t *testing.T) {
	c := TransCookies("a=1; b=2; _m_h5_tk=tokenpart_123")
	if c["a"] != "1" || c["b"] != "2" {
		t.Fatalf("unexpected: %v", c)
	}
	if got := SignToken("a=1; _m_h5_tk=tokenpart_123"); got != "tokenpart" {
		t.Fatalf("SignToken = %q, want tokenpart", got)
	}
}
