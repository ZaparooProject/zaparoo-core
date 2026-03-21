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
	"testing"

	gozapscript "github.com/ZaparooProject/go-zapscript"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/userdb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/playlists"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fixtures"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/zapscript"
	"github.com/stretchr/testify/mock"
)

// benchPipelineLaunchers provides test launchers for the NES system.
var benchPipelineLaunchers = []platforms.Launcher{{
	ID:         "nes-launcher",
	SystemID:   "NES",
	Extensions: []string{".nes"},
	Folders:    []string{"/roms/system/"},
}}

// pipelineBenchEnv holds shared benchmark environment for scan-to-launch tests.
type pipelineBenchEnv struct {
	db        *database.Database
	cfg       *config.Instance
	pl        *mocks.MockPlatform
	lm        *state.LauncherManager
	cleanup   func()
	exprEnv   gozapscript.ArgExprEnv
	gameNames []string
}

// setupPipelineBench creates the full environment needed for scan-to-launch
// benchmarking: real MediaDB with titles, mock UserDB, mock platform, real config.
func setupPipelineBench(b *testing.B, n int) *pipelineBenchEnv {
	b.Helper()

	// Real MediaDB with production schema
	mediaDB, mediaCleanup := testhelpers.NewInMemoryMediaDB(b)

	// Seed canonical tags and populate with titles
	ss := &database.ScanState{
		SystemIDs:  make(map[string]int),
		TitleIDs:   make(map[string]int),
		MediaIDs:   make(map[string]int),
		TagTypeIDs: make(map[string]int),
		TagIDs:     make(map[string]int),
	}
	if err := mediascanner.SeedCanonicalTags(mediaDB, ss); err != nil {
		b.Fatal(err)
	}

	filenames := fixtures.BuildBenchFilenames(n)
	if err := mediaDB.BeginTransaction(true); err != nil {
		b.Fatal(err)
	}
	for i, fn := range filenames {
		_, _, err := mediascanner.AddMediaPath(mediaDB, ss, "NES", fn, false, false, nil)
		if i == 0 && err != nil {
			b.Fatal(err)
		}
		if (i+1)%10_000 == 0 {
			if err := mediaDB.CommitTransaction(); err != nil {
				b.Fatal(err)
			}
			mediascanner.FlushScanStateMaps(ss)
			if err := mediaDB.BeginTransaction(true); err != nil {
				b.Fatal(err)
			}
		}
	}
	if err := mediaDB.CommitTransaction(); err != nil {
		b.Fatal(err)
	}
	if err := mediaDB.RebuildSlugSearchCache(); err != nil {
		b.Fatal(err)
	}

	// Mock UserDB — only needs GetEnabledMappings and GetZapLinkHost
	mockUserDB := testhelpers.NewMockUserDBI()
	mockUserDB.On("GetEnabledMappings").Return([]database.Mapping{}, nil)
	mockUserDB.On("GetZapLinkHost", mock.Anything).Return(false, false, nil)

	db := &database.Database{
		MediaDB: mediaDB,
		UserDB:  mockUserDB,
	}

	// Real config
	cfg, err := testhelpers.NewTestConfig(nil, b.TempDir())
	if err != nil {
		b.Fatal(err)
	}

	// Mock platform — only LaunchMedia, LookupMapping, ID need stubbing
	pl := mocks.NewMockPlatform()
	pl.On("LaunchMedia", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	pl.On("LookupMapping", mock.Anything).Return("", false)
	pl.On("ID").Return("test-platform")
	pl.On("Launchers", mock.Anything).Return(benchPipelineLaunchers)

	// Launcher manager and cache
	lm := state.NewLauncherManager()
	helpers.GlobalLauncherCache.InitializeFromSlice(benchPipelineLaunchers)

	// Extract game names
	const pathPrefix = "/roms/system/"
	const ext = ".nes"
	gameNames := make([]string, n)
	for i, fn := range filenames {
		gameNames[i] = fn[len(pathPrefix) : len(fn)-len(ext)]
	}

	return &pipelineBenchEnv{
		db:        db,
		cfg:       cfg,
		pl:        pl,
		lm:        lm,
		exprEnv:   gozapscript.ArgExprEnv{Platform: "test-platform"},
		gameNames: gameNames,
		cleanup:   mediaCleanup,
	}
}

func BenchmarkScanToLaunch_ExactMatch(b *testing.B) {
	b.ReportAllocs()
	env := setupPipelineBench(b, 10_000)
	defer env.cleanup()

	// Token text: @nes/GameName triggers launch.title command
	gameName := env.gameNames[0]
	tokenText := "@nes/" + gameName

	token := tokens.Token{
		Text:   tokenText,
		UID:    "04:AA:BB:CC:DD:EE:FF",
		Source: "bench",
	}

	b.ResetTimer()
	for b.Loop() {
		// 1. Mapping check (real production code)
		mappedValue, hasMapping := getMapping(env.cfg, env.db, env.pl, token)
		scriptText := token.Text
		if hasMapping {
			scriptText = mappedValue
		}

		// 2. ZapScript parse (real, external go-zapscript module)
		parser := gozapscript.NewParser(scriptText)
		script, err := parser.ParseScript()
		if err != nil {
			b.Fatal(err)
		}

		// 3. Command execution (real RunCommand -> cmdTitle -> ResolveTitle -> mock launch)
		for i, cmd := range script.Cmds {
			_, err = zapscript.RunCommand(
				env.pl, env.cfg, playlists.PlaylistController{}, token, cmd,
				len(script.Cmds), i, env.db, env.lm, &env.exprEnv,
			)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkScanToLaunch_DirectPath(b *testing.B) {
	b.ReportAllocs()
	env := setupPipelineBench(b, 1_000)
	defer env.cleanup()

	// Direct file path — no title resolution, just mapping check + parse + launch
	token := tokens.Token{
		Text:   "/roms/system/Super Mario Bros 1.nes",
		UID:    "04:AA:BB:CC:DD:EE:FF",
		Source: "bench",
	}

	b.ResetTimer()
	for b.Loop() {
		// 1. Mapping check
		mappedValue, hasMapping := getMapping(env.cfg, env.db, env.pl, token)
		scriptText := token.Text
		if hasMapping {
			scriptText = mappedValue
		}

		// 2. ZapScript parse
		parser := gozapscript.NewParser(scriptText)
		script, err := parser.ParseScript()
		if err != nil {
			b.Fatal(err)
		}

		// 3. Command execution (cmdLaunch with direct path)
		for i, cmd := range script.Cmds {
			_, err = zapscript.RunCommand(
				env.pl, env.cfg, playlists.PlaylistController{}, token, cmd,
				len(script.Cmds), i, env.db, env.lm, &env.exprEnv,
			)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkScanToLaunch_WithMapping(b *testing.B) {
	b.ReportAllocs()
	env := setupPipelineBench(b, 10_000)
	defer env.cleanup()

	// Add a mapping rule to UserDB
	gameName := env.gameNames[0]
	mappings := []database.Mapping{{
		Type:     userdb.MappingTypeID,
		Match:    userdb.MatchTypeExact,
		Pattern:  "04:aa:bb:cc:dd:ee:ff",
		Override: "@nes/" + gameName,
		Enabled:  true,
	}}
	// Reset and re-stub with a mapping result
	env.db.UserDB.(*testhelpers.MockUserDBI).ExpectedCalls = nil
	env.db.UserDB.(*testhelpers.MockUserDBI).On("GetEnabledMappings").Return(mappings, nil)
	env.db.UserDB.(*testhelpers.MockUserDBI).On("GetZapLinkHost", mock.Anything).Return(false, false, nil)

	token := tokens.Token{
		Text:   "",
		UID:    "04:AA:BB:CC:DD:EE:FF",
		Source: "bench",
	}

	b.ResetTimer()
	for b.Loop() {
		// 1. Mapping check — should match on UID
		mappedValue, hasMapping := getMapping(env.cfg, env.db, env.pl, token)
		if !hasMapping {
			b.Fatal("expected mapping match")
		}

		// 2. ZapScript parse of mapped value
		parser := gozapscript.NewParser(mappedValue)
		script, err := parser.ParseScript()
		if err != nil {
			b.Fatal(err)
		}

		// 3. Command execution
		for i, cmd := range script.Cmds {
			_, err = zapscript.RunCommand(
				env.pl, env.cfg, playlists.PlaylistController{}, token, cmd,
				len(script.Cmds), i, env.db, env.lm, &env.exprEnv,
			)
			if err != nil {
				b.Fatal(err)
			}
		}
	}
}
