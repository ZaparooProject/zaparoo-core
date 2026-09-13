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
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

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
