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
)

const (
	launchBoxPipeName = `\\.\pipe\zaparoo-launchbox-ipc`
)

// Plugin message types (matches C# plugin JSON structure)
//
//nolint:tagliatelle // JSON tags must match C# plugin structure (PascalCase)
type pluginEvent struct {
	Event           string `json:"Event"`
	ID              string `json:"Id,omitempty"`
	Title           string `json:"Title,omitempty"`
	Platform        string `json:"Platform,omitempty"`
	ApplicationPath string `json:"ApplicationPath,omitempty"`
}

//nolint:tagliatelle // JSON tags must match C# plugin structure (PascalCase)
type pluginCommand struct {
	Command string `json:"Command"`
	ID      string `json:"Id,omitempty"`
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
	systemdefs.SystemAliceMC10: "Matra and Hachette Alice",
	systemdefs.SystemAmiga:     "Commodore Amiga",
	systemdefs.SystemAmigaCD32: "Commodore Amiga CD32",
	systemdefs.SystemAmstrad:   "Amstrad CPC",
	// "Amstrad GX4000",                  // No Zaparoo system
	systemdefs.SystemAndroid: "Android",
	systemdefs.SystemApogee:  "Apogee BK-01",
	systemdefs.SystemAppleII: "Apple II",
	// "Apple IIGS",                      // No Zaparoo system
	systemdefs.SystemIOS:       "Apple iOS",
	systemdefs.SystemMacOS:     "Apple Mac OS",
	systemdefs.SystemAquarius:  "Mattel Aquarius",
	systemdefs.SystemArcade:    "Arcade",
	systemdefs.SystemArduboy:   "Arduboy",
	systemdefs.SystemAtari2600: "Atari 2600",
	systemdefs.SystemAtari5200: "Atari 5200",
	systemdefs.SystemAtari7800: "Atari 7800",
	systemdefs.SystemAtari800:  "Atari 800",
	systemdefs.SystemJaguar:    "Atari Jaguar",
	systemdefs.SystemJaguarCD:  "Atari Jaguar CD",
	systemdefs.SystemAtariLynx: "Atari Lynx",
	systemdefs.SystemAtariST:   "Atari ST",
	systemdefs.SystemAtariXEGS: "Atari XEGS",
	systemdefs.SystemAstrocade: "Bally Astrocade",
	// "Bandai Super Vision 8000",        // No Zaparoo system
	systemdefs.SystemBBCMicro: "BBC Microcomputer System",
	// "Camputers Lynx",                  // No Zaparoo system
	systemdefs.SystemCasioPV1000: "Casio PV-1000",
	// "Casio Loopy",                     // No Zaparoo system
	systemdefs.SystemColecoAdam:   "Coleco ADAM",
	systemdefs.SystemColecoVision: "ColecoVision",
	systemdefs.SystemC64:          "Commodore 64",
	// "Commodore 128",                   // No Zaparoo system
	// "Commodore CDTV",                  // No Zaparoo system
	// "Commodore MAX Machine",           // No Zaparoo system
	systemdefs.SystemPET2001: "Commodore PET",
	systemdefs.SystemC16:     "Commodore Plus 4",
	systemdefs.SystemVIC20:   "Commodore VIC-20",
	// "Dragon 32/64",                    // No Zaparoo system
	// "EACA EG2000 Colour Genie",        // No Zaparoo system
	// "Elektor TV Games Computer",       // No Zaparoo system
	systemdefs.SystemBK0011M: "Elektronika BK",
	systemdefs.SystemArcadia: "Emerson Arcadia 2001",
	// "Enterprise",                      // No Zaparoo system
	systemdefs.SystemAdventureVision: "Entex Adventure Vision",
	// "Epoch Super Cassette Vision",     // No Zaparoo system
	systemdefs.SystemGamePocket: "Epoch Game Pocket Computer",
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
	systemdefs.SystemVC4000:  "Interton VC 4000",
	systemdefs.SystemJupiter: "Jupiter Ace",
	// "Linux",                           // No Zaparoo system
	systemdefs.SystemOdyssey2: "Magnavox Odyssey 2",
	// "Magnavox Odyssey",                // No Zaparoo system
	// "Mattel HyperScan",                // No Zaparoo system
	systemdefs.SystemIntellivision: "Mattel Intellivision",
	systemdefs.SystemMegaDuck:      "Mega Duck",
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
	systemdefs.SystemSwitch: "Nintendo Switch",
	// "Nintendo Switch 2",               // No Zaparoo system (future platform)
	systemdefs.SystemVirtualBoy: "Nintendo Virtual Boy",
	systemdefs.SystemWii:        "Nintendo Wii",
	systemdefs.SystemWiiU:       "Nintendo Wii U",
	systemdefs.SystemNGage:      "Nokia N-Gage",
	// "Nuon",                            // No Zaparoo system
	// "OpenBOR",                         // No Zaparoo system
	systemdefs.SystemOric:         "Oric Atmos",
	systemdefs.SystemMultivision:  "Othello Multivision",
	systemdefs.SystemOuya:         "Ouya",
	systemdefs.SystemSuperGrafx:   "PC Engine SuperGrafx",
	systemdefs.SystemCDI:          "Philips CD-i",
	systemdefs.SystemVideopacPlus: "Philips Videopac+",
	// "Philips VG 5000",                 // No Zaparoo system
	systemdefs.SystemPico8: "PICO-8",
	// "Pinball",                         // No Zaparoo system
	// "RCA Studio II",                   // No Zaparoo system
	systemdefs.SystemSAMCoupe:   "SAM Coupé",
	systemdefs.SystemAtomiswave: "Sammy Atomiswave",
	systemdefs.SystemScummVM:    "ScummVM",
	systemdefs.SystemSega32X:    "Sega 32X",
	systemdefs.SystemMegaCD:     "Sega CD",
	// "Sega CD 32X",                     // No Zaparoo system
	systemdefs.SystemDreamcast: "Sega Dreamcast",
	// "Sega Dreamcast VMU",              // No Zaparoo system
	systemdefs.SystemGameGear:     "Sega Game Gear",
	systemdefs.SystemGenesis:      "Sega Genesis",
	systemdefs.SystemHikaru:       "Sega Hikaru",
	systemdefs.SystemMasterSystem: "Sega Master System",
	systemdefs.SystemModel1:       "Sega Model 1",
	systemdefs.SystemModel2:       "Sega Model 2",
	systemdefs.SystemModel3:       "Sega Model 3",
	systemdefs.SystemNAOMI:        "Sega Naomi",
	systemdefs.SystemNAOMI2:       "Sega Naomi 2",
	// "Sega Pico",                       // No Zaparoo system
	systemdefs.SystemSaturn: "Sega Saturn",
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
	// "Sony PocketStation",              // No Zaparoo system
	systemdefs.SystemPSP: "Sony PSP",
	// "Sony PSP Minis",                  // No Zaparoo system
	systemdefs.SystemSpectravideo: "Spectravideo",
	systemdefs.SystemSNES:         "Super Nintendo Entertainment System",
	// "Taito Type X",                    // No Zaparoo system
	systemdefs.SystemTRS80: "Tandy TRS-80",
	// "Tapwave Zodiac",                  // No Zaparoo system
	systemdefs.SystemTI994A:    "Texas Instruments TI 99/4A",
	systemdefs.SystemGameCom:   "Tiger Game.com",
	systemdefs.SystemTomyTutor: "Tomy Tutor",
	// "TRS-80 Color Computer",           // SystemCoCo2 exists but name doesn't match well
	// "Uzebox",                          // No Zaparoo system
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

// LaunchBoxPipeServer manages named pipe communication with the LaunchBox plugin
type LaunchBoxPipeServer struct {
	onGameStarted  func(id, title, platform, path string)
	onGameExited   func(id, title string)
	onWriteRequest func(id, title, platform string)
	listener       net.Listener
	conn           net.Conn
	ctx            context.Context
	cancel         context.CancelFunc
	writer         *bufio.Writer
	connMu         syncutil.Mutex
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
func (s *LaunchBoxPipeServer) SetGameStartedHandler(handler func(id, title, platform, path string)) {
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

		// Handle this connection
		go s.handleConnection(conn)
	}
}

func (s *LaunchBoxPipeServer) handleConnection(conn net.Conn) {
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
		scanner.Buffer(make([]byte, 4096), 1024*1024) // 1MB buffer

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
			s.onGameStarted(event.ID, event.Title, event.Platform, event.ApplicationPath)
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
	pipe.SetGameStartedHandler(func(id, title, platform, _ string) {
		// Convert LaunchBox platform name to Zaparoo system ID
		systemID, ok := lbSysMapReverse[platform]
		if !ok {
			log.Debug().Msgf("unknown LaunchBox platform: %s, skipping ActiveMedia", platform)
			return
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

	pipe.SetGameExitedHandler(func(_, title string) {
		log.Info().Msgf("LaunchBox game stopped: %s", title)
		p.setActiveMedia(nil)
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

// NewLaunchBoxLauncher creates the LaunchBox launcher
func (p *Platform) NewLaunchBoxLauncher() platforms.Launcher {
	return platforms.Launcher{
		ID:      "LaunchBox",
		Schemes: []string{shared.SchemeLaunchBox},
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

			var lbXML launchBoxXML
			if err := xml.NewDecoder(xmlFile).Decode(&lbXML); err != nil {
				return results, fmt.Errorf("failed to decode XML: %w", err)
			}

			for _, game := range lbXML.Games {
				results = append(results, platforms.ScanResult{
					Path:  virtualpath.CreateVirtualPath(shared.SchemeLaunchBox, game.ID, game.Title),
					Name:  game.Title,
					NoExt: true,
				})
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
