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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/sim"
	simreader "github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/sim"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/fakeset"
)

// instance is a running sim found through its root.
type instance struct {
	state  *sim.State
	client *sim.Client
	layout sim.Layout
}

func connect(g globals) (*instance, error) {
	layout, err := sim.NewLayout(g.root)
	if err != nil {
		return nil, fmt.Errorf("sim layout: %w", err)
	}
	st, err := layout.ReadState()
	if err != nil {
		return nil, fmt.Errorf("connect to sim: %w", err)
	}
	if !processAlive(st.PID) {
		return nil, fmt.Errorf("sim from %s is not running (pid %d gone); start it with serve", layout.Root, st.PID)
	}
	return &instance{layout: layout, state: st, client: sim.NewClient(layout.Socket)}, nil
}

func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("%s: %w", fs.Name(), err)
	}
	return nil
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("write json: %w", err)
	}
	return nil
}

// output writes v as JSON with --json, otherwise text.
func output(g globals, w io.Writer, v any, text string) error {
	if g.json {
		return writeJSON(w, v)
	}
	if text != "" {
		_, _ = fmt.Fprintln(w, text)
	}
	return nil
}

func ok(g globals, w io.Writer, text string) error {
	return output(g, w, map[string]bool{"ok": true}, text)
}

func cmdStatus(ctx context.Context, g globals, args []string, stdout io.Writer) error {
	if err := parse(newFlags("status"), args); err != nil {
		return err
	}
	in, err := connect(g)
	if err != nil {
		return err
	}
	st, err := in.client.Status(ctx)
	if err != nil {
		return fmt.Errorf("sim status: %w", err)
	}
	if g.json {
		return writeJSON(stdout, struct {
			Instance *sim.State `json:"instance"`
			sim.Status
		}{in.state, st})
	}
	_, _ = fmt.Fprintf(stdout, "pid %d  api %s  root %s  journal seq %d\n",
		in.state.PID, in.state.APIURL, in.layout.Root, st.JournalSeq)
	for _, name := range sortedKeys(in.state.Fakes) {
		_, _ = fmt.Fprintf(stdout, "fake %s: %s\n", name, in.state.Fakes[name])
	}
	printReaders(stdout, st.Readers)
	if st.ActiveMedia != nil {
		_, _ = fmt.Fprintf(stdout, "active: %s %s (%s) launcher %s\n",
			st.ActiveMedia.SystemID, st.ActiveMedia.Name, st.ActiveMedia.Path, st.ActiveMedia.LauncherID)
	} else {
		_, _ = fmt.Fprintln(stdout, "active: none")
	}
	if st.TrackedPID != 0 {
		_, _ = fmt.Fprintf(stdout, "tracked pid: %d\n", st.TrackedPID)
	}
	for _, pid := range sortedKeys(st.Children) {
		_, _ = fmt.Fprintf(stdout, "child %s: %s\n", pid, st.Children[pid])
	}
	for _, f := range st.Faults {
		_, _ = fmt.Fprintf(stdout, "fault %s\n", faultText(f))
	}
	return nil
}

func printReaders(w io.Writer, rs []simreader.DeviceStatus) {
	if len(rs) == 0 {
		_, _ = fmt.Fprintln(w, "readers: none")
	}
	for _, r := range rs {
		state := "unplugged"
		switch {
		case r.Plugged && r.Open:
			state = "open"
		case r.Plugged:
			state = "plugged, not open"
		}
		line := fmt.Sprintf("reader %s (%s): %s", r.Name, r.ReaderID, state)
		if r.Present != nil {
			line += fmt.Sprintf(", tag %s %q", r.Present.UID, r.Present.Text)
		}
		if r.Writing {
			line += ", write pending"
		}
		if r.PendingFault != "" {
			line += ", next write fails: " + r.PendingFault
		}
		_, _ = fmt.Fprintln(w, line)
	}
}

func cmdStop(ctx context.Context, g globals, args []string, stdout io.Writer) error {
	if err := parse(newFlags("stop"), args); err != nil {
		return err
	}
	in, err := connect(g)
	if err != nil {
		return err
	}
	if err := in.client.Stop(ctx); err != nil {
		return fmt.Errorf("stop sim: %w", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for processAlive(in.state.PID) {
		if time.Now().After(deadline) {
			return fmt.Errorf("sim pid %d still running after stop", in.state.PID)
		}
		time.Sleep(100 * time.Millisecond)
	}
	return ok(g, stdout, "sim stopped")
}

func cmdReaders(ctx context.Context, g globals, args []string, stdout io.Writer) error {
	sub := "ls"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	in, err := connect(g)
	if err != nil {
		return err
	}
	switch sub {
	case "ls", "list":
		rs, err := in.client.Readers(ctx)
		if err != nil {
			return fmt.Errorf("list readers: %w", err)
		}
		if g.json {
			return writeJSON(stdout, rs)
		}
		printReaders(stdout, rs)
		return nil
	case "add":
		fs := newFlags("readers add")
		var req sim.AddReaderRequest
		fs.BoolVar(&req.NoWrite, "no-write", false, "reader cannot write")
		fs.BoolVar(&req.NotRemovable, "no-removable", false, "reader cannot report removal, like a barcode scanner")
		fs.BoolVar(&req.NoAutoDetect, "no-auto-detect", false, "only a [[readers.connect]] entry opens it")
		fs.IntVar(&req.WriteWaitMS, "write-wait-ms", 0, "how long a write waits for a card")
		name, rest := leadingArg(args)
		if err := parse(fs, rest); err != nil {
			return err
		}
		if name == "" {
			name = fs.Arg(0)
		}
		if name == "" {
			return errors.New("readers add: name required")
		}
		req.Name = name
		r, err := in.client.AddReader(ctx, req)
		if err != nil {
			return fmt.Errorf("add reader: %w", err)
		}
		return output(g, stdout, r, "added reader "+r.Name)
	case "rm", "remove":
		if len(args) != 1 {
			return errors.New("readers rm: name required")
		}
		if err := in.client.RemoveReader(ctx, args[0]); err != nil {
			return fmt.Errorf("remove reader: %w", err)
		}
		return ok(g, stdout, "removed reader "+args[0])
	default:
		return fmt.Errorf("readers: unknown subcommand %q (ls, add, rm)", sub)
	}
}

// leadingArg splits off a positional argument given before the flags.
func leadingArg(args []string) (arg string, rest []string) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:]
	}
	return "", args
}

// defaultReader picks the reader a command acts on when none is named: the
// only reader, or sim0.
func defaultReader(ctx context.Context, in *instance, name string) (string, error) {
	if name != "" {
		return name, nil
	}
	rs, err := in.client.Readers(ctx)
	if err != nil {
		return "", fmt.Errorf("list readers: %w", err)
	}
	switch len(rs) {
	case 0:
		return "", errors.New("no sim readers: add one with readers add NAME")
	case 1:
		return rs[0].Name, nil
	}
	for _, r := range rs {
		if r.Name == "sim0" {
			return r.Name, nil
		}
	}
	return "", errors.New("several readers: pass --reader")
}

func cmdTap(ctx context.Context, g globals, args []string, stdout io.Writer) error {
	return tap(ctx, g, "tap", args, stdout, false)
}

func cmdHold(ctx context.Context, g globals, args []string, stdout io.Writer) error {
	return tap(ctx, g, "hold", args, stdout, true)
}

func tap(ctx context.Context, g globals, name string, args []string, stdout io.Writer, keep bool) error {
	fs := newFlags(name)
	var tag simreader.Tag
	reader := fs.String("reader", "", "reader name (default: the only reader, or sim0)")
	fs.StringVar(&tag.UID, "uid", "", "tag UID (default: random)")
	fs.StringVar(&tag.Data, "data", "", "raw tag data as hex")
	fs.StringVar(&tag.Type, "type", "", "tag type (default ntag215)")
	holdMS := fs.Int("hold-ms", 200, "remove the tag after this long")
	fs.BoolVar(&keep, "keep", keep, "leave the tag on the reader")
	if err := parse(fs, args); err != nil {
		return err
	}
	tag.Text = strings.Join(fs.Args(), " ")
	if tag.Text == "" && tag.UID == "" && tag.Data == "" {
		return fmt.Errorf("%s: give the tag text, --uid or --data", name)
	}
	in, err := connect(g)
	if err != nil {
		return err
	}
	r, err := defaultReader(ctx, in, *reader)
	if err != nil {
		return err
	}
	hold := time.Duration(*holdMS) * time.Millisecond
	if keep {
		hold = 0
	}
	got, err := in.client.Tap(ctx, r, tag, hold)
	if err != nil {
		return fmt.Errorf("tap: %w", err)
	}
	text := fmt.Sprintf("tapped %s on %s: %q", got.UID, r, got.Text)
	if keep {
		text += " (left on reader)"
	}
	return output(g, stdout, got, text)
}

// readerCommand runs a reader action that takes only --reader.
func readerCommand(
	name string,
	act func(c *sim.Client, ctx context.Context, reader string) error,
) command {
	return func(ctx context.Context, g globals, args []string, stdout io.Writer) error {
		fs := newFlags(name)
		reader := fs.String("reader", "", "reader name")
		if err := parse(fs, args); err != nil {
			return err
		}
		in, err := connect(g)
		if err != nil {
			return err
		}
		r, err := defaultReader(ctx, in, *reader)
		if err != nil {
			return err
		}
		if err := act(in.client, ctx, r); err != nil {
			return err
		}
		return ok(g, stdout, name+": "+r)
	}
}

var (
	cmdRemove      = readerCommand("remove", (*sim.Client).Remove)
	cmdReaderError = readerCommand("reader-error", (*sim.Client).ReaderError)
	cmdUnplug      = readerCommand("unplug", (*sim.Client).Unplug)
	cmdPlug        = readerCommand("plug", (*sim.Client).Plug)
)

func cmdScanError(ctx context.Context, g globals, args []string, stdout io.Writer) error {
	fs := newFlags("scan-error")
	reader := fs.String("reader", "", "reader name")
	msg := fs.String("message", "sim scan error", "error message")
	if err := parse(fs, args); err != nil {
		return err
	}
	in, err := connect(g)
	if err != nil {
		return err
	}
	r, err := defaultReader(ctx, in, *reader)
	if err != nil {
		return err
	}
	if err := in.client.ScanError(ctx, r, *msg); err != nil {
		return fmt.Errorf("inject scan error: %w", err)
	}
	return ok(g, stdout, "scan error on "+r)
}

func cmdWriteComplete(ctx context.Context, g globals, args []string, stdout io.Writer) error {
	fs := newFlags("write-complete")
	reader := fs.String("reader", "", "reader name")
	uid := fs.String("uid", "", "UID of the card presented (default: random)")
	if err := parse(fs, args); err != nil {
		return err
	}
	in, err := connect(g)
	if err != nil {
		return err
	}
	r, err := defaultReader(ctx, in, *reader)
	if err != nil {
		return err
	}
	tag, err := in.client.CompleteWrite(ctx, r, *uid)
	if err != nil {
		return fmt.Errorf("complete write: %w", err)
	}
	return output(g, stdout, tag, fmt.Sprintf(
		"presented card %s on %s for the pending write; `--json readers ls` lists what was written", tag.UID, r))
}

func cmdWriteFail(ctx context.Context, g globals, args []string, stdout io.Writer) error {
	fs := newFlags("write-fail")
	reader := fs.String("reader", "", "reader name")
	mode := fs.String("mode", string(simreader.WriteFailError), "error, removed, no-tag or unsupported")
	if err := parse(fs, args); err != nil {
		return err
	}
	in, err := connect(g)
	if err != nil {
		return err
	}
	r, err := defaultReader(ctx, in, *reader)
	if err != nil {
		return err
	}
	if err := in.client.FailNextWrite(ctx, r, simreader.WriteFailure(*mode)); err != nil {
		return fmt.Errorf("fail next write: %w", err)
	}
	return ok(g, stdout, "next write on "+r+" fails: "+*mode)
}

func cmdLaunches(ctx context.Context, g globals, args []string, stdout io.Writer) error {
	return journal(ctx, g, "launches", args, stdout, sim.LaunchKinds)
}

func cmdJournal(ctx context.Context, g globals, args []string, stdout io.Writer) error {
	return journal(ctx, g, "journal", args, stdout, nil)
}

func journal(ctx context.Context, g globals, name string, args []string, stdout io.Writer, kinds []string) error {
	fs := newFlags(name)
	since := fs.Int64("since", 0, "only events after this sequence number")
	follow := fs.Bool("follow", false, "keep printing new events")
	timeout := fs.Duration("timeout", 0, "with --follow, stop after this long")
	kindFlag := ""
	if kinds == nil {
		fs.StringVar(&kindFlag, "kind", "", "comma-separated event kinds")
	}
	if err := parse(fs, args); err != nil {
		return err
	}
	if kindFlag != "" {
		kinds = splitList(kindFlag)
	}
	in, err := connect(g)
	if err != nil {
		return err
	}
	printEvent := func(e *sim.Event) error {
		if g.json {
			data, encErr := json.Marshal(e)
			if encErr != nil {
				return fmt.Errorf("encode event: %w", encErr)
			}
			_, encErr = fmt.Fprintln(stdout, string(data))
			return encErr //nolint:wrapcheck // stdout write.
		}
		_, writeErr := fmt.Fprintln(stdout, eventText(e))
		return writeErr //nolint:wrapcheck // stdout write.
	}
	if *follow {
		if *timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, *timeout)
			defer cancel()
		}
		err = in.client.Follow(ctx, *since, kinds, func(e sim.Event) error { return printEvent(&e) })
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, sim.ErrStopFollowing) {
			return nil
		}
		return err //nolint:wrapcheck // Already descriptive.
	}
	events, err := in.client.Journal(ctx, *since, kinds...)
	if err != nil {
		return fmt.Errorf("read journal: %w", err)
	}
	if g.json {
		if events == nil {
			events = []sim.Event{}
		}
		return writeJSON(stdout, events)
	}
	for i := range events {
		if printErr := printEvent(&events[i]); printErr != nil {
			return printErr
		}
	}
	return nil
}

func eventText(e *sim.Event) string {
	var b strings.Builder
	fmt.Fprintf(&b, "#%d %s %s", e.Seq, e.Time.Format("15:04:05.000"), e.Kind)
	if e.Launcher != "" {
		fmt.Fprintf(&b, " launcher=%s", e.Launcher)
	}
	if e.System != "" {
		fmt.Fprintf(&b, " system=%s", e.System)
	}
	if e.PID != 0 {
		fmt.Fprintf(&b, " pid=%d", e.PID)
	}
	if e.Intent != "" {
		fmt.Fprintf(&b, " intent=%s", e.Intent)
	}
	if e.Path != "" {
		fmt.Fprintf(&b, " path=%q", e.Path)
	}
	if e.Detail != "" {
		fmt.Fprintf(&b, " detail=%q", e.Detail)
	}
	for _, k := range sortedKeys(e.Args) {
		fmt.Fprintf(&b, " %s=%q", k, e.Args[k])
	}
	if e.Error != "" {
		fmt.Fprintf(&b, " error=%q", e.Error)
	}
	return b.String()
}

func cmdFault(ctx context.Context, g globals, args []string, stdout io.Writer) error {
	sub := "ls"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	in, err := connect(g)
	if err != nil {
		return err
	}
	switch sub {
	case "ls", "list":
		fs, err := in.client.Faults(ctx)
		if err != nil {
			return fmt.Errorf("list faults: %w", err)
		}
		if g.json {
			if fs == nil {
				fs = []sim.Fault{}
			}
			return writeJSON(stdout, fs)
		}
		if len(fs) == 0 {
			_, _ = fmt.Fprintln(stdout, "no faults")
		}
		for _, f := range fs {
			_, _ = fmt.Fprintln(stdout, faultText(f))
		}
		return nil
	case "set":
		fs := newFlags("fault set")
		var f sim.Fault
		fs.StringVar(&f.Launcher, "launcher", "*", "launcher ID, or * for every launcher")
		fs.IntVar(&f.FailLaunches, "fail-launches", 0, "fail the next N launches")
		fs.IntVar(&f.LaunchDelayMS, "launch-delay-ms", 0, "delay each launch")
		fs.IntVar(&f.CrashAfterMS, "crash-after-ms", 0, "kill the launched process after this long")
		fs.IntVar(&f.ExitAfterMS, "exit-after-ms", 0, "the launched process exits by itself after this long")
		fs.IntVar(&f.FailStops, "fail-stops", 0, "the next N stops fail and leave media running")
		fs.BoolVar(&f.FailPreflight, "fail-preflight", false, "Preflight refuses every launch")
		fs.BoolVar(&f.ReadyTimeout, "ready-timeout", false, "WaitForReady never returns")
		fs.BoolVar(&f.NotRunning, "not-running", false, "GameRunning reports nothing running")
		fs.BoolVar(&f.Unavailable, "unavailable", false, "Availability fails")
		fs.BoolVar(&f.FailKill, "fail-kill", false, "a custom Kill fails, so signals are used")
		if err := parse(fs, args); err != nil {
			return err
		}
		if err := in.client.SetFault(ctx, f); err != nil {
			return fmt.Errorf("set fault: %w", err)
		}
		return output(g, stdout, f, "fault set: "+faultText(f))
	case "clear":
		fs := newFlags("fault clear")
		launcher := fs.String("launcher", "", "launcher ID (default: every fault)")
		if err := parse(fs, args); err != nil {
			return err
		}
		if err := in.client.ClearFault(ctx, *launcher); err != nil {
			return fmt.Errorf("clear fault: %w", err)
		}
		return ok(g, stdout, "faults cleared")
	default:
		return fmt.Errorf("fault: unknown subcommand %q (ls, set, clear)", sub)
	}
}

func faultText(f sim.Fault) string {
	data, _ := json.Marshal(f)
	return string(data)
}

// rpc calls a JSON-RPC method on the instance's API.
func rpc(ctx context.Context, apiURL, method string, params json.RawMessage) (json.RawMessage, error) {
	return sim.NewAPI(apiURL).Call(ctx, method, params) //nolint:wrapcheck // Already names the method.
}

func cmdAPI(ctx context.Context, g globals, args []string, stdout io.Writer) error {
	if len(args) < 1 || len(args) > 2 {
		return errors.New("api: usage: api METHOD [JSON_PARAMS]")
	}
	in, err := connect(g)
	if err != nil {
		return err
	}
	var params json.RawMessage
	if len(args) == 2 {
		if !json.Valid([]byte(args[1])) {
			return errors.New("api: params are not valid JSON")
		}
		params = json.RawMessage(args[1])
	}
	result, err := rpc(ctx, in.state.APIURL, args[0], params)
	if err != nil {
		return err
	}
	var pretty bytes.Buffer
	if len(result) == 0 || json.Indent(&pretty, result, "", "  ") != nil {
		pretty.Reset()
		pretty.WriteString("null")
	}
	_, err = fmt.Fprintln(stdout, pretty.String())
	return err //nolint:wrapcheck // stdout write.
}

type notification = sim.Notification

func cmdWait(ctx context.Context, g globals, args []string, stdout io.Writer) error {
	name, rest := leadingArg(args)
	fs := newFlags("wait")
	timeout := fs.Duration("timeout", 30*time.Second, "give up after this long")
	var matches multiFlag
	fs.Var(&matches, "match", "KEY=VALUE a params field must have; dotted keys reach nested fields (repeatable)")
	if err := parse(fs, rest); err != nil {
		return err
	}
	if name == "" {
		name = fs.Arg(0)
	}
	if name == "" {
		return errors.New("wait: notification name required, such as media.started")
	}
	in, err := connect(g)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	n, err := waitNotification(ctx, in.state.APIURL, func(n notification) bool {
		return n.Method == name && sim.MatchParams(n.Params, matches)
	}, nil)
	if err != nil {
		return fmt.Errorf("wait %s: %w", name, err)
	}
	if g.json {
		return writeJSON(stdout, n)
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, n.Params, "", "  ") != nil {
		pretty.Write(n.Params)
	}
	_, _ = fmt.Fprintf(stdout, "%s %s\n", n.Method, pretty.String())
	return nil
}

// waitNotification reads the API event stream until match accepts one.
func waitNotification(
	ctx context.Context,
	apiURL string,
	match func(notification) bool,
	subscribed func() error,
) (notification, error) {
	return sim.NewAPI(apiURL).WaitNotification(ctx, match, subscribed) //nolint:wrapcheck // Self-describing.
}

type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }

func (m *multiFlag) Set(v string) error {
	if !strings.Contains(v, "=") {
		return errors.New("want KEY=VALUE")
	}
	*m = append(*m, v)
	return nil
}

func cmdFake(ctx context.Context, g globals, args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("fake: name required (%s)", strings.Join(fakeset.Names, ", "))
	}
	name, rest := args[0], args[1:]
	cli, found := fakeCLIs()[name]
	if !found {
		return fmt.Errorf("fake: unknown fake %q (have %s)", name, strings.Join(fakeset.Names, ", "))
	}
	in, err := connect(g)
	if err != nil {
		return err
	}
	if _, running := in.state.Fakes[name]; !running {
		return fmt.Errorf("fake %s is not running: restart serve with --fakes %s", name, name)
	}
	prefix := "/fakes/" + name + "/"
	call := func(ctx context.Context, method, path string, body []byte) ([]byte, error) {
		return in.client.Call(ctx, method, prefix+strings.TrimPrefix(path, "/"), body)
	}
	return cli(ctx, rest, call, stdout)
}

func splitList(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
