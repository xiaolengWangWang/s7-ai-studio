package pointtable

import (
	"archive/zip"
	"bytes"
	"os"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
	"s7-ai-studio/internal/device"
)

func TestParseCSVResolvesViSCADAIdentifiers(t *testing.T) {
	csv := "说明：热力站\n*属性标识,属性名称,标准化名称,单位,模拟值\nDB1.R0,一次供温,m_001t,℃,85.0\nDB1.X52.1,通信状态,m_001com,,false\n,空行,,,\nDB1.DBW4,原生地址,,,7\nDB1.DBD8,原生浮点,,,1.5\nDB1.R0,重复,,,1\nDB1.R12,坏值,,,abc\nDB1.DBX9.3,位,,,1\n"
	table, e := Parse("t.csv", []byte(csv))
	if e != nil {
		t.Fatal(e)
	}
	if len(table.Points) != 3 || table.Points[0].Name != "一次供温" || table.Points[0].Row != 3 || table.Points[1].Standard != "m_001com" || table.Points[2].Address != "DB1.DBX9.3" {
		t.Fatalf("%+v", table.Points)
	}
	want := []string{"第 6 行 DB1.DBW4", "第 7 行 DB1.DBD8", "第 8 行 DB1.R0：与第 3 行地址重复", "第 9 行 DB1.R12：期望值无效"}
	if len(table.Skipped) != len(want) {
		t.Fatalf("%q", table.Skipped)
	}
	for i, w := range want {
		if !strings.HasPrefix(table.Skipped[i], w) {
			t.Errorf("skipped[%d] = %q, want prefix %q", i, table.Skipped[i], w)
		}
	}
	a, e := table.Points[0].Resolve()
	if e != nil || a.Type != "REAL" || a.DBNumber != 1 || a.ByteOffset != 0 {
		t.Fatalf("%+v %v", a, e)
	}
}

func TestParseCSVTypeColumnAndGBK(t *testing.T) {
	text := "地址,名称,类型,期望值\nDB1.DBD8,原生浮点,REAL,1.5\nMW10,整数,INT,-3\nDB1.R0,数据类型编码,3,2\n"
	gbk, e := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(text))
	if e != nil {
		t.Fatal(e)
	}
	table, e := Parse("gbk.CSV", gbk)
	if e != nil || len(table.Points) != 3 || table.Points[0].Type != "REAL" || table.Points[1].Name != "整数" || table.Points[2].Type != "" {
		t.Fatalf("%+v %v", table, e)
	}
}

func TestParseRejectsUnusableFiles(t *testing.T) {
	for name, data := range map[string]string{
		"a.txt":  "属性标识\nDB1.R0\n",
		"b.csv":  "名称,值\nx,1\n",
		"c.csv":  "属性标识\nWHNB.PV\n",
		"d.csv":  "b\x14#e\x00\x00\x00\x00\x00\x00\x00\x00E-SafeNet\x00\x00\x00LOCK",
		"e.xlsx": "not a zip",
	} {
		if _, e := Parse(name, []byte(data)); e == nil {
			t.Errorf("%s accepted", name)
		}
	}
	var many strings.Builder
	many.WriteString("属性标识\n")
	for i := 0; i <= MaxPoints; i++ {
		many.WriteString("DB1.X" + strconv.Itoa(i) + ".0\n")
	}
	if _, e := Parse("many.csv", []byte(many.String())); e == nil || !strings.Contains(e.Error(), "1024") {
		t.Fatalf("too many points accepted: %v", e)
	}
}

// viSCADA 导出的 xlsx：说明 / 设备类属性信息 / 示例 三页，行内字符串，数据类型列是编码。
func TestParseViSCADAExportXLSX(t *testing.T) {
	sheet := func(rows ...string) string {
		return `<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>` + strings.Join(rows, "") + `</sheetData></worksheet>`
	}
	cell := func(ref, text string) string {
		return `<c r="` + ref + `" t="inlineStr"><is><t>` + text + `</t></is></c>`
	}
	files := map[string]string{
		"xl/workbook.xml":            `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="说明" r:id="rId1"/><sheet name="设备类属性信息" r:id="rId2"/><sheet name="示例" r:id="rId3"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Target="worksheets/sheet2.xml"/><Relationship Id="rId3" Target="/xl/worksheets/sheet3.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   sheet(`<row r="1">` + cell("A1", "属性标识为必填") + `</row>`),
		"xl/worksheets/sheet2.xml": sheet(
			`<row r="1">`+cell("A1", "属性ID")+cell("B1", "属性标识")+cell("C1", "属性名称")+cell("D1", "标准化名称")+cell("F1", "单位")+cell("H1", "数据类型")+`</row>`,
			`<row r="2">`+cell("B2", "DB1.R4")+cell("C2", "一次回温")+cell("D2", "m_002t")+cell("F2", "℃")+`<c r="H2"><v>3</v></c></row>`,
			`<row r="5">`+cell("B5", "DB1.X52.0")+cell("C5", "阀门控制")+`<c r="H5"><v>6</v></c></row>`),
		"xl/worksheets/sheet3.xml": sheet(`<row r="1">` + cell("B1", "属性标识") + `</row><row r="2">` + cell("B2", "DB9.R0") + `</row>`),
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, body := range files {
		w, _ := z.Create(name)
		w.Write([]byte(body))
	}
	z.Close()
	table, e := Parse("属性.xlsx", buf.Bytes())
	if e != nil {
		t.Fatal(e)
	}
	if table.Sheet != "设备类属性信息" || len(table.Points) != 2 || table.Points[0].Address != "DB1.R4" || table.Points[0].Unit != "℃" || table.Points[1].Row != 5 || table.Points[0].Type != "" {
		t.Fatalf("%+v", table)
	}
}

func TestCheck(t *testing.T) {
	real, _ := device.ParseAddress("DB1.R12", "REAL", 1)
	bit, _ := device.ParseAddress("DB1.X52.1", "BOOL", 1)
	good := func(a device.Address, v device.Value) device.ReadResult {
		return device.ReadResult{Address: a, Value: v, Quality: "GOOD"}
	}
	// 0.6 经 32 位浮点读回为 0.6000000238418579，仍应判为一致。
	r := good(real, device.Value{Type: "REAL", Real: float64(float32(0.6))})
	for _, tc := range []struct {
		r        device.ReadResult
		expected string
		want     string
	}{
		{r, "0.60", Match},
		{r, "0.61", Mismatch},
		{r, "", ""},
		{r, "x", Invalid},
		{device.ReadResult{Address: real, Quality: "BAD"}, "0.6", Failed},
		{good(bit, device.Value{Type: "BOOL"}), "false", Match},
		{good(bit, device.Value{Type: "BOOL"}), "1", Mismatch},
	} {
		if got := Check(tc.r, tc.expected); got != tc.want {
			t.Errorf("Check(%q) = %q, want %q", tc.expected, got, tc.want)
		}
	}
	if got := Tally([]string{Match, Match, Mismatch, "", Invalid}); got != "一致 2 · 不一致 1 · 读取失败 0 · 未核对 1 · 期望值无效 1" {
		t.Fatal(got)
	}
}

func TestExampleHeatStationTable(t *testing.T) {
	data, e := os.ReadFile("../../assets/examples/viscada-heat-station-s7.csv")
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(data[:min(len(data), 32)], []byte("E-SafeNet")) {
		t.Skip("示例点表在本机被 DLP 加密，CI 上会读取明文")
	}
	table, e := Parse("viscada-heat-station-s7.csv", data)
	if e != nil || len(table.Points) != 47 || len(table.Skipped) != 0 {
		t.Fatalf("%d points, skipped %q, %v", len(table.Points), table.Skipped, e)
	}
	types := map[device.DataType]int{}
	for _, p := range table.Points {
		a, e := p.Resolve()
		if e != nil || p.Expected == "" || p.Standard == "" {
			t.Fatalf("%+v %v", p, e)
		}
		types[a.Type]++
	}
	if types["REAL"] != 39 || types["BOOL"] != 8 {
		t.Fatalf("%v", types)
	}
}
