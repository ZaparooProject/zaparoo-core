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

package gamelistxml

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestScrapeLoop_ArcadeSetNameBundle(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	root := filepath.Join(t.TempDir(), "_Arcade")
	custom := t.TempDir()
	bundle := filepath.Join(custom, systemdefs.SystemArcade)
	mra := filepath.Join(root, "Pac-Man (Midway).mra")
	image := filepath.Join(bundle, "images", "pacman.png")
	for path, content := range map[string]string{
		mra:   "<misterromdescription><setname>pacman</setname></misterromdescription>",
		image: "image",
		filepath.Join(bundle, "gamelist.xml"): `<gameList><game><path>./pacman.zip</path>
<name>Different catalog title</name><desc>Arcade metadata</desc><image>./images/pacman.png</image>
</game></gameList>`,
	} {
		require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, afero.WriteFile(fs, path, []byte(content), 0o600))
	}
	mdb := helpers.NewMockMediaDBI()
	mdb.On("GetTitlesBySystemID", systemdefs.SystemArcade).Return([]database.TitleWithSystem{{
		DBID: 1, Slug: "pacman", Name: "Pac-Man", SystemDBID: 100,
	}}, nil)
	mdb.On("GetMediaBySystemID", systemdefs.SystemArcade).Return([]database.MediaWithFullPath{{
		DBID: 10, MediaTitleDBID: 1, Path: mra,
	}}, nil)
	mdb.On("ApplyScrapeResult", mock.Anything, int64(10), int64(1),
		mock.MatchedBy(func(w *database.ScrapeWrite) bool {
			prop, ok := propertyByType(w.MediaProps, "property:image-image")
			return assert.True(t, ok) && assert.Equal(t, filepath.ToSlash(image), prop.Text)
		})).Return(nil).Once()
	s := &GamelistXMLScraper{db: mdb, fs: fs, cfg: newCustomGamelistConfig(t, custom), matchArcadeSets: true}
	ch := make(chan scraper.ScrapeUpdate, 128)
	s.scrapeLoop(t.Context(), scraper.ScrapeOptions{Force: true, Pauser: syncutil.NewPauser()},
		[]scraper.ScrapeSystem{{ID: systemdefs.SystemArcade, DBID: 100, ROMPaths: []string{root}}}, mdb, ch)
	var done scraper.ScrapeUpdate
	for update := range ch {
		require.NoError(t, update.FatalErr)
		if update.Done {
			done = update
		}
	}
	assert.Equal(t, 1, done.Matched)
	mdb.AssertExpectations(t)
}

func TestArcadeMatching(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, sourcePath, extraGame, secondSet      string
		want                                        int
		duplicateTitle, consumed, missing, disabled bool
	}{
		{name: "zip identity", sourcePath: "./pacman.zip", want: 1},
		{name: "set name", sourcePath: "pacman", want: 1},
		{name: "7z and case", sourcePath: "./PACMAN.7Z", want: 1},
		{name: "foreign absolute source", sourcePath: "/other/mame/pacman.zip", want: 1},
		{name: "foreign windows source", sourcePath: `C:\roms\pacman.zip`, want: 1},
		{name: "sibling source identity only", sourcePath: "../mame/pacman.zip", want: 1},
		{name: "duplicate set", sourcePath: "pacman.zip", secondSet: "pacman"},
		{name: "same title duplicate", sourcePath: "pacman.zip", secondSet: "pacman", duplicateTitle: true},
		{name: "scraped duplicate stays ambiguous", sourcePath: "pacman.zip", secondSet: "pacman", consumed: true},
		{name: "missing duplicate not a target", sourcePath: "pacman.zip", secondSet: "pacman", missing: true, want: 1},
		{name: "already scraped set", sourcePath: "pacman.zip", consumed: true},
		{name: "unrelated set", sourcePath: "pacman.zip", secondSet: "puckman", want: 1},
		{name: "non MiSTer unchanged", sourcePath: "pacman.zip", disabled: true},
		{
			name: "exact MRA wins", sourcePath: "pacman.zip", want: 1,
			extraGame: `<game><path>./Pac-Man.mra</path><desc>Exact path</desc></game>`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			root := filepath.Join(t.TempDir(), "_Arcade")
			require.NoError(t, fs.MkdirAll(root, 0o750))
			first := database.Media{DBID: 10, MediaTitleDBID: 1, Path: filepath.Join(root, "Pac-Man.mra")}
			second := database.Media{DBID: 20, MediaTitleDBID: 2, Path: filepath.Join(root, "Alternate.mra")}
			if tc.duplicateTitle {
				second.MediaTitleDBID = first.MediaTitleDBID
			}
			indexes := mediaByPath(first)
			rows := []database.MediaWithFullPath{{
				DBID: first.DBID, MediaTitleDBID: first.MediaTitleDBID, Path: first.Path,
			}}
			require.NoError(t, afero.WriteFile(fs, first.Path,
				[]byte(`<misterromdescription><setname>pacman</setname></misterromdescription>`), 0o600))
			if tc.secondSet != "" {
				indexes = mediaByPath(first, second)
				rows = append(rows, database.MediaWithFullPath{
					DBID: second.DBID, MediaTitleDBID: second.MediaTitleDBID, Path: second.Path, IsMissing: tc.missing,
				})
				require.NoError(t, afero.WriteFile(fs, second.Path,
					[]byte(`<misterromdescription><setname>`+tc.secondSet+`</setname></misterromdescription>`), 0o600))
			}
			if tc.consumed {
				delete(indexes.MediaByPathFold, pathFoldKey(first.Path))
			}
			gl, err := esapi.ParseGameListXML([]byte(`<gameList><game><path>` + tc.sourcePath +
				`</path><name>Catalog title</name><desc>Set metadata</desc></game>` + tc.extraGame + `</gameList>`))
			require.NoError(t, err)
			parsed := parsedGamelistSystem{Files: []parsedGamelistFile{{RootPath: root, Games: gl.Games}}}
			s := &GamelistXMLScraper{fs: fs, matchArcadeSets: !tc.disabled}
			indexes.ArcadeBySetName, err = s.indexArcadeSets(t.Context(), rows, parsed)
			require.NoError(t, err)
			// A title slug must not rescue a known ambiguous or consumed set.
			if tc.secondSet == "pacman" || tc.consumed {
				indexes.TitlesBySlug["catalogtitle"] = database.MediaTitle{DBID: 1, Slug: "catalogtitle"}
			}
			records, err := s.loadRecordsFromParsed(t.Context(), scraper.ScrapeSystem{
				ID: systemdefs.SystemArcade, ROMPaths: []string{root},
			}, indexes, parsed)
			require.NoError(t, err)
			require.Len(t, records, tc.want)
			if tc.want == 0 {
				return
			}
			assert.Equal(t, first.DBID, records[0].MatchedMediaDBID)
			assert.True(t, records[0].MediaLevelWriteSafe)
			if tc.extraGame != "" {
				assert.Equal(t, "Exact path", records[0].Game.Desc)
			} else {
				assert.Equal(t, gamelistMatchArcadeSet, records[0].MatchKind)
			}
		})
	}
}

func TestArcadeUnknownSetPreservesSlugMatch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	media := database.Media{DBID: 10, MediaTitleDBID: 1, Path: filepath.Join(root, "Pac-Man.mra")}
	indexes := mediaBySlugAndPath("pacman", &database.MediaTitle{DBID: 1, Slug: "pacman"}, media)
	indexes.ArcadeBySetName = map[string][]database.Media{"pacman": {media}}
	records, err := (&GamelistXMLScraper{}).loadRecordsFromParsed(t.Context(),
		scraper.ScrapeSystem{ID: systemdefs.SystemArcade, ROMPaths: []string{root}}, indexes,
		parsedGamelistSystem{Files: []parsedGamelistFile{{RootPath: root, Games: []esapi.Game{{
			Path: "unknown.zip", Name: "Pac-Man",
		}}}}})
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, gamelistMatchSlugOnly, records[0].MatchKind)
	assert.Equal(t, media.DBID, records[0].MatchedMediaDBID)
}

func TestArcadeArtworkFallbackAndBoundary(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	root := t.TempDir()
	bundle := filepath.Join(t.TempDir(), "Arcade")
	image := filepath.Join(bundle, "media", "images", "pacman.png")
	require.NoError(t, fs.MkdirAll(filepath.Dir(image), 0o750))
	require.NoError(t, afero.WriteFile(fs, image, []byte("art"), 0o600))
	s := &GamelistXMLScraper{fs: fs}
	mapped := s.MapToDB(&GamelistRecord{
		SystemRootPath: root, AssetRootPath: bundle, MatchKind: gamelistMatchArcadeSet,
		MediaLevelWriteSafe: true, RequireExistingImage: true,
		MediaDirsByRoot: []map[string]string{statMediaDirsFS(fs, bundle)},
		Game: esapi.Game{
			Path: "/foreign/roms/PACMAN.ZIP", Image: "../../private.png", Manual: "../../secret.pdf",
		},
	})
	prop, ok := propertyByType(mapped.MediaProps, "property:image-image")
	require.True(t, ok)
	assert.Equal(t, filepath.ToSlash(image), prop.Text)
	_, ok = propertyByType(mapped.MediaProps, "property:manual")
	assert.False(t, ok, "identity matching must not broaden asset path permissions")
}

func TestReadArcadeSetName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, xml, want string }{
		{"valid", `<misterromdescription><setname> PACMAN </setname></misterromdescription>`, "pacman"},
		{"legacy encoding", `<?xml version="1.0" encoding="ISO-8859-1"?><misterromdescription>` +
			`<name>Caf` + "\xe9" + `</name><setname>pacman</setname></misterromdescription>`, "pacman"},
		{"trailing comment", `<misterromdescription><setname>pacman</setname>` +
			`</misterromdescription><!--ok-->`, "pacman"},
		{"missing", `<misterromdescription/>`, ""},
		{"malformed", `<misterromdescription><setname>pacman</setname>`, ""},
		{"wrong root", `<game><setname>pacman</setname></game>`, ""},
		{"duplicate", `<misterromdescription><setname>pacman</setname>` +
			`<setname>puckman</setname></misterromdescription>`, ""},
		{"nested set", `<misterromdescription><rom><setname>pacman</setname></rom></misterromdescription>`, ""},
		{"unsafe set", `<misterromdescription><setname>../pacman.zip</setname></misterromdescription>`, ""},
		{"extra document", `<misterromdescription><setname>pacman</setname></misterromdescription><extra/>`, ""},
		{"oversized", strings.Repeat(" ", maxArcadeMRABytes+1), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			filename := filepath.Join(t.TempDir(), "test.mra")
			require.NoError(t, fs.MkdirAll(filepath.Dir(filename), 0o750))
			require.NoError(t, afero.WriteFile(fs, filename, []byte(tc.xml), 0o600))
			assert.Equal(t, tc.want, readArcadeSetName(fs, filename))
		})
	}
}

func TestArcadeBundleSQLiteRepeatAndForce(t *testing.T) {
	t.Parallel()
	for _, systemID := range []string{systemdefs.SystemArcade, systemdefs.SystemCPS1} {
		t.Run(systemID, func(t *testing.T) {
			t.Parallel()
			mdb, cleanup := helpers.NewInMemoryMediaDB(t)
			t.Cleanup(cleanup)
			ctx := t.Context()
			require.NoError(t, mediascanner.SeedCanonicalTags(ctx, mdb))
			system, err := mdb.FindOrInsertSystem(database.System{SystemID: systemID, Name: systemID})
			require.NoError(t, err)
			title, err := mdb.InsertMediaTitle(&database.MediaTitle{
				SystemDBID: system.DBID, Slug: "pacman", Name: "Pac-Man",
			})
			require.NoError(t, err)
			fs := afero.NewMemMapFs()
			root := filepath.Join(t.TempDir(), "_Arcade")
			custom := t.TempDir()
			bundle := filepath.Join(custom, systemID)
			mra := filepath.Join(root, "Pac-Man.mra")
			image := filepath.Join(bundle, "images", "pacman.png")
			for filename, content := range map[string]string{
				mra:   `<misterromdescription><setname>pacman</setname></misterromdescription>`,
				image: "art",
				filepath.Join(bundle, "gamelist.xml"): `<gameList><game><path>./pacman.zip</path>` +
					`<name>Catalog name</name><desc>Arcade description</desc>` +
					`<image>./images/pacman.png</image></game></gameList>`,
			} {
				require.NoError(t, fs.MkdirAll(filepath.Dir(filename), 0o750))
				require.NoError(t, afero.WriteFile(fs, filename, []byte(content), 0o600))
			}
			media, err := mdb.InsertMedia(database.Media{
				MediaTitleDBID: title.DBID, SystemDBID: system.DBID, Path: mra,
			})
			require.NoError(t, err)
			s := &GamelistXMLScraper{db: mdb, fs: fs, matchArcadeSets: true, cfg: newCustomGamelistConfig(t, custom)}
			for i, force := range []bool{false, false, true} {
				ch := make(chan scraper.ScrapeUpdate, 128)
				s.scrapeLoop(ctx, scraper.ScrapeOptions{Force: force, Pauser: syncutil.NewPauser()},
					[]scraper.ScrapeSystem{{ID: systemID, DBID: system.DBID, ROMPaths: []string{root}}}, mdb, ch)
				var done scraper.ScrapeUpdate
				for update := range ch {
					require.NoError(t, update.Err)
					require.NoError(t, update.FatalErr)
					if update.Done {
						done = update
					}
				}
				wantMatched := 1
				if i == 1 {
					wantMatched = 0
				}
				assert.Equal(t, wantMatched, done.Matched)
				props, propsErr := mdb.GetMediaProperties(ctx, media.DBID)
				require.NoError(t, propsErr)
				prop, ok := propertyByType(props, "property:image-image")
				require.True(t, ok)
				assert.Equal(t, filepath.ToSlash(image), prop.Text)
				assert.Len(t, props, 1, "repeated scrapes must not duplicate artwork properties")
				titleProps, titleErr := mdb.GetMediaTitleProperties(ctx, title.DBID)
				require.NoError(t, titleErr)
				desc, ok := propertyByType(titleProps, "property:description")
				require.True(t, ok)
				assert.Equal(t, "Arcade description", desc.Text)
				scraped, scrapeErr := mdb.GetScrapedMediaIDs(ctx, "gamelist.xml", system.DBID)
				require.NoError(t, scrapeErr)
				assert.Contains(t, scraped, media.DBID)
			}
		})
	}
}

func TestArcadeCompanionSetMatch(t *testing.T) {
	t.Parallel()
	media := database.Media{DBID: 10, MediaTitleDBID: 1, Path: filepath.Join(t.TempDir(), "Pac-Man.mra")}
	indexes := mediaByPath(media)
	indexes.ArcadeBySetName = map[string][]database.Media{"pacman": {media}}
	child := companionChild{ResolvedPath: filepath.Join(t.TempDir(), "pacman.zip")}
	system := scraper.ScrapeSystem{ID: systemdefs.SystemArcade}
	matched := matchCompanionChildMedia(system, child, indexes, nil)
	require.Equal(t, []database.Media{media}, matched.Media)
	assert.True(t, matched.MediaLevelWriteSafe)
	indexes.ArcadeBySetName["pacman"] = append(indexes.ArcadeBySetName["pacman"], database.Media{DBID: 20})
	assert.Empty(t, matchCompanionChildMedia(system, child, indexes, nil).Media)
}

func FuzzArcadeSetIdentity(f *testing.F) {
	f.Add([]byte(`<misterromdescription><setname>pacman</setname></misterromdescription>`), "./pacman.zip")
	f.Add([]byte(`<misterromdescription><setname>one</setname>`+
		`<setname>two</setname></misterromdescription>`), "../two.7z")
	f.Add([]byte(`<misterromdescription>`), `C:\roms\pacman.zip`)
	f.Fuzz(func(t *testing.T, data []byte, source string) {
		if len(data) > maxArcadeMRABytes+1 || len(source) > 4096 {
			t.Skip()
		}
		fs := afero.NewMemMapFs()
		filename := filepath.Join("arcade", "test.mra")
		require.NoError(t, fs.MkdirAll(filepath.Dir(filename), 0o750))
		require.NoError(t, afero.WriteFile(fs, filename, data, 0o600))
		set := readArcadeSetName(fs, filename)
		if set != "" {
			assert.Equal(t, set, strings.ToLower(set))
			assert.Equal(t, set, arcadeSetStem(set))
		}
		stem := arcadeSetStem(source)
		if stem != "" {
			assert.NotContains(t, stem, "/")
			assert.NotContains(t, stem, `\`)
			assert.LessOrEqual(t, len(stem), 128)
		}
	})
}

func TestIndexArcadeSetsCanceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := (&GamelistXMLScraper{matchArcadeSets: true}).indexArcadeSets(ctx,
		[]database.MediaWithFullPath{{Path: "missing.mra"}},
		parsedGamelistSystem{Files: []parsedGamelistFile{{Games: []esapi.Game{{Path: "pacman.zip"}}}}})
	require.ErrorIs(t, err, context.Canceled)
}
