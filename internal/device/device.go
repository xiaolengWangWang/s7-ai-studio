package device

import (
	"context"
	"time"
)

type Protocol string

const ProtocolS7TCP Protocol = "S7_TCP"

// Legacy protocol identifiers describe recorded history, not available drivers.
const (
	ProtocolModbusTCP    Protocol = "MODBUS_TCP"
	ProtocolRTUOverTCP   Protocol = "RTU_OVER_TCP"
	ProtocolASCIIOverTCP Protocol = "ASCII_OVER_TCP"
	ProtocolModbusRTU    Protocol = "MODBUS_RTU"
	ProtocolModbusASCII  Protocol = "MODBUS_ASCII"
)

type Capabilities struct {
	Protocol       Protocol `json:"protocol"`
	Family         string   `json:"family,omitempty"`
	PDULength      int      `json:"pdu_length"`
	ReadAreas      []Area   `json:"read_areas"`
	WriteAreas     []Area   `json:"write_areas"`
	Absolute       bool     `json:"absolute"`
	Symbolic       bool     `json:"symbolic"`
	NativeBitWrite bool     `json:"native_bit_write"`
	Verified       bool     `json:"verified"`
}

type ReadResult struct {
	Address Address   `json:"address"`
	Value   Value     `json:"value"`
	Quality string    `json:"quality"`
	Time    time.Time `json:"time"`
	Error   string    `json:"error,omitempty"`
}
type WriteResult struct {
	Address Address `json:"address"`
	State   string  `json:"state"`
	Error   string  `json:"error,omitempty"`
}
type Driver interface {
	Capabilities() Capabilities
	Read(context.Context, []Address) ([]ReadResult, error)
	Write(context.Context, Address, Value) (WriteResult, error)
	Close() error
}
