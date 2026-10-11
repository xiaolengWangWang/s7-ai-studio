// Package s7 implements classic S7 absolute-address communication over ISO-on-TCP.
package s7

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

var errInvalidTPKT = errors.New("invalid TPKT header or length")

func ReadTPKT(r io.Reader, limit int) ([]byte, error) {
	payload, _, err := readTPKT(r, limit)
	return payload, err
}

// readTPKT preserves the received wire, including partial or invalid headers.
func readTPKT(r io.Reader, limit int) (payload, wire []byte, err error) {
	var h [4]byte
	headerBytes, e := io.ReadFull(r, h[:])
	if e != nil {
		return nil, append([]byte(nil), h[:headerBytes]...), e
	}
	n := int(binary.BigEndian.Uint16(h[2:]))
	if h[0] != 3 || h[1] != 0 || n < 7 || limit < 7 || n > limit || n > 65535 {
		return nil, append([]byte(nil), h[:]...), fmt.Errorf("%w %d", errInvalidTPKT, n)
	}
	b := make([]byte, n)
	copy(b, h[:])
	received, e := io.ReadFull(r, b[4:])
	return b[4 : 4+received], b[:4+received], e
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
