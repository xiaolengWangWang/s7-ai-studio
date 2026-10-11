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
