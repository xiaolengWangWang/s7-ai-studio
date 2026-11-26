package s7

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

func TestConnectionStatsIndependentRejectionAndContinuation(t *testing.T) {
	a, b := net.Pipe()
	defer b.Close()
	c := &Client{conn: a, opts: Options{Timeout: time.Second}, gate: make(chan struct{}, 1), closed: make(chan struct{}), pduLength: 64, maxTPKT: 1028}
	c.gate <- struct{}{}
	defer c.Close()
	done := make(chan error, 1)
	go func() {
		for step := 0; step < 3; step++ {
			wire, err := ReadTPKT(b, 1028)
			if err != nil {
				done <- err
				return
			}
			req := wire[3:]
			ref := binary.BigEndian.Uint16(req[4:6])
			p := []byte{0, 1, 0x12, 8, 0x12, 0x84, 1, 0x25, 0, 0, 0, 0}
			var d []byte
			if step == 0 {
				p[9] = 1
				d = []byte{0xff, 9, 0, 12, 1, 0x32, 0, 1, 0, 18, 0, 1, 0, 1, 0, 0}
			} else if step == 1 {
				if req[1] != 7 || req[17] != 0x25 {
					done <- ErrClosed
					return
				}
				d = make([]byte, 18)
				d[0], d[1] = 0xff, 9
				binary.BigEndian.PutUint16(d[2:], 14)
				binary.BigEndian.PutUint16(d[4:], 0)
				binary.BigEndian.PutUint16(d[6:], 2)
				binary.BigEndian.PutUint16(d[8:], 3)
				binary.BigEndian.PutUint16(d[10:], 4)
				binary.BigEndian.PutUint16(d[12:], 5)
				binary.BigEndian.PutUint16(d[14:], 6)
				binary.BigEndian.PutUint16(d[16:], 7)
			} else {
				p[10], p[11] = 0xd4, 0x02
			}
			resp := job(ref, p, d)
			resp[1] = 7
			if err = WriteTPKT(b, append([]byte{2, 0xf0, 0x80}, resp...)); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	got := c.ConnectionStats(context.Background())
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got.CurrentError != nil || got.Current == nil || got.Current.PG != 2 || got.Current.OS != 3 || got.Current.Configured != 4 || got.Current.Established != 5 || got.Current.Free != 6 || got.Current.SharedUsed != 7 || got.MaxError == nil || got.Max != nil || c.IsClosed() {
		t.Fatalf("unexpected stats: %+v current %+v", got, got.Current)
	}
}
