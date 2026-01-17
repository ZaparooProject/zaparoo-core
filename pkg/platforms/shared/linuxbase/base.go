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
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	widgetmodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/widgets/models"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v4/process"
)

// Timeout constants for process termination.
const (
	// SIGTERMTimeout is how long to wait for graceful SIGTERM shutdown.
	SIGTERMTimeout = 3 * time.Second
	// SIGKILLTimeout is how long to wait after SIGKILL before proceeding.
	SIGKILLTimeout = 500 * time.Millisecond
)

// Base provides common functionality for all Linux-family platforms.
// Platforms embed this struct and override methods as needed.
type Base struct {
	launcherManager platforms.LauncherContextManager
	clock           clockwork.Clock
	activeMedia     func() *models.ActiveMedia
	setActiveMedia  func(*models.ActiveMedia)
	trackedProcess  *os.Process
	platformID      string
	processMu       syncutil.RWMutex
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
	_ *config.Instance,
	launcherManager platforms.LauncherContextManager,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
	_ *database.Database,
) error {
	b.launcherManager = launcherManager
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

	// Kill any existing tracked process before setting new one
	if b.trackedProcess != nil {
		if err := b.trackedProcess.Kill(); err != nil {
			log.Warn().Err(err).Msg("failed to kill previous tracked process")
		}
	}

	b.trackedProcess = proc
	log.Debug().Msgf("set tracked process: %v", proc)
}

// StopActiveLauncher kills tracked process and all its children, then clears active media.
// Uses SIGTERM first for graceful shutdown, then SIGKILL after timeout.
func (b *Base) StopActiveLauncher(_ platforms.StopIntent) error {
	if b.launcherManager != nil {
		b.launcherManager.NewContext()
	}

	b.processMu.Lock()
	defer b.processMu.Unlock()

	if b.trackedProcess != nil {
		proc := b.trackedProcess // Capture to avoid race with Wait goroutine
		pid := int32(proc.Pid)   //nolint:gosec // PID fits in int32
		done := make(chan struct{})
		go func() {
			_, _ = proc.Wait()
			close(done)
		}()

		procs := getProcessTree(pid)
		if len(procs) == 0 {
			log.Debug().Int32("pid", pid).Msg("process not found, may have already exited")
		} else {
			log.Debug().Int("count", len(procs)).Int32("rootPid", pid).Msg("terminating process tree")

			// Children are terminated before parent to avoid orphaning
			terminateProcessTree(procs)

			if !b.waitForExit(done, SIGTERMTimeout) {
				log.Debug().Msg("SIGTERM timeout, sending SIGKILL")
				killProcessTree(procs)
			}
		}

		select {
		case <-done:
			log.Debug().Msg("tracked process exited")
		case <-b.clock.After(SIGKILLTimeout):
			log.Debug().Msg("process cleanup timeout, proceeding anyway")
		}

		b.trackedProcess = nil
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
