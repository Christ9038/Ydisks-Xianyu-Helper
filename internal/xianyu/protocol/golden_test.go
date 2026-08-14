package protocol

import (
	_ "embed"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// sampleB64 保存sampleB64，供当前处理流程使用
//
//go:embed testdata/sample.b64
var sampleB64 string

// expectedDecrypt 保存expectedDecrypt，供当前处理流程使用
//
//go:embed testdata/expected_decrypt.json
var expectedDecrypt string

// TestGenerateSign_Golden 锁定签名结果。
func TestGenerateSign_Golden(t *testing.T) {
	// got 保存got，供当前处理流程使用
	got := GenerateSign("1700000000000", "abc_token", `{"appKey":"x"}`)
	// want 保存want，供当前处理流程使用
	want := "497ff18ef9c6d4792ba5aeef0e99929a"
	if got != want {
		t.Fatalf("sign mismatch:\n got %s\nwant %s", got, want)
	}
}

// TestDecrypt_Golden 用真实抓包样本锁定解密输出。
// 比较方式：两侧都按 JSON 解析（UseNumber 保留整数精度），reflect.DeepEqual 结构相等。
// TestDecrypt_Golden 负责TestDecryptGolden相关处理。
func TestDecrypt_Golden(t *testing.T) {
	// got、err 保存got、err，供当前处理流程使用
	got, err := Decrypt(strings.TrimSpace(sampleB64))
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	// gotV 保存gotV，供当前处理流程使用
	gotV := mustParseJSONUseNumber(t, got)
	// wantV 保存wantV，供当前处理流程使用
	wantV := mustParseJSONUseNumber(t, strings.TrimSpace(expectedDecrypt))
	if !reflect.DeepEqual(gotV, wantV) {
		// gj 保存gj，供当前处理流程使用
		gj, _ := json.MarshalIndent(gotV, "", "  ")
		// wj 保存wj，供当前处理流程使用
		wj, _ := json.MarshalIndent(wantV, "", "  ")
		t.Fatalf("decrypt mismatch:\n--- got ---\n%s\n--- want ---\n%s", gj, wj)
	}
}

// TestMessagePackSignedIntegers 负责Test消息PackSignedIntegers相关处理。
func TestMessagePackSignedIntegers(t *testing.T) {
	// cases 保存cases，供当前处理流程使用
	cases := []struct {
		name string
		raw  []byte
		want int64
	}{
		{"negative fixint", []byte{0xff}, -1},
		{"int8", []byte{0xd0, 0x80}, -128},
		{"int16", []byte{0xd1, 0xff, 0xfe}, -2},
		{"int32", []byte{0xd2, 0xff, 0xff, 0xff, 0xfd}, -3},
		{"int64", []byte{0xd3, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xfc}, -4},
	}
	// tc 表示当前遍历过程中的tc
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// decoder 保存decoder，供当前处理流程使用
			decoder := &msgpackDecoder{data: tc.raw}
			// got、err 保存got、err，供当前处理流程使用
			got, err := decoder.decodeValue()
			if err != nil || got != tc.want {
				t.Fatalf("decodeValue() = %#v, %v; want %d", got, err, tc.want)
			}
		})
	}
}

// TestGeneratedIdentifiers 负责TestGeneratedIdentifiers相关处理。
func TestGeneratedIdentifiers(t *testing.T) {
	if // mid 保存mid，供当前处理流程使用
	mid := GenerateMid(); !strings.HasSuffix(mid, " 0") {
		t.Fatalf("invalid mid: %q", mid)
	}
	if // uuid 保存uuid，供当前处理流程使用
	uuid := GenerateUUID(); !strings.HasPrefix(uuid, "-") || !strings.HasSuffix(uuid, "1") {
		t.Fatalf("invalid uuid: %q", uuid)
	}
	// deviceID 保存deviceID，供当前处理流程使用
	deviceID := GenerateDeviceID("123")
	if len(deviceID) != 40 || !strings.HasSuffix(deviceID, "-123") || deviceID[14] != '4' {
		t.Fatalf("invalid device ID: %q", deviceID)
	}
}

// mustParseJSONUseNumber 负责mustParseJSONUseNumber相关处理。
func mustParseJSONUseNumber(t *testing.T, s string) any {
	t.Helper()
	// dec 保存dec，供当前处理流程使用
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	// v 保存v，供当前处理流程使用
	var v any
	if // err 保存err，供当前处理流程使用
	err := dec.Decode(&v); err != nil {
		t.Fatalf("parse json: %v", err)
	}
	return v
}

// TestTransCookies 基本解析。
func TestTransCookies(t *testing.T) {
	// c 保存c，供当前处理流程使用
	c := TransCookies("a=1; b=2; _m_h5_tk=tokenpart_123")
	if c["a"] != "1" || c["b"] != "2" {
		t.Fatalf("unexpected: %v", c)
	}
	if // got 保存got，供当前处理流程使用
	got := SignToken("a=1; _m_h5_tk=tokenpart_123"); got != "tokenpart" {
		t.Fatalf("SignToken = %q, want tokenpart", got)
	}
}
