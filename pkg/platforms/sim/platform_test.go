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

package sim_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/hoststatus"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/sim"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/harness"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func kinds(events []sim.Event) []string {
	out := make([]string, 0, len(events))
	for i := range events {
		out = append(out, events[i].Kind)
	}
	return out
}

// TestPlatformRecordsHostActions covers the platform methods that stand in for
// the host: input, screenshots, device status and power. Each only records.
func TestPlatformRecordsHostActions(t *testing.T) {
	in := harness.New(t, &harness.Options{Readers: []string{"sim0"}})
	p := in.Platform
	since := p.Journal().Last()

	require.NoError(t, p.KeyboardPress("{enter}"))
	require.NoError(t, p.GamepadPress("a"))
	require.NoError(t, p.LaunchSystem(nil, "SNES"))
	require.NoError(t, p.LaunchSystem(nil, platforms.SystemMenu))
	require.NoError(t, p.RefreshLauncherDependencies())

	shot, err := p.Screenshot()
	require.NoError(t, err)
	assert.NotEmpty(t, shot.Data)
	assert.FileExists(t, shot.Path)

	sys, err := p.DeviceSystem()
	require.NoError(t, err)
	assert.Equal(t, sim.Hostname, sys.Hostname)
	pwr, err := p.PowerStatus()
	require.NoError(t, err)
	assert.Equal(t, power.SourceExternal, pwr.Source)
	vols, err := p.DeviceStorage()
	require.NoError(t, err)
	require.Len(t, vols, 1)
	assert.Equal(t, in.Root, vols[0].Path)

	actions := p.PowerActions(context.Background())
	require.NotEmpty(t, actions)
	for action, avail := range actions {
		assert.Equal(t, hoststatus.Supported, avail, action)
	}
	commit, err := p.PreparePowerAction(context.Background(), hoststatus.PowerActions[0])
	require.NoError(t, err)
	require.NoError(t, commit())

	got := kinds(p.Journal().Since(since))
	for _, want := range []string{
		sim.EventKeyboard, sim.EventGamepad, sim.EventLaunchSystem, sim.EventMenu,
		sim.EventRefresh, sim.EventScreenshot, sim.EventPower,
	} {
		assert.Contains(t, got, want)
	}
}

// TestControlReaderActions drives reader errors and card writes through the
// control client, and reads the journal as a stream.
func TestControlReaderActions(t *testing.T) {
	in := harness.New(t, &harness.Options{Readers: []string{"sim0"}})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, in.Control.ReaderError(ctx, "sim0"))
	require.NoError(t, in.Control.ScanError(ctx, "sim0", "bad read"))
	require.NoError(t, in.WaitReaders(ctx, "sim0"))

	api := in.API
	_, err := api.Call(ctx, "version", nil)
	require.NoError(t, err)

	written := make(chan error, 1)
	go func() {
		_, callErr := api.Call(ctx, "readers.write", map[string]string{"text": "**launch.random:snes"})
		written <- callErr
	}()
	var tag struct{ UID string }
	require.Eventually(t, func() bool {
		got, writeErr := in.Control.CompleteWrite(ctx, "sim0", "04cc33")
		tag.UID = got.UID
		return writeErr == nil
	}, 20*time.Second, 50*time.Millisecond)
	assert.Equal(t, "04cc33", tag.UID)
	require.NoError(t, <-written)

	// Follow streams through the control socket until told to stop.
	err = in.Control.Follow(ctx, 0, nil, func(sim.Event) error { return sim.ErrStopFollowing })
	require.True(t, err == nil || errors.Is(err, sim.ErrStopFollowing), "follow: %v", err)
}

// TestBehaviourLauncherHooks calls the behaviour launchers' hooks directly, with
// and without their faults.
func TestBehaviourLauncherHooks(t *testing.T) {
	in := harness.New(t, &harness.Options{Readers: []string{"sim0"}})
	p, cfg := in.Platform, in.Config
	byID := map[string]platforms.Launcher{}
	for _, l := range p.Launchers(cfg) {
		byID[l.ID] = l
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	media := &models.ActiveMedia{Path: "/sim/ready.bin", LauncherID: sim.LauncherReady}

	ready := byID[sim.LauncherReady]
	require.NoError(t, ready.WaitForReady(ctx, cfg, media))
	running, err := ready.GameRunning(ctx, cfg, media)
	require.NoError(t, err)
	assert.True(t, running)
	p.Faults().Set(sim.Fault{Launcher: sim.LauncherReady, ReadyTimeout: true, NotRunning: true})
	short, cancelShort := context.WithTimeout(ctx, 20*time.Millisecond)
	require.ErrorIs(t, ready.WaitForReady(short, cfg, media), context.DeadlineExceeded)
	cancelShort()
	running, err = ready.GameRunning(ctx, cfg, media)
	require.NoError(t, err)
	assert.False(t, running)

	require.Error(t, byID[sim.LauncherUnavailable].Availability(cfg))
	require.Error(t, byID[sim.LauncherPreflight].Preflight(cfg, "/sim/p.bin", nil))

	details := byID[sim.LauncherDetails]
	proc, err := details.Launch(cfg, "/sim/details.bin", &platforms.LaunchOptions{Action: zapscript.ActionDetails})
	require.NoError(t, err)
	assert.Nil(t, proc)

	proc, err = byID[sim.LauncherNoActive].Launch(cfg, "/sim/noactive.bin", nil)
	require.NoError(t, err)
	assert.Nil(t, proc)

	kill := byID[sim.LauncherKill]
	proc, err = kill.Launch(cfg, "/sim/kill.bin", nil)
	require.NoError(t, err)
	require.NotNil(t, proc)
	require.NoError(t, kill.Kill(cfg))
	p.Faults().Set(sim.Fault{Launcher: sim.LauncherKill, FailKill: true})
	require.Error(t, kill.Kill(cfg))
	p.Faults().Clear(sim.LauncherKill)

	// An external child that exits by itself clears the media it published.
	since := p.Journal().Last()
	p.Faults().Set(sim.Fault{Launcher: sim.LauncherExternal, ExitAfterMS: 600})
	_, err = byID[sim.LauncherExternal].Launch(cfg, "/sim/external.bin", nil)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		for _, e := range p.Journal().Since(since, sim.EventExternal) {
			if e.Detail == "cleared" {
				return true
			}
		}
		return false
	}, 10*time.Second, 50*time.Millisecond)
}
