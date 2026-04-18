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
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	widgetmodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/widgets/models"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

//nolint:gochecknoglobals // Package-level configuration
var knownMountPoints = []string{"/media/sd", "/media/usb", "/media/nvme", "/media/nfs"}

//nolint:gochecknoglobals // Package-level configuration
var storageTokenMap = map[string]string{
	"sd":   "/media/sd",
	"usb":  "/media/usb",
	"nvme": "/media/nvme",
	"nfs":  "/media/nfs",
}

const replayCfgPath = "/media/sd/config/replay.cfg"

// installDir is the root directory for all Zaparoo files on ReplayOS.
// /media/sd is the persistent exFAT user-data partition and survives OS updates.
const installDir = "/media/sd/zaparoo"

const (
	autostartDir  = "_autostart"
	autostartFile = "autostart.auto"
)

// Platform implements the platforms.Platform interface for ReplayOS.
type Platform struct {
	cmd            command.Executor
	ctx            context.Context
	clock          clockwork.Clock
	activeMedia    func() *models.ActiveMedia
	setActiveMedia func(*models.ActiveMedia)
	stopTracker    func() error
	cancel         context.CancelFunc
	shared.LinuxInput
	activeStorage    string
	pendingROMPath   string
	lastKnownCore    string
	procPath         string
	storagePaths     []string
	trackerMu        syncutil.RWMutex
	keyboardRealMode bool
}

func (p *Platform) cmdExec() command.Executor {
	if p.cmd == nil {
		return &command.RealExecutor{}
	}
	return p.cmd
}

func (p *Platform) getClock() clockwork.Clock {
	if p.clock == nil {
		return clockwork.NewRealClock()
	}
	return p.clock
}

func (p *Platform) procDir() string {
	if p.procPath == "" {
		return "/proc"
	}
	return p.procPath
}

func (*Platform) ID() string {
	return platformids.ReplayOS
}

func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	return linuxbase.SupportedReaders(cfg, p)
}

func (p *Platform) StartPre(cfg *config.Instance) error {
	p.ctx, p.cancel = context.WithCancel(context.Background())

	active, all, err := detectStorages(replayCfgPath, knownMountPoints, storageTokenMap)
	if err != nil {
		log.Warn().Err(err).Msg("failed to detect ReplayOS storage, some features will be limited")
	}
	p.activeStorage = active
	p.storagePaths = all
	if active != "" {
		log.Info().Str("active", active).Strs("all", all).Msg("detected ReplayOS storage")
	}

	p.keyboardRealMode = readRealMode(replayCfgPath)

	if err := p.InitDevices(cfg, false); err != nil {
		return fmt.Errorf("failed to initialize input devices: %w", err)
	}

	return nil
}

func (p *Platform) StartPost(
	_ *config.Instance,
	_ platforms.LauncherContextManager,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
	_ *database.Database,
) error {
	p.activeMedia = activeMedia
	p.setActiveMedia = setActiveMedia

	if p.activeStorage != "" {
		stopTracker, err := p.startGameTracker(setActiveMedia)
		if err != nil {
			log.Warn().Err(err).Msg("failed to start game tracker")
		} else {
			p.stopTracker = stopTracker
			log.Info().Msg("game tracker started")
		}
	}

	return nil
}

func (p *Platform) Stop() error {
	if p.cancel != nil {
		p.cancel()
	}
	if p.stopTracker != nil {
		if err := p.stopTracker(); err != nil {
			log.Warn().Err(err).Msg("error stopping game tracker")
		}
	}
	p.CloseDevices()
	return nil
}

func (*Platform) SetTrackedProcess(_ *os.Process) {
	// No-op: ReplayOS uses a systemd service, not tracked processes.
}

func (*Platform) ScanHook(_ *tokens.Token) error {
	return nil
}

func (p *Platform) RootDirs(cfg *config.Instance) []string {
	roots := cfg.IndexRoots()

	for _, sp := range p.storagePaths {
		roots = append(roots, filepath.Join(sp, "roms"))
	}

	seen := make(map[string]struct{})
	result := make([]string, 0, len(roots))
	for _, r := range roots {
		normalized := filepath.Clean(r)
		if _, exists := seen[normalized]; !exists {
			seen[normalized] = struct{}{}
			result = append(result, normalized)
		}
	}

	return result
}

func (*Platform) Settings() platforms.Settings {
	return platforms.Settings{
		DataDir:   installDir,
		ConfigDir: installDir,
		TempDir:   filepath.Join(os.TempDir(), config.AppName),
		LogDir:    filepath.Join(installDir, config.LogsDir),
	}
}

func (p *Platform) StopActiveLauncher(_ platforms.StopIntent) error {
	log.Info().Msg("stopping active launcher")

	if p.activeStorage != "" {
		deleteAutostart(p.activeStorage)
	}

	p.restartReplayService()

	p.trackerMu.Lock()
	p.lastKnownCore = ""
	p.trackerMu.Unlock()

	if p.setActiveMedia != nil {
		p.setActiveMedia(nil)
	}

	return nil
}

func (p *Platform) ReturnToMenu() error {
	return p.StopActiveLauncher(platforms.StopForMenu)
}

func (*Platform) LaunchSystem(_ *config.Instance, _ string) error {
	return platforms.ErrNotSupported
}

func (p *Platform) LaunchMedia(
	cfg *config.Instance, path string, launcher *platforms.Launcher, db *database.Database,
	opts *platforms.LaunchOptions,
) error {
	log.Info().Str("path", path).Msg("launch media")

	if launcher == nil {
		foundLauncher, err := helpers.FindLauncher(cfg, p, path)
		if err != nil {
			return fmt.Errorf("launch media: error finding launcher: %w", err)
		}
		launcher = &foundLauncher
	}

	log.Info().Str("launcher", launcher.ID).Str("path", path).Msg("launch media: using launcher")
	err := platforms.DoLaunch(&platforms.LaunchParams{
		Config:         cfg,
		Platform:       p,
		SetActiveMedia: p.setActiveMedia,
		Launcher:       launcher,
		Path:           path,
		DB:             db,
		Options:        opts,
	}, helpers.GetPathName)
	if err != nil {
		return fmt.Errorf("launch media: error launching: %w", err)
	}

	return nil
}

func (*Platform) ForwardCmd(_ *platforms.CmdEnv) (platforms.CmdResult, error) {
	return platforms.CmdResult{}, nil
}

func (*Platform) LookupMapping(_ *tokens.Token) (string, bool) {
	return "", false
}

func (p *Platform) Launchers(cfg *config.Instance) []platforms.Launcher {
	launchers := make([]platforms.Launcher, 0, len(SystemMap)+1)

	for folder, info := range SystemMap {
		launchers = append(launchers, platforms.Launcher{
			ID:         info.GetLauncherID(),
			SystemID:   info.SystemID,
			Extensions: info.Extensions,
			Folders:    []string{folder},
			Launch:     p.launchGame,
		})
	}

	launchers = append(launchers, platforms.Launcher{
		ID:            "Generic",
		Extensions:    []string{".sh"},
		AllowListOnly: true,
		Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			if err := p.cmdExec().Start(context.Background(), path); err != nil {
				return nil, fmt.Errorf("failed to start command: %w", err)
			}
			return nil, nil //nolint:nilnil // Command launches don't return a process handle
		},
	})

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

func (*Platform) ManagedByPackageManager() bool {
	return false
}

// launchGame spawns a background goroutine that deletes the autostart file
// and runs a health check after the game has had time to load.
func (p *Platform) launchGame(
	_ *config.Instance, path string, _ *platforms.LaunchOptions,
) (*os.Process, error) {
	if p.activeStorage == "" {
		return nil, errors.New("no ReplayOS storage detected")
	}

	romStorage, found := storageRootFor(path, p.storagePaths)
	if !found {
		return nil, fmt.Errorf("ROM path is not under any known ReplayOS storage: %s", path)
	}
	if romStorage != p.activeStorage {
		return nil, fmt.Errorf(
			"ROM is on %s but ReplayOS is configured for %s; change storage in REPLAY OPTIONS > SYSTEM > STORAGE",
			filepath.Base(romStorage), filepath.Base(p.activeStorage),
		)
	}

	if err := writeAutostart(p.activeStorage, path); err != nil {
		return nil, fmt.Errorf("failed to write autostart file: %w", err)
	}

	p.trackerMu.Lock()
	p.pendingROMPath = path
	p.trackerMu.Unlock()

	p.restartReplayService()

	activeStorage := p.activeStorage
	ctx := p.ctx
	go func() {
		t1 := time.NewTimer(healthCheckDelay)
		defer t1.Stop()
		select {
		case <-ctx.Done():
			return
		case <-t1.C:
		}
		deleteAutostart(activeStorage)

		t2 := time.NewTimer(healthCheckDelay)
		defer t2.Stop()
		select {
		case <-ctx.Done():
			return
		case <-t2.C:
		}
		p.healthCheck(path)
	}()

	return nil, nil //nolint:nilnil // Autostart launches don't return a process handle
}

func storageRootFor(romPath string, paths []string) (string, bool) {
	for _, sp := range paths {
		if helpers.PathHasPrefix(romPath, filepath.Join(sp, "roms")) {
			return sp, true
		}
	}
	return "", false
}

// detectStorages probes mountPaths for a roms/ directory and resolves the
// active mount from replay.cfg. A missing or unparseable config is non-fatal:
// the first detected mount is used as a fallback.
func detectStorages(
	cfgPath string, mountPaths []string, tokenMap map[string]string,
) (active string, all []string, err error) {
	// Probe all mount points first so we always return the full list.
	for _, path := range mountPaths {
		romsPath := filepath.Join(path, "roms")
		if info, statErr := os.Stat(romsPath); statErr == nil && info.IsDir() {
			all = append(all, path)
		}
	}

	// Parse system_storage from replay.cfg to find the canonical active mount.
	token, parseErr := readStorageToken(cfgPath)
	if parseErr != nil {
		if len(all) > 0 {
			log.Warn().Err(parseErr).Str("fallback", all[0]).
				Msg("could not read replay.cfg system_storage, using first detected mount")
			return all[0], all, nil
		}
		return "", all, fmt.Errorf("no ReplayOS storage found and could not read replay.cfg: %w", parseErr)
	}

	mount, ok := tokenMap[token]
	if !ok {
		if len(all) > 0 {
			log.Warn().Str("token", token).Str("fallback", all[0]).
				Msg("unknown system_storage value, using first detected mount")
			return all[0], all, nil
		}
		return "", all, fmt.Errorf("unknown system_storage value %q and no storage detected", token)
	}

	if len(all) == 0 {
		return "", all, fmt.Errorf("no ReplayOS storage found (system_storage=%s)", token)
	}

	if !slices.Contains(all, mount) {
		log.Warn().Str("token", token).Str("resolved", mount).Str("fallback", all[0]).
			Msg("system_storage mount has no roms directory, using first detected mount")
		return all[0], all, nil
	}

	return mount, all, nil
}

func readStorageToken(cfgPath string) (string, error) {
	data, err := os.ReadFile(cfgPath) //nolint:gosec // Path is a package-level constant
	if err != nil {
		return "", fmt.Errorf("failed to read replay.cfg: %w", err)
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "system_storage" {
			continue
		}
		return strings.Trim(strings.TrimSpace(val), `"`), nil
	}

	return "", fmt.Errorf("system_storage not found in %s", cfgPath)
}

// readRealMode returns true when input_kbd_real_mode is "true" or absent,
// matching the RePlayOS default of Real Mode ON.
func readRealMode(cfgPath string) bool {
	data, err := os.ReadFile(cfgPath) //nolint:gosec // Path is a package-level constant
	if err != nil {
		return true
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "input_kbd_real_mode" {
			continue
		}
		return strings.Trim(strings.TrimSpace(val), `"`) != "false"
	}

	return true
}

// writeAutostart writes the autostart file with a path relative to the storage
// mount (e.g. /roms/nintendo_snes/game.sfc), as ReplayOS requires.
func writeAutostart(storagePath, romPath string) error {
	dir := filepath.Join(storagePath, "roms", autostartDir)
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // System directory
		return fmt.Errorf("failed to create autostart directory: %w", err)
	}

	romsDir := filepath.Join(storagePath, "roms")
	rel, err := filepath.Rel(romsDir, romPath)
	if err != nil {
		return fmt.Errorf("failed to make ROM path relative to storage: %w", err)
	}
	autostartPath := "/roms/" + rel

	filePath := filepath.Join(dir, autostartFile)
	if err := os.WriteFile(filePath, []byte(autostartPath+"\n"), 0o644); err != nil { //nolint:gosec // System file
		return fmt.Errorf("failed to write autostart file: %w", err)
	}

	log.Debug().Str("file", filePath).Str("rom", autostartPath).Msg("wrote autostart file")
	return nil
}

func deleteAutostart(storagePath string) {
	filePath := filepath.Join(storagePath, "roms", autostartDir, autostartFile)
	err := os.Remove(filePath)
	switch {
	case err == nil:
		log.Debug().Str("file", filePath).Msg("deleted autostart file")
	case errors.Is(err, os.ErrNotExist):
		// Nothing to delete; not an error.
	default:
		log.Warn().Err(err).Str("file", filePath).Msg("failed to delete autostart file")
	}
}

func (p *Platform) restartReplayService() {
	log.Debug().Msg("restarting replay.service")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := p.cmdExec().Run(ctx, "systemctl", "restart", "replay.service"); err != nil {
		log.Error().Err(err).Msg("failed to restart replay.service")
	}
}
