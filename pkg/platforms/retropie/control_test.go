//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package retropie

import (
	"context"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	esPID         = 600
	runcommandPID = 700
	emulatorPID   = 701
)

var testROM = filepath.Join(testRomsDir, "nes", "Super Game (USA).nes")

func (tp *testPlatform) prepareLaunch(t *testing.T) {
	t.Helper()
	tp.startLoop(t)
	writeTestFile(t, tp.fs, filepath.Join(testConfigsDir, "nes", "emulators.cfg"), "default = \"lr-fceumm\"\n", 0o644)
	writeTestFile(t, tp.fs, testROM, "rom", 0o644)
	tp.procs.add(esPID, loopPID, esComm, "/opt/retropie/supplementary/emulationstation/emulationstation")
}

func (tp *testPlatform) requestExists(t *testing.T) bool {
	t.Helper()
	ok, err := afero.Exists(tp.fs, filepath.Join(testStateDir, requestFile))
	require.NoError(t, err)
	return ok
}

// takeRequest stands in for the autostart loop picking up the request once
// EmulationStation has gone.
func (tp *testPlatform) takeRequest(t *testing.T) {
	t.Helper()
	data, err := afero.ReadFile(tp.fs, filepath.Join(testStateDir, requestFile))
	require.NoError(t, err)
	want, err := encodeRequest(requestSystem, "nes", testROM)
	require.NoError(t, err)
	assert.Equal(t, want, data)
	require.NoError(t, tp.fs.Remove(filepath.Join(testStateDir, requestFile)))
}

func TestLaunchClosesEmulationStation(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	tp.prepareLaunch(t)
	writeTestFile(t, tp.fs, filepath.Join(testStateDir, holdFile), "", 0o600)
	tp.procs.onSignal = func(f *fakeProcs, pid int, sig syscall.Signal) {
		if pid == esPID && sig == syscall.SIGTERM {
			f.exit(esPID)
			tp.takeRequest(t)
		}
	}
	autoAdvance(t, tp.clock, pollInterval)

	require.NoError(t, tp.launch(context.Background(), "nes", requestSystem, testROM))

	assert.Equal(t, []sentSignal{{pid: esPID, sig: syscall.SIGTERM}}, tp.procs.sent())
	assert.True(t, tp.awaitingStart, "launch waits for runcommand to report the game")
	held, err := afero.Exists(tp.fs, filepath.Join(testStateDir, holdFile))
	require.NoError(t, err)
	assert.False(t, held, "a request clears the hold")
}

func TestLaunchKillsStuckEmulationStation(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	tp.prepareLaunch(t)
	tp.procs.onSignal = func(f *fakeProcs, pid int, sig syscall.Signal) {
		if pid == esPID && sig == syscall.SIGKILL {
			f.exit(esPID)
			tp.takeRequest(t)
		}
	}
	autoAdvance(t, tp.clock, pollInterval)
	start := tp.clock.Now()

	require.NoError(t, tp.launch(context.Background(), "nes", requestSystem, testROM))

	assert.Equal(t, []sentSignal{
		{pid: esPID, sig: syscall.SIGTERM},
		{pid: esPID, sig: syscall.SIGKILL},
	}, tp.procs.sent())
	assert.GreaterOrEqual(t, tp.clock.Since(start), esTermGrace)
}

func TestLaunchWithdrawsUntakenRequest(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	tp.prepareLaunch(t)
	tp.procs.onSignal = func(f *fakeProcs, pid int, _ syscall.Signal) { f.exit(pid) }
	autoAdvance(t, tp.clock, pollInterval)

	err := tp.launch(context.Background(), "nes", requestSystem, testROM)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not take the launch request")
	assert.False(t, tp.requestExists(t), "a request the loop never took is withdrawn")
	assert.False(t, tp.awaitingStart)
}

func TestLaunchRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setup   func(t *testing.T, tp *testPlatform)
		name    string
		system  string
		path    string
		wantErr string
		kind    requestKind
	}{
		{
			name: "loop not running", system: "nes", kind: requestSystem, path: testROM,
			setup: func(t *testing.T, tp *testPlatform) {
				t.Helper()
				tp.procs.mu.Lock()
				tp.procs.exit(loopPID)
				tp.procs.mu.Unlock()
			},
			wantErr: "autostart loop is not running",
		},
		{
			name: "stale loop pid", system: "nes", kind: requestSystem, path: testROM,
			setup: func(t *testing.T, tp *testPlatform) {
				t.Helper()
				tp.procs.mu.Lock()
				tp.procs.cmdlines[loopPID] = "/usr/bin/something-else"
				tp.procs.mu.Unlock()
			},
			wantErr: "autostart loop is not running",
		},
		{
			name: "no emulator installed", system: "nes", kind: requestSystem, path: testROM,
			setup: func(t *testing.T, tp *testPlatform) {
				t.Helper()
				require.NoError(t, tp.fs.Remove(filepath.Join(testConfigsDir, "nes", "emulators.cfg")))
			},
			wantErr: "no emulator is installed",
		},
		{
			name: "missing rom", system: "nes", kind: requestSystem,
			path: filepath.Join(testRomsDir, "nes", "gone.nes"), wantErr: "media not found",
		},
		{
			name: "quote in path", system: "nes", kind: requestSystem,
			path: filepath.Join(testRomsDir, "nes", `a"b.nes`), wantErr: "quote",
		},
		{
			name: "backtick in path", system: "nes", kind: requestSystem,
			path: filepath.Join(testRomsDir, "nes", "a`id`.nes"), wantErr: "backtick",
		},
		{
			name: "directory instead of a file", system: "nes", kind: requestSystem,
			path: filepath.Join(testRomsDir, "nes", "folder.nes"),
			setup: func(t *testing.T, tp *testPlatform) {
				t.Helper()
				require.NoError(t, tp.fs.MkdirAll(filepath.Join(testRomsDir, "nes", "folder.nes"), 0o755))
			},
			wantErr: "not a regular file",
		},
		{
			name: "port outside the ports folder", system: "ports", kind: requestPort,
			path: testROM, wantErr: "RetroPie ports must be scripts",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tp := newTestPlatform(t)
			tp.prepareLaunch(t)
			if tt.setup != nil {
				tt.setup(t, tp)
			}
			err := tp.launch(context.Background(), tt.system, tt.kind, tt.path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.False(t, tp.requestExists(t))
			assert.Empty(t, tp.procs.sent(), "nothing is closed for a refused launch")
		})
	}
}

func TestLaunchPort(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	tp.prepareLaunch(t)
	port := filepath.Join(testRomsDir, "ports", "Doom.sh")
	writeTestFile(t, tp.fs, port, "#!/bin/bash\n", 0o755)
	tp.procs.onSignal = func(f *fakeProcs, pid int, _ syscall.Signal) {
		f.exit(pid)
		data, err := afero.ReadFile(tp.fs, filepath.Join(testStateDir, requestFile))
		require.NoError(t, err)
		assert.Equal(t, "port\x00ports\x00"+port+"\x00", string(data))
		require.NoError(t, tp.fs.Remove(filepath.Join(testStateDir, requestFile)))
	}
	autoAdvance(t, tp.clock, pollInterval)

	require.NoError(t, tp.launch(context.Background(), "ports", requestPort, port))
	assert.False(t, tp.awaitingStart, "ports are not tracked")
}

func (tp *testPlatform) startGame(emulator string) {
	tp.procs.add(runcommandPID, loopPID, "runcommand.sh",
		"/bin/bash /opt/retropie/supplementary/runcommand/runcommand.sh 0 _SYS_ nes "+testROM)
	tp.procs.add(emulatorPID, runcommandPID, "retroarch", "/opt/retropie/emulators/retroarch/bin/retroarch")
	tp.run = runEvent{event: eventStart, pid: runcommandPID, system: "nes", emulator: emulator, rom: testROM}
}

func enableNetworkCommands(t *testing.T, tp *testPlatform) {
	t.Helper()
	writeTestFile(t, tp.fs, filepath.Join(testConfigsDir, "all", "retroarch.cfg"),
		"# network_cmd_enable = \"false\"\nnetwork_cmd_enable = \"true\"\nnetwork_cmd_port = \"55400\"\n", 0o644)
}

func TestStopQuitsRetroArch(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	tp.startGame("lr-fceumm")
	enableNetworkCommands(t, tp)
	tp.sendQuit = func(context.Context, string) error {
		tp.procs.mu.Lock()
		defer tp.procs.mu.Unlock()
		tp.procs.exit(emulatorPID)
		tp.procs.exit(runcommandPID)
		return nil
	}
	autoAdvance(t, tp.clock, pollInterval)

	require.NoError(t, tp.StopActiveLauncher(platforms.StopForPreemption))

	assert.Empty(t, tp.procs.sent(), "a clean RetroArch quit needs no signals")
	assert.Zero(t, tp.run.pid)
	held, err := afero.Exists(tp.fs, filepath.Join(testStateDir, holdFile))
	require.NoError(t, err)
	assert.True(t, held, "a preemption holds the loop for the next request")
	sets := tp.media.sets()
	require.NotEmpty(t, sets)
	assert.Nil(t, sets[len(sets)-1])
}

func TestStopSendsQuitToConfiguredPort(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	tp.startGame("lr-fceumm")
	enableNetworkCommands(t, tp)
	writeTestFile(t, tp.fs, filepath.Join(testConfigsDir, "nes", "retroarch.cfg"),
		"#include \"/opt/retropie/configs/all/retroarch.cfg\"\nnetwork_cmd_port = 55401\n", 0o644)
	tp.procs.onSignal = func(f *fakeProcs, pid int, _ syscall.Signal) {
		f.exit(pid)
		f.exit(runcommandPID)
	}
	autoAdvance(t, tp.clock, pollInterval)

	require.NoError(t, tp.StopActiveLauncher(platforms.StopForMenu))

	assert.Equal(t, []string{"127.0.0.1:55401"}, tp.quitAddrs())
	assert.Equal(t, []sentSignal{{pid: emulatorPID, sig: syscall.SIGTERM}}, tp.procs.sent(),
		"a quit that does not end the game falls back to signals")
	held, err := afero.Exists(tp.fs, filepath.Join(testStateDir, holdFile))
	require.NoError(t, err)
	assert.False(t, held, "returning to the menu lets EmulationStation start")
}

func TestStopSignalsEmulatorWithoutNetworkCommands(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	tp.startGame("lr-fceumm")
	tp.procs.onSignal = func(f *fakeProcs, pid int, sig syscall.Signal) {
		if pid == emulatorPID && sig == syscall.SIGTERM {
			f.exit(emulatorPID)
			f.exit(runcommandPID)
		}
	}
	autoAdvance(t, tp.clock, pollInterval)

	require.NoError(t, tp.StopActiveLauncher(platforms.StopForMenu))

	assert.Empty(t, tp.quitAddrs())
	assert.Equal(t, []sentSignal{{pid: emulatorPID, sig: syscall.SIGTERM}}, tp.procs.sent())
}

func TestStopKillsStuckGame(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	tp.startGame("mupen64plus")
	tp.procs.onSignal = func(f *fakeProcs, pid int, sig syscall.Signal) {
		if sig == syscall.SIGKILL {
			f.exit(pid)
		}
	}
	autoAdvance(t, tp.clock, pollInterval)

	require.NoError(t, tp.StopActiveLauncher(platforms.StopForMenu))

	assert.Empty(t, tp.quitAddrs(), "only libretro emulators take RetroArch commands")
	assert.Equal(t, []sentSignal{
		{pid: emulatorPID, sig: syscall.SIGTERM},
		{pid: emulatorPID, sig: syscall.SIGKILL},
		{pid: runcommandPID, sig: syscall.SIGKILL},
	}, tp.procs.sent())
	assert.Zero(t, tp.run.pid)
}

func TestStopReportsGameThatWillNotDie(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	tp.startGame("mupen64plus")
	autoAdvance(t, tp.clock, pollInterval)

	err := tp.StopActiveLauncher(platforms.StopForMenu)
	require.ErrorIs(t, err, platforms.ErrStopFailed)
	assert.Equal(t, runcommandPID, tp.run.pid, "a game still running stays tracked")
}

func TestStopWithNoGame(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	require.NoError(t, tp.StopActiveLauncher(platforms.StopForPreemption))
	assert.Empty(t, tp.procs.sent())

	// A game that already ended without its end hook is forgotten.
	tp.run = runEvent{event: eventStart, pid: 999, system: "nes", rom: testROM}
	require.NoError(t, tp.StopActiveLauncher(platforms.StopForPreemption))
	assert.Zero(t, tp.run.pid)
	assert.Empty(t, tp.procs.sent())
}

func TestNetworkCmdPort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		all      string
		system   string
		wantPort int
		wantOK   bool
	}{
		{name: "missing config"},
		{name: "disabled", all: "network_cmd_enable = \"false\"\n"},
		{name: "default port", all: "network_cmd_enable = \"true\"\n", wantPort: 55355, wantOK: true},
		{name: "unquoted", all: "network_cmd_enable=true\nnetwork_cmd_port=5000\n", wantPort: 5000, wantOK: true},
		{name: "last line wins", all: "network_cmd_enable = \"true\"\nnetwork_cmd_enable = \"false\"\n"},
		{
			name: "system disables", all: "network_cmd_enable = \"true\"\n",
			system: "network_cmd_enable = \"false\"\n",
		},
		{
			name: "system enables", all: "network_cmd_enable = \"false\"\n",
			system: "network_cmd_enable = \"true\"\n", wantPort: 55355, wantOK: true,
		},
		{name: "bad port", all: "network_cmd_enable = \"true\"\nnetwork_cmd_port = \"x\"\n"},
		{name: "port out of range", all: "network_cmd_enable = \"true\"\nnetwork_cmd_port = \"70000\"\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			if tt.all != "" {
				writeTestFile(t, fs, filepath.Join(testConfigsDir, "all", "retroarch.cfg"), tt.all, 0o644)
			}
			if tt.system != "" {
				writeTestFile(t, fs, filepath.Join(testConfigsDir, "nes", "retroarch.cfg"), tt.system, 0o644)
			}
			port, ok := networkCmdPort(fs, testConfigsDir, "nes")
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantPort, port)
		})
	}
}

func TestWaitForGame(t *testing.T) {
	t.Parallel()

	t.Run("ready once runcommand reports", func(t *testing.T) {
		t.Parallel()
		tp := newTestPlatform(t)
		tp.awaitingStart = true
		tp.procs.add(runcommandPID, loopPID, "runcommand.sh", "bash runcommand.sh 0 _SYS_ nes x")
		autoAdvance(t, tp.clock, readyInterval)

		done := make(chan error, 1)
		go func() { done <- tp.waitForGame(context.Background(), nil, nil) }()
		tp.applyEvent(runEvent{event: eventStart, pid: runcommandPID, system: "nes", rom: testROM})
		require.NoError(t, <-done)

		running, err := tp.gameRunning(context.Background(), nil, nil)
		require.NoError(t, err)
		assert.True(t, running)
	})

	t.Run("gives up when the wait ends", func(t *testing.T) {
		t.Parallel()
		tp := newTestPlatform(t)
		tp.awaitingStart = true
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		require.Error(t, tp.waitForGame(ctx, nil, nil))
		assert.False(t, tp.awaitingStart)

		running, err := tp.gameRunning(context.Background(), nil, nil)
		require.NoError(t, err)
		assert.False(t, running)
	})
}

func TestStopIgnoresReusedRuncommandPID(t *testing.T) {
	t.Parallel()
	tp := newTestPlatform(t)
	tp.run = runEvent{event: eventStart, pid: runcommandPID, system: "nes", emulator: "lr-fceumm", rom: testROM}
	tp.procs.add(runcommandPID, 1, "sshd", "sshd: pi")
	tp.procs.add(emulatorPID, runcommandPID, "bash", "-bash")

	running, err := tp.gameRunning(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.False(t, running, "a reused pid is not the game")

	require.NoError(t, tp.StopActiveLauncher(platforms.StopForMenu))
	assert.Empty(t, tp.procs.sent(), "a reused pid and its children are never signalled")
	assert.Zero(t, tp.run.pid)
}
