package device

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
)

type Value struct {
	Type     DataType `json:"type"`
	Integer  string   `json:"integer,omitempty"`
	Boolean  bool     `json:"boolean,omitempty"`
	Real     float64  `json:"real,omitempty"`
	Text     string   `json:"text,omitempty"`
	Raw      []byte   `json:"raw,omitempty"`
	Elements []Value  `json:"elements,omitempty"`
}

func EncodeValue(a Address, v Value) ([]byte, error) {
	if err := a.Validate(); err != nil {
		return nil, err
	}
	if v.Type != "" && v.Type != a.Type {
		return nil, fmt.Errorf("value type differs from address type")
	}
	if a.Count > 1 {
		if len(v.Elements) != a.Count {
			return nil, fmt.Errorf("expected %d array elements", a.Count)
		}
		one := a
		one.Count = 1
		out := make([]byte, 0, a.ByteLength())
		for _, item := range v.Elements {
			b, e := EncodeValue(one, item)
			if e != nil {
				return nil, e
			}
			out = append(out, b...)
		}
		return out, nil
	}
	b := make([]byte, a.ByteLength())
	switch a.Type {
	case "BOOL":
		if v.Boolean {
			b[0] = 1
		}
	case "RAW":
		if len(v.Raw) != len(b) {
			return nil, fmt.Errorf("raw length mismatch")
		}
		copy(b, v.Raw)
	case "STRING":
		if len(v.Text) > a.StringLength {
			return nil, fmt.Errorf("string exceeds maximum length")
		}
		for _, c := range []byte(v.Text) {
			if c > 127 {
				return nil, fmt.Errorf("STRING supports ASCII bytes; use RAW for a PLC-specific encoding")
			}
		}
		b[0], b[1] = byte(a.StringLength), byte(len(v.Text))
		copy(b[2:], v.Text)
	case "REAL":
		if math.IsNaN(v.Real) || math.IsInf(v.Real, 0) || math.IsInf(float64(float32(v.Real)), 0) {
			return nil, fmt.Errorf("non-finite REAL")
		}
		binary.BigEndian.PutUint32(b, math.Float32bits(float32(v.Real)))
	case "LREAL":
		if math.IsNaN(v.Real) || math.IsInf(v.Real, 0) {
			return nil, fmt.Errorf("non-finite LREAL")
		}
		binary.BigEndian.PutUint64(b, math.Float64bits(v.Real))
	default:
		bits := len(b) * 8
		var n uint64
		if a.Type == "SINT" || a.Type == "INT" || a.Type == "DINT" || a.Type == "LINT" {
			s, e := strconv.ParseInt(v.Integer, 10, bits)
			if e != nil {
				return nil, e
			}
			n = uint64(s)
		} else {
			u, e := strconv.ParseUint(v.Integer, 10, bits)
			if e != nil {
				return nil, e
			}
			n = u
		}
		for i := len(b) - 1; i >= 0; i-- {
			b[i] = byte(n)
			n >>= 8
		}
	}
	return b, nil
}

func DecodeValue(a Address, b []byte) (Value, error) {
	v := Value{Type: a.Type}
	if err := a.Validate(); err != nil {
		return v, err
	}
	if len(b) != a.ByteLength() {
		return v, fmt.Errorf("expected %d bytes, received %d", a.ByteLength(), len(b))
	}
	v.Raw = append([]byte(nil), b...)
	if a.Count > 1 {
		one := a
		one.Count = 1
		step := one.ByteLength()
		for i := 0; i < a.Count; i++ {
			item, e := DecodeValue(one, b[i*step:(i+1)*step])
			if e != nil {
				return v, e
			}
			v.Elements = append(v.Elements, item)
		}
		return v, nil
	}
	switch a.Type {
	case "BOOL":
		if b[0] > 1 {
			return v, fmt.Errorf("invalid BOOL value")
		}
		v.Boolean = b[0] != 0
	case "RAW":
	case "STRING":
		if int(b[0]) != a.StringLength || b[1] > b[0] {
			return v, fmt.Errorf("invalid STRING header")
		}
		v.Text = string(b[2 : 2+int(b[1])])
	case "REAL":
		v.Real = float64(math.Float32frombits(binary.BigEndian.Uint32(b)))
		if math.IsNaN(v.Real) || math.IsInf(v.Real, 0) {
			return v, fmt.Errorf("non-finite REAL")
		}
	case "LREAL":
		v.Real = math.Float64frombits(binary.BigEndian.Uint64(b))
		if math.IsNaN(v.Real) || math.IsInf(v.Real, 0) {
			return v, fmt.Errorf("non-finite LREAL")
		}
	default:
		var n uint64
		for _, x := range b {
			n = n<<8 | uint64(x)
		}
		if a.Type == "SINT" || a.Type == "INT" || a.Type == "DINT" || a.Type == "LINT" {
			shift := 64 - len(b)*8
			v.Integer = strconv.FormatInt(int64(n<<shift)>>shift, 10)
		} else {
			v.Integer = strconv.FormatUint(n, 10)
		}
	}
	return v, nil
}
