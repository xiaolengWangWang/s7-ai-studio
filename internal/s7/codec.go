package s7

import (
	"encoding/binary"
	"fmt"
	"s7-ai-studio/internal/device"
	"time"
)

type pdu struct {
	kind         byte
	reference    uint16
	params, data []byte
}

func parsePDU(b []byte, reference uint16) (pdu, error) {
	var p pdu
	if len(b) < 10 || b[0] != 0x32 || b[2] != 0 || b[3] != 0 {
		return p, fmt.Errorf("invalid S7 header")
	}
	p.kind = b[1]
	head := 10
	if p.kind == 3 {
		head = 12
	} else if p.kind != 1 {
		return p, fmt.Errorf("unsupported S7 message type %02x", p.kind)
	}
	if len(b) < head {
		return p, fmt.Errorf("short S7 header")
	}
	p.reference = binary.BigEndian.Uint16(b[4:6])
	if reference != 0 && p.reference != reference {
		return p, fmt.Errorf("unexpected S7 request reference")
	}
	par, data := int(binary.BigEndian.Uint16(b[6:8])), int(binary.BigEndian.Uint16(b[8:10]))
	if head+par+data != len(b) {
		return p, fmt.Errorf("S7 parameter/data length mismatch")
	}
	if head == 12 && (b[10] != 0 || b[11] != 0) {
		return p, fmt.Errorf("S7 global error %02x%02x", b[10], b[11])
	}
	p.params = b[head : head+par]
	p.data = b[head+par:]
	return p, nil
}
func job(reference uint16, params, data []byte) []byte {
	b := make([]byte, 10+len(params)+len(data))
	b[0], b[1] = 0x32, 1
	binary.BigEndian.PutUint16(b[4:6], reference)
	binary.BigEndian.PutUint16(b[6:8], uint16(len(params)))
	binary.BigEndian.PutUint16(b[8:10], uint16(len(data)))
	copy(b[10:], params)
	copy(b[10+len(params):], data)
	return b
}
func encodeSetup(reference uint16, size uint16) ([]byte, error) {
	if size < 64 || size > 960 {
		return nil, fmt.Errorf("requested PDU must be 64..960 bytes")
	}
	p := []byte{0xf0, 0, 0, 1, 0, 1, 0, 0}
	binary.BigEndian.PutUint16(p[6:], size)
	return job(reference, p, nil), nil
}
func encodeItem(a device.Address) ([]byte, error) {
	if e := a.Validate(); e != nil {
		return nil, e
	}
	if a.Symbol != "" {
		return nil, fmt.Errorf("classic S7 does not support symbolic addresses")
	}
	b := []byte{0x12, 0x0a, 0x10, 2, 0, 0, 0, 0, 0, 0, 0, 0}
	n := a.ByteLength()
	if a.Type == "BOOL" {
		b[3] = 1
		n = 1
	}
	binary.BigEndian.PutUint16(b[4:6], uint16(n))
	binary.BigEndian.PutUint16(b[6:8], a.DBNumber)
	switch a.Area {
	case device.AreaDB:
		b[8] = 0x84
	case device.AreaM:
		b[8] = 0x83
	case device.AreaI:
		b[8] = 0x81
	case device.AreaQ:
		b[8] = 0x82
	}
	bit := a.ByteOffset*8 + uint32(a.BitOffset)
	b[9], b[10], b[11] = byte(bit>>16), byte(bit>>8), byte(bit)
	return b, nil
}
func encodeRead(reference uint16, addresses []device.Address) ([]byte, error) {
	if len(addresses) < 1 || len(addresses) > 20 {
		return nil, fmt.Errorf("read requires 1..20 items")
	}
	p := []byte{4, byte(len(addresses))}
	for _, a := range addresses {
		b, e := encodeItem(a)
		if e != nil {
			return nil, e
		}
		p = append(p, b...)
	}
	return job(reference, p, nil), nil
}
func encodeWrite(reference uint16, a device.Address, raw []byte) ([]byte, error) {
	item, e := encodeItem(a)
	if e != nil {
		return nil, e
	}
	if len(raw) != a.ByteLength() || len(raw) > 8191 {
		return nil, fmt.Errorf("write size mismatch")
	}
	params := append([]byte{5, 1}, item...)
	data := make([]byte, 4+len(raw))
	data[1] = 4
	n := len(raw) * 8
	if a.Type == "BOOL" {
		data[1] = 3
		n = 1
	}
	binary.BigEndian.PutUint16(data[2:4], uint16(n))
	copy(data[4:], raw)
	return job(reference, params, data), nil
}
func decodeRead(b []byte, addresses []device.Address) ([]device.ReadResult, error) {
	p, e := parsePDU(b, 0)
	if e != nil {
		return nil, e
	}
	if p.kind != 3 || len(p.params) != 2 || p.params[0] != 4 || int(p.params[1]) != len(addresses) {
		return nil, fmt.Errorf("read response item count or function mismatch")
	}
	r := make([]device.ReadResult, len(addresses))
	offset := 0
	for i, a := range addresses {
		r[i] = device.ReadResult{Address: a, Quality: "BAD", Time: time.Now().UTC()}
		if len(p.data)-offset < 4 {
			return nil, fmt.Errorf("short read item")
		}
		d := p.data[offset:]
		ret, transport := d[0], d[1]
		n := int(binary.BigEndian.Uint16(d[2:4]))
		offset += 4
		if ret != 0xff {
			if transport != 0 || n != 0 {
				return nil, fmt.Errorf("invalid rejected read item")
			}
			r[i].Error = fmt.Sprintf("S7 item error 0x%02x", ret)
			continue
		}
		bytes := 0
		switch transport {
		case 3:
			if a.Type != "BOOL" || n != 1 {
				return nil, fmt.Errorf("unexpected bit transport")
			}
			bytes = 1
		case 4, 5, 6, 7:
			if n%8 != 0 {
				return nil, fmt.Errorf("unaligned byte transport")
			}
			bytes = n / 8
		case 9:
			bytes = n
		default:
			return nil, fmt.Errorf("unsupported response transport %02x", transport)
		}
		if bytes != a.ByteLength() || bytes > len(p.data)-offset {
			return nil, fmt.Errorf("read item length mismatch")
		}
		v, err := device.DecodeValue(a, p.data[offset:offset+bytes])
		if err != nil {
			r[i].Error = err.Error()
		} else {
			r[i].Quality = "GOOD"
			r[i].Value = v
		}
		offset += bytes
		if i < len(addresses)-1 && bytes%2 != 0 {
			if offset >= len(p.data) || p.data[offset] != 0 {
				return nil, fmt.Errorf("missing item padding")
			}
			offset++
		}
	}
	if offset != len(p.data) && !(offset+1 == len(p.data) && p.data[offset] == 0) {
		return nil, fmt.Errorf("extra read data")
	}
	return r, nil
}
