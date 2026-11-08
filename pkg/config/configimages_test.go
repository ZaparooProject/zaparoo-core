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

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImagePathRoundTrip(t *testing.T) {
	t.Parallel()
	imageDir := filepath.Join(t.TempDir(), "images")

	text := ImagePath("libretro/NES/Named_Boxarts/A (USA).png")
	assert.Equal(t, "images://libretro/NES/Named_Boxarts/A (USA).png", text)
	assert.True(t, IsImagePath(text))
	assert.False(t, IsImagePath("/data/libretro-thumbnails/A.png"))
	assert.False(t, IsImagePath("source://abc/A.png"))

	got, ok := ResolveImagePath(imageDir, text)
	require.True(t, ok)
	assert.Equal(t, filepath.Join(imageDir, "libretro", "NES", "Named_Boxarts", "A (USA).png"), got)
}

// The text comes from the database, so nothing in it may leave the folder.
func TestResolveImagePathRejectsEscapes(t *testing.T) {
	t.Parallel()
	imageDir := filepath.Join(t.TempDir(), "images")
	for _, text := range []string{
		"images://",
		"images://../secret.png",
		"images://libretro/../../secret.png",
		"images://libretro/./a.png",
		"images:///etc/passwd",
		"images://libretro//a.png",
		`images://libretro\..\..\a.png`,
		"/abs/a.png",
	} {
		_, ok := ResolveImagePath(imageDir, text)
		assert.False(t, ok, text)
	}
	_, ok := ResolveImagePath("", "images://libretro/a.png")
	assert.False(t, ok, "no image folder")
	_, ok = ResolveImagePath("relative", "images://libretro/a.png")
	assert.False(t, ok, "a relative image folder")
}

func TestImageDir(t *testing.T) {
	t.Parallel()
	dataDir := filepath.Join(t.TempDir(), "data")
	cfg, err := NewConfig(t.TempDir(), Values{})
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(dataDir, "images"), cfg.ImageDir(dataDir), "defaults under the data folder")
	assert.Empty(t, cfg.ImageDirSetting())

	custom := filepath.Join(t.TempDir(), "nas", "images")
	cfg.SetImageDir(custom)
	assert.Equal(t, custom, cfg.ImageDir(dataDir))
	assert.Equal(t, custom, cfg.ImageDirSetting())

	cfg.SetImageDir("covers")
	got := cfg.ImageDir(dataDir)
	assert.True(t, filepath.IsAbs(got), "a relative folder always resolves to an absolute one")
	assert.Equal(t, "covers", filepath.Base(got))

	cfg.SetImageDir("  ")
	assert.Equal(t, filepath.Join(dataDir, "images"), cfg.ImageDir(dataDir))
}

func TestThumbCacheMaxMB(t *testing.T) {
	t.Parallel()
	cfg, err := NewConfig(t.TempDir(), Values{})
	require.NoError(t, err)

	assert.Equal(t, DefaultThumbCacheMaxMB, cfg.ThumbCacheMaxMB())
	assert.Equal(t, int64(1<<30), cfg.ThumbCacheMaxBytes(), "1 GiB by default")

	cfg.SetThumbCacheMaxMB(256)
	assert.Equal(t, int64(256<<20), cfg.ThumbCacheMaxBytes())

	cfg.SetThumbCacheMaxMB(0)
	assert.Equal(t, DefaultThumbCacheMaxMB, cfg.ThumbCacheMaxMB(), "zero restores the default")
	cfg.SetThumbCacheMaxMB(-5)
	assert.Equal(t, DefaultThumbCacheMaxMB, cfg.ThumbCacheMaxMB())
}

func TestImageSettingsRoundTripThroughTheConfigFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg, err := NewConfig(dir, Values{})
	require.NoError(t, err)
	custom := filepath.Join(t.TempDir(), "images")
	cfg.SetImageDir(custom)
	cfg.SetThumbCacheMaxMB(300)
	require.NoError(t, cfg.Save())

	data, err := os.ReadFile(filepath.Join(dir, CfgFile)) //nolint:gosec // test temp dir
	require.NoError(t, err)
	assert.Contains(t, string(data), "image_dir")
	assert.Contains(t, string(data), "thumb_cache_max_mb = 300")

	loaded, err := NewConfig(dir, Values{})
	require.NoError(t, err)
	assert.Equal(t, custom, loaded.ImageDirSetting())
	assert.Equal(t, 300, loaded.ThumbCacheMaxMB())
}
