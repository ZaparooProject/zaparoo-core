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

package localmedia

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/mocks"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/scantest"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// sourceRootPlatform is a minimal platforms.SourceRootReader for the
// granted-root-join tests below: only SourceRoots is ever consulted by
// resolveSystemsFromPlatform (ReadSourceDir is a scrape/index-time concern,
// unused here), but Go requires both to satisfy the interface.
type sourceRootPlatform struct {
	*mocks.MockPlatform
	roots []string
}

func (p *sourceRootPlatform) SourceRoots(context.Context) ([]string, error) {
	return p.roots, nil
}

func (*sourceRootPlatform) ReadSourceDir(context.Context, string) ([]platforms.SourceEntry, error) {
	return nil, nil
}

func TestResolveSystemsUsesIndexedSourceRootForVirtualLauncher(t *testing.T) {
	t.Parallel()
	db, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)
	root := t.TempDir()
	scantest.IndexScanResults(t, db, systemdefs.SystemScummVM, database.ScanReconcileOpts{}, platforms.ScanResult{
		Path: "scummvm://monkey/Monkey%20Island", NoExt: true,
		Source: &platforms.MediaSource{
			Path: filepath.Join(root, "Monkey"), Root: root, Kind: platforms.MediaSourceDirectory,
		},
	})

	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)
	pl := &mocks.MockPlatform{}
	pl.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string(nil))
	pl.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{{
		ID: "ScummVM", SystemID: systemdefs.SystemScummVM, Schemes: []string{shared.SchemeScummVM},
	}})

	systems, err := resolveSystemsFromPlatform(t.Context(), cfg, pl, afero.NewMemMapFs(), db, nil)
	require.NoError(t, err)
	require.Len(t, systems, 1)
	require.Equal(t, []string{root}, systems[0].ROMPaths)
}

// The exact gap found testing #1606 on a real device: Android's own
// source-root indexing walk (mediascanner/source_roots.go) never sets
// ScanResult.Source, so GetMediaSourceRoots - this scraper's only other way
// to find a granted root - stays empty for every Android system, and the
// folder-cover scraper silently finds nothing to scan, for any system, every
// time. A granted root must still resolve by joining it against the system's
// own launcher folders, the same way media.browse's root discovery already
// does for the identical reason.
func TestResolveSystemsJoinsGrantedSourceRootsAgainstLauncherFolders(t *testing.T) {
	t.Parallel()
	db, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	const root = "source://abc123"
	scantest.IndexScanResults(t, db, systemdefs.SystemNES, database.ScanReconcileOpts{},
		platforms.ScanResult{Path: root + "/NES/Game.nes", Name: "Game"})

	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)
	pl := &sourceRootPlatform{MockPlatform: &mocks.MockPlatform{}, roots: []string{root}}
	pl.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string(nil))
	pl.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{{
		ID: "TestNES", SystemID: systemdefs.SystemNES, Folders: []string{"NES"}, Extensions: []string{".nes"},
	}})

	systems, err := resolveSystemsFromPlatform(t.Context(), cfg, pl, afero.NewMemMapFs(), db, nil)
	require.NoError(t, err)
	require.Len(t, systems, 1)
	assert.Equal(t, []string{root + "/NES"}, systems[0].ROMPaths)
}

// A root the host no longer grants must never be offered to the scraper: a
// stale ROMPaths entry from before a revocation would try to walk a folder
// Core no longer has access to.
func TestResolveSystemsOmitsARevokedSourceRoot(t *testing.T) {
	t.Parallel()
	db, cleanup := helpers.NewInMemoryMediaDB(t)
	t.Cleanup(cleanup)

	const root = "source://abc123"
	scantest.IndexScanResults(t, db, systemdefs.SystemNES, database.ScanReconcileOpts{},
		platforms.ScanResult{Path: root + "/NES/Game.nes", Name: "Game"})

	cfg, err := config.NewConfig(t.TempDir(), config.BaseDefaults)
	require.NoError(t, err)
	pl := &sourceRootPlatform{MockPlatform: &mocks.MockPlatform{}, roots: nil}
	pl.On("RootDirs", mock.AnythingOfType("*config.Instance")).Return([]string(nil))
	pl.On("Launchers", mock.AnythingOfType("*config.Instance")).Return([]platforms.Launcher{{
		ID: "TestNES", SystemID: systemdefs.SystemNES, Folders: []string{"NES"}, Extensions: []string{".nes"},
	}})

	systems, err := resolveSystemsFromPlatform(t.Context(), cfg, pl, afero.NewMemMapFs(), db, nil)
	require.NoError(t, err)
	assert.Empty(t, systems, "no granted root means no ROMPaths, so the system is skipped entirely")
}
