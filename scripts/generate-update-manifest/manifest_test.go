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

package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsUpdateArchive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		want bool
	}{
		{name: "zaparoo-linux_amd64-2.16.1.tar.gz", want: true},
		{name: "zaparoo-mister_arm-2.16.1.zip", want: true},
		{name: "zaparoo-amd64-2.16.1-setup.exe", want: false},
		{name: "checksums.txt", want: false},
		{name: "checksums.txt.sig", want: false},
		{name: "manifest.yaml", want: false},
		{name: "zaparoo-batocera-2.16.1.ipk", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isUpdateArchive(tt.name))
		})
	}
}

func TestChannelForPrerelease(t *testing.T) {
	t.Parallel()

	assert.Equal(t, channelStable, channelForPrerelease(false))
	assert.Equal(t, channelBeta, channelForPrerelease(true))
}

// TestNormalizeManifest_BackfillsLegacyFields covers manifests published
// before channels and rollouts existed. Leaving rollout at its zero value
// would read as "held back from every device".
func TestNormalizeManifest_BackfillsLegacyFields(t *testing.T) {
	t.Parallel()

	m := &manifest{
		LastReleaseID: 2,
		LastAssetID:   3,
		Releases: []*release{
			{ID: 1, TagName: "v2.16.0", Prerelease: false},
			{ID: 2, TagName: "v2.17.0-beta1", Prerelease: true},
		},
	}

	normalizeManifest(m)

	assert.Equal(t, channelStable, m.Releases[0].Channel)
	assert.Equal(t, fullRollout, m.Releases[0].Rollout)
	assert.Equal(t, channelBeta, m.Releases[1].Channel)
	assert.Equal(t, fullRollout, m.Releases[1].Rollout)
}

// TestNormalizeManifest_LeavesExplicitRolloutAlone makes sure a deliberate
// rollout of zero survives a load/save round trip rather than being read as
// a legacy entry and reset to 100.
func TestNormalizeManifest_LeavesExplicitRolloutAlone(t *testing.T) {
	t.Parallel()

	m := &manifest{
		Releases: []*release{
			{ID: 1, TagName: "v2.16.0", Channel: channelStable, Rollout: 0},
		},
	}

	normalizeManifest(m)

	assert.Equal(t, 0, m.Releases[0].Rollout)
}

func TestNormalizeManifest_RewritesRelativeArchiveURLs(t *testing.T) {
	t.Parallel()

	m := &manifest{
		Releases: []*release{{
			ID:      1,
			TagName: "v2.16.0",
			Channel: channelStable,
			Rollout: fullRollout,
			Assets: []*asset{
				{ID: 1, Name: "zaparoo-linux_amd64-2.16.0.tar.gz", URL: "zaparoo-linux_amd64-2.16.0.tar.gz"},
				{ID: 2, Name: checksumsName, URL: checksumsName},
			},
		}},
	}

	normalizeManifest(m)

	assert.Equal(t,
		githubReleaseDownloadBase+"/v2.16.0/zaparoo-linux_amd64-2.16.0.tar.gz",
		m.Releases[0].Assets[0].URL,
	)
	assert.Equal(t, checksumsName, m.Releases[0].Assets[1].URL)
}

func TestNormalizeManifest_AddsMissingChecksumSignature(t *testing.T) {
	t.Parallel()

	m := &manifest{
		LastAssetID: 2,
		Releases: []*release{{
			ID:      1,
			TagName: "v2.16.0",
			Channel: channelStable,
			Rollout: fullRollout,
			Assets: []*asset{
				{ID: 1, Name: "zaparoo-linux_amd64-2.16.0.tar.gz", URL: githubReleaseDownloadBase + "/v2.16.0/x"},
				{ID: 2, Name: checksumsName, URL: checksumsName},
			},
		}},
	}

	normalizeManifest(m)

	require.Len(t, m.Releases[0].Assets, 3)
	assert.Equal(t, checksumsSigName, m.Releases[0].Assets[2].Name)
	assert.Equal(t, checksumsSigName, m.Releases[0].Assets[2].URL)
	assert.Equal(t, int64(3), m.LastAssetID)
}

func TestManifestRoundTrip(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	path := filepath.Join("out", manifestName)

	original := newTestManifest(t, []string{"v2.16.0", "v2.17.0-beta1"}, []string{channelStable, channelBeta})
	stampManifest(original, 42, "k1", time.Date(2026, 8, 17, 2, 0, 0, 0, time.UTC))
	require.NoError(t, writeManifest(fs, original, path))

	loaded, err := loadManifest(fs, path)
	require.NoError(t, err)

	assert.Equal(t, currentManifestVersion, loaded.ManifestVersion)
	assert.Equal(t, int64(42), loaded.Generation)
	assert.Equal(t, "k1", loaded.KeyID)
	assert.Equal(t, time.Date(2026, 8, 17, 2, 0, 0, 0, time.UTC), loaded.IssuedAt.UTC())
	assert.Equal(t, tagsOf(original), tagsOf(loaded))
	assert.Equal(t, channelBeta, loaded.Releases[1].Channel)
	assert.True(t, loaded.Releases[1].Prerelease)
	assert.Equal(t,
		original.Releases[0].Assets[0].SHA256,
		loaded.Releases[0].Assets[0].SHA256,
	)
}

func TestLoadManifest_NonexistentFile(t *testing.T) {
	t.Parallel()

	_, err := loadManifest(afero.NewMemMapFs(), filepath.Join("nope", manifestName))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading existing manifest")
}

func TestFindRelease(t *testing.T) {
	t.Parallel()

	m := newTestManifest(t, []string{"v2.16.0", "v2.17.0"}, []string{channelStable, channelStable})

	require.NotNil(t, findRelease(m, "v2.17.0"))
	assert.Equal(t, "v2.17.0", findRelease(m, "v2.17.0").TagName)
	assert.Nil(t, findRelease(m, "v9.9.9"))
}

func TestArchiveAssets_ExcludesMetadata(t *testing.T) {
	t.Parallel()

	m := newTestManifest(t, []string{"v2.16.0"}, []string{channelStable})
	archives := archiveAssets(m.Releases[0])

	assert.Len(t, archives, len(expectedTargets()))
	for _, a := range archives {
		assert.False(t, isMetadataAsset(a.Name))
	}
}
