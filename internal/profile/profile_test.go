package profile

import (
	"s7-ai-studio/internal/device"
	"testing"
)

func TestProfilesDoNotClaimValidation(t *testing.T) {
	for _, family := range []string{"s7-200", "s7-200-cn", "s7-200-smart", "s7-200-smart-g2", "s7-300", "s7-400", "s7-1200", "s7-1200-g2", "s7-1500", "et-200s", "et-200x", "et-200pro", "et-200sp", "winac", "software-controller", "open-controller", "siplus", "drive-controller", "logo", "logo-legacy", "s5"} {
		p, e := Lookup(family)
		if e != nil || p.Verified {
			t.Fatalf("%s %+v %v", family, p, e)
		}
	}
}
func TestExplicitTSAPOverridesRackSlot(t *testing.T) {
	p, _ := Lookup("s7-300")
	l, r, e := ConnectionTSAP(p, 0, 2, nil, nil)
	if e != nil || l != 0x100 || r != 0x102 {
		t.Fatalf("%x %x %v", l, r, e)
	}
	l, r, e = ConnectionTSAP(p, 3, 4, nil, nil)
	if e != nil || r != 0x164 {
		t.Fatalf("%x %v", r, e)
	}
	local, remote := uint16(0x1000), uint16(0x2000)
	l, r, e = ConnectionTSAP(p, 9, 99, &local, &remote)
	if e != nil || l != local || r != remote {
		t.Fatalf("%x %x %v", l, r, e)
	}
	if _, _, e = ConnectionTSAP(p, 8, 0, nil, nil); e == nil {
		t.Fatal("rack overflow accepted")
	}
	logo, _ := Lookup("logo")
	if _, _, e = ConnectionTSAP(logo, 0, 0, nil, nil); e == nil {
		t.Fatal("LOGO TSAP guessed")
	}
}
func TestVAddressMappingRequiresProfile(t *testing.T) {
	p, _ := Lookup("s7-200-smart")
	if _, e := ResolveAddress(p, "VW100", "INT", 1); e == nil {
		t.Fatal("missing mapping accepted")
	}
	p.VMapping = &Mapping{DB: 1, BaseOffset: 10}
	a, e := ResolveAddress(p, "VW100", "INT", 1)
	if e != nil || a.DBNumber != 1 || a.ByteOffset != 110 || a.Original != "VW100" {
		t.Fatalf("%+v %v", a, e)
	}
	p, _ = Lookup("s7-300")
	if _, e = ResolveAddress(p, "VW100", device.DataType("INT"), 1); e == nil {
		t.Fatal("V mapping invented")
	}
}
func TestUnsupportedFamilyRoute(t *testing.T) {
	p, _ := Lookup("s5")
	if _, _, e := ConnectionTSAP(p, 0, 1, nil, nil); e == nil {
		t.Fatal("S5 routed to S7 TCP")
	}
	if _, e := Lookup("not-a-plc"); e == nil {
		t.Fatal("unknown family accepted")
	}
}

func TestGenericS7TCPRequiresConfiguredTSAP(t *testing.T) {
	p, err := Lookup("generic-s7-tcp")
	if err != nil || p.Verified {
		t.Fatalf("profile = %+v, %v", p, err)
	}
	if _, _, err := ConnectionTSAP(p, 0, 1, nil, nil); err == nil {
		t.Fatal("generic profile guessed a remote TSAP")
	}
	remote := uint16(0x0201)
	_, got, err := ConnectionTSAP(p, 0, 1, nil, &remote)
	if err != nil || got != remote {
		t.Fatalf("remote TSAP = %04x, %v", got, err)
	}
}
