package ui

import (
	"context"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"s7-ai-studio/internal/device"
	"s7-ai-studio/internal/service"
	"strings"
	"time"
)

func (d *Desktop) points() ([]service.Point, error) {
	n, e := number(d.count, "数量")
	if e != nil {
		return nil, e
	}
	if n < 1 || n > 65535 {
		return nil, fmt.Errorf("数量须为 1..65535")
	}
	var out []service.Point
	for _, a := range strings.Split(d.addresses.Text, ",") {
		out = append(out, service.Point{Address: strings.TrimSpace(a), Type: device.DataType(d.kind.Selected), Count: n})
	}
	return out, nil
}
func (d *Desktop) pointsPanel() fyne.CanvasObject {
	d.addresses = entry("DB1.DBD0", "逗号分隔多个地址")
	d.kind = widget.NewSelect([]string{"BOOL", "BYTE", "SINT", "USINT", "WORD", "INT", "UINT", "DWORD", "DINT", "UDINT", "LWORD", "LINT", "ULINT", "REAL", "LREAL", "STRING[32]", "STRING[80]", "STRING[254]", "RAW"}, nil)
	d.kind.SetSelected("DINT")
	d.count = entry("1", "数量")
	d.interval = entry("1000", "间隔 ms")
	d.readBtn = widget.NewButton("读取一次", func() {
		p, e := d.points()
		if e != nil {
			dialog.ShowError(e, d.win)
			return
		}
		d.run(func(ctx context.Context) error { return d.controller.Read(ctx, p) })
	})
	d.pollBtn = widget.NewButton("开始轮询", func() {
		p, e := d.points()
		if e != nil {
			dialog.ShowError(e, d.win)
			return
		}
		n, e := number(d.interval, "轮询间隔")
		if e != nil {
			dialog.ShowError(e, d.win)
			return
		}
		d.run(func(ctx context.Context) error {
			return d.controller.StartPolling(ctx, p, time.Duration(n)*time.Millisecond)
		})
	})
	d.stopPollBtn = widget.NewButton("停止轮询", func() { d.controller.StopPolling(); d.refresh(d.controller.Snapshot()) })
	fields := container.NewBorder(nil, nil, widget.NewLabel("地址"), container.NewHBox(widget.NewLabel("类型"), d.kind, widget.NewLabel("数量"), container.NewGridWrap(fyne.NewSize(75, 36), d.count)), d.addresses)
	actions := container.NewHBox(d.readBtn, d.pollBtn, d.stopPollBtn, widget.NewLabel("间隔（ms）"), container.NewGridWrap(fyne.NewSize(90, 36), d.interval), widget.NewLabel("跨 PDU 读取非原子快照"))
	d.table = widget.NewTable(func() (int, int) { return len(d.rows), 6 }, func() fyne.CanvasObject { return widget.NewLabel("") }, func(id widget.TableCellID, o fyne.CanvasObject) {
		l := o.(*widget.Label)
		l.SetText(d.rows[id.Row][id.Col])
		l.Truncation = fyne.TextTruncateEllipsis
	})
	d.table.ShowHeaderRow = true
	heads := []string{"地址", "类型", "值", "质量", "时间", "错误"}
	d.table.CreateHeader = func() fyne.CanvasObject {
		return widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	}
	d.table.UpdateHeader = func(id widget.TableCellID, o fyne.CanvasObject) {
		if id.Col >= 0 {
			o.(*widget.Label).SetText(heads[id.Col])
		}
	}
	for i, w := range []float32{155, 75, 185, 85, 115, 290} {
		d.table.SetColumnWidth(i, w)
	}
	d.table.OnSelected = func(id widget.TableCellID) {
		if id.Row < len(d.rows) {
			r := d.rows[id.Row]
			dialog.ShowInformation("点位结果", strings.Join([]string{"地址：" + r[0], "类型：" + r[1], "值：" + r[2], "质量：" + r[3], "时间：" + r[4], "错误：" + r[5]}, "\n"), d.win)
		}
	}
	d.allowWrite = widget.NewCheck("允许写入（操作前仍需确认）", func(bool) { d.updateControls() })
	d.writeValue = entry("123", "一个值或逗号分隔数组")
	d.writeBtn = widget.NewButton("确认写入…", d.confirmWrite)
	d.writeStatus = widget.NewLabel("")
	d.writeStatus.Wrapping = fyne.TextWrapBreak
	writeLine := container.NewBorder(nil, nil, widget.NewLabel("写入值"), d.writeBtn, d.writeValue)
	bottom := container.NewVBox(widget.NewSeparator(), d.allowWrite, writeLine, container.NewVScroll(d.writeStatus))
	bottom.Objects[3].(*container.Scroll).SetMinSize(fyne.NewSize(0, 64))
	return container.NewBorder(container.NewVBox(fields, actions), bottom, nil, nil, d.table)
}
func (d *Desktop) confirmWrite() {
	if !d.allowWrite.Checked {
		return
	}
	p, e := d.points()
	if e != nil {
		dialog.ShowError(e, d.win)
		return
	}
	if len(p) != 1 {
		dialog.ShowInformation("写入限制", "每次写入只能指定一个地址。", d.win)
		return
	}
	s := d.controller.Snapshot()
	req := service.WriteRequest{Point: p[0], Value: d.writeValue.Text, Generation: s.Generation}
	d.confirmation = dialog.NewConfirm("确认 PLC 写入", fmt.Sprintf("目标：%s\n地址：%s\n类型：%s × %d\n值：%s\n\n分段写入不具备事务原子性。UNKNOWN 不会自动重试。", s.Target, p[0].Address, p[0].Type, p[0].Count, req.Value), func(ok bool) {
		if !ok {
			return
		}
		approved := d.allowWrite.Checked
		d.run(func(ctx context.Context) error { return d.controller.Write(ctx, req, approved) })
	}, d.win)
	d.confirmation.SetConfirmText("确认写入")
	d.confirmation.SetDismissText("取消")
	d.confirmation.Show()
}
