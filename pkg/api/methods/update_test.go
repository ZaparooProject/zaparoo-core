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

package methods

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/permissions"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	backupcoordinator "github.com/ZaparooProject/zaparoo-core/v2/pkg/service/backup/coordinator"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleUpdateCheck_DevelopmentVersion(t *testing.T) {
	devVersions := []string{"DEVELOPMENT", "abc1234-dev"}

	for _, v := range devVersions {
		t.Run(v, func(t *testing.T) {
			original := config.AppVersion
			config.AppVersion = v
			t.Cleanup(func() { config.AppVersion = original })

			mockPlatform := mocks.NewMockPlatform()
			mockPlatform.SetupBasicMock()

			env := requests.RequestEnv{
				Context:  t.Context(),
				Platform: mockPlatform,
				Config:   &config.Instance{},
				IsLocal:  true,
			}

			result, err := HandleUpdateCheck(env, updater.Check)
			require.NoError(t, err)

			resp, ok := result.(models.UpdateCheckResponse)
			require.True(t, ok)
			assert.Equal(t, v, resp.CurrentVersion)
			assert.False(t, resp.UpdateAvailable)
			assert.Empty(t, resp.LatestVersion)
		})
	}
}

func TestHandleUpdateCheck_UpdateAvailable(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()

	env := requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
		Config:   &config.Instance{},
		IsLocal:  true,
	}

	checkFn := func(_ context.Context, _ updater.Options) (*updater.Result, error) {
		return &updater.Result{
			CurrentVersion:  "2.9.0",
			LatestVersion:   "2.10.0",
			UpdateAvailable: true,
			ReleaseNotes:    "New features",
		}, nil
	}

	result, err := HandleUpdateCheck(env, checkFn)
	require.NoError(t, err)

	resp, ok := result.(models.UpdateCheckResponse)
	require.True(t, ok)
	assert.Equal(t, "2.9.0", resp.CurrentVersion)
	assert.Equal(t, "2.10.0", resp.LatestVersion)
	assert.True(t, resp.UpdateAvailable)
	assert.Equal(t, "New features", resp.ReleaseNotes)
}

func TestHandleUpdateCheck_BetaChannel(t *testing.T) {
	t.Parallel()

	// Set up by hand rather than through SetupBasicMock: the basic mock reports
	// an empty DataDir, and an empty DataDir is what silently disables the
	// generation watermark, so this test needs a platform that has one.
	dataDir := t.TempDir()
	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.On("ID").Return("mock-platform")
	mockPlatform.On("Settings").Return(platforms.Settings{DataDir: dataDir})
	mockPlatform.On("ManagedByPackageManager").Return(false)

	cfg := &config.Instance{}
	cfg.SetUpdateChannel(config.UpdateChannelBeta)

	env := requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
		Config:   cfg,
		IsLocal:  true,
	}

	var received updater.Options
	checkFn := func(_ context.Context, opts updater.Options) (*updater.Result, error) {
		received = opts
		return &updater.Result{
			CurrentVersion:  "2.9.0",
			LatestVersion:   "2.10.0-beta1",
			UpdateAvailable: true,
		}, nil
	}

	_, err := HandleUpdateCheck(env, checkFn)
	require.NoError(t, err)
	assert.Equal(t, "beta", received.Channel)
	// An empty PlatformID selects no archive at all, so it has to arrive too.
	assert.Equal(t, "mock-platform", received.PlatformID)
	assert.Equal(t, dataDir, received.DataDir)
}

func TestHandleUpdateCheck_NoUpdateAvailable(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()

	env := requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
		Config:   &config.Instance{},
		IsLocal:  true,
	}

	checkFn := func(_ context.Context, _ updater.Options) (*updater.Result, error) {
		return &updater.Result{
			CurrentVersion:  "2.10.0",
			LatestVersion:   "2.10.0",
			UpdateAvailable: false,
		}, nil
	}

	result, err := HandleUpdateCheck(env, checkFn)
	require.NoError(t, err)

	resp, ok := result.(models.UpdateCheckResponse)
	require.True(t, ok)
	assert.Equal(t, "2.10.0", resp.CurrentVersion)
	assert.False(t, resp.UpdateAvailable)
}

func TestHandleUpdateCheck_Error(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()

	env := requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
		Config:   &config.Instance{},
		IsLocal:  true,
	}

	checkFn := func(_ context.Context, _ updater.Options) (*updater.Result, error) {
		return nil, errors.New("network timeout")
	}

	result, err := HandleUpdateCheck(env, checkFn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update check failed")
	assert.Contains(t, err.Error(), "network timeout")
	assert.Nil(t, result)
}

// A check is not a privileged action — a household member may reasonably want
// to know an update exists — but it makes the device fetch signed metadata and
// write it to disk, so an unpaired remote client cannot have it either.
func TestHandleUpdateCheck_Authorization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clientRole  string
		isLocal     bool
		wantAllowed bool
	}{
		{name: "unpaired remote is refused"},
		{name: "local unpaired is allowed", isLocal: true, wantAllowed: true},
		{name: "paired member is allowed", clientRole: string(permissions.RoleMember), wantAllowed: true},
		{name: "paired admin is allowed", clientRole: string(permissions.RoleAdmin), wantAllowed: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockPlatform := mocks.NewMockPlatform()
			mockPlatform.SetupBasicMock()

			checked := false
			checkFn := func(_ context.Context, _ updater.Options) (*updater.Result, error) {
				checked = true
				return &updater.Result{CurrentVersion: "2.9.0"}, nil
			}

			env := requests.RequestEnv{
				Context:    t.Context(),
				Platform:   mockPlatform,
				Config:     &config.Instance{},
				ClientRole: tt.clientRole,
				IsLocal:    tt.isLocal,
			}

			_, err := HandleUpdateCheck(env, checkFn)
			if !tt.wantAllowed {
				require.ErrorIs(t, err, ErrForbidden)
				assert.False(t, checked, "a refused request must not reach the release server")
				return
			}
			require.NoError(t, err)
			assert.True(t, checked)
		})
	}
}

func TestUpdateProgressFn_ForwardsEveryField(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	appState, ns := state.NewState(mockPlatform, "test-boot")
	t.Cleanup(appState.StopService)
	t.Cleanup(func() { drainCh(ns) })

	progressFn := updateProgressFn(&requests.RequestEnv{State: appState})
	require.NotNil(t, progressFn)
	progressFn(updater.Progress{
		Stage:           updater.ProgressDownloading,
		Version:         "2.10.0",
		Trigger:         "manual",
		Error:           "test detail",
		BytesDownloaded: 1234,
		BytesTotal:      5678,
	})

	select {
	case notification := <-ns:
		assert.Equal(t, models.NotificationUpdateState, notification.Method)
		var payload models.UpdateStateNotification
		require.NoError(t, json.Unmarshal(notification.Params, &payload))
		assert.Equal(t, string(updater.ProgressDownloading), payload.Stage)
		assert.Equal(t, "2.10.0", payload.Version)
		assert.Equal(t, "manual", payload.Trigger)
		assert.Equal(t, "test detail", payload.Error)
		assert.Equal(t, int64(1234), payload.BytesDownloaded)
		assert.Equal(t, int64(5678), payload.BytesTotal)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for update progress notification")
	}
}

func TestUpdateGateDeps_ReportsStateSignals(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	appState, ns := state.NewState(mockPlatform, "test-boot")
	t.Cleanup(appState.StopService)
	t.Cleanup(func() { drainCh(ns) })

	deps := updateGateDeps(&requests.RequestEnv{Platform: mockPlatform, State: appState})
	require.NotNil(t, deps.BackupActive)
	require.NotNil(t, deps.BackgroundMedia)
	require.NotNil(t, deps.ActivePlaylist)
	assert.False(t, deps.BackupActive())
	assert.False(t, deps.BackgroundMedia())
	assert.False(t, deps.ActivePlaylist())

	lease, err := appState.BackupCoordinator().Begin(
		t.Context(), backupcoordinator.OperationLocalCreate, backupcoordinator.OperationRead,
	)
	require.NoError(t, err)
	appState.SetBackgroundMedia(models.NewActiveMedia(
		"Audio", "Audio", "song.mp3", "Song", platforms.NativeAudioLauncherID,
	))
	appState.SetActivePlaylist(&playlists.Playlist{ID: "playlist"})
	assert.True(t, deps.BackupActive())
	assert.True(t, deps.BackgroundMedia())
	assert.True(t, deps.ActivePlaylist())

	lease.Release()
	appState.SetBackgroundMedia(nil)
	appState.SetActivePlaylist(nil)
	assert.False(t, deps.BackupActive())
	assert.False(t, deps.BackgroundMedia())
	assert.False(t, deps.ActivePlaylist())
}

func TestUpdateRestartGuard_AfterWriteSupersedesFallback(t *testing.T) {
	t.Parallel()

	var restartCalls atomic.Int32
	var releaseCalls atomic.Int32
	guard := newUpdateRestartGuard(time.Hour, "2.9.0", "2.10.0",
		func() { restartCalls.Add(1) }, func() { releaseCalls.Add(1) })

	guard.afterWrite()
	guard.afterWrite()

	assert.Equal(t, int32(1), restartCalls.Load())
	assert.Equal(t, int32(1), releaseCalls.Load())
}

func TestUpdateRestartGuard_FallbackSupersedesLateAfterWrite(t *testing.T) {
	t.Parallel()

	var restartCalls atomic.Int32
	var releaseCalls atomic.Int32
	guard := newUpdateRestartGuard(5*time.Millisecond, "2.9.0", "2.10.0",
		func() { restartCalls.Add(1) }, func() { releaseCalls.Add(1) })

	require.Eventually(t, func() bool {
		return restartCalls.Load() == 1 && releaseCalls.Load() == 1
	}, time.Second, 5*time.Millisecond)
	guard.afterWrite()

	assert.Equal(t, int32(1), restartCalls.Load())
	assert.Equal(t, int32(1), releaseCalls.Load())
}

func TestHandleUpdateApply_DevelopmentVersion(t *testing.T) {
	devVersions := []string{"DEVELOPMENT", "abc1234-dev"}

	for _, v := range devVersions {
		t.Run(v, func(t *testing.T) {
			original := config.AppVersion
			config.AppVersion = v
			t.Cleanup(func() { config.AppVersion = original })

			mockPlatform := mocks.NewMockPlatform()
			mockPlatform.SetupBasicMock()

			env := requests.RequestEnv{
				Context:  t.Context(),
				Platform: mockPlatform,
				Config:   &config.Instance{},
				IsLocal:  true,
			}

			result, err := HandleUpdateApply(env, updater.Apply, func() {})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "development builds")
			assert.Nil(t, result)
		})
	}
}

// Replacing the binary decides what code the device runs from then on, so
// update.apply needs the capability and a request from the device itself or
// from a paired client. An unpaired remote request resolves to admin, so it
// has to be refused here in its own right.
func TestHandleUpdateApply_Authorization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		clientRole  string
		isLocal     bool
		wantAllowed bool
	}{
		{name: "paired member is refused", clientRole: string(permissions.RoleMember)},
		{name: "unknown role degrades to member and is refused", clientRole: "superuser"},
		{name: "unpaired remote is refused", wantAllowed: false},
		{name: "paired admin is allowed", clientRole: string(permissions.RoleAdmin), wantAllowed: true},
		{name: "local member is allowed", clientRole: string(permissions.RoleMember), isLocal: true, wantAllowed: true},
		{name: "local unpaired is allowed", isLocal: true, wantAllowed: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockPlatform := mocks.NewMockPlatform()
			mockPlatform.SetupBasicMock()

			applied := false
			applyFn := func(_ context.Context, _ updater.Options) (string, error) {
				applied = true
				return "2.0.0", nil
			}

			env := requests.RequestEnv{
				Context:    t.Context(),
				Platform:   mockPlatform,
				Config:     &config.Instance{},
				ClientRole: tt.clientRole,
				IsLocal:    tt.isLocal,
			}

			result, err := HandleUpdateApply(env, applyFn, func() {})
			if !tt.wantAllowed {
				require.ErrorIs(t, err, ErrForbidden)
				assert.Nil(t, result)
				assert.False(t, applied, "a refused request must not reach the updater")
				return
			}
			require.NoError(t, err)
			assert.True(t, applied)

			// Run the callback so the restart guard's fallback timer does not
			// outlive the test.
			callback, ok := result.(models.ResponseWithCallback)
			require.True(t, ok)
			callback.AfterWrite()
		})
	}
}

func TestHandleUpdateApply_Error(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()

	env := requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
		Config:   &config.Instance{},
		IsLocal:  true,
	}

	applyFn := func(_ context.Context, _ updater.Options) (string, error) {
		return "", errors.New("download failed")
	}

	result, err := HandleUpdateApply(env, applyFn, func() {})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update apply failed")
	assert.Contains(t, err.Error(), "download failed")
	assert.Nil(t, result)
}

func TestHandleUpdateApply_ErrorReleasesAcquiredGates(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	appState, ns := state.NewState(mockPlatform, "test-boot")
	t.Cleanup(appState.StopService)
	t.Cleanup(func() { drainCh(ns) })

	result, err := HandleUpdateApply(requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
		Config:   &config.Instance{},
		State:    appState,
		IsLocal:  true,
	}, func(context.Context, updater.Options) (string, error) {
		return "", errors.New("download failed")
	}, func() {})
	require.Error(t, err)
	assert.Nil(t, result)

	finishRestore, err := appState.BeginRestoreGate()
	require.NoError(t, err, "failed apply retained restore access")
	finishRestore(false)

	releaseMedia, err := appState.AcquireUpdateMediaGate(t.Context())
	require.NoError(t, err, "failed apply retained media gate")
	releaseMedia()
}

func TestHandleUpdateApply_UpdateInProgress(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	env := requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
		Config:   &config.Instance{},
		IsLocal:  true,
	}
	applyFn := func(context.Context, updater.Options) (string, error) {
		return "", updater.ErrUpdateInProgress
	}

	result, err := HandleUpdateApply(env, applyFn, func() {})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update already in progress")
	assert.Nil(t, result)
}

// A full disk is the user's problem to fix, so the directory and the shortfall
// have to survive into the client error rather than being wrapped away.
func TestHandleUpdateApply_InsufficientSpace(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	env := requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
		Config:   &config.Instance{},
		IsLocal:  true,
	}
	applyFn := func(context.Context, updater.Options) (string, error) {
		return "", fmt.Errorf("%w: /media/fat has 12 MB free, need at least 90 MB",
			updater.ErrInsufficientSpace)
	}

	result, err := HandleUpdateApply(env, applyFn, func() {})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient disk space for the update")
	assert.Contains(t, err.Error(), "/media/fat has 12 MB free, need at least 90 MB")
	assert.Nil(t, result)
}

func TestHandleUpdateApply_PlatformUnsupported(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	env := requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
		Config:   &config.Instance{},
		IsLocal:  true,
	}
	applyFn := func(context.Context, updater.Options) (string, error) {
		return "", fmt.Errorf("%w: use the Windows installer instead", updater.ErrPlatformUnsupported)
	}

	result, err := HandleUpdateApply(env, applyFn, func() {})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "this platform cannot install updates in place")
	assert.Contains(t, err.Error(), "use the Windows installer instead")
	assert.Nil(t, result)
}

func TestHandleUpdateApply_ActiveMedia(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	st, _ := state.NewState(mockPlatform, "test-boot")
	t.Cleanup(st.StopService)
	st.SetActiveMedia(models.NewActiveMedia(
		"SNES", "Super Nintendo", filepath.Join("roms", "game.sfc"), "Game", "test-launcher",
	))

	env := requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
		Config:   &config.Instance{},
		State:    st,
		IsLocal:  true,
	}
	applyFn := func(context.Context, updater.Options) (string, error) {
		t.Fatal("applyFn should not be called while media is active")
		return "", nil
	}

	result, err := HandleUpdateApply(env, applyFn, func() {})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "media is playing")
	assert.Nil(t, result)
}

// A token write is recorded against the reader doing it, so the gate has to
// ask about every reader rather than about a reader ID no writer ever uses.
// Restarting part-way through an NDEF write leaves a half-written token.
func TestHandleUpdateApply_ReaderWriting(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	st, _ := state.NewState(mockPlatform, "test-boot")
	t.Cleanup(st.StopService)
	st.SetReaderWriteActive(true, "pn532_uart:/dev/ttyUSB0")

	applyFn := func(context.Context, updater.Options) (string, error) {
		t.Fatal("applyFn should not be called while a reader is writing")
		return "", nil
	}

	// Writing a token is data the user is part-way through, so force does not
	// get past it either.
	for _, params := range []json.RawMessage{nil, json.RawMessage(`{"force":true}`)} {
		env := requests.RequestEnv{
			Context:  t.Context(),
			Platform: mockPlatform,
			Config:   &config.Instance{},
			State:    st,
			Params:   params,
			IsLocal:  true,
		}

		result, err := HandleUpdateApply(env, applyFn, func() {})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "a token is being written")
		assert.Nil(t, result)
	}
}

// Media playing is the user's session, not their data, so a client that has
// asked them about it can go ahead.
func TestHandleUpdateApply_ForcePastActiveMedia(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	st, _ := state.NewState(mockPlatform, "test-boot")
	t.Cleanup(st.StopService)
	st.SetActiveMedia(models.NewActiveMedia(
		"SNES", "Super Nintendo", filepath.Join("roms", "game.sfc"), "Game", "test-launcher",
	))

	env := requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
		Config:   &config.Instance{},
		State:    st,
		Params:   json.RawMessage(`{"force":true}`),
		IsLocal:  true,
	}
	applyFn := func(context.Context, updater.Options) (string, error) {
		return "2.10.0", nil
	}

	result, err := HandleUpdateApply(env, applyFn, func() {})
	require.NoError(t, err)
	callback, ok := result.(models.ResponseWithCallback)
	require.True(t, ok)
	callback.AfterWrite()
}

// Force is a person accepting the loss of what is on screen. It is not a
// person able to make a flat battery last, so it does not get past one.
func TestHandleUpdateApply_ForceDoesNotBypassLowBattery(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	mockPlatform.SetPowerStatus(power.Status{Source: power.SourceBattery, Percent: 5})

	env := requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
		Config:   &config.Instance{},
		Params:   json.RawMessage(`{"force":true}`),
		IsLocal:  true,
	}
	applyFn := func(context.Context, updater.Options) (string, error) {
		t.Fatal("applyFn should not be called on a flat battery")
		return "", nil
	}

	result, err := HandleUpdateApply(env, applyFn, func() {})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the battery is at 5%")
	assert.Nil(t, result)
}

// The download can run for minutes, which is long enough for someone to
// unplug the device, so the reading is taken again before anything is
// replaced.
func TestHandleUpdateApply_PreQuiesceRechecksPower(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()

	env := requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
		Config:   &config.Instance{},
		IsLocal:  true,
	}
	applyFn := func(ctx context.Context, opts updater.Options) (string, error) {
		require.NotNil(t, opts.PreQuiesce)
		require.NoError(t, opts.PreQuiesce(ctx))
		// The charger comes out part-way through the download.
		mockPlatform.SetPowerStatus(power.Status{Source: power.SourceBattery, Percent: 3})
		return "", opts.PreQuiesce(ctx)
	}

	result, err := HandleUpdateApply(env, applyFn, func() {})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the battery is at 3%")
	assert.Nil(t, result)
}

func TestHandleUpdateApply_HoldsMediaGateUntilRestart(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()
	st, _ := state.NewState(mockPlatform, "test-boot")
	t.Cleanup(st.StopService)
	env := requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
		Config:   &config.Instance{},
		State:    st,
		IsLocal:  true,
	}
	applyFn := func(context.Context, updater.Options) (string, error) {
		return "2.10.0", nil
	}

	result, err := HandleUpdateApply(env, applyFn, st.RestartService)
	require.NoError(t, err)
	response, ok := result.(models.ResponseWithCallback)
	require.True(t, ok)

	launchErr := make(chan error, 1)
	go func() {
		access, acquireErr := st.AcquireMediaLaunch()
		if access.Release != nil {
			access.Release()
		}
		launchErr <- acquireErr
	}()
	select {
	case err := <-launchErr:
		require.NoError(t, err)
		t.Fatal("media launch gate released before update restart")
	case <-time.After(50 * time.Millisecond):
	}

	response.AfterWrite()
	select {
	case err := <-launchErr:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("queued media launch did not stop after update restart")
	}
}

func TestHandleUpdateApply_IndexingInProgress(t *testing.T) {
	t.Parallel()

	statuses := []string{
		mediadb.IndexingStatusRunning,
		mediadb.IndexingStatusPending,
	}

	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			t.Parallel()

			mockPlatform := mocks.NewMockPlatform()
			mockPlatform.SetupBasicMock()

			mockMediaDB := helpers.NewMockMediaDBI()
			mockMediaDB.On("GetIndexingStatus").Return(status, nil)

			env := requests.RequestEnv{
				Context:  t.Context(),
				Platform: mockPlatform,
				Config:   &config.Instance{},
				Database: &database.Database{MediaDB: mockMediaDB},
				IsLocal:  true,
			}

			applyFn := func(_ context.Context, _ updater.Options) (string, error) {
				t.Fatal("applyFn should not be called during indexing")
				return "", nil
			}

			result, err := HandleUpdateApply(env, applyFn, func() {})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "the media database is being generated")
			assert.Nil(t, result)

			mockMediaDB.AssertExpectations(t)
		})
	}
}

func TestHandleUpdateApply_IndexingCompleted(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()

	mockMediaDB := helpers.NewMockMediaDBI()
	mockMediaDB.On("GetIndexingStatus").Return(mediadb.IndexingStatusCompleted, nil)
	mockMediaDB.On("GetOptimizationStatus").Return(mediadb.IndexingStatusCompleted, nil)
	mockMediaDB.On("GetScrapingStatus").Return(mediadb.IndexingStatusCompleted, nil)
	mockUserDB := helpers.NewMockUserDBI()

	env := requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
		Config:   &config.Instance{},
		Database: &database.Database{UserDB: mockUserDB, MediaDB: mockMediaDB},
		IsLocal:  true,
	}

	applyFn := func(_ context.Context, opts updater.Options) (string, error) {
		assert.Same(t, mockUserDB, opts.UserDB)
		return "2.10.0", nil
	}

	restartCalled := false
	restartFn := func() { restartCalled = true }

	result, err := HandleUpdateApply(env, applyFn, restartFn)
	require.NoError(t, err)

	rwc, ok := result.(models.ResponseWithCallback)
	require.True(t, ok)
	require.NotNil(t, rwc.AfterWrite)

	resp, ok := rwc.Result.(models.UpdateApplyResponse)
	require.True(t, ok)
	assert.Equal(t, "2.10.0", resp.NewVersion)

	rwc.AfterWrite()
	assert.True(t, restartCalled)

	mockMediaDB.AssertExpectations(t)
}

func TestHandleUpdateCheck_ReportsEverythingAClientNeeds(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()

	cfg := &config.Instance{}
	cfg.SetUpdateCheck(true)
	cfg.SetUpdateInstall(true)

	env := requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
		Config:   cfg,
		IsLocal:  true,
	}

	checkedAt := time.Date(2026, 8, 18, 9, 30, 0, 0, time.UTC)
	deferredSince := time.Date(2026, 8, 17, 21, 0, 0, 0, time.UTC)
	finishedAt := time.Date(2026, 8, 10, 4, 0, 0, 0, time.UTC)

	checkFn := func(_ context.Context, _ updater.Options) (*updater.Result, error) {
		return &updater.Result{
			CheckedAt:        checkedAt,
			CurrentVersion:   "2.9.0",
			LatestVersion:    "2.10.0",
			UpdateAvailable:  true,
			ReleaseNotes:     "New features",
			Channel:          "stable",
			Eligibility:      updater.EligibilityEligible,
			RolloutHeld:      true,
			DeferredReason:   updater.ReasonActiveMedia,
			DeferredSince:    deferredSince,
			BlockedReason:    updater.ReasonActiveMedia,
			BlockedMessage:   "media is playing",
			BlockedForceable: true,
			LastResult: &updater.OutcomeReport{
				At:          finishedAt,
				Outcome:     "rolledBack",
				FromVersion: "2.8.0",
				ToVersion:   "2.9.0",
				Detail:      "the new build would not start",
			},
		}, nil
	}

	result, err := HandleUpdateCheck(env, checkFn)
	require.NoError(t, err)

	resp, ok := result.(models.UpdateCheckResponse)
	require.True(t, ok)
	assert.Equal(t, "stable", resp.Channel)
	assert.Equal(t, updater.EligibilityEligible, resp.Eligibility)
	assert.True(t, resp.RolloutHeld)
	assert.True(t, resp.AutoInstall)
	assert.Equal(t, updater.ReasonActiveMedia, resp.DeferredReason)
	require.NotNil(t, resp.CheckedAt)
	assert.True(t, checkedAt.Equal(*resp.CheckedAt))
	require.NotNil(t, resp.DeferredSince)
	assert.True(t, deferredSince.Equal(*resp.DeferredSince))

	// blockedBy is what a client reads to decide between hiding the update
	// button and offering to go ahead anyway.
	require.NotNil(t, resp.BlockedBy)
	assert.Equal(t, updater.ReasonActiveMedia, resp.BlockedBy.Reason)
	assert.Equal(t, "media is playing", resp.BlockedBy.Message)
	assert.True(t, resp.BlockedBy.Forceable)

	require.NotNil(t, resp.LastResult)
	assert.True(t, finishedAt.Equal(resp.LastResult.At))
	assert.Equal(t, "rolledBack", resp.LastResult.Outcome)
	assert.Equal(t, "2.8.0", resp.LastResult.FromVersion)
	assert.Equal(t, "2.9.0", resp.LastResult.ToVersion)
	assert.Equal(t, "the new build would not start", resp.LastResult.Detail)
}

func TestHandleUpdateCheck_OmitsWhatIsNotHappening(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()

	env := requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
		Config:   &config.Instance{},
		IsLocal:  true,
	}

	checkFn := func(_ context.Context, _ updater.Options) (*updater.Result, error) {
		return &updater.Result{
			CurrentVersion:  "2.10.0",
			LatestVersion:   "2.10.0",
			UpdateAvailable: false,
		}, nil
	}

	result, err := HandleUpdateCheck(env, checkFn)
	require.NoError(t, err)
	resp, ok := result.(models.UpdateCheckResponse)
	require.True(t, ok)
	assert.Nil(t, resp.BlockedBy)
	assert.Nil(t, resp.LastResult)
	assert.Nil(t, resp.CheckedAt)
	assert.Nil(t, resp.DeferredSince)
	assert.False(t, resp.AutoInstall)

	// A zero time must not reach a client as 0001-01-01, and an absent block
	// must not reach it as an empty object it has to special-case.
	encoded, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "blockedBy")
	assert.NotContains(t, string(encoded), "checkedAt")
	assert.NotContains(t, string(encoded), "lastResult")
	assert.NotContains(t, string(encoded), "deferredSince")
}

// TestHandleUpdateCheck_ReadsTheGate proves a check reports what the device is
// busy with, so a client can say why the update button is not there.
func TestHandleUpdateCheck_ReadsTheGate(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()

	appState, ns := state.NewState(mockPlatform, "test-boot-uuid")
	t.Cleanup(appState.StopService)
	t.Cleanup(func() { drainCh(ns) })
	appState.SetActiveMedia(&models.ActiveMedia{SystemID: "SNES", Name: "Test Game"})

	env := requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
		Config:   &config.Instance{},
		State:    appState,
		IsLocal:  true,
	}

	var received updater.Options
	checkFn := func(_ context.Context, opts updater.Options) (*updater.Result, error) {
		received = opts
		return &updater.Result{
			CurrentVersion:  "2.9.0",
			LatestVersion:   "2.10.0",
			UpdateAvailable: true,
		}, nil
	}

	_, err := HandleUpdateCheck(env, checkFn)
	require.NoError(t, err)

	require.NotNil(t, received.Gate)
	require.NotNil(t, received.Gate.ActiveMedia)
	assert.True(t, received.Gate.ActiveMedia())
	require.NotNil(t, received.Gate.Power)
	assert.NotEmpty(t, received.Gate.Power().Source)
}

func TestHandleUpdateCheck_DevelopmentVersionReportsEligibility(t *testing.T) {
	original := config.AppVersion
	config.AppVersion = "DEVELOPMENT"
	t.Cleanup(func() { config.AppVersion = original })

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()

	cfg := &config.Instance{}
	cfg.SetUpdateChannel(config.UpdateChannelBeta)

	env := requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
		Config:   cfg,
		IsLocal:  true,
	}

	checkFn := func(_ context.Context, _ updater.Options) (*updater.Result, error) {
		return nil, updater.ErrDevelopmentVersion
	}

	result, err := HandleUpdateCheck(env, checkFn)
	require.NoError(t, err)
	resp, ok := result.(models.UpdateCheckResponse)
	require.True(t, ok)
	assert.Equal(t, updater.EligibilityDevelopment, resp.Eligibility)
	assert.Equal(t, "beta", resp.Channel)
	assert.False(t, resp.UpdateAvailable)
}

// TestHandleUpdateApply_ForceDoesNotElevate proves force is a way past what a
// person can be asked about, not a way past who they are.
func TestHandleUpdateApply_ForceDoesNotElevate(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()

	applied := false
	applyFn := func(_ context.Context, _ updater.Options) (string, error) {
		applied = true
		return "2.0.0", nil
	}

	env := requests.RequestEnv{
		Context:    t.Context(),
		Platform:   mockPlatform,
		Config:     &config.Instance{},
		ClientRole: string(permissions.RoleMember),
		Params:     json.RawMessage(`{"force":true}`),
	}

	result, err := HandleUpdateApply(env, applyFn, func() {})
	require.ErrorIs(t, err, ErrForbidden)
	assert.Nil(t, result)
	assert.False(t, applied)
}

func TestHandleUpdateApply_InvalidParams(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()

	applied := false
	applyFn := func(_ context.Context, _ updater.Options) (string, error) {
		applied = true
		return "2.0.0", nil
	}

	env := requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
		Config:   &config.Instance{},
		IsLocal:  true,
		Params:   json.RawMessage(`{"force":"yes"}`),
	}

	result, err := HandleUpdateApply(env, applyFn, func() {})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid params")
	assert.Nil(t, result)
	assert.False(t, applied)
}

func TestHandleUpdateApply_NoParamsIsNotForced(t *testing.T) {
	t.Parallel()

	mockPlatform := mocks.NewMockPlatform()
	mockPlatform.SetupBasicMock()

	appState, ns := state.NewState(mockPlatform, "test-boot-uuid")
	t.Cleanup(appState.StopService)
	t.Cleanup(func() { drainCh(ns) })
	appState.SetActiveMedia(&models.ActiveMedia{SystemID: "SNES", Name: "Test Game"})

	applied := false
	applyFn := func(_ context.Context, _ updater.Options) (string, error) {
		applied = true
		return "2.0.0", nil
	}

	env := requests.RequestEnv{
		Context:  t.Context(),
		Platform: mockPlatform,
		Config:   &config.Instance{},
		State:    appState,
		IsLocal:  true,
	}

	result, err := HandleUpdateApply(env, applyFn, func() {})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "media is playing")
	assert.Nil(t, result)
	assert.False(t, applied)
}
