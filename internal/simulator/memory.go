package simulator

import (
	"fmt"
	"s7-ai-studio/internal/device"
	"sync"
)

type memoryKey struct {
	area device.Area
	db   uint16
}
type Memory struct {
	mu      sync.RWMutex
	regions map[memoryKey][]byte
}

func NewMemory() *Memory {
	m := &Memory{regions: make(map[memoryKey][]byte)}
	for _, area := range []device.Area{"M", "I", "Q"} {
		m.regions[memoryKey{area: area}] = make([]byte, 4096)
	}
	for _, db := range []uint16{1, 2} {
		m.regions[memoryKey{area: "DB", db: db}] = make([]byte, 4096)
	}
	return m
}
func (m *Memory) Get(a device.Address) ([]byte, error) {
	if e := a.Validate(); e != nil {
		return nil, e
	}
	if a.Symbol != "" {
		return nil, fmt.Errorf("simulator has no symbol store")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	r := m.regions[memoryKey{a.Area, a.DBNumber}]
	start, end := int(a.ByteOffset), int(a.ByteOffset)+a.ByteLength()
	if end > len(r) {
		return nil, fmt.Errorf("address is outside configured simulator memory")
	}
	if a.Type == "BOOL" {
		return []byte{(r[start] >> a.BitOffset) & 1}, nil
	}
	return append([]byte(nil), r[start:end]...), nil
}
func (m *Memory) Set(a device.Address, raw []byte) error { return m.set(a, raw, true) }
func (m *Memory) set(a device.Address, raw []byte, grow bool) error {
	if e := a.Validate(); e != nil {
		return e
	}
	if a.Symbol != "" || len(raw) != a.ByteLength() {
		return fmt.Errorf("memory address or length mismatch")
	}
	if a.Type == "BOOL" && raw[0] > 1 {
		return fmt.Errorf("invalid bit value")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := memoryKey{a.Area, a.DBNumber}
	r := m.regions[key]
	start, end := int(a.ByteOffset), int(a.ByteOffset)+len(raw)
	if end > len(r) {
		if !grow {
			return fmt.Errorf("address is outside configured simulator memory")
		}
		if m.regions == nil {
			m.regions = make(map[memoryKey][]byte)
		}
		if len(m.regions) >= 256 && r == nil {
			return fmt.Errorf("too many simulator regions")
		}
		r = append(r, make([]byte, end-len(r))...)
		m.regions[key] = r
	}
	if a.Type == "BOOL" {
		mask := byte(1 << a.BitOffset)
		if raw[0] != 0 {
			r[start] |= mask
		} else {
			r[start] &^= mask
		}
	} else {
		copy(r[start:end], raw)
	}
	return nil
}
