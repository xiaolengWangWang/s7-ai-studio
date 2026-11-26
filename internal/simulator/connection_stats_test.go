package simulator

import (
	"context"
	"net"
	"s7-ai-studio/internal/s7"
	"testing"
)

func TestConnectionStatsCapacityAndUnsupported(t *testing.T) {
	for _, capacity := range []uint16{0, 3} {
		t.Run(string(rune('0'+capacity)), func(t *testing.T) {
			srv, err := New(Options{PDULimit: 64, MaxConnections: capacity})
			if err != nil {
				t.Fatal(err)
			}
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			defer srv.Close()
			go srv.Serve(ctx, ln)
			client, err := s7.Dial(ctx, ln.Addr().String(), s7.DefaultOptions())
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			stats := client.ConnectionStats(ctx)
			if capacity == 0 {
				if stats.Current != nil || stats.Max != nil || stats.CurrentError == nil || stats.MaxError == nil || client.IsClosed() {
					t.Fatalf("unsupported: %+v", stats)
				}
				return
			}
			if stats.CurrentError != nil || stats.MaxError != nil || stats.Current == nil || stats.Max == nil {
				t.Fatalf("stats: %+v", stats)
			}
			if stats.Current.SharedUsed != 1 || stats.Current.Free != 2 || *stats.Max != 3 {
				t.Fatalf("stats: %+v max %d", stats.Current, *stats.Max)
			}
		})
	}
}
