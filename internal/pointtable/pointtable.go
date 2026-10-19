// Package pointtable 读取 viSCADA 物模型属性表或 CSV 点表，并按期望值核对读数。
package pointtable

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"s7-ai-studio/internal/device"
)

// MaxPoints 与一次读取允许的点数一致。
const MaxPoints = 1024

// 核对结论。期望值为空时不核对，结论为空字符串。
const (
	Match    = "一致"
	Mismatch = "不一致"
	Failed   = "读取失败"
	Invalid  = "期望值无效"
)

type Point struct {
	Row                            int // 源文件行号，从 1 开始
	Address                        string
	Type                           device.DataType // 可为空：viSCADA 标识和位地址自带类型
	Name, Standard, Unit, Expected string
}

type Table struct {
	Name, Sheet string
	Points      []Point
	Skipped     []string // “第 N 行：原因”
}

// 表头别名按优先级排列；比较前去掉空格、星号和括号里的说明。
var columns = map[string][]string{
	"address":  {"属性标识", "viscada标识", "地址", "address"},
	"name":     {"属性名称", "变量名称", "变量名", "名称", "name"},
	"standard": {"标准化名称"},
	"unit":     {"单位", "unit"},
	"type":     {"s7类型", "类型", "数据类型", "type"},
	"expected": {"期望值", "模拟值", "验证值", "预期值", "expected"},
}

var bitAddress = regexp.MustCompile(`\.[0-9]+$`)

// Resolve 解析点位地址。类型列为空时，viSCADA 标识按标识前缀取类型，位地址按 BOOL。
func (p Point) Resolve() (device.Address, error) {
	t := p.Type
	if t == "" {
		if pt, ok := device.PlatformType(p.Address); ok {
			t = pt
		} else if bitAddress.MatchString(p.Address) {
			t = "BOOL"
		} else {
			return device.Address{}, fmt.Errorf("%s 缺少 S7 类型：类型列填 REAL、INT 等，或改用 viSCADA 标识（如 DB1.R0）", p.Address)
		}
	}
	return device.ParseAddress(p.Address, t, 1)
}

func Load(path string) (Table, error) {
	info, e := os.Stat(path)
	if e != nil {
		return Table{}, e
	}
	if info.Size() > 16<<20 {
		return Table{}, errors.New("点表文件超过 16 MB")
	}
	data, e := os.ReadFile(path)
	if e != nil {
		return Table{}, e
	}
	return Parse(filepath.Base(path), data)
}

// Parse 按扩展名读取 .xlsx 或 .csv。xlsx 优先取 viSCADA 导出的“设备类属性信息”工作表。
func Parse(name string, data []byte) (Table, error) {
	t := Table{Name: name}
	if bytes.Contains(data[:min(len(data), 32)], []byte("E-SafeNet")) {
		return t, errors.New("文件已被 E-SafeNet 透明加密，本程序只能读到密文；请改用浏览器从 viSCADA 直接下载的原始文件，或请管理员为本程序放行")
	}
	var rows []row
	switch strings.ToLower(filepath.Ext(name)) {
	case ".xlsx":
		sheets, e := readXLSX(data)
		if e != nil {
			return t, e
		}
		for i, s := range sheets {
			if s.name == "设备类属性信息" {
				sheets[0], sheets[i] = sheets[i], sheets[0]
				break
			}
		}
		for _, s := range sheets {
			if _, ok := header(s.rows); ok {
				t.Sheet, rows = s.name, s.rows
				break
			}
		}
	case ".csv":
		var e error
		if rows, e = readCSV(data); e != nil {
			return t, e
		}
	default:
		return t, errors.New("点表须为 .xlsx 或 .csv")
	}
	at, ok := header(rows)
	if !ok {
		return t, errors.New("没有找到“属性标识”或“地址”列")
	}
	cols := map[string]int{}
	for key, aliases := range columns {
		cols[key] = find(rows[at].cells, aliases)
	}
	seen := map[string]int{}
	for _, r := range rows[at+1:] {
		cell := func(key string) string {
			if i := cols[key]; i >= 0 && i < len(r.cells) {
				return strings.TrimSpace(r.cells[i])
			}
			return ""
		}
		p := Point{Row: r.num, Address: cell("address"), Name: cell("name"), Standard: cell("standard"), Unit: cell("unit"), Expected: cell("expected")}
		if p.Address == "" {
			continue
		}
		// viSCADA 的数据类型列是 1/3/6 等编码，不是 S7 类型；只认 S7 类型名。
		if kind := device.DataType(strings.ToUpper(cell("type"))); kind != "" {
			if _, e := device.ParseAddress("DB1.DBB0", kind, 1); e == nil {
				p.Type = kind
			}
		}
		skip := func(reason string) {
			t.Skipped = append(t.Skipped, fmt.Sprintf("第 %d 行 %s：%s", r.num, p.Address, reason))
		}
		a, e := p.Resolve()
		if e != nil {
			skip(e.Error())
			continue
		}
		key := a.String()
		if first, ok := seen[key]; ok {
			skip(fmt.Sprintf("与第 %d 行地址重复", first))
			continue
		}
		if p.Expected != "" {
			if _, e := device.ParseValue(a, p.Expected); e != nil {
				skip("期望值无效：" + e.Error())
				continue
			}
		}
		seen[key] = r.num
		t.Points = append(t.Points, p)
	}
	if len(t.Points) == 0 {
		return t, fmt.Errorf("点表没有可读取的 S7 点位%s", firstSkipped(t.Skipped))
	}
	if len(t.Points) > MaxPoints {
		return t, fmt.Errorf("点表有 %d 个点位，一次最多读取 %d 个，请拆分后导入", len(t.Points), MaxPoints)
	}
	return t, nil
}

func firstSkipped(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return "（" + s[0] + "）"
}

func readCSV(data []byte) ([]row, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(data) { // Excel 在中文 Windows 上另存的 CSV 是 GBK
		d, e := simplifiedchinese.GB18030.NewDecoder().Bytes(data)
		if e != nil {
			return nil, errors.New("CSV 既不是 UTF-8 也不是 GBK 编码")
		}
		data = d
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	var rows []row
	for {
		cells, e := r.Read()
		if e != nil {
			if errors.Is(e, io.EOF) {
				return rows, nil
			}
			return nil, fmt.Errorf("CSV：%w", e)
		}
		line, _ := r.FieldPos(0)
		rows = append(rows, row{num: line, cells: cells})
	}
}

func normalize(h string) string {
	h = strings.ToLower(strings.NewReplacer(" ", "", "　", "", "*", "", "＊", "").Replace(h))
	if i := strings.IndexAny(h, "(（"); i >= 0 {
		h = h[:i]
	}
	return h
}

func find(cells []string, aliases []string) int {
	for _, alias := range aliases {
		for i, c := range cells {
			if normalize(c) == alias {
				return i
			}
		}
	}
	return -1
}

// header 在前 20 行里找含地址列的表头行。
func header(rows []row) (int, bool) {
	for i := 0; i < len(rows) && i < 20; i++ {
		if find(rows[i].cells, columns["address"]) >= 0 {
			return i, true
		}
	}
	return 0, false
}

// Check 按期望值核对一次读数：两边按点位类型编码后逐字节比较，REAL 即按 32 位精度比较。
func Check(r device.ReadResult, expected string) string {
	if r.Quality != "GOOD" {
		return Failed
	}
	if strings.TrimSpace(expected) == "" {
		return ""
	}
	want, e := device.ParseValue(r.Address, expected)
	if e != nil {
		return Invalid
	}
	a, _ := device.EncodeValue(r.Address, want)
	b, e := device.EncodeValue(r.Address, r.Value)
	if e != nil || !bytes.Equal(a, b) {
		return Mismatch
	}
	return Match
}

// Tally 汇总核对结论，例如“一致 47 · 不一致 0 · 读取失败 0 · 未核对 0”。
func Tally(verdicts []string) string {
	n := map[string]int{}
	for _, v := range verdicts {
		n[v]++
	}
	s := fmt.Sprintf("一致 %d · 不一致 %d · 读取失败 %d · 未核对 %d", n[Match], n[Mismatch], n[Failed], n[""])
	if n[Invalid] > 0 {
		s += fmt.Sprintf(" · 期望值无效 %d", n[Invalid])
	}
	return s
}
