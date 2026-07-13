//go:build linux

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

package linuxbase

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/gamelistxml"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/localmedia"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/idle"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	widgetmodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/widgets/models"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v4/process"
)

// Timeout constants for process termination.
const (
	// CustomKillTimeout is how long to wait for launcher-specific graceful shutdown.
	CustomKillTimeout = 500 * time.Millisecond
	// SIGTERMTimeout is how long to wait for graceful SIGTERM shutdown.
	SIGTERMTimeout = 3 * time.Second
	// SIGKILLTimeout is how long to wait after SIGKILL before proceeding.
	SIGKILLTimeout = 500 * time.Millisecond
)

// Base provides common functionality for all Linux-family platforms.
// Platforms embed this struct and override methods as needed.
type Base struct {
	launcherManager         platforms.LauncherContextManager
	serviceCtx              context.Context
	clock                   clockwork.Clock
	activeMedia             func() *models.ActiveMedia
	setActiveMedia          func(*models.ActiveMedia)
	trackedProcess          *os.Process
	completedTrackedProcess *os.Process
	trackedProcessDone      chan struct{}
	lastConfig              *config.Instance
	platformID              string
	lastLauncher            platforms.Launcher
	processMu               syncutil.RWMutex
	processWaitClaimed      bool
}

// NewBase creates a new Base with the given platform ID.
func NewBase(platformID string) *Base {
	return &Base{
		platformID: platformID,
		clock:      clockwork.NewRealClock(),
	}
}

// SetClock sets the clock for testing. Must be called before using the Base.
func (b *Base) SetClock(clock clockwork.Clock) {
	b.clock = clock
}

// ID returns the platform identifier.
func (b *Base) ID() string {
	return b.platformID
}

// StartPre is a no-op for Linux platforms.
func (*Base) StartPre(_ *config.Instance) error {
	return nil
}

// StartPost initializes the platform after service startup.
func (b *Base) StartPost(
	ctx context.Context,
	_ *config.Instance,
	launcherManager platforms.LauncherContextManager,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
	_ *database.Database,
	_ *idle.Scheduler,
) error {
	b.launcherManager = launcherManager
	b.serviceCtx = ctx
	b.activeMedia = activeMedia
	b.setActiveMedia = setActiveMedia
	return nil
}

// Stop is a no-op for Linux platforms.
func (*Base) Stop() error {
	return nil
}

// SetTrackedProcess stores a process handle, killing any existing tracked process.
func (b *Base) SetTrackedProcess(proc *os.Process) {
	b.processMu.Lock()
	defer b.processMu.Unlock()

	// Process handles may be recreated for the same PID when a tracker restarts.
	// Keep existing lifecycle state instead of signaling the live process.
	if b.trackedProcess != nil && proc != nil && b.trackedProcess.Pid == proc.Pid {
		return
	}

	// Kill any existing tracked process before setting new one.
	if b.trackedProcess != nil && b.trackedProcess != proc {
		if err := b.trackedProcess.Kill(); err != nil {
			log.Warn().Err(err).Msg("failed to kill previous tracked process")
		}
	}

	b.trackedProcess = proc
	b.completedTrackedProcess = nil
	b.processWaitClaimed = false
	if proc == nil {
		b.trackedProcessDone = nil
	} else {
		b.trackedProcessDone = make(chan struct{})
	}
	log.Debug().Msgf("set tracked process: %v", proc)
}

// ClearTrackedProcessPID forgets a completed externally-owned process without
// signaling it. The PID check prevents an older lifecycle event from clearing
// a newer tracked process.
func (b *Base) ClearTrackedProcessPID(pid int) bool {
	b.processMu.Lock()
	defer b.processMu.Unlock()

	if b.trackedProcess == nil || b.trackedProcess.Pid != pid {
		return false
	}

	b.trackedProcess = nil
	b.completedTrackedProcess = nil
	b.trackedProcessDone = nil
	b.processWaitClaimed = false
	return true
}

// WaitTrackedProcess waits for and reaps proc. StopActiveLauncher coordinates
// through the same completion channel so os.Process.Wait is called exactly once.
func (b *Base) WaitTrackedProcess(proc *os.Process) error {
	b.processMu.Lock()
	if b.trackedProcess != proc {
		b.processMu.Unlock()
		return errors.New("process is no longer tracked")
	}
	if b.trackedProcessDone == nil {
		b.trackedProcessDone = make(chan struct{})
	}
	done := b.trackedProcessDone
	if b.processWaitClaimed {
		b.processMu.Unlock()
		<-done
		return nil
	}
	b.processWaitClaimed = true
	b.processMu.Unlock()

	_, err := proc.Wait()
	b.finishTrackedProcess(proc, done)
	if err != nil {
		return fmt.Errorf("wait for tracked process: %w", err)
	}
	return nil
}

func (b *Base) finishTrackedProcess(proc *os.Process, done chan struct{}) {
	close(done)

	b.processMu.Lock()
	defer b.processMu.Unlock()
	if b.trackedProcess == proc {
		b.trackedProcess = nil
		b.completedTrackedProcess = proc
		b.trackedProcessDone = nil
		b.processWaitClaimed = false
	}
}

// ClearTrackedProcessMedia clears active media only if proc completed without
// a newer process being tracked. This prevents an old process waiter from
// clearing the active media published by a replacement launch.
func (b *Base) ClearTrackedProcessMedia(proc *os.Process) bool {
	b.processMu.Lock()
	if b.completedTrackedProcess != proc || b.trackedProcess != nil {
		b.processMu.Unlock()
		return false
	}
	b.completedTrackedProcess = nil
	b.processMu.Unlock()

	if b.setActiveMedia != nil {
		b.setActiveMedia(nil)
	}
	return true
}

func (b *Base) claimProcessWaiterLocked(proc *os.Process) <-chan struct{} {
	if b.trackedProcessDone == nil {
		b.trackedProcessDone = make(chan struct{})
	}
	done := b.trackedProcessDone
	if b.processWaitClaimed {
		return done
	}

	b.processWaitClaimed = true
	go func() {
		_, err := proc.Wait()
		if err != nil {
			log.Debug().Err(err).Int("pid", proc.Pid).Msg("tracked process wait completed with error")
		}
		b.finishTrackedProcess(proc, done)
	}()
	return done
}

// StopActiveLauncher stops the tracked process tree and clears active media.
// Launcher-specific shutdown runs first, followed by SIGTERM and SIGKILL fallback.
func (b *Base) StopActiveLauncher(_ platforms.StopIntent) error {
	if b.launcherManager != nil {
		b.launcherManager.NewContext()
	}

	b.processMu.Lock()
	proc := b.trackedProcess
	customKill := b.lastLauncher.Kill
	cfg := b.lastConfig
	b.lastLauncher = platforms.Launcher{}
	b.lastConfig = nil
	var done <-chan struct{}
	if proc != nil {
		done = b.claimProcessWaiterLocked(proc)
	}
	b.processMu.Unlock()

	if proc != nil {
		pid := int32(proc.Pid) //nolint:gosec // PID fits in int32
		exited := false
		if customKill != nil {
			log.Debug().Msg("using custom Kill function for launcher")
			if err := customKill(cfg); err != nil {
				log.Warn().Err(err).Msg("custom Kill function failed, falling back to signals")
			} else {
				exited = b.waitForExit(done, CustomKillTimeout)
			}
		}

		if !exited {
			procs := getProcessTree(pid)
			if len(procs) == 0 {
				log.Debug().Int32("pid", pid).Msg("process not found, may have already exited")
			} else {
				log.Debug().Int("count", len(procs)).Int32("rootPid", pid).Msg("terminating process tree")
				terminateProcessTree(procs)
				if !b.waitForExit(done, SIGTERMTimeout) {
					log.Debug().Msg("SIGTERM timeout, sending SIGKILL")
					killProcessTree(getProcessTree(pid))
				}
			}
		}

		select {
		case <-done:
			log.Debug().Msg("tracked process exited")
		case <-b.clock.After(SIGKILLTimeout):
			log.Debug().Msg("process cleanup timeout, proceeding anyway")
		}

		b.processMu.Lock()
		if b.trackedProcess == proc {
			b.trackedProcess = nil
			b.trackedProcessDone = nil
			b.processWaitClaimed = false
		}
		b.processMu.Unlock()
	}

	if b.setActiveMedia != nil {
		b.setActiveMedia(nil)
	}
	return nil
}

// waitForExit waits for the done channel or timeout, returns true if exited.
func (b *Base) waitForExit(done <-chan struct{}, timeout time.Duration) bool {
	select {
	case <-done:
		log.Debug().Msg("tracked process exited gracefully")
		return true
	case <-b.clock.After(timeout):
		return false
	}
}

// getProcessTree returns the process and all its descendants.
// Descendants are ordered before their parents for proper termination order.
func getProcessTree(pid int32) []*process.Process {
	proc, err := process.NewProcess(pid)
	if err != nil {
		return nil
	}

	descendants := getAllDescendants(proc)
	result := make([]*process.Process, 0, len(descendants)+1)
	result = append(result, descendants...)
	result = append(result, proc)
	return result
}

// getAllDescendants recursively finds all descendant processes (depth-first).
func getAllDescendants(proc *process.Process) []*process.Process {
	children, err := proc.Children()
	if err != nil || len(children) == 0 {
		return nil
	}
	descendants := make([]*process.Process, 0, len(children))
	for _, child := range children {
		descendants = append(descendants, getAllDescendants(child)...)
		descendants = append(descendants, child)
	}
	return descendants
}

// terminateProcessTree sends SIGTERM to all processes in the tree.
func terminateProcessTree(procs []*process.Process) {
	for _, proc := range procs {
		if err := proc.Terminate(); err != nil {
			log.Debug().Err(err).Int32("pid", proc.Pid).Msg("failed to terminate process")
		} else {
			log.Debug().Int32("pid", proc.Pid).Msg("sent SIGTERM to process")
		}
	}
}

// killProcessTree sends SIGKILL to all processes in the tree.
func killProcessTree(procs []*process.Process) {
	for _, proc := range procs {
		if err := proc.Kill(); err != nil {
			log.Debug().Err(err).Int32("pid", proc.Pid).Msg("failed to kill process")
		} else {
			log.Debug().Int32("pid", proc.Pid).Msg("sent SIGKILL to process")
		}
	}
}

// ScanHook is a no-op for Linux platforms.
func (*Base) ScanHook(_ *tokens.Token) error {
	return nil
}

// RootDirs returns the configured index roots.
func (*Base) RootDirs(cfg *config.Instance) []string {
	return cfg.IndexRoots()
}

// ReturnToMenu is a no-op for the base Linux implementation.
// Platforms with a menu concept (like SteamOS) should override this method.
func (*Base) ReturnToMenu() error {
	return nil
}

// LaunchSystem returns an error as launching systems is not supported.
func (*Base) LaunchSystem(_ *config.Instance, _ string) error {
	return errors.New("launching systems is not supported")
}

// LaunchMedia launches media using the appropriate launcher.
// The platform parameter is required to access the platform's Launchers method
// (struct embedding means Base cannot call methods defined on the outer type).
func (b *Base) LaunchMedia(
	cfg *config.Instance,
	path string,
	launcher *platforms.Launcher,
	db *database.Database,
	opts *platforms.LaunchOptions,
	p platforms.Platform,
) error {
	log.Info().Msgf("launch media: %s", path)

	var err error
	if launcher == nil {
		// Auto-detect launcher
		var foundLauncher platforms.Launcher
		foundLauncher, err = helpers.FindLauncher(cfg, p, path)
		if err != nil {
			return fmt.Errorf("launch media: error finding launcher: %w", err)
		}
		launcher = &foundLauncher
	}

	log.Info().Msgf("launch media: using launcher %s for: %s", launcher.ID, path)
	err = platforms.DoLaunch(&platforms.LaunchParams{
		Context:        b.serviceCtx,
		Config:         cfg,
		Platform:       p,
		SetActiveMedia: b.setActiveMedia,
		Launcher:       launcher,
		Path:           path,
		DB:             db,
		Options:        opts,
	}, helpers.GetPathName)
	if err != nil {
		return fmt.Errorf("launch media: error launching: %w", err)
	}

	b.processMu.Lock()
	b.lastLauncher = *launcher
	b.lastConfig = cfg
	b.processMu.Unlock()

	return nil
}

// KeyboardPress is a no-op for Linux platforms.
func (*Base) KeyboardPress(_ string) error {
	return nil
}

// GamepadPress is a no-op for Linux platforms.
func (*Base) GamepadPress(_ string) error {
	return nil
}

// Screenshot is not supported on generic Linux platforms.
func (*Base) Screenshot() (*platforms.ScreenshotResult, error) {
	return nil, platforms.ErrNotSupported
}

// ForwardCmd returns an empty result (no command forwarding on Linux platforms).
func (*Base) ForwardCmd(_ *platforms.CmdEnv) (platforms.CmdResult, error) {
	return platforms.CmdResult{}, nil
}

// LookupMapping returns false (no token mappings on Linux platforms).
func (*Base) LookupMapping(_ *tokens.Token) (string, bool) {
	return "", false
}

// ShowNotice returns ErrNotSupported (no UI widgets on Linux platforms).
func (*Base) ShowNotice(
	_ *config.Instance,
	_ widgetmodels.NoticeArgs,
) (func() error, time.Duration, error) {
	return nil, 0, platforms.ErrNotSupported
}

// ShowLoader returns ErrNotSupported (no UI widgets on Linux platforms).
func (*Base) ShowLoader(
	_ *config.Instance,
	_ widgetmodels.NoticeArgs,
) (func() error, error) {
	return nil, platforms.ErrNotSupported
}

// ShowPicker returns ErrNotSupported (no UI widgets on Linux platforms).
func (*Base) ShowPicker(
	_ *config.Instance,
	_ widgetmodels.PickerArgs,
) error {
	return platforms.ErrNotSupported
}

// ConsoleManager returns a no-op console manager.
func (*Base) ConsoleManager() platforms.ConsoleManager {
	return platforms.NoOpConsoleManager{}
}

// ManagedByPackageManager returns false. Platforms that embed Base and have
// package manager detection should override this method.
func (*Base) ManagedByPackageManager() bool {
	return false
}

// Scrapers returns the default set of metadata scrapers for Linux-based platforms.
func (*Base) Scrapers(_ *config.Instance) map[string]platforms.Scraper {
	gamelist := gamelistxml.NewPlatformScraper()
	media := localmedia.NewPlatformScraper()
	return map[string]platforms.Scraper{gamelist.ID: gamelist, media.ID: media}
}
