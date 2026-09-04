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
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/gamelistxml"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/localmedia"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/deverr"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/kodi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam/steamtracker"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/windows/windowfocus"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/acr122pcsc"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/externaldrive"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/file"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/mqtt"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/pn532"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/rs232barcode"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/simpleserial"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/tty2oled"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/idle"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/adrg/xdg"
	"github.com/rs/zerolog/log"
)

type processWindowFocuser interface {
	Focus(context.Context, uint32) error
}

type Platform struct {
	activeMedia             func() *models.ActiveMedia
	setActiveMedia          func(*models.ActiveMedia)
	customPlatformToSystem  map[string]string
	systemToCustomPlatforms map[string][]string
	trackedProcess          *os.Process
	completedTrackedProcess *os.Process
	launchBoxPipe           *LaunchBoxPipeServer
	steamTracker            *steamtracker.WindowsPlatformIntegration
	launcherManager         platforms.LauncherContextManager
	windowFocuser           processWindowFocuser
	windowFocusCancel       context.CancelFunc
	launchBoxActiveID       string
	launchBoxActiveAppPath  string
	lastLauncher            platforms.Launcher
	processMu               syncutil.RWMutex
	platformMappingsMu      syncutil.RWMutex
	launchBoxPipeLock       syncutil.Mutex
	launchBoxActiveMu       syncutil.Mutex
}

const errWindowsInvalidParameter syscall.Errno = 87

var retroBatLaunchSettleDelay = 2 * time.Second

// Stop escalation timings. Vars rather than consts so tests can shorten them.
var (
	// windowsStopBudget bounds a whole stop, including the launcher's own Kill
	// and both escalation phases.
	windowsStopBudget          = 20 * time.Second
	windowsCustomKillTimeout   = 3 * time.Second
	windowsGracefulStopTimeout = 4 * time.Second
	windowsForcedStopTimeout   = 3 * time.Second
	windowsStopPollInterval    = 100 * time.Millisecond

	// Indirected for tests; both end the whole tree, the first by asking.
	windowsTaskKillTree      = runTaskKillPIDTreeGraceful
	windowsForceTaskKillTree = runTaskKillPIDTree

	// Indirected for tests: reading the live Steam AppID needs a real registry.
	windowsMediaStillRunning = (*Platform).mediaStillRunning
)

func (*Platform) ID() string {
	return platformids.Windows
}

func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	allReaders := []readers.Reader{
		pn532.NewReader(cfg),
		file.NewReader(cfg),
		simpleserial.NewReader(cfg),
		rs232barcode.NewReader(cfg),
		acr122pcsc.NewAcr122Pcsc(cfg),
		tty2oled.NewReader(cfg, p),
		mqtt.NewReader(cfg),
		externaldrive.NewReader(cfg),
	}

	var enabled []readers.Reader
	for _, r := range allReaders {
		metadata := r.Metadata()
		driver := config.DriverInfo{
			ID:                metadata.ID,
			DefaultEnabled:    metadata.DefaultEnabled,
			DefaultAutoDetect: metadata.DefaultAutoDetect,
		}
		if cfg.IsReaderEnabled(driver, config.ReaderEnableContextCandidate) {
			enabled = append(enabled, r)
		}
	}
	return enabled
}

func (*Platform) StartPre(_ *config.Instance) error {
	return nil
}

func (p *Platform) StartPost(
	_ context.Context,
	cfg *config.Instance,
	launcherManager platforms.LauncherContextManager,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
	_ *database.Database,
	_ *idle.Scheduler,
) error {
	p.activeMedia = activeMedia
	p.setActiveMedia = setActiveMedia
	p.launcherManager = launcherManager
	p.windowFocuser = windowfocus.New()

	// Initialize LaunchBox pipe server if LaunchBox is installed
	p.initLaunchBoxPipe(cfg)

	// Start Steam tracker for external Steam game detection. The tracker needs
	// the same Steam root the scanner resolves from the registry, otherwise it
	// cannot read app manifests to name games or locate their processes. It is
	// resolved per use because the tracker can start after Steam is installed,
	// by which point a root captured here would be a stale fallback.
	steamClient := steam.NewClient(steam.DefaultWindowsOptions())
	p.steamTracker = steamtracker.NewWindowsPlatformIntegration(
		p.SetTrackedProcess,
		p.ClearTrackedProcessPID,
		activeMedia,
		setActiveMedia,
		func() string { return steamClient.FindSteamDir(cfg) },
	)
	if err := p.steamTracker.Start(); err != nil {
		log.Warn().Err(err).Msg("steam game tracker failed to start")
	}

	return nil
}

func (p *Platform) Stop() error {
	if p.launcherManager != nil {
		p.launcherManager.NewContext()
	}

	// Stop Steam tracker
	if p.steamTracker != nil {
		p.steamTracker.Stop()
	}

	// Stop LaunchBox named pipe server
	p.launchBoxPipeLock.Lock()
	if p.launchBoxPipe != nil {
		p.launchBoxPipe.Stop()
		p.launchBoxPipe = nil
	}
	p.launchBoxPipeLock.Unlock()

	return nil
}

func (*Platform) ScanHook(_ *tokens.Token) error {
	return nil
}

func (*Platform) RootDirs(cfg *config.Instance) []string {
	return cfg.IndexRoots()
}

func (*Platform) Settings() platforms.Settings {
	return platforms.Settings{
		DataDir:    filepath.Join(xdg.DataHome, config.AppName),
		ConfigDir:  filepath.Join(xdg.ConfigHome, config.AppName),
		TempDir:    filepath.Join(os.TempDir(), config.AppName),
		LogDir:     filepath.Join(xdg.DataHome, config.AppName, config.LogsDir),
		ZipsAsDirs: false,
	}
}

// SetTrackedProcess adopts proc as the process Core will stop next.
//
// Note this kills whatever was previously tracked: replacing the tracked
// process is how a new launch preempts the old one. Callers that merely want
// to forget a process must use ClearTrackedProcessPID instead.
func (p *Platform) SetTrackedProcess(proc *os.Process) {
	p.processMu.Lock()
	if p.trackedProcess != nil && proc != nil && p.trackedProcess.Pid == proc.Pid {
		p.processMu.Unlock()
		return
	}

	if p.windowFocusCancel != nil {
		p.windowFocusCancel()
		p.windowFocusCancel = nil
	}
	if p.trackedProcess != nil {
		if err := p.trackedProcess.Kill(); err != nil && !isProcessFinishedError(err) {
			log.Warn().Err(err).Msg("failed to kill previous tracked process")
		}
	}

	p.trackedProcess = proc
	p.completedTrackedProcess = nil
	focuser := p.windowFocuser
	if proc == nil || focuser == nil {
		p.processMu.Unlock()
		log.Debug().Msgf("set tracked process: %v", proc)
		return
	}

	focusCtx := context.Background()
	if p.launcherManager != nil {
		if ctx := p.launcherManager.GetContext(); ctx != nil {
			focusCtx = ctx
		}
	}
	focusCtx, p.windowFocusCancel = context.WithCancel(focusCtx)
	p.processMu.Unlock()

	log.Debug().Msgf("set tracked process: %v", proc)
	pid := uint32(proc.Pid) //nolint:gosec // Windows process IDs are 32-bit values
	go func() {
		if err := focuser.Focus(focusCtx, pid); err != nil && !errors.Is(err, context.Canceled) {
			log.Debug().Err(err).Int("pid", proc.Pid).Msg("launched process window was not focused")
		}
	}()
}

func isProcessFinishedError(err error) bool {
	return errors.Is(err, os.ErrProcessDone) || errors.Is(err, errWindowsInvalidParameter)
}

// WaitTrackedProcess waits for proc and removes its stale process handle when it exits.
func (p *Platform) WaitTrackedProcess(proc *os.Process) error {
	_, err := proc.Wait()

	p.processMu.Lock()
	if p.trackedProcess == proc {
		p.trackedProcess = nil
		p.completedTrackedProcess = proc
	}
	p.processMu.Unlock()

	if err != nil {
		return fmt.Errorf("wait for tracked process: %w", err)
	}
	return nil
}

// ClearTrackedProcessMedia clears active media only when proc is still the latest completed launch.
func (p *Platform) ClearTrackedProcessMedia(proc *os.Process) bool {
	p.processMu.Lock()
	if p.completedTrackedProcess != proc || p.trackedProcess != nil {
		p.processMu.Unlock()
		return false
	}
	p.completedTrackedProcess = nil
	p.processMu.Unlock()

	if p.setActiveMedia != nil {
		p.setActiveMedia(nil)
	}
	return true
}

func (p *Platform) setLastLauncher(l *platforms.Launcher) {
	p.processMu.Lock()
	defer p.processMu.Unlock()
	p.lastLauncher = *l
}

// ClearTrackedProcessPID forgets a completed externally-owned process without
// signalling it. The PID check prevents a stale lifecycle event from clearing a
// newer tracked process.
func (p *Platform) ClearTrackedProcessPID(pid int) bool {
	p.processMu.Lock()
	defer p.processMu.Unlock()

	if p.trackedProcess == nil || p.trackedProcess.Pid != pid {
		return false
	}

	p.trackedProcess = nil
	p.completedTrackedProcess = nil
	return true
}

// waitForProcessExit polls until proc has exited, the phase timeout elapses,
// or the overall stop budget runs out. Windows has no signal to wait on here:
// the process may be one Core never started, so there is no handle to reap,
// only liveness to observe.
func waitForProcessExit(proc *os.Process, timeout time.Duration, budget time.Time) bool {
	deadline := time.Now().Add(timeout)
	if deadline.After(budget) {
		deadline = budget
	}
	for {
		if !helpers.IsProcessRunning(proc) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(windowsStopPollInterval)
	}
}

// stopTrackedProcessTree ends proc and everything it spawned. Games routinely
// run behind a launcher stub, so terminating the single tracked PID would
// leave the actual game running. taskkill without /F asks the tree to close
// first, which lets a game save and shut down cleanly; /F follows only if that
// is ignored.
func stopTrackedProcessTree(proc *os.Process, budget time.Time) bool {
	if !helpers.IsProcessRunning(proc) {
		return true
	}
	if proc.Pid < 0 {
		return false
	}
	pid := uint32(proc.Pid) //nolint:gosec // Windows PIDs are 32-bit and checked non-negative above.

	// taskkill runs under the same budget: a hung invocation would otherwise
	// block the stop RPC handler with no bound at all.
	ctx, cancel := context.WithDeadline(context.Background(), budget)
	defer cancel()

	if err := windowsTaskKillTree(ctx, pid); err != nil {
		log.Debug().Err(err).Uint32("pid", pid).Msg("graceful tree close failed")
	}
	if waitForProcessExit(proc, windowsGracefulStopTimeout, budget) {
		log.Debug().Uint32("pid", pid).Msg("tracked process tree closed gracefully")
		return true
	}

	log.Debug().Uint32("pid", pid).Msg("close timeout, forcing tree kill")
	if err := windowsForceTaskKillTree(ctx, pid); err != nil {
		log.Warn().Err(err).Uint32("pid", pid).Msg("forced tree kill failed")
	}
	return waitForProcessExit(proc, windowsForcedStopTimeout, budget)
}

// clearLastLauncher forgets the launcher a confirmed stop just acted on, but
// only while it is still the current one, so a launch that started during the
// stop keeps its own launcher.
func (p *Platform) clearLastLauncher(stopped *platforms.Launcher) {
	p.processMu.Lock()
	defer p.processMu.Unlock()
	if p.lastLauncher.ID == stopped.ID {
		p.lastLauncher = platforms.Launcher{}
	}
}

// clearTrackedProcess forgets proc, but only while it is still the tracked
// process. A nil proc clears nothing: the stop snapshotted no handle, so there
// is nothing of its own to forget, and wiping the field would discard a
// process the Steam tracker or LaunchBox published while the stop ran.
func (p *Platform) clearTrackedProcess(proc *os.Process) {
	if proc == nil {
		return
	}
	p.processMu.Lock()
	defer p.processMu.Unlock()
	if p.trackedProcess == proc {
		p.trackedProcess = nil
	}
}

// mediaStillRunning reports whether Core can positively confirm the media is
// still running. It is deliberately one-sided: false means "no evidence", not
// "confirmed stopped", so callers must not treat it as proof of a clean exit.
//
// Steam publishes the running AppID in the registry, which is independent of
// whether Core managed to find the game's process, and is the only source
// available once process tracking has failed.
func (p *Platform) mediaStillRunning() bool {
	if p.activeMedia == nil {
		return false
	}
	current := p.activeMedia()
	if current == nil {
		return false
	}

	appID, ok := steam.ExtractAppIDFromPath(current.Path)
	if !ok {
		return false
	}
	running, err := steamtracker.GetRunningAppID()
	if err != nil {
		log.Debug().Err(err).Msg("could not read Steam running AppID")
		return false
	}
	return running != 0 && running == appID
}

// stopWithoutMechanism handles a stop when Core holds neither a process handle
// nor a launcher Kill function.
//
// Having no way to stop something is not the same as knowing it is still
// running, and only the second justifies refusing the caller. Reporting
// failure without evidence left a launch unable to preempt anything, so the
// media is only kept when Core can actually confirm it is still there.
func (p *Platform) stopWithoutMechanism(launcher *platforms.Launcher) error {
	if p.activeMedia == nil || p.activeMedia() == nil {
		return nil
	}

	// Launchers that never hand back a handle by design -- a browser tab, a
	// detached script -- have nothing to stop, so clearing media is the honest
	// outcome. Reading the zero-value lifecycle as fire-and-forget is
	// deliberate and matches DoLaunch, which also ignores any process returned
	// by a launcher that did not opt into tracking.
	byDesign := launcher.ID != "" && launcher.Lifecycle == platforms.LifecycleFireAndForget

	if !byDesign && windowsMediaStillRunning(p) {
		log.Warn().Msg("active media is still running and cannot be stopped")
		return fmt.Errorf("windows: no tracked process for running media: %w", platforms.ErrStopFailed)
	}

	if !byDesign {
		log.Debug().Msg("no tracked process for active media, clearing unconfirmed media")
	}
	if p.setActiveMedia != nil {
		p.setActiveMedia(nil)
	}
	return nil
}

func (p *Platform) StopActiveLauncher(_ platforms.StopIntent) error {
	if p.launcherManager != nil {
		p.launcherManager.NewContext()
	}

	// The launcher is kept until the stop is confirmed. Clearing it up front
	// would leave a failed stop with no Kill function to retry with, so every
	// later attempt would report failure without trying anything.
	p.processMu.Lock()
	proc := p.trackedProcess
	lastLauncher := p.lastLauncher
	p.completedTrackedProcess = nil
	if p.windowFocusCancel != nil {
		p.windowFocusCancel()
		p.windowFocusCancel = nil
	}
	p.processMu.Unlock()

	customKill := lastLauncher.Kill
	if customKill == nil && proc == nil {
		return p.stopWithoutMechanism(&lastLauncher)
	}

	// One budget for the whole escalation. Stacking each phase's timeout in
	// series would let a misbehaving launcher hold up the stop RPC, and every
	// launch that preempts it, for the sum of them.
	budget := time.Now().Add(windowsStopBudget)

	stopped := false
	if customKill != nil {
		log.Debug().Msg("using custom Kill function for launcher")
		if err := customKill(&config.Instance{}); err != nil {
			log.Warn().Err(err).Msg("custom Kill function failed, falling back to process tree")
		} else {
			stopped = proc == nil || waitForProcessExit(proc, windowsCustomKillTimeout, budget)
		}
	}

	if !stopped && proc != nil {
		stopped = stopTrackedProcessTree(proc, budget)
	}

	if !stopped {
		// Nothing was playing, so nothing failed to stop: a launcher Kill that
		// reports "no active game" is the expected answer when Core is idle,
		// and must not be mistaken for media that refused to die.
		if p.activeMedia == nil || p.activeMedia() == nil {
			p.clearLastLauncher(&lastLauncher)
			p.clearTrackedProcess(proc)
			return nil
		}
		// Keep the handle so a retry still has something to act on, and leave
		// active media alone so Core's state keeps matching reality.
		log.Warn().Msg("active launcher did not stop, leaving active media in place")
		return fmt.Errorf("windows: %w", platforms.ErrStopFailed)
	}

	p.clearLastLauncher(&lastLauncher)
	p.clearTrackedProcess(proc)
	if p.setActiveMedia != nil {
		p.setActiveMedia(nil)
	}
	return nil
}

func (*Platform) ReturnToMenu() error {
	// No menu concept on this platform
	return nil
}

func (*Platform) LaunchSystem(_ *config.Instance, _ string) error {
	return errors.New("launching systems is not supported")
}

func (p *Platform) LaunchMedia(
	cfg *config.Instance, path string, launcher *platforms.Launcher,
	db *database.Database, opts *platforms.LaunchOptions,
) error {
	log.Info().Msgf("launch media: %s", path)

	if launcher == nil {
		foundLauncher, err := helpers.FindLauncher(cfg, p, path)
		if err != nil {
			return fmt.Errorf("launch media: error finding launcher: %w", err)
		}
		launcher = &foundLauncher
	}

	log.Info().Msgf("launch media: using launcher %s for: %s", launcher.ID, path)

	if isRetroBatLauncher(launcher) {
		_, running, runningErr := esapi.APIRunningGame()
		if runningErr != nil {
			log.Warn().Err(runningErr).Msg("RetroBat ES API unavailable, assuming no game is running")
		} else if running {
			log.Info().Msg("exiting current RetroBat media")
			if stopErr := p.StopActiveLauncher(platforms.StopForPreemption); stopErr != nil {
				return fmt.Errorf("failed to stop active RetroBat launcher: %w", stopErr)
			}
			time.Sleep(retroBatLaunchSettleDelay)
		}
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

	p.setLastLauncher(launcher)

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
	const staticLauncherCount = 14
	launchers := make([]platforms.Launcher, 0, staticLauncherCount+len(esde.SystemMap))

	launchers = append(launchers,
		kodi.NewKodiLocalLauncher(),
		kodi.NewKodiMovieLauncher(),
		kodi.NewKodiTVLauncher(),
		kodi.NewKodiMusicLauncher(),
		kodi.NewKodiSongLauncher(),
		kodi.NewKodiAlbumLauncher(),
		kodi.NewKodiArtistLauncher(),
		kodi.NewKodiTVShowLauncher(),
		newWindowsSteamLauncher(),
		platforms.Launcher{
			ID:       "Flashpoint",
			SystemID: systemdefs.SystemPC,
			Schemes:  []string{shared.SchemeFlashpoint},
			Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
				// Handle native Flashpoint URL format: flashpoint://run/123
				// Normalize to standard virtual path format: flashpoint://123
				if strings.HasPrefix(path, "flashpoint://run/") {
					path = strings.Replace(path, "flashpoint://run/", "flashpoint://", 1)
				}

				id, err := virtualpath.ExtractSchemeID(path, shared.SchemeFlashpoint)
				if err != nil {
					return nil, fmt.Errorf("failed to extract Flashpoint game ID from path: %w", err)
				}

				//nolint:gosec // Safe: launches Flashpoint with game ID from internal database
				cmd := exec.CommandContext(context.Background(),
					helpers.ComSpec(), "/c",
					"start",
					"flashpoint://run/"+id,
				)
				cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
				err = cmd.Start()
				if err != nil {
					return nil, fmt.Errorf("failed to start flashpoint: %w", err)
				}
				return nil, nil //nolint:nilnil // Flashpoint launches don't return a process handle
			},
		},
		platforms.Launcher{
			ID:        "WebBrowser",
			Schemes:   []string{"http", "https"},
			Lifecycle: platforms.LifecycleFireAndForget,
			Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
				//nolint:gosec // Safe: opens URL in default browser via cmd start
				cmd := exec.CommandContext(context.Background(),
					helpers.ComSpec(), "/c",
					"start",
					path,
				)
				cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
				err := cmd.Start()
				if err != nil {
					return nil, fmt.Errorf("failed to open URL in browser: %w", err)
				}
				return nil, nil //nolint:nilnil // Browser launches don't return a process handle
			},
		},
		platforms.Launcher{
			ID:            "GenericExecutable",
			Extensions:    []string{".exe"},
			AllowListOnly: true,
			Lifecycle:     platforms.LifecycleBlocking,
			Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
				//nolint:gosec // Safe: executes user-configured allow-listed executable
				cmd := exec.CommandContext(context.Background(), path)
				cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
				if err := cmd.Start(); err != nil {
					return nil, fmt.Errorf("failed to start executable: %w", err)
				}
				return cmd.Process, nil
			},
		},
		platforms.Launcher{
			ID:            "GenericScript",
			Extensions:    []string{".bat", ".cmd", ".lnk", ".a3x", ".ahk"},
			AllowListOnly: true,
			Lifecycle:     platforms.LifecycleFireAndForget,
			Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
				ext := strings.ToLower(filepath.Ext(path))
				var cmd *exec.Cmd
				// Extensions not in default PATHEXT need START command for proper execution
				if ext == ".lnk" || ext == ".a3x" || ext == ".ahk" {
					//nolint:gosec // Safe: executes user-configured allow-listed script
					cmd = exec.CommandContext(context.Background(), helpers.ComSpec(), "/c", "start", "", path)
				} else {
					// .bat, .cmd work fine with direct execution
					//nolint:gosec // Safe: executes user-configured allow-listed script
					cmd = exec.CommandContext(context.Background(), helpers.ComSpec(), "/c", path)
				}
				cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
				err := cmd.Start()
				if err != nil {
					return nil, fmt.Errorf("failed to start script: %w", err)
				}
				return nil, nil //nolint:nilnil // Script launches don't return a process handle
			},
		},
		p.NewLaunchBoxLauncher(),
	)

	launchers = append(launchers, getRetroBatLaunchers()...)

	launchers = append(launchers, deverr.GetDevErrLaunchers()...)

	return helpers.CombineLaunchers(cfg, p, launchers)
}

// newWindowsSteamLauncher builds the Steam launcher with external lifecycle
// tracking. Steam owns the game process, so ActiveMedia must come from the
// tracker seeing the game actually start rather than from firing the steam://
// URL, which succeeds even when Steam then blocks on a dialog.
func newWindowsSteamLauncher() platforms.Launcher {
	launcher := steam.NewSteamLauncher(steam.DefaultWindowsOptions())
	launcher.Lifecycle = platforms.LifecycleExternal
	return launcher
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
	return map[string]platforms.Scraper{gamelist.ID: gamelist, media.ID: media}
}
