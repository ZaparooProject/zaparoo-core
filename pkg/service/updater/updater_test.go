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
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater/otameta"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	selfupdate "github.com/creativeprojects/go-selfupdate"
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

func TestAssetFilter_RejectsWiderArchOnSamePlatform(t *testing.T) {
	t.Parallel()

	// Regression guard: the filter used to be ^zaparoo-<plat>_<arch> with no
	// trailing separator, so an arm device prefix-matched arm64 archives.
	confusables := []struct{ platform, goarch string }{
		{platform: "mister", goarch: "arm"},
		{platform: "batocera", goarch: "arm"},
		{platform: "libreelec", goarch: "arm"},
	}

	for _, c := range confusables {
		t.Run(c.platform, func(t *testing.T) {
			t.Parallel()

			re := regexp.MustCompile(assetFilter(c.platform, c.goarch))
			assert.True(t, re.MatchString(
				otameta.ArchiveBaseName(c.platform, c.goarch, "2.16.1")+".zip"))
			assert.False(t, re.MatchString(
				otameta.ArchiveBaseName(c.platform, c.goarch+"64", "2.16.1")+".zip"))
		})
	}
}

func TestVerifiedSource_DownloadsNestedValidationAsset(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/checksums.txt.sig", r.URL.Path)
		_, _ = w.Write([]byte("signature"))
	}))
	t.Cleanup(server.Close)

	source := testSource()
	release := testValidationChainRelease(server.URL)

	reader, err := source.DownloadReleaseAsset(t.Context(), release, 3)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, reader.Close())
	}()

	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, []byte("signature"), data)
}

func TestVerifiedSource_DownloadReleaseAssetBranches(t *testing.T) {
	t.Parallel()

	t.Run("nil release returns error", func(t *testing.T) {
		t.Parallel()

		reader, err := testSource().DownloadReleaseAsset(t.Context(), nil, 1)
		require.ErrorIs(t, err, selfupdate.ErrInvalidRelease)
		assert.Nil(t, reader)
	})

	t.Run("downloads primary asset", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/archive.zip", r.URL.Path)
			_, _ = w.Write([]byte("primary"))
		}))
		t.Cleanup(server.Close)

		release := testValidationChainRelease(server.URL)
		release.AssetURL = server.URL + "/archive.zip"

		reader, err := testSource().DownloadReleaseAsset(t.Context(), release, 1)
		require.NoError(t, err)
		defer func() {
			require.NoError(t, reader.Close())
		}()

		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		assert.Equal(t, "primary", string(data))
	})

	t.Run("downloads first validation asset", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/checksums.txt", r.URL.Path)
			_, _ = w.Write([]byte("checksums"))
		}))
		t.Cleanup(server.Close)

		release := testValidationChainRelease(server.URL)
		release.ValidationAssetURL = server.URL + "/checksums.txt"

		reader, err := testSource().DownloadReleaseAsset(t.Context(), release, 2)
		require.NoError(t, err)
		defer func() {
			require.NoError(t, reader.Close())
		}()

		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		assert.Equal(t, "checksums", string(data))
	})

	t.Run("unknown asset returns error", func(t *testing.T) {
		t.Parallel()

		reader, err := testSource().DownloadReleaseAsset(t.Context(), testValidationChainRelease(""), 99)
		require.ErrorIs(t, err, selfupdate.ErrAssetNotFound)
		assert.Nil(t, reader)
	})

	t.Run("empty nested validation URL returns error", func(t *testing.T) {
		t.Parallel()

		release := testValidationChainRelease("")
		release.ValidationChain[1].ValidationAssetURL = ""
		reader, err := testSource().DownloadReleaseAsset(t.Context(), release, 3)
		require.ErrorIs(t, err, selfupdate.ErrAssetNotFound)
		assert.Nil(t, reader)
	})

	t.Run("non-OK nested validation response returns error", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.NotFoundHandler())
		t.Cleanup(server.Close)

		reader, err := testSource().DownloadReleaseAsset(t.Context(), testValidationChainRelease(server.URL), 3)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "status 404")
		assert.Nil(t, reader)
	})
}

func testSource() *verifiedSource {
	return &verifiedSource{
		baseURL:    updateURL,
		transport:  http.DefaultTransport.(*http.Transport).Clone(),
		platformID: "linux",
		goarch:     "amd64",
		key:        &keyRef{},
	}
}

func testValidationChainRelease(serverURL string) *selfupdate.Release {
	return &selfupdate.Release{
		AssetID:           1,
		ValidationAssetID: 2,
		//nolint:govet // Field order is fixed by go-selfupdate's exported Release type.
		ValidationChain: []struct {
			ValidationAssetID                       int64
			ValidationAssetName, ValidationAssetURL string
		}{
			{
				ValidationAssetID:   2,
				ValidationAssetName: "checksums.txt",
				ValidationAssetURL:  serverURL + "/checksums.txt",
			},
			{
				ValidationAssetID:   3,
				ValidationAssetName: "checksums.txt.sig",
				ValidationAssetURL:  serverURL + "/checksums.txt.sig",
			},
		},
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
