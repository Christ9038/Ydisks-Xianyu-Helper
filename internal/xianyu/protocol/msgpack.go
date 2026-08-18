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
	case 0xc4: // bin 8
		// n、err 用于本次流程后续判断的n、err
		n, err := d.readByte()
		if err != nil {
			return nil, err
		}
		return d.readBytes(int(n))
	case 0xc5: // bin 16
		// b、err 用于本次流程后续判断的b、err
		b, err := d.readBytes(2)
		if err != nil {
			return nil, err
		}
		return d.readBytes(int(binary.BigEndian.Uint16(b)))
	case 0xc6: // bin 32
		// b、err 用于本次流程后续判断的b、err
		b, err := d.readBytes(4)
		if err != nil {
			return nil, err
		}
		return d.readBytes(int(binary.BigEndian.Uint32(b)))
	case 0xca: // float 32
		// b、err 用于本次流程后续判断的b、err
		b, err := d.readBytes(4)
		if err != nil {
			return nil, err
		}
		// bits 用于本次流程后续判断的bits
		bits := binary.BigEndian.Uint32(b)
		return float64(math.Float32frombits(bits)), nil
	case 0xcb: // float 64
		// b、err 用于本次流程后续判断的b、err
		b, err := d.readBytes(8)
		if err != nil {
			return nil, err
		}
		return math.Float64frombits(binary.BigEndian.Uint64(b)), nil
	case 0xcc: // uint 8
		// n、err 用于本次流程后续判断的n、err
		n, err := d.readByte()
		return uint64(n), err
	case 0xcd: // uint 16
		// b、err 用于本次流程后续判断的b、err
		b, err := d.readBytes(2)
		if err != nil {
			return nil, err
		}
		return uint64(binary.BigEndian.Uint16(b)), nil
	case 0xce: // uint 32
		// b、err 用于本次流程后续判断的b、err
		b, err := d.readBytes(4)
		if err != nil {
			return nil, err
		}
		return uint64(binary.BigEndian.Uint32(b)), nil
	case 0xcf: // uint 64
		// b、err 用于本次流程后续判断的b、err
		b, err := d.readBytes(8)
		if err != nil {
			return nil, err
		}
		return binary.BigEndian.Uint64(b), nil
	case 0xd0: // int 8
		// n、err 用于本次流程后续判断的n、err
		n, err := d.readByte()
		// #nosec G115 -- MessagePack 有符号整数使用二进制补码编码，此转换用于符号扩展。
		return int64(int8(n)), err
	case 0xd1: // int 16
		// b、err 用于本次流程后续判断的b、err
		b, err := d.readBytes(2)
		if err != nil {
			return nil, err
		}
		return int64(int16(binary.BigEndian.Uint16(b))), nil // #nosec G115 -- 协议要求的符号扩展
	case 0xd2: // int 32
		// b、err 用于本次流程后续判断的b、err
		b, err := d.readBytes(4)
		if err != nil {
			return nil, err
		}
		return int64(int32(binary.BigEndian.Uint32(b))), nil // #nosec G115 -- 协议要求的符号扩展
	case 0xd3: // int 64
		// b、err 用于本次流程后续判断的b、err
		b, err := d.readBytes(8)
		if err != nil {
			return nil, err
		}
		return int64(binary.BigEndian.Uint64(b)), nil // #nosec G115 -- 协议要求的符号扩展
	case 0xd9: // str 8
		// n、err 用于本次流程后续判断的n、err
		n, err := d.readByte()
		if err != nil {
			return nil, err
		}
		return d.readString(int(n))
	case 0xda: // str 16
		// b、err 用于本次流程后续判断的b、err
		b, err := d.readBytes(2)
		if err != nil {
			return nil, err
		}
		return d.readString(int(binary.BigEndian.Uint16(b)))
	case 0xdb: // str 32
		// b、err 用于本次流程后续判断的b、err
		b, err := d.readBytes(4)
		if err != nil {
			return nil, err
		}
		return d.readString(int(binary.BigEndian.Uint32(b)))
	case 0xdc: // array 16
		// b、err 用于本次流程后续判断的b、err
		b, err := d.readBytes(2)
		if err != nil {
			return nil, err
		}
		return d.decodeArray(int(binary.BigEndian.Uint16(b)))
	case 0xdd: // array 32
		// b、err 用于本次流程后续判断的b、err
		b, err := d.readBytes(4)
		if err != nil {
			return nil, err
		}
		return d.decodeArray(int(binary.BigEndian.Uint32(b)))
	case 0xde: // map 16
		// b、err 用于本次流程后续判断的b、err
		b, err := d.readBytes(2)
		if err != nil {
			return nil, err
		}
		return d.decodeMap(int(binary.BigEndian.Uint16(b)))
	case 0xdf: // map 32
		// b、err 用于本次流程后续判断的b、err
		b, err := d.readBytes(4)
		if err != nil {
			return nil, err
		}
		return d.decodeMap(int(binary.BigEndian.Uint32(b)))
	}
	return nil, fmt.Errorf("msgpack: unknown format byte 0x%02x", fb)
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
