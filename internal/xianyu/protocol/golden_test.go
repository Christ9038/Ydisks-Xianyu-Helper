package protocol

import (
	_ "embed"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// sampleB64 用于本次流程后续判断的sampleB64
//
//go:embed testdata/sample.b64
var sampleB64 string

// expectedDecrypt 用于本次流程后续判断的expectedDecrypt
//
//go:embed testdata/expected_decrypt.json
var expectedDecrypt string

// TestGenerateSign_Golden 锁定签名结果。
func TestGenerateSign_Golden(t *testing.T) {
	// got 用于本次流程后续判断的got
	got := GenerateSign("1700000000000", "abc_token", `{"appKey":"x"}`)
	// want 用于本次流程后续判断的want
	want := "497ff18ef9c6d4792ba5aeef0e99929a"
	if got != want {
		t.Fatalf("sign mismatch:\n got %s\nwant %s", got, want)
	}
}

// TestDecrypt_Golden 用真实抓包样本锁定解密输出。
// 比较方式：两侧都按 JSON 解析（UseNumber 保留整数精度），reflect.DeepEqual 结构相等。
// TestDecrypt_Golden 封装TestDecryptGolden业务协调。
func TestDecrypt_Golden(t *testing.T) {
	// got、err 用于本次流程后续判断的got、err
	got, err := Decrypt(strings.TrimSpace(sampleB64))
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	// gotV 用于本次流程后续判断的gotV
	gotV := mustParseJSONUseNumber(t, got)
	// wantV 用于本次流程后续判断的wantV
	wantV := mustParseJSONUseNumber(t, strings.TrimSpace(expectedDecrypt))
	if !reflect.DeepEqual(gotV, wantV) {
		// gj 用于本次流程后续判断的gj
		gj, _ := json.MarshalIndent(gotV, "", "  ")
		// wj 用于本次流程后续判断的wj
		wj, _ := json.MarshalIndent(wantV, "", "  ")
		t.Fatalf("decrypt mismatch:\n--- got ---\n%s\n--- want ---\n%s", gj, wj)
	}
}

// TestMessagePackSignedIntegers 封装Test消息PackSignedIntegers业务协调。
func TestMessagePackSignedIntegers(t *testing.T) {
	// cases 用于本次流程后续判断的cases
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
			// decoder 用于本次流程后续判断的decoder
			decoder := &msgpackDecoder{data: tc.raw}
			// got、err 用于本次流程后续判断的got、err
			got, err := decoder.decodeValue()
			if err != nil || got != tc.want {
				t.Fatalf("decodeValue() = %#v, %v; want %d", got, err, tc.want)
			}
		})
	}
}

// TestGeneratedIdentifiers 封装TestGeneratedIdentifiers业务协调。
func TestGeneratedIdentifiers(t *testing.T) {
	if // mid 用于本次流程后续判断的mid
	mid := GenerateMid(); !strings.HasSuffix(mid, " 0") {
		t.Fatalf("invalid mid: %q", mid)
	}
	if // uuid 用于本次流程后续判断的uuid
	uuid := GenerateUUID(); !strings.HasPrefix(uuid, "-") || !strings.HasSuffix(uuid, "1") {
		t.Fatalf("invalid uuid: %q", uuid)
	}
	// deviceID 用于本次流程后续判断的deviceID
	deviceID := GenerateDeviceID("123")
	if len(deviceID) != 40 || !strings.HasSuffix(deviceID, "-123") || deviceID[14] != '4' {
		t.Fatalf("invalid device ID: %q", deviceID)
	}
}

// mustParseJSONUseNumber 封装mustParseJSONUseNumber业务协调。
func mustParseJSONUseNumber(t *testing.T, s string) any {
	t.Helper()
	// dec 用于本次流程后续判断的dec
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	// v 用于本次流程后续判断的v
	var v any
	if // err 用于本次流程后续判断的err
	err := dec.Decode(&v); err != nil {
		t.Fatalf("parse json: %v", err)
	}
	return v
}

// TestTransCookies 基本解析。
func TestTransCookies(t *testing.T) {
	// c 用于本次流程后续判断的c
	c := TransCookies("a=1; b=2; _m_h5_tk=tokenpart_123")
	if c["a"] != "1" || c["b"] != "2" {
		t.Fatalf("unexpected: %v", c)
	}
	if // got 用于本次流程后续判断的got
	got := SignToken("a=1; _m_h5_tk=tokenpart_123"); got != "tokenpart" {
		t.Fatalf("SignToken = %q, want tokenpart", got)
	}
}
