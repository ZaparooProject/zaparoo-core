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

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeManifestFor marshals a manifest to a path so verify reads the same bytes
// a client would.
func writeManifestFor(t *testing.T, fs afero.Fs, m *manifest, path string) {
	t.Helper()
	require.NoError(t, writeManifest(fs, m, path))
}

func TestVerifySelectionMatrix_FullMatrix(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	m := newTestManifest(t, []string{"v2.15.1", "v2.16.0"}, []string{channelStable, channelBeta})
	writeManifestFor(t, fs, m, "manifest.yaml")

	res, err := verifySelectionMatrix(fs, "manifest.yaml")
	require.NoError(t, err)
	assert.Equal(t, 2, res.releases)
	assert.Equal(t, 2*len(expectedTargets()), res.selections)
}

// The newest release in a channel is what devices are offered, so a gap there
// means some platform sees an update it cannot install.
func TestVerifySelectionMatrix_NewestMissingTargetFails(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	m := newTestManifest(t, []string{"v2.16.0"}, []string{channelStable})

	rel := findRelease(m, "v2.16.0")
	require.NotNil(t, rel)
	kept := make([]*asset, 0, len(rel.Assets))
	for _, a := range rel.Assets {
		if a.Name == archiveName(target{Platform: "mister", Arch: "arm", Ext: "zip"}, "2.16.0") {
			continue
		}
		kept = append(kept, a)
	}
	rel.Assets = kept
	writeManifestFor(t, fs, m, "manifest.yaml")

	_, err := verifySelectionMatrix(fs, "manifest.yaml")
	require.ErrorIs(t, err, errSelectionMatrix)
	assert.Contains(t, err.Error(), "no archive for mister_arm")
}

// An older release legitimately predates platforms that did not exist yet, so
// its gaps are not a promote failure.
func TestVerifySelectionMatrix_OlderMissingTargetIsFine(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	m := newTestManifest(t, []string{"v2.15.1", "v2.16.0"}, []string{channelStable, channelStable})

	old := findRelease(m, "v2.15.1")
	require.NotNil(t, old)
	old.Assets = old.Assets[:1]
	writeManifestFor(t, fs, m, "manifest.yaml")

	res, err := verifySelectionMatrix(fs, "manifest.yaml")
	require.NoError(t, err)
	assert.Equal(t, 2, res.releases)
}

// Two archives one device could both install means the client has to guess, so
// it fails regardless of how old the release is.
func TestVerifySelectionMatrix_AmbiguityFailsOnAnyRelease(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	m := newTestManifest(t, []string{"v2.15.1", "v2.16.0"}, []string{channelStable, channelStable})

	old := findRelease(m, "v2.15.1")
	require.NotNil(t, old)
	old.Assets = append(old.Assets, &asset{
		ID:   9001,
		Name: "zaparoo-linux_amd64-2.15.1.zip",
		URL:  githubReleaseDownloadBase + "/v2.15.1/zaparoo-linux_amd64-2.15.1.zip",
	})
	writeManifestFor(t, fs, m, "manifest.yaml")

	_, err := verifySelectionMatrix(fs, "manifest.yaml")
	require.ErrorIs(t, err, errSelectionMatrix)
	assert.Contains(t, err.Error(), "v2.15.1: linux_amd64")
}

// The defect this check exists for: a manifest entry claiming a version its
// archives are not named for must resolve to nothing installable.
func TestVerifySelectionMatrix_RelabelledVersionFails(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	m := newTestManifest(t, []string{"v2.15.1"}, []string{channelStable})

	rel := findRelease(m, "v2.15.1")
	require.NotNil(t, rel)
	rel.TagName = "v99.0.0"
	rel.Name = "v99.0.0"
	writeManifestFor(t, fs, m, "manifest.yaml")

	_, err := verifySelectionMatrix(fs, "manifest.yaml")
	require.ErrorIs(t, err, errSelectionMatrix)
	assert.Contains(t, err.Error(), "v99.0.0: no archive for")
}

// Each channel has its own newest release, so a gap in the newest beta fails
// even when stable is complete.
func TestVerifySelectionMatrix_ChecksNewestOfEachChannel(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	m := newTestManifest(t,
		[]string{"v2.15.1", "v2.16.0-beta.1"},
		[]string{channelStable, channelBeta})

	beta := findRelease(m, "v2.16.0-beta.1")
	require.NotNil(t, beta)
	beta.Assets = beta.Assets[:2]
	writeManifestFor(t, fs, m, "manifest.yaml")

	_, err := verifySelectionMatrix(fs, "manifest.yaml")
	require.ErrorIs(t, err, errSelectionMatrix)
	assert.Contains(t, err.Error(), "v2.16.0-beta.1: no archive for")
}

func TestVerifySelectionMatrix_MissingFile(t *testing.T) {
	t.Parallel()

	_, err := verifySelectionMatrix(afero.NewMemMapFs(), "nope.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading manifest to verify")
}

func TestVerifySelectionMatrix_Unparseable(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "manifest.yaml", []byte("manifest_version: 99\n"), 0o600))

	_, err := verifySelectionMatrix(fs, "manifest.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing manifest to verify")
}

func TestRun_Verify(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	m := newTestManifest(t, []string{"v2.16.0"}, []string{channelStable})
	m.Generation = 412
	manifestPath := filepath.Join("published", "manifest.yaml")
	writeManifestFor(t, fs, m, manifestPath)

	res, err := run(fs, &options{
		mode:         modeVerify,
		manifestPath: manifestPath,
	}, testNow)
	require.NoError(t, err)
	assert.Equal(t, int64(412), res.generation)
	assert.Equal(t, len(expectedTargets()), res.selections)

	// Verify writes nothing.
	exists, err := afero.Exists(fs, "_publish")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestRun_VerifyRequiresManifest(t *testing.T) {
	t.Parallel()

	_, err := run(afero.NewMemMapFs(), &options{mode: modeVerify}, testNow)
	require.ErrorIs(t, err, errUsage)
	assert.Contains(t, err.Error(), "verify requires --manifest")
}

func TestWriteSummary_Verify(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, writeSummary(fs, "summary.md", modeVerify, &result{
		generation: 412,
		releases:   3,
		selections: 66,
	}))

	data, err := afero.ReadFile(fs, "summary.md")
	require.NoError(t, err)
	assert.Contains(t, string(data), "| Installable selections | 66 |")
	assert.NotContains(t, string(data), "checksums.txt size")
}
