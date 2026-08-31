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

func TestScanArchives_HashesEveryArchive(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	dir := "archives"
	want := writeFullMatrix(t, fs, dir, "v2.16.1")
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, checksumsName), []byte("ignored"), 0o600))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "zaparoo-setup.exe"), []byte("ignored"), 0o600))

	got, err := scanArchives(fs, dir)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestScanArchives_NoArchives(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("empty", 0o750))

	_, err := scanArchives(fs, "empty")
	require.ErrorIs(t, err, errNoAssets)
}

func TestScanArchives_MissingDirectory(t *testing.T) {
	t.Parallel()

	_, err := scanArchives(afero.NewMemMapFs(), "nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading archives directory")
}

func TestCrossCheckGithubDigests_Matches(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	files := writeFullMatrix(t, fs, "archives", "v2.16.1")
	writeGithubRelease(t, fs, "release.json", "v2.16.1", false, files)

	rel, err := loadGithubRelease(fs, "release.json")
	require.NoError(t, err)
	require.NoError(t, crossCheckGithubDigests(rel, files))
}

func TestCrossCheckGithubDigests_Failures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mutate  func(rel *githubRelease, files []archiveFile) (*githubRelease, []archiveFile)
		name    string
		wantMsg string
	}{
		{
			name: "digest mismatch",
			mutate: func(rel *githubRelease, files []archiveFile) (*githubRelease, []archiveFile) {
				rel.Assets[0].Digest = "sha256:" + digestOf([]byte("tampered"))
				return rel, files
			},
			wantMsg: "local file hashes to",
		},
		{
			name: "github digest absent",
			mutate: func(rel *githubRelease, files []archiveFile) (*githubRelease, []archiveFile) {
				rel.Assets[0].Digest = ""
				return rel, files
			},
			wantMsg: "has no sha256 digest",
		},
		{
			name: "size disagrees",
			mutate: func(rel *githubRelease, files []archiveFile) (*githubRelease, []archiveFile) {
				rel.Assets[0].Size = 999999
				return rel, files
			},
			wantMsg: "bytes, local file is",
		},
		{
			name: "archive not downloaded",
			mutate: func(rel *githubRelease, files []archiveFile) (*githubRelease, []archiveFile) {
				return rel, files[1:]
			},
			wantMsg: "was not downloaded",
		},
		{
			name: "archive not in release",
			mutate: func(rel *githubRelease, files []archiveFile) (*githubRelease, []archiveFile) {
				rel.Assets = rel.Assets[1:]
				return rel, files
			},
			wantMsg: "not present in the GitHub release",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			files := writeFullMatrix(t, fs, "archives", "v2.16.1")
			writeGithubRelease(t, fs, "release.json", "v2.16.1", false, files)
			rel, err := loadGithubRelease(fs, "release.json")
			require.NoError(t, err)

			rel, files = tt.mutate(rel, files)

			err = crossCheckGithubDigests(rel, files)
			require.ErrorIs(t, err, errDigestMismatch)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestValidateGithubReleaseMetadata(t *testing.T) {
	t.Parallel()

	publishedAt := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	valid := func() *githubRelease {
		return &githubRelease{
			TagName:     "v2.16.1",
			URL:         "https://github.com/ZaparooProject/zaparoo-core/releases/tag/v2.16.1",
			PublishedAt: publishedAt,
		}
	}

	tests := []struct {
		mutate  func(*githubRelease)
		name    string
		channel string
		wantErr string
	}{
		{name: "valid stable", channel: channelStable, mutate: func(*githubRelease) {}},
		{
			name: "valid beta", channel: channelBeta,
			mutate: func(r *githubRelease) { r.IsPrerelease = true },
		},
		{
			name: "missing tag", channel: channelStable,
			mutate: func(r *githubRelease) { r.TagName = "" }, wantErr: "missing tagName",
		},
		{
			name: "tag mismatch", channel: channelStable,
			mutate: func(r *githubRelease) { r.TagName = "v2.16.2" }, wantErr: "does not match version",
		},
		{
			name: "missing url", channel: channelStable,
			mutate: func(r *githubRelease) { r.URL = "" }, wantErr: "missing url",
		},
		{
			name: "missing published at", channel: channelStable,
			mutate: func(r *githubRelease) { r.PublishedAt = time.Time{} }, wantErr: "missing publishedAt",
		},
		{
			name: "draft", channel: channelStable,
			mutate: func(r *githubRelease) { r.IsDraft = true }, wantErr: "is a draft",
		},
		{
			name: "prerelease promoted to stable", channel: channelStable,
			mutate: func(r *githubRelease) { r.IsPrerelease = true }, wantErr: "belongs in the beta channel",
		},
		{
			name: "stable promoted to beta", channel: channelBeta,
			mutate: func(*githubRelease) {}, wantErr: "belongs in the stable channel",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rel := valid()
			tt.mutate(rel)

			err := validateGithubReleaseMetadata(rel, "v2.16.1", tt.channel)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestBuildReleaseAssets(t *testing.T) {
	t.Parallel()

	files := []archiveFile{
		{Name: "zaparoo-linux_amd64-2.16.1.tar.gz", SHA256: "aa", Size: 10},
		{Name: "zaparoo-mister_arm-2.16.1.zip", SHA256: "bb", Size: 20},
	}

	assets, lastID := buildReleaseAssets("v2.16.1", files, 7)

	require.Len(t, assets, 4)
	assert.Equal(t, int64(11), lastID)
	assert.Equal(t, int64(8), assets[0].ID)
	assert.Equal(t,
		githubReleaseDownloadBase+"/v2.16.1/zaparoo-linux_amd64-2.16.1.tar.gz",
		assets[0].URL,
	)
	assert.Equal(t, "aa", assets[0].SHA256)
	assert.Equal(t, checksumsName, assets[2].Name)
	assert.Equal(t, checksumsName, assets[2].URL)
	assert.Empty(t, assets[2].SHA256)
	assert.Equal(t, checksumsSigName, assets[3].Name)
}
