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
package pinup

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/assets"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

// Timeouts bound every wait the integration performs. Nothing waits without
// one, but the values are generous: Popper ignores an exit request while the
// emulator is still loading a table, and only Popper can close a table without
// leaving it stuck, so a stop that lands during a load has to outwait the load.
type Timeouts struct {
	// MenuStart is how long PinUpMenu.exe may take to appear after Core
	// starts it.
	MenuStart time.Duration
	// ServerStart is how long the web remote may take to answer after Core
	// starts PuPServer.exe.
	ServerStart time.Duration
	// LaunchAccept is how long the emulator process may take to appear after
	// Popper is asked to launch a table.
	LaunchAccept time.Duration
	// LaunchRetry is how long to wait for the emulator process before asking
	// Popper again. Popper drops a launch that arrives while it is still
	// returning to the wheel after the previous table, and it starts the
	// emulator within a second when it accepts, so a quiet few seconds means
	// the request was lost rather than slow.
	LaunchRetry time.Duration
	// StopWait is how long the emulator may take to exit after Popper is
	// asked to close the table. It covers a table that is still loading when
	// the request arrives, because Popper drops exits until the emulator is
	// up, and killing the emulator instead leaves Popper's launch helpers
	// waiting on a player window for up to a minute, ignoring every launch.
	StopWait time.Duration
	// ExitRetry is how often the exit event is repeated while waiting, since
	// a request that lands during the load is dropped rather than queued.
	ExitRetry time.Duration
	// LaunchSettle is the pause after PinUpMenu.exe appears before it is sent
	// commands.
	LaunchSettle time.Duration
	// Poll is the process list polling interval.
	Poll time.Duration
}

// DefaultTimeouts returns the production waits.
func DefaultTimeouts() Timeouts {
	return Timeouts{
		MenuStart:    30 * time.Second,
		ServerStart:  10 * time.Second,
		LaunchAccept: 20 * time.Second,
		LaunchRetry:  6 * time.Second,
		StopWait:     25 * time.Second,
		ExitRetry:    5 * time.Second,
		LaunchSettle: time.Second,
		Poll:         500 * time.Millisecond,
	}
}

// Deps are the platform hooks and external boundaries the integration uses.
// Every boundary is an interface or function so tests can script Popper, the
// emulator processes and the clock. The emulator process is never handed to
// the platform: it is followed by PID, and stopped only through Popper, so
// the platform's process-tree kill cannot leave Popper stuck mid-launch.
type Deps struct {
	ActiveMedia    func() *models.ActiveMedia
	SetActiveMedia func(*models.ActiveMedia)
	Frontend       Frontend
	Processes      ProcessLister
	Clock          clockwork.Clock
	HTTP           HTTPDoer
	Locator        Locator
	Timeouts       Timeouts
}

var (
	// ErrNoActiveTable is returned by StopTable when Core is not tracking a
	// Popper launch.
	ErrNoActiveTable = errors.New("no active PinUP Popper table")
	// ErrTableRunning is returned when a table did not exit within the wait
	// after Popper was asked to close it.
	ErrTableRunning = errors.New("a PinUP Popper table is still running")
	// ErrWebRemoteMissing is returned when the install lacks PuPServer.exe,
	// the only documented way to ask Popper to launch a table.
	ErrWebRemoteMissing = errors.New("PinUP Popper web remote (PuPServer.exe) is not installed")
	// ErrNoStopMechanism is returned when no web remote is available to ask
	// Popper to close a table.
	ErrNoStopMechanism = errors.New("PinUP Popper web remote is not available to close the table")
)

// Integration launches tables through Popper and tracks the emulator process
// Popper starts for them. It owns ActiveMedia for Popper launches, as the
// launcher has an external lifecycle.
type Integration struct {
	tracked    map[int]trackedLaunch
	done       chan struct{}
	remote     *Remote
	activePath string
	activeExes []string
	deps       Deps
	active     launchKey
	wg         sync.WaitGroup
	nextID     int
	stopOnce   sync.Once
	mu         syncutil.Mutex
	// Serialize initial launches, retries and exit requests. Shutdown uses the
	// same lock to drain admission before waiting for registered workers.
	requestMu syncutil.Mutex
}

// NewIntegration wires the integration; nil boundaries get real ones.
func NewIntegration(deps *Deps) *Integration {
	if deps.Clock == nil {
		deps.Clock = clockwork.NewRealClock()
	}
	if deps.Processes == nil {
		deps.Processes = NewProcessLister()
	}
	if deps.HTTP == nil {
		deps.HTTP = &http.Client{Timeout: remoteRequestTimeout}
	}
	if deps.Timeouts == (Timeouts{}) {
		deps.Timeouts = DefaultTimeouts()
	}
	return &Integration{
		deps:    *deps,
		tracked: make(map[int]trackedLaunch),
		done:    make(chan struct{}),
	}
}

// Stop ends every watcher and forgets the tables they followed.
func (i *Integration) Stop() {
	i.stopOnce.Do(func() {
		close(i.done)
	})
	// Launch registers its watcher before releasing requestMu. Do not hold
	// this lock while waiting: a watcher may be finishing a retry.
	i.requestMu.Lock()
	i.requestMu.Unlock() //nolint:gocritic,staticcheck // Admission barrier; workers need this lock while draining.
	i.wg.Wait()

	i.mu.Lock()
	defer i.mu.Unlock()
	for gameID := range i.tracked {
		delete(i.tracked, gameID)
	}
	i.active = launchKey{}
	i.activePath = ""
	i.activeExes = nil
}

// Locate resolves the install from config, registry and default paths.
func (i *Integration) Locate(cfg *config.Instance) (Install, error) {
	configDir := ""
	if cfg != nil {
		configDir = strings.TrimSpace(cfg.LookupLauncherDefaults(LauncherID, nil).InstallDir)
	}
	inst, err := i.deps.Locator.Locate(configDir)
	if err != nil {
		return Install{}, err
	}
	return inst, nil
}

// Available reports whether Popper is installed.
func (i *Integration) Available(cfg *config.Instance) error {
	if _, err := i.Locate(cfg); err != nil {
		return err
	}
	return nil
}

// Scan reads the launchable tables. An absent install contributes nothing
// rather than failing the scan; a configured directory that does not hold
// Popper is a real error the user should see.
func (i *Integration) Scan(ctx context.Context, cfg *config.Instance) ([]platforms.ScanResult, error) {
	inst, err := i.Locate(cfg)
	if errors.Is(err, ErrNotInstalled) {
		log.Debug().Msg("PinUP Popper not installed, skipping scan")
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan PinUP Popper tables: %w", err)
	}
	lib, err := ReadLibrary(ctx, inst.DBPath)
	if err != nil {
		return nil, fmt.Errorf("scan PinUP Popper tables: %w", err)
	}
	return ScanResults(lib), nil
}

// Launch asks Popper to start a table and begins tracking the emulator it
// spawns. It returns once the request has been sent; ActiveMedia is
// published by a watcher when the emulator appears.
func (i *Integration) Launch(cfg *config.Instance, path string) error {
	i.requestMu.Lock()
	defer i.requestMu.Unlock()
	select {
	case <-i.done:
		return context.Canceled
	default:
	}
	gameID, err := ParseTablePath(path)
	if err != nil {
		return err
	}
	inst, err := i.Locate(cfg)
	if err != nil {
		return fmt.Errorf("launch PinUP Popper table: %w", err)
	}

	ctx := context.Background()
	lib, err := ReadLibrary(ctx, inst.DBPath)
	if err != nil {
		return fmt.Errorf("launch PinUP Popper table: %w", err)
	}
	table, emu, ok := lib.Table(gameID)
	if !ok {
		return fmt.Errorf("PinUP Popper game %d is not an enabled pinball table", gameID)
	}
	candidates := TableExecutables(&table, &emu)
	if len(candidates) == 0 {
		return fmt.Errorf(
			"PinUP Popper emulator %q has no process Core can track; set its Process Name in Popper's emulator setup",
			emu.Name,
		)
	}

	if menuErr := i.ensureMenu(ctx, &inst); menuErr != nil {
		return menuErr
	}
	remote, err := i.ensureRemote(ctx, cfg, &inst)
	if err != nil {
		return err
	}
	if closeErr := i.closeRunningTables(ctx, remote, libraryExecutables(lib)); closeErr != nil {
		return closeErr
	}

	before, err := i.snapshotPIDs(candidates)
	if err != nil {
		return err
	}
	key := i.claim(gameID, path, candidates)
	if err := remote.LaunchGame(ctx, gameID); err != nil {
		i.release(key)
		return fmt.Errorf("launch PinUP Popper table: %w", err)
	}
	log.Info().Int("gameID", gameID).Str("table", table.DisplayName()).
		Str("emulator", emu.Name).Msg("asked PinUP Popper to launch table")

	i.wg.Add(1)
	go func() {
		defer i.wg.Done()
		i.watch(key, remote, path, table.DisplayName(), candidates, before)
	}()
	return nil
}

// StopTable asks Popper to close the running table and waits, bounded, for
// the emulator to go. It never clears ActiveMedia itself: the platform does
// that once the stop is confirmed, and the watcher's own exit handling is
// idempotent. A timeout is an error, which the platform reports as a failed
// stop because Core holds no process handle to escalate with.
func (i *Integration) StopTable() error {
	i.requestMu.Lock()
	defer i.requestMu.Unlock()

	i.mu.Lock()
	key := i.active
	exes := i.activeExes
	launch, tracked := i.tracked[key.gameID]
	remote := i.remote
	i.mu.Unlock()

	if key == (launchKey{}) && !tracked {
		return ErrNoActiveTable
	}

	pid := 0
	if tracked {
		pid = launch.pid
	}
	exited, err := i.closeTable(context.Background(), remote, func(procs []ProcessInfo) bool {
		if pid != 0 {
			return !pidRunning(procs, pid, exes)
		}
		return len(matchingProcesses(procs, exes)) == 0
	})
	if err != nil {
		return err
	}
	if !exited {
		return fmt.Errorf("%w: emulator did not exit within %s of the exit request",
			ErrTableRunning, i.deps.Timeouts.StopWait)
	}
	// Retire pending launches too: no process yet does not mean the watcher
	// has finished, and a late process must not reclaim media after a stop.
	i.release(key)
	return nil
}

// closeTable asks Popper to close the running table and waits, up to
// StopWait, for gone to report the emulator has left, repeating the exit
// event every ExitRetry because a request that lands while the emulator is
// still loading is dropped.
func (i *Integration) closeTable(ctx context.Context, remote *Remote, gone func([]ProcessInfo) bool) (bool, error) {
	if err := i.sendEmuExit(ctx, remote); err != nil {
		return false, err
	}
	deadline := i.deps.Clock.Now().Add(i.deps.Timeouts.StopWait)
	retry := i.deps.Timeouts.ExitRetry
	if retry <= 0 {
		retry = i.deps.Timeouts.StopWait
	}
	for {
		remaining := deadline.Sub(i.deps.Clock.Now())
		if remaining <= 0 {
			return false, nil
		}
		if i.waitUntil(min(retry, remaining), gone) {
			return true, nil
		}
		if !i.deps.Clock.Now().Before(deadline) {
			return false, nil
		}
		log.Debug().Msg("PinUP Popper table still running, repeating exit event")
		if err := i.sendEmuExit(ctx, remote); err != nil {
			return false, err
		}
	}
}

// EmulatorRunning reports whether the emulator of the tracked launch is
// still alive. It lets the platform refuse to report a stop it cannot
// confirm.
func (i *Integration) EmulatorRunning() bool {
	i.mu.Lock()
	key := i.active
	exes := i.activeExes
	launch, tracked := i.tracked[key.gameID]
	i.mu.Unlock()
	if key == (launchKey{}) {
		return false
	}
	procs, err := i.deps.Processes.List()
	if err != nil {
		return false
	}
	if tracked {
		return pidRunning(procs, launch.pid, exes)
	}
	return len(matchingProcesses(procs, exes)) > 0
}

// closeRunningTables handles a table started by hand from the wheel, which
// Core is not tracking: Popper is asked to close it and given StopWait to do
// so before the new launch goes out.
func (i *Integration) closeRunningTables(ctx context.Context, remote *Remote, exes []string) error {
	procs, err := i.deps.Processes.List()
	if err != nil {
		return fmt.Errorf("list processes: %w", err)
	}
	running := matchingProcesses(procs, exes)
	if len(running) == 0 {
		return nil
	}
	log.Info().Str("exe", running[0].Exe).Msg("closing running PinUP Popper table before launch")
	closed, err := i.closeTable(ctx, remote, func(procs []ProcessInfo) bool {
		return len(matchingProcesses(procs, exes)) == 0
	})
	if err != nil {
		return err
	}
	if !closed {
		return fmt.Errorf("%w: cannot launch over it", ErrTableRunning)
	}
	return nil
}

// sendEmuExit asks Popper to close the current table through the web remote,
// which runs the emulator's exit script and returns Popper to the wheel.
func (*Integration) sendEmuExit(ctx context.Context, remote *Remote) error {
	if remote == nil {
		return ErrNoStopMechanism
	}
	if err := remote.SendEvent(ctx, EventEmuExit); err != nil {
		return fmt.Errorf("send PinUP Popper exit event: %w", err)
	}
	return nil
}

// ensureMenu starts PinUpMenu.exe when it is not running and waits for it.
func (i *Integration) ensureMenu(ctx context.Context, inst *Install) error {
	procs, err := i.deps.Processes.List()
	if err != nil {
		return fmt.Errorf("list processes: %w", err)
	}
	if _, running := processRunning(procs, menuExeName); running {
		return nil
	}
	log.Info().Str("dir", inst.Dir).Msg("starting PinUP Popper")
	if err := i.deps.Frontend.StartMenu(ctx, inst); err != nil {
		return fmt.Errorf("start PinUP Popper: %w", err)
	}
	started := i.waitUntil(i.deps.Timeouts.MenuStart, func(procs []ProcessInfo) bool {
		_, running := processRunning(procs, menuExeName)
		return running
	})
	if !started {
		return fmt.Errorf("PinUP Popper did not start within %s", i.deps.Timeouts.MenuStart)
	}
	i.sleep(i.deps.Timeouts.LaunchSettle)
	return nil
}

// ensureRemote finds a web remote that answers: the configured server_url,
// the one used last time, Popper's own on port 80, or one Core starts.
func (i *Integration) ensureRemote(ctx context.Context, cfg *config.Instance, inst *Install) (*Remote, error) {
	configured := ""
	if cfg != nil {
		configured = strings.TrimSpace(cfg.LookupLauncherDefaults(LauncherID, nil).ServerURL)
	}
	if configured != "" {
		remote, err := NewRemote(configured, i.deps.HTTP)
		if err != nil {
			return nil, err
		}
		if err := remote.Probe(ctx); err != nil {
			return nil, fmt.Errorf("configured PinUP Popper server_url is not answering: %w", err)
		}
		i.setRemote(remote)
		return remote, nil
	}

	if current := i.currentRemote(); current != nil && current.Probe(ctx) == nil {
		return current, nil
	}

	procs, err := i.deps.Processes.List()
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	if _, running := processRunning(procs, serverExeName); running {
		for _, port := range []int{DefaultServerPort, CoreServerPort} {
			remote, remoteErr := NewRemote(ServerURL(port), i.deps.HTTP)
			if remoteErr != nil {
				continue
			}
			if remote.Probe(ctx) == nil {
				i.setRemote(remote)
				return remote, nil
			}
		}
		return nil, fmt.Errorf("PuPServer.exe is running but does not answer on ports %d or %d; set server_url",
			DefaultServerPort, CoreServerPort)
	}

	if inst.ServerExe == "" {
		return nil, ErrWebRemoteMissing
	}
	log.Info().Int("port", CoreServerPort).Msg("starting PinUP Popper web remote")
	if startErr := i.deps.Frontend.StartServer(ctx, inst, CoreServerPort); startErr != nil {
		return nil, fmt.Errorf("start PinUP Popper web remote: %w", startErr)
	}
	remote, err := NewRemote(ServerURL(CoreServerPort), i.deps.HTTP)
	if err != nil {
		return nil, err
	}
	answered := i.waitUntil(i.deps.Timeouts.ServerStart, func([]ProcessInfo) bool {
		return remote.Probe(ctx) == nil
	})
	if !answered {
		return nil, fmt.Errorf("PinUP Popper web remote did not answer within %s", i.deps.Timeouts.ServerStart)
	}
	i.setRemote(remote)
	return remote, nil
}

func (i *Integration) currentRemote() *Remote {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.remote
}

func (i *Integration) setRemote(remote *Remote) {
	i.mu.Lock()
	i.remote = remote
	i.mu.Unlock()
}

// snapshotPIDs records which candidate processes exist before a launch so
// the watcher only adopts one Popper started for this launch.
func (i *Integration) snapshotPIDs(candidates []string) (map[int]struct{}, error) {
	procs, err := i.deps.Processes.List()
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	before := make(map[int]struct{})
	for _, p := range matchingProcesses(procs, candidates) {
		before[p.PID] = struct{}{}
	}
	return before, nil
}

// claim records a new run as the one that owns ActiveMedia from now on.
func (i *Integration) claim(gameID int, path string, exes []string) launchKey {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.nextID++
	i.active = launchKey{gameID: gameID, lifecycleID: i.nextID}
	i.activePath = path
	i.activeExes = exes
	return i.active
}

// release drops the claim if the run still holds it.
func (i *Integration) release(key launchKey) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.active == key {
		i.active = launchKey{}
		i.activePath = ""
		i.activeExes = nil
	}
}

func (i *Integration) isActive(key launchKey) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	select {
	case <-i.done:
		return false
	default:
		return i.active == key
	}
}

// retryLaunch rechecks ownership after acquiring the request lock. Checking
// only in the watcher would allow a concurrent stop to send its exit first.
func (i *Integration) retryLaunch(key launchKey, remote *Remote) bool {
	i.requestMu.Lock()
	defer i.requestMu.Unlock()
	if !i.isActive(key) {
		return false
	}
	log.Info().Int("gameID", key.gameID).Msg("PinUP Popper has not started the table yet, asking again")
	if err := remote.LaunchGame(context.Background(), key.gameID); err != nil {
		log.Warn().Err(err).Int("gameID", key.gameID).Msg("repeating PinUP Popper launch failed")
	}
	return true
}

// watch waits for the emulator process Popper starts, records it, publishes
// ActiveMedia and then follows the process until it exits. The launch is
// repeated once if nothing appears within LaunchRetry.
func (i *Integration) watch(
	key launchKey, remote *Remote, path, name string, candidates []string, before map[int]struct{},
) {
	started := i.deps.Clock.Now()
	deadline := started.Add(i.deps.Timeouts.LaunchAccept)
	retried := i.deps.Timeouts.LaunchRetry <= 0
	pid := 0
	for pid == 0 {
		if !i.isActive(key) {
			return
		}
		procs, err := i.deps.Processes.List()
		if err != nil {
			log.Debug().Err(err).Msg("listing processes while waiting for PinUP Popper table")
		}
		pid = newCandidate(procs, candidates, before)
		if pid != 0 {
			break
		}
		now := i.deps.Clock.Now()
		if !now.Before(deadline) {
			log.Warn().Int("gameID", key.gameID).Strs("executables", candidates).
				Msg("PinUP Popper table process did not appear; not tracking this launch")
			i.release(key)
			return
		}
		if !retried && !now.Before(started.Add(i.deps.Timeouts.LaunchRetry)) {
			retried = true
			if !i.retryLaunch(key, remote) {
				return
			}
		}
		if !i.sleep(i.deps.Timeouts.Poll) {
			return
		}
	}

	if !i.adopt(key, pid, path, name) {
		return
	}
	for i.sleep(i.deps.Timeouts.Poll) {
		procs, err := i.deps.Processes.List()
		if err != nil {
			log.Debug().Err(err).Msg("listing processes while following PinUP Popper table")
			continue
		}
		if !pidRunning(procs, pid, candidates) {
			i.onExit(key, pid, path)
			return
		}
	}
}

// newCandidate picks the process to adopt: the first candidate executable, in
// preference order, with a PID that did not exist before the launch.
func newCandidate(procs []ProcessInfo, candidates []string, before map[int]struct{}) int {
	for _, candidate := range candidates {
		for _, p := range procs {
			if _, existed := before[p.PID]; existed {
				continue
			}
			if MatchesExecutable(p.Exe, []string{candidate}) {
				return p.PID
			}
		}
	}
	return 0
}

// adopt records and publishes the process, but only while the run is still
// active. Recording and publishing happen under one hold of the lock so a
// concurrent stop or relaunch cannot slip between them.
func (i *Integration) adopt(key launchKey, pid int, path, name string) bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.active != key {
		return false
	}
	select {
	case <-i.done:
		return false
	default:
	}

	i.tracked[key.gameID] = trackedLaunch{pid: pid, lifecycleID: key.lifecycleID}

	systemName := systemdefs.SystemPinball
	if meta, err := assets.GetSystemMetadata(systemdefs.SystemPinball); err == nil {
		systemName = meta.Name
	}
	activeMedia := models.NewActiveMedia(systemdefs.SystemPinball, systemName, path, name, LauncherID)
	log.Info().Int("gameID", key.gameID).Int("pid", pid).Str("table", name).Msg("PinUP Popper table started")
	if i.deps.SetActiveMedia != nil {
		i.deps.SetActiveMedia(activeMedia)
	}
	return true
}

// onExit forgets the exited process and clears ActiveMedia, but only for the
// run that recorded it: a late exit for an earlier run must not discard the
// process or media of the run that replaced it.
func (i *Integration) onExit(key launchKey, pid int, path string) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if launch, ok := i.tracked[key.gameID]; ok && launch.lifecycleID == key.lifecycleID {
		delete(i.tracked, key.gameID)
	}
	if i.active != key {
		log.Debug().Int("gameID", key.gameID).Msg("ignoring stale PinUP Popper table exit")
		return
	}
	i.active = launchKey{}
	i.activePath = ""
	i.activeExes = nil
	log.Info().Int("gameID", key.gameID).Int("pid", pid).Msg("PinUP Popper table exited")

	if i.deps.ActiveMedia == nil || i.deps.SetActiveMedia == nil {
		return
	}
	current := i.deps.ActiveMedia()
	if current == nil || current.Path != path {
		return
	}
	i.deps.SetActiveMedia(nil)
}

// waitUntil polls the process list until cond holds or timeout passes.
func (i *Integration) waitUntil(timeout time.Duration, cond func([]ProcessInfo) bool) bool {
	deadline := i.deps.Clock.Now().Add(timeout)
	for {
		procs, err := i.deps.Processes.List()
		if err != nil {
			log.Debug().Err(err).Msg("listing processes for PinUP Popper wait")
		} else if cond(procs) {
			return true
		}
		if !i.deps.Clock.Now().Before(deadline) {
			return false
		}
		if !i.sleep(i.deps.Timeouts.Poll) {
			return false
		}
	}
}

// sleep waits for d unless the integration is stopping.
func (i *Integration) sleep(d time.Duration) bool {
	select {
	case <-i.done:
		return false
	case <-i.deps.Clock.After(d):
		return true
	}
}

// libraryExecutables collects the executables of every emulator in the
// library, for spotting a table started outside Core.
func libraryExecutables(lib Library) []string {
	seen := make(map[string]struct{})
	exes := make([]string, 0, len(lib.Emulators)*2)
	for id := range lib.Emulators {
		emu := lib.Emulators[id]
		for _, exe := range CandidateExecutables(&emu) {
			key := strings.ToLower(exe)
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			exes = append(exes, exe)
		}
	}
	return exes
}
