//go:build linux

package mister

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/launchables"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mgls"
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

const (
	misterLaunchableCategoryComputer = "Computer"
	misterLaunchableCategoryConsole  = "Console"
	misterLaunchableCategoryOther    = "Other"
)

type misterCoreLaunchableDefinition struct {
	ID       uuid.UUID
	Name     string
	Category string
	CorePath string
}

var misterCoreLaunchableDefinitions = []misterCoreLaunchableDefinition{
	{ID: launchables.MisterConsoleAY38500, Name: "AY-3-8500", Category: misterLaunchableCategoryConsole, CorePath: filepath.Join("_Console", "AY-3-8500")},
	{ID: launchables.MisterConsoleBBCBridgeCompanion, Name: "BBC Bridge Companion", Category: misterLaunchableCategoryConsole, CorePath: filepath.Join("_Console", "BBCBridgeCompanion")},
	{ID: launchables.MisterConsoleMyVision, Name: "My Vision", Category: misterLaunchableCategoryConsole, CorePath: filepath.Join("_Console", "MyVision")},
	{ID: launchables.MisterConsoleSuperVision8000, Name: "Super Vision 8000", Category: misterLaunchableCategoryConsole, CorePath: filepath.Join("_Console", "Super_Vision_8000")},
	{ID: launchables.MisterComputerAltair8800, Name: "Altair 8800", Category: misterLaunchableCategoryComputer, CorePath: filepath.Join("_Computer", "Altair8800")},
	{ID: launchables.MisterComputerArchie, Name: "Archie", Category: misterLaunchableCategoryComputer, CorePath: filepath.Join("_Computer", "Archie")},
	{ID: launchables.MisterComputerAtariST, Name: "Atari ST", Category: misterLaunchableCategoryComputer, CorePath: filepath.Join("_Computer", "AtariST")},
	{ID: launchables.MisterComputerC128, Name: "Commodore 128", Category: misterLaunchableCategoryComputer, CorePath: filepath.Join("_Computer", "C128")},
	{ID: launchables.MisterComputerCoCo3, Name: "CoCo 3", Category: misterLaunchableCategoryComputer, CorePath: filepath.Join("_Computer", "CoCo3")},
	{ID: launchables.MisterComputerColecoAdam, Name: "Coleco Adam", Category: misterLaunchableCategoryComputer, CorePath: filepath.Join("_Computer", "ColecoAdam")},
	{ID: launchables.MisterComputerEG2000, Name: "EG2000 Colour Genie", Category: misterLaunchableCategoryComputer, CorePath: filepath.Join("_Computer", "eg2000")},
	{ID: launchables.MisterComputerEnterprise, Name: "Enterprise", Category: misterLaunchableCategoryComputer, CorePath: filepath.Join("_Computer", "Enterprise")},
	{ID: launchables.MisterComputerHomelab, Name: "Homelab", Category: misterLaunchableCategoryComputer, CorePath: filepath.Join("_Computer", "Homelab")},
	{ID: launchables.MisterComputerIQ151, Name: "IQ-151", Category: misterLaunchableCategoryComputer, CorePath: filepath.Join("_Computer", "IQ151")},
	{ID: launchables.MisterComputerMacLC, Name: "Mac LC", Category: misterLaunchableCategoryComputer, CorePath: filepath.Join("_Computer", "MacLC")},
	{ID: launchables.MisterComputerOndraSPO186, Name: "Ondra SPO186", Category: misterLaunchableCategoryComputer, CorePath: filepath.Join("_Computer", "Ondra_SPO186")},
	{ID: launchables.MisterComputerPC88, Name: "PC-88", Category: misterLaunchableCategoryComputer, CorePath: filepath.Join("_Computer", "PC88")},
	{ID: launchables.MisterComputerPCjr, Name: "PCjr", Category: misterLaunchableCategoryComputer, CorePath: filepath.Join("_Computer", "PCjr")},
	{ID: launchables.MisterComputerSharpMZ, Name: "Sharp MZ", Category: misterLaunchableCategoryComputer, CorePath: filepath.Join("_Computer", "SharpMZ")},
	{ID: launchables.MisterComputerTK2000, Name: "TK2000", Category: misterLaunchableCategoryComputer, CorePath: filepath.Join("_Computer", "TK2000")},
	{ID: launchables.MisterComputerTandy1000, Name: "Tandy 1000", Category: misterLaunchableCategoryComputer, CorePath: filepath.Join("_Computer", "Tandy1000")},
	{ID: launchables.MisterComputerVT52, Name: "VT52", Category: misterLaunchableCategoryComputer, CorePath: filepath.Join("_Computer", "VT52")},
}

// Launchables exposes launch-only MiSTer core entries that do not already have
// media launchers.
func (p *Platform) Launchables(*config.Instance) []launchables.Launchable {
	items := []launchables.Launchable{
		launchables.VirtualSystem{
			ID:       launchables.MisterOtherChess,
			Name:     "Chess",
			Category: misterLaunchableCategoryOther,
			Launch:   p.launchOtherCore(filepath.Join("_Other", "Chess")),
			Test:     testOtherCore("Chess"),
		},
		launchables.VirtualSystem{
			ID:       launchables.MisterOtherDonut,
			Name:     "Donut",
			Category: misterLaunchableCategoryOther,
			Launch:   p.launchOtherCore(filepath.Join("_Other", "Donut")),
			Test:     testOtherCore("Donut"),
		},
		launchables.VirtualSystem{
			ID:       launchables.MisterOtherEpochGalaxyII,
			Name:     "Epoch Galaxy II",
			Category: misterLaunchableCategoryOther,
			Launch:   p.launchOtherCore(filepath.Join("_Other", "EpochGalaxyII")),
			Test:     testOtherCore("EpochGalaxyII"),
		},
		launchables.VirtualSystem{
			ID:       launchables.MisterOtherFlappyBird,
			Name:     "Flappy Bird",
			Category: misterLaunchableCategoryOther,
			Launch:   p.launchOtherCore(filepath.Join("_Other", "FlappyBird")),
			Test:     testOtherCore("FlappyBird"),
		},
		launchables.VirtualSystem{
			ID:       launchables.MisterOtherGameOfLife,
			Name:     "Game of Life",
			Category: misterLaunchableCategoryOther,
			Launch:   p.launchOtherCore(filepath.Join("_Other", "GameOfLife")),
			Test:     testOtherCore("GameOfLife"),
		},
		launchables.VirtualSystem{
			ID:       launchables.MisterOtherGBMidi,
			Name:     "GBMidi",
			Category: misterLaunchableCategoryOther,
			Launch:   p.launchOtherCore(filepath.Join("_Other", "GBMidi")),
			Test:     testOtherCore("GBMidi"),
		},
		launchables.VirtualSystem{
			ID:       launchables.MisterOtherGenMidi,
			Name:     "GenMidi",
			Category: misterLaunchableCategoryOther,
			Launch:   p.launchOtherCore(filepath.Join("_Other", "GenMidi")),
			Test:     testOtherCore("GenMidi"),
		},
		launchables.VirtualSystem{
			ID:       launchables.MisterOtherSlugCross,
			Name:     "Slug Cross",
			Category: misterLaunchableCategoryOther,
			Launch:   p.launchOtherCore(filepath.Join("_Other", "SlugCross")),
			Test:     testOtherCore("SlugCross"),
		},
		launchables.VirtualSystem{
			ID:       launchables.MisterOtherTomyScramble,
			Name:     "Tomy Scramble",
			Category: misterLaunchableCategoryOther,
			Launch:   p.launchOtherCore(filepath.Join("_Other", "TomyScramble")),
			Test:     testOtherCore("TomyScramble"),
		},
		// 3S-ARM is a native ARM port of Street Fighter III: 3rd Strike that
		// ships as an _Other core but is a real arcade game, so it is exposed
		// as virtual media under the Arcade system rather than an Other entry.
		launchables.VirtualMedia{
			ID:       launchables.MisterArcadeThirdStrike,
			Name:     "Street Fighter III: 3rd Strike (3S-ARM)",
			SystemID: systemdefs.SystemArcade,
			Launch:   p.launchOtherCore(filepath.Join("_Other", "3S-ARM")),
			Test:     testOtherCore("3S-ARM"),
		},
	}

	for _, def := range misterCoreLaunchableDefinitions {
		items = append(items, launchables.VirtualSystem{
			ID:       def.ID,
			Name:     def.Name,
			Category: def.Category,
			Launch:   p.launchCore(def.CorePath),
			Test:     testCore(def.CorePath),
		})
	}

	return items
}

func testOtherCore(shortName string) func(*config.Instance) bool {
	return testCore(filepath.Join("_Other", shortName))
}

func testCore(corePath string) func(*config.Instance) bool {
	return func(*config.Instance) bool {
		return coreExists(misterconfig.SDRootDir, corePath)
	}
}

func otherCoreExists(rootDir, shortName string) bool {
	return coreExists(rootDir, filepath.Join("_Other", shortName))
}

func coreExists(rootDir, corePath string) bool {
	matches, err := filepath.Glob(filepath.Join(rootDir, corePath+"*.rbf"))
	return err == nil && len(matches) > 0
}

func (p *Platform) closeLaunchConsole() error {
	if p.closeConsole != nil {
		if err := p.closeConsole(); err != nil {
			return fmt.Errorf("close MiSTer console: %w", err)
		}
		return nil
	}
	if err := p.ConsoleManager().Close(); err != nil {
		return fmt.Errorf("close MiSTer console: %w", err)
	}
	return nil
}

func (p *Platform) launchShortCoreFile(corePath string) error {
	if p.launchShortCore != nil {
		if err := p.launchShortCore(corePath); err != nil {
			return fmt.Errorf("launch MiSTer short core: %w", err)
		}
		return nil
	}
	if err := mgls.LaunchShortCore(corePath); err != nil {
		return fmt.Errorf("launch MiSTer short core: %w", err)
	}
	return nil
}

func (p *Platform) launchOtherCore(
	corePath string,
) func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
	return p.launchCore(corePath)
}

func (p *Platform) launchCore(
	corePath string,
) func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
	return func(_ *config.Instance, _ string, _ *platforms.LaunchOptions) (*os.Process, error) {
		if err := p.closeLaunchConsole(); err != nil {
			log.Warn().Err(err).Msg("failed to close console before FPGA launch")
		}
		if err := p.launchShortCoreFile(corePath); err != nil {
			return nil, fmt.Errorf("failed to launch MiSTer core %s: %w", corePath, err)
		}
		return nil, nil //nolint:nilnil // MiSTer launches don't return a process handle
	}
}
