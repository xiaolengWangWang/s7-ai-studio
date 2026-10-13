package ui

import (
	"context"
	"fmt"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"io"
	"s7-ai-studio/internal/device"
	"s7-ai-studio/internal/pointtable"
	"s7-ai-studio/internal/service"
	"strings"
	"time"
)

var resultHeads = []string{"地址", "名称", "类型", "值", "核对", "质量", "时间", "错误"}

// pointKey 让点表元数据按地址文字匹配读数，忽略大小写和空格。
func pointKey(address string) string { return strings.ToUpper(strings.TrimSpace(address)) }

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
		a = strings.TrimSpace(a)
		p := service.Point{Address: a, Type: device.DataType(d.kind.Selected), Count: n}
		// 点表里的点按各自类型读一个元素；viSCADA 标识自带类型，不受“类型”下拉框影响。
		if m, ok := d.meta[pointKey(a)]; ok {
			p.Type, p.Count = m.Type, 1
			if p.Type == "" {
				resolved, _ := m.Resolve() // 导入时已校验
				p.Type = resolved.Type
			}
		} else if t, ok := device.PlatformType(a); ok {
			p.Type = t
		}
		out = append(out, p)
	}
	return out, nil
}

// cellImportance 给核对、质量和错误列着色，其余列保持默认颜色。
func cellImportance(row []string, col int) widget.Importance {
	switch {
	case col == 4 && row[4] == pointtable.Match, col == 5 && row[5] == "GOOD":
		return widget.SuccessImportance
	case col == 4 && row[4] != "", col == 5, col == 7:
		return widget.DangerImportance
	}
	return widget.MediumImportance
}

// importTable 载入点表：地址框换成点表里的全部点，名称、单位和期望值用于显示与核对。
func (d *Desktop) importTable(name string, data []byte) error {
	t, e := pointtable.Parse(name, data)
	if e != nil {
		return e
	}
	d.pointTable = &t
	d.meta = make(map[string]pointtable.Point, len(t.Points))
	addresses := make([]string, len(t.Points))
	for i, p := range t.Points {
		d.meta[pointKey(p.Address)] = p
		addresses[i] = p.Address
	}
	d.addresses.SetText(strings.Join(addresses, ", "))
	d.count.SetText("1")
	d.refresh(d.controller.Snapshot())
	if len(t.Skipped) > 0 {
		shown := t.Skipped
		if len(shown) > 12 {
			shown = append(shown[:12:12], fmt.Sprintf("……另有 %d 行", len(t.Skipped)-12))
		}
		dialog.ShowInformation("部分行未导入", fmt.Sprintf("导入 %d 个点，跳过 %d 行：\n%s", len(t.Points), len(t.Skipped), strings.Join(shown, "\n")), d.win)
	}
	return nil
}

func (d *Desktop) openTable() {
	open := dialog.NewFileOpen(func(r fyne.URIReadCloser, e error) {
		if e != nil {
			dialog.ShowError(e, d.win)
			return
		}
		if r == nil {
			return
		}
		defer r.Close()
		data, e := io.ReadAll(io.LimitReader(r, 16<<20+1))
		if e == nil && len(data) > 16<<20 {
			e = fmt.Errorf("点表文件超过 16 MB")
		}
		if e == nil {
			e = d.importTable(r.URI().Name(), data)
		}
		if e != nil {
			dialog.ShowError(e, d.win)
		}
	}, d.win)
	open.SetFilter(storage.NewExtensionFileFilter([]string{".xlsx", ".csv"}))
	open.SetConfirmText("导入")
	open.SetDismissText("取消")
	open.Show()
}

func (d *Desktop) clearTable() {
	d.pointTable, d.meta = nil, nil
	d.refresh(d.controller.Snapshot())
}

// copyResults 把结果表按制表符分隔复制，可直接粘贴到 Excel。
func (d *Desktop) copyResults() {
	lines := []string{strings.Join(resultHeads, "\t")}
	for _, r := range d.rows {
		lines = append(lines, strings.Join(r, "\t"))
	}
	d.app.Clipboard().SetContent(strings.Join(lines, "\n"))
}

func (d *Desktop) pointsPanel() fyne.CanvasObject {
	d.addresses = entry("DB1.DBD0", "地址，多个用逗号分隔，例如 DB1.DBD0, M0.7；也可填 viSCADA 标识 DB1.R0, DB1.X52.0")
	d.kind = widget.NewSelect([]string{"BOOL", "BYTE", "SINT", "USINT", "WORD", "INT", "UINT", "DWORD", "DINT", "UDINT", "LWORD", "LINT", "ULINT", "REAL", "LREAL", "STRING[32]", "STRING[80]", "STRING[254]", "RAW"}, nil)
	d.kind.SetSelected("DINT")
	d.count = entry("1", "数量")
	d.interval = entry("1000", "间隔 ms")
	d.readBtn = widget.NewButtonWithIcon("读取一次", theme.ViewRefreshIcon(), func() {
		p, e := d.points()
		if e != nil {
			dialog.ShowError(e, d.win)
			return
		}
		values := d.inputValues()
		d.run(func(ctx context.Context) error {
			e := d.controller.Read(ctx, p)
			if e == nil {
				d.saveInputs(values)
			}
			return e
		})
	})
	d.readBtn.Importance = widget.HighImportance
	submit(d.addresses, d.readBtn)
	d.pollBtn = widget.NewButtonWithIcon("开始轮询", theme.MediaPlayIcon(), func() {
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
		values := d.inputValues()
		d.run(func(ctx context.Context) error {
			e := d.controller.StartPolling(ctx, p, time.Duration(n)*time.Millisecond)
			if e == nil {
				d.saveInputs(values)
			}
			return e
		})
	})
	d.stopPollBtn = widget.NewButtonWithIcon("停止轮询", theme.MediaStopIcon(), func() { d.controller.StopPolling(); d.refresh(d.controller.Snapshot()) })
	d.importBtn = widget.NewButtonWithIcon("导入点表…", theme.FileIcon(), d.openTable)
	d.clearTableBtn = widget.NewButtonWithIcon("清除点表", theme.ContentClearIcon(), d.clearTable)
	d.copyResultBtn = widget.NewButtonWithIcon("复制结果", theme.ContentCopyIcon(), d.copyResults)
	d.tableInfo = widget.NewLabel("")
	d.tableInfo.Truncation = fyne.TextTruncateEllipsis
	fields := container.NewBorder(nil, nil, widget.NewLabel("地址"), container.NewHBox(widget.NewLabel("类型"), d.kind, widget.NewLabel("数量"), container.NewGridWrap(fyne.NewSize(75, 36), d.count)), d.addresses)
	actions := container.NewBorder(nil, nil, container.NewHBox(d.readBtn, d.pollBtn, d.stopPollBtn, widget.NewLabel("间隔（ms）"), container.NewGridWrap(fyne.NewSize(90, 36), d.interval)), container.NewHBox(d.importBtn, d.clearTableBtn, d.copyResultBtn))
	hint := widget.NewLabel("viSCADA 物模型导出（.xlsx）或 CSV：按“属性标识/地址”列读取，“模拟值/期望值”列用于核对。多个地址跨 PDU 读取时不是同一时刻的原子快照。")
	hint.Importance = widget.LowImportance
	hint.Truncation = fyne.TextTruncateEllipsis
	d.table = widget.NewTable(func() (int, int) { return len(d.rows), len(resultHeads) }, func() fyne.CanvasObject {
		l := widget.NewLabel("")
		l.Truncation = fyne.TextTruncateEllipsis
		return l
	}, func(id widget.TableCellID, o fyne.CanvasObject) {
		setText(o.(*widget.Label), d.rows[id.Row][id.Col], cellImportance(d.rows[id.Row], id.Col))
	})
	d.table.ShowHeaderRow = true
	d.table.CreateHeader = func() fyne.CanvasObject {
		return widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	}
	d.table.UpdateHeader = func(id widget.TableCellID, o fyne.CanvasObject) {
		if id.Col >= 0 {
			o.(*widget.Label).SetText(resultHeads[id.Col])
		}
	}
	for i, w := range []float32{120, 205, 70, 150, 150, 75, 115, 230} {
		d.table.SetColumnWidth(i, w)
	}
	d.table.OnSelected = func(id widget.TableCellID) {
		d.table.UnselectAll()
		if id.Row >= 0 && id.Row < len(d.rows) {
			r := d.rows[id.Row]
			lines := make([]string, len(resultHeads))
			for i, h := range resultHeads {
				lines[i] = h + "：" + r[i]
			}
			if m, ok := d.meta[pointKey(r[0])]; ok {
				lines = append(lines, "标准化名称："+m.Standard, "期望值："+m.Expected, fmt.Sprintf("点表行：%d", m.Row))
			}
			dialog.ShowInformation("点位结果", strings.Join(lines, "\n"), d.win)
		}
	}
	d.emptyHint = widget.NewLabelWithStyle("暂无读取结果。连接后点击“读取一次”或“开始轮询”；也可先启动本机演示，或导入点表。", fyne.TextAlignCenter, fyne.TextStyle{})
	d.emptyHint.Importance = widget.LowImportance
	d.allowWrite = widget.NewCheck("允许写入（操作前仍需确认）", func(bool) { d.updateControls() })
	d.writeValue = entry("123", "一个值或逗号分隔数组")
	d.writeBtn = widget.NewButtonWithIcon("确认写入…", theme.UploadIcon(), d.confirmWrite)
	d.writeBtn.Importance = widget.WarningImportance
	submit(d.writeValue, d.writeBtn)
	d.writeStatus = widget.NewLabel("")
	d.writeStatus.Wrapping = fyne.TextWrapBreak
	writeLine := container.NewBorder(nil, nil, widget.NewLabel("写入值"), d.writeBtn, d.writeValue)
	status := container.NewVScroll(d.writeStatus)
	status.SetMinSize(fyne.NewSize(0, 64))
	bottom := container.NewVBox(widget.NewSeparator(), d.allowWrite, writeLine, status)
	return container.NewBorder(container.NewVBox(fields, actions, d.tableInfo, hint), bottom, nil, nil, container.NewStack(d.table, container.NewCenter(d.emptyHint)))
}

// resultRows 把读数转成表格行，并按点表期望值核对；返回各行核对结论。
func (d *Desktop) resultRows(results []device.ReadResult) ([][]string, []string) {
	rows := make([][]string, 0, len(results))
	verdicts := make([]string, 0, len(results))
	for _, r := range results {
		addr := r.Address.Original
		if addr == "" {
			addr = r.Address.String()
		}
		m := d.meta[pointKey(addr)]
		value := "—"
		if r.Quality == "GOOD" {
			value = device.FormatValue(r.Value)
			if m.Unit != "" {
				value += " " + m.Unit
			}
		}
		verdict := pointtable.Check(r, m.Expected)
		verdicts = append(verdicts, verdict)
		if verdict == pointtable.Mismatch {
			verdict += "（期望 " + m.Expected + "）"
		}
		typ := string(r.Address.Type)
		if r.Address.Type == "STRING" {
			typ = fmt.Sprintf("STRING[%d]", r.Address.StringLength)
		}
		rows = append(rows, []string{addr, m.Name, typ, value, verdict, r.Quality, r.Time.Local().Format("15:04:05.000"), r.Error})
	}
	return rows, verdicts
}

// tableSummary 显示当前点表与最近一次读取的核对汇总。
func (d *Desktop) tableSummary(verdicts []string) (string, widget.Importance) {
	t := d.pointTable
	if t == nil {
		return "", widget.LowImportance
	}
	text := fmt.Sprintf("点表：%s", t.Name)
	if t.Sheet != "" {
		text += "（" + t.Sheet + "）"
	}
	text += fmt.Sprintf(" · %d 点", len(t.Points))
	if len(t.Skipped) > 0 {
		text += fmt.Sprintf(" · 跳过 %d 行", len(t.Skipped))
	}
	if len(verdicts) == 0 {
		return text + " · 尚未读取", widget.MediumImportance
	}
	importance := widget.SuccessImportance
	for _, v := range verdicts {
		if v != "" && v != pointtable.Match {
			importance = widget.DangerImportance
		}
	}
	return text + " · " + pointtable.Tally(verdicts), importance
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
