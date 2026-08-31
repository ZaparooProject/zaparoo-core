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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// archiveContent makes each synthetic archive's bytes unique to its filename
// so digests differ between targets and between versions.
func archiveContent(name string) []byte {
	return []byte("zaparoo archive fixture: " + name)
}

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// writeFullMatrix creates one synthetic archive per expected build target in
// dir and returns the archives as the generator would discover them.
func writeFullMatrix(t *testing.T, fs afero.Fs, dir, tag string) []archiveFile {
	t.Helper()
	require.NoError(t, fs.MkdirAll(dir, 0o750))

	version := versionFromTag(tag)
	targets := expectedTargets()
	files := make([]archiveFile, 0, len(targets))
	for _, target := range targets {
		name := archiveName(target, version)
		content := archiveContent(name)
		require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, name), content, 0o600))
		files = append(files, archiveFile{
			Name:   name,
			SHA256: digestOf(content),
			Size:   int64(len(content)),
		})
	}
	return files
}

// matrixArchives returns a complete set of hashed archives for a tag without
// touching a filesystem, for tests that only exercise manifest operations.
func matrixArchives(t *testing.T, tag string) []archiveFile {
	t.Helper()

	version := versionFromTag(tag)
	targets := expectedTargets()
	files := make([]archiveFile, 0, len(targets))
	for _, target := range targets {
		name := archiveName(target, version)
		content := archiveContent(name)
		files = append(files, archiveFile{
			Name:   name,
			SHA256: digestOf(content),
			Size:   int64(len(content)),
		})
	}
	return files
}

// writeGithubRelease writes release metadata matching the archives, as
// `gh release view --json` would report it.
func writeGithubRelease(
	t *testing.T, fs afero.Fs, path, tag string, prerelease bool, files []archiveFile,
) {
	t.Helper()

	assets := make([]githubAsset, 0, len(files))
	for _, f := range files {
		assets = append(assets, githubAsset{
			Name:   f.Name,
			URL:    githubReleaseDownloadBase + "/" + tag + "/" + f.Name,
			Digest: "sha256:" + f.SHA256,
			Size:   f.Size,
		})
	}

	data, err := json.Marshal(githubRelease{
		TagName:      tag,
		URL:          "https://github.com/ZaparooProject/zaparoo-core/releases/tag/" + tag,
		PublishedAt:  time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
		Assets:       assets,
		IsPrerelease: prerelease,
	})
	require.NoError(t, err)
	require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, afero.WriteFile(fs, path, data, 0o600))
}

// newTestManifest builds a manifest holding the given releases, each with a
// complete asset matrix, published one day apart in the order given.
func newTestManifest(t *testing.T, tags, channels []string) *manifest {
	t.Helper()
	require.Len(t, channels, len(tags))

	m := &manifest{ManifestVersion: currentManifestVersion, Generation: 1, KeyID: "k1"}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, tag := range tags {
		_, err := promote(m, &promoteOptions{
			Tag:          tag,
			Channel:      channels[i],
			Rollout:      fullRollout,
			ReleaseNotes: "notes for " + tag,
			PublishedAt:  base.AddDate(0, 0, i),
			Archives:     matrixArchives(t, tag),
		})
		require.NoError(t, err)
	}
	return m
}

func tagsOf(m *manifest) []string {
	tags := make([]string, 0, len(m.Releases))
	for _, rel := range m.Releases {
		tags = append(tags, rel.TagName)
	}
	return tags
}

func entriesSorted(entries []checksumEntry) bool {
	return sort.SliceIsSorted(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
}
