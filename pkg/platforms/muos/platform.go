//go:build linux && !android

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

package muos

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
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
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/idle"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	// runningInstance marks launchers that hand content to the muOS
	// frontend instead of starting a process of their own.
	runningInstance = "muos"
	installDirName  = "zaparoo"
	cacheTTL        = 30 * time.Second
)

// Platform runs content through the muOS frontend's content loader.
type Platform struct {
	fs             afero.Fs
	clock          clockwork.Clock
	cmd            command.Executor
	signal         func(int, syscall.Signal) error
	sleep          func(time.Duration)
	activeMedia    func() *models.ActiveMedia
	setActiveMedia func(*models.ActiveMedia)
	cancel         context.CancelFunc
	trackerDone    chan struct{}
	db             *coreDB
	dbLoaded       time.Time
	launchersAt    time.Time
	launchers      []platforms.Launcher
	procPath       string
	arch           string
	pending        string
	lastOverlay    string
	shared.InputManager
	pid       int
	cacheMu   syncutil.Mutex
	trackerMu syncutil.Mutex
	launchMu  syncutil.Mutex
	launching bool
}

func (p *Platform) filesystem() afero.Fs {
	if p.fs == nil {
		return afero.NewOsFs()
	}
	return p.fs
}

func (p *Platform) getClock() clockwork.Clock {
	if p.clock == nil {
		return clockwork.NewRealClock()
	}
	return p.clock
}

func (p *Platform) cmdExec() command.Executor {
	if p.cmd == nil {
		return &command.RealExecutor{}
	}
	return p.cmd
}

func (p *Platform) sendSignal(pid int, sig syscall.Signal) error {
	if p.signal == nil {
		if err := syscall.Kill(pid, sig); err != nil {
			return fmt.Errorf("signal %d: %w", pid, err)
		}
		return nil
	}
	return p.signal(pid, sig)
}

func (p *Platform) pause(d time.Duration) {
	if p.sleep == nil {
		time.Sleep(d)
		return
	}
	p.sleep(d)
}

func (p *Platform) procDir() string {
	if p.procPath == "" {
		return "/proc"
	}
	return p.procPath
}

func (p *Platform) selfPID() int {
	if p.pid == 0 {
		return os.Getpid()
	}
	return p.pid
}

func (p *Platform) muArch() string {
	if p.arch == "" {
		return muosArch()
	}
	return p.arch
}

// coreDB returns the core manifests, reloading them when the cached copy
// is old so core changes made in the frontend are picked up.
func (p *Platform) coreDB() *coreDB {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()
	now := p.getClock().Now()
	if p.db == nil || now.Sub(p.dbLoaded) > cacheTTL {
		p.db = loadCoreDB(p.filesystem(), p.muArch())
		p.dbLoaded = now
	}
	return p.db
}

func (*Platform) ID() string {
	return platformids.MuOS
}

func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	return linuxbase.SupportedReaders(cfg, p)
}

func (p *Platform) StartPre(cfg *config.Instance) error {
	if err := p.InitDevices(cfg, false); err != nil {
		log.Warn().Err(err).Msg("failed to initialize input devices, input commands are unavailable")
	}
	return nil
}

func (p *Platform) StartPost(
	ctx context.Context,
	_ *config.Instance,
	_ platforms.LauncherContextManager,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
	_ *database.Database,
	_ *idle.Scheduler,
) error {
	p.activeMedia = activeMedia
	p.setActiveMedia = setActiveMedia
	p.startTracker(ctx)
	return nil
}

func (p *Platform) Stop() error {
	p.stopTracker()
	p.CloseDevices()
	return nil
}

func (*Platform) SetTrackedProcess(_ *os.Process) {}

func (*Platform) ScanHook(_ *tokens.Token) error {
	return nil
}

func (p *Platform) RootDirs(cfg *config.Instance) []string {
	roots := cfg.IndexRoots()
	for _, mount := range mountPoints(p.filesystem()) {
		roots = append(roots, filepath.Join(mount, romsDirName))
	}

	seen := make(map[string]struct{}, len(roots))
	result := make([]string, 0, len(roots))
	for _, r := range roots {
		r = filepath.Clean(r)
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		result = append(result, r)
	}
	return result
}

// installDir is where Zaparoo keeps its data on the primary SD card, next
// to the frontend's own MUOS folder.
func (p *Platform) installDir() string {
	return filepath.Join(romMount(p.filesystem()), "MUOS", installDirName)
}

func (p *Platform) Settings() platforms.Settings {
	dir := p.installDir()
	return platforms.Settings{
		DataDir:   dir,
		ConfigDir: dir,
		TempDir:   filepath.Join(os.TempDir(), config.AppName),
		LogDir:    filepath.Join(dir, config.LogsDir),
	}
}

func (p *Platform) StopActiveLauncher(_ platforms.StopIntent) error {
	fs := p.filesystem()

	p.trackerMu.Lock()
	p.pending = ""
	p.trackerMu.Unlock()
	if err := removeIfExists(fs, contentFile); err != nil {
		log.Warn().Err(err).Msg("failed to remove pending muOS content request")
	}

	if _, running := readOverlay(fs); running {
		if fg := p.foreground(); fg != "" && !isMenuModule(fg) {
			if err := p.closeProcess(fg); err != nil {
				return err
			}
		}
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
	log.Info().Str("path", path).Msg("launch media")

	if launcher == nil {
		found, err := helpers.FindLauncher(cfg, p, path)
		if err != nil {
			return fmt.Errorf("launch media: error finding launcher: %w", err)
		}
		launcher = &found
	}

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
	return helpers.CombineLaunchers(cfg, p, p.platformLaunchers())
}

func (*Platform) ConsoleManager() platforms.ConsoleManager {
	return platforms.NoOpConsoleManager{}
}

func (*Platform) ManagedByPackageManager() bool {
	return false
}

func (*Platform) Scrapers(_ *config.Instance) map[string]platforms.Scraper {
	gamelist := gamelistxml.NewPlatformScraper()
	media := localmedia.NewPlatformScraper()
	thumbnails := libretrothumbs.NewPlatformScraper()
	catalog := gamedatabase.NewPlatformScraper(nil)
	return map[string]platforms.Scraper{
		gamelist.ID: gamelist, media.ID: media, thumbnails.ID: thumbnails, catalog.ID: catalog,
	}
}
