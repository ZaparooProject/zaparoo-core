//go:build linux || darwin

package mister

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/gamesdb"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/rs/zerolog/log"
	"github.com/wizzomafizzo/mrext/pkg/games"
	"github.com/wizzomafizzo/mrext/pkg/mister"
)

func launch(cfg *config.Instance, path string) error {
	err := mister.LaunchGenericFile(UserConfigToMrext(cfg), path)
	if err != nil {
		return err
	}

	log.Debug().Msgf("setting active game: %s", path)
	return mister.SetActiveGame(path)
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

func launchMPlayer(pl Platform) func(*config.Instance, string) error {
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
		Launch:     launch,
	},
	// Consoles
	{
		Id:         gamesdb.SystemAdventureVision,
		SystemId:   gamesdb.SystemAdventureVision,
		Folders:    []string{"AVision"},
		Extensions: []string{".bin"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemArcadia,
		SystemId:   gamesdb.SystemArcadia,
		Folders:    []string{"Arcadia"},
		Extensions: []string{".bin"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemAstrocade,
		SystemId:   gamesdb.SystemAstrocade,
		Folders:    []string{"Astrocade"},
		Extensions: []string{".bin"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemAtari2600,
		SystemId:   gamesdb.SystemAtari2600,
		Folders:    []string{"ATARI7800", "Atari2600"},
		Extensions: []string{".a26"},
		Launch:     launch,
	},
	{
		Id:       "LLAPIAtari2600",
		SystemId: gamesdb.SystemAtari2600,
		Launch:   launchAltCore(gamesdb.SystemAtari2600, "_LLAPI/Atari7800_LLAPI"),
	},
	{
		Id:         gamesdb.SystemAtari5200,
		SystemId:   gamesdb.SystemAtari5200,
		Folders:    []string{"ATARI5200"},
		Extensions: []string{".a52"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemAtari7800,
		SystemId:   gamesdb.SystemAtari7800,
		Folders:    []string{"ATARI7800"},
		Extensions: []string{".a78"},
		Launch:     launch,
	},
	{
		Id:       "LLAPIAtari7800",
		SystemId: gamesdb.SystemAtari7800,
		Launch:   launchAltCore(gamesdb.SystemAtari7800, "_LLAPI/Atari7800_LLAPI"),
	},
	{
		Id:         gamesdb.SystemAtariLynx,
		SystemId:   gamesdb.SystemAtariLynx,
		Folders:    []string{"AtariLynx"},
		Extensions: []string{".lnx"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemCasioPV1000,
		SystemId:   gamesdb.SystemCasioPV1000,
		Folders:    []string{"Casio_PV-1000"},
		Extensions: []string{".bin"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemChannelF,
		SystemId:   gamesdb.SystemChannelF,
		Folders:    []string{"ChannelF"},
		Extensions: []string{".rom", ".bin"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemColecoVision,
		SystemId:   gamesdb.SystemColecoVision,
		Folders:    []string{"Coleco"},
		Extensions: []string{".col", ".bin", ".rom"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemCreatiVision,
		SystemId:   gamesdb.SystemCreatiVision,
		Folders:    []string{"CreatiVision"},
		Extensions: []string{".rom", ".bin", ".bas"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemFDS,
		SystemId:   gamesdb.SystemFDS,
		Folders:    []string{"NES", "FDS"},
		Extensions: []string{".fds"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemGamate,
		SystemId:   gamesdb.SystemGamate,
		Folders:    []string{"Gamate"},
		Extensions: []string{".bin"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemGameboy,
		SystemId:   gamesdb.SystemGameboy,
		Folders:    []string{"GAMEBOY"},
		Extensions: []string{".gb"},
		Launch:     launch,
	},
	{
		Id:       "LLAPIGameboy",
		SystemId: gamesdb.SystemGameboy,
		Launch:   launchAltCore(gamesdb.SystemGameboy, "_LLAPI/Gameboy_LLAPI"),
	},
	{
		Id:         gamesdb.SystemGameboyColor,
		SystemId:   gamesdb.SystemGameboyColor,
		Folders:    []string{"GAMEBOY", "GBC"},
		Extensions: []string{".gbc"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemGameboy2P,
		SystemId:   gamesdb.SystemGameboy2P,
		Folders:    []string{"GAMEBOY2P"},
		Extensions: []string{".gb", ".gbc"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemGameGear,
		SystemId:   gamesdb.SystemGameGear,
		Folders:    []string{"SMS", "GameGear"},
		Extensions: []string{".gg"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemGameNWatch,
		SystemId:   gamesdb.SystemGameNWatch,
		Folders:    []string{"GameNWatch"},
		Extensions: []string{".bin"},
		Launch:     launch,
	},
	{
		Id:         "GameAndWatch",
		SystemId:   gamesdb.SystemGameNWatch,
		Folders:    []string{"Game and Watch"},
		Extensions: []string{".gnw"},
		Launch:     launchAggGnw,
	},
	{
		Id:         gamesdb.SystemGBA,
		SystemId:   gamesdb.SystemGBA,
		Folders:    []string{"GBA"},
		Extensions: []string{".gba"},
		Launch:     launch,
	},
	{
		Id:       "LLAPIGBA",
		SystemId: gamesdb.SystemGBA,
		Launch:   launchAltCore(gamesdb.SystemGBA, "_LLAPI/GBA_LLAPI"),
	},
	{
		Id:         gamesdb.SystemGBA2P,
		SystemId:   gamesdb.SystemGBA2P,
		Folders:    []string{"GBA2P"},
		Extensions: []string{".gba"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemGenesis,
		SystemId:   gamesdb.SystemGenesis,
		Folders:    []string{"MegaDrive", "Genesis"},
		Extensions: []string{".gen", ".bin", ".md"},
		Launch:     launch,
	},
	{
		Id:       "SindenGenesis",
		SystemId: gamesdb.SystemGenesis,
		Launch:   launchSinden(gamesdb.SystemGenesis, "Genesis"),
	},
	{
		Id:       "SindenMegaDrive",
		SystemId: gamesdb.SystemGenesis,
		Launch:   launchSinden(gamesdb.SystemGenesis, "MegaDrive"),
	},
	{
		Id:       "LLAPIMegaDrive",
		SystemId: gamesdb.SystemGenesis,
		Launch:   launchAltCore(gamesdb.SystemGenesis, "_LLAPI/MegaDrive_LLAPI"),
	},
	{
		Id:         gamesdb.SystemIntellivision,
		SystemId:   gamesdb.SystemIntellivision,
		Folders:    []string{"Intellivision"},
		Extensions: []string{".int", ".bin"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemMasterSystem,
		SystemId:   gamesdb.SystemMasterSystem,
		Folders:    []string{"SMS"},
		Extensions: []string{".sms"},
		Launch:     launch,
	},
	{
		Id:       "SindenSMS",
		SystemId: gamesdb.SystemMasterSystem,
		Launch:   launchSinden(gamesdb.SystemMasterSystem, "SMS"),
	},
	{
		Id:       "LLAPISMS",
		SystemId: gamesdb.SystemMasterSystem,
		Launch:   launchAltCore(gamesdb.SystemMasterSystem, "_LLAPI/SMS_LLAPI"),
	},
	{
		Id:         gamesdb.SystemMegaCD,
		SystemId:   gamesdb.SystemMegaCD,
		Folders:    []string{"MegaCD"},
		Extensions: []string{".cue", ".chd"},
		Launch:     launch,
	},
	{
		Id:       "SindenMegaCD",
		SystemId: gamesdb.SystemMegaCD,
		Launch:   launchSinden(gamesdb.SystemMegaCD, "MegaCD"),
	},
	{
		Id:       "LLAPIMegaCD",
		SystemId: gamesdb.SystemMegaCD,
		Launch:   launchAltCore(gamesdb.SystemMegaCD, "_LLAPI/MegaCD_LLAPI"),
	},
	{
		Id:         gamesdb.SystemMegaDuck,
		SystemId:   gamesdb.SystemMegaDuck,
		Folders:    []string{"GAMEBOY", "MegaDuck"},
		Extensions: []string{".bin"},
		Launch:     launch,
	},
	{
		Id:       "LLAPINeoGeo",
		SystemId: gamesdb.SystemNeoGeo,
		Launch:   launchAltCore(gamesdb.SystemNeoGeo, "_LLAPI/NeoGeo_LLAPI"),
	},
	{
		Id:         gamesdb.SystemNeoGeoCD,
		SystemId:   gamesdb.SystemNeoGeoCD,
		Folders:    []string{"NeoGeo-CD", "NEOGEO"},
		Extensions: []string{".cue", ".chd"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemNES,
		SystemId:   gamesdb.SystemNES,
		Folders:    []string{"NES"},
		Extensions: []string{".nes"},
		Launch:     launch,
	},
	{
		Id:       "SindenNES",
		SystemId: gamesdb.SystemNES,
		Launch:   launchSinden(gamesdb.SystemNES, "NES"),
	},
	{
		Id:         gamesdb.SystemNESMusic,
		SystemId:   gamesdb.SystemNESMusic,
		Folders:    []string{"NES"},
		Extensions: []string{".nsf"},
		Launch:     launch,
	},
	{
		Id:       "LLAPINES",
		SystemId: gamesdb.SystemNES,
		Launch:   launchAltCore(gamesdb.SystemNES, "_LLAPI/NES_LLAPI"),
	},
	{
		Id:         gamesdb.SystemNintendo64,
		SystemId:   gamesdb.SystemNintendo64,
		Folders:    []string{"N64"},
		Extensions: []string{".n64", ".z64"},
		Launch:     launch,
	},
	{
		Id:       "LLAPINintendo64",
		SystemId: gamesdb.SystemNintendo64,
		Launch:   launchAltCore(gamesdb.SystemNintendo64, "_LLAPI/N64_LLAPI"),
	},
	{
		Id:       "LLAPI80MHzNintendo64",
		SystemId: gamesdb.SystemNintendo64,
		Launch:   launchAltCore(gamesdb.SystemNintendo64, "_LLAPI/N64_80MHz_LLAPI"),
	},
	{
		Id:       "80MHzNintendo64",
		SystemId: gamesdb.SystemNintendo64,
		Launch:   launchAltCore(gamesdb.SystemNintendo64, "_Console/N64_80MHz"),
	},
	{
		Id:       "PWMNintendo64",
		SystemId: gamesdb.SystemNintendo64,
		Launch:   launchAltCore(gamesdb.SystemNintendo64, "_ConsolePWM/N64_PWM"),
	},
	{
		Id:       "PWM80MHzNintendo64",
		SystemId: gamesdb.SystemNintendo64,
		Launch:   launchAltCore(gamesdb.SystemNintendo64, "_ConsolePWM/_Turbo/N64_80MHz_PWM"),
	},
	{
		Id:         gamesdb.SystemOdyssey2,
		SystemId:   gamesdb.SystemOdyssey2,
		Folders:    []string{"ODYSSEY2"},
		Extensions: []string{".bin"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemPocketChallengeV2,
		SystemId:   gamesdb.SystemPocketChallengeV2,
		Folders:    []string{"WonderSwan", "PocketChallengeV2"},
		Extensions: []string{".pc2"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemPokemonMini,
		SystemId:   gamesdb.SystemPokemonMini,
		Folders:    []string{"PokemonMini"},
		Extensions: []string{".min"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemPSX,
		SystemId:   gamesdb.SystemPSX,
		Folders:    []string{"PSX"},
		Extensions: []string{".cue", ".chd", ".exe"},
		Launch:     launch,
	},
	{
		Id:       "LLAPIPSX",
		SystemId: gamesdb.SystemPSX,
		Launch:   launchAltCore(gamesdb.SystemPSX, "_LLAPI/PSX_LLAPI"),
	},
	{
		Id:       "SindenPSX",
		SystemId: gamesdb.SystemPSX,
		Launch:   launchSinden(gamesdb.SystemPSX, "PSX"),
	},
	{
		Id:       "2XPSX",
		SystemId: gamesdb.SystemPSX,
		Launch:   launchAltCore(gamesdb.SystemPSX, "_Console/PSX2XCPU"),
	},
	{
		Id:       "PWMPSX",
		SystemId: gamesdb.SystemPSX,
		Launch:   launchAltCore(gamesdb.SystemPSX, "_ConsolePWM/PSX_PWM"),
	},
	{
		Id:       "PWM2XPSX",
		SystemId: gamesdb.SystemPSX,
		Launch:   launchAltCore(gamesdb.SystemPSX, "_ConsolePWM/_Turbo/PSX2XCPU_PWM"),
	},
	{
		Id:         gamesdb.SystemSega32X,
		SystemId:   gamesdb.SystemSega32X,
		Folders:    []string{"S32X"},
		Extensions: []string{".32x"},
		Launch:     launch,
	},
	{
		Id:       "LLAPIS32X",
		SystemId: gamesdb.SystemSega32X,
		Launch:   launchAltCore(gamesdb.SystemPSX, "_LLAPI/S32X_LLAPI"),
	},
	{
		Id:         gamesdb.SystemSG1000,
		SystemId:   gamesdb.SystemSG1000,
		Folders:    []string{"SG1000", "Coleco", "SMS"},
		Extensions: []string{".sg"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemSuperGameboy,
		SystemId:   gamesdb.SystemSuperGameboy,
		Folders:    []string{"SGB"},
		Extensions: []string{".sgb", ".gb", ".gbc"},
		Launch:     launch,
	},
	{
		Id:       "LLAPISuperGameboy",
		SystemId: gamesdb.SystemSuperGameboy,
		Launch:   launchAltCore(gamesdb.SystemSuperGameboy, "_LLAPI/SGB_LLAPI"),
	},
	{
		Id:         gamesdb.SystemSuperVision,
		SystemId:   gamesdb.SystemSuperVision,
		Folders:    []string{"SuperVision"},
		Extensions: []string{".bin", ".sv"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemSaturn,
		SystemId:   gamesdb.SystemSaturn,
		Folders:    []string{"Saturn"},
		Extensions: []string{".cue", ".chd"},
		Launch:     launch,
	},
	{
		Id:       "LLAPISaturn",
		SystemId: gamesdb.SystemSaturn,
		Launch:   launchAltCore(gamesdb.SystemSaturn, "_LLAPI/Saturn_LLAPI"),
	},
	{
		Id:       "PWMSaturn",
		SystemId: gamesdb.SystemPSX,
		Launch:   launchAltCore(gamesdb.SystemPSX, "_ConsolePWM/Saturn_PWM"),
	},
	{
		Id:         gamesdb.SystemSNES,
		SystemId:   gamesdb.SystemSNES,
		Folders:    []string{"SNES"},
		Extensions: []string{".sfc", ".smc", ".bin", ".bs"},
		Launch:     launch,
	},
	{
		Id:       "LLAPISNES",
		SystemId: gamesdb.SystemSNES,
		Launch:   launchAltCore(gamesdb.SystemSNES, "_LLAPI/SNES_LLAPI"),
	},
	{
		Id:       "SindenSNES",
		SystemId: gamesdb.SystemSNES,
		Launch:   launchSinden(gamesdb.SystemSNES, "SNES"),
	},
	{
		Id:         gamesdb.SystemSNESMusic,
		SystemId:   gamesdb.SystemSNESMusic,
		Folders:    []string{"SNES"},
		Extensions: []string{".spc"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemSuperGrafx,
		SystemId:   gamesdb.SystemSuperGrafx,
		Folders:    []string{"TGFX16"},
		Extensions: []string{".sgx"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemTurboGrafx16,
		SystemId:   gamesdb.SystemTurboGrafx16,
		Folders:    []string{"TGFX16"},
		Extensions: []string{".pce", ".bin"},
		Launch:     launch,
	},
	{
		Id:       "LLAPITurboGrafx16",
		SystemId: gamesdb.SystemTurboGrafx16,
		Launch:   launchAltCore(gamesdb.SystemTurboGrafx16, "_LLAPI/TurboGrafx16_LLAPI"),
	},
	{
		Id:         gamesdb.SystemTurboGrafx16CD,
		SystemId:   gamesdb.SystemTurboGrafx16CD,
		Folders:    []string{"TGFX16-CD"},
		Extensions: []string{".cue", ".chd"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemVC4000,
		SystemId:   gamesdb.SystemVC4000,
		Folders:    []string{"VC4000"},
		Extensions: []string{".bin"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemVectrex,
		SystemId:   gamesdb.SystemVectrex,
		Folders:    []string{"VECTREX"},
		Extensions: []string{".vec", ".bin", ".rom"}, // TODO: overlays (.ovr)
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemWonderSwan,
		SystemId:   gamesdb.SystemWonderSwan,
		Folders:    []string{"WonderSwan"},
		Extensions: []string{".ws"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemWonderSwanColor,
		SystemId:   gamesdb.SystemWonderSwanColor,
		Folders:    []string{"WonderSwan", "WonderSwanColor"},
		Extensions: []string{".wsc"},
		Launch:     launch,
	},
	// Computers
	{
		Id:         gamesdb.SystemAcornAtom,
		SystemId:   gamesdb.SystemAcornAtom,
		Folders:    []string{"AcornAtom"},
		Extensions: []string{".vhd"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemAcornElectron,
		SystemId:   gamesdb.SystemAcornElectron,
		Folders:    []string{"AcornElectron"},
		Extensions: []string{".vhd"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemAliceMC10,
		SystemId:   gamesdb.SystemAliceMC10,
		Folders:    []string{"AliceMC10"},
		Extensions: []string{".c10"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemAmstrad,
		SystemId:   gamesdb.SystemAmstrad,
		Folders:    []string{"Amstrad"},
		Extensions: []string{".dsk", ".cdt"}, // TODO: globbing support? for .e??
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemAmstradPCW,
		SystemId:   gamesdb.SystemAmstradPCW,
		Folders:    []string{"Amstrad PCW"},
		Extensions: []string{".dsk"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemDOS,
		SystemId:   gamesdb.SystemDOS,
		Folders:    []string{"AO486"},
		Extensions: []string{".img", ".ima", ".vhd", ".vfd", ".iso", ".cue", ".chd"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemApogee,
		SystemId:   gamesdb.SystemApogee,
		Folders:    []string{"APOGEE"},
		Extensions: []string{".rka", ".rkr", ".gam"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemAppleI,
		SystemId:   gamesdb.SystemAppleI,
		Folders:    []string{"Apple-I"},
		Extensions: []string{".txt"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemAppleII,
		SystemId:   gamesdb.SystemAppleII,
		Folders:    []string{"Apple-II"},
		Extensions: []string{".dsk", ".do", ".po", ".nib", ".hdv"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemAquarius,
		SystemId:   gamesdb.SystemAquarius,
		Folders:    []string{"AQUARIUS"},
		Extensions: []string{".bin", ".caq"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemAtari800,
		SystemId:   gamesdb.SystemAtari800,
		Folders:    []string{"ATARI800"},
		Extensions: []string{".atr", ".xex", ".xfd", ".atx", ".car", ".rom", ".bin"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemBBCMicro,
		SystemId:   gamesdb.SystemBBCMicro,
		Folders:    []string{"BBCMicro"},
		Extensions: []string{".ssd", ".dsd", ".vhd"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemBK0011M,
		SystemId:   gamesdb.SystemBK0011M,
		Folders:    []string{"BK0011M"},
		Extensions: []string{".bin", ".dsk", ".vhd"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemC16,
		SystemId:   gamesdb.SystemC16,
		Folders:    []string{"C16"},
		Extensions: []string{".d64", ".g64", ".prg", ".tap", ".bin"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemC64,
		SystemId:   gamesdb.SystemC64,
		Folders:    []string{"C64"},
		Extensions: []string{".d64", ".g64", ".t64", ".d81", ".prg", ".crt", ".reu", ".tap"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemCasioPV2000,
		SystemId:   gamesdb.SystemCasioPV2000,
		Folders:    []string{"Casio_PV-2000"},
		Extensions: []string{".bin"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemCoCo2,
		SystemId:   gamesdb.SystemCoCo2,
		Folders:    []string{"CoCo2"},
		Extensions: []string{".dsk", ".cas", ".ccc", ".rom"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemEDSAC,
		SystemId:   gamesdb.SystemEDSAC,
		Folders:    []string{"EDSAC"},
		Extensions: []string{".tap"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemGalaksija,
		SystemId:   gamesdb.SystemGalaksija,
		Folders:    []string{"Galaksija"},
		Extensions: []string{".tap"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemInteract,
		SystemId:   gamesdb.SystemInteract,
		Folders:    []string{"Interact"},
		Extensions: []string{".cin", ".k7"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemJupiter,
		SystemId:   gamesdb.SystemJupiter,
		Folders:    []string{"Jupiter"},
		Extensions: []string{".ace"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemLaser,
		SystemId:   gamesdb.SystemLaser,
		Folders:    []string{"Laser"},
		Extensions: []string{".vz"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemLynx48,
		SystemId:   gamesdb.SystemLynx48,
		Folders:    []string{"Lynx48"},
		Extensions: []string{".tap"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemMacPlus,
		SystemId:   gamesdb.SystemMacPlus,
		Folders:    []string{"MACPLUS"},
		Extensions: []string{".dsk", ".img", ".vhd"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemMSX,
		SystemId:   gamesdb.SystemMSX,
		Folders:    []string{"MSX"},
		Extensions: []string{".vhd"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemMultiComp,
		SystemId:   gamesdb.SystemMultiComp,
		Folders:    []string{"MultiComp"},
		Extensions: []string{".img"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemOrao,
		SystemId:   gamesdb.SystemOrao,
		Folders:    []string{"ORAO"},
		Extensions: []string{".tap"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemOric,
		SystemId:   gamesdb.SystemOric,
		Folders:    []string{"Oric"},
		Extensions: []string{".dsk"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemPCXT,
		SystemId:   gamesdb.SystemPCXT,
		Folders:    []string{"PCXT"},
		Extensions: []string{".img", ".vhd", ".ima", ".vfd"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemPDP1,
		SystemId:   gamesdb.SystemPDP1,
		Folders:    []string{"PDP1"},
		Extensions: []string{".bin", ".rim", ".pdp"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemPET2001,
		SystemId:   gamesdb.SystemPET2001,
		Folders:    []string{"PET2001"},
		Extensions: []string{".prg", ".tap"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemPMD85,
		SystemId:   gamesdb.SystemPMD85,
		Folders:    []string{"PMD85"},
		Extensions: []string{".rmm"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemQL,
		SystemId:   gamesdb.SystemQL,
		Folders:    []string{"QL"},
		Extensions: []string{".mdv", ".win"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemRX78,
		SystemId:   gamesdb.SystemRX78,
		Folders:    []string{"RX78"},
		Extensions: []string{".bin"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemSAMCoupe,
		SystemId:   gamesdb.SystemSAMCoupe,
		Folders:    []string{"SAMCOUPE"},
		Extensions: []string{".dsk", ".mgt", ".img"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemSordM5,
		SystemId:   gamesdb.SystemSordM5,
		Folders:    []string{"Sord M5"},
		Extensions: []string{".bin", ".rom", ".cas"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemSpecialist,
		SystemId:   gamesdb.SystemSpecialist,
		Folders:    []string{"SPMX"},
		Extensions: []string{".rks", ".odi"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemSVI328,
		SystemId:   gamesdb.SystemSVI328,
		Folders:    []string{"SVI328"},
		Extensions: []string{".cas", ".bin", ".rom"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemTatungEinstein,
		SystemId:   gamesdb.SystemTatungEinstein,
		Folders:    []string{"TatungEinstein"},
		Extensions: []string{".dsk"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemTI994A,
		SystemId:   gamesdb.SystemTI994A,
		Folders:    []string{"TI-99_4A"},
		Extensions: []string{".bin", ".m99"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemTomyTutor,
		SystemId:   gamesdb.SystemTomyTutor,
		Folders:    []string{"TomyTutor"},
		Extensions: []string{".bin", ".cas"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemTRS80,
		SystemId:   gamesdb.SystemTRS80,
		Folders:    []string{"TRS-80"},
		Extensions: []string{".jvi", ".dsk", ".cas"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemTSConf,
		SystemId:   gamesdb.SystemTSConf,
		Folders:    []string{"TSConf"},
		Extensions: []string{".vhf"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemUK101,
		SystemId:   gamesdb.SystemUK101,
		Folders:    []string{"UK101"},
		Extensions: []string{".txt", ".bas", ".lod"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemVector06C,
		SystemId:   gamesdb.SystemVector06C,
		Folders:    []string{"VECTOR06"},
		Extensions: []string{".rom", ".com", ".c00", ".edd", ".fdd"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemVIC20,
		SystemId:   gamesdb.SystemVIC20,
		Folders:    []string{"VIC20"},
		Extensions: []string{".d64", ".g64", ".prg", ".tap", ".crt"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemX68000,
		SystemId:   gamesdb.SystemX68000,
		Folders:    []string{"X68000"},
		Extensions: []string{".d88", ".hdf"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemZX81,
		SystemId:   gamesdb.SystemZX81,
		Folders:    []string{"ZX81"},
		Extensions: []string{".p", ".0"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemZXSpectrum,
		SystemId:   gamesdb.SystemZXSpectrum,
		Folders:    []string{"Spectrum"},
		Extensions: []string{".tap", ".csw", ".tzx", ".sna", ".z80", ".trd", ".img", ".dsk", ".mgt"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemZXNext,
		SystemId:   gamesdb.SystemZXNext,
		Folders:    []string{"ZXNext"},
		Extensions: []string{".vhd"},
		Launch:     launch,
	},
	// Other
	{
		Id:         gamesdb.SystemArcade,
		SystemId:   gamesdb.SystemArcade,
		Folders:    []string{"_Arcade"},
		Extensions: []string{".mra"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemArduboy,
		SystemId:   gamesdb.SystemArduboy,
		Folders:    []string{"Arduboy"},
		Extensions: []string{".hex", ".bin"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemChip8,
		SystemId:   gamesdb.SystemChip8,
		Folders:    []string{"Chip8"},
		Extensions: []string{".ch8"},
		Launch:     launch,
	},
	{
		Id:         gamesdb.SystemGroovy,
		SystemId:   gamesdb.SystemGroovy,
		Folders:    []string{"Groovy"},
		Extensions: []string{".gmc"},
		Launch:     launchGroovyCore(),
	},
}
