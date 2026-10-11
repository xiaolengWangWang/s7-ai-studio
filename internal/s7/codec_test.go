package s7

import (
	"bytes"
	"encoding/hex"
	"s7-ai-studio/internal/device"
	"testing"
)

func fromHex(s string) []byte {
	b, e := hex.DecodeString(s)
	if e != nil {
		panic(e)
	}
	return b
}
func TestSetupAndReadGoldenPDU(t *testing.T) {
	b, e := encodeSetup(1, 480)
	if e != nil || !bytes.Equal(b, fromHex("32010000000100080000f0000001000101e0")) {
		t.Fatalf("%x %v", b, e)
	}
	a, _ := device.ParseAddress("DB1.DBD4", "REAL", 1)
	b, e = encodeRead(1, []device.Address{a})
	if e != nil || !bytes.Equal(b, fromHex("320100000001000e00000401120a10020004000184000020")) {
		t.Fatalf("%x %v", b, e)
	}
}
func TestWriteBitGoldenPDU(t *testing.T) {
	a, _ := device.ParseAddress("M0.7", "BOOL", 1)
	b, e := encodeWrite(1, a, []byte{1})
	if e != nil || !bytes.Equal(b, fromHex("320100000001000e00050501120a100100010000830000070003000101")) {
		t.Fatalf("%x %v", b, e)
	}
}
func TestReadPDUItemErrors(t *testing.T) {
	a, _ := device.ParseAddress("DB1.DBD0", "DINT", 1)
	other := a
	other.DBNumber = 9
	r, e := decodeRead(fromHex("3203000000010002000c00000402ff0400200000002a05000000"), []device.Address{a, other})
	if e != nil || len(r) != 2 || r[0].Quality != "GOOD" || r[0].Value.Integer != "42" || r[1].Quality != "BAD" || r[1].Error == "" {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestPDUReferenceAndLengthValidation(t *testing.T) {
	good := fromHex("320300000001000800000000f0000001000101e0")
	if _, e := parsePDU(good, 1); e != nil {
		t.Fatal(e)
	}
	if _, e := parsePDU(good, 2); e == nil {
		t.Fatal("wrong reference accepted")
	}
	if _, e := parsePDU(good[:len(good)-1], 1); e == nil {
		t.Fatal("truncated PDU accepted")
	}
	a, _ := device.ParseAddress("MB0", "BYTE", 1)
	for _, s := range []string{"3203000000010002000500000401ff04001000", "3203000000010002000500000401ff99000800"} {
		if _, e := decodeRead(fromHex(s), []device.Address{a}); e == nil {
			t.Fatal("bad item length/transport accepted")
		}
	}
}
func FuzzDecodePDU(f *testing.F) {
	f.Add(fromHex("320300000001000800000000f0000001000101e0"))
	f.Fuzz(func(t *testing.T, b []byte) {
		parsePDU(b, 1)
		a := device.Address{Area: "M", Type: "BYTE", Count: 1}
		decodeRead(b, []device.Address{a})
	})
}
