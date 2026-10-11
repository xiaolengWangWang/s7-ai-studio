// Package ui contains the Chinese single-workspace desktop.
package ui

import (
	"context"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"log"
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
	heading, status, recordStatus, writeStatus                                                               *widget.Label
	family                                                                                                   *widget.Select
	target, rack, slot, localTSAP, remoteTSAP, timeout, vdb, voffset, addresses, count, interval, writeValue *widget.Entry
	kind                                                                                                     *widget.Select
	connectBtn, disconnectBtn, demoBtn, stopDemoBtn, readBtn, pollBtn, stopPollBtn, writeBtn                 *widget.Button
	allowWrite                                                                                               *widget.Check
	confirmation                                                                                             *dialog.ConfirmDialog
	table                                                                                                    *widget.Table
	rows                                                                                                     [][]string
	packets                                                                                                  *widget.List
	wire                                                                                                     *widget.Entry
	logs                                                                                                     *widget.Label
}

func NewDesktop(a fyne.App, c *service.Controller, version string) *Desktop {
	ctx, cancel := context.WithCancel(context.Background())
	d := &Desktop{app: a, controller: c, ctx: ctx, cancel: cancel}
	d.win = a.NewWindow("S7 AI Studio — " + version)
	d.win.SetIcon(icon.Application())
	d.heading = widget.NewLabelWithStyle("S7 AI Studio  "+version+" · GUI 预览版", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	d.status = widget.NewLabel("未连接")
	d.status.Wrapping = fyne.TextWrapWord
	d.recordStatus = widget.NewLabel("")
	d.recordStatus.Wrapping = fyne.TextWrapBreak
	top := container.NewVBox(d.heading, d.connectionPanel(), d.status)
	tabs := container.NewAppTabs(container.NewTabItem("点位与读写", d.pointsPanel()), container.NewTabItem("报文", d.packetPanel()), container.NewTabItem("日志", d.logPanel()))
	d.win.SetContent(container.NewPadded(container.NewBorder(top, d.recordStatus, nil, nil, tabs)))
	w, h := platform.WorkArea()
	d.win.Resize(platform.FitSize(fyne.NewSize(1180, 820), w, h, d.win.Canvas().Scale()))
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
func enable(w *widget.Button, yes bool) {
	if yes {
		w.Enable()
	} else {
		w.Disable()
	}
}
func (d *Desktop) updateControls() {
	s := d.snapshot
	ready := s.State == "connected"
	idle := d.busy == 0
	enable(d.connectBtn, idle)
	enable(d.disconnectBtn, s.State == "connecting" || ready || s.State == "error")
	enable(d.demoBtn, idle && s.DemoTarget == "")
	enable(d.stopDemoBtn, s.DemoTarget != "")
	enable(d.readBtn, ready && idle)
	enable(d.pollBtn, ready && idle && !s.Polling)
	enable(d.stopPollBtn, s.Polling)
	enable(d.writeBtn, ready && idle && d.allowWrite.Checked)
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
	states := map[string]string{"disconnected": "未连接", "connecting": "连接中…", "connected": "已连接", "error": "通讯失败，需重新连接", "closed": "已关闭"}
	text := states[s.State]
	if s.Target != "" {
		text += "  " + s.Target
	}
	if s.DemoTarget != "" {
		text += "  | 本机演示：合成协议对端"
	}
	if s.Polling {
		text += "  | 轮询中"
	}
	if s.LastError != "" {
		text += "\n" + s.LastError
	}
	d.status.SetText(text)
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
	writeText := "写入默认关闭。勾选允许写入后，仍须确认目标与数值。"
	if r := s.WriteResult; r != nil {
		writeText = "写入 " + r.State + " · 读回" + s.Readback
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
	d.writeStatus.SetText(writeText)
	d.packets.Refresh()
	d.logs.SetText(joinLogs(s.Logs))
	record := "记录目录：" + s.RecordDir + "  · 按天 / 50 MB 轮转"
	if s.RecordError != "" {
		record += "\n记录失败：" + s.RecordError
	} else {
		record += "  · 异步记录启用"
	}
	d.recordStatus.SetText(record)
	d.updateControls()
}
