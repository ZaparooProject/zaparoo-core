//go:build linux

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

package replayos

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
)

const (
	coresDir         = "/opt/replay/cores"
	menuCore         = "replay_libretro.so"
	replayBinaryPath = "/opt/replay/replay"
	recFileDir       = "_recent"
	trackerInterval  = 2 * time.Second
	healthCheckDelay = 5 * time.Second
)

//nolint:gochecknoglobals // Package-level configuration
var coreSystemMap = map[string]string{
	// Arcade
	"fbneo_libretro.so":         systemdefs.SystemArcade,
	"mamearcade_libretro.so":    systemdefs.SystemArcade,
	"mame2003_plus_libretro.so": systemdefs.SystemArcade,
	"mame_libretro.so":          systemdefs.SystemArcade,
	"mame2010_libretro.so":      systemdefs.SystemArcade,
	"flycast_libretro.so":       systemdefs.SystemDreamcast,
	"flycast_gles2_libretro.so": systemdefs.SystemDreamcast,
	"flycast_2021_libretro.so":  systemdefs.SystemDreamcast,
	"flycast2_libretro.so":      systemdefs.SystemDreamcast,
	"lr_flycast_libretro.so":    systemdefs.SystemDreamcast,

	// Nintendo
	"fceumm_libretro.so":           systemdefs.SystemNES,
	"nestopia_libretro.so":         systemdefs.SystemNES,
	"quicknes_libretro.so":         systemdefs.SystemNES,
	"snes9x_libretro.so":           systemdefs.SystemSNES,
	"snes9x2010_libretro.so":       systemdefs.SystemSNES,
	"bsnes_libretro.so":            systemdefs.SystemSNES,
	"mgba_libretro.so":             systemdefs.SystemGBA,
	"vbam_libretro.so":             systemdefs.SystemGBA,
	"gpsp_libretro.so":             systemdefs.SystemGBA,
	"gambatte_libretro.so":         systemdefs.SystemGameboy,
	"sameboy_libretro.so":          systemdefs.SystemGameboy,
	"mupen64plus_next_libretro.so": systemdefs.SystemNintendo64,
	"parallel_n64_libretro.so":     systemdefs.SystemNintendo64,
	"melondsds_libretro.so":        systemdefs.SystemNDS,
	"melonds_libretro.so":          systemdefs.SystemNDS,
	"desmume_libretro.so":          systemdefs.SystemNDS,

	// Sega
	"genesis_plus_gx_libretro.so":      systemdefs.SystemGenesis,
	"genesis_plus_gx_wide_libretro.so": systemdefs.SystemGenesis,
	"picodrive_libretro.so":            systemdefs.SystemGenesis,
	"blastem_libretro.so":              systemdefs.SystemGenesis,
	"mednafen_saturn_libretro.so":      systemdefs.SystemSaturn,
	"beetle_saturn_libretro.so":        systemdefs.SystemSaturn,
	"yabause_libretro.so":              systemdefs.SystemSaturn,
	"gearsystem_libretro.so":           systemdefs.SystemMasterSystem,
	"smsplus_libretro.so":              systemdefs.SystemMasterSystem,

	// Sony
	"pcsx_rearmed_libretro.so": systemdefs.SystemPSX,
	"mednafen_psx_libretro.so": systemdefs.SystemPSX,
	"beetle_psx_libretro.so":   systemdefs.SystemPSX,
	"swanstation_libretro.so":  systemdefs.SystemPSX,

	// Atari
	"stella_libretro.so":        systemdefs.SystemAtari2600,
	"stella2014_libretro.so":    systemdefs.SystemAtari2600,
	"prosystem_libretro.so":     systemdefs.SystemAtari7800,
	"atari800_libretro.so":      systemdefs.SystemAtari5200,
	"handy_libretro.so":         systemdefs.SystemAtariLynx,
	"beetle_lynx_libretro.so":   systemdefs.SystemAtariLynx,
	"mednafen_lynx_libretro.so": systemdefs.SystemAtariLynx,
	"virtualjaguar_libretro.so": systemdefs.SystemJaguar,

	// NEC
	"mednafen_pce_libretro.so":      systemdefs.SystemTurboGrafx16,
	"mednafen_pce_fast_libretro.so": systemdefs.SystemTurboGrafx16,
	"beetle_pce_libretro.so":        systemdefs.SystemTurboGrafx16,
	"beetle_pce_fast_libretro.so":   systemdefs.SystemTurboGrafx16,

	// SNK
	"fbneo_neogeo_libretro.so": systemdefs.SystemNeoGeo,
	"neocd_libretro.so":        systemdefs.SystemNeoGeoCD,
	"mednafen_ngp_libretro.so": systemdefs.SystemNeoGeoPocket,
	"beetle_ngp_libretro.so":   systemdefs.SystemNeoGeoPocket,
	"race_libretro.so":         systemdefs.SystemNeoGeoPocket,

	// Computers
	"vice_x64_libretro.so":     systemdefs.SystemC64,
	"vice_x64sc_libretro.so":   systemdefs.SystemC64,
	"vice_xscpu64_libretro.so": systemdefs.SystemC64,
	"dosbox_pure_libretro.so":  systemdefs.SystemDOS,
	"dosbox_svn_libretro.so":   systemdefs.SystemDOS,
	"scummvm_libretro.so":      systemdefs.SystemScummVM,
	"fmsx_libretro.so":         systemdefs.SystemMSX,
	"bluemsx_libretro.so":      systemdefs.SystemMSX,
	"fuse_libretro.so":         systemdefs.SystemZXSpectrum,
	"px68k_libretro.so":        systemdefs.SystemX68000,
	"puae_libretro.so":         systemdefs.SystemAmiga,
	"uae4arm_libretro.so":      systemdefs.SystemAmiga,
	"cap32_libretro.so":        systemdefs.SystemAmstrad,

	// Other
	"opera_libretro.so":    systemdefs.System3DO,
	"4do_libretro.so":      systemdefs.System3DO,
	"cdi2015_libretro.so":  systemdefs.SystemCDI,
	"same_cdi_libretro.so": systemdefs.SystemCDI,
}

// getReplayPID returns the PID of the replay binary. replay.service's MainPID
// is a bash launcher, so the actual binary is found via its child processes.
func (p *Platform) getReplayPID() (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := p.cmdExec().Output(
		ctx, "systemctl", "show", "-p", "MainPID", "--value", "replay.service",
	)
	if err != nil {
		return 0, fmt.Errorf("failed to get replay service PID: %w", err)
	}

	pidStr := strings.TrimSpace(string(out))
	if pidStr == "" || pidStr == "0" {
		return 0, fmt.Errorf("replay service not running (PID=%s)", pidStr)
	}

	mainPID, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, fmt.Errorf("invalid PID %q: %w", pidStr, err)
	}

	if p.isReplayBinary(mainPID) {
		return mainPID, nil
	}

	childPID, err := p.findReplayChild(mainPID)
	if err != nil {
		return 0, err
	}

	return childPID, nil
}

func (p *Platform) isReplayBinary(pid int) bool {
	exe, err := os.Readlink(filepath.Join(p.procDir(), strconv.Itoa(pid), "exe"))
	if err != nil {
		return false
	}
	return exe == replayBinaryPath
}

// findReplayChild reads /proc/{pid}/task/{pid}/children and returns the PID
// of the first child whose executable is the replay binary.
func (p *Platform) findReplayChild(parentPID int) (int, error) {
	pidStr := strconv.Itoa(parentPID)
	childrenPath := filepath.Join(p.procDir(), pidStr, "task", pidStr, "children")
	data, err := os.ReadFile(childrenPath) //nolint:gosec // PID comes from systemctl
	if err != nil {
		return 0, fmt.Errorf("failed to read children of PID %d: %w", parentPID, err)
	}

	for _, field := range strings.Fields(string(data)) {
		var childPID int
		if _, err := fmt.Sscanf(field, "%d", &childPID); err != nil {
			continue
		}
		if p.isReplayBinary(childPID) {
			return childPID, nil
		}
	}

	return 0, fmt.Errorf("replay binary not found among children of PID %d", parentPID)
}

func (p *Platform) getLoadedCore(pid int) string {
	mapsPath := filepath.Join(p.procDir(), strconv.Itoa(pid), "maps")

	f, err := os.Open(mapsPath) //nolint:gosec // PID comes from systemctl
	if err != nil {
		log.Trace().Err(err).Msg("failed to open proc maps")
		return ""
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, coresDir) {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}

		mappedPath := fields[len(fields)-1]
		if strings.HasPrefix(mappedPath, coresDir) && strings.HasSuffix(mappedPath, ".so") {
			core := filepath.Base(mappedPath)
			// Skip the menu core - it's always loaded and isn't a game
			if core == menuCore {
				continue
			}
			return core
		}
	}

	return ""
}

// parseRecFile resolves the absolute ROM path from a .rec file. .rec files
// contain a relative path like /roms/system/game.ext relative to the storage mount.
func parseRecFile(storagePath, filePath string) (string, error) {
	data, err := os.ReadFile(filePath) //nolint:gosec // Path from inotify event
	if err != nil {
		return "", fmt.Errorf("failed to read rec file: %w", err)
	}

	relPath := strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
	if relPath == "" {
		return "", fmt.Errorf("empty rec file: %s", filePath)
	}

	if after, ok := strings.CutPrefix(relPath, "/roms/"); ok {
		return filepath.Join(storagePath, "roms", after), nil
	}

	return filepath.Join(storagePath, relPath), nil
}

func systemIDFromROMPath(romPath, storagePath string) string {
	if romPath == "" || storagePath == "" {
		return ""
	}

	romsDir := filepath.Join(storagePath, "roms")
	rel, err := filepath.Rel(romsDir, romPath)
	if err != nil {
		return ""
	}

	folder := strings.SplitN(rel, string(filepath.Separator), 2)[0]
	if info, ok := SystemMap[folder]; ok {
		return info.SystemID
	}

	return ""
}

// startRecentWatcher watches _recent/ for .rec files written when a game is
// launched from the ReplayOS menu, storing the ROM path for the tracker.
// The returned done channel closes after watcher.Close() drains event channels.
func (p *Platform) startRecentWatcher() (*fsnotify.Watcher, <-chan struct{}, error) {
	recentDir := filepath.Join(p.activeStorage, "roms", recFileDir)
	if err := os.MkdirAll(recentDir, 0o755); err != nil { //nolint:gosec // System directory
		return nil, nil, fmt.Errorf("failed to create recent dir: %w", err)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&(fsnotify.Create|fsnotify.Write) == 0 {
					continue
				}
				if !strings.HasSuffix(event.Name, ".rec") {
					continue
				}

				romPath, err := parseRecFile(p.activeStorage, event.Name)
				if err != nil {
					log.Warn().Err(err).Str("file", event.Name).Msg("failed to parse rec file")
					continue
				}

				log.Info().Str("rom", romPath).Msg("recent watcher: detected game launch from menu")

				p.trackerMu.Lock()
				p.pendingROMPath = romPath
				p.trackerMu.Unlock()
			case watchErr, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Warn().Err(watchErr).Msg("recent watcher error")
			}
		}
	}()

	if err := watcher.Add(recentDir); err != nil {
		_ = watcher.Close()
		<-done
		return nil, nil, fmt.Errorf("failed to watch recent dir (%s): %w", recentDir, err)
	}

	log.Info().Str("dir", recentDir).Msg("recent watcher started")
	return watcher, done, nil
}

// startGameTracker polls /proc/{pid}/maps for loaded cores and watches _recent/
// for menu-launched games. Returns a cleanup function that blocks until all
// goroutines exit.
func (p *Platform) startGameTracker(
	setActiveMedia func(*models.ActiveMedia),
) (func() error, error) {
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // G118: cancel returned in closure
	ticker := p.getClock().NewTicker(trackerInterval)

	recentWatcher, watcherDone, err := p.startRecentWatcher()
	if err != nil {
		log.Warn().Err(err).Msg("failed to start recent watcher, menu-launched games won't have ROM paths")
	}

	trackerDone := make(chan struct{})
	go func() {
		defer close(trackerDone)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.Chan():
				p.checkAndUpdateRunningGame(setActiveMedia)
			case <-ctx.Done():
				return
			}
		}
	}()

	return func() error {
		cancel()
		<-trackerDone
		if recentWatcher != nil {
			if closeErr := recentWatcher.Close(); closeErr != nil {
				log.Warn().Err(closeErr).Msg("error closing recent watcher")
			}
			<-watcherDone
		}
		return nil
	}, nil
}

func (p *Platform) checkAndUpdateRunningGame(
	setActiveMedia func(*models.ActiveMedia),
) {
	pid, err := p.getReplayPID()
	if err != nil {
		log.Trace().Err(err).Msg("tracker: replay service not running")

		p.trackerMu.Lock()
		hadGame := p.lastKnownCore != ""
		p.lastKnownCore = ""
		p.trackerMu.Unlock()

		if hadGame {
			log.Info().Msg("tracker: replay service stopped, clearing active media")
			setActiveMedia(nil)
		}
		return
	}

	core := p.getLoadedCore(pid)

	p.trackerMu.Lock()
	lastCore := p.lastKnownCore
	if core == lastCore {
		p.trackerMu.Unlock()
		return
	}
	p.lastKnownCore = core
	p.trackerMu.Unlock()

	if core == "" {
		if lastCore != "" {
			log.Info().Msg("tracker: game closed (core unloaded)")
			setActiveMedia(nil)
		}
		return
	}

	// Consume pending ROM path (set by launchGame or recent watcher)
	p.trackerMu.Lock()
	romPath := p.pendingROMPath
	p.pendingROMPath = ""
	p.trackerMu.Unlock()

	// Determine system ID: prefer the ROM path folder (more precise than
	// core mapping, since cores like gambatte/mgba handle multiple systems),
	// fall back to the core-to-system mapping.
	systemID := systemIDFromROMPath(romPath, p.activeStorage)
	if systemID == "" {
		var ok bool
		systemID, ok = coreSystemMap[core]
		if !ok {
			log.Warn().Str("core", core).Msg("tracker: unknown core")
		}
	}

	systemMeta, err := assets.GetSystemMetadata(systemID)
	systemName := systemID
	if err == nil {
		systemName = systemMeta.Name
	}

	romName := ""
	if romPath != "" {
		romName = strings.TrimSuffix(filepath.Base(romPath), filepath.Ext(romPath))
	}

	media := models.NewActiveMedia(systemID, systemName, romPath, romName, "")

	if lastCore == "" {
		log.Info().
			Str("core", core).
			Str("system", systemID).
			Str("rom", romPath).
			Msg("tracker: game started")
	} else {
		log.Info().
			Str("core", core).
			Str("system", systemID).
			Str("rom", romPath).
			Msg("tracker: game changed")
	}

	setActiveMedia(media)
}

// healthCheck logs a warning if no core is loaded after a game launch. Does not
// restart the service; the game may still be loading or in an unexpected state.
func (p *Platform) healthCheck(romPath string) {
	pid, err := p.getReplayPID()
	if err != nil {
		log.Warn().Err(err).Str("rom", romPath).Msg("health check: replay service not running after launch")
		return
	}

	core := p.getLoadedCore(pid)
	if core == "" {
		log.Warn().Str("rom", romPath).Msg("health check: no game core loaded after launch")
	} else {
		log.Debug().Str("core", core).Str("rom", romPath).Msg("health check: core loaded successfully")
	}
}
