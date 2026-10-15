package device

import (
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ParseValue 把界面或点表里的文字转成该地址类型的值；数组用逗号分隔。
func ParseValue(a Address, text string) (Value, error) {
	v := Value{Type: a.Type}
	if a.Count > 1 {
		one := a
		one.Count = 1
		for _, part := range strings.Split(text, ",") {
			item, e := ParseValue(one, strings.TrimSpace(part))
			if e != nil {
				return v, e
			}
			v.Elements = append(v.Elements, item)
		}
	} else {
		var e error
		switch a.Type {
		case "BOOL":
			v.Boolean, e = strconv.ParseBool(strings.TrimSpace(text))
		case "REAL", "LREAL":
			v.Real, e = strconv.ParseFloat(strings.TrimSpace(text), 64)
		case "STRING", "CHAR":
			v.Text = text
		case "RAW":
			v.Raw, e = hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(text), "0x"))
		default:
			v.Integer = strings.TrimSpace(text)
		}
		if e != nil {
			return v, fmt.Errorf("无效写入值: %w", e)
		}
	}
	_, e := EncodeValue(a, v)
	return v, e
}

func FormatValue(v Value) string {
	if len(v.Elements) > 0 {
		parts := make([]string, len(v.Elements))
		for i, x := range v.Elements {
			parts[i] = FormatValue(x)
		}
		return strings.Join(parts, ",")
	}
	switch v.Type {
	case "BOOL":
		return strconv.FormatBool(v.Boolean)
	case "REAL": // 按 32 位取最短写法：0.6 而不是 0.6000000238418579
		return formatFloat(v.Real, 32)
	case "LREAL":
		return formatFloat(v.Real, 64)
	case "STRING", "CHAR":
		return strconv.Quote(v.Text)
	case "RAW":
		return hex.EncodeToString(v.Raw)
	default:
		return v.Integer
	}
}

// formatFloat 常见量程用普通小数（4200000），过大过小才用指数。
func formatFloat(f float64, bits int) string {
	if a := math.Abs(f); a == 0 || a >= 1e-4 && a < 1e15 {
		return strconv.FormatFloat(f, 'f', -1, bits)
	}
	return strconv.FormatFloat(f, 'g', -1, bits)
}
