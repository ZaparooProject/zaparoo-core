//go:build windows

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
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
	"bufio"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v4/process"
)

const (
	launchBoxPipeName = `\\.\pipe\zaparoo-launchbox-ipc`

	// launchBoxScannerMaxBuffer is the maximum buffer size for reading from the LaunchBox pipe.
	// Must be large enough to handle JSON responses for platforms with thousands of games
	// (e.g., NES with 8888 games can produce ~3MB responses).
	launchBoxScannerMaxBuffer = 16 * 1024 * 1024 // 16MB

	// launchBoxStopTimeout bounds how long Core waits for the plugin to report
	// the result of a stop. LaunchBox runs shutdown screens and per-emulator
	// exit scripts, so this allows for more than a bare process kill, while
	// still leaving room inside the platform's overall stop budget for the
	// process-tree fallback.
	launchBoxStopTimeout = 12 * time.Second

	// launchBoxStopProtocolVersion is the first plugin protocol version that
	// understands the Stop command. Plugins that report nothing predate the
	// handshake and are treated as unable to stop.
	launchBoxStopProtocolVersion = 2
)

// errLaunchBoxStopUnsupported reports that the connected plugin is too old to
// stop a game. Core falls back to killing the reported process tree.
var errLaunchBoxStopUnsupported = errors.New("LaunchBox plugin does not support stopping games")

// Plugin message types (matches C# plugin JSON structure)
//
//nolint:tagliatelle // JSON tags must match C# plugin structure (PascalCase)
type pluginEvent struct {
	Event           string `json:"Event"`
	ID              string `json:"Id,omitempty"`
	Title           string `json:"Title,omitempty"`
	Platform        string `json:"Platform,omitempty"`
	ApplicationPath string `json:"ApplicationPath,omitempty"`
	// Pid is the process LaunchBox started for this game, when the plugin
	// could resolve it. Zero means unknown.
	Pid int `json:"Pid,omitempty"`
}

// The Event field on the structs below is not read during dispatch, which
// happens on the raw event name before unmarshalling. It is kept so each type
// documents the wire message it maps to and round-trips correctly.

// launchBoxHelloEvent is the plugin's handshake, announcing what it can do.
//
//nolint:tagliatelle // JSON tags must match C# plugin structure (PascalCase)
type launchBoxHelloEvent struct {
	Event           string `json:"Event"`
	PluginVersion   string `json:"PluginVersion,omitempty"`
	ProtocolVersion int    `json:"ProtocolVersion,omitempty"`
}

// launchBoxProcessEvent reports the process LaunchBox started for a game,
// sent separately from MediaStarted because resolving it takes a moment and
// media publication must not wait on it.
//
//nolint:tagliatelle // JSON tags must match C# plugin structure (PascalCase)
type launchBoxProcessEvent struct {
	Event string `json:"Event"`
	ID    string `json:"Id,omitempty"`
	Pid   int    `json:"Pid,omitempty"`
}

// launchBoxStopResultEvent reports the outcome of a Stop command.
// Status is one of "completed", "failed" or "unsupported".
//
//nolint:tagliatelle // JSON tags must match C# plugin structure (PascalCase)
type launchBoxStopResultEvent struct {
	Event  string `json:"Event"`
	ID     string `json:"Id,omitempty"`
	Status string `json:"Status,omitempty"`
	Error  string `json:"Error,omitempty"`
}

// launchBoxErrorEvent is the plugin reporting that a command failed.
//
//nolint:tagliatelle // JSON tags must match C# plugin structure (PascalCase)
type launchBoxErrorEvent struct {
	Event   string `json:"Event"`
	Command string `json:"Command,omitempty"`
	Error   string `json:"Error,omitempty"`
}

//nolint:tagliatelle // JSON tags must match C# plugin structure (PascalCase)
type pluginCommand struct {
	Command  string `json:"Command"`
	ID       string `json:"Id,omitempty"`
	Platform string `json:"Platform,omitempty"`
}

//nolint:tagliatelle // JSON tags must match C# plugin structure (PascalCase)
type launchBoxPlatformInfo struct {
	Name     string `json:"Name"`
	ScrapeAs string `json:"ScrapeAs"`
}

//nolint:tagliatelle // JSON tags must match C# plugin structure (PascalCase)
type launchBoxPlatformsEvent struct {
	Event     string                  `json:"Event"`
	Platforms []launchBoxPlatformInfo `json:"Platforms"`
}

// LaunchBoxAdditionalApp represents an additional application for a LaunchBox game
//
//nolint:tagliatelle // JSON tags must match C# plugin structure (PascalCase)
type LaunchBoxAdditionalApp struct {
	ID   string `json:"Id"`
	Name string `json:"Name"`
}

// LaunchBoxGameInfo represents a game from the LaunchBox plugin
//
//nolint:tagliatelle // JSON tags must match C# plugin structure (PascalCase)
type LaunchBoxGameInfo struct {
	ID             string                   `json:"Id"`
	Title          string                   `json:"Title"`
	Platform       string                   `json:"Platform"`
	AdditionalApps []LaunchBoxAdditionalApp `json:"AdditionalApps"`
}

//nolint:tagliatelle // JSON tags must match C# plugin structure (PascalCase)
type launchBoxGamesEvent struct {
	Event    string              `json:"Event"`
	Platform string              `json:"Platform"`
	Error    string              `json:"Error,omitempty"`
	Games    []LaunchBoxGameInfo `json:"Games"`
}

// launchBoxGamesResponse is used internally for the synchronous request channel
type launchBoxGamesResponse struct {
	Error string
	Games []LaunchBoxGameInfo
}

// LaunchBox XML types
type launchBoxXML struct {
	Games []launchBoxGame `xml:"Game"`
}

type launchBoxGame struct {
	Title string `xml:"Title"`
	ID    string `xml:"ID"`
}

// lbSysMap maps Zaparoo system IDs to LaunchBox platform names
var lbSysMap = map[string]string{
	systemdefs.System3DO:           "3DO Interactive Multiplayer",
	systemdefs.SystemAcornAtom:     "Acorn Atom",
	systemdefs.SystemAcornElectron: "Acorn Electron",
	systemdefs.SystemArchimedes:    "Acorn Archimedes",
	// "APF Imagination Machine",         // No Zaparoo system
	// "Aamber Pegasus",                  // No Zaparoo system
	systemdefs.SystemAliceMC10:     "Matra and Hachette Alice",
	systemdefs.SystemAmiga:         "Commodore Amiga",
	systemdefs.SystemAmigaCD32:     "Commodore Amiga CD32",
	systemdefs.SystemAmstrad:       "Amstrad CPC",
	systemdefs.SystemAmstradGX4000: "Amstrad GX4000",
	systemdefs.SystemAndroid:       "Android",
	systemdefs.SystemApogee:        "Apogee BK-01",
	systemdefs.SystemAppleII:       "Apple II",
	systemdefs.SystemAppleIIGS:     "Apple IIGS",
	systemdefs.SystemIOS:           "Apple iOS",
	systemdefs.SystemMacOS:         "Apple Mac OS",
	systemdefs.SystemAquarius:      "Mattel Aquarius",
	systemdefs.SystemArcade:        "Arcade",
	systemdefs.SystemArduboy:       "Arduboy",
	systemdefs.SystemAtari2600:     "Atari 2600",
	systemdefs.SystemAtari5200:     "Atari 5200",
	systemdefs.SystemAtari7800:     "Atari 7800",
	systemdefs.SystemAtari800:      "Atari 800",
	systemdefs.SystemJaguar:        "Atari Jaguar",
	systemdefs.SystemJaguarCD:      "Atari Jaguar CD",
	systemdefs.SystemAtariLynx:     "Atari Lynx",
	systemdefs.SystemAtariST:       "Atari ST",
	systemdefs.SystemAtariXEGS:     "Atari XEGS",
	systemdefs.SystemAstrocade:     "Bally Astrocade",
	// "Bandai Super Vision 8000",        // No Zaparoo system
	systemdefs.SystemBBCMicro: "BBC Microcomputer System",
	// "Camputers Lynx",                  // No Zaparoo system
	systemdefs.SystemCasioPV1000:  "Casio PV-1000",
	systemdefs.SystemCasioLoopy:   "Casio Loopy",
	systemdefs.SystemColecoAdam:   "Coleco ADAM",
	systemdefs.SystemColecoVision: "ColecoVision",
	systemdefs.SystemC64:          "Commodore 64",
	// "Commodore 128",                   // No Zaparoo system
	systemdefs.SystemCommodoreCDTV: "Commodore CDTV",
	// "Commodore MAX Machine",           // No Zaparoo system
	systemdefs.SystemPET2001:        "Commodore PET",
	systemdefs.SystemCommodorePlus4: "Commodore Plus 4",
	systemdefs.SystemVIC20:          "Commodore VIC-20",
	systemdefs.SystemDragon32:       "Dragon 32/64",
	// "EACA EG2000 Colour Genie",        // No Zaparoo system
	systemdefs.SystemElektorTVGamesComputer: "Elektor TV Games Computer",
	systemdefs.SystemBK0011M:                "Elektronika BK",
	systemdefs.SystemArcadia:                "Emerson Arcadia 2001",
	// "Enterprise",                      // No Zaparoo system
	systemdefs.SystemAdventureVision:     "Entex Adventure Vision",
	systemdefs.SystemSuperCassetteVision: "Epoch Super Cassette Vision",
	systemdefs.SystemGamePocket:          "Epoch Game Pocket Computer",
	// "Exelvision EXL 100",              // No Zaparoo system
	// "Exidy Sorcerer",                  // No Zaparoo system
	systemdefs.SystemChannelF:  "Fairchild Channel F",
	systemdefs.SystemFM7:       "Fujitsu FM-7",
	systemdefs.SystemFMTowns:   "Fujitsu FM Towns Marty",
	systemdefs.SystemSuperACan: "Funtech Super Acan",
	// "Game Wave Family Entertainment System", // No Zaparoo system
	systemdefs.SystemGP32: "GamePark GP32",
	// "GameWave",                        // No Zaparoo system
	systemdefs.SystemVectrex:    "GCE Vectrex",
	systemdefs.SystemGameMaster: "Hartung Game Master",
	// "Hector HRX",                      // No Zaparoo system
	systemdefs.SystemVC4000:          "Interton VC 4000",
	systemdefs.SystemJupiter:         "Jupiter Ace",
	systemdefs.SystemLinux:           "Linux",
	systemdefs.SystemOdyssey2:        "Magnavox Odyssey 2",
	systemdefs.SystemMagnavoxOdyssey: "Magnavox Odyssey",
	systemdefs.SystemHyperScan:       "Mattel HyperScan",
	systemdefs.SystemIntellivision:   "Mattel Intellivision",
	systemdefs.SystemMegaDuck:        "Mega Duck",
	// "Memotech MTX512",                 // No Zaparoo system
	systemdefs.SystemDOS:      "MS-DOS",
	systemdefs.SystemMSX:      "Microsoft MSX",
	systemdefs.SystemMSX2:     "Microsoft MSX2",
	systemdefs.SystemMSX2Plus: "Microsoft MSX2+",
	systemdefs.SystemXbox:     "Microsoft Xbox",
	systemdefs.SystemXbox360:  "Microsoft Xbox 360",
	systemdefs.SystemXboxOne:  "Microsoft Xbox One",
	systemdefs.SystemSeriesXS: "Microsoft Xbox Series X/S",
	// "MUGEN",                           // No Zaparoo system
	systemdefs.SystemNamco22:        "Namco System 22",
	systemdefs.SystemPC88:           "NEC PC-8801",
	systemdefs.SystemPC98:           "NEC PC-9801",
	systemdefs.SystemPCFX:           "NEC PC-FX",
	systemdefs.SystemTurboGrafx16:   "NEC TurboGrafx-16",
	systemdefs.SystemTurboGrafx16CD: "NEC TurboGrafx-CD",
	systemdefs.System3DS:            "Nintendo 3DS",
	systemdefs.SystemNintendo64:     "Nintendo 64",
	// "Nintendo 64DD",                   // Part of Nintendo64 system
	systemdefs.SystemNDS:          "Nintendo DS",
	systemdefs.SystemNES:          "Nintendo Entertainment System",
	systemdefs.SystemFDS:          "Nintendo Famicom Disk System",
	systemdefs.SystemGameboy:      "Nintendo Game Boy",
	systemdefs.SystemGBA:          "Nintendo Game Boy Advance",
	systemdefs.SystemGameboyColor: "Nintendo Game Boy Color",
	systemdefs.SystemGameNWatch:   "Nintendo Game & Watch",
	systemdefs.SystemGameCube:     "Nintendo GameCube",
	systemdefs.SystemPokemonMini:  "Nintendo Pokemon Mini",
	// "Nintendo Satellaview",            // No Zaparoo system
	systemdefs.SystemSwitch:          "Nintendo Switch",
	systemdefs.SystemNintendoSwitch2: "Nintendo Switch 2",
	systemdefs.SystemVirtualBoy:      "Nintendo Virtual Boy",
	systemdefs.SystemWii:             "Nintendo Wii",
	systemdefs.SystemWiiU:            "Nintendo Wii U",
	systemdefs.SystemNGage:           "Nokia N-Gage",
	systemdefs.SystemNuon:            "Nuon",
	systemdefs.SystemOpenBOR:         "OpenBOR",
	systemdefs.SystemOric:            "Oric Atmos",
	systemdefs.SystemMultivision:     "Othello Multivision",
	systemdefs.SystemOuya:            "Ouya",
	systemdefs.SystemSuperGrafx:      "PC Engine SuperGrafx",
	systemdefs.SystemCDI:             "Philips CD-i",
	systemdefs.SystemVideopacPlus:    "Philips Videopac+",
	// "Philips VG 5000",                 // No Zaparoo system
	systemdefs.SystemPico8:   "PICO-8",
	systemdefs.SystemPinball: "Pinball",
	// "RCA Studio II",                   // No Zaparoo system
	systemdefs.SystemSAMCoupe:     "SAM Coupé",
	systemdefs.SystemAtomiswave:   "Sammy Atomiswave",
	systemdefs.SystemScummVM:      "ScummVM",
	systemdefs.SystemSega32X:      "Sega 32X",
	systemdefs.SystemMegaCD:       "Sega CD",
	systemdefs.SystemSegaCD32X:    "Sega CD 32X",
	systemdefs.SystemDreamcast:    "Sega Dreamcast",
	systemdefs.SystemVMU:          "Sega Dreamcast VMU",
	systemdefs.SystemGameGear:     "Sega Game Gear",
	systemdefs.SystemGenesis:      "Sega Genesis",
	systemdefs.SystemHikaru:       "Sega Hikaru",
	systemdefs.SystemMasterSystem: "Sega Master System",
	systemdefs.SystemModel1:       "Sega Model 1",
	systemdefs.SystemModel2:       "Sega Model 2",
	systemdefs.SystemModel3:       "Sega Model 3",
	systemdefs.SystemNAOMI:        "Sega Naomi",
	systemdefs.SystemNAOMI2:       "Sega Naomi 2",
	systemdefs.SystemSegaPico:     "Sega Pico",
	systemdefs.SystemSaturn:       "Sega Saturn",
	// "Sega SC-3000",                    // No Zaparoo system
	systemdefs.SystemSG1000: "Sega SG-1000",
	// "Sega ST-V",                       // No Zaparoo system
	// "Sega System 16",                  // No Zaparoo system
	// "Sega System 32",                  // No Zaparoo system
	systemdefs.SystemTriforce: "Sega Triforce",
	systemdefs.SystemX1:       "Sharp X1",
	// "Sharp MZ-2500",                   // No Zaparoo system
	systemdefs.SystemX68000:            "Sharp X68000",
	systemdefs.SystemZX81:              "Sinclair ZX-81",
	systemdefs.SystemZXSpectrum:        "Sinclair ZX Spectrum",
	systemdefs.SystemNeoGeoAES:         "SNK Neo Geo AES",
	systemdefs.SystemNeoGeoMVS:         "SNK Neo Geo MVS",
	systemdefs.SystemNeoGeoCD:          "SNK Neo Geo CD",
	systemdefs.SystemNeoGeoPocket:      "SNK Neo Geo Pocket",
	systemdefs.SystemNeoGeoPocketColor: "SNK Neo Geo Pocket Color",
	systemdefs.SystemSordM5:            "Sord M5",
	systemdefs.SystemPSX:               "Sony Playstation",
	systemdefs.SystemPS2:               "Sony Playstation 2",
	systemdefs.SystemPS3:               "Sony Playstation 3",
	systemdefs.SystemPS4:               "Sony Playstation 4",
	systemdefs.SystemPS5:               "Sony Playstation 5",
	systemdefs.SystemVita:              "Sony Playstation Vita",
	systemdefs.SystemPocketStation:     "Sony PocketStation",
	systemdefs.SystemPSP:               "Sony PSP",
	// "Sony PSP Minis",                  // No Zaparoo system
	systemdefs.SystemSpectravideo: "Spectravideo",
	systemdefs.SystemSNES:         "Super Nintendo Entertainment System",
	// "Taito Type X",                    // No Zaparoo system
	systemdefs.SystemTRS80:         "Tandy TRS-80",
	systemdefs.SystemTapwaveZodiac: "Tapwave Zodiac",
	systemdefs.SystemTI994A:        "Texas Instruments TI 99/4A",
	systemdefs.SystemGameCom:       "Tiger Game.com",
	systemdefs.SystemTomyTutor:     "Tomy Tutor",
	// "TRS-80 Color Computer",           // SystemCoCo2 exists but name doesn't match well
	systemdefs.SystemUzebox:       "Uzebox",
	systemdefs.SystemVector06C:    "Vector-06C",
	systemdefs.SystemCreatiVision: "VTech CreatiVision",
	systemdefs.SystemSocrates:     "VTech Socrates",
	systemdefs.SystemVSmile:       "VTech V.Smile",
	// "WASM-4",                          // No Zaparoo system
	systemdefs.SystemSuperVision: "Watara Supervision",
	// "Web Browser",                     // No Zaparoo system
	systemdefs.SystemPC: "Windows",
	// "Windows 3.X",                     // No Zaparoo system
	systemdefs.SystemWonderSwan:      "WonderSwan",
	systemdefs.SystemWonderSwanColor: "WonderSwan Color",
	// "WoW Action Max",                  // No Zaparoo system
	// "XaviXPORT",                       // No Zaparoo system
	// "ZiNc",                            // No Zaparoo system
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

	if def := cfg.LookupLauncherDefaults("LaunchBox", nil); def.InstallDir != "" {
		dirs = append([]string{def.InstallDir}, dirs...)
	}

	for _, dir := range dirs {
		if _, err := os.Stat(dir); err == nil {
			return dir, nil
		}
	}

	return "", errors.New("launchbox directory not found")
}

// pendingGamesRequest tracks a pending synchronous game request during scanning
type pendingGamesRequest struct {
	response chan launchBoxGamesResponse
	platform string
}

type pendingStopRequest struct {
	response chan launchBoxStopResultEvent
	gameID   string
}

// LaunchBoxPipeServer manages named pipe communication with the LaunchBox plugin
type LaunchBoxPipeServer struct {
	ctx                 context.Context
	listener            net.Listener
	conn                net.Conn
	onGameStarted       func(id, title, platform, path string, pid int)
	onGameExited        func(id, title string)
	onWriteRequest      func(id, title, platform string)
	onPlatformsReceived func(platforms []launchBoxPlatformInfo)
	cancel              context.CancelFunc
	writer              *bufio.Writer
	onCommandError      func(command, message string)
	onGameProcess       func(id string, pid int)
	// For synchronous game requests during scanning
	pendingGamesReq   pendingGamesRequest
	pendingStopReq    pendingStopRequest
	pluginVersion     string
	protocolVersion   int
	connMu            syncutil.Mutex
	pendingGamesReqMu syncutil.Mutex
	pendingStopReqMu  syncutil.Mutex
	handshakeMu       syncutil.RWMutex
}

// NewLaunchBoxPipeServer creates a new named pipe server
func NewLaunchBoxPipeServer() *LaunchBoxPipeServer {
	// TODO: should be reusing the service context here
	ctx, cancel := context.WithCancel(context.Background())
	return &LaunchBoxPipeServer{
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start begins listening for LaunchBox plugin connections
func (s *LaunchBoxPipeServer) Start() error {
	listener, err := winio.ListenPipe(launchBoxPipeName, nil)
	if err != nil {
		return fmt.Errorf("failed to create named pipe: %w", err)
	}

	s.listener = listener
	log.Info().Msgf("LaunchBox named pipe server listening on %s", launchBoxPipeName)

	go s.acceptConnections()

	return nil
}

// Stop gracefully shuts down the pipe server
func (s *LaunchBoxPipeServer) Stop() {
	s.cancel()

	s.connMu.Lock()
	if s.conn != nil {
		if err := s.conn.Close(); err != nil {
			log.Warn().Err(err).Msg("error closing LaunchBox pipe connection")
		}
		s.conn = nil
		s.writer = nil
	}
	s.connMu.Unlock()

	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			log.Warn().Err(err).Msg("error closing LaunchBox pipe listener")
		}
	}

	log.Debug().Msg("LaunchBox named pipe server stopped")
}

// SetGameStartedHandler sets the callback for game started events
func (s *LaunchBoxPipeServer) SetGameStartedHandler(handler func(id, title, platform, path string, pid int)) {
	s.onGameStarted = handler
}

// SetGameExitedHandler sets the callback for game exited events
func (s *LaunchBoxPipeServer) SetGameExitedHandler(handler func(id, title string)) {
	s.onGameExited = handler
}

// SetWriteRequestHandler sets the callback for write request events
func (s *LaunchBoxPipeServer) SetWriteRequestHandler(handler func(id, title, platform string)) {
	s.onWriteRequest = handler
}

// SetPlatformsReceivedHandler sets the callback for platforms received events
func (s *LaunchBoxPipeServer) SetPlatformsReceivedHandler(handler func(plats []launchBoxPlatformInfo)) {
	s.onPlatformsReceived = handler
}

// RequestPlatforms sends a GetPlatforms command to the LaunchBox plugin
func (s *LaunchBoxPipeServer) RequestPlatforms() error {
	s.connMu.Lock()
	defer s.connMu.Unlock()

	if s.writer == nil {
		return errors.New("LaunchBox plugin not connected")
	}

	cmd := pluginCommand{
		Command: "GetPlatforms",
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal GetPlatforms command: %w", err)
	}

	if _, err := s.writer.WriteString(string(data) + "\n"); err != nil {
		return fmt.Errorf("failed to write GetPlatforms command: %w", err)
	}

	if err := s.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush GetPlatforms command: %w", err)
	}

	log.Debug().Msg("sent GetPlatforms command to LaunchBox plugin")
	return nil
}

// RequestGamesForPlatformSync sends a GetGamesForPlatform command and waits for the response.
// This is used by the scanner to query games on-demand per-platform instead of caching all games.
func (s *LaunchBoxPipeServer) RequestGamesForPlatformSync(
	ctx context.Context,
	platform string,
) ([]LaunchBoxGameInfo, error) {
	respChan := make(chan launchBoxGamesResponse, 1)

	s.pendingGamesReqMu.Lock()
	if s.pendingGamesReq.response != nil {
		s.pendingGamesReqMu.Unlock()
		return nil, errors.New("games request already in flight")
	}
	s.pendingGamesReq.platform = platform
	s.pendingGamesReq.response = respChan
	s.pendingGamesReqMu.Unlock()

	defer func() {
		s.pendingGamesReqMu.Lock()
		s.pendingGamesReq.platform = ""
		s.pendingGamesReq.response = nil
		s.pendingGamesReqMu.Unlock()
	}()

	// The pending request is registered before the send so a fast reply
	// cannot arrive with nobody waiting for it. The connection check and the
	// write happen under one hold of the lock inside sendCommand; a plugin
	// that disconnects in between would otherwise leave a nil writer to
	// dereference.
	if err := s.sendCommand(pluginCommand{Command: "GetGamesForPlatform", Platform: platform}); err != nil {
		return nil, err
	}

	log.Debug().Msgf("sent GetGamesForPlatform command for: %s", platform)

	// Wait for response with timeout
	select {
	case resp := <-respChan:
		if resp.Error != "" {
			return nil, fmt.Errorf("LaunchBox plugin error: %s", resp.Error)
		}
		return resp.Games, nil
	case <-time.After(30 * time.Second):
		return nil, errors.New("timeout waiting for games from LaunchBox")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// LaunchGame sends a launch command to the LaunchBox plugin
func (s *LaunchBoxPipeServer) LaunchGame(gameID string) error {
	s.connMu.Lock()
	defer s.connMu.Unlock()

	if s.writer == nil {
		return errors.New("LaunchBox plugin not connected")
	}

	cmd := pluginCommand{
		Command: "Launch",
		ID:      gameID,
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal launch command: %w", err)
	}

	if _, err := s.writer.WriteString(string(data) + "\n"); err != nil {
		return fmt.Errorf("failed to write launch command: %w", err)
	}

	if err := s.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush launch command: %w", err)
	}

	log.Debug().Msgf("sent launch command for game ID: %s", gameID)
	return nil
}

// SetGameProcessHandler sets the callback for a resolved game process.
func (s *LaunchBoxPipeServer) SetGameProcessHandler(handler func(id string, pid int)) {
	s.onGameProcess = handler
}

// SetCommandErrorHandler sets the callback for plugin command failures.
func (s *LaunchBoxPipeServer) SetCommandErrorHandler(handler func(command, message string)) {
	s.onCommandError = handler
}

// setHandshake records what the connected plugin reported about itself.
func (s *LaunchBoxPipeServer) setHandshake(pluginVersion string, protocolVersion int) {
	s.handshakeMu.Lock()
	s.pluginVersion = pluginVersion
	s.protocolVersion = protocolVersion
	s.handshakeMu.Unlock()
}

// resetHandshake forgets the previous plugin's capabilities so a reconnecting
// older plugin is not credited with the newer one's features.
func (s *LaunchBoxPipeServer) resetHandshake() {
	s.setHandshake("", 0)
}

// SupportsStop reports whether the connected plugin can stop a running game.
func (s *LaunchBoxPipeServer) SupportsStop() bool {
	s.handshakeMu.RLock()
	defer s.handshakeMu.RUnlock()
	return s.protocolVersion >= launchBoxStopProtocolVersion
}

// StopGame asks the plugin to stop a running game and waits for the result.
// Going through the plugin rather than killing the process directly lets
// LaunchBox run its own shutdown path, including the emulator's configured
// exit script.
func (s *LaunchBoxPipeServer) StopGame(ctx context.Context, gameID string) error {
	if !s.SupportsStop() {
		return errLaunchBoxStopUnsupported
	}

	respChan := make(chan launchBoxStopResultEvent, 1)
	s.pendingStopReqMu.Lock()
	if s.pendingStopReq.response != nil {
		s.pendingStopReqMu.Unlock()
		return errors.New("stop request already in flight")
	}
	s.pendingStopReq = pendingStopRequest{gameID: gameID, response: respChan}
	s.pendingStopReqMu.Unlock()

	defer func() {
		s.pendingStopReqMu.Lock()
		s.pendingStopReq = pendingStopRequest{}
		s.pendingStopReqMu.Unlock()
	}()

	if err := s.sendCommand(pluginCommand{Command: "Stop", ID: gameID}); err != nil {
		return err
	}
	log.Debug().Msgf("sent stop command for game ID: %s", gameID)

	select {
	case resp := <-respChan:
		switch resp.Status {
		case "completed":
			return nil
		case "unsupported":
			return errLaunchBoxStopUnsupported
		default:
			if resp.Error != "" {
				return fmt.Errorf("LaunchBox stop failed: %s", resp.Error)
			}
			return fmt.Errorf("LaunchBox stop reported status %q", resp.Status)
		}
	case <-time.After(launchBoxStopTimeout):
		return errors.New("timeout waiting for LaunchBox to stop game")
	case <-ctx.Done():
		return fmt.Errorf("stop cancelled: %w", ctx.Err())
	}
}

// deliverStopResult hands a stop outcome to a waiting StopGame, if any.
//
// The send must never block: this runs on the pipe reader goroutine, and the
// response channel holds a single result. A plugin that reports an outcome
// twice would otherwise fill the buffer and then wedge the reader forever
// while holding pendingStopReqMu, stopping every later event from being
// processed. Only the first result matters, so extras are dropped.
func (s *LaunchBoxPipeServer) deliverStopResult(result launchBoxStopResultEvent) {
	s.pendingStopReqMu.Lock()
	defer s.pendingStopReqMu.Unlock()

	if s.pendingStopReq.response == nil {
		return
	}
	// An empty ID means the plugin did not say which game it acted on; the
	// only outstanding request owns it.
	if result.ID != "" && s.pendingStopReq.gameID != result.ID {
		return
	}

	select {
	case s.pendingStopReq.response <- result:
	default:
		log.Debug().Str("id", result.ID).Msg("discarding duplicate LaunchBox stop result")
	}
}

// sendCommand writes a command to the plugin.
func (s *LaunchBoxPipeServer) sendCommand(cmd pluginCommand) error {
	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal %s command: %w", cmd.Command, err)
	}

	s.connMu.Lock()
	defer s.connMu.Unlock()
	if s.writer == nil {
		return errors.New("LaunchBox plugin not connected")
	}
	if _, err := s.writer.WriteString(string(data) + "\n"); err != nil {
		return fmt.Errorf("failed to write %s command: %w", cmd.Command, err)
	}
	if err := s.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush %s command: %w", cmd.Command, err)
	}
	return nil
}

// IsConnected returns true if the LaunchBox plugin is connected
func (s *LaunchBoxPipeServer) IsConnected() bool {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	return s.conn != nil
}

// sendPing sends a heartbeat ping to keep the connection alive
func (s *LaunchBoxPipeServer) sendPing() error {
	s.connMu.Lock()
	defer s.connMu.Unlock()

	if s.writer == nil {
		return errors.New("writer not available")
	}

	cmd := pluginCommand{
		Command: "Ping",
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal ping command: %w", err)
	}

	if _, err := s.writer.WriteString(string(data) + "\n"); err != nil {
		return fmt.Errorf("failed to write ping command: %w", err)
	}

	if err := s.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush ping command: %w", err)
	}

	return nil
}

func (s *LaunchBoxPipeServer) acceptConnections() {
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				log.Warn().Err(err).Msg("failed to accept LaunchBox pipe connection")
				continue
			}
		}

		log.Info().Msg("LaunchBox plugin connected")

		// Close previous connection if exists
		s.connMu.Lock()
		if s.conn != nil {
			if closeErr := s.conn.Close(); closeErr != nil {
				log.Warn().Err(closeErr).Msg("error closing previous LaunchBox connection")
			}
		}
		s.conn = conn
		s.writer = bufio.NewWriter(conn)
		s.connMu.Unlock()

		// Start reading before writing anything. The plugin announces itself
		// as soon as it connects, so a write issued while nothing is draining
		// the pipe blocks both sides until the connection dies.
		go s.handleConnection(conn)

		// Request platform mappings from the plugin
		if err := s.RequestPlatforms(); err != nil {
			log.Warn().Err(err).Msg("failed to request platforms from LaunchBox plugin")
		}
	}
}

func (s *LaunchBoxPipeServer) handleConnection(conn net.Conn) {
	// A reconnecting plugin may be an older build; do not carry the previous
	// connection's advertised capabilities over to it.
	s.resetHandshake()
	defer func() {
		s.connMu.Lock()
		if s.conn == conn {
			s.conn = nil
			s.writer = nil
			log.Info().Msg("LaunchBox plugin disconnected")
		}
		s.connMu.Unlock()
		if err := conn.Close(); err != nil {
			log.Debug().Err(err).Msg("error closing LaunchBox pipe connection")
		}
	}()

	// Start heartbeat ticker to keep connection alive
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// Channel to signal scanner completion
	scanDone := make(chan struct{})

	// Scanner goroutine
	go func() {
		defer close(scanDone)
		scanner := bufio.NewScanner(conn)
		scanner.Buffer(make([]byte, 4096), launchBoxScannerMaxBuffer)

		for scanner.Scan() {
			select {
			case <-s.ctx.Done():
				return
			default:
			}

			s.handleEvent(scanner.Text())
		}

		// Check for errors (ignore EOF and closed connection)
		if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
			log.Warn().Err(err).Msg("error reading from LaunchBox pipe")
		}
	}()

	// Main loop: handle heartbeat and wait for scanner completion
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-scanDone:
			// Scanner finished (connection closed or error)
			return
		case <-ticker.C:
			// Send heartbeat ping
			if err := s.sendPing(); err != nil {
				log.Debug().Err(err).Msg("failed to send heartbeat ping")
				return
			}
		}
	}
}

func (s *LaunchBoxPipeServer) handleEvent(data string) {
	var event pluginEvent
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		log.Warn().Err(err).Msg("failed to unmarshal LaunchBox event")
		return
	}

	if event.Event == "" {
		log.Warn().Msg("LaunchBox event missing 'Event' field")
		return
	}

	switch event.Event {
	case "MediaStarted":
		log.Info().Msgf("LaunchBox game started: %s (ID: %s)", event.Title, event.ID)

		if s.onGameStarted != nil {
			s.onGameStarted(event.ID, event.Title, event.Platform, event.ApplicationPath, event.Pid)
		}

	case "MediaStopped":
		log.Info().Msgf("LaunchBox game stopped: %s (ID: %s)", event.Title, event.ID)

		if s.onGameExited != nil {
			s.onGameExited(event.ID, event.Title)
		}

	case "Write":
		log.Info().Msgf("LaunchBox write request: %s (ID: %s)", event.Title, event.ID)

		if s.onWriteRequest != nil {
			s.onWriteRequest(event.ID, event.Title, event.Platform)
		}

	case "Hello":
		var hello launchBoxHelloEvent
		if err := json.Unmarshal([]byte(data), &hello); err != nil {
			log.Warn().Err(err).Msg("failed to unmarshal LaunchBox Hello event")
			return
		}
		s.setHandshake(hello.PluginVersion, hello.ProtocolVersion)
		log.Info().
			Str("pluginVersion", hello.PluginVersion).
			Int("protocolVersion", hello.ProtocolVersion).
			Bool("supportsStop", s.SupportsStop()).
			Msg("LaunchBox plugin handshake received")

	case "MediaProcess":
		var procEvent launchBoxProcessEvent
		if err := json.Unmarshal([]byte(data), &procEvent); err != nil {
			log.Warn().Err(err).Msg("failed to unmarshal LaunchBox MediaProcess event")
			return
		}
		log.Debug().Str("id", procEvent.ID).Int("pid", procEvent.Pid).
			Msg("LaunchBox reported game process")
		if s.onGameProcess != nil {
			s.onGameProcess(procEvent.ID, procEvent.Pid)
		}

	case "MediaStopResult":
		var result launchBoxStopResultEvent
		if err := json.Unmarshal([]byte(data), &result); err != nil {
			log.Warn().Err(err).Msg("failed to unmarshal LaunchBox MediaStopResult event")
			return
		}
		log.Info().Str("id", result.ID).Str("status", result.Status).
			Msg("LaunchBox reported stop result")
		s.deliverStopResult(result)

	case "Error":
		var errEvent launchBoxErrorEvent
		if err := json.Unmarshal([]byte(data), &errEvent); err != nil {
			log.Warn().Err(err).Msg("failed to unmarshal LaunchBox Error event")
			return
		}
		log.Warn().Str("command", errEvent.Command).Str("error", errEvent.Error).
			Msg("LaunchBox plugin reported a command failure")
		// A failed Stop must not leave StopGame blocked until its timeout.
		if strings.EqualFold(errEvent.Command, "Stop") {
			s.deliverStopResult(launchBoxStopResultEvent{
				Event:  "MediaStopResult",
				Status: "failed",
				Error:  errEvent.Error,
			})
		}
		if s.onCommandError != nil {
			s.onCommandError(errEvent.Command, errEvent.Error)
		}

	case "Platforms":
		var platformsEvent launchBoxPlatformsEvent
		if err := json.Unmarshal([]byte(data), &platformsEvent); err != nil {
			log.Warn().Err(err).Msg("failed to unmarshal LaunchBox Platforms event")
			return
		}

		log.Info().Msgf("received %d platforms from LaunchBox", len(platformsEvent.Platforms))

		if s.onPlatformsReceived != nil {
			s.onPlatformsReceived(platformsEvent.Platforms)
		}

	case "Games":
		var gamesEvent launchBoxGamesEvent
		if err := json.Unmarshal([]byte(data), &gamesEvent); err != nil {
			log.Warn().Err(err).Msg("failed to unmarshal LaunchBox Games event")
			return
		}

		if gamesEvent.Error != "" {
			log.Warn().Msgf("LaunchBox plugin error for platform %s: %s",
				gamesEvent.Platform, gamesEvent.Error)
		} else {
			log.Debug().Msgf("received %d games from LaunchBox for platform %s",
				len(gamesEvent.Games), gamesEvent.Platform)
		}

		// Send to pending request if one exists for this platform
		s.pendingGamesReqMu.Lock()
		if s.pendingGamesReq.response != nil && s.pendingGamesReq.platform == gamesEvent.Platform {
			s.pendingGamesReq.response <- launchBoxGamesResponse{
				Games: gamesEvent.Games,
				Error: gamesEvent.Error,
			}
		}
		s.pendingGamesReqMu.Unlock()

	default:
		log.Debug().Msgf("unknown LaunchBox event type: %s", event.Event)
	}
}

func (p *Platform) initLaunchBoxPipe(cfg *config.Instance) {
	// Check if LaunchBox is installed
	lbDir, err := findLaunchBoxDir(cfg)
	if err != nil {
		log.Debug().Msg("LaunchBox not detected, skipping named pipe server initialization")
		return
	}

	log.Debug().Msgf("LaunchBox detected at: %s", lbDir)

	// Build reverse lookup map (LaunchBox platform name -> Zaparoo system ID)
	lbSysMapReverse := make(map[string]string, len(lbSysMap))
	for sysID, lbName := range lbSysMap {
		lbSysMapReverse[lbName] = sysID
	}

	// Start LaunchBox named pipe server
	pipe := NewLaunchBoxPipeServer()

	// Set event handlers
	pipe.SetGameStartedHandler(func(id, title, platform, appPath string, pid int) {
		p.setLaunchBoxActiveGame(id, appPath)

		// Older plugins report the process inline; newer ones follow up with a
		// MediaProcess event once they have resolved it.
		p.trackLaunchBoxProcess(id, pid)

		// Try custom platform mapping first (from plugin's ScrapeAs data)
		p.platformMappingsMu.RLock()
		systemID, ok := p.customPlatformToSystem[platform]
		p.platformMappingsMu.RUnlock()

		if !ok {
			// Fall back to hardcoded reverse map
			systemID, ok = lbSysMapReverse[platform]
			if !ok {
				log.Debug().Msgf("unknown LaunchBox platform: %s, skipping ActiveMedia", platform)
				return
			}
		}

		// Get system name from metadata
		systemName := platform // Fallback to LaunchBox platform name
		systemMeta, err := assets.GetSystemMetadata(systemID)
		if err != nil {
			log.Debug().Err(err).Msgf("no system metadata for: %s", systemID)
		} else {
			systemName = systemMeta.Name
		}

		// Build virtual path for the game
		virtualPath := virtualpath.CreateVirtualPath(shared.SchemeLaunchBox, id, title)

		// Create and set ActiveMedia
		activeMedia := models.NewActiveMedia(
			systemID,
			systemName,
			virtualPath,
			title,
			"LaunchBox",
		)

		log.Info().Msgf("LaunchBox game started: SystemID='%s', SystemName='%s', Path='%s', Name='%s', LauncherID='%s'",
			activeMedia.SystemID, activeMedia.SystemName, activeMedia.Path, activeMedia.Name, activeMedia.LauncherID)

		p.setActiveMedia(activeMedia)
	})

	pipe.SetGameExitedHandler(func(id, title string) {
		log.Info().Msgf("LaunchBox game stopped: %s", title)
		// Only the game Core believes is running may clear active media; a
		// late exit for a previous game must not wipe a newer launch.
		if !p.clearLaunchBoxActiveID(id) {
			log.Debug().Msgf("ignoring stale LaunchBox exit for: %s", id)
			return
		}
		p.setActiveMedia(nil)
	})

	pipe.SetGameProcessHandler(func(id string, pid int) {
		// The plugin resolves the process LaunchBox started so Core can stop
		// the whole tree if the plugin's own stop path fails or is missing.
		p.trackLaunchBoxProcess(id, pid)
	})

	pipe.SetCommandErrorHandler(func(command, message string) {
		// A rejected launch previously looked like success because the plugin's
		// error event was dropped. Surface it so the failure is visible.
		log.Warn().Msgf("LaunchBox plugin rejected %s: %s", command, message)
	})

	pipe.SetWriteRequestHandler(func(id, title, _ string) {
		text := virtualpath.CreateVirtualPath("launchbox", id, title)
		log.Info().Msgf("LaunchBox write request: %s", text)

		// Send write request to API
		params, err := json.Marshal(&models.ReaderWriteParams{
			Text: text,
		})
		if err != nil {
			log.Error().Err(err).Msg("failed to marshal write params")
			return
		}

		_, err = client.LocalClient(context.Background(), cfg, models.MethodReadersWrite, string(params))
		if err != nil {
			log.Error().Err(err).Msg("failed to send write request to API")
			return
		}

		log.Info().Msgf("write request sent to API: %s", text)
	})

	pipe.SetPlatformsReceivedHandler(func(platforms []launchBoxPlatformInfo) {
		p.platformMappingsMu.Lock()
		defer p.platformMappingsMu.Unlock()

		p.customPlatformToSystem = make(map[string]string)
		p.systemToCustomPlatforms = make(map[string][]string)

		for _, plat := range platforms {
			// Use ScrapeAs to find the Zaparoo system ID via lbSysMap
			canonicalName := plat.ScrapeAs
			if canonicalName == "" {
				canonicalName = plat.Name
			}

			// Look up in lbSysMap (which maps Zaparoo system ID -> LaunchBox canonical name)
			for sysID, lbName := range lbSysMap {
				if strings.EqualFold(lbName, canonicalName) {
					p.customPlatformToSystem[plat.Name] = sysID
					// Only set reverse mapping if it's a custom name
					if !strings.EqualFold(plat.Name, lbName) {
						p.systemToCustomPlatforms[sysID] = append(p.systemToCustomPlatforms[sysID], plat.Name)
					}
					log.Debug().Msgf("mapped LaunchBox platform %q (ScrapeAs: %q) -> %s",
						plat.Name, plat.ScrapeAs, sysID)
					break
				}
			}
		}

		log.Info().Msgf("built %d custom platform mappings from LaunchBox", len(p.customPlatformToSystem))
	})

	if err := pipe.Start(); err != nil {
		log.Warn().Err(err).Msg("failed to start LaunchBox named pipe server")
		// Don't fail platform initialization if pipe server fails
		return
	}

	p.launchBoxPipeLock.Lock()
	p.launchBoxPipe = pipe
	p.launchBoxPipeLock.Unlock()

	log.Info().Msg("LaunchBox named pipe server initialized")
}

// trackLaunchBoxProcess adopts the process the plugin reported for a launch.
//
// The PID arrives over the pipe after the plugin has polled for it, so by the
// time Core opens it the original process may have exited and Windows may have
// reused the number. Adopting the wrong PID would mean force-killing an
// unrelated process tree on the next stop, so the image path is checked
// against the application LaunchBox was asked to run before it is trusted.
func (p *Platform) trackLaunchBoxProcess(gameID string, pid int) {
	if pid <= 0 {
		return
	}

	appPath, current := p.launchBoxActiveGame()
	if gameID != "" && current != "" && gameID != current {
		log.Debug().Str("id", gameID).Msg("ignoring process for a replaced LaunchBox game")
		return
	}

	if !launchBoxProcessMatches(pid, appPath) {
		log.Debug().Int("pid", pid).Str("appPath", appPath).
			Msg("ignoring LaunchBox process that no longer matches the launched application")
		return
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		log.Debug().Err(err).Int("pid", pid).Msg("could not open LaunchBox game process")
		return
	}
	p.SetTrackedProcess(proc)
}

// launchBoxProcessMatches reports whether pid is still running the expected
// application. An application path Core cannot check -- a steam:// URL, or a
// process whose image path is unreadable -- is accepted, because the plugin
// only reports a PID it resolved itself.
func launchBoxProcessMatches(pid int, appPath string) bool {
	if appPath == "" || strings.Contains(appPath, "://") {
		return true
	}
	proc, err := process.NewProcess(int32(pid)) //nolint:gosec // Windows PIDs fit in int32.
	if err != nil {
		return false
	}
	exe, err := proc.Exe()
	if err != nil || exe == "" {
		return true
	}
	return strings.EqualFold(filepath.Clean(exe), filepath.Clean(appPath))
}

// setLaunchBoxActiveGame records the game Core considers currently running and
// the application LaunchBox was asked to run for it.
func (p *Platform) setLaunchBoxActiveGame(id, appPath string) {
	p.launchBoxActiveMu.Lock()
	p.launchBoxActiveID = id
	p.launchBoxActiveAppPath = appPath
	p.launchBoxActiveMu.Unlock()
}

// launchBoxActiveGame returns the running game's application path and id.
func (p *Platform) launchBoxActiveGame() (appPath, id string) {
	p.launchBoxActiveMu.Lock()
	defer p.launchBoxActiveMu.Unlock()
	return p.launchBoxActiveAppPath, p.launchBoxActiveID
}

// clearLaunchBoxActiveID clears the active game only when id still owns it.
func (p *Platform) clearLaunchBoxActiveID(id string) bool {
	p.launchBoxActiveMu.Lock()
	defer p.launchBoxActiveMu.Unlock()
	if p.launchBoxActiveID != id {
		return false
	}
	p.launchBoxActiveID = ""
	p.launchBoxActiveAppPath = ""
	return true
}

func (p *Platform) launchBoxActiveGameID() string {
	p.launchBoxActiveMu.Lock()
	defer p.launchBoxActiveMu.Unlock()
	return p.launchBoxActiveID
}

// stopLaunchBoxGame asks the plugin to end the running game. Returning an
// error lets StopActiveLauncher fall back to killing the tracked process tree.
func (p *Platform) stopLaunchBoxGame(_ *config.Instance) error {
	gameID := p.launchBoxActiveGameID()
	if gameID == "" {
		return errors.New("no active LaunchBox game")
	}

	p.launchBoxPipeLock.Lock()
	pipe := p.launchBoxPipe
	p.launchBoxPipeLock.Unlock()

	if pipe == nil || !pipe.IsConnected() {
		return errors.New("LaunchBox plugin not connected")
	}

	ctx, cancel := context.WithTimeout(context.Background(), launchBoxStopTimeout)
	defer cancel()
	if err := pipe.StopGame(ctx, gameID); err != nil {
		return err
	}

	p.clearLaunchBoxActiveID(gameID)
	return nil
}

// NewLaunchBoxLauncher creates the LaunchBox launcher
func (p *Platform) NewLaunchBoxLauncher() platforms.Launcher {
	return platforms.Launcher{
		ID:      "LaunchBox",
		Schemes: []string{shared.SchemeLaunchBox},
		// LaunchBox owns the game process and reports its lifecycle over the
		// pipe, so ActiveMedia comes from MediaStarted rather than from the
		// launch command being accepted.
		Lifecycle: platforms.LifecycleExternal,
		Kill:      p.stopLaunchBoxGame,
		Scanner: func(
			ctx context.Context,
			cfg *config.Instance,
			systemId string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			// Build list of LaunchBox platforms to query
			p.platformMappingsMu.RLock()
			customPlatforms := p.systemToCustomPlatforms[systemId]
			p.platformMappingsMu.RUnlock()

			// Start with custom platforms, then add canonical name from hardcoded map
			var platformsToQuery []string
			platformsToQuery = append(platformsToQuery, customPlatforms...)

			// Add canonical platform from hardcoded map if it exists
			if canonicalPlatform, ok := lbSysMap[systemId]; ok {
				platformsToQuery = append(platformsToQuery, canonicalPlatform)
			}

			if len(platformsToQuery) == 0 {
				return results, nil
			}

			// Try plugin first (includes additional apps for merged games)
			p.launchBoxPipeLock.Lock()
			pipe := p.launchBoxPipe
			p.launchBoxPipeLock.Unlock()

			if pipe != nil && pipe.IsConnected() {
				pluginSucceeded := false
				for _, lbSys := range platformsToQuery {
					games, err := pipe.RequestGamesForPlatformSync(ctx, lbSys)
					if err != nil {
						log.Debug().Err(err).Msgf("plugin query failed for platform %s", lbSys)
						continue
					}
					pluginSucceeded = true
					for _, game := range games {
						// Add the primary game
						results = append(results, platforms.ScanResult{
							Path:  virtualpath.CreateVirtualPath(shared.SchemeLaunchBox, game.ID, game.Title),
							Name:  game.Title,
							NoExt: true,
						})

						// Add additional applications (merged games, secondary discs, etc.)
						for _, app := range game.AdditionalApps {
							results = append(results, platforms.ScanResult{
								Path:  virtualpath.CreateVirtualPath(shared.SchemeLaunchBox, app.ID, app.Name),
								Name:  app.Name,
								NoExt: true,
							})
						}
					}
					log.Debug().Msgf("scanned %d items from LaunchBox plugin for %s", len(games), lbSys)
				}
				if pluginSucceeded {
					return results, nil
				}
				log.Debug().Msg("all plugin queries failed, falling back to XML")
			}

			// Fall back to XML parsing (no additional apps available)
			lbDir, err := findLaunchBoxDir(cfg)
			if err != nil {
				return results, err
			}

			platformsDir := filepath.Join(lbDir, "Data", "Platforms")
			if _, statErr := os.Stat(lbDir); os.IsNotExist(statErr) {
				return results, errors.New("LaunchBox platforms dir not found")
			}

			for _, lbSys := range platformsToQuery {
				xmlPath := filepath.Join(platformsDir, lbSys+".xml")
				if _, statErr := os.Stat(xmlPath); os.IsNotExist(statErr) {
					log.Debug().Msgf("LaunchBox platform xml not found: %s", xmlPath)
					continue
				}

				//nolint:gosec // Safe: reads game database XML files from controlled directories
				xmlFile, err := os.Open(xmlPath)
				if err != nil {
					log.Warn().Err(err).Msgf("failed to open XML file %s", xmlPath)
					continue
				}

				var lbXML launchBoxXML
				if err := xml.NewDecoder(xmlFile).Decode(&lbXML); err != nil {
					if closeErr := xmlFile.Close(); closeErr != nil {
						log.Warn().Err(closeErr).Msg("error closing xml file")
					}
					log.Warn().Err(err).Msgf("failed to decode XML for %s", lbSys)
					continue
				}

				if closeErr := xmlFile.Close(); closeErr != nil {
					log.Warn().Err(closeErr).Msg("error closing xml file")
				}

				for _, game := range lbXML.Games {
					results = append(results, platforms.ScanResult{
						Path:  virtualpath.CreateVirtualPath(shared.SchemeLaunchBox, game.ID, game.Title),
						Name:  game.Title,
						NoExt: true,
					})
				}
				log.Debug().Msgf("scanned %d games from LaunchBox XML for %s", len(lbXML.Games), lbSys)
			}

			return results, nil
		},
		Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			id, err := virtualpath.ExtractSchemeID(path, shared.SchemeLaunchBox)
			if err != nil {
				return nil, fmt.Errorf("failed to extract LaunchBox game ID from path: %w", err)
			}

			// Use named pipe to launch game via plugin
			p.launchBoxPipeLock.Lock()
			pipe := p.launchBoxPipe
			p.launchBoxPipeLock.Unlock()

			if pipe == nil || !pipe.IsConnected() {
				return nil, errors.New("LaunchBox plugin not connected")
			}

			if err := pipe.LaunchGame(id); err != nil {
				return nil, fmt.Errorf("failed to send launch command to LaunchBox: %w", err)
			}

			return nil, nil
		},
	}
}
