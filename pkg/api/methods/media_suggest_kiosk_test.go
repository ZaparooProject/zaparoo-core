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

package methods

import (
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/recommend"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// newSuggestKioskFixture is a locked device whose shared profile may see
// every system but NES, with a mocked media database.
func newSuggestKioskFixture(t *testing.T) (*kioskFixture, *helpers.MockMediaDBI) {
	t.Helper()
	f := newKioskFixture(t)
	require.NoError(t, f.db.UserDB.ReplaceContentRule("", &database.ContentRule{
		Mode: database.ContentModeAll,
		Entries: []database.ContentRuleEntry{
			{Effect: database.ContentEffectDeny, Kind: database.ContentKindSystem, SystemID: "NES"},
		},
	}))
	f.lockOpen(t)
	mediaDB := helpers.NewMockMediaDBI()
	f.db.MediaDB = mediaDB
	return f, mediaDB
}

//nolint:gocritic // test helper takes the environment by value, as handlers do
func withSuggestConfig(env requests.RequestEnv) requests.RequestEnv {
	env.Config = &config.Instance{}
	return env
}

// A locked device lists no version from a system its profile cannot see,
// and never lists hidden ones, whatever the caller asks for.
func TestHandleMediaVersions_KioskScope(t *testing.T) {
	t.Parallel()
	f, mediaDB := newSuggestKioskFixture(t)
	mediaDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{101}).
		Return(map[int64]database.MediaFullRow{101: mediaFullRow(101, contraTitle, "")}, nil)
	mediaDB.On("GetVersionGroupMembers", mock.Anything, []int64{contraTitle}).
		Return(map[int64][]int64{}, nil)
	mediaDB.On("SearchMediaWithFilters", mock.Anything,
		mock.MatchedBy(func(filters *database.SearchFilters) bool { return filters.ExcludeHidden })).
		Return(contraRows()[:2], nil)

	mediaID := int64(101)
	params := models.MediaVersionsParams{MediaID: &mediaID, IncludeHidden: ptrTo(true)}

	locked := callVersions(t, withSuggestConfig(withKioskParams(t, f.local(), params)))
	assert.Empty(t, locked.Versions, "NES versions are closed to the locked profile")

	// A paired admin is not held to the scope, and gets hidden media on
	// request, so the hidden-only stub above does not match its read.
	mediaDB.On("SearchMediaWithFilters", mock.Anything,
		mock.MatchedBy(func(filters *database.SearchFilters) bool { return !filters.ExcludeHidden })).
		Return(contraRows()[:2], nil)
	admin := callVersions(t, withSuggestConfig(withKioskParams(t, f.paired("admin"), params)))
	assert.Len(t, admin.Versions, 2)
}

// Asking a locked device for suggestions only from closed systems is refused,
// the same as searching them.
func TestSuggestions_KioskRefusesClosedSystems(t *testing.T) {
	t.Parallel()
	f, _ := newSuggestKioskFixture(t)
	mediaID := int64(101)
	closed := []string{"NES"}

	_, err := HandleMediaSimilar(withSuggestConfig(withKioskParams(t, f.local(),
		models.MediaSimilarParams{MediaID: &mediaID, Systems: &closed})))
	require.ErrorIs(t, err, ErrKioskLocked)

	_, err = HandleMediaRecommended(withSuggestConfig(withKioskParams(t, f.local(),
		models.MediaRecommendedParams{Systems: &closed})))
	require.ErrorIs(t, err, ErrKioskLocked)
}

// A seed is shown as its section's heading, so a seed from a system the
// locked profile cannot see drops the section even when its items are open.
func TestHydrateRecommendedPlan_KioskDropsClosedSeed(t *testing.T) {
	t.Parallel()
	f, mediaDB := newSuggestKioskFixture(t)
	rows := []database.SearchResultWithCursor{
		suggestionRow(1, "Gradius"), suggestionRow(2, "Gradius II"),
		suggestionRow(3, "R-Type"), suggestionRow(4, "Salamander"),
	}
	for i := 1; i < len(rows); i++ {
		rows[i].SystemID = "SNES"
		rows[i].Path = "/roms/SNES/" + rows[i].Name + ".sfc"
	}
	mediaDB.On("SearchMediaWithFilters", mock.Anything, mock.Anything).Return(rows, nil)
	plan := &recommend.Plan{Sections: []recommend.PlanSection{{
		Kind:          recommend.SectionSeed,
		Signal:        recommend.SignalPlayed,
		SeedTitleDBID: 1,
		Items:         []recommend.PlanItem{{TitleDBID: 2}, {TitleDBID: 3}, {TitleDBID: 4}},
	}}}

	locked := withSuggestConfig(f.local())
	response, err := hydrateRecommendedPlan(&locked, plan, nil, recommend.DefaultMaxResults)
	require.NoError(t, err)
	assert.Empty(t, response.Sections)

	admin := withSuggestConfig(f.paired("admin"))
	response, err = hydrateRecommendedPlan(&admin, plan, nil, recommend.DefaultMaxResults)
	require.NoError(t, err)
	require.Len(t, response.Sections, 1)
	assert.Len(t, response.Sections[0].Items, 3)
}
