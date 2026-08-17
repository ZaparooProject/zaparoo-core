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
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStampManifest(t *testing.T) {
	t.Parallel()

	m := &manifest{}
	now := time.Date(2026, 8, 17, 2, 30, 45, 123456789, time.UTC)

	stampManifest(m, 412, "k2", now)

	assert.Equal(t, currentManifestVersion, m.ManifestVersion)
	assert.Equal(t, int64(412), m.Generation)
	assert.Equal(t, "k2", m.KeyID)
	assert.Equal(t, time.Date(2026, 8, 17, 2, 30, 45, 0, time.UTC), m.IssuedAt,
		"sub-second precision is dropped so the stamp round-trips through YAML unchanged")
}

// TestStampManifest_NormalizesToUTC keeps the published bytes independent of
// the runner's timezone, so two publishes of the same content differ only
// where they are meant to.
func TestStampManifest_NormalizesToUTC(t *testing.T) {
	t.Parallel()

	zone := time.FixedZone("AEST", 10*60*60)
	m := &manifest{}

	stampManifest(m, 1, "k1", time.Date(2026, 8, 17, 12, 0, 0, 0, zone))

	assert.Equal(t, time.UTC, m.IssuedAt.Location())
	assert.Equal(t, time.Date(2026, 8, 17, 2, 0, 0, 0, time.UTC), m.IssuedAt)
}

func TestValidateGeneration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		previous int64
		next     int64
		wantErr  bool
	}{
		{name: "advances", previous: 411, next: 412},
		{name: "jumps ahead", previous: 411, next: 900},
		{name: "bootstrap", previous: 0, next: 1},
		{name: "identical", previous: 412, next: 412, wantErr: true},
		{name: "rolls back", previous: 412, next: 411, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateGeneration(tt.previous, tt.next)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, errGenerationNotAdvanced)
		})
	}
}

// TestPromote_RejectsIncompleteMatrix stops a release that failed to build for
// one platform from being published as if it were complete.
func TestPromote_RejectsIncompleteMatrix(t *testing.T) {
	t.Parallel()

	m := &manifest{}
	files := []archiveFile{{Name: "zaparoo-linux_amd64-2.16.1.tar.gz", SHA256: "aa", Size: 1}}

	rel, err := promote(m, &promoteOptions{
		Tag:         "v2.16.1",
		Channel:     channelStable,
		Rollout:     fullRollout,
		PublishedAt: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
		Archives:    files,
	})

	require.Error(t, err)
	assert.Nil(t, rel)
	assert.Contains(t, err.Error(), "missing archives")
	assert.Empty(t, m.Releases, "a rejected promote must not mutate the manifest")
}

func TestPromote_AddsRelease(t *testing.T) {
	t.Parallel()

	m := &manifest{}
	rel, err := promote(m, &promoteOptions{
		Tag:            "v2.16.1",
		Channel:        channelBeta,
		Rollout:        25,
		MinUpgradeFrom: "2.6.0",
		ReleaseNotes:   "notes",
		PublishedAt:    time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
		Archives:       matrixArchives(t, "v2.16.1"),
	})
	require.NoError(t, err)

	require.Len(t, m.Releases, 1)
	assert.Equal(t, "v2.16.1", rel.TagName)
	assert.Equal(t, "v2.16.1", rel.Name)
	assert.Equal(t, channelBeta, rel.Channel)
	assert.True(t, rel.Prerelease, "channel is the source of truth for the prerelease flag")
	assert.Equal(t, 25, rel.Rollout)
	assert.Equal(t, "2.6.0", rel.MinUpgradeFrom)
	assert.Equal(t, int64(1), rel.ID)
	assert.Len(t, rel.Assets, len(expectedTargets())+2)
	assert.Equal(t, int64(len(expectedTargets())+2), m.LastAssetID)
}

func TestPromote_RejectsDuplicateTag(t *testing.T) {
	t.Parallel()

	m := newTestManifest(t, []string{"v2.16.1"}, []string{channelStable})

	_, err := promote(m, &promoteOptions{
		Tag:      "v2.16.1",
		Channel:  channelStable,
		Rollout:  fullRollout,
		Archives: matrixArchives(t, "v2.16.1"),
	})

	require.ErrorIs(t, err, errReleaseExists)
	assert.Len(t, m.Releases, 1)
}

// TestPromote_ReplaceKeepsReleaseID means a re-promote (say to correct the
// rollout or notes) does not look like a brand new release to a client that
// tracks release IDs.
func TestPromote_ReplaceKeepsReleaseID(t *testing.T) {
	t.Parallel()

	m := newTestManifest(t, []string{"v2.16.0", "v2.16.1"}, []string{channelStable, channelStable})
	originalID := findRelease(m, "v2.16.0").ID

	rel, err := promote(m, &promoteOptions{
		Tag:          "v2.16.0",
		Channel:      channelStable,
		Rollout:      10,
		ReleaseNotes: "corrected notes",
		Archives:     matrixArchives(t, "v2.16.0"),
		Replace:      true,
	})
	require.NoError(t, err)

	assert.Equal(t, originalID, rel.ID)
	assert.Equal(t, 10, rel.Rollout)
	assert.Equal(t, "corrected notes", rel.ReleaseNotes)
	assert.Equal(t, []string{"v2.16.0", "v2.16.1"}, tagsOf(m), "replace keeps position")
	assert.Len(t, m.Releases, 2)
}

func TestPromote_RejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantErr error
		name    string
		archive bool
		rollout int
	}{
		{name: "rollout above 100", rollout: 101, archive: true, wantErr: errInvalidRollout},
		{name: "negative rollout", rollout: -1, archive: true, wantErr: errInvalidRollout},
		{name: "no archives", rollout: fullRollout, archive: false, wantErr: errNoAssets},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var archives []archiveFile
			if tt.archive {
				archives = matrixArchives(t, "v2.16.1")
			}

			m := &manifest{}
			_, err := promote(m, &promoteOptions{
				Tag:      "v2.16.1",
				Channel:  channelStable,
				Rollout:  tt.rollout,
				Archives: archives,
			})

			require.ErrorIs(t, err, tt.wantErr)
			assert.Empty(t, m.Releases)
		})
	}
}

func TestSetRollout(t *testing.T) {
	t.Parallel()

	m := newTestManifest(t, []string{"v2.16.1"}, []string{channelStable})

	rel, err := setRollout(m, "v2.16.1", 0)
	require.NoError(t, err)
	assert.Equal(t, 0, rel.Rollout)
	assert.Equal(t, 0, m.Releases[0].Rollout, "the halt must land on the manifest, not a copy")

	_, err = setRollout(m, "v2.16.1", 101)
	require.ErrorIs(t, err, errInvalidRollout)

	_, err = setRollout(m, "v9.9.9", 50)
	require.ErrorIs(t, err, errNoSuchRelease)
}

func TestWithdraw(t *testing.T) {
	t.Parallel()

	m := newTestManifest(t,
		[]string{"v2.16.0", "v2.16.1"},
		[]string{channelStable, channelStable},
	)

	rel, err := withdraw(m, "v2.16.1")
	require.NoError(t, err)
	assert.Equal(t, "v2.16.1", rel.TagName)
	assert.Equal(t, []string{"v2.16.0"}, tagsOf(m))

	_, err = withdraw(m, "v2.16.1")
	require.ErrorIs(t, err, errNoSuchRelease)
}

// TestWithdraw_RefusesToEmptyManifest guards against a withdraw leaving clients
// with a manifest offering nothing at all.
func TestWithdraw_RefusesToEmptyManifest(t *testing.T) {
	t.Parallel()

	m := newTestManifest(t, []string{"v2.16.1"}, []string{channelStable})

	_, err := withdraw(m, "v2.16.1")
	require.ErrorIs(t, err, errManifestWouldBeEmpty)
	assert.Len(t, m.Releases, 1)
}

func TestApplyRetention_KeepsNewestPerChannel(t *testing.T) {
	t.Parallel()

	tags := []string{
		"v2.10.0", "v2.11.0-beta1", "v2.11.0", "v2.12.0-beta1",
		"v2.12.0", "v2.13.0-beta1", "v2.13.0",
	}
	channels := []string{
		channelStable, channelBeta, channelStable, channelBeta,
		channelStable, channelBeta, channelStable,
	}
	m := newTestManifest(t, tags, channels)

	dropped := applyRetention(m, 2, 1, 0)

	assert.Equal(t, []string{"v2.10.0", "v2.11.0", "v2.11.0-beta1", "v2.12.0-beta1"}, dropped)
	assert.Equal(t, []string{"v2.12.0", "v2.13.0-beta1", "v2.13.0"}, tagsOf(m),
		"surviving releases keep their original order")
}

func TestApplyRetention_TruncatesSupersededNotes(t *testing.T) {
	t.Parallel()

	m := newTestManifest(t,
		[]string{"v2.16.0", "v2.16.1", "v2.17.0-beta1"},
		[]string{channelStable, channelStable, channelBeta},
	)
	long := strings.Repeat("a", 4000)
	for _, rel := range m.Releases {
		rel.ReleaseNotes = long
	}

	dropped := applyRetention(m, 5, 5, 100)

	assert.Empty(t, dropped)
	assert.Len(t, findRelease(m, "v2.16.0").ReleaseNotes, 100+len(notesTruncationMarker))
	assert.Equal(t, long, findRelease(m, "v2.16.1").ReleaseNotes,
		"the newest stable release keeps its full notes")
	assert.Equal(t, long, findRelease(m, "v2.17.0-beta1").ReleaseNotes,
		"the newest beta release keeps its full notes")
}

// TestApplyRetention_ZeroNotesLimitDisablesTruncation keeps the flag able to
// turn the behaviour off entirely.
func TestApplyRetention_ZeroNotesLimitDisablesTruncation(t *testing.T) {
	t.Parallel()

	m := newTestManifest(t, []string{"v2.16.0", "v2.16.1"}, []string{channelStable, channelStable})
	long := strings.Repeat("a", 4000)
	m.Releases[0].ReleaseNotes = long

	applyRetention(m, 5, 5, 0)

	assert.Equal(t, long, m.Releases[0].ReleaseNotes)
}

func TestTruncateNotes(t *testing.T) {
	t.Parallel()

	t.Run("under the limit is untouched", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "short notes", truncateNotes("short notes", 100))
	})

	t.Run("marker makes truncation visible", func(t *testing.T) {
		t.Parallel()
		got := truncateNotes(strings.Repeat("a", 50), 10)
		assert.Equal(t, strings.Repeat("a", 10)+notesTruncationMarker, got)
	})

	t.Run("multi-byte runes are not split", func(t *testing.T) {
		t.Parallel()
		got := truncateNotes(strings.Repeat("é", 50), 10)
		assert.Equal(t, strings.Repeat("é", 10)+notesTruncationMarker, got)
		assert.True(t, utf8.ValidString(got))
	})

	t.Run("trailing whitespace is trimmed before the marker", func(t *testing.T) {
		t.Parallel()
		got := truncateNotes("hello    world", 8)
		assert.Equal(t, "hello"+notesTruncationMarker, got)
	})
}

func TestBackfillDigests(t *testing.T) {
	t.Parallel()

	m := newTestManifest(t, []string{"v2.16.1"}, []string{channelStable})
	archives := archiveAssets(m.Releases[0])
	original := archives[0].SHA256
	archives[0].SHA256 = ""
	archives[1].SHA256 = ""

	backfillDigests(m, []checksumEntry{
		{Name: archives[0].Name, SHA256: original},
		{Name: "some-other-file.tar.gz", SHA256: "unused"},
	})

	assert.Equal(t, original, archives[0].SHA256)
	assert.Empty(t, archives[1].SHA256, "an unknown archive stays empty for requireDigests to catch")
}

// TestBackfillDigests_DoesNotOverwrite makes sure a locally computed digest is
// never replaced by one carried forward from a previously published file.
func TestBackfillDigests_DoesNotOverwrite(t *testing.T) {
	t.Parallel()

	m := newTestManifest(t, []string{"v2.16.1"}, []string{channelStable})
	archives := archiveAssets(m.Releases[0])
	local := archives[0].SHA256

	backfillDigests(m, []checksumEntry{{Name: archives[0].Name, SHA256: "0000"}})

	assert.Equal(t, local, archives[0].SHA256)
}

func TestRequireDigests(t *testing.T) {
	t.Parallel()

	m := newTestManifest(t, []string{"v2.16.1"}, []string{channelStable})
	require.NoError(t, requireDigests(m))

	archives := archiveAssets(m.Releases[0])
	archives[0].SHA256 = ""

	err := requireDigests(m)
	require.ErrorIs(t, err, errMissingDigest)
	assert.Contains(t, err.Error(), "v2.16.1/"+archives[0].Name)
}

// TestChecksumsFromManifest_MirrorsRetention is the property that stops a
// pruned release from remaining installable via a stale checksums.txt line.
func TestChecksumsFromManifest_MirrorsRetention(t *testing.T) {
	t.Parallel()

	m := newTestManifest(t,
		[]string{"v2.16.0", "v2.16.1"},
		[]string{channelStable, channelStable},
	)
	require.Len(t, checksumsFromManifest(m), 2*len(expectedTargets()))

	applyRetention(m, 1, 1, 0)

	entries := checksumsFromManifest(m)
	require.Len(t, entries, len(expectedTargets()))
	for _, e := range entries {
		assert.NotContains(t, e.Name, "2.16.0")
	}
	assert.True(t, entriesSorted(entries), "checksums must be stable across runs")
}

func TestChecksumsFromManifest_SkipsMetadataAndDuplicates(t *testing.T) {
	t.Parallel()

	m := &manifest{Releases: []*release{
		{
			TagName: "v2.16.1",
			Channel: channelStable,
			Assets: []*asset{
				{Name: "zaparoo-linux_amd64-2.16.1.tar.gz", SHA256: "bb"},
				{Name: "zaparoo-linux_amd64-2.16.1.tar.gz", SHA256: "bb"},
				{Name: "zaparoo-mister_arm-2.16.1.zip", SHA256: ""},
				{Name: checksumsName},
				{Name: checksumsSigName},
			},
		},
	}}

	entries := checksumsFromManifest(m)

	require.Len(t, entries, 1)
	assert.Equal(t, "zaparoo-linux_amd64-2.16.1.tar.gz", entries[0].Name)
}

func TestChecksumsRoundTrip(t *testing.T) {
	t.Parallel()

	m := newTestManifest(t, []string{"v2.16.1"}, []string{channelStable})
	entries := checksumsFromManifest(m)

	parsed, err := parseChecksums(renderChecksums(entries))
	require.NoError(t, err)
	assert.Equal(t, entries, parsed)
}

func TestParseChecksums(t *testing.T) {
	t.Parallel()

	t.Run("tolerates blank lines, CRLF and binary mode", func(t *testing.T) {
		t.Parallel()

		entries, err := parseChecksums([]byte(
			"aa  zaparoo-linux_amd64-2.16.1.tar.gz\r\n" +
				"\n" +
				"bb  *zaparoo-mister_arm-2.16.1.zip\n",
		))
		require.NoError(t, err)

		assert.Equal(t, []checksumEntry{
			{Name: "zaparoo-linux_amd64-2.16.1.tar.gz", SHA256: "aa"},
			{Name: "zaparoo-mister_arm-2.16.1.zip", SHA256: "bb"},
		}, entries)
	})

	t.Run("rejects a line without the sha256sum separator", func(t *testing.T) {
		t.Parallel()

		_, err := parseChecksums([]byte("aa zaparoo-linux_amd64-2.16.1.tar.gz\n"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "line 1")
	})

	t.Run("rejects an empty digest", func(t *testing.T) {
		t.Parallel()

		_, err := parseChecksums([]byte("  zaparoo-linux_amd64-2.16.1.tar.gz\n"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing a digest or filename")
	})
}

func TestSetMetadataAssetSizes(t *testing.T) {
	t.Parallel()

	m := newTestManifest(t, []string{"v2.16.1"}, []string{channelStable})

	setMetadataAssetSizes(m, 1234)

	for _, a := range m.Releases[0].Assets {
		switch a.Name {
		case checksumsName:
			assert.Equal(t, int64(1234), a.Size)
		case checksumsSigName:
			assert.Equal(t, int64(ed25519SignatureSize), a.Size)
		default:
			assert.NotZero(t, a.Size)
		}
	}
}
