// Package service owns the desktop session and its cancellable operations.
package service

import (
	"fmt"
	"net"
	"s7-ai-studio/internal/device"
	"s7-ai-studio/internal/profile"
	"s7-ai-studio/internal/s7"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Family, Target        string
	Rack, Slot            int
	LocalTSAP, RemoteTSAP string
	Timeout               time.Duration
	VDB                   int
	VOffset               uint32
}
type Point struct {
	Address string
	Type    device.DataType
	Count   int
}
type WriteRequest struct {
	Point      Point
	Value      string
	Generation uint64
}

func DefaultConfig() Config {
	return Config{Family: "s7-300", Rack: -1, Slot: -1, Timeout: 2 * time.Second}
}
func (cfg Config) validate() (profile.Profile, s7.Options, error) {
	p, e := profile.Lookup(cfg.Family)
	o := s7.DefaultOptions()
	if e != nil {
		return p, o, e
	}
	host, port, e := net.SplitHostPort(strings.TrimSpace(cfg.Target))
	if e != nil || strings.TrimSpace(host) == "" {
		return p, o, fmt.Errorf("目标须为明确的 host:port")
	}
	n, e := strconv.Atoi(port)
	if e != nil || n < 1 || n > 65535 {
		return p, o, fmt.Errorf("端口须为 1..65535")
	}
	if cfg.Timeout <= 0 || cfg.Timeout > 5*time.Minute {
		return p, o, fmt.Errorf("超时须大于零且不超过 5 分钟")
	}
	rack, slot := cfg.Rack, cfg.Slot
	if rack == -1 {
		rack = p.Rack
	}
	if slot == -1 {
		slot = p.Slot
	}
	if rack < 0 || rack > 7 || slot < 0 || slot > 31 {
		return p, o, fmt.Errorf("rack 须为 0..7，slot 须为 0..31")
	}
	parse := func(text string) (*uint16, error) {
		if strings.TrimSpace(text) == "" {
			return nil, nil
		}
		n, e := strconv.ParseUint(strings.TrimSpace(text), 0, 16)
		v := uint16(n)
		return &v, e
	}
	l, e := parse(cfg.LocalTSAP)
	if e != nil {
		return p, o, fmt.Errorf("local TSAP: %w", e)
	}
	r, e := parse(cfg.RemoteTSAP)
	if e != nil {
		return p, o, fmt.Errorf("remote TSAP: %w", e)
	}
	o.LocalTSAP, o.RemoteTSAP, e = profile.ConnectionTSAP(p, rack, slot, l, r)
	if e != nil {
		return p, o, e
	}
	o.Timeout = cfg.Timeout
	o.ReadRetries = 0
	if cfg.VDB < 0 || cfg.VDB > 65535 || cfg.VOffset > 1<<21-1 || (cfg.VDB == 0 && cfg.VOffset != 0) {
		return p, o, fmt.Errorf("V 映射须提供有效 DB 与偏移")
	}
	if cfg.VDB > 0 {
		p.VMapping = &profile.Mapping{DB: uint16(cfg.VDB), BaseOffset: cfg.VOffset}
	}
	return p, o, nil
}
func resolve(p profile.Profile, points []Point) ([]device.Address, error) {
	if len(points) < 1 || len(points) > 1024 {
		return nil, fmt.Errorf("请输入 1..1024 个点位")
	}
	aa := make([]device.Address, len(points))
	for i, x := range points {
		a, e := profile.ResolveAddress(p, x.Address, x.Type, x.Count)
		if e != nil {
			return nil, e
		}
		if a.Symbol != "" {
			return nil, fmt.Errorf("本版只支持绝对地址")
		}
		aa[i] = a
	}
	return aa, nil
}
