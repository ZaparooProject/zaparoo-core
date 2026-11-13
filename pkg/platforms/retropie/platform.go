//go:build linux && !android

/*
Zaparoo Core
Copyright (C) 2024, 2025 Callan Barrett

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package retropie

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/gamedatabase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/gamelistxml"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/libretrothumbs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/localmedia"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/idle"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/adrg/xdg"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	// defaultConfigsDir is RetroPie's configuration root, owned by the
	// RetroPie user.
	defaultConfigsDir = "/opt/retropie/configs"
	runcommandPath    = "/opt/retropie/supplementary/runcommand/runcommand.sh"
	stateSubdir       = "retropie"

	// gameReadyTimeout covers stopping EmulationStation, runcommand's
	// launch screen and its key-press window before the game reports.
	gameReadyTimeout = 45 * time.Second
	readyInterval    = 250 * time.Millisecond
)

type Platform struct {
	fs             afero.Fs
	procs          procOps
	clock          clockwork.Clock
	sendQuit       func(ctx context.Context, addr string) error
	activeMedia    func() *models.ActiveMedia
	setActiveMedia func(*models.ActiveMedia)
	stopWatcher    func() error
	trackedProcess *os.Process
	// completedProcess is the last tracked process that exited while still
	// tracked, so only its own waiter may clear media.
	completedProcess *os.Process
	processDone      chan struct{}
	configsDir       string
	homeDir          string
	stateDir         string
	romsDir          string
	run              runEvent
	processMu        syncutil.RWMutex
	mu               syncutil.Mutex
	waitClaimed      bool
	// awaitingStart is set while a launch from Core waits for runcommand to
	// report the game.
	awaitingStart bool
}

// defaults fills in the real host boundaries for fields a test did not set.
func (p *Platform) defaults() {
	if p.fs == nil {
		p.fs = afero.NewOsFs()
	}
	if p.procs == nil {
		p.procs = procFS{}
	}
	if p.clock == nil {
		p.clock = clockwork.NewRealClock()
	}
	if p.sendQuit == nil {
		p.sendQuit = sendRetroArchQuit
	}
	if p.configsDir == "" {
		p.configsDir = defaultConfigsDir
	}
	if p.homeDir == "" {
		p.homeDir = xdg.Home
	}
	if p.stateDir == "" {
		p.stateDir = filepath.Join(helpers.DataDir(p), stateSubdir)
	}
	if p.romsDir == "" {
		p.romsDir = filepath.Join(p.homeDir, "RetroPie", "roms")
	}
}

func (*Platform) ID() string {
	return platformids.RetroPie
}

func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	return linuxbase.SupportedReaders(cfg, p)
}

func (p *Platform) StartPre(_ *config.Instance) error {
	p.defaults()
	if err := p.fs.MkdirAll(p.stateDir, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", p.stateDir, err)
	}
	return nil
}

func (p *Platform) StartPost(
	_ context.Context,
	_ *config.Instance,
	_ platforms.LauncherContextManager,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
	_ *database.Database,
	_ *idle.Scheduler,
) error {
	p.defaults()
	p.activeMedia = activeMedia
	p.setActiveMedia = setActiveMedia

	stop, err := p.startStateWatcher()
	if err != nil {
		log.Warn().Err(err).Msg("retropie: game tracking unavailable")
		return nil
	}
	p.stopWatcher = stop
	if _, ok := readLoopPID(p.fs, p.procs, p.stateDir); !ok {
		log.Warn().Msg("retropie: the Zaparoo autostart loop is not running; " +
			"run \"zaparoo -install\" and reboot so games can be launched")
	}
	return nil
}

func (p *Platform) Stop() error {
	if p.stopWatcher != nil {
		if err := p.stopWatcher(); err != nil {
			log.Warn().Err(err).Msg("retropie: error stopping state watcher")
		}
		p.stopWatcher = nil
	}
	return nil
}

func (p *Platform) SetTrackedProcess(proc *os.Process) {
	p.processMu.Lock()
	defer p.processMu.Unlock()
	p.trackedProcess = proc
	p.completedProcess = nil
	p.waitClaimed = false
	if proc == nil {
		p.processDone = nil
	} else {
		p.processDone = make(chan struct{})
	}
}

// WaitTrackedProcess waits for and reaps proc. It returns an error when proc is
// no longer the tracked process, for example after Core stopped it.
func (p *Platform) WaitTrackedProcess(proc *os.Process) error {
	p.processMu.Lock()
	if proc == nil || p.trackedProcess != proc {
		p.processMu.Unlock()
		return errors.New("process is no longer tracked")
	}
	done := p.processDone
	if p.waitClaimed {
		p.processMu.Unlock()
		<-done
		return nil
	}
	p.waitClaimed = true
	p.processMu.Unlock()

	_, err := proc.Wait()

	p.processMu.Lock()
	if p.trackedProcess == proc {
		p.trackedProcess = nil
		p.completedProcess = proc
		p.processDone = nil
		p.waitClaimed = false
	}
	p.processMu.Unlock()
	close(done)

	if err != nil {
		return fmt.Errorf("wait for tracked process: %w", err)
	}
	return nil
}

// ClearTrackedProcessMedia clears active media only if proc exited without a
// newer process being tracked, so a late exit never clears a newer launch.
func (p *Platform) ClearTrackedProcessMedia(proc *os.Process) bool {
	p.processMu.Lock()
	if p.completedProcess != proc || p.trackedProcess != nil {
		p.processMu.Unlock()
		return false
	}
	p.completedProcess = nil
	p.processMu.Unlock()

	if p.setActiveMedia != nil {
		p.setActiveMedia(nil)
	}
	return true
}

func (*Platform) ScanHook(_ *tokens.Token) error {
	return nil
}

func (p *Platform) RootDirs(cfg *config.Instance) []string {
	p.defaults()
	roots := append(cfg.IndexRoots(), p.romsDir)
	seen := make(map[string]struct{}, len(roots))
	result := make([]string, 0, len(roots))
	for _, root := range roots {
		root = filepath.Clean(root)
		if _, ok := seen[root]; ok {
			continue
		}
		seen[root] = struct{}{}
		result = append(result, root)
	}
	return result
}

func (*Platform) Settings() platforms.Settings {
	return linuxbase.Settings()
}

func (p *Platform) StopActiveLauncher(intent platforms.StopIntent) error {
	p.defaults()

	p.processMu.Lock()
	if p.trackedProcess != nil {
		if err := p.trackedProcess.Kill(); err != nil {
			log.Warn().Err(err).Msg("failed to kill tracked process")
		} else {
			log.Debug().Msg("killed tracked process")
		}
		p.trackedProcess = nil
		p.processDone = nil
		p.waitClaimed = false
	}
	p.completedProcess = nil
	p.processMu.Unlock()

	if err := p.stopRuncommand(intent); err != nil {
		return err
	}

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
	p.defaults()
	log.Info().Str("path", path).Msg("launch media")

	// Stopping a game for a new launch asks the autostart loop to wait for
	// the request instead of starting EmulationStation. The wait ends here
	// whichever launcher ran, so a launch that is not a RetroPie game never
	// leaves the loop waiting.
	defer func() {
		if err := removeIfExists(p.fs, filepath.Join(p.stateDir, holdFile)); err != nil {
			log.Warn().Err(err).Msg("retropie: failed to clear launch hold")
		}
	}()

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

func (*Platform) KeyboardPress(_ string) error {
	return nil
}

func (*Platform) GamepadPress(_ string) error {
	return nil
}

func (*Platform) Screenshot() (*platforms.ScreenshotResult, error) {
	return nil, platforms.ErrNotSupported
}

func (*Platform) ForwardCmd(_ *platforms.CmdEnv) (platforms.CmdResult, error) {
	return platforms.CmdResult{}, platforms.ErrNotSupported
}

func (*Platform) LookupMapping(_ *tokens.Token) (string, bool) {
	return "", false
}

func (p *Platform) Launchers(cfg *config.Instance) []platforms.Launcher {
	p.defaults()
	folders := make([]string, 0, len(systemMap))
	for folder := range systemMap {
		folders = append(folders, folder)
	}
	sort.Strings(folders)

	launchers := make([]platforms.Launcher, 0, len(folders)+1)
	for _, folder := range folders {
		info := systemMap[folder]
		launcher := platforms.Launcher{
			ID:         info.launcherID,
			SystemID:   info.systemID,
			Extensions: append([]string(nil), info.extensions...),
			Folders:    []string{folder},
			Launch:     p.launcherFunc(folder, info.kind),
		}
		// Ports are scripts that may not go through runcommand, so nothing
		// reports when they start.
		if info.kind == requestSystem {
			launcher.WaitForReady = p.waitForGame
			launcher.ReadyTimeout = func(*models.ActiveMedia) time.Duration { return gameReadyTimeout }
			launcher.GameRunning = p.gameRunning
		}
		launchers = append(launchers, launcher)
	}

	launchers = append(launchers, platforms.Launcher{
		ID:            "Generic",
		Extensions:    []string{".sh"},
		AllowListOnly: true,
		Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
			err := command.StartAndReap(exec.CommandContext(context.Background(), path))
			if err != nil {
				return nil, fmt.Errorf("failed to start command: %w", err)
			}
			return nil, nil //nolint:nilnil // Command launches don't return a process handle
		},
	})

	return helpers.CombineLaunchers(cfg, p, launchers)
}

func (*Platform) ConsoleManager() platforms.ConsoleManager {
	return platforms.NoOpConsoleManager{}
}

func (*Platform) ManagedByPackageManager() bool {
	return false
}

func (*Platform) Scrapers(_ *config.Instance) map[string]platforms.Scraper {
	// ES falls back to these when a ROM folder has no gamelist.xml, and the
	// ES scraper and Skyscraper write there by default.
	gamelist := gamelistxml.NewPlatformScraper(gamelistxml.WithGamelistDirs(
		filepath.Join(xdg.Home, ".emulationstation", "gamelists"),
		"/etc/emulationstation/gamelists",
	))
	media := localmedia.NewPlatformScraper()
	thumbnails := libretrothumbs.NewPlatformScraper()
	catalog := gamedatabase.NewPlatformScraper(nil)
	return map[string]platforms.Scraper{
		gamelist.ID: gamelist, media.ID: media, thumbnails.ID: thumbnails, catalog.ID: catalog,
	}
}
