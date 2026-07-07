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

package service

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestResolveTokenProperties_ResolvesSingleMatch(t *testing.T) {
	t.Parallel()

	path := filepath.Join("games", "psx", "game.cue")
	svc, mediaDB, platform := newPropertyMatchService(t, nil)
	mediaDB.On(
		"SearchMediaByProperty", mock.Anything, systemdefs.SystemPSX, string(tags.TagPropertyGameID), "SLUS-12345",
	).Return([]database.SearchResult{{SystemID: systemdefs.SystemPSX, Path: path, MediaID: 7}}, nil)
	platform.On("LookupMapping", mock.Anything).Return("", false)

	token := &tokens.Token{UID: "legacy-id", ScanTime: time.Now()}
	resolveTokenProperties(context.Background(), svc, token, []readers.ScanProperty{{
		System: systemdefs.SystemPSX,
		Name:   string(tags.TagPropertyGameID),
		Value:  "SLUS-12345",
	}})

	require.Equal(t, "**launch:"+path, token.Text)
}

func TestResolveTokenProperties_ExistingMappingWins(t *testing.T) {
	t.Parallel()

	mappings := []database.Mapping{{
		Type:     userdb.MappingTypeID,
		Match:    userdb.MatchTypeExact,
		Pattern:  "legacy-id",
		Override: "**launch:" + filepath.Join("mapped", "game.cue"),
		Enabled:  true,
	}}
	svc, mediaDB, platform := newPropertyMatchService(t, mappings)
	platform.On("LookupMapping", mock.Anything).Return("", false).Maybe()

	token := &tokens.Token{UID: "legacy-id", ScanTime: time.Now()}
	resolveTokenProperties(context.Background(), svc, token, []readers.ScanProperty{{
		System: systemdefs.SystemPSX,
		Name:   string(tags.TagPropertyGameID),
		Value:  "SLUS-12345",
	}})

	require.Empty(t, token.Text)
	mediaDB.AssertNotCalled(t, "SearchMediaByProperty", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestResolveTokenProperties_AmbiguousMatchDoesNotLaunch(t *testing.T) {
	t.Parallel()

	svc, mediaDB, platform := newPropertyMatchService(t, nil)
	mediaDB.On(
		"SearchMediaByProperty", mock.Anything, systemdefs.SystemPSX, string(tags.TagPropertyGameID), "SLUS-12345",
	).Return([]database.SearchResult{
		{SystemID: systemdefs.SystemPSX, Path: filepath.Join("games", "psx", "a.cue"), MediaID: 7},
		{SystemID: systemdefs.SystemPSX, Path: filepath.Join("games", "psx", "b.cue"), MediaID: 8},
	}, nil)
	platform.On("LookupMapping", mock.Anything).Return("", false)

	token := &tokens.Token{UID: "legacy-id", ScanTime: time.Now()}
	resolveTokenProperties(context.Background(), svc, token, []readers.ScanProperty{{
		System: systemdefs.SystemPSX,
		Name:   string(tags.TagPropertyGameID),
		Value:  "SLUS-12345",
	}})

	require.Empty(t, token.Text)
}

func TestResolveTokenProperties_PathWithCommaRoundTrips(t *testing.T) {
	t.Parallel()

	path := filepath.Join("games", "psx", "Some Game, The (Disc 1).cue")
	svc, mediaDB, platform := newPropertyMatchService(t, nil)
	mediaDB.On(
		"SearchMediaByProperty", mock.Anything, systemdefs.SystemPSX, string(tags.TagPropertyGameID), "SLUS-12345",
	).Return([]database.SearchResult{{SystemID: systemdefs.SystemPSX, Path: path, MediaID: 7}}, nil)
	platform.On("LookupMapping", mock.Anything).Return("", false)

	token := &tokens.Token{UID: "legacy-id", ScanTime: time.Now()}
	resolveTokenProperties(context.Background(), svc, token, []readers.ScanProperty{{
		System: systemdefs.SystemPSX,
		Name:   string(tags.TagPropertyGameID),
		Value:  "SLUS-12345",
	}})

	require.NotEmpty(t, token.Text)

	script, err := gozapscript.NewParser(token.Text).ParseScript()
	require.NoError(t, err)
	require.Len(t, script.Cmds, 1)
	require.Equal(t, gozapscript.ZapScriptCmdLaunch, script.Cmds[0].Name)
	require.Len(t, script.Cmds[0].Args, 1)
	require.Equal(t, path, script.Cmds[0].Args[0])
}

func TestResolveTokenProperties_DeduplicatesSameMediaIDAcrossProperties(t *testing.T) {
	t.Parallel()

	path := filepath.Join("games", "psx", "game.cue")
	svc, mediaDB, platform := newPropertyMatchService(t, nil)
	mediaDB.On(
		"SearchMediaByProperty", mock.Anything, systemdefs.SystemPSX, string(tags.TagPropertyGameID), "SLUS-12345",
	).Return([]database.SearchResult{{SystemID: systemdefs.SystemPSX, Path: path, MediaID: 7}}, nil)
	mediaDB.On(
		"SearchMediaByProperty", mock.Anything, systemdefs.SystemPSX, string(tags.TagPropertyGameID), "SLUS-12345-DUPE",
	).Return([]database.SearchResult{{SystemID: systemdefs.SystemPSX, Path: path, MediaID: 7}}, nil)
	platform.On("LookupMapping", mock.Anything).Return("", false)

	token := &tokens.Token{UID: "legacy-id", ScanTime: time.Now()}
	resolveTokenProperties(context.Background(), svc, token, []readers.ScanProperty{
		{System: systemdefs.SystemPSX, Name: string(tags.TagPropertyGameID), Value: "SLUS-12345"},
		{System: systemdefs.SystemPSX, Name: string(tags.TagPropertyGameID), Value: "SLUS-12345-DUPE"},
	})

	require.Equal(t, "**launch:"+path, token.Text)
}

func TestResolveTokenProperties_SearchErrorDoesNotLaunch(t *testing.T) {
	t.Parallel()

	svc, mediaDB, platform := newPropertyMatchService(t, nil)
	mediaDB.On(
		"SearchMediaByProperty", mock.Anything, systemdefs.SystemPSX, string(tags.TagPropertyGameID), "SLUS-12345",
	).Return(nil, context.Canceled)
	platform.On("LookupMapping", mock.Anything).Return("", false)

	token := &tokens.Token{UID: "legacy-id", ScanTime: time.Now()}
	resolveTokenProperties(context.Background(), svc, token, []readers.ScanProperty{{
		System: systemdefs.SystemPSX,
		Name:   string(tags.TagPropertyGameID),
		Value:  "SLUS-12345",
	}})

	require.Empty(t, token.Text)
}

func TestResolveTokenProperties_IgnoresNonDBMatchProperties(t *testing.T) {
	t.Parallel()

	svc, mediaDB, platform := newPropertyMatchService(t, nil)
	platform.On("LookupMapping", mock.Anything).Return("", false)

	token := &tokens.Token{UID: "legacy-id", ScanTime: time.Now()}
	resolveTokenProperties(context.Background(), svc, token, []readers.ScanProperty{{
		Name:  "amiibo-marker",
		Value: "some-marker-value",
	}})

	require.Empty(t, token.Text)
	mediaDB.AssertNotCalled(t, "SearchMediaByProperty", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func newPropertyMatchService(
	t *testing.T,
	mappings []database.Mapping,
) (*ServiceContext, *testhelpers.MockMediaDBI, *mocks.MockPlatform) {
	t.Helper()

	userDB := &testhelpers.MockUserDBI{}
	userDB.On("GetEnabledMappings").Return(mappings, nil)
	mediaDB := &testhelpers.MockMediaDBI{}
	platform := mocks.NewMockPlatform()
	svc := &ServiceContext{
		Config: &config.Instance{},
		DB: &database.Database{
			UserDB:  userDB,
			MediaDB: mediaDB,
		},
		Platform: platform,
	}
	return svc, mediaDB, platform
}
