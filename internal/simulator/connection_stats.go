package simulator

import (
	"encoding/binary"
	"net"
	"s7-ai-studio/internal/s7"
)

func (s *Server) handleConnectionSZL(c net.Conn, ref uint16, p, d []byte, pdu int) bool {
	if len(p) != 8 || p[0] != 0 || p[1] != 1 || p[2] != 0x12 || p[3] != 4 || p[4] != 0x11 || p[5] != 0x44 || p[6] != 1 || p[7] != 0 || len(d) != 8 || d[0] != 0xff || d[1] != 9 || binary.BigEndian.Uint16(d[2:4]) != 4 || binary.BigEndian.Uint16(d[6:8]) != 1 {
		return false
	}
	id := binary.BigEndian.Uint16(d[4:6])
	params := []byte{0, 1, 0x12, 8, 0x12, 0x84, 1, 0, 0, 0, 0, 0}
	data := []byte{0x0a, 0, 0, 0}
	var record []byte
	if s.opts.MaxConnections > 0 {
		switch id {
		case 0x0132:
			s.mu.Lock()
			count := s.establishedConnectionsLocked()
			s.mu.Unlock()
			// The simulator's sessions all use the shared pool.
			record = make([]byte, 18)
			binary.BigEndian.PutUint16(record[14:], uint16(int(s.opts.MaxConnections)-count))
			binary.BigEndian.PutUint16(record[16:], uint16(count))
		case 0x0131:
			record = make([]byte, 6)
			binary.BigEndian.PutUint16(record[2:], uint16(s.opts.PDULimit))
			binary.BigEndian.PutUint16(record[4:], s.opts.MaxConnections)
		}
	}
	if record != nil {
		binary.BigEndian.PutUint16(record, 1)
		data = make([]byte, 12+len(record))
		data[0], data[1] = 0xff, 9
		binary.BigEndian.PutUint16(data[2:4], uint16(len(data)-4))
		binary.BigEndian.PutUint16(data[4:6], id)
		binary.BigEndian.PutUint16(data[6:8], 1)
		binary.BigEndian.PutUint16(data[8:10], uint16(len(record)))
		binary.BigEndian.PutUint16(data[10:12], 1)
		copy(data[12:], record)
	}
	response := make([]byte, 10+len(params)+len(data))
	response[0], response[1] = 0x32, 7
	binary.BigEndian.PutUint16(response[4:6], ref)
	binary.BigEndian.PutUint16(response[6:8], uint16(len(params)))
	binary.BigEndian.PutUint16(response[8:10], uint16(len(data)))
	copy(response[10:], params)
	copy(response[10+len(params):], data)
	if len(response) > pdu {
		return false
	}
	return s7.WriteTPKT(c, append([]byte{2, 0xf0, 0x80}, response...)) == nil
}

func (s *Server) establishedConnectionsLocked() int {
	count := 0
	for _, established := range s.connections {
		if established {
			count++
		}
	}
	return count
}
