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

package database_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

//nolint:tagliatelle // Shared fixture schema mirrors snake_case wire contract.
type fingerprintFixtures struct {
	Vectors       []fingerprintVector `json:"vectors"`
	PolicyVersion int                 `json:"policy_version"`
}

//nolint:tagliatelle // Shared fixture schema mirrors snake_case wire contract.
type fingerprintVector struct {
	Name             string                 `json:"name"`
	CanonicalPayload string                 `json:"canonical_payload"`
	Identity         database.MediaIdentity `json:"identity"`
}

func TestLookupMediaIdentity_CapturesCompleteScannerSnapshot(t *testing.T) {
	t.Parallel()

	path := filepath.Join("games", "snes", "Game.sfc")
	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, path).
		Return([]database.SearchResult{{
			SystemID: systemdefs.SystemSNES,
			Name:     "Game",
			Path:     path,
			Slug:     "game",
			MediaID:  42,
		}}, nil).Once()
	mediaDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(42)).
		Return([]database.TagInfo{
			{Type: "region", Tag: "us"},
			{Type: "extension", Tag: "sfc"},
			{Type: "user", Tag: "favorite"},
			{Type: "property", Tag: "description"},
			{Type: "rating", Tag: "95"},
			{Type: "genre", Tag: "platformer"},
			{Type: "gamefamily", Tag: "mario"},
			{Type: "scraper.gamelist.xml", Tag: "scraped"},
			{Type: "scraper-run.gamelist.xml", Tag: "run-1"},
		}, nil).Once()

	identity, found, err := database.LookupMediaIdentity(
		context.Background(), mediaDB, "super-nintendo", path,
	)

	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "Game", identity.MediaType)
	assert.Equal(t, systemdefs.SystemSNES, identity.CanonicalSystemID)
	assert.Equal(t, "Game", identity.DisplayName)
	assert.Equal(t, "game", identity.CoreSlug)
	assert.Equal(t, database.CurrentMediaIdentityPolicyVersion, identity.PolicyVersion)
	assert.Equal(t, []database.MediaIdentityTag{
		{Type: "extension", Value: "sfc", Role: database.MediaIdentityTagRoleContext},
		{Type: "region", Value: "us", Role: database.MediaIdentityTagRoleIdentity},
	}, identity.Tags)
	assert.Equal(t, []string{"extension:sfc", "region:us"}, identity.LegacyTags())
	assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, identity.ObservationFingerprint)
	mediaDB.AssertExpectations(t)
}

func TestLookupMediaIdentity_ReportsSuccessfulEmptyTags(t *testing.T) {
	t.Parallel()

	path := filepath.Join("games", "snes", "Untagged.sfc")
	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, path).
		Return([]database.SearchResult{{
			SystemID: systemdefs.SystemSNES,
			Name:     "Untagged",
			Path:     path,
			Slug:     "untagged",
			MediaID:  43,
		}}, nil).Once()
	mediaDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(43)).
		Return([]database.TagInfo{}, nil).Once()

	identity, found, err := database.LookupMediaIdentity(
		context.Background(), mediaDB, systemdefs.SystemSNES, path,
	)

	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "Untagged", identity.DisplayName)
	assert.Empty(t, identity.Tags)
	assert.NotNil(t, identity.Tags)
	assert.NotEmpty(t, identity.ObservationFingerprint)
	mediaDB.AssertExpectations(t)
}

func TestLookupMediaIdentity_DistinguishesNotFoundAndFailure(t *testing.T) {
	t.Parallel()

	path := filepath.Join("games", "nes", "Missing.nes")
	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		mediaDB := testhelpers.NewMockMediaDBI()
		mediaDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, path).
			Return([]database.SearchResult{}, nil).Once()

		identity, found, err := database.LookupMediaIdentity(
			context.Background(), mediaDB, systemdefs.SystemNES, path,
		)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, identity)
		mediaDB.AssertExpectations(t)
	})

	t.Run("transient failure", func(t *testing.T) {
		t.Parallel()
		mediaDB := testhelpers.NewMockMediaDBI()
		mediaDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, path).
			Return([]database.SearchResult(nil), errors.New("database busy")).Once()

		identity, found, err := database.LookupMediaIdentity(
			context.Background(), mediaDB, systemdefs.SystemNES, path,
		)
		require.Error(t, err)
		assert.False(t, found)
		assert.Empty(t, identity)
		mediaDB.AssertExpectations(t)
	})

	// Rows that can never resolve must report found=false without error, so
	// backfill sweeps skip them instead of aborting on them forever.
	t.Run("unknown indexed system is unresolvable", func(t *testing.T) {
		t.Parallel()
		mediaDB := testhelpers.NewMockMediaDBI()
		mediaDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, path).
			Return([]database.SearchResult{{
				SystemID: "NotARealSystem",
				Name:     "Missing",
				Path:     path,
				Slug:     "missing",
				MediaID:  44,
			}}, nil).Once()

		identity, found, err := database.LookupMediaIdentity(
			context.Background(), mediaDB, systemdefs.SystemNES, path,
		)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, identity)
		mediaDB.AssertExpectations(t)
	})

	t.Run("unbuildable identity is unresolvable", func(t *testing.T) {
		t.Parallel()
		mediaDB := testhelpers.NewMockMediaDBI()
		// An empty slug (e.g. a pure-symbol title) fails the fingerprint
		// contract's required-field check and can never succeed on retry.
		mediaDB.On("SearchMediaPathExact", mock.Anything, mock.Anything, path).
			Return([]database.SearchResult{{
				SystemID: systemdefs.SystemNES,
				Name:     "!!!",
				Path:     path,
				Slug:     "",
				MediaID:  45,
			}}, nil).Once()
		mediaDB.On("GetMediaTagsByMediaDBID", mock.Anything, int64(45)).
			Return([]database.TagInfo{}, nil).Once()

		identity, found, err := database.LookupMediaIdentity(
			context.Background(), mediaDB, systemdefs.SystemNES, path,
		)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Empty(t, identity)
		mediaDB.AssertExpectations(t)
	})
}

func TestMediaIdentityTagRolePolicyV1(t *testing.T) {
	t.Parallel()

	for _, tagType := range []string{"unfinished", "region", "rev", "lang", "track"} {
		role, err := database.MediaIdentityTagRoleForPolicy(
			database.CurrentMediaIdentityPolicyVersion, tagType,
		)
		require.NoError(t, err)
		assert.Equal(t, database.MediaIdentityTagRoleIdentity, role, tagType)
	}
	for _, tagType := range []string{"extension", "unknown", "season"} {
		role, err := database.MediaIdentityTagRoleForPolicy(
			database.CurrentMediaIdentityPolicyVersion, tagType,
		)
		require.NoError(t, err)
		assert.Equal(t, database.MediaIdentityTagRoleContext, role, tagType)
	}
	_, err := database.MediaIdentityTagRoleForPolicy(
		database.CurrentMediaIdentityPolicyVersion+1, "region",
	)
	require.Error(t, err)
}

func TestMediaIdentityFingerprint_SharedVectors(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join("testdata", "media_identity_fingerprints_v1.json"))
	require.NoError(t, err)
	var fixtures fingerprintFixtures
	require.NoError(t, json.Unmarshal(data, &fixtures))
	assert.Equal(t, database.CurrentMediaIdentityPolicyVersion, fixtures.PolicyVersion)
	require.NotEmpty(t, fixtures.Vectors)

	for _, vector := range fixtures.Vectors {
		t.Run(vector.Name, func(t *testing.T) {
			t.Parallel()
			payload, payloadErr := database.MediaIdentityFingerprintPayload(&vector.Identity)
			require.NoError(t, payloadErr)
			assert.Equal(t, vector.CanonicalPayload, string(payload))

			fingerprint, fingerprintErr := database.ComputeMediaIdentityFingerprint(&vector.Identity)
			require.NoError(t, fingerprintErr)
			assert.Equal(t, vector.Identity.ObservationFingerprint, fingerprint)
		})
	}
}

func TestMediaIdentityFingerprint_IsStableAcrossOrderingAndDisplayChanges(t *testing.T) {
	t.Parallel()

	base := database.MediaIdentity{
		MediaType:         "Game",
		CanonicalSystemID: systemdefs.SystemSNES,
		DisplayName:       "Super Mario World",
		CoreSlug:          "supermarioworld",
		Tags: []database.MediaIdentityTag{
			{Type: "region", Value: "us"},
			{Type: "extension", Value: "sfc"},
			{Type: "region", Value: "us"},
		},
		PolicyVersion: database.CurrentMediaIdentityPolicyVersion,
	}
	want, err := database.ComputeMediaIdentityFingerprint(&base)
	require.NoError(t, err)

	changed := base
	changed.DisplayName = "SUPER MARIO WORLD!"
	changed.Tags = []database.MediaIdentityTag{
		{Type: "extension", Value: "sfc", Role: database.MediaIdentityTagRoleIdentity},
		{Type: "region", Value: "us", Role: database.MediaIdentityTagRoleContext},
	}
	got, err := database.ComputeMediaIdentityFingerprint(&changed)
	require.NoError(t, err)
	assert.Equal(t, want, got, "display text, input order, duplicates, and supplied roles must not affect fingerprint")
}

func TestMediaIdentityFingerprint_CollisionFamilies(t *testing.T) {
	t.Parallel()

	base := database.MediaIdentity{
		MediaType:         "Game",
		CanonicalSystemID: systemdefs.SystemSNES,
		DisplayName:       "Collision Game",
		CoreSlug:          "collisiongame",
		Tags: []database.MediaIdentityTag{
			{Type: "extension", Value: "sfc"},
			{Type: "region", Value: "us"},
		},
		PolicyVersion: database.CurrentMediaIdentityPolicyVersion,
	}
	baseFingerprint, err := database.ComputeMediaIdentityFingerprint(&base)
	require.NoError(t, err)

	tests := []struct {
		mutate func(*database.MediaIdentity)
		name   string
	}{
		{
			name:   "canonical system",
			mutate: func(v *database.MediaIdentity) { v.CanonicalSystemID = systemdefs.SystemNES },
		},
		{name: "media type", mutate: func(v *database.MediaIdentity) { v.MediaType = "Application" }},
		{name: "Core slug", mutate: func(v *database.MediaIdentity) { v.CoreSlug = "collisiongame2" }},
		{name: "region and language", mutate: func(v *database.MediaIdentity) {
			v.Tags = append(v.Tags, database.MediaIdentityTag{Type: "lang", Value: "ja"})
		}},
		{name: "revision and edition", mutate: func(v *database.MediaIdentity) {
			v.Tags = append(v.Tags, database.MediaIdentityTag{Type: "rev", Value: "2"})
		}},
		{name: "unfinished and unlicensed", mutate: func(v *database.MediaIdentity) {
			v.Tags = append(v.Tags, database.MediaIdentityTag{Type: "unfinished", Value: "proto"})
		}},
		{name: "disc set and track", mutate: func(v *database.MediaIdentity) {
			v.Tags = append(v.Tags, database.MediaIdentityTag{Type: "disc", Value: "2"})
		}},
		{name: "context extension", mutate: func(v *database.MediaIdentity) {
			v.Tags[0].Value = "zip"
		}},
		{
			name: "non-latin slug",
			mutate: func(v *database.MediaIdentity) {
				v.CoreSlug = "\u60aa\u9b54\u57ce\u30c9\u30e9\u30ad\u30e5\u30e9"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			candidate := base
			candidate.Tags = append([]database.MediaIdentityTag(nil), base.Tags...)
			tt.mutate(&candidate)
			fingerprint, fingerprintErr := database.ComputeMediaIdentityFingerprint(&candidate)
			require.NoError(t, fingerprintErr)
			assert.NotEqual(t, baseFingerprint, fingerprint)
		})
	}
}

func TestMediaIdentityEncoding_RoundTripsAndToleratesLegacyValues(t *testing.T) {
	t.Parallel()

	identity := &database.MediaIdentity{
		MediaType:              "Game",
		CanonicalSystemID:      systemdefs.SystemNES,
		DisplayName:            "Game",
		CoreSlug:               "game",
		ObservationFingerprint: "sha256:test",
		Tags:                   []database.MediaIdentityTag{},
		PolicyVersion:          database.CurrentMediaIdentityPolicyVersion,
	}
	assert.Equal(t, identity, database.DecodeMediaIdentity(database.EncodeMediaIdentity(identity)))
	assert.Nil(t, database.DecodeMediaIdentity(""))
	assert.Nil(t, database.DecodeMediaIdentity("{"))
	assert.Empty(t, database.EncodeMediaIdentity(nil))
}
