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
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/inbox"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	selfupdate "github.com/creativeprojects/go-selfupdate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type stubSource struct{}

func (stubSource) ListReleases(context.Context, selfupdate.Repository) ([]selfupdate.SourceRelease, error) {
	return nil, nil
}

func (stubSource) DownloadReleaseAsset(context.Context, *selfupdate.Release, int64) (io.ReadCloser, error) {
	return nil, selfupdate.ErrAssetNotFound
}

func TestCheck_DevelopmentVersion(t *testing.T) {
	devVersions := []string{"DEVELOPMENT", "abc1234-dev"}

	for _, v := range devVersions {
		t.Run(v, func(t *testing.T) {
			original := config.AppVersion
			config.AppVersion = v
			t.Cleanup(func() { config.AppVersion = original })

			result, err := Check(t.Context(), "linux", "stable")
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

			version, err := Apply(t.Context(), "linux", "stable")
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
	CheckAndNotify(t.Context(), cfg, "linux", nil, func(_ context.Context, _ int) bool {
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
	CheckAndNotify(t.Context(), cfg, "linux", nil, func(_ context.Context, _ int) bool {
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

	CheckAndNotify(t.Context(), cfg, "linux", nil, alwaysOnline, Check, false)
}

func TestCheckAndNotify_NoInternet(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	cfg.SetAutoUpdate(true)

	CheckAndNotify(t.Context(), cfg, "linux", nil, func(_ context.Context, _ int) bool {
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

	checkFn := func(_ context.Context, _, _ string) (*Result, error) {
		return &Result{
			CurrentVersion:  "2.9.0",
			LatestVersion:   "2.10.0",
			UpdateAvailable: true,
			ReleaseNotes:    "New features",
		}, nil
	}

	CheckAndNotify(t.Context(), cfg, "linux", inboxSvc, alwaysOnline, checkFn, false)

	mockUserDB.AssertExpectations(t)
}

func TestCheckAndNotify_BetaChannel(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	cfg.SetAutoUpdate(true)
	cfg.SetUpdateChannel(config.UpdateChannelBeta)

	var receivedChannel string
	checkFn := func(_ context.Context, _, channel string) (*Result, error) {
		receivedChannel = channel
		return &Result{
			CurrentVersion:  "2.10.0",
			LatestVersion:   "2.10.0",
			UpdateAvailable: false,
		}, nil
	}

	CheckAndNotify(t.Context(), cfg, "linux", nil, alwaysOnline, checkFn, false)

	assert.Equal(t, "beta", receivedChannel)
}

func TestCheckAndNotify_NoUpdateAvailable(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	cfg.SetAutoUpdate(true)

	checkFn := func(_ context.Context, _, _ string) (*Result, error) {
		return &Result{
			CurrentVersion:  "2.10.0",
			LatestVersion:   "2.10.0",
			UpdateAvailable: false,
		}, nil
	}

	// inboxSvc is nil — would panic if code tried to post a message
	CheckAndNotify(t.Context(), cfg, "linux", nil, alwaysOnline, checkFn, false)
}

func TestCheckAndNotify_CheckError(t *testing.T) {
	t.Parallel()

	cfg := &config.Instance{}
	cfg.SetAutoUpdate(true)

	checkFn := func(_ context.Context, _, _ string) (*Result, error) {
		return nil, errors.New("network timeout")
	}

	// inboxSvc is nil — would panic if code tried to post a message
	CheckAndNotify(t.Context(), cfg, "linux", nil, alwaysOnline, checkFn, false)
}

func TestCheck_CancelledContext(t *testing.T) {
	original := config.AppVersion
	config.AppVersion = "1.0.0"
	t.Cleanup(func() { config.AppVersion = original })

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	result, err := Check(ctx, "linux", "stable")
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestApply_CancelledContext(t *testing.T) {
	original := config.AppVersion
	config.AppVersion = "1.0.0"
	t.Cleanup(func() { config.AppVersion = original })

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	version, err := Apply(ctx, "linux", "stable")
	require.Error(t, err)
	assert.Empty(t, version)
}

func TestValidationChainHTTPSource_DownloadsNestedValidationAsset(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/checksums.txt.sig", r.URL.Path)
		_, _ = w.Write([]byte("signature"))
	}))
	t.Cleanup(server.Close)

	source := &validationChainHTTPSource{source: stubSource{}, transport: http.DefaultTransport.(*http.Transport).Clone()}
	release := &selfupdate.Release{
		AssetID:           1,
		ValidationAssetID: 2,
		ValidationChain: []struct {
			ValidationAssetID                       int64
			ValidationAssetName, ValidationAssetURL string
		}{
			{ValidationAssetID: 2, ValidationAssetName: "checksums.txt", ValidationAssetURL: server.URL + "/checksums.txt"},
			{ValidationAssetID: 3, ValidationAssetName: "checksums.txt.sig", ValidationAssetURL: server.URL + "/checksums.txt.sig"},
		},
	}

	reader, err := source.DownloadReleaseAsset(t.Context(), release, 3)
	require.NoError(t, err)
	defer reader.Close()

	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, []byte("signature"), data)
}
