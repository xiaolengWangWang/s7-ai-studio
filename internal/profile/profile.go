// Package profile provides connection suggestions, never proof of CPU identity.
package profile

import (
	"fmt"
	"regexp"
	"s7-ai-studio/internal/device"
	"slices"
	"strconv"
	"strings"
)

type Mapping struct {
	DB         uint16
	BaseOffset uint32
}
type Profile struct {
	Family     string
	Routes     []string
	Rack, Slot int
	NeedsTSAP  bool
	VMapping   *Mapping
	Verified   bool
}

func Lookup(family string) (Profile, error) {
	family = strings.ToLower(strings.TrimSpace(family))
	p := Profile{Family: family, Routes: []string{"s7-tcp"}, Slot: 1}
	switch family {
	case "s7-300":
		p.Slot = 2
	case "s7-1200", "s7-1200-g2", "s7-1500", "et-200sp", "et-200pro", "software-controller", "open-controller", "drive-controller":
		p.Routes = append(p.Routes, "opcua")
	case "s7-200", "s7-200-cn":
		p.Routes = append(p.Routes, "ppi")
		p.NeedsTSAP = true
	case "s7-200-smart", "s7-200-smart-g2", "s7-400", "et-200s", "et-200x", "winac", "siplus", "logo":
		p.NeedsTSAP = true
	case "s5":
		p.Routes = []string{"s5", "gateway"}
		p.NeedsTSAP = true
	case "logo-legacy":
		p.Routes = nil
		p.NeedsTSAP = true
	default:
		return p, fmt.Errorf("unknown PLC family %q", family)
	}
	return p, nil
}

func ConnectionTSAP(p Profile, rack, slot int, local, remote *uint16) (uint16, uint16, error) {
	if !slices.Contains(p.Routes, "s7-tcp") {
		return 0, 0, fmt.Errorf("%s has no direct S7 TCP route", p.Family)
	}
	l := uint16(0x100)
	if local != nil {
		l = *local
	}
	if remote != nil {
		return l, *remote, nil
	}
	if p.NeedsTSAP {
		return 0, 0, fmt.Errorf("%s requires an explicit remote TSAP from the PLC or gateway configuration", p.Family)
	}
	if rack < 0 || rack > 7 || slot < 0 || slot > 31 {
		return 0, 0, fmt.Errorf("rack must be 0..7 and slot 0..31")
	}
	return l, uint16(0x100 | rack<<5 | slot), nil
}

var vAddress = regexp.MustCompile(`^V([BWDX]?)([0-9]+)(?:\.([0-9]+))?$`)

func ResolveAddress(p Profile, text string, kind device.DataType, count int) (device.Address, error) {
	input := strings.TrimSpace(text)
	m := vAddress.FindStringSubmatch(strings.ToUpper(input))
	if m == nil {
		return device.ParseAddress(input, kind, count)
	}
	if p.VMapping == nil {
		return device.Address{}, fmt.Errorf("%s requires an explicit V/VM-to-DB mapping", p.Family)
	}
	if p.Family != "s7-200" && p.Family != "s7-200-cn" && p.Family != "s7-200-smart" && p.Family != "s7-200-smart-g2" && p.Family != "logo" {
		return device.Address{}, fmt.Errorf("%s does not expose V/VM addresses", p.Family)
	}
	offset, e := strconv.ParseUint(m[2], 10, 32)
	if e != nil {
		return device.Address{}, e
	}
	offset += uint64(p.VMapping.BaseOffset)
	if offset > 1<<21-1 {
		return device.Address{}, fmt.Errorf("mapped offset exceeds S7 address range")
	}
	suffix := m[1]
	if suffix == "" {
		suffix = "B"
	}
	if m[3] != "" {
		suffix = "X"
	}
	address := fmt.Sprintf("DB%d.DB%s%d", p.VMapping.DB, suffix, offset)
	if m[3] != "" {
		address += "." + m[3]
	}
	a, e := device.ParseAddress(address, kind, count)
	a.Original = input
	return a, e
}
