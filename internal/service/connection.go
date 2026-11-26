package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"s7-ai-studio/internal/device"
	"s7-ai-studio/internal/profile"
	"s7-ai-studio/internal/s7"
)

type ConnectionInfo struct {
	LocalTSAP, RemoteTSAP uint16
	Rack, Slot            int
	RackSlotKnown         bool
	PDULength             int
	Resources             ConnectionResources
}

type ConnectionResources struct {
	ReadAt                 time.Time
	Current                s7.ConnectionUsage
	Maximum                uint16
	CurrentError, MaxError string
}

func connectionInfo(cfg Config, p profile.Profile, opts s7.Options) ConnectionInfo {
	info := ConnectionInfo{LocalTSAP: opts.LocalTSAP, RemoteTSAP: opts.RemoteTSAP}
	if strings.TrimSpace(cfg.RemoteTSAP) == "" {
		info.RackSlotKnown = true // The profile encoded the selected rack and slot.
	} else if kind := opts.RemoteTSAP >> 8; kind >= 1 && kind <= 3 {
		switch p.Family {
		case "s7-300", "s7-400", "s7-1200", "s7-1200-g2", "s7-1500":
			info.RackSlotKnown = true
		}
	}
	if info.RackSlotKnown {
		info.Rack, info.Slot = int(byte(opts.RemoteTSAP)>>5), int(opts.RemoteTSAP&31)
	}
	return info
}

func (info ConnectionInfo) endpoint() string {
	if info.RackSlotKnown {
		return fmt.Sprintf("路由机架 %d / 槽 %d · TSAP 0x%04X → 0x%04X", info.Rack, info.Slot, info.LocalTSAP, info.RemoteTSAP)
	}
	return fmt.Sprintf("TSAP 0x%04X → 0x%04X", info.LocalTSAP, info.RemoteTSAP)
}

func handshakeStage(operation string) string {
	switch operation {
	case "CONNECT":
		return "COTP 连接握手"
	case "SETUP":
		return "S7 PDU 协商"
	default:
		return ""
	}
}

func appendRecentPacket(packets []device.Event, packet device.Event, limit int) []device.Event {
	if len(packets) == limit {
		packets[0] = device.Event{}
		packets = packets[1:]
	}
	return append(packets, packet)
}

// RefreshConnectionStats reads the device's connection resources on the current session.
func (c *Controller) RefreshConnectionStats(ctx context.Context) error {
	s, ctx, done, err := c.acquire(ctx, 0)
	if err != nil {
		return err
	}
	defer done()

	resources := ConnectionResources{}
	source, ok := s.d.(interface {
		ConnectionStats(context.Context) s7.ConnectionStats
	})
	var stats s7.ConnectionStats
	if ok {
		stats = source.ConnectionStats(ctx)
	}
	resources.ReadAt = time.Now()
	if stats.Current != nil {
		resources.Current = *stats.Current
	} else {
		resources.CurrentError = connectionStatsError(stats.CurrentError)
	}
	if stats.Max != nil {
		resources.Maximum = *stats.Max
	} else {
		resources.MaxError = connectionStatsError(stats.MaxError)
	}
	c.mu.Lock()
	if c.closed || c.s != s {
		c.mu.Unlock()
		return context.Canceled
	}
	c.snap.Connection.Resources = resources
	switch {
	case resources.CurrentError == "" && resources.MaxError == "":
		c.logLocked(LogInfo, LogDiagnostic, "已读取设备当前连接和最大通信连接数", "", device.Event{})
	case resources.CurrentError == "":
		c.logLocked(LogWarning, LogDiagnostic, "已读取设备当前连接，最大通信连接数不可用："+resources.MaxError, "", device.Event{})
	case resources.MaxError == "":
		c.logLocked(LogWarning, LogDiagnostic, "已读取设备最大通信连接数，当前连接不可用："+resources.CurrentError, "", device.Event{})
	default:
		c.logLocked(LogWarning, LogDiagnostic, fmt.Sprintf("设备连接数不可用 · 当前连接：%s · 最大连接数：%s", resources.CurrentError, resources.MaxError), "", device.Event{})
	}
	c.mu.Unlock()
	if driver, ok := s.d.(interface{ IsClosed() bool }); ok && driver.IsClosed() {
		c.failure(s, errors.Join(stats.CurrentError, stats.MaxError))
	}
	return nil
}

func connectionStatsError(err error) string {
	if err != nil {
		return err.Error()
	}
	return "设备或驱动未提供数据"
}
