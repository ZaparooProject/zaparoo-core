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

package updater

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func statusFn(status string) func() (string, error) {
	return func() (string, error) { return status, nil }
}

func alwaysTrue() bool { return true }

// externalPower is what most devices report, and what the tests that are not
// about power want out of the way.
func externalPower() power.Status {
	return power.Status{Source: power.SourceExternal}
}

// TestCanApplyUpdateSignals walks the whole gate table: what blocks a person
// pressing update, what blocks the device deciding for itself, and which of
// those a person may go ahead through anyway.
func TestCanApplyUpdateSignals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mutate        func(*GateDeps)
		name          string
		wantReason    string
		wantForceable bool
		wantExpires   bool
		blocksManual  bool
		blocksAuto    bool
	}{
		{
			name:         "nothing happening",
			mutate:       func(*GateDeps) {},
			blocksManual: false,
			blocksAuto:   false,
		},
		{
			name: "media indexing",
			mutate: func(d *GateDeps) {
				d.IndexingStatus = statusFn(mediadb.IndexingStatusRunning)
			},
			blocksManual: true, blocksAuto: true,
			wantReason: ReasonMediaIndexing,
		},
		{
			name: "media indexing queued",
			mutate: func(d *GateDeps) {
				d.IndexingStatus = statusFn(mediadb.IndexingStatusPending)
			},
			blocksManual: true, blocksAuto: true,
			wantReason: ReasonMediaIndexing,
		},
		{
			name: "database optimization",
			mutate: func(d *GateDeps) {
				d.OptimizationStatus = statusFn(mediadb.IndexingStatusRunning)
			},
			blocksManual: true, blocksAuto: true,
			wantReason: ReasonMediaOptimizing,
		},
		{
			name: "metadata scraping",
			mutate: func(d *GateDeps) {
				d.ScrapingStatus = statusFn(mediadb.IndexingStatusRunning)
			},
			blocksManual: true, blocksAuto: true,
			wantReason: ReasonMediaScraping,
		},
		{
			name:         "backup running",
			mutate:       func(d *GateDeps) { d.BackupActive = alwaysTrue },
			blocksManual: true, blocksAuto: true,
			wantReason: ReasonBackupActive,
		},
		{
			name:         "token being written",
			mutate:       func(d *GateDeps) { d.ReaderWriteActive = alwaysTrue },
			blocksManual: true, blocksAuto: true,
			wantReason: ReasonReaderWriting,
		},
		{
			name:         "media playing",
			mutate:       func(d *GateDeps) { d.ActiveMedia = alwaysTrue },
			blocksManual: true, blocksAuto: true,
			wantReason: ReasonActiveMedia, wantForceable: true, wantExpires: true,
		},
		{
			name:         "media playing in the background",
			mutate:       func(d *GateDeps) { d.BackgroundMedia = alwaysTrue },
			blocksManual: true, blocksAuto: true,
			wantReason: ReasonBackgroundMedia, wantForceable: true, wantExpires: true,
		},
		{
			name:         "playlist running",
			mutate:       func(d *GateDeps) { d.ActivePlaylist = alwaysTrue },
			blocksManual: true, blocksAuto: true,
			wantReason: ReasonActivePlaylist, wantForceable: true, wantExpires: true,
		},
		{
			name: "api still busy",
			mutate: func(d *GateDeps) {
				d.WaitForIdle = func(context.Context) error { return errors.New("still busy") }
			},
			blocksManual: false, blocksAuto: true,
			wantReason: ReasonAPIBusy, wantExpires: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, mode := range []Mode{ModeManual, ModeAuto} {
				deps := &GateDeps{Power: externalPower}
				tt.mutate(deps)

				decision, err := CanApplyUpdate(t.Context(), deps, mode, false)
				require.NoError(t, err)
				decision.Release()

				wantBlocked := tt.blocksManual
				if mode == ModeAuto {
					wantBlocked = tt.blocksAuto
				}
				if !wantBlocked {
					assert.True(t, decision.OK, "%s should not block %s", tt.name, mode)
					continue
				}
				require.False(t, decision.OK, "%s should block %s", tt.name, mode)
				assert.Equal(t, tt.wantReason, decision.Reason)
				assert.NotEmpty(t, decision.Message)
				assert.Equal(t, tt.wantExpires, decision.Expires)
				if mode == ModeAuto {
					assert.False(t, decision.Forceable, "an automatic install never forces")
					continue
				}
				assert.Equal(t, tt.wantForceable, decision.Forceable)
			}
		})
	}
}

// Force is a person accepting that their session ends. It is not a way past
// anything that would cost them data.
func TestCanApplyUpdateForce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mutate    func(*GateDeps)
		name      string
		wantForce bool
	}{
		{name: "media playing", mutate: func(d *GateDeps) { d.ActiveMedia = alwaysTrue }, wantForce: true},
		{name: "background media", mutate: func(d *GateDeps) { d.BackgroundMedia = alwaysTrue }, wantForce: true},
		{name: "playlist running", mutate: func(d *GateDeps) { d.ActivePlaylist = alwaysTrue }, wantForce: true},
		{
			name:   "media indexing",
			mutate: func(d *GateDeps) { d.IndexingStatus = statusFn(mediadb.IndexingStatusRunning) },
		},
		{name: "backup running", mutate: func(d *GateDeps) { d.BackupActive = alwaysTrue }},
		{name: "token being written", mutate: func(d *GateDeps) { d.ReaderWriteActive = alwaysTrue }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			deps := &GateDeps{Power: externalPower}
			tt.mutate(deps)

			decision, err := CanApplyUpdate(t.Context(), deps, ModeManual, true)
			require.NoError(t, err)
			decision.Release()
			assert.Equal(t, tt.wantForce, decision.OK)
		})
	}
}

// An automatic install ignores force outright, so a caller that passes it by
// mistake cannot restart a device mid-game with nobody watching.
func TestCanApplyUpdateAutoIgnoresForce(t *testing.T) {
	t.Parallel()

	deps := &GateDeps{Power: externalPower, ActiveMedia: alwaysTrue}
	decision, err := CanApplyUpdate(t.Context(), deps, ModeAuto, true)
	require.NoError(t, err)
	decision.Release()

	require.False(t, decision.OK)
	assert.Equal(t, ReasonActiveMedia, decision.Reason)
	assert.False(t, decision.Forceable)
}

// A cabinet that plays something every waking hour would defer forever, so the
// soft signals stop counting once a version has waited long enough. The hard
// ones never do.
func TestCanApplyUpdateSoftSignalDeadline(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		since   time.Time
		mutate  func(*GateDeps)
		name    string
		wantErr string
		wantOK  bool
	}{
		{
			name:   "not deferred yet",
			since:  time.Time{},
			mutate: func(d *GateDeps) { d.ActiveMedia = alwaysTrue },
		},
		{
			name:   "deferred, still inside the deadline",
			since:  now.Add(-autoInstallDeadline + time.Minute),
			mutate: func(d *GateDeps) { d.ActiveMedia = alwaysTrue },
		},
		{
			name:   "deferred past the deadline",
			since:  now.Add(-autoInstallDeadline - time.Minute),
			mutate: func(d *GateDeps) { d.ActiveMedia = alwaysTrue },
			wantOK: true,
		},
		{
			name:   "a busy api also gives up waiting",
			since:  now.Add(-autoInstallDeadline - time.Minute),
			mutate: func(d *GateDeps) { d.WaitForIdle = func(context.Context) error { return errors.New("busy") } },
			wantOK: true,
		},
		{
			name:  "indexing never expires",
			since: now.Add(-100 * autoInstallDeadline),
			mutate: func(d *GateDeps) {
				d.IndexingStatus = statusFn(mediadb.IndexingStatusRunning)
			},
			wantErr: ReasonMediaIndexing,
		},
		{
			name:    "a flat battery never expires",
			since:   now.Add(-100 * autoInstallDeadline),
			mutate:  func(d *GateDeps) { d.Power = func() power.Status { return battery(5) } },
			wantErr: ReasonPowerLow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			since := tt.since
			deps := &GateDeps{
				Power:         externalPower,
				Now:           func() time.Time { return now },
				DeferredSince: func() time.Time { return since },
			}
			tt.mutate(deps)

			decision, err := CanApplyUpdate(t.Context(), deps, ModeAuto, false)
			require.NoError(t, err)
			decision.Release()
			assert.Equal(t, tt.wantOK, decision.OK)
			if tt.wantErr != "" {
				assert.Equal(t, tt.wantErr, decision.Reason)
			}
		})
	}
}

func battery(percent int) power.Status {
	return power.Status{Source: power.SourceBattery, Percent: percent}
}

// The power policy is the one part of the gate that is different for the two
// modes, because nobody is there to plug in a device that installs on its own.
func TestCanApplyUpdatePower(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		wantReason  string
		status      power.Status
		manualOK    bool
		autoOK      bool
		forcedOK    bool
		forceableOn bool
	}{
		{
			name:     "no battery",
			status:   power.Status{Source: power.SourceNoBattery},
			manualOK: true, autoOK: true, forcedOK: true,
		},
		{
			name:     "on a charger",
			status:   power.Status{Source: power.SourceExternal},
			manualOK: true, autoOK: true, forcedOK: true,
		},
		{
			name:     "full battery",
			status:   battery(95),
			manualOK: true, autoOK: true, forcedOK: true,
		},
		{
			name:     "half battery clears both floors",
			status:   battery(40),
			manualOK: true, autoOK: true, forcedOK: true,
		},
		{
			name:     "just under the automatic floor",
			status:   battery(39),
			manualOK: true, autoOK: false, forcedOK: true,
			wantReason: ReasonPowerLow,
		},
		{
			name:     "at the manual floor",
			status:   battery(20),
			manualOK: true, autoOK: false, forcedOK: true,
			wantReason: ReasonPowerLow,
		},
		{
			name:     "under the manual floor",
			status:   battery(19),
			manualOK: false, autoOK: false, forcedOK: false,
			wantReason: ReasonPowerLow,
		},
		{
			name:     "battery level cannot be read",
			status:   power.Status{Source: power.SourceUnknown},
			manualOK: false, autoOK: false, forcedOK: true,
			wantReason: ReasonPowerUnknown, forceableOn: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status := tt.status
			newDeps := func() *GateDeps {
				return &GateDeps{Power: func() power.Status { return status }}
			}

			manual, err := CanApplyUpdate(t.Context(), newDeps(), ModeManual, false)
			require.NoError(t, err)
			manual.Release()
			assert.Equal(t, tt.manualOK, manual.OK, "manual")

			auto, err := CanApplyUpdate(t.Context(), newDeps(), ModeAuto, false)
			require.NoError(t, err)
			auto.Release()
			assert.Equal(t, tt.autoOK, auto.OK, "auto")

			forced, err := CanApplyUpdate(t.Context(), newDeps(), ModeManual, true)
			require.NoError(t, err)
			forced.Release()
			assert.Equal(t, tt.forcedOK, forced.OK, "forced")

			if !tt.manualOK {
				assert.Equal(t, tt.wantReason, manual.Reason)
				assert.Equal(t, tt.forceableOn, manual.Forceable)
				assert.False(t, manual.Expires, "power never waits out the deadline")
			}
		})
	}
}

// A device with no way to read its power is not a device on a flat battery, so
// a platform that reports nothing is left alone.
func TestCanApplyUpdateWithoutPowerReading(t *testing.T) {
	t.Parallel()

	decision, err := CanApplyUpdate(t.Context(), &GateDeps{}, ModeAuto, false)
	require.NoError(t, err)
	decision.Release()
	assert.True(t, decision.OK)
}

// The restore gate is held, not polled, so nothing can start a restore between
// the check and the install.
func TestCanApplyUpdateHoldsRestoreGate(t *testing.T) {
	t.Parallel()

	released := false
	deps := &GateDeps{
		Power: externalPower,
		AcquireRestore: func() (func(), error) {
			return func() { released = true }, nil
		},
	}

	decision, err := CanApplyUpdate(t.Context(), deps, ModeManual, false)
	require.NoError(t, err)
	require.True(t, decision.OK)
	assert.False(t, released, "the gate must stay held until the caller lets go")
	decision.Release()
	assert.True(t, released)
}

func TestCanApplyUpdateRestoreInProgress(t *testing.T) {
	t.Parallel()

	deps := &GateDeps{
		Power:          externalPower,
		AcquireRestore: func() (func(), error) { return nil, errors.New("restore in progress") },
	}

	decision, err := CanApplyUpdate(t.Context(), deps, ModeManual, false)
	require.NoError(t, err)
	decision.Release()
	require.False(t, decision.OK)
	assert.Equal(t, ReasonRestoreActive, decision.Reason)
	assert.False(t, decision.Forceable)
}

// The two gates are taken in the order the rest of the service takes them, and
// held together until the caller lets go.
func TestCanApplyUpdateHoldsBothGatesInOrder(t *testing.T) {
	t.Parallel()

	var order []string
	deps := &GateDeps{
		Power: externalPower,
		AcquireRestore: func() (func(), error) {
			order = append(order, "restore")
			return func() { order = append(order, "release restore") }, nil
		},
		AcquireMediaGate: func(context.Context) (func(), error) {
			order = append(order, "media")
			return func() { order = append(order, "release media") }, nil
		},
	}

	decision, err := CanApplyUpdate(t.Context(), deps, ModeManual, false)
	require.NoError(t, err)
	require.True(t, decision.OK)
	assert.Equal(t, []string{"restore", "media"}, order)
	decision.Release()
	assert.Equal(t, []string{"restore", "media", "release media", "release restore"}, order)
}

// The media gate has to be held before the gate reads what is playing, or a
// launch that starts in between is one the install would kill.
func TestCanApplyUpdateReadsMediaBehindTheGate(t *testing.T) {
	t.Parallel()

	gateHeld := false
	playing := false
	deps := &GateDeps{
		Power: externalPower,
		AcquireMediaGate: func(context.Context) (func(), error) {
			gateHeld = true
			// Whatever was launching settles while the gate is being taken.
			playing = true
			return func() { gateHeld = false }, nil
		},
		ActiveMedia: func() bool { return playing },
	}

	decision, err := CanApplyUpdate(t.Context(), deps, ModeManual, false)
	require.NoError(t, err)
	require.False(t, decision.OK)
	assert.Equal(t, ReasonActiveMedia, decision.Reason)
	assert.False(t, gateHeld, "a refused update must not keep launches blocked")
}

// A cancelled request is not an answer, so it comes back as an error rather
// than as a reason a client would show someone.
func TestCanApplyUpdateMediaGateCancelled(t *testing.T) {
	t.Parallel()

	restoreReleased := false
	deps := &GateDeps{
		Power: externalPower,
		AcquireRestore: func() (func(), error) {
			return func() { restoreReleased = true }, nil
		},
		AcquireMediaGate: func(context.Context) (func(), error) {
			return nil, context.Canceled
		},
	}

	decision, err := CanApplyUpdate(t.Context(), deps, ModeManual, false)
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, decision.OK)
	assert.Empty(t, decision.Reason)
	assert.True(t, restoreReleased)
}

// A database that cannot answer is a problem to notice elsewhere. Refusing
// every update over it would leave the device with no way to be fixed.
func TestCanApplyUpdateStatusError(t *testing.T) {
	t.Parallel()

	deps := &GateDeps{
		Power: externalPower,
		IndexingStatus: func() (string, error) {
			return "", errors.New("database is closed")
		},
	}

	decision, err := CanApplyUpdate(t.Context(), deps, ModeManual, false)
	require.NoError(t, err)
	decision.Release()
	assert.True(t, decision.OK)
}

func TestPowerReady(t *testing.T) {
	t.Parallel()

	flat := &GateDeps{Power: func() power.Status { return battery(5) }}
	blocked := PowerReady(flat, ModeManual, true)
	require.False(t, blocked.OK)

	var gateErr *GateError
	require.ErrorAs(t, blocked.Err(), &gateErr)
	assert.Equal(t, ReasonPowerLow, gateErr.Reason)
	assert.Contains(t, gateErr.Error(), "5%")

	charged := &GateDeps{Power: externalPower}
	ready := PowerReady(charged, ModeAuto, false)
	assert.True(t, ready.OK)
	require.NoError(t, ready.Err())
}

// PowerReady is the second check, run once the download is done. Everything
// else has already been decided by then and must not be asked again: a game
// launched during the download does not undo an install that is nearly
// finished.
func TestPowerReadyIgnoresOtherSignals(t *testing.T) {
	t.Parallel()

	deps := &GateDeps{
		Power:          externalPower,
		ActiveMedia:    alwaysTrue,
		IndexingStatus: statusFn(mediadb.IndexingStatusRunning),
	}

	assert.True(t, PowerReady(deps, ModeManual, false).OK)
}
