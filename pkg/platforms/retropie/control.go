//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package retropie

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/retroarch"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	pollInterval = 100 * time.Millisecond

	// requestTimeout bounds how long a launch waits for the autostart loop to
	// take the request, including closing EmulationStation.
	requestTimeout = 20 * time.Second
	// esTermGrace is how long EmulationStation gets to quit cleanly, saving
	// its settings, before it is killed.
	esTermGrace = 15 * time.Second

	quitGrace = 3 * time.Second
	termGrace = 3 * time.Second
	killGrace = 2 * time.Second

	defaultNetworkCmdPort = 55355
	maxRetroArchConfig    = 1 << 20
)

var errLoopNotRunning = errors.New(
	"the Zaparoo autostart loop is not running on the console; " +
		"run \"zaparoo -install\" and reboot, and keep \"Boot to EmulationStation\" enabled",
)

func sendRetroArchQuit(ctx context.Context, addr string) error {
	stop, ok := retroarch.Controls(addr)[platforms.ControlStop]
	if !ok || stop.Func == nil {
		return errors.New("retroarch stop control unavailable")
	}
	if err := stop.Func(ctx, nil, platforms.ControlParams{}); err != nil {
		return fmt.Errorf("send retroarch quit: %w", err)
	}
	return nil
}

func (p *Platform) launcherFunc(
	folder string, kind requestKind,
) func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
	return func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
		if err := p.launch(context.Background(), folder, kind, path); err != nil {
			return nil, err
		}
		return nil, nil //nolint:nilnil // The game runs under the console's autostart loop, not Core.
	}
}

// launch hands a game to the autostart loop on the console. The loop only
// runs requests while EmulationStation is closed, so EmulationStation is
// asked to quit and the loop starts it again when the game ends.
func (p *Platform) launch(ctx context.Context, folder string, kind requestKind, path string) error {
	p.defaults()
	path = filepath.Clean(path)
	if err := p.checkLaunchable(folder, kind, path); err != nil {
		return err
	}
	if _, ok := readLoopPID(p.fs, p.procs, p.stateDir); !ok {
		return errLoopNotRunning
	}
	if err := p.stopRuncommand(platforms.StopForPreemption); err != nil {
		return err
	}
	data, err := encodeRequest(kind, folder, path)
	if err != nil {
		return err
	}

	p.mu.Lock()
	p.awaitingStart = kind == requestSystem
	p.mu.Unlock()

	requestPath := filepath.Join(p.stateDir, requestFile)
	if err := writeRequest(p.fs, p.stateDir, data); err != nil {
		p.clearAwaiting()
		return err
	}
	if err := removeIfExists(p.fs, filepath.Join(p.stateDir, holdFile)); err != nil {
		log.Warn().Err(err).Msg("retropie: failed to clear launch hold")
	}

	if err := p.waitRequestTaken(ctx, requestPath); err != nil {
		p.clearAwaiting()
		if rmErr := removeIfExists(p.fs, requestPath); rmErr != nil {
			log.Warn().Err(rmErr).Msg("retropie: failed to withdraw launch request")
		}
		return err
	}
	log.Info().Str("system", folder).Str("path", path).Msg("retropie: launch request taken")
	return nil
}

func (p *Platform) checkLaunchable(folder string, kind requestKind, path string) error {
	if err := validateROMPath(path); err != nil {
		return err
	}
	info, err := p.fs.Stat(path)
	if err != nil {
		return fmt.Errorf("media not found: %w", err)
	}
	// The autostart loop only runs regular files, so anything else would be
	// taken and dropped while Core waits for a game that never starts.
	if !info.Mode().IsRegular() {
		return fmt.Errorf("media is not a regular file: %s", path)
	}
	switch kind {
	case requestSystem:
		// runcommand exits before its hooks run when a system has no
		// emulators configured, so the launch would vanish silently.
		cfgPath := filepath.Join(p.configsDir, folder, "emulators.cfg")
		if !fileExists(p.fs, cfgPath) {
			return fmt.Errorf("no emulator is installed for RetroPie system %q (%s not found)", folder, cfgPath)
		}
	case requestPort:
		// A port is a shell script run as the console user, so only scripts
		// in RetroPie's own ports folder are run.
		portsDir := filepath.Join(p.romsDir, "ports")
		if filepath.Dir(path) != portsDir {
			return fmt.Errorf("RetroPie ports must be scripts in %s: %s", portsDir, path)
		}
	default:
		return fmt.Errorf("invalid request kind: %q", kind)
	}
	return nil
}

// waitRequestTaken waits for the loop to take the request, closing
// EmulationStation so the loop gets the chance.
func (p *Platform) waitRequestTaken(ctx context.Context, requestPath string) error {
	start := p.clock.Now()
	var termAt time.Time
	killed := false
	for {
		if !fileExists(p.fs, requestPath) {
			return nil
		}
		elapsed := p.clock.Since(start)
		if elapsed >= requestTimeout {
			return fmt.Errorf("the console did not take the launch request within %s", requestTimeout)
		}
		if es := p.procs.findByComm(esComm); len(es) > 0 {
			switch {
			case termAt.IsZero():
				p.signalAll(es, syscall.SIGTERM)
				termAt = p.clock.Now()
			case !killed && p.clock.Since(termAt) >= esTermGrace:
				log.Warn().Ints("pids", es).Msg("retropie: EmulationStation did not quit, killing it")
				p.signalAll(es, syscall.SIGKILL)
				killed = true
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("launch canceled: %w", ctx.Err())
		case <-p.clock.After(pollInterval):
		}
	}
}

func (p *Platform) signalAll(pids []int, sig syscall.Signal) {
	for _, pid := range pids {
		if err := p.procs.signal(pid, sig); err != nil {
			log.Warn().Err(err).Int("pid", pid).Msg("retropie: failed to signal process")
		}
	}
}

func (p *Platform) clearAwaiting() {
	p.mu.Lock()
	p.awaitingStart = false
	p.mu.Unlock()
}

// runcommandAlive reports whether pid is still a runcommand process. The
// cmdline check keeps a pid the kernel has handed to another process from
// being counted as the game or signalled.
func (p *Platform) runcommandAlive(pid int) bool {
	return p.procs.alive(pid) && strings.Contains(p.procs.cmdline(pid), "runcommand")
}

// waitExit polls until pid has exited or timeout passes.
func (p *Platform) waitExit(pid int, timeout time.Duration) bool {
	start := p.clock.Now()
	for {
		if !p.runcommandAlive(pid) {
			return true
		}
		if p.clock.Since(start) >= timeout {
			return false
		}
		<-p.clock.After(pollInterval)
	}
}

// stopRuncommand ends the game runcommand is running. A preemption leaves a
// hold for the autostart loop, so it waits for the next request instead of
// starting EmulationStation between the two games.
func (p *Platform) stopRuncommand(intent platforms.StopIntent) error {
	p.mu.Lock()
	run := p.run
	p.mu.Unlock()
	if run.pid == 0 {
		return nil
	}
	if !p.runcommandAlive(run.pid) {
		p.clearRun(run.pid)
		return nil
	}

	if intent == platforms.StopForPreemption {
		holdPath := filepath.Join(p.stateDir, holdFile)
		if err := writeFileReplacing(p.fs, holdPath, nil, 0o600); err != nil {
			log.Warn().Err(err).Msg("retropie: failed to write launch hold")
		}
	}

	if !p.quitRetroArch(run) {
		if !p.killRuncommandChildren(run.pid) {
			return fmt.Errorf("%w: runcommand pid %d is still running", platforms.ErrStopFailed, run.pid)
		}
	}
	p.clearRun(run.pid)
	return nil
}

// quitRetroArch asks a libretro game to quit through RetroArch's network
// commands, which lets RetroArch write saves on the way out. It reports
// whether runcommand finished.
func (p *Platform) quitRetroArch(run runEvent) bool {
	if !strings.HasPrefix(run.emulator, "lr-") {
		return false
	}
	port, ok := networkCmdPort(p.fs, p.configsDir, run.system)
	if !ok {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	if err := p.sendQuit(ctx, addr); err != nil {
		log.Debug().Err(err).Msg("retropie: retroarch quit failed")
		return false
	}
	return p.waitExit(run.pid, quitGrace)
}

// killRuncommandChildren ends the emulator under runcommand and lets
// runcommand finish, so it runs its end hook and restores the video mode.
// runcommand itself is killed only if it is still there after that.
func (p *Platform) killRuncommandChildren(pid int) bool {
	p.signalAll(p.procs.descendants(pid), syscall.SIGTERM)
	if p.waitExit(pid, termGrace) {
		return true
	}
	log.Warn().Int("pid", pid).Msg("retropie: game did not exit, killing it")
	p.signalAll(append(p.procs.descendants(pid), pid), syscall.SIGKILL)
	return p.waitExit(pid, killGrace)
}

func (p *Platform) clearRun(pid int) {
	p.mu.Lock()
	if p.run.pid == pid {
		p.run = runEvent{}
	}
	p.mu.Unlock()
}

func (p *Platform) waitForGame(ctx context.Context, _ *config.Instance, _ *models.ActiveMedia) error {
	err := platforms.PollReady(ctx, p.clock, readyInterval, func(context.Context) (bool, error) {
		p.mu.Lock()
		defer p.mu.Unlock()
		return !p.awaitingStart && p.run.pid != 0, nil
	})
	if err != nil {
		p.clearAwaiting()
		return fmt.Errorf("retropie game did not start: %w", err)
	}
	return nil
}

func (p *Platform) gameRunning(context.Context, *config.Instance, *models.ActiveMedia) (bool, error) {
	p.mu.Lock()
	pid := p.run.pid
	p.mu.Unlock()
	return pid != 0 && p.runcommandAlive(pid), nil
}

// networkCmdPort reports the RetroArch network command port when the
// interface is enabled for system. RetroPie's per-system config includes the
// shared one, so a per-system setting wins.
func networkCmdPort(fs afero.Fs, configsDir, system string) (int, bool) {
	values := map[string]string{}
	for _, path := range []string{
		filepath.Join(configsDir, "all", "retroarch.cfg"),
		filepath.Join(configsDir, system, "retroarch.cfg"),
	} {
		for k, v := range readRetroArchConfig(fs, path) {
			values[k] = v
		}
	}
	if values["network_cmd_enable"] != "true" {
		return 0, false
	}
	port := defaultNetworkCmdPort
	if raw, ok := values["network_cmd_port"]; ok {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 || n > 65535 {
			return 0, false
		}
		port = n
	}
	return port, true
}

// readRetroArchConfig reads the network command keys from a RetroArch config
// file. Later lines win, as in RetroArch.
func readRetroArchConfig(fs afero.Fs, path string) map[string]string {
	data, err := afero.ReadFile(fs, path)
	if err != nil || len(data) > maxRetroArchConfig {
		return nil
	}
	values := map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key != "network_cmd_enable" && key != "network_cmd_port" {
			continue
		}
		values[key] = strings.Trim(strings.TrimSpace(value), `"`)
	}
	return values
}
