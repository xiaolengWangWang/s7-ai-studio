package device

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type eachDriver struct {
	Driver
	read func(context.Context, []Address) ([]ReadResult, error)
}

func (d eachDriver) Read(ctx context.Context, aa []Address) ([]ReadResult, error) {
	return d.read(ctx, aa)
}

func TestReadEachContinuesAfterRejectedPoint(t *testing.T) {
	aa := []Address{{Area: AreaM, Type: "BYTE", Count: 1}, {Area: AreaM, ByteOffset: 1, Type: "BYTE", Count: 1}, {Area: AreaM, ByteOffset: 2, Type: "BYTE", Count: 1}}
	calls, progress := 0, 0
	d := eachDriver{read: func(_ context.Context, got []Address) ([]ReadResult, error) {
		if len(got) != 1 || got[0].ByteOffset != uint32(calls) {
			t.Fatalf("request %d must contain only the next point: %+v", calls, got)
		}
		calls++
		r := ReadResult{Address: got[0], Quality: "GOOD"}
		if calls == 2 {
			r.Quality, r.Error = "BAD", "access denied"
		}
		return []ReadResult{r}, nil
	}}
	rr, err := ReadEach(context.Background(), d, aa, func(r ReadResult) {
		if r.Address.ByteOffset != uint32(progress) {
			t.Fatalf("out-of-order progress: %+v", r)
		}
		progress++
	})
	if err != nil || calls != 3 || progress != 3 || len(rr) != 3 || rr[0].Quality != "GOOD" || rr[1].Quality != "BAD" || rr[2].Quality != "GOOD" {
		t.Fatalf("results=%+v calls=%d progress=%d err=%v", rr, calls, progress, err)
	}
}

func TestReadEachPreservesCompletedResultsAndMarksUnattempted(t *testing.T) {
	aa := []Address{{Area: AreaM, Type: "BYTE", Count: 1}, {Area: AreaM, ByteOffset: 1, Type: "BYTE", Count: 1}, {Area: AreaM, ByteOffset: 2, Type: "BYTE", Count: 1}}
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "transport", true: "cancel"}[cancel], func(t *testing.T) {
			ctx, stop := context.WithCancel(context.Background())
			defer stop()
			calls := 0
			d := eachDriver{read: func(_ context.Context, got []Address) ([]ReadResult, error) {
				calls++
				if calls == 2 {
					return nil, errors.New("connection lost")
				}
				return []ReadResult{{Address: got[0], Quality: "GOOD"}}, nil
			}}
			rr, err := ReadEach(ctx, d, aa, func(ReadResult) {
				if cancel {
					stop()
				}
			})
			wantCalls := 2
			if cancel {
				wantCalls = 1
			}
			if err == nil || calls != wantCalls || len(rr) != 3 || rr[0].Quality != "GOOD" || rr[2].Quality != "UNKNOWN" || !strings.Contains(rr[2].Error, "未读取") {
				t.Fatalf("results=%+v calls=%d err=%v", rr, calls, err)
			}
		})
	}
}

func TestReadEachValidatesAllPointsBeforeSending(t *testing.T) {
	calls := 0
	d := eachDriver{read: func(context.Context, []Address) ([]ReadResult, error) {
		calls++
		return nil, nil
	}}
	aa := []Address{{Area: AreaM, Type: "BYTE", Count: 1}, {Area: AreaM, Type: "BOOL", Count: 2}}
	if _, err := ReadEach(context.Background(), d, aa, nil); err == nil || calls != 0 {
		t.Fatalf("invalid later address reached the device: calls=%d err=%v", calls, err)
	}
}
