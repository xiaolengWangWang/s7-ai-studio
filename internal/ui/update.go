package ui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"runtime"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"s7-ai-studio/internal/update"
)

type updateView struct {
	d                    *Desktop
	current, cacheDir    string
	status               *widget.Label
	notes                *widget.RichText
	progress             *widget.ProgressBar
	checkBtn, installBtn *widget.Button
	cancelBtn            *widget.Button
	confirmation         *dialog.ConfirmDialog
	release              update.Release
	cancel               context.CancelFunc
	pending, restarting  bool
	canInstall           func() bool
	install              func(string) (string, error)
	restart              func(string) error
}

func newUpdateView(d *Desktop, version string) *updateView {
	u := &updateView{d: d, current: version, cacheDir: update.CacheDir(), canInstall: update.CanInstall, install: update.Install, restart: update.Restart}
	u.status = widget.NewLabel("点击“检查更新”获取最新正式版。")
	u.status.Wrapping = fyne.TextWrapWord
	u.notes = widget.NewRichText()
	u.notes.Wrapping = fyne.TextWrapWord
	u.progress = widget.NewProgressBar()
	u.progress.Hide()
	u.checkBtn = widget.NewButtonWithIcon("检查更新", theme.ViewRefreshIcon(), u.check)
	u.installBtn = widget.NewButtonWithIcon("下载并安装更新", theme.DownloadIcon(), u.confirmInstall)
	u.cancelBtn = widget.NewButton("取消", func() {
		if u.cancel != nil {
			u.cancel()
		}
	})
	u.cancelBtn.Hide()
	u.installBtn.Disable()
	return u
}

func (u *updateView) panel() fyne.CanvasObject {
	page, _ := url.Parse(update.PageURL())
	return container.NewVScroll(container.NewVBox(
		widget.NewLabelWithStyle("当前版本："+u.current+" 正式版", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		container.NewHBox(u.checkBtn, u.installBtn, u.cancelBtn, widget.NewHyperlink("手动下载", page)),
		u.status, u.progress,
		widget.NewLabel("安装前请断开 PLC 并停止本机演示。更新会保存记录、关闭当前程序并重启。"),
		u.notes))
}

func (u *updateView) ready() bool {
	s := u.d.controller.Snapshot()
	a, ok := u.release.Asset(runtime.GOOS, runtime.GOARCH)
	_, checked := u.release.Checksum(a.Name)
	return !u.pending && !u.restarting && u.d.busy == 0 && u.canInstall() &&
		update.Newer(u.release.Version(), u.current) && ok && checked && a.URL != "" &&
		s.State != "connected" && s.State != "connecting" && s.State != "closed" && s.DemoTarget == ""
}

func (u *updateView) refresh() {
	enable(u.checkBtn, !u.pending && !u.restarting && u.d.busy == 0)
	enable(u.installBtn, u.ready())
	visible(u.cancelBtn, u.pending && !u.restarting)
}

// operation uses the desktop worker lifecycle so closing the app cancels requests
// and waits for them before records are closed and the process exits.
func (u *updateView) operation(message string, fn func(context.Context) (string, error)) {
	ctx, cancel := context.WithCancel(u.d.ctx)
	u.cancel, u.pending = cancel, true
	u.status.SetText(message)
	u.d.run(func(context.Context) error {
		target, err := fn(ctx)
		cancel()
		fyne.DoAndWait(func() {
			if u.d.ctx.Err() != nil {
				return
			}
			u.cancel, u.pending = nil, false
			if err != nil {
				u.restarting = false
				if errors.Is(err, context.Canceled) {
					u.status.SetText("更新操作已取消，可重新检查或继续下载。")
				} else {
					u.status.SetText("更新失败：" + err.Error())
				}
			} else if target != "" {
				u.restarting = true
				u.status.SetText("更新已安装，正在保存记录并重启…")
				go u.finishRestart(target)
			}
			u.refresh()
		})
		return nil
	})
}

func (u *updateView) check() {
	if u.pending || u.restarting || u.d.busy != 0 {
		return
	}
	u.operation("正在检查 Gitee / GitHub 正式版…", func(ctx context.Context) (string, error) {
		ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		rel, err := update.Latest(ctx)
		if err != nil {
			return "", err
		}
		fyne.DoAndWait(func() {
			if u.d.ctx.Err() != nil {
				return
			}
			u.release = rel
			u.notes.ParseMarkdown(rel.Notes())
			if update.Newer(rel.Version(), u.current) {
				u.status.SetText(fmt.Sprintf("发现新版本 %s（%s），请查看更新说明。", rel.Version(), rel.Source))
				if !u.canInstall() {
					u.status.SetText(u.status.Text + " 当前运行方式请使用手动下载。")
				}
			} else {
				u.status.SetText("当前已是最新正式版（" + u.current + "）。")
			}
		})
		return "", nil
	})
}

func (u *updateView) confirmInstall() {
	if !u.ready() {
		return
	}
	release := u.release
	u.confirmation = dialog.NewConfirm("安装软件更新", "下载并校验版本 "+release.Version()+"，安装成功后保存记录并重启软件。是否继续？", func(yes bool) {
		if !yes {
			return
		}
		if release.Tag != u.release.Tag {
			u.status.SetText("版本信息已变化，请重新确认安装。")
			return
		}
		if !u.ready() {
			return
		}
		a, _ := release.Asset(runtime.GOOS, runtime.GOARCH)
		sum, _ := release.Checksum(a.Name)
		u.progress.SetValue(0)
		u.progress.Show()
		u.operation("正在下载并校验安装包…", func(ctx context.Context) (string, error) {
			pkg, err := update.Download(ctx, a, sum, u.cacheDir, func(done, total int64) {
				fyne.Do(func() {
					if u.d.ctx.Err() == nil && u.pending {
						if total > 0 {
							u.progress.SetValue(float64(done) / float64(total))
						}
					}
				})
			})
			if err != nil {
				return "", err
			}
			if err := ctx.Err(); err != nil {
				return "", err
			}
			fyne.DoAndWait(func() {
				if ctx.Err() != nil {
					return
				}
				u.restarting = true
				u.status.SetText("校验通过，正在安装更新…")
				u.refresh()
			})
			if err := ctx.Err(); err != nil {
				return "", err
			}
			return u.install(pkg)
		})
	}, u.d.win)
	u.confirmation.SetConfirmText("下载并安装")
	u.confirmation.SetDismissText("取消")
	u.confirmation.Show()
}

func (u *updateView) finishRestart(target string) {
	err := u.d.Shutdown()
	if err == nil {
		err = u.restart(target)
	}
	fyne.Do(func() {
		if err != nil {
			u.status.SetText("更新已安装，请手动重新启动软件：" + err.Error())
			return
		}
		u.d.win.SetCloseIntercept(nil)
		u.d.win.Close()
		u.d.app.Quit()
	})
}
