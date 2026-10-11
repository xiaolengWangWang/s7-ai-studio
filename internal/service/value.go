package service

import (
	"encoding/hex"
	"fmt"
	"s7-ai-studio/internal/device"
	"strconv"
	"strings"
)

func ParseValue(a device.Address, text string) (device.Value, error) {
	v := device.Value{Type: a.Type}
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
		case "STRING":
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
	_, e := device.EncodeValue(a, v)
	return v, e
}
func FormatValue(v device.Value) string {
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
	case "REAL", "LREAL":
		return strconv.FormatFloat(v.Real, 'g', -1, 64)
	case "STRING":
		return strconv.Quote(v.Text)
	case "RAW":
		return hex.EncodeToString(v.Raw)
	default:
		return v.Integer
	}
}
