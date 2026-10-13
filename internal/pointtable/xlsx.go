package pointtable

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

// 最小的 xlsx 读取：只取单元格文字（公式取缓存结果），不处理合并单元格和日期格式。

type sheet struct {
	name string
	rows []row
}
type row struct {
	num   int // Excel 行号，从 1 开始
	cells []string
}

const maxXLSXBytes = 64 << 20 // 解压后的 XML 总量，避免小文件解压占满内存

func readXLSX(data []byte) ([]sheet, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, errors.New("不是 xlsx 文件")
	}
	files := map[string]*zip.File{}
	for _, f := range z.File {
		files[f.Name] = f
	}
	remaining := int64(maxXLSXBytes)
	read := func(name string) ([]byte, error) {
		f, ok := files[name]
		if !ok {
			return nil, fmt.Errorf("xlsx 缺少 %s", name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		b, err := io.ReadAll(io.LimitReader(rc, remaining+1))
		if int64(len(b)) > remaining {
			return nil, errors.New("xlsx 解压后超过 64 MB，请删除无关工作表后导入")
		}
		remaining -= int64(len(b))
		return b, err
	}
	var shared []string
	if _, ok := files["xl/sharedStrings.xml"]; ok {
		b, err := read("xl/sharedStrings.xml")
		if err != nil {
			return nil, err
		}
		var sst struct {
			Items []xlsxText `xml:"si"`
		}
		if err := xml.Unmarshal(b, &sst); err != nil {
			return nil, err
		}
		for _, it := range sst.Items {
			shared = append(shared, it.text())
		}
	}
	b, err := read("xl/workbook.xml")
	if err != nil {
		return nil, err
	}
	var wb struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			RID  string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := xml.Unmarshal(b, &wb); err != nil {
		return nil, err
	}
	targets := map[string]string{}
	if _, ok := files["xl/_rels/workbook.xml.rels"]; ok {
		b, err := read("xl/_rels/workbook.xml.rels")
		if err != nil {
			return nil, err
		}
		var rels struct {
			Rels []struct {
				ID     string `xml:"Id,attr"`
				Target string `xml:"Target,attr"`
			} `xml:"Relationship"`
		}
		if err := xml.Unmarshal(b, &rels); err != nil {
			return nil, err
		}
		for _, r := range rels.Rels {
			t := strings.TrimPrefix(r.Target, "/")
			if !strings.HasPrefix(t, "xl/") {
				t = path.Join("xl", t)
			}
			targets[r.ID] = t
		}
	}
	var out []sheet
	for i, s := range wb.Sheets {
		name := targets[s.RID]
		if name == "" {
			name = fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1)
		}
		b, err := read(name)
		if err != nil {
			return nil, err
		}
		rows, err := parseSheet(b, shared)
		if err != nil {
			return nil, fmt.Errorf("工作表“%s”：%w", s.Name, err)
		}
		out = append(out, sheet{name: s.Name, rows: rows})
	}
	return out, nil
}

// xlsxText 是共享字符串或行内字符串：普通文字在 <t> 里，带格式的文字分成多个 <r><t>。
type xlsxText struct {
	T    string `xml:"t"`
	Runs []struct {
		T string `xml:"t"`
	} `xml:"r"`
}

func (x xlsxText) text() string {
	if len(x.Runs) == 0 {
		return x.T
	}
	var sb strings.Builder
	for _, r := range x.Runs {
		sb.WriteString(r.T)
	}
	return sb.String()
}

func parseSheet(b []byte, shared []string) ([]row, error) {
	decoder := xml.NewDecoder(bytes.NewReader(b))
	var rows []row
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return rows, nil
		}
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "row" {
			continue
		}
		var r struct {
			Num   int `xml:"r,attr"`
			Cells []struct {
				Ref    string   `xml:"r,attr"`
				Type   string   `xml:"t,attr"`
				Value  string   `xml:"v"`
				Inline xlsxText `xml:"is"`
			} `xml:"c"`
		}
		if err := decoder.DecodeElement(&r, &start); err != nil {
			return nil, err
		}
		if r.Num == 0 {
			r.Num = len(rows) + 1
		}
		var cells []string
		next := 0
		for _, c := range r.Cells {
			col := next
			if c.Ref != "" {
				col = colIndex(c.Ref) // 空单元格不写进文件，按引用补齐
			}
			if col < 0 || col >= 256 {
				continue // 点表只用前几列；远处的格式区域不展开
			}
			next = col + 1
			v := c.Value
			switch c.Type {
			case "s":
				i, err := strconv.Atoi(v)
				if err != nil || i < 0 || i >= len(shared) {
					return nil, fmt.Errorf("单元格 %s 引用了不存在的字符串", c.Ref)
				}
				v = shared[i]
			case "inlineStr":
				v = c.Inline.text()
			case "", "n":
				// 85.0 按 Excel 显示的样子写回 85
				if f, err := strconv.ParseFloat(v, 64); err == nil {
					v = strconv.FormatFloat(f, 'f', -1, 64)
				}
			}
			if strings.TrimSpace(v) == "" {
				continue
			}
			if col >= len(cells) {
				cells = append(cells, make([]string, col+1-len(cells))...)
			}
			cells[col] = v
		}
		if len(cells) > 0 {
			rows = append(rows, row{num: r.Num, cells: cells})
		}
	}
}

// colIndex 把 “AB12” 这样的单元格引用换成从 0 开始的列号。
func colIndex(ref string) int {
	n := 0
	for _, ch := range ref {
		if ch < 'A' || ch > 'Z' {
			break
		}
		n = n*26 + int(ch-'A'+1)
		if n > 16384 {
			return -1
		}
	}
	return n - 1
}
