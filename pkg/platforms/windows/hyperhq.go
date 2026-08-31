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
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
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
	hyperHqPipeName = `\\.\pipe\zaparoo-hyperhq-ipc`

	// hyperHqScannerMaxBuffer is the maximum buffer size for reading from the HyperHQ pipe.
	// Must be large enough to handle JSON responses for systems with thousands of games.
	hyperHqScannerMaxBuffer = 16 * 1024 * 1024 // 16MB
)

// HyperHQ wire-protocol types. PascalCase to match the bridge plugin's serialiser.
//
//nolint:tagliatelle // JSON tags must match HyperHQ plugin structure (PascalCase)
type hqEvent struct {
	Event             string `json:"Event"`
	ID                string `json:"Id,omitempty"`
	Title             string `json:"Title,omitempty"`
	Platform          string `json:"Platform,omitempty"`
	SystemReferenceID string `json:"SystemReferenceId,omitempty"`
}

//nolint:tagliatelle // JSON tags must match HyperHQ plugin structure (PascalCase)
type hqCommand struct {
	Command           string `json:"Command"`
	ID                string `json:"Id,omitempty"`
	SystemID          string `json:"SystemId,omitempty"`
	SystemName        string `json:"SystemName,omitempty"`
	SystemReferenceID string `json:"SystemReferenceId,omitempty"`
}

type hqSystemQueryTarget struct {
	ID          string
	Name        string
	ReferenceID string
}

// HqSystemInfo represents a HyperHQ system as reported by the plugin.
//
//nolint:tagliatelle // JSON tags must match HyperHQ plugin structure (PascalCase)
type HqSystemInfo struct {
	ID          string `json:"Id"`
	Name        string `json:"Name"`
	ReferenceID string `json:"ReferenceId"`
	Platform    string `json:"Platform"`
}

//nolint:tagliatelle // JSON tags must match HyperHQ plugin structure (PascalCase)
type hqSystemsEvent struct {
	Event   string         `json:"Event"`
	Systems []HqSystemInfo `json:"Systems"`
}

// HqGameInfo represents a HyperHQ game as reported by the plugin.
//
//nolint:tagliatelle // JSON tags must match HyperHQ plugin structure (PascalCase)
type HqGameInfo struct {
	ID       string `json:"Id"`
	Title    string `json:"Title"`
	Platform string `json:"Platform"`
}

//nolint:tagliatelle // JSON tags must match HyperHQ plugin structure (PascalCase)
type hqGamesEvent struct {
	Event             string       `json:"Event"`
	SystemID          string       `json:"SystemId,omitempty"`
	SystemName        string       `json:"SystemName,omitempty"`
	SystemReferenceID string       `json:"SystemReferenceId"`
	Error             string       `json:"Error,omitempty"`
	Games             []HqGameInfo `json:"Games"`
}

// hqGamesResponse is used internally for the synchronous request channel.
type hqGamesResponse struct {
	Error string
	Games []HqGameInfo
}

// hqSystemAliases maps HyperHQ system names to Zaparoo system IDs. HyperHQ
// also supports custom systems, so unmapped names are logged for future aliases.
var hqSystemAliases = map[string]string{
	"3DO Interactive Multiplayer":         systemdefs.System3DO,
	"Acorn Archimedes":                    systemdefs.SystemArchimedes,
	"Acorn Atom":                          systemdefs.SystemAcornAtom,
	"Acorn BBC Micro":                     systemdefs.SystemBBCMicro,
	"Acorn Electron":                      systemdefs.SystemAcornElectron,
	"Android":                             systemdefs.SystemAndroid,
	"Apogee BK-01":                        systemdefs.SystemApogee,
	"Apple II":                            systemdefs.SystemAppleII,
	"Apple iOS":                           systemdefs.SystemIOS,
	"Apple Mac OS":                        systemdefs.SystemMacOS,
	"Arcade (MAME)":                       systemdefs.SystemArcade,
	"Arcade (TeknoParrot)":                systemdefs.SystemArcade,
	"Atari 2600":                          systemdefs.SystemAtari2600,
	"Atari 5200":                          systemdefs.SystemAtari5200,
	"Atari 7800":                          systemdefs.SystemAtari7800,
	"Atari 800":                           systemdefs.SystemAtari800,
	"Atari Jaguar":                        systemdefs.SystemJaguar,
	"Atari Jaguar CD":                     systemdefs.SystemJaguarCD,
	"Atari Lynx":                          systemdefs.SystemAtariLynx,
	"Atari ST":                            systemdefs.SystemAtariST,
	"Atari XEGS":                          systemdefs.SystemAtariXEGS,
	"Bally Astrocade":                     systemdefs.SystemAstrocade,
	"Bandai Sufami Turbo":                 systemdefs.SystemSufami,
	"Bandai WonderSwan":                   systemdefs.SystemWonderSwan,
	"Bandai WonderSwan Color":             systemdefs.SystemWonderSwanColor,
	"BBC Microcomputer System":            systemdefs.SystemBBCMicro,
	"Casio PV-1000":                       systemdefs.SystemCasioPV1000,
	"Casio PV-2000":                       systemdefs.SystemCasioPV2000,
	"Coleco ADAM":                         systemdefs.SystemColecoAdam,
	"ColecoVision":                        systemdefs.SystemColecoVision,
	"Commodore 16":                        systemdefs.SystemC16,
	"Commodore 64":                        systemdefs.SystemC64,
	"Commodore Amiga":                     systemdefs.SystemAmiga,
	"Commodore Amiga CD32":                systemdefs.SystemAmigaCD32,
	"Commodore PET":                       systemdefs.SystemPET2001,
	"Commodore Plus 4":                    systemdefs.SystemC16,
	"Commodore VIC-20":                    systemdefs.SystemVIC20,
	"Creatronic Mega Duck":                systemdefs.SystemMegaDuck,
	"Daphne":                              systemdefs.SystemDAPHNE,
	"DICE":                                systemdefs.SystemDICE,
	"Elektronika BK 0011":                 systemdefs.SystemBK0011M,
	"Emerson Arcadia 2001":                systemdefs.SystemArcadia,
	"Entex Adventure Vision":              systemdefs.SystemAdventureVision,
	"Epoch Game Pocket Computer":          systemdefs.SystemGamePocket,
	"Fairchild Channel F":                 systemdefs.SystemChannelF,
	"Fujitsu FM Towns":                    systemdefs.SystemFMTowns,
	"Fujitsu FM Towns Marty":              systemdefs.SystemFMTowns,
	"Fujitsu FM-7":                        systemdefs.SystemFM7,
	"Funtech Super Acan":                  systemdefs.SystemSuperACan,
	"GamePark GP32":                       systemdefs.SystemGP32,
	"GCE Vectrex":                         systemdefs.SystemVectrex,
	"Hartung Game Master":                 systemdefs.SystemGameMaster,
	"Hypseus Singe":                       systemdefs.SystemSinge,
	"Interton VC 4000":                    systemdefs.SystemVC4000,
	"Jupiter Ace":                         systemdefs.SystemJupiter,
	"Matra and Hachette Alice":            systemdefs.SystemAliceMC10,
	"Mattel Aquarius":                     systemdefs.SystemAquarius,
	"Mattel Intellivision":                systemdefs.SystemIntellivision,
	"Microsoft MS-DOS":                    systemdefs.SystemDOS,
	"Microsoft MSX":                       systemdefs.SystemMSX,
	"Microsoft MSX2":                      systemdefs.SystemMSX2,
	"Microsoft MSX2+":                     systemdefs.SystemMSX2Plus,
	"Microsoft Windows":                   systemdefs.SystemWindows,
	"Microsoft Windows 3.x":               systemdefs.SystemWindows,
	"Microsoft Xbox":                      systemdefs.SystemXbox,
	"Microsoft Xbox 360":                  systemdefs.SystemXbox360,
	"Microsoft Xbox One":                  systemdefs.SystemXboxOne,
	"NEC PC Engine SuperGrafx":            systemdefs.SystemSuperGrafx,
	"NEC PC-8801":                         systemdefs.SystemPC88,
	"NEC PC-9801":                         systemdefs.SystemPC98,
	"NEC PC-FX":                           systemdefs.SystemPCFX,
	"NEC TurboGrafx-16":                   systemdefs.SystemTurboGrafx16,
	"NEC TurboGrafx-CD":                   systemdefs.SystemTurboGrafx16CD,
	"Nintendo 3DS":                        systemdefs.System3DS,
	"Nintendo 64":                         systemdefs.SystemNintendo64,
	"Nintendo DS":                         systemdefs.SystemNDS,
	"Nintendo Entertainment System":       systemdefs.SystemNES,
	"Nintendo Famicom Disk System":        systemdefs.SystemFDS,
	"Nintendo Game Boy":                   systemdefs.SystemGameboy,
	"Nintendo Game Boy Advance":           systemdefs.SystemGBA,
	"Nintendo Game Boy Color":             systemdefs.SystemGameboyColor,
	"Nintendo GameCube":                   systemdefs.SystemGameCube,
	"Nintendo Pokémon Mini":               systemdefs.SystemPokemonMini,
	"Nintendo Satellaview":                systemdefs.SystemSufami,
	"Nintendo Super Gameboy":              systemdefs.SystemSuperGameboy,
	"Nintendo Switch":                     systemdefs.SystemSwitch,
	"Nintendo Virtual Boy":                systemdefs.SystemVirtualBoy,
	"Nintendo Wii":                        systemdefs.SystemWii,
	"Nintendo Wii U":                      systemdefs.SystemWiiU,
	"Philips CD-i":                        systemdefs.SystemCDI,
	"Sammy Atomiswave":                    systemdefs.SystemAtomiswave,
	"ScummVM":                             systemdefs.SystemScummVM,
	"Sega 32X":                            systemdefs.SystemSega32X,
	"Sega CD":                             systemdefs.SystemMegaCD,
	"Sega Dreamcast":                      systemdefs.SystemDreamcast,
	"Sega Game Gear":                      systemdefs.SystemGameGear,
	"Sega Genesis":                        systemdefs.SystemGenesis,
	"Sega Hikaru":                         systemdefs.SystemHikaru,
	"Sega Master System":                  systemdefs.SystemMasterSystem,
	"Sega Model 2":                        systemdefs.SystemModel2,
	"Sega Model 3":                        systemdefs.SystemModel3,
	"Sega Naomi":                          systemdefs.SystemNAOMI,
	"Sega Naomi 2":                        systemdefs.SystemNAOMI2,
	"Sega Saturn":                         systemdefs.SystemSaturn,
	"Sega SG-1000":                        systemdefs.SystemSG1000,
	"Sega ST-V":                           systemdefs.SystemArcade,
	"Sega Triforce":                       systemdefs.SystemTriforce,
	"Sharp X1":                            systemdefs.SystemX1,
	"Sharp X68000":                        systemdefs.SystemX68000,
	"Sinclair ZX Spectrum":                systemdefs.SystemZXSpectrum,
	"Sinclair ZX81":                       systemdefs.SystemZX81,
	"SNK Neo Geo AES":                     systemdefs.SystemNeoGeoAES,
	"SNK Neo Geo CD":                      systemdefs.SystemNeoGeoCD,
	"SNK Neo Geo MVS":                     systemdefs.SystemNeoGeoMVS,
	"SNK Neo Geo Pocket":                  systemdefs.SystemNeoGeoPocket,
	"SNK Neo Geo Pocket Color":            systemdefs.SystemNeoGeoPocketColor,
	"Sony Playstation":                    systemdefs.SystemPSX,
	"Sony Playstation 2":                  systemdefs.SystemPS2,
	"Sony Playstation 3":                  systemdefs.SystemPS3,
	"Sony Playstation 4":                  systemdefs.SystemPS4,
	"Sony Playstation 5":                  systemdefs.SystemPS5,
	"Sony Playstation Portable":           systemdefs.SystemPSP,
	"Sony Playstation Vita":               systemdefs.SystemVita,
	"Sony PSP Minis":                      systemdefs.SystemPSP,
	"Sord M5":                             systemdefs.SystemSordM5,
	"Spectravideo":                        systemdefs.SystemSpectravideo,
	"Super Nintendo Entertainment System": systemdefs.SystemSNES,
	"Tandy TRS-80":                        systemdefs.SystemTRS80,
	"Tandy TRS-80 Color Computer":         systemdefs.SystemCoCo2,
	"Tangerine Oric Atmos":                systemdefs.SystemOric,
	"Texas Instruments TI 99/4A":          systemdefs.SystemTI994A,
	"Tiger Game.com":                      systemdefs.SystemGameCom,
	"Tomy Tutor":                          systemdefs.SystemTomyTutor,
	"Vector-06C":                          systemdefs.SystemVector06C,
	"VTech CreatiVision":                  systemdefs.SystemCreatiVision,
	"VTech Socrates":                      systemdefs.SystemSocrates,
	"VTech V.Smile":                       systemdefs.SystemVSmile,
	"Watara Supervision":                  systemdefs.SystemSuperVision,
}

// hqInstallSubdirs is the set of well-known relative paths a HyperHQ install can occupy
// under a parent directory. We probe each candidate parent (LOCALAPPDATA, PROGRAMDATA,
// drive roots, configured override) joined with these. Verify exact paths during the
// real-install validation step.
var hqInstallSubdirs = []string{
	"HyperHQ",
	filepath.Join("HyperSpin", "HyperHQ"),
	filepath.Join("HyperSpin2", "HyperHQ"),
}

func findHyperHqDir(cfg *config.Instance) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	parents := []string{
		os.Getenv("LOCALAPPDATA"),
		os.Getenv("PROGRAMDATA"),
		filepath.Join(home, "Documents"),
		home,
		"C:\\Program Files",
		"C:\\Program Files (x86)",
		"C:\\",
		"D:\\",
		"E:\\",
	}

	var dirs []string
	for _, parent := range parents {
		if parent == "" {
			continue
		}
		for _, sub := range hqInstallSubdirs {
			dirs = append(dirs, filepath.Join(parent, sub))
		}
	}

	if def := cfg.LookupLauncherDefaults("HyperHQ", nil); def.InstallDir != "" {
		dirs = append([]string{def.InstallDir}, dirs...)
	}

	for _, dir := range dirs {
		// #nosec G304 G703 -- candidate paths are well-known install locations
		// composed from Windows environment variables, used only for an existence check.
		if _, err := os.Stat(dir); err == nil {
			return dir, nil
		}
	}

	return "", errors.New("HyperHQ directory not found")
}

// pendingHqGamesRequest tracks a pending synchronous game request during scanning.
//
// Single-in-flight is enforced at the call site so concurrent requests can't
// race. The slot is matched on the pair of HyperHQ system id and reference id.
type pendingHqGamesRequest struct {
	response chan hqGamesResponse
	queryKey string
}

// HyperHqPipeServer manages named pipe communication with the HyperHQ bridge plugin.
type HyperHqPipeServer struct {
	ctx               context.Context
	listener          net.Listener
	conn              net.Conn
	onGameStarted     func(id, title, platform, systemReferenceID string)
	onGameExited      func(id, title string)
	onSystemsReceived func(systems []HqSystemInfo)
	cancel            context.CancelFunc
	writer            *bufio.Writer
	pendingGamesReq   pendingHqGamesRequest
	connMu            syncutil.Mutex
	pendingGamesReqMu syncutil.Mutex
}

// NewHyperHqPipeServer creates a new named pipe server for the HyperHQ bridge.
func NewHyperHqPipeServer() *HyperHqPipeServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &HyperHqPipeServer{
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start begins listening for HyperHQ plugin connections.
func (s *HyperHqPipeServer) Start() error {
	listener, err := winio.ListenPipe(hyperHqPipeName, nil)
	if err != nil {
		return fmt.Errorf("failed to create named pipe: %w", err)
	}

	s.listener = listener
	log.Info().Msgf("HyperHQ named pipe server listening on %s", hyperHqPipeName)

	go s.acceptConnections()

	return nil
}

// Stop gracefully shuts down the pipe server.
func (s *HyperHqPipeServer) Stop() {
	s.cancel()

	s.connMu.Lock()
	if s.conn != nil {
		if err := s.conn.Close(); err != nil {
			log.Warn().Err(err).Msg("error closing HyperHQ pipe connection")
		}
		s.conn = nil
		s.writer = nil
	}
	s.connMu.Unlock()

	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			log.Warn().Err(err).Msg("error closing HyperHQ pipe listener")
		}
	}

	log.Debug().Msg("HyperHQ named pipe server stopped")
}

// SetGameStartedHandler sets the callback for game started events.
func (s *HyperHqPipeServer) SetGameStartedHandler(
	handler func(id, title, platform, systemReferenceID string),
) {
	s.onGameStarted = handler
}

// SetGameExitedHandler sets the callback for game exited events.
func (s *HyperHqPipeServer) SetGameExitedHandler(handler func(id, title string)) {
	s.onGameExited = handler
}

// SetSystemsReceivedHandler sets the callback for the Systems list event.
func (s *HyperHqPipeServer) SetSystemsReceivedHandler(handler func(systems []HqSystemInfo)) {
	s.onSystemsReceived = handler
}

// RequestSystems sends a GetSystems command to the HyperHQ plugin.
func (s *HyperHqPipeServer) RequestSystems() error {
	s.connMu.Lock()
	defer s.connMu.Unlock()

	if s.writer == nil {
		return errors.New("HyperHQ plugin not connected")
	}

	cmd := hqCommand{Command: "GetSystems"}
	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal GetSystems command: %w", err)
	}

	if _, err := s.writer.WriteString(string(data) + "\n"); err != nil {
		return fmt.Errorf("failed to write GetSystems command: %w", err)
	}

	if err := s.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush GetSystems command: %w", err)
	}

	log.Debug().Msg("sent GetSystems command to HyperHQ plugin")
	return nil
}

// RequestGamesForSystemSync sends a GetGamesForSystem command and waits for the response.
// Used by the scanner to query games on-demand per-system.
func (s *HyperHqPipeServer) RequestGamesForSystemSync(
	ctx context.Context,
	target hqSystemQueryTarget,
) ([]HqGameInfo, error) {
	s.connMu.Lock()
	if s.writer == nil {
		s.connMu.Unlock()
		return nil, errors.New("HyperHQ plugin not connected")
	}
	s.connMu.Unlock()

	respChan := make(chan hqGamesResponse, 1)

	s.pendingGamesReqMu.Lock()
	if s.pendingGamesReq.response != nil {
		s.pendingGamesReqMu.Unlock()
		return nil, errors.New("games request already in flight")
	}
	queryKey := hqSystemQueryKey(target)
	s.pendingGamesReq.queryKey = queryKey
	s.pendingGamesReq.response = respChan
	s.pendingGamesReqMu.Unlock()

	defer func() {
		s.pendingGamesReqMu.Lock()
		s.pendingGamesReq.queryKey = ""
		s.pendingGamesReq.response = nil
		s.pendingGamesReqMu.Unlock()
	}()

	cmd := hqCommand{
		Command:           "GetGamesForSystem",
		SystemID:          target.ID,
		SystemName:        target.Name,
		SystemReferenceID: target.ReferenceID,
	}
	data, err := json.Marshal(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal GetGamesForSystem command: %w", err)
	}

	s.connMu.Lock()
	if s.writer == nil {
		s.connMu.Unlock()
		return nil, errors.New("HyperHQ plugin not connected")
	}
	if _, err := s.writer.WriteString(string(data) + "\n"); err != nil {
		s.connMu.Unlock()
		return nil, fmt.Errorf("failed to write GetGamesForSystem command: %w", err)
	}
	if err := s.writer.Flush(); err != nil {
		s.connMu.Unlock()
		return nil, fmt.Errorf("failed to flush GetGamesForSystem command: %w", err)
	}
	s.connMu.Unlock()

	log.Debug().Msgf(
		"sent GetGamesForSystem command for HyperHQ system: id=%q referenceId=%q",
		target.ID, target.ReferenceID,
	)

	select {
	case resp := <-respChan:
		if resp.Error != "" {
			return nil, fmt.Errorf("HyperHQ plugin error: %s", resp.Error)
		}
		return resp.Games, nil
	case <-time.After(30 * time.Second):
		return nil, errors.New("timeout waiting for games from HyperHQ")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// LaunchGame sends a launch command to the HyperHQ plugin.
func (s *HyperHqPipeServer) LaunchGame(gameID string) error {
	s.connMu.Lock()
	defer s.connMu.Unlock()

	if s.writer == nil {
		return errors.New("HyperHQ plugin not connected")
	}

	cmd := hqCommand{Command: "Launch", ID: gameID}
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

// IsConnected returns true if the HyperHQ plugin is connected.
func (s *HyperHqPipeServer) IsConnected() bool {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	return s.conn != nil
}

func (s *HyperHqPipeServer) sendPing() error {
	s.connMu.Lock()
	defer s.connMu.Unlock()

	if s.writer == nil {
		return errors.New("writer not available")
	}

	cmd := hqCommand{Command: "Ping"}
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

func (s *HyperHqPipeServer) acceptConnections() {
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
				log.Warn().Err(err).Msg("failed to accept HyperHQ pipe connection")
				continue
			}
		}

		log.Info().Msg("HyperHQ plugin connected")

		s.connMu.Lock()
		if s.conn != nil {
			if closeErr := s.conn.Close(); closeErr != nil {
				log.Warn().Err(closeErr).Msg("error closing previous HyperHQ connection")
			}
		}
		s.conn = conn
		s.writer = bufio.NewWriter(conn)
		s.connMu.Unlock()

		// Request system mappings as soon as the bridge is ready.
		if err := s.RequestSystems(); err != nil {
			log.Warn().Err(err).Msg("failed to request systems from HyperHQ plugin")
		}

		go s.handleConnection(conn)
	}
}

func (s *HyperHqPipeServer) handleConnection(conn net.Conn) {
	defer func() {
		s.connMu.Lock()
		if s.conn == conn {
			s.conn = nil
			s.writer = nil
			log.Info().Msg("HyperHQ plugin disconnected")
		}
		s.connMu.Unlock()
		if err := conn.Close(); err != nil {
			log.Debug().Err(err).Msg("error closing HyperHQ pipe connection")
		}
	}()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	scanDone := make(chan struct{})

	go func() {
		defer close(scanDone)
		scanner := bufio.NewScanner(conn)
		scanner.Buffer(make([]byte, 4096), hyperHqScannerMaxBuffer)

		for scanner.Scan() {
			select {
			case <-s.ctx.Done():
				return
			default:
			}

			s.handleEvent(scanner.Text())
		}

		if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
			log.Warn().Err(err).Msg("error reading from HyperHQ pipe")
		}
	}()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-scanDone:
			return
		case <-ticker.C:
			if err := s.sendPing(); err != nil {
				log.Debug().Err(err).Msg("failed to send heartbeat ping")
				return
			}
		}
	}
}

func (s *HyperHqPipeServer) handleEvent(data string) {
	var event hqEvent
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		log.Warn().Err(err).Msg("failed to unmarshal HyperHQ event")
		return
	}

	if event.Event == "" {
		log.Warn().Msg("HyperHQ event missing 'Event' field")
		return
	}

	switch event.Event {
	case "MediaStarted":
		log.Info().Msgf("HyperHQ game started: %s (ID: %s)", event.Title, event.ID)
		if s.onGameStarted != nil {
			s.onGameStarted(event.ID, event.Title, event.Platform, event.SystemReferenceID)
		}

	case "MediaStopped":
		log.Info().Msgf("HyperHQ game stopped: %s (ID: %s)", event.Title, event.ID)
		if s.onGameExited != nil {
			s.onGameExited(event.ID, event.Title)
		}

	case "Systems":
		var systemsEvent hqSystemsEvent
		if err := json.Unmarshal([]byte(data), &systemsEvent); err != nil {
			log.Warn().Err(err).Msg("failed to unmarshal HyperHQ Systems event")
			return
		}

		log.Info().Msgf("received %d systems from HyperHQ", len(systemsEvent.Systems))

		if s.onSystemsReceived != nil {
			s.onSystemsReceived(systemsEvent.Systems)
		}

	case "Games":
		var gamesEvent hqGamesEvent
		if err := json.Unmarshal([]byte(data), &gamesEvent); err != nil {
			log.Warn().Err(err).Msg("failed to unmarshal HyperHQ Games event")
			return
		}

		if gamesEvent.Error != "" {
			log.Warn().Msgf("HyperHQ plugin error for system %s: %s",
				gamesEvent.SystemReferenceID, gamesEvent.Error)
		} else {
			log.Debug().Msgf("received %d games from HyperHQ for system %s",
				len(gamesEvent.Games), gamesEvent.SystemReferenceID)
		}

		s.pendingGamesReqMu.Lock()
		if s.pendingGamesReq.response != nil &&
			s.pendingGamesReq.queryKey == hqSystemQueryKey(hqSystemQueryTarget{
				ID:          gamesEvent.SystemID,
				Name:        gamesEvent.SystemName,
				ReferenceID: gamesEvent.SystemReferenceID,
			}) {
			s.pendingGamesReq.response <- hqGamesResponse{
				Games: gamesEvent.Games,
				Error: gamesEvent.Error,
			}
		}
		s.pendingGamesReqMu.Unlock()

	default:
		log.Debug().Msgf("unknown HyperHQ event type: %s", event.Event)
	}
}

// buildHqMappings derives the runtime maps from a HyperHQ Systems event.
// Pure function so it can be unit-tested without touching Platform state.
func shouldIgnoreEmptyHqSystemsRefresh(
	systems []HqSystemInfo,
	hqSystemKeyToSystem map[string]string,
	systemToHqSystems map[string][]hqSystemQueryTarget,
) bool {
	return len(systems) == 0 && (len(hqSystemKeyToSystem) > 0 || len(systemToHqSystems) > 0)
}

func buildHqMappings(
	systems []HqSystemInfo,
) (hqSystemKeyToSystem map[string]string, systemToHqSystems map[string][]hqSystemQueryTarget) {
	hqSystemKeyToSystem = make(map[string]string)
	systemToHqSystems = make(map[string][]hqSystemQueryTarget)
	lookup := buildHqSystemLookup()

	for _, sys := range systems {
		sysID := systemdefs.SystemCustom
		for _, candidate := range []string{sys.Platform, sys.Name, sys.ReferenceID, sys.ID} {
			if mappedID, ok := lookup[hqSystemLookupKey(candidate)]; ok {
				sysID = mappedID
				break
			}
		}

		if sys.ID != "" || sys.Name != "" || sys.ReferenceID != "" {
			systemToHqSystems[sysID] = append(systemToHqSystems[sysID], hqSystemQueryTarget{
				ID:          sys.ID,
				Name:        sys.Name,
				ReferenceID: sys.ReferenceID,
			})
		}
		if sys.ID != "" {
			hqSystemKeyToSystem[sys.ID] = sysID
		}
		if sys.ReferenceID != "" {
			hqSystemKeyToSystem[sys.ReferenceID] = sysID
		}
	}

	return hqSystemKeyToSystem, systemToHqSystems
}

func hqSystemQueryKey(target hqSystemQueryTarget) string {
	return target.ID + "\x00" + target.Name + "\x00" + target.ReferenceID
}

func buildHqSystemLookup() map[string]string {
	lookup := make(map[string]string, len(hqSystemAliases)+len(systemdefs.Systems))
	for alias, sysID := range hqSystemAliases {
		lookup[hqSystemLookupKey(alias)] = sysID
	}
	for sysID := range systemdefs.Systems {
		lookup[hqSystemLookupKey(sysID)] = sysID
	}
	return lookup
}

func hqSystemLookupKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func (p *Platform) initHyperHqPipe(cfg *config.Instance) {
	hqDir, err := findHyperHqDir(cfg)
	if err != nil {
		log.Debug().Msg("HyperHQ not detected, skipping named pipe server initialization")
		return
	}
	log.Debug().Msgf("HyperHQ detected at: %s", hqDir)

	pipe := NewHyperHqPipeServer()

	pipe.SetGameStartedHandler(func(id, title, platform, systemReferenceID string) {
		// Resolve the Zaparoo system ID, accepting either HyperHQ's system id or
		// reference id because event payloads have varied across app versions.
		p.hqMappingsMu.RLock()
		systemID, ok := p.hqSystemKeyToSystem[systemReferenceID]
		p.hqMappingsMu.RUnlock()

		if !ok {
			if sysID, found := buildHqSystemLookup()[hqSystemLookupKey(platform)]; found {
				systemID = sysID
			} else {
				systemID = systemdefs.SystemCustom
				log.Warn().Msgf(
					"using Custom system for unmapped HyperHQ system: refId=%q platform=%q",
					systemReferenceID, platform,
				)
			}
		}

		systemName := platform
		if systemID != systemdefs.SystemCustom {
			if systemMeta, err := assets.GetSystemMetadata(systemID); err == nil {
				systemName = systemMeta.Name
			} else {
				log.Debug().Err(err).Msgf("no system metadata for: %s", systemID)
			}
		}
		if systemName == "" {
			systemName = systemID
		}

		virtualPath := virtualpath.CreateVirtualPath(shared.SchemeHyperHq, id, title)

		activeMedia := models.NewActiveMedia(
			systemID,
			systemName,
			virtualPath,
			title,
			"HyperHQ",
		)

		log.Info().Msgf(
			"HyperHQ game started: SystemID='%s', SystemName='%s', Path='%s', Name='%s', LauncherID='%s'",
			activeMedia.SystemID, activeMedia.SystemName, activeMedia.Path,
			activeMedia.Name, activeMedia.LauncherID,
		)

		p.setActiveMedia(activeMedia)
	})

	pipe.SetGameExitedHandler(func(_, title string) {
		log.Info().Msgf("HyperHQ game stopped: %s", title)
		p.setActiveMedia(nil)
	})

	pipe.SetSystemsReceivedHandler(func(systems []HqSystemInfo) {
		p.hqMappingsMu.RLock()
		ignoreEmpty := shouldIgnoreEmptyHqSystemsRefresh(systems, p.hqSystemKeyToSystem, p.systemToHqSystems)
		p.hqMappingsMu.RUnlock()
		if ignoreEmpty {
			log.Warn().Msg("ignoring empty HyperHQ systems response; keeping existing mappings")
			return
		}

		systemKeyToSys, sysToHqSystems := buildHqMappings(systems)

		p.hqMappingsMu.Lock()
		p.hqSystemKeyToSystem = systemKeyToSys
		p.systemToHqSystems = sysToHqSystems
		p.hqMappingsMu.Unlock()

		log.Info().Msgf("built %d HyperHQ system mappings (%d Zaparoo systems covered)",
			len(systemKeyToSys), len(sysToHqSystems))
		for _, sys := range systems {
			queryID := sys.ID
			if queryID == "" {
				queryID = sys.ReferenceID
			}
			if systemKeyToSys[queryID] == systemdefs.SystemCustom {
				log.Warn().Msgf(
					"using Custom system for unmapped HyperHQ system: name=%q referenceId=%q platform=%q",
					sys.Name, sys.ReferenceID, sys.Platform,
				)
			}
		}
	})

	if err := pipe.Start(); err != nil {
		log.Warn().Err(err).Msg("failed to start HyperHQ named pipe server")
		return
	}

	p.hyperHqPipeLock.Lock()
	p.hyperHqPipe = pipe
	p.hyperHqPipeLock.Unlock()

	log.Info().Msg("HyperHQ named pipe server initialized")
}

// NewHyperHqLauncher creates the HyperHQ launcher.
func (p *Platform) NewHyperHqLauncher() platforms.Launcher {
	return platforms.Launcher{
		ID:                 "HyperHQ",
		Schemes:            []string{shared.SchemeHyperHq},
		SkipFilesystemScan: true,
		Lifecycle:          platforms.LifecycleFireAndForget,
		Scanner: func(
			ctx context.Context,
			_ *config.Instance,
			systemID string,
			results []platforms.ScanResult,
		) ([]platforms.ScanResult, error) {
			p.hqMappingsMu.RLock()
			hqSystems := append([]hqSystemQueryTarget(nil), p.systemToHqSystems[systemID]...)
			p.hqMappingsMu.RUnlock()

			if len(hqSystems) == 0 {
				return results, nil
			}

			p.hyperHqPipeLock.Lock()
			pipe := p.hyperHqPipe
			p.hyperHqPipeLock.Unlock()

			if pipe == nil || !pipe.IsConnected() {
				log.Debug().Msgf(
					"HyperHQ plugin not connected, skipping scan for system %s", systemID,
				)
				return results, nil
			}

			for _, hqSystem := range hqSystems {
				games, err := pipe.RequestGamesForSystemSync(ctx, hqSystem)
				if err != nil {
					log.Debug().Err(err).Msgf(
						"HyperHQ query failed for system id=%q referenceId=%q",
						hqSystem.ID, hqSystem.ReferenceID,
					)
					continue
				}
				for _, game := range games {
					results = append(results, platforms.ScanResult{
						Path:  virtualpath.CreateVirtualPath(shared.SchemeHyperHq, game.ID, game.Title),
						Name:  game.Title,
						NoExt: true,
					})
				}
				log.Debug().Msgf(
					"scanned %d games from HyperHQ for system id=%q referenceId=%q",
					len(games), hqSystem.ID, hqSystem.ReferenceID,
				)
			}

			return results, nil
		},
		Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			id, err := virtualpath.ExtractSchemeID(path, shared.SchemeHyperHq)
			if err != nil {
				return nil, fmt.Errorf("failed to extract HyperHQ game ID from path: %w", err)
			}

			p.hyperHqPipeLock.Lock()
			pipe := p.hyperHqPipe
			p.hyperHqPipeLock.Unlock()

			if pipe == nil || !pipe.IsConnected() {
				return nil, errors.New("HyperHQ plugin not connected")
			}

			if err := pipe.LaunchGame(id); err != nil {
				return nil, fmt.Errorf("failed to send launch command to HyperHQ: %w", err)
			}

			return nil, nil //nolint:nilnil // HyperHQ launches don't return a process handle
		},
	}
}
