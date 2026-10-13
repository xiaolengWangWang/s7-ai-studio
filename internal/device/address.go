// Package device defines protocol-independent addresses, values and observations.
package device

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Area string
type DataType string

const (
	AreaDB Area = "DB"
	AreaM  Area = "M"
	AreaI  Area = "I"
	AreaQ  Area = "Q"
)

type Address struct {
	Area         Area     `json:"area,omitempty"`
	DBNumber     uint16   `json:"db,omitempty"`
	ByteOffset   uint32   `json:"byte_offset,omitempty"`
	BitOffset    uint8    `json:"bit_offset,omitempty"`
	Type         DataType `json:"type"`
	Count        int      `json:"count"`
	StringLength int      `json:"string_length,omitempty"`
	Symbol       string   `json:"symbol,omitempty"`
	Original     string   `json:"original,omitempty"`
}

var dbAddress = regexp.MustCompile(`^DB([0-9]+)\.DB([BWDXL])([0-9]+)(?:\.([0-9]+))?$`)
var areaAddress = regexp.MustCompile(`^([MIQ])([BWDL]?)([0-9]+)(?:\.([0-9]+))?$`)
var stringType = regexp.MustCompile(`^STRING\[([0-9]+)\]$`)

// viSCADA 物模型的 S7 属性标识（telegraf s7comm 写法），如 DB1.R0、DB1.X52.0：类型由标识决定。
var platformAddress = regexp.MustCompile(`^DB([0-9]+)\.(X|B|W|DW|I|DI|R)([0-9]+)(?:\.([0-9]+))?$`)
var platformTypes = map[string]DataType{"X": "BOOL", "B": "BYTE", "W": "WORD", "DW": "DWORD", "I": "INT", "DI": "DINT", "R": "REAL"}

// PlatformType 返回 viSCADA 属性标识隐含的类型；不是该写法时返回 false。
func PlatformType(text string) (DataType, bool) {
	m := platformAddress.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(text)))
	if m == nil {
		return "", false
	}
	return platformTypes[m[2]], true
}

func TypeSize(kind DataType) int {
	switch kind {
	case "BOOL", "BYTE", "SINT", "USINT", "RAW":
		return 1
	case "WORD", "INT", "UINT":
		return 2
	case "DWORD", "DINT", "UDINT", "REAL":
		return 4
	case "LWORD", "LINT", "ULINT", "LREAL":
		return 8
	}
	return 0
}

func (a Address) ByteLength() int {
	if a.Type == "STRING" {
		return (a.StringLength + 2) * a.Count
	}
	return TypeSize(a.Type) * a.Count
}

func (a Address) Validate() error {
	if a.Count < 1 || a.Count > 65535 {
		return fmt.Errorf("invalid element count %d", a.Count)
	}
	if a.Type == "STRING" {
		if a.StringLength < 1 || a.StringLength > 254 || a.Count != 1 {
			return fmt.Errorf("STRING requires a maximum length of 1..254 and count 1")
		}
	} else if TypeSize(a.Type) == 0 {
		return fmt.Errorf("unsupported type %q", a.Type)
	}
	if a.Type == "BOOL" && a.Count != 1 {
		return fmt.Errorf("BOOL requires count 1; use separate bit addresses")
	}
	if a.ByteLength() < 1 || a.ByteLength() > 65535 {
		return fmt.Errorf("value exceeds 65535 bytes")
	}
	if a.BitOffset > 7 || (a.Type != "BOOL" && a.BitOffset != 0) {
		return fmt.Errorf("invalid bit offset")
	}
	if a.Symbol != "" {
		if a.Area != "" || a.DBNumber != 0 || a.ByteOffset != 0 || a.BitOffset != 0 {
			return fmt.Errorf("symbol and absolute address are mutually exclusive")
		}
		return nil
	}
	if a.Area != AreaDB && a.Area != AreaM && a.Area != AreaI && a.Area != AreaQ {
		return fmt.Errorf("unsupported area %q", a.Area)
	}
	if a.Area != AreaDB && a.DBNumber != 0 {
		return fmt.Errorf("DB number requires DB area")
	}
	if uint64(a.ByteOffset)+uint64(a.ByteLength()) > 1<<21 {
		return fmt.Errorf("address exceeds the 24-bit S7 bit address range")
	}
	return nil
}

func ParseAddress(text string, kind DataType, count int) (Address, error) {
	a := Address{Type: DataType(strings.ToUpper(string(kind))), Count: count, Original: strings.TrimSpace(text)}
	if m := stringType.FindStringSubmatch(string(a.Type)); m != nil {
		n, e := strconv.ParseUint(m[1], 10, 16)
		if e != nil {
			return a, e
		}
		a.Type = "STRING"
		a.StringLength = int(n)
	}
	if strings.HasPrefix(strings.ToLower(a.Original), "symbol:") {
		a.Symbol = strings.TrimSpace(a.Original[7:])
		if a.Symbol == "" {
			return a, fmt.Errorf("empty symbol")
		}
		return a, a.Validate()
	}
	t := strings.ToUpper(a.Original)
	var off, bit, suffix string
	if m := dbAddress.FindStringSubmatch(t); m != nil {
		n, e := strconv.ParseUint(m[1], 10, 16)
		if e != nil {
			return a, fmt.Errorf("invalid DB number: %w", e)
		}
		a.Area = AreaDB
		a.DBNumber = uint16(n)
		suffix, off, bit = m[2], m[3], m[4]
	} else if m := platformAddress.FindStringSubmatch(t); m != nil {
		n, e := strconv.ParseUint(m[1], 10, 16)
		if e != nil {
			return a, fmt.Errorf("invalid DB number: %w", e)
		}
		if want := platformTypes[m[2]]; a.Type != want {
			return a, fmt.Errorf("viSCADA 标识 %s 对应 %s，不能按 %s 读写", a.Original, want, a.Type)
		}
		a.Area = AreaDB
		a.DBNumber = uint16(n)
		off, bit = m[3], m[4]
		if m[2] == "X" {
			suffix = "X"
		}
	} else if m := areaAddress.FindStringSubmatch(t); m != nil {
		a.Area = Area(m[1])
		suffix, off, bit = m[2], m[3], m[4]
	} else {
		return a, fmt.Errorf("invalid absolute address %q", text)
	}
	n, e := strconv.ParseUint(off, 10, 32)
	if e != nil {
		return a, fmt.Errorf("invalid byte offset: %w", e)
	}
	a.ByteOffset = uint32(n)
	if bit != "" {
		n, e = strconv.ParseUint(bit, 10, 8)
		if e != nil {
			return a, e
		}
		a.BitOffset = uint8(n)
		if a.Type != "BOOL" {
			return a, fmt.Errorf("bit address requires BOOL")
		}
	}
	if suffix == "X" && (bit == "" || a.Type != "BOOL") {
		return a, fmt.Errorf("DBX requires BOOL and a bit index")
	}
	if suffix == "W" && TypeSize(a.Type) != 2 || suffix == "D" && TypeSize(a.Type) != 4 || suffix == "L" && TypeSize(a.Type) != 8 {
		return a, fmt.Errorf("address width and type disagree")
	}
	return a, a.Validate()
}

func (a Address) String() string {
	if a.Symbol != "" {
		return "symbol:" + a.Symbol
	}
	if a.Area == AreaDB {
		if a.Type == "BOOL" {
			return fmt.Sprintf("DB%d.DBX%d.%d", a.DBNumber, a.ByteOffset, a.BitOffset)
		}
		return fmt.Sprintf("DB%d.DBB%d", a.DBNumber, a.ByteOffset)
	}
	if a.Type == "BOOL" {
		return fmt.Sprintf("%s%d.%d", a.Area, a.ByteOffset, a.BitOffset)
	}
	return fmt.Sprintf("%sB%d", a.Area, a.ByteOffset)
}
