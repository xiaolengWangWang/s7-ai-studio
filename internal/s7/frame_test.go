package s7

import (
	"bytes"
	"io"
	"testing"
)

type byteReader struct{ io.Reader }

func (r byteReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.Reader.Read(p)
}
func TestTPKTFragmentedAndCoalesced(t *testing.T) {
	r := byteReader{bytes.NewReader([]byte{3, 0, 0, 7, 2, 240, 128, 3, 0, 0, 7, 2, 240, 128})}
	for i := 0; i < 2; i++ {
		b, e := ReadTPKT(r, 1024)
		if e != nil || !bytes.Equal(b, []byte{2, 240, 128}) {
			t.Fatalf("%x %v", b, e)
		}
	}
	var out bytes.Buffer
	if e := WriteTPKT(&out, []byte{2, 240, 128}); e != nil || !bytes.Equal(out.Bytes(), []byte{3, 0, 0, 7, 2, 240, 128}) {
		t.Fatalf("%x %v", out.Bytes(), e)
	}
}
func TestTPKTInvalidLength(t *testing.T) {
	for _, b := range [][]byte{{3, 0, 0, 3}, {3, 0, 255, 255}, {2, 0, 0, 7, 2, 240, 128}, {3, 1, 0, 7, 2, 240, 128}, {3, 0, 0, 8, 2, 240, 128}} {
		if _, e := ReadTPKT(bytes.NewReader(b), 1024); e == nil {
			t.Fatalf("accepted %x", b)
		}
	}
}

func TestTPKTTruncationReturnsOnlyReceivedPayload(t *testing.T) {
	got, err := ReadTPKT(bytes.NewReader([]byte{3, 0, 0, 8, 2, 240}), 1024)
	if err == nil || !bytes.Equal(got, []byte{2, 240}) {
		t.Fatalf("fabricated payload: %x, %v", got, err)
	}
}
func FuzzReadTPKT(f *testing.F) {
	f.Add([]byte{3, 0, 0, 7, 2, 240, 128})
	f.Fuzz(func(t *testing.T, b []byte) { ReadTPKT(bytes.NewReader(b), 1024) })
}
