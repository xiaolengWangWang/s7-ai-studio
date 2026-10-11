package s7

import (
	"context"
	"fmt"
	"s7-ai-studio/internal/device"
	"time"
)

type readPart struct {
	address       device.Address
	owner, offset int
}

func splitAddress(a device.Address, maxBytes int) []readPart {
	if a.ByteLength() <= maxBytes {
		return []readPart{{address: a}}
	}
	stride := device.TypeSize(a.Type)
	if stride < 1 {
		stride = 1
	}
	maxBytes = maxBytes / stride * stride
	parts := []readPart{}
	for off := 0; off < a.ByteLength(); {
		n := min(maxBytes, a.ByteLength()-off)
		part := a
		part.ByteOffset += uint32(off)
		part.Type = "RAW"
		part.StringLength = 0
		part.Count = n
		parts = append(parts, readPart{address: part, offset: off})
		off += n
	}
	return parts
}
func (c *Client) Read(ctx context.Context, addresses []device.Address) ([]device.ReadResult, error) {
	if len(addresses) < 1 || len(addresses) > 1024 {
		return nil, fmt.Errorf("read requires 1..1024 addresses")
	}
	var parts []readPart
	total := 0
	for owner, a := range addresses {
		if e := a.Validate(); e != nil {
			return nil, e
		}
		if a.Symbol != "" {
			return nil, fmt.Errorf("symbolic access requires another driver")
		}
		total += a.ByteLength()
		if total > 16<<20 {
			return nil, fmt.Errorf("read exceeds 16 MiB result limit")
		}
		for _, part := range splitAddress(a, c.pduLength-19) {
			part.owner = owner
			parts = append(parts, part)
		}
	}
	if e := c.lock(ctx); e != nil {
		return nil, e
	}
	events := []device.Event{}
	defer func() { c.finish(events) }()
	results := make([]device.ReadResult, len(addresses))
	raws := make([][]byte, len(addresses))
	for i, a := range addresses {
		results[i] = device.ReadResult{Address: a, Quality: "UNKNOWN", Time: time.Now().UTC()}
		raws[i] = make([]byte, a.ByteLength())
	}
	for start := 0; start < len(parts); {
		end := start
		responseBytes := 14
		for end < len(parts) && end-start < 20 {
			n := parts[end].address.ByteLength()
			if 12+12*(end-start+1) > c.pduLength || responseBytes+4+n+n%2 > c.pduLength {
				break
			}
			responseBytes += 4 + n + n%2
			end++
		}
		if end == start {
			return results, fmt.Errorf("negotiated PDU cannot carry this item")
		}
		batch := make([]device.Address, end-start)
		for i := range batch {
			batch[i] = parts[start+i].address
		}
		ref := c.nextRef()
		req, e := encodeRead(ref, batch)
		if e != nil {
			return results, e
		}
		response, e := c.exchange(ctx, append([]byte{2, 0xf0, 0x80}, req...), "READ", batch, &events)
		if e != nil {
			return markUnfinished(results, e), e
		}
		if _, e = c.response(response, ref); e != nil {
			c.Close()
			setEventError(&events, e)
			return markUnfinished(results, e), e
		}
		got, e := decodeRead(response[3:], batch)
		if e != nil {
			c.Close()
			setEventError(&events, e)
			return markUnfinished(results, e), e
		}
		last := &events[len(events)-1]
		for i, item := range got {
			last.Results = append(last.Results, device.EventResult{Read: &got[i]})
			part := parts[start+i]
			owner := part.owner
			if item.Quality != "GOOD" {
				results[owner].Quality = "BAD"
				results[owner].Error = item.Error
				last.Status = device.StatusException
			} else {
				copy(raws[owner][part.offset:], item.Value.Raw)
			}
		}
		start = end
	}
	for i := range results {
		if results[i].Quality == "BAD" {
			continue
		}
		v, e := device.DecodeValue(addresses[i], raws[i])
		results[i].Time = time.Now().UTC()
		if e != nil {
			results[i].Quality = "BAD"
			results[i].Error = e.Error()
		} else {
			results[i].Quality = "GOOD"
			results[i].Value = v
		}
	}
	return results, nil
}
func markUnfinished(r []device.ReadResult, e error) []device.ReadResult {
	for i := range r {
		if r[i].Quality == "UNKNOWN" {
			r[i].Error = e.Error()
		}
	}
	return r
}

func (c *Client) Write(ctx context.Context, a device.Address, v device.Value) (device.WriteResult, error) {
	result := device.WriteResult{Address: a, State: "REJECTED"}
	raw, e := device.EncodeValue(a, v)
	if e != nil {
		result.Error = e.Error()
		return result, e
	}
	if a.Symbol != "" {
		e = fmt.Errorf("symbolic writes require another driver")
		result.Error = e.Error()
		return result, e
	}
	parts := splitAddress(a, c.pduLength-28)
	if e = c.lock(ctx); e != nil {
		result.Error = e.Error()
		return result, e
	}
	events := []device.Event{}
	defer func() { c.finish(events) }()
	for index, part := range parts {
		ref := c.nextRef()
		req, e := encodeWrite(ref, part.address, raw[part.offset:part.offset+part.address.ByteLength()])
		if e != nil {
			result.Error = e.Error()
			return result, e
		}
		if e = ctx.Err(); e != nil {
			result.Error = e.Error()
			if index > 0 {
				result.State = "UNKNOWN"
			}
			return result, e
		}
		response, e := c.exchange(ctx, append([]byte{2, 0xf0, 0x80}, req...), "WRITE", []device.Address{part.address}, &events)
		if e != nil {
			result.State = "UNKNOWN"
			result.Error = e.Error()
			result.Segments = append(result.Segments, device.WriteSegment{Address: part.address, State: "UNKNOWN", Error: e.Error()})
			return result, e
		}
		p, e := c.response(response, ref)
		if e != nil || p.kind != 3 || len(p.params) != 2 || p.params[0] != 5 || p.params[1] != 1 || len(p.data) != 1 {
			c.Close()
			if e == nil {
				e = fmt.Errorf("invalid write response")
			}
			setEventError(&events, e)
			result.State = "UNKNOWN"
			result.Error = e.Error()
			result.Segments = append(result.Segments, device.WriteSegment{Address: part.address, State: "UNKNOWN", Error: e.Error()})
			return result, e
		}
		segment := device.WriteResult{Address: part.address, State: "CONFIRMED"}
		if p.data[0] != 0xff {
			segment.State = "REJECTED"
			segment.Error = fmt.Sprintf("S7 write item error 0x%02x", p.data[0])
			events[len(events)-1].Status = device.StatusException
		}
		events[len(events)-1].Results = []device.EventResult{{Write: &segment}}
		result.Segments = append(result.Segments, device.WriteSegment{Address: segment.Address, State: segment.State, Error: segment.Error})
		if segment.State != "CONFIRMED" {
			result.Error = segment.Error
			if index > 0 {
				result.State = "UNKNOWN"
			}
			return result, fmt.Errorf("%s", segment.Error)
		}
	}
	result.State = "CONFIRMED"
	return result, nil
}
