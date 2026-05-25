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
	"unicode/utf8"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeTitleParseEnv(t *testing.T, systemID, path string) requests.RequestEnv {
	t.Helper()
	params, err := json.Marshal(map[string]string{
		"systemId": systemID,
		"path":     path,
	})
	require.NoError(t, err)
	return requests.RequestEnv{
		Context: context.Background(),
		Config:  &config.Instance{},
		Params:  params,
	}
}

func TestHandleMediaTitleParse_InvalidParams(t *testing.T) {
	t.Parallel()

	nesPath := filepath.Join("roms", "nes", "game.nes")

	missingSystemParams, err := json.Marshal(map[string]string{"path": nesPath})
	require.NoError(t, err)

	emptySystemParams, err := json.Marshal(map[string]string{"systemId": "", "path": nesPath})
	require.NoError(t, err)

	tests := []struct {
		name   string
		params []byte
	}{
		{
			name:   "missing systemId",
			params: missingSystemParams,
		},
		{
			name:   "empty systemId",
			params: emptySystemParams,
		},
		{
			name:   "missing path",
			params: []byte(`{"systemId": "NES"}`),
		},
		{
			name:   "empty path",
			params: []byte(`{"systemId": "NES", "path": ""}`),
		},
		{
			name:   "invalid JSON",
			params: []byte(`not valid json`),
		},
		{
			name:   "null params",
			params: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := requests.RequestEnv{
				Context: context.Background(),
				Config:  &config.Instance{},
				Params:  tt.params,
			}
			_, err := HandleMediaTitleParse(env)
			require.Error(t, err)
		})
	}
}

func TestHandleMediaTitleParse_SimpleGame(t *testing.T) {
	t.Parallel()

	env := makeTitleParseEnv(t, "NES",
		filepath.Join("roms", "nes", "Super Mario Bros.nes"),
	)

	result, err := HandleMediaTitleParse(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaTitleParseResponse)
	require.True(t, ok)
	assert.Equal(t, "Super Mario Bros", resp.Name)
	assert.NotEmpty(t, resp.Slug)
	assert.Nil(t, resp.SecondarySlug)
	assert.Equal(t, utf8.RuneCountInString(resp.Slug), resp.SlugLength)
	assert.Positive(t, resp.SlugWordCount)
}

func TestHandleMediaTitleParse_TitleWithColonProducesSecondarySlug(t *testing.T) {
	t.Parallel()

	env := makeTitleParseEnv(t, "NES",
		filepath.Join("roms", "nes", "The Legend of Zelda: Links Awakening.nes"),
	)

	result, err := HandleMediaTitleParse(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaTitleParseResponse)
	require.True(t, ok)
	assert.NotEmpty(t, resp.Name)
	assert.NotEmpty(t, resp.Slug)
	require.NotNil(t, resp.SecondarySlug)
	assert.NotEmpty(t, *resp.SecondarySlug)
	assert.Equal(t, utf8.RuneCountInString(resp.Slug), resp.SlugLength)
}

func TestHandleMediaTitleParse_TitleWithDashProducesSecondarySlug(t *testing.T) {
	t.Parallel()

	env := makeTitleParseEnv(t, "SNES",
		filepath.Join("roms", "snes", "Donkey Kong Country - Tropical Freeze.sfc"),
	)

	result, err := HandleMediaTitleParse(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaTitleParseResponse)
	require.True(t, ok)
	assert.NotEmpty(t, resp.Name)
	require.NotNil(t, resp.SecondarySlug)
	assert.NotEmpty(t, *resp.SecondarySlug)
}

func TestHandleMediaTitleParse_NoSubtitleHasNilSecondarySlug(t *testing.T) {
	t.Parallel()

	env := makeTitleParseEnv(t, "NES",
		filepath.Join("roms", "nes", "Tetris.nes"),
	)

	result, err := HandleMediaTitleParse(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaTitleParseResponse)
	require.True(t, ok)
	assert.Equal(t, "Tetris", resp.Name)
	assert.Nil(t, resp.SecondarySlug)
	assert.Positive(t, resp.SlugLength)
	assert.Positive(t, resp.SlugWordCount)
}

func TestHandleMediaTitleParse_UnknownSystemFallsBack(t *testing.T) {
	t.Parallel()

	env := makeTitleParseEnv(t, "UNKNOWN_SYSTEM_XYZ",
		filepath.Join("roms", "misc", "Some Game.rom"),
	)

	result, err := HandleMediaTitleParse(env)
	require.NoError(t, err)

	resp, ok := result.(models.MediaTitleParseResponse)
	require.True(t, ok)
	assert.Equal(t, "Some Game", resp.Name)
	assert.NotEmpty(t, resp.Slug)
}

func TestHandleMediaTitleParse_SlugLengthMatchesRuneCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		systemID string
		filename string
	}{
		{
			name:     "NES multi-word",
			systemID: "NES",
			filename: "Mega Man 2.nes",
		},
		{
			name:     "Genesis multi-word",
			systemID: "Genesis",
			filename: "Sonic the Hedgehog 2.md",
		},
		{
			name:     "SNES single word",
			systemID: "SNES",
			filename: "Tetris.sfc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := makeTitleParseEnv(t, tt.systemID,
				filepath.Join("roms", tt.filename),
			)

			result, err := HandleMediaTitleParse(env)
			require.NoError(t, err)

			resp, ok := result.(models.MediaTitleParseResponse)
			require.True(t, ok)
			assert.Equal(t, utf8.RuneCountInString(resp.Slug), resp.SlugLength,
				"SlugLength must equal rune count of Slug")
			assert.Positive(t, resp.SlugWordCount)
		})
	}
}

func TestHandleMediaTitleParse_ResponseType(t *testing.T) {
	t.Parallel()

	env := makeTitleParseEnv(t, "NES",
		filepath.Join("roms", "nes", "Castlevania.nes"),
	)

	result, err := HandleMediaTitleParse(env)
	require.NoError(t, err)
	require.NotNil(t, result)

	_, ok := result.(models.MediaTitleParseResponse)
	require.True(t, ok, "result must be MediaTitleParseResponse")
}
