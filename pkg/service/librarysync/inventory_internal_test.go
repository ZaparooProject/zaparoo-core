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

package librarysync

import (
	"context"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var testTime = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

func TestPageIdentitiesKeepsOnlySyncedMediaTypes(t *testing.T) {
	t.Parallel()
	mediaDB := testhelpers.NewMockMediaDBI()
	mediaDB.On("GetMediaTagsByMediaDBIDs", mock.Anything, []int64{1, 4}).Return(map[int64][]database.TagInfo{
		1: {{Type: "region", Tag: "us"}},
		4: {{Type: "region", Tag: "us"}},
	}, nil).Once()

	identities, err := pageIdentities(context.Background(), mediaDB, []database.LibraryMediaRow{
		{MediaDBID: 1, SystemID: systemdefs.SystemNES, Name: "Metroid", Slug: "metroid"},
		{MediaDBID: 2, SystemID: systemdefs.SystemMusicTrack, Name: "Song", Slug: "song"},
		{MediaDBID: 3, SystemID: "NotASystem", Name: "Thing", Slug: "thing"},
		{MediaDBID: 4, SystemID: systemdefs.SystemNES, Name: "Metroid", Slug: "metroid"},
	})
	require.NoError(t, err)
	require.Len(t, identities, 1, "only games are listed, and one game once")
	for _, identity := range identities {
		assert.Equal(t, "Game", identity.MediaType)
		assert.Equal(t, systemdefs.SystemNES, identity.CanonicalSystemID)
	}
	mediaDB.AssertExpectations(t)
}

func TestResolveAnswersIgnoreMalformedResults(t *testing.T) {
	t.Parallel()
	ordinal := func(v int64) *int64 { return &v }
	identities := []*database.MediaIdentity{
		{ObservationFingerprint: "sha256:a"},
		{ObservationFingerprint: "sha256:b"},
		{ObservationFingerprint: "sha256:c"},
	}
	response := resolveResponse{Items: []resolveResult{
		{Index: 0, Status: resolveStatusResolved, Ordinal: ordinal(7)},
		{Index: 0, Status: resolveStatusResolved, Ordinal: ordinal(8)},
		{Index: 1, Status: resolveStatusResolved, Ordinal: ordinal(0)},
		{Index: 2, Status: resolveStatusRejected, Code: "invalid_fingerprint"},
		{Index: 9, Status: resolveStatusResolved, Ordinal: ordinal(3)},
		{Index: 1, Status: "pending"},
	}}
	answers := resolveAnswers(identities, &response, testTime, 4)
	require.Len(t, answers, 2)
	assert.Equal(t, uint32(7), answers[0].Ordinal, "the first answer for an item stands")
	assert.Equal(t, "sha256:c", answers[1].Fingerprint)
	assert.Equal(t, "invalid_fingerprint", answers[1].Code)
	assert.Equal(t, int64(4), answers[1].SeenGeneration)
}
