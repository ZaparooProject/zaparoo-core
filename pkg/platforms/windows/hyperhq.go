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
	"strconv"
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

	// hyperHqLaunchTimeout bounds waiting for the bridge to move the wheel
	// and have HyperHQ accept the launch. It sits above the bridge's own
	// limits for both steps so the bridge's answer arrives first.
	hyperHqLaunchTimeout = 20 * time.Second

	// hyperHqStopTimeout bounds waiting for HyperHQ to stop a game. It sits
	// above the bridge's stop limit and inside windowsStopBudget.
	hyperHqStopTimeout = 12 * time.Second
)

// HyperHQ wire-protocol types. PascalCase to match the bridge plugin's serialiser.
//
//nolint:tagliatelle // JSON tags must match HyperHQ plugin structure (PascalCase)
type hqEvent struct {
	Event             string `json:"Event"`
	ID                string `json:"Id,omitempty"`
	RequestID         string `json:"RequestId,omitempty"`
	Title             string `json:"Title,omitempty"`
	Platform          string `json:"Platform,omitempty"`
	SystemReferenceID string `json:"SystemReferenceId,omitempty"`
	Error             string `json:"Error,omitempty"`
	WasRunning        bool   `json:"WasRunning,omitempty"`
	Stopped           bool   `json:"Stopped,omitempty"`
}

//nolint:tagliatelle // JSON tags must match HyperHQ plugin structure (PascalCase)
type hqCommand struct {
	Command           string `json:"Command"`
	ID                string `json:"Id,omitempty"`
	RequestID         string `json:"RequestId,omitempty"`
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

// pendingHqResult is a Launch or Stop command waiting for the bridge to
// report its outcome. The request id stops a result that arrives after its
// caller gave up from being taken as the answer to a later command.
type pendingHqResult struct {
	response  chan hqEvent
	requestID string
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
	pendingLaunch     pendingHqResult
	pendingStop       pendingHqResult
	activeGameID      string
	nextRequestID     uint64
	connMu            syncutil.Mutex
	pendingGamesReqMu syncutil.Mutex
	pendingResultMu   syncutil.Mutex
	activeGameMu      syncutil.Mutex
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

// LaunchGame asks the bridge to move the HyperSpin wheel to a game and then
// launch it, and waits for the bridge to report whether HyperHQ accepted
// both steps. The game itself is reported running by the MediaStarted event.
func (s *HyperHqPipeServer) LaunchGame(ctx context.Context, gameID string) error {
	result, err := s.awaitResult(ctx, &s.pendingLaunch, &hqCommand{Command: "Launch", ID: gameID}, hyperHqLaunchTimeout)
	if err != nil {
		return err
	}
	if result.Error != "" {
		return fmt.Errorf("HyperHQ launch failed: %s", result.Error)
	}
	return nil
}

// StopGame asks HyperHQ to stop the running HyperSpin game and waits for the
// outcome. Going through HyperHQ lets HyperSpin run its own exit handling for
// the emulator rather than Core killing a process it never started.
func (s *HyperHqPipeServer) StopGame(ctx context.Context) error {
	result, err := s.awaitResult(ctx, &s.pendingStop, &hqCommand{Command: "Stop"}, hyperHqStopTimeout)
	if err != nil {
		return err
	}
	return hqStopOutcome(&result)
}

// hqStopOutcome turns a StopResult into a Kill result. HyperHQ reports
// success without doing anything when no game is running, which counts as
// stopped; a game it reports still running does not.
func hqStopOutcome(result *hqEvent) error {
	switch {
	case result.Error != "":
		return fmt.Errorf("HyperHQ stop failed: %s", result.Error)
	case result.Stopped, !result.WasRunning:
		return nil
	default:
		return errors.New("HyperHQ reported the game still running")
	}
}

// awaitResult sends cmd to the bridge and waits for the result event it
// triggers. Only one command per slot may be outstanding.
func (s *HyperHqPipeServer) awaitResult(
	ctx context.Context,
	slot *pendingHqResult,
	cmd *hqCommand,
	timeout time.Duration,
) (hqEvent, error) {
	respChan := make(chan hqEvent, 1)

	s.pendingResultMu.Lock()
	if slot.response != nil {
		s.pendingResultMu.Unlock()
		return hqEvent{}, fmt.Errorf("HyperHQ %s already in flight", cmd.Command)
	}
	s.nextRequestID++
	cmd.RequestID = strconv.FormatUint(s.nextRequestID, 10)
	*slot = pendingHqResult{requestID: cmd.RequestID, response: respChan}
	s.pendingResultMu.Unlock()

	defer func() {
		s.pendingResultMu.Lock()
		*slot = pendingHqResult{}
		s.pendingResultMu.Unlock()
	}()

	if err := s.sendCommand(cmd); err != nil {
		return hqEvent{}, err
	}
	log.Debug().Msgf("sent HyperHQ %s command: id=%q requestId=%s", cmd.Command, cmd.ID, cmd.RequestID)

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case result := <-respChan:
		return result, nil
	case <-timer.C:
		return hqEvent{}, fmt.Errorf("timeout waiting for HyperHQ %s result", cmd.Command)
	case <-ctx.Done():
		return hqEvent{}, fmt.Errorf("HyperHQ %s cancelled: %w", cmd.Command, ctx.Err())
	case <-s.ctx.Done():
		return hqEvent{}, fmt.Errorf("HyperHQ %s cancelled: %w", cmd.Command, s.ctx.Err())
	}
}

// deliverResult hands a Launch or Stop outcome to the command waiting for it.
//
// It runs on the pipe reader goroutine, so the send must never block: a
// bridge that reported an outcome twice would otherwise wedge every later
// event behind it. Results for a request nobody is waiting on are dropped.
func (s *HyperHqPipeServer) deliverResult(slot *pendingHqResult, result *hqEvent) {
	s.pendingResultMu.Lock()
	defer s.pendingResultMu.Unlock()

	if slot.response == nil || slot.requestID != result.RequestID {
		log.Debug().Msgf("dropping stale HyperHQ %s for request %q", result.Event, result.RequestID)
		return
	}

	select {
	case slot.response <- *result:
	default:
	}
}

// sendCommand writes one command line to the bridge. The connection check and
// the write share one hold of connMu so a disconnect in between cannot leave a
// nil writer to dereference.
func (s *HyperHqPipeServer) sendCommand(cmd *hqCommand) error {
	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal %s command: %w", cmd.Command, err)
	}

	s.connMu.Lock()
	defer s.connMu.Unlock()

	if s.writer == nil {
		return errors.New("HyperHQ plugin not connected")
	}
	if _, err := s.writer.WriteString(string(data) + "\n"); err != nil {
		return fmt.Errorf("failed to write %s command: %w", cmd.Command, err)
	}
	if err := s.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush %s command: %w", cmd.Command, err)
	}
	return nil
}

// setActiveGame records the game HyperHQ last reported starting.
func (s *HyperHqPipeServer) setActiveGame(id string) {
	s.activeGameMu.Lock()
	s.activeGameID = id
	s.activeGameMu.Unlock()
}

// endActiveGame clears the active game when id still owns it. A close for an
// earlier game must not clear a newer one; a close without an id cannot be
// attributed, so it is taken as the active game's.
func (s *HyperHqPipeServer) endActiveGame(id string) bool {
	s.activeGameMu.Lock()
	defer s.activeGameMu.Unlock()

	if id != "" && s.activeGameID != id {
		return false
	}
	s.activeGameID = ""
	return true
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
		s.setActiveGame(event.ID)
		if s.onGameStarted != nil {
			s.onGameStarted(event.ID, event.Title, event.Platform, event.SystemReferenceID)
		}

	case "MediaStopped":
		log.Info().Msgf("HyperHQ game stopped: %s (ID: %s)", event.Title, event.ID)
		if !s.endActiveGame(event.ID) {
			log.Debug().Msgf("ignoring stale HyperHQ exit for: %s", event.ID)
			return
		}
		if s.onGameExited != nil {
			s.onGameExited(event.ID, event.Title)
		}

	case "LaunchResult":
		s.deliverResult(&s.pendingLaunch, &event)

	case "StopResult":
		s.deliverResult(&s.pendingStop, &event)

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

		// A game started from the HyperSpin wheel never went through
		// LaunchMedia, which is the only other place the launcher is
		// recorded. Without it, stopping falls through to clearing active
		// media while the game keeps running.
		launcher := p.NewHyperHqLauncher()
		p.setLastLauncher(&launcher)
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

// stopHyperHqGame asks HyperHQ to end the running game. HyperHQ stops
// whatever HyperSpin is running, so this also covers games started from the
// wheel rather than through Core. Returning an error lets StopActiveLauncher
// report that the game is still running.
func (p *Platform) stopHyperHqGame(_ *config.Instance) error {
	p.hyperHqPipeLock.Lock()
	pipe := p.hyperHqPipe
	p.hyperHqPipeLock.Unlock()

	if pipe == nil || !pipe.IsConnected() {
		return errors.New("HyperHQ plugin not connected")
	}
	return pipe.StopGame(context.Background())
}

// NewHyperHqLauncher creates the HyperHQ launcher.
func (p *Platform) NewHyperHqLauncher() platforms.Launcher {
	return platforms.Launcher{
		ID:                 "HyperHQ",
		Schemes:            []string{shared.SchemeHyperHq},
		Test:               shared.SchemeIDTest(shared.SchemeHyperHq),
		SkipFilesystemScan: true,
		// HyperHQ owns the game process and reports its lifecycle through the
		// bridge, so ActiveMedia comes from MediaStarted rather than from the
		// launch command being accepted.
		Lifecycle: platforms.LifecycleExternal,
		Kill:      p.stopHyperHqGame,
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

			if err := pipe.LaunchGame(context.Background(), id); err != nil {
				return nil, fmt.Errorf("failed to launch HyperHQ game %s: %w", id, err)
			}

			return nil, nil //nolint:nilnil // HyperHQ launches don't return a process handle
		},
	}
}
