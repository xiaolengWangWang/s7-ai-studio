// Package ui contains the Chinese single-workspace desktop.
package ui

import (
	"context"
	"errors"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"log"
	"os"
	"s7-ai-studio/assets/icon"
	"s7-ai-studio/internal/service"
	"s7-ai-studio/platform"
	"sync"
	"time"
)

type Desktop struct {
	app                                                                                                      fyne.App
	win                                                                                                      fyne.Window
	controller                                                                                               *service.Controller
	ctx                                                                                                      context.Context
	cancel                                                                                                   context.CancelFunc
	workerMu                                                                                                 sync.Mutex
	workers, refreshWorkers                                                                                  sync.WaitGroup
	stopped                                                                                                  bool
	once, showOnce                                                                                           sync.Once
	shutdownErr                                                                                              error
	busy                                                                                                     int
	revision, generation                                                                                     uint64
	snapshot                                                                                                 service.Snapshot
	heading, status, lastError, recordStatus, writeStatus, emptyHint, packetCount, logCount                  *widget.Label
	activity                                                                                                 *widget.Activity
	family                                                                                                   *widget.Select
	target, rack, slot, localTSAP, remoteTSAP, timeout, vdb, voffset, addresses, count, interval, writeValue *widget.Entry
	kind                                                                                                     *widget.Select
	connectBtn, disconnectBtn, demoBtn, stopDemoBtn, readBtn, pollBtn, stopPollBtn, writeBtn                 *widget.Button
	copyPacketBtn, copyLogBtn, openRecordBtn                                                                 *widget.Button
	allowWrite                                                                                               *widget.Check
	confirmation                                                                                             *dialog.ConfirmDialog
	table                                                                                                    *widget.Table
	rows                                                                                                     [][]string
	packets                                                                                                  *widget.List
	packetSelection                                                                                          int
	wire, logs                                                                                               *widget.Label
	logScroll                                                                                                *container.Scroll
}

func NewDesktop(a fyne.App, c *service.Controller, version string) *Desktop {
	ctx, cancel := context.WithCancel(context.Background())
	d := &Desktop{app: a, controller: c, ctx: ctx, cancel: cancel, packetSelection: -1}
	d.win = a.NewWindow("S7 AI Studio " + version)
	d.win.SetIcon(icon.Application())
	d.heading = widget.NewLabelWithStyle("S7 AI Studio  "+version+" · GUI 预览版", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	d.status = widget.NewLabelWithStyle("未连接", fyne.TextAlignTrailing, fyne.TextStyle{Bold: true})
	d.activity = widget.NewActivity()
	d.activity.Hide()
	d.lastError = widget.NewLabel("")
	d.lastError.Importance = widget.DangerImportance
	d.lastError.Wrapping = fyne.TextWrapWord
	d.lastError.Hide()
	d.recordStatus = widget.NewLabel("")
	d.recordStatus.Wrapping = fyne.TextWrapBreak
	d.openRecordBtn = widget.NewButtonWithIcon("打开记录目录", theme.FolderOpenIcon(), d.openRecords)
	header := container.NewBorder(nil, nil, d.heading, container.NewHBox(d.activity, d.status))
	top := container.NewVBox(header, d.connectionPanel(), d.lastError)
	tabs := container.NewAppTabs(
		container.NewTabItemWithIcon("点位与读写", theme.ListIcon(), d.pointsPanel()),
		container.NewTabItemWithIcon("报文", theme.StorageIcon(), d.packetPanel()),
		container.NewTabItemWithIcon("日志", theme.DocumentIcon(), d.logPanel()))
	body := container.NewVScroll(container.NewBorder(top, nil, nil, nil, tabs))
	footer := container.NewBorder(widget.NewSeparator(), nil, nil, d.openRecordBtn, d.recordStatus)
	d.win.SetContent(container.NewPadded(container.NewBorder(nil, footer, nil, nil, body)))
	w, h := platform.WorkArea()
	d.win.Resize(platform.FitSize(fyne.NewSize(1180, 820), w, h, d.win.Canvas().Scale()))
	d.loadInputs()
	d.refresh(c.Snapshot())
	d.win.SetCloseIntercept(func() {
		d.cancel()
		go func() {
			if e := d.Shutdown(); e != nil {
				log.Printf("保存记录/退出失败: %v", e)
			}
			fyne.Do(func() { d.win.SetCloseIntercept(nil); d.win.Close(); a.Quit() })
		}()
	})
	return d
}
func (d *Desktop) Window() fyne.Window { return d.win }
func (d *Desktop) Show() {
	d.showOnce.Do(func() {
		d.refreshWorkers.Add(1)
		go func() {
			defer d.refreshWorkers.Done()
			tick := time.NewTicker(250 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case <-d.ctx.Done():
					return
				case <-tick.C:
					s := d.controller.Snapshot()
					fyne.Do(func() {
						if d.ctx.Err() == nil {
							d.refresh(s)
						}
					})
				}
			}
		}()
	})
	d.win.Show()
}
func (d *Desktop) Shutdown() error {
	d.once.Do(func() {
		d.workerMu.Lock()
		d.stopped = true
		d.cancel()
		d.workerMu.Unlock()
		d.shutdownErr = d.controller.Close()
		d.workers.Wait()
		d.refreshWorkers.Wait()
	})
	return d.shutdownErr
}
func (d *Desktop) run(fn func(context.Context) error) {
	d.workerMu.Lock()
	if d.stopped {
		d.workerMu.Unlock()
		return
	}
	d.workers.Add(1)
	d.workerMu.Unlock()
	d.busy++
	d.updateControls()
	go func() {
		defer d.workers.Done()
		e := fn(d.ctx)
		fyne.Do(func() {
			if d.ctx.Err() != nil {
				return
			}
			d.busy--
			d.refresh(d.controller.Snapshot())
			d.updateControls()
			if e != nil {
				dialog.ShowError(e, d.win)
			}
		})
	}()
}
func (d *Desktop) openRecords() {
	dir := d.snapshot.RecordDir
	if info, e := os.Stat(dir); e != nil || !info.IsDir() {
		dialog.ShowError(errors.New("记录目录不可用："+dir), d.win)
		return
	}
	if e := platform.OpenFile(dir); e != nil {
		dialog.ShowError(e, d.win)
	}
}
func enable(w *widget.Button, yes bool) {
	if yes {
		w.Enable()
	} else {
		w.Disable()
	}
}
func visible(o fyne.CanvasObject, yes bool) {
	if yes {
		o.Show()
	} else {
		o.Hide()
	}
}

// setText 只在内容变化时更新，避免每 250 ms 刷新打断用户选中文本。
func setText(l *widget.Label, text string, importance widget.Importance) {
	if l.Text != text || l.Importance != importance {
		l.Importance = importance
		l.SetText(text)
	}
}
func (d *Desktop) updateControls() {
	s := d.snapshot
	ready := s.State == "connected"
	idle := d.busy == 0
	enable(d.connectBtn, idle)
	enable(d.disconnectBtn, s.State == "connecting" || ready || s.State == "error")
	visible(d.demoBtn, s.DemoTarget == "")
	enable(d.demoBtn, idle && s.DemoTarget == "")
	visible(d.stopDemoBtn, s.DemoTarget != "")
	enable(d.readBtn, ready && idle)
	visible(d.pollBtn, !s.Polling)
	enable(d.pollBtn, ready && idle && !s.Polling)
	visible(d.stopPollBtn, s.Polling)
	enable(d.writeBtn, ready && idle && d.allowWrite.Checked)
	enable(d.copyPacketBtn, d.wire.Text != "")
	enable(d.copyLogBtn, d.logs.Text != "")
	visible(d.activity, !idle || s.State == "connecting")
	if d.activity.Visible() {
		d.activity.Start()
	} else {
		d.activity.Stop()
	}
}

var stateText = map[string]string{"disconnected": "未连接", "connecting": "连接中…", "connected": "已连接", "error": "通讯失败，需重新连接", "closed": "已关闭"}
var stateImportance = map[string]widget.Importance{"connecting": widget.WarningImportance, "connected": widget.SuccessImportance, "error": widget.DangerImportance}

func writeImportance(state, readback string) widget.Importance {
	switch {
	case state == "CONFIRMED" && readback == "一致":
		return widget.SuccessImportance
	case state == "REJECTED":
		return widget.DangerImportance
	}
	return widget.WarningImportance
}
func (d *Desktop) refresh(s service.Snapshot) {
	if s.Revision < d.revision {
		return
	}
	d.revision = s.Revision
	if d.generation != s.Generation {
		d.generation = s.Generation
		d.allowWrite.SetChecked(false)
		if s.DemoTarget != "" && s.Target == s.DemoTarget {
			d.target.SetText(s.Target)
			d.family.SetSelected("s7-300")
			d.addresses.SetText("DB1.DBD0")
			d.kind.SetSelected("DINT")
			d.count.SetText("1")
		}
	}
	d.snapshot = s
	text := "● " + stateText[s.State]
	if s.Target != "" {
		text += "  " + s.Target
	}
	if s.DemoTarget != "" {
		text += "  · 本机演示（合成协议对端）"
	}
	if s.Polling {
		text += "  · 轮询中"
	}
	setText(d.status, text, stateImportance[s.State])
	setText(d.lastError, s.LastError, widget.DangerImportance)
	visible(d.lastError, s.LastError != "")
	d.rows = nil
	for _, r := range s.Results {
		addr := r.Address.Original
		if addr == "" {
			addr = r.Address.String()
		}
		value := "—"
		if r.Quality == "GOOD" {
			value = service.FormatValue(r.Value)
		}
		d.rows = append(d.rows, []string{addr, string(r.Address.Type), value, r.Quality, r.Time.Local().Format("15:04:05.000"), r.Error})
	}
	d.table.Refresh()
	visible(d.emptyHint, len(d.rows) == 0)
	writeText, importance := "写入默认关闭。勾选“允许写入”后，仍须在确认框核对目标、地址、类型与值。", widget.LowImportance
	if r := s.WriteResult; r != nil {
		writeText, importance = "写入 "+r.State+" · 读回"+s.Readback, writeImportance(r.State, s.Readback)
		if r.Error != "" {
			writeText += "\n" + r.Error
		}
		for _, part := range r.Segments {
			writeText += "\n" + part.Address.String() + "  " + part.State
			if part.Error != "" {
				writeText += "  " + part.Error
			}
		}
	}
	setText(d.writeStatus, writeText, importance)
	d.packets.Refresh()
	setText(d.packetCount, packetSummary(s.Packets), widget.LowImportance)
	if d.packetSelection >= 0 && d.packetSelection < len(s.Packets) {
		setText(d.wire, packetText(s.Packets[d.packetSelection]), widget.MediumImportance)
	} else if d.packetSelection >= 0 {
		d.packets.UnselectAll()
	}
	logs := joinLogs(s.Logs)
	if logs != d.logs.Text {
		d.logs.SetText(logs)
		d.logScroll.ScrollToBottom()
	}
	setText(d.logCount, logSummary(len(s.Logs)), widget.LowImportance)
	record, importance := "记录目录："+s.RecordDir+"  · 按天 / 50 MB 轮转  · 异步记录启用", widget.MediumImportance
	if s.RecordError != "" {
		record, importance = "记录目录："+s.RecordDir+"\n记录失败："+s.RecordError, widget.DangerImportance
	}
	setText(d.recordStatus, record, importance)
	d.updateControls()
}
