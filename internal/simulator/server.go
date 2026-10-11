// Package simulator provides a synthetic classic S7 peer, not a Siemens CPU emulator.
package simulator

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"s7-ai-studio/internal/device"
	"s7-ai-studio/internal/s7"
	"sync"
	"sync/atomic"
	"time"
)

type Options struct {
	PDULimit int
	Memory   *Memory
	Fault    Fault
}
type Stats struct{ Reads, Writes uint64 }
type Server struct {
	opts          Options
	mu            sync.Mutex
	listener      net.Listener
	connections   map[net.Conn]bool
	done          chan struct{}
	once          sync.Once
	wg            sync.WaitGroup
	started       bool
	reads, writes atomic.Uint64
}

func New(opts Options) (*Server, error) {
	if opts.PDULimit == 0 {
		opts.PDULimit = 480
	}
	if opts.PDULimit < 64 || opts.PDULimit > 960 {
		return nil, fmt.Errorf("PDU limit must be 64..960")
	}
	if opts.Fault.Delay < 0 {
		return nil, fmt.Errorf("negative fault delay")
	}
	a := opts.Fault.RejectArea
	if a != "" && a != "DB" && a != "M" && a != "I" && a != "Q" {
		return nil, fmt.Errorf("invalid rejected area")
	}
	if opts.Memory == nil {
		opts.Memory = NewMemory()
	}
	return &Server{opts: opts, connections: make(map[net.Conn]bool), done: make(chan struct{})}, nil
}
func (s *Server) Stats() Stats { return Stats{s.reads.Load(), s.writes.Load()} }
func (s *Server) Close() error {
	s.once.Do(func() {
		s.mu.Lock()
		close(s.done)
		if s.listener != nil {
			s.listener.Close()
		}
		for c := range s.connections {
			c.Close()
		}
		s.mu.Unlock()
	})
	s.wg.Wait()
	return nil
}
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	s.mu.Lock()
	select {
	case <-s.done:
		s.mu.Unlock()
		listener.Close()
		return fmt.Errorf("simulator closed")
	default:
	}
	if s.started {
		s.mu.Unlock()
		return fmt.Errorf("simulator Serve already started")
	}
	s.started = true
	s.listener = listener
	s.mu.Unlock()
	stop := context.AfterFunc(ctx, func() { s.Close() })
	defer stop()
	defer s.Close()
	for {
		c, e := listener.Accept()
		if e != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			select {
			case <-s.done:
				return nil
			default:
				return e
			}
		}
		s.mu.Lock()
		select {
		case <-s.done:
			s.mu.Unlock()
			c.Close()
			return nil
		default:
		}
		s.connections[c] = true
		s.wg.Add(1)
		s.mu.Unlock()
		go func() {
			defer s.wg.Done()
			defer func() { c.Close(); s.mu.Lock(); delete(s.connections, c); s.mu.Unlock() }()
			s.handle(c)
		}()
	}
}
func (s *Server) handle(c net.Conn) {
	c.SetDeadline(time.Now().Add(30 * time.Second))
	cr, e := s7.ReadTPKT(c, 1028)
	if e != nil || len(cr) != 18 || cr[0] != 17 || cr[1] != 0xe0 || cr[6] != 0 || cr[7] != 0xc1 || cr[8] != 2 || cr[11] != 0xc2 || cr[12] != 2 || cr[15] != 0xc0 || cr[16] != 1 || cr[17] != 10 {
		return
	}
	cc := []byte{17, 0xd0, cr[4], cr[5], 0, 1, 0, 0xc1, 2, cr[13], cr[14], 0xc2, 2, cr[9], cr[10], 0xc0, 1, 10}
	if s7.WriteTPKT(c, cc) != nil {
		return
	}
	negotiated := 0
	for {
		c.SetDeadline(time.Now().Add(30 * time.Second))
		frame, e := s7.ReadTPKT(c, 1028)
		if e != nil {
			return
		}
		if len(frame) < 13 || frame[0] != 2 || frame[1] != 0xf0 || frame[2] != 0x80 {
			return
		}
		job := frame[3:]
		if len(job) < 10 || job[0] != 0x32 || job[1] != 1 || job[2] != 0 || job[3] != 0 {
			return
		}
		ref := binary.BigEndian.Uint16(job[4:6])
		pl, dl := int(binary.BigEndian.Uint16(job[6:8])), int(binary.BigEndian.Uint16(job[8:10]))
		if ref == 0 || 10+pl+dl != len(job) || pl < 2 {
			return
		}
		params, data := job[10:10+pl], job[10+pl:]
		var rp, rd []byte
		if params[0] == 0xf0 {
			if negotiated != 0 || pl != 8 || dl != 0 {
				return
			}
			requested := int(binary.BigEndian.Uint16(params[6:]))
			if requested < 64 {
				return
			}
			negotiated = min(requested, s.opts.PDULimit)
			rp = []byte{0xf0, 0, 0, 1, 0, 1, byte(negotiated >> 8), byte(negotiated)}
		} else {
			if negotiated == 0 || len(job) > negotiated {
				return
			}
			items, e := serverItems(params)
			if e != nil {
				return
			}
			rp = []byte{params[0], byte(len(items))}
			switch params[0] {
			case 4:
				if dl != 0 {
					return
				}
				s.reads.Add(1)
				for i, a := range items {
					raw, e := s.opts.Memory.Get(a)
					code := byte(0xff)
					if e != nil {
						code = 5
					}
					if s.opts.Fault.RejectArea == a.Area {
						code = 3
					}
					if code != 0xff {
						rd = append(rd, code, 0, 0, 0)
						continue
					}
					transport := byte(4)
					bits := len(raw) * 8
					if a.Type == "BOOL" {
						transport = 3
						bits = 1
					}
					rd = append(rd, code, transport, byte(bits>>8), byte(bits))
					rd = append(rd, raw...)
					if i < len(items)-1 && len(raw)%2 != 0 {
						rd = append(rd, 0)
					}
				}
			case 5:
				if len(items) != 1 || len(data) < 4 {
					return
				}
				a := items[0]
				bits := int(binary.BigEndian.Uint16(data[2:4]))
				expect := a.ByteLength() * 8
				transport := byte(4)
				if a.Type == "BOOL" {
					expect = 1
					transport = 3
				}
				if data[0] != 0 || data[1] != transport || bits != expect || len(data) != 4+a.ByteLength() {
					return
				}
				s.writes.Add(1)
				code := byte(0xff)
				if s.opts.Fault.RejectArea == a.Area {
					code = 3
				} else if s.opts.Memory.set(a, data[4:], false) != nil {
					code = 5
				}
				rd = []byte{code}
				if s.opts.Fault.DropWriteAck && code == 0xff {
					continue
				}
			default:
				return
			}
			if delay := s.opts.Fault.Delay; delay > 0 {
				timer := time.NewTimer(delay)
				select {
				case <-s.done:
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		}
		response := serverAck(ref, rp, rd)
		if len(response) > negotiated {
			return
		}
		if s.opts.Fault.WrongReference && params[0] != 0xf0 {
			response[5]++
		}
		payload := append([]byte{2, 0xf0, 0x80}, response...)
		if s.opts.Fault.Truncate && params[0] != 0xf0 {
			wire := make([]byte, len(payload)+4)
			wire[0] = 3
			binary.BigEndian.PutUint16(wire[2:4], uint16(len(wire)))
			copy(wire[4:], payload)
			c.Write(wire[:len(wire)-1])
			return
		}
		if s7.WriteTPKT(c, payload) != nil {
			return
		}
	}
}
func serverItems(p []byte) ([]device.Address, error) {
	if len(p) < 2 || p[1] == 0 || p[1] > 20 || len(p) != 2+12*int(p[1]) {
		return nil, fmt.Errorf("invalid item parameters")
	}
	items := make([]device.Address, int(p[1]))
	for i := range items {
		b := p[2+i*12 : 2+(i+1)*12]
		if b[0] != 0x12 || b[1] != 10 || b[2] != 0x10 || (b[3] != 1 && b[3] != 2) {
			return nil, fmt.Errorf("unsupported S7ANY syntax")
		}
		a := device.Address{Type: "RAW", Count: int(binary.BigEndian.Uint16(b[4:6])), DBNumber: binary.BigEndian.Uint16(b[6:8])}
		bit := uint32(b[9])<<16 | uint32(b[10])<<8 | uint32(b[11])
		a.ByteOffset = bit / 8
		a.BitOffset = uint8(bit % 8)
		if b[3] == 1 {
			a.Type = "BOOL"
		}
		switch b[8] {
		case 0x81:
			a.Area = "I"
		case 0x82:
			a.Area = "Q"
		case 0x83:
			a.Area = "M"
		case 0x84:
			a.Area = "DB"
		default:
			return nil, fmt.Errorf("unsupported area")
		}
		if e := a.Validate(); e != nil {
			return nil, e
		}
		items[i] = a
	}
	return items, nil
}
func serverAck(ref uint16, params, data []byte) []byte {
	b := make([]byte, 12+len(params)+len(data))
	b[0], b[1] = 0x32, 3
	binary.BigEndian.PutUint16(b[4:6], ref)
	binary.BigEndian.PutUint16(b[6:8], uint16(len(params)))
	binary.BigEndian.PutUint16(b[8:10], uint16(len(data)))
	copy(b[12:], params)
	copy(b[12+len(params):], data)
	return b
}
