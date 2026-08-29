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
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/updatepayload"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater/otameta"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func linuxOptions() Options {
	return Options{PlatformID: "linux", Channel: config.UpdateChannelStable}
}

func TestCheck_DevelopmentVersion(t *testing.T) {
	devVersions := []string{"DEVELOPMENT", "abc1234-dev"}

	for _, v := range devVersions {
		t.Run(v, func(t *testing.T) {
			original := config.AppVersion
			config.AppVersion = v
			t.Cleanup(func() { config.AppVersion = original })

			result, err := Check(t.Context(), linuxOptions())
			require.ErrorIs(t, err, ErrDevelopmentVersion)
			assert.Nil(t, result)
		})
	}
}

func TestApply_DevelopmentVersion(t *testing.T) {
	devVersions := []string{"DEVELOPMENT", "abc1234-dev"}

	for _, v := range devVersions {
		t.Run(v, func(t *testing.T) {
			original := config.AppVersion
			config.AppVersion = v
			t.Cleanup(func() { config.AppVersion = original })

			version, err := Apply(t.Context(), linuxOptions())
			require.ErrorIs(t, err, ErrDevelopmentVersion)
			assert.Empty(t, version)
		})
	}
}

func alwaysOnline(_ context.Context, _ int) bool { return true }

// A package manager owning the install is a reason not to install, not a
// reason not to look. Someone whose package manager is lagging behind has no
// other way to find out.
func TestCheckAndNotify_ManagedInstallStillChecks(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{} // Updates.Check is nil

	waitCalled := false
	CheckAndNotify(t.Context(), cfg, linuxOptions(), nil, func(_ context.Context, _ int) bool {
		waitCalled = true
		return false
	}, Check, true)

	assert.True(t, waitCalled)
}

func TestRolloutHeld_UsesSelectedReleaseWhenChannelsShareVersion(t *testing.T) {
	t.Parallel()

	stable := &otameta.Release{
		TagName: "v2.10.0",
		Channel: config.UpdateChannelStable,
		Rollout: 0,
	}
	beta := &otameta.Release{
		TagName: "v2.10.0",
		Channel: config.UpdateChannelBeta,
		Rollout: 100,
	}

	assert.True(t, rolloutHeld("device-1", stable))
	assert.False(t, rolloutHeld("device-1", beta))
}

func TestInstallAdvice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		platformID string
		want       string
		managed    bool
	}{
		{platformID: platformids.Mister, managed: false, want: "Use the App or TUI to update."},
		{platformID: platformids.Mister, managed: true, want: "Run update_all to install it."},
		{
			platformID: platformids.Batocera, managed: true,
			want: "Install it through the Batocera package manager.",
		},
		{
			platformID: platformids.Linux, managed: true,
			want: "Your package manager installs updates on this device.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.platformID+"/"+strconv.FormatBool(tt.managed), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, installAdvice(tt.platformID, tt.managed))
		})
	}
}

func TestCheckAndNotify_DisabledConfig(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	cfg.SetUpdateCheck(false)

	waitCalled := false
	CheckAndNotify(t.Context(), cfg, linuxOptions(), nil, func(_ context.Context, _ int) bool {
		waitCalled = true
		return true
	}, Check, false)

	assert.False(t, waitCalled)
}

func TestCheckAndNotify_DevelopmentVersion(t *testing.T) {
	original := config.AppVersion
	config.AppVersion = "abc1234-dev"
	t.Cleanup(func() { config.AppVersion = original })

	cfg := &config.Instance{}
	cfg.SetUpdateCheck(true)

	CheckAndNotify(t.Context(), cfg, linuxOptions(), nil, alwaysOnline, Check, false)
}

func TestCheckAndNotify_NoInternet(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	cfg.SetUpdateCheck(true)

	CheckAndNotify(t.Context(), cfg, linuxOptions(), nil, func(_ context.Context, _ int) bool {
		return false
	}, Check, false)
}

func TestCheckAndNotify_UpdateAvailable(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	cfg.SetUpdateCheck(true)

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("AddInboxMessage", mock.MatchedBy(func(msg *database.InboxMessage) bool {
		return msg.Title == "Zaparoo 2.10.0 is available" &&
			msg.Category == inbox.CategoryUpdateAvailable
	})).Return(&database.InboxMessage{DBID: 1, Title: "Zaparoo 2.10.0 is available"}, nil)

	ns := make(chan models.Notification, 10)
	inboxSvc := inbox.NewService(mockUserDB, ns)

	checkFn := func(_ context.Context, _ Options) (*Result, error) {
		return &Result{
			CurrentVersion:  "2.9.0",
			LatestVersion:   "2.10.0",
			UpdateAvailable: true,
			ReleaseNotes:    "New features",
		}, nil
	}

	CheckAndNotify(t.Context(), cfg, linuxOptions(), inboxSvc, alwaysOnline, checkFn, false)

	mockUserDB.AssertExpectations(t)
}

func TestCheckAndNotify_DeduplicatesOfferedVersion(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	cfg.SetUpdateCheck(true)
	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("AddInboxMessage", mock.Anything).
		Return(&database.InboxMessage{DBID: 1}, nil).Once()
	inboxSvc := inbox.NewService(mockUserDB, make(chan models.Notification, 10))
	opts := linuxOptions()
	opts.DataDir = t.TempDir()
	checkFn := func(context.Context, Options) (*Result, error) {
		return &Result{
			CurrentVersion: "2.9.0", LatestVersion: "2.10.0",
			UpdateAvailable: true,
		}, nil
	}

	CheckAndNotify(t.Context(), cfg, opts, inboxSvc, alwaysOnline, checkFn, false)
	CheckAndNotify(t.Context(), cfg, opts, inboxSvc, alwaysOnline, checkFn, false)

	mockUserDB.AssertExpectations(t)
	assert.Equal(t, "2.10.0", lastOfferedVersion(stateDirFor(opts.DataDir)))
}

// A package-managed device still gets told a release exists, so the message has
// to point at the thing that actually installs it there.
func TestCheckAndNotify_ManagedInstallBodyNamesThePackageManager(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	cfg.SetUpdateCheck(true)

	mockUserDB := helpers.NewMockUserDBI()
	mockUserDB.On("AddInboxMessage", mock.MatchedBy(func(msg *database.InboxMessage) bool {
		return strings.Contains(msg.Body, "Run update_all to install it.")
	})).Return(&database.InboxMessage{DBID: 1}, nil)

	ns := make(chan models.Notification, 10)
	inboxSvc := inbox.NewService(mockUserDB, ns)

	checkFn := func(_ context.Context, _ Options) (*Result, error) {
		return &Result{
			CurrentVersion:  "2.9.0",
			LatestVersion:   "2.10.0",
			UpdateAvailable: true,
		}, nil
	}

	opts := Options{PlatformID: platformids.Mister, Channel: config.UpdateChannelStable}
	CheckAndNotify(t.Context(), cfg, opts, inboxSvc, alwaysOnline, checkFn, true)

	mockUserDB.AssertExpectations(t)
}

func TestCheckAndNotify_BetaChannel(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	cfg.SetUpdateCheck(true)
	cfg.SetUpdateChannel(config.UpdateChannelBeta)

	var receivedChannel string
	checkFn := func(_ context.Context, opts Options) (*Result, error) {
		receivedChannel = opts.Channel
		return &Result{
			CurrentVersion:  "2.10.0",
			LatestVersion:   "2.10.0",
			UpdateAvailable: false,
		}, nil
	}

	// The caller-supplied channel is deliberately wrong: the configured channel
	// must win, or a stale Options would pin a device to the wrong track.
	CheckAndNotify(t.Context(), cfg, linuxOptions(), nil, alwaysOnline, checkFn, false)

	assert.Equal(t, config.UpdateChannelBeta, receivedChannel)
}

func TestCheckAndNotify_NoUpdateAvailable(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	cfg.SetUpdateCheck(true)

	checkFn := func(_ context.Context, _ Options) (*Result, error) {
		return &Result{
			CurrentVersion:  "2.10.0",
			LatestVersion:   "2.10.0",
			UpdateAvailable: false,
		}, nil
	}

	// inboxSvc is nil — would panic if code tried to post a message
	CheckAndNotify(t.Context(), cfg, linuxOptions(), nil, alwaysOnline, checkFn, false)
}

func TestCheckAndNotify_CheckError(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	cfg.SetUpdateCheck(true)

	checkFn := func(_ context.Context, _ Options) (*Result, error) {
		return nil, errors.New("network timeout")
	}

	// inboxSvc is nil — would panic if code tried to post a message
	CheckAndNotify(t.Context(), cfg, linuxOptions(), nil, alwaysOnline, checkFn, false)
}

func TestCheck_CancelledContext(t *testing.T) {
	original := config.AppVersion
	config.AppVersion = "1.0.0"
	t.Cleanup(func() { config.AppVersion = original })

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	result, err := Check(ctx, linuxOptions())
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestUpdatePayloadFilesOnlyForUnmanagedInstall(t *testing.T) {
	t.Parallel()

	payload := []updatepayload.File{{ArchivePath: "scripts/helper.sh"}}
	assert.Equal(t, payload, updatePayloadFiles(&Options{Payload: payload}))
	assert.Empty(t, updatePayloadFiles(&Options{Payload: payload, Managed: true}))
	assert.Empty(t, updatePayloadFiles(&Options{}))
}

func TestAutoInstallReleaseAllowed(t *testing.T) {
	t.Parallel()

	held := &otameta.Release{TagName: "v2.10.0", Rollout: 0}
	full := &otameta.Release{TagName: "v2.10.0", Rollout: 100}
	require.NoError(t, autoInstallReleaseAllowed(&Options{Mode: ModeManual, Managed: true}, held))
	require.ErrorIs(t,
		autoInstallReleaseAllowed(&Options{Mode: ModeAuto, Managed: true}, full),
		errAutoInstallIneligible)
	require.ErrorIs(t,
		autoInstallReleaseAllowed(&Options{Mode: ModeAuto, DeviceID: "device-1"}, held),
		errAutoInstallIneligible)
	assert.NoError(t, autoInstallReleaseAllowed(&Options{Mode: ModeAuto}, full))
}

func TestApply_UpdateInProgress(t *testing.T) {
	original := config.AppVersion
	config.AppVersion = "1.0.0"
	t.Cleanup(func() { config.AppVersion = original })
	applyInProgress.Store(true)
	t.Cleanup(func() { applyInProgress.Store(false) })

	version, err := Apply(t.Context(), Options{})
	require.ErrorIs(t, err, ErrUpdateInProgress)
	assert.Empty(t, version)
}

func TestApply_CancelledContext(t *testing.T) {
	original := config.AppVersion
	config.AppVersion = "1.0.0"
	t.Cleanup(func() { config.AppVersion = original })

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	opts := linuxOptions()
	opts.UserDB = &installTestBackupper{}
	opts.DataDir = t.TempDir()

	version, err := Apply(ctx, opts)
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, version)
}

func TestApply_RejectsIncompleteOptions(t *testing.T) {
	original := config.AppVersion
	config.AppVersion = "1.0.0"
	t.Cleanup(func() { config.AppVersion = original })

	withDataDir := linuxOptions()
	withDataDir.DataDir = t.TempDir()

	withUserDB := linuxOptions()
	withUserDB.UserDB = &installTestBackupper{}

	for name, opts := range map[string]Options{
		"no user database":   withDataDir,
		"no data directory":  withUserDB,
		"neither of the two": linuxOptions(),
	} {
		t.Run(name, func(t *testing.T) {
			version, err := Apply(t.Context(), opts)
			require.Error(t, err)
			assert.Empty(t, version)
		})
	}
}

// An update that has not been confirmed or rolled back yet owns the binary
// backup and the marker, so a second Apply must refuse before it reaches the
// network rather than overwrite them.
func TestApply_RefusesWhileAnUpdateIsUnresolved(t *testing.T) {
	original := config.AppVersion
	config.AppVersion = "1.0.0"
	t.Cleanup(func() { config.AppVersion = original })

	dataDir := t.TempDir()
	require.NoError(t, saveMarker(stateDirFor(dataDir), &pendingMarker{
		State:         markerConfirming,
		TargetVersion: "2.2.0",
	}))

	opts := linuxOptions()
	opts.UserDB = &installTestBackupper{}
	opts.DataDir = dataDir

	version, err := Apply(t.Context(), opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "2.2.0")
	assert.Empty(t, version)
}

func TestClearDeferralForRelease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		recordedVersion string
		releaseVersion  string
		wantDeferral    bool
	}{
		{
			name:            "matching release keeps deferral",
			recordedVersion: "v2.5.0",
			releaseVersion:  "v2.5.0",
			wantDeferral:    true,
		},
		{
			name:            "different release clears deferral",
			recordedVersion: "v2.5.0",
			releaseVersion:  "v2.6.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "updater")
			require.NoError(t, recordDeferral(dir, tt.recordedVersion, ReasonActiveMedia))
			before := peekDeferralState(dir)
			require.NotNil(t, before)

			require.NoError(t, clearDeferralForRelease(dir, tt.releaseVersion))
			after := peekDeferralState(dir)
			if !tt.wantDeferral {
				assert.Nil(t, after)
				return
			}
			require.NotNil(t, after)
			assert.Equal(t, before.Version, after.Version)
			assert.Equal(t, before.Reason, after.Reason)
			assert.Equal(t, before.Since, after.Since)
		})
	}
}

func TestNoteGate_NoGateConfigured(t *testing.T) {
	t.Parallel()

	result := &Result{}
	noteGate(t.Context(), &Options{}, result, filepath.Join(t.TempDir(), "updater"), "v2.5.0")

	assert.Empty(t, result.BlockedReason)
	assert.Empty(t, result.BlockedMessage)
}

func TestNoteGate_NothingInTheWay(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "updater")
	result := &Result{}
	opts := &Options{Gate: &GateDeps{Power: externalPower}}
	noteGate(t.Context(), opts, result, dir, "v2.5.0")

	assert.Empty(t, result.BlockedReason)
	assert.Nil(t, peekDeferral(dir, "v2.5.0"))
}

func TestNoteGate_SoftSignalStartsTheClock(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "updater")
	result := &Result{}
	opts := &Options{Gate: &GateDeps{
		Power:       externalPower,
		ActiveMedia: alwaysTrue,
	}}
	noteGate(t.Context(), opts, result, dir, "v2.5.0")

	assert.Equal(t, ReasonActiveMedia, result.BlockedReason)
	assert.NotEmpty(t, result.BlockedMessage)
	assert.True(t, result.BlockedForceable, "a person may go ahead through their own game")

	// Something that will pass on its own is what the automatic install's
	// patience is measured against, so the check writes it down.
	deferral := peekDeferral(dir, "v2.5.0")
	require.NotNil(t, deferral)
	assert.Equal(t, ReasonActiveMedia, deferral.Reason)
}

func TestNoteGate_HardSignalIsReportedButNotDeferred(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "updater")
	result := &Result{}
	opts := &Options{Gate: &GateDeps{
		Power:          externalPower,
		IndexingStatus: statusFn(mediadb.IndexingStatusRunning),
	}}
	noteGate(t.Context(), opts, result, dir, "v2.5.0")

	assert.Equal(t, ReasonMediaIndexing, result.BlockedReason)
	assert.False(t, result.BlockedForceable)
	// Indexing never times out into being safe, so there is no clock to start.
	assert.Nil(t, peekDeferral(dir, "v2.5.0"))
}

func TestNoteGate_ReportsWithoutTakingAnyGate(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "updater")
	restoreTaken, mediaTaken := false, false
	opts := &Options{Gate: &GateDeps{
		Power: externalPower,
		AcquireRestore: func() (func(), error) {
			restoreTaken = true
			return func() {}, nil
		},
		AcquireMediaGate: func(context.Context) (func(), error) {
			mediaTaken = true
			return func() {}, nil
		},
	}}
	noteGate(t.Context(), opts, &Result{}, dir, "v2.5.0")

	// A check only reports. Taking either gate would block backups and
	// launches every time a client asked whether an update was available.
	assert.False(t, restoreTaken)
	assert.False(t, mediaTaken)
}

// Status answers from disk. The check that produced those findings is the only
// thing that talks to the network, so nothing here may need it.
func TestStatus_AnswersFromRecordedFindingsWithoutTheNetwork(t *testing.T) {
	original := config.AppVersion
	config.AppVersion = "2.10.0"
	t.Cleanup(func() { config.AppVersion = original })

	dataDir := t.TempDir()
	stateDir := stateDirFor(dataDir)
	require.NoError(t, recordScheduledCheck(stateDir, true))
	require.NoError(t, recordCheckFindings(stateDir, &lastCheckFindings{
		LatestVersion:   "2.11.0",
		UpdateAvailable: true,
	}))

	// No manifest, no server, no baseURL: reaching for any of them would fail
	// rather than quietly fall back.
	result := Status(t.Context(), Options{DataDir: dataDir, Channel: "stable"})
	require.NotNil(t, result)
	assert.Equal(t, "2.10.0", result.CurrentVersion)
	assert.Equal(t, "2.11.0", result.LatestVersion)
	assert.True(t, result.UpdateAvailable)
	assert.Equal(t, "stable", result.Channel)
	assert.False(t, result.CheckedAt.IsZero(), "must say how old the answer is")
}

// The recorded findings describe the moment the check ran. Once the update has
// been installed they would otherwise keep offering a version already running.
func TestStatus_DoesNotOfferAVersionAlreadyInstalled(t *testing.T) {
	original := config.AppVersion
	config.AppVersion = "2.11.0"
	t.Cleanup(func() { config.AppVersion = original })

	dataDir := t.TempDir()
	require.NoError(t, recordCheckFindings(stateDirFor(dataDir), &lastCheckFindings{
		LatestVersion:   "2.11.0",
		UpdateAvailable: true,
	}))

	result := Status(t.Context(), Options{DataDir: dataDir})
	require.NotNil(t, result)
	assert.False(t, result.UpdateAvailable, "recomputed against the running version")
	assert.Equal(t, "2.11.0", result.LatestVersion)
}

// A check refuses outright on a development build. Status is what a screen
// shows, and "this is a dev build so updates do not apply" is the useful answer.
func TestStatus_ReportsADevelopmentBuildInsteadOfRefusing(t *testing.T) {
	original := config.AppVersion
	config.AppVersion = "abc1234-dev"
	t.Cleanup(func() { config.AppVersion = original })

	_, checkErr := Check(t.Context(), Options{DataDir: t.TempDir()})
	require.ErrorIs(t, checkErr, ErrDevelopmentVersion, "premise: a check refuses here")

	result := Status(t.Context(), Options{DataDir: t.TempDir()})
	require.NotNil(t, result)
	assert.Equal(t, "abc1234-dev", result.CurrentVersion)
	assert.Equal(t, EligibilityDevelopment, result.Eligibility)
	assert.False(t, result.UpdateAvailable)
}

// Nothing has checked yet. Saying so is different from saying it is up to date.
func TestStatus_SaysNothingIsKnownBeforeTheFirstCheck(t *testing.T) {
	original := config.AppVersion
	config.AppVersion = "2.10.0"
	t.Cleanup(func() { config.AppVersion = original })

	result := Status(t.Context(), Options{DataDir: t.TempDir()})
	require.NotNil(t, result)
	assert.Empty(t, result.LatestVersion)
	assert.False(t, result.UpdateAvailable)
	assert.True(t, result.CheckedAt.IsZero(), "no check has happened, so there is no time to report")
}

// withdrawnManifest is a manifest whose only release cannot be installed here,
// which is what a device sees once a release has been withdrawn.
func withdrawnManifest(generation int64) string {
	return fmt.Sprintf(`manifest_version: 1
generation: %d
issued_at: 2026-08-17T02:00:00Z
key_id: test1
last_release_id: 1
last_asset_id: 10
releases:
  - id: 1
    name: v2.16.1
    tag_name: v2.16.1
    channel: stable
    published_at: 2026-08-10T00:00:00Z
    assets:
      - id: 10
        name: zaparoo-zapos_arm64-2.16.1.tar.gz
        size: 8123456
        sha256: aaaa
        url: https://github.com/ZaparooProject/zaparoo-core/releases/download/v2.16.1/zaparoo-zapos_arm64-2.16.1.tar.gz
`, generation)
}

// Withdrawing a release is the emergency stop, so a check that finds nothing
// left to install has to say so durably. Recording only when a release was
// found would leave the previous offer standing, and Status would keep offering
// a version that no longer exists until some later check happened to find a
// different one.
func TestCheck_ForgetsAnOfferOnceTheReleaseIsWithdrawn(t *testing.T) {
	original := config.AppVersion
	config.AppVersion = "2.10.0"
	t.Cleanup(func() { config.AppVersion = original })

	dataDir := t.TempDir()
	stateDir := stateDirFor(dataDir)
	require.NoError(t, recordCheckFindings(stateDir, &lastCheckFindings{
		LatestVersion:   "2.16.1",
		UpdateAvailable: true,
	}))
	require.True(t, Status(t.Context(), Options{DataDir: dataDir}).UpdateAvailable,
		"the offer has to be there before the check that should clear it")

	ms := newManifestServer(t, withdrawnManifest(9))
	src := ms.source(stateDir, "linux", "amd64")
	previous := newSession
	newSession = func(Options) (*session, error) {
		return &session{source: src, close: func() {}}, nil
	}
	t.Cleanup(func() { newSession = previous })

	result, err := Check(t.Context(), Options{
		DataDir: dataDir, Channel: "stable", PlatformID: "linux",
	})
	require.NoError(t, err)
	assert.Empty(t, result.LatestVersion)
	assert.False(t, result.UpdateAvailable)

	// The result alone is not the fix: what Status reports afterwards is.
	status := Status(t.Context(), Options{DataDir: dataDir, Channel: "stable"})
	require.NotNil(t, status)
	assert.Empty(t, status.LatestVersion, "the withdrawn version must stop being offered")
	assert.False(t, status.UpdateAvailable)
}
