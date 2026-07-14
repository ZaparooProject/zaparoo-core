//go:build linux

package mister

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	Name     string
	Category string
	CorePath string
	ID       uuid.UUID
}

var misterCoreLaunchableDefinitions = []misterCoreLaunchableDefinition{
	misterConsoleCore(launchables.MisterConsoleAY38500, "AY-3-8500", "AY-3-8500"),
	misterConsoleCore(launchables.MisterConsoleBBCBridgeCompanion, "BBC Bridge Companion", "BBCBridgeCompanion"),
	misterConsoleCore(launchables.MisterConsoleMyVision, "My Vision", "MyVision"),
	misterConsoleCore(launchables.MisterConsoleSuperVision8000, "Super Vision 8000", "Super_Vision_8000"),
	misterComputerCore(launchables.MisterComputerAltair8800, "Altair 8800", "Altair8800"),
	misterComputerCore(launchables.MisterComputerArchie, "Archie", "Archie"),
	misterComputerCore(launchables.MisterComputerAtariST, "Atari ST", "AtariST"),
	misterComputerCore(launchables.MisterComputerC128, "Commodore 128", "C128"),
	misterComputerCore(launchables.MisterComputerCoCo3, "CoCo 3", "CoCo3"),
	misterComputerCore(launchables.MisterComputerColecoAdam, "Coleco Adam", "ColecoAdam"),
	misterComputerCore(launchables.MisterComputerEG2000, "EG2000 Colour Genie", "eg2000"),
	misterComputerCore(launchables.MisterComputerEnterprise, "Enterprise", "Enterprise"),
	misterComputerCore(launchables.MisterComputerHomelab, "Homelab", "Homelab"),
	misterComputerCore(launchables.MisterComputerIQ151, "IQ-151", "IQ151"),
	misterComputerCore(launchables.MisterComputerMacLC, "Mac LC", "MacLC"),
	misterComputerCore(launchables.MisterComputerOndraSPO186, "Ondra SPO186", "Ondra_SPO186"),
	misterComputerCore(launchables.MisterComputerPC88, "PC-88", "PC88"),
	misterComputerCore(launchables.MisterComputerPCjr, "PCjr", "PCjr"),
	misterComputerCore(launchables.MisterComputerSharpMZ, "Sharp MZ", "SharpMZ"),
	misterComputerCore(launchables.MisterComputerTK2000, "TK2000", "TK2000"),
	misterComputerCore(launchables.MisterComputerTandy1000, "Tandy 1000", "Tandy1000"),
	misterComputerCore(launchables.MisterComputerVT52, "VT52", "VT52"),
}

func misterConsoleCore(id uuid.UUID, name, coreName string) misterCoreLaunchableDefinition {
	return misterCoreLaunchableDefinition{
		ID:       id,
		Name:     name,
		Category: misterLaunchableCategoryConsole,
		CorePath: filepath.Join("_Console", coreName),
	}
}

func misterComputerCore(id uuid.UUID, name, coreName string) misterCoreLaunchableDefinition {
	return misterCoreLaunchableDefinition{
		ID:       id,
		Name:     name,
		Category: misterLaunchableCategoryComputer,
		CorePath: filepath.Join("_Computer", coreName),
	}
}

type misterOtherLaunchableDefinition struct {
	ConfigID string
	Name     string
	Category string
	LoadPath string
	ID       uuid.UUID
}

var misterOtherLaunchableDefinitions = []misterOtherLaunchableDefinition{
	{
		ConfigID: "MisterOtherChess", ID: launchables.MisterOtherChess,
		Name: "Chess", Category: misterLaunchableCategoryOther, LoadPath: filepath.Join("_Other", "Chess"),
	},
	{
		ConfigID: "MisterOtherDonut", ID: launchables.MisterOtherDonut,
		Name: "Donut", Category: misterLaunchableCategoryOther, LoadPath: filepath.Join("_Other", "Donut"),
	},
	{
		ConfigID: "MisterOtherEpochGalaxyII", ID: launchables.MisterOtherEpochGalaxyII,
		Name: "Epoch Galaxy II", Category: misterLaunchableCategoryOther,
		LoadPath: filepath.Join("_Other", "EpochGalaxyII"),
	},
	{
		ConfigID: "MisterOtherFlappyBird", ID: launchables.MisterOtherFlappyBird,
		Name: "Flappy Bird", Category: misterLaunchableCategoryOther,
		LoadPath: filepath.Join("_Other", "FlappyBird"),
	},
	{
		ConfigID: "MisterOtherGameOfLife", ID: launchables.MisterOtherGameOfLife,
		Name: "Game of Life", Category: misterLaunchableCategoryOther,
		LoadPath: filepath.Join("_Other", "GameOfLife"),
	},
	{
		ConfigID: "MisterOtherGBMidi", ID: launchables.MisterOtherGBMidi,
		Name: "GBMidi", Category: misterLaunchableCategoryOther, LoadPath: filepath.Join("_Other", "GBMidi"),
	},
	{
		ConfigID: "MisterOtherGenMidi", ID: launchables.MisterOtherGenMidi,
		Name: "GenMidi", Category: misterLaunchableCategoryOther, LoadPath: filepath.Join("_Other", "GenMidi"),
	},
	{
		ConfigID: "MisterOtherSlugCross", ID: launchables.MisterOtherSlugCross,
		Name: "Slug Cross", Category: misterLaunchableCategoryOther,
		LoadPath: filepath.Join("_Other", "SlugCross"),
	},
	{
		ConfigID: "MisterOtherTomyScramble", ID: launchables.MisterOtherTomyScramble,
		Name: "Tomy Scramble", Category: misterLaunchableCategoryOther,
		LoadPath: filepath.Join("_Other", "TomyScramble"),
	},
}

// mergeOtherLaunchableDefinitions overlays custom virtual systems onto built-in
// _Other definitions while preserving fixed UUIDs for matching built-ins.
func mergeOtherLaunchableDefinitions(
	builtins []misterOtherLaunchableDefinition,
	userEntries []config.LaunchersCustom,
) []misterOtherLaunchableDefinition {
	merged := make([]misterOtherLaunchableDefinition, 0, len(builtins)+len(userEntries))
	merged = append(merged, builtins...)

	index := make(map[string]int, len(merged))
	for i, def := range merged {
		index[strings.ToLower(def.ConfigID)] = i
	}

	for i := range userEntries {
		entry := &userEntries[i]
		id := strings.ToLower(entry.ID)
		if i, ok := index[id]; ok {
			merged[i].Name = entry.Name
			merged[i].Category = entry.Category
			merged[i].LoadPath = filepath.FromSlash(entry.LoadPath)
			continue
		}
		merged = append(merged, misterOtherLaunchableDefinition{
			ConfigID: entry.ID,
			Name:     entry.Name,
			Category: entry.Category,
			LoadPath: filepath.FromSlash(entry.LoadPath),
			ID: uuid.NewSHA1(
				launchables.ZaparooLaunchableNamespace,
				[]byte(config.CustomLauncherBackendMisterCore+":"+id),
			),
		})
		index[id] = len(merged) - 1
	}

	return merged
}

// Launchables exposes launch-only MiSTer core entries that do not already have
// media launchers.
func (p *Platform) Launchables(cfg *config.Instance) []launchables.Launchable {
	customVirtualSystems := make([]config.LaunchersCustom, 0)
	customLaunchers := cfg.CustomLaunchers()
	for i := range customLaunchers {
		entry := &customLaunchers[i]
		if entry.Backend == config.CustomLauncherBackendMisterCore &&
			entry.Kind == config.CustomLauncherKindVirtualSystem {
			customVirtualSystems = append(customVirtualSystems, *entry)
		}
	}
	otherDefs := mergeOtherLaunchableDefinitions(misterOtherLaunchableDefinitions, customVirtualSystems)

	items := make([]launchables.Launchable, 0, len(otherDefs)+1+len(misterCoreLaunchableDefinitions))
	for _, def := range otherDefs {
		items = append(items, launchables.VirtualSystem{
			ID:       def.ID,
			Name:     def.Name,
			Category: def.Category,
			Launch:   p.launchCore(def.LoadPath),
			Test:     testCore(def.LoadPath),
		})
	}

	// 3S-ARM is a native ARM port of Street Fighter III: 3rd Strike that
	// ships as an _Other core but is a real arcade game, so it is exposed
	// as virtual media under the Arcade system rather than an Other entry.
	items = append(items, launchables.VirtualMedia{
		ID:       launchables.MisterArcadeThirdStrike,
		Name:     "Street Fighter III: 3rd Strike (3S-ARM)",
		SystemID: systemdefs.SystemArcade,
		Launch:   p.launchOtherCore(filepath.Join("_Other", "3S-ARM")),
		Test:     testOtherCore("3S-ARM"),
	})

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
	matches, err := filepath.Glob(filepath.Join(rootDir, corePath+"*"))
	if err != nil {
		return false
	}
	for _, match := range matches {
		if filepath.Ext(strings.ToLower(match)) == ".rbf" {
			return true
		}
	}
	return false
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
