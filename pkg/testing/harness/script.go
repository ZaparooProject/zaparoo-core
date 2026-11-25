//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later
//
// This file is part of Zaparoo Core.
//
// Zaparoo Core is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// Zaparoo Core is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.

package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/sim"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	simreader "github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/sim"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/simfixtures"
	"github.com/rogpeppe/go-internal/testscript"
)

// Script timeouts. A notification or event command fails after ScriptWaitTimeout
// unless it passes -timeout; a negated one passes if nothing matches within
// ScriptAbsentTimeout.
var (
	ScriptWaitTimeout   = 15 * time.Second
	ScriptAbsentTimeout = 750 * time.Millisecond
)

const scriptStateKey = "zaparoo-sim"

// scriptState is one script's instance and its cursors.
type scriptState struct {
	in *Instance
	// background is the result of the last api -bg call.
	background chan apiResult
	// notifications is the index of the next notification wait looks at.
	notifications int
	// journal is the journal sequence event looks after.
	journal int64
}

type apiResult struct {
	err    error
	method string
	result json.RawMessage
	// skipTo, when above the notification cursor, moves it past the
	// notifications that refused attempts published.
	skipTo int
}

// ScriptSetup prepares a testscript environment for the sim commands. Use it
// as testscript.Params.Setup, or call it from your own Setup.
func ScriptSetup(env *testscript.Env) error {
	env.Values[scriptStateKey] = &scriptState{}
	return nil
}

// ScriptCommands returns the testscript commands that drive a sim instance:
//
//	sim-start [flags]          boot an instance (see sim-start -h); sets $ROOT and $ROMS
//	sim-stop                   close it (also done when the script ends)
//	rom [-size N] PATH...      create files under $ROMS, empty or N zero bytes
//	fixtures [flags]           generate a ROM tree under $ROMS (see simfixtures);
//	                           prints "files N media M"
//	index [SYSTEM,...]         run media.generate and wait for it; prints
//	                           "media N"
//	idle                       wait until no index, optimization or scrape runs
//	tap [-reader R] [-uid U] [-data HEX] [-type T] [-prop NAME=VALUE]... [-for MS] TEXT...
//	                           present a tag whose text is the remaining
//	                           arguments joined by spaces, so $ROMS/a b.sfc
//	                           works unquoted; -for removes it after MS;
//	                           -prop adds a scan property, as an optical
//	                           drive reports disc.kind, disc.system, disc.drive
//	remove [-reader R]         take the tag off the reader
//	api [-bg] METHOD [JSON...] call the Core API; result as indented JSON on stdout.
//	                           ! api expects an error, its message on stderr.
//	                           -bg runs a blocking call such as readers.write in
//	                           the background
//	api-wait                   wait for the api -bg call and report it like api
//	notification [-timeout D] METHOD [KEY=VALUE...]
//	                           wait for a Core notification after the cursor and
//	                           move the cursor past it; ! notification asserts
//	                           none arrives (testscript's own wait is for
//	                           background commands)
//	event [-peek] [-timeout D] KIND [KEY=VALUE...]
//	                           the same for sim journal events (launch, stop, ...);
//	                           -peek leaves the cursor where it was, for events
//	                           whose order is not fixed
//	played PATH...             wait until play history's newest entry is the
//	                           media at the path the arguments join to; history
//	                           is written after media.started is sent
//	mark                       move both cursors to now
//	fault JSON | fault clear [LAUNCHER]
//	write-complete [-reader R] [UID]
//	                           present a tag to the pending write
//	write-fail [-reader R] MODE
//	                           make the next write fail (error, removed, no-tag, unsupported)
//	sounds                     print the sounds played since the last sounds, one per line
//	status                     print the sim control status as JSON
//
// KEY=VALUE matches use dotted paths into the JSON params or event, such as
// media.systemId=SNES or launcher=SimBlocking.
func ScriptCommands() map[string]func(ts *testscript.TestScript, neg bool, args []string) {
	return map[string]func(ts *testscript.TestScript, neg bool, args []string){
		"sim-start":      cmdSimStart,
		"sim-stop":       cmdSimStop,
		"rom":            cmdROM,
		"fixtures":       cmdFixtures,
		"index":          cmdIndex,
		"idle":           cmdIdle,
		"tap":            cmdTap,
		"remove":         cmdRemove,
		"api":            cmdAPI,
		"api-wait":       cmdAPIWait,
		"notification":   cmdNotification,
		"event":          cmdEvent,
		"played":         cmdPlayed,
		"mark":           cmdMark,
		"fault":          cmdFault,
		"write-complete": cmdWriteComplete,
		"write-fail":     cmdWriteFail,
		"sounds":         cmdSounds,
		"status":         cmdStatus,
	}
}

func state(ts *testscript.TestScript) *scriptState {
	st, ok := ts.Value(scriptStateKey).(*scriptState)
	if !ok {
		ts.Fatalf("sim commands need harness.ScriptSetup")
	}
	return st
}

func running(ts *testscript.TestScript) *scriptState {
	st := state(ts)
	if st.in == nil {
		ts.Fatalf("no sim instance: run sim-start first")
	}
	return st
}

func noNeg(ts *testscript.TestScript, neg bool, name string) {
	if neg {
		ts.Fatalf("%s does not support !", name)
	}
}

func scriptFlags(_ *testscript.TestScript, name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func parse(ts *testscript.TestScript, fs *flag.FlagSet, args []string) {
	if err := fs.Parse(args); err != nil {
		ts.Fatalf("%s: %v", fs.Name(), err)
	}
}

func splitComma(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func cmdSimStart(ts *testscript.TestScript, neg bool, args []string) {
	noNeg(ts, neg, "sim-start")
	st := state(ts)
	if st.in != nil {
		ts.Fatalf("sim-start: an instance is already running")
	}
	fs := scriptFlags(ts, "sim-start")
	fakes := fs.String("fakes", "", "comma-separated fakes")
	readerNames := fs.String("readers", DefaultReader, "comma-separated sim readers; none for no readers")
	scanMode := fs.String("scan-mode", "", "readers.scan.mode: tap or hold")
	exitDelay := fs.Float64("exit-delay", 0, "readers.scan.exit_delay in seconds")
	guard := fs.Bool("launch-guard", false, "enable the launch guard")
	guardConfirm := fs.Bool("guard-confirm", false, "the launch guard needs an API confirm")
	discOrder := fs.String("disc-order", "", "readers.drivers.opticaldrive.launch_order")
	audioCD := fs.String("audio-cd-launcher", "", "readers.drivers.opticaldrive.audio_cd_launcher")
	parse(ts, fs, args)
	if fs.NArg() > 0 {
		ts.Fatalf("sim-start: unexpected argument %q", fs.Arg(0))
	}

	opts := Options{Fakes: splitComma(*fakes)}
	opts.Readers = []string{}
	if *readerNames != "none" {
		opts.Readers = splitComma(*readerNames)
	}
	opts.Values = func(v *config.Values) {
		if *scanMode != "" {
			v.Readers.Scan.Mode = *scanMode
		}
		v.Readers.Scan.ExitDelay = float32(*exitDelay)
		if *guard || *guardConfirm {
			v.Readers.Scan.LaunchGuard.Enabled = true
			v.Readers.Scan.LaunchGuard.RequireConfirm = *guardConfirm
		}
		if *discOrder != "" || *audioCD != "" {
			if v.Readers.Drivers == nil {
				v.Readers.Drivers = map[string]config.DriverConfig{}
			}
			v.Readers.Drivers["opticaldrive"] = config.DriverConfig{
				LaunchOrder:     *discOrder,
				AudioCDLauncher: *audioCD,
			}
		}
	}
	in, err := Start(context.Background(), &opts)
	ts.Check(err)
	st.in = in
	st.notifications = 0
	st.journal = 0
	ts.Defer(func() { _ = in.Close() })
	ts.Setenv("ROOT", in.Root)
	ts.Setenv("ROMS", in.Layout.ROMs)
}

func cmdSimStop(ts *testscript.TestScript, neg bool, _ []string) {
	noNeg(ts, neg, "sim-stop")
	st := running(ts)
	ts.Check(st.in.Close())
	st.in = nil
}

func cmdROM(ts *testscript.TestScript, neg bool, args []string) {
	noNeg(ts, neg, "rom")
	st := running(ts)
	fs := scriptFlags(ts, "rom")
	size := fs.Int("size", 0, "bytes of zeros to write")
	if err := fs.Parse(args); err != nil {
		ts.Fatalf("rom: %v", err)
	}
	if fs.NArg() == 0 || *size < 0 {
		ts.Fatalf("usage: rom [-size N] PATH...")
	}
	for _, rel := range fs.Args() {
		if filepath.IsAbs(rel) || strings.HasPrefix(filepath.Clean(rel), "..") {
			ts.Fatalf("rom: %s must be relative to $ROMS", rel)
		}
		path := filepath.Join(st.in.Layout.ROMs, rel)
		ts.Check(os.MkdirAll(filepath.Dir(path), 0o750))
		ts.Check(os.WriteFile(path, make([]byte, *size), 0o600))
	}
}

func cmdFixtures(ts *testscript.TestScript, neg bool, args []string) {
	noNeg(ts, neg, "fixtures")
	st := running(ts)
	fs := scriptFlags(ts, "fixtures")
	opts := simfixtures.Options{Root: st.in.Layout.ROMs}
	systems := fs.String("systems", "", "comma-separated systems or folders")
	fs.BoolVar(&opts.All, "all", false, "every system")
	fs.IntVar(&opts.PerSystem, "per-system", 0, "titles per system")
	fs.Int64Var(&opts.Seed, "seed", 1, "seed")
	fs.StringVar(&opts.Source, "source", "", "gamedatabase, synthetic or dats")
	fs.StringVar(&opts.DatsDir, "dats", "", "DAT directory for -source dats")
	fs.BoolVar(&opts.Realistic, "realistic", false, "No-Intro style names")
	fs.BoolVar(&opts.Zips, "zips", false, "some titles as zips")
	fs.BoolVar(&opts.Gamelist, "gamelist", false, "gamelist.xml per system")
	fs.BoolVar(&opts.MediaImages, "media-images", false, "media-folder images")
	fs.BoolVar(&opts.Subfolders, "subfolders", false, "nested folders")
	fs.BoolVar(&opts.EdgeCases, "edge-cases", false, "scanner edge cases")
	parse(ts, fs, args)
	opts.Systems = splitComma(*systems)
	ctx, cancel := ctxFor(StartTimeout)
	defer cancel()
	report, err := simfixtures.Generate(ctx, opts)
	ts.Check(err)
	st.in.Fixtures = report
	_, _ = fmt.Fprintf(ts.Stdout(), "files %d media %d\n", report.Files, report.Media)
}

func cmdIndex(ts *testscript.TestScript, neg bool, args []string) {
	noNeg(ts, neg, "index")
	st := running(ts)
	if len(args) > 1 {
		ts.Fatalf("usage: index [SYSTEM,...]")
	}
	var systems []string
	if len(args) == 1 {
		systems = splitComma(args[0])
	}
	ctx, cancel := ctxFor(StartTimeout)
	defer cancel()
	status, err := st.in.API.Index(ctx, systems)
	ts.Check(err)
	media := 0
	if status != nil && status.TotalMedia != nil {
		media = *status.TotalMedia
	}
	_, _ = fmt.Fprintf(ts.Stdout(), "media %d\n", media)
}

func cmdIdle(ts *testscript.TestScript, neg bool, _ []string) {
	noNeg(ts, neg, "idle")
	st := running(ts)
	ctx, cancel := ctxFor(StartTimeout)
	defer cancel()
	ts.Check(st.in.API.WaitIdle(ctx))
}

func ctxFor(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

func cmdTap(ts *testscript.TestScript, neg bool, args []string) {
	noNeg(ts, neg, "tap")
	st := running(ts)
	fs := scriptFlags(ts, "tap")
	reader := fs.String("reader", "", "sim reader (default: the first)")
	uid := fs.String("uid", "", "tag UID (default: generated)")
	data := fs.String("data", "", "tag data as hex")
	tokenType := fs.String("type", "", "token type")
	forMS := fs.Int("for", 0, "remove the tag again after this many ms")
	var props scanProperties
	fs.Var(&props, "prop", "scan property NAME=VALUE the reader reports; repeatable")
	parse(ts, fs, args)
	dev, err := st.in.Reader(*reader)
	ts.Check(err)
	text := strings.Join(fs.Args(), " ")
	_, err = dev.Tap(&simreader.Tag{UID: *uid, Text: text, Data: *data, Type: *tokenType, Properties: props},
		time.Duration(*forMS)*time.Millisecond)
	ts.Check(err)
}

// scanProperties collects repeated -prop NAME=VALUE flags.
type scanProperties []readers.ScanProperty

func (p *scanProperties) String() string { return fmt.Sprint(*p) }

func (p *scanProperties) Set(v string) error {
	name, value, ok := strings.Cut(v, "=")
	if !ok || name == "" {
		return fmt.Errorf("want NAME=VALUE, got %q", v)
	}
	*p = append(*p, readers.ScanProperty{Name: name, Value: value})
	return nil
}

func cmdRemove(ts *testscript.TestScript, neg bool, args []string) {
	noNeg(ts, neg, "remove")
	st := running(ts)
	fs := scriptFlags(ts, "remove")
	reader := fs.String("reader", "", "sim reader (default: the first)")
	parse(ts, fs, args)
	dev, err := st.in.Reader(*reader)
	ts.Check(err)
	ts.Check(dev.RemoveTag())
}

func cmdAPI(ts *testscript.TestScript, neg bool, args []string) {
	st := running(ts)
	bg := len(args) > 0 && args[0] == "-bg"
	if bg {
		args = args[1:]
	}
	if len(args) < 1 {
		ts.Fatalf("usage: api [-bg] METHOD [JSON...]")
	}
	var params any
	if len(args) > 1 {
		// Joined, so JSON with spaces needs no quoting.
		params = json.RawMessage(strings.Join(args[1:], " "))
	}
	retryBusy := !neg && args[0] == models.MethodMediaScrape
	call := func() apiResult {
		ctx, cancel := ctxFor(ScriptWaitTimeout)
		defer cancel()
		skipTo := 0
		for {
			from := st.in.Events.Len()
			result, err := st.in.API.Call(ctx, args[0], params)
			if !retryBusy || !scrapeBusy(err) {
				return apiResult{method: args[0], result: result, err: err, skipTo: skipTo}
			}
			if strings.Contains(err.Error(), scrapePendingMsg) {
				// This refusal also publishes a failed, done media.scraping
				// notification, which would satisfy a later wait for done.
				skipTo = skipRefusal(ctx, st, from, params)
			}
			select {
			case <-ctx.Done():
				return apiResult{method: args[0], result: result, err: err, skipTo: skipTo}
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	if !bg {
		reportAPI(ts, neg, call())
		return
	}
	noNeg(ts, neg, "api -bg")
	if st.background != nil {
		ts.Fatalf("api -bg: the previous background call has not been waited for")
	}
	st.background = make(chan apiResult, 1)
	go func(ch chan<- apiResult) { ch <- call() }(st.background)
}

// scrapeBusy reports a media.scrape refusal caused by earlier scrape work.
// Between one queued scrape job finishing and the next starting,
// media.scrape.status reports no scrape while media.scrape still refuses, so
// no idle check can close the gap; a scenario that expects to start a scrape
// retries instead. A negated api call never retries, so refusals stay
// testable.
func scrapeBusy(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "scraping already in progress") ||
		strings.Contains(msg, scrapePendingMsg)
}

const scrapePendingMsg = "scraping jobs are pending"

// skipRefusal waits for the failed media.scraping notification a refused
// media.scrape publishes and returns the cursor just past it, or from when it
// does not arrive.
func skipRefusal(ctx context.Context, st *scriptState, from int, params any) int {
	var p struct {
		ScraperID string `json:"scraperId"`
	}
	if raw, ok := params.(json.RawMessage); ok {
		_ = json.Unmarshal(raw, &p)
	}
	wctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, next, err := st.in.Events.Wait(wctx, from, models.NotificationMediaScraping,
		"scraperId="+p.ScraperID, "state=failed", "error=failed to start media scrape")
	if err != nil {
		return from
	}
	return next
}

func cmdAPIWait(ts *testscript.TestScript, neg bool, _ []string) {
	st := running(ts)
	if st.background == nil {
		ts.Fatalf("api-wait: no api -bg call")
	}
	res := <-st.background
	st.background = nil
	reportAPI(ts, neg, res)
}

func reportAPI(ts *testscript.TestScript, neg bool, res apiResult) {
	if st := running(ts); res.skipTo > st.notifications {
		st.notifications = res.skipTo
	}
	if neg {
		if res.err == nil {
			ts.Fatalf("api %s: succeeded, want an error", res.method)
		}
		_, _ = fmt.Fprintln(ts.Stderr(), res.err)
		return
	}
	if res.err != nil {
		ts.Fatalf("api %s: %v", res.method, res.err)
	}
	var out bytes.Buffer
	if len(res.result) == 0 || json.Indent(&out, res.result, "", "  ") != nil {
		out.Reset()
		out.Write(res.result)
	}
	out.WriteByte('\n')
	_, _ = ts.Stdout().Write(out.Bytes())
}

// waitFlags parses [-timeout D] NAME [KEY=VALUE...].
func waitFlags(
	ts *testscript.TestScript, name string, neg bool, args []string,
) (target string, matches []string, timeout time.Duration) {
	fs := scriptFlags(ts, name)
	timeoutFlag := fs.Duration("timeout", 0, "how long to wait")
	parse(ts, fs, args)
	if fs.NArg() < 1 {
		ts.Fatalf("usage: %s [-timeout D] NAME [KEY=VALUE...]", name)
	}
	d := *timeoutFlag
	if d == 0 {
		d = ScriptWaitTimeout
		if neg {
			d = ScriptAbsentTimeout
		}
	}
	return fs.Arg(0), fs.Args()[1:], d
}

func cmdNotification(ts *testscript.TestScript, neg bool, args []string) {
	st := running(ts)
	method, matches, timeout := waitFlags(ts, "notification", neg, args)
	ctx, cancel := ctxFor(timeout)
	defer cancel()
	n, next, err := st.in.Events.Wait(ctx, st.notifications, method, matches...)
	switch {
	case neg && err == nil:
		ts.Fatalf("notification: got %s %s, want none", n.Method, n.Params)
	case neg:
		return
	case err != nil:
		ts.Fatalf("%v; seen since cursor:\n%s", err, describeNotifications(st.in.Events.All(st.notifications)))
	}
	st.notifications = next
	ts.Logf("%s %s", n.Method, n.Params)
}

func describeNotifications(ns []sim.Notification) string {
	var b strings.Builder
	for _, n := range ns {
		fmt.Fprintf(&b, "  %s %s\n", n.Method, n.Params)
	}
	if b.Len() == 0 {
		return "  (none)"
	}
	return b.String()
}

func eventMatches(e *sim.Event, kind string, matches []string) bool {
	if e.Kind != kind {
		return false
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return false
	}
	return sim.MatchParams(raw, matches)
}

func cmdEvent(ts *testscript.TestScript, neg bool, args []string) {
	st := running(ts)
	peek := len(args) > 0 && args[0] == "-peek"
	if peek {
		args = args[1:]
	}
	kind, matches, timeout := waitFlags(ts, "event", neg, args)
	journal := st.in.Platform.Journal()
	deadline := time.Now().Add(timeout)
	for {
		events := journal.Since(st.journal)
		for i := range events {
			e := &events[i]
			if !eventMatches(e, kind, matches) {
				continue
			}
			if neg {
				raw, _ := json.Marshal(e)
				ts.Fatalf("event: got %s, want none", raw)
			}
			if !peek {
				st.journal = e.Seq
			}
			raw, _ := json.Marshal(e)
			ts.Logf("%s", raw)
			return
		}
		if time.Now().After(deadline) {
			if neg {
				return
			}
			var b strings.Builder
			events = journal.Since(st.journal)
			for i := range events {
				raw, _ := json.Marshal(&events[i])
				fmt.Fprintf(&b, "  %s\n", raw)
			}
			ts.Fatalf("event %s %v: none after %s; journal since cursor:\n%s", kind, matches, timeout, b.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func cmdPlayed(ts *testscript.TestScript, neg bool, args []string) {
	noNeg(ts, neg, "played")
	st := running(ts)
	if len(args) == 0 {
		ts.Fatalf("usage: played PATH...")
	}
	want := strings.Join(args, " ")
	ctx, cancel := ctxFor(ScriptWaitTimeout)
	defer cancel()
	last := ""
	for {
		result, err := st.in.API.Call(ctx, models.MethodMediaHistory, map[string]any{"limit": 1})
		if err == nil {
			var history models.MediaHistoryResponse
			ts.Check(json.Unmarshal(result, &history))
			if len(history.Entries) > 0 {
				last = history.Entries[0].MediaPath
				if last == want {
					return
				}
			}
		}
		select {
		case <-ctx.Done():
			ts.Fatalf("played: newest history entry is %q, want %q (last error: %v)", last, want, err)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func cmdMark(ts *testscript.TestScript, neg bool, _ []string) {
	noNeg(ts, neg, "mark")
	st := running(ts)
	st.notifications = st.in.Events.Len()
	st.journal = st.in.Platform.Journal().Last()
}

func cmdFault(ts *testscript.TestScript, neg bool, args []string) {
	noNeg(ts, neg, "fault")
	st := running(ts)
	switch {
	case len(args) >= 1 && args[0] == "clear":
		id := ""
		if len(args) > 1 {
			id = args[1]
		}
		st.in.Platform.Faults().Clear(id)
	case len(args) == 1:
		var f sim.Fault
		dec := json.NewDecoder(strings.NewReader(args[0]))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&f); err != nil {
			ts.Fatalf("fault: %v", err)
		}
		st.in.Platform.Faults().Set(f)
	default:
		ts.Fatalf("usage: fault JSON | fault clear [LAUNCHER]")
	}
}

func cmdWriteComplete(ts *testscript.TestScript, neg bool, args []string) {
	noNeg(ts, neg, "write-complete")
	st := running(ts)
	fs := scriptFlags(ts, "write-complete")
	reader := fs.String("reader", "", "sim reader (default: the first)")
	parse(ts, fs, args)
	dev, err := st.in.Reader(*reader)
	ts.Check(err)
	ctx, cancel := ctxFor(ScriptWaitTimeout)
	defer cancel()
	for !dev.Status().Writing {
		if ctx.Err() != nil {
			ts.Fatalf("write-complete: no write pending on %s", dev.Status().Name)
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, err = dev.CompleteWrite(fs.Arg(0))
	ts.Check(err)
}

func cmdWriteFail(ts *testscript.TestScript, neg bool, args []string) {
	noNeg(ts, neg, "write-fail")
	st := running(ts)
	fs := scriptFlags(ts, "write-fail")
	reader := fs.String("reader", "", "sim reader (default: the first)")
	parse(ts, fs, args)
	if fs.NArg() != 1 {
		ts.Fatalf("usage: write-fail [-reader R] MODE")
	}
	dev, err := st.in.Reader(*reader)
	ts.Check(err)
	dev.FailNextWrite(simreader.WriteFailure(fs.Arg(0)))
}

// cmdSounds prints the sounds played since the last call and forgets them.
// A sound can play after the notification a script waited for, so -wait
// NAME first waits until that sound is among them.
func cmdSounds(ts *testscript.TestScript, neg bool, args []string) {
	noNeg(ts, neg, "sounds")
	st := running(ts)
	fs := scriptFlags(ts, "sounds")
	wait := fs.String("wait", "", "sound to wait for")
	timeout := fs.Duration("timeout", ScriptWaitTimeout, "how long to wait")
	parse(ts, fs, args)
	if fs.NArg() > 0 {
		ts.Fatalf("usage: sounds [-wait NAME] [-timeout D]")
	}
	played := st.in.Audio.Played()
	deadline := time.Now().Add(*timeout)
	for *wait != "" && !slices.Contains(played, *wait) {
		if time.Now().After(deadline) {
			ts.Fatalf("sounds: no %q after %s; played %v", *wait, *timeout, played)
		}
		time.Sleep(20 * time.Millisecond)
		played = st.in.Audio.Played()
	}
	for _, s := range played {
		_, _ = fmt.Fprintln(ts.Stdout(), s)
	}
	st.in.Audio.Reset()
}

func cmdStatus(ts *testscript.TestScript, neg bool, _ []string) {
	noNeg(ts, neg, "status")
	st := running(ts)
	raw, err := json.MarshalIndent(st.in.Platform.StatusNow(), "", "  ")
	ts.Check(err)
	_, _ = fmt.Fprintf(ts.Stdout(), "%s\n", raw)
}
