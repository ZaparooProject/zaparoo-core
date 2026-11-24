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

package simfixtures

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"image/png"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/tags"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esmedia"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tree lists every file under root as "rel sha256", sorted.
func tree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p) //nolint:gosec // Test reads its own temp tree.
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return fmt.Errorf("rel: %w", err)
		}
		out = append(out, fmt.Sprintf("%s %x", filepath.ToSlash(rel), sha256.Sum256(data)))
		return nil
	})
	require.NoError(t, err)
	slices.Sort(out)
	return out
}

// genIn generates opts into a fresh temp root.
func genIn(t *testing.T, in *Options) *Report {
	t.Helper()
	opts := *in
	opts.Root = t.TempDir()
	report, err := Generate(context.Background(), opts)
	require.NoError(t, err)
	return report
}

func systemReport(t *testing.T, r *Report, folder string) *SystemReport {
	t.Helper()
	for i := range r.Systems {
		if r.Systems[i].Folder == folder {
			return &r.Systems[i]
		}
	}
	require.Failf(t, "missing system", "folder %s not in report", folder)
	return nil
}

func relMedia(sys *SystemReport, root string) []string {
	out := make([]string, 0, len(sys.MediaPaths))
	for _, p := range sys.MediaPaths {
		rel, _ := filepath.Rel(filepath.Join(root, sys.Folder), p)
		out = append(out, filepath.ToSlash(rel))
	}
	return out
}

func allOptions(systems ...string) *Options {
	return &Options{
		Systems: systems, PerSystem: 25, Seed: 7, Realistic: true, Zips: true, Gamelist: true,
		MediaImages: true, Subfolders: true, EdgeCases: true,
	}
}

func TestGenerateDeterministic(t *testing.T) {
	t.Parallel()

	opts := allOptions("snes", "PSX", "arcade", "c64")
	a := genIn(t, opts)
	b := genIn(t, opts)
	assert.Equal(t, tree(t, a.Root), tree(t, b.Root))
	for i := range a.Systems {
		assert.Equal(t, relMedia(&a.Systems[i], a.Root), relMedia(&b.Systems[i], b.Root))
	}
	bRoot := b.Root
	a.Root, b.Root = "", ""
	for i := range a.Systems {
		a.Systems[i].MediaPaths, b.Systems[i].MediaPaths = nil, nil
	}
	assert.Equal(t, a, b)

	opts.Seed = 8
	c := genIn(t, opts)
	assert.NotEqual(t, tree(t, bRoot), tree(t, c.Root))
}

func TestGenerateSystemUnaffectedByOthers(t *testing.T) {
	t.Parallel()

	alone := genIn(t, &Options{Systems: []string{"snes"}, Seed: 3, Realistic: true})
	together := genIn(t, &Options{Systems: []string{"nes", "snes", "gba"}, Seed: 3, Realistic: true})
	assert.Equal(t, relMedia(systemReport(t, alone, "snes"), alone.Root),
		relMedia(systemReport(t, together, "snes"), together.Root))
}

func TestResolveTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		wantErr string
		in      []string
		want    []string
	}{
		{name: "folder", in: []string{"snes"}, want: []string{"snes"}},
		{name: "system ID any case", in: []string{"SNES", "snes"}, want: []string{"snes"}},
		{name: "ID with lower-case folder", in: []string{"gba"}, want: []string{"gba"}},
		{name: "non-canonical folder kept", in: []string{"c128", "c64"}, want: []string{"c128", "c64"}},
		{name: "ID to canonical folder", in: []string{"Genesis", "Nintendo64"}, want: []string{"megadrive", "n64"}},
		{name: "sorted by system ID", in: []string{"snes", "nes"}, want: []string{"nes", "snes"}},
		{name: "blank entries ignored", in: []string{" ", "nes"}, want: []string{"nes"}},
		{name: "unknown", in: []string{"atari9000"}, wantErr: "atari9000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveTargets(tt.in, false)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			folders := make([]string, 0, len(got))
			for _, target := range got {
				folders = append(folders, target.folder)
			}
			assert.Equal(t, tt.want, folders)
		})
	}
}

func TestResolveTargetsAll(t *testing.T) {
	t.Parallel()

	ids := make(map[string]bool)
	for _, info := range esde.SystemMap {
		ids[info.SystemID] = true
	}
	got, err := resolveTargets(nil, true)
	require.NoError(t, err)
	assert.Len(t, got, len(ids))
	for _, target := range got {
		assert.Equal(t, CanonicalFolder(target.info.SystemID), target.folder)
	}
}

func TestCanonicalFolder(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "snes", CanonicalFolder(systemdefs.SystemSNES))
	assert.Equal(t, "arcade", CanonicalFolder(systemdefs.SystemArcade))
	assert.Equal(t, "psx", CanonicalFolder("psx"))
	assert.Empty(t, CanonicalFolder("NotASystem"))
	// Every canonical folder maps back to its ID.
	for _, info := range esde.SystemMap {
		folder := CanonicalFolder(info.SystemID)
		assert.Equal(t, info.SystemID, esde.SystemMap[folder].SystemID)
	}
}

func TestRomExtension(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"snes":         ".smc",
		"nes":          ".nes",
		"megadrive":    ".gen",
		"mastersystem": ".sms",
		"psx":          ".chd",
		"arcade":       ".zip",
		"neogeo":       ".zip",
		"gba":          ".gba",
	}
	for folder, want := range tests {
		t.Run(folder, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, romExtension(esde.SystemMap[folder].Extensions))
		})
	}
	// Every system gets an extension it accepts.
	for folder, info := range esde.SystemMap {
		ext := romExtension(info.Extensions)
		assert.True(t, hasExt(info.Extensions, ext), "%s: %s not in %v", folder, ext, info.Extensions)
	}
}

func TestGenerateValidation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tests := []struct {
		name    string
		wantErr string
		opts    Options
	}{
		{name: "no root", opts: Options{Systems: []string{"snes"}}, wantErr: "Root"},
		{name: "no systems", opts: Options{Root: t.TempDir()}, wantErr: "no systems"},
		{name: "bad source", opts: Options{Root: t.TempDir(), All: true, Source: "nope"}, wantErr: "unknown source"},
		{name: "dats without dir", opts: Options{Root: t.TempDir(), All: true, Source: SourceDATs}, wantErr: "DatsDir"},
		{
			name:    "dats dir missing",
			opts:    Options{Root: t.TempDir(), All: true, Source: SourceDATs, DatsDir: "/does/not/exist"},
			wantErr: "read DatsDir",
		},
		{
			name:    "dats dir empty",
			opts:    Options{Root: t.TempDir(), All: true, Source: SourceDATs, DatsDir: t.TempDir()},
			wantErr: "no .dat",
		},
		{name: "negative count", opts: Options{Root: t.TempDir(), All: true, PerSystem: -1}, wantErr: "negative"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Generate(ctx, tt.opts)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestGeneratePlain(t *testing.T) {
	t.Parallel()

	r := genIn(t, &Options{Systems: []string{"snes", "psx", "arcade"}, PerSystem: 12, Seed: 1})
	assert.Equal(t, SourceGameDatabase, r.Source)
	assert.Equal(t, 36, r.Media)
	assert.Equal(t, 36, r.Files)
	for _, sys := range r.Systems {
		assert.Equal(t, 12, sys.Titles, sys.Folder)
		assert.Equal(t, 12, sys.Media, sys.Folder)
		assert.Len(t, sys.Samples, maxSamples)
		assert.False(t, sys.Gamelist)
		for _, rel := range relMedia(&sys, r.Root) {
			assert.Equal(t, sys.Extension, path.Ext(rel), rel)
			assert.NotContains(t, rel, "/", "no subfolders without Subfolders")
			assert.NotContains(t, rel, "(", "no tags without Realistic")
		}
		_, err := os.Stat(filepath.Join(r.Root, sys.Folder, "media"))
		require.ErrorIs(t, err, fs.ErrNotExist)
	}
	assert.Equal(t, SourceGameDatabase, systemReport(t, r, "snes").Source)
	assert.Equal(t, SourceSynthetic, systemReport(t, r, "psx").Source, "PSX has no GameDataBase bundle")
}

// Titles from the GameDataBase must slug like their dataset row, which is the
// key the gamedatabase scraper joins on, even with dump tags added.
func TestGameDatabaseTitlesMatchScraperSlug(t *testing.T) {
	t.Parallel()

	opts := Options{
		Root: t.TempDir(), Systems: []string{"snes", "megadrive", "gb"}, PerSystem: 200, Seed: 5,
		Realistic: true, Source: SourceGameDatabase,
	}
	rng := systemRand(opts.Seed, "snes")
	titles, source, err := pickTitles(&opts, systemdefs.SystemSNES, nil, rng)
	require.NoError(t, err)
	require.Equal(t, SourceGameDatabase, source)
	require.Len(t, titles, 200)

	r := genIn(t, &opts)
	for _, sys := range r.Systems {
		require.Equal(t, SourceGameDatabase, sys.Source, sys.Folder)
	}
	// Recompute each system's plan and check every title's files.
	for _, sys := range r.Systems {
		info := esde.SystemMap[sys.Folder]
		plan := planFor(t, &opts, target{folder: sys.Folder, info: info})
		for _, f := range plan.files {
			if f.game < 0 || strings.HasSuffix(f.rel, ".m3u") {
				continue
			}
			stem := strings.TrimSuffix(path.Base(f.rel), path.Ext(f.rel))
			assert.Equal(t, titleSlug(plan.titles[f.game].name), titleSlug(stem), f.rel)
		}
	}
}

// planFor rebuilds the plan Generate made for one system.
func planFor(t *testing.T, in *Options, tg target) *systemPlan {
	t.Helper()
	opts := *in
	if opts.Source == "" {
		opts.Source = SourceGameDatabase
	}
	if opts.PerSystem == 0 {
		opts.PerSystem = DefaultPerSystem
	}
	plan, _, err := planSystem(&opts, tg, nil)
	require.NoError(t, err)
	return plan
}

func TestGenerateArcadeSets(t *testing.T) {
	t.Parallel()

	r := genIn(t, &Options{Systems: []string{"arcade"}, PerSystem: 30, Seed: 2, Realistic: true})
	sys := systemReport(t, r, "arcade")
	assert.Greater(t, sys.Media, 30, "realistic arcade adds clone sets")
	for _, p := range sys.MediaPaths {
		base := filepath.Base(p)
		assert.Equal(t, ".zip", filepath.Ext(base))
		assert.True(t, validSetName(strings.TrimSuffix(base, ".zip")), base)
		zr, err := zip.OpenReader(p)
		require.NoError(t, err, base)
		assert.Len(t, zr.File, 1)
		require.NoError(t, zr.Close())
	}
}

func TestGenerateRealistic(t *testing.T) {
	t.Parallel()

	r := genIn(t, &Options{Systems: []string{"snes", "psx"}, PerSystem: 60, Seed: 4, Realistic: true})

	snes := relMedia(systemReport(t, r, "snes"), r.Root)
	assert.Greater(t, len(snes), 60, "extra releases")
	for _, rel := range snes {
		assert.Contains(t, rel, "(", "every realistic name has a region: %s", rel)
	}
	assert.True(t, slices.ContainsFunc(snes, func(s string) bool { return strings.Contains(s, "(Japan)") }))

	psx := relMedia(systemReport(t, r, "psx"), r.Root)
	var playlists []string
	for _, rel := range psx {
		if path.Ext(rel) == ".m3u" {
			playlists = append(playlists, rel)
		}
	}
	require.NotEmpty(t, playlists, "multi-disc games on a disc system")
	for _, rel := range playlists {
		data, err := os.ReadFile(filepath.Join(r.Root, "psx", rel)) //nolint:gosec // Test reads its own temp tree.
		require.NoError(t, err)
		lines := strings.Fields(strings.ReplaceAll(string(data), " ", "_"))
		assert.GreaterOrEqual(t, len(lines), 2)
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			assert.Contains(t, line, "(Disc ")
			assert.FileExists(t, filepath.Join(r.Root, "psx", path.Dir(rel), line))
		}
	}
}

func TestGenerateZips(t *testing.T) {
	t.Parallel()

	r := genIn(t, &Options{Systems: []string{"snes", "psx"}, PerSystem: 40, Seed: 9, Zips: true})
	zips := 0
	for _, p := range systemReport(t, r, "snes").MediaPaths {
		if filepath.Ext(p) != ".zip" {
			continue
		}
		zips++
		zr, err := zip.OpenReader(p)
		require.NoError(t, err)
		require.Len(t, zr.File, 1)
		assert.Equal(t, ".smc", path.Ext(zr.File[0].Name))
		require.NoError(t, zr.Close())
	}
	assert.Positive(t, zips)
	for _, p := range systemReport(t, r, "psx").MediaPaths {
		assert.NotEqual(t, ".zip", filepath.Ext(p), "psx does not take zips")
	}
}

func TestGenerateSubfolders(t *testing.T) {
	t.Parallel()

	r := genIn(t, &Options{Systems: []string{"snes"}, PerSystem: 40, Seed: 1, Subfolders: true})
	rels := relMedia(systemReport(t, r, "snes"), r.Root)
	assert.True(t, slices.ContainsFunc(rels, func(s string) bool { return strings.Count(s, "/") == 1 }))
	assert.True(t, slices.ContainsFunc(rels, func(s string) bool { return strings.Count(s, "/") == 2 }))
	assert.True(t, slices.ContainsFunc(rels, func(s string) bool { return !strings.Contains(s, "/") }))
}

func TestGenerateGamelist(t *testing.T) {
	t.Parallel()

	r := genIn(t, allOptions("snes", "psx", "arcade"))
	for _, sys := range r.Systems {
		require.True(t, sys.Gamelist)
		dir := filepath.Join(r.Root, sys.Folder)
		// The reader the gamelist.xml scraper uses.
		gl, err := esapi.ReadGameListXMLLimitFSContext(context.Background(), afero.NewOsFs(),
			filepath.Join(dir, "gamelist.xml"), esapi.MaxGameListXMLSize)
		require.NoError(t, err, sys.Folder)
		assert.Zero(t, gl.Skipped)
		assert.Len(t, gl.Games, sys.Media-edgeIndexed(sys.Extension))
		withArt := 0
		for i := range gl.Games {
			g := &gl.Games[i]
			assert.NotEmpty(t, g.Name)
			assert.NotEmpty(t, g.Developer)
			_, err := esapi.ParseESDate(g.ReleaseDate)
			require.NoError(t, err, g.ReleaseDate)
			assert.FileExists(t, esmedia.ResolvePath(g.Path, dir))
			if g.Thumbnail != "" {
				withArt++
				assert.FileExists(t, esmedia.ResolvePath(g.Thumbnail, dir))
			}
		}
		assert.Equal(t, sys.Titles, withArt, "every title's primary entry has a cover")
	}
}

// edgeIndexed is how many indexed edge-case files a system gets.
func edgeIndexed(ext string) int {
	if isArchiveExt(ext) {
		return 7
	}
	return 8
}

func TestGenerateMediaImages(t *testing.T) {
	t.Parallel()

	opts := Options{
		Systems: []string{"snes", "psx"}, PerSystem: 30, Seed: 11, MediaImages: true,
		Subfolders: true, Realistic: true,
	}
	r := genIn(t, &opts)
	osFS := afero.NewOsFs()
	for _, sys := range r.Systems {
		dir := filepath.Join(r.Root, sys.Folder)
		plan := planFor(t, &opts, target{folder: sys.Folder, info: esde.SystemMap[sys.Folder]})
		dirs := esmedia.StatMediaDirsFS(osFS, dir)
		require.NotEmpty(t, dirs)
		found := 0
		for _, f := range plan.files {
			if !f.primary {
				continue
			}
			// The lookup the media-folder scraper makes for a box art image.
			names := esmedia.ArtworkFallbackNames(filepath.Join(dir, filepath.FromSlash(f.rel)), dir)
			file := esmedia.FindFileFS(osFS, names,
				esmedia.ArtworkDirCandidates[string(tags.TagPropertyImageBoxart)], dirs)
			require.NotNil(t, file, f.rel)
			assert.Equal(t, "image/png", file.ContentType)
			data, err := os.ReadFile(file.Path)
			require.NoError(t, err)
			_, err = png.Decode(bytes.NewReader(data))
			require.NoError(t, err)
			found++
		}
		assert.Equal(t, sys.Titles, found)
		assert.GreaterOrEqual(t, sys.Images, sys.Titles)
	}
}

func TestGenerateEdgeCases(t *testing.T) {
	t.Parallel()

	r := genIn(t, &Options{Systems: []string{"snes", "arcade"}, PerSystem: 5, Seed: 1, EdgeCases: true})
	snes := systemReport(t, r, "snes")
	assert.Equal(t, 5+edgeIndexed(".smc"), snes.Media)
	assert.Equal(t, 6, snes.Skipped)
	rels := relMedia(snes, r.Root)
	assert.Contains(t, rels, edgeCJK+".smc")
	assert.Contains(t, rels, edgeUpperExt+".SMC")
	assert.True(t, slices.ContainsFunc(rels, func(s string) bool { return len(s) > 190 }))
	dir := filepath.Join(r.Root, "snes")
	assert.FileExists(t, filepath.Join(dir, "Ignored", ".zaparooignore"))
	assert.FileExists(t, filepath.Join(dir, "._Apple Double (USA).smc"))
	assert.FileExists(t, filepath.Join(dir, ".hidden", "Hidden Game (USA).smc"))

	arcade := systemReport(t, r, "arcade")
	assert.Equal(t, 5+edgeIndexed(".zip"), arcade.Media)
}

func TestGenerateSyntheticSource(t *testing.T) {
	t.Parallel()

	r := genIn(t, &Options{Systems: []string{"snes"}, PerSystem: 300, Seed: 1, Source: SourceSynthetic})
	sys := systemReport(t, r, "snes")
	assert.Equal(t, SourceSynthetic, sys.Source)
	assert.Equal(t, 300, sys.Media)
	slugsSeen := make(map[string]bool)
	for _, rel := range relMedia(sys, r.Root) {
		slug := titleSlug(strings.TrimSuffix(rel, ".smc"))
		assert.False(t, slugsSeen[slug], "duplicate slug %s", slug)
		slugsSeen[slug] = true
	}
}

func TestGenerateFillsBeyondBundle(t *testing.T) {
	t.Parallel()

	// The virtualboy bundle has far fewer than 500 games.
	r := genIn(t, &Options{Systems: []string{"virtualboy"}, PerSystem: 500, Seed: 1})
	sys := systemReport(t, r, "virtualboy")
	assert.Equal(t, 500, sys.Media)
	assert.Equal(t, SourceGameDatabase, sys.Source)
}

const testDAT = `<?xml version="1.0"?>
<datafile>
	<header><name>Nintendo - Super Nintendo Entertainment System</name></header>
	<game name="Alpha Quest (USA)"><description>Alpha Quest (USA)</description></game>
	<game name="Alpha Quest (Japan)"><description>Alpha Quest (Japan)</description></game>
	<game name="Beta Racer (Europe) (Rev 1)"><description>Beta Racer</description></game>
	<game name="Gamma: Saga (USA) [!]"><description>Gamma</description></game>
</datafile>
`

func TestGenerateDATs(t *testing.T) {
	t.Parallel()

	dats := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dats, "snes.dat"), []byte(testDAT), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dats, "notes.txt"), []byte("ignored"), 0o600))

	tests := []struct {
		name      string
		want      []string
		realistic bool
	}{
		{
			name:      "realistic keeps dump names",
			realistic: true,
			want: []string{
				"Alpha Quest (Japan).smc", "Alpha Quest (USA).smc", "Beta Racer (Europe) (Rev 1).smc",
				"Gamma - Saga (USA) [!].smc",
			},
		},
		{
			name: "plain strips tags",
			want: []string{"Alpha Quest.smc", "Beta Racer.smc", "Gamma - Saga.smc"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := genIn(t, &Options{
				Systems: []string{"snes", "nes"}, PerSystem: 10, Seed: 1, Source: SourceDATs,
				DatsDir: dats, Realistic: tt.realistic,
			})
			snes := systemReport(t, r, "snes")
			assert.Equal(t, SourceDATs, snes.Source)
			got := relMedia(snes, r.Root)
			for _, want := range tt.want {
				assert.Contains(t, got, want)
			}
			nes := systemReport(t, r, "nes")
			assert.Equal(t, SourceSynthetic, nes.Source, "no NES DAT")
			assert.Equal(t, 10, nes.Titles)
		})
	}
}

func TestGenerateCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Generate(ctx, Options{Root: t.TempDir(), Systems: []string{"snes"}})
	require.ErrorIs(t, err, context.Canceled)
}

func TestSanitizeStem(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"3x3 Eyes: Jūma Hōkan":    "3x3 Eyes - Jūma Hōkan",
		"Raguy / Blue's Journey":  "Raguy - Blue's Journey",
		`What? "Quoted" <x> a|b*`: "What Quoted x ab",
		"Trailing dots...":        "Trailing dots",
		"Ratio 1:2":               "Ratio 1-2",
		"  spaced   out  ":        "spaced out",
	}
	for in, want := range tests {
		assert.Equal(t, want, sanitizeStem(in), in)
	}
}

func TestBenchFilenames(t *testing.T) {
	t.Parallel()

	a := BenchFilenames(500)
	assert.Equal(t, a, BenchFilenames(500))
	assert.Len(t, a, 500)
	// The committed benchmark baseline was measured on these inputs.
	assert.Equal(t, []string{
		"/roms/system/Dark Racer Rising 94.nes",
		"/roms/system/Dark Fighter Revolution 81.nes",
		"/roms/system/Grand Battle Adventure 3.nes",
	}, a[:3])
	for _, p := range a {
		require.True(t, strings.HasPrefix(p, "/roms/system/") && strings.HasSuffix(p, ".nes"), p)
		assert.NotContains(t, p, "(")
	}
}

func esdeExtensions(folder string) []string {
	return esde.SystemMap[folder].Extensions
}
