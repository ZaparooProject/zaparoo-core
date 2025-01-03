package windows

import (
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"

	"github.com/ZaparooProject/zaparoo-core/pkg/database/gamesdb"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/acr122_pcsc"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/file"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/pn532_uart"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/simple_serial"
	"github.com/rs/zerolog/log"
)

type Platform struct {
}

func (p *Platform) Id() string {
	return "windows"
}

func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	return []readers.Reader{
		file.NewReader(cfg),
		simple_serial.NewReader(cfg),
		acr122_pcsc.NewAcr122Pcsc(cfg),
		pn532_uart.NewReader(cfg),
	}
}

func (p *Platform) StartPre(_ *config.Instance) error {
	return nil
}

func (p *Platform) StartPost(_ *config.Instance, _ chan<- models.Notification) error {
	return nil
}

func (p *Platform) Stop() error {
	return nil
}

func (p *Platform) AfterScanHook(token tokens.Token) error {
	return nil
}

func (p *Platform) ReadersUpdateHook(readers map[string]*readers.Reader) error {
	return nil
}

func (p *Platform) RootDirs(cfg *config.Instance) []string {
	return []string{}
}

func (p *Platform) ZipsAsDirs() bool {
	return false
}

func (p *Platform) DataDir() string {
	// TODO: this could be AppData instead
	return utils.ExeDir()
}

func (p *Platform) LogDir() string {
	return utils.ExeDir()
}

func (p *Platform) ConfigDir() string {
	return utils.ExeDir()
}

func (p *Platform) TempDir() string {
	return filepath.Join(os.TempDir(), config.AppName)
}

func (p *Platform) NormalizePath(cfg *config.Instance, path string) string {
	return path
}

func LaunchMenu() error {
	return nil
}

func (p *Platform) KillLauncher() error {
	return nil
}

func (p *Platform) GetActiveLauncher() string {
	return ""
}

func (p *Platform) PlayFailSound(cfg *config.Instance) {
}

func (p *Platform) PlaySuccessSound(cfg *config.Instance) {
}

func (p *Platform) ActiveSystem() string {
	return ""
}

func (p *Platform) ActiveGame() string {
	return ""
}

func (p *Platform) ActiveGameName() string {
	return ""
}

func (p *Platform) ActiveGamePath() string {
	return ""
}

func (p *Platform) LaunchSystem(cfg *config.Instance, id string) error {
	log.Info().Msgf("launching system: %s", id)
	return nil
}

func (p *Platform) LaunchFile(cfg *config.Instance, path string) error {
	log.Info().Msgf("launching file: %s", path)

	launchers := utils.PathToLaunchers(cfg, p, path)

	if len(launchers) == 0 {
		return errors.New("no launcher found")
	}

	// just pick the first one for now
	return launchers[0].Launch(cfg, path)
}

func (p *Platform) KeyboardInput(input string) error {
	return nil
}

func (p *Platform) KeyboardPress(name string) error {
	return nil
}

func (p *Platform) GamepadPress(name string) error {
	return nil
}

func (p *Platform) ForwardCmd(env platforms.CmdEnv) error {
	return nil
}

func (p *Platform) LookupMapping(_ tokens.Token) (string, bool) {
	return "", false
}

var lbSysMap = map[string]string{
	gamesdb.System3DO:               "3DO Interactive Multiplayer",
	gamesdb.SystemAmiga:             "Commodore Amiga",
	gamesdb.SystemAmstrad:           "Amstrad CPC",
	gamesdb.SystemAndroid:           "Android",
	gamesdb.SystemArcade:            "Arcade",
	gamesdb.SystemAtari2600:         "Atari 2600",
	gamesdb.SystemAtari5200:         "Atari 5200",
	gamesdb.SystemAtari7800:         "Atari 7800",
	gamesdb.SystemJaguar:            "Atari Jaguar",
	gamesdb.SystemJaguarCD:          "Atari Jaguar CD",
	gamesdb.SystemAtariLynx:         "Atari Lynx",
	gamesdb.SystemAtariXEGS:         "Atari XEGS",
	gamesdb.SystemColecoVision:      "ColecoVision",
	gamesdb.SystemC64:               "Commodore 64",
	gamesdb.SystemIntellivision:     "Mattel Intellivision",
	gamesdb.SystemIOS:               "Apple iOS",
	gamesdb.SystemMacOS:             "Apple Mac OS",
	gamesdb.SystemXbox:              "Microsoft Xbox",
	gamesdb.SystemXbox360:           "Microsoft Xbox 360",
	gamesdb.SystemXboxOne:           "Microsoft Xbox One",
	gamesdb.SystemNeoGeoPocket:      "SNK Neo Geo Pocket",
	gamesdb.SystemNeoGeoPocketColor: "SNK Neo Geo Pocket Color",
	gamesdb.SystemNeoGeo:            "SNK Neo Geo AES",
	gamesdb.System3DS:               "Nintendo 3DS",
	gamesdb.SystemNintendo64:        "Nintendo 64",
	gamesdb.SystemNDS:               "Nintendo DS",
	gamesdb.SystemNES:               "Nintendo Entertainment System",
	gamesdb.SystemGameboy:           "Nintendo Game Boy",
	gamesdb.SystemGBA:               "Nintendo Game Boy Advance",
	gamesdb.SystemGameboyColor:      "Nintendo Game Boy Color",
	gamesdb.SystemGameCube:          "Nintendo GameCube",
	gamesdb.SystemVirtualBoy:        "Nintendo Virtual Boy",
	gamesdb.SystemWii:               "Nintendo Wii",
	gamesdb.SystemWiiU:              "Nintendo Wii U",
	gamesdb.SystemOuya:              "Ouya",
	gamesdb.SystemCDI:               "Philips CD-i",
	gamesdb.SystemSega32X:           "Sega 32X",
	gamesdb.SystemMegaCD:            "Sega CD",
	gamesdb.SystemDreamcast:         "Sega Dreamcast",
	gamesdb.SystemGameGear:          "Sega Game Gear",
	gamesdb.SystemGenesis:           "Sega Genesis",
	gamesdb.SystemMasterSystem:      "Sega Master System",
	gamesdb.SystemSaturn:            "Sega Saturn",
	gamesdb.SystemZXSpectrum:        "Sinclair ZX Spectrum",
	gamesdb.SystemPSX:               "Sony Playstation",
	gamesdb.SystemPS2:               "Sony Playstation 2",
	gamesdb.SystemPS3:               "Sony Playstation 3",
	gamesdb.SystemPS4:               "Sony Playstation 4",
	gamesdb.SystemVita:              "Sony Playstation Vita",
	gamesdb.SystemPSP:               "Sony PSP",
	gamesdb.SystemSNES:              "Super Nintendo Entertainment System",
	gamesdb.SystemTurboGrafx16:      "NEC TurboGrafx-16",
	gamesdb.SystemWonderSwan:        "WonderSwan",
	gamesdb.SystemWonderSwanColor:   "WonderSwan Color",
	gamesdb.SystemOdyssey2:          "Magnavox Odyssey 2",
	gamesdb.SystemChannelF:          "Fairchild Channel F",
	gamesdb.SystemBBCMicro:          "BBC Microcomputer System",
	//gamesdb.REPLACE: "Memotech MTX512",
	//gamesdb.REPLACE: "Camputers Lynx",
	gamesdb.SystemGameCom:       "Tiger Game.com",
	gamesdb.SystemOric:          "Oric Atmos",
	gamesdb.SystemAcornElectron: "Acorn Electron",
	//gamesdb.REPLACE: "Dragon 32/64",
	gamesdb.SystemAdventureVision: "Entex Adventure Vision",
	//gamesdb.REPLACE: "APF Imagination Machine",
	gamesdb.SystemAquarius: "Mattel Aquarius",
	gamesdb.SystemJupiter:  "Jupiter Ace",
	gamesdb.SystemSAMCoupe: "SAM Coupé",
	//gamesdb.REPLACE: "Enterprise",
	//gamesdb.REPLACE: "EACA EG2000 Colour Genie",
	//gamesdb.REPLACE: "Acorn Archimedes",
	//gamesdb.REPLACE: "Tapwave Zodiac",
	//gamesdb.REPLACE: "Atari ST",
	gamesdb.SystemAstrocade: "Bally Astrocade",
	//gamesdb.REPLACE: "Magnavox Odyssey",
	gamesdb.SystemArcadia:     "Emerson Arcadia 2001",
	gamesdb.SystemSG1000:      "Sega SG-1000",
	gamesdb.SystemSuperVision: "Epoch Super Cassette Vision",
	gamesdb.SystemMSX:         "Microsoft MSX",
	gamesdb.SystemDOS:         "MS-DOS",
	gamesdb.SystemPC:          "Windows",
	//gamesdb.REPLACE: "Web Browser",
	//gamesdb.REPLACE: "Sega Model 2",
	//gamesdb.REPLACE: "Namco System 22",
	//gamesdb.REPLACE: "Sega Model 3",
	//gamesdb.REPLACE: "Sega System 32",
	//gamesdb.REPLACE: "Sega System 16",
	//gamesdb.REPLACE: "Sammy Atomiswave",
	//gamesdb.REPLACE: "Sega Naomi",
	//gamesdb.REPLACE: "Sega Naomi 2",
	gamesdb.SystemAtari800: "Atari 800",
	//gamesdb.REPLACE: "Sega Model 1",
	//gamesdb.REPLACE: "Sega Pico",
	gamesdb.SystemAcornAtom: "Acorn Atom",
	//gamesdb.REPLACE: "Amstrad GX4000",
	gamesdb.SystemAppleII: "Apple II",
	//gamesdb.REPLACE: "Apple IIGS",
	//gamesdb.REPLACE: "Casio Loopy",
	gamesdb.SystemCasioPV1000: "Casio PV-1000",
	//gamesdb.REPLACE: "Coleco ADAM",
	//gamesdb.REPLACE: "Commodore 128",
	//gamesdb.REPLACE: "Commodore Amiga CD32",
	//gamesdb.REPLACE: "Commodore CDTV",
	//gamesdb.REPLACE: "Commodore Plus 4",
	//gamesdb.REPLACE: "Commodore VIC-20",
	//gamesdb.REPLACE: "Fujitsu FM Towns Marty",
	gamesdb.SystemVectrex: "GCE Vectrex",
	//gamesdb.REPLACE: "Nuon",
	gamesdb.SystemMegaDuck: "Mega Duck",
	gamesdb.SystemX68000:   "Sharp X68000",
	gamesdb.SystemTRS80:    "Tandy TRS-80",
	//gamesdb.REPLACE: "Elektronika BK",
	//gamesdb.REPLACE: "Epoch Game Pocket Computer",
	//gamesdb.REPLACE: "Funtech Super Acan",
	//gamesdb.REPLACE: "GamePark GP32",
	//gamesdb.REPLACE: "Hartung Game Master",
	//gamesdb.REPLACE: "Interton VC 4000",
	//gamesdb.REPLACE: "MUGEN",
	//gamesdb.REPLACE: "OpenBOR",
	//gamesdb.REPLACE: "Philips VG 5000",
	//gamesdb.REPLACE: "Philips Videopac+",
	//gamesdb.REPLACE: "RCA Studio II",
	//gamesdb.REPLACE: "ScummVM",
	//gamesdb.REPLACE: "Sega Dreamcast VMU",
	//gamesdb.REPLACE: "Sega SC-3000",
	//gamesdb.REPLACE: "Sega ST-V",
	//gamesdb.REPLACE: "Sinclair ZX-81",
	gamesdb.SystemSordM5: "Sord M5",
	gamesdb.SystemTI994A: "Texas Instruments TI 99/4A",
	//gamesdb.REPLACE: "Pinball",
	gamesdb.SystemCreatiVision: "VTech CreatiVision",
	//gamesdb.REPLACE: "Watara Supervision",
	//gamesdb.REPLACE: "WoW Action Max",
	//gamesdb.REPLACE: "ZiNc",
	gamesdb.SystemFDS: "Nintendo Famicom Disk System",
	//gamesdb.REPLACE: "NEC PC-FX",
	gamesdb.SystemSuperGrafx:     "PC Engine SuperGrafx",
	gamesdb.SystemTurboGrafx16CD: "NEC TurboGrafx-CD",
	//gamesdb.REPLACE: "TRS-80 Color Computer",
	gamesdb.SystemGameNWatch: "Nintendo Game & Watch",
	gamesdb.SystemNeoGeoCD:   "SNK Neo Geo CD",
	//gamesdb.REPLACE: "Nintendo Satellaview",
	//gamesdb.REPLACE: "Taito Type X",
	//gamesdb.REPLACE: "XaviXPORT",
	//gamesdb.REPLACE: "Mattel HyperScan",
	//gamesdb.REPLACE: "Game Wave Family Entertainment System",
	//gamesdb.SystemSega32X: "Sega CD 32X",
	//gamesdb.REPLACE: "Aamber Pegasus",
	//gamesdb.REPLACE: "Apogee BK-01",
	//gamesdb.REPLACE: "Commodore MAX Machine",
	//gamesdb.REPLACE: "Commodore PET",
	//gamesdb.REPLACE: "Exelvision EXL 100",
	//gamesdb.REPLACE: "Exidy Sorcerer",
	//gamesdb.REPLACE: "Fujitsu FM-7",
	//gamesdb.REPLACE: "Hector HRX",
	//gamesdb.REPLACE: "Matra and Hachette Alice",
	//gamesdb.REPLACE: "Microsoft MSX2",
	//gamesdb.REPLACE: "Microsoft MSX2+",
	//gamesdb.REPLACE: "NEC PC-8801",
	//gamesdb.REPLACE: "NEC PC-9801",
	//gamesdb.REPLACE: "Nintendo 64DD",
	gamesdb.SystemPokemonMini: "Nintendo Pokemon Mini",
	//gamesdb.REPLACE: "Othello Multivision",
	//gamesdb.REPLACE: "VTech Socrates",
	gamesdb.SystemVector06C: "Vector-06C",
	gamesdb.SystemTomyTutor: "Tomy Tutor",
	//gamesdb.REPLACE: "Spectravideo",
	//gamesdb.REPLACE: "Sony PSP Minis",
	//gamesdb.REPLACE: "Sony PocketStation",
	//gamesdb.REPLACE: "Sharp X1",
	//gamesdb.REPLACE: "Sharp MZ-2500",
	//gamesdb.REPLACE: "Sega Triforce",
	//gamesdb.REPLACE: "Sega Hikaru",
	//gamesdb.SystemNeoGeo: "SNK Neo Geo MVS",
	gamesdb.SystemSwitch: "Nintendo Switch",
	//gamesdb.REPLACE: "Windows 3.X",
	//gamesdb.REPLACE: "Nokia N-Gage",
	//gamesdb.REPLACE: "GameWave",
	//gamesdb.REPLACE: "Linux",
	gamesdb.SystemPS5: "Sony Playstation 5",
	//gamesdb.REPLACE: "PICO-8",
	//gamesdb.REPLACE: "VTech V.Smile",
	gamesdb.SystemSeriesXS: "Microsoft Xbox Series X/S",
}

type LaunchBox struct {
	Games []LaunchBoxGame `xml:"Game"`
}

type LaunchBoxGame struct {
	Title string `xml:"Title"`
	ID    string `xml:"ID"`
}

func findLaunchBoxDir() (string, error) {
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

	for _, dir := range dirs {
		if _, err := os.Stat(dir); err == nil {
			return dir, nil
		}
	}

	return "", fmt.Errorf("launchbox directory not found")
}

func (p *Platform) Launchers() []platforms.Launcher {
	return []platforms.Launcher{
		{
			Id:       "Steam",
			SystemId: gamesdb.SystemPC,
			Schemes:  []string{"steam"},
			Scanner: func(
				cfg *config.Instance,
				systemId string,
				results []platforms.ScanResult,
			) ([]platforms.ScanResult, error) {
				// TODO: detect this path from registry
				root := "C:\\Program Files (x86)\\Steam\\steamapps"
				appResults, err := utils.ScanSteamApps(root)
				if err != nil {
					return nil, err
				}
				return append(results, appResults...), nil
			},
			Launch: func(cfg *config.Instance, path string) error {
				id := strings.TrimPrefix(path, "steam://")
				id = strings.TrimPrefix(id, "rungameid/")
				return exec.Command(
					"cmd", "/c",
					"start",
					"steam://rungameid/"+id,
				).Start()
			},
		},
		{
			Id:       "Flashpoint",
			SystemId: gamesdb.SystemPC,
			Schemes:  []string{"flashpoint"},
			Launch: func(cfg *config.Instance, path string) error {
				id := strings.TrimPrefix(path, "flashpoint://")
				id = strings.TrimPrefix(id, "run/")
				return exec.Command(
					"cmd", "/c",
					"start",
					"flashpoint://run/"+id,
				).Start()
			},
		},
		{
			Id:            "Generic",
			Extensions:    []string{".exe", ".bat", ".cmd", ".lnk", ".a3x", ".ahk"},
			AllowListOnly: true,
			Launch: func(cfg *config.Instance, path string) error {
				return exec.Command("cmd", "/c", path).Start()
			},
		},
		{
			Id:      "LaunchBox",
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

				lbDir, err := findLaunchBoxDir()
				if err != nil {
					return results, err
				}

				platformsDir := filepath.Join(lbDir, "Data", "Platforms")
				if _, err := os.Stat(lbDir); os.IsNotExist(err) {
					return results, errors.New("LaunchBox platforms dir not found")
				}

				xmlPath := filepath.Join(platformsDir, lbSys+".xml")
				if _, err := os.Stat(xmlPath); os.IsNotExist(err) {
					log.Debug().Msgf("LaunchBox platform xml not found: %s", xmlPath)
					return results, nil
				}

				xmlFile, err := os.Open(xmlPath)
				if err != nil {
					return results, err
				}
				defer func(xmlFile *os.File) {
					err := xmlFile.Close()
					if err != nil {
						log.Warn().Err(err).Msg("error closing xml file")
					}
				}(xmlFile)

				data, err := io.ReadAll(xmlFile)
				if err != nil {
					return results, err
				}

				var lbXml LaunchBox
				err = xml.Unmarshal(data, &lbXml)
				if err != nil {
					return results, err
				}

				for _, game := range lbXml.Games {
					results = append(results, platforms.ScanResult{
						Path: "launchbox://" + game.ID,
						Name: game.Title,
					})
				}

				return results, nil
			},
			Launch: func(cfg *config.Instance, path string) error {
				lbDir, err := findLaunchBoxDir()
				if err != nil {
					return err
				}

				cliLauncher := filepath.Join(lbDir, "ThirdParty", "CLI_Launcher", "CLI_Launcher.exe")
				if _, err := os.Stat(cliLauncher); os.IsNotExist(err) {
					return errors.New("CLI_Launcher not found")
				}

				id := strings.TrimPrefix(path, "launchbox://")
				return exec.Command(cliLauncher, "launch_by_id", id).Start()
			},
		},
	}
}
