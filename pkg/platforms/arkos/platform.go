//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

// Package arkos implements ArkOS, dArkOS and arkos4clone handhelds, where
// EmulationStation-fcamod is the frontend and has no API: Core stops ES, runs
// the game with the command ES would use, and starts ES again afterwards.
package arkos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/launchers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase/procscanner"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/idle"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v4/process"
	"github.com/spf13/afero"
)

// InstallDir holds the binary, config, database and logs. It sits beside the
// other per-app configs in the frontend user's home, which ArkOS updates leave
// alone.
const InstallDir = "/home/ark/.config/zaparoo"

const (
	esGameTermTimeout = 3 * time.Second
	esGamePollEvery   = 100 * time.Millisecond
)

// romRoots are the ArkOS ROM partitions: /roms on the OS card, /roms2 on a
// second card when one is used.
//
//nolint:gochecknoglobals // Package-level configuration
var romRoots = []string{"/roms", "/roms2"}

// Platform implements platforms.Platform for ArkOS.
type Platform struct {
	*linuxbase.Base
	fs             afero.Fs
	cmd            command.Executor
	fe             *frontend
	esMatcher      *esGameMatcher
	procScanner    *procscanner.Scanner
	clock          clockwork.Clock
	startGame      func(*launchSpec) (*os.Process, error)
	runScript      func(context.Context, *scriptSpec) error
	lookupAccount  func() (*account, error)
	pendingEnd     *pendingGameEnd
	killESGame     func(pid int)
	activeMedia    func() *models.ActiveMedia
	setActiveMedia func(*models.ActiveMedia)
	esGamePath     string
	esGamePID      int
	scriptTimeout  time.Duration
	esGameMu       syncutil.Mutex
	eventMu        syncutil.Mutex
}

// pendingGameEnd is a game Core started whose game-start event has run and
// whose game-end event has not.
type pendingGameEnd struct {
	acct *account
	pid  int
}

// NewPlatform creates an ArkOS platform.
func NewPlatform() *Platform {
	fs := afero.NewOsFs()
	cmd := &command.RealExecutor{}
	p := &Platform{
		Base:          linuxbase.NewBase(platformids.ArkOS),
		fs:            fs,
		cmd:           cmd,
		fe:            newFrontend(cmd, fs),
		clock:         clockwork.NewRealClock(),
		startGame:     startGameProcess,
		runScript:     runScriptProcess,
		scriptTimeout: defaultScriptTimeout,
		lookupAccount: lookupFrontendAccount,
		esMatcher:     &esGameMatcher{fs: fs, procDir: "/proc", roots: romRoots},
	}
	p.killESGame = p.terminateTree
	return p
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

// RootDirs returns configured index roots plus the ROM partitions present.
func (p *Platform) RootDirs(cfg *config.Instance) []string {
	roots := slices.Clone(cfg.IndexRoots())
	for _, root := range romRoots {
		if info, err := p.fs.Stat(root); err == nil && info.IsDir() {
			roots = append(roots, root)
		}
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

// StartPost restores ES if a previous run left it stopped, then watches for
// games ES launches itself.
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

	p.fe.recoverStale()

	p.procScanner = procscanner.New()
	p.procScanner.Watch(p.esMatcher, procscanner.Callbacks{
		OnStart: p.esGameStarted,
		OnStop:  p.esGameStopped,
	})
	if err := p.procScanner.Start(); err != nil {
		log.Warn().Err(err).Msg("process scanner failed to start")
		p.procScanner = nil
	}
	return nil
}

// Stop ends a game Core started and gives the display back to ES.
func (p *Platform) Stop() error {
	if p.procScanner != nil {
		p.procScanner.Stop()
		p.procScanner = nil
	}
	if p.fe.runningGame() != 0 {
		if err := p.Base.StopActiveLauncher(platforms.StopForMenu); err != nil {
			log.Warn().Err(err).Msg("failed to stop game on shutdown")
		}
	}
	p.endGame(0)
	p.fe.restore()
	//nolint:wrapcheck // Pass-through to base implementation
	return p.Base.Stop()
}

// StopActiveLauncher stops the running game, whether Core or ES started it.
// ES resumes by itself when its own game ends; for Core's games ES is started
// once the game has exited, unless a replacement launch is under way.
func (p *Platform) StopActiveLauncher(intent platforms.StopIntent) error {
	if pid := p.takeESGame(); pid != 0 {
		log.Info().Int("pid", pid).Msg("stopping game launched by EmulationStation")
		p.killESGame(pid)
	}
	//nolint:wrapcheck // Pass-through to base implementation
	return p.Base.StopActiveLauncher(intent)
}

// ReturnToMenu stops the running game, which returns to ES.
func (p *Platform) ReturnToMenu() error {
	return p.StopActiveLauncher(platforms.StopForMenu)
}

// WaitTrackedProcess reaps a game Core started and then lets ES come back.
func (p *Platform) WaitTrackedProcess(proc *os.Process) error {
	err := p.Base.WaitTrackedProcess(proc)
	p.endGame(proc.Pid)
	p.fe.gameExited(proc.Pid)
	//nolint:wrapcheck // Pass-through to base implementation
	return err
}

// LaunchMedia launches media and makes sure ES is running again if the launch
// stopped it but nothing is left running.
func (p *Platform) LaunchMedia(
	cfg *config.Instance,
	path string,
	launcher *platforms.Launcher,
	db *database.Database,
	opts *platforms.LaunchOptions,
) error {
	p.fe.beginLaunch()
	defer p.fe.endLaunch()
	//nolint:wrapcheck // Pass-through to base implementation
	return p.Base.LaunchMedia(cfg, path, launcher, db, opts, p)
}

// Launchers returns one launcher per ES system Zaparoo knows, built from
// es_systems.cfg, plus custom and generic launchers.
func (p *Platform) Launchers(cfg *config.Instance) []platforms.Launcher {
	builtIn := p.esLaunchers()
	builtIn = append(builtIn, launchers.NewGenericLauncher())
	return helpers.CombineLaunchers(cfg, p, builtIn)
}

func (p *Platform) esLaunchers() []platforms.Launcher {
	systems, err := loadESSystems(p.fs)
	if err != nil {
		log.Warn().Err(err).Msg("cannot read es_systems.cfg, no system launchers")
		return nil
	}
	settings := loadESSettings(p.fs)

	sort.SliceStable(systems, func(i, j int) bool { return systems[i].Name < systems[j].Name })
	result := make([]platforms.Launcher, 0, len(systems))
	byID := map[string]int{}
	for i := range systems {
		sys := &systems[i]
		info, ok := lookupSystem(sys)
		if !ok {
			log.Debug().Str("system", sys.Name).Msg("ES system has no Zaparoo mapping")
			continue
		}
		id := info.GetLauncherID()
		folder := sys.folder()
		exts := sys.extensions()
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
		launch := resolveLaunch(sys, settings, nil)
		byID[id] = len(result)
		result = append(result, platforms.Launcher{
			ID:         id,
			SystemID:   info.SystemID,
			Folders:    []string{folder},
			Extensions: exts,
			Lifecycle:  platforms.LifecycleBlocking,
			Launch:     p.launchGame,
			Controls:   p.retroArchControls(&launch),
		})
	}
	return result
}

// launchGame runs a ROM the way ES would: it resolves the ES system for the
// path, stops ES, and starts the system's command as the frontend user.
func (p *Platform) launchGame(
	_ *config.Instance, path string, _ *platforms.LaunchOptions,
) (*os.Process, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("ROM path is not absolute: %s", path)
	}
	path = filepath.Clean(path)
	if info, err := p.fs.Stat(path); err != nil {
		return nil, fmt.Errorf("ROM not found: %w", err)
	} else if info.IsDir() {
		return nil, fmt.Errorf("ROM path is a directory: %s", path)
	}

	systems, err := loadESSystems(p.fs)
	if err != nil {
		return nil, err
	}
	sys, err := systemForPath(systems, path, romRoots)
	if err != nil {
		return nil, err
	}
	acct, err := p.lookupAccount()
	if err != nil {
		return nil, err
	}
	launch := p.resolveGameLaunch(sys, path, acct.home)
	spec, err := newLaunchSpec(&launch, acct, path)
	if err != nil {
		return nil, err
	}

	if stopErr := p.fe.stopES(); stopErr != nil {
		return nil, fmt.Errorf("stop EmulationStation: %w", stopErr)
	}
	// A game being replaced may not have been reaped yet; its game-end
	// scripts run before this game's game-start ones, as they would in ES.
	p.endGame(0)
	p.fireEvent(acct, eventGameStart, gameStartArgs(path))
	proc, err := p.startGame(spec)
	if err == nil && proc == nil {
		err = errors.New("game process did not start")
	}
	if err != nil {
		p.fireEvent(acct, eventGameEnd, [2]string{})
		p.fe.launchFailed()
		return nil, err
	}
	p.eventMu.Lock()
	p.pendingEnd = &pendingGameEnd{acct: acct, pid: proc.Pid}
	p.eventMu.Unlock()
	p.fe.gameStarted(proc.Pid)
	log.Info().
		Str("system", sys.Name).
		Str("emulator", launch.emulator).
		Str("core", launch.core).
		Int("pid", proc.Pid).
		Msg("launched game")
	return proc, nil
}

// resolveGameLaunch resolves a ROM's launch with its own gamelist choices
// and ES settings.
func (p *Platform) resolveGameLaunch(sys *esSystem, romPath, home string) esLaunch {
	settings := loadESSettings(p.fs)
	meta := loadGameMeta(p.fs, sys, romPath, home)
	game := &esGame{
		emulator: meta.emulator,
		core:     meta.core,
		governor: meta.governor,
		name: func() string {
			return esGameName(p.fs, sys, settings, meta, romPath, home)
		},
	}
	return resolveLaunch(sys, settings, game)
}

// endGame runs the game-end scripts for the game Core last started, once.
// With a pid, only that game's exit counts; 0 ends whichever game is pending.
func (p *Platform) endGame(pid int) {
	p.eventMu.Lock()
	pending := p.pendingEnd
	if pending == nil || (pid != 0 && pending.pid != pid) {
		p.eventMu.Unlock()
		return
	}
	p.pendingEnd = nil
	p.eventMu.Unlock()
	p.fireEvent(pending.acct, eventGameEnd, [2]string{})
}

// terminateTree stops a process ES started and its children: SIGTERM, then
// SIGKILL for whatever is still running after esGameTermTimeout.
func (p *Platform) terminateTree(pid int) {
	root, err := process.NewProcess(int32(pid)) //nolint:gosec // PID fits in int32
	if err != nil {
		return
	}
	tree := append(descendants(root), root)
	for _, proc := range tree {
		if err := proc.Terminate(); err != nil {
			log.Debug().Err(err).Int32("pid", proc.Pid).Msg("failed to terminate process")
		}
	}
	deadline := p.clock.Now().Add(esGameTermTimeout)
	for p.clock.Now().Before(deadline) {
		if running, _ := root.IsRunning(); !running {
			return
		}
		p.clock.Sleep(esGamePollEvery)
	}
	for _, proc := range tree {
		if running, _ := proc.IsRunning(); running {
			_ = proc.Kill()
		}
	}
}

func descendants(proc *process.Process) []*process.Process {
	children, err := proc.Children()
	if err != nil {
		return nil
	}
	result := make([]*process.Process, 0, len(children))
	for _, child := range children {
		result = append(result, descendants(child)...)
		result = append(result, child)
	}
	return result
}
