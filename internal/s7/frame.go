// Package s7 implements classic S7 absolute-address communication over ISO-on-TCP.
package s7

import (
	"encoding/binary"
	"fmt"
	"io"
)

func ReadTPKT(r io.Reader, limit int) ([]byte, error) {
	var h [4]byte
	if _, e := io.ReadFull(r, h[:]); e != nil {
		return nil, e
	}
	n := int(binary.BigEndian.Uint16(h[2:]))
	if h[0] != 3 || h[1] != 0 || n < 7 || limit < 7 || n > limit || n > 65535 {
		return nil, fmt.Errorf("invalid TPKT header or length %d", n)
	}
	b := make([]byte, n-4)
	_, e := io.ReadFull(r, b)
	return b, e
}
func WriteTPKT(w io.Writer, payload []byte) error {
	if len(payload) < 3 || len(payload) > 65531 {
		return fmt.Errorf("invalid TPKT payload length")
	}
	b := make([]byte, len(payload)+4)
	b[0] = 3
	binary.BigEndian.PutUint16(b[2:4], uint16(len(b)))
	copy(b[4:], payload)
	for len(b) > 0 {
		n, e := w.Write(b)
		if e != nil {
			return e
		}
		if n < 1 || n > len(b) {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
