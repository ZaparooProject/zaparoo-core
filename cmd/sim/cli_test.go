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
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/sim"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// execEnv makes the test binary act as zaparoo-sim, so a detached serve can
// re-run it.
const execEnv = "ZAPAROO_SIM_TEST_EXEC"

func TestMain(m *testing.M) {
	if os.Getenv(execEnv) == "1" {
		main()
		return
	}
	m.Run()
}

type cli struct {
	t    *testing.T
	root string
}

// run runs one zaparoo-sim command against the root and returns its stdout.
func (c cli) run(args ...string) (string, error) {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var out bytes.Buffer
	err := run(ctx, append([]string{"--root", c.root}, args...), &out, io.Discard)
	return out.String(), err
}

func (c cli) ok(args ...string) string {
	c.t.Helper()
	out, err := c.run(args...)
	require.NoError(c.t, err, "zaparoo-sim %v", args)
	return out
}

func (c cli) fails(contains string, args ...string) {
	c.t.Helper()
	_, err := c.run(args...)
	require.Error(c.t, err, "zaparoo-sim %v", args)
	assert.Contains(c.t, err.Error(), contains)
}

func waitReady(t *testing.T, root string, done <-chan error) *sim.State {
	t.Helper()
	layout, err := sim.NewLayout(root)
	require.NoError(t, err)
	deadline := time.After(time.Minute)
	for {
		select {
		case err := <-done:
			t.Fatalf("serve exited before ready: %v", err)
		case <-deadline:
			t.Fatal("sim not ready after a minute")
		case <-time.After(50 * time.Millisecond):
		}
		if st, err := layout.ReadState(); err == nil && st.Ready {
			return st
		}
	}
}

// startForeground runs serve --foreground in this process until the test ends.
func startForeground(t *testing.T, root string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		serveArgs := append([]string{"--root", root, "serve", "--foreground"}, args...)
		done <- run(ctx, serveArgs, io.Discard, io.Discard)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			assert.NoError(t, err)
		case <-time.After(30 * time.Second):
			t.Error("serve did not stop")
		}
	})
	waitReady(t, root, done)
}

func TestCLIErrors(t *testing.T) {
	t.Setenv("ZAPAROO_SIM_ROOT", "")
	ctx := context.Background()
	var out bytes.Buffer

	require.ErrorIs(t, run(ctx, nil, &out, io.Discard), errUsage)
	require.ErrorIs(t, run(ctx, []string{"--bogus"}, &out, io.Discard), errUsage)
	require.ErrorIs(t, run(ctx, []string{"bogus"}, &out, io.Discard), errUsage)
	require.ErrorContains(t, run(ctx, []string{"--root"}, &out, io.Discard), "--root needs")
	require.ErrorContains(t, run(ctx, []string{"status"}, &out, io.Discard), "no sim root")

	require.NoError(t, run(ctx, []string{"--help"}, &out, io.Discard))
	require.NoError(t, run(ctx, []string{"--json", "help"}, &out, io.Discard))
	assert.Contains(t, out.String(), "Usage: zaparoo-sim")

	// Nothing serves from an empty root.
	c := cli{t: t, root: t.TempDir()}
	for _, cmd := range [][]string{
		{"status"},
		{"readers"},
		{"fault"},
		{"api", "version"},
		{"stop"},
		{"tap", "x"},
		{"remove"},
		{"scan-error"},
		{"write-complete"},
		{"write-fail"},
		{"launches"},
		{"wait", "media.started"},
		{"fixtures", "index"},
		{"fake", "online"},
	} {
		_, err := c.run(cmd...)
		require.Error(t, err, "%v", cmd)
	}
	err := run(ctx, []string{"--root=" + c.root, "status"}, &out, io.Discard)
	require.Error(t, err)

	c.fails("serve", "serve", "--bogus")
	c.fails("pass --systems or --all", "fixtures", "gen")
	c.fails("subcommand required", "fixtures")
	c.fails("unknown subcommand", "fixtures", "bogus")
	c.fails("give the tag text", "tap")
	c.fails("name required", "wait")
	c.fails("name required", "fake")
	c.fails("unknown fake", "fake", "bogus")
	c.fails("usage", "api")
	c.fails("flag provided but not defined", "status", "--bogus")

	// With no sim running, bigdb stages straight into the root.
	out.Reset()
	bigdb := []string{"--root", c.root, "--json", "fixtures", "bigdb", "--entries", "50"}
	require.NoError(t, run(ctx, bigdb, &out, io.Discard))
	assert.Contains(t, out.String(), "50")
}

func TestCLIForeground(t *testing.T) {
	c := cli{t: t, root: t.TempDir()}
	c.ok("fixtures", "gen", "--systems", "snes,nes", "--per-system", "5")
	assert.Contains(t, c.ok("--json", "fixtures", "gen", "--systems", "snes", "--per-system", "5"), `"files"`)
	startForeground(t, c.root, "--readers", "sim0", "--fakes", "libretro,romm,logs,online")
	// Ready means the API answers; Core opens the reader shortly after.
	require.Eventually(t, func() bool {
		out, err := c.run("readers")
		return err == nil && strings.Contains(out, "sim0 (") && strings.Contains(out, "): open")
	}, 30*time.Second, 50*time.Millisecond)

	c.fails("already running", "serve", "--foreground")
	c.fails("stop the sim first", "fixtures", "bigdb")

	assert.Contains(t, c.ok("status"), "reader sim0")
	var st struct {
		Instance sim.State `json:"instance"`
	}
	require.NoError(t, json.Unmarshal([]byte(c.ok("status", "--json")), &st))
	assert.Equal(t, os.Getpid(), st.Instance.PID)
	assert.NotEqual(t, 7497, st.Instance.APIPort)

	// Readers.
	assert.Contains(t, c.ok("readers", "add", "sim1", "--no-write", "--no-removable"), "added reader sim1")
	c.ok("readers", "add", "--write-wait-ms", "100", "sim2")
	assert.Contains(t, c.ok("readers", "ls"), "reader sim1")
	assert.Contains(t, c.ok("--json", "readers"), `"sim2"`)
	c.ok("readers", "rm", "sim1")
	c.ok("--json", "readers", "remove", "sim2")
	c.fails("name required", "readers", "add")
	c.fails("name required", "readers", "rm")
	c.fails("unknown subcommand", "readers", "bogus")

	// Library and API.
	// Indexing starts a scrape, which refuses a second index until it ends,
	// so index once.
	assert.Contains(t, c.ok("--json", "fixtures", "index", "--systems", "SNES,NES"), "totalMedia")
	assert.Contains(t, c.ok("api", "version"), "version")
	assert.Contains(t, c.ok("api", "media.search", `{"query":"","maxResults":1}`), "results")
	c.fails("not valid JSON", "api", "version", "{")
	c.fails("usage", "api", "a", "b", "c")
	c.fails("media.bogus", "api", "media.bogus")

	// Wait for a launch, tapping until the waiter has subscribed.
	waited := make(chan string, 1)
	go func() {
		out, err := c.run("wait", "media.started", "--match", "systemId=SNES", "--timeout", "30s")
		if err != nil {
			out = "error: " + err.Error()
		}
		waited <- out
	}()
	var got string
	for got == "" {
		c.ok("tap", "--hold-ms", "50", "**launch.random:snes")
		select {
		case got = <-waited:
		case <-time.After(time.Second):
		}
	}
	assert.Contains(t, got, "media.started")
	c.fails("want KEY=VALUE", "wait", "media.started", "--match", "nope")
	c.fails("deadline exceeded", "wait", "media.stopped", "--timeout", "10ms", "--match", "systemId=NONE")

	assert.Contains(t, c.ok("launches"), "launch")
	assert.Contains(t, c.ok("--json", "launches", "--since", "0"), `"kind"`)
	assert.Contains(t, c.ok("journal", "--kind", "launch"), "launcher=")
	assert.Contains(t, c.ok("journal", "--json"), `"seq"`)
	c.ok("journal", "--follow", "--timeout", "200ms")
	c.ok("--json", "launches", "--follow", "--timeout", "200ms")
	assert.Contains(t, c.ok("status"), "active:")

	// Tag and reader actions.
	assert.Contains(t, c.ok("hold", "--uid", "04aa", "--type", "ntag215", "**launch.random:nes"), "left on reader")
	assert.Contains(t, c.ok("readers"), "tag 04aa")
	c.ok("--json", "remove")
	c.ok("tap", "--reader", "sim0", "--keep", "--data", "0102")
	c.ok("remove", "--reader", "sim0")
	c.ok("reader-error")
	c.ok("scan-error", "--message", "bad read")
	c.ok("unplug")
	assert.Contains(t, c.ok("readers"), "unplugged")
	c.ok("plug")

	// Card writes: complete one, then fail the next.
	writeDone := make(chan error, 1)
	go func() {
		_, err := c.run("api", "readers.write", `{"text":"**launch.random:snes"}`)
		writeDone <- err
	}()
	require.Eventually(t, func() bool {
		_, err := c.run("write-complete", "--uid", "04cc33")
		return err == nil
	}, 20*time.Second, 50*time.Millisecond)
	require.NoError(t, <-writeDone)
	c.ok("remove")
	c.fails("complete write", "write-complete")
	assert.Contains(t, c.ok("write-fail", "--mode", "removed"), "fails: removed")
	assert.Contains(t, c.ok("status"), "next write fails")

	// Faults.
	assert.Contains(t, c.ok("fault"), "no faults")
	assert.Contains(t, c.ok("--json", "fault", "ls"), "[]")
	c.ok("fault", "set", "--launcher", "*", "--fail-launches", "1", "--launch-delay-ms", "1")
	assert.Contains(t, c.ok("fault", "list"), "failLaunches")
	assert.Contains(t, c.ok("status"), "fault ")
	c.ok("fault", "clear")
	c.fails("unknown subcommand", "fault", "bogus")
	c.fails("flag provided", "fault", "set", "--bogus")

	// Fakes.
	c.ok("fake", "libretro", "thumbnails")
	c.ok("fake", "logs", "uploads")
	c.ok("fake", "online", "account")
	c.fails("", "fake", "online", "bogus")
}

func TestCLIDetached(t *testing.T) {
	t.Setenv(execEnv, "1")
	c := cli{t: t, root: t.TempDir()}
	t.Cleanup(func() {
		layout, err := sim.NewLayout(c.root)
		if err != nil {
			return
		}
		if st, err := layout.ReadState(); err == nil && processAlive(st.PID) {
			_ = syscall.Kill(st.PID, syscall.SIGKILL)
		}
	})

	assert.Contains(t, c.ok("serve", "--readers", "sim0", "--fakes", "online"), "fake online:")
	c.fails("already running", "serve")
	assert.Contains(t, c.ok("status"), "fake online:")
	assert.Contains(t, c.ok("stop"), "sim stopped")
	c.fails("", "status")

	var st sim.State
	require.NoError(t, json.Unmarshal([]byte(c.ok("--json", "serve")), &st))
	assert.True(t, st.Ready)
	c.ok("--json", "stop")
}
