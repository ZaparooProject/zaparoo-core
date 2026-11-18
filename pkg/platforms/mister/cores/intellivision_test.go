//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package cores

import (
	"archive/zip"
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mgl"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, name := range entries {
		f, err := w.Create(name)
		require.NoError(t, err)
		_, err = f.Write([]byte("x"))
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
			intvWrite(t, fs, cfgPath, nil)
			for _, other := range tc.others {
				intvWrite(t, fs, filepath.Join(intvGameDir(), other), nil)
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
				intvWrite(t, fs, filepath.Join(intvGameDir(), tc.extra), nil)
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
	intvWrite(t, fs, filepath.Join(intvGameDir(), `Tom & Jerry "Demo".cfg`), nil)

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

func TestIntellivisionHookRegistered(t *testing.T) {
	t.Parallel()
	_, ok := systemHooks["Intellivision"]
	assert.True(t, ok)
}
