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
	d.packets = widget.NewList(func() int { return len(d.shownPackets) }, func() fyne.CanvasObject {
		label := widget.NewLabel("15:00:00.000 RX #65535 读取 SUCCESS")
		label.Truncation = fyne.TextTruncateEllipsis
		return container.NewBorder(nil, nil, widget.NewIcon(theme.UploadIcon()), nil, label)
	}, func(id widget.ListItemID, o fyne.CanvasObject) {
		if id < 0 || id >= len(d.shownPackets) {
			return
		}
		e := d.shownPackets[id]
		row := o.(*fyne.Container).Objects
		dir := theme.UploadIcon()
		if e.Dir == device.DirRX {
			dir = theme.DownloadIcon()
		}
		row[1].(*widget.Icon).SetResource(dir)
		importance := widget.MediumImportance
		if e.Error != "" || (e.Status != device.StatusSuccess && e.Status != device.StatusSent) {
			importance = widget.DangerImportance
		}
		sequence := fmt.Sprintf("#%d", e.RequestID)
		if d.packetFilter == "连接" && e.ConnectionID != "" {
			sequence = e.ConnectionID[:min(6, len(e.ConnectionID))] + "/" + sequence
		}
		setText(row[0].(*widget.Label), fmt.Sprintf("%s %s %s %s %s", e.Time.Local().Format("15:04:05.000"), e.Dir, sequence, packetOperation(e.Operation), e.Status), importance)
	})
	d.packets.OnSelected = func(id widget.ListItemID) {
		packets := d.shownPackets
		if id < 0 || id >= len(packets) {
			return
		}
		d.packetSelection = id
		d.wire.SetText(packetText(packets[id]))
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
	filter := widget.NewSelect([]string{"全部", "连接", "读取"}, func(value string) {
		d.packets.UnselectAll()
		d.packetSelection = -1
		d.wire.SetText("")
		d.packetFilter = value
		d.refreshPackets()
	})
	filter.SetSelected("全部")
	hint := widget.NewLabel("全部/读取取自最近 256 条报文。连接保留跨会话的最近 64 条。")
	hint.Importance = widget.LowImportance
	hint.Truncation = fyne.TextTruncateEllipsis
	split := container.NewHSplit(d.packets, container.NewScroll(d.wire))
	split.Offset = .42
	return container.NewBorder(container.NewBorder(nil, nil, hint, container.NewHBox(filter, d.packetCount, d.copyPacketBtn)), nil, nil, nil, split)
}

func packetOperation(operation string) string {
	switch operation {
	case "CONNECT":
		return "连接握手"
	case "SETUP":
		return "PDU 协商"
	case "CONNECTION_STATUS":
		return "连接统计"
	case "READ":
		return "读取"
	case "WRITE":
		return "写入"
	default:
		return operation
	}
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

// filteredPackets retains the snapshot order, so a selected row always refers to the visible event.
func filteredPackets(packets []device.Event, filter string) []device.Event {
	if filter != "连接" && filter != "读取" {
		return packets
	}
	result := make([]device.Event, 0, len(packets))
	for _, e := range packets {
		connection := e.Operation == "CONNECT" || e.Operation == "SETUP" || e.Operation == "CONNECTION_STATUS"
		if (filter == "连接" && connection) || (filter == "读取" && e.Operation == "READ") {
			result = append(result, e)
		}
	}
	return result
}

func (d *Desktop) visiblePackets() []device.Event {
	if d.packetFilter == "连接" && d.snapshot.ConnectionPackets != nil {
		return d.snapshot.ConnectionPackets
	}
	return filteredPackets(d.snapshot.Packets, d.packetFilter)
}

func (d *Desktop) refreshPackets() {
	old := d.shownPackets
	packets := d.visiblePackets()
	d.shownPackets = packets
	selected := d.packetSelection
	if selected >= 0 && selected < len(old) && old[selected].ConnectionID != "" {
		packet := old[selected]
		selected = -1
		for i, candidate := range packets {
			if candidate.ConnectionID == packet.ConnectionID && candidate.RequestID == packet.RequestID && candidate.Dir == packet.Dir {
				selected = i
				break
			}
		}
	}
	switch {
	case selected >= 0 && selected < len(packets):
		if selected != d.packetSelection {
			d.packets.Select(selected)
		} else {
			setText(d.wire, packetText(packets[selected]), widget.MediumImportance)
		}
	case d.packetSelection >= 0:
		d.packets.UnselectAll()
	}
	d.packets.Refresh()
	setText(d.packetCount, fmt.Sprintf("%s · 显示 %d 条", packetSummary(packets), len(packets)), widget.LowImportance)
}

func packetText(e device.Event) string {
	var out strings.Builder
	fmt.Fprintf(&out, "时间：%s\n方向：%s\n操作：%s\n请求 ID：%d\n状态：%s\n错误：%s\n连接：%s\n", e.Time.Local().Format("2006-01-02 15:04:05.000"), e.Dir, e.Operation, e.RequestID, e.Status, e.Error, e.ConnectionID)
	if e.Dir == device.DirRX {
		fmt.Fprintf(&out, "往返时间：%s\n", e.RTT)
	}
	raw := e.Raw
	if len(raw) >= 4 && raw[0] == 3 {
		fmt.Fprintf(&out, "TPKT 声明长度：%d 字节\n", binary.BigEndian.Uint16(raw[2:4]))
		if int(binary.BigEndian.Uint16(raw[2:4])) != len(raw) {
			out.WriteString("报文不完整或长度不一致，以下仅显示可解析字段。\n")
		}
	}
	if e.Operation == "CONNECT" && len(raw) >= 11 && raw[0] == 3 && (raw[5] == 0xe0 || raw[5] == 0xd0) {
		end := 5 + int(raw[4])
		if end > len(raw) {
			end = len(raw)
		}
		for i := 11; i+2 <= end; {
			code, n := raw[i], int(raw[i+1])
			i += 2
			if i+n > end {
				break
			}
			switch code {
			case 0xc1, 0xc2:
				if n == 2 {
					label := "源 TSAP"
					if code == 0xc2 {
						label = "目标 TSAP"
					}
					fmt.Fprintf(&out, "%s：0x%04X\n", label, binary.BigEndian.Uint16(raw[i:i+2]))
				}
			case 0xc0:
				if n == 1 && raw[i] < 31 {
					fmt.Fprintf(&out, "TPDU 大小：%d 字节\n", uint64(1)<<raw[i])
				}
			}
			i += n
		}
	}
	if len(raw) >= 17 && raw[0] == 3 && raw[4] == 2 && raw[5] == 0xf0 && raw[7] == 0x32 {
		fmt.Fprintf(&out, "PDU 引用：%d\n", binary.BigEndian.Uint16(raw[11:13]))
		header := 10
		if raw[8] == 2 || raw[8] == 3 {
			header = 12
		}
		start := 7 + header
		paramLength := int(binary.BigEndian.Uint16(raw[13:15]))
		if e.Operation == "SETUP" && paramLength == 8 && len(raw) >= start+8 && raw[start] == 0xf0 {
			pduLengthLabel := "协商 PDU 长度"
			if e.Dir == device.DirTX {
				pduLengthLabel = "请求 PDU 长度"
			}
			fmt.Fprintf(&out, "MaxAmQ 调用方：%d\nMaxAmQ 被调用方：%d\n%s：%d 字节\nMaxAmQ 表示单个 S7 会话允许并发的 PDU 作业数。\n", binary.BigEndian.Uint16(raw[start+2:start+4]), binary.BigEndian.Uint16(raw[start+4:start+6]), pduLengthLabel, binary.BigEndian.Uint16(raw[start+6:start+8]))
		}
		if e.Operation == "CONNECTION_STATUS" && raw[8] == 7 {
			data := start + paramLength
			if len(raw) >= data+8 && raw[data] == 0xff && raw[data+1] == 9 {
				id := binary.BigEndian.Uint16(raw[data+4 : data+6])
				if id == 0x0131 || id == 0x0132 {
					fmt.Fprintf(&out, "SZL ID：0x%04X  索引：0x%04X\n", id, binary.BigEndian.Uint16(raw[data+6:data+8]))
				}
			}
		}
	}
	for i, target := range e.Targets {
		address := target.String()
		if target.Original != "" && target.Original != address {
			address = fmt.Sprintf("%s（报文地址 %s）", target.Original, address)
		}
		fmt.Fprintf(&out, "测点 %d：%s  类型：%s  数量：%d\n", i+1, address, target.Type, target.Count)
		if i < len(e.Results) && e.Results[i].Read != nil {
			r := e.Results[i].Read
			fmt.Fprintf(&out, "  质量：%s", r.Quality)
			if r.Error != "" {
				fmt.Fprintf(&out, "  错误：%s", r.Error)
			} else {
				fmt.Fprintf(&out, "  值：%s", device.FormatValue(r.Value))
			}
			out.WriteByte('\n')
		}
	}
	fmt.Fprintf(&out, "\n真实报文字节（%d bytes）\n%s", len(raw), hex.Dump(raw))
	return out.String()
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
