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

package sim

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	simreader "github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/sim"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestPlatform(t *testing.T, opts *Options) *Platform {
	t.Helper()
	if opts.Layout.Root == "" {
		l, err := NewLayout(filepath.Join(t.TempDir(), "root"))
		require.NoError(t, err)
		opts.Layout = l
	}
	p, err := New(opts)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Stop() })
	return p
}

func TestCheckRootRefusesUnsafeRoots(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	for _, root := range []string{"/", home} {
		l, layoutErr := NewLayout(root)
		require.NoError(t, layoutErr)
		require.Error(t, l.CheckRoot(), root)
	}
	_, err = NewLayout(" ")
	require.Error(t, err)

	dir := t.TempDir()
	l, err := NewLayout(dir)
	require.NoError(t, err)
	require.NoError(t, l.CheckRoot(), "an empty directory is fine")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o600))
	require.Error(t, l.CheckRoot(), "a non-empty directory without the marker is refused")
	require.NoError(t, l.Ensure())
	require.NoError(t, l.CheckRoot(), "a marked sim root is reused")

	missing, err := NewLayout(filepath.Join(dir, "new"))
	require.NoError(t, err)
	require.NoError(t, missing.CheckRoot())
}

func TestDefaultsKeepTheSimIsolated(t *testing.T) {
	t.Parallel()
	l, err := NewLayout(t.TempDir())
	require.NoError(t, err)
	v := l.Defaults(DefaultsOptions{APIPort: 17497, Readers: []string{"sim0", "sim1"}})
	require.NotNil(t, v.Service.APIPort)
	assert.Equal(t, 17497, *v.Service.APIPort)
	assert.Equal(t, "127.0.0.1", v.Service.APIListen)
	assert.False(t, *v.Service.Discovery.Enabled)
	assert.False(t, *v.Updates.Check)
	assert.False(t, *v.Updates.Install)
	assert.False(t, v.ErrorReporting)
	assert.Equal(t, []string{l.ROMs}, v.Launchers.IndexRoot)
	for _, id := range RealDrivers() {
		require.NotNil(t, v.Readers.Drivers[id].AutoDetect, id)
		assert.False(t, *v.Readers.Drivers[id].AutoDetect, id)
	}
	require.Len(t, v.Readers.Connect, 2)
	assert.Equal(t, "sim", v.Readers.Connect[0].Driver)
	assert.Equal(t, "sim1", v.Readers.Connect[1].Path)
}

func TestStateRoundTrip(t *testing.T) {
	t.Parallel()
	l, err := NewLayout(t.TempDir())
	require.NoError(t, err)
	_, err = l.ReadState()
	require.Error(t, err)
	require.NoError(t, l.WriteState(&State{PID: 42, APIPort: 1234, Ready: true}))
	st, err := l.ReadState()
	require.NoError(t, err)
	assert.Equal(t, 42, st.PID)
	assert.True(t, st.Ready)
}

func TestFaultsMergeAndCount(t *testing.T) {
	t.Parallel()
	f := NewFaults()
	f.Set(Fault{LaunchDelayMS: 50, NotRunning: true, FailLaunches: 1})
	f.Set(Fault{Launcher: "a", CrashAfterMS: 10, FailStops: 2})

	a := f.For("a")
	assert.Equal(t, 50, a.LaunchDelayMS, "inherits the global delay")
	assert.Equal(t, 10, a.CrashAfterMS)
	assert.True(t, a.NotRunning)
	b := f.For("b")
	assert.Equal(t, "b", b.Launcher)
	assert.Zero(t, b.CrashAfterMS)

	assert.True(t, f.TakeLaunchFailure("a"), "falls back to the global counter")
	assert.False(t, f.TakeLaunchFailure("b"))
	assert.True(t, f.TakeStopFailure("a"))
	assert.True(t, f.TakeStopFailure("a"))
	assert.False(t, f.TakeStopFailure("a"))

	require.Len(t, f.List(), 2)
	f.Clear("a")
	require.Len(t, f.List(), 1)
	f.Clear("")
	assert.Empty(t, f.List())
}

func TestJournalSinceSubscribeAndFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j, err := NewJournal(path)
	require.NoError(t, err)
	ch, cancel := j.Subscribe()
	j.Record(&Event{Kind: EventLaunch, Launcher: "x"})
	j.Record(&Event{Kind: EventStop})
	j.Record(&Event{Kind: EventLaunch, Launcher: "y"})
	assert.Equal(t, int64(3), j.Last())
	assert.Len(t, j.Since(0), 3)
	got := j.Since(1, EventLaunch)
	require.Len(t, got, 1)
	assert.Equal(t, "y", got[0].Launcher)
	assert.Equal(t, EventLaunch, (<-ch).Kind)
	cancel()
	cancel()
	require.NoError(t, j.Close())
	data, err := os.ReadFile(path) //nolint:gosec // test path
	require.NoError(t, err)
	assert.Len(t, splitLines(string(data)), 3)
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return out
}

func TestControlReadersAndFaults(t *testing.T) {
	t.Parallel()
	p := newTestPlatform(t, &Options{})
	c := NewHandlerClient(p.ControlHandler())
	ctx := context.Background()

	st, err := c.AddReader(ctx, AddReaderRequest{Name: "r1", NotRemovable: true})
	require.NoError(t, err)
	assert.Equal(t, "r1", st.Name)
	_, err = c.AddReader(ctx, AddReaderRequest{Name: "r1"})
	require.Error(t, err)
	list, err := c.Readers(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)

	_, err = c.Tap(ctx, "r1", simreader.Tag{Text: "x"}, 0)
	require.ErrorContains(t, err, "not connected", "no Core reader has opened the device")
	require.Error(t, c.Remove(ctx, "missing"))
	require.NoError(t, c.Unplug(ctx, "r1"))
	require.NoError(t, c.Plug(ctx, "r1"))
	require.NoError(t, c.FailNextWrite(ctx, "r1", simreader.WriteFailError))
	require.NoError(t, c.RemoveReader(ctx, "r1"))

	require.NoError(t, c.SetFault(ctx, Fault{Launcher: "sim-tracked", FailLaunches: 1}))
	faults, err := c.Faults(ctx)
	require.NoError(t, err)
	require.Len(t, faults, 1)
	ev, err := c.Journal(ctx, 0, EventFault)
	require.NoError(t, err)
	require.Len(t, ev, 1)
	require.NoError(t, c.ClearFault(ctx, ""))
	faults, err = c.Faults(ctx)
	require.NoError(t, err)
	assert.Empty(t, faults)

	status, err := c.Status(ctx)
	require.NoError(t, err)
	assert.Equal(t, p.Layout().Root, status.Root)
}

func TestControlFollowAndWait(t *testing.T) {
	t.Parallel()
	p := newTestPlatform(t, &Options{})
	c := NewHandlerClient(p.ControlHandler())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		time.Sleep(20 * time.Millisecond)
		p.Journal().Record(&Event{Kind: EventMenu})
		p.Journal().Record(&Event{Kind: EventLaunch, Launcher: "want"})
	}()
	ev, err := c.WaitEvent(ctx, p.Journal().Last(), func(e Event) bool { return e.Launcher == "want" })
	require.NoError(t, err)
	assert.Equal(t, EventLaunch, ev.Kind)

	launches, err := c.Launches(ctx, 0)
	require.NoError(t, err)
	for _, e := range launches {
		assert.NotEqual(t, EventMenu, e.Kind, "launches leaves out other kinds")
	}
}

func TestControlSocketAndMounts(t *testing.T) {
	t.Parallel()
	root, err := os.MkdirTemp("", "zsim")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	l, err := NewLayout(root)
	require.NoError(t, err)
	stopped := make(chan struct{})
	p := newTestPlatform(t, &Options{
		Layout: l,
		Socket: true,
		OnStop: func() { close(stopped) },
		Mounts: map[string]http.Handler{"/fakes/x/": http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(r.URL.Path)) //nolint:gosec // test handler echoes the path to check prefix stripping
		})},
	})
	require.NoError(t, p.StartPost(context.Background(), nil, nil, nil, nil, nil, nil))

	info, err := os.Stat(l.Socket)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	c := NewClient(l.Socket)
	ctx := context.Background()
	body, err := c.Call(ctx, http.MethodGet, "/fakes/x/hello", nil)
	require.NoError(t, err)
	assert.Equal(t, "/hello", string(body))

	_, err = startControl(p, l.Socket)
	require.ErrorContains(t, err, "already serving")

	require.NoError(t, c.Stop(ctx))
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("OnStop not called")
	}
	require.NoError(t, p.Stop())
	_, err = os.Stat(l.Socket)
	assert.ErrorIs(t, err, os.ErrNotExist, "the socket is removed on stop")
}

func TestMatchParams(t *testing.T) {
	t.Parallel()
	params := json.RawMessage(`{"systemId":"snes","count":3,"media":{"name":"Game","ok":true}}`)
	assert.True(t, MatchParams(params, nil))
	assert.True(t, MatchParams(params, []string{"systemId=snes"}))
	assert.True(t, MatchParams(params, []string{"count=3", "media.name=Game", "media.ok=true"}))
	assert.False(t, MatchParams(params, []string{"systemId=nes"}))
	assert.False(t, MatchParams(params, []string{"media.missing=x"}))
	assert.False(t, MatchParams(params, []string{"systemId.deep=x"}))
	assert.False(t, MatchParams(json.RawMessage("not json"), []string{"a=b"}))
}

const spawnHelperEnv = "ZAPAROO_SIM_SPAWN_HELPER_ROOT"

// TestSpawnHelperProcess is not a test: TestChildDiesWithSim runs the test
// binary as this helper, which starts a child, prints its PID and waits to be
// killed.
func TestSpawnHelperProcess(t *testing.T) {
	root := os.Getenv(spawnHelperEnv)
	if root == "" {
		t.Skip("helper process for TestChildDiesWithSim")
	}
	l, err := NewLayout(root)
	require.NoError(t, err)
	p, err := New(&Options{Layout: l})
	require.NoError(t, err)
	proc, err := p.spawn("helper", "", 0, false, nil)
	require.NoError(t, err)
	_, _ = fmt.Fprintf(os.Stdout, "child=%d\n", proc.Pid)
	select {}
}

// A sim that dies without Stop, such as one killed by a test timeout or
// SIGKILL, must not leave its emulator stand-ins running.
func TestChildDiesWithSim(t *testing.T) {
	t.Parallel()
	//nolint:gosec // re-runs this test binary as the helper
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSpawnHelperProcess$", "-test.count=1")
	cmd.Env = append(os.Environ(), spawnHelperEnv+"="+filepath.Join(t.TempDir(), "root"))
	out, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	pid := 0
	scanner := bufio.NewScanner(out)
	for pid == 0 && scanner.Scan() {
		if v, ok := strings.CutPrefix(scanner.Text(), "child="); ok {
			pid, err = strconv.Atoi(v)
			require.NoError(t, err)
		}
	}
	require.NotZero(t, pid, "helper never reported its child")
	require.NoError(t, syscall.Kill(pid, 0), "child is running before the sim dies")

	require.NoError(t, cmd.Process.Kill())
	_ = cmd.Wait()
	assert.Eventually(t, func() bool {
		return errors.Is(syscall.Kill(pid, 0), syscall.ESRCH)
	}, 5*time.Second, 10*time.Millisecond, "child outlived the sim")
}

func TestSpawnRefusedAfterStop(t *testing.T) {
	t.Parallel()
	p := newTestPlatform(t, &Options{})
	require.NoError(t, p.Stop())
	_, err := p.spawn("late", "", 0, false, nil)
	require.ErrorIs(t, err, errStopped)
	p.childMu.Lock()
	defer p.childMu.Unlock()
	assert.Empty(t, p.children)
}
