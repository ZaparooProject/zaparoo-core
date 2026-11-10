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

// Package lakka implements the Lakka platform, where RetroArch is both the
// menu and the emulator.
package lakka

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/retroarch"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/idle"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	storageDir = "/storage"
	// retroArchExecutable is the binary retroarch.service runs.
	retroArchExecutable = "/usr/bin/retroarch"
	// retroArchCoresDir is Lakka's libretro_directory, assembled at boot from
	// the image's cores and any the player added.
	retroArchCoresDir   = "/tmp/cores"
	retroArchNetworkCmd = "127.0.0.1:55355"

	launchTimeout = 45 * time.Second
	stopTimeout   = 15 * time.Second
)

// Platform implements Lakka support.
type Platform struct {
	*linuxbase.Base
	fs            afero.Fs
	frontend      *frontend
	playlists     *playlistStore
	stopTracker   context.CancelFunc
	serviceCtx    context.Context
	coresDir      string
	retroArchExec string
	mu            syncutil.Mutex
}

// NewPlatform creates a Lakka platform.
func NewPlatform() *Platform {
	return newTestablePlatform(afero.NewOsFs(), &command.RealExecutor{}, clockwork.NewRealClock(), retroArchNetworkCmd)
}

func newTestablePlatform(fs afero.Fs, exec command.Executor, clock clockwork.Clock, addr string) *Platform {
	scanned := make(map[string]bool)
	for _, launch := range retroarch.CoreLaunches(coreProfile(runtime.GOARCH)) {
		if launch.Scan {
			scanned[launch.SystemID] = true
		}
	}
	return &Platform{
		Base:          linuxbase.NewBase(platformids.Lakka),
		fs:            fs,
		frontend:      newFrontend(exec, clock, addr),
		playlists:     newPlaylistStore(fs, playlistsDir, func(id string) bool { return scanned[id] }),
		coresDir:      retroArchCoresDir,
		retroArchExec: retroArchExecutable,
	}
}

// Settings returns paths under Lakka's writable /storage partition.
func (*Platform) Settings() platforms.Settings {
	dataDir := filepath.Join(storageDir, ".local", "share", config.AppName)
	return platforms.Settings{
		DataDir:   dataDir,
		ConfigDir: filepath.Join(storageDir, ".config", config.AppName),
		LogDir:    filepath.Join(dataDir, config.LogsDir),
		TempDir:   filepath.Join(os.TempDir(), config.AppName),
	}
}

// RootDirs returns configured roots and Lakka's ROM share.
func (*Platform) RootDirs(cfg *config.Instance) []string {
	roots := cfg.IndexRoots()
	romsDir := filepath.Join(storageDir, "roms")
	for _, root := range roots {
		if filepath.Clean(root) == romsDir {
			return roots
		}
	}
	return append(roots, romsDir)
}

// SupportedReaders returns the standard Linux reader set.
func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	return linuxbase.SupportedReaders(cfg, p)
}

// StartPost makes sure RetroArch is up and starts watching what it plays.
func (p *Platform) StartPost(
	ctx context.Context,
	cfg *config.Instance,
	launcherManager platforms.LauncherContextManager,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
	db *database.Database,
	scheduler *idle.Scheduler,
) error {
	if err := p.Base.StartPost(ctx, cfg, launcherManager, activeMedia, setActiveMedia, db, scheduler); err != nil {
		return err //nolint:wrapcheck // Base error already has context.
	}

	if retroArchCfg, err := readRetroArchConfig(p.fs); err != nil {
		log.Warn().Err(err).Msg("could not read retroarch configuration")
	} else if !NetworkCommandsEnabled(retroArchCfg) {
		log.Warn().Str("path", RetroArchConfigPath).
			Msg("retroarch network commands are off; run Zaparoo with -install to turn them on")
	}

	p.frontend.ensureFrontend(ctx)

	trackCtx, cancel := context.WithCancel(ctx)
	p.mu.Lock()
	p.serviceCtx = ctx
	p.stopTracker = cancel
	p.mu.Unlock()
	t := &tracker{frontend: p.frontend, activeMedia: activeMedia, setActiveMedia: setActiveMedia}
	go t.run(trackCtx)
	return nil
}

// Stop ends the tracker. A running game is left alone.
func (p *Platform) Stop() error {
	p.mu.Lock()
	cancel := p.stopTracker
	p.stopTracker = nil
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

// StopActiveLauncher closes the running game and leaves the RetroArch menu.
func (p *Platform) StopActiveLauncher(intent platforms.StopIntent) error {
	ctx, cancel := context.WithTimeout(p.context(), stopTimeout)
	defer cancel()
	if err := p.frontend.stop(ctx); err != nil {
		if errors.Is(err, platforms.ErrStopFailed) {
			return err
		}
		log.Warn().Err(err).Msg("error stopping retroarch content")
	}
	return p.Base.StopActiveLauncher(intent) //nolint:wrapcheck // Base error already has context.
}

// ReturnToMenu closes the running game.
func (p *Platform) ReturnToMenu() error {
	return p.StopActiveLauncher(platforms.StopForMenu)
}

// LaunchMedia launches media through the shared Linux implementation.
func (p *Platform) LaunchMedia(
	cfg *config.Instance,
	path string,
	launcher *platforms.Launcher,
	db *database.Database,
	opts *platforms.LaunchOptions,
) error {
	return p.Base.LaunchMedia(cfg, path, launcher, db, opts, p) //nolint:wrapcheck // Base error already has context.
}

// Launchers returns custom launchers followed by RetroArch core launchers and
// the launchers for RetroArch playlist entries.
func (p *Platform) Launchers(cfg *config.Instance) []platforms.Launcher {
	opts := p.retroArchOptions()
	launches := p.resolveCores(retroarch.CoreLaunches(coreProfile(runtime.GOARCH)))
	builtIn := retroarch.NewLaunchers(opts, launches)
	controls := p.controls()
	playlistSystems := make(map[string]bool)
	for i := range builtIn {
		// The first scanning launcher of a system indexes its playlist entries
		// and launches them with the core the playlist recorded.
		playlistSystem := ""
		if launches[i].Scan && !playlistSystems[launches[i].SystemID] {
			playlistSystem = launches[i].SystemID
			playlistSystems[playlistSystem] = true
			builtIn[i].Scanner = p.scanPlaylists
		}
		p.adaptLauncher(&builtIn[i], controls, playlistSystem)
	}
	builtIn = append(builtIn, p.playlistLaunchers(playlistSystems)...)
	custom := helpers.ParseCustomLaunchers(p, cfg.CustomLaunchers())
	return helpers.CombineParsedLaunchers(custom, builtIn)
}

// lakkaAlternateCores lists the cores Lakka ships for systems whose shared
// default core it does not build, in order of preference.
//
//nolint:gochecknoglobals // Static core data.
var lakkaAlternateCores = map[string][]string{
	systemdefs.System3DS: {"citra_libretro.so", "panda3ds_libretro.so"},
	systemdefs.SystemPS2: {"pcsx2_libretro.so", "play_libretro.so"},
}

// resolveCores points each launch at the core file Lakka actually has. Some
// Lakka cores are not lower case, such as FreeIntv_libretro.so, and some
// systems use a different core than the shared default. Launches whose core
// cannot be found are left alone so launching reports the missing core.
func (p *Platform) resolveCores(launches []retroarch.CoreLaunch) []retroarch.CoreLaunch {
	entries, err := afero.ReadDir(p.fs, p.coresDir)
	if err != nil {
		log.Debug().Err(err).Str("dir", p.coresDir).Msg("cannot list retroarch cores")
		return launches
	}
	installed := make(map[string]string, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			installed[strings.ToLower(e.Name())] = e.Name()
		}
	}
	find := func(core string) (string, bool) {
		name, ok := installed[strings.ToLower(core)]
		return name, ok
	}
	for i := range launches {
		launch := &launches[i]
		if name, ok := find(launch.Core); ok {
			launch.Core = name
			continue
		}
		for _, alt := range lakkaAlternateCores[launch.SystemID] {
			if name, ok := find(alt); ok {
				launch.Core = name
				break
			}
		}
	}
	return launches
}

// coreProfile picks the core set for the device. Lakka builds the same cores
// as the desktop for x86 PCs; on ARM boards the lighter appliance picks run
// better.
func coreProfile(arch string) retroarch.Profile {
	if arch == "amd64" {
		return retroarch.ProfileDesktop
	}
	return retroarch.ProfileApplianceARM
}

func (p *Platform) retroArchOptions() retroarch.Options {
	return retroarch.Options{
		FS:             p.fs,
		Exec:           []string{p.retroArchExec},
		CoresDir:       p.coresDir,
		NetworkCmdAddr: p.frontend.addr,
	}
}

// adaptLauncher sends the launch to the running RetroArch rather than starting
// a child of Core, and stops through the platform so the menu comes back. A
// launcher given a playlist system runs that system's playlist entries with
// the core their playlist recorded.
func (p *Platform) adaptLauncher(
	launcher *platforms.Launcher,
	controls map[string]platforms.Control,
	playlistSystem string,
) {
	build := launcher.BuildLaunchCommand
	if playlistSystem != "" {
		build = p.playlistCommand(build, playlistSystem)
		launcher.BuildLaunchCommand = build
	}
	launcher.Lifecycle = platforms.LifecycleFireAndForget
	launcher.Kill = nil
	launcher.Controls = controls
	launcher.Launch = func(cfg *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
		cmd, err := build(cfg, path, opts)
		if err != nil {
			return nil, err
		}
		argv := append([]string{cmd.Executable}, cmd.Args...)
		ctx, cancel := context.WithTimeout(p.context(), launchTimeout)
		defer cancel()
		return nil, p.frontend.launch(ctx, argv, path)
	}
	launcher.GameRunning = func(ctx context.Context, _ *config.Instance, _ *models.ActiveMedia) (bool, error) {
		return p.frontend.gameRunning(ctx)
	}
}

// controls are RetroArch's network command controls, except stop, which must
// close content rather than quit: quitting RetroArch on Lakka is quitting the
// whole frontend.
func (p *Platform) controls() map[string]platforms.Control {
	controls := retroarch.Controls(p.frontend.addr)
	controls[platforms.ControlStop] = platforms.Control{
		Func: func(context.Context, *config.Instance, platforms.ControlParams) error {
			return p.StopActiveLauncher(platforms.StopForMenu)
		},
	}
	return controls
}

func (p *Platform) context() context.Context {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.serviceCtx != nil {
		return p.serviceCtx
	}
	return context.Background()
}
