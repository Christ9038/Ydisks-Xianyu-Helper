package protocol

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

// b64 返回输入字节的 base64 编码，方便构造 Decrypt 输入。
func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// mp 构造单值 msgpack 字节流并返回其 base64 表示，供 Decrypt 用。
func mp(b ...byte) string { return b64(b) }

// TestMessagePack_DecodePrimitives 覆盖 msgpack 各原生类型的解码。
func TestMessagePack_DecodePrimitives(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := []struct {
		name string
		raw  []byte
		want any
	}{
		// positive fixint
		{"positive fixint 0", []byte{0x00}, int64(0)},
		{"positive fixint 127", []byte{0x7f}, int64(127)},
		// negative fixint
		{"negative fixint -32", []byte{0xe0}, int64(-32)},
		{"negative fixint -1", []byte{0xff}, int64(-1)},
		// nil / bool
		{"nil", []byte{0xc0}, nil},
		{"false", []byte{0xc2}, false},
		{"true", []byte{0xc3}, true},
		// uint family
		{"uint8", []byte{0xcc, 0xff}, uint64(255)},
		{"uint16", []byte{0xcd, 0x01, 0x00}, uint64(256)},
		{"uint32", []byte{0xce, 0x00, 0x01, 0x00, 0x00}, uint64(65536)},
		{"uint64", []byte{0xcf, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00}, uint64(4294967296)},
		// int family (signed positives here)
		{"int8 positive", []byte{0xd0, 0x7f}, int64(127)},
		{"int16 positive", []byte{0xd1, 0x01, 0x00}, int64(256)},
		{"int32 positive", []byte{0xd2, 0x00, 0x01, 0x00, 0x00}, int64(65536)},
		{"int64 positive", []byte{0xd3, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00}, int64(4294967296)},
		// float family
		{"float32", []byte{0xca, 0x40, 0x49, 0x0f, 0xdb}, float64(math.Float32frombits(0x40490fdb))}, // 3.1415927
		{"float64", []byte{0xcb, 0x40, 0x09, 0x21, 0xfb, 0x54, 0x44, 0x2d, 0x18}, math.Float64frombits(0x400921fb54442d18)},
		// fixstr / str8 / str16 / str32
		{"fixstr", []byte{0xa5, 'h', 'e', 'l', 'l', 'o'}, "hello"},
		{"str8", append([]byte{0xd9, 0x03}, []byte("abc")...), "abc"},
		{"str16", append([]byte{0xda, 0x00, 0x03}, []byte("abc")...), "abc"},
		{"str32", append([]byte{0xdb, 0x00, 0x00, 0x00, 0x03}, []byte("abc")...), "abc"},
		// bin family
		{"bin8", append([]byte{0xc4, 0x03}, []byte("xyz")...), []byte("xyz")},
		{"bin16", append([]byte{0xc5, 0x00, 0x03}, []byte("xyz")...), []byte("xyz")},
		{"bin32", append([]byte{0xc6, 0x00, 0x00, 0x00, 0x03}, []byte("xyz")...), []byte("xyz")},
	}
	// tc 表示当前遍历过程中的tc
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// d 用于本次流程后续判断的d
			d := &msgpackDecoder{data: tc.raw}
			// got、err 用于本次流程后续判断的got、err
			got, err := d.decodeValue()
			if err != nil {
				t.Fatalf("decodeValue() err: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("decodeValue() = %#v (%T), want %#v (%T)", got, got, tc.want, tc.want)
			}
		})
	}
}

// TestMessagePack_DecodeArrays 覆盖 fixarray / array16 / array32 与嵌套数组。
func TestMessagePack_DecodeArrays(t *testing.T) {
	t.Run("fixarray empty", func(t *testing.T) {
		// d 用于本次流程后续判断的d
		d := &msgpackDecoder{data: []byte{0x90}}
		// got、err 用于本次流程后续判断的got、err
		got, err := d.decodeValue()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		// want 用于本次流程后续判断的want
		want := []any{}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	})
	t.Run("fixarray mixed types", func(t *testing.T) {
		// [1, "a", true, nil]
		// d 用于本次流程后续判断的d
		d := &msgpackDecoder{data: []byte{0x94, 0x01, 0xa1, 'a', 0xc3, 0xc0}}
		// got、err 用于本次流程后续判断的got、err
		got, err := d.decodeValue()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		// want 用于本次流程后续判断的want
		want := []any{int64(1), "a", true, nil}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	})
	t.Run("array16", func(t *testing.T) {
		// array16 with 2 elements: [1, 2]
		// d 用于本次流程后续判断的d
		d := &msgpackDecoder{data: []byte{0xdc, 0x00, 0x02, 0x01, 0x02}}
		// got、err 用于本次流程后续判断的got、err
		got, err := d.decodeValue()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		// want 用于本次流程后续判断的want
		want := []any{int64(1), int64(2)}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	})
	t.Run("array32", func(t *testing.T) {
		// array32 with 2 elements: [1, 2]
		// d 用于本次流程后续判断的d
		d := &msgpackDecoder{data: []byte{0xdd, 0x00, 0x00, 0x00, 0x02, 0x01, 0x02}}
		// got、err 用于本次流程后续判断的got、err
		got, err := d.decodeValue()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		// want 用于本次流程后续判断的want
		want := []any{int64(1), int64(2)}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	})
	t.Run("nested arrays", func(t *testing.T) {
		// [[1, 2], [3, 4]]
		// d 用于本次流程后续判断的d
		d := &msgpackDecoder{data: []byte{0x92, 0x92, 0x01, 0x02, 0x92, 0x03, 0x04}}
		// got、err 用于本次流程后续判断的got、err
		got, err := d.decodeValue()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		// want 用于本次流程后续判断的want
		want := []any{[]any{int64(1), int64(2)}, []any{int64(3), int64(4)}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	})
}

// TestMessagePack_DecodeMaps 覆盖 fixmap / map16 / map32 及嵌套 map。
func TestMessagePack_DecodeMaps(t *testing.T) {
	t.Run("fixmap empty", func(t *testing.T) {
		// d 用于本次流程后续判断的d
		d := &msgpackDecoder{data: []byte{0x80}}
		// got、err 用于本次流程后续判断的got、err
		got, err := d.decodeValue()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		// want 用于本次流程后续判断的want
		want := map[any]any{}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	})
	t.Run("fixmap string keys", func(t *testing.T) {
		// {"a": 1, "b": 2}
		// d 用于本次流程后续判断的d
		d := &msgpackDecoder{data: []byte{0x82, 0xa1, 'a', 0x01, 0xa1, 'b', 0x02}}
		// got、err 用于本次流程后续判断的got、err
		got, err := d.decodeValue()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		// want 用于本次流程后续判断的want
		want := map[any]any{"a": int64(1), "b": int64(2)}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	})
	t.Run("fixmap integer keys", func(t *testing.T) {
		// {1: "a", 2: "b"}
		// d 用于本次流程后续判断的d
		d := &msgpackDecoder{data: []byte{0x82, 0x01, 0xa1, 'a', 0x02, 0xa1, 'b'}}
		// got、err 用于本次流程后续判断的got、err
		got, err := d.decodeValue()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		// want 用于本次流程后续判断的want
		want := map[any]any{int64(1): "a", int64(2): "b"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	})
	t.Run("map16", func(t *testing.T) {
		// map16 with 1 pair: {"k": 1}
		// d 用于本次流程后续判断的d
		d := &msgpackDecoder{data: []byte{0xde, 0x00, 0x01, 0xa1, 'k', 0x01}}
		// got、err 用于本次流程后续判断的got、err
		got, err := d.decodeValue()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		// want 用于本次流程后续判断的want
		want := map[any]any{"k": int64(1)}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	})
	t.Run("map32", func(t *testing.T) {
		// map32 with 1 pair: {"k": 1}
		// d 用于本次流程后续判断的d
		d := &msgpackDecoder{data: []byte{0xdf, 0x00, 0x00, 0x00, 0x01, 0xa1, 'k', 0x01}}
		// got、err 用于本次流程后续判断的got、err
		got, err := d.decodeValue()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		// want 用于本次流程后续判断的want
		want := map[any]any{"k": int64(1)}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	})
	t.Run("nested map and array", func(t *testing.T) {
		// {"arr": [1, 2], "obj": {"k": "v"}}
		// raw 用于本次流程后续判断的原始
		raw := []byte{
			0x82,
			0xa3, 'a', 'r', 'r', 0x92, 0x01, 0x02,
			0xa3, 'o', 'b', 'j', 0x81, 0xa1, 'k', 0xa1, 'v',
		}
		// d 用于本次流程后续判断的d
		d := &msgpackDecoder{data: raw}
		// got、err 用于本次流程后续判断的got、err
		got, err := d.decodeValue()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		// want 用于本次流程后续判断的want
		want := map[any]any{
			"arr": []any{int64(1), int64(2)},
			"obj": map[any]any{"k": "v"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	})
}

// TestMessagePack_Errors 覆盖解码错误路径：截断、未知格式字节。
func TestMessagePack_Errors(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := []struct {
		name string
		raw  []byte
	}{
		{"empty input", nil},
		{"truncated uint16", []byte{0xcd, 0x01}},
		{"truncated uint32", []byte{0xce, 0x00, 0x01}},
		{"truncated uint64", []byte{0xcf, 0x00, 0x00, 0x00, 0x01}},
		{"truncated int16", []byte{0xd1, 0x01}},
		{"truncated int32", []byte{0xd2, 0x00, 0x01}},
		{"truncated int64", []byte{0xd3, 0x00, 0x00, 0x00, 0x01}},
		{"truncated float32", []byte{0xca, 0x40, 0x49}},
		{"truncated float64", []byte{0xcb, 0x40, 0x09, 0x21}},
		{"truncated bin8", []byte{0xc4, 0x05, 'a', 'b'}},
		{"bin8 missing length byte", []byte{0xc4}},
		{"truncated bin16", []byte{0xc5, 0x00, 0x05, 'a'}},
		{"bin16 missing length", []byte{0xc5, 0x00}},
		{"truncated bin32", []byte{0xc6, 0x00, 0x00, 0x00, 0x05, 'a'}},
		{"bin32 missing length", []byte{0xc6, 0x00, 0x00, 0x00}},
		{"truncated str8", []byte{0xd9, 0x05, 'a', 'b'}},
		{"str8 missing length byte", []byte{0xd9}},
		{"truncated str16", []byte{0xda, 0x00, 0x05, 'a'}},
		{"str16 missing length", []byte{0xda, 0x00}},
		{"truncated str32", []byte{0xdb, 0x00, 0x00, 0x00, 0x05, 'a'}},
		{"str32 missing length", []byte{0xdb, 0x00, 0x00, 0x00}},
		{"truncated array16 length", []byte{0xdc, 0x00}},
		{"truncated array32 length", []byte{0xdd, 0x00, 0x00, 0x00}},
		{"truncated map16 length", []byte{0xde, 0x00}},
		{"truncated map32 length", []byte{0xdf, 0x00, 0x00, 0x00}},
		{"unknown format byte 0xc1", []byte{0xc1}},
		{"bin32 overflow count", []byte{0xc6, 0xff, 0xff, 0xff, 0xff}},
		{"truncated array element", []byte{0x92, 0x01}},
		{"truncated map value", []byte{0x81, 0xa1, 'k'}},
		{"truncated map key", []byte{0x81}},
	}
	// tc 表示当前遍历过程中的tc
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// d 用于本次流程后续判断的d
			d := &msgpackDecoder{data: tc.raw}
			// err 用于本次流程后续判断的err
			_, err := d.decodeValue()
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

// TestDecrypt_Types 端到端覆盖 Decrypt 对各 msgpack 类型的 JSON 归一化输出。
func TestDecrypt_Types(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		// got、err 用于本次流程后续判断的got、err
		got, err := Decrypt(mp(0xc0))
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got != "null" {
			t.Fatalf("got %q, want %q", got, "null")
		}
	})
	t.Run("bool true", func(t *testing.T) {
		// got、err 用于本次流程后续判断的got、err
		got, err := Decrypt(mp(0xc3))
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if got != "true" {
			t.Fatalf("got %q, want %q", got, "true")
		}
	})
	t.Run("integer key map", func(t *testing.T) {
		// {1: "a"} → {"1":"a"}
		// got、err 用于本次流程后续判断的got、err
		got, err := Decrypt(mp(0x81, 0x01, 0xa1, 'a'))
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		// want 用于本次流程后续判断的want
		want := `{"1":"a"}`
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
	t.Run("nested map array", func(t *testing.T) {
		// {"arr":[1,2],"obj":{"k":"v"}}
		// raw 用于本次流程后续判断的原始
		raw := []byte{
			0x82,
			0xa3, 'a', 'r', 'r', 0x92, 0x01, 0x02,
			0xa3, 'o', 'b', 'j', 0x81, 0xa1, 'k', 0xa1, 'v',
		}
		// got、err 用于本次流程后续判断的got、err
		got, err := Decrypt(mp(raw...))
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		// JSON 结构比对（顺序无关）。
		var gv, wv any
		if // err 用于本次流程后续判断的err
		err := json.Unmarshal([]byte(got), &gv); err != nil {
			t.Fatalf("unmarshal got: %v", err)
		}
		if // err 用于本次流程后续判断的err
		err := json.Unmarshal([]byte(`{"arr":[1,2],"obj":{"k":"v"}}`), &wv); err != nil {
			t.Fatalf("unmarshal want: %v", err)
		}
		if !reflect.DeepEqual(gv, wv) {
			t.Fatalf("got %s, want struct equal", got)
		}
	})
	t.Run("bin to utf8 string", func(t *testing.T) {
		// bin8 of "hello"
		// got、err 用于本次流程后续判断的got、err
		got, err := Decrypt(mp(append([]byte{0xc4, 0x05}, []byte("hello")...)...))
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		// want 用于本次流程后续判断的want
		want := `"hello"`
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
	t.Run("bin with invalid utf8 dropped", func(t *testing.T) {
		// bin8 with 0xff (invalid utf8 lead byte) → ToValidUTF8 drops it.
		// got、err 用于本次流程后续判断的got、err
		got, err := Decrypt(mp(0xc4, 0x02, 0xff, 0x41))
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		// 0xff dropped, 0x41='A' kept.
		if got != `"A"` {
			t.Fatalf("got %q, want %q", got, `"A"`)
		}
	})
	t.Run("float64", func(t *testing.T) {
		// bits 用于本次流程后续判断的bits
		bits := math.Float64bits(3.14)
		// buf 用于本次流程后续判断的buf
		buf := make([]byte, 9)
		buf[0] = 0xcb
		binary.BigEndian.PutUint64(buf[1:], bits)
		// got、err 用于本次流程后续判断的got、err
		got, err := Decrypt(mp(buf...))
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		// g 用于本次流程后续判断的g
		var g float64
		if // err 用于本次流程后续判断的err
		err := json.Unmarshal([]byte(got), &g); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if g != 3.14 {
			t.Fatalf("got %v, want 3.14", g)
		}
	})
}

// TestDecrypt_Base64Padding 覆盖缺 padding 的 base64 自动补齐路径。
func TestDecrypt_Base64Padding(t *testing.T) {
	// msgpack fixstr "hij" → 字节 0xa3 'h' 'i' 'j'，再 base64 编码。
	// 4 字节输入 → base64 输出 8 字符（含 1 个 '=' padding）。
	// full 用于本次流程后续判断的full
	full := b64([]byte{0xa3, 'h', 'i', 'j'})
	// trimmed 用于本次流程后续判断的trimmed
	trimmed := strings.TrimRight(full, "=")
	// 去掉 padding 后长度模 4 ≠ 0，应触发补齐逻辑并解码成功。
	got, err := Decrypt(trimmed)
	if err != nil {
		t.Fatalf("Decrypt without padding err: %v", err)
	}
	if got != `"hij"` {
		t.Fatalf("got %q, want %q", got, `"hij"`)
	}
}

// TestDecrypt_NonASCIIStripped 覆盖 stripNonASCII：输入含非 ASCII 字节时被剥离。
func TestDecrypt_NonASCIIStripped(t *testing.T) {
	// msgpack fixstr "hi" → 0xa2 'h' 'i'，再 base64 编码。
	full := b64([]byte{0xa2, 'h', 'i'})
	// 0x80 是非 ASCII，会被 stripNonASCII 删除，剩余部分仍可解码。
	got, err := Decrypt("\x80" + full)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != `"hi"` {
		t.Fatalf("got %q, want %q", got, `"hi"`)
	}
}

// TestDecrypt_Errors 覆盖 Decrypt 的错误返回路径。
func TestDecrypt_Errors(t *testing.T) {
	t.Run("empty string", func(t *testing.T) {
		// 空串 base64 解码成功为空字节，再 msgpack 解码会因 readByte 失败。
		_, err := Decrypt("")
		if err == nil {
			t.Fatal("expected error for empty input")
		}
	})
	t.Run("invalid base64", func(t *testing.T) {
		// 含非 base64 字符且补 padding 后仍非法。
		_, err := Decrypt("!!!!")
		if err == nil {
			t.Fatal("expected error for invalid base64")
		}
		if !strings.Contains(err.Error(), "base64") {
			t.Fatalf("error should mention base64, got: %v", err)
		}
	})
	t.Run("invalid msgpack bytes", func(t *testing.T) {
		// 解码为合法 base64 但内容是非法 msgpack 字节流（0xc1 reserved）。
		_, err := Decrypt(mp(0xc1))
		if err == nil {
			t.Fatal("expected error for invalid msgpack")
		}
		if !strings.Contains(err.Error(), "解密失败") {
			t.Fatalf("error should be wrapped, got: %v", err)
		}
	})
	t.Run("json marshal error", func(t *testing.T) {
		// raw 是包含 NaN 浮点值的 MessagePack 数据，JSON 不允许序列化该值。
		raw := []byte{0xcb, 0x7f, 0xf8, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01}
		// err 是 JSON 序列化 NaN 时返回的解密错误。
		_, err := Decrypt(mp(raw...))
		if err == nil || !strings.Contains(err.Error(), "JSON 序列化") {
			t.Fatalf("NaN 应触发 JSON 序列化错误: %v", err)
		}
	})
}

// TestNormalizeForJSON 覆盖 normalizeForJSON 的各分支。
func TestNormalizeForJSON(t *testing.T) {
	t.Run("map[any]any with mixed keys", func(t *testing.T) {
		// in 用于本次流程后续判断的in
		in := map[any]any{
			"str":        "v",
			int64(2):     int64(3),
			uint64(4):    uint64(5),
			true:         false,
			nil:          "n",
			float64(1.5): "f",
		}
		// out 用于本次流程后续判断的out
		out := normalizeForJSON(in)
		// m、ok 用于本次流程后续判断的m、ok
		m, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any, got %T", out)
		}
		if m["str"] != "v" || m["2"] != int64(3) || m["4"] != uint64(5) {
			t.Fatalf("unexpected map: %#v", m)
		}
		if m["true"] != false || m["null"] != "n" || m["1.5"] != "f" {
			t.Fatalf("unexpected map: %#v", m)
		}
	})
	t.Run("nested []any", func(t *testing.T) {
		// in 用于本次流程后续判断的in
		in := []any{int64(1), []any{int64(2), "x"}}
		// out 用于本次流程后续判断的out
		out := normalizeForJSON(in)
		// arr、ok 用于本次流程后续判断的arr、ok
		arr, ok := out.([]any)
		if !ok {
			t.Fatalf("expected []any, got %T", out)
		}
		if arr[0] != int64(1) {
			t.Fatalf("arr[0] = %#v", arr[0])
		}
		if // inner、ok 用于本次流程后续判断的inner、ok
		inner, ok := arr[1].([]any); !ok || inner[0] != int64(2) || inner[1] != "x" {
			t.Fatalf("arr[1] = %#v", arr[1])
		}
	})
	t.Run("[]byte normalized to valid utf8", func(t *testing.T) {
		// in 用于本次流程后续判断的in
		in := []byte{0x41, 0xff, 0x42} // 'A', invalid, 'B'
		// out 用于本次流程后续判断的out
		out := normalizeForJSON(in)
		if out != "AB" {
			t.Fatalf("got %q, want %q", out, "AB")
		}
	})
	t.Run("scalar passthrough", func(t *testing.T) {
		// in 表示当前遍历过程中的in
		for _, in := range []any{int64(1), uint64(2), float64(3.5), "s", true, nil} {
			if // out 用于本次流程后续判断的out
			out := normalizeForJSON(in); out != in {
				t.Fatalf("normalizeForJSON(%#v) = %#v, want passthrough", in, out)
			}
		}
	})
	t.Run("map[any]any with unknown key type falls to default", func(t *testing.T) {
		// custom 用于本次流程后续判断的custom
		type custom struct{ X int }
		// in 用于本次流程后续判断的in
		in := map[any]any{custom{1}: "v"}
		// out 用于本次流程后续判断的out
		out := normalizeForJSON(in)
		// m、ok 用于本次流程后续判断的m、ok
		m, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any, got %T", out)
		}
		if len(m) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(m))
		}
	})
}

// TestKeyToString 覆盖 keyToString 的各类型分支。
func TestKeyToString(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"string", "abc", "abc"},
		{"int64", int64(-42), "-42"},
		{"uint64", uint64(42), "42"},
		{"bool true", true, "true"},
		{"bool false", false, "false"},
		{"nil", nil, "null"},
		{"float64", float64(1.5), "1.5"},
		{"bytes valid utf8", []byte("hello"), "hello"},
		{"bytes invalid utf8", []byte{0xff, 0x41}, "A"},
	}
	// tc 表示当前遍历过程中的tc
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if // got 用于本次流程后续判断的got
			got := keyToString(tc.in); got != tc.want {
				t.Fatalf("keyToString(%#v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
	t.Run("default fallback", func(t *testing.T) {
		// custom 用于本次流程后续判断的custom
		type custom struct{ X int }
		// got 用于本次流程后续判断的got
		got := keyToString(custom{X: 7})
		if got == "" {
			t.Fatal("expected non-empty default fallback")
		}
		// fmt.Sprintf("%v", custom{7}) → "{7}"
		if got != "{7}" {
			t.Fatalf("got %q, want {7}", got)
		}
	})
}

// TestStripNonASCII 覆盖 stripNonASCII 行为。
func TestStripNonASCII(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"pure ascii", "hello", "hello"},
		{"mixed", "héllo", "hllo"}, // é 是 2 个 UTF-8 字节，均被剥离
		{"all non ascii", "你好", ""},
		{"control chars below 0x80 kept", "\x01\x02", "\x01\x02"},
		{"del 0x7f kept", "a\x7fb", "a\x7fb"},
		{"0x80 stripped", "a\x80b", "ab"},
	}
	// tc 表示当前遍历过程中的tc
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if // got 用于本次流程后续判断的got
			got := stripNonASCII(tc.in); got != tc.want {
				t.Fatalf("stripNonASCII(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
