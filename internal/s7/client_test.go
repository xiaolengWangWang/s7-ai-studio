package s7

import (
	"context"
	"encoding/binary"
	"net"
	"s7-ai-studio/internal/device"
	"sync/atomic"
	"testing"
	"time"
)

type peerTrace struct {
	writes, reads, maxSize atomic.Int32
	readStarted            chan struct{}
	memory                 atomic.Int32
}

func testPeer(t *testing.T, mode string, size int) (string, *peerTrace) {
	t.Helper()
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	trace := &peerTrace{readStarted: make(chan struct{}, 1)}
	trace.memory.Store(0x55)
	t.Cleanup(func() { ln.Close() })
	go func() {
		c, e := ln.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		t.Cleanup(func() { c.Close() })
		c.SetDeadline(time.Now().Add(5 * time.Second))
		if _, e = ReadTPKT(c, 2048); e != nil {
			return
		}
		if e = WriteTPKT(c, fromHex("11d00001000200c0010ac1020100c2020102")); e != nil {
			return
		}
		for {
			b, e := ReadTPKT(c, 2048)
			if e != nil {
				return
			}
			if len(b) < 13 {
				return
			}
			p := b[3:]
			reference := binary.BigEndian.Uint16(p[4:6])
			paramLen := int(binary.BigEndian.Uint16(p[6:8]))
			if 10+paramLen > len(p) {
				return
			}
			params := p[10 : 10+paramLen]
			var response []byte
			switch params[0] {
			case 0xf0:
				response = peerResponse(reference, []byte{0xf0, 0, 0, 1, 0, 1, byte(size >> 8), byte(size)}, nil)
			case 4:
				trace.reads.Add(1)
				select {
				case trace.readStarted <- struct{}{}:
				default:
				}
				if mode == "delay" {
					<-time.After(2 * time.Second)
					continue
				}
				var data []byte
				count := int(params[1])
				for i := 0; i < count; i++ {
					if len(params) < 2+(i+1)*12 {
						return
					}
					item := params[2+i*12 : 2+(i+1)*12]
					n := int(binary.BigEndian.Uint16(item[4:6]))
					if mode == "mixed" && i == 1 {
						data = append(data, 5, 0, 0, 0)
						continue
					}
					transport := byte(4)
					bits := n * 8
					if item[3] == 1 {
						transport = 3
						bits = 1
						n = 1
					}
					data = append(data, 0xff, transport, byte(bits>>8), byte(bits))
					raw := make([]byte, n)
					if item[3] == 1 {
						raw[0] = byte((trace.memory.Load() >> uint(item[11]%8)) & 1)
					}
					data = append(data, raw...)
					if i < count-1 && n%2 != 0 {
						data = append(data, 0)
					}
				}
				response = peerResponse(reference, []byte{4, byte(count)}, data)
				if mode == "bad-ref" {
					response[5]++
				}
			case 5:
				trace.writes.Add(1)
				if params[5] == 1 {
					bit := uint(params[13] % 8)
					old := trace.memory.Load()
					value := p[len(p)-1]
					if value != 0 {
						old |= 1 << bit
					} else {
						old &^= 1 << bit
					}
					trace.memory.Store(old)
				}
				if mode == "drop-write" {
					<-time.After(2 * time.Second)
					continue
				}
				response = peerResponse(reference, []byte{5, 1}, []byte{0xff})
			default:
				return
			}
			if len(p) > int(trace.maxSize.Load()) {
				trace.maxSize.Store(int32(len(p)))
			}
			if e = WriteTPKT(c, append([]byte{2, 0xf0, 0x80}, response...)); e != nil {
				return
			}
		}
	}()
	return ln.Addr().String(), trace
}
func peerResponse(ref uint16, params, data []byte) []byte {
	b := make([]byte, 12+len(params)+len(data))
	b[0], b[1] = 0x32, 3
	binary.BigEndian.PutUint16(b[4:6], ref)
	binary.BigEndian.PutUint16(b[6:8], uint16(len(params)))
	binary.BigEndian.PutUint16(b[8:10], uint16(len(data)))
	copy(b[12:], params)
	copy(b[12+len(params):], data)
	return b
}
func dialTest(t *testing.T, target string, timeout time.Duration) *Client {
	t.Helper()
	o := DefaultOptions()
	o.LocalTSAP, o.RemoteTSAP = 0x100, 0x102
	o.Timeout = timeout
	c, e := Dial(context.Background(), target, o)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func TestHandshakeNegotiatesSmallerPDU(t *testing.T) {
	target, _ := testPeer(t, "", 128)
	c := dialTest(t, target, time.Second)
	cap := c.Capabilities()
	if cap.PDULength != 128 || cap.Family != "" || cap.Verified {
		t.Fatalf("%+v", cap)
	}
}
func TestReadBatchPerItemError(t *testing.T) {
	target, _ := testPeer(t, "mixed", 480)
	c := dialTest(t, target, time.Second)
	a, _ := device.ParseAddress("MW0", "INT", 1)
	b := a
	b.ByteOffset = 10
	r, e := c.Read(context.Background(), []device.Address{a, b})
	if e != nil || len(r) != 2 || r[0].Quality != "GOOD" || r[1].Quality != "BAD" {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestReadSplitRespectsPDU(t *testing.T) {
	target, tr := testPeer(t, "", 128)
	c := dialTest(t, target, time.Second)
	a, _ := device.ParseAddress("MW0", "INT", 200)
	r, e := c.Read(context.Background(), []device.Address{a})
	if e != nil || len(r) != 1 || r[0].Quality != "GOOD" || len(r[0].Value.Elements) != 200 || tr.reads.Load() < 2 || tr.maxSize.Load() > 128 {
		t.Fatalf("%+v %v reads=%d size=%d", r, e, tr.reads.Load(), tr.maxSize.Load())
	}
}
func TestTimeoutInvalidatesSession(t *testing.T) {
	target, tr := testPeer(t, "delay", 480)
	c := dialTest(t, target, 50*time.Millisecond)
	a, _ := device.ParseAddress("MB0", "BYTE", 1)
	if _, e := c.Read(context.Background(), []device.Address{a}); e == nil {
		t.Fatal("timeout accepted")
	}
	if _, e := c.Read(context.Background(), []device.Address{a}); e == nil {
		t.Fatal("invalid session reused")
	}
	if tr.reads.Load() != 1 {
		t.Fatal("timeout read resent on misaligned session")
	}
}
func TestWriteUncertainNeverRetries(t *testing.T) {
	target, tr := testPeer(t, "drop-write", 480)
	c := dialTest(t, target, 50*time.Millisecond)
	a, _ := device.ParseAddress("MW0", "INT", 1)
	r, e := c.Write(context.Background(), a, device.Value{Type: "INT", Integer: "42"})
	if e == nil || r.State != "UNKNOWN" || tr.writes.Load() != 1 {
		t.Fatalf("%+v %v writes=%d", r, e, tr.writes.Load())
	}
}
func TestCancelQueuedRequest(t *testing.T) {
	target, tr := testPeer(t, "delay", 480)
	c := dialTest(t, target, time.Second)
	a, _ := device.ParseAddress("MB0", "BYTE", 1)
	done := make(chan struct{})
	go func() { defer close(done); c.Read(context.Background(), []device.Address{a}) }()
	<-tr.readStarted
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, e := c.Read(ctx, []device.Address{a}); e == nil || time.Since(start) > 200*time.Millisecond {
		t.Fatal("queued cancellation did not return promptly")
	}
	c.Close()
	<-done
	if tr.reads.Load() != 1 {
		t.Fatal("cancelled read sent")
	}
}
func TestCloseUnblocksRead(t *testing.T) {
	target, tr := testPeer(t, "delay", 480)
	c := dialTest(t, target, time.Second)
	a, _ := device.ParseAddress("MB0", "BYTE", 1)
	done := make(chan error, 1)
	go func() { _, e := c.Read(context.Background(), []device.Address{a}); done <- e }()
	<-tr.readStarted
	c.Close()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("closed read succeeded")
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Close blocked")
	}
}
func TestNativeBitWritePreservesNeighbours(t *testing.T) {
	target, tr := testPeer(t, "", 480)
	c := dialTest(t, target, time.Second)
	a, _ := device.ParseAddress("M0.1", "BOOL", 1)
	r, e := c.Write(context.Background(), a, device.Value{Type: "BOOL", Boolean: true})
	if e != nil || r.State != "CONFIRMED" || tr.memory.Load() != 0x57 {
		t.Fatalf("%+v %v memory=%x", r, e, tr.memory.Load())
	}
}
func TestReadRejectsInvalidAddressBeforeSending(t *testing.T) {
	target, tr := testPeer(t, "", 480)
	c := dialTest(t, target, time.Second)
	a := device.Address{Area: "M", Type: "DINT", Count: 1, ByteOffset: 1 << 21}
	if _, e := c.Read(context.Background(), []device.Address{a}); e == nil || tr.reads.Load() != 0 {
		t.Fatal("invalid address transmitted")
	}
}
func TestObserverCanCloseAndEventsAreFrozen(t *testing.T) {
	target, _ := testPeer(t, "", 480)
	o := DefaultOptions()
	o.LocalTSAP, o.RemoteTSAP = 0x100, 0x102
	var c *Client
	var events []device.Event
	o.Observer = device.ObserverFunc(func(e device.Event) {
		if e.Operation == "READ" {
			events = append(events, e)
			c.Close()
		}
	})
	var e error
	c, e = Dial(context.Background(), target, o)
	if e != nil {
		t.Fatal(e)
	}
	a, _ := device.ParseAddress("MB0", "BYTE", 1)
	r, e := c.Read(context.Background(), []device.Address{a})
	if e != nil || len(events) != 2 {
		t.Fatalf("%v events=%d", e, len(events))
	}
	r[0].Value.Raw[0] = 99
	if len(events[1].Results) != 1 || events[1].Results[0].Read.Value.Raw[0] != 0 {
		t.Fatal("observer result mutated")
	}
}
