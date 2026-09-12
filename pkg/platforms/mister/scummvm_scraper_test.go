//go:build linux

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
					writeScummVMScrapeFile(t, fs, scummvmIniPath,
						fmt.Sprintf("[one]\npath=%s\n[two]\npath=%s\n", oneDir, twoDir))
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
					scantest.IndexMediaPaths(t, db, systemdefs.SystemScummVM, one, two)
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
			writeScummVMScrapeFile(t, fs, scummvmIniPath, fmt.Sprintf(
				"[one]\npath=%s\n[two]\npath=%s\n",
				filepath.Join(firstRoot, "game"), filepath.Join(secondRoot, "game")))
			writeScummVMScrapeFile(t, fs, filepath.Join(firstRoot, "media", "boxart", "game.png"), "first target only")
			db, cleanup := helpers.NewInMemoryMediaDB(t)
			t.Cleanup(cleanup)
			one := virtualpath.CreateVirtualPath("scummvm", "one", "First")
			two := virtualpath.CreateVirtualPath("scummvm", "two", "Second")
			scantest.IndexMediaPaths(t, db, systemdefs.SystemScummVM, one, two)
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

func TestScummVMLocalArtworkForceCleanup(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	root := t.TempDir()
	dir := filepath.Join(root, "game.v1")
	cover := filepath.Join(root, "media", "boxart", "game.v1.png")
	writeScummVMScrapeFile(t, fs, scummvmIniPath, fmt.Sprintf("[one]\npath=%s\n", dir))
	writeScummVMScrapeFile(t, fs, cover, "image")
	db, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	virtual := virtualpath.CreateVirtualPath("scummvm", "one", "First")
	scantest.IndexMediaPaths(t, db, systemdefs.SystemScummVM, virtual)
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
			writeScummVMScrapeFile(t, fs, scummvmIniPath,
				fmt.Sprintf("[monkey-target]\ndescription=New display title\npath=%s\n", gameDir))
			writeScummVMScrapeFile(t, fs, cover, "image")
			writeScummVMScrapeFile(t, fs, filepath.Join(root, "gamelist.xml"),
				`<gameList><game><path>./monkey.v1</path><name>Unrelated scraper title</name>`+
					`<desc>Adventure metadata</desc></game></gameList>`)
			db, cleanup := helpers.NewInMemoryMediaDB(t)
			t.Cleanup(cleanup)
			virtual := virtualpath.CreateVirtualPath("scummvm", "monkey-target", "Old display title")
			scantest.IndexMediaPaths(t, db, systemdefs.SystemScummVM, virtual)

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
