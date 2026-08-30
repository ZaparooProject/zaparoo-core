//go:build linux

package mister

import (
	"sort"
	"strings"
	"sync"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/cores"
)

// unstableCorePattern is the RBF name pattern the unstable nightlies use:
// <Core>_unstable_<8-digit date>_<hash>.rbf. It is registered without a
// directory so a nightly resolves wherever it was installed — the
// unstable_nightlies_folder database puts them in _Unstable, but the scripts
// people use to fetch individual cores do not always agree. Glob resolution
// picks the newest match, so a folder holding several dated builds of one core
// launches the latest.
func unstableCorePattern(rbfName string) string {
	return rbfName + "_unstable_<date>_<hash>"
}

// unstableNightlyCore is one core published by the unstable nightlies database
// (MiSTer-unstable-nightlies/Unstable_Folder_MiSTer).
type unstableNightlyCore struct {
	// systemID pins the launcher to a single system. Empty means the entry
	// expands to one launcher per system whose stock core is rbfNames[0], so a
	// nightly that serves several systems — the NES core also runs FDS — can be
	// preferred for all of them.
	systemID string
	// idSuffix overrides the launcher ID suffix, which defaults to the system
	// ID. Needed where one system has more than one nightly.
	idSuffix string
	// rbfNames are the core's RBF base names in resolution order. A second name
	// is a legacy name the same core used to ship under.
	rbfNames []string
}

// unstableNightlyCores lists the cores published by the unstable nightlies
// database. Arcade cores (Galaga, Galaxian, QBert, SEGASYS1, SpaceInvaders,
// Tecmo) are omitted because arcade media launches through an MRA that names
// its own RBF, as are the menu core and the MiSTer main binary. Cores with no
// Zaparoo system (AtariST, PC88, ST-V) are omitted too.
var unstableNightlyCores = []unstableNightlyCore{
	{rbfNames: []string{"3DO"}},
	{rbfNames: []string{"AcornAtom"}},
	{rbfNames: []string{"AcornElectron"}},
	{rbfNames: []string{"AliceMC10"}},
	{rbfNames: []string{"Amstrad"}},
	{rbfNames: []string{"Apple-II"}},
	{rbfNames: []string{"Atari7800"}},
	{rbfNames: []string{"AtariLynx"}},
	{rbfNames: []string{"C64"}},
	{rbfNames: []string{"CDi"}},
	{rbfNames: []string{"Chip8"}},
	{rbfNames: []string{"ColecoVision"}},
	{rbfNames: []string{"GBA"}},
	{rbfNames: []string{"Gameboy"}},
	{rbfNames: []string{"MSX"}},
	{rbfNames: []string{"MacPlus"}},
	{rbfNames: []string{"MegaCD"}},
	// The Genesis system's stock core is MegaDrive; Genesis is the name the
	// same core shipped under before it was renamed.
	{rbfNames: []string{"MegaDrive", "Genesis"}},
	// The Minimig nightly also backs the Amiga system, but MiSTer's Amiga
	// launcher is bespoke — it resolves AmigaVision listing files and virtual
	// MGL paths — so a generic alt core would launch that media wrong. Only
	// AmigaCD32, which uses the plain launch path, gets a nightly.
	{
		systemID: systemdefs.SystemAmigaCD32,
		rbfNames: []string{"Minimig"},
	},
	{rbfNames: []string{"NES"}},
	{rbfNames: []string{"NeoGeo"}},
	{rbfNames: []string{"PSX"}},
	{rbfNames: []string{"S32X"}},
	{rbfNames: []string{"SMS"}},
	{rbfNames: []string{"SNES"}},
	{rbfNames: []string{"Saturn"}},
	{rbfNames: []string{"TatungEinstein"}},
	{rbfNames: []string{"Ti994a"}},
	{rbfNames: []string{"TurboGrafx16"}},
	{rbfNames: []string{"X68000"}},
	{rbfNames: []string{"ZX-Spectrum"}},
	{rbfNames: []string{"ZXNext"}},
	{rbfNames: []string{"ao486"}},
	// Dual-SDRAM nightly, matching the DualRAMSaturn stable alt core.
	{
		systemID: systemdefs.SystemSaturn,
		idSuffix: "DualRAMSaturn",
		rbfNames: []string{"Saturn_DualSDRAM"},
	},
}

// systemsByStockCore indexes every system by its stock core's RBF name,
// lowercased. Built once: cores.Systems is a static table, and CreateLaunchers
// runs on every Launchers call.
var systemsByStockCore = sync.OnceValue(func() map[string][]string {
	index := make(map[string][]string, len(cores.Systems))
	for systemID, core := range cores.Systems {
		if core.RBF == "" {
			continue
		}
		_, shortName := splitAltCorePath(core.RBF)
		key := strings.ToLower(shortName)
		index[key] = append(index[key], systemID)
	}
	for key := range index {
		sort.Strings(index[key])
	}
	return index
})

// createUnstableLaunchers builds an Unstable* launcher per nightly core and
// system. They carry no folders or extensions: like the other alt core
// families they only ever launch media the system's stock launcher already
// indexed, reached through launchers.preference or an explicit launcher arg.
func createUnstableLaunchers() []platforms.Launcher {
	launchers := make([]platforms.Launcher, 0, len(unstableNightlyCores))

	for _, entry := range unstableNightlyCores {
		patterns := make([]string, 0, len(entry.rbfNames))
		for _, rbfName := range entry.rbfNames {
			patterns = append(patterns, unstableCorePattern(rbfName))
		}

		systemIDs := []string{entry.systemID}
		if entry.systemID == "" {
			systemIDs = systemsByStockCore()[strings.ToLower(entry.rbfNames[0])]
		}

		for _, systemID := range systemIDs {
			suffix := entry.idSuffix
			if suffix == "" {
				suffix = systemID
			}
			launcherID := "Unstable" + suffix
			launchers = append(launchers, platforms.Launcher{
				ID:       launcherID,
				SystemID: systemID,
				Launch: launchAltCoreCandidates(
					launcherID, systemID, patterns[0], patterns[1:]...,
				),
			})
		}
	}

	return launchers
}
