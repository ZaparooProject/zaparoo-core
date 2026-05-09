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
	SystemReferenceID string `json:"SystemReferenceId,omitempty"`
}

// HqSystemInfo represents a HyperHQ system as reported by the plugin.
//
//nolint:tagliatelle // JSON tags must match HyperHQ plugin structure (PascalCase)
type HqSystemInfo struct {
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
	SystemReferenceID string       `json:"SystemReferenceId"`
	Error             string       `json:"Error,omitempty"`
	Games             []HqGameInfo `json:"Games"`
}

// hqGamesResponse is used internally for the synchronous request channel.
type hqGamesResponse struct {
	Error string
	Games []HqGameInfo
}

// hqSysMap maps Zaparoo system IDs to HyperHQ canonical platform names.
// Seeded from lbSysMap because HyperHQ ships with conventions inherited from the
// HyperSpin / LaunchBox ecosystem; this is expected to diverge once a live
// `getSystems` response is observed during validation. Both maps are read-only
// at runtime so sharing the reference is safe.
var hqSysMap = lbSysMap

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
// race. The slot is matched on systemReferenceID — adequate so long as no
// caller times out and immediately retries the same system, since a late
// Games event from the cancelled request is indistinguishable from a fresh
// response on the wire. A robust fix would carry a per-request token in the
// pipe protocol; revisit once the HyperHQ integration is validated against a
// real install.
type pendingHqGamesRequest struct {
	response          chan hqGamesResponse
	systemReferenceID string
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
	systemReferenceID string,
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
	s.pendingGamesReq.systemReferenceID = systemReferenceID
	s.pendingGamesReq.response = respChan
	s.pendingGamesReqMu.Unlock()

	defer func() {
		s.pendingGamesReqMu.Lock()
		s.pendingGamesReq.systemReferenceID = ""
		s.pendingGamesReq.response = nil
		s.pendingGamesReqMu.Unlock()
	}()

	cmd := hqCommand{Command: "GetGamesForSystem", SystemReferenceID: systemReferenceID}
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

	log.Debug().Msgf("sent GetGamesForSystem command for: %s", systemReferenceID)

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
			s.pendingGamesReq.systemReferenceID == gamesEvent.SystemReferenceID {
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
func buildHqMappings(
	systems []HqSystemInfo,
) (refIDToSystem map[string]string, systemToRefIDs map[string][]string) {
	refIDToSystem = make(map[string]string)
	systemToRefIDs = make(map[string][]string)

	for _, sys := range systems {
		canonical := sys.Platform
		if canonical == "" {
			canonical = sys.Name
		}

		for sysID, hqName := range hqSysMap {
			if strings.EqualFold(hqName, canonical) {
				refIDToSystem[sys.ReferenceID] = sysID
				systemToRefIDs[sysID] = append(systemToRefIDs[sysID], sys.ReferenceID)
				break
			}
		}
	}

	return refIDToSystem, systemToRefIDs
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
		// Resolve the Zaparoo system ID, preferring the live mapping by ReferenceId,
		// falling back to the canonical Platform name.
		p.hqMappingsMu.RLock()
		systemID, ok := p.hqRefIDToSystem[systemReferenceID]
		p.hqMappingsMu.RUnlock()

		if !ok {
			for sysID, hqName := range hqSysMap {
				if strings.EqualFold(hqName, platform) {
					systemID = sysID
					ok = true
					break
				}
			}
		}

		if !ok {
			log.Debug().Msgf(
				"unknown HyperHQ system: refId=%q platform=%q, skipping ActiveMedia",
				systemReferenceID, platform,
			)
			return
		}

		systemName := platform
		if systemMeta, err := assets.GetSystemMetadata(systemID); err == nil {
			systemName = systemMeta.Name
		} else {
			log.Debug().Err(err).Msgf("no system metadata for: %s", systemID)
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
		refToSys, sysToRefs := buildHqMappings(systems)

		p.hqMappingsMu.Lock()
		p.hqRefIDToSystem = refToSys
		p.systemToHqRefIDs = sysToRefs
		p.hqMappingsMu.Unlock()

		log.Info().Msgf("built %d HyperHQ system mappings (%d Zaparoo systems covered)",
			len(refToSys), len(sysToRefs))
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
			refIDs := append([]string(nil), p.systemToHqRefIDs[systemID]...)
			p.hqMappingsMu.RUnlock()

			if len(refIDs) == 0 {
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

			for _, refID := range refIDs {
				games, err := pipe.RequestGamesForSystemSync(ctx, refID)
				if err != nil {
					log.Debug().Err(err).Msgf("HyperHQ query failed for system %s", refID)
					continue
				}
				for _, game := range games {
					results = append(results, platforms.ScanResult{
						Path:  virtualpath.CreateVirtualPath(shared.SchemeHyperHq, game.ID, game.Title),
						Name:  game.Title,
						NoExt: true,
					})
				}
				log.Debug().Msgf("scanned %d games from HyperHQ for system %s", len(games), refID)
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
