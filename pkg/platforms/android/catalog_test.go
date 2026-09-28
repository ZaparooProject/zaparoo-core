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

package android

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed catalog/retroarch-aarch64-buildbot-2026-09-19.txt
var buildbotInventory string

//go:embed catalog/retroarch-aarch64-exclusions-v1.json
var exclusionsJSON []byte

const reviewedCoreInfoRevision = "5a74858ab2f7a50cebb5a6330895bc38899531c0"

type coreExclusions struct {
	ProfilesSourceRevision string          `json:"profilesSourceRevision"`
	Cores                  []coreExclusion `json:"cores"`
	Version                int             `json:"version"`
}

type coreExclusion struct {
	Core   string `json:"core"`
	Reason string `json:"reason"`
}

func decodeRetroArchCatalog(t *testing.T) retroArchCatalog {
	t.Helper()
	var catalog retroArchCatalog
	require.NoError(t, decodeCatalog(retroArchCatalogJSON, maxRetroArchCatalogBytes, &catalog))
	return catalog
}

func TestCatalogIdentityAndBound(t *testing.T) {
	t.Parallel()

	catalog := decodeRetroArchCatalog(t)
	assert.Equal(t, 1, catalog.Version)
	assert.Equal(t, "com.retroarch.aarch64", catalog.Package)
	assert.Equal(t, "com.retroarch.browser.retroactivity.RetroActivityFuture", catalog.Activity)
	assert.Len(t, catalog.Profiles, 260)
	assert.LessOrEqual(t, len(catalog.Profiles), maxRetroArchProfiles)
	assert.LessOrEqual(t, len(retroArchCatalogJSON), maxRetroArchCatalogBytes)
	assert.Equal(t, reviewedCoreInfoRevision, catalog.Source.CoreInfoRevision)
}

func TestCatalogProfilesAreUniqueAndBounded(t *testing.T) {
	t.Parallel()

	var raw struct {
		Profiles []map[string]json.RawMessage `json:"profiles"`
	}
	require.NoError(t, json.Unmarshal(retroArchCatalogJSON, &raw))
	for _, profile := range raw.Profiles {
		keys := make([]string, 0, len(profile))
		for key := range profile {
			keys = append(keys, key)
		}
		assert.ElementsMatch(t, []string{"id", "name", "system", "core", "file", "extensions"}, keys)
	}

	coreFile := regexp.MustCompile(`^[A-Za-z0-9_-]+_libretro(?:_android)?\.so$`)
	ids := make(map[string]struct{})
	systemCores := make(map[string]struct{})
	for _, profile := range decodeRetroArchCatalog(t).Profiles {
		assert.True(t, validDottedName(profile.ID), profile.ID)
		assert.True(t, validCoreName(profile.Core), profile.ID)
		assert.Regexp(t, coreFile, profile.File, profile.ID)
		assert.True(t, validCatalogText(profile.Name), profile.ID)
		assert.NotEmpty(t, profile.Extensions, profile.ID)
		assert.LessOrEqual(t, len(profile.Extensions), maxLaunchExtensions, profile.ID)
		seenExtensions := make(map[string]struct{}, len(profile.Extensions))
		for _, extension := range profile.Extensions {
			assert.Regexp(t, extensionPattern, extension, profile.ID)
			assert.NotContains(t, seenExtensions, extension, profile.ID)
			seenExtensions[extension] = struct{}{}
		}
		assert.NotContains(t, ids, profile.ID)
		ids[profile.ID] = struct{}{}
		systemCore := profile.System + "/" + profile.Core
		assert.NotContains(t, systemCores, systemCore)
		systemCores[systemCore] = struct{}{}
	}
}

func TestCatalogAccountsForEveryBuildbotArtifact(t *testing.T) {
	t.Parallel()

	artifacts := make(map[string]struct{})
	artifactCores := make(map[string]struct{})
	artifactSuffix := regexp.MustCompile(`_libretro(?:_android)?\.so\.zip$`)
	for name := range strings.Lines(buildbotInventory) {
		name = strings.TrimSpace(name)
		require.Regexp(t, artifactSuffix, name)
		artifacts[name] = struct{}{}
		artifactCores[artifactSuffix.ReplaceAllString(name, "")] = struct{}{}
	}
	require.Len(t, artifacts, 236)

	var exclusions coreExclusions
	require.NoError(t, decodeCatalog(exclusionsJSON, maxRetroArchCatalogBytes, &exclusions))
	assert.Equal(t, reviewedCoreInfoRevision, exclusions.ProfilesSourceRevision)
	excludedCores := make(map[string]struct{}, len(exclusions.Cores))
	for _, row := range exclusions.Cores {
		assert.NotEmpty(t, row.Reason, row.Core)
		excludedCores[row.Core] = struct{}{}
	}
	assert.Len(t, excludedCores, 67)

	includedFiles := make(map[string]struct{})
	accounted := make(map[string]struct{}, len(artifactCores))
	for _, profile := range decodeRetroArchCatalog(t).Profiles {
		includedFiles[profile.File] = struct{}{}
		assert.Contains(t, artifacts, profile.File+".zip", "catalog core missing from the buildbot inventory")
		assert.NotContains(t, excludedCores, profile.Core, "core is both included and excluded")
		accounted[profile.Core] = struct{}{}
	}
	assert.Len(t, includedFiles, 169)
	for core := range excludedCores {
		accounted[core] = struct{}{}
	}
	assert.Equal(t, artifactCores, accounted, "every artifact is either included or explicitly excluded")
}

func TestCatalogContentURIProfilesAreBoundedAndReviewed(t *testing.T) {
	t.Parallel()

	entries, err := loadStandaloneCatalog(standaloneCatalogJSON)
	require.NoError(t, err)
	type target struct{ pkg, system string }
	expected := map[string]target{
		"DuckStation.PSX":  {"com.github.stenzek.duckstation", "PSX"},
		"PPSSPP.PSP":       {"org.ppsspp.ppsspp", "PSP"},
		"Dolphin.GameCube": {"org.dolphinemu.dolphinemu", "GameCube"},
		"Dolphin.Wii":      {"org.dolphinemu.dolphinemu", "Wii"},
	}
	require.Len(t, entries, len(expected))
	for i := range entries {
		definition := &entries[i].definition
		want, known := expected[definition.ID]
		require.True(t, known, definition.ID)
		assert.Equal(t, want, target{definition.Package, definition.System})
		assert.Equal(t, 2, definition.Version)
		assert.Equal(t, "android.intent.action.VIEW", definition.Action)
		assert.Equal(t, StrategyContentURI, definition.Strategy)
		assert.Equal(t, "none", definition.StorageAccess)
		assert.True(t, definition.GrantReadURI)
		assert.True(t, definition.ClipData)
		assert.Zero(t, definition.MaxTargetSDK)
		assert.NotEmpty(t, definition.Extensions)
		assert.NotContains(t, definition.Extensions, ".m3u", "playlists need multi-document grants")
		assert.NotEqual(t, definition.DataSource != "", len(definition.Extras) > 0,
			"media reaches the app as intent data or as an extra, never both")
		assert.Empty(t, entries[i].coreFile)
		assert.NotEqual(t, "Android", entries[i].group, "every reviewed app has its own launcher group")
	}
}

func TestCatalogOverridesAndCompatibilityIDs(t *testing.T) {
	t.Parallel()

	byID := make(map[string]retroArchProfile)
	for _, profile := range decodeRetroArchCatalog(t).Profiles {
		byID[profile.ID] = profile
		assert.NotEqual(t, "mame", profile.Core, "the arm64 index has no unsuffixed MAME core")
		assert.NotEqual(t, "vice_x128", profile.Core, "a C128 core must not pass as a C64 launcher")
	}
	assert.Equal(t, "NES", byID["RetroArch.Mesen"].System)
	assert.Equal(t, "mesen", byID["RetroArch.Mesen"].Core)
	assert.Equal(t, "fbneo", byID["RetroArch.FBNeo.Arcade"].Core)
	assert.Equal(t, "mednafen_pce", byID["RetroArch.MednafenPce.TurboGrafx16"].Core)
	assert.Equal(t, "mednafen_pce_libretro_android.so", byID["RetroArch.MednafenPce.TurboGrafx16"].File)
	assert.Equal(t, "azahar_libretro.so", byID["RetroArch.Azahar.System3DS"].File)
	assert.Equal(t, "mupen64plus_next_gles3", byID["RetroArch.Mupen64PlusNext"].Core)
}

func TestLoadCatalogExpandsEveryProfile(t *testing.T) {
	t.Parallel()

	entries, err := loadCatalog()
	require.NoError(t, err)
	require.Len(t, entries, 264)
	for i := range entries {
		definition := &entries[i].definition
		require.NoError(t, definition.Validate(), definition.ID)
		assert.NotEmpty(t, entries[i].group, definition.ID)
		if definition.Strategy != StrategyFilesystemPath {
			continue
		}
		assert.Equal(t, retroArchGroup, entries[i].group)
		assert.Contains(t, definition.Extras, LaunchExtra{
			Name: "LIBRETRO", Type: "string", Source: "application_data", Suffix: "cores/" + entries[i].coreFile,
		})
	}
}

// Registration order is the precedence Core falls back on, so the order of a
// system's launchers is reviewed data. These systems pin the policy: a reviewed
// standalone app first, then mature compatibility ahead of accuracy.
func TestCatalogOrderIsLauncherPrecedence(t *testing.T) {
	t.Parallel()

	entries, err := loadCatalog()
	require.NoError(t, err)
	bySystem := make(map[string][]string)
	for i := range entries {
		system := entries[i].definition.System
		bySystem[system] = append(bySystem[system], entries[i].definition.ID)
	}

	assert.Equal(t, []string{
		"DuckStation.PSX", "RetroArch.Swanstation.PSX", "RetroArch.BeetlePSXHW",
		"RetroArch.MednafenPsx.PSX", "RetroArch.PcsxRearmed.PSX",
	}, bySystem["PSX"])
	assert.Equal(t, []string{"PPSSPP.PSP", "RetroArch.PPSSPP"}, bySystem["PSP"])
	assert.Equal(t, []string{"Dolphin.GameCube", "RetroArch.Dolphin.GameCube"}, bySystem["GameCube"])
	assert.Equal(t, []string{
		"RetroArch.Mesen", "RetroArch.FCEUmm", "RetroArch.Fixnes.NES", "RetroArch.Mesen2.NES",
		"RetroArch.Nestopia.NES", "RetroArch.Quicknes.NES", "RetroArch.Rustynes.NES",
	}, bySystem["NES"])
	assert.Equal(t, []string{
		"RetroArch.Mupen64PlusNext", "RetroArch.Mupen64plusNextGles2.Nintendo64", "RetroArch.ParallelN64.Nintendo64",
	}, bySystem["Nintendo64"])

	for system, first := range map[string]string{
		"Arcade":       "RetroArch.FBNeo.Arcade",
		"Gameboy":      "RetroArch.Gambatte.Gameboy",
		"GBA":          "RetroArch.mGBA",
		"SNES":         "RetroArch.SNES9x",
		"TurboGrafx16": "RetroArch.MednafenPce.TurboGrafx16",
		"Wii":          "Dolphin.Wii",
	} {
		require.NotEmpty(t, bySystem[system], system)
		assert.Equal(t, first, bySystem[system][0], system)
	}
	assert.Equal(t, []string{"RetroArch.FBNeo.Arcade", "RetroArch.Mame2003Plus.Arcade", "RetroArch.Mamearcade.Arcade"},
		bySystem["Arcade"][:3])
}

func TestLoadCatalogRejectsMalformedData(t *testing.T) {
	t.Parallel()

	for name, data := range map[string]string{
		"empty":          "",
		"unknown field":  `{"version":1,"surprise":true}`,
		"trailing value": `{"version":1}{}`,
		"wrong package": `{"version":1,"package":"org.example.other","activity":"` + retroArchActivity +
			`","source":{"buildbot":"b","coreInfoRepository":"r","coreInfoRevision":"v","reviewed":"d"},` +
			`"profiles":[{"id":"RetroArch.Test","name":"T","system":"NES","core":"t",` +
			`"file":"t_libretro_android.so","extensions":[".nes"]}]}`,
		"path in core file": `{"version":1,"package":"` + retroArchPackage + `","activity":"` + retroArchActivity +
			`","source":{"buildbot":"b","coreInfoRepository":"r","coreInfoRevision":"v","reviewed":"d"},` +
			`"profiles":[{"id":"RetroArch.Test","name":"T","system":"NES","core":"t",` +
			`"file":"../t_libretro_android.so","extensions":[".nes"]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := loadRetroArchCatalog([]byte(data))
			require.ErrorIs(t, err, ErrLaunchDefinition)
		})
	}

	_, err := loadStandaloneCatalog([]byte(`{"catalogVersion":1,"reviewed":"d","profiles":[{"version":1}]}`))
	require.ErrorIs(t, err, ErrLaunchDefinition)
}
