package ui

import (
	"context"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
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

// submit 让回车与点击按钮等效；按钮不可用时忽略。
func submit(e *widget.Entry, b *widget.Button) {
	e.OnSubmitted = func(string) {
		if !b.Disabled() && b.Visible() {
			b.OnTapped()
		}
	}
}
func (d *Desktop) connectionPanel() fyne.CanvasObject {
	d.family = widget.NewSelect([]string{"s7-300", "s7-1200", "s7-1200-g2", "s7-1500", "s7-200", "s7-200-cn", "s7-200-smart", "s7-200-smart-g2", "s7-400", "et-200sp", "et-200pro", "et-200s", "et-200x", "software-controller", "open-controller", "drive-controller", "winac", "siplus", "logo"}, nil)
	d.family.SetSelected("s7-300")
	d.target = entry("", "PLC 地址 host:port，例如 192.168.0.10:102")
	d.connectBtn = widget.NewButtonWithIcon("连接", theme.LoginIcon(), func() {
		cfg, e := d.config()
		if e != nil {
			dialog.ShowError(e, d.win)
			return
		}
		values := d.inputValues()
		d.run(func(ctx context.Context) error {
			e := d.controller.Connect(ctx, cfg)
			if e == nil {
				d.saveInputs(values)
			}
			return e
		})
	})
	d.connectBtn.Importance = widget.HighImportance
	submit(d.target, d.connectBtn)
	d.disconnectBtn = widget.NewButtonWithIcon("断开", theme.LogoutIcon(), func() { d.run(func(context.Context) error { return d.controller.Disconnect() }) })
	d.demoBtn = widget.NewButtonWithIcon("启动本机演示", theme.MediaPlayIcon(), func() { d.run(d.controller.StartDemo) })
	d.stopDemoBtn = widget.NewButtonWithIcon("停止演示", theme.MediaStopIcon(), func() { d.run(func(context.Context) error { return d.controller.StopDemo() }) })
	actions := container.NewHBox(d.connectBtn, d.disconnectBtn, widget.NewSeparator(), d.demoBtn, d.stopDemoBtn)
	line := container.NewBorder(nil, nil, container.NewHBox(widget.NewLabel("系列"), d.family), actions, d.target)
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

// inputs 是启动时恢复的连接与点位输入。写入值和“允许写入”不保存。
func (d *Desktop) inputs() map[string]*widget.Entry {
	return map[string]*widget.Entry{"target": d.target, "rack": d.rack, "slot": d.slot, "localTSAP": d.localTSAP, "remoteTSAP": d.remoteTSAP, "timeout": d.timeout, "vdb": d.vdb, "voffset": d.voffset, "addresses": d.addresses, "count": d.count, "interval": d.interval}
}
func (d *Desktop) inputValues() map[string]string {
	values := map[string]string{"family": d.family.Selected, "kind": d.kind.Selected}
	for k, e := range d.inputs() {
		values[k] = strings.TrimSpace(e.Text)
	}
	// 本机演示每次使用新的临时端口，下次启动不应预填。
	if s := d.snapshot; s.DemoTarget != "" && values["target"] == s.DemoTarget {
		delete(values, "target")
	}
	return values
}
func (d *Desktop) saveInputs(values map[string]string) {
	p := d.app.Preferences()
	for k, v := range values {
		p.SetString("input."+k, v)
	}
}
func (d *Desktop) loadInputs() {
	p := d.app.Preferences()
	for k, e := range d.inputs() {
		e.SetText(p.StringWithFallback("input."+k, e.Text))
	}
	d.family.SetSelected(p.StringWithFallback("input.family", d.family.Selected))
	d.kind.SetSelected(p.StringWithFallback("input.kind", d.kind.Selected))
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
		return cfg, fmt.Errorf("V 偏移须为 0..2097151")
	}
	cfg.VOffset = uint32(n)
	return cfg, nil
}
