//go:build windows

// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"sync"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/kodi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/acr122pcsc"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/externaldrive"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/file"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/mqtt"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/pn532"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/pn532uart"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/simpleserial"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/tty2oled"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	widgetmodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/widgets/models"
	"github.com/adrg/xdg"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows/registry"
)

type Platform struct {
	activeMedia    func() *models.ActiveMedia
	setActiveMedia func(*models.ActiveMedia)
	trackedProcess *os.Process
	processMu      sync.RWMutex
}

func (*Platform) ID() string {
	return platforms.PlatformIDWindows
}

func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	allReaders := []readers.Reader{
		pn532.NewReader(cfg),
		pn532uart.NewReader(cfg),
		file.NewReader(cfg),
		simpleserial.NewReader(cfg),
		acr122pcsc.NewAcr122Pcsc(cfg),
		tty2oled.NewReader(cfg, p),
		mqtt.NewReader(cfg),
		externaldrive.NewReader(cfg),
	}

	var enabled []readers.Reader
	for _, r := range allReaders {
		metadata := r.Metadata()
		if cfg.IsDriverEnabled(metadata.ID, metadata.DefaultEnabled) {
			enabled = append(enabled, r)
		}
	}
	return enabled
}

func (*Platform) StartPre(_ *config.Instance) error {
	return nil
}

func (p *Platform) StartPost(
	_ *config.Instance,
	_ platforms.LauncherContextManager,
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

func (*Platform) ScanHook(_ *tokens.Token) error {
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

func (p *Platform) SetTrackedProcess(proc *os.Process) {
	p.processMu.Lock()
	defer p.processMu.Unlock()

	// Kill any existing tracked process before setting new one
	if p.trackedProcess != nil {
		if err := p.trackedProcess.Kill(); err != nil {
			log.Warn().Err(err).Msg("failed to kill previous tracked process")
		}
	}

	p.trackedProcess = proc
	log.Debug().Msgf("set tracked process: %v", proc)
}

func (p *Platform) StopActiveLauncher(_ platforms.StopIntent) error {
	p.processMu.Lock()
	defer p.processMu.Unlock()

	// Kill tracked process if exists
	if p.trackedProcess != nil {
		if err := p.trackedProcess.Kill(); err != nil {
			log.Warn().Err(err).Msg("failed to kill tracked process")
		}
		p.trackedProcess = nil
		log.Debug().Msg("killed tracked process")
	}

	p.setActiveMedia(nil)
	return nil
}

func (*Platform) ReturnToMenu() error {
	// No menu concept on this platform
	return nil
}


func (*Platform) LaunchSystem(_ *config.Instance, _ string) error {
	return errors.New("launching systems is not supported")
}

func (p *Platform) LaunchMedia(cfg *config.Instance, path string, launcher *platforms.Launcher) error {
	log.Info().Msgf("launch media: %s", path)

	if launcher == nil {
		foundLauncher, err := helpers.FindLauncher(cfg, p, path)
		if err != nil {
			return fmt.Errorf("launch media: error finding launcher: %w", err)
		}
		launcher = &foundLauncher
	}

	log.Info().Msgf("launch media: using launcher %s for: %s", launcher.ID, path)
	err := helpers.DoLaunch(cfg, p, p.setActiveMedia, launcher, path)
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

func (*Platform) ForwardCmd(_ *platforms.CmdEnv) (platforms.CmdResult, error) {
	return platforms.CmdResult{}, nil
}

func (*Platform) LookupMapping(_ *tokens.Token) (string, bool) {
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

func findSteamDir(cfg *config.Instance) string {
	const fallbackPath = "C:\\Program Files (x86)\\Steam"

	// Check for user-configured Steam install directory first
	if def, ok := cfg.LookupLauncherDefaults("Steam"); ok && def.InstallDir != "" {
		if _, err := os.Stat(def.InstallDir); err == nil {
			log.Debug().Msgf("using user-configured Steam directory: %s", def.InstallDir)
			return def.InstallDir
		}
		log.Warn().Msgf("user-configured Steam directory not found: %s", def.InstallDir)
	}

	// Try 64-bit systems first (most common)
	paths := []string{
		`SOFTWARE\Wow6432Node\Valve\Steam`,
		`SOFTWARE\Valve\Steam`,
	}

	for _, path := range paths {
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}

		installPath, _, err := key.GetStringValue("InstallPath")
		if closeErr := key.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("error closing registry key")
		}
		if err != nil {
			continue
		}

		// Validate the path exists
		if _, statErr := os.Stat(installPath); statErr == nil {
			log.Debug().Msgf("found Steam installation via registry: %s", installPath)
			return installPath
		}
	}

	log.Debug().Msgf("Steam registry detection failed, using fallback: %s", fallbackPath)
	return fallbackPath
}

func findLaunchBoxDir(cfg *config.Instance) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
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
		kodi.NewKodiLocalLauncher(),
		kodi.NewKodiMovieLauncher(),
		kodi.NewKodiTVLauncher(),
		kodi.NewKodiMusicLauncher(),
		kodi.NewKodiSongLauncher(),
		kodi.NewKodiAlbumLauncher(),
		kodi.NewKodiArtistLauncher(),
		kodi.NewKodiTVShowLauncher(),
		{
			ID:       "Steam",
			SystemID: systemdefs.SystemPC,
			Schemes:  []string{"steam"},
			Scanner: func(
				_ context.Context,
				cfg *config.Instance,
				_ string,
				results []platforms.ScanResult,
			) ([]platforms.ScanResult, error) {
				steamRoot := findSteamDir(cfg)
				steamAppsRoot := filepath.Join(steamRoot, "steamapps")

				// Scan official Steam apps
				appResults, err := helpers.ScanSteamApps(steamAppsRoot)
				if err != nil {
					return nil, fmt.Errorf("failed to scan Steam apps: %w", err)
				}
				results = append(results, appResults...)

				// Scan non-Steam games (shortcuts)
				shortcutResults, err := helpers.ScanSteamShortcuts(steamRoot)
				if err != nil {
					log.Warn().Err(err).Msg("failed to scan Steam shortcuts, continuing without them")
				} else {
					results = append(results, shortcutResults...)
				}

				return results, nil
			},
			Launch: func(_ *config.Instance, path string) (*os.Process, error) {
				id := strings.TrimPrefix(path, "steam://")
				id = strings.TrimPrefix(id, "rungameid/")
				id = strings.SplitN(id, "/", 2)[0]
				//nolint:gosec // Safe: launches Steam with game ID from internal database
				cmd := exec.CommandContext(context.Background(),
					"cmd", "/c",
					"start",
					"steam://rungameid/"+id,
				)
				cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
				err := cmd.Start()
				if err != nil {
					return nil, fmt.Errorf("failed to start steam: %w", err)
				}
				return nil, nil //nolint:nilnil // Steam launches don't return a process handle
			},
		},
		{
			ID:       "Flashpoint",
			SystemID: systemdefs.SystemPC,
			Schemes:  []string{"flashpoint"},
			Launch: func(_ *config.Instance, path string) (*os.Process, error) {
				id := strings.TrimPrefix(path, "flashpoint://")
				id = strings.TrimPrefix(id, "run/")
				id = strings.SplitN(id, "/", 2)[0]
				//nolint:gosec // Safe: launches Flashpoint with game ID from internal database
				cmd := exec.CommandContext(context.Background(),
					"cmd", "/c",
					"start",
					"flashpoint://run/"+id,
				)
				cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
				err := cmd.Start()
				if err != nil {
					return nil, fmt.Errorf("failed to start flashpoint: %w", err)
				}
				return nil, nil //nolint:nilnil // Flashpoint launches don't return a process handle
			},
		},
		{
			ID:            "GenericExecutable",
			Extensions:    []string{".exe"},
			AllowListOnly: true,
			Lifecycle:     platforms.LifecycleBlocking, // Block for executables to track completion
			Launch: func(_ *config.Instance, path string) (*os.Process, error) {
				cmd := exec.CommandContext(context.Background(), path)
				cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
				if err := cmd.Start(); err != nil {
					return nil, fmt.Errorf("failed to start executable: %w", err)
				}
				return cmd.Process, nil
			},
		},
		{
			ID:            "GenericScript",
			Extensions:    []string{".bat", ".cmd", ".lnk", ".a3x", ".ahk"},
			AllowListOnly: true,
			Lifecycle:     platforms.LifecycleFireAndForget, // Fire-and-forget for scripts
			Launch: func(_ *config.Instance, path string) (*os.Process, error) {
				ext := strings.ToLower(filepath.Ext(path))
				var cmd *exec.Cmd
				// Extensions not in default PATHEXT need START command for proper execution
				if ext == ".lnk" || ext == ".a3x" || ext == ".ahk" {
					cmd = exec.CommandContext(context.Background(), "cmd", "/c", "start", "", path)
				} else {
					// .bat, .cmd work fine with direct execution
					cmd = exec.CommandContext(context.Background(), "cmd", "/c", path)
				}
				cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
				err := cmd.Start()
				if err != nil {
					return nil, fmt.Errorf("failed to start script: %w", err)
				}
				return nil, nil //nolint:nilnil // Script launches don't return a process handle
			},
		},
		{
			ID:      "LaunchBox",
			Schemes: []string{"launchbox"},
			Scanner: func(
				_ context.Context,
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

				//nolint:gosec // Safe: reads game database XML files from controlled directories
				xmlFile, err := os.Open(xmlPath)
				if err != nil {
					return results, fmt.Errorf("failed to open XML file %s: %w", xmlPath, err)
				}
				defer func(xmlFile *os.File) {
					if closeErr := xmlFile.Close(); closeErr != nil {
						log.Warn().Err(closeErr).Msg("error closing xml file")
					}
				}(xmlFile)

				data, err := io.ReadAll(xmlFile)
				if err != nil {
					return results, fmt.Errorf("failed to read XML file: %w", err)
				}

				var lbXML LaunchBox
				err = xml.Unmarshal(data, &lbXML)
				if err != nil {
					return results, fmt.Errorf("failed to unmarshal XML: %w", err)
				}

				for _, game := range lbXML.Games {
					results = append(results, platforms.ScanResult{
						Path:  helpers.CreateVirtualPath("launchbox", game.ID, game.Title),
						Name:  game.Title,
						NoExt: true,
					})
				}

				return results, nil
			},
			Launch: func(cfg *config.Instance, path string) (*os.Process, error) {
				lbDir, err := findLaunchBoxDir(cfg)
				if err != nil {
					return nil, err
				}

				cliLauncher := filepath.Join(lbDir, "ThirdParty", "CLI_Launcher", "CLI_Launcher.exe")
				if _, err := os.Stat(cliLauncher); os.IsNotExist(err) {
					return nil, errors.New("CLI_Launcher not found")
				}

				id := strings.TrimPrefix(path, "launchbox://")
				id = strings.SplitN(id, "/", 2)[0]
				//nolint:gosec // Safe: cliLauncher is validated file path, id comes from internal game database
				cmd := exec.CommandContext(context.Background(), cliLauncher, "launch_by_id", id)
				cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
				return nil, cmd.Start()
			},
		},
	}

	// Add RetroBat launchers if available
	retroBatLaunchers := getRetroBatLaunchers(cfg)
	launchers = append(launchers, retroBatLaunchers...)

	return append(helpers.ParseCustomLaunchers(p, cfg.CustomLaunchers()), launchers...)
}

func (*Platform) ShowNotice(
	_ *config.Instance,
	_ widgetmodels.NoticeArgs,
) (func() error, time.Duration, error) {
	return nil, 0, platforms.ErrNotSupported
}

func (*Platform) ShowLoader(
	_ *config.Instance,
	_ widgetmodels.NoticeArgs,
) (func() error, error) {
	return nil, platforms.ErrNotSupported
}

func (*Platform) ShowPicker(
	_ *config.Instance,
	_ widgetmodels.PickerArgs,
) error {
	return platforms.ErrNotSupported
}

func (*Platform) ConsoleManager() platforms.ConsoleManager {
	return platforms.NoOpConsoleManager{}
}
