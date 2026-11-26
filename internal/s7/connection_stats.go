package s7

import (
	"context"
	"encoding/binary"
	"fmt"
	"s7-ai-studio/internal/device"
)

type ConnectionUsage struct{ PG, OS, Configured, Established, Free, SharedUsed uint16 }
type ConnectionStats struct {
	Current                *ConnectionUsage
	Max                    *uint16
	CurrentError, MaxError error
}

// ConnectionStats reads the CPU's independent connection resource records.
func (c *Client) ConnectionStats(ctx context.Context) (result ConnectionStats) {
	if err := c.lock(ctx); err != nil {
		result.CurrentError, result.MaxError = err, err
		return
	}
	var events []device.Event
	defer func() { c.finish(events) }()
	current, err := c.readConnectionSZL(ctx, 0x0132, &events)
	if err == nil {
		if len(current) < 18 {
			err = fmt.Errorf("short connection usage record")
			setEventError(&events, err)
		} else {
			word := func(i int) uint16 { return binary.BigEndian.Uint16(current[i : i+2]) }
			result.Current = &ConnectionUsage{word(6), word(8), word(10), word(12), word(14), word(16)}
		}
	}
	result.CurrentError = err
	maximum, err := c.readConnectionSZL(ctx, 0x0131, &events)
	if err == nil {
		if len(maximum) < 6 {
			err = fmt.Errorf("short connection maximum record")
			setEventError(&events, err)
		} else {
			n := binary.BigEndian.Uint16(maximum[4:6])
			result.Max = &n
		}
	}
	result.MaxError = err
	return
}

func (c *Client) readConnectionSZL(ctx context.Context, id uint16, events *[]device.Event) ([]byte, error) {
	var assembled []byte
	var seq byte
	for part := 0; part < 16; part++ {
		params := []byte{0, 1, 0x12, 4, 0x11, 0x44, 1, 0}
		data := []byte{0xff, 9, 0, 4, byte(id >> 8), byte(id), 0, 1}
		if part > 0 {
			params = []byte{0, 1, 0x12, 8, 0x12, 0x44, 1, seq, 0, 0, 0, 0}
			data = []byte{0x0a, 0, 0, 0}
		}
		ref := c.nextRef()
		req := job(ref, params, data)
		req[1] = 7
		raw, err := c.exchange(ctx, append([]byte{2, 0xf0, 0x80}, req...), "CONNECTION_STATUS", nil, events)
		if err != nil {
			return nil, err
		}
		p, err := c.response(raw, ref)
		if err == nil && (p.kind != 7 || len(p.params) != 12 || p.params[0] != 0 || p.params[1] != 1 || p.params[2] != 0x12 || p.params[3] != 8 || p.params[4] != 0x12 || p.params[5] != 0x84 || p.params[6] != 1) {
			err = fmt.Errorf("invalid SZL response parameters")
		}
		if err != nil {
			c.Close()
			setEventError(events, err)
			return nil, err
		}
		d := p.data
		code := binary.BigEndian.Uint16(p.params[10:12])
		if code != 0 || (len(d) >= 1 && d[0] != 0xff) {
			if len(d) == 0 {
				err = fmt.Errorf("SZL 0x%04x rejected: parameter error 0x%04x", id, code)
			} else {
				err = fmt.Errorf("SZL 0x%04x rejected: parameter error 0x%04x, return 0x%02x", id, code, d[0])
			}
			last := &(*events)[len(*events)-1]
			last.Status = device.StatusException
			last.Error = err.Error()
			return nil, err
		}
		if len(d) < 4 || int(binary.BigEndian.Uint16(d[2:4])) != len(d)-4 {
			err = fmt.Errorf("invalid SZL data length")
			c.Close()
			setEventError(events, err)
			return nil, err
		}
		if d[1] != 9 {
			err = fmt.Errorf("invalid SZL transport")
			c.Close()
			setEventError(events, err)
			return nil, err
		}
		payload := d[4:]
		if part == 0 {
			if len(payload) < 4 || binary.BigEndian.Uint16(payload[:2]) != id || binary.BigEndian.Uint16(payload[2:4]) != 1 {
				err = fmt.Errorf("unexpected SZL identity")
				c.Close()
				setEventError(events, err)
				return nil, err
			}
			payload = payload[4:]
		}
		assembled = append(assembled, payload...)
		if len(assembled) > 8192 {
			err = fmt.Errorf("SZL response exceeds limit")
			c.Close()
			setEventError(events, err)
			return nil, err
		}
		seq = p.params[7]
		if p.params[9] == 0 {
			if len(assembled) < 4 {
				err = fmt.Errorf("short SZL list header")
				setEventError(events, err)
				return nil, err
			}
			size, count := int(binary.BigEndian.Uint16(assembled[:2])), int(binary.BigEndian.Uint16(assembled[2:4]))
			if count != 1 || size == 0 || size*count != len(assembled)-4 || size < 2 || binary.BigEndian.Uint16(assembled[4:6]) != 1 {
				err = fmt.Errorf("invalid SZL record layout")
				setEventError(events, err)
				return nil, err
			}
			return assembled[4:], nil
		}
	}
	err := fmt.Errorf("SZL continuation limit exceeded")
	c.Close()
	setEventError(events, err)
	return nil, err
}
