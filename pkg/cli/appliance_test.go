//go:build linux

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

package cli

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/rivo/tview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const applianceDataDir = "/storage/zaparoo-data"

func appliancePlatform(t *testing.T) *mocks.MockPlatform {
	t.Helper()
	pl := mocks.NewMockPlatform()
	pl.On("Settings").Return(platforms.Settings{
		DataDir:          applianceDataDir,
		HostManagedPaths: true,
	})
	return pl
}

// failingDeps fails any boundary a test did not expect to be crossed.
func failingDeps(t *testing.T) applianceDeps {
	t.Helper()
	return applianceDeps{
		startService: func(platforms.Platform, *config.Instance) (*service.StartResult, error) {
			t.Error("unexpected service start")
			return nil, errors.New("unexpected")
		},
		execAfterRollback: func(error) error {
			t.Error("unexpected rollback exec")
			return nil
		},
		execIfRequested: func(func() bool) error {
			t.Error("unexpected restart exec")
			return nil
		},
		spawnDaemon: func(*config.Instance) (func(), error) {
			t.Error("unexpected daemon spawn")
			return func() {}, nil
		},
		showTUI: func(*config.Instance, platforms.Platform, func() (*tview.Application, error)) error {
			t.Error("unexpected TUI")
			return nil
		},
		buildTUI: func(*config.Instance, platforms.Platform, func() bool, string, string) (*tview.Application, error) {
			t.Error("unexpected TUI build")
			return tview.NewApplication(), nil
		},
		isServiceRunning: func(*config.Instance) bool {
			t.Error("unexpected service probe")
			return false
		},
	}
}

type fakeStartedService struct {
	result  *service.StartResult
	done    chan struct{}
	stopErr error
	stops   int
}

func newFakeStartedService(restartRequested bool) *fakeStartedService {
	f := &fakeStartedService{done: make(chan struct{})}
	f.result = &service.StartResult{
		Done:             f.done,
		RestartRequested: func() bool { return restartRequested },
		Stop: func() error {
			f.stops++
			return f.stopErr
		},
	}
	return f
}

func startWith(svc *fakeStartedService) func(platforms.Platform, *config.Instance) (*service.StartResult, error) {
	return func(platforms.Platform, *config.Instance) (*service.StartResult, error) {
		return svc.result, nil
	}
}

func TestRunApplianceDaemonStopsServiceOnSignal(t *testing.T) {
	t.Parallel()

	svc := newFakeStartedService(false)
	deps := failingDeps(t)
	deps.startService = startWith(svc)
	sigs := make(chan os.Signal, 1)
	sigs <- syscall.SIGTERM

	require.NoError(t, runApplianceDaemonWith(deps, appliancePlatform(t), &config.Instance{}, sigs))
	assert.Equal(t, 1, svc.stops)
}

func TestRunApplianceDaemonReportsStopFailure(t *testing.T) {
	t.Parallel()

	svc := newFakeStartedService(false)
	svc.stopErr = errors.New("stop failed")
	deps := failingDeps(t)
	deps.startService = startWith(svc)
	sigs := make(chan os.Signal, 1)
	sigs <- syscall.SIGINT

	err := runApplianceDaemonWith(deps, appliancePlatform(t), &config.Instance{}, sigs)
	require.ErrorIs(t, err, svc.stopErr)
	assert.Contains(t, err.Error(), "error stopping service")
}

func TestRunApplianceDaemonReExecsAfterInternalShutdown(t *testing.T) {
	t.Parallel()

	svc := newFakeStartedService(true)
	close(svc.done)
	deps := failingDeps(t)
	deps.startService = startWith(svc)
	var asked []bool
	deps.execIfRequested = func(requested func() bool) error {
		asked = append(asked, requested())
		return nil
	}

	require.NoError(t, runApplianceDaemonWith(deps, appliancePlatform(t), &config.Instance{}, nil))
	assert.Equal(t, []bool{true}, asked, "the service's restart request must reach the re-exec")
	assert.Zero(t, svc.stops, "a service that shut itself down is not stopped again")
}

func TestRunApplianceDaemonReportsReExecFailure(t *testing.T) {
	t.Parallel()

	svc := newFakeStartedService(true)
	close(svc.done)
	deps := failingDeps(t)
	deps.startService = startWith(svc)
	execErr := errors.New("exec failed")
	deps.execIfRequested = func(func() bool) error { return execErr }

	err := runApplianceDaemonWith(deps, appliancePlatform(t), &config.Instance{}, nil)
	require.ErrorIs(t, err, execErr)
	assert.Contains(t, err.Error(), "failed to re-exec for restart")
}

func TestRunApplianceDaemonReExecsAfterRollback(t *testing.T) {
	t.Parallel()

	startErr := errors.Join(updater.ErrRolledBack, errors.New("new build failed health check"))
	execErr := errors.New("exec failed")
	deps := failingDeps(t)
	deps.startService = func(platforms.Platform, *config.Instance) (*service.StartResult, error) {
		return nil, startErr
	}
	var got error
	deps.execAfterRollback = func(err error) error {
		got = err
		return execErr
	}

	err := runApplianceDaemonWith(deps, appliancePlatform(t), &config.Instance{}, nil)
	require.ErrorIs(t, err, execErr)
	assert.Contains(t, err.Error(), "restarting after update rollback")
	assert.Equal(t, startErr, got, "the rollback error must reach the re-exec")
}

func TestRunApplianceDaemonReportsStartFailure(t *testing.T) {
	t.Parallel()

	startErr := errors.New("database locked")
	deps := failingDeps(t)
	deps.startService = func(platforms.Platform, *config.Instance) (*service.StartResult, error) {
		return nil, startErr
	}

	err := runApplianceDaemonWith(deps, appliancePlatform(t), &config.Instance{}, nil)
	require.ErrorIs(t, err, startErr)
	assert.Contains(t, err.Error(), "error starting service")
}

type tuiCall struct {
	logPath   string
	label     string
	isRunning bool
}

// recordTUI runs the builder the way tui.BuildAndRetry does and records what
// the TUI was built with.
func recordTUI(deps *applianceDeps, showErr error) *[]tuiCall {
	calls := &[]tuiCall{}
	deps.showTUI = func(_ *config.Instance, _ platforms.Platform, build func() (*tview.Application, error)) error {
		_, err := build()
		if err != nil {
			return err
		}
		return showErr
	}
	deps.buildTUI = func(
		_ *config.Instance, _ platforms.Platform, isRunning func() bool, logPath, label string,
	) (*tview.Application, error) {
		*calls = append(*calls, tuiCall{logPath: logPath, label: label, isRunning: isRunning()})
		return tview.NewApplication(), nil
	}
	return calls
}

func TestRunApplianceTUIShowsTUIAgainstSpawnedDaemon(t *testing.T) {
	t.Parallel()

	deps := failingDeps(t)
	stopped := 0
	deps.spawnDaemon = func(*config.Instance) (func(), error) {
		return func() { stopped++ }, nil
	}
	deps.isServiceRunning = func(*config.Instance) bool { return true }
	calls := recordTUI(&deps, nil)

	require.NoError(t, runApplianceTUIWith(deps, appliancePlatform(t), &config.Instance{}))
	assert.Equal(t, []tuiCall{{
		logPath:   filepath.Join(applianceDataDir, config.LogFile),
		label:     "storage",
		isRunning: true,
	}}, *calls)
	assert.Equal(t, 1, stopped, "the spawned daemon must be released when the TUI exits")
}

func TestRunApplianceTUIReportsTUIFailure(t *testing.T) {
	t.Parallel()

	deps := failingDeps(t)
	stopped := 0
	deps.spawnDaemon = func(*config.Instance) (func(), error) {
		return func() { stopped++ }, nil
	}
	deps.isServiceRunning = func(*config.Instance) bool { return false }
	showErr := errors.New("no terminal")
	calls := recordTUI(&deps, showErr)

	err := runApplianceTUIWith(deps, appliancePlatform(t), &config.Instance{})
	require.ErrorIs(t, err, showErr)
	assert.Contains(t, err.Error(), "error displaying TUI")
	assert.Equal(t, 1, stopped)
	require.Len(t, *calls, 1)
	assert.False(t, (*calls)[0].isRunning, "the TUI must see the daemon's real state")
}

func TestRunApplianceTUIReportsSpawnFailure(t *testing.T) {
	t.Parallel()

	deps := failingDeps(t)
	spawnErr := errors.New("spawn failed")
	deps.spawnDaemon = func(*config.Instance) (func(), error) { return nil, spawnErr }

	err := runApplianceTUIWith(deps, appliancePlatform(t), &config.Instance{})
	require.ErrorIs(t, err, spawnErr)
	assert.Contains(t, err.Error(), "error spawning daemon")
}

type fakeBackgroundService struct {
	runningErr  error
	autoErr     error
	startErr    error
	startCalls  int
	autoRunning bool
	running     bool
}

func (f *fakeBackgroundService) RunningForAutoStart() (bool, error) {
	return f.autoRunning, f.autoErr
}

func (f *fakeBackgroundService) Start() error {
	f.startCalls++
	return f.startErr
}

func (f *fakeBackgroundService) Running() (bool, error) {
	return f.running, f.runningErr
}

func TestRunBackgroundServiceTUI(t *testing.T) {
	t.Parallel()

	startErr := errors.New("start failed")
	showErr := errors.New("no terminal")

	tests := []struct {
		name       string
		showErr    error
		wantErr    error
		svc        fakeBackgroundService
		wantStarts int
		wantUp     bool
	}{
		{
			name:   "already running is not started again",
			svc:    fakeBackgroundService{autoRunning: true, running: true},
			wantUp: true,
		},
		{
			name:       "stopped service is started",
			svc:        fakeBackgroundService{running: true},
			wantStarts: 1,
			wantUp:     true,
		},
		{
			name:       "no terminal leaves the started service running",
			svc:        fakeBackgroundService{running: true},
			showErr:    showErr,
			wantStarts: 1,
			wantUp:     true,
		},
		{
			name:       "failed start still shows the TUI",
			svc:        fakeBackgroundService{startErr: startErr},
			wantStarts: 1,
		},
		{
			name:       "failed start without a terminal is an error",
			svc:        fakeBackgroundService{startErr: startErr},
			showErr:    showErr,
			wantErr:    startErr,
			wantStarts: 1,
		},
		{
			name:       "PID file conflict reads as not running",
			svc:        fakeBackgroundService{running: true, runningErr: errors.New("pid conflict")},
			wantStarts: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := tt.svc
			deps := failingDeps(t)
			calls := recordTUI(&deps, tt.showErr)

			err := runBackgroundServiceTUIWith(deps, appliancePlatform(t), &config.Instance{}, &svc)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Contains(t, err.Error(), "could not start service")
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantStarts, svc.startCalls)
			assert.Equal(t, []tuiCall{{
				logPath:   filepath.Join(applianceDataDir, config.LogFile),
				label:     "storage",
				isRunning: tt.wantUp,
			}}, *calls)
		})
	}
}

func TestRunBackgroundServiceTUIReportsStatusFailure(t *testing.T) {
	t.Parallel()

	statusErr := errors.New("status failed")
	svc := &fakeBackgroundService{autoErr: statusErr}

	err := runBackgroundServiceTUIWith(failingDeps(t), appliancePlatform(t), &config.Instance{}, svc)
	require.ErrorIs(t, err, statusErr)
	assert.Contains(t, err.Error(), "error checking service status")
	assert.Zero(t, svc.startCalls)
}

func TestDefaultApplianceDepsAreWired(t *testing.T) {
	t.Parallel()

	deps := defaultApplianceDeps()
	assert.NotNil(t, deps.startService)
	assert.NotNil(t, deps.execAfterRollback)
	assert.NotNil(t, deps.execIfRequested)
	assert.NotNil(t, deps.spawnDaemon)
	assert.NotNil(t, deps.showTUI)
	assert.NotNil(t, deps.buildTUI)
	assert.NotNil(t, deps.isServiceRunning)
}

//nolint:paralleltest // sends a real signal to the test process
func TestNotifyShutdownDeliversSIGTERM(t *testing.T) {
	sigs, stop := notifyShutdown()
	defer stop()

	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGTERM))
	select {
	case sig := <-sigs:
		assert.Equal(t, syscall.SIGTERM, sig)
	case <-time.After(5 * time.Second):
		t.Fatal("SIGTERM was not delivered")
	}
}
