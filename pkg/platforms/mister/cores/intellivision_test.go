//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package cores

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mgl"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var intvCFGData = []byte("[vars]\necs = 1\n")

func intvGameDir() string {
	return filepath.Join(misterconfig.SDRootDir, "games", "Intellivision")
}

func intvWrite(t *testing.T, fs afero.Fs, path string, data []byte) {
	t.Helper()
	require.NoError(t, fs.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, afero.WriteFile(fs, path, data, 0o600))
}

func intvGenerate(t *testing.T, fs afero.Fs, mediaPath string) string {
	t.Helper()
	core, err := GetCore("Intellivision")
	require.NoError(t, err)
	override, err := hookIntellivisionWithFS(fs, core, mediaPath)
	require.NoError(t, err)
	require.NotEmpty(t, override)
	document, err := mgl.Generate(core, core.RBF, mediaPath, override)
	require.NoError(t, err)
	return document
}

func intvZip(t *testing.T, entries ...string) []byte {
	t.Helper()
	data := make(map[string][]byte, len(entries))
	for _, name := range entries {
		data[name] = intvCFGData
	}
	return intvZipData(t, entries, data)
}

func intvZipData(t *testing.T, names []string, data map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, name := range names {
		f, err := w.Create(name)
		require.NoError(t, err)
		_, err = f.Write(data[name])
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}

func TestHookIntellivisionLoadsSiblingCFG(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		rom    string
		cfg    string
		others []string
	}{
		{name: "bin with cfg", rom: "Game.bin", cfg: "Game.cfg"},
		{name: "int with cfg", rom: "Game.int", cfg: "Game.cfg"},
		{name: "uppercase names", rom: "GAME.BIN", cfg: "GAME.CFG"},
		{name: "ignores other cfgs", rom: "Game.bin", cfg: "Game.cfg", others: []string{"Game 2.cfg", "Other.cfg"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			mediaPath := filepath.Join(intvGameDir(), tc.rom)
			cfgPath := filepath.Join(intvGameDir(), tc.cfg)
			intvWrite(t, fs, mediaPath, nil)
			intvWrite(t, fs, cfgPath, intvCFGData)
			for _, other := range tc.others {
				intvWrite(t, fs, filepath.Join(intvGameDir(), other), intvCFGData)
			}

			document := intvGenerate(t, fs, mediaPath)
			option := `<option name="Format" value="CFG mapping"/>`
			rom := `<file delay="1" type="f" index="1" path="../../../../..` + mediaPath + `"/>`
			cfg := `<file delay="1" type="f" index="2" path="../../../../..` + cfgPath + `"/>`
			assert.Contains(t, document, option)
			assert.Contains(t, document, rom)
			assert.Contains(t, document, cfg)
			assert.Equal(t, 2, strings.Count(document, "<file "))
			assert.Less(t, strings.Index(document, option), strings.Index(document, rom),
				"the option must be set before the files load")
			assert.Less(t, strings.Index(document, rom), strings.Index(document, cfg))
		})
	}
}

func TestHookIntellivisionRawWithoutCFG(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		rom   string
		extra string
	}{
		{name: "bin without cfg", rom: "Game.bin"},
		{name: "cfg for another game", rom: "Game.bin", extra: "Other.cfg"},
		{name: "rom ignores cfg", rom: "Game.rom", extra: "Game.cfg"},
		{name: "cfg is a directory", rom: "Game.int", extra: filepath.Join("Game.cfg", "x")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			mediaPath := filepath.Join(intvGameDir(), tc.rom)
			intvWrite(t, fs, mediaPath, nil)
			if tc.extra != "" {
				intvWrite(t, fs, filepath.Join(intvGameDir(), tc.extra), intvCFGData)
			}

			document := intvGenerate(t, fs, mediaPath)
			assert.Contains(t, document, `<option name="Format" value="Raw &amp; Intellicart"/>`)
			assert.Contains(t, document,
				`<file delay="1" type="f" index="1" path="../../../../..`+mediaPath+`"/>`)
			assert.Equal(t, 1, strings.Count(document, "<file "))
			assert.NotContains(t, document, "ECS")
		})
	}
}

func TestHookIntellivisionEscapesPaths(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	mediaPath := filepath.Join(intvGameDir(), `Tom & Jerry "Demo".bin`)
	intvWrite(t, fs, mediaPath, nil)
	intvWrite(t, fs, filepath.Join(intvGameDir(), `Tom & Jerry "Demo".cfg`), intvCFGData)

	document := intvGenerate(t, fs, mediaPath)
	assert.Contains(t, document, "Tom &amp; Jerry &#34;Demo&#34;.bin")
	assert.Contains(t, document, "Tom &amp; Jerry &#34;Demo&#34;.cfg")
	assert.NotContains(t, document, "Tom & Jerry")
}

func TestHookIntellivisionCFGInsideZip(t *testing.T) {
	t.Parallel()
	zipPath := filepath.Join(intvGameDir(), "Games.zip")
	for _, tc := range []struct {
		name    string
		inner   string
		wantCFG string
		entries []string
	}{
		{
			name: "same folder", inner: "Homebrew/Game.bin",
			entries: []string{"Homebrew/Game.bin", "Homebrew/Game.cfg"}, wantCFG: "Homebrew/Game.cfg",
		},
		{
			name: "uppercase extension", inner: "Game.int",
			entries: []string{"Game.int", "Game.CFG"}, wantCFG: "Game.CFG",
		},
		{
			name: "folder named like a zip inside the zip", inner: "Extras.zip/Game.bin",
			entries: []string{"Extras.zip/Game.bin", "Extras.zip/Game.cfg"}, wantCFG: "Extras.zip/Game.cfg",
		},
		{
			name: "cfg in another folder", inner: "Homebrew/Game.bin",
			entries: []string{"Homebrew/Game.bin", "Other/Game.cfg"},
		},
		{
			name: "no cfg", inner: "Game.bin",
			entries: []string{"Game.bin", "Other.cfg"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			intvWrite(t, fs, zipPath, intvZip(t, tc.entries...))
			mediaPath := zipPath + "/" + tc.inner

			document := intvGenerate(t, fs, mediaPath)
			assert.Contains(t, document,
				`<file delay="1" type="f" index="1" path="../../../../..`+mediaPath+`"/>`)
			if tc.wantCFG == "" {
				assert.Contains(t, document, `value="Raw &amp; Intellicart"`)
				assert.Equal(t, 1, strings.Count(document, "<file "))
				return
			}
			assert.Contains(t, document, `value="CFG mapping"`)
			assert.Contains(t, document,
				`<file delay="1" type="f" index="2" path="../../../../..`+zipPath+"/"+tc.wantCFG+`"/>`)
		})
	}
}

func TestHookIntellivisionUnreadableZipLoadsRaw(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	zipPath := filepath.Join(intvGameDir(), "Games.zip")
	intvWrite(t, fs, zipPath, []byte("not a zip"))

	document := intvGenerate(t, fs, zipPath+"/Game.bin")
	assert.Contains(t, document, `value="Raw &amp; Intellicart"`)
	assert.Equal(t, 1, strings.Count(document, "<file "))
}

func TestHookIntellivisionEmptyPath(t *testing.T) {
	t.Parallel()
	core, err := GetCore("Intellivision")
	require.NoError(t, err)
	override, err := hookIntellivisionWithFS(afero.NewMemMapFs(), core, "")
	require.NoError(t, err)
	assert.Empty(t, override)
}

func TestHookIntellivisionMissingOrInvalidZipLoadsRaw(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"missing zip", "zip is a directory"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			zipPath := filepath.Join(intvGameDir(), "Games.zip")
			if name == "zip is a directory" {
				require.NoError(t, fs.MkdirAll(zipPath, 0o750))
			}

			document := intvGenerate(t, fs, zipPath+"/Game.bin")
			assert.Contains(t, document, `value="Raw &amp; Intellicart"`)
			assert.Equal(t, 1, strings.Count(document, "<file "))
		})
	}
}

func TestHookIntellivisionRejectsUnsupportedExtension(t *testing.T) {
	t.Parallel()
	core, err := GetCore("Intellivision")
	require.NoError(t, err)
	override, err := hookIntellivisionWithFS(afero.NewMemMapFs(), core, filepath.Join(intvGameDir(), "Game.txt"))
	require.Error(t, err)
	assert.Empty(t, override)
}

func TestRunSystemHookIntellivisionReadsDisk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "Game.bin")
	cfgPath := filepath.Join(dir, "Game.cfg")
	require.NoError(t, os.WriteFile(mediaPath, nil, 0o600))
	require.NoError(t, os.WriteFile(cfgPath, intvCFGData, 0o600))

	core, err := GetCore("Intellivision")
	require.NoError(t, err)
	override, err := RunSystemHook(nil, core, mediaPath)
	require.NoError(t, err)
	assert.Contains(t, override, `value="CFG mapping"`)
	assert.Contains(t, override, `index="2" path="../../../../..`+cfgPath+`"`)
}

func TestHookIntellivisionIgnoresEmptyCFG(t *testing.T) {
	t.Parallel()
	t.Run("loose", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		mediaPath := filepath.Join(intvGameDir(), "Game.bin")
		intvWrite(t, fs, mediaPath, nil)
		intvWrite(t, fs, filepath.Join(intvGameDir(), "Game.cfg"), []byte{})

		document := intvGenerate(t, fs, mediaPath)
		assert.Contains(t, document, `value="Raw &amp; Intellicart"`)
		assert.Equal(t, 1, strings.Count(document, "<file "))
	})
	t.Run("zip", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		zipPath := filepath.Join(intvGameDir(), "Games.zip")
		names := []string{"Game.bin", "Game.cfg"}
		intvWrite(t, fs, zipPath, intvZipData(t, names, map[string][]byte{"Game.bin": intvCFGData}))

		document := intvGenerate(t, fs, zipPath+"/Game.bin")
		assert.Contains(t, document, `value="Raw &amp; Intellicart"`)
		assert.Equal(t, 1, strings.Count(document, "<file "))
	})
}

func TestSplitZipPath(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		path    string
		zipPath string
		inner   string
		ok      bool
	}{
		{path: "/games/Games.zip/Game.bin", zipPath: "/games/Games.zip", inner: "Game.bin", ok: true},
		{path: "/games/GAMES.ZIP/Sub/Game.bin", zipPath: "/games/GAMES.ZIP", inner: "Sub/Game.bin", ok: true},
		{path: "/media/fat/a.zip/b.zip/Game.bin", zipPath: "/media/fat/a.zip", inner: "b.zip/Game.bin", ok: true},
		{path: "/media/fat/İ.zip/Game.bin", zipPath: "/media/fat/İ.zip", inner: "Game.bin", ok: true},
		{path: "/media/fat/ȺȺȺȺȺȺȺȺȺ.zip/Game.bin", zipPath: "/media/fat/ȺȺȺȺȺȺȺȺȺ.zip", inner: "Game.bin", ok: true},
		{path: "/media/fat/games/Game.zip", ok: false},
		{path: "/media/fat/games/Game.bin", ok: false},
	} {
		zipPath, inner, ok := splitZipPath(tc.path)
		assert.Equal(t, tc.ok, ok, tc.path)
		assert.Equal(t, tc.zipPath, zipPath, tc.path)
		assert.Equal(t, tc.inner, inner, tc.path)
	}
}
