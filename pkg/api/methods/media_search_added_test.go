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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flagTimesUserDB is a real user database that reports chosen set times, so
// an order by time does not depend on the clock.
type flagTimesUserDB struct {
	database.UserDBI
	times map[database.MediaUserFlag][]database.MediaUserFlagTime
}

func (d *flagTimesUserDB) ListMediaUserFlagTimes(flag database.MediaUserFlag) ([]database.MediaUserFlagTime, error) {
	return d.times[flag], nil
}

func addedSearchEnv(t *testing.T) (env requests.RequestEnv, names map[int64]string) {
	t.Helper()
	mediaDB, cleanup := testhelpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	userDB, userCleanup := testhelpers.NewInMemoryUserDB(t)
	t.Cleanup(userCleanup)

	files := []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot"}
	paths := make([]string, len(files))
	for i, name := range files {
		paths[i] = filepath.Join("roms", "NES", name+".nes")
	}
	ids := addTestMediaPaths(t, mediaDB, paths...)
	names = make(map[int64]string, len(ids))
	for i, id := range ids {
		names[id] = files[i]
	}
	// Everything but Foxtrot is a favorite; Delta and Echo were starred
	// before set times were recorded.
	setAt := map[string]int64{"Alpha": 100, "Bravo": 300, "Charlie": 200, "Delta": 0, "Echo": 0}
	times := make([]database.MediaUserFlagTime, 0, len(setAt))
	for i, name := range files {
		at, favorite := setAt[name]
		if !favorite {
			continue
		}
		require.NoError(t, mediaDB.UpdateMediaTags(context.Background(), ids[i], nil,
			[]database.MediaTagRef{{Type: "user", Tag: "favorite"}}))
		times = append(times, database.MediaUserFlagTime{
			SystemID: "NES", Path: pathutil.CanonicalMediaPath(paths[i]), SetAt: at,
		})
	}
	// Bravo and Charlie are liked too, with set times the other way round,
	// so an order shows which flag the search follows.
	likedAt := map[string]int64{"Bravo": 100, "Charlie": 900}
	liked := make([]database.MediaUserFlagTime, 0, len(likedAt))
	for i, name := range files {
		at, ok := likedAt[name]
		if !ok {
			continue
		}
		require.NoError(t, mediaDB.UpdateMediaTags(context.Background(), ids[i], nil,
			[]database.MediaTagRef{{Type: "user", Tag: "liked"}}))
		liked = append(liked, database.MediaUserFlagTime{
			SystemID: "NES", Path: pathutil.CanonicalMediaPath(paths[i]), SetAt: at,
		})
	}
	env = requests.RequestEnv{
		Context: context.Background(),
		Database: &database.Database{MediaDB: mediaDB, UserDB: &flagTimesUserDB{
			UserDBI: userDB,
			times: map[database.MediaUserFlag][]database.MediaUserFlagTime{
				database.MediaUserFlagFavorite: times, database.MediaUserFlagLiked: liked,
			},
		}},
	}
	return env, names
}

func searchNames(t *testing.T, env *requests.RequestEnv, params string, names map[int64]string) (
	got []string, next *string,
) {
	t.Helper()
	result, err := HandleMediaSearch(withParams(env, params))
	require.NoError(t, err)
	searched, ok := result.(models.SearchResults)
	require.True(t, ok)
	for i := range searched.Results {
		got = append(got, names[searched.Results[i].MediaID])
	}
	if searched.Pagination != nil {
		next = searched.Pagination.NextCursor
	}
	return got, next
}

func TestMediaSearchAddedDescOrdersByWhenTheFlagWasSet(t *testing.T) {
	t.Parallel()
	env, names := addedSearchEnv(t)

	got, next := searchNames(t, &env, `{"tags":["user:favorite"],"sort":"added-desc"}`, names)
	assert.Equal(t, []string{"Bravo", "Charlie", "Alpha", "Delta", "Echo"}, got)
	assert.Nil(t, next)
}

func TestMediaSearchAddedDescPagesWithACursor(t *testing.T) {
	t.Parallel()
	env, names := addedSearchEnv(t)

	var all []string
	cursor := ""
	for range 5 {
		params := map[string]any{"tags": []string{"user:favorite"}, "sort": "added-desc", "maxResults": 2}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err := json.Marshal(params)
		require.NoError(t, err)
		page, next := searchNames(t, &env, string(raw), names)
		assert.LessOrEqual(t, len(page), 2)
		all = append(all, page...)
		if next == nil {
			break
		}
		cursor = *next
	}
	assert.Equal(t, []string{"Bravo", "Charlie", "Alpha", "Delta", "Echo"}, all,
		"paging crosses from dated rows into undated ones without repeats or gaps")
}

func TestMediaSearchAddedDescFollowsTheFirstFlagTag(t *testing.T) {
	t.Parallel()
	env, names := addedSearchEnv(t)
	// Both flags match Bravo and Charlie; liked comes first, so its times
	// order them, the reverse of the favorite order.
	got, _ := searchNames(t, &env, `{"tags":["user:liked","user:favorite"],"sort":"added-desc"}`, names)
	assert.Equal(t, []string{"Charlie", "Bravo"}, got)
}

func TestMediaSearchAddedDescRefusesOtherUses(t *testing.T) {
	t.Parallel()
	env, _ := addedSearchEnv(t)
	for _, params := range []string{
		`{"sort":"added-desc"}`,
		`{"tags":["user:hidden"],"sort":"added-desc"}`,
		`{"tags":["-user:favorite"],"sort":"added-desc"}`,
		`{"tags":["~user:favorite"],"sort":"added-desc"}`,
		`{"tags":["user:favorite"],"sort":"added-desc","groupBy":"title"}`,
		`{"tags":["user:favorite"],"sort":"added-desc","titleId":1}`,
	} {
		_, err := HandleMediaSearch(withParams(&env, params))
		require.Error(t, err, params)
		var clientErr *models.ClientError
		require.ErrorAs(t, err, &clientErr, params)
	}
	// A cursor from another sort is refused.
	_, next := searchNames(t, &env, `{"tags":["user:favorite"],"sort":"name-asc","maxResults":1}`, nil)
	require.NotNil(t, next)
	_, err := HandleMediaSearch(withParams(&env,
		`{"tags":["user:favorite"],"sort":"added-desc","cursor":"`+*next+`"}`))
	require.Error(t, err)
}
