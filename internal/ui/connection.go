package ui

import (
	"context"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"s7-ai-studio/internal/service"
	"strconv"
	"strings"
	"time"
)

func entry(text, placeholder string) *widget.Entry {
	e := widget.NewEntry()
	e.SetText(text)
	e.SetPlaceHolder(placeholder)
	return e
}
func (d *Desktop) connectionPanel() fyne.CanvasObject {
	d.family = widget.NewSelect([]string{"s7-300", "s7-1200", "s7-1200-g2", "s7-1500", "s7-200", "s7-200-cn", "s7-200-smart", "s7-200-smart-g2", "s7-400", "et-200sp", "et-200pro", "et-200s", "et-200x", "software-controller", "open-controller", "drive-controller", "winac", "siplus", "logo"}, nil)
	d.family.SetSelected("s7-300")
	d.target = entry("", "192.168.0.10:102")
	d.connectBtn = widget.NewButton("连接", func() {
		cfg, e := d.config()
		if e != nil {
			dialog.ShowError(e, d.win)
			return
		}
		d.run(func(ctx context.Context) error { return d.controller.Connect(ctx, cfg) })
	})
	d.connectBtn.Importance = widget.HighImportance
	d.disconnectBtn = widget.NewButton("断开", func() { d.run(func(context.Context) error { return d.controller.Disconnect() }) })
	d.demoBtn = widget.NewButton("启动本机演示", func() { d.run(d.controller.StartDemo) })
	d.stopDemoBtn = widget.NewButton("停止演示", func() { d.run(func(context.Context) error { return d.controller.StopDemo() }) })
	line := container.NewBorder(nil, nil, container.NewHBox(widget.NewLabel("系列"), d.family), container.NewHBox(d.connectBtn, d.disconnectBtn, d.demoBtn, d.stopDemoBtn), d.target)
	d.rack = entry("-1", "自动 -1")
	d.slot = entry("-1", "自动 -1")
	d.localTSAP = entry("", "默认 0x0100")
	d.remoteTSAP = entry("", "按现场配置")
	d.timeout = entry("2000", "毫秒")
	d.vdb = entry("0", "0 = 未映射")
	d.voffset = entry("0", "字节偏移")
	form := widget.NewForm(widget.NewFormItem("Rack / Slot", container.NewGridWithColumns(2, d.rack, d.slot)), widget.NewFormItem("Local / Remote TSAP", container.NewGridWithColumns(2, d.localTSAP, d.remoteTSAP)), widget.NewFormItem("超时（ms）", d.timeout), widget.NewFormItem("V 映射 DB / 偏移", container.NewGridWithColumns(2, d.vdb, d.voffset)))
	return container.NewVBox(line, widget.NewAccordion(widget.NewAccordionItem("高级连接配置（系列档案未经实机认证）", form)))
}
func number(e *widget.Entry, name string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(e.Text))
	if err != nil {
		return 0, fmt.Errorf("%s 须为整数", name)
	}
	return n, nil
}
func (d *Desktop) config() (service.Config, error) {
	cfg := service.DefaultConfig()
	cfg.Family = d.family.Selected
	cfg.Target = strings.TrimSpace(d.target.Text)
	var e error
	cfg.Rack, e = number(d.rack, "Rack")
	if e != nil {
		return cfg, e
	}
	cfg.Slot, e = number(d.slot, "Slot")
	if e != nil {
		return cfg, e
	}
	n, e := number(d.timeout, "超时")
	if e != nil {
		return cfg, e
	}
	cfg.Timeout = time.Duration(n) * time.Millisecond
	cfg.LocalTSAP = d.localTSAP.Text
	cfg.RemoteTSAP = d.remoteTSAP.Text
	cfg.VDB, e = number(d.vdb, "V 映射 DB")
	if e != nil {
		return cfg, e
	}
	n, e = number(d.voffset, "V 映射偏移")
	if e != nil || n < 0 || int64(n) > 1<<21-1 {
		return cfg, fmt.Errorf("V 偏移須为 0..2097151")
	}
	cfg.VOffset = uint32(n)
	return cfg, nil
}
