//go:build linux

package mister

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/launchables"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mgls"
	"github.com/rs/zerolog/log"
)

const misterLaunchableCategoryOther = "Other"

// Launchables exposes launch-only MiSTer _Other entries that do not already
// have media or alternate-core launchers.
func (p *Platform) Launchables(*config.Instance) []launchables.Launchable {
	return []launchables.Launchable{
		launchables.VirtualSystem{
			ID:       launchables.MisterOtherChess,
			Name:     "Chess",
			Category: misterLaunchableCategoryOther,
			Launch:   p.launchOtherCore("_Other/Chess"),
			Test:     testOtherCore("Chess"),
		},
		launchables.VirtualSystem{
			ID:       launchables.MisterOtherDonut,
			Name:     "Donut",
			Category: misterLaunchableCategoryOther,
			Launch:   p.launchOtherCore("_Other/Donut"),
			Test:     testOtherCore("Donut"),
		},
		launchables.VirtualSystem{
			ID:       launchables.MisterOtherEpochGalaxyII,
			Name:     "Epoch Galaxy II",
			Category: misterLaunchableCategoryOther,
			Launch:   p.launchOtherCore("_Other/EpochGalaxyII"),
			Test:     testOtherCore("EpochGalaxyII"),
		},
		launchables.VirtualSystem{
			ID:       launchables.MisterOtherFlappyBird,
			Name:     "Flappy Bird",
			Category: misterLaunchableCategoryOther,
			Launch:   p.launchOtherCore("_Other/FlappyBird"),
			Test:     testOtherCore("FlappyBird"),
		},
		launchables.VirtualSystem{
			ID:       launchables.MisterOtherGameOfLife,
			Name:     "Game of Life",
			Category: misterLaunchableCategoryOther,
			Launch:   p.launchOtherCore("_Other/GameOfLife"),
			Test:     testOtherCore("GameOfLife"),
		},
		launchables.VirtualSystem{
			ID:       launchables.MisterOtherGBMidi,
			Name:     "GBMidi",
			Category: misterLaunchableCategoryOther,
			Launch:   p.launchOtherCore("_Other/GBMidi"),
			Test:     testOtherCore("GBMidi"),
		},
		launchables.VirtualSystem{
			ID:       launchables.MisterOtherGenMidi,
			Name:     "GenMidi",
			Category: misterLaunchableCategoryOther,
			Launch:   p.launchOtherCore("_Other/GenMidi"),
			Test:     testOtherCore("GenMidi"),
		},
		launchables.VirtualSystem{
			ID:       launchables.MisterOtherSlugCross,
			Name:     "Slug Cross",
			Category: misterLaunchableCategoryOther,
			Launch:   p.launchOtherCore("_Other/SlugCross"),
			Test:     testOtherCore("SlugCross"),
		},
		launchables.VirtualSystem{
			ID:       launchables.MisterOtherTomyScramble,
			Name:     "Tomy Scramble",
			Category: misterLaunchableCategoryOther,
			Launch:   p.launchOtherCore("_Other/TomyScramble"),
			Test:     testOtherCore("TomyScramble"),
		},
	}
}

func testOtherCore(shortName string) func(*config.Instance) bool {
	return func(*config.Instance) bool {
		return otherCoreExists(misterconfig.SDRootDir, shortName)
	}
}

func otherCoreExists(rootDir, shortName string) bool {
	matches, err := filepath.Glob(filepath.Join(rootDir, "_Other", shortName+"*.rbf"))
	return err == nil && len(matches) > 0
}

func (p *Platform) launchOtherCore(
	corePath string,
) func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
	return func(_ *config.Instance, _ string, _ *platforms.LaunchOptions) (*os.Process, error) {
		if err := p.ConsoleManager().Close(); err != nil {
			log.Warn().Err(err).Msg("failed to close console before FPGA launch")
		}
		if err := mgls.LaunchShortCore(corePath); err != nil {
			return nil, fmt.Errorf("failed to launch MiSTer core %s: %w", corePath, err)
		}
		return nil, nil //nolint:nilnil // MiSTer launches don't return a process handle
	}
}
