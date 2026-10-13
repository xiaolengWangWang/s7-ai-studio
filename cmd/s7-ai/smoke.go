package main

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net"
	"os"
	"path/filepath"
	"s7-ai-studio/internal/device"
	"s7-ai-studio/internal/recorder"
	"s7-ai-studio/internal/service"
	"time"
)

type smokeReport struct {
	Version, Target, Initial, Readback, WriteState string
	Passed, PortReleased                           bool
	Packets                                        int
	Archives                                       []string
	Time                                           time.Time
}

func runSmoke(ctx context.Context, c *service.Controller, capture func() image.Image, dir string) error {
	defer c.Close()
	if e := os.MkdirAll(dir, 0755); e != nil {
		return e
	}
	if e := c.StartDemo(ctx); e != nil {
		return e
	}
	s := c.Snapshot()
	if s.Target != s.DemoTarget || s.DemoTarget == "" || len(s.Results) != 1 {
		return fmt.Errorf("验证必须使用自有本机演示")
	}
	initial := device.FormatValue(s.Results[0].Value)
	if initial != "42" {
		return fmt.Errorf("初值 %s != 42", initial)
	}
	p := service.Point{Address: "DB1.DBD0", Type: "DINT", Count: 1}
	if e := c.Write(ctx, service.WriteRequest{Point: p, Value: "123", Generation: s.Generation}, true); e != nil {
		return e
	}
	s = c.Snapshot()
	if s.WriteResult == nil || s.WriteResult.State != "CONFIRMED" || s.Readback != "一致" || len(s.Results) != 1 || device.FormatValue(s.Results[0].Value) != "123" {
		return fmt.Errorf("写入/读回验证失败")
	}
	im := capture()
	if im == nil {
		return fmt.Errorf("未获得 GUI 截图")
	}
	f, e := os.Create(filepath.Join(dir, "native-gui.png"))
	if e != nil {
		return e
	}
	e = png.Encode(f, im)
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	if e = c.Close(); e != nil {
		return e
	}
	if state := c.Snapshot(); state.RecordError != "" || state.Dropped != 0 {
		return fmt.Errorf("记录失败：%s；丢弃 %d", state.RecordError, state.Dropped)
	}
	l, e := net.Listen("tcp", s.DemoTarget)
	if e != nil {
		return fmt.Errorf("演示端口未释放: %w", e)
	}
	l.Close()
	files, e := filepath.Glob(filepath.Join(s.RecordDir, "*.sqlite3"))
	if e != nil || len(files) == 0 {
		return fmt.Errorf("没有记录归档")
	}
	packets := 0
	for _, path := range files {
		r, e := recorder.Open(path)
		if e != nil {
			return e
		}
		ss, e := r.Sessions(10)
		if e != nil {
			r.Close()
			return e
		}
		for _, session := range ss {
			if session.End.IsZero() {
				r.Close()
				return fmt.Errorf("记录会话未结束")
			}
			packets += session.Packets
		}
		if e = r.Close(); e != nil {
			return e
		}
	}
	if packets < 10 {
		return fmt.Errorf("真实报文记录不足: %d", packets)
	}
	report := smokeReport{Version: version, Target: s.Target, Initial: initial, Readback: "123", WriteState: s.WriteResult.State, Passed: true, PortReleased: true, Packets: packets, Archives: files, Time: time.Now().UTC()}
	b, e := json.MarshalIndent(report, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(dir, "smoke.json"), b, 0644)
}
