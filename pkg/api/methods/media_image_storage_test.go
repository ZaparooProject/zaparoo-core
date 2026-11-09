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

package methods

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models/requests"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/permissions"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/libretrothumbs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/state"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const libretroBoxartText = "images://libretro/NES/Named_Boxarts/A (USA).png"

func writeLibretroBoxart(t *testing.T, imageDir string) string {
	t.Helper()
	path := filepath.Join(imageDir, "libretro", "NES", "Named_Boxarts", "A (USA).png")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte("boxart bytes"), 0o600))
	return path
}

func loadImageFile(t *testing.T, imageDir, text string) (mediaImageFile, bool) {
	t.Helper()
	file, stale, err := loadMediaImageFile(
		context.Background(), afero.NewOsFs(), imageDir, nil, sourceImageTestRow(),
		&database.MediaProperty{Text: text}, true, "property:image-boxart", 1024, nil,
	)
	require.NoError(t, err)
	return file, stale
}

// An images:// property is read from the image folder and its thumbnails are
// named by the images:// path, so moving the folder keeps them.
func TestLoadMediaImageFileResolvesImagePaths(t *testing.T) {
	t.Parallel()
	imageDir := filepath.Join(t.TempDir(), "images")
	writeLibretroBoxart(t, imageDir)

	file, stale := loadImageFile(t, imageDir, libretroBoxartText)
	require.False(t, stale)
	assert.Equal(t, []byte("boxart bytes"), file.data)
	assert.Contains(t, file.sourceKey, ":"+libretroBoxartText, "keyed by the images:// path")

	moved := filepath.Join(t.TempDir(), "moved")
	require.NoError(t, os.Rename(imageDir, moved))
	movedFile, stale := loadImageFile(t, moved, libretroBoxartText)
	require.False(t, stale)
	assert.Equal(t, file.sourceKey, movedFile.sourceKey, "a moved folder keeps its thumbnails")

	_, stale = loadImageFile(t, imageDir, libretroBoxartText)
	assert.True(t, stale, "a file missing from the image folder is a stale property")
}

func TestLoadMediaImageFileRejectsImagePathsOutsideTheFolder(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	imageDir := filepath.Join(root, "images")
	require.NoError(t, os.MkdirAll(imageDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "secret.png"), []byte("secret"), 0o600))

	file, stale := loadImageFile(t, imageDir, "images://../secret.png")
	assert.True(t, stale)
	assert.Nil(t, file.data)

	_, stale = loadImageFile(t, "", libretroBoxartText)
	assert.True(t, stale, "no image folder resolves nothing")
}

// A recorded cover whose source is an images:// path is checked in the image
// folder, with the key loadMediaImageFile gives it.
func TestRecordedSourceCurrentResolvesImagePaths(t *testing.T) {
	t.Parallel()
	imageDir := filepath.Join(t.TempDir(), "images")
	writeLibretroBoxart(t, imageDir)
	file, stale := loadImageFile(t, imageDir, libretroBoxartText)
	require.False(t, stale)
	thumb := &database.MediaCoverThumb{SourcePath: libretroBoxartText, SourceKey: file.sourceKey}

	assert.True(t, recordedSourceCurrent(afero.NewOsFs(), imageDir, thumb))
	assert.False(t, recordedSourceCurrent(afero.NewOsFs(), filepath.Join(t.TempDir(), "other"), thumb))
	thumb.SourcePath = "images://../A (USA).png"
	assert.False(t, recordedSourceCurrent(afero.NewOsFs(), imageDir, thumb))
}

// The thumbnail cache shares the image folder with scraped and user images;
// a version reap only ever empties its own thumbs folder.
func TestReapStaleVersionsLeavesTheRestOfTheImageFolder(t *testing.T) {
	t.Parallel()
	fs := afero.NewMemMapFs()
	imageDir := filepath.Join(string(filepath.Separator), "images")
	keep := []string{
		filepath.Join(imageDir, "libretro", "NES", "Named_Boxarts", "A.png"),
		filepath.Join(imageDir, "mine", "cover.png"),
		filepath.Join(imageDir, "v2", "not-a-cache.png"),
	}
	for _, path := range keep {
		require.NoError(t, afero.WriteFile(fs, path, []byte("x"), 0o600))
	}
	stale := filepath.Join(imageDir, "thumbs", "v2", "ab", "old.webp")
	require.NoError(t, afero.WriteFile(fs, stale, []byte("x"), 0o600))

	cache := newTestThumbCache(fs, filepath.Join(imageDir, "thumbs", mediaThumbCacheVersionDir()))
	cache.reapStaleVersions()

	for _, path := range keep {
		ok, err := afero.Exists(fs, path)
		require.NoError(t, err)
		assert.True(t, ok, path)
	}
	ok, err := afero.Exists(fs, stale)
	require.NoError(t, err)
	assert.False(t, ok, "an old cache version is still reaped")

	// A cache directory that is not thumbs/v<N> reaps nothing at all.
	stray := newTestThumbCache(fs, filepath.Join(imageDir, "mine"))
	stray.reapStaleVersions()
	for _, path := range keep {
		ok, err := afero.Exists(fs, path)
		require.NoError(t, err)
		assert.True(t, ok, path)
	}
}

// A thumbs folder linked to somewhere else is not Core's to empty.
func TestReapStaleVersionsSkipsALinkedThumbsFolder(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := filepath.Join(root, "elsewhere")
	other := filepath.Join(target, "not-a-cache.png")
	require.NoError(t, os.MkdirAll(target, 0o750))
	require.NoError(t, os.WriteFile(other, []byte("x"), 0o600))
	imageDir := filepath.Join(root, "images")
	require.NoError(t, os.MkdirAll(imageDir, 0o750))
	require.NoError(t, os.Symlink(target, filepath.Join(imageDir, "thumbs")))

	cache := newTestThumbCache(afero.NewOsFs(), filepath.Join(imageDir, "thumbs", mediaThumbCacheVersionDir()))
	cache.reapStaleVersions()

	_, err := os.Stat(other)
	assert.NoError(t, err, "files behind a linked thumbs folder are kept")
}

func imageSettingsEnv(t *testing.T, pl platforms.Platform, cfg *config.Instance, params any) requests.RequestEnv {
	t.Helper()
	appState, ns := state.NewState(pl, "test-boot-uuid")
	t.Cleanup(func() { drainCh(ns) })
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	return requests.RequestEnv{
		Context:  context.Background(),
		Platform: pl,
		Config:   cfg,
		State:    appState,
		Params:   raw,
		IsLocal:  true,
	}
}

// The image settings round-trip through settings.update and settings, and a
// change moves the running thumbnail cache without restarting Core.
func TestImageSettingsUpdateReloadsTheThumbCache(t *testing.T) {
	// Not parallel: installs the process-wide thumb cache.
	dataDir := t.TempDir()
	pl := mocks.NewMockPlatform()
	pl.On("ID").Return("test-platform").Maybe()
	pl.On("ManagedByPackageManager").Return(false).Maybe()
	pl.On("Settings").Return(platforms.Settings{DataDir: dataDir, HostManagedPaths: true})
	cfg, err := config.NewConfig(t.TempDir(), config.Values{})
	require.NoError(t, err)

	stop := InitMediaThumbCache(pl, cfg)
	t.Cleanup(func() { mediaThumbCachePointer.Store(nil) })
	first := mediaThumbCachePointer.Load()
	require.NotNil(t, first)
	assert.Equal(t, filepath.Join(dataDir, "images", "thumbs", mediaThumbCacheVersionDir()), first.dir)
	assert.Equal(t, int64(1<<30), first.maxBytes.Load())

	capMB := 64
	_, err = HandleSettingsUpdate(imageSettingsEnv(t, pl, cfg,
		models.UpdateSettingsParams{MediaThumbCacheMaxMB: &capMB}))
	require.NoError(t, err)
	assert.Same(t, first, mediaThumbCachePointer.Load(), "a new cap is applied in place")
	assert.Equal(t, int64(64<<20), first.maxBytes.Load())

	newDir := filepath.Join(t.TempDir(), "nas", "images")
	_, err = HandleSettingsUpdate(imageSettingsEnv(t, pl, cfg,
		models.UpdateSettingsParams{MediaImageDir: &newDir}))
	require.NoError(t, err)
	moved := mediaThumbCachePointer.Load()
	require.NotSame(t, first, moved)
	assert.Equal(t, filepath.Join(newDir, "thumbs", mediaThumbCacheVersionDir()), moved.dir)
	assert.Equal(t, int64(64<<20), moved.maxBytes.Load())
	exists, err := afero.DirExists(moved.fs, moved.dir)
	require.NoError(t, err)
	assert.True(t, exists)

	result, err := HandleSettings(imageSettingsEnv(t, pl, cfg, struct{}{}))
	require.NoError(t, err)
	resp, ok := result.(models.SettingsResponse)
	require.True(t, ok)
	require.NotNil(t, resp.MediaImageDir)
	assert.Equal(t, newDir, *resp.MediaImageDir)
	assert.Equal(t, 64, resp.MediaThumbCacheMaxMB)

	stopped := make(chan struct{})
	go func() {
		stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("stopping the thumb cache did not wait out the replaced one")
	}
	select {
	case <-first.reapDone:
	default:
		t.Fatal("the replaced cache's reaps were left running")
	}
	assert.False(t, ReloadMediaThumbCache(cfg), "a stopped cache is not reloaded")
}

// A save that fails leaves the image folder and cache cap as they were, in
// the config and in the running thumbnail cache.
func TestImageSettingsUpdateKeepsTheOldValuesWhenTheSaveFails(t *testing.T) {
	// Not parallel: installs the process-wide thumb cache.
	dataDir := t.TempDir()
	pl := mocks.NewMockPlatform()
	pl.On("ID").Return("test-platform").Maybe()
	pl.On("ManagedByPackageManager").Return(false).Maybe()
	pl.On("Settings").Return(platforms.Settings{DataDir: dataDir, HostManagedPaths: true})
	cfg, saveFS := newSaveFailConfig(t)

	stop := InitMediaThumbCache(pl, cfg)
	t.Cleanup(func() {
		stop()
		mediaThumbCachePointer.Store(nil)
	})
	cache := mediaThumbCachePointer.Load()
	require.NotNil(t, cache)

	saveFS.failSaves.Store(true)

	newDir := filepath.Join(t.TempDir(), "nas", "images")
	capMB := 64
	_, err := HandleSettingsUpdate(imageSettingsEnv(t, pl, cfg,
		models.UpdateSettingsParams{MediaImageDir: &newDir, MediaThumbCacheMaxMB: &capMB}))
	require.ErrorContains(t, err, "failed to save config")

	assert.Empty(t, cfg.ImageDirSetting())
	assert.Zero(t, cfg.ThumbCacheMaxMBSetting())
	assert.Same(t, cache, mediaThumbCachePointer.Load(), "the cache stays on the old folder")
	assert.Equal(t, int64(1<<30), cache.maxBytes.Load())
}

// Not parallel: it holds the process-wide scraping status.
//
//nolint:paralleltest // uses the global scraping status
func TestImageDirSettingRefusedWhileImagesDownload(t *testing.T) {
	dataDir := t.TempDir()
	pl := mocks.NewMockPlatform()
	pl.On("ID").Return("test-platform").Maybe()
	pl.On("Settings").Return(platforms.Settings{DataDir: dataDir, HostManagedPaths: true})
	current := filepath.Join(t.TempDir(), "current")
	next := filepath.Join(t.TempDir(), "next")
	cfg, err := config.NewConfig(t.TempDir(), config.Values{})
	require.NoError(t, err)
	cfg.SetImageDir(current)

	update := func(dir string) error {
		_, updateErr := HandleSettingsUpdate(
			imageSettingsEnv(t, pl, cfg, models.UpdateSettingsParams{MediaImageDir: &dir}))
		return updateErr
	}

	ClearScrapingStatus()
	t.Cleanup(ClearScrapingStatus)
	require.True(t, scrapingStatusInstance.startIfNotRunning(libretrothumbs.ScraperID, false))

	err = update(next)
	var categorized *models.CategorizedError
	require.ErrorAs(t, err, &categorized)
	assert.Equal(t, models.ErrorCategoryBusy, categorized.Category)
	assert.Equal(t, current, cfg.ImageDirSetting())

	require.NoError(t, update(current), "sending the current folder changes nothing")

	ClearScrapingStatus()
	require.True(t, scrapingStatusInstance.startIfNotRunning("gamelist.xml", false))
	require.NoError(t, update(next), "other scrapers do not write to the image folder")
	assert.Equal(t, next, cfg.ImageDirSetting())
}

func TestImageDirSettingValidation(t *testing.T) {
	t.Parallel()
	dataDir := t.TempDir()
	pl := mocks.NewMockPlatform()
	pl.On("ID").Return("test-platform").Maybe()
	pl.On("Settings").Return(platforms.Settings{DataDir: dataDir, HostManagedPaths: true})
	cfg, err := config.NewConfig(t.TempDir(), config.Values{})
	require.NoError(t, err)
	file := filepath.Join(t.TempDir(), "file.txt")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))

	root := filepath.VolumeName(dataDir) + string(filepath.Separator)
	refused := []string{root, file, filepath.Join(file, "images"), filepath.Join(file, "a", "b")}
	// A link to nothing cannot have the folder created through it. Windows
	// only allows links with a privilege CI may not hold.
	broken := filepath.Join(t.TempDir(), "broken")
	if linkErr := os.Symlink(filepath.Join(t.TempDir(), "gone"), broken); linkErr == nil {
		refused = append(refused, filepath.Join(broken, "images"))
	} else if runtime.GOOS != "windows" {
		require.NoError(t, linkErr)
	}
	for _, dir := range refused {
		_, updateErr := HandleSettingsUpdate(
			imageSettingsEnv(t, pl, cfg, models.UpdateSettingsParams{MediaImageDir: &dir}))
		require.Error(t, updateErr, dir)
	}
	assert.Empty(t, cfg.ImageDirSetting(), "a refused folder is not stored")

	remote := imageSettingsEnv(t, pl, cfg, models.UpdateSettingsParams{MediaImageDir: &dataDir})
	remote.IsLocal = false
	remote.ClientRole = string(permissions.RoleMember)
	_, err = HandleSettingsUpdate(remote)
	require.Error(t, err)
	assert.Empty(t, cfg.ImageDirSetting())

	missing := filepath.Join(t.TempDir(), "new", "images")
	_, err = HandleSettingsUpdate(
		imageSettingsEnv(t, pl, cfg, models.UpdateSettingsParams{MediaImageDir: &missing}))
	require.NoError(t, err, "a folder that does not exist yet is created later")
	assert.Equal(t, missing, cfg.ImageDirSetting())
}
