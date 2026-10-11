package simulator

import (
	"s7-ai-studio/internal/device"
	"time"
)

type Fault struct {
	Delay          time.Duration
	RejectArea     device.Area
	DropWriteAck   bool
	Truncate       bool
	WrongReference bool
}
