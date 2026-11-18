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
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func artworkRandomEnv(mediaDB *helpers.MockMediaDBI, params string) requests.RequestEnv {
	env := requests.RequestEnv{Context: context.Background(), Params: json.RawMessage(params)}
	if mediaDB != nil {
		env.Database = &database.Database{MediaDB: mediaDB}
	}
	return env
}

func TestHandleMediaArtworkRandom_InvalidParams(t *testing.T) {
	t.Parallel()

	excludeIDs := make([]string, 501)
	for i := range excludeIDs {
		excludeIDs[i] = strconv.Itoa(i + 1)
	}
	tests := []struct {
		name   string
		params string
	}{
		{name: "no params", params: ""},
		{name: "missing imageTypes", params: `{"limit":5}`},
		{name: "empty imageTypes", params: `{"imageTypes":[]}`},
		{name: "unknown image type", params: `{"imageTypes":["boxart","poster"]}`},
		{name: "full type tag is not a short name", params: `{"imageTypes":["property:image-boxart"]}`},
		{name: "too many image types", params: `{"imageTypes":[` +
			strings.Repeat(`"boxart",`, 12) + `"boxart"]}`},
		{name: "zero limit", params: `{"imageTypes":["boxart"],"limit":0}`},
		{name: "limit above 50", params: `{"imageTypes":["boxart"],"limit":51}`},
		{name: "too many excludeIds", params: `{"imageTypes":["boxart"],"excludeIds":[` +
			strings.Join(excludeIDs, ",") + `]}`},
		{name: "non-positive excludeId", params: `{"imageTypes":["boxart"],"excludeIds":[0]}`},
		{name: "unknown field", params: `{"imageTypes":["boxart"],"query":"mario"}`},
		{name: "empty system", params: `{"imageTypes":["boxart"],"systems":[""]}`},
		{name: "unknown system", params: `{"imageTypes":["boxart"],"systems":["NotASystem"]}`},
		{name: "bad tag filter", params: `{"imageTypes":["boxart"],"tags":["nocolon"]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mediaDB := helpers.NewMockMediaDBI()
			_, err := HandleMediaArtworkRandom(artworkRandomEnv(mediaDB, tt.params))
			require.Error(t, err)
			var clientErr *models.ClientError
			require.ErrorAs(t, err, &clientErr)
			mediaDB.AssertNotCalled(t, "SampleArtworkMedia", mock.Anything, mock.Anything)
		})
	}
}

func TestHandleMediaArtworkRandom_BuildsQueryAndMapsResults(t *testing.T) {
	t.Parallel()

	mediaDB := helpers.NewMockMediaDBI()
	var got *database.ArtworkSampleQuery
	mediaDB.On("SampleArtworkMedia", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			query, ok := args.Get(1).(*database.ArtworkSampleQuery)
			require.True(t, ok)
			got = query
		}).
		Return([]database.ArtworkSample{
			{
				MediaID:    42,
				SystemID:   "SNES",
				Name:       "Super Metroid",
				Path:       "/roms/SNES/Super Metroid.sfc",
				ImageTypes: []string{"property:image-screenshot", "property:image-boxart"},
			},
		}, nil)

	result, err := HandleMediaArtworkRandom(artworkRandomEnv(mediaDB, `{
		"imageTypes": ["screenshot", "boxart"],
		"systems": ["SNES"],
		"tags": ["-user:favorite"],
		"pathPrefix": "/roms/SNES",
		"limit": 3,
		"excludeIds": [7, 9]
	}`))
	require.NoError(t, err)

	require.NotNil(t, got)
	assert.Equal(t, []string{"property:image-screenshot", "property:image-boxart"}, got.ImageTypes)
	assert.Equal(t, []string{"SNES"}, got.Systems)
	assert.Equal(t, "/roms/SNES", got.PathPrefix)
	assert.Equal(t, []zapscript.TagFilter{
		{Type: "user", Value: "favorite", Operator: zapscript.TagOperatorNOT},
	}, got.Tags)
	assert.Equal(t, 3, got.Limit)
	assert.Equal(t, []int64{7, 9}, got.ExcludeDBIDs)
	assert.True(t, got.ExcludeHidden)

	resp, ok := result.(models.MediaArtworkRandomResponse)
	require.True(t, ok)
	require.Len(t, resp.Items, 1)
	item := resp.Items[0]
	assert.Equal(t, int64(42), item.MediaID)
	assert.Equal(t, "SNES", item.System.ID)
	assert.NotEmpty(t, item.System.Name)
	assert.Equal(t, "Super Metroid", item.Name)
	assert.Equal(t, "/roms/SNES/Super Metroid.sfc", item.Path)
	assert.Equal(t, []string{"screenshot", "boxart"}, item.ImageTypes)

	encoded, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.JSONEq(t, `{"items":[{
		"mediaId": 42,
		"system": `+string(mustJSON(t, item.System))+`,
		"name": "Super Metroid",
		"path": "/roms/SNES/Super Metroid.sfc",
		"imageTypes": ["screenshot", "boxart"]
	}]}`, string(encoded))
}

func TestHandleMediaArtworkRandom_Defaults(t *testing.T) {
	t.Parallel()

	mediaDB := helpers.NewMockMediaDBI()
	mediaDB.On("SampleArtworkMedia", mock.Anything, mock.MatchedBy(func(q *database.ArtworkSampleQuery) bool {
		return q.Limit == defaultMediaArtworkRandomLimit && q.ExcludeHidden &&
			len(q.Systems) == 0 && q.PathPrefix == "" && len(q.Tags) == 0 && len(q.ExcludeDBIDs) == 0
	})).Return([]database.ArtworkSample{}, nil)

	result, err := HandleMediaArtworkRandom(artworkRandomEnv(mediaDB, `{"imageTypes":["boxart"]}`))
	require.NoError(t, err)
	resp, ok := result.(models.MediaArtworkRandomResponse)
	require.True(t, ok)
	require.NotNil(t, resp.Items)
	assert.Empty(t, resp.Items)
	encoded, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.JSONEq(t, `{"items":[]}`, string(encoded))
	mediaDB.AssertExpectations(t)
}

func TestHandleMediaArtworkRandom_HiddenOptIn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		params        string
		excludeHidden bool
	}{
		{name: "includeHidden", params: `{"imageTypes":["boxart"],"includeHidden":true}`},
		{name: "required user tag", params: `{"imageTypes":["boxart"],"tags":["user:hidden"]}`},
		{
			name:          "excluded user tag keeps hidden out",
			params:        `{"imageTypes":["boxart"],"tags":["-user:hidden"]}`,
			excludeHidden: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mediaDB := helpers.NewMockMediaDBI()
			mediaDB.On("SampleArtworkMedia", mock.Anything, mock.MatchedBy(func(q *database.ArtworkSampleQuery) bool {
				return q.ExcludeHidden == tt.excludeHidden
			})).Return([]database.ArtworkSample{}, nil)
			_, err := HandleMediaArtworkRandom(artworkRandomEnv(mediaDB, tt.params))
			require.NoError(t, err)
			mediaDB.AssertExpectations(t)
		})
	}
}

func TestHandleMediaArtworkRandom_Errors(t *testing.T) {
	t.Parallel()

	t.Run("no media database", func(t *testing.T) {
		t.Parallel()
		_, err := HandleMediaArtworkRandom(artworkRandomEnv(nil, `{"imageTypes":["boxart"]}`))
		require.Error(t, err)
	})

	t.Run("database error", func(t *testing.T) {
		t.Parallel()
		mediaDB := helpers.NewMockMediaDBI()
		boom := errors.New("database is closed")
		mediaDB.On("SampleArtworkMedia", mock.Anything, mock.Anything).Return(nil, boom)
		_, err := HandleMediaArtworkRandom(artworkRandomEnv(mediaDB, `{"imageTypes":["boxart"]}`))
		require.ErrorIs(t, err, boom)
		var clientErr *models.ClientError
		assert.NotErrorAs(t, err, &clientErr)
	})

	t.Run("cancelled before a slot is free", func(t *testing.T) {
		t.Parallel()
		mediaDB := helpers.NewMockMediaDBI()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		env := artworkRandomEnv(mediaDB, `{"imageTypes":["boxart"]}`)
		env.Context = ctx
		// A cancelled context may still win the select when a slot is free, so
		// either outcome is valid; the handler must not hang or panic.
		mediaDB.On("SampleArtworkMedia", mock.Anything, mock.Anything).
			Return(nil, context.Canceled).Maybe()
		_, err := HandleMediaArtworkRandom(env)
		require.ErrorIs(t, err, context.Canceled)
	})
}
