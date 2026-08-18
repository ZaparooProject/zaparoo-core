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
	"regexp"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
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

func TestCheckAndNotify_ManagedInstallDefaultsOff(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{} // AutoUpdate is nil

	waitCalled := false
	CheckAndNotify(t.Context(), cfg, linuxOptions(), nil, func(_ context.Context, _ int) bool {
		waitCalled = true
		return true
	}, Check, true)

	assert.False(t, waitCalled)
}

func TestCheckAndNotify_DisabledConfig(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	cfg.SetAutoUpdate(false)

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
	cfg.SetAutoUpdate(true)

	CheckAndNotify(t.Context(), cfg, linuxOptions(), nil, alwaysOnline, Check, false)
}

func TestCheckAndNotify_NoInternet(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	cfg.SetAutoUpdate(true)

	CheckAndNotify(t.Context(), cfg, linuxOptions(), nil, func(_ context.Context, _ int) bool {
		return false
	}, Check, false)
}

func TestCheckAndNotify_UpdateAvailable(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	cfg.SetAutoUpdate(true)

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

func TestCheckAndNotify_BetaChannel(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	cfg.SetAutoUpdate(true)
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
	cfg.SetAutoUpdate(true)

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
	cfg.SetAutoUpdate(true)

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

	version, err := Apply(ctx, linuxOptions())
	require.Error(t, err)
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
