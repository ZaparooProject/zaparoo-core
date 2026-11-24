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

package sim

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
	"github.com/rs/zerolog/log"
)

// Behaviour launcher IDs. Each one owns the ROM folder of the same name in
// lower case (SimBlocking -> roms/simblocking) with the extension ".sim", and
// covers one part of the launcher contract.
const (
	LauncherFireForget      = "SimFireForget"
	LauncherBlocking        = "SimBlocking"
	LauncherExternal        = "SimExternal"
	LauncherRunningInstance = "SimRunningInstance"
	LauncherScanner         = "SimScanner"
	LauncherScheme          = "SimScheme"
	LauncherDetails         = "SimDetails"
	LauncherReady           = "SimReady"
	LauncherUnavailable     = "SimUnavailable"
	LauncherPreflight       = "SimPreflight"
	LauncherNoActive        = "SimNoActive"
	LauncherKill            = "SimKill"

	// BehaviourExtension is the file extension behaviour launchers index.
	BehaviourExtension = ".sim"
	// BehaviourSystem is the system behaviour launchers report.
	BehaviourSystem = systemdefs.SystemPC
	// SchemeName is the URI scheme of SimScheme's virtual media.
	SchemeName = "simscheme"
	// ScannerScheme is the URI scheme of SimScanner's extra virtual entries.
	ScannerScheme = "simscanner"
	// SystemLauncherPrefix starts the ID of every per-system sim launcher:
	// SimSNES, SimGenesis and so on.
	SystemLauncherPrefix = "Sim"
	// RunningInstance is the UsesRunningInstance value of SimRunningInstance.
	RunningInstance = "sim"

	defaultBlockingDuration = 2 * time.Second
	defaultFireForgetLife   = 200 * time.Millisecond
	defaultExternalDelay    = 300 * time.Millisecond
	defaultReadyDelay       = 300 * time.Millisecond
	virtualEntries          = 5
)

var (
	errInjectedLaunch    = errors.New("sim: injected launch failure")
	errInjectedPreflight = errors.New("sim: injected preflight failure")
	errInjectedKill      = errors.New("sim: injected kill failure")
	errUnavailable       = errors.New("sim: launcher made unavailable")
	errPreflightRefused  = errors.New("sim: SimPreflight always refuses")
	errStopped           = errors.New("sim: platform stopped")
)

// BehaviourFolders returns the ROM folder of every behaviour launcher that
// indexes files, keyed by launcher ID.
func BehaviourFolders() map[string]string {
	out := map[string]string{}
	for _, id := range []string{
		LauncherFireForget, LauncherBlocking, LauncherExternal, LauncherRunningInstance,
		LauncherScanner, LauncherDetails, LauncherReady, LauncherUnavailable,
		LauncherPreflight, LauncherNoActive, LauncherKill,
	} {
		out[id] = strings.ToLower(id)
	}
	return out
}

// SystemLauncherID returns the ID of the sim launcher for a system.
func SystemLauncherID(systemID string) string {
	return SystemLauncherPrefix + systemID
}

// systemEntry is one Zaparoo system and the ES-DE folders that hold it.
type systemEntry struct {
	id         string
	folders    []string
	extensions []string
}

// systemEntries groups esde.SystemMap by system ID.
func systemEntries() []systemEntry {
	byID := map[string]*systemEntry{}
	for folder, info := range esde.SystemMap {
		e, ok := byID[info.SystemID]
		if !ok {
			e = &systemEntry{id: info.SystemID}
			byID[info.SystemID] = e
		}
		e.folders = append(e.folders, folder)
		e.extensions = append(e.extensions, info.Extensions...)
	}
	out := make([]systemEntry, 0, len(byID))
	for _, e := range byID {
		sort.Strings(e.folders)
		e.extensions = uniqueLower(e.extensions)
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

func uniqueLower(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.ToLower(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// builtinLaunchers returns every sim launcher.
func (p *Platform) builtinLaunchers() []platforms.Launcher {
	entries := systemEntries()
	ls := make([]platforms.Launcher, 0, len(entries)+16)
	for _, e := range entries {
		ls = append(ls, p.systemLauncher(e))
	}
	return append(ls, p.behaviourLaunchers()...)
}

func (p *Platform) systemLauncher(e systemEntry) platforms.Launcher {
	id := SystemLauncherID(e.id)
	l := platforms.Launcher{
		ID:         id,
		SystemID:   e.id,
		Folders:    e.folders,
		Extensions: e.extensions,
		Lifecycle:  platforms.LifecycleTracked,
		Controls:   p.controls(id),
	}
	p.guard(&l)
	l.Launch = func(_ *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
		if err := p.beginLaunch(id, e.id, path, opts); err != nil {
			return nil, err
		}
		return p.spawn(id, path, p.childLife(id, 0), true, nil)
	}
	return l
}

// controls returns recording controls for a launcher.
func (p *Platform) controls(launcherID string) map[string]platforms.Control {
	names := []string{
		platforms.ControlSaveState, platforms.ControlLoadState, platforms.ControlReset,
		platforms.ControlTogglePause, platforms.ControlToggleMenu, platforms.ControlFastForward,
	}
	out := make(map[string]platforms.Control, len(names))
	for _, name := range names {
		out[name] = platforms.Control{
			Func: func(_ context.Context, _ *config.Instance, params platforms.ControlParams) error {
				p.journal.Record(&Event{
					Kind: EventControl, Launcher: launcherID, Detail: name, Args: params.Args,
				})
				return nil
			},
		}
	}
	return out
}

// guard adds the fault checks every sim launcher shares.
func (p *Platform) guard(l *platforms.Launcher) {
	id := l.ID
	l.Availability = func(*config.Instance) error {
		if p.faults.For(id).Unavailable {
			p.journal.Record(&Event{Kind: EventFault, Launcher: id, Detail: "unavailable"})
			return errUnavailable
		}
		return nil
	}
	l.Preflight = func(_ *config.Instance, path string, _ *platforms.LaunchOptions) error {
		if p.faults.For(id).FailPreflight {
			p.journal.Record(&Event{Kind: EventFault, Launcher: id, Path: path, Detail: "failPreflight"})
			return errInjectedPreflight
		}
		return nil
	}
}

// beginLaunch records a launch and applies launch faults.
func (p *Platform) beginLaunch(id, system, path string, opts *platforms.LaunchOptions) error {
	action := ""
	if opts != nil {
		action = opts.Action
	}
	p.mu.Lock()
	p.lastLaunch = id
	p.mu.Unlock()
	f := p.faults.For(id)
	if f.LaunchDelayMS > 0 {
		time.Sleep(ms(f.LaunchDelayMS))
	}
	if p.faults.TakeLaunchFailure(id) {
		p.journal.Record(&Event{
			Kind: EventFault, Launcher: id, System: system, Path: path, Detail: "failLaunches",
		})
		return errInjectedLaunch
	}
	p.journal.Record(&Event{Kind: EventLaunch, Launcher: id, System: system, Path: path, Intent: action})
	return nil
}

// childLife returns how long a launched child lives: forever (0) unless the
// launcher has a default or exitAfterMs is set.
func (p *Platform) childLife(id string, def time.Duration) time.Duration {
	if f := p.faults.For(id); f.ExitAfterMS > 0 {
		return ms(f.ExitAfterMS)
	}
	return def
}

// spawn starts a sleep child standing in for an emulator. A zero life sleeps
// forever. Tracked children are reaped by linuxbase, so the sim only watches
// /proc for their exit; untracked ones are reaped here and onExit runs after.
func (p *Platform) spawn(
	id, path string,
	life time.Duration,
	tracked bool,
	onExit func(natural bool),
) (*os.Process, error) {
	arg := "infinity"
	if life > 0 {
		arg = strconv.FormatFloat(life.Seconds(), 'f', 3, 64)
	}
	select {
	case <-p.done:
		return nil, errStopped
	default:
	}
	// The child's lifetime is managed by tracking and kill, not a context.
	//nolint:gosec,noctx // fixed binary and argument, no user input
	cmd := exec.Command(p.sleepPath, arg)
	cmd.Dir = p.layout.Temp
	// Stop kills children on a clean exit; Pdeathsig covers the rest, so a
	// crashed, killed or timed-out sim never leaves one running.
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("sim: start child: %w", err)
	}
	proc := cmd.Process
	c, ok := p.addChild(id, proc)
	if !ok {
		_ = proc.Kill()
		_ = cmd.Wait()
		return nil, errStopped
	}
	p.journal.Record(&Event{Kind: EventProcess, Launcher: id, Path: path, PID: proc.Pid, Detail: arg})

	crashed := make(chan struct{})
	if f := p.faults.For(id); f.CrashAfterMS > 0 {
		time.AfterFunc(ms(f.CrashAfterMS), func() {
			close(crashed)
			p.journal.Record(&Event{
				Kind: EventFault, Launcher: id, Path: path, PID: proc.Pid, Detail: "crashAfterMs",
			})
			_ = proc.Signal(syscall.SIGKILL)
		})
	}
	stopping := c.stopping

	finish := func(status string) {
		p.removeChild(proc.Pid)
		natural := false
		select {
		case <-crashed:
			status = "crashed"
		case <-stopping:
			status = "stopped"
		default:
			// Tracked children are reaped by Core, so "exited" carries no
			// status; neither crashed nor stopped means it ran to its end.
			natural = status == "exit 0" || status == "exited"
		}
		p.journal.Record(&Event{Kind: EventProcessExit, Launcher: id, Path: path, PID: proc.Pid, Detail: status})
		if onExit != nil {
			onExit(natural)
		}
	}
	if tracked {
		go p.watchProc(proc.Pid, func() { finish("exited") })
	} else {
		go func() {
			err := cmd.Wait()
			status := "exit 0"
			if err != nil {
				status = err.Error()
			}
			finish(status)
		}()
	}
	return proc, nil
}

// watchProc waits until pid is a zombie or gone, without reaping it.
func (p *Platform) watchProc(pid int, done func()) {
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-t.C:
		}
		data, err := os.ReadFile(statPath) //nolint:gosec // fixed /proc path
		if err != nil {
			done()
			return
		}
		// The state follows the parenthesised command name.
		s := string(data)
		if i := strings.LastIndexByte(s, ')'); i >= 0 && i+2 < len(s) && s[i+2] == 'Z' {
			done()
			return
		}
	}
}

func (p *Platform) behaviourLaunchers() []platforms.Launcher {
	folders := BehaviourFolders()
	base := func(id string, lc platforms.LauncherLifecycle) platforms.Launcher {
		l := platforms.Launcher{
			ID:         id,
			SystemID:   BehaviourSystem,
			Folders:    []string{folders[id]},
			Extensions: []string{BehaviourExtension},
			Lifecycle:  lc,
			Controls:   p.controls(id),
		}
		p.guard(&l)
		return l
	}
	var ls []platforms.Launcher

	ff := base(LauncherFireForget, platforms.LifecycleFireAndForget)
	ff.Launch = func(_ *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
		if err := p.beginLaunch(ff.ID, ff.SystemID, path, opts); err != nil {
			return nil, err
		}
		_, err := p.spawn(ff.ID, path, p.childLife(ff.ID, defaultFireForgetLife), false, nil)
		return nil, err
	}
	ls = append(ls, ff)

	// Core's blocking waiter reaps the child and clears the media; the sim
	// reports MediaFinished when the child ran to its end.
	blocking := base(LauncherBlocking, platforms.LifecycleBlocking)
	p.withFinished(&blocking)
	ls = append(ls, blocking)

	ext := base(LauncherExternal, platforms.LifecycleExternal)
	ext.Launch = func(_ *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
		if err := p.beginLaunch(ext.ID, ext.SystemID, path, opts); err != nil {
			return nil, err
		}
		return nil, p.launchExternal(ext.ID, path)
	}
	ext.Kill = func(*config.Instance) error { return p.killExternal(ext.ID) }
	ls = append(ls, ext)

	ri := base(LauncherRunningInstance, platforms.LifecycleFireAndForget)
	ri.UsesRunningInstance = RunningInstance
	ri.Launch = func(_ *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
		return nil, p.beginLaunch(ri.ID, ri.SystemID, path, opts)
	}
	ls = append(ls, ri)

	scanner := base(LauncherScanner, platforms.LifecycleTracked)
	p.systemStyle(&scanner)
	scanner.Schemes = []string{ScannerScheme}
	scanner.Scanner = func(
		_ context.Context, _ *config.Instance, _ string, results []platforms.ScanResult,
	) ([]platforms.ScanResult, error) {
		p.journal.Record(&Event{Kind: EventLaunch, Launcher: LauncherScanner, Detail: "scanner"})
		for i := 1; i <= virtualEntries; i++ {
			results = append(results, platforms.ScanResult{
				Path:  fmt.Sprintf("%s://extra/%d", ScannerScheme, i),
				Name:  fmt.Sprintf("Scanner Extra %d", i),
				NoExt: true,
			})
		}
		return results, nil
	}
	scanner.VirtualPathExists = func(_ *config.Instance, path string) bool {
		return virtualIndex(path, ScannerScheme+"://extra/") > 0
	}
	ls = append(ls, scanner)

	scheme := base(LauncherScheme, platforms.LifecycleTracked)
	p.systemStyle(&scheme)
	scheme.Folders = nil
	scheme.Extensions = nil
	scheme.Schemes = []string{SchemeName}
	scheme.SkipFilesystemScan = true
	scheme.Scanner = func(
		_ context.Context, _ *config.Instance, _ string, results []platforms.ScanResult,
	) ([]platforms.ScanResult, error) {
		for i := 1; i <= virtualEntries; i++ {
			results = append(results, platforms.ScanResult{
				Path:  fmt.Sprintf("%s://game/%d", SchemeName, i),
				Name:  fmt.Sprintf("Scheme Game %d", i),
				NoExt: true,
			})
		}
		return results, nil
	}
	scheme.VirtualPathExists = func(_ *config.Instance, path string) bool {
		return virtualIndex(path, SchemeName+"://game/") > 0
	}
	ls = append(ls, scheme)

	details := base(LauncherDetails, platforms.LifecycleTracked)
	p.systemStyle(&details)
	details.SupportsDetails = true
	launchDetails := details.Launch
	details.Launch = func(cfg *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
		if opts != nil && platforms.IsActionDetails(opts.Action) {
			return nil, p.beginLaunch(details.ID, details.SystemID, path, opts)
		}
		return launchDetails(cfg, path, opts)
	}
	ls = append(ls, details)

	ready := base(LauncherReady, platforms.LifecycleTracked)
	p.systemStyle(&ready)
	ready.WaitForReady = func(ctx context.Context, _ *config.Instance, m *models.ActiveMedia) error {
		return p.waitReady(ctx, LauncherReady, m)
	}
	ready.GameRunning = func(_ context.Context, _ *config.Instance, m *models.ActiveMedia) (bool, error) {
		running := !p.faults.For(LauncherReady).NotRunning
		p.journal.Record(&Event{
			Kind: EventRunning, Launcher: LauncherReady, Path: mediaPath(m), Detail: strconv.FormatBool(running),
		})
		return running, nil
	}
	ls = append(ls, ready)

	unavailable := base(LauncherUnavailable, platforms.LifecycleTracked)
	p.systemStyle(&unavailable)
	unavailable.AvailabilityReason = "SimUnavailable is never available"
	unavailable.Availability = func(*config.Instance) error {
		p.journal.Record(&Event{Kind: EventFault, Launcher: LauncherUnavailable, Detail: "unavailable"})
		return errUnavailable
	}
	ls = append(ls, unavailable)

	preflight := base(LauncherPreflight, platforms.LifecycleTracked)
	p.systemStyle(&preflight)
	preflight.Preflight = func(_ *config.Instance, path string, _ *platforms.LaunchOptions) error {
		p.journal.Record(&Event{Kind: EventFault, Launcher: LauncherPreflight, Path: path, Detail: "preflight"})
		return errPreflightRefused
	}
	ls = append(ls, preflight)

	noactive := base(LauncherNoActive, platforms.LifecycleFireAndForget)
	noactive.NoActiveMedia = true
	noactive.Launch = func(_ *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
		return nil, p.beginLaunch(noactive.ID, noactive.SystemID, path, opts)
	}
	ls = append(ls, noactive)

	kill := base(LauncherKill, platforms.LifecycleTracked)
	p.systemStyle(&kill)
	kill.Kill = func(*config.Instance) error {
		if p.faults.For(LauncherKill).FailKill {
			p.journal.Record(&Event{Kind: EventFault, Launcher: LauncherKill, Detail: "failKill"})
			return errInjectedKill
		}
		pids := p.childrenOf(LauncherKill)
		p.journal.Record(&Event{Kind: EventKill, Launcher: LauncherKill, Detail: fmt.Sprint(pids)})
		for _, pid := range pids {
			p.markStopped(pid)
			_ = syscall.Kill(pid, syscall.SIGTERM)
		}
		return nil
	}
	ls = append(ls, kill)

	return ls
}

// systemStyle gives a behaviour launcher the per-system Tracked launch.
func (p *Platform) systemStyle(l *platforms.Launcher) {
	id, system := l.ID, l.SystemID
	l.Launch = func(_ *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
		if err := p.beginLaunch(id, system, path, opts); err != nil {
			return nil, err
		}
		return p.spawn(id, path, p.childLife(id, 0), true, nil)
	}
}

// withFinished wraps a blocking launcher so a child that exits by itself
// reports MediaFinished, as a player reaching the end of its media does.
func (p *Platform) withFinished(l *platforms.Launcher) {
	id, system := l.ID, l.SystemID
	l.Launch = func(_ *config.Instance, path string, opts *platforms.LaunchOptions) (*os.Process, error) {
		if err := p.beginLaunch(id, system, path, opts); err != nil {
			return nil, err
		}
		var finished func()
		if opts != nil {
			finished = opts.MediaFinished
		}
		life := p.childLife(id, defaultBlockingDuration)
		proc, err := p.spawn(id, path, life, true, func(natural bool) {
			if natural && finished != nil {
				p.journal.Record(&Event{Kind: EventFinished, Launcher: id, Path: path})
				finished()
			}
		})
		return proc, err
	}
}

func (p *Platform) waitReady(ctx context.Context, id string, m *models.ActiveMedia) error {
	f := p.faults.For(id)
	p.journal.Record(&Event{
		Kind: EventReady, Launcher: id, Path: mediaPath(m), Detail: "wait",
	})
	if f.ReadyTimeout {
		<-ctx.Done()
		p.journal.Record(&Event{Kind: EventFault, Launcher: id, Path: mediaPath(m), Detail: "readyTimeout"})
		return ctx.Err() //nolint:wrapcheck // readiness callers check context errors
	}
	select {
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // readiness callers check context errors
	case <-time.After(defaultReadyDelay):
	}
	p.journal.Record(&Event{Kind: EventReady, Launcher: id, Path: mediaPath(m), Detail: "ready"})
	return nil
}

// launchExternal starts a child Core does not track and publishes its media
// itself after a delay, the way a platform tracker reports a game it saw start.
func (p *Platform) launchExternal(id, path string) error {
	p.mu.Lock()
	gen := p.externalGen + 1
	p.externalGen = gen
	p.mu.Unlock()

	proc, err := p.spawn(id, path, p.childLife(id, 0), false, func(bool) {
		p.mu.Lock()
		current := p.externalGen == gen
		p.mu.Unlock()
		if current && p.activeMediaIs(id, path) {
			p.journal.Record(&Event{Kind: EventExternal, Launcher: id, Path: path, Detail: "cleared"})
			p.publish(nil)
		}
	})
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.externalPID = proc.Pid
	p.mu.Unlock()
	time.AfterFunc(defaultExternalDelay, func() {
		p.mu.Lock()
		current := p.externalGen == gen && p.hasChild(proc.Pid)
		p.mu.Unlock()
		if !current {
			return
		}
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		media := models.NewActiveMedia(BehaviourSystem, "PC", path, name, id)
		p.journal.Record(&Event{Kind: EventExternal, Launcher: id, Path: path, PID: proc.Pid, Detail: "published"})
		p.publish(media)
	})
	return nil
}

func (p *Platform) killExternal(id string) error {
	if p.faults.For(id).FailKill {
		p.journal.Record(&Event{Kind: EventFault, Launcher: id, Detail: "failKill"})
		return errInjectedKill
	}
	p.mu.Lock()
	pid := p.externalPID
	p.externalPID = 0
	p.externalGen++
	p.mu.Unlock()
	p.journal.Record(&Event{Kind: EventKill, Launcher: id, PID: pid})
	if pid > 0 {
		p.markStopped(pid)
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}
	return nil
}

func (p *Platform) activeMediaIs(id, path string) bool {
	p.mu.Lock()
	get := p.activeMedia
	p.mu.Unlock()
	if get == nil {
		return false
	}
	m := get()
	return m != nil && m.LauncherID == id && m.Path == path
}

func (p *Platform) publish(m *models.ActiveMedia) {
	p.mu.Lock()
	set := p.setActiveMedia
	p.mu.Unlock()
	if set == nil {
		log.Warn().Msg("sim: no active media callback yet")
		return
	}
	set(m)
}

func virtualIndex(path, prefix string) int {
	rest, ok := strings.CutPrefix(path, prefix)
	if !ok {
		return 0
	}
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest = rest[:i]
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 1 || n > virtualEntries {
		return 0
	}
	return n
}

func mediaPath(m *models.ActiveMedia) string {
	if m == nil {
		return ""
	}
	return m.Path
}
