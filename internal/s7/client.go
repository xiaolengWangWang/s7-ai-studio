package s7

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"s7-ai-studio/internal/device"
	"sync"
	"time"
)

type Options struct {
	LocalTSAP, RemoteTSAP uint16
	Timeout               time.Duration
	ReadRetries           int
	RequestedPDU          uint16
	Observer              device.Observer
}

func DefaultOptions() Options {
	return Options{Timeout: time.Second, ReadRetries: 1, RequestedPDU: 480}
}

type Client struct {
	conn               net.Conn
	opts               Options
	pduLength, maxTPKT int
	gate               chan struct{}
	closed             chan struct{}
	once               sync.Once
	connectionID       string
	reference          uint16
	requestID          uint64
}

var ErrClosed = errors.New("S7 session is closed; establish a new connection")

func Dial(ctx context.Context, target string, opts Options) (*Client, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = time.Second
	}
	if opts.RequestedPDU == 0 {
		opts.RequestedPDU = 480
	}
	if opts.ReadRetries < 0 || opts.ReadRetries > 1 {
		return nil, fmt.Errorf("read retries must be 0 or 1")
	}
	if _, e := encodeSetup(1, opts.RequestedPDU); e != nil {
		return nil, e
	}
	d := net.Dialer{Timeout: opts.Timeout, KeepAlive: 30 * time.Second}
	conn, e := d.DialContext(ctx, "tcp", target)
	if e != nil {
		return nil, e
	}
	c := &Client{conn: conn, opts: opts, gate: make(chan struct{}, 1), closed: make(chan struct{}), connectionID: rand.Text(), maxTPKT: 1028}
	c.gate <- struct{}{}
	var events []device.Event
	defer func() { c.observe(events) }()
	cr := []byte{17, 0xe0, 0, 0, 0, 1, 0, 0xc1, 2, byte(opts.LocalTSAP >> 8), byte(opts.LocalTSAP), 0xc2, 2, byte(opts.RemoteTSAP >> 8), byte(opts.RemoteTSAP), 0xc0, 1, 0x0a}
	cc, e := c.exchange(ctx, cr, "CONNECT", nil, &events)
	if e != nil {
		c.Close()
		return nil, e
	}
	code, e := validateCC(cc)
	if e != nil {
		c.Close()
		return nil, e
	}
	c.maxTPKT = (1 << code) + 4
	requested := opts.RequestedPDU
	if budget := (1 << code) - 3; int(requested) > budget {
		requested = uint16(budget)
	}
	reference := c.nextRef()
	setup, e := encodeSetup(reference, requested)
	if e != nil {
		c.Close()
		return nil, e
	}
	raw, e := c.exchange(ctx, append([]byte{2, 0xf0, 0x80}, setup...), "SETUP", nil, &events)
	if e != nil {
		c.Close()
		return nil, e
	}
	p, e := c.response(raw, reference)
	if e != nil || p.kind != 3 || len(p.params) != 8 || p.params[0] != 0xf0 || p.params[1] != 0 || len(p.data) != 0 {
		c.Close()
		if e == nil {
			e = fmt.Errorf("invalid S7 setup response")
		}
		return nil, e
	}
	n := int(binary.BigEndian.Uint16(p.params[6:8]))
	if n < 64 || n > int(requested) || binary.BigEndian.Uint16(p.params[2:4]) < 1 || binary.BigEndian.Uint16(p.params[4:6]) < 1 {
		c.Close()
		return nil, fmt.Errorf("invalid negotiated S7 parameters")
	}
	c.pduLength = n
	return c, nil
}

func validateCC(b []byte) (byte, error) {
	if len(b) < 7 || int(b[0])+1 != len(b) || b[1] != 0xd0 || binary.BigEndian.Uint16(b[2:4]) != 1 || b[6] != 0 {
		return 0, fmt.Errorf("invalid COTP connection confirmation")
	}
	var code byte
	for i := 7; i < len(b); {
		if i+2 > len(b) {
			return 0, fmt.Errorf("short COTP parameter")
		}
		n := int(b[i+1])
		if i+2+n > len(b) {
			return 0, fmt.Errorf("invalid COTP parameter length")
		}
		if b[i] == 0xc0 {
			if n != 1 {
				return 0, fmt.Errorf("invalid COTP TPDU size")
			}
			code = b[i+2]
		}
		i += 2 + n
	}
	if code < 7 || code > 10 {
		return 0, fmt.Errorf("unsupported COTP TPDU size")
	}
	return code, nil
}
func (c *Client) Capabilities() device.Capabilities {
	return device.Capabilities{Protocol: device.ProtocolS7TCP, PDULength: c.pduLength, Absolute: true, NativeBitWrite: true, ReadAreas: []device.Area{"DB", "M", "I", "Q"}, WriteAreas: []device.Area{"DB", "M", "I", "Q"}}
}
func (c *Client) Close() error {
	var e error
	c.once.Do(func() { close(c.closed); e = c.conn.Close() })
	return e
}
func (c *Client) nextRef() uint16 {
	c.reference++
	if c.reference == 0 {
		c.reference = 1
	}
	return c.reference
}
func (c *Client) lock(ctx context.Context) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	select {
	case <-c.closed:
		return ErrClosed
	default:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return ErrClosed
	case <-c.gate:
	}
	select {
	case <-c.closed:
		c.gate <- struct{}{}
		return ErrClosed
	default:
	}
	if e := ctx.Err(); e != nil {
		c.gate <- struct{}{}
		return e
	}
	return nil
}
func (c *Client) finish(events []device.Event) { c.gate <- struct{}{}; c.observe(events) }
func (c *Client) observe(events []device.Event) {
	if c.opts.Observer != nil {
		for _, e := range events {
			c.opts.Observer.OnEvent(e.Clone())
		}
	}
}
func wireBytes(b []byte) []byte {
	out := make([]byte, 4+len(b))
	out[0] = 3
	binary.BigEndian.PutUint16(out[2:4], uint16(len(out)))
	copy(out[4:], b)
	return out
}
func (c *Client) exchange(ctx context.Context, payload []byte, operation string, targets []device.Address, events *[]device.Event) ([]byte, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	select {
	case <-c.closed:
		return nil, ErrClosed
	default:
	}
	start := time.Now()
	deadline := start.Add(c.opts.Timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if e := c.conn.SetDeadline(deadline); e != nil {
		return nil, e
	}
	stop := context.AfterFunc(ctx, func() { c.conn.SetDeadline(time.Now()) })
	defer stop()
	c.requestID++
	tx := device.Event{Time: start.UTC(), RequestID: c.requestID, ConnectionID: c.connectionID, Protocol: device.ProtocolS7TCP, Dir: device.DirTX, Operation: operation, Targets: targets, Status: device.StatusSent, Raw: wireBytes(payload)}
	*events = append(*events, tx.Clone())
	err := WriteTPKT(c.conn, payload)
	var raw []byte
	if err == nil {
		raw, err = ReadTPKT(c.conn, c.maxTPKT)
	}
	stop()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	rx := tx
	rx.Time = time.Now().UTC()
	rx.Dir = device.DirRX
	rx.Raw = nil
	rx.RTT = time.Since(start)
	rx.Status = device.StatusSuccess
	if raw != nil {
		rx.Raw = wireBytes(raw)
	}
	if err != nil {
		c.Close()
		rx.Status = device.StatusConnectionError
		if ctx.Err() != nil {
			rx.Status = device.StatusCancelled
		} else {
			var n net.Error
			if errors.As(err, &n) && n.Timeout() {
				rx.Status = device.StatusTimeout
			}
		}
		rx.Error = err.Error()
	}
	*events = append(*events, rx.Clone())
	return raw, err
}
func (c *Client) response(raw []byte, reference uint16) (pdu, error) {
	if len(raw) < 3 || raw[0] != 2 || raw[1] != 0xf0 || raw[2] != 0x80 {
		return pdu{}, fmt.Errorf("invalid or segmented COTP data")
	}
	if c.pduLength > 0 && len(raw)-3 > c.pduLength {
		return pdu{}, fmt.Errorf("response exceeds negotiated PDU")
	}
	return parsePDU(raw[3:], reference)
}
func setEventError(events *[]device.Event, err error) {
	if len(*events) > 0 {
		last := &(*events)[len(*events)-1]
		last.Status = device.StatusParseError
		last.Error = err.Error()
	}
}
