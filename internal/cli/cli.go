// Package cli exposes the headless S7 TCP diagnostic interface.
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"s7-ai-studio/internal/device"
	"s7-ai-studio/internal/pointtable"
	"s7-ai-studio/internal/profile"
	"s7-ai-studio/internal/recorder"
	"s7-ai-studio/internal/s7"
)

type dialFunc func(context.Context, string, s7.Options) (device.Driver, error)

// Run returns 0 for success, 1 for communication/partial-result failure and 2
// for invalid input. All flags must precede the read/write subcommand.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return run(ctx, args, stdout, stderr, func(ctx context.Context, target string, opts s7.Options) (device.Driver, error) {
		return s7.Dial(ctx, target, opts)
	})
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, dial dialFunc) (code int) {
	fs := flag.NewFlagSet("s7-cli", flag.ContinueOnError)
	fs.SetOutput(stderr)
	target := fs.String("target", "", "PLC host:port (required)")
	family := fs.String("family", "s7-300", "connection profile; does not identify the actual CPU")
	route := fs.String("route", "s7-tcp", "transport route (Stage A: s7-tcp only)")
	kind := fs.String("type", "INT", "BOOL/BYTE/INT/DINT/LINT/REAL/LREAL/.../STRING[n]/RAW")
	asJSON := fs.Bool("json", false, "emit JSON; integer values are decimal strings")
	allowWrite := fs.Bool("allow-write", false, "explicitly allow writes")
	recordDir := fs.String("record", "", "record events to daily SQLite files in this directory")
	timeout := fs.Duration("timeout", time.Second, "timeout for each network exchange")
	rack := fs.Int("rack", -1, "rack override; default comes from the profile")
	slot := fs.Int("slot", -1, "slot override; default comes from the profile")
	localText := fs.String("local-tsap", "", "local TSAP, e.g. 0x0100")
	remoteText := fs.String("remote-tsap", "", "remote TSAP from PLC/gateway configuration")
	vDB := fs.Int("v-db", -1, "explicit V/VM mapping DB number")
	vOffset := fs.Uint("v-offset", 0, "explicit V/VM mapping base byte offset")
	pointsFile := fs.String("points", "", "point table for verify: viSCADA property export (.xlsx) or .csv")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: s7-cli [flags] read ADDRESS[,ADDRESS...] [COUNT]\n       s7-cli [flags] -allow-write write ADDRESS VALUE[,VALUE...]\n       s7-cli [flags] -points TABLE verify\nviSCADA identifiers such as DB1.R0 and DB1.X52.0 carry their own type unless -type is given.")
		fs.PrintDefaults()
	}
	inputError := func(err error) int { fmt.Fprintln(stderr, err); return 2 }
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *route != "s7-tcp" {
		return inputError(fmt.Errorf("route %q 未实现；阶段 A 仅提供 s7-tcp", *route))
	}
	p, err := profile.Lookup(*family)
	if err != nil {
		return inputError(err)
	}
	if !slices.Contains(p.Routes, *route) {
		return inputError(fmt.Errorf("%s does not expose %s", p.Family, *route))
	}
	host, port, err := net.SplitHostPort(*target)
	if err != nil || host == "" {
		return inputError(fmt.Errorf("-target requires host:port"))
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return inputError(fmt.Errorf("target port must be 1..65535"))
	}
	if *timeout <= 0 {
		return inputError(fmt.Errorf("timeout must be positive"))
	}
	if *rack == -1 {
		*rack = p.Rack
	}
	if *slot == -1 {
		*slot = p.Slot
	}
	local, err := parseTSAP(*localText)
	if err != nil {
		return inputError(err)
	}
	remote, err := parseTSAP(*remoteText)
	if err != nil {
		return inputError(err)
	}
	l, r, err := profile.ConnectionTSAP(p, *rack, *slot, local, remote)
	if err != nil {
		return inputError(err)
	}
	if *vDB != -1 {
		if *vDB < 1 || *vDB > 65535 || *vOffset > 1<<21-1 {
			return inputError(fmt.Errorf("invalid V/VM mapping"))
		}
		p.VMapping = &profile.Mapping{DB: uint16(*vDB), BaseOffset: uint32(*vOffset)}
	} else if *vOffset != 0 {
		return inputError(fmt.Errorf("-v-offset requires -v-db"))
	}
	typed := false
	fs.Visit(func(f *flag.Flag) { typed = typed || f.Name == "type" })
	// viSCADA 标识自带类型；只有显式给出 -type 时才按 -type 校验。
	kindOf := func(text string) device.DataType {
		if t, ok := device.PlatformType(text); ok && !typed {
			return t
		}
		return device.DataType(strings.ToUpper(*kind))
	}
	words := fs.Args()
	if len(words) < 2 && !(len(words) == 1 && words[0] == "verify") {
		fs.Usage()
		return 2
	}
	operation := words[0]
	var addresses []device.Address
	var value device.Value
	var table pointtable.Table
	switch operation {
	case "verify":
		if *pointsFile == "" || len(words) != 1 {
			return inputError(fmt.Errorf("verify requires -points TABLE and no addresses"))
		}
		table, err = pointtable.Load(*pointsFile)
		if err != nil {
			return inputError(err)
		}
		for _, p := range table.Points {
			a, e := p.Resolve()
			if e != nil {
				return inputError(e)
			}
			addresses = append(addresses, a)
		}
		for _, s := range table.Skipped {
			fmt.Fprintln(stderr, "skipped:", s)
		}
	case "read":
		if len(words) > 3 {
			return inputError(fmt.Errorf("read expects address list and optional count"))
		}
		count := 1
		if len(words) == 3 {
			count, err = strconv.Atoi(words[2])
			if err != nil {
				return inputError(err)
			}
		}
		texts := strings.Split(words[1], ",")
		if len(texts) > 1024 {
			return inputError(fmt.Errorf("at most 1024 read addresses are allowed"))
		}
		for _, text := range texts {
			a, e := profile.ResolveAddress(p, text, kindOf(text), count)
			if e != nil {
				return inputError(e)
			}
			addresses = append(addresses, a)
		}
	case "write":
		if !*allowWrite {
			return inputError(fmt.Errorf("write requires -allow-write"))
		}
		if len(words) != 3 {
			return inputError(fmt.Errorf("write expects one address and value"))
		}
		count := 1
		if !strings.HasPrefix(string(kindOf(words[1])), "STRING[") {
			count = len(strings.Split(words[2], ","))
		}
		a, e := profile.ResolveAddress(p, words[1], kindOf(words[1]), count)
		if e != nil {
			return inputError(e)
		}
		value, err = device.ParseValue(a, words[2])
		if err != nil {
			return inputError(err)
		}
		if _, err = device.EncodeValue(a, value); err != nil {
			return inputError(err)
		}
		addresses = []device.Address{a}
	default:
		return inputError(fmt.Errorf("unknown command %q", operation))
	}
	opts := s7.DefaultOptions()
	opts.LocalTSAP, opts.RemoteTSAP, opts.Timeout = l, r, *timeout
	var recording *recorder.Recorder
	var session int64
	if *recordDir != "" {
		recording, err = recorder.OpenDaily(*recordDir)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		defer func() {
			if session != 0 {
				if e := recording.EndSession(session); e != nil {
					fmt.Fprintln(stderr, e)
					code = 1
				}
			}
			if e := recording.Close(); e != nil {
				fmt.Fprintln(stderr, e)
				code = 1
			}
			if n := recording.Dropped.Load(); n > 0 {
				fmt.Fprintf(stderr, "recorder dropped %d events\n", n)
				code = 1
			}
		}()
		session, err = recording.StartSession(device.ProtocolS7TCP, *target, 0)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		opts.Observer = device.ObserverFunc(func(event device.Event) {
			event.Family = p.Family
			recording.Record(session, event)
		})
	}
	driver, err := dial(ctx, *target, opts)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer driver.Close()
	if operation == "verify" {
		results, readErr := device.ReadEach(ctx, driver, addresses, nil)
		code = 0
		if readErr != nil {
			fmt.Fprintln(stderr, readErr)
			code = 1
		}
		var ok bool
		ok, err = verifyReport(stdout, table, results, *asJSON)
		if !ok {
			code = 1
		}
	} else if operation == "read" {
		results, readErr := driver.Read(ctx, addresses)
		code = 0
		if readErr != nil {
			fmt.Fprintln(stderr, readErr)
			code = 1
		}
		for _, result := range results {
			if result.Quality != "GOOD" {
				code = 1
			}
		}
		if *asJSON {
			err = emitJSON(stdout, struct {
				Operation string              `json:"operation"`
				Protocol  device.Protocol     `json:"protocol"`
				Results   []device.ReadResult `json:"results"`
			}{"read", device.ProtocolS7TCP, results})
		} else {
			for _, result := range results {
				if _, e := fmt.Fprintf(stdout, "%s = %s [%s] %s\n", result.Address.String(), device.FormatValue(result.Value), result.Quality, result.Error); e != nil {
					err = e
					break
				}
			}
		}
	} else {
		result, writeErr := driver.Write(ctx, addresses[0], value)
		if writeErr != nil {
			fmt.Fprintln(stderr, writeErr)
			code = 1
		}
		if result.State != "CONFIRMED" {
			code = 1
		}
		if *asJSON {
			err = emitJSON(stdout, struct {
				Operation string             `json:"operation"`
				Result    device.WriteResult `json:"result"`
			}{"write", result})
		} else {
			_, err = fmt.Fprintf(stdout, "%s [%s] %s\n", result.Address.String(), result.State, result.Error)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return code
}

func parseTSAP(text string) (*uint16, error) {
	if text == "" {
		return nil, nil
	}
	n, err := strconv.ParseUint(text, 0, 16)
	if err != nil {
		return nil, fmt.Errorf("invalid TSAP %q: %w", text, err)
	}
	u := uint16(n)
	return &u, nil
}

type verifyItem struct {
	Row      int               `json:"row"`
	Address  string            `json:"address"`
	Name     string            `json:"name,omitempty"`
	Standard string            `json:"standard_name,omitempty"`
	Unit     string            `json:"unit,omitempty"`
	Expected string            `json:"expected,omitempty"`
	Value    string            `json:"value,omitempty"`
	Verdict  string            `json:"verdict,omitempty"`
	Result   device.ReadResult `json:"result"`
}

// verifyReport 输出逐点核对结果；全部读到且没有不一致时 ok 为 true。
func verifyReport(w io.Writer, table pointtable.Table, results []device.ReadResult, asJSON bool) (ok bool, err error) {
	ok = len(results) == len(table.Points)
	items := make([]verifyItem, 0, len(results))
	verdicts := make([]string, 0, len(results))
	for i, r := range results {
		if i >= len(table.Points) {
			break
		}
		p := table.Points[i]
		it := verifyItem{Row: p.Row, Address: p.Address, Name: p.Name, Standard: p.Standard, Unit: p.Unit, Expected: p.Expected, Verdict: pointtable.Check(r, p.Expected), Result: r}
		if r.Quality == "GOOD" {
			it.Value = device.FormatValue(r.Value)
		}
		ok = ok && r.Quality == "GOOD" && (it.Verdict == "" || it.Verdict == pointtable.Match)
		items = append(items, it)
		verdicts = append(verdicts, it.Verdict)
	}
	summary := fmt.Sprintf("%d 点：%s", len(items), pointtable.Tally(verdicts))
	if asJSON {
		return ok, emitJSON(w, struct {
			Operation string       `json:"operation"`
			Table     string       `json:"table"`
			Sheet     string       `json:"sheet,omitempty"`
			Passed    bool         `json:"passed"`
			Summary   string       `json:"summary"`
			Skipped   []string     `json:"skipped,omitempty"`
			Items     []verifyItem `json:"items"`
		}{"verify", table.Name, table.Sheet, ok, summary, table.Skipped, items})
	}
	for _, it := range items {
		value := it.Value
		if value == "" {
			value = "—"
		}
		line := fmt.Sprintf("%s\t%s\t= %s [%s]", it.Address, it.Name, value, it.Result.Quality)
		if it.Expected != "" {
			line += fmt.Sprintf("\t期望 %s %s", it.Expected, it.Verdict)
		}
		if it.Result.Error != "" {
			line += "\t" + it.Result.Error
		}
		if _, err = fmt.Fprintln(w, line); err != nil {
			return ok, err
		}
	}
	_, err = fmt.Fprintln(w, summary)
	return ok, err
}

func emitJSON(w io.Writer, v any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(v)
}
