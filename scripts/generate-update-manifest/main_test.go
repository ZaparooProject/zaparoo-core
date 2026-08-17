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
	"strings"
	"testing"
	"time"

	"github.com/creativeprojects/go-selfupdate"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const (
	testOutDir      = "_publish"
	testLiveDir     = "live"
	testArchivesDir = "archives"
)

var testNow = time.Date(2026, 8, 17, 2, 0, 0, 0, time.UTC)

// publishFixture is a filesystem staged as the promote workflow stages it: the
// currently published manifest and checksums, plus freshly downloaded archives.
type publishFixture struct {
	fs           afero.Fs
	manifestPath string
	checksums    string
}

func newPublishFixture(t *testing.T, tags, channels []string) *publishFixture {
	t.Helper()

	f := &publishFixture{
		fs:           afero.NewMemMapFs(),
		manifestPath: filepath.Join(testLiveDir, manifestName),
		checksums:    filepath.Join(testLiveDir, checksumsName),
	}

	// Staged exactly as a previous run of this tool would have published it.
	m := newTestManifest(t, tags, channels)
	checksums := renderChecksums(checksumsFromManifest(m))
	setMetadataAssetSizes(m, int64(len(checksums)))
	stampManifest(m, 400, "k1", testNow.AddDate(0, 0, -14))
	require.NoError(t, writeManifest(f.fs, m, f.manifestPath))
	require.NoError(t, afero.WriteFile(f.fs, f.checksums, checksums, 0o600))

	return f
}

// stageArchives writes the release archives a promote would have downloaded.
func (f *publishFixture) stageArchives(t *testing.T, tag string) []archiveFile {
	t.Helper()
	return writeFullMatrix(t, f.fs, testArchivesDir, tag)
}

func (f *publishFixture) promoteOpts(tag, channel string) *options {
	return &options{
		mode:          modePromote,
		manifestPath:  f.manifestPath,
		checksumsPath: f.checksums,
		outDir:        testOutDir,
		keyID:         "k1",
		retainStable:  5,
		retainBeta:    5,
		notesLimit:    2000,
		tag:           tag,
		channel:       channel,
		rollout:       fullRollout,
		archivesDir:   testArchivesDir,
	}
}

func (f *publishFixture) published(t *testing.T) (*manifest, []checksumEntry) {
	t.Helper()

	m, err := loadManifest(f.fs, filepath.Join(testOutDir, manifestName))
	require.NoError(t, err)

	data, err := afero.ReadFile(f.fs, filepath.Join(testOutDir, checksumsName))
	require.NoError(t, err)
	entries, err := parseChecksums(data)
	require.NoError(t, err)

	return m, entries
}

func TestRun_Promote(t *testing.T) {
	t.Parallel()

	f := newPublishFixture(t, []string{"v2.16.0"}, []string{channelStable})
	f.stageArchives(t, "v2.16.1")

	opts := f.promoteOpts("v2.16.1", channelStable)
	opts.rollout = 25
	opts.minUpgradeFrom = "2.6.0"
	opts.releaseNotes = "the notes"

	res, err := run(f.fs, opts, testNow)
	require.NoError(t, err)

	assert.Equal(t, "v2.16.1", res.tag)
	assert.Equal(t, channelStable, res.channel)
	assert.Equal(t, 25, res.rollout)
	assert.Equal(t, len(expectedTargets()), res.archives)
	assert.Equal(t, int64(401), res.generation)
	assert.Equal(t, 2, res.releases)
	assert.Empty(t, res.dropped)

	m, entries := f.published(t)
	assert.Equal(t, []string{"v2.16.0", "v2.16.1"}, tagsOf(m))
	assert.Equal(t, testNow, m.IssuedAt.UTC())

	rel := findRelease(m, "v2.16.1")
	require.NotNil(t, rel)
	assert.Equal(t, "2.6.0", rel.MinUpgradeFrom)
	assert.Equal(t, "the notes", rel.ReleaseNotes)
	assert.Len(t, entries, 2*len(expectedTargets()))
	assert.Len(t, renderChecksums(entries), res.checksumBytes)
}

// TestRun_PromoteCrossChecksGithub exercises the enrichment path, where the
// release metadata supplies the URL and publish time but never a digest.
func TestRun_PromoteCrossChecksGithub(t *testing.T) {
	t.Parallel()

	f := newPublishFixture(t, []string{"v2.16.0"}, []string{channelStable})
	files := f.stageArchives(t, "v2.17.0-beta1")
	writeGithubRelease(t, f.fs, "release.json", "v2.17.0-beta1", true, files)

	opts := f.promoteOpts("v2.17.0-beta1", channelBeta)
	opts.githubRelease = "release.json"

	_, err := run(f.fs, opts, testNow)
	require.NoError(t, err)

	m, _ := f.published(t)
	rel := findRelease(m, "v2.17.0-beta1")
	require.NotNil(t, rel)
	assert.Equal(t, channelBeta, rel.Channel)
	assert.True(t, rel.Prerelease)
	assert.Equal(t,
		"https://github.com/ZaparooProject/zaparoo-core/releases/tag/v2.17.0-beta1",
		rel.URL,
	)
	assert.Equal(t, time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC), rel.PublishedAt.UTC())
}

// TestRun_PromoteRejectsTamperedArchive is the check that keeps GitHub's
// release API out of the trust base: the local bytes and the published digest
// must agree or nothing is published.
func TestRun_PromoteRejectsTamperedArchive(t *testing.T) {
	t.Parallel()

	f := newPublishFixture(t, []string{"v2.16.0"}, []string{channelStable})
	files := f.stageArchives(t, "v2.16.1")
	writeGithubRelease(t, f.fs, "release.json", "v2.16.1", false, files)

	tampered := filepath.Join(testArchivesDir, files[0].Name)
	require.NoError(t, afero.WriteFile(f.fs, tampered, []byte("swapped payload"), 0o600))

	opts := f.promoteOpts("v2.16.1", channelStable)
	opts.githubRelease = "release.json"

	_, err := run(f.fs, opts, testNow)
	require.ErrorIs(t, err, errDigestMismatch)

	exists, statErr := afero.Exists(f.fs, filepath.Join(testOutDir, manifestName))
	require.NoError(t, statErr)
	assert.False(t, exists, "a failed promote must not write anything to publish")
}

func TestRun_PromoteRetainsNewestReleases(t *testing.T) {
	t.Parallel()

	f := newPublishFixture(t,
		[]string{"v2.13.0", "v2.14.0", "v2.15.0", "v2.16.0"},
		[]string{channelStable, channelStable, channelStable, channelStable},
	)
	f.stageArchives(t, "v2.16.1")

	opts := f.promoteOpts("v2.16.1", channelStable)
	opts.retainStable = 3

	res, err := run(f.fs, opts, testNow)
	require.NoError(t, err)

	assert.Equal(t, []string{"v2.13.0", "v2.14.0"}, res.dropped)

	m, entries := f.published(t)
	assert.Equal(t, []string{"v2.15.0", "v2.16.0", "v2.16.1"}, tagsOf(m))

	// Pruning has to reach checksums.txt too, or a dropped release stays
	// installable through the old signed digest list.
	require.Len(t, entries, 3*len(expectedTargets()))
	for _, e := range entries {
		assert.NotContains(t, e.Name, "2.13.0")
		assert.NotContains(t, e.Name, "2.14.0")
	}
}

// TestRun_PromoteRejectsReleasePrunedByRetention covers promoting a release
// older than everything already retained in its channel. Retention would drop
// it again immediately, so the run would report success while publishing a
// manifest the release is not in.
func TestRun_PromoteRejectsReleasePrunedByRetention(t *testing.T) {
	t.Parallel()

	f := newPublishFixture(t,
		[]string{"v2.17.0", "v2.18.0", "v2.19.0"},
		[]string{channelStable, channelStable, channelStable},
	)

	// The staged manifest dates its releases well before the promoted one, so
	// push them past it to make the promoted release the oldest in the channel.
	live, err := loadManifest(f.fs, f.manifestPath)
	require.NoError(t, err)
	for i, rel := range live.Releases {
		rel.PublishedAt = time.Date(2026, 9, 1+i, 0, 0, 0, 0, time.UTC)
	}
	require.NoError(t, writeManifest(f.fs, live, f.manifestPath))

	files := f.stageArchives(t, "v2.16.1")
	writeGithubRelease(t, f.fs, "release.json", "v2.16.1", false, files)

	opts := f.promoteOpts("v2.16.1", channelStable)
	opts.githubRelease = "release.json"
	opts.retainStable = 3

	_, err = run(f.fs, opts, testNow)
	require.ErrorIs(t, err, errPromotedReleasePruned)

	// Nothing is published, so the live manifest stands.
	exists, statErr := afero.Exists(f.fs, filepath.Join(testOutDir, manifestName))
	require.NoError(t, statErr)
	assert.False(t, exists, "a pruned promote must not publish a manifest")
}

// TestRun_PromoteBackfillsLegacyDigests covers releases promoted before the
// manifest carried per-asset digests: their hashes come from the previously
// published, signature-verified checksums file.
func TestRun_PromoteBackfillsLegacyDigests(t *testing.T) {
	t.Parallel()

	f := newPublishFixture(t, []string{"v2.16.0"}, []string{channelStable})

	legacy, err := loadManifest(f.fs, f.manifestPath)
	require.NoError(t, err)
	for _, a := range archiveAssets(legacy.Releases[0]) {
		a.SHA256 = ""
	}
	require.NoError(t, writeManifest(f.fs, legacy, f.manifestPath))

	f.stageArchives(t, "v2.16.1")
	_, err = run(f.fs, f.promoteOpts("v2.16.1", channelStable), testNow)
	require.NoError(t, err)

	m, _ := f.published(t)
	for _, a := range archiveAssets(findRelease(m, "v2.16.0")) {
		assert.NotEmpty(t, a.SHA256, "%s lost its digest", a.Name)
	}
}

// TestRun_PromoteFailsOnUnverifiableRelease refuses to ship an archive no
// client can check, rather than quietly dropping it from the manifest.
func TestRun_PromoteFailsOnUnverifiableRelease(t *testing.T) {
	t.Parallel()

	f := newPublishFixture(t, []string{"v2.16.0"}, []string{channelStable})

	legacy, err := loadManifest(f.fs, f.manifestPath)
	require.NoError(t, err)
	for _, a := range archiveAssets(legacy.Releases[0]) {
		a.SHA256 = ""
	}
	require.NoError(t, writeManifest(f.fs, legacy, f.manifestPath))

	f.stageArchives(t, "v2.16.1")
	opts := f.promoteOpts("v2.16.1", channelStable)
	opts.checksumsPath = ""

	_, err = run(f.fs, opts, testNow)
	require.ErrorIs(t, err, errMissingDigest)
}

func TestRun_Rollout(t *testing.T) {
	t.Parallel()

	f := newPublishFixture(t,
		[]string{"v2.16.0", "v2.16.1"},
		[]string{channelStable, channelStable},
	)

	opts := f.promoteOpts("v2.16.1", channelStable)
	opts.mode = modeRollout
	opts.rollout = 0
	opts.archivesDir = ""

	res, err := run(f.fs, opts, testNow)
	require.NoError(t, err)

	assert.Equal(t, 0, res.rollout)
	assert.Equal(t, int64(401), res.generation)

	m, entries := f.published(t)
	assert.Equal(t, 0, findRelease(m, "v2.16.1").Rollout)
	assert.Equal(t, fullRollout, findRelease(m, "v2.16.0").Rollout)
	assert.Len(t, entries, 2*len(expectedTargets()),
		"halting a rollout leaves the release installable manually")
}

func TestRun_Withdraw(t *testing.T) {
	t.Parallel()

	f := newPublishFixture(t,
		[]string{"v2.16.0", "v2.16.1"},
		[]string{channelStable, channelStable},
	)

	opts := f.promoteOpts("v2.16.1", channelStable)
	opts.mode = modeWithdraw
	opts.archivesDir = ""

	res, err := run(f.fs, opts, testNow)
	require.NoError(t, err)

	assert.Equal(t, "v2.16.1", res.tag)
	assert.Equal(t, 1, res.releases)

	m, entries := f.published(t)
	assert.Equal(t, []string{"v2.16.0"}, tagsOf(m))
	require.Len(t, entries, len(expectedTargets()))
	for _, e := range entries {
		assert.NotContains(t, e.Name, "2.16.1",
			"a withdrawn release must lose its signed digests as well")
	}
}

// TestRun_PromoteRepairsLegacyManifest covers the first promote after this tool
// ships: the live manifest predates per-asset digests and metadata sizes, so
// the releases carried forward have to pick those up from the signed checksums
// file or requireDigests refuses to publish them.
func TestRun_PromoteRepairsLegacyManifest(t *testing.T) {
	t.Parallel()

	f := newPublishFixture(t,
		[]string{"v2.16.0", "v2.16.1"},
		[]string{channelStable, channelStable},
	)

	legacy, err := loadManifest(f.fs, f.manifestPath)
	require.NoError(t, err)
	for _, rel := range legacy.Releases {
		for _, a := range rel.Assets {
			a.SHA256 = ""
			if isMetadataAsset(a.Name) {
				a.Size = 0
			}
		}
	}
	require.NoError(t, writeManifest(f.fs, legacy, f.manifestPath))

	f.stageArchives(t, "v2.17.0")
	res, err := run(f.fs, f.promoteOpts("v2.17.0", channelStable), testNow)
	require.NoError(t, err)
	assert.Equal(t, int64(401), res.generation)

	m, entries := f.published(t)
	assert.Equal(t, []string{"v2.16.0", "v2.16.1", "v2.17.0"}, tagsOf(m))
	assert.Len(t, entries, 3*len(expectedTargets()))
	for _, rel := range m.Releases {
		for _, a := range archiveAssets(rel) {
			assert.NotEmpty(t, a.SHA256, "%s lost its digest", a.Name)
		}
	}
}

// TestRun_GenerationFloor lets CI raise the counter past a stale cached fetch
// of the live manifest.
func TestRun_GenerationFloor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		floor int64
		want  int64
	}{
		{name: "floor below the natural next value is ignored", floor: 12, want: 401},
		{name: "floor above wins", floor: 5000, want: 5000},
		{name: "unset falls back to increment", floor: 0, want: 401},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newPublishFixture(t, []string{"v2.16.0"}, []string{channelStable})
			f.stageArchives(t, "v2.16.1")

			opts := f.promoteOpts("v2.16.1", channelStable)
			opts.generationFloor = tt.floor

			res, err := run(f.fs, opts, testNow)
			require.NoError(t, err)
			assert.Equal(t, tt.want, res.generation)
		})
	}
}

// TestRun_GenerationAlwaysAdvances is the anti-replay property a client's
// watermark depends on: two publishes never carry the same generation.
func TestRun_GenerationAlwaysAdvances(t *testing.T) {
	t.Parallel()

	f := newPublishFixture(t, []string{"v2.16.0"}, []string{channelStable})
	f.stageArchives(t, "v2.16.1")

	first, err := run(f.fs, f.promoteOpts("v2.16.1", channelStable), testNow)
	require.NoError(t, err)

	// The next run reads what the last one published, as the workflow does.
	f.manifestPath = filepath.Join(testOutDir, manifestName)
	f.checksums = filepath.Join(testOutDir, checksumsName)

	opts := f.promoteOpts("v2.16.1", channelStable)
	opts.rollout = 0
	opts.mode = modeRollout
	opts.archivesDir = ""

	second, err := run(f.fs, opts, testNow)
	require.NoError(t, err)

	assert.Greater(t, second.generation, first.generation)
}

func TestRun_Bootstrap(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeFullMatrix(t, fs, testArchivesDir, "v2.16.1")

	res, err := run(fs, &options{
		mode:         modePromote,
		outDir:       testOutDir,
		keyID:        "k1",
		retainStable: 5,
		retainBeta:   5,
		notesLimit:   2000,
		tag:          "v2.16.1",
		channel:      channelStable,
		rollout:      fullRollout,
		archivesDir:  testArchivesDir,
	}, testNow)
	require.NoError(t, err)

	assert.Equal(t, int64(1), res.generation)
	assert.Equal(t, 1, res.releases)
}

func TestRun_InvalidOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mutate  func(*options)
		name    string
		wantMsg string
	}{
		{
			name:    "promote without a tag",
			mutate:  func(o *options) { o.tag = "" },
			wantMsg: "--tag and --archives-dir",
		},
		{
			name:    "promote without archives",
			mutate:  func(o *options) { o.archivesDir = "" },
			wantMsg: "--tag and --archives-dir",
		},
		{
			name:    "unknown channel",
			mutate:  func(o *options) { o.channel = "nightly" },
			wantMsg: "--channel must be",
		},
		{
			name:    "unknown mode",
			mutate:  func(o *options) { o.mode = "publish" },
			wantMsg: "unknown mode",
		},
		{
			name:    "withdraw without a manifest",
			mutate:  func(o *options) { o.mode = modeWithdraw; o.manifestPath = "" },
			wantMsg: "withdraw requires --manifest",
		},
		{
			name:    "rollout without a tag",
			mutate:  func(o *options) { o.mode = modeRollout; o.tag = "" },
			wantMsg: "rollout requires --tag",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newPublishFixture(t, []string{"v2.16.0"}, []string{channelStable})
			f.stageArchives(t, "v2.16.1")

			opts := f.promoteOpts("v2.16.1", channelStable)
			tt.mutate(opts)

			_, err := run(f.fs, opts, testNow)
			require.ErrorIs(t, err, errUsage)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

// TestRun_ProducedManifestDecodesInGoSelfupdate is the standing compatibility
// guard for the fleet. Every deployed 2.x client parses the published manifest
// with go-selfupdate's HttpManifest, which decodes without KnownFields, so the
// Zaparoo-only fields must be ignored rather than fatal — and nothing the
// generator emits may drop a release or an asset on the way through.
func TestRun_ProducedManifestDecodesInGoSelfupdate(t *testing.T) {
	t.Parallel()

	f := newPublishFixture(t,
		[]string{"v2.16.0", "v2.17.0-beta1"},
		[]string{channelStable, channelBeta},
	)
	f.stageArchives(t, "v2.16.1")

	opts := f.promoteOpts("v2.16.1", channelStable)
	opts.minUpgradeFrom = "2.6.0"
	opts.rollout = 25

	_, err := run(f.fs, opts, testNow)
	require.NoError(t, err)

	data, err := afero.ReadFile(f.fs, filepath.Join(testOutDir, manifestName))
	require.NoError(t, err)

	var legacy selfupdate.HttpManifest
	require.NoError(t, yaml.Unmarshal(data, &legacy),
		"deployed clients would fail to parse the published manifest")

	ours, err := loadManifest(f.fs, filepath.Join(testOutDir, manifestName))
	require.NoError(t, err)

	require.Len(t, legacy.Releases, len(ours.Releases))
	for i, rel := range ours.Releases {
		got := legacy.Releases[i]
		assert.Equal(t, rel.TagName, got.TagName)
		assert.Equal(t, rel.Name, got.Name)
		assert.Equal(t, rel.URL, got.URL)
		assert.Equal(t, rel.ReleaseNotes, got.ReleaseNotes)
		assert.Equal(t, rel.Prerelease, got.Prerelease)
		assert.Equal(t, rel.PublishedAt.UTC(), got.PublishedAt.UTC())

		require.Len(t, got.Assets, len(rel.Assets), "release %s lost assets", rel.TagName)
		for j, a := range rel.Assets {
			assert.Equal(t, a.Name, got.Assets[j].Name)
			assert.Equal(t, a.URL, got.Assets[j].URL)
			assert.Equal(t, int(a.Size), got.Assets[j].Size)
		}
	}
}

func TestWriteSummary(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	path := filepath.Join("summary", "step.md")

	require.NoError(t, writeSummary(fs, path, modePromote, &result{
		tag:           "v2.16.1",
		channel:       channelStable,
		rollout:       25,
		archives:      22,
		generation:    401,
		releases:      3,
		checksumBytes: 4096,
		dropped:       []string{"v2.14.0", "v2.13.0"},
	}))

	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	out := string(data)

	assert.Contains(t, out, "## Update manifest promote")
	assert.Contains(t, out, "| Release | `v2.16.1` |")
	assert.Contains(t, out, "| Rollout | 25% |")
	assert.Contains(t, out, "| Generation | 401 |")
	assert.Contains(t, out, "`v2.13.0`, `v2.14.0`")
	assert.Less(t, strings.Index(out, "v2.13.0"), strings.Index(out, "v2.14.0"),
		"pruned tags are sorted so the summary is reproducible")
}

func TestWriteSummary_NoPathIsANoOp(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, writeSummary(fs, "", modeRollout, &result{generation: 1}))

	names, err := afero.ReadDir(fs, ".")
	require.NoError(t, err)
	assert.Empty(t, names)
}
