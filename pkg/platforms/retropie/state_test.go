//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package retropie

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeRequest(t *testing.T) {
	t.Parallel()

	data, err := encodeRequest(requestSystem, "nes", "/roms/nes/It's a game; $(id).nes")
	require.NoError(t, err)
	assert.Equal(t, "sys\x00nes\x00/roms/nes/It's a game; $(id).nes\x00", string(data),
		"shell syntax is carried as data; the loop never evaluates it")

	for name, tc := range map[string]struct {
		kind   requestKind
		system string
		rom    string
	}{
		"bad kind":           {kind: "exec", system: "nes", rom: "/r.nes"},
		"system traversal":   {kind: requestSystem, system: "../nes", rom: "/r.nes"},
		"system upper":       {kind: requestSystem, system: "NES", rom: "/r.nes"},
		"system empty":       {kind: requestSystem, system: "", rom: "/r.nes"},
		"relative rom":       {kind: requestSystem, system: "nes", rom: "r.nes"},
		"newline in rom":     {kind: requestSystem, system: "nes", rom: "/r\n.nes"},
		"nul in rom":         {kind: requestSystem, system: "nes", rom: "/r\x00.nes"},
		"double quote":       {kind: requestSystem, system: "nes", rom: `/a"b.nes`},
		"backslash":          {kind: requestSystem, system: "nes", rom: `/a\b.nes`},
		"backtick":           {kind: requestSystem, system: "nes", rom: "/a`b`.nes"},
		"delete control rom": {kind: requestSystem, system: "nes", rom: "/a\x7f.nes"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := encodeRequest(tc.kind, tc.system, tc.rom)
			require.Error(t, err)
		})
	}
}

func TestWriteRequestReplacesAtomically(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, writeRequest(fs, testStateDir, []byte("a")))
	require.NoError(t, writeRequest(fs, testStateDir, []byte("b")))
	assert.Equal(t, "b", readTestFile(t, fs, filepath.Join(testStateDir, requestFile)))
	entries, err := afero.ReadDir(fs, testStateDir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary files are left behind")
}

func TestParseState(t *testing.T) {
	t.Parallel()

	ev, err := parseState([]byte("start\x00700\x00nes\x00lr-fceumm\x00/roms/nes/a.nes\x00"))
	require.NoError(t, err)
	assert.Equal(t, runEvent{
		event: eventStart, pid: 700, system: "nes", emulator: "lr-fceumm", rom: "/roms/nes/a.nes",
	}, ev)

	ev, err = parseState([]byte("end\x00700\x00\x00\x00\x00"))
	require.NoError(t, err)
	assert.Equal(t, eventEnd, ev.event)

	for name, data := range map[string]string{
		"empty":        "",
		"truncated":    "start\x00700\x00nes",
		"few fields":   "start\x00700\x00nes\x00",
		"many fields":  "start\x00700\x00nes\x00e\x00r\x00x\x00",
		"bad event":    "launch\x00700\x00nes\x00e\x00r\x00",
		"bad pid":      "start\x00abc\x00nes\x00e\x00r\x00",
		"zero pid":     "start\x000\x00nes\x00e\x00r\x00",
		"negative pid": "start\x00-1\x00nes\x00e\x00r\x00",
		"too large":    "start\x00700\x00nes\x00e\x00" + strings.Repeat("a", maxStateSize) + "\x00",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := parseState([]byte(data))
			require.Error(t, err)
		})
	}
}

func TestApplyEventPublishesGame(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	tp.awaitingStart = true
	tp.procs.add(runcommandPID, loopPID, "runcommand.sh", "/bin/bash runcommand.sh 0 _SYS_ nes x")

	tp.applyEvent(runEvent{event: eventStart, pid: runcommandPID, system: "nes", emulator: "lr-fceumm", rom: testROM})

	assert.False(t, tp.awaitingStart)
	assert.Equal(t, runcommandPID, tp.run.pid)
	media := tp.media.get()
	require.NotNil(t, media)
	assert.Equal(t, systemdefs.SystemNES, media.SystemID)
	assert.Equal(t, "Super Game (USA)", media.Name)
	assert.Equal(t, testROM, media.Path)
	assert.Equal(t, "NES", media.LauncherID)

	tp.applyEvent(runEvent{event: eventEnd, pid: 12345, system: "nes", rom: testROM})
	assert.NotNil(t, tp.media.get(), "the end of another runcommand is ignored")
	assert.Equal(t, runcommandPID, tp.run.pid)

	tp.applyEvent(runEvent{event: eventEnd, pid: runcommandPID, system: "nes", rom: testROM})
	assert.Nil(t, tp.media.get())
	assert.Zero(t, tp.run.pid)
}

func TestApplyEventKeepsCoreMedia(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	tp.procs.add(runcommandPID, loopPID, "runcommand.sh", "/bin/bash runcommand.sh")
	indexed := models.NewActiveMedia(systemdefs.SystemNES, "Nintendo", testROM, "Indexed Name", "NES")
	tp.media.set(indexed)

	tp.applyEvent(runEvent{event: eventStart, pid: runcommandPID, system: "nes", rom: testROM})

	assert.Same(t, indexed, tp.media.get(), "a launch from Core keeps its indexed name")
	assert.Equal(t, runcommandPID, tp.run.pid)
}

func TestApplyEventIgnoresStaleStart(t *testing.T) {
	t.Parallel()

	t.Run("process gone", func(t *testing.T) {
		t.Parallel()
		tp := newTestPlatform(t)
		tp.applyEvent(runEvent{event: eventStart, pid: runcommandPID, system: "nes", rom: testROM})
		assert.Zero(t, tp.run.pid)
		assert.Empty(t, tp.media.sets())
	})

	t.Run("pid reused", func(t *testing.T) {
		t.Parallel()
		tp := newTestPlatform(t)
		tp.procs.add(runcommandPID, 1, "sshd", "sshd: pi")
		tp.applyEvent(runEvent{event: eventStart, pid: runcommandPID, system: "nes", rom: testROM})
		assert.Zero(t, tp.run.pid)
		assert.Empty(t, tp.media.sets())
	})
}

func TestApplyEventPortIsTrackedWithoutMedia(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	tp.procs.add(runcommandPID, loopPID, "runcommand.sh", "/bin/bash runcommand.sh 0 _PORT_ doom x")
	tp.applyEvent(runEvent{event: eventStart, pid: runcommandPID, system: "doom", rom: "/x.wad"})
	assert.Equal(t, runcommandPID, tp.run.pid, "a running port can still be stopped")
	assert.Empty(t, tp.media.sets())
}

func TestStateWatcherFollowsHooks(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	stateDir := t.TempDir()
	tp.fs = afero.NewOsFs()
	tp.Platform.fs = tp.fs
	tp.stateDir = stateDir
	tp.procs.add(runcommandPID, loopPID, "runcommand.sh", "/bin/bash runcommand.sh")

	writeState := func(data string) {
		tmp := filepath.Join(stateDir, stateFile+".tmp")
		require.NoError(t, os.WriteFile(tmp, []byte(data), 0o600))
		require.NoError(t, os.Rename(tmp, filepath.Join(stateDir, stateFile)))
	}
	// A start left from before Core started is picked up at once.
	writeState("start\x00700\x00nes\x00lr-fceumm\x00" + testROM + "\x00")

	stop, err := tp.startStateWatcher()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, stop()) })
	require.NotNil(t, tp.media.get())

	writeState("end\x00700\x00nes\x00lr-fceumm\x00" + testROM + "\x00")
	require.Eventually(t, func() bool { return tp.media.get() == nil }, 5*time.Second, 10*time.Millisecond)
}

func TestReadLoopPID(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)

	_, ok := readLoopPID(tp.fs, tp.procs, testStateDir)
	assert.False(t, ok, "no pid file")

	tp.startLoop(t)
	pid, ok := readLoopPID(tp.fs, tp.procs, testStateDir)
	assert.True(t, ok)
	assert.Equal(t, loopPID, pid)

	writeTestFile(t, tp.fs, filepath.Join(testStateDir, loopPIDFile), "garbage", 0o644)
	_, ok = readLoopPID(tp.fs, tp.procs, testStateDir)
	assert.False(t, ok)
}
