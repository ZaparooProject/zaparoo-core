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

package misterdocs

import (
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCandidateDocsRoots_PreservesOrderAndDeduplicates(t *testing.T) {
	t.Parallel()

	got := candidateDocsRoots([]string{
		filepath.Join("media", "usb0", "games"),
		filepath.Join("media", "usb0"),
		filepath.Join("media", "fat"),
	})
	assert.Equal(t, []string{
		filepath.Join("media", "usb0", "docs"),
		filepath.Join("media", "usb0", "games", "docs"),
		filepath.Join("media", "fat", "docs"),
	}, got)
}

func TestDiscoverSources_FindsArtworkAndManualsByFormat(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	root := filepath.Join("media", "fat")
	artwork := filepath.Join(root, "docs", "SNES", "Artwork")
	manuals := filepath.Join(root, "docs", "NES", "Famicom Disk System Manuals")
	require.NoError(t, fs.MkdirAll(artwork, 0o750))
	require.NoError(t, fs.MkdirAll(manuals, 0o750))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(artwork, "index.tsv"), []byte("#name\tkey\n"), 0o600))

	sources, err := discoverSources(fs, []string{root})
	require.NoError(t, err)
	require.Len(t, sources, 2)
	assert.ElementsMatch(t, []sourceDir{
		{Path: artwork, SystemID: systemdefs.SystemSNES, Kind: sourceArtwork},
		{Path: manuals, SystemID: systemdefs.SystemFDS, Kind: sourceManuals},
	}, sources)
}

func TestSourceIDsForTarget_IncludesFallbacks(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{systemdefs.SystemSNESMSU1, systemdefs.SystemSNES},
		sourceIDsForTarget(systemdefs.SystemSNESMSU1))
}

func TestOrderedTargetSystems_ResolvesAliasesAndFiltersUnindexed(t *testing.T) {
	t.Parallel()

	got := orderedTargetSystems(
		[]string{systemdefs.SystemSNES, systemdefs.SystemGenesis},
		[]string{"MegaDrive", "SNES", "NES"},
	)
	assert.Equal(t, []string{systemdefs.SystemGenesis, systemdefs.SystemSNES}, got)
}
