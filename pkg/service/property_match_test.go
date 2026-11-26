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
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
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

func TestResolveTokenProperties_AmbiguousMatchSelectsFirst(t *testing.T) {
	t.Parallel()

	firstPath := filepath.Join("games", "psx", "a.cue")
	svc, mediaDB, platform := newPropertyMatchService(t, nil)
	mediaDB.On(
		"SearchMediaByProperty", mock.Anything, systemdefs.SystemPSX, string(tags.TagPropertyGameID), "SLUS-12345",
	).Return([]database.SearchResult{
		{SystemID: systemdefs.SystemPSX, Path: firstPath, MediaID: 7},
		{SystemID: systemdefs.SystemPSX, Path: filepath.Join("games", "psx", "b.cue"), MediaID: 8},
	}, nil)
	mediaDB.On("GetMediaTagsByMediaDBIDs", mock.Anything, []int64{7, 8}).
		Return(map[int64][]database.TagInfo{}, nil)
	platform.On("LookupMapping", mock.Anything).Return("", false)

	token := &tokens.Token{UID: "legacy-id", ScanTime: time.Now()}
	resolveTokenProperties(context.Background(), svc, token, []readers.ScanProperty{{
		System: systemdefs.SystemPSX,
		Name:   string(tags.TagPropertyGameID),
		Value:  "SLUS-12345",
	}})

	require.Equal(t, "**launch:"+firstPath, token.Text)
	mediaDB.AssertExpectations(t)
}

func TestResolveTokenProperties_AmbiguousMatchPrefersNonVariant(t *testing.T) {
	t.Parallel()

	hackPath := filepath.Join("games", "psx", "game-hack.cue")
	officialPath := filepath.Join("games", "psx", "game.cue")
	svc, mediaDB, platform := newPropertyMatchService(t, nil)
	mediaDB.On(
		"SearchMediaByProperty", mock.Anything, systemdefs.SystemPSX, string(tags.TagPropertyGameID), "SLUS-12345",
	).Return([]database.SearchResult{
		{SystemID: systemdefs.SystemPSX, Path: hackPath, MediaID: 7},
		{SystemID: systemdefs.SystemPSX, Path: officialPath, MediaID: 8},
	}, nil)
	mediaDB.On("GetMediaTagsByMediaDBIDs", mock.Anything, []int64{7, 8}).Return(map[int64][]database.TagInfo{
		7: {{Type: string(tags.TagTypeUnlicensed), Tag: string(tags.TagUnlicensedHack)}},
	}, nil)
	platform.On("LookupMapping", mock.Anything).Return("", false)

	token := &tokens.Token{UID: "legacy-id", ScanTime: time.Now()}
	resolveTokenProperties(context.Background(), svc, token, []readers.ScanProperty{{
		System: systemdefs.SystemPSX,
		Name:   string(tags.TagPropertyGameID),
		Value:  "SLUS-12345",
	}})

	require.Equal(t, "**launch:"+officialPath, token.Text)
	mediaDB.AssertExpectations(t)
}

func TestResolveTokenProperties_AmbiguousMatchAllVariantsSelectsFirst(t *testing.T) {
	t.Parallel()

	firstPath := filepath.Join("games", "psx", "game-hack.cue")
	svc, mediaDB, platform := newPropertyMatchService(t, nil)
	mediaDB.On(
		"SearchMediaByProperty", mock.Anything, systemdefs.SystemPSX, string(tags.TagPropertyGameID), "SLUS-12345",
	).Return([]database.SearchResult{
		{SystemID: systemdefs.SystemPSX, Path: firstPath, MediaID: 7},
		{SystemID: systemdefs.SystemPSX, Path: filepath.Join("games", "psx", "game-translation.cue"), MediaID: 8},
	}, nil)
	mediaDB.On("GetMediaTagsByMediaDBIDs", mock.Anything, []int64{7, 8}).Return(map[int64][]database.TagInfo{
		7: {{Type: string(tags.TagTypeDump), Tag: string(tags.TagDumpModified)}},
		8: {{Type: string(tags.TagTypeUnlicensed), Tag: string(tags.TagUnlicensedTranslation)}},
	}, nil)
	platform.On("LookupMapping", mock.Anything).Return("", false)

	token := &tokens.Token{UID: "legacy-id", ScanTime: time.Now()}
	resolveTokenProperties(context.Background(), svc, token, []readers.ScanProperty{{
		System: systemdefs.SystemPSX,
		Name:   string(tags.TagPropertyGameID),
		Value:  "SLUS-12345",
	}})

	require.Equal(t, "**launch:"+firstPath, token.Text)
	mediaDB.AssertExpectations(t)
}

func TestResolveTokenProperties_TagLookupErrorSelectsFirst(t *testing.T) {
	t.Parallel()

	firstPath := filepath.Join("games", "psx", "a.cue")
	svc, mediaDB, platform := newPropertyMatchService(t, nil)
	mediaDB.On(
		"SearchMediaByProperty", mock.Anything, systemdefs.SystemPSX, string(tags.TagPropertyGameID), "SLUS-12345",
	).Return([]database.SearchResult{
		{SystemID: systemdefs.SystemPSX, Path: firstPath, MediaID: 7},
		{SystemID: systemdefs.SystemPSX, Path: filepath.Join("games", "psx", "b.cue"), MediaID: 8},
	}, nil)
	mediaDB.On("GetMediaTagsByMediaDBIDs", mock.Anything, []int64{7, 8}).Return(nil, context.Canceled)
	platform.On("LookupMapping", mock.Anything).Return("", false)

	token := &tokens.Token{UID: "legacy-id", ScanTime: time.Now()}
	resolveTokenProperties(context.Background(), svc, token, []readers.ScanProperty{{
		System: systemdefs.SystemPSX,
		Name:   string(tags.TagPropertyGameID),
		Value:  "SLUS-12345",
	}})

	require.Equal(t, "**launch:"+firstPath, token.Text)
	mediaDB.AssertExpectations(t)
}

func TestIsDeprioritizedPropertyMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tag  database.TagInfo
		want bool
	}{
		{
			name: "sample",
			tag:  database.TagInfo{Type: string(tags.TagTypeUnfinished), Tag: string(tags.TagUnfinishedSample)},
			want: true,
		},
		{
			name: "preview",
			tag:  database.TagInfo{Type: string(tags.TagTypeUnfinished), Tag: string(tags.TagUnfinishedPreview)},
			want: true,
		},
		{
			name: "prerelease",
			tag:  database.TagInfo{Type: string(tags.TagTypeUnfinished), Tag: string(tags.TagUnfinishedPrerelease)},
			want: true,
		},
		{
			name: "bootleg",
			tag:  database.TagInfo{Type: string(tags.TagTypeUnlicensed), Tag: string(tags.TagUnlicensedBootleg)},
			want: true,
		},
		{
			name: "hack",
			tag:  database.TagInfo{Type: string(tags.TagTypeUnlicensed), Tag: string(tags.TagUnlicensedHack)},
			want: true,
		},
		{
			name: "clone",
			tag:  database.TagInfo{Type: string(tags.TagTypeUnlicensed), Tag: string(tags.TagUnlicensedClone)},
			want: true,
		},
		{
			name: "translation",
			tag:  database.TagInfo{Type: string(tags.TagTypeUnlicensed), Tag: string(tags.TagUnlicensedTranslation)},
			want: true,
		},
		{
			name: "legacy translation",
			tag: database.TagInfo{
				Type: string(tags.TagTypeUnlicensed), Tag: string(tags.TagUnlicensedTranslationOld),
			},
			want: true,
		},
		{
			name: "hacked dump",
			tag:  database.TagInfo{Type: string(tags.TagTypeDump), Tag: string(tags.TagDumpHacked)},
			want: true,
		},
		{
			name: "modified dump",
			tag:  database.TagInfo{Type: string(tags.TagTypeDump), Tag: string(tags.TagDumpModified)},
			want: true,
		},
		{
			name: "translated dump",
			tag:  database.TagInfo{Type: string(tags.TagTypeDump), Tag: string(tags.TagDumpTranslated)},
			want: true,
		},
		{
			name: "bad dump",
			tag:  database.TagInfo{Type: string(tags.TagTypeDump), Tag: string(tags.TagDumpBad)},
			want: true,
		},
		{
			name: "alpha prefix",
			tag: database.TagInfo{
				Type: string(tags.TagTypeUnfinished), Tag: string(tags.TagUnfinishedAlpha) + ":1",
			},
			want: true,
		},
		{
			name: "numbered beta",
			tag:  database.TagInfo{Type: string(tags.TagTypeUnfinished), Tag: string(tags.TagUnfinishedBeta2)},
			want: true,
		},
		{
			name: "prototype",
			tag:  database.TagInfo{Type: string(tags.TagTypeUnfinished), Tag: string(tags.TagUnfinishedProto)},
			want: true,
		},
		{
			name: "numbered demo",
			tag:  database.TagInfo{Type: string(tags.TagTypeUnfinished), Tag: string(tags.TagUnfinishedDemo1)},
			want: true,
		},
		{
			name: "verified dump",
			tag:  database.TagInfo{Type: string(tags.TagTypeDump), Tag: string(tags.TagDumpVerified)},
			want: false,
		},
		{
			name: "region",
			tag:  database.TagInfo{Type: string(tags.TagTypeRegion), Tag: string(tags.TagRegionUS)},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, isDeprioritizedPropertyMatch([]database.TagInfo{tt.tag}))
		})
	}
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

func discProperties(kind, system string) []readers.ScanProperty {
	props := []readers.ScanProperty{{Name: readers.PropertyDiscKind, Value: kind}}
	if system != "" {
		props = append(props, readers.ScanProperty{Name: readers.PropertyDiscSystem, Value: system})
	}
	return append(props, readers.ScanProperty{Name: readers.PropertyDiscDrive, Value: "sr0"})
}

func withDiscLaunchers(svc *ServiceContext, schemes ...string) {
	cache := &helpers.LauncherCache{}
	launchers := make([]platforms.Launcher, 0, len(schemes))
	for _, scheme := range schemes {
		launchers = append(launchers, platforms.Launcher{ID: scheme + "-launcher", Schemes: []string{scheme}})
	}
	cache.InitializeFromSlice(launchers)
	svc.LauncherCache = cache
}

func withDiscConfig(t *testing.T, svc *ServiceContext, driver config.DriverConfig) { //nolint:gocritic // hugeParam
	t.Helper()
	cfg, err := config.NewConfig(t.TempDir(), config.Values{Readers: config.Readers{
		Drivers: map[string]config.DriverConfig{"opticaldrive": driver},
	}})
	require.NoError(t, err)
	svc.Config = cfg
}

func TestResolveTokenProperties_DiscLaunchOrder(t *testing.T) {
	t.Parallel()

	const imagePath = "games/PSX/Game.chd"
	const discPath = `**launch:"physicaldisc://sr0/PSX"`
	const imageText = "**launch:" + imagePath
	tests := []struct {
		name         string
		order        string
		want         string
		hasImage     bool
		discLauncher bool
	}{
		{name: "image first prefers the image", order: "", hasImage: true, discLauncher: true, want: imageText},
		{name: "image first falls back to the disc", order: "", discLauncher: true, want: discPath},
		{
			name: "disc first prefers the disc", order: config.DiscLaunchDiscFirst,
			hasImage: true, discLauncher: true, want: discPath,
		},
		{
			name: "disc first falls back to the image", order: config.DiscLaunchDiscFirst,
			hasImage: true, want: imageText,
		},
		{name: "image only ignores the disc", order: config.DiscLaunchImageOnly, discLauncher: true, want: ""},
		{name: "disc only ignores the image", order: config.DiscLaunchDiscOnly, hasImage: true, want: ""},
		{name: "unknown order is image first", order: "bogus", hasImage: true, discLauncher: true, want: imageText},
		{name: "nothing can launch", order: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc, mediaDB, platform := newPropertyMatchService(t, nil)
			withDiscConfig(t, svc, config.DriverConfig{LaunchOrder: tt.order})
			if tt.discLauncher {
				withDiscLaunchers(svc, readers.PhysicalDiscScheme)
			} else {
				withDiscLaunchers(svc)
			}
			var results []database.SearchResult
			if tt.hasImage {
				results = []database.SearchResult{{SystemID: systemdefs.SystemPSX, Path: imagePath, MediaID: 1}}
			}
			mediaDB.On(
				"SearchMediaByProperty", mock.Anything, systemdefs.SystemPSX,
				string(tags.TagPropertyGameID), "SLUS-12345",
			).Return(results, nil).Maybe()
			platform.On("LookupMapping", mock.Anything).Return("", false)

			token := &tokens.Token{UID: "disc", ScanTime: time.Now()}
			props := append([]readers.ScanProperty{{
				System: systemdefs.SystemPSX, Name: string(tags.TagPropertyGameID), Value: "SLUS-12345",
			}}, discProperties(readers.DiscKindData, systemdefs.SystemPSX)...)
			resolveTokenProperties(context.Background(), svc, token, props)

			require.Equal(t, tt.want, token.Text)
		})
	}
}

func TestResolveTokenProperties_DiscWithoutSystemNeverLaunchesTheDrive(t *testing.T) {
	t.Parallel()
	svc, _, platform := newPropertyMatchService(t, nil)
	withDiscConfig(t, svc, config.DriverConfig{LaunchOrder: config.DiscLaunchDiscOnly})
	withDiscLaunchers(svc, readers.PhysicalDiscScheme)
	platform.On("LookupMapping", mock.Anything).Return("", false)

	token := &tokens.Token{UID: "disc", ScanTime: time.Now()}
	resolveTokenProperties(context.Background(), svc, token, discProperties(readers.DiscKindData, ""))

	require.Empty(t, token.Text)
}

func TestResolveTokenProperties_RejectsAnInvalidDriveName(t *testing.T) {
	t.Parallel()
	svc, _, platform := newPropertyMatchService(t, nil)
	withDiscConfig(t, svc, config.DriverConfig{LaunchOrder: config.DiscLaunchDiscOnly})
	withDiscLaunchers(svc, readers.PhysicalDiscScheme)
	platform.On("LookupMapping", mock.Anything).Return("", false)

	token := &tokens.Token{UID: "disc", ScanTime: time.Now()}
	resolveTokenProperties(context.Background(), svc, token, []readers.ScanProperty{
		{Name: readers.PropertyDiscKind, Value: readers.DiscKindData},
		{Name: readers.PropertyDiscSystem, Value: systemdefs.SystemPSX},
		{Name: readers.PropertyDiscDrive, Value: "sr0||**input.keyboard:x"},
	})

	require.Empty(t, token.Text)
}

func TestResolveTokenProperties_AudioDisc(t *testing.T) {
	t.Parallel()
	native := platforms.Launcher{ID: "NativeAudio", Schemes: []string{readers.CDAudioScheme}}
	hiFi := platforms.Launcher{ID: "MiSTerHiFi", Schemes: []string{readers.CDAudioScheme}, ExplicitOnly: true}
	unavailable := hiFi
	unavailable.Availability = func(*config.Instance) error { return errors.New("not installed") }
	tests := []struct {
		name      string
		launcher  string
		want      string
		launchers []platforms.Launcher
	}{
		{name: "default player", launchers: []platforms.Launcher{native, hiFi}, want: `**launch:"cdaudio://sr0"`},
		{
			name: "configured player", launcher: "MiSTerHiFi", launchers: []platforms.Launcher{native, hiFi},
			want: `**launch:"cdaudio://sr0"?launcher=MiSTerHiFi`,
		},
		{name: "configured player missing", launcher: "MiSTerHiFi", launchers: []platforms.Launcher{native}},
		{
			name: "configured player unavailable", launcher: "MiSTerHiFi",
			launchers: []platforms.Launcher{native, unavailable},
		},
		{name: "only an explicit player", launchers: []platforms.Launcher{hiFi}},
		{name: "no player"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc, mediaDB, platform := newPropertyMatchService(t, nil)
			withDiscConfig(t, svc, config.DriverConfig{AudioCDLauncher: tt.launcher})
			cache := &helpers.LauncherCache{}
			cache.InitializeFromSlice(tt.launchers)
			svc.LauncherCache = cache
			platform.On("LookupMapping", mock.Anything).Return("", false)

			token := &tokens.Token{UID: "cdda:1", ScanTime: time.Now()}
			resolveTokenProperties(context.Background(), svc, token, discProperties(readers.DiscKindAudio, ""))

			require.Equal(t, tt.want, token.Text)
			mediaDB.AssertNotCalled(t, "SearchMediaByProperty",
				mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		})
	}
}

func TestResolveTokenProperties_PhysicalDiscLauncherMustPlayIt(t *testing.T) {
	t.Parallel()
	psxOnly := platforms.Launcher{
		ID: "PhysicalDiscPSX", Schemes: []string{readers.PhysicalDiscScheme},
		Test: func(_ *config.Instance, path string) bool { return strings.HasSuffix(path, "/PSX") },
	}
	noProvider := psxOnly
	noProvider.Availability = func(*config.Instance) error { return errors.New("no provider") }
	tests := []struct {
		name     string
		system   string
		want     string
		launcher platforms.Launcher
	}{
		{
			name: "plays the system", system: systemdefs.SystemPSX, launcher: psxOnly,
			want: `**launch:"physicaldisc://sr0/PSX"`,
		},
		{name: "other system", system: systemdefs.SystemSaturn, launcher: psxOnly},
		{name: "unavailable", system: systemdefs.SystemPSX, launcher: noProvider},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc, _, platform := newPropertyMatchService(t, nil)
			withDiscConfig(t, svc, config.DriverConfig{LaunchOrder: config.DiscLaunchDiscOnly})
			cache := &helpers.LauncherCache{}
			cache.InitializeFromSlice([]platforms.Launcher{tt.launcher})
			svc.LauncherCache = cache
			platform.On("LookupMapping", mock.Anything).Return("", false)

			token := &tokens.Token{UID: "disc", ScanTime: time.Now()}
			resolveTokenProperties(context.Background(), svc, token, discProperties(readers.DiscKindData, tt.system))

			require.Equal(t, tt.want, token.Text)
		})
	}
}

func TestResolveTokenProperties_DiscTokenFileWins(t *testing.T) {
	t.Parallel()
	svc, _, _ := newPropertyMatchService(t, nil)
	withDiscLaunchers(svc, readers.PhysicalDiscScheme)

	token := &tokens.Token{UID: "disc", Text: "**launch.random:snes", ScanTime: time.Now()}
	resolveTokenProperties(context.Background(), svc, token, discProperties(readers.DiscKindData, systemdefs.SystemPSX))

	require.Equal(t, "**launch.random:snes", token.Text)
}
