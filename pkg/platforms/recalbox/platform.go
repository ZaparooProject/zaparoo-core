//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package recalbox implements Recalbox. Games are launched through
// EmulationStation's UDP command port so ES stays in charge of emulator
// configuration, and followed through the state file ES rewrites on every
// event.
package recalbox

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/launchers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/idle"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	// InstallDir holds the binary, config, database and logs on the share
	// partition, which Recalbox upgrades keep.
	InstallDir = "/recalbox/share/system/zaparoo"
	// ServiceBinaryDir is on tmpfs, which can execute files.
	ServiceBinaryDir = "/tmp/zaparoo-bin"
	// romsRoot is the internal ROM folder.
	romsRoot = "/recalbox/share/roms"
	// shareInitRomsRoot holds the ROMs Recalbox ships, which ES lists too.
	shareInitRomsRoot = "/recalbox/share_init/roms"
	// externalsRoot is where Recalbox mounts USB drives.
	externalsRoot = "/recalbox/share/externals"
)

// Platform implements platforms.Platform for Recalbox.
type Platform struct {
	lastStart      time.Time
	serviceCtx     context.Context
	fs             afero.Fs
	clock          clockwork.Clock
	changed        chan struct{}
	signal         func(pid int, sig syscall.Signal) error
	setActiveMedia func(*models.ActiveMedia)
	subscribe      func(ctx context.Context, onEvent func())
	cancel         context.CancelFunc
	wake           chan struct{}
	*linuxbase.Base
	activeMedia func() *models.ActiveMedia
	observed    esState
	procDir     string
	lastKey     string
	gamePath    string
	commandAddr string
	generation  uint64
	stateMu     syncutil.Mutex
	refreshMu   syncutil.Mutex
	startMu     syncutil.Mutex
	seenState   bool
}

// NewPlatform creates a Recalbox platform.
func NewPlatform() *Platform {
	return &Platform{
		Base:        linuxbase.NewBase(platformids.Recalbox),
		fs:          afero.NewOsFs(),
		clock:       clockwork.NewRealClock(),
		signal:      killProcess,
		subscribe:   subscribeESEvents,
		wake:        make(chan struct{}, 1),
		changed:     make(chan struct{}),
		commandAddr: esCommandAddr,
		procDir:     procRoot,
	}
}

// SupportedReaders returns the standard Linux reader set.
func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	return linuxbase.SupportedReaders(cfg, p)
}

// Settings keeps everything in InstallDir.
func (*Platform) Settings() platforms.Settings {
	return platforms.Settings{
		DataDir:   InstallDir,
		ConfigDir: InstallDir,
		LogDir:    filepath.Join(InstallDir, config.LogsDir),
		TempDir:   filepath.Join(os.TempDir(), config.AppName),
	}
}

// ServiceBinaryDir is where the service runs from. The share is FAT mounted
// without exec bits, so the binary is copied to tmpfs first.
func (*Platform) ServiceBinaryDir() string {
	return ServiceBinaryDir
}

// romRoots lists the folders EmulationStation reads systems from, in its
// order: the stock ROMs, the share, then each external drive's first ROM
// layout (recalbox/roms, roms, or the drive itself).
func (p *Platform) romRoots() []string {
	var roots []string
	for _, dir := range []string{shareInitRomsRoot, romsRoot} {
		if p.isDir(dir) {
			roots = append(roots, dir)
		}
	}
	drives, err := afero.ReadDir(p.fs, externalsRoot)
	if err != nil {
		return roots
	}
	for _, drive := range drives {
		if !drive.IsDir() {
			continue
		}
		mount := filepath.Join(externalsRoot, drive.Name())
		root := mount
		for _, inner := range []string{filepath.Join("recalbox", "roms"), "roms"} {
			if p.isDir(filepath.Join(mount, inner)) {
				root = filepath.Join(mount, inner)
				break
			}
		}
		roots = append(roots, root)
	}
	return roots
}

func (p *Platform) isDir(path string) bool {
	info, err := p.fs.Stat(path)
	return err == nil && info.IsDir()
}

// RootDirs returns configured index roots plus the folders ES reads ROMs from.
func (p *Platform) RootDirs(cfg *config.Instance) []string {
	roots := slices.Clone(cfg.IndexRoots())
	roots = append(roots, p.romRoots()...)
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

// StartPost starts following EmulationStation events.
func (p *Platform) StartPost(
	ctx context.Context,
	cfg *config.Instance,
	launcherManager platforms.LauncherContextManager,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
	db *database.Database,
	scheduler *idle.Scheduler,
) error {
	//nolint:wrapcheck // Pass-through to base implementation
	if err := p.Base.StartPost(ctx, cfg, launcherManager, activeMedia, setActiveMedia, db, scheduler); err != nil {
		return err
	}
	p.activeMedia = activeMedia
	p.setActiveMedia = setActiveMedia

	trackCtx, cancel := context.WithCancel(ctx)
	p.stateMu.Lock()
	p.serviceCtx = trackCtx
	p.cancel = cancel
	p.stateMu.Unlock()
	go p.track(trackCtx)
	return nil
}

// Stop ends event tracking. A running game is left to ES.
func (p *Platform) Stop() error {
	p.stateMu.Lock()
	cancel := p.cancel
	p.cancel = nil
	p.stateMu.Unlock()
	if cancel != nil {
		cancel()
	}
	//nolint:wrapcheck // Pass-through to base implementation
	return p.Base.Stop()
}

func (p *Platform) ctx() context.Context {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	if p.serviceCtx != nil {
		return p.serviceCtx
	}
	return context.Background()
}

// StopActiveLauncher stops the running game, whether Core or ES started it.
// Active media stays set when the game could not be stopped.
func (p *Platform) StopActiveLauncher(intent platforms.StopIntent) error {
	if err := p.stopGame(); err != nil {
		return err
	}
	p.forgetGame()
	//nolint:wrapcheck // Pass-through to base implementation
	return p.Base.StopActiveLauncher(intent)
}

// ReturnToMenu stops the running game, which returns to ES.
func (p *Platform) ReturnToMenu() error {
	return p.StopActiveLauncher(platforms.StopForMenu)
}

// LaunchMedia launches media with the platform's launchers.
func (p *Platform) LaunchMedia(
	cfg *config.Instance,
	path string,
	launcher *platforms.Launcher,
	db *database.Database,
	opts *platforms.LaunchOptions,
) error {
	//nolint:wrapcheck // Pass-through to base implementation
	return p.Base.LaunchMedia(cfg, path, launcher, db, opts, p)
}

// Launchers returns one launcher per Recalbox system Zaparoo knows, plus
// custom and generic launchers.
func (p *Platform) Launchers(cfg *config.Instance) []platforms.Launcher {
	builtIn := p.esLaunchers()
	builtIn = append(builtIn, launchers.NewGenericLauncher())
	return helpers.CombineLaunchers(cfg, p, builtIn)
}

func (p *Platform) esLaunchers() []platforms.Launcher {
	systems, err := loadSystems(p.fs)
	if err != nil {
		log.Warn().Err(err).Msg("cannot read Recalbox system list, no system launchers")
		return nil
	}
	sort.SliceStable(systems, func(i, j int) bool { return systems[i].Name < systems[j].Name })

	result := make([]platforms.Launcher, 0, len(systems))
	byID := map[string]int{}
	for i := range systems {
		sys := &systems[i]
		info, ok := lookupSystem(sys.Name)
		if !ok {
			log.Debug().Str("system", sys.Name).Msg("Recalbox system has no Zaparoo mapping")
			continue
		}
		folder := sys.folder()
		if folder == "" {
			log.Debug().Str("system", sys.Name).Msg("Recalbox system has no usable ROM folder")
			continue
		}
		id := info.GetLauncherID()
		exts := sys.extensions(info)
		if at, seen := byID[id]; seen {
			l := &result[at]
			if !slices.Contains(l.Folders, folder) {
				l.Folders = append(l.Folders, folder)
			}
			for _, ext := range exts {
				if !slices.Contains(l.Extensions, ext) {
					l.Extensions = append(l.Extensions, ext)
				}
			}
			continue
		}
		byID[id] = len(result)
		result = append(result, platforms.Launcher{
			ID:         id,
			SystemID:   info.SystemID,
			Folders:    []string{folder},
			Extensions: slices.Clone(exts),
			Lifecycle:  platforms.LifecycleExternal,
			Launch:     p.launchGame,
		})
	}
	return result
}
