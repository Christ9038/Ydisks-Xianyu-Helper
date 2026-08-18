package protocol

import (
	"encoding/binary"
	"fmt"
	"math"
)

// msgpackDecoder 解析闲鱼消息使用的 MessagePack 数据。
// 解码结果类型：int64/uint64/float64/string/bool/nil/[]byte/[]any/map[any]any。
// bin 解码为 []byte，map 键保留原类型（整数键为 int64）。
// msgpackDecoder 用于本次流程后续判断的msgpackDecoder
type msgpackDecoder struct {
	data []byte
	pos  int
}

// readByte 封装readByte业务协调。
func (d *msgpackDecoder) readByte() (byte, error) {
	if d.pos >= len(d.data) {
		return 0, fmt.Errorf("msgpack: unexpected end of data")
	}
	// b 用于本次流程后续判断的b
	b := d.data[d.pos]
	d.pos++
	return b, nil
}

// readBytes 封装readBytes业务协调。
func (d *msgpackDecoder) readBytes(n int) ([]byte, error) {
	if d.pos+n > len(d.data) {
		return nil, fmt.Errorf("msgpack: unexpected end of data")
	}
	// r 用于本次流程后续判断的r
	r := d.data[d.pos : d.pos+n]
	d.pos += n
	return r, nil
}

// decodeValue 读取一个 MessagePack 首字节，并把固定长度编码与带长度前缀的编码分派给对应解码器。
func (d *msgpackDecoder) decodeValue() (any, error) {
	// fb 是当前值的格式首字节；err 表示输入已截断时的读取失败。
	fb, err := d.readByte()
	if err != nil {
		return nil, err
	}
	// fixedValue、handled、fixedErr 分别保存固定编码结果、是否命中和固定编码读取失败。
	fixedValue, handled, fixedErr := d.decodeFixedValue(fb)
	if handled {
		return fixedValue, fixedErr
	}
	if fb >= 0xe0 {
		// #nosec G115 -- negative fixint 使用二进制补码编码。
		return int64(int8(fb)), nil
	}
	return d.decodeTypedValue(fb)
}

// decodeFixedValue 解码首字节自身携带长度或数值的正整数、短 map、短数组和短字符串；handled 为 false 时交由带类型码的解码器处理。
func (d *msgpackDecoder) decodeFixedValue(fb byte) (value any, handled bool, err error) {
	switch {
	// positive fixint
	case fb <= 0x7f:
		return int64(fb), true, nil
	// fixmap
	case fb >= 0x80 && fb <= 0x8f:
		value, err = d.decodeMap(int(fb & 0x0f))
		return value, true, err
	// fixarray
	case fb >= 0x90 && fb <= 0x9f:
		value, err = d.decodeArray(int(fb & 0x0f))
		return value, true, err
	// fixstr
	case fb >= 0xa0 && fb <= 0xbf:
		value, err = d.readString(int(fb & 0x1f))
		return value, true, err
	}
	return nil, false, nil
}

// decodeTypedValue 解码带显式类型码的 nil、布尔、二进制、数值、字符串、数组和 map；未知类型码保持原有错误文本。
func (d *msgpackDecoder) decodeTypedValue(fb byte) (any, error) {
	switch fb {
	case 0xc0:
		return nil, nil
	case 0xc2:
		return false, nil
	case 0xc3:
		return true, nil
	case 0xc4, 0xc5, 0xc6:
		return d.decodeBinary(fb)
	case 0xca, 0xcb, 0xcc, 0xcd, 0xce, 0xcf, 0xd0, 0xd1, 0xd2, 0xd3:
		return d.decodeNumber(fb)
	case 0xd9, 0xda, 0xdb:
		return d.decodeLongString(fb)
	case 0xdc, 0xdd, 0xde, 0xdf:
		return d.decodeCollection(fb)
	}
	return nil, fmt.Errorf("msgpack: unknown format byte 0x%02x", fb)
}

// decodeBinary 解码带长度前缀的二进制值。
func (d *msgpackDecoder) decodeBinary(fb byte) (any, error) {
	// width 是当前二进制长度字段的字节数。
	width := map[byte]int{0xc4: 1, 0xc5: 2, 0xc6: 4}[fb]
	// length、err 保存二进制载荷长度及读取错误。
	length, err := d.readLength(width)
	if err != nil {
		return nil, err
	}
	return d.readBytes(length)
}

// decodeNumber 解码浮点数、无符号整数和有符号整数。
func (d *msgpackDecoder) decodeNumber(fb byte) (any, error) {
	// width 是当前数值载荷的字节数。
	width := map[byte]int{0xca: 4, 0xcb: 8, 0xcc: 1, 0xcd: 2, 0xce: 4, 0xcf: 8, 0xd0: 1, 0xd1: 2, 0xd2: 4, 0xd3: 8}[fb]
	// data、err 保存原始数值载荷及读取错误。
	data, err := d.readBytes(width)
	if err != nil {
		return nil, err
	}
	switch fb {
	case 0xca:
		return float64(math.Float32frombits(binary.BigEndian.Uint32(data))), nil
	case 0xcb:
		return math.Float64frombits(binary.BigEndian.Uint64(data)), nil
	case 0xcc:
		return uint64(data[0]), nil
	case 0xcd:
		return uint64(binary.BigEndian.Uint16(data)), nil
	case 0xce:
		return uint64(binary.BigEndian.Uint32(data)), nil
	case 0xcf:
		return binary.BigEndian.Uint64(data), nil
	case 0xd0:
		return int64(int8(data[0])), nil // #nosec G115 -- MessagePack 二进制补码符号扩展。
	case 0xd1:
		return int64(int16(binary.BigEndian.Uint16(data))), nil // #nosec G115 -- MessagePack 二进制补码符号扩展。
	case 0xd2:
		return int64(int32(binary.BigEndian.Uint32(data))), nil // #nosec G115 -- MessagePack 二进制补码符号扩展。
	default:
		return int64(binary.BigEndian.Uint64(data)), nil // #nosec G115 -- MessagePack 二进制补码符号扩展。
	}
}

// decodeLongString 解码带长度前缀的字符串。
func (d *msgpackDecoder) decodeLongString(fb byte) (any, error) {
	return d.decodeSized(fb, func(length int) (any, error) {
		return d.readString(length)
	})
}

// decodeCollection 解码带长度前缀的数组或 map。
func (d *msgpackDecoder) decodeCollection(fb byte) (any, error) {
	return d.decodeSized(fb, func(length int) (any, error) {
		if fb == 0xdc || fb == 0xdd {
			return d.decodeArray(length)
		}
		return d.decodeMap(length)
	})
}

// decodeSized 按格式码读取长度并交给对应的载荷解码函数。
func (d *msgpackDecoder) decodeSized(fb byte, decode func(int) (any, error)) (any, error) {
	// width 是当前格式码对应的长度前缀字节数。
	width := 1
	if fb == 0xda || fb == 0xdc || fb == 0xde {
		width = 2
	} else if fb != 0xd9 {
		width = 4
	}
	// length、err 分别是读取出的载荷长度与底层字节读取错误。
	length, err := d.readLength(width)
	if err != nil {
		return nil, err
	}
	return decode(length)
}

// readLength 读取指定宽度的无符号大端长度。
func (d *msgpackDecoder) readLength(width int) (int, error) {
	// data、err 分别是长度前缀的原始字节与读取错误。
	data, err := d.readBytes(width)
	if err != nil {
		return 0, err
	}
	switch width {
	case 1:
		return int(data[0]), nil
	case 2:
		return int(binary.BigEndian.Uint16(data)), nil
	default:
		return int(binary.BigEndian.Uint32(data)), nil
	}
}

// readString 封装readString业务协调。
func (d *msgpackDecoder) readString(n int) (string, error) {
	// b、err 用于本次流程后续判断的b、err
	b, err := d.readBytes(n)
	if err != nil {
		return "", err
	}
	return string(b), nil // UTF-8
}

// decodeArray 封装decodeArray业务协调。
func (d *msgpackDecoder) decodeArray(n int) (any, error) {
	// arr 用于本次流程后续判断的arr
	arr := make([]any, n)
	for // i 用于本次流程后续判断的i
	i := 0; i < n; i++ {
		// v、err 用于本次流程后续判断的v、err
		v, err := d.decodeValue()
		if err != nil {
			return nil, err
		}
		arr[i] = v
	}
	return arr, nil
}

// decodeMap 封装decodeMap业务协调。
func (d *msgpackDecoder) decodeMap(n int) (any, error) {
	// m 用于本次流程后续判断的m
	m := make(map[any]any, n)
	for // i 用于本次流程后续判断的i
	i := 0; i < n; i++ {
		// k、err 用于本次流程后续判断的k、err
		k, err := d.decodeValue()
		if err != nil {
			return nil, err
		}
		// v、err 用于本次流程后续判断的v、err
		v, err := d.decodeValue()
		if err != nil {
			return nil, err
		}
		m[k] = v
	}
	return m, nil
}
