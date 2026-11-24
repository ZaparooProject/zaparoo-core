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

// Package sim is a desktop simulation platform for developing and testing
// Core. It runs the generic Linux process handling for real, but its
// launchers start invisible sleep children instead of emulators, its readers
// are driven over a control socket, and every directory lives under one root.
// It never discovers host emulators, reads host frontend config or shows
// anything on screen. See docs/sim.md.
package sim

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"sync"
	"syscall"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/hoststatus"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/power"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/linuxbase"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/file"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/mqtt"
	simreader "github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/sim"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/simpleserial"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/idle"
)

// Hostname is what the sim reports as the device's name.
const Hostname = "zaparoo-sim"

// RealDrivers lists the real software reader drivers the sim offers besides
// its own. They load only with an explicit [[readers.connect]] entry and
// never auto-detect.
func RealDrivers() []string {
	return []string{"file", "simpleserial", "mqtt"}
}

// Options configures a sim platform.
type Options struct {
	// Mounts adds handlers to the control socket, keyed by path prefix such
	// as "/fakes/romm/". The prefix is stripped before the handler runs.
	Mounts map[string]http.Handler
	// Hub holds the sim readers. Nil makes a new one.
	Hub *simreader.Hub
	// OnStop runs when a client asks the sim to shut down.
	OnStop func()
	// Layout is where everything lives. Required.
	Layout Layout
	// Socket serves the control socket from StartPost.
	Socket bool
}

type child struct {
	proc     *os.Process
	stopping chan struct{}
	launcher string
	once     sync.Once
}

func (c *child) markStopping() {
	c.once.Do(func() { close(c.stopping) })
}

// Platform is the sim platform.
type Platform struct {
	hub            *simreader.Hub
	activeMedia    func() *models.ActiveMedia
	setActiveMedia func(*models.ActiveMedia)
	cfg            *config.Instance
	journal        *Journal
	faults         *Faults
	control        *controlServer
	children       map[int]*child
	done           chan struct{}
	*linuxbase.Base
	layout Layout
	// lastLaunch is the sim launcher of the launch in progress, so a failed
	// launch is journaled with the launcher Core chose.
	lastLaunch  string
	sleepPath   string
	opts        Options
	builtin     []platforms.Launcher
	externalGen uint64
	externalPID int
	trackedPID  int
	stopOnce    sync.Once
	mu          syncutil.Mutex
	childMu     syncutil.Mutex
}

// New returns a sim platform. The layout's root must pass CheckRoot. New
// keeps its own copy of opts.
func New(opts *Options) (*Platform, error) {
	if opts.Layout.Root == "" {
		return nil, errors.New("sim: layout is required")
	}
	if err := opts.Layout.CheckRoot(); err != nil {
		return nil, err
	}
	if err := opts.Layout.Ensure(); err != nil {
		return nil, err
	}
	sleepPath, err := exec.LookPath("sleep")
	if err != nil {
		return nil, fmt.Errorf("sim: sleep binary not found: %w", err)
	}
	journal, err := NewJournal(opts.Layout.Journal)
	if err != nil {
		return nil, fmt.Errorf("sim: open journal: %w", err)
	}
	hub := opts.Hub
	if hub == nil {
		hub = simreader.NewHub()
	}
	p := &Platform{
		Base:      linuxbase.NewBase(platformids.Sim),
		opts:      *opts,
		layout:    opts.Layout,
		journal:   journal,
		faults:    NewFaults(),
		hub:       hub,
		children:  map[int]*child{},
		done:      make(chan struct{}),
		sleepPath: sleepPath,
	}
	p.builtin = p.builtinLaunchers()
	return p, nil
}

// Journal returns the platform's event journal.
func (p *Platform) Journal() *Journal { return p.journal }

// Faults returns the platform's fault set.
func (p *Platform) Faults() *Faults { return p.faults }

// Hub returns the platform's sim readers.
func (p *Platform) Hub() *simreader.Hub { return p.hub }

// Layout returns the platform's directories.
func (p *Platform) Layout() Layout { return p.layout }

// Settings returns the sim's directories. Paths are host managed so Core
// never probes for a portable directory, and self-update is off.
func (p *Platform) Settings() platforms.Settings {
	return platforms.Settings{
		DataDir:           p.layout.Data,
		ConfigDir:         p.layout.Config,
		TempDir:           p.layout.Temp,
		LogDir:            p.layout.Logs,
		HostManagedPaths:  true,
		DisableSelfUpdate: true,
		// The desktop's adapter may be driving the user's controllers.
		DisableBluetooth: true,
	}
}

// SupportedReaders returns the sim reader and any real software driver that
// has an explicit connection configured.
func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	connected := map[string]bool{}
	for _, c := range cfg.Readers().Connect {
		if c.IsEnabled() {
			connected[readers.NormalizeDriverID(c.Driver)] = true
		}
	}
	all := []readers.Reader{simreader.NewReaderWithHub(cfg, p.hub)}
	if connected["file"] {
		all = append(all, file.NewReader(cfg))
	}
	if connected["simpleserial"] {
		all = append(all, simpleserial.NewReader(cfg))
	}
	if connected["mqtt"] {
		all = append(all, mqtt.NewReader(cfg))
	}
	var enabled []readers.Reader
	for _, r := range all {
		md := r.Metadata()
		if cfg.IsReaderEnabled(config.DriverInfo{
			ID: md.ID, DefaultEnabled: md.DefaultEnabled, DefaultAutoDetect: md.DefaultAutoDetect,
		}, config.ReaderEnableContextCandidate) {
			enabled = append(enabled, r)
		}
	}
	return enabled
}

// StartPost keeps Core's active media callbacks and starts the control socket.
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
		return err //nolint:wrapcheck // pass-through
	}
	p.mu.Lock()
	p.cfg = cfg
	p.activeMedia = activeMedia
	p.setActiveMedia = setActiveMedia
	p.mu.Unlock()
	if !p.opts.Socket {
		return nil
	}
	srv, err := startControl(p, p.layout.Socket)
	if err != nil {
		return err
	}
	p.mu.Lock()
	p.control = srv
	p.mu.Unlock()
	return nil
}

// Stop ends the control socket, kills every child the sim started and
// closes the journal.
func (p *Platform) Stop() error {
	p.stopOnce.Do(func() {
		close(p.done)
		p.mu.Lock()
		srv := p.control
		p.control = nil
		p.mu.Unlock()
		if srv != nil {
			srv.close()
		}
		p.childMu.Lock()
		for pid, c := range p.children {
			c.markStopping()
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
		p.childMu.Unlock()
		_ = p.journal.Close()
	})
	return p.Base.Stop() //nolint:wrapcheck // pass-through
}

// Launchers returns custom launchers from config plus the sim launchers.
func (p *Platform) Launchers(cfg *config.Instance) []platforms.Launcher {
	custom := helpers.ParseCustomLaunchers(p, cfg.CustomLaunchers())
	builtin := append([]platforms.Launcher(nil), p.builtin...)
	return helpers.CombineParsedLaunchers(custom, builtin)
}

// LaunchMedia records the request and launches through linuxbase.
func (p *Platform) LaunchMedia(
	cfg *config.Instance,
	path string,
	launcher *platforms.Launcher,
	db *database.Database,
	opts *platforms.LaunchOptions,
) error {
	e := Event{Kind: EventLaunchRequest, Path: path}
	if launcher != nil {
		e.Launcher, e.System = launcher.ID, launcher.SystemID
	}
	if opts != nil {
		e.Intent = opts.Action
	}
	p.journal.Record(&e)
	p.mu.Lock()
	p.lastLaunch = ""
	p.mu.Unlock()
	err := p.Base.LaunchMedia(cfg, path, launcher, db, opts, p)
	if err != nil {
		e.Kind, e.Error = EventLaunchError, err.Error()
		if e.Launcher == "" {
			p.mu.Lock()
			e.Launcher = p.lastLaunch
			p.mu.Unlock()
		}
		p.journal.Record(&e)
		return err //nolint:wrapcheck // already wrapped by linuxbase
	}
	return nil
}

func stopIntentName(intent platforms.StopIntent) string {
	switch intent {
	case platforms.StopForPreemption:
		return "preemption"
	case platforms.StopForMenu:
		return "menu"
	case platforms.StopForConsoleReset:
		return "console-reset"
	default:
		return fmt.Sprintf("intent-%d", intent)
	}
}

// StopActiveLauncher records the stop, applies stop faults and stops through
// linuxbase.
func (p *Platform) StopActiveLauncher(intent platforms.StopIntent) error {
	launcherID, path := "", ""
	p.mu.Lock()
	get := p.activeMedia
	p.mu.Unlock()
	if get != nil {
		if m := get(); m != nil {
			launcherID, path = m.LauncherID, m.Path
		}
	}
	e := Event{Kind: EventStop, Launcher: launcherID, Path: path, Intent: stopIntentName(intent)}
	if launcherID != "" && p.faults.TakeStopFailure(launcherID) {
		e.Kind, e.Error = EventStopError, platforms.ErrStopFailed.Error()
		p.journal.Record(&e)
		return fmt.Errorf("sim: injected stop failure: %w", platforms.ErrStopFailed)
	}
	p.mu.Lock()
	tracked := p.trackedPID
	p.trackedPID = 0
	p.mu.Unlock()
	if tracked > 0 {
		p.markStopped(tracked)
		e.PID = tracked
	}
	p.journal.Record(&e)
	return p.Base.StopActiveLauncher(intent) //nolint:wrapcheck // pass-through
}

// SetTrackedProcess records the process and tracks it through linuxbase,
// which kills any process tracked before it.
func (p *Platform) SetTrackedProcess(proc *os.Process) {
	pid := 0
	if proc != nil {
		pid = proc.Pid
	}
	p.mu.Lock()
	prev := p.trackedPID
	p.trackedPID = pid
	p.mu.Unlock()
	if prev > 0 && prev != pid {
		p.markStopped(prev)
	}
	p.journal.Record(&Event{Kind: EventTracked, Launcher: p.childLauncher(pid), PID: pid})
	p.Base.SetTrackedProcess(proc)
}

// ReturnToMenu stops the active media, as on desktop Linux.
func (p *Platform) ReturnToMenu() error {
	p.journal.Record(&Event{Kind: EventMenu})
	return p.StopActiveLauncher(platforms.StopForMenu)
}

// LaunchSystem records the request. The menu system returns to the menu.
func (p *Platform) LaunchSystem(_ *config.Instance, systemID string) error {
	p.journal.Record(&Event{Kind: EventLaunchSystem, System: systemID})
	if systemID == platforms.SystemMenu {
		return p.ReturnToMenu()
	}
	return nil
}

// KeyboardPress records the key.
func (p *Platform) KeyboardPress(key string) error {
	p.journal.Record(&Event{Kind: EventKeyboard, Detail: key})
	return nil
}

// GamepadPress records the button.
func (p *Platform) GamepadPress(button string) error {
	p.journal.Record(&Event{Kind: EventGamepad, Detail: button})
	return nil
}

// Screenshot returns a generated PNG and saves it under the temp dir.
func (p *Platform) Screenshot() (*platforms.ScreenshotResult, error) {
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	for y := range 240 {
		for x := range 320 {
			//nolint:gosec // x < 320 and y < 240, so both fit in a uint8
			img.Set(x, y, color.RGBA{R: uint8(x * 255 / 320), G: uint8(y * 255 / 240), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("sim: encode screenshot: %w", err)
	}
	if err := os.WriteFile(p.layout.Snapshot, buf.Bytes(), 0o600); err != nil {
		return nil, fmt.Errorf("sim: write screenshot: %w", err)
	}
	p.journal.Record(&Event{Kind: EventScreenshot, Path: p.layout.Snapshot})
	return &platforms.ScreenshotResult{Path: p.layout.Snapshot, Data: buf.Bytes()}, nil
}

// WaitForMediaReady is the readiness wait for launchers without their own,
// such as custom launchers. It returns at once unless readyTimeout is set.
func (p *Platform) WaitForMediaReady(ctx context.Context, _ *config.Instance, m *models.ActiveMedia) error {
	id := ""
	if m != nil {
		id = m.LauncherID
	}
	if p.faults.For(id).ReadyTimeout {
		return p.waitReady(ctx, id, m)
	}
	p.journal.Record(&Event{Kind: EventReady, Launcher: id, Path: mediaPath(m), Detail: "platform"})
	return nil
}

// RefreshLauncherDependencies records the refresh. The sim caches nothing.
func (p *Platform) RefreshLauncherDependencies() error {
	p.journal.Record(&Event{Kind: EventRefresh})
	return nil
}

// DeviceSystem reports the sim device.
func (*Platform) DeviceSystem() (hoststatus.System, error) {
	return hoststatus.System{Hostname: Hostname, Model: "Zaparoo Sim"}, nil
}

// DevicePower reports mains power with no batteries.
func (*Platform) DevicePower() (power.Detail, error) {
	return power.Detail{Source: power.SourceExternal}, nil
}

// PowerStatus reports mains power.
func (*Platform) PowerStatus() (power.Status, error) {
	return power.Status{Source: power.SourceExternal}, nil
}

// DeviceNetwork reports one wired link with no internet, so nothing reads
// the host's real network.
func (*Platform) DeviceNetwork() (hoststatus.Network, error) {
	return hoststatus.Network{
		Type:      hoststatus.LinkWired,
		Interface: "sim0",
		Interfaces: []hoststatus.Interface{
			{Name: "sim0", Type: hoststatus.LinkWired, Addresses: []string{"127.0.0.1"}, Up: true},
		},
		Internet:              hoststatus.InternetNone,
		InternetAuthoritative: true,
	}, nil
}

// DeviceBluetooth reports no adapter.
func (*Platform) DeviceBluetooth() (hoststatus.Bluetooth, error) {
	return hoststatus.Bluetooth{Present: false}, nil
}

// DeviceStorage reports the sim root's filesystem.
func (p *Platform) DeviceStorage() ([]hoststatus.Volume, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(p.layout.Root, &st); err != nil {
		return nil, fmt.Errorf("sim: statfs: %w", err)
	}
	bsize := uint64(st.Bsize) //nolint:gosec // block size is positive
	total, free := st.Blocks*bsize, st.Bavail*bsize
	return []hoststatus.Volume{{
		Path:  p.layout.Root,
		Roles: []string{hoststatus.RoleMedia, hoststatus.RoleData},
		Total: total, Free: free, Used: total - st.Bfree*bsize,
	}}, nil
}

// DeviceDisplay reports one external display.
func (*Platform) DeviceDisplay() (hoststatus.Display, error) {
	return hoststatus.Display{ExternalConnected: true, ExternalActive: true}, nil
}

// DeviceControllers reports no controllers.
func (*Platform) DeviceControllers() ([]hoststatus.Controller, error) {
	return []hoststatus.Controller{}, nil
}

// PowerActions offers every power action. None of them touch the host.
func (*Platform) PowerActions(context.Context) map[hoststatus.PowerAction]hoststatus.Availability {
	out := map[hoststatus.PowerAction]hoststatus.Availability{}
	for _, a := range hoststatus.PowerActions {
		out[a] = hoststatus.Supported
	}
	return out
}

// PreparePowerAction returns a commit that only records the action.
func (p *Platform) PreparePowerAction(_ context.Context, action hoststatus.PowerAction) (func() error, error) {
	return func() error {
		p.journal.Record(&Event{Kind: EventPower, Detail: string(action)})
		return nil
	}, nil
}

// addChild registers proc so Stop kills it. It refuses once Stop has run,
// since Stop's sweep has already passed and would never reach it.
func (p *Platform) addChild(launcherID string, proc *os.Process) (*child, bool) {
	c := &child{proc: proc, launcher: launcherID, stopping: make(chan struct{})}
	p.childMu.Lock()
	defer p.childMu.Unlock()
	select {
	case <-p.done:
		return nil, false
	default:
	}
	p.children[proc.Pid] = c
	return c, true
}

func (p *Platform) removeChild(pid int) {
	p.childMu.Lock()
	delete(p.children, pid)
	p.childMu.Unlock()
}

// childLauncher returns the launcher that started pid, if the sim did.
func (p *Platform) childLauncher(pid int) string {
	p.childMu.Lock()
	defer p.childMu.Unlock()
	if c := p.children[pid]; c != nil {
		return c.launcher
	}
	return ""
}

func (p *Platform) hasChild(pid int) bool {
	p.childMu.Lock()
	defer p.childMu.Unlock()
	_, ok := p.children[pid]
	return ok
}

// markStopped notes that Core is stopping pid, so its exit is a stop and not
// a crash or a natural end.
func (p *Platform) markStopped(pid int) {
	p.childMu.Lock()
	c := p.children[pid]
	p.childMu.Unlock()
	if c != nil {
		c.markStopping()
	}
}

// childrenOf returns the live children a launcher started.
func (p *Platform) childrenOf(launcherID string) []int {
	p.childMu.Lock()
	defer p.childMu.Unlock()
	var out []int
	for pid, c := range p.children {
		if c.launcher == launcherID {
			out = append(out, pid)
		}
	}
	sort.Ints(out)
	return out
}

// Children returns every live child PID with the launcher that started it.
func (p *Platform) Children() map[int]string {
	p.childMu.Lock()
	defer p.childMu.Unlock()
	out := make(map[int]string, len(p.children))
	for pid, c := range p.children {
		out[pid] = c.launcher
	}
	return out
}

// ActiveMedia returns Core's active media, or nil before StartPost.
func (p *Platform) ActiveMedia() *models.ActiveMedia {
	p.mu.Lock()
	get := p.activeMedia
	p.mu.Unlock()
	if get == nil {
		return nil
	}
	return get()
}
