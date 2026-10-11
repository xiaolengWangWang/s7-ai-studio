package ui

import (
	"encoding/hex"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"strings"
)

func (d *Desktop) packetPanel() fyne.CanvasObject {
	d.wire = widget.NewMultiLineEntry()
	d.wire.TextStyle = fyne.TextStyle{Monospace: true}
	d.wire.Wrapping = fyne.TextWrapBreak
	d.wire.Disable()
	d.packets = widget.NewList(func() int { return len(d.snapshot.Packets) }, func() fyne.CanvasObject { return widget.NewLabel("15:00:00.000 RX #65535 READ SUCCESS") }, func(id widget.ListItemID, o fyne.CanvasObject) {
		e := d.snapshot.Packets[id]
		o.(*widget.Label).SetText(fmt.Sprintf("%s %s #%d %s %s", e.Time.Local().Format("15:04:05.000"), e.Dir, e.RequestID, e.Operation, e.Status))
	})
	d.packets.OnSelected = func(id widget.ListItemID) {
		if id >= len(d.snapshot.Packets) {
			return
		}
		e := d.snapshot.Packets[id]
		d.wire.SetText(fmt.Sprintf("时间：%s\n方向：%s\n操作：%s\n引用：%d\n状态：%s\n错误：%s\n连接：%s\n\n真实报文字节（%d bytes）\n%s", e.Time.Local().Format("2006-01-02 15:04:05.000"), e.Dir, e.Operation, e.RequestID, e.Status, e.Error, e.ConnectionID, len(e.Raw), hex.Dump(e.Raw)))
	}
	split := container.NewHSplit(d.packets, d.wire)
	split.Offset = .42
	return container.NewBorder(widget.NewLabel("显示最近 256 条；握手及完整收发由记录器归档。"), nil, nil, nil, split)
}
func joinLogs(logs []string) string { return strings.Join(logs, "\n") }
func (d *Desktop) logPanel() fyne.CanvasObject {
	d.logs = widget.NewLabel("")
	d.logs.Wrapping = fyne.TextWrapBreak
	return container.NewVScroll(d.logs)
}
