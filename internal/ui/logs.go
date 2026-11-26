package ui

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"s7-ai-studio/internal/service"
)

type logView struct {
	d                       *Desktop
	root                    fyne.CanvasObject
	entries, shown          []service.LogEntry
	selected                uint64
	list                    *widget.List
	detail, count, empty    *widget.Label
	level, category         *widget.Select
	search                  *widget.Entry
	copyBtn, copyResultsBtn *widget.Button
	packetBtn, exportBtn    *widget.Button
}

func newLogView(d *Desktop) *logView {
	l := &logView{d: d}
	l.detail = widget.NewLabel("选择一条日志查看详情")
	l.detail.Wrapping = fyne.TextWrapBreak
	l.detail.Selectable = true
	l.count = widget.NewLabel("")
	l.empty = widget.NewLabel("暂无符合条件的日志")
	l.empty.Alignment = fyne.TextAlignCenter
	l.list = widget.NewList(func() int { return len(l.shown) }, func() fyne.CanvasObject {
		label := widget.NewLabel("")
		label.Truncation = fyne.TextTruncateEllipsis
		return label
	}, func(id widget.ListItemID, obj fyne.CanvasObject) {
		if id < 0 || id >= len(l.shown) {
			return
		}
		e := l.shown[id]
		importance := widget.MediumImportance
		switch e.Level {
		case service.LogWarning:
			importance = widget.WarningImportance
		case service.LogError:
			importance = widget.DangerImportance
		}
		setText(obj.(*widget.Label), fmt.Sprintf("%s  %s  %s  %s", e.Time.Local().Format("15:04:05.000"), e.Level, logCategoryName(e.Category), strings.ReplaceAll(e.Message, "\n", " ")), importance)
	})
	l.list.OnSelected = func(id widget.ListItemID) {
		if id < 0 || id >= len(l.shown) {
			return
		}
		e := l.shown[id]
		l.selected = e.ID
		setText(l.detail, formatLogEntry(e), widget.MediumImportance)
		enable(l.copyBtn, true)
		enable(l.packetBtn, e.ConnectionID != "" && e.RequestID != 0)
	}
	l.level = widget.NewSelect([]string{"全部级别", "INFO", "WARN", "ERROR"}, func(string) { l.applyFilter() })
	l.category = widget.NewSelect([]string{"全部分类", "连接", "诊断", "读取", "写入", "轮询", "系统"}, func(string) { l.applyFilter() })
	l.search = widget.NewEntry()
	l.search.SetPlaceHolder("搜索消息、目标或请求 ID")
	l.search.OnChanged = func(string) { l.applyFilter() }
	l.copyBtn = widget.NewButton("复制详情", func() { l.d.app.Clipboard().SetContent(l.detail.Text) })
	l.copyResultsBtn = widget.NewButtonWithIcon("复制结果", theme.ContentCopyIcon(), func() { l.d.app.Clipboard().SetContent(formatLogEntries(l.shown)) })
	l.packetBtn = widget.NewButton("查看报文", func() {
		for _, e := range l.shown {
			if e.ID == l.selected {
				l.d.showLogPacket(e)
				return
			}
		}
	})
	l.exportBtn = widget.NewButtonWithIcon("导出", theme.DocumentSaveIcon(), l.export)
	l.copyBtn.Disable()
	l.copyResultsBtn.Disable()
	l.packetBtn.Disable()
	l.exportBtn.Disable()
	filters := container.NewBorder(nil, nil, container.NewHBox(l.level, l.category), nil, l.search)
	actions := container.NewBorder(nil, nil, l.count, container.NewHBox(l.packetBtn, l.copyBtn, l.copyResultsBtn, l.exportBtn))
	listArea := container.NewStack(l.list, container.NewCenter(l.empty))
	split := container.NewHSplit(listArea, container.NewScroll(l.detail))
	split.Offset = .5
	l.root = container.NewBorder(container.NewVBox(filters, actions), nil, nil, nil, split)
	l.level.SetSelected("全部级别")
	l.category.SetSelected("全部分类")
	l.applyFilter()
	return l
}

func (l *logView) panel() fyne.CanvasObject { return l.root }

func (l *logView) refresh(entries []service.LogEntry) {
	if slices.Equal(l.entries, entries) {
		return
	}
	l.entries = append(l.entries[:0], entries...)
	l.applyFilter()
}

func (l *logView) applyFilter() {
	if l.list == nil {
		return
	}
	query := strings.ToLower(strings.TrimSpace(l.search.Text))
	level, category := l.level.Selected, l.category.Selected
	shown := make([]service.LogEntry, 0, len(l.entries))
	for i := len(l.entries) - 1; i >= 0; i-- {
		e := l.entries[i]
		if level != "" && level != "全部级别" && string(e.Level) != level {
			continue
		}
		if category != "" && category != "全部分类" && logCategoryName(e.Category) != category {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(fmt.Sprintf("%s %s %s %d", e.Message, e.Target, e.ConnectionID, e.RequestID)), query) {
			continue
		}
		shown = append(shown, e)
	}
	oldIndex, newIndex := -1, -1
	for i, e := range l.shown {
		if e.ID == l.selected && l.selected != 0 {
			oldIndex = i
			break
		}
	}
	for i, e := range shown {
		if e.ID == l.selected && l.selected != 0 {
			newIndex = i
			break
		}
	}
	l.shown = shown
	if newIndex < 0 && l.selected != 0 {
		l.list.UnselectAll()
		l.selected = 0
		setText(l.detail, "选择一条日志查看详情", widget.MediumImportance)
		enable(l.copyBtn, false)
		enable(l.packetBtn, false)
	} else if newIndex >= 0 && newIndex != oldIndex {
		l.list.Select(newIndex)
	}
	l.list.Refresh()
	empty := "暂无符合条件的日志"
	if len(l.entries) == 0 {
		empty = "暂无日志"
	}
	setText(l.empty, empty, widget.MediumImportance)
	visible(l.empty, len(shown) == 0)
	enable(l.copyResultsBtn, len(shown) > 0)
	enable(l.exportBtn, len(shown) > 0)
	setText(l.count, fmt.Sprintf("显示 %d / %d 条", len(shown), len(l.entries)), widget.MediumImportance)
}

func logCategoryName(c service.LogCategory) string {
	switch c {
	case service.LogConnection:
		return "连接"
	case service.LogDiagnostic:
		return "诊断"
	case service.LogRead:
		return "读取"
	case service.LogWrite:
		return "写入"
	case service.LogPolling:
		return "轮询"
	case service.LogSystem:
		return "系统"
	default:
		return string(c)
	}
}

func formatLogEntry(e service.LogEntry) string {
	text := fmt.Sprintf("时间：%s\n级别：%s\n分类：%s\n", e.Time.Local().Format("2006-01-02 15:04:05.000"), e.Level, logCategoryName(e.Category))
	if e.Target != "" {
		text += "目标：" + e.Target + "\n"
	}
	if e.ConnectionID != "" {
		text += "连接 ID：" + e.ConnectionID + "\n"
	}
	if e.RequestID != 0 {
		text += fmt.Sprintf("请求 ID：%d\n", e.RequestID)
	}
	return text + "\n" + e.Message
}

func formatLogEntries(entries []service.LogEntry) string {
	var out strings.Builder
	for i, e := range entries {
		if i > 0 {
			out.WriteString("\n\n")
		}
		out.WriteString(strings.ReplaceAll(formatLogEntry(e), "\r\n", "\n"))
	}
	return out.String()
}

func (l *logView) export() {
	// Capture the visible snapshot when the dialog opens.
	entries := append([]service.LogEntry(nil), l.shown...)
	save := dialog.NewFileSave(func(w fyne.URIWriteCloser, err error) {
		if err != nil {
			dialog.ShowError(err, l.d.win)
			return
		}
		if w == nil {
			return
		}
		_, writeErr := io.WriteString(w, "\ufeff"+strings.ReplaceAll(formatLogEntries(entries), "\n", "\r\n"))
		closeErr := w.Close()
		if writeErr != nil {
			dialog.ShowError(writeErr, l.d.win)
		} else if closeErr != nil {
			dialog.ShowError(closeErr, l.d.win)
		}
	}, l.d.win)
	save.SetFileName("S7AIStudio-日志.txt")
	save.SetConfirmText("导出")
	save.SetDismissText("取消")
	save.SetFilter(storage.NewExtensionFileFilter([]string{".txt"}))
	save.Show()
}
