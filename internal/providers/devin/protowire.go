package devin

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// Local protobuf wire helpers. google.golang.org/protobuf cannot be fetched in
// this environment; keep a minimal subset sufficient for Devin Connect payloads.

type WireType int8

const (
	VarintType  WireType = 0
	Fixed64Type WireType = 1
	BytesType   WireType = 2
	Fixed32Type WireType = 5
)

var errParse = errors.New("protowire: parse error")

func ParseError(n int) error {
	if n < 0 {
		return fmt.Errorf("%w (%d)", errParse, n)
	}
	if n == 0 {
		return errParse
	}
	return nil
}

func AppendVarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

func AppendTag(b []byte, num int, typ WireType) []byte {
	return AppendVarint(b, uint64(num)<<3|uint64(typ))
}

func AppendBytes(b []byte, v []byte) []byte {
	b = AppendVarint(b, uint64(len(v)))
	return append(b, v...)
}

func AppendString(b []byte, v string) []byte {
	return AppendBytes(b, []byte(v))
}

func AppendFixed64(b []byte, v uint64) []byte {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], v)
	return append(b, buf[:]...)
}

func AppendFixed32(b []byte, v uint32) []byte {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], v)
	return append(b, buf[:]...)
}

func AppendFloat64(b []byte, v float64) []byte {
	return AppendFixed64(b, math.Float64bits(v))
}

func ConsumeVarint(b []byte) (v uint64, n int) {
	var y uint64
	for i := 0; i < len(b) && i < 10; i++ {
		c := b[i]
		y |= uint64(c&0x7f) << (7 * i)
		if c < 0x80 {
			return y, i + 1
		}
	}
	return 0, -1
}

func ConsumeTag(b []byte) (num int, typ WireType, n int) {
	v, n := ConsumeVarint(b)
	if n <= 0 {
		return 0, 0, n
	}
	return int(v >> 3), WireType(v & 7), n
}

func ConsumeBytes(b []byte) (v []byte, n int) {
	size, sn := ConsumeVarint(b)
	if sn <= 0 {
		return nil, sn
	}
	if uint64(len(b)-sn) < size {
		return nil, -1
	}
	end := sn + int(size)
	return b[sn:end], end
}

func ConsumeFixed64(b []byte) (v uint64, n int) {
	if len(b) < 8 {
		return 0, -1
	}
	return binary.LittleEndian.Uint64(b[:8]), 8
}

func ConsumeFixed32(b []byte) (v uint32, n int) {
	if len(b) < 4 {
		return 0, -1
	}
	return binary.LittleEndian.Uint32(b[:4]), 4
}

func ConsumeFieldValue(num int, typ WireType, b []byte) (n int) {
	_ = num
	switch typ {
	case VarintType:
		_, n = ConsumeVarint(b)
		return n
	case Fixed64Type:
		_, n = ConsumeFixed64(b)
		return n
	case BytesType:
		_, n = ConsumeBytes(b)
		return n
	case Fixed32Type:
		_, n = ConsumeFixed32(b)
		return n
	default:
		return -1
	}
}
