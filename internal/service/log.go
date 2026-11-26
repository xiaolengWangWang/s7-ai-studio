package service

import (
	"fmt"
	"s7-ai-studio/internal/device"
	"s7-ai-studio/internal/recorder"
	"time"
)

type LogLevel string

const (
	LogInfo    LogLevel = "INFO"
	LogWarning LogLevel = "WARN"
	LogError   LogLevel = "ERROR"
)

type LogCategory string

const (
	LogConnection LogCategory = "connection"
	LogDiagnostic LogCategory = "diagnostic"
	LogRead       LogCategory = "read"
	LogWrite      LogCategory = "write"
	LogPolling    LogCategory = "polling"
	LogSystem     LogCategory = "system"
)

type LogEntry struct {
	ID                            uint64
	Time                          time.Time
	Level                         LogLevel
	Category                      LogCategory
	Target, Message, ConnectionID string
	RequestID                     uint64
}

// Called under c.mu. Archive only significant session transitions, not packet traffic.
func (c *Controller) logLocked(level LogLevel, category LogCategory, message, kind string, packet device.Event) {
	c.logID++
	entry := LogEntry{ID: c.logID, Time: time.Now(), Level: level, Category: category, Target: c.snap.Target, Message: message, ConnectionID: packet.ConnectionID, RequestID: packet.RequestID}
	if category == LogSystem {
		entry.Target = ""
	}
	c.snap.Logs = append(c.snap.Logs, entry)
	if len(c.snap.Logs) > 300 {
		c.snap.Logs = c.snap.Logs[len(c.snap.Logs)-300:]
	}
	c.snap.Revision++
	if kind != "" && c.rec != nil && c.s != nil && c.s.id != 0 {
		archived := recorder.Event{Time: entry.Time, Kind: kind, Detail: message}
		if packet.ConnectionID != "" && packet.RequestID != 0 {
			archived.Analysis = fmt.Sprintf("连接 ID：%s\n请求 ID：%d", packet.ConnectionID, packet.RequestID)
			archived.RX = packet.Raw
		}
		c.recordErrorLocked(c.rec.Log(c.s.id, archived))
	}
}

func (c *Controller) recordErrorLocked(err error) {
	if err == nil || err.Error() == c.snap.RecordError {
		return
	}
	c.snap.RecordError = err.Error()
	c.logLocked(LogError, LogSystem, "记录失败 · "+err.Error(), "", device.Event{})
}
