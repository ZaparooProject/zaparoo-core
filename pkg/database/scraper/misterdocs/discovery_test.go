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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type denyOpenFs struct {
	afero.Fs
	deniedPath string
}

func (fs denyOpenFs) Open(name string) (afero.File, error) {
	if filepath.Clean(name) == filepath.Clean(fs.deniedPath) {
		return nil, os.ErrPermission
	}
	file, err := fs.Fs.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", name, err)
	}
	return file, nil
}

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
		filepath.Clean("/media/usb6/docs"),
		filepath.Clean("/media/usb7/docs"),
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

func TestDiscoverSources_SkipsUnreadableRootAndContinues(t *testing.T) {
	t.Parallel()

	baseFS := afero.NewMemMapFs()
	deniedBase := filepath.Join("media", "usb")
	deniedRoot := filepath.Join(deniedBase, "docs")
	goodBase := filepath.Join("media", "fat")
	artwork := filepath.Join(goodBase, "docs", "SNES", artworkDirName)
	require.NoError(t, baseFS.MkdirAll(deniedRoot, 0o750))
	require.NoError(t, baseFS.MkdirAll(artwork, 0o750))
	require.NoError(t, afero.WriteFile(baseFS, filepath.Join(artwork, indexFileName), []byte("#name\tkey\n"), 0o600))
	fs := denyOpenFs{Fs: baseFS, deniedPath: deniedRoot}

	sources, err := discoverSources(fs, []string{deniedBase, goodBase})
	require.NoError(t, err)
	assert.Equal(t, []sourceDir{{Path: artwork, SystemID: systemdefs.SystemSNES, Kind: sourceArtwork}}, sources)
}

func TestDiscoverSources_IgnoresUnsupportedLayouts(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	root := filepath.Join("media", "fat")
	docsRoot := filepath.Join(root, "docs")
	require.NoError(t, fs.MkdirAll(filepath.Join(docsRoot, "Unknown", "Manuals"), 0o750))
	require.NoError(t, fs.MkdirAll(filepath.Join(docsRoot, "SNES", artworkDirName), 0o750))
	require.NoError(t, fs.MkdirAll(filepath.Join(docsRoot, "SNES", "Cheats"), 0o750))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(docsRoot, "README.txt"), []byte("text"), 0o600))

	sources, err := discoverSources(fs, []string{root})
	require.NoError(t, err)
	assert.Empty(t, sources)
}

func TestResolveSourceSystem_FallsBackToParent(t *testing.T) {
	t.Parallel()

	assert.Equal(t, systemdefs.SystemSNES, resolveSourceSystem("SNES", "Scans Manuals"))
	assert.Empty(t, resolveSourceSystem("Unknown", "Scans Manuals"))
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

// TestResolveSourceSystem_ResolvesEveryPublishedPackFolder pins the folder
// names the MiSTer Artwork Pack publishes (PACK_FORMAT.md, "Published
// systems"), so a rename on either side is caught here rather than as a pack
// that silently imports nothing. Satellaview is published too but has no
// system in systemdefs, so it is deliberately absent.
func TestResolveSourceSystem_ResolvesEveryPublishedPackFolder(t *testing.T) {
	t.Parallel()

	folders := map[string]string{
		"3DO":                systemdefs.System3DO,
		"ATARI5200":          systemdefs.SystemAtari5200,
		"ATARI7800":          systemdefs.SystemAtari7800,
		"AmigaCD32":          systemdefs.SystemAmigaCD32,
		"Arcade":             systemdefs.SystemArcade,
		"Atari2600":          systemdefs.SystemAtari2600,
		"AtariLynx":          systemdefs.SystemAtariLynx,
		"CD-i":               systemdefs.SystemCDI,
		"Coleco":             systemdefs.SystemColecoVision,
		"FDS":                systemdefs.SystemFDS,
		"GAMEBOY":            systemdefs.SystemGameboy,
		"GBA":                systemdefs.SystemGBA,
		"GBC":                systemdefs.SystemGameboyColor,
		"GameGear":           systemdefs.SystemGameGear,
		"Genesis":            systemdefs.SystemGenesis,
		"Intellivision":      systemdefs.SystemIntellivision,
		"Jaguar":             systemdefs.SystemJaguar,
		"MegaCD":             systemdefs.SystemMegaCD,
		"N64":                systemdefs.SystemNintendo64,
		"NEOGEO":             systemdefs.SystemNeoGeo,
		"NES":                systemdefs.SystemNES,
		"NeoGeo-CD":          systemdefs.SystemNeoGeoCD,
		"NeoGeoPocket":       systemdefs.SystemNeoGeoPocket,
		"NeoGeoPocket-Color": systemdefs.SystemNeoGeoPocketColor,
		"ODYSSEY2":           systemdefs.SystemOdyssey2,
		"PSX":                systemdefs.SystemPSX,
		"S32X":               systemdefs.SystemSega32X,
		"SG-1000":            systemdefs.SystemSG1000,
		"SMS":                systemdefs.SystemMasterSystem,
		"SNES":               systemdefs.SystemSNES,
		"Saturn":             systemdefs.SystemSaturn,
		"SuperGrafx":         systemdefs.SystemSuperGrafx,
		"TGFX16":             systemdefs.SystemTurboGrafx16,
		"TGFX16-CD":          systemdefs.SystemTurboGrafx16CD,
		"VECTREX":            systemdefs.SystemVectrex,
		"VirtualBoy":         systemdefs.SystemVirtualBoy,
		"WonderSwan":         systemdefs.SystemWonderSwan,
		"WonderSwanColor":    systemdefs.SystemWonderSwanColor,
	}
	for folder, want := range folders {
		assert.Equal(t, want, resolveSourceSystem(folder, ""), "docs/%s/Artwork", folder)
	}
}

func TestSourceIDsForTarget_FollowsArtworkPackSiblingRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		target string
		want   []string
	}{
		// Game Boy and Game Boy Color each try the other; Super Game Boy has
		// no pack of its own and reads both.
		{target: systemdefs.SystemGameboy, want: []string{systemdefs.SystemGameboy, systemdefs.SystemGameboyColor}},
		{target: systemdefs.SystemGameboyColor, want: []string{
			systemdefs.SystemGameboyColor, systemdefs.SystemGameboy,
		}},
		{target: systemdefs.SystemSuperGameboy, want: []string{
			systemdefs.SystemSuperGameboy, systemdefs.SystemGameboy, systemdefs.SystemGameboyColor,
		}},
		// A disk release may borrow the cartridge box; a cartridge must never
		// receive the disk release's.
		{target: systemdefs.SystemFDS, want: []string{systemdefs.SystemFDS, systemdefs.SystemNES}},
		{target: systemdefs.SystemNES, want: []string{systemdefs.SystemNES}},
		// Separately catalogued systems do not fill each other's gaps, even
		// where the general system fallbacks say they may.
		{target: systemdefs.SystemSG1000, want: []string{systemdefs.SystemSG1000}},
		{target: systemdefs.SystemNeoGeoPocketColor, want: []string{systemdefs.SystemNeoGeoPocketColor}},
		// Variants of one catalogue still inherit from it.
		{target: systemdefs.SystemCPS2, want: []string{systemdefs.SystemCPS2, systemdefs.SystemArcade}},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, sourceIDsForTarget(tt.target), tt.target)
	}
}

func TestDiscoverSources_AcceptsArtworkDirectoryWithoutIndex(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	root := filepath.Join("media", "fat")
	artwork := filepath.Join(root, "docs", "Genesis", artworkDirName)
	require.NoError(t, fs.MkdirAll(artwork, 0o750))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(artwork, "Sonic (USA).jpg"), []byte("image"), 0o600))

	sources, err := discoverSources(fs, []string{root})
	require.NoError(t, err)
	assert.Equal(t, []sourceDir{{Path: artwork, SystemID: systemdefs.SystemGenesis, Kind: sourceArtwork}}, sources)
}
