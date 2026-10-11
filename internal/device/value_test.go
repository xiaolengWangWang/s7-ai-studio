package device

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestValueInt64Precision(t *testing.T) {
	a, _ := ParseAddress("DB1.DBB0", "LINT", 1)
	v := Value{Type: "LINT", Integer: "9007199254740993"}
	b, err := EncodeValue(a, v)
	if err != nil || !bytes.Equal(b, []byte{0, 32, 0, 0, 0, 0, 0, 1}) {
		t.Fatalf("%x %v", b, err)
	}
	got, err := DecodeValue(a, b)
	if err != nil || got.Integer != v.Integer {
		t.Fatalf("%+v %v", got, err)
	}
	j, _ := json.Marshal(got)
	if !bytes.Contains(j, []byte(`"integer":"9007199254740993"`)) {
		t.Fatal(string(j))
	}
}

func TestValueJSONKeepsActiveZeroFields(t *testing.T) {
	for _, tc := range []struct {
		value    Value
		field    string
		expected string
	}{
		{Value{Type: "BOOL"}, "boolean", "false"},
		{Value{Type: "REAL"}, "real", "0"},
		{Value{Type: "LREAL"}, "real", "0"},
		{Value{Type: "STRING"}, "text", `""`},
	} {
		t.Run(string(tc.value.Type), func(t *testing.T) {
			b, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err = json.Unmarshal(b, &fields); err != nil {
				t.Fatal(err)
			}
			if string(fields[tc.field]) != tc.expected {
				t.Fatalf("active zero value missing: %s", b)
			}
			for _, other := range []string{"integer", "boolean", "real", "text"} {
				if other != tc.field && fields[other] != nil {
					t.Fatalf("unrelated scalar field %s: %s", other, b)
				}
			}
		})
	}
	array := Value{Type: "REAL", Elements: []Value{{Type: "REAL"}, {Type: "REAL", Real: 1}}}
	b, err := json.Marshal(array)
	if err != nil || !bytes.Contains(b, []byte(`"real":0`)) {
		t.Fatalf("zero array element missing: %s %v", b, err)
	}
}

func TestValueScalarWireTypes(t *testing.T) {
	for _, tc := range []struct {
		kind  DataType
		value Value
		wire  []byte
	}{
		{"INT", Value{Type: "INT", Integer: "-2"}, []byte{255, 254}},
		{"UINT", Value{Type: "UINT", Integer: "65535"}, []byte{255, 255}},
		{"REAL", Value{Type: "REAL", Real: 1.5}, []byte{63, 192, 0, 0}},
		{"LREAL", Value{Type: "LREAL", Real: 1.5}, []byte{63, 248, 0, 0, 0, 0, 0, 0}},
		{"BOOL", Value{Type: "BOOL", Boolean: true}, []byte{1}},
	} {
		a := Address{Area: "DB", DBNumber: 1, Type: tc.kind, Count: 1}
		b, err := EncodeValue(a, tc.value)
		if err != nil || !bytes.Equal(b, tc.wire) {
			t.Fatalf("%s %x %v", tc.kind, b, err)
		}
		got, err := DecodeValue(a, tc.wire)
		if err != nil || got.Integer != tc.value.Integer || got.Real != tc.value.Real || got.Boolean != tc.value.Boolean {
			t.Fatalf("%s %+v %v", tc.kind, got, err)
		}
	}
	a := Address{Area: "M", Type: "INT", Count: 1}
	if _, err := EncodeValue(a, Value{Type: "INT", Integer: "32768"}); err == nil {
		t.Fatal("INT overflow accepted")
	}
	if _, err := DecodeValue(a, []byte{0}); err == nil {
		t.Fatal("short INT accepted")
	}
}

func TestSiemensStringLength(t *testing.T) {
	a := Address{Area: "DB", DBNumber: 1, Type: "STRING", StringLength: 5, Count: 1}
	b, err := EncodeValue(a, Value{Type: "STRING", Text: "Hi"})
	if err != nil || !bytes.Equal(b, []byte{5, 2, 'H', 'i', 0, 0, 0}) {
		t.Fatalf("%x %v", b, err)
	}
	v, err := DecodeValue(a, b)
	if err != nil || v.Text != "Hi" {
		t.Fatalf("%+v %v", v, err)
	}
	if _, err = DecodeValue(a, []byte{5, 6, 0, 0, 0, 0, 0}); err == nil {
		t.Fatal("actual length exceeds maximum")
	}
	if _, err = EncodeValue(a, Value{Type: "STRING", Text: "abcdef"}); err == nil {
		t.Fatal("oversized string accepted")
	}
}

func TestValueArrayPreservesEachElement(t *testing.T) {
	a := Address{Area: "M", Type: "INT", Count: 2}
	v, err := DecodeValue(a, []byte{0, 1, 255, 254})
	if err != nil || len(v.Elements) != 2 || v.Elements[0].Integer != "1" || v.Elements[1].Integer != "-2" {
		t.Fatalf("%+v %v", v, err)
	}
	b, err := EncodeValue(a, v)
	if err != nil || !bytes.Equal(b, []byte{0, 1, 255, 254}) {
		t.Fatalf("%x %v", b, err)
	}
}
