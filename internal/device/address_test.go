package device

import "testing"

func TestAddressValidation(t *testing.T) {
	for _, tc := range []struct {
		text   string
		kind   DataType
		count  int
		area   Area
		db     uint16
		offset uint32
		bit    uint8
	}{
		{"DB1.DBX0.7", "BOOL", 1, "DB", 1, 0, 7},
		{"DB23.DBD4", "REAL", 1, "DB", 23, 4, 0},
		{"mw10", "INT", 2, "M", 0, 10, 0},
		{"I0.1", "BOOL", 1, "I", 0, 0, 1},
		{"QB12", "BYTE", 1, "Q", 0, 12, 0},
	} {
		a, err := ParseAddress(tc.text, tc.kind, tc.count)
		if err != nil || a.Area != tc.area || a.DBNumber != tc.db || a.ByteOffset != tc.offset || a.BitOffset != tc.bit {
			t.Fatalf("%s: %+v %v", tc.text, a, err)
		}
	}
	for _, tc := range []struct {
		text  string
		kind  DataType
		count int
	}{
		{"DB1.DBX0.8", "BOOL", 1}, {"DB65536.DBW0", "INT", 1},
		{"MW-1", "INT", 1}, {"MD2097151", "DINT", 1}, {"MB999999999999999999999", "BYTE", 1},
		{"MW0", "INT", -1}, {"MW0", "INT", 0}, {"MB0", "BYTE", 1 << 30},
		{"VW100", "INT", 1}, {"DB1.DBX0.0", "INT", 1}, {"DB1.DBB0", "INVALID", 1},
		{"DB1.DBW0", "REAL", 1}, {"M0.0", "BOOL", 2},
	} {
		if _, err := ParseAddress(tc.text, tc.kind, tc.count); err == nil {
			t.Errorf("accepted invalid %+v", tc)
		}
	}
}

func TestViSCADAIdentifiers(t *testing.T) {
	for _, tc := range []struct {
		text   string
		kind   DataType
		offset uint32
		bit    uint8
	}{
		{"DB1.R0", "REAL", 0, 0}, {"db1.x52.1", "BOOL", 52, 1}, {"DB1.I4", "INT", 4, 0},
		{"DB1.W6", "WORD", 6, 0}, {"DB1.DW8", "DWORD", 8, 0}, {"DB1.DI12", "DINT", 12, 0}, {"DB1.B0", "BYTE", 0, 0},
	} {
		kind, ok := PlatformType(tc.text)
		if !ok || kind != tc.kind {
			t.Fatalf("%s: %s %v", tc.text, kind, ok)
		}
		a, err := ParseAddress(tc.text, kind, 1)
		if err != nil || a.Area != AreaDB || a.DBNumber != 1 || a.ByteOffset != tc.offset || a.BitOffset != tc.bit || a.Original != tc.text {
			t.Fatalf("%s: %+v %v", tc.text, a, err)
		}
	}
	if _, ok := PlatformType("DB1.DBD0"); ok {
		t.Fatal("native address treated as viSCADA identifier")
	}
	for _, tc := range []struct {
		text  string
		kind  DataType
		count int
	}{{"DB1.R0", "DINT", 1}, {"DB1.X52", "BOOL", 1}, {"DB1.R0.1", "REAL", 1}, {"DB1.X52.8", "BOOL", 1}, {"DB1.X52.0", "BOOL", 2}} {
		if _, err := ParseAddress(tc.text, tc.kind, tc.count); err == nil {
			t.Errorf("accepted %+v", tc)
		}
	}
}

func TestTelegrafAdditionalDBTypes(t *testing.T) {
	for _, tc := range []struct {
		text   string
		kind   DataType
		offset uint32
		length int
	}{
		{"DB1.C0", "CHAR", 0, 1},
		{"DB1.LI8", "LINT", 8, 8},
		{"DB1.LR16", "LREAL", 16, 8},
		{"DB1.S24.32", "STRING[32]", 24, 34},
		{"DB1.S24.032", "STRING[32]", 24, 34},
	} {
		kind, ok := PlatformType(tc.text)
		if !ok || kind != tc.kind {
			t.Fatalf("%s type = %s, %v", tc.text, kind, ok)
		}
		a, err := ParseAddress(tc.text, kind, 1)
		if err != nil || a.ByteOffset != tc.offset || a.ByteLength() != tc.length {
			t.Fatalf("%s address = %+v, %v", tc.text, a, err)
		}
	}
	for _, text := range []string{"DB1.S24", "DB1.S24.0", "DB1.S24.255", "DB1.C0.1", "DB1.LR0.1"} {
		if kind, ok := PlatformType(text); ok {
			if _, err := ParseAddress(text, kind, 1); err == nil {
				t.Errorf("accepted invalid Telegraf address %s", text)
			}
		}
	}
}

func TestTelegrafProcessAndMarkerAreas(t *testing.T) {
	for _, tc := range []struct {
		text   string
		area   Area
		kind   DataType
		offset uint32
		bit    uint8
	}{
		{"PE0.X2.3", AreaI, "BOOL", 2, 3},
		{"PA0.B4", AreaQ, "BYTE", 4, 0},
		{"MK0.R8", AreaM, "REAL", 8, 0},
	} {
		kind, ok := PlatformType(tc.text)
		if !ok || kind != tc.kind {
			t.Fatalf("%s type = %s, %v", tc.text, kind, ok)
		}
		a, err := ParseAddress(tc.text, kind, 1)
		if err != nil || a.Area != tc.area || a.ByteOffset != tc.offset || a.BitOffset != tc.bit {
			t.Fatalf("%s address = %+v, %v", tc.text, a, err)
		}
	}
	if _, err := ParseAddress("MK1.B0", "BYTE", 1); err == nil {
		t.Fatal("ignored area index accepted")
	}
}

func TestAddressStringAndSymbol(t *testing.T) {
	a, err := ParseAddress("DB1.DBB2", "STRING[20]", 1)
	if err != nil || a.StringLength != 20 || a.ByteLength() != 22 {
		t.Fatalf("%+v %v", a, err)
	}
	for _, kind := range []DataType{"STRING", "STRING[0]", "STRING[255]"} {
		if _, err := ParseAddress("DB1.DBB2", kind, 1); err == nil {
			t.Errorf("accepted %s", kind)
		}
	}
	a, err = ParseAddress("symbol:Motor.Speed", "REAL", 1)
	if err != nil || a.Symbol != "Motor.Speed" || a.Area != "" {
		t.Fatalf("%+v %v", a, err)
	}
}
