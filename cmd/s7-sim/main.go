package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"s7-ai-studio/internal/device"
	"s7-ai-studio/internal/simulator"
	"time"
)

func main() { os.Exit(run()) }

func run() int {
	fs := flag.NewFlagSet("s7-sim", flag.ContinueOnError)
	listen := fs.String("listen", "127.0.0.1:1102", "listen address")
	pdu := fs.Uint("pdu", 480, "maximum S7 PDU size (64..960)")
	delay := fs.Duration("delay", 0*time.Second, "delay read/write replies")
	reject := fs.String("reject-area", "", "reject DB, M, I or Q area")
	dropACK := fs.Bool("drop-write-ack", false, "apply writes but discard their acknowledgment")
	truncate := fs.Bool("truncate", false, "truncate a read/write response and close the connection")
	wrongRef := fs.Bool("wrong-reference", false, "reply with an incorrect PDU reference")
	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 || *pdu < 64 || *pdu > 960 {
		fmt.Fprintln(os.Stderr, "PDU must be 64..960; no positional arguments are accepted")
		return 2
	}
	memory := simulator.NewMemory()
	a, _ := device.ParseAddress("DB1.DBD0", "DINT", 1)
	b, _ := device.EncodeValue(a, device.Value{Type: "DINT", Integer: "42"})
	if err := memory.Set(a, b); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	server, err := simulator.New(simulator.Options{PDULimit: int(*pdu), Memory: memory, Fault: simulator.Fault{Delay: *delay, RejectArea: device.Area(*reject), DropWriteAck: *dropACK, Truncate: *truncate, WrongReference: *wrongRef}})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	defer server.Close()
	fmt.Printf("S7 simulator listening on %s; DB1.DBD0 DINT=42; synthetic peer only\n", ln.Addr())
	if err = server.Serve(ctx, ln); err != nil && ctx.Err() == nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
