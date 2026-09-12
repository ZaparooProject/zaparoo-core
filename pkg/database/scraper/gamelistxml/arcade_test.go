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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediascanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
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

func TestArcadeSetStemRejectsNonIdentities(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, sourcePath string }{
		{"nul byte", "pac\x00man.zip"},
		{"newline", "pacman\n.zip"},
		{"carriage return", "pacman\r.zip"},
		{"url", "http://example.com/pacman.zip"},
		{"archive with empty stem", "./.zip"},
		{"over length limit", strings.Repeat("a", 129) + ".zip"},
		{"space", "Pac Man.zip"},
		{"unsupported extension", "pacman.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, arcadeSetStem(tc.sourcePath))
		})
	}
	assert.Equal(t, strings.Repeat("a", 128), arcadeSetStem(strings.Repeat("a", 128)+".zip"))
}

// hostileFs refuses to open a descriptor that stats cleanly, standing in for a
// file whose permissions or backing storage fail after the walk saw it.
type hostileFs struct {
	afero.Fs
	openErr error
}

func (h hostileFs) Open(name string) (afero.File, error) {
	if h.openErr != nil {
		return nil, h.openErr
	}
	return h.Fs.Open(name) //nolint:wrapcheck // test double forwards verbatim
}

func wrapTestFs(wrap func(afero.Fs) afero.Fs, base afero.Fs) afero.Fs {
	if wrap == nil {
		return base
	}
	return wrap(base)
}

func TestReadArcadeSetNameRejectsUnreadableDescriptors(t *testing.T) {
	t.Parallel()
	valid := `<misterromdescription><setname>pacman</setname></misterromdescription>`
	for _, tc := range []struct {
		fs            func(afero.Fs) afero.Fs
		name, content string
	}{
		{
			name: "unopenable", content: valid,
			fs: func(base afero.Fs) afero.Fs {
				return hostileFs{Fs: base, openErr: os.ErrPermission}
			},
		},
		{
			name: "header longer than the read bound",
			content: `<misterromdescription><about>` + strings.Repeat("x", maxArcadeMRAHeaderBytes) +
				`</about><setname>pacman</setname></misterromdescription>`,
		},
		{name: "trailing text", content: valid + "garbage"},
		{name: "trailing syntax error", content: valid + "<"},
		{name: "trailing directive", content: valid + "<!DOCTYPE mra>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base := afero.NewMemMapFs()
			filename := filepath.Join(t.TempDir(), "test.mra")
			require.NoError(t, base.MkdirAll(filepath.Dir(filename), 0o750))
			require.NoError(t, afero.WriteFile(base, filename, []byte(tc.content), 0o600))
			assert.Empty(t, readArcadeSetName(wrapTestFs(tc.fs, base), filename))
		})
	}
}

func TestReadArcadeSetNameSkipsSymlinkedDescriptors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "Pac-Man.mra")
	require.NoError(t, os.WriteFile(target,
		[]byte(`<misterromdescription><setname>pacman</setname></misterromdescription>`), 0o600))
	alias := filepath.Join(dir, "alias.mra")
	require.NoError(t, os.Symlink(target, alias))
	fs := afero.NewOsFs()
	assert.Equal(t, "pacman", readArcadeSetName(fs, target))
	assert.Empty(t, readArcadeSetName(fs, alias),
		"an Arcade Organizer alias must not read as a second row for the same set")
}

func TestIndexArcadeSetsWithoutSetReferences(t *testing.T) {
	t.Parallel()
	s := &GamelistXMLScraper{
		fs:              hostileFs{Fs: afero.NewMemMapFs(), openErr: os.ErrPermission},
		matchArcadeSets: true,
	}
	bySet, err := s.indexArcadeSets(t.Context(),
		[]database.MediaWithFullPath{{DBID: 10, Path: filepath.Join("_Arcade", "Pac-Man.mra")}},
		parsedGamelistSystem{Files: []parsedGamelistFile{{Games: []esapi.Game{
			{Path: "./Pac Man.rom"}, {Path: ""},
		}}}})
	require.NoError(t, err)
	assert.Empty(t, bySet, "no set-name references means no descriptor reads at all")
}

func TestIndexArcadeSetsSkipsUnusableDescriptors(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	root := filepath.Join(t.TempDir(), "_Arcade")
	require.NoError(t, fs.MkdirAll(root, 0o750))
	good := filepath.Join(root, "Pac-Man.mra")
	truncated := filepath.Join(root, "Truncated.mra")
	require.NoError(t, afero.WriteFile(fs, good,
		[]byte(`<misterromdescription><setname>pacman</setname></misterromdescription>`), 0o600))
	require.NoError(t, afero.WriteFile(fs, truncated, []byte(`<misterromdescription><setname>pac`), 0o600))
	bySet, err := (&GamelistXMLScraper{fs: fs, matchArcadeSets: true}).indexArcadeSets(t.Context(),
		[]database.MediaWithFullPath{
			{DBID: 10, MediaTitleDBID: 1, Path: good},
			{DBID: 20, MediaTitleDBID: 2, Path: truncated},
			{DBID: 30, MediaTitleDBID: 3, Path: filepath.Join(root, "Absent.mra")},
			{DBID: 40, MediaTitleDBID: 4, Path: filepath.Join(root, "Not a descriptor.txt")},
		},
		parsedGamelistSystem{Files: []parsedGamelistFile{{
			RootPath: root, Games: []esapi.Game{{Path: "./pacman.zip"}},
		}}})
	require.NoError(t, err)
	require.Len(t, bySet["pacman"], 1, "a descriptor that cannot be parsed must not make its set ambiguous")
	assert.Equal(t, int64(10), bySet["pacman"][0].DBID)
}

// TestResolveSystemsKeepsCustomBundleWithoutLauncherPaths covers the granular
// MiSTer arcade systems: their media is indexed by the arcade classifier rather
// than by a launcher scan folder, so path discovery finds nothing for them and
// their bundle would never be read.
func TestResolveSystemsKeepsCustomBundleWithoutLauncherPaths(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	custom := t.TempDir()
	bundle := filepath.Join(custom, systemdefs.SystemCPS1, "gamelist.xml")
	require.NoError(t, fs.MkdirAll(filepath.Dir(bundle), 0o750))
	require.NoError(t, afero.WriteFile(fs, bundle,
		[]byte(`<gameList><game><path>./sf2.zip</path><name>Street Fighter II</name></game></gameList>`), 0o600))
	pl := mocks.NewMockPlatform()
	pl.SetupBasicMock()
	mdb := helpers.NewMockMediaDBI()
	mdb.On("IndexedSystems").Return([]string{systemdefs.SystemCPS1, systemdefs.SystemCPS2}, nil)
	mdb.On("FindSystemBySystemID", systemdefs.SystemCPS1).Return(database.System{DBID: 1}, nil)
	mdb.On("FindSystemBySystemID", systemdefs.SystemCPS2).Return(database.System{DBID: 2}, nil)

	systems, _, err := resolveSystemsFromPlatform(t.Context(), newCustomGamelistConfig(t, custom), pl, fs, mdb, nil)
	require.NoError(t, err)
	require.Len(t, systems, 1, "only the system with an installed bundle survives having no launcher paths")
	assert.Equal(t, systemdefs.SystemCPS1, systems[0].ID)
	assert.Empty(t, systems[0].ROMPaths)

	plain, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)
	systems, _, err = resolveSystemsFromPlatform(t.Context(), plain, pl, fs, mdb, nil)
	require.NoError(t, err)
	assert.Empty(t, systems, "without a configured bundle directory the systems are still skipped")
}

// TestPlatformScraperGatesArcadeSetsByPlatform pins the wiring: set-name
// matching reads MRA descriptors off the scrape path, so it must stay off for
// platforms that have no `_Arcade` to read.
func TestPlatformScraperGatesArcadeSetsByPlatform(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		platformID string
		want       bool
	}{
		{platformID: ids.Mister, want: true},
		{platformID: ids.Mistex, want: true},
		{platformID: ids.Batocera, want: false},
		{platformID: "mock-platform", want: false},
	} {
		t.Run(tc.platformID, func(t *testing.T) {
			t.Parallel()
			pl := mocks.NewMockPlatform()
			pl.SetupBasicMock()
			pl.ExpectedCalls = nil
			pl.On("ID").Return(tc.platformID)
			pl.On("RootDirs", mock.Anything).Return([]string{})
			pl.On("Launchers", mock.Anything).Return([]platforms.Launcher{})
			mdb := helpers.NewMockMediaDBI()
			mdb.On("IndexedSystems").Return([]string{}, nil)
			cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
			require.NoError(t, err)
			ch := make(chan scraper.ScrapeUpdate, 8)
			require.NoError(t, NewPlatformScraper().Scrape(t.Context(), cfg, pl, afero.NewMemMapFs(),
				&database.Database{MediaDB: mdb}, scraper.ScrapeOptions{Pauser: syncutil.NewPauser()}, nil, ch))
			var last scraper.ScrapeUpdate
			for update := range ch {
				require.NoError(t, update.FatalErr)
				last = update
			}
			assert.True(t, last.Done)
			assert.Equal(t, tc.want, arcadeSetMatchingEnabled(pl))
		})
	}
}

func TestArcadeArtworkFallbackExtensions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, sourcePath, artwork string }{
		{"png", "./pacman.zip", "pacman.png"},
		{"jpg", "./pacman.zip", "pacman.jpg"},
		{"jpeg", "./pacman.zip", "pacman.jpeg"},
		{"webp", "./pacman.zip", "pacman.webp"},
		{"upper case source keeps its own casing", "./PACMAN.ZIP", "PACMAN.jpg"},
		{"upper case source falls back to lower", "./PACMAN.ZIP", "pacman.jpg"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			bundle := filepath.Join(t.TempDir(), "Arcade")
			image := filepath.Join(bundle, "media", "images", tc.artwork)
			require.NoError(t, fs.MkdirAll(filepath.Dir(image), 0o750))
			require.NoError(t, afero.WriteFile(fs, image, []byte("art"), 0o600))
			mapped := (&GamelistXMLScraper{fs: fs}).MapToDB(&GamelistRecord{
				SystemRootPath: t.TempDir(), AssetRootPath: bundle, MatchKind: gamelistMatchArcadeSet,
				MediaLevelWriteSafe: true,
				MediaDirsByRoot:     []map[string]string{statMediaDirsFS(fs, bundle)},
				Game:                esapi.Game{Path: tc.sourcePath},
			})
			prop, ok := propertyByType(mapped.MediaProps, "property:image-image")
			require.True(t, ok, "set-name artwork must use the same extensions as every other match")
			assert.Equal(t, filepath.ToSlash(image), prop.Text)
		})
	}
}

// TestArcadeSetNameWinsOverCompetingSlug pins the precedence that makes clone
// sets safe: `<setname>` is the arcade ROM's identity, while a catalog <name>
// is only a label several sets can share. Ranking the slug first would send a
// clone's metadata to the parent's MRA.
func TestArcadeSetNameWinsOverCompetingSlug(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	root := filepath.Join(t.TempDir(), "_Arcade")
	require.NoError(t, fs.MkdirAll(root, 0o750))
	midway := database.Media{DBID: 10, MediaTitleDBID: 1, Path: filepath.Join(root, "Pac-Man (Midway).mra")}
	japan := database.Media{DBID: 20, MediaTitleDBID: 2, Path: filepath.Join(root, "PuckMan (Japan).mra")}
	for path, setName := range map[string]string{midway.Path: "pacman", japan.Path: "puckman"} {
		require.NoError(t, afero.WriteFile(fs, path,
			[]byte(`<misterromdescription><setname>`+setName+`</setname></misterromdescription>`), 0o600))
	}
	indexes := mediaBySlugAndPath("pacman", &database.MediaTitle{DBID: 1, Slug: "pacman"}, midway, japan)
	gl, err := esapi.ParseGameListXML([]byte(
		`<gameList><game><path>./puckman.zip</path><name>Pac-Man</name><desc>Japan set</desc></game></gameList>`))
	require.NoError(t, err)
	parsed := parsedGamelistSystem{Files: []parsedGamelistFile{{RootPath: root, Games: gl.Games}}}
	s := &GamelistXMLScraper{fs: fs, matchArcadeSets: true}
	indexes.ArcadeBySetName, err = s.indexArcadeSets(t.Context(), []database.MediaWithFullPath{
		{DBID: midway.DBID, MediaTitleDBID: midway.MediaTitleDBID, Path: midway.Path},
		{DBID: japan.DBID, MediaTitleDBID: japan.MediaTitleDBID, Path: japan.Path},
	}, parsed)
	require.NoError(t, err)
	records, err := s.loadRecordsFromParsed(t.Context(),
		scraper.ScrapeSystem{ID: systemdefs.SystemArcade, ROMPaths: []string{root}}, indexes, parsed)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, japan.DBID, records[0].MatchedMediaDBID,
		"the entry named its set, so the slug of its display name must not redirect the write")
	assert.Equal(t, gamelistMatchArcadeSet, records[0].MatchKind)
}

// TestArcadeSetNameOutranksTitleGuess reproduces what a Skraper arcade bundle
// does on real hardware: MAME clone sets share one display name, so an entry
// with no indexed set of its own slug-matches the title and takes the canonical
// MRA from the entry that named that set exactly.
func TestArcadeSetNameOutranksTitleGuess(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, gamelist string
	}{
		{
			name: "guess read first",
			gamelist: `<game><path>./rtypeclone.zip</path><name>R-Type</name><desc>GUESS</desc></game>` +
				`<game><path>./rtype.zip</path><name>R-Type</name><desc>SET</desc></game>`,
		},
		{
			name: "guess read last",
			gamelist: `<game><path>./rtype.zip</path><name>R-Type</name><desc>SET</desc></game>` +
				`<game><path>./rtypeclone.zip</path><name>R-Type</name><desc>GUESS</desc></game>`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			root := filepath.Join(t.TempDir(), "_Arcade")
			require.NoError(t, fs.MkdirAll(root, 0o750))
			media := database.Media{DBID: 10, MediaTitleDBID: 1, Path: filepath.Join(root, "R-Type (World).mra")}
			require.NoError(t, afero.WriteFile(fs, media.Path,
				[]byte(`<misterromdescription><setname>rtype</setname></misterromdescription>`), 0o600))
			indexes := mediaBySlugAndPath("rtype", &database.MediaTitle{DBID: 1, Slug: "rtype"}, media)
			gl, err := esapi.ParseGameListXML([]byte(`<gameList>` + tc.gamelist + `</gameList>`))
			require.NoError(t, err)
			parsed := parsedGamelistSystem{Files: []parsedGamelistFile{{RootPath: root, Games: gl.Games}}}
			s := &GamelistXMLScraper{fs: fs, matchArcadeSets: true}
			indexes.ArcadeBySetName, err = s.indexArcadeSets(t.Context(), []database.MediaWithFullPath{{
				DBID: media.DBID, MediaTitleDBID: media.MediaTitleDBID, Path: media.Path,
			}}, parsed)
			require.NoError(t, err)
			records, err := s.loadRecordsFromParsed(t.Context(),
				scraper.ScrapeSystem{ID: systemdefs.SystemArcade, ROMPaths: []string{root}}, indexes, parsed)
			require.NoError(t, err)
			require.Len(t, records, 1)
			assert.Equal(t, media.DBID, records[0].MatchedMediaDBID)
			assert.Equal(t, "SET", records[0].Game.Desc,
				"a clone sharing the display name must not displace the exact set-name match")
			assert.Equal(t, gamelistMatchArcadeSet, records[0].MatchKind)
			assert.True(t, records[0].MediaLevelWriteSafe)
		})
	}
}

// TestArcadeSetNameYieldsToPathMatch keeps the other half of the rule: a record
// that named the row by path is at least as exact as the set name, so it holds
// the row and the set entry is dropped.
func TestArcadeSetNameYieldsToPathMatch(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	root := filepath.Join(t.TempDir(), "_Arcade")
	require.NoError(t, fs.MkdirAll(root, 0o750))
	media := database.Media{DBID: 10, MediaTitleDBID: 1, Path: filepath.Join(root, "R-Type.mra")}
	require.NoError(t, afero.WriteFile(fs, media.Path,
		[]byte(`<misterromdescription><setname>rtype</setname></misterromdescription>`), 0o600))
	indexes := mediaBySlugAndPath("rtype", &database.MediaTitle{DBID: 1, Slug: "rtype"}, media)
	gl, err := esapi.ParseGameListXML([]byte(`<gameList>` +
		`<game><path>./rtype.zip</path><name>R-Type</name><desc>SET</desc></game>` +
		`<game><path>./R-Type.mra</path><name>R-Type</name><desc>PATH</desc></game>` +
		`</gameList>`))
	require.NoError(t, err)
	parsed := parsedGamelistSystem{Files: []parsedGamelistFile{{RootPath: root, Games: gl.Games}}}
	s := &GamelistXMLScraper{fs: fs, matchArcadeSets: true}
	indexes.ArcadeBySetName, err = s.indexArcadeSets(t.Context(), []database.MediaWithFullPath{{
		DBID: media.DBID, MediaTitleDBID: media.MediaTitleDBID, Path: media.Path,
	}}, parsed)
	require.NoError(t, err)
	records, err := s.loadRecordsFromParsed(t.Context(),
		scraper.ScrapeSystem{ID: systemdefs.SystemArcade, ROMPaths: []string{root}}, indexes, parsed)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "PATH", records[0].Game.Desc)
}

func TestArcadeCompanionSetMatchThroughIndex(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	root := filepath.Join(t.TempDir(), "_Arcade")
	require.NoError(t, fs.MkdirAll(root, 0o750))
	media := database.Media{DBID: 10, MediaTitleDBID: 1, Path: filepath.Join(root, "Pac-Man.mra")}
	require.NoError(t, afero.WriteFile(fs, media.Path,
		[]byte(`<misterromdescription><setname>pacman</setname></misterromdescription>`), 0o600))
	gl, err := esapi.ParseGameListXML([]byte(`<gameList>` +
		`<game source="ZaparooCompanion" id="42"><name>Pac-Man</name><desc>Parent</desc></game>` +
		`<game source="ZaparooCompanion" parentid="42"><path>./pacman.zip</path><region>jp</region></game>` +
		`</gameList>`))
	require.NoError(t, err)
	parsed := parsedGamelistSystem{Files: []parsedGamelistFile{{RootPath: root, Games: gl.Games}}}
	indexes := mediaByPath(media)
	s := &GamelistXMLScraper{fs: fs, matchArcadeSets: true}
	indexes.ArcadeBySetName, err = s.indexArcadeSets(t.Context(), []database.MediaWithFullPath{{
		DBID: media.DBID, MediaTitleDBID: media.MediaTitleDBID, Path: media.Path,
	}}, parsed)
	require.NoError(t, err)
	require.Contains(t, indexes.ArcadeBySetName, "pacman",
		"companion child ROM references must contribute the set names the index reads")
	_, children := companionEntriesFromParsed(t.Context(), scraper.ScrapeSystem{ID: systemdefs.SystemArcade}, parsed)
	require.Len(t, children, 1)
	matched := matchCompanionChildMedia(scraper.ScrapeSystem{ID: systemdefs.SystemArcade}, children[0], indexes, nil)
	require.Equal(t, []database.Media{media}, matched.Media)
	assert.True(t, matched.MediaLevelWriteSafe)
}

func TestScrapeLoop_ArcadeIndexCanceled(t *testing.T) {
	t.Parallel()
	base := afero.NewMemMapFs()
	root := filepath.Join(t.TempDir(), "_Arcade")
	custom := t.TempDir()
	bundle := filepath.Join(custom, systemdefs.SystemArcade)
	rows := make([]database.MediaWithFullPath, 0, 2)
	for i, name := range []string{"Pac-Man.mra", "Ms. Pac-Man.mra"} {
		path := filepath.Join(root, name)
		require.NoError(t, base.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, afero.WriteFile(base, path,
			[]byte(`<misterromdescription><setname>pacman</setname></misterromdescription>`), 0o600))
		rows = append(rows, database.MediaWithFullPath{DBID: int64(10 + i), MediaTitleDBID: 1, Path: path})
	}
	glPath := filepath.Join(bundle, "gamelist.xml")
	require.NoError(t, base.MkdirAll(bundle, 0o750))
	require.NoError(t, afero.WriteFile(base, glPath,
		[]byte(`<gameList><game><path>./pacman.zip</path><name>Pac-Man</name></game></gameList>`), 0o600))
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	mdb := helpers.NewMockMediaDBI()
	mdb.On("GetTitlesBySystemID", systemdefs.SystemArcade).Return([]database.TitleWithSystem{{
		DBID: 1, Slug: "pacman", Name: "Pac-Man", SystemDBID: 100,
	}}, nil)
	mdb.On("GetMediaBySystemID", systemdefs.SystemArcade).Return(rows, nil)
	s := &GamelistXMLScraper{
		db: mdb, fs: cancelOnOpenFs{Fs: base, suffix: ".mra", cancel: cancel},
		cfg: newCustomGamelistConfig(t, custom), matchArcadeSets: true,
	}
	ch := make(chan scraper.ScrapeUpdate, 128)
	s.scrapeLoop(ctx, scraper.ScrapeOptions{Force: true, Pauser: syncutil.NewPauser()},
		[]scraper.ScrapeSystem{{ID: systemdefs.SystemArcade, DBID: 100, ROMPaths: []string{root}}}, mdb, ch)
	var done scraper.ScrapeUpdate
	for update := range ch {
		require.NoError(t, update.FatalErr, "cancellation is not a scrape failure")
		if update.Done {
			done = update
		}
	}
	assert.True(t, done.Done)
	assert.Equal(t, 0, done.Matched)
	mdb.AssertNotCalled(t, "ApplyScrapeResult", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

// cancelOnOpenFs cancels the scrape the first time a matching file is opened,
// standing in for a user stopping a scrape while descriptors are being read.
type cancelOnOpenFs struct {
	afero.Fs
	cancel context.CancelFunc
	suffix string
}

func (c cancelOnOpenFs) Open(name string) (afero.File, error) {
	if strings.HasSuffix(strings.ToLower(name), c.suffix) {
		c.cancel()
	}
	return c.Fs.Open(name) //nolint:wrapcheck // test double forwards verbatim
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
		{"unterminated header past the read bound", strings.Repeat(" ", maxArcadeMRAHeaderBytes+1), ""},
		{"payload stops the header scan", `<misterromdescription><setname>pacman</setname>` +
			`<rom index="0"><part>` + strings.Repeat("A", maxArcadeMRAHeaderBytes*2) +
			`</part></rom></misterromdescription>`, "pacman"},
		{"set name after the payload is not a duplicate", `<misterromdescription><setname>pacman</setname>` +
			`<rom index="0"/><setname>puckman</setname></misterromdescription>`, "pacman"},
		{"payload before any set name", `<misterromdescription><rom index="0"/>` +
			`<setname>pacman</setname></misterromdescription>`, ""},
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
		if len(data) > maxArcadeMRAHeaderBytes+1 || len(source) > 4096 {
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
