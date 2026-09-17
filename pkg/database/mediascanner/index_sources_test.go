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

package mediascanner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/mediadb"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	testhelpers "github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestIndexSourcesSuccessfulContributions(t *testing.T) {
	// The production indexer currently uses the global launcher cache.
	for _, mode := range []string{"success", "empty", "failed", "filtered", "cancelled", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "game.nes"), []byte("fixture"), 0o600))
			cfg, err := testhelpers.NewTestConfig(testhelpers.NewMemoryFS(), t.TempDir())
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			local := platforms.Launcher{
				ID: "filesystem", SystemID: systemdefs.SystemNES,
				Folders: []string{dir}, Extensions: []string{".nes"},
			}
			custom := platforms.Launcher{
				ID: "catalog", SystemID: systemdefs.SystemNES,
				SkipFilesystemScan: true, Schemes: []string{"catalog"},
				Scanner: func(
					context.Context, *config.Instance, string, []platforms.ScanResult,
				) ([]platforms.ScanResult, error) {
					switch mode {
					case "empty":
						return nil, nil
					case "failed":
						return nil, errors.New("source unavailable")
					case "cancelled":
						cancel()
						return nil, context.Canceled
					}
					return []platforms.ScanResult{{Path: "catalog://1/game", Name: "Catalog Game"}}, nil
				},
			}
			if mode == "unavailable" {
				custom.Availability = func(*config.Instance) error { return errors.New("not installed") }
			}
			launchers := []platforms.Launcher{local, custom}
			if mode == "filtered" {
				launchers = append(launchers, platforms.Launcher{
					ID: "filter", SystemID: systemdefs.SystemNES,
					Scanner: func(
						_ context.Context, _ *config.Instance, _ string, files []platforms.ScanResult,
					) ([]platforms.ScanResult, error) {
						return files[:1], nil
					},
				})
			}
			pl := mocks.NewMockPlatform()
			pl.On("ID").Return("test")
			pl.On("Settings").Return(platforms.Settings{})
			pl.On("RootDirs", mock.Anything).Return([]string{})
			pl.On("Launchers", mock.Anything).Return(launchers)
			db, cleanup := testhelpers.NewTestDatabase(t)
			defer cleanup()
			testLauncherCacheMutex.Lock()
			previous := helpers.GlobalLauncherCache
			cache := &helpers.LauncherCache{}
			cache.InitializeFromSlice(launchers)
			helpers.GlobalLauncherCache = cache
			defer func() { helpers.GlobalLauncherCache = previous; testLauncherCacheMutex.Unlock() }()
			var sources []IndexedSource
			called := false
			_, err = NewNamesIndexWithSources(ctx, pl, cfg, []systemdefs.System{{ID: systemdefs.SystemNES}},
				db, func(IndexStatus) {}, nil, &IndexSourceOptions{
					LauncherIDs: []string{"filesystem", "catalog"},
					Completed: func(result []IndexedSource) {
						called = true
						status, statusErr := db.MediaDB.GetIndexingStatus()
						require.NoError(t, statusErr)
						require.Equal(t, mediadb.IndexingStatusCompleted, status)
						sources = result
					},
				})
			if mode == "cancelled" {
				require.ErrorIs(t, err, context.Canceled)
				require.False(t, called)
				return
			}
			require.NoError(t, err)
			require.True(t, called)
			want := []IndexedSource{{LauncherID: "filesystem", SystemID: systemdefs.SystemNES, Files: 1}}
			if mode == "success" {
				want = append(want, IndexedSource{LauncherID: "catalog", SystemID: systemdefs.SystemNES, Files: 1})
			}
			require.ElementsMatch(t, want, sources)
		})
	}
}
