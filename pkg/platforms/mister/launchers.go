//go:build linux || darwin

package mister

import (
	"archive/zip"
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
	"github.com/wizzomafizzo/mrext/pkg/games"
	"github.com/wizzomafizzo/mrext/pkg/mister"
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
		s, err := games.GetSystem(systemID)
		if err != nil {
			return err
		}

		path = checkInZip(path)

		err = mister.LaunchGame(UserConfigToMrext(cfg), *s, path)
		if err != nil {
			return err
		}

		log.Debug().Msgf("setting active game: %s", path)
		return mister.SetActiveGame(path)
	}
}

func launchSinden(
	systemId string,
	rbfName string,
) func(*config.Instance, string) error {
	return func(cfg *config.Instance, path string) error {
		s, err := games.GetSystem(systemId)
		if err != nil {
			return err
		}
		path = checkInZip(path)

		sn := *s
		sn.Rbf = "_Sinden/" + rbfName + "_Sinden"
		sn.SetName = rbfName + "_Sinden"
		sn.SetNameSameDir = true

		log.Debug().Str("rbf", sn.Rbf).Msgf("launching Sinden: %v", sn)

		err = mister.LaunchGame(UserConfigToMrext(cfg), sn, path)
		if err != nil {
			return err
		}

		return mister.SetActiveGame(path)
	}
}

func launchAggGnw(cfg *config.Instance, path string) error {
	s, err := games.GetSystem("GameNWatch")
	if err != nil {
		return err
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
		return err
	}

	return mister.SetActiveGame(path)
}

func launchAltCore(
	systemId string,
	rbfPath string,
) func(*config.Instance, string) error {
	return func(cfg *config.Instance, path string) error {
		s, err := games.GetSystem(systemId)
		if err != nil {
			return err
		}
		path = checkInZip(path)

		sn := *s
		sn.Rbf = rbfPath

		log.Debug().Str("rbf", sn.Rbf).Msgf("launching alt core: %v", sn)

		err = mister.LaunchGame(UserConfigToMrext(cfg), sn, path)
		if err != nil {
			return err
		}

		return mister.SetActiveGame(path)
	}
}

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
			return err
		}

		return mister.SetActiveGame(path)
	}
}

func killCore(_ *config.Instance) error {
	return mister.LaunchMenu()
}

func launchMPlayer(pl *Platform) func(*config.Instance, string) error {
	return func(_ *config.Instance, path string) error {
		if len(path) == 0 {
			return fmt.Errorf("no path specified")
		}

		vt := "4"

		if pl.ActiveSystem() != "" {

		}

		//err := mister.LaunchMenu()
		//if err != nil {
		//	return err
		//}
		//time.Sleep(3 * time.Second)

		err := cleanConsole(vt)
		if err != nil {
			return err
		}

		err = openConsole(pl.kbd, vt)
		if err != nil {
			return err
		}

		time.Sleep(500 * time.Millisecond)
		err = mister.SetVideoMode(640, 480)
		if err != nil {
			return fmt.Errorf("error setting video mode: %w", err)
		}

		cmd := exec.Command(
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
			err := mister.LaunchMenu()
			if err != nil {
				log.Warn().Err(err).Msg("error launching menu")
			}

			err = restoreConsole(vt)
			if err != nil {
				log.Warn().Err(err).Msg("error restoring console")
			}
		}

		err = cmd.Start()
		if err != nil {
			restore()
			return err
		}

		err = cmd.Wait()
		if err != nil {
			restore()
			return err
		}

		restore()
		return nil
	}
}

func killMPlayer(_ *config.Instance) error {
	psCmd := exec.Command("sh", "-c", "ps aux | grep mplayer | grep -v grep")
	output, err := psCmd.Output()
	if err != nil {
		log.Info().Msgf("mplayer processes not detected.")
		return nil
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

		killCmd := exec.Command("kill", "-9", pid)
		if err := killCmd.Run(); err != nil {
			log.Error().Msgf("failed to kill process %s: %v", pid, err)
		}
	}

	return nil
}

var Launchers = []platforms.Launcher{
	{
		Id:         "Generic",
		Extensions: []string{".mgl", ".rbf", ".mra"},
		Launch: func(cfg *config.Instance, path string) error {
			err := mister.LaunchGenericFile(UserConfigToMrext(cfg), path)
			if err != nil {
				return err
			}
			log.Debug().Msgf("setting active game: %s", path)
			return mister.SetActiveGame(path)
		},
	},
	// Consoles
	{
		Id:         systemdefs.SystemAdventureVision,
		SystemId:   systemdefs.SystemAdventureVision,
		Folders:    []string{"AVision"},
		Extensions: []string{".bin"},
		Launch:     launch(systemdefs.SystemAdventureVision),
	},
	{
		Id:         systemdefs.SystemArcadia,
		SystemId:   systemdefs.SystemArcadia,
		Folders:    []string{"Arcadia"},
		Extensions: []string{".bin"},
		Launch:     launch(systemdefs.SystemArcadia),
	},
	{
		Id:         systemdefs.SystemAmigaCD32,
		SystemId:   systemdefs.SystemAmigaCD32,
		Folders:    []string{"AmigaCD32"},
		Extensions: []string{".cue", ".chd"},
		Launch:     launch(systemdefs.SystemAmigaCD32),
	},
	{
		Id:         systemdefs.SystemAstrocade,
		SystemId:   systemdefs.SystemAstrocade,
		Folders:    []string{"Astrocade"},
		Extensions: []string{".bin"},
		Launch:     launch(systemdefs.SystemAstrocade),
	},
	{
		Id:         systemdefs.SystemAtari2600,
		SystemId:   systemdefs.SystemAtari2600,
		Folders:    []string{"ATARI7800", "Atari2600"},
		Extensions: []string{".a26"},
		Launch:     launch(systemdefs.SystemAtari2600),
	},
	{
		Id:       "LLAPIAtari2600",
		SystemId: systemdefs.SystemAtari2600,
		Launch:   launchAltCore(systemdefs.SystemAtari2600, "_LLAPI/Atari7800_LLAPI"),
	},
	{
		Id:         systemdefs.SystemAtari5200,
		SystemId:   systemdefs.SystemAtari5200,
		Folders:    []string{"ATARI5200"},
		Extensions: []string{".a52"},
		Launch:     launch(systemdefs.SystemAtari5200),
	},
	{
		Id:         systemdefs.SystemAtari7800,
		SystemId:   systemdefs.SystemAtari7800,
		Folders:    []string{"ATARI7800"},
		Extensions: []string{".a78"},
		Launch:     launch(systemdefs.SystemAtari7800),
	},
	{
		Id:       "LLAPIAtari7800",
		SystemId: systemdefs.SystemAtari7800,
		Launch:   launchAltCore(systemdefs.SystemAtari7800, "_LLAPI/Atari7800_LLAPI"),
	},
	{
		Id:         systemdefs.SystemAtariLynx,
		SystemId:   systemdefs.SystemAtariLynx,
		Folders:    []string{"AtariLynx"},
		Extensions: []string{".lnx"},
		Launch:     launch(systemdefs.SystemAtariLynx),
	},
	{
		Id:         systemdefs.SystemCasioPV1000,
		SystemId:   systemdefs.SystemCasioPV1000,
		Folders:    []string{"Casio_PV-1000"},
		Extensions: []string{".bin"},
		Launch:     launch(systemdefs.SystemCasioPV1000),
	},
	{
		Id:         systemdefs.SystemCDI,
		SystemId:   systemdefs.SystemCDI,
		Folders:    []string{"CD-i"},
		Extensions: []string{".cue", ".chd"},
		Launch:     launch(systemdefs.SystemCDI),
	},
	{
		Id:         systemdefs.SystemChannelF,
		SystemId:   systemdefs.SystemChannelF,
		Folders:    []string{"ChannelF"},
		Extensions: []string{".rom", ".bin"},
		Launch:     launch(systemdefs.SystemChannelF),
	},
	{
		Id:         systemdefs.SystemColecoVision,
		SystemId:   systemdefs.SystemColecoVision,
		Folders:    []string{"Coleco"},
		Extensions: []string{".col", ".bin", ".rom"},
		Launch:     launch(systemdefs.SystemColecoVision),
	},
	{
		Id:         systemdefs.SystemCreatiVision,
		SystemId:   systemdefs.SystemCreatiVision,
		Folders:    []string{"CreatiVision"},
		Extensions: []string{".rom", ".bin", ".bas"},
		Launch:     launch(systemdefs.SystemCreatiVision),
	},
	{
		Id:         systemdefs.SystemFDS,
		SystemId:   systemdefs.SystemFDS,
		Folders:    []string{"NES", "FDS"},
		Extensions: []string{".fds"},
		Launch:     launch(systemdefs.SystemFDS),
	},
	{
		Id:         systemdefs.SystemGamate,
		SystemId:   systemdefs.SystemGamate,
		Folders:    []string{"Gamate"},
		Extensions: []string{".bin"},
		Launch:     launch(systemdefs.SystemGamate),
	},
	{
		Id:         systemdefs.SystemGameboy,
		SystemId:   systemdefs.SystemGameboy,
		Folders:    []string{"GAMEBOY"},
		Extensions: []string{".gb"},
		Launch:     launch(systemdefs.SystemGameboy),
	},
	{
		Id:       "LLAPIGameboy",
		SystemId: systemdefs.SystemGameboy,
		Launch:   launchAltCore(systemdefs.SystemGameboy, "_LLAPI/Gameboy_LLAPI"),
	},
	{
		Id:         systemdefs.SystemGameboyColor,
		SystemId:   systemdefs.SystemGameboyColor,
		Folders:    []string{"GAMEBOY", "GBC"},
		Extensions: []string{".gbc"},
		Launch:     launch(systemdefs.SystemGameboyColor),
	},
	{
		Id:         systemdefs.SystemGameboy2P,
		SystemId:   systemdefs.SystemGameboy2P,
		Folders:    []string{"GAMEBOY2P"},
		Extensions: []string{".gb", ".gbc"},
		Launch:     launch(systemdefs.SystemGameboy2P),
	},
	{
		Id:         systemdefs.SystemGameGear,
		SystemId:   systemdefs.SystemGameGear,
		Folders:    []string{"SMS", "GameGear"},
		Extensions: []string{".gg"},
		Launch:     launch(systemdefs.SystemGameGear),
	},
	{
		Id:         systemdefs.SystemGameNWatch,
		SystemId:   systemdefs.SystemGameNWatch,
		Folders:    []string{"GameNWatch"},
		Extensions: []string{".bin"},
		Launch:     launch(systemdefs.SystemGameNWatch),
	},
	{
		Id:         "GameAndWatch",
		SystemId:   systemdefs.SystemGameNWatch,
		Folders:    []string{"Game and Watch"},
		Extensions: []string{".gnw"},
		Launch:     launchAggGnw,
	},
	{
		Id:         systemdefs.SystemGBA,
		SystemId:   systemdefs.SystemGBA,
		Folders:    []string{"GBA"},
		Extensions: []string{".gba"},
		Launch:     launch(systemdefs.SystemGBA),
	},
	{
		Id:       "LLAPIGBA",
		SystemId: systemdefs.SystemGBA,
		Launch:   launchAltCore(systemdefs.SystemGBA, "_LLAPI/GBA_LLAPI"),
	},
	{
		Id:         systemdefs.SystemGBA2P,
		SystemId:   systemdefs.SystemGBA2P,
		Folders:    []string{"GBA2P"},
		Extensions: []string{".gba"},
		Launch:     launch(systemdefs.SystemGBA2P),
	},
	{
		Id:         systemdefs.SystemGenesis,
		SystemId:   systemdefs.SystemGenesis,
		Folders:    []string{"MegaDrive", "Genesis"},
		Extensions: []string{".gen", ".bin", ".md"},
		Launch:     launch(systemdefs.SystemGenesis),
	},
	{
		Id:       "SindenGenesis",
		SystemId: systemdefs.SystemGenesis,
		Launch:   launchSinden(systemdefs.SystemGenesis, "Genesis"),
	},
	{
		Id:       "SindenMegaDrive",
		SystemId: systemdefs.SystemGenesis,
		Launch:   launchSinden(systemdefs.SystemGenesis, "MegaDrive"),
	},
	{
		Id:       "LLAPIMegaDrive",
		SystemId: systemdefs.SystemGenesis,
		Launch:   launchAltCore(systemdefs.SystemGenesis, "_LLAPI/MegaDrive_LLAPI"),
	},
	{
		Id:         systemdefs.SystemIntellivision,
		SystemId:   systemdefs.SystemIntellivision,
		Folders:    []string{"Intellivision"},
		Extensions: []string{".int", ".bin"},
		Launch:     launch(systemdefs.SystemIntellivision),
	},
	{
		Id:         systemdefs.SystemJaguar,
		SystemId:   systemdefs.SystemJaguar,
		Folders:    []string{"Jaguar"},
		Extensions: []string{".jag", ".j64", ".rom", ".bin"},
		Launch:     launch(systemdefs.SystemJaguar),
	},
	{
		Id:         systemdefs.SystemMasterSystem,
		SystemId:   systemdefs.SystemMasterSystem,
		Folders:    []string{"SMS"},
		Extensions: []string{".sms"},
		Launch:     launch(systemdefs.SystemMasterSystem),
	},
	{
		Id:       "SindenSMS",
		SystemId: systemdefs.SystemMasterSystem,
		Launch:   launchSinden(systemdefs.SystemMasterSystem, "SMS"),
	},
	{
		Id:       "LLAPISMS",
		SystemId: systemdefs.SystemMasterSystem,
		Launch:   launchAltCore(systemdefs.SystemMasterSystem, "_LLAPI/SMS_LLAPI"),
	},
	{
		Id:         systemdefs.SystemMegaCD,
		SystemId:   systemdefs.SystemMegaCD,
		Folders:    []string{"MegaCD"},
		Extensions: []string{".cue", ".chd"},
		Launch:     launch(systemdefs.SystemMegaCD),
	},
	{
		Id:       "SindenMegaCD",
		SystemId: systemdefs.SystemMegaCD,
		Launch:   launchSinden(systemdefs.SystemMegaCD, "MegaCD"),
	},
	{
		Id:       "LLAPIMegaCD",
		SystemId: systemdefs.SystemMegaCD,
		Launch:   launchAltCore(systemdefs.SystemMegaCD, "_LLAPI/MegaCD_LLAPI"),
	},
	{
		Id:         systemdefs.SystemMegaDuck,
		SystemId:   systemdefs.SystemMegaDuck,
		Folders:    []string{"GAMEBOY", "MegaDuck"},
		Extensions: []string{".bin"},
		Launch:     launch(systemdefs.SystemMegaDuck),
	},
	{
		Id:       "LLAPINeoGeo",
		SystemId: systemdefs.SystemNeoGeo,
		Launch:   launchAltCore(systemdefs.SystemNeoGeo, "_LLAPI/NeoGeo_LLAPI"),
	},
	{
		Id:         systemdefs.SystemNeoGeoCD,
		SystemId:   systemdefs.SystemNeoGeoCD,
		Folders:    []string{"NeoGeo-CD", "NEOGEO"},
		Extensions: []string{".cue", ".chd"},
		Launch:     launch(systemdefs.SystemNeoGeoCD),
	},
	{
		Id:         systemdefs.SystemNES,
		SystemId:   systemdefs.SystemNES,
		Folders:    []string{"NES"},
		Extensions: []string{".nes"},
		Launch:     launch(systemdefs.SystemNES),
	},
	{
		Id:       "SindenNES",
		SystemId: systemdefs.SystemNES,
		Launch:   launchSinden(systemdefs.SystemNES, "NES"),
	},
	{
		Id:         systemdefs.SystemNESMusic,
		SystemId:   systemdefs.SystemNESMusic,
		Folders:    []string{"NES"},
		Extensions: []string{".nsf"},
		Launch:     launch(systemdefs.SystemNESMusic),
	},
	{
		Id:       "LLAPINES",
		SystemId: systemdefs.SystemNES,
		Launch:   launchAltCore(systemdefs.SystemNES, "_LLAPI/NES_LLAPI"),
	},
	{
		Id:         systemdefs.SystemNintendo64,
		SystemId:   systemdefs.SystemNintendo64,
		Folders:    []string{"N64"},
		Extensions: []string{".n64", ".z64"},
		Launch:     launch(systemdefs.SystemNintendo64),
	},
	{
		Id:       "LLAPINintendo64",
		SystemId: systemdefs.SystemNintendo64,
		Launch:   launchAltCore(systemdefs.SystemNintendo64, "_LLAPI/N64_LLAPI"),
	},
	{
		Id:       "LLAPI80MHzNintendo64",
		SystemId: systemdefs.SystemNintendo64,
		Launch:   launchAltCore(systemdefs.SystemNintendo64, "_LLAPI/N64_80MHz_LLAPI"),
	},
	{
		Id:       "80MHzNintendo64",
		SystemId: systemdefs.SystemNintendo64,
		Launch:   launchAltCore(systemdefs.SystemNintendo64, "_Console/N64_80MHz"),
	},
	{
		Id:       "PWMNintendo64",
		SystemId: systemdefs.SystemNintendo64,
		Launch:   launchAltCore(systemdefs.SystemNintendo64, "_ConsolePWM/N64_PWM"),
	},
	{
		Id:       "PWM80MHzNintendo64",
		SystemId: systemdefs.SystemNintendo64,
		Launch:   launchAltCore(systemdefs.SystemNintendo64, "_ConsolePWM/_Turbo/N64_80MHz_PWM"),
	},
	{
		Id:         systemdefs.SystemOdyssey2,
		SystemId:   systemdefs.SystemOdyssey2,
		Folders:    []string{"ODYSSEY2"},
		Extensions: []string{".bin"},
		Launch:     launch(systemdefs.SystemOdyssey2),
	},
	{
		Id:         systemdefs.SystemPocketChallengeV2,
		SystemId:   systemdefs.SystemPocketChallengeV2,
		Folders:    []string{"WonderSwan", "PocketChallengeV2"},
		Extensions: []string{".pc2"},
		Launch:     launch(systemdefs.SystemPocketChallengeV2),
	},
	{
		Id:         systemdefs.SystemPokemonMini,
		SystemId:   systemdefs.SystemPokemonMini,
		Folders:    []string{"PokemonMini"},
		Extensions: []string{".min"},
		Launch:     launch(systemdefs.SystemPokemonMini),
	},
	{
		Id:         systemdefs.SystemPSX,
		SystemId:   systemdefs.SystemPSX,
		Folders:    []string{"PSX"},
		Extensions: []string{".cue", ".chd", ".exe"},
		Launch:     launch(systemdefs.SystemPSX),
	},
	{
		Id:       "LLAPIPSX",
		SystemId: systemdefs.SystemPSX,
		Launch:   launchAltCore(systemdefs.SystemPSX, "_LLAPI/PSX_LLAPI"),
	},
	{
		Id:       "SindenPSX",
		SystemId: systemdefs.SystemPSX,
		Launch:   launchSinden(systemdefs.SystemPSX, "PSX"),
	},
	{
		Id:       "2XPSX",
		SystemId: systemdefs.SystemPSX,
		Launch:   launchAltCore(systemdefs.SystemPSX, "_Console/PSX2XCPU"),
	},
	{
		Id:       "PWMPSX",
		SystemId: systemdefs.SystemPSX,
		Launch:   launchAltCore(systemdefs.SystemPSX, "_ConsolePWM/PSX_PWM"),
	},
	{
		Id:       "PWM2XPSX",
		SystemId: systemdefs.SystemPSX,
		Launch:   launchAltCore(systemdefs.SystemPSX, "_ConsolePWM/_Turbo/PSX2XCPU_PWM"),
	},
	{
		Id:         systemdefs.SystemSega32X,
		SystemId:   systemdefs.SystemSega32X,
		Folders:    []string{"S32X"},
		Extensions: []string{".32x"},
		Launch:     launch(systemdefs.SystemSega32X),
	},
	{
		Id:       "LLAPIS32X",
		SystemId: systemdefs.SystemSega32X,
		Launch:   launchAltCore(systemdefs.SystemPSX, "_LLAPI/S32X_LLAPI"),
	},
	{
		Id:         systemdefs.SystemSG1000,
		SystemId:   systemdefs.SystemSG1000,
		Folders:    []string{"SG1000", "Coleco", "SMS"},
		Extensions: []string{".sg"},
		Launch:     launch(systemdefs.SystemSG1000),
	},
	{
		Id:         systemdefs.SystemSuperGameboy,
		SystemId:   systemdefs.SystemSuperGameboy,
		Folders:    []string{"SGB"},
		Extensions: []string{".sgb", ".gb", ".gbc"},
		Launch:     launch(systemdefs.SystemSuperGameboy),
	},
	{
		Id:       "LLAPISuperGameboy",
		SystemId: systemdefs.SystemSuperGameboy,
		Launch:   launchAltCore(systemdefs.SystemSuperGameboy, "_LLAPI/SGB_LLAPI"),
	},
	{
		Id:         systemdefs.SystemSuperVision,
		SystemId:   systemdefs.SystemSuperVision,
		Folders:    []string{"SuperVision"},
		Extensions: []string{".bin", ".sv"},
		Launch:     launch(systemdefs.SystemSuperVision),
	},
	{
		Id:         systemdefs.SystemSaturn,
		SystemId:   systemdefs.SystemSaturn,
		Folders:    []string{"Saturn"},
		Extensions: []string{".cue", ".chd"},
		Launch:     launch(systemdefs.SystemSaturn),
	},
	{
		Id:       "LLAPISaturn",
		SystemId: systemdefs.SystemSaturn,
		Launch:   launchAltCore(systemdefs.SystemSaturn, "_LLAPI/Saturn_LLAPI"),
	},
	{
		Id:       "PWMSaturn",
		SystemId: systemdefs.SystemPSX,
		Launch:   launchAltCore(systemdefs.SystemPSX, "_ConsolePWM/Saturn_PWM"),
	},
	{
		Id:         systemdefs.SystemSNES,
		SystemId:   systemdefs.SystemSNES,
		Folders:    []string{"SNES"},
		Extensions: []string{".sfc", ".smc", ".bin", ".bs"},
		Launch:     launch(systemdefs.SystemSNES),
	},
	{
		Id:       "LLAPISNES",
		SystemId: systemdefs.SystemSNES,
		Launch:   launchAltCore(systemdefs.SystemSNES, "_LLAPI/SNES_LLAPI"),
	},
	{
		Id:       "SindenSNES",
		SystemId: systemdefs.SystemSNES,
		Launch:   launchSinden(systemdefs.SystemSNES, "SNES"),
	},
	{
		Id:         systemdefs.SystemSNESMusic,
		SystemId:   systemdefs.SystemSNESMusic,
		Folders:    []string{"SNES"},
		Extensions: []string{".spc"},
		Launch:     launch(systemdefs.SystemSNESMusic),
	},
	{
		Id:         systemdefs.SystemSuperGrafx,
		SystemId:   systemdefs.SystemSuperGrafx,
		Folders:    []string{"TGFX16"},
		Extensions: []string{".sgx"},
		Launch:     launch(systemdefs.SystemSuperGrafx),
	},
	{
		Id:         systemdefs.SystemTurboGrafx16,
		SystemId:   systemdefs.SystemTurboGrafx16,
		Folders:    []string{"TGFX16"},
		Extensions: []string{".pce", ".bin"},
		Launch:     launch(systemdefs.SystemTurboGrafx16),
	},
	{
		Id:       "LLAPITurboGrafx16",
		SystemId: systemdefs.SystemTurboGrafx16,
		Launch:   launchAltCore(systemdefs.SystemTurboGrafx16, "_LLAPI/TurboGrafx16_LLAPI"),
	},
	{
		Id:         systemdefs.SystemTurboGrafx16CD,
		SystemId:   systemdefs.SystemTurboGrafx16CD,
		Folders:    []string{"TGFX16-CD"},
		Extensions: []string{".cue", ".chd"},
		Launch:     launch(systemdefs.SystemTurboGrafx16CD),
	},
	{
		Id:         systemdefs.SystemVC4000,
		SystemId:   systemdefs.SystemVC4000,
		Folders:    []string{"VC4000"},
		Extensions: []string{".bin"},
		Launch:     launch(systemdefs.SystemVC4000),
	},
	{
		Id:         systemdefs.SystemVectrex,
		SystemId:   systemdefs.SystemVectrex,
		Folders:    []string{"VECTREX"},
		Extensions: []string{".vec", ".bin", ".rom"}, // TODO: overlays (.ovr)
		Launch:     launch(systemdefs.SystemVectrex),
	},
	{
		Id:         systemdefs.SystemWonderSwan,
		SystemId:   systemdefs.SystemWonderSwan,
		Folders:    []string{"WonderSwan"},
		Extensions: []string{".ws"},
		Launch:     launch(systemdefs.SystemWonderSwan),
	},
	{
		Id:         systemdefs.SystemWonderSwanColor,
		SystemId:   systemdefs.SystemWonderSwanColor,
		Folders:    []string{"WonderSwan", "WonderSwanColor"},
		Extensions: []string{".wsc"},
		Launch:     launch(systemdefs.SystemWonderSwanColor),
	},
	// Computers
	{
		Id:         systemdefs.SystemAcornAtom,
		SystemId:   systemdefs.SystemAcornAtom,
		Folders:    []string{"AcornAtom"},
		Extensions: []string{".vhd"},
		Launch:     launch(systemdefs.SystemAcornAtom),
	},
	{
		Id:         systemdefs.SystemAcornElectron,
		SystemId:   systemdefs.SystemAcornElectron,
		Folders:    []string{"AcornElectron"},
		Extensions: []string{".vhd"},
		Launch:     launch(systemdefs.SystemAcornElectron),
	},
	{
		Id:         systemdefs.SystemAliceMC10,
		SystemId:   systemdefs.SystemAliceMC10,
		Folders:    []string{"AliceMC10"},
		Extensions: []string{".c10"},
		Launch:     launch(systemdefs.SystemAliceMC10),
	},
	{
		Id:         systemdefs.SystemAmstrad,
		SystemId:   systemdefs.SystemAmstrad,
		Folders:    []string{"Amstrad"},
		Extensions: []string{".dsk", ".cdt"}, // TODO: globbing support? for .e??
		Launch:     launch(systemdefs.SystemAmstrad),
	},
	{
		Id:         systemdefs.SystemAmstradPCW,
		SystemId:   systemdefs.SystemAmstradPCW,
		Folders:    []string{"Amstrad PCW"},
		Extensions: []string{".dsk"},
		Launch:     launch(systemdefs.SystemAmstradPCW),
	},
	{
		Id:         systemdefs.SystemDOS,
		SystemId:   systemdefs.SystemDOS,
		Folders:    []string{"AO486"},
		Extensions: []string{".img", ".ima", ".vhd", ".vfd", ".iso", ".cue", ".chd"},
		Launch:     launch(systemdefs.SystemDOS),
	},
	{
		Id:         systemdefs.SystemApogee,
		SystemId:   systemdefs.SystemApogee,
		Folders:    []string{"APOGEE"},
		Extensions: []string{".rka", ".rkr", ".gam"},
		Launch:     launch(systemdefs.SystemApogee),
	},
	{
		Id:         systemdefs.SystemAppleI,
		SystemId:   systemdefs.SystemAppleI,
		Folders:    []string{"Apple-I"},
		Extensions: []string{".txt"},
		Launch:     launch(systemdefs.SystemAppleI),
	},
	{
		Id:         systemdefs.SystemAppleII,
		SystemId:   systemdefs.SystemAppleII,
		Folders:    []string{"Apple-II"},
		Extensions: []string{".dsk", ".do", ".po", ".nib", ".hdv"},
		Launch:     launch(systemdefs.SystemAppleII),
	},
	{
		Id:         systemdefs.SystemAquarius,
		SystemId:   systemdefs.SystemAquarius,
		Folders:    []string{"AQUARIUS"},
		Extensions: []string{".bin", ".caq"},
		Launch:     launch(systemdefs.SystemAquarius),
	},
	{
		Id:         systemdefs.SystemAtari800,
		SystemId:   systemdefs.SystemAtari800,
		Folders:    []string{"ATARI800"},
		Extensions: []string{".atr", ".xex", ".xfd", ".atx", ".car", ".rom", ".bin"},
		Launch:     launch(systemdefs.SystemAtari800),
	},
	{
		Id:         systemdefs.SystemBBCMicro,
		SystemId:   systemdefs.SystemBBCMicro,
		Folders:    []string{"BBCMicro"},
		Extensions: []string{".ssd", ".dsd", ".vhd"},
		Launch:     launch(systemdefs.SystemBBCMicro),
	},
	{
		Id:         systemdefs.SystemBK0011M,
		SystemId:   systemdefs.SystemBK0011M,
		Folders:    []string{"BK0011M"},
		Extensions: []string{".bin", ".dsk", ".vhd"},
		Launch:     launch(systemdefs.SystemBK0011M),
	},
	{
		Id:         systemdefs.SystemC16,
		SystemId:   systemdefs.SystemC16,
		Folders:    []string{"C16"},
		Extensions: []string{".d64", ".g64", ".prg", ".tap", ".bin"},
		Launch:     launch(systemdefs.SystemC16),
	},
	{
		Id:         systemdefs.SystemC64,
		SystemId:   systemdefs.SystemC64,
		Folders:    []string{"C64"},
		Extensions: []string{".d64", ".g64", ".t64", ".d81", ".prg", ".crt", ".reu", ".tap"},
		Launch:     launch(systemdefs.SystemC64),
	},
	{
		Id:         systemdefs.SystemCasioPV2000,
		SystemId:   systemdefs.SystemCasioPV2000,
		Folders:    []string{"Casio_PV-2000"},
		Extensions: []string{".bin"},
		Launch:     launch(systemdefs.SystemCasioPV2000),
	},
	{
		Id:         systemdefs.SystemCoCo2,
		SystemId:   systemdefs.SystemCoCo2,
		Folders:    []string{"CoCo2"},
		Extensions: []string{".dsk", ".cas", ".ccc", ".rom"},
		Launch:     launch(systemdefs.SystemCoCo2),
	},
	{
		Id:         systemdefs.SystemEDSAC,
		SystemId:   systemdefs.SystemEDSAC,
		Folders:    []string{"EDSAC"},
		Extensions: []string{".tap"},
		Launch:     launch(systemdefs.SystemEDSAC),
	},
	{
		Id:         systemdefs.SystemGalaksija,
		SystemId:   systemdefs.SystemGalaksija,
		Folders:    []string{"Galaksija"},
		Extensions: []string{".tap"},
		Launch:     launch(systemdefs.SystemGalaksija),
	},
	{
		Id:         systemdefs.SystemInteract,
		SystemId:   systemdefs.SystemInteract,
		Folders:    []string{"Interact"},
		Extensions: []string{".cin", ".k7"},
		Launch:     launch(systemdefs.SystemInteract),
	},
	{
		Id:         systemdefs.SystemJupiter,
		SystemId:   systemdefs.SystemJupiter,
		Folders:    []string{"Jupiter"},
		Extensions: []string{".ace"},
		Launch:     launch(systemdefs.SystemJupiter),
	},
	{
		Id:         systemdefs.SystemLaser,
		SystemId:   systemdefs.SystemLaser,
		Folders:    []string{"Laser"},
		Extensions: []string{".vz"},
		Launch:     launch(systemdefs.SystemLaser),
	},
	{
		Id:         systemdefs.SystemLynx48,
		SystemId:   systemdefs.SystemLynx48,
		Folders:    []string{"Lynx48"},
		Extensions: []string{".tap"},
		Launch:     launch(systemdefs.SystemLynx48),
	},
	{
		Id:         systemdefs.SystemMacPlus,
		SystemId:   systemdefs.SystemMacPlus,
		Folders:    []string{"MACPLUS"},
		Extensions: []string{".dsk", ".img", ".vhd"},
		Launch:     launch(systemdefs.SystemMacPlus),
	},
	{
		Id:         systemdefs.SystemMSX,
		SystemId:   systemdefs.SystemMSX,
		Folders:    []string{"MSX"},
		Extensions: []string{".vhd"},
		Launch:     launch(systemdefs.SystemMSX),
	},
	{
		Id:         "MSX1",
		SystemId:   systemdefs.SystemMSX,
		Folders:    []string{"MSX1"},
		Extensions: []string{".dsk", ".rom"},
		Launch:     launchAltCore(systemdefs.SystemMSX, "_Console/MSX1"),
	},
	{
		Id:         systemdefs.SystemMultiComp,
		SystemId:   systemdefs.SystemMultiComp,
		Folders:    []string{"MultiComp"},
		Extensions: []string{".img"},
		Launch:     launch(systemdefs.SystemMultiComp),
	},
	{
		Id:         systemdefs.SystemOrao,
		SystemId:   systemdefs.SystemOrao,
		Folders:    []string{"ORAO"},
		Extensions: []string{".tap"},
		Launch:     launch(systemdefs.SystemOrao),
	},
	{
		Id:         systemdefs.SystemOric,
		SystemId:   systemdefs.SystemOric,
		Folders:    []string{"Oric"},
		Extensions: []string{".dsk"},
		Launch:     launch(systemdefs.SystemOric),
	},
	{
		Id:         systemdefs.SystemPCXT,
		SystemId:   systemdefs.SystemPCXT,
		Folders:    []string{"PCXT"},
		Extensions: []string{".img", ".vhd", ".ima", ".vfd"},
		Launch:     launch(systemdefs.SystemPCXT),
	},
	{
		Id:         systemdefs.SystemPDP1,
		SystemId:   systemdefs.SystemPDP1,
		Folders:    []string{"PDP1"},
		Extensions: []string{".bin", ".rim", ".pdp"},
		Launch:     launch(systemdefs.SystemPDP1),
	},
	{
		Id:         systemdefs.SystemPET2001,
		SystemId:   systemdefs.SystemPET2001,
		Folders:    []string{"PET2001"},
		Extensions: []string{".prg", ".tap"},
		Launch:     launch(systemdefs.SystemPET2001),
	},
	{
		Id:         systemdefs.SystemPMD85,
		SystemId:   systemdefs.SystemPMD85,
		Folders:    []string{"PMD85"},
		Extensions: []string{".rmm"},
		Launch:     launch(systemdefs.SystemPMD85),
	},
	{
		Id:         systemdefs.SystemQL,
		SystemId:   systemdefs.SystemQL,
		Folders:    []string{"QL"},
		Extensions: []string{".mdv", ".win"},
		Launch:     launch(systemdefs.SystemQL),
	},
	{
		Id:         systemdefs.SystemRX78,
		SystemId:   systemdefs.SystemRX78,
		Folders:    []string{"RX78"},
		Extensions: []string{".bin"},
		Launch:     launch(systemdefs.SystemRX78),
	},
	{
		Id:         systemdefs.SystemSAMCoupe,
		SystemId:   systemdefs.SystemSAMCoupe,
		Folders:    []string{"SAMCOUPE"},
		Extensions: []string{".dsk", ".mgt", ".img"},
		Launch:     launch(systemdefs.SystemSAMCoupe),
	},
	{
		Id:         systemdefs.SystemSordM5,
		SystemId:   systemdefs.SystemSordM5,
		Folders:    []string{"Sord M5"},
		Extensions: []string{".bin", ".rom", ".cas"},
		Launch:     launch(systemdefs.SystemSordM5),
	},
	{
		Id:         systemdefs.SystemSpecialist,
		SystemId:   systemdefs.SystemSpecialist,
		Folders:    []string{"SPMX"},
		Extensions: []string{".rks", ".odi"},
		Launch:     launch(systemdefs.SystemSpecialist),
	},
	{
		Id:         systemdefs.SystemSVI328,
		SystemId:   systemdefs.SystemSVI328,
		Folders:    []string{"SVI328"},
		Extensions: []string{".cas", ".bin", ".rom"},
		Launch:     launch(systemdefs.SystemSVI328),
	},
	{
		Id:         systemdefs.SystemTatungEinstein,
		SystemId:   systemdefs.SystemTatungEinstein,
		Folders:    []string{"TatungEinstein"},
		Extensions: []string{".dsk"},
		Launch:     launch(systemdefs.SystemTatungEinstein),
	},
	{
		Id:         systemdefs.SystemTI994A,
		SystemId:   systemdefs.SystemTI994A,
		Folders:    []string{"TI-99_4A"},
		Extensions: []string{".bin", ".m99"},
		Launch:     launch(systemdefs.SystemTI994A),
	},
	{
		Id:         systemdefs.SystemTomyTutor,
		SystemId:   systemdefs.SystemTomyTutor,
		Folders:    []string{"TomyTutor"},
		Extensions: []string{".bin", ".cas"},
		Launch:     launch(systemdefs.SystemTomyTutor),
	},
	{
		Id:         systemdefs.SystemTRS80,
		SystemId:   systemdefs.SystemTRS80,
		Folders:    []string{"TRS-80"},
		Extensions: []string{".jvi", ".dsk", ".cas"},
		Launch:     launch(systemdefs.SystemTRS80),
	},
	{
		Id:         systemdefs.SystemTSConf,
		SystemId:   systemdefs.SystemTSConf,
		Folders:    []string{"TSConf"},
		Extensions: []string{".vhf"},
		Launch:     launch(systemdefs.SystemTSConf),
	},
	{
		Id:         systemdefs.SystemUK101,
		SystemId:   systemdefs.SystemUK101,
		Folders:    []string{"UK101"},
		Extensions: []string{".txt", ".bas", ".lod"},
		Launch:     launch(systemdefs.SystemUK101),
	},
	{
		Id:         systemdefs.SystemVector06C,
		SystemId:   systemdefs.SystemVector06C,
		Folders:    []string{"VECTOR06"},
		Extensions: []string{".rom", ".com", ".c00", ".edd", ".fdd"},
		Launch:     launch(systemdefs.SystemVector06C),
	},
	{
		Id:         systemdefs.SystemVIC20,
		SystemId:   systemdefs.SystemVIC20,
		Folders:    []string{"VIC20"},
		Extensions: []string{".d64", ".g64", ".prg", ".tap", ".crt"},
		Launch:     launch(systemdefs.SystemVIC20),
	},
	{
		Id:         systemdefs.SystemX68000,
		SystemId:   systemdefs.SystemX68000,
		Folders:    []string{"X68000"},
		Extensions: []string{".d88", ".hdf"},
		Launch:     launch(systemdefs.SystemX68000),
	},
	{
		Id:         systemdefs.SystemZX81,
		SystemId:   systemdefs.SystemZX81,
		Folders:    []string{"ZX81"},
		Extensions: []string{".p", ".0"},
		Launch:     launch(systemdefs.SystemZX81),
	},
	{
		Id:         systemdefs.SystemZXSpectrum,
		SystemId:   systemdefs.SystemZXSpectrum,
		Folders:    []string{"Spectrum"},
		Extensions: []string{".tap", ".csw", ".tzx", ".sna", ".z80", ".trd", ".img", ".dsk", ".mgt"},
		Launch:     launch(systemdefs.SystemZXSpectrum),
	},
	{
		Id:         systemdefs.SystemZXNext,
		SystemId:   systemdefs.SystemZXNext,
		Folders:    []string{"ZXNext"},
		Extensions: []string{".vhd"},
		Launch:     launch(systemdefs.SystemZXNext),
	},
	// Other
	{
		Id:         systemdefs.SystemArcade,
		SystemId:   systemdefs.SystemArcade,
		Folders:    []string{"_Arcade"},
		Extensions: []string{".mra"},
		Launch:     launch(systemdefs.SystemArcade),
	},
	{
		Id:         systemdefs.SystemArduboy,
		SystemId:   systemdefs.SystemArduboy,
		Folders:    []string{"Arduboy"},
		Extensions: []string{".hex", ".bin"},
		Launch:     launch(systemdefs.SystemArduboy),
	},
	{
		Id:         systemdefs.SystemChip8,
		SystemId:   systemdefs.SystemChip8,
		Folders:    []string{"Chip8"},
		Extensions: []string{".ch8"},
		Launch:     launch(systemdefs.SystemChip8),
	},
	{
		Id:         systemdefs.SystemGroovy,
		SystemId:   systemdefs.SystemGroovy,
		Folders:    []string{"Groovy"},
		Extensions: []string{".gmc"},
		Launch:     launch(systemdefs.SystemGroovy),
	},
}
