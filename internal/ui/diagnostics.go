package ui

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"s7-ai-studio/internal/device"
	"strings"
)

func (d *Desktop) packetPanel() fyne.CanvasObject {
	d.wire = widget.NewLabel("")
	d.wire.TextStyle = fyne.TextStyle{Monospace: true}
	d.wire.Selectable = true
	d.packets = widget.NewList(func() int { return len(d.snapshot.Packets) }, func() fyne.CanvasObject {
		return container.NewHBox(widget.NewIcon(theme.UploadIcon()), widget.NewLabel("15:00:00.000 RX #65535 READ SUCCESS"))
	}, func(id widget.ListItemID, o fyne.CanvasObject) {
		e := d.snapshot.Packets[id]
		row := o.(*fyne.Container).Objects
		dir := theme.UploadIcon()
		if e.Dir == device.DirRX {
			dir = theme.DownloadIcon()
		}
		row[0].(*widget.Icon).SetResource(dir)
		importance := widget.MediumImportance
		if e.Error != "" || (e.Status != device.StatusSuccess && e.Status != device.StatusSent) {
			importance = widget.DangerImportance
		}
		setText(row[1].(*widget.Label), fmt.Sprintf("%s %s #%d %s %s", e.Time.Local().Format("15:04:05.000"), e.Dir, e.RequestID, e.Operation, e.Status), importance)
	})
	d.packets.OnSelected = func(id widget.ListItemID) {
		if id < 0 || id >= len(d.snapshot.Packets) {
			return
		}
		d.packetSelection = id
		d.wire.SetText(packetText(d.snapshot.Packets[id]))
		d.updateControls()
	}
	d.packets.OnUnselected = func(widget.ListItemID) {
		d.packetSelection = -1
		d.wire.SetText("")
		d.updateControls()
	}
	d.copyPacketBtn = widget.NewButtonWithIcon("复制详情", theme.ContentCopyIcon(), func() { d.app.Clipboard().SetContent(d.wire.Text) })
	d.packetCount = widget.NewLabel("")
	d.packetCount.Importance = widget.LowImportance
	hint := widget.NewLabel("显示最近 256 条；握手及完整收发由记录器归档。")
	hint.Importance = widget.LowImportance
	split := container.NewHSplit(d.packets, container.NewScroll(d.wire))
	split.Offset = .42
	return container.NewBorder(container.NewBorder(nil, nil, hint, container.NewHBox(d.packetCount, d.copyPacketBtn)), nil, nil, nil, split)
}
func packetSummary(packets []device.Event) string {
	var tx, rx int
	for _, e := range packets {
		if e.Dir == device.DirRX {
			rx++
		} else {
			tx++
		}
	}
	return fmt.Sprintf("TX %d · RX %d", tx, rx)
}
func packetText(e device.Event) string {
	reference := "—"
	if len(e.Raw) >= 17 && e.Raw[0] == 3 && e.Raw[4] == 2 && e.Raw[5] == 0xf0 && e.Raw[7] == 0x32 {
		reference = fmt.Sprint(binary.BigEndian.Uint16(e.Raw[11:13]))
	}
	return fmt.Sprintf("时间：%s\n方向：%s\n操作：%s\n请求 ID：%d\nPDU 引用：%s\n状态：%s\n错误：%s\n连接：%s\n\n真实报文字节（%d bytes）\n%s", e.Time.Local().Format("2006-01-02 15:04:05.000"), e.Dir, e.Operation, e.RequestID, reference, e.Status, e.Error, e.ConnectionID, len(e.Raw), hex.Dump(e.Raw))
}
func joinLogs(logs []string) string { return strings.Join(logs, "\n") }
func logSummary(n int) string       { return fmt.Sprintf("最近 %d 条（最多 100 条）", n) }
func (d *Desktop) logPanel() fyne.CanvasObject {
	d.logs = widget.NewLabel("")
	d.logs.Wrapping = fyne.TextWrapBreak
	d.logs.Selectable = true
	d.logScroll = container.NewVScroll(d.logs)
	d.copyLogBtn = widget.NewButtonWithIcon("复制日志", theme.ContentCopyIcon(), func() { d.app.Clipboard().SetContent(d.logs.Text) })
	d.logCount = widget.NewLabel("")
	d.logCount.Importance = widget.LowImportance
	return container.NewBorder(container.NewBorder(nil, nil, d.logCount, d.copyLogBtn), nil, nil, nil, d.logScroll)
}
