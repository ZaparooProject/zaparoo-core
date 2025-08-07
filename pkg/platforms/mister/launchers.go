//go:build linux

package mister

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/rs/zerolog/log"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister/mrext/games"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/mister/mrext/mister"
)

func checkInZip(path string) string {
	if !strings.HasSuffix(strings.ToLower(path), ".zip") {
		return path
	}

	fileInfo, err := os.Stat(path)
	if err != nil || fileInfo.IsDir() {
		log.Error().Err(err).Msgf("failed to access the zip file at path: %s", path)
		return path
	}

	zipReader, err := zip.OpenReader(path)
	if err != nil {
		log.Error().Err(err).Msgf("failed to open zip file: %s", path)
		return path
	}
	defer func(zipReader *zip.ReadCloser) {
		err := zipReader.Close()
		if err != nil {
			log.Error().Err(err).Msgf("failed to close zip file: %s", path)
		}
	}(zipReader)

	var firstFilePath string
	matchingFilePath := ""
	zipName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

	for _, file := range zipReader.File {
		if file.FileInfo().IsDir() {
			continue
		}

		if firstFilePath == "" {
			firstFilePath = file.Name
		}

		if strings.EqualFold(strings.TrimSuffix(filepath.Base(file.Name), filepath.Ext(file.Name)), zipName) {
			matchingFilePath = file.Name
			break
		}
	}

	if matchingFilePath != "" {
		log.Debug().Msgf("found matching file: %s", matchingFilePath)
		return filepath.Join(path, matchingFilePath)
	} else if firstFilePath != "" && len(zipReader.File) == 1 {
		log.Debug().Msgf("found single file in zip archive: %s", firstFilePath)
		return filepath.Join(path, firstFilePath)
	}

	log.Warn().Msgf("no suitable file found in zip archive: %s", path)
	return path
}

func launch(systemID string) func(*config.Instance, string) error {
	return func(cfg *config.Instance, path string) error {
		if filepath.Ext(strings.ToLower(path)) == ".mgl" {
			err := mister.LaunchGenericFile(UserConfigToMrext(cfg), path)
			if err != nil {
				log.Error().Err(err).Msg("error launching mgl")
				return fmt.Errorf("failed to launch generic file: %w", err)
			}
			err = mister.SetActiveGame(path)
			if err != nil {
				return fmt.Errorf("failed to set active game: %w", err)
			}
			return nil
		}

		s, err := games.GetSystem(systemID)
		if err != nil {
			return fmt.Errorf("failed to get system %s: %w", systemID, err)
		}

		path = checkInZip(path)

		err = mister.LaunchGame(UserConfigToMrext(cfg), *s, path)
		if err != nil {
			return fmt.Errorf("failed to launch game: %w", err)
		}

		log.Debug().Msgf("setting active game: %s", path)
		err = mister.SetActiveGame(path)
		if err != nil {
			return fmt.Errorf("failed to set active game: %w", err)
		}
		return nil
	}
}

func launchSinden(
	systemID string,
	rbfName string,
) func(*config.Instance, string) error {
	return func(cfg *config.Instance, path string) error {
		s, err := games.GetSystem(systemID)
		if err != nil {
			return fmt.Errorf("failed to get system %s: %w", systemID, err)
		}
		path = checkInZip(path)

		sn := *s

		newRBF := "Light Gun/" + rbfName + "-Sinden"
		oldRBF := "_Sinden/" + rbfName + "_Sinden"

		newMatches, err := filepath.Glob(filepath.Join(SDRootDir, newRBF) + "*")
		if err != nil {
			log.Debug().Err(err).Msg("error checking for new Sinden RBF")
		}
		if len(newMatches) > 0 {
			sn.Rbf = newRBF
		} else {
			// just fallback on trying the old path
			sn.Rbf = oldRBF
		}

		sn.SetName = rbfName + "_Sinden"
		sn.SetNameSameDir = true

		log.Debug().Str("rbf", sn.Rbf).Msgf("launching Sinden: %v", sn)

		err = mister.LaunchGame(UserConfigToMrext(cfg), sn, path)
		if err != nil {
			return fmt.Errorf("failed to launch game: %w", err)
		}

		err = mister.SetActiveGame(path)
		if err != nil {
			return fmt.Errorf("failed to set active game: %w", err)
		}
		return nil
	}
}

func launchAggGnw(cfg *config.Instance, path string) error {
	s, err := games.GetSystem("GameNWatch")
	if err != nil {
		return fmt.Errorf("failed to get GameNWatch system: %w", err)
	}
	path = checkInZip(path)

	sn := *s
	sn.Rbf = "_Console/GameAndWatch"
	sn.Folder = []string{"Game and Watch"}
	sn.Slots = []games.Slot{
		{
			Exts: []string{".gnw"},
			Mgl: &games.MglParams{
				Delay:  1,
				Method: "f",
				Index:  1,
			},
		},
	}

	err = mister.LaunchGame(UserConfigToMrext(cfg), sn, path)
	if err != nil {
		return fmt.Errorf("failed to launch game: %w", err)
	}

	err = mister.SetActiveGame(path)
	if err != nil {
		return fmt.Errorf("failed to set active game: %w", err)
	}
	return nil
}

func launchAltCore(
	systemID string,
	rbfPath string,
) func(*config.Instance, string) error {
	return func(cfg *config.Instance, path string) error {
		s, err := games.GetSystem(systemID)
		if err != nil {
			return fmt.Errorf("failed to get system %s: %w", systemID, err)
		}
		path = checkInZip(path)

		sn := *s
		sn.Rbf = rbfPath

		log.Debug().Str("rbf", sn.Rbf).Msgf("launching alt core: %v", sn)

		err = mister.LaunchGame(UserConfigToMrext(cfg), sn, path)
		if err != nil {
			return fmt.Errorf("failed to launch game: %w", err)
		}

		err = mister.SetActiveGame(path)
		if err != nil {
			return fmt.Errorf("failed to set active game: %w", err)
		}
		return nil
	}
}

//nolint:unused // keeping as reference for future implementation
func launchGroovyCore() func(*config.Instance, string) error {
	// Merge into mrext?
	return func(cfg *config.Instance, path string) error {
		sn := games.System{
			Id:           "Groovy",
			Name:         "Groovy",
			Category:     games.CategoryOther,
			Manufacturer: "Sergi Clara",
			ReleaseDate:  "2024-03-02",
			Alias:        []string{"Groovy"},
			Folder:       []string{"Groovy"},
			Rbf:          "_Utility/Groovy",
			Slots: []games.Slot{
				{
					Label: "GMC",
					Exts:  []string{".gmc"},
					Mgl: &games.MglParams{
						Delay:  2,
						Method: "f",
						Index:  1,
					},
				},
			},
		}

		log.Debug().Msgf("launching Groovy core: %v", sn)

		err := mister.LaunchGame(UserConfigToMrext(cfg), sn, path)
		if err != nil {
			return fmt.Errorf("failed to launch game: %w", err)
		}

		err = mister.SetActiveGame(path)
		if err != nil {
			return fmt.Errorf("failed to set active game: %w", err)
		}
		return nil
	}
}

func launchDOS() func(*config.Instance, string) error {
	return func(cfg *config.Instance, path string) error {
		if filepath.Ext(strings.ToLower(path)) == ".mgl" {
			err := mister.LaunchGenericFile(UserConfigToMrext(cfg), path)
			if err != nil {
				log.Error().Err(err).Msg("error launching mgl")
				return fmt.Errorf("failed to launch generic file: %w", err)
			}
			err = mister.SetActiveGame(path)
			if err != nil {
				return fmt.Errorf("failed to set active game: %w", err)
			}
			return nil
		}

		s, err := games.GetSystem("ao486")
		if err != nil {
			return fmt.Errorf("failed to get ao486 system: %w", err)
		}

		path = checkInZip(path)

		err = mister.LaunchGame(UserConfigToMrext(cfg), *s, path)
		if err != nil {
			return fmt.Errorf("failed to launch game: %w", err)
		}

		log.Debug().Msgf("setting active game: %s", path)
		err = mister.SetActiveGame(path)
		if err != nil {
			return fmt.Errorf("failed to set active game: %w", err)
		}
		return nil
	}
}

func launchAtari2600() func(*config.Instance, string) error {
	return func(cfg *config.Instance, path string) error {
		s, err := games.GetSystem("Atari2600")
		if err != nil {
			return fmt.Errorf("failed to get Atari2600 system: %w", err)
		}
		path = checkInZip(path)

		sn := *s
		sn.Slots = []games.Slot{
			{
				Exts: []string{".a26", ".bin"},
				Mgl: &games.MglParams{
					Delay:  1,
					Method: "f",
					Index:  1,
				},
			},
		}

		err = mister.LaunchGame(UserConfigToMrext(cfg), sn, path)
		if err != nil {
			return fmt.Errorf("failed to launch game: %w", err)
		}

		err = mister.SetActiveGame(path)
		if err != nil {
			return fmt.Errorf("failed to set active game: %w", err)
		}
		return nil
	}
}

func launchMPlayer(pl *Platform) func(*config.Instance, string) error {
	return func(_ *config.Instance, path string) error {
		if path == "" {
			return errors.New("no path specified")
		}

		vt := "4"

		err := cleanConsole(vt)
		if err != nil {
			return err
		}

		err = openConsole(pl, vt)
		if err != nil {
			return err
		}

		time.Sleep(500 * time.Millisecond)
		err = mister.SetVideoMode(640, 480)
		if err != nil {
			return fmt.Errorf("error setting video mode: %w", err)
		}

		cmd := exec.CommandContext( //nolint:gosec // Path comes from internal launcher system, not user input
			context.Background(),
			"nice",
			"-n",
			"-20",
			filepath.Join(LinuxDir, "mplayer"),
			"-cache",
			"8192",
			path,
		)
		cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+LinuxDir)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		restore := func() {
			menuErr := mister.LaunchMenu()
			if menuErr != nil {
				log.Warn().Err(menuErr).Msg("error launching menu")
			}

			err = restoreConsole(vt)
			if err != nil {
				log.Warn().Err(err).Msg("error restoring console")
			}
		}

		err = cmd.Start()
		if err != nil {
			restore()
			return fmt.Errorf("failed to start command: %w", err)
		}

		err = cmd.Wait()
		if err != nil {
			restore()
			return fmt.Errorf("failed to wait for command: %w", err)
		}

		restore()
		return nil
	}
}

func killMPlayer(_ *config.Instance) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	psCmd := exec.CommandContext(ctx, "sh", "-c", "ps aux | grep mplayer | grep -v grep")
	output, err := psCmd.Output()
	if err != nil {
		log.Info().Msgf("mplayer processes not detected.")
		return fmt.Errorf("failed to get process output: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}

		log.Debug().Msgf("processing line: %s", line)

		fields := strings.Fields(line)
		if len(fields) < 2 {
			log.Warn().Msgf("unexpected line format: %s", line)
			continue
		}

		pid := fields[0]
		log.Info().Msgf("killing mplayer process with PID: %s", pid)

		killCtx, killCancel := context.WithTimeout(context.Background(), 10*time.Second)
		//nolint:gosec // PID from system ps command, not user input
		killCmd := exec.CommandContext(killCtx, "kill", "-9", pid)
		if err := killCmd.Run(); err != nil {
			log.Error().Msgf("failed to kill process %s: %v", pid, err)
		}
		killCancel()
	}

	return nil
}

var Launchers = []platforms.Launcher{
	// Consoles
	{
		ID:         systemdefs.SystemAdventureVision,
		SystemID:   systemdefs.SystemAdventureVision,
		Folders:    []string{"AVision"},
		Extensions: []string{".bin"},
		Launch:     launch(systemdefs.SystemAdventureVision),
	},
	{
		ID:         systemdefs.SystemArcadia,
		SystemID:   systemdefs.SystemArcadia,
		Folders:    []string{"Arcadia"},
		Extensions: []string{".bin"},
		Launch:     launch(systemdefs.SystemArcadia),
	},
	{
		ID:         systemdefs.SystemAmigaCD32,
		SystemID:   systemdefs.SystemAmigaCD32,
		Folders:    []string{"AmigaCD32"},
		Extensions: []string{".cue", ".chd"},
		Launch:     launch(systemdefs.SystemAmigaCD32),
	},
	{
		ID:         systemdefs.SystemAstrocade,
		SystemID:   systemdefs.SystemAstrocade,
		Folders:    []string{"Astrocade"},
		Extensions: []string{".bin"},
		Launch:     launch(systemdefs.SystemAstrocade),
	},
	{
		ID:         systemdefs.SystemAtari2600,
		SystemID:   systemdefs.SystemAtari2600,
		Folders:    []string{"ATARI7800", "Atari2600"},
		Extensions: []string{".a26"},
		Launch:     launchAtari2600(),
		Test: func(_ *config.Instance, path string) bool {
			lowerPath := strings.ToLower(path)
			// TODO: really, this should specifically check on the root dirs,
			// 		 but we'd need to modify the test function to have access
			//       to the platform interface. it's probably a safe enough bet
			//       that something in an atari2600 subdir is for atari2600
			if (strings.Contains(lowerPath, "/atari2600/") ||
				strings.Contains(lowerPath, "/atari 2600/")) &&
				filepath.Ext(lowerPath) == ".bin" {
				return true
			}
			return false
		},
	},
	{
		ID:       "LLAPIAtari2600",
		SystemID: systemdefs.SystemAtari2600,
		Launch:   launchAltCore(systemdefs.SystemAtari2600, "_LLAPI/Atari7800_LLAPI"),
	},
	{
		ID:         systemdefs.SystemAtari5200,
		SystemID:   systemdefs.SystemAtari5200,
		Folders:    []string{"ATARI5200"},
		Extensions: []string{".a52"},
		Launch:     launch(systemdefs.SystemAtari5200),
	},
	{
		ID:         systemdefs.SystemAtari7800,
		SystemID:   systemdefs.SystemAtari7800,
		Folders:    []string{"ATARI7800"},
		Extensions: []string{".a78"},
		Launch:     launch(systemdefs.SystemAtari7800),
	},
	{
		ID:       "LLAPIAtari7800",
		SystemID: systemdefs.SystemAtari7800,
		Launch:   launchAltCore(systemdefs.SystemAtari7800, "_LLAPI/Atari7800_LLAPI"),
	},
	{
		ID:         systemdefs.SystemAtariLynx,
		SystemID:   systemdefs.SystemAtariLynx,
		Folders:    []string{"AtariLynx"},
		Extensions: []string{".lnx"},
		Launch:     launch(systemdefs.SystemAtariLynx),
	},
	{
		ID:         systemdefs.SystemCasioPV1000,
		SystemID:   systemdefs.SystemCasioPV1000,
		Folders:    []string{"Casio_PV-1000"},
		Extensions: []string{".bin"},
		Launch:     launch(systemdefs.SystemCasioPV1000),
	},
	{
		ID:         systemdefs.SystemCDI,
		SystemID:   systemdefs.SystemCDI,
		Folders:    []string{"CD-i"},
		Extensions: []string{".cue", ".chd"},
		Launch:     launch(systemdefs.SystemCDI),
	},
	{
		ID:         systemdefs.SystemChannelF,
		SystemID:   systemdefs.SystemChannelF,
		Folders:    []string{"ChannelF"},
		Extensions: []string{".rom", ".bin"},
		Launch:     launch(systemdefs.SystemChannelF),
	},
	{
		ID:         systemdefs.SystemColecoVision,
		SystemID:   systemdefs.SystemColecoVision,
		Folders:    []string{"Coleco"},
		Extensions: []string{".col", ".bin", ".rom"},
		Launch:     launch(systemdefs.SystemColecoVision),
	},
	{
		ID:         systemdefs.SystemCreatiVision,
		SystemID:   systemdefs.SystemCreatiVision,
		Folders:    []string{"CreatiVision"},
		Extensions: []string{".rom", ".bin", ".bas"},
		Launch:     launch(systemdefs.SystemCreatiVision),
	},
	{
		ID:         systemdefs.SystemFDS,
		SystemID:   systemdefs.SystemFDS,
		Folders:    []string{"NES", "FDS"},
		Extensions: []string{".fds"},
		Launch:     launch(systemdefs.SystemFDS),
	},
	{
		ID:         systemdefs.SystemGamate,
		SystemID:   systemdefs.SystemGamate,
		Folders:    []string{"Gamate"},
		Extensions: []string{".bin"},
		Launch:     launch(systemdefs.SystemGamate),
	},
	{
		ID:         systemdefs.SystemGameboy,
		SystemID:   systemdefs.SystemGameboy,
		Folders:    []string{"GAMEBOY"},
		Extensions: []string{".gb"},
		Launch:     launch(systemdefs.SystemGameboy),
	},
	{
		ID:       "LLAPIGameboy",
		SystemID: systemdefs.SystemGameboy,
		Launch:   launchAltCore(systemdefs.SystemGameboy, "_LLAPI/Gameboy_LLAPI"),
	},
	{
		ID:         systemdefs.SystemGameboyColor,
		SystemID:   systemdefs.SystemGameboyColor,
		Folders:    []string{"GAMEBOY", "GBC"},
		Extensions: []string{".gbc"},
		Launch:     launch(systemdefs.SystemGameboyColor),
	},
	{
		ID:         systemdefs.SystemGameboy2P,
		SystemID:   systemdefs.SystemGameboy2P,
		Folders:    []string{"GAMEBOY2P"},
		Extensions: []string{".gb", ".gbc"},
		Launch:     launch(systemdefs.SystemGameboy2P),
	},
	{
		ID:         systemdefs.SystemGameGear,
		SystemID:   systemdefs.SystemGameGear,
		Folders:    []string{"SMS", "GameGear"},
		Extensions: []string{".gg"},
		Launch:     launch(systemdefs.SystemGameGear),
	},
	{
		ID:         systemdefs.SystemGameNWatch,
		SystemID:   systemdefs.SystemGameNWatch,
		Folders:    []string{"GameNWatch"},
		Extensions: []string{".bin"},
		Launch:     launch(systemdefs.SystemGameNWatch),
	},
	{
		ID:         "GameAndWatch",
		SystemID:   systemdefs.SystemGameNWatch,
		Folders:    []string{"Game and Watch"},
		Extensions: []string{".gnw"},
		Launch:     launchAggGnw,
	},
	{
		ID:         systemdefs.SystemGBA,
		SystemID:   systemdefs.SystemGBA,
		Folders:    []string{"GBA"},
		Extensions: []string{".gba"},
		Launch:     launch(systemdefs.SystemGBA),
	},
	{
		ID:       "LLAPIGBA",
		SystemID: systemdefs.SystemGBA,
		Launch:   launchAltCore(systemdefs.SystemGBA, "_LLAPI/GBA_LLAPI"),
	},
	{
		ID:         systemdefs.SystemGBA2P,
		SystemID:   systemdefs.SystemGBA2P,
		Folders:    []string{"GBA2P"},
		Extensions: []string{".gba"},
		Launch:     launch(systemdefs.SystemGBA2P),
	},
	{
		ID:         systemdefs.SystemGenesis,
		SystemID:   systemdefs.SystemGenesis,
		Folders:    []string{"MegaDrive", "Genesis"},
		Extensions: []string{".gen", ".bin", ".md"},
		Launch:     launch(systemdefs.SystemGenesis),
	},
	{
		ID:       "SindenGenesis",
		SystemID: systemdefs.SystemGenesis,
		Launch:   launchSinden(systemdefs.SystemGenesis, "Genesis"),
	},
	{
		ID:       "SindenMegaDrive",
		SystemID: systemdefs.SystemGenesis,
		Launch:   launchSinden(systemdefs.SystemGenesis, "MegaDrive"),
	},
	{
		ID:       "LLAPIMegaDrive",
		SystemID: systemdefs.SystemGenesis,
		Launch:   launchAltCore(systemdefs.SystemGenesis, "_LLAPI/MegaDrive_LLAPI"),
	},
	{
		ID:         systemdefs.SystemIntellivision,
		SystemID:   systemdefs.SystemIntellivision,
		Folders:    []string{"Intellivision"},
		Extensions: []string{".int", ".bin"},
		Launch:     launch(systemdefs.SystemIntellivision),
	},
	{
		ID:         systemdefs.SystemJaguar,
		SystemID:   systemdefs.SystemJaguar,
		Folders:    []string{"Jaguar"},
		Extensions: []string{".jag", ".j64", ".rom", ".bin"},
		Launch:     launch(systemdefs.SystemJaguar),
	},
	{
		ID:         systemdefs.SystemMasterSystem,
		SystemID:   systemdefs.SystemMasterSystem,
		Folders:    []string{"SMS"},
		Extensions: []string{".sms"},
		Launch:     launch(systemdefs.SystemMasterSystem),
	},
	{
		ID:       "SindenSMS",
		SystemID: systemdefs.SystemMasterSystem,
		Launch:   launchSinden(systemdefs.SystemMasterSystem, "SMS"),
	},
	{
		ID:       "LLAPISMS",
		SystemID: systemdefs.SystemMasterSystem,
		Launch:   launchAltCore(systemdefs.SystemMasterSystem, "_LLAPI/SMS_LLAPI"),
	},
	{
		ID:         systemdefs.SystemMegaCD,
		SystemID:   systemdefs.SystemMegaCD,
		Folders:    []string{"MegaCD"},
		Extensions: []string{".cue", ".chd"},
		Launch:     launch(systemdefs.SystemMegaCD),
	},
	{
		ID:       "SindenMegaCD",
		SystemID: systemdefs.SystemMegaCD,
		Launch:   launchSinden(systemdefs.SystemMegaCD, "MegaCD"),
	},
	{
		ID:       "LLAPIMegaCD",
		SystemID: systemdefs.SystemMegaCD,
		Launch:   launchAltCore(systemdefs.SystemMegaCD, "_LLAPI/MegaCD_LLAPI"),
	},
	{
		ID:         systemdefs.SystemMegaDuck,
		SystemID:   systemdefs.SystemMegaDuck,
		Folders:    []string{"GAMEBOY", "MegaDuck"},
		Extensions: []string{".bin"},
		Launch:     launch(systemdefs.SystemMegaDuck),
	},
	{
		ID:       "LLAPINeoGeo",
		SystemID: systemdefs.SystemNeoGeo,
		Launch:   launchAltCore(systemdefs.SystemNeoGeo, "_LLAPI/NeoGeo_LLAPI"),
	},
	{
		ID:         systemdefs.SystemNeoGeoCD,
		SystemID:   systemdefs.SystemNeoGeoCD,
		Folders:    []string{"NeoGeo-CD", "NEOGEO"},
		Extensions: []string{".cue", ".chd"},
		Launch:     launch(systemdefs.SystemNeoGeoCD),
	},
	{
		ID:         systemdefs.SystemNES,
		SystemID:   systemdefs.SystemNES,
		Folders:    []string{"NES"},
		Extensions: []string{".nes"},
		Launch:     launch(systemdefs.SystemNES),
	},
	{
		ID:       "SindenNES",
		SystemID: systemdefs.SystemNES,
		Launch:   launchSinden(systemdefs.SystemNES, "NES"),
	},
	{
		ID:         systemdefs.SystemNESMusic,
		SystemID:   systemdefs.SystemNESMusic,
		Folders:    []string{"NES"},
		Extensions: []string{".nsf"},
		Launch:     launch(systemdefs.SystemNESMusic),
	},
	{
		ID:       "LLAPINES",
		SystemID: systemdefs.SystemNES,
		Launch:   launchAltCore(systemdefs.SystemNES, "_LLAPI/NES_LLAPI"),
	},
	{
		ID:         systemdefs.SystemNintendo64,
		SystemID:   systemdefs.SystemNintendo64,
		Folders:    []string{"N64"},
		Extensions: []string{".n64", ".z64"},
		Launch:     launch(systemdefs.SystemNintendo64),
	},
	{
		ID:       "LLAPINintendo64",
		SystemID: systemdefs.SystemNintendo64,
		Launch:   launchAltCore(systemdefs.SystemNintendo64, "_LLAPI/N64_LLAPI"),
	},
	{
		ID:       "LLAPI80MHzNintendo64",
		SystemID: systemdefs.SystemNintendo64,
		Launch:   launchAltCore(systemdefs.SystemNintendo64, "_LLAPI/N64_80MHz_LLAPI"),
	},
	{
		ID:       "80MHzNintendo64",
		SystemID: systemdefs.SystemNintendo64,
		Launch:   launchAltCore(systemdefs.SystemNintendo64, "_Console/N64_80MHz"),
	},
	{
		ID:       "PWMNintendo64",
		SystemID: systemdefs.SystemNintendo64,
		Launch:   launchAltCore(systemdefs.SystemNintendo64, "_ConsolePWM/N64_PWM"),
	},
	{
		ID:       "PWM80MHzNintendo64",
		SystemID: systemdefs.SystemNintendo64,
		Launch:   launchAltCore(systemdefs.SystemNintendo64, "_ConsolePWM/_Turbo/N64_80MHz_PWM"),
	},
	{
		ID:         systemdefs.SystemOdyssey2,
		SystemID:   systemdefs.SystemOdyssey2,
		Folders:    []string{"ODYSSEY2"},
		Extensions: []string{".bin"},
		Launch:     launch(systemdefs.SystemOdyssey2),
	},
	{
		ID:         systemdefs.SystemPocketChallengeV2,
		SystemID:   systemdefs.SystemPocketChallengeV2,
		Folders:    []string{"WonderSwan", "PocketChallengeV2"},
		Extensions: []string{".pc2"},
		Launch:     launch(systemdefs.SystemPocketChallengeV2),
	},
	{
		ID:         systemdefs.SystemPokemonMini,
		SystemID:   systemdefs.SystemPokemonMini,
		Folders:    []string{"PokemonMini"},
		Extensions: []string{".min"},
		Launch:     launch(systemdefs.SystemPokemonMini),
	},
	{
		ID:         systemdefs.SystemPSX,
		SystemID:   systemdefs.SystemPSX,
		Folders:    []string{"PSX"},
		Extensions: []string{".cue", ".chd", ".exe"},
		Launch:     launch(systemdefs.SystemPSX),
	},
	{
		ID:       "LLAPIPSX",
		SystemID: systemdefs.SystemPSX,
		Launch:   launchAltCore(systemdefs.SystemPSX, "_LLAPI/PSX_LLAPI"),
	},
	{
		ID:       "SindenPSX",
		SystemID: systemdefs.SystemPSX,
		Launch:   launchSinden(systemdefs.SystemPSX, "PSX"),
	},
	{
		ID:       "2XPSX",
		SystemID: systemdefs.SystemPSX,
		Launch:   launchAltCore(systemdefs.SystemPSX, "_Console/PSX2XCPU"),
	},
	{
		ID:       "PWMPSX",
		SystemID: systemdefs.SystemPSX,
		Launch:   launchAltCore(systemdefs.SystemPSX, "_ConsolePWM/PSX_PWM"),
	},
	{
		ID:       "PWM2XPSX",
		SystemID: systemdefs.SystemPSX,
		Launch:   launchAltCore(systemdefs.SystemPSX, "_ConsolePWM/_Turbo/PSX2XCPU_PWM"),
	},
	{
		ID:         systemdefs.SystemSega32X,
		SystemID:   systemdefs.SystemSega32X,
		Folders:    []string{"S32X"},
		Extensions: []string{".32x"},
		Launch:     launch(systemdefs.SystemSega32X),
	},
	{
		ID:       "LLAPIS32X",
		SystemID: systemdefs.SystemSega32X,
		Launch:   launchAltCore(systemdefs.SystemPSX, "_LLAPI/S32X_LLAPI"),
	},
	{
		ID:         systemdefs.SystemSG1000,
		SystemID:   systemdefs.SystemSG1000,
		Folders:    []string{"SG1000", "Coleco", "SMS"},
		Extensions: []string{".sg"},
		Launch:     launch(systemdefs.SystemSG1000),
	},
	{
		ID:         systemdefs.SystemSuperGameboy,
		SystemID:   systemdefs.SystemSuperGameboy,
		Folders:    []string{"SGB"},
		Extensions: []string{".sgb", ".gb", ".gbc"},
		Launch:     launch(systemdefs.SystemSuperGameboy),
	},
	{
		ID:       "LLAPISuperGameboy",
		SystemID: systemdefs.SystemSuperGameboy,
		Launch:   launchAltCore(systemdefs.SystemSuperGameboy, "_LLAPI/SGB_LLAPI"),
	},
	{
		ID:         systemdefs.SystemSuperVision,
		SystemID:   systemdefs.SystemSuperVision,
		Folders:    []string{"SuperVision"},
		Extensions: []string{".bin", ".sv"},
		Launch:     launch(systemdefs.SystemSuperVision),
	},
	{
		ID:         systemdefs.SystemSaturn,
		SystemID:   systemdefs.SystemSaturn,
		Folders:    []string{"Saturn"},
		Extensions: []string{".cue", ".chd"},
		Launch:     launch(systemdefs.SystemSaturn),
	},
	{
		ID:       "LLAPISaturn",
		SystemID: systemdefs.SystemSaturn,
		Launch:   launchAltCore(systemdefs.SystemSaturn, "_LLAPI/Saturn_LLAPI"),
	},
	{
		ID:       "PWMSaturn",
		SystemID: systemdefs.SystemPSX,
		Launch:   launchAltCore(systemdefs.SystemPSX, "_ConsolePWM/Saturn_PWM"),
	},
	{
		ID:         systemdefs.SystemSNES,
		SystemID:   systemdefs.SystemSNES,
		Folders:    []string{"SNES"},
		Extensions: []string{".sfc", ".smc", ".bin", ".bs"},
		Launch:     launch(systemdefs.SystemSNES),
	},
	{
		ID:       "LLAPISNES",
		SystemID: systemdefs.SystemSNES,
		Launch:   launchAltCore(systemdefs.SystemSNES, "_LLAPI/SNES_LLAPI"),
	},
	{
		ID:       "SindenSNES",
		SystemID: systemdefs.SystemSNES,
		Launch:   launchSinden(systemdefs.SystemSNES, "SNES"),
	},
	{
		ID:         systemdefs.SystemSNESMusic,
		SystemID:   systemdefs.SystemSNESMusic,
		Folders:    []string{"SNES"},
		Extensions: []string{".spc"},
		Launch:     launch(systemdefs.SystemSNESMusic),
	},
	{
		ID:         systemdefs.SystemSuperGrafx,
		SystemID:   systemdefs.SystemSuperGrafx,
		Folders:    []string{"TGFX16"},
		Extensions: []string{".sgx"},
		Launch:     launch(systemdefs.SystemSuperGrafx),
	},
	{
		ID:         systemdefs.SystemTurboGrafx16,
		SystemID:   systemdefs.SystemTurboGrafx16,
		Folders:    []string{"TGFX16"},
		Extensions: []string{".pce", ".bin"},
		Launch:     launch(systemdefs.SystemTurboGrafx16),
	},
	{
		ID:       "LLAPITurboGrafx16",
		SystemID: systemdefs.SystemTurboGrafx16,
		Launch:   launchAltCore(systemdefs.SystemTurboGrafx16, "_LLAPI/TurboGrafx16_LLAPI"),
	},
	{
		ID:         systemdefs.SystemTurboGrafx16CD,
		SystemID:   systemdefs.SystemTurboGrafx16CD,
		Folders:    []string{"TGFX16-CD"},
		Extensions: []string{".cue", ".chd"},
		Launch:     launch(systemdefs.SystemTurboGrafx16CD),
	},
	{
		ID:         systemdefs.SystemVC4000,
		SystemID:   systemdefs.SystemVC4000,
		Folders:    []string{"VC4000"},
		Extensions: []string{".bin"},
		Launch:     launch(systemdefs.SystemVC4000),
	},
	{
		ID:         systemdefs.SystemVectrex,
		SystemID:   systemdefs.SystemVectrex,
		Folders:    []string{"VECTREX"},
		Extensions: []string{".vec", ".bin", ".rom"}, // TODO: overlays (.ovr)
		Launch:     launch(systemdefs.SystemVectrex),
	},
	{
		ID:         systemdefs.SystemWonderSwan,
		SystemID:   systemdefs.SystemWonderSwan,
		Folders:    []string{"WonderSwan"},
		Extensions: []string{".ws"},
		Launch:     launch(systemdefs.SystemWonderSwan),
	},
	{
		ID:         systemdefs.SystemWonderSwanColor,
		SystemID:   systemdefs.SystemWonderSwanColor,
		Folders:    []string{"WonderSwan", "WonderSwanColor"},
		Extensions: []string{".wsc"},
		Launch:     launch(systemdefs.SystemWonderSwanColor),
	},
	// Computers
	{
		ID:         systemdefs.SystemAcornAtom,
		SystemID:   systemdefs.SystemAcornAtom,
		Folders:    []string{"AcornAtom"},
		Extensions: []string{".vhd"},
		Launch:     launch(systemdefs.SystemAcornAtom),
	},
	{
		ID:         systemdefs.SystemAcornElectron,
		SystemID:   systemdefs.SystemAcornElectron,
		Folders:    []string{"AcornElectron"},
		Extensions: []string{".vhd"},
		Launch:     launch(systemdefs.SystemAcornElectron),
	},
	{
		ID:         systemdefs.SystemAliceMC10,
		SystemID:   systemdefs.SystemAliceMC10,
		Folders:    []string{"AliceMC10"},
		Extensions: []string{".c10"},
		Launch:     launch(systemdefs.SystemAliceMC10),
	},
	{
		ID:         systemdefs.SystemAmstrad,
		SystemID:   systemdefs.SystemAmstrad,
		Folders:    []string{"Amstrad"},
		Extensions: []string{".dsk", ".cdt"}, // TODO: globbing support? for .e??
		Launch:     launch(systemdefs.SystemAmstrad),
	},
	{
		ID:         systemdefs.SystemAmstradPCW,
		SystemID:   systemdefs.SystemAmstradPCW,
		Folders:    []string{"Amstrad PCW"},
		Extensions: []string{".dsk"},
		Launch:     launch(systemdefs.SystemAmstradPCW),
	},
	{
		ID:         systemdefs.SystemDOS,
		SystemID:   systemdefs.SystemDOS,
		Folders:    []string{"AO486"},
		Extensions: []string{".img", ".ima", ".vhd", ".vfd", ".iso", ".cue", ".chd", ".mgl"},
		Launch:     launchDOS(),
	},
	{
		ID:         systemdefs.SystemApogee,
		SystemID:   systemdefs.SystemApogee,
		Folders:    []string{"APOGEE"},
		Extensions: []string{".rka", ".rkr", ".gam"},
		Launch:     launch(systemdefs.SystemApogee),
	},
	{
		ID:         systemdefs.SystemAppleI,
		SystemID:   systemdefs.SystemAppleI,
		Folders:    []string{"Apple-I"},
		Extensions: []string{".txt"},
		Launch:     launch(systemdefs.SystemAppleI),
	},
	{
		ID:         systemdefs.SystemAppleII,
		SystemID:   systemdefs.SystemAppleII,
		Folders:    []string{"Apple-II"},
		Extensions: []string{".dsk", ".do", ".po", ".nib", ".hdv"},
		Launch:     launch(systemdefs.SystemAppleII),
	},
	{
		ID:         systemdefs.SystemAquarius,
		SystemID:   systemdefs.SystemAquarius,
		Folders:    []string{"AQUARIUS"},
		Extensions: []string{".bin", ".caq"},
		Launch:     launch(systemdefs.SystemAquarius),
	},
	{
		ID:         systemdefs.SystemAtari800,
		SystemID:   systemdefs.SystemAtari800,
		Folders:    []string{"ATARI800"},
		Extensions: []string{".atr", ".xex", ".xfd", ".atx", ".car", ".rom", ".bin"},
		Launch:     launch(systemdefs.SystemAtari800),
	},
	{
		ID:         systemdefs.SystemBBCMicro,
		SystemID:   systemdefs.SystemBBCMicro,
		Folders:    []string{"BBCMicro"},
		Extensions: []string{".ssd", ".dsd", ".vhd"},
		Launch:     launch(systemdefs.SystemBBCMicro),
	},
	{
		ID:         systemdefs.SystemBK0011M,
		SystemID:   systemdefs.SystemBK0011M,
		Folders:    []string{"BK0011M"},
		Extensions: []string{".bin", ".dsk", ".vhd"},
		Launch:     launch(systemdefs.SystemBK0011M),
	},
	{
		ID:         systemdefs.SystemC16,
		SystemID:   systemdefs.SystemC16,
		Folders:    []string{"C16"},
		Extensions: []string{".d64", ".g64", ".prg", ".tap", ".bin"},
		Launch:     launch(systemdefs.SystemC16),
	},
	{
		ID:         systemdefs.SystemC64,
		SystemID:   systemdefs.SystemC64,
		Folders:    []string{"C64"},
		Extensions: []string{".d64", ".g64", ".t64", ".d81", ".prg", ".crt", ".reu", ".tap"},
		Launch:     launch(systemdefs.SystemC64),
	},
	{
		ID:         systemdefs.SystemCasioPV2000,
		SystemID:   systemdefs.SystemCasioPV2000,
		Folders:    []string{"Casio_PV-2000"},
		Extensions: []string{".bin"},
		Launch:     launch(systemdefs.SystemCasioPV2000),
	},
	{
		ID:         systemdefs.SystemCoCo2,
		SystemID:   systemdefs.SystemCoCo2,
		Folders:    []string{"CoCo2"},
		Extensions: []string{".dsk", ".cas", ".ccc", ".rom"},
		Launch:     launch(systemdefs.SystemCoCo2),
	},
	{
		ID:         systemdefs.SystemEDSAC,
		SystemID:   systemdefs.SystemEDSAC,
		Folders:    []string{"EDSAC"},
		Extensions: []string{".tap"},
		Launch:     launch(systemdefs.SystemEDSAC),
	},
	{
		ID:         systemdefs.SystemGalaksija,
		SystemID:   systemdefs.SystemGalaksija,
		Folders:    []string{"Galaksija"},
		Extensions: []string{".tap"},
		Launch:     launch(systemdefs.SystemGalaksija),
	},
	{
		ID:         systemdefs.SystemInteract,
		SystemID:   systemdefs.SystemInteract,
		Folders:    []string{"Interact"},
		Extensions: []string{".cin", ".k7"},
		Launch:     launch(systemdefs.SystemInteract),
	},
	{
		ID:         systemdefs.SystemJupiter,
		SystemID:   systemdefs.SystemJupiter,
		Folders:    []string{"Jupiter"},
		Extensions: []string{".ace"},
		Launch:     launch(systemdefs.SystemJupiter),
	},
	{
		ID:         systemdefs.SystemLaser,
		SystemID:   systemdefs.SystemLaser,
		Folders:    []string{"Laser"},
		Extensions: []string{".vz"},
		Launch:     launch(systemdefs.SystemLaser),
	},
	{
		ID:         systemdefs.SystemLynx48,
		SystemID:   systemdefs.SystemLynx48,
		Folders:    []string{"Lynx48"},
		Extensions: []string{".tap"},
		Launch:     launch(systemdefs.SystemLynx48),
	},
	{
		ID:         systemdefs.SystemMacPlus,
		SystemID:   systemdefs.SystemMacPlus,
		Folders:    []string{"MACPLUS"},
		Extensions: []string{".dsk", ".img", ".vhd"},
		Launch:     launch(systemdefs.SystemMacPlus),
	},
	{
		ID:         systemdefs.SystemMSX,
		SystemID:   systemdefs.SystemMSX,
		Folders:    []string{"MSX"},
		Extensions: []string{".vhd"},
		Launch:     launch(systemdefs.SystemMSX),
	},
	{
		ID:         "MSX1",
		SystemID:   systemdefs.SystemMSX,
		Folders:    []string{"MSX1"},
		Extensions: []string{".dsk", ".rom"},
		Launch:     launchAltCore(systemdefs.SystemMSX, "_Console/MSX1"),
	},
	{
		ID:         systemdefs.SystemMultiComp,
		SystemID:   systemdefs.SystemMultiComp,
		Folders:    []string{"MultiComp"},
		Extensions: []string{".img"},
		Launch:     launch(systemdefs.SystemMultiComp),
	},
	{
		ID:         systemdefs.SystemOrao,
		SystemID:   systemdefs.SystemOrao,
		Folders:    []string{"ORAO"},
		Extensions: []string{".tap"},
		Launch:     launch(systemdefs.SystemOrao),
	},
	{
		ID:         systemdefs.SystemOric,
		SystemID:   systemdefs.SystemOric,
		Folders:    []string{"Oric"},
		Extensions: []string{".dsk"},
		Launch:     launch(systemdefs.SystemOric),
	},
	{
		ID:         systemdefs.SystemPCXT,
		SystemID:   systemdefs.SystemPCXT,
		Folders:    []string{"PCXT"},
		Extensions: []string{".img", ".vhd", ".ima", ".vfd"},
		Launch:     launch(systemdefs.SystemPCXT),
	},
	{
		ID:         systemdefs.SystemPDP1,
		SystemID:   systemdefs.SystemPDP1,
		Folders:    []string{"PDP1"},
		Extensions: []string{".bin", ".rim", ".pdp"},
		Launch:     launch(systemdefs.SystemPDP1),
	},
	{
		ID:         systemdefs.SystemPET2001,
		SystemID:   systemdefs.SystemPET2001,
		Folders:    []string{"PET2001"},
		Extensions: []string{".prg", ".tap"},
		Launch:     launch(systemdefs.SystemPET2001),
	},
	{
		ID:         systemdefs.SystemPMD85,
		SystemID:   systemdefs.SystemPMD85,
		Folders:    []string{"PMD85"},
		Extensions: []string{".rmm"},
		Launch:     launch(systemdefs.SystemPMD85),
	},
	{
		ID:         systemdefs.SystemQL,
		SystemID:   systemdefs.SystemQL,
		Folders:    []string{"QL"},
		Extensions: []string{".mdv", ".win"},
		Launch:     launch(systemdefs.SystemQL),
	},
	{
		ID:         systemdefs.SystemRX78,
		SystemID:   systemdefs.SystemRX78,
		Folders:    []string{"RX78"},
		Extensions: []string{".bin"},
		Launch:     launch(systemdefs.SystemRX78),
	},
	{
		ID:         systemdefs.SystemSAMCoupe,
		SystemID:   systemdefs.SystemSAMCoupe,
		Folders:    []string{"SAMCOUPE"},
		Extensions: []string{".dsk", ".mgt", ".img"},
		Launch:     launch(systemdefs.SystemSAMCoupe),
	},
	{
		ID:         systemdefs.SystemSordM5,
		SystemID:   systemdefs.SystemSordM5,
		Folders:    []string{"Sord M5"},
		Extensions: []string{".bin", ".rom", ".cas"},
		Launch:     launch(systemdefs.SystemSordM5),
	},
	{
		ID:         systemdefs.SystemSpecialist,
		SystemID:   systemdefs.SystemSpecialist,
		Folders:    []string{"SPMX"},
		Extensions: []string{".rks", ".odi"},
		Launch:     launch(systemdefs.SystemSpecialist),
	},
	{
		ID:         systemdefs.SystemSVI328,
		SystemID:   systemdefs.SystemSVI328,
		Folders:    []string{"SVI328"},
		Extensions: []string{".cas", ".bin", ".rom"},
		Launch:     launch(systemdefs.SystemSVI328),
	},
	{
		ID:         systemdefs.SystemTatungEinstein,
		SystemID:   systemdefs.SystemTatungEinstein,
		Folders:    []string{"TatungEinstein"},
		Extensions: []string{".dsk"},
		Launch:     launch(systemdefs.SystemTatungEinstein),
	},
	{
		ID:         systemdefs.SystemTI994A,
		SystemID:   systemdefs.SystemTI994A,
		Folders:    []string{"TI-99_4A"},
		Extensions: []string{".bin", ".m99"},
		Launch:     launch(systemdefs.SystemTI994A),
	},
	{
		ID:         systemdefs.SystemTomyTutor,
		SystemID:   systemdefs.SystemTomyTutor,
		Folders:    []string{"TomyTutor"},
		Extensions: []string{".bin", ".cas"},
		Launch:     launch(systemdefs.SystemTomyTutor),
	},
	{
		ID:         systemdefs.SystemTRS80,
		SystemID:   systemdefs.SystemTRS80,
		Folders:    []string{"TRS-80"},
		Extensions: []string{".jvi", ".dsk", ".cas"},
		Launch:     launch(systemdefs.SystemTRS80),
	},
	{
		ID:         systemdefs.SystemTSConf,
		SystemID:   systemdefs.SystemTSConf,
		Folders:    []string{"TSConf"},
		Extensions: []string{".vhf"},
		Launch:     launch(systemdefs.SystemTSConf),
	},
	{
		ID:         systemdefs.SystemUK101,
		SystemID:   systemdefs.SystemUK101,
		Folders:    []string{"UK101"},
		Extensions: []string{".txt", ".bas", ".lod"},
		Launch:     launch(systemdefs.SystemUK101),
	},
	{
		ID:         systemdefs.SystemVector06C,
		SystemID:   systemdefs.SystemVector06C,
		Folders:    []string{"VECTOR06"},
		Extensions: []string{".rom", ".com", ".c00", ".edd", ".fdd"},
		Launch:     launch(systemdefs.SystemVector06C),
	},
	{
		ID:         systemdefs.SystemVIC20,
		SystemID:   systemdefs.SystemVIC20,
		Folders:    []string{"VIC20"},
		Extensions: []string{".d64", ".g64", ".prg", ".tap", ".crt"},
		Launch:     launch(systemdefs.SystemVIC20),
	},
	{
		ID:         systemdefs.SystemX68000,
		SystemID:   systemdefs.SystemX68000,
		Folders:    []string{"X68000"},
		Extensions: []string{".d88", ".hdf", ".mgl"},
		Launch:     launch(systemdefs.SystemX68000),
	},
	{
		ID:         systemdefs.SystemZX81,
		SystemID:   systemdefs.SystemZX81,
		Folders:    []string{"ZX81"},
		Extensions: []string{".p", ".0"},
		Launch:     launch(systemdefs.SystemZX81),
	},
	{
		ID:         systemdefs.SystemZXSpectrum,
		SystemID:   systemdefs.SystemZXSpectrum,
		Folders:    []string{"Spectrum"},
		Extensions: []string{".tap", ".csw", ".tzx", ".sna", ".z80", ".trd", ".img", ".dsk", ".mgt"},
		Launch:     launch(systemdefs.SystemZXSpectrum),
	},
	{
		ID:         systemdefs.SystemZXNext,
		SystemID:   systemdefs.SystemZXNext,
		Folders:    []string{"ZXNext"},
		Extensions: []string{".vhd"},
		Launch:     launch(systemdefs.SystemZXNext),
	},
	// Other
	{
		ID:         systemdefs.SystemArcade,
		SystemID:   systemdefs.SystemArcade,
		Folders:    []string{"_Arcade"},
		Extensions: []string{".mra"},
		Launch:     launch(systemdefs.SystemArcade),
	},
	{
		ID:         systemdefs.SystemArduboy,
		SystemID:   systemdefs.SystemArduboy,
		Folders:    []string{"Arduboy"},
		Extensions: []string{".hex", ".bin"},
		Launch:     launch(systemdefs.SystemArduboy),
	},
	{
		ID:         systemdefs.SystemChip8,
		SystemID:   systemdefs.SystemChip8,
		Folders:    []string{"Chip8"},
		Extensions: []string{".ch8"},
		Launch:     launch(systemdefs.SystemChip8),
	},
	{
		ID:         systemdefs.SystemGroovy,
		SystemID:   systemdefs.SystemGroovy,
		Folders:    []string{"Groovy"},
		Extensions: []string{".gmc"},
		Launch:     launch(systemdefs.SystemGroovy),
	},
	{
		ID:         "Generic",
		Extensions: []string{".mgl", ".rbf", ".mra"},
		Launch: func(cfg *config.Instance, path string) error {
			err := mister.LaunchGenericFile(UserConfigToMrext(cfg), path)
			if err != nil {
				return fmt.Errorf("failed to launch generic file: %w", err)
			}
			log.Debug().Msgf("setting active game: %s", path)
			err = mister.SetActiveGame(path)
			if err != nil {
				return fmt.Errorf("failed to set active game: %w", err)
			}
			return nil
		},
	},
}
