package windows

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/acr122pcsc"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/file"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/pn532uart"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/simpleserial"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	widgetmodels "github.com/ZaparooProject/zaparoo-core/pkg/ui/widgets/models"
	"github.com/adrg/xdg"
	"github.com/rs/zerolog/log"
)

type Platform struct {
	activeMedia    func() *models.ActiveMedia
	setActiveMedia func(*models.ActiveMedia)
}

func (*Platform) ID() string {
	return platforms.PlatformIDWindows
}

func (*Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	return []readers.Reader{
		file.NewReader(cfg),
		simpleserial.NewReader(cfg),
		acr122pcsc.NewAcr122Pcsc(cfg),
		pn532uart.NewReader(cfg),
	}
}

func (*Platform) StartPre(_ *config.Instance) error {
	return nil
}

func (p *Platform) StartPost(
	_ *config.Instance,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
) error {
	p.activeMedia = activeMedia
	p.setActiveMedia = setActiveMedia
	return nil
}

func (*Platform) Stop() error {
	return nil
}

func (*Platform) ScanHook(_ tokens.Token) error { //nolint:gocritic // unused parameter required by interface
	return nil
}

func (*Platform) RootDirs(cfg *config.Instance) []string {
	return cfg.IndexRoots()
}

func (*Platform) Settings() platforms.Settings {
	return platforms.Settings{
		DataDir:    filepath.Join(xdg.DataHome, config.AppName),
		ConfigDir:  filepath.Join(xdg.ConfigHome, config.AppName),
		TempDir:    filepath.Join(os.TempDir(), config.AppName),
		ZipsAsDirs: false,
	}
}

func (*Platform) NormalizePath(_ *config.Instance, path string) string {
	return path
}

func (p *Platform) StopActiveLauncher() error {
	p.setActiveMedia(nil)
	return nil
}

func (*Platform) PlayAudio(_ string) error {
	return nil
}

func (*Platform) LaunchSystem(_ *config.Instance, _ string) error {
	return errors.New("launching systems is not supported")
}

func (p *Platform) LaunchMedia(cfg *config.Instance, path string) error {
	log.Info().Msgf("launch media: %s", path)
	launcher, err := helpers.FindLauncher(cfg, p, path)
	if err != nil {
		return fmt.Errorf("launch media: error finding launcher: %w", err)
	}

	log.Info().Msgf("launch media: using launcher %s for: %s", launcher.ID, path)
	err = helpers.DoLaunch(cfg, p, p.setActiveMedia, &launcher, path)
	if err != nil {
		return fmt.Errorf("launch media: error launching: %w", err)
	}

	return nil
}

func (*Platform) KeyboardPress(_ string) error {
	return nil
}

func (*Platform) GamepadPress(_ string) error {
	return nil
}

func (*Platform) ForwardCmd(_ platforms.CmdEnv) (platforms.CmdResult, error) { //nolint:gocritic // unused parameter required by interface
	return platforms.CmdResult{}, nil
}

func (*Platform) LookupMapping(_ tokens.Token) (string, bool) { //nolint:gocritic // unused parameter required by interface
	return "", false
}

var lbSysMap = map[string]string{
	systemdefs.System3DO:               "3DO Interactive Multiplayer",
	systemdefs.SystemAmiga:             "Commodore Amiga",
	systemdefs.SystemAmstrad:           "Amstrad CPC",
	systemdefs.SystemAndroid:           "Android",
	systemdefs.SystemArcade:            "Arcade",
	systemdefs.SystemAtari2600:         "Atari 2600",
	systemdefs.SystemAtari5200:         "Atari 5200",
	systemdefs.SystemAtari7800:         "Atari 7800",
	systemdefs.SystemJaguar:            "Atari Jaguar",
	systemdefs.SystemJaguarCD:          "Atari Jaguar CD",
	systemdefs.SystemAtariLynx:         "Atari Lynx",
	systemdefs.SystemAtariXEGS:         "Atari XEGS",
	systemdefs.SystemColecoVision:      "ColecoVision",
	systemdefs.SystemC64:               "Commodore 64",
	systemdefs.SystemIntellivision:     "Mattel Intellivision",
	systemdefs.SystemIOS:               "Apple iOS",
	systemdefs.SystemMacOS:             "Apple Mac OS",
	systemdefs.SystemXbox:              "Microsoft Xbox",
	systemdefs.SystemXbox360:           "Microsoft Xbox 360",
	systemdefs.SystemXboxOne:           "Microsoft Xbox One",
	systemdefs.SystemNeoGeoPocket:      "SNK Neo Geo Pocket",
	systemdefs.SystemNeoGeoPocketColor: "SNK Neo Geo Pocket Color",
	systemdefs.SystemNeoGeo:            "SNK Neo Geo AES",
	systemdefs.System3DS:               "Nintendo 3DS",
	systemdefs.SystemNintendo64:        "Nintendo 64",
	systemdefs.SystemNDS:               "Nintendo DS",
	systemdefs.SystemNES:               "Nintendo Entertainment System",
	systemdefs.SystemGameboy:           "Nintendo Game Boy",
	systemdefs.SystemGBA:               "Nintendo Game Boy Advance",
	systemdefs.SystemGameboyColor:      "Nintendo Game Boy Color",
	systemdefs.SystemGameCube:          "Nintendo GameCube",
	systemdefs.SystemVirtualBoy:        "Nintendo Virtual Boy",
	systemdefs.SystemWii:               "Nintendo Wii",
	systemdefs.SystemWiiU:              "Nintendo Wii U",
	systemdefs.SystemOuya:              "Ouya",
	systemdefs.SystemCDI:               "Philips CD-i",
	systemdefs.SystemSega32X:           "Sega 32X",
	systemdefs.SystemMegaCD:            "Sega CD",
	systemdefs.SystemDreamcast:         "Sega Dreamcast",
	systemdefs.SystemGameGear:          "Sega Game Gear",
	systemdefs.SystemGenesis:           "Sega Genesis",
	systemdefs.SystemMasterSystem:      "Sega Master System",
	systemdefs.SystemSaturn:            "Sega Saturn",
	systemdefs.SystemZXSpectrum:        "Sinclair ZX Spectrum",
	systemdefs.SystemPSX:               "Sony Playstation",
	systemdefs.SystemPS2:               "Sony Playstation 2",
	systemdefs.SystemPS3:               "Sony Playstation 3",
	systemdefs.SystemPS4:               "Sony Playstation 4",
	systemdefs.SystemVita:              "Sony Playstation Vita",
	systemdefs.SystemPSP:               "Sony PSP",
	systemdefs.SystemSNES:              "Super Nintendo Entertainment System",
	systemdefs.SystemTurboGrafx16:      "NEC TurboGrafx-16",
	systemdefs.SystemWonderSwan:        "WonderSwan",
	systemdefs.SystemWonderSwanColor:   "WonderSwan Color",
	systemdefs.SystemOdyssey2:          "Magnavox Odyssey 2",
	systemdefs.SystemChannelF:          "Fairchild Channel F",
	systemdefs.SystemBBCMicro:          "BBC Microcomputer System",
	// systemdefs.REPLACE: "Memotech MTX512",
	// systemdefs.REPLACE: "Camputers Lynx",
	systemdefs.SystemGameCom:       "Tiger Game.com",
	systemdefs.SystemOric:          "Oric Atmos",
	systemdefs.SystemAcornElectron: "Acorn Electron",
	// systemdefs.REPLACE: "Dragon 32/64",
	systemdefs.SystemAdventureVision: "Entex Adventure Vision",
	// systemdefs.REPLACE: "APF Imagination Machine",
	systemdefs.SystemAquarius: "Mattel Aquarius",
	systemdefs.SystemJupiter:  "Jupiter Ace",
	systemdefs.SystemSAMCoupe: "SAM Coupé",
	// systemdefs.REPLACE: "Enterprise",
	// systemdefs.REPLACE: "EACA EG2000 Colour Genie",
	// systemdefs.REPLACE: "Acorn Archimedes",
	// systemdefs.REPLACE: "Tapwave Zodiac",
	// systemdefs.REPLACE: "Atari ST",
	systemdefs.SystemAstrocade: "Bally Astrocade",
	// systemdefs.REPLACE: "Magnavox Odyssey",
	systemdefs.SystemArcadia:     "Emerson Arcadia 2001",
	systemdefs.SystemSG1000:      "Sega SG-1000",
	systemdefs.SystemSuperVision: "Epoch Super Cassette Vision",
	systemdefs.SystemMSX:         "Microsoft MSX",
	systemdefs.SystemDOS:         "MS-DOS",
	systemdefs.SystemPC:          "Windows",
	// systemdefs.REPLACE: "Web Browser",
	// systemdefs.REPLACE: "Sega Model 2",
	// systemdefs.REPLACE: "Namco System 22",
	// systemdefs.REPLACE: "Sega Model 3",
	// systemdefs.REPLACE: "Sega System 32",
	// systemdefs.REPLACE: "Sega System 16",
	// systemdefs.REPLACE: "Sammy Atomiswave",
	// systemdefs.REPLACE: "Sega Naomi",
	// systemdefs.REPLACE: "Sega Naomi 2",
	systemdefs.SystemAtari800: "Atari 800",
	// systemdefs.REPLACE: "Sega Model 1",
	// systemdefs.REPLACE: "Sega Pico",
	systemdefs.SystemAcornAtom: "Acorn Atom",
	// systemdefs.REPLACE: "Amstrad GX4000",
	systemdefs.SystemAppleII: "Apple II",
	// systemdefs.REPLACE: "Apple IIGS",
	// systemdefs.REPLACE: "Casio Loopy",
	systemdefs.SystemCasioPV1000: "Casio PV-1000",
	// systemdefs.REPLACE: "Coleco ADAM",
	// systemdefs.REPLACE: "Commodore 128",
	// systemdefs.REPLACE: "Commodore Amiga CD32",
	// systemdefs.REPLACE: "Commodore CDTV",
	// systemdefs.REPLACE: "Commodore Plus 4",
	// systemdefs.REPLACE: "Commodore VIC-20",
	// systemdefs.REPLACE: "Fujitsu FM Towns Marty",
	systemdefs.SystemVectrex: "GCE Vectrex",
	// systemdefs.REPLACE: "Nuon",
	systemdefs.SystemMegaDuck: "Mega Duck",
	systemdefs.SystemX68000:   "Sharp X68000",
	systemdefs.SystemTRS80:    "Tandy TRS-80",
	// systemdefs.REPLACE: "Elektronika BK",
	// systemdefs.REPLACE: "Epoch Game Pocket Computer",
	// systemdefs.REPLACE: "Funtech Super Acan",
	// systemdefs.REPLACE: "GamePark GP32",
	// systemdefs.REPLACE: "Hartung Game Master",
	// systemdefs.REPLACE: "Interton VC 4000",
	// systemdefs.REPLACE: "MUGEN",
	// systemdefs.REPLACE: "OpenBOR",
	// systemdefs.REPLACE: "Philips VG 5000",
	// systemdefs.REPLACE: "Philips Videopac+",
	// systemdefs.REPLACE: "RCA Studio II",
	// systemdefs.REPLACE: "ScummVM",
	// systemdefs.REPLACE: "Sega Dreamcast VMU",
	// systemdefs.REPLACE: "Sega SC-3000",
	// systemdefs.REPLACE: "Sega ST-V",
	// systemdefs.REPLACE: "Sinclair ZX-81",
	systemdefs.SystemSordM5: "Sord M5",
	systemdefs.SystemTI994A: "Texas Instruments TI 99/4A",
	// systemdefs.REPLACE: "Pinball",
	systemdefs.SystemCreatiVision: "VTech CreatiVision",
	// systemdefs.REPLACE: "Watara Supervision",
	// systemdefs.REPLACE: "WoW Action Max",
	// systemdefs.REPLACE: "ZiNc",
	systemdefs.SystemFDS: "Nintendo Famicom Disk System",
	// systemdefs.REPLACE: "NEC PC-FX",
	systemdefs.SystemSuperGrafx:     "PC Engine SuperGrafx",
	systemdefs.SystemTurboGrafx16CD: "NEC TurboGrafx-CD",
	// systemdefs.REPLACE: "TRS-80 Color Computer",
	systemdefs.SystemGameNWatch: "Nintendo Game & Watch",
	systemdefs.SystemNeoGeoCD:   "SNK Neo Geo CD",
	// systemdefs.REPLACE: "Nintendo Satellaview",
	// systemdefs.REPLACE: "Taito Type X",
	// systemdefs.REPLACE: "XaviXPORT",
	// systemdefs.REPLACE: "Mattel HyperScan",
	// systemdefs.REPLACE: "Game Wave Family Entertainment System",
	// systemdefs.SystemSega32X: "Sega CD 32X",
	// systemdefs.REPLACE: "Aamber Pegasus",
	// systemdefs.REPLACE: "Apogee BK-01",
	// systemdefs.REPLACE: "Commodore MAX Machine",
	// systemdefs.REPLACE: "Commodore PET",
	// systemdefs.REPLACE: "Exelvision EXL 100",
	// systemdefs.REPLACE: "Exidy Sorcerer",
	// systemdefs.REPLACE: "Fujitsu FM-7",
	// systemdefs.REPLACE: "Hector HRX",
	// systemdefs.REPLACE: "Matra and Hachette Alice",
	// systemdefs.REPLACE: "Microsoft MSX2",
	// systemdefs.REPLACE: "Microsoft MSX2+",
	// systemdefs.REPLACE: "NEC PC-8801",
	// systemdefs.REPLACE: "NEC PC-9801",
	// systemdefs.REPLACE: "Nintendo 64DD",
	systemdefs.SystemPokemonMini: "Nintendo Pokemon Mini",
	// systemdefs.REPLACE: "Othello Multivision",
	// systemdefs.REPLACE: "VTech Socrates",
	systemdefs.SystemVector06C: "Vector-06C",
	systemdefs.SystemTomyTutor: "Tomy Tutor",
	// systemdefs.REPLACE: "Spectravideo",
	// systemdefs.REPLACE: "Sony PSP Minis",
	// systemdefs.REPLACE: "Sony PocketStation",
	// systemdefs.REPLACE: "Sharp X1",
	// systemdefs.REPLACE: "Sharp MZ-2500",
	// systemdefs.REPLACE: "Sega Triforce",
	// systemdefs.REPLACE: "Sega Hikaru",
	// systemdefs.SystemNeoGeo: "SNK Neo Geo MVS",
	systemdefs.SystemSwitch: "Nintendo Switch",
	// systemdefs.REPLACE: "Windows 3.X",
	// systemdefs.REPLACE: "Nokia N-Gage",
	// systemdefs.REPLACE: "GameWave",
	// systemdefs.REPLACE: "Linux",
	systemdefs.SystemPS5: "Sony Playstation 5",
	// systemdefs.REPLACE: "PICO-8",
	// systemdefs.REPLACE: "VTech V.Smile",
	systemdefs.SystemSeriesXS: "Microsoft Xbox Series X/S",
}

type LaunchBox struct {
	Games []LaunchBoxGame `xml:"Game"`
}

type LaunchBoxGame struct {
	Title string `xml:"Title"`
	ID    string `xml:"ID"`
}

func findLaunchBoxDir(cfg *config.Instance) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	dirs := []string{
		filepath.Join(home, "LaunchBox"),
		filepath.Join(home, "Documents", "LaunchBox"),
		filepath.Join(home, "My Games", "LaunchBox"),
		"C:\\Program Files (x86)\\LaunchBox",
		"C:\\Program Files\\LaunchBox",
		"C:\\LaunchBox",
		"D:\\LaunchBox",
		"E:\\LaunchBox",
	}

	def, ok := cfg.LookupLauncherDefaults("LaunchBox")
	if ok && def.InstallDir != "" {
		dirs = append([]string{def.InstallDir}, dirs...)
	}

	for _, dir := range dirs {
		if _, err := os.Stat(dir); err == nil {
			return dir, nil
		}
	}

	return "", errors.New("launchbox directory not found")
}

func (p *Platform) Launchers(cfg *config.Instance) []platforms.Launcher {
	launchers := []platforms.Launcher{
		{
			ID:       "Steam",
			SystemID: systemdefs.SystemPC,
			Schemes:  []string{"steam"},
			Scanner: func(
				_ *config.Instance,
				_ string,
				results []platforms.ScanResult,
			) ([]platforms.ScanResult, error) {
				// TODO: detect this path from registry
				root := "C:\\Program Files (x86)\\Steam\\steamapps"
				appResults, err := helpers.ScanSteamApps(root)
				if err != nil {
					return nil, err
				}
				return append(results, appResults...), nil
			},
			Launch: func(_ *config.Instance, path string) error {
				id := strings.TrimPrefix(path, "steam://")
				id = strings.TrimPrefix(id, "rungameid/")
				id = strings.SplitN(id, "/", 2)[0]
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				return exec.CommandContext(ctx,
					"cmd", "/c",
					"start",
					"steam://rungameid/"+id,
				).Start()
			},
		},
		{
			ID:       "Flashpoint",
			SystemID: systemdefs.SystemPC,
			Schemes:  []string{"flashpoint"},
			Launch: func(_ *config.Instance, path string) error {
				id := strings.TrimPrefix(path, "flashpoint://")
				id = strings.TrimPrefix(id, "run/")
				id = strings.SplitN(id, "/", 2)[0]
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				return exec.CommandContext(ctx,
					"cmd", "/c",
					"start",
					"flashpoint://run/"+id,
				).Start()
			},
		},
		{
			ID:            "Generic",
			Extensions:    []string{".exe", ".bat", ".cmd", ".lnk", ".a3x", ".ahk"},
			AllowListOnly: true,
			Launch: func(_ *config.Instance, path string) error {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				return exec.CommandContext(ctx, "cmd", "/c", path).Start()
			},
		},
		{
			ID:      "LaunchBox",
			Schemes: []string{"launchbox"},
			Scanner: func(
				cfg *config.Instance,
				systemId string,
				results []platforms.ScanResult,
			) ([]platforms.ScanResult, error) {
				lbSys, ok := lbSysMap[systemId]
				if !ok {
					return results, nil
				}

				lbDir, err := findLaunchBoxDir(cfg)
				if err != nil {
					return results, err
				}

				platformsDir := filepath.Join(lbDir, "Data", "Platforms")
				if _, statErr := os.Stat(lbDir); os.IsNotExist(statErr) {
					return results, errors.New("LaunchBox platforms dir not found")
				}

				xmlPath := filepath.Join(platformsDir, lbSys+".xml")
				if _, statErr := os.Stat(xmlPath); os.IsNotExist(statErr) {
					log.Debug().Msgf("LaunchBox platform xml not found: %s", xmlPath)
					return results, nil
				}

				xmlFile, err := os.Open(xmlPath)
				if err != nil {
					return results, err
				}
				defer func(xmlFile *os.File) {
					if closeErr := xmlFile.Close(); closeErr != nil {
						log.Warn().Err(closeErr).Msg("error closing xml file")
					}
				}(xmlFile)

				data, err := io.ReadAll(xmlFile)
				if err != nil {
					return results, err
				}

				var lbXML LaunchBox
				err = xml.Unmarshal(data, &lbXML)
				if err != nil {
					return results, err
				}

				for _, game := range lbXML.Games {
					results = append(results, platforms.ScanResult{
						Path: "launchbox://" + game.ID + "/" + game.Title,
						Name: game.Title,
					})
				}

				return results, nil
			},
			Launch: func(cfg *config.Instance, path string) error {
				lbDir, err := findLaunchBoxDir(cfg)
				if err != nil {
					return err
				}

				cliLauncher := filepath.Join(lbDir, "ThirdParty", "CLI_Launcher", "CLI_Launcher.exe")
				if _, err := os.Stat(cliLauncher); os.IsNotExist(err) {
					return errors.New("CLI_Launcher not found")
				}

				id := strings.TrimPrefix(path, "launchbox://")
				id = strings.SplitN(id, "/", 2)[0]
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				return exec.CommandContext(ctx, cliLauncher, "launch_by_id", id).Start()
			},
		},
	}

	return append(helpers.ParseCustomLaunchers(p, cfg.CustomLaunchers()), launchers...)
}

func (*Platform) ShowNotice(
	_ *config.Instance,
	_ widgetmodels.NoticeArgs,
) (func() error, time.Duration, error) {
	return nil, 0, nil
}

func (*Platform) ShowLoader(
	_ *config.Instance,
	_ widgetmodels.NoticeArgs,
) (func() error, error) {
	return nil, nil
}

func (*Platform) ShowPicker(
	_ *config.Instance,
	_ widgetmodels.PickerArgs,
) error {
	return nil
}
