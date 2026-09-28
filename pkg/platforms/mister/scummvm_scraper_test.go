//go:build linux && !android

package mister

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Keep real MiSTer scraper/source wiring without probing host storage roots.
type scummVMScrapePlatform struct{ Platform }

func (*scummVMScrapePlatform) RootDirs(*config.Instance) []string { return nil }
func (p *scummVMScrapePlatform) Launchers(*config.Instance) []platforms.Launcher {
	return []platforms.Launcher{createScummVMLauncher(&p.Platform)}
}

func writeScummVMScrapeFile(t *testing.T, fs afero.Fs, path, data string) {
	t.Helper()
	require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, afero.WriteFile(fs, path, []byte(data), 0o600))
}

func indexScummVMResults(t *testing.T, db database.MediaDBI, games ...ScummVMGame) {
	t.Helper()
	results := make([]platforms.ScanResult, 0, len(games))
	sources := scummVMMetadataSources(games)
	for i, game := range games {
		results = append(results, platforms.ScanResult{
			Path: virtualpath.CreateVirtualPath("scummvm", game.TargetID, game.Description),
			Name: game.Description, Source: sources[i], NoExt: true,
		})
	}
	scantest.IndexScanResults(t, db, systemdefs.SystemScummVM, database.ScanReconcileOpts{}, results...)
}

func runScummVMScraper(
	t *testing.T, fs afero.Fs, db database.MediaDBI, id string, opts scraper.ScrapeOptions,
) {
	t.Helper()
	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)
	pl := &scummVMScrapePlatform{}
	s := pl.Scrapers(cfg)[id]
	ch := make(chan scraper.ScrapeUpdate)
	require.NoError(t, s.Scrape(context.Background(), cfg, pl, fs, &database.Database{MediaDB: db}, opts, nil, ch))
	var last scraper.ScrapeUpdate
	for update := range ch {
		assert.NoError(t, update.Err)
		assert.NoError(t, update.FatalErr)
		last = update
	}
	require.True(t, last.Done)
}

func TestScummVMGamelistTargetMatching(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name            string
		path            string
		marker          string
		folder          bool
		sharedDirectory bool
		wantMatch       bool
	}{
		{name: "directory", path: "./one", wantMatch: true},
		{name: "folder entry", path: "./one", folder: true, wantMatch: true},
		{name: "virtual target", path: "scummvm://one/Different%20name", wantMatch: true},
		{name: "marker contents", path: "./different.scummvm", marker: "one\r\n", wantMatch: true},
		{name: "unknown marker target", path: "./one.scummvm", marker: "unknown"},
		{name: "marker outside roots", path: "../outside.scummvm", marker: "one"},
		{name: "multiline marker", path: "./one.scummvm", marker: "one\ntwo"},
		{name: "oversized marker", path: "./one.scummvm", marker: strings.Repeat(" ", 1024) + "one"},
		{name: "unknown directory does not guess title", path: "./unknown"},
		{name: "unknown URI does not guess title", path: "scummvm://unknown/First"},
		{name: "shared directory is ambiguous", path: "./one", sharedDirectory: true},
		{
			name: "explicit target disambiguates directory", path: "scummvm://one/First",
			sharedDirectory: true, wantMatch: true,
		},
		{
			name: "marker disambiguates directory", path: "./different.scummvm", marker: "one",
			sharedDirectory: true, wantMatch: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, scoped := range []bool{false, true} {
				t.Run(fmt.Sprintf("scoped=%t", scoped), func(t *testing.T) {
					t.Parallel()
					fs := afero.NewMemMapFs()
					root := t.TempDir()
					oneDir, twoDir := filepath.Join(root, "one"), filepath.Join(root, "two")
					if tc.sharedDirectory {
						twoDir = oneDir
					}
					if tc.marker != "" {
						writeScummVMScrapeFile(t, fs, filepath.Join(root, tc.path), tc.marker)
					}
					kind := "game"
					if tc.folder {
						kind = "folder"
					}
					writeScummVMScrapeFile(t, fs, filepath.Join(root, "gamelist.xml"), fmt.Sprintf(
						`<gameList><%s><path>%s</path><name>First</name><image>./cover.png</image></%s>`+
							`<game><path>scummvm://two/Second</path><image>./second.png</image></game></gameList>`,
						kind, tc.path, kind))
					db, cleanup := helpers.NewInMemoryMediaDB(t)
					t.Cleanup(cleanup)
					one := virtualpath.CreateVirtualPath("scummvm", "one", "First")
					two := virtualpath.CreateVirtualPath("scummvm", "two", "Second")
					indexScummVMResults(t, db,
						ScummVMGame{TargetID: "one", Description: "First", Path: oneDir},
						ScummVMGame{TargetID: "two", Description: "Second", Path: twoDir},
					)
					rows, err := db.GetMediaBySystemID(systemdefs.SystemScummVM)
					require.NoError(t, err)
					opts := scraper.ScrapeOptions{Force: true}
					if scoped {
						for _, row := range rows {
							if row.Path == one {
								opts.Scope = &database.ScrapeScope{
									SystemID: systemdefs.SystemScummVM, Path: one, MediaID: row.DBID,
								}
							}
						}
						require.NotNil(t, opts.Scope)
					}
					runScummVMScraper(t, fs, db, "gamelist.xml", opts)
					for _, row := range rows {
						props, err := db.GetMediaPropertyMetadata(context.Background(), row.DBID)
						require.NoError(t, err)
						wantMatch := tc.wantMatch
						if row.Path == two {
							wantMatch = !scoped
						}
						if wantMatch {
							assert.NotEmpty(t, props, "path %s", row.Path)
						} else {
							assert.Empty(t, props, "path %s", row.Path)
						}
					}
				})
			}
		})
	}
}

func TestScummVMLocalArtworkDoesNotCrossTargets(t *testing.T) {
	t.Parallel()
	for _, shared := range []bool{false, true} {
		t.Run(fmt.Sprintf("shared=%t", shared), func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			root := t.TempDir()
			firstRoot, secondRoot := filepath.Join(root, "SD"), filepath.Join(root, "NAS")
			if shared {
				secondRoot = firstRoot
			}
			writeScummVMScrapeFile(t, fs, filepath.Join(firstRoot, "media", "boxart", "game.png"), "first target only")
			db, cleanup := helpers.NewInMemoryMediaDB(t)
			t.Cleanup(cleanup)
			one := virtualpath.CreateVirtualPath("scummvm", "one", "First")
			indexScummVMResults(t, db,
				ScummVMGame{TargetID: "one", Description: "First", Path: filepath.Join(firstRoot, "game")},
				ScummVMGame{TargetID: "two", Description: "Second", Path: filepath.Join(secondRoot, "game")},
			)
			rows, err := db.GetMediaBySystemID(systemdefs.SystemScummVM)
			require.NoError(t, err)
			for _, row := range rows {
				runScummVMScraper(t, fs, db, "media-folder", scraper.ScrapeOptions{
					Force: true,
					Scope: &database.ScrapeScope{SystemID: systemdefs.SystemScummVM, Path: row.Path, MediaID: row.DBID},
				})
				props, err := db.GetMediaPropertyMetadata(context.Background(), row.DBID)
				require.NoError(t, err)
				if !shared && row.Path == one {
					assert.NotEmpty(t, props)
				} else {
					assert.Empty(t, props)
				}
			}
		})
	}
}

// Variants of one game live in subfolders of its game folder, beside ordinary
// games that sit directly in the collection.
func TestScummVMVariantFolders(t *testing.T) {
	t.Parallel()
	const own, inherited, none = "own", "inherited", ""
	for _, tc := range []struct {
		name     string
		scraper  string
		gamelist string
		english  string
		french   string
		artwork  []string
	}{
		{
			name: "artwork named for the variant", scraper: "media-folder",
			artwork: []string{filepath.Join("kyra3", "dos-english.png")}, english: own,
		},
		{
			name: "artwork named for the game folder", scraper: "media-folder",
			artwork: []string{"kyra3.png"}, english: inherited, french: inherited,
		},
		{
			name: "variant artwork outranks the game folder", scraper: "media-folder",
			artwork: []string{"kyra3.png", filepath.Join("kyra3", "dos-english.png")},
			english: own, french: inherited,
		},
		{
			name: "entry for the variant", scraper: "gamelist.xml",
			gamelist: `<game><path>./kyra3/dos-english</path><image>./own.png</image></game>`, english: own,
		},
		{
			name: "entry for the game folder", scraper: "gamelist.xml",
			gamelist: `<game><path>./kyra3</path><image>./inherited.png</image></game>`,
			english:  inherited, french: inherited,
		},
		{
			name: "folder entry for the game folder", scraper: "gamelist.xml",
			gamelist: `<folder><path>./kyra3</path><image>./inherited.png</image></folder>`,
			english:  inherited, french: inherited,
		},
		{
			name: "variant entry outranks an earlier game folder entry", scraper: "gamelist.xml",
			gamelist: `<game><path>./kyra3</path><image>./inherited.png</image></game>` +
				`<game><path>./kyra3/dos-english</path><image>./own.png</image></game>`,
			english: own, french: inherited,
		},
		{
			name: "game folder entry falls back to artwork named for it", scraper: "gamelist.xml",
			gamelist: `<game><path>./kyra3</path><desc>Text only</desc></game>`,
			artwork:  []string{"kyra3.png"}, english: inherited, french: inherited,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			root := t.TempDir()
			boxart := filepath.Join(root, "media", "boxart")
			writeScummVMScrapeFile(t, fs, filepath.Join(root, "own.png"), "image")
			writeScummVMScrapeFile(t, fs, filepath.Join(root, "inherited.png"), "image")
			for _, name := range tc.artwork {
				writeScummVMScrapeFile(t, fs, filepath.Join(boxart, name), "image")
			}
			if tc.gamelist != "" {
				writeScummVMScrapeFile(t, fs, filepath.Join(root, "gamelist.xml"),
					"<gameList>"+tc.gamelist+"</gameList>")
			}
			db, cleanup := helpers.NewInMemoryMediaDB(t)
			t.Cleanup(cleanup)
			indexScummVMResults(t, db,
				ScummVMGame{TargetID: "monkey", Description: "Monkey", Path: filepath.Join(root, "monkey")},
				ScummVMGame{TargetID: "en", Description: "English", Path: filepath.Join(root, "kyra3", "dos-english")},
				ScummVMGame{TargetID: "fr", Description: "French", Path: filepath.Join(root, "kyra3", "dos-french")},
			)
			runScummVMScraper(t, fs, db, tc.scraper, scraper.ScrapeOptions{Force: true})

			images := map[string]string{
				own: filepath.Join(root, "own.png"), inherited: filepath.Join(root, "inherited.png"),
			}
			if len(tc.artwork) > 0 {
				images[inherited] = filepath.Join(boxart, "kyra3.png")
				images[own] = filepath.Join(boxart, "kyra3", "dos-english.png")
			}
			want := map[string]string{
				virtualpath.CreateVirtualPath("scummvm", "monkey", "Monkey"): none,
				virtualpath.CreateVirtualPath("scummvm", "en", "English"):    tc.english,
				virtualpath.CreateVirtualPath("scummvm", "fr", "French"):     tc.french,
			}
			rows, err := db.GetMediaBySystemID(systemdefs.SystemScummVM)
			require.NoError(t, err)
			require.Len(t, rows, len(want))
			for _, row := range rows {
				props, err := db.GetMediaPropertyMetadata(context.Background(), row.DBID)
				require.NoError(t, err)
				var got []string
				for _, prop := range props {
					if strings.HasPrefix(prop.TypeTag, "property:image-") {
						got = append(got, prop.Text)
					}
				}
				if want[row.Path] == none {
					assert.Empty(t, got, "path %s", row.Path)
					continue
				}
				assert.Equal(t, []string{filepath.ToSlash(images[want[row.Path]])}, got, "path %s", row.Path)
			}
		})
	}
}

// Metadata for a game folder reaches the variants inside it and stops there: it
// never crosses to targets that share a directory, never reaches a variant
// nested deeper, and the collection root is not a game folder.
func TestScummVMVariantFoldersStopAtTheirScope(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		scraper string
	}{
		{name: "gamelist entry", scraper: "gamelist.xml"},
		{name: "media folder artwork", scraper: "media-folder"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			// A named collection folder, so artwork named after it is a case
			// the fixture can state.
			root := filepath.Join(t.TempDir(), "GAMES")
			kyra := filepath.Join(root, "kyra3")
			writeScummVMScrapeFile(t, fs, filepath.Join(root, "inherited.png"), "image")
			// GAMES.png is named after the collection folder and must never be
			// claimed. kyra3.png would also answer the gamelist entry, so it is
			// only laid down for the scraper under test.
			artwork := []string{"GAMES.png"}
			if tc.scraper == "media-folder" {
				artwork = append(artwork, "kyra3.png")
			}
			for _, name := range artwork {
				writeScummVMScrapeFile(t, fs, filepath.Join(root, "media", "boxart", name), "image")
			}
			if tc.scraper == "gamelist.xml" {
				writeScummVMScrapeFile(t, fs, filepath.Join(root, "gamelist.xml"),
					"<gameList><game><path>./kyra3</path><image>./inherited.png</image></game></gameList>")
			}

			db, cleanup := helpers.NewInMemoryMediaDB(t)
			t.Cleanup(cleanup)
			shared := filepath.Join(kyra, "macintosh")
			indexScummVMResults(t, db,
				ScummVMGame{TargetID: "monkey", Description: "Monkey", Path: filepath.Join(root, "monkey")},
				ScummVMGame{TargetID: "en", Description: "English", Path: filepath.Join(kyra, "dos-english")},
				ScummVMGame{TargetID: "mac-en", Description: "Mac English", Path: shared},
				ScummVMGame{TargetID: "mac-de", Description: "Mac German", Path: shared},
				ScummVMGame{TargetID: "deep", Description: "Deep", Path: filepath.Join(kyra, "dos", "english")},
			)
			runScummVMScraper(t, fs, db, tc.scraper, scraper.ScrapeOptions{Force: true})

			inherited := filepath.Join(root, "inherited.png")
			if tc.scraper == "media-folder" {
				inherited = filepath.Join(root, "media", "boxart", "kyra3.png")
			}
			want := map[string]string{
				virtualpath.CreateVirtualPath("scummvm", "en", "English"):         inherited,
				virtualpath.CreateVirtualPath("scummvm", "monkey", "Monkey"):      "",
				virtualpath.CreateVirtualPath("scummvm", "mac-en", "Mac English"): "",
				virtualpath.CreateVirtualPath("scummvm", "mac-de", "Mac German"):  "",
				virtualpath.CreateVirtualPath("scummvm", "deep", "Deep"):          "",
			}
			rows, err := db.GetMediaBySystemID(systemdefs.SystemScummVM)
			require.NoError(t, err)
			require.Len(t, rows, len(want))
			for _, row := range rows {
				props, err := db.GetMediaPropertyMetadata(context.Background(), row.DBID)
				require.NoError(t, err)
				var got []string
				for _, prop := range props {
					if strings.HasPrefix(prop.TypeTag, "property:image-") {
						got = append(got, prop.Text)
					}
				}
				if want[row.Path] == "" {
					assert.Empty(t, got, "path %s", row.Path)
					continue
				}
				assert.Equal(t, []string{filepath.ToSlash(want[row.Path])}, got, "path %s", row.Path)
			}
		})
	}
}

// ScummVM adds one target per language or platform it detects on a disc, all
// configured on the same folder. Metadata for that folder belongs to every one
// of them, while a folder holding different games stays ambiguous.
func TestScummVMTargetsSharingAGameFolder(t *testing.T) {
	t.Parallel()
	const kyra3 = "The Legend of Kyrandia 3 Malcolm's Revenge (CD DOS, Multilanguage)"
	const tentacle = "Day Of The Tentacle (CD Dos)"
	kyraEntry := `<game><path>./` + kyra3 + `.scummvm</path><image>./folder.png</image></game>`
	macEntry := `<game><path>scummvm://kyra3-mac/Mac</path><image>./own.png</image></game>`
	kyraTargets := []string{"kyra3", "kyra3-1", "kyra3-2", "kyra3-mac", "kyra3-mac-1", "kyra3-mac-2"}
	for _, tc := range []struct {
		want     map[string]string
		name     string
		scraper  string
		gamelist string
		artwork  []string
		scoped   bool
	}{
		{
			name: "gamelist entry for the folder", scraper: "gamelist.xml", gamelist: kyraEntry,
			want: map[string]string{"*kyra3": "folder.png"},
		},
		{
			name: "target entry after the folder entry", scraper: "gamelist.xml", gamelist: kyraEntry + macEntry,
			want: map[string]string{"*kyra3": "folder.png", "kyra3-mac": "own.png"},
		},
		{
			name: "target entry before the folder entry", scraper: "gamelist.xml", gamelist: macEntry + kyraEntry,
			want: map[string]string{"*kyra3": "folder.png", "kyra3-mac": "own.png"},
		},
		{
			name: "scoped to one target", scraper: "gamelist.xml", gamelist: kyraEntry, scoped: true,
			want: map[string]string{"kyra3-1": "folder.png"},
		},
		{
			name: "artwork named for the folder without its extension", scraper: "media-folder",
			artwork: []string{kyra3 + ".png", tentacle + ".png"},
			want: map[string]string{
				"*kyra3":   filepath.Join("media", "boxart", kyra3+".png"),
				"tentacle": filepath.Join("media", "boxart", tentacle+".png"),
			},
		},
		{
			name: "folder holding different games", scraper: "gamelist.xml",
			gamelist: `<game><path>./compilation</path><image>./folder.png</image></game>` +
				`<game><path>scummvm://comp-b/B</path><image>./own.png</image></game>`,
			want: map[string]string{"comp-b": "own.png"},
		},
		{
			name: "artwork for a folder holding different games", scraper: "media-folder",
			artwork: []string{"compilation.png"}, want: map[string]string{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			root := filepath.Join(t.TempDir(), "ScummVM")
			ini := filepath.Join(root, "scummvm.ini")
			var b strings.Builder
			b.WriteString("[scummvm]\nlastselectedgame=kyra3-1\n")
			target := func(id, gameID, dir string) {
				fmt.Fprintf(&b, "\n[%s]\ndescription=%s\npath=%s\nengineid=kyra\ngameid=%s\n",
					id, id, filepath.Join(root, dir), gameID)
			}
			for _, id := range kyraTargets {
				target(id, "kyra3", kyra3+".scummvm")
			}
			target("tentacle", "tentacle", tentacle+".scummvm")
			target("comp-a", "a", "compilation")
			target("comp-b", "b", "compilation")
			writeScummVMScrapeFile(t, fs, ini, b.String())
			for _, name := range []string{"folder.png", "own.png"} {
				writeScummVMScrapeFile(t, fs, filepath.Join(root, name), "image")
			}
			for _, name := range tc.artwork {
				writeScummVMScrapeFile(t, fs, filepath.Join(root, "media", "boxart", name), "image")
			}
			if tc.gamelist != "" {
				writeScummVMScrapeFile(t, fs, filepath.Join(root, "gamelist.xml"),
					"<gameList>"+tc.gamelist+"</gameList>")
			}
			games, err := parseScummVMIniFS(context.Background(), fs, ini)
			require.NoError(t, err)
			require.Len(t, games, len(kyraTargets)+3)

			db, cleanup := helpers.NewInMemoryMediaDB(t)
			t.Cleanup(cleanup)
			indexScummVMResults(t, db, games...)
			rows, err := db.GetMediaBySystemID(systemdefs.SystemScummVM)
			require.NoError(t, err)
			require.Len(t, rows, len(games))
			opts := scraper.ScrapeOptions{Force: true}
			if tc.scoped {
				for _, row := range rows {
					if row.Path == virtualpath.CreateVirtualPath("scummvm", "kyra3-1", "kyra3-1") {
						opts.Scope = &database.ScrapeScope{
							SystemID: systemdefs.SystemScummVM, Path: row.Path, MediaID: row.DBID,
						}
					}
				}
				require.NotNil(t, opts.Scope)
			}
			runScummVMScraper(t, fs, db, tc.scraper, opts)

			want := make(map[string]string, len(games))
			for id, image := range tc.want {
				if id == "*kyra3" {
					for _, kyra := range kyraTargets {
						if _, own := want[kyra]; !own {
							want[kyra] = image
						}
					}
					continue
				}
				want[id] = image
			}
			for _, row := range rows {
				props, err := db.GetMediaPropertyMetadata(context.Background(), row.DBID)
				require.NoError(t, err)
				var got []string
				for _, prop := range props {
					if strings.HasPrefix(prop.TypeTag, "property:image-") {
						got = append(got, prop.Text)
					}
				}
				id, err := virtualpath.ExtractSchemeID(row.Path, "scummvm")
				require.NoError(t, err)
				if want[id] == "" {
					assert.Empty(t, got, "target %s", id)
					continue
				}
				assert.Equal(t, []string{filepath.ToSlash(filepath.Join(root, want[id]))}, got, "target %s", id)
			}
		})
	}
}

func TestScummVMLocalArtworkForceCleanup(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	root := t.TempDir()
	dir := filepath.Join(root, "game.v1")
	cover := filepath.Join(root, "media", "boxart", "game.v1.png")
	writeScummVMScrapeFile(t, fs, cover, "image")
	db, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	indexScummVMResults(t, db, ScummVMGame{TargetID: "one", Description: "First", Path: dir})
	runScummVMScraper(t, fs, db, "media-folder", scraper.ScrapeOptions{Force: true})
	rows, err := db.GetMediaBySystemID(systemdefs.SystemScummVM)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	props, err := db.GetMediaPropertyMetadata(context.Background(), rows[0].DBID)
	require.NoError(t, err)
	require.NotEmpty(t, props)
	require.NoError(t, fs.Remove(cover))
	runScummVMScraper(t, fs, db, "media-folder", scraper.ScrapeOptions{Force: true})
	props, err = db.GetMediaPropertyMetadata(context.Background(), rows[0].DBID)
	require.NoError(t, err)
	assert.Empty(t, props, "force cleanup must use the same directory identity as discovery")
}

func TestScummVMScrapersImportConfiguredGame(t *testing.T) {
	t.Parallel()
	for _, scraperID := range []string{"gamelist.xml", "media-folder"} {
		t.Run(scraperID, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			root := filepath.Join(t.TempDir(), "ScummVM", "GAMES")
			gameDir := filepath.Join(root, "monkey.v1")
			cover := filepath.Join(root, "media", "boxart", "monkey.v1.png")
			writeScummVMScrapeFile(t, fs, cover, "image")
			writeScummVMScrapeFile(t, fs, filepath.Join(root, "gamelist.xml"),
				`<gameList><game><path>./monkey.v1</path><name>Unrelated scraper title</name>`+
					`<desc>Adventure metadata</desc></game></gameList>`)
			db, cleanup := helpers.NewInMemoryMediaDB(t)
			t.Cleanup(cleanup)
			virtual := virtualpath.CreateVirtualPath("scummvm", "monkey-target", "Old display title")
			indexScummVMResults(t, db, ScummVMGame{
				TargetID: "monkey-target", Description: "Old display title", Path: gameDir,
			})

			runScummVMScraper(t, fs, db, scraperID, scraper.ScrapeOptions{Force: true})

			rows, err := db.GetMediaBySystemID(systemdefs.SystemScummVM)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			assert.Equal(t, virtual, rows[0].Path, "scraping must not rewrite launch paths or index raw game files")
			props, err := db.GetMediaPropertyMetadata(context.Background(), rows[0].DBID)
			require.NoError(t, err)
			require.NotEmpty(t, props, "configured ScummVM source must not be skipped")
			found := false
			for _, prop := range props {
				if prop.TypeTag == tags.PropertyTypeTag(tags.TagPropertyImageBoxart) {
					assert.Equal(t, filepath.ToSlash(cover), prop.Text)
					found = true
				}
			}
			assert.True(t, found, "directory artwork must preserve dots in folder names")
			if scraperID == "gamelist.xml" {
				titleProps, err := db.GetMediaTitlePropertyMetadata(context.Background(), rows[0].MediaTitleDBID)
				require.NoError(t, err)
				foundDescription := false
				for _, prop := range titleProps {
					if prop.TypeTag == tags.PropertyTypeTag(tags.TagPropertyDescription) {
						assert.Equal(t, "Adventure metadata", prop.Text)
						foundDescription = true
					}
				}
				assert.True(t, foundDescription)
			}
		})
	}
}
