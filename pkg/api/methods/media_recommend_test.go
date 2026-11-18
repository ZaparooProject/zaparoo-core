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
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/recommend"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	genreTagID  = int64(501)
	seriesTagID = int64(502)
)

// shooterFacets describes each title as a shooter. Titles 1 and 2 are also of
// the Gradius series.
func shooterFacets(titleDBIDs ...int64) []database.TitleFacetTag {
	facets := make([]database.TitleFacetTag, 0, 2*len(titleDBIDs))
	for _, id := range titleDBIDs {
		facets = append(facets, database.TitleFacetTag{
			TitleDBID: id, SystemID: "NES", TagDBID: genreTagID, Type: "genre", Tag: "shooter:horizontal",
		})
		if id <= 2 {
			facets = append(facets, database.TitleFacetTag{
				TitleDBID: id, SystemID: "NES", TagDBID: seriesTagID, Type: "search", Tag: "franchise:gradius",
			})
		}
	}
	return facets
}

func suggestionRow(titleID int64, name string) database.SearchResultWithCursor {
	return database.SearchResultWithCursor{
		SystemID: "NES", Name: name, Path: "/roms/NES/" + name + ".nes",
		MediaID: titleID*10 + 1, MediaTitleID: titleID,
	}
}

// expectSimilarReads stubs the index for a library where title 1 is a
// shooter and candidates are the titles like it.
func expectSimilarReads(mediaDB *helpers.MockMediaDBI, candidates ...int64) {
	mediaDB.On("GetTitleFacetTags", mock.Anything, []int64{1}, mock.Anything).Return(shooterFacets(1), nil)
	// Any other read is of candidates, the seed's own series among them.
	mediaDB.On("GetTitleFacetTags", mock.Anything, mock.Anything, mock.Anything).
		Return(shooterFacets(append([]int64{1}, candidates...)...), nil)
	mediaDB.On("MediaTitleCountEstimate", mock.Anything).Return(int64(1000), nil)
	mediaDB.On("CountTitlesByTag", mock.Anything, mock.Anything, mock.Anything).
		Return(map[int64]int{genreTagID: 40, seriesTagID: 6}, nil)
	found := make([]database.TagSharingTitle, 0, len(candidates))
	for _, id := range candidates {
		found = append(found, database.TagSharingTitle{TitleDBID: id, Score: 1})
	}
	mediaDB.On("FindTitlesSharingTags", mock.Anything, mock.Anything, []int64{1}, mock.Anything).
		Return(found, nil)
	// A read that leaves nothing out is of a whole series.
	mediaDB.On("FindTitlesSharingTags", mock.Anything, mock.Anything, []int64(nil), mock.Anything).
		Return([]database.TagSharingTitle{{TitleDBID: 1, Score: 1}, {TitleDBID: 2, Score: 1}}, nil)
}

func TestHandleMediaSimilar(t *testing.T) {
	t.Parallel()
	mediaDB := helpers.NewMockMediaDBI()
	mediaDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{11}).
		Return(map[int64]database.MediaFullRow{11: mediaFullRow(11, 1, "")}, nil)
	expectSimilarReads(mediaDB, 2, 3, 4)
	// Title 3's media is hidden, so the read does not return it.
	mediaDB.On("SearchMediaWithFilters", mock.Anything,
		mock.MatchedBy(func(filters *database.SearchFilters) bool {
			return filters.ExcludeHidden && assert.ObjectsAreEqual([]int64{2, 3, 4}, filters.TitleDBIDs)
		})).Return([]database.SearchResultWithCursor{
		suggestionRow(4, "Salamander"), suggestionRow(2, "Gradius II"),
	}, nil)

	mediaID := int64(11)
	result, err := HandleMediaSimilar(versionsEnv(t, mediaDB, models.MediaSimilarParams{MediaID: &mediaID}))
	require.NoError(t, err)
	response, ok := result.(models.MediaSimilarResponse)
	require.True(t, ok)

	require.Len(t, response.Results, 2, "hidden media is not suggested")
	assert.Equal(t, int64(21), response.Results[0].MediaID)
	assert.Equal(t, "Gradius II", response.Results[0].Name)
	assert.Equal(t, int64(41), response.Results[1].MediaID)
	require.Len(t, response.Results[0].Reasons, 2)
	assert.Equal(t, recommend.ReasonSeries, response.Results[0].Reasons[0].Code)
	assert.Equal(t, []database.TagInfo{{Type: "search", Tag: "franchise:gradius"}}, response.Results[0].Reasons[0].Tags)
	assert.Equal(t, recommend.ReasonGenre, response.Results[0].Reasons[1].Code)

	encoded, err := json.Marshal(response)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"reasons":[{"code":"series"`)
}

func TestHandleMediaSimilar_NothingKnown(t *testing.T) {
	t.Parallel()
	mediaDB := helpers.NewMockMediaDBI()
	mediaDB.On("GetMediaWithTitleAndSystemByIDs", mock.Anything, []int64{11}).
		Return(map[int64]database.MediaFullRow{11: mediaFullRow(11, 1, "")}, nil)

	mediaID := int64(11)
	result, err := HandleMediaSimilar(versionsEnv(t, mediaDB, models.MediaSimilarParams{MediaID: &mediaID}))
	require.NoError(t, err)
	response, ok := result.(models.MediaSimilarResponse)
	require.True(t, ok)
	assert.NotNil(t, response.Results)
	assert.Empty(t, response.Results, "unscraped media has nothing to be compared by")
	mediaDB.AssertNotCalled(t, "FindTitlesSharingTags", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestHandleMediaSimilar_RejectsBadRequests(t *testing.T) {
	t.Parallel()
	mediaID := int64(11)
	tooMany := 51
	unknown := []string{"NotASystem"}
	tests := []struct {
		params any
		name   string
	}{
		{name: "no media named", params: models.MediaSimilarParams{}},
		{name: "system without path", params: models.MediaSimilarParams{System: "NES"}},
		{name: "page too large", params: models.MediaSimilarParams{MediaID: &mediaID, MaxResults: &tooMany}},
		{name: "unknown system", params: models.MediaSimilarParams{MediaID: &mediaID, Systems: &unknown}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := HandleMediaSimilar(versionsEnv(t, helpers.NewMockMediaDBI(), tt.params))
			require.Error(t, err)
		})
	}
}

// recommendedFixture is a library where the shared profile and one named
// profile each played a different shooter.
type recommendedFixture struct {
	mediaDB *helpers.MockMediaDBI
	userDB  *helpers.MockUserDBI
	state   *state.State
	clock   *clockwork.FakeClock
}

func newRecommendedFixture(t *testing.T) *recommendedFixture {
	t.Helper()
	fixture := &recommendedFixture{
		mediaDB: helpers.NewMockMediaDBI(),
		userDB:  helpers.NewMockUserDBI(),
		clock:   clockwork.NewFakeClockAt(time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)),
	}
	st, _ := state.NewState(mocks.NewMockPlatform(), "test-boot")
	fixture.state = st

	previousClock, previousPlans := recommendClock, recommendedPlans
	recommendClock, recommendedPlans = fixture.clock, recommend.NewPlanCache()
	t.Cleanup(func() { recommendClock, recommendedPlans = previousClock, previousPlans })

	path := "/" + filepath.ToSlash(filepath.Join("roms", "NES", "Gradius.nes"))
	played := []database.MediaPlayTotal{{
		SystemID: "NES", MediaPath: path, PlayTime: 2 * 60 * 60, SessionCount: 3,
		LastPlayedAt: fixture.clock.Now().Add(-time.Hour),
	}}
	fixture.userDB.On("GetMediaPlayTotals", mock.Anything, "", mock.Anything, recommend.MinSessionSecs, mock.Anything).
		Return(played, nil)
	fixture.userDB.On("GetMediaPlayTotals", mock.Anything, "kid", mock.Anything, mock.Anything, mock.Anything).
		Return([]database.MediaPlayTotal(nil), nil)
	fixture.userDB.On("MediaHistoryVersion", mock.Anything).Return("7.0", nil)
	fixture.mediaDB.On("FindMediaIDsByPaths", mock.Anything, mock.Anything).
		Return([]database.MediaPathID{{SystemID: "NES", Path: path, DBID: 11, MediaTitleDBID: 1}}, nil)
	fixture.mediaDB.On("IndexGeneration").Return(int64(3), nil)
	expectSimilarReads(fixture.mediaDB, 2, 3, 4, 5)
	fixture.mediaDB.On("SearchMediaWithFilters", mock.Anything, mock.Anything).
		Return([]database.SearchResultWithCursor{
			suggestionRow(1, "Gradius"), suggestionRow(2, "Gradius II"),
			suggestionRow(3, "R-Type"), suggestionRow(4, "Salamander"), suggestionRow(5, "Darius"),
		}, nil)
	return fixture
}

func (f *recommendedFixture) call(t *testing.T, params any) models.MediaRecommendedResponse {
	t.Helper()
	env := requests.RequestEnv{
		Context:  context.Background(),
		Database: &database.Database{MediaDB: f.mediaDB, UserDB: f.userDB},
		Config:   &config.Instance{},
		State:    f.state,
	}
	if params != nil {
		encoded, err := json.Marshal(params)
		require.NoError(t, err)
		env.Params = encoded
	}
	result, err := HandleMediaRecommended(env)
	require.NoError(t, err)
	response, ok := result.(models.MediaRecommendedResponse)
	require.True(t, ok)
	return response
}

func (f *recommendedFixture) plansBuilt() int {
	built := 0
	for i := range f.mediaDB.Calls {
		if f.mediaDB.Calls[i].Method == "MediaTitleCountEstimate" {
			built++
		}
	}
	return built
}

//nolint:paralleltest // swaps the package's recommendation clock and plan cache
func TestHandleMediaRecommended(t *testing.T) {
	fixture := newRecommendedFixture(t)
	fixture.mediaDB.On("MediaPreferencesRevision", mock.Anything).Return("1", nil)

	response := fixture.call(t, nil)
	require.Len(t, response.Sections, 1)
	section := response.Sections[0]
	assert.Equal(t, recommend.SectionSeed, section.Kind)
	assert.Equal(t, recommend.SignalPlayed, section.Signal)
	require.NotNil(t, section.Seed)
	assert.Equal(t, int64(11), section.Seed.MediaID)
	assert.Equal(t, "Gradius", section.Seed.Name)
	require.Len(t, section.Items, 4)
	assert.Equal(t, int64(21), section.Items[0].MediaID, "the game of the seed's series leads")
	for i := range section.Items {
		assert.NotEqual(t, int64(11), section.Items[i].MediaID, "the seed is not recommended")
		assert.NotEmpty(t, section.Items[i].Reasons)
	}

	encoded, err := json.Marshal(response)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"sections":[{"seed":{`)
	assert.Contains(t, string(encoded), `"kind":"seed","signal":"played"`)
}

//nolint:paralleltest // swaps the package's recommendation clock and plan cache
func TestHandleMediaRecommended_PlanIsReusedUntilSomethingChanges(t *testing.T) {
	fixture := newRecommendedFixture(t)
	revision := fixture.mediaDB.On("MediaPreferencesRevision", mock.Anything).Return("1", nil)

	first := fixture.call(t, nil)
	require.Equal(t, 1, fixture.plansBuilt())
	assert.Equal(t, first, fixture.call(t, nil))
	assert.Equal(t, 1, fixture.plansBuilt(), "an unchanged library and history reuse the plan")

	maxResults := 2
	sized := fixture.call(t, models.MediaRecommendedParams{MaxResults: &maxResults})
	assert.Equal(t, 2, fixture.plansBuilt(), "another size is another plan")
	require.Len(t, sized.Sections, 1)
	assert.Len(t, sized.Sections[0].Items, 2)

	// A flag changing moves the preferences revision.
	revision.Unset()
	fixture.mediaDB.On("MediaPreferencesRevision", mock.Anything).Return("2", nil)
	fixture.call(t, nil)
	assert.Equal(t, 3, fixture.plansBuilt(), "a changed flag builds a new plan")

	fixture.state.SetActiveProfile(&models.ActiveProfile{ProfileID: "kid", Name: "Kid"})
	kid := fixture.call(t, nil)
	assert.NotNil(t, kid.Sections)
	assert.Empty(t, kid.Sections, "a profile with no play of its own has nothing recommended")
	fixture.userDB.AssertCalled(t,
		"GetMediaPlayTotals", mock.Anything, "kid", mock.Anything, mock.Anything, mock.Anything)

	fixture.state.SetActiveProfile(nil)
	assert.Equal(t, first, fixture.call(t, nil), "switching back does not serve the other profile's plan")

	fixture.clock.Advance(24 * time.Hour)
	built := fixture.plansBuilt()
	fixture.call(t, nil)
	assert.Equal(t, built+1, fixture.plansBuilt(), "a new day weighs play afresh")
}

//nolint:paralleltest // swaps the package's recommendation clock and plan cache
func TestHandleMediaRecommended_HiddenSeedDropsItsSection(t *testing.T) {
	fixture := newRecommendedFixture(t)
	fixture.mediaDB.On("MediaPreferencesRevision", mock.Anything).Return("1", nil)
	for _, call := range fixture.mediaDB.ExpectedCalls {
		if call.Method == "SearchMediaWithFilters" {
			call.Unset()
			break
		}
	}
	fixture.mediaDB.On("SearchMediaWithFilters", mock.Anything, mock.Anything).
		Return([]database.SearchResultWithCursor{
			suggestionRow(2, "Gradius II"), suggestionRow(3, "R-Type"), suggestionRow(4, "Salamander"),
		}, nil)
	assert.Empty(t, fixture.call(t, nil).Sections)
}

func TestHandleMediaRecommended_RejectsBadRequests(t *testing.T) {
	t.Parallel()
	tooMany, tooManySections := 31, 13
	unknown := []string{"NotASystem"}
	for name, params := range map[string]models.MediaRecommendedParams{
		"section too large": {MaxResults: &tooMany},
		"too many sections": {MaxSections: &tooManySections},
		"unknown system":    {Systems: &unknown},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := HandleMediaRecommended(versionsEnv(t, helpers.NewMockMediaDBI(), params))
			require.Error(t, err)
		})
	}
}
