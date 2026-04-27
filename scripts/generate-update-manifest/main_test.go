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
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func createAssetFile(t *testing.T, dir, name string, size int) {
	t.Helper()
	err := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0o600)
	require.NoError(t, err)
}

func TestBuildManifest_ValidAssets(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	createAssetFile(t, dir, "zaparoo-linux_amd64.tar.gz", 1024)
	createAssetFile(t, dir, "zaparoo-windows_amd64.zip", 2048)
	createAssetFile(t, dir, "checksums.txt", 256)

	m, err := buildManifest("v2.10.0", dir, "", false, nil)
	require.NoError(t, err)

	require.Len(t, m.Releases, 1)
	assert.Equal(t, "v2.10.0", m.Releases[0].Name)
	assert.Equal(t, "v2.10.0", m.Releases[0].TagName)
	assert.Equal(t, int64(1), m.Releases[0].ID)

	require.Len(t, m.Releases[0].Assets, 3)
	assert.Equal(t, int64(3), m.LastAssetID)
	assert.Equal(t, int64(1), m.LastReleaseID)

	// Verify assets have correct sizes and version-prefixed URLs
	assetsByName := make(map[string]*asset)
	for _, a := range m.Releases[0].Assets {
		assetsByName[a.Name] = a
	}
	assert.Equal(t, int64(1024), assetsByName["zaparoo-linux_amd64.tar.gz"].Size)
	assert.Equal(t, "v2.10.0/zaparoo-linux_amd64.tar.gz", assetsByName["zaparoo-linux_amd64.tar.gz"].URL)
	assert.Equal(t, int64(2048), assetsByName["zaparoo-windows_amd64.zip"].Size)
	assert.Equal(t, "v2.10.0/zaparoo-windows_amd64.zip", assetsByName["zaparoo-windows_amd64.zip"].URL)
	assert.Equal(t, int64(256), assetsByName["checksums.txt"].Size)
	assert.Equal(t, "checksums.txt", assetsByName["checksums.txt"].URL)
}

func TestBuildManifest_IncludesChecksumSignature(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	createAssetFile(t, dir, "zaparoo-linux_amd64.tar.gz", 1024)
	createAssetFile(t, dir, "checksums.txt", 256)
	createAssetFile(t, dir, "checksums.txt.sig", 64)

	m, err := buildManifest("v2.10.0", dir, "", false, nil)
	require.NoError(t, err)

	assetsByName := make(map[string]*asset)
	for _, a := range m.Releases[0].Assets {
		assetsByName[a.Name] = a
	}
	assert.Equal(t, "checksums.txt", assetsByName["checksums.txt"].URL)
	assert.Equal(t, "checksums.txt.sig", assetsByName["checksums.txt.sig"].URL)
}

func TestBuildManifestFromGithubRelease(t *testing.T) {
	t.Parallel()

	archiveURL := githubReleaseDownloadBase + "/v2.11.0/zaparoo-linux_amd64-2.11.0.tar.gz"
	setupURL := githubReleaseDownloadBase + "/v2.11.0/zaparoo-amd64-2.11.0-setup.exe"

	dir := t.TempDir()
	createAssetFile(t, dir, "checksums.txt", 128)
	createAssetFile(t, dir, "checksums.txt.sig", 64)

	publishedAt := time.Date(2026, 4, 27, 1, 2, 3, 0, time.UTC)
	release := &githubRelease{
		TagName:     "v2.11.0",
		URL:         "https://github.com/ZaparooProject/zaparoo-core/releases/tag/v2.11.0",
		PublishedAt: publishedAt,
		Assets: []githubAsset{
			{
				Name: "zaparoo-linux_amd64-2.11.0.tar.gz",
				URL:  archiveURL,
				Size: 1024,
			},
			{
				Name: "zaparoo-amd64-2.11.0-setup.exe",
				URL:  setupURL,
				Size: 2048,
			},
		},
	}

	assets, err := assetsFromGithubRelease(afero.NewOsFs(), release, dir)
	require.NoError(t, err)
	m, err := buildManifestFromAssets("v2.11.0", release.URL, release.PublishedAt, "notes", false, assets, nil)
	require.NoError(t, err)

	require.Len(t, m.Releases, 1)
	releaseManifest := m.Releases[0]
	assert.Equal(t, release.URL, releaseManifest.URL)
	assert.Equal(t, publishedAt, releaseManifest.PublishedAt)
	require.Len(t, releaseManifest.Assets, 3)

	assetsByName := make(map[string]*asset)
	for _, a := range releaseManifest.Assets {
		assetsByName[a.Name] = a
	}
	assert.Equal(t,
		"https://github.com/ZaparooProject/zaparoo-core/releases/download/v2.11.0/zaparoo-linux_amd64-2.11.0.tar.gz",
		assetsByName["zaparoo-linux_amd64-2.11.0.tar.gz"].URL,
	)
	assert.NotContains(t, assetsByName, "zaparoo-amd64-2.11.0-setup.exe")
	assert.Equal(t, "checksums.txt", assetsByName["checksums.txt"].URL)
	assert.Equal(t, "checksums.txt.sig", assetsByName["checksums.txt.sig"].URL)
}

func TestLoadGithubRelease(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	releasePath := "release.json"
	release := githubRelease{
		TagName: "v1.0.0",
		Assets:  []githubAsset{{Name: "zaparoo-linux_amd64-1.0.0.tar.gz", URL: "https://example.com/asset", Size: 100}},
	}
	data, err := json.Marshal(release)
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, releasePath, data, 0o600))

	loaded, err := loadGithubRelease(fs, releasePath)
	require.NoError(t, err)
	assert.Equal(t, "v1.0.0", loaded.TagName)
	require.Len(t, loaded.Assets, 1)
}

func TestBuildManifest_OnlyMetadataFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	createAssetFile(t, dir, "checksums.txt", 256)
	createAssetFile(t, dir, "checksums.txt.sig", 64)

	m, err := buildManifest("v1.0.0", dir, "", false, nil)
	require.ErrorIs(t, err, errNoAssets)
	assert.Nil(t, m)
}

func TestBuildManifestFromAssets_OnlyMetadataFiles(t *testing.T) {
	t.Parallel()

	assets := []releaseAsset{
		{Name: "checksums.txt", URL: "checksums.txt", Size: 256},
		{Name: "checksums.txt.sig", URL: "checksums.txt.sig", Size: 64},
	}

	m, err := buildManifestFromAssets("v1.0.0", "", time.Time{}, "", false, assets, nil)
	require.ErrorIs(t, err, errNoAssets)
	assert.Nil(t, m)
}

func TestBuildManifest_SkipsNonAssetFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	createAssetFile(t, dir, "zaparoo-linux_amd64.tar.gz", 100)
	createAssetFile(t, dir, "README.md", 50)
	createAssetFile(t, dir, "random-file.txt", 50)

	m, err := buildManifest("v1.0.0", dir, "", false, nil)
	require.NoError(t, err)

	require.Len(t, m.Releases[0].Assets, 1)
	assert.Equal(t, "zaparoo-linux_amd64.tar.gz", m.Releases[0].Assets[0].Name)
}

func TestBuildManifest_SkipsManifestYAML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	createAssetFile(t, dir, "zaparoo-linux_amd64.tar.gz", 100)
	createAssetFile(t, dir, "manifest.yaml", 500)

	m, err := buildManifest("v1.0.0", dir, "", false, nil)
	require.NoError(t, err)

	require.Len(t, m.Releases[0].Assets, 1)
	assert.Equal(t, "zaparoo-linux_amd64.tar.gz", m.Releases[0].Assets[0].Name)
}

func TestBuildManifest_SkipsDirectories(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	createAssetFile(t, dir, "zaparoo-linux_amd64.tar.gz", 100)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "zaparoo-subdir"), 0o750))

	m, err := buildManifest("v1.0.0", dir, "", false, nil)
	require.NoError(t, err)

	require.Len(t, m.Releases[0].Assets, 1)
}

func TestBuildManifest_EmptyDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	m, err := buildManifest("v1.0.0", dir, "", false, nil)
	require.ErrorIs(t, err, errNoAssets)
	assert.Nil(t, m)
}

func TestBuildManifest_OnlyNonAssetFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	createAssetFile(t, dir, "README.md", 50)
	createAssetFile(t, dir, "manifest.yaml", 500)

	m, err := buildManifest("v1.0.0", dir, "", false, nil)
	require.ErrorIs(t, err, errNoAssets)
	assert.Nil(t, m)
}

func TestBuildManifest_NonexistentDirectory(t *testing.T) {
	t.Parallel()

	m, err := buildManifest("v1.0.0", "/nonexistent/path", "", false, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading assets directory")
	assert.Nil(t, m)
}

func TestBuildManifest_AssetURLIncludesVersion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	createAssetFile(t, dir, "zaparoo-mister_arm.tar.gz", 100)

	m, err := buildManifest("v1.0.0", dir, "", false, nil)
	require.NoError(t, err)

	assert.Equal(t, "v1.0.0/zaparoo-mister_arm.tar.gz", m.Releases[0].Assets[0].URL)
}

func TestBuildManifest_ReleaseNotes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	createAssetFile(t, dir, "zaparoo-linux_amd64.tar.gz", 100)

	notes := "## What's New\n- Added self-update support"
	m, err := buildManifest("v2.10.0", dir, notes, false, nil)
	require.NoError(t, err)

	assert.Equal(t, notes, m.Releases[0].ReleaseNotes)
}

func TestBuildManifest_EmptyReleaseNotes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	createAssetFile(t, dir, "zaparoo-linux_amd64.tar.gz", 100)

	m, err := buildManifest("v1.0.0", dir, "", false, nil)
	require.NoError(t, err)

	assert.Empty(t, m.Releases[0].ReleaseNotes)
}

func TestWriteManifest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputPath := filepath.Join(dir, "manifest.yaml")

	m := &manifest{
		LastReleaseID: 1,
		LastAssetID:   1,
		Releases: []*release{
			{
				ID:      1,
				Name:    "v1.0.0",
				TagName: "v1.0.0",
				Assets: []*asset{
					{ID: 1, Name: "zaparoo-test.tar.gz", Size: 100, URL: "zaparoo-test.tar.gz"},
				},
			},
		},
	}

	err := writeManifest(m, outputPath)
	require.NoError(t, err)

	data, err := os.ReadFile(outputPath) //nolint:gosec // Test file with controlled path
	require.NoError(t, err)

	var parsed manifest
	require.NoError(t, yaml.Unmarshal(data, &parsed))

	assert.Equal(t, int64(1), parsed.LastReleaseID)
	assert.Equal(t, int64(1), parsed.LastAssetID)
	require.Len(t, parsed.Releases, 1)
	assert.Equal(t, "v1.0.0", parsed.Releases[0].TagName)
	require.Len(t, parsed.Releases[0].Assets, 1)
	assert.Equal(t, "zaparoo-test.tar.gz", parsed.Releases[0].Assets[0].Name)
}

func TestWriteManifest_CreatesSubdirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	outputPath := filepath.Join(dir, "sub", "dir", "manifest.yaml")

	m := &manifest{
		LastReleaseID: 1,
		LastAssetID:   1,
		Releases: []*release{
			{
				ID:      1,
				Name:    "v1.0.0",
				TagName: "v1.0.0",
				Assets: []*asset{
					{ID: 1, Name: "test.tar.gz", Size: 100, URL: "test.tar.gz"},
				},
			},
		},
	}

	err := writeManifest(m, outputPath)
	require.NoError(t, err)

	_, err = os.Stat(outputPath)
	require.NoError(t, err)
}

func TestBuildManifest_Prerelease(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	createAssetFile(t, dir, "zaparoo-linux_amd64.tar.gz", 1024)

	m, err := buildManifest("v2.11.0-beta1", dir, "Beta release", true, nil)
	require.NoError(t, err)

	require.Len(t, m.Releases, 1)
	assert.True(t, m.Releases[0].Prerelease)
	assert.Equal(t, "v2.11.0-beta1", m.Releases[0].TagName)
	assert.Equal(t, "Beta release", m.Releases[0].ReleaseNotes)
}

func TestBuildManifest_MergeWithExisting(t *testing.T) {
	t.Parallel()

	existing := &manifest{
		LastReleaseID: 1,
		LastAssetID:   2,
		Releases: []*release{
			{
				ID:      1,
				Name:    "v2.10.0",
				TagName: "v2.10.0",
				Assets: []*asset{
					{ID: 1, Name: "zaparoo-linux_amd64.tar.gz", Size: 1024, URL: "v2.10.0/zaparoo-linux_amd64.tar.gz"},
					{ID: 2, Name: "checksums.txt", Size: 256, URL: "checksums.txt"},
				},
			},
		},
	}

	dir := t.TempDir()
	createAssetFile(t, dir, "zaparoo-linux_amd64.tar.gz", 2048)
	createAssetFile(t, dir, "checksums.txt", 512)

	m, err := buildManifest("v2.11.0-beta1", dir, "Beta", true, existing)
	require.NoError(t, err)

	// Should have both releases.
	require.Len(t, m.Releases, 2)

	// First release is the existing stable.
	assert.Equal(t, "v2.10.0", m.Releases[0].TagName)
	assert.Equal(t, int64(1), m.Releases[0].ID)
	assert.False(t, m.Releases[0].Prerelease)

	// Second release is the new prerelease.
	assert.Equal(t, "v2.11.0-beta1", m.Releases[1].TagName)
	assert.Equal(t, int64(2), m.Releases[1].ID)
	assert.True(t, m.Releases[1].Prerelease)

	// IDs should continue from existing.
	assert.Equal(t, int64(2), m.LastReleaseID)
	assert.Equal(t, int64(4), m.LastAssetID) // 2 existing + 2 new
	assert.Equal(t, int64(3), m.Releases[1].Assets[0].ID)
}

func TestLoadManifest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")

	original := &manifest{
		LastReleaseID: 1,
		LastAssetID:   1,
		Releases: []*release{
			{
				ID:      1,
				Name:    "v2.10.0",
				TagName: "v2.10.0",
				Assets: []*asset{
					{ID: 1, Name: "test.tar.gz", Size: 100, URL: "v2.10.0/test.tar.gz"},
				},
			},
		},
	}

	require.NoError(t, writeManifest(original, manifestPath))

	loaded, err := loadManifest(manifestPath)
	require.NoError(t, err)

	assert.Equal(t, int64(1), loaded.LastReleaseID)
	assert.Equal(t, int64(1), loaded.LastAssetID)
	require.Len(t, loaded.Releases, 1)
	assert.Equal(t, "v2.10.0", loaded.Releases[0].TagName)
}

func TestLoadManifest_NormalizesArchiveURLsToGitHub(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")

	original := &manifest{
		LastReleaseID: 1,
		LastAssetID:   2,
		Releases: []*release{
			{
				ID:      1,
				Name:    "v2.10.0",
				TagName: "v2.10.0",
				Assets: []*asset{
					{
						ID:   1,
						Name: "zaparoo-linux_amd64-2.10.0.tar.gz",
						Size: 100,
						URL:  "zaparoo-linux_amd64-2.10.0.tar.gz",
					},
					{ID: 2, Name: "checksums.txt", Size: 100, URL: "checksums.txt"},
				},
			},
		},
	}

	require.NoError(t, writeManifest(original, manifestPath))

	loaded, err := loadManifest(manifestPath)
	require.NoError(t, err)
	require.Len(t, loaded.Releases, 1)
	require.Len(t, loaded.Releases[0].Assets, 3)
	assert.Equal(t,
		"https://github.com/ZaparooProject/zaparoo-core/releases/download/v2.10.0/zaparoo-linux_amd64-2.10.0.tar.gz",
		loaded.Releases[0].Assets[0].URL,
	)
	assert.Equal(t, "checksums.txt", loaded.Releases[0].Assets[1].URL)
	assert.Equal(t, int64(3), loaded.Releases[0].Assets[2].ID)
	assert.Equal(t, "checksums.txt.sig", loaded.Releases[0].Assets[2].Name)
	assert.Equal(t, "checksums.txt.sig", loaded.Releases[0].Assets[2].URL)
	assert.Equal(t, int64(3), loaded.LastAssetID)
}

func TestLoadManifest_NonexistentFile(t *testing.T) {
	t.Parallel()

	_, err := loadManifest("/nonexistent/manifest.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading existing manifest")
}
