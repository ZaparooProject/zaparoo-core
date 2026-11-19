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

package lakka

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/jonboulle/clockwork"
	"github.com/rs/zerolog/log"
)

const (
	retroArchService = "retroarch.service"
	// transientUnit is the systemd unit a game runs in when RetroArch cannot
	// load content over its network command interface.
	transientUnit = "zaparoo-retroarch.service"

	commandGetStatus    = "GET_STATUS"
	commandLoadContent  = "LOAD_CONTENT"
	commandCloseContent = "CLOSE_CONTENT"

	statusPlayingPrefix = "GET_STATUS PLAYING "
	statusPausedPrefix  = "GET_STATUS PAUSED "

	// maxCommandLen is the largest datagram RetroArch reads in one go; a
	// longer one is cut short and would load the wrong path.
	maxCommandLen = 2047

	pollInterval = 200 * time.Millisecond
	// loadIgnoredWindow is how long RetroArch may keep answering with its old
	// status before LOAD_CONTENT is taken as unknown to it. A build that knows
	// the command stops answering while it loads.
	loadIgnoredWindow = 3 * time.Second
	// loadProbeTimeout bounds the first LOAD_CONTENT, which is also the
	// capability probe.
	loadProbeTimeout = 30 * time.Second
	// closeRetryAfter is when an unanswered CLOSE_CONTENT is pressed again.
	// RetroArch 1.22.2 treats it as a hotkey, which Lakka's quit_press_twice
	// default makes a double press.
	closeRetryAfter = time.Second
	closeTimeout    = 4 * time.Second
)

// transientEnvironmentFiles are retroarch.service's EnvironmentFile lines, so a
// game started outside the service sees the same environment.
//
//nolint:gochecknoglobals // Static unit data.
var transientEnvironmentFiles = []string{
	"/usr/lib/retroarch/retroarch-env.conf",
	"-/run/libreelec/retroarch.conf",
	"-/storage/.cache/services/retroarch.conf",
}

type loadCapability int

const (
	loadUnknown loadCapability = iota
	loadSupported
	loadUnsupported
)

// frontend drives Lakka's RetroArch, which is both the menu and the emulator.
type frontend struct {
	exec         command.Executor
	clock        clockwork.Clock
	addr         string
	launchedPath string
	queryTimeout time.Duration
	mu           syncutil.Mutex
	capability   loadCapability
}

func newFrontend(exec command.Executor, clock clockwork.Clock, addr string) *frontend {
	return &frontend{
		exec:         exec,
		clock:        clock,
		addr:         addr,
		queryTimeout: 500 * time.Millisecond,
	}
}

// launch starts content in RetroArch. argv is a full "retroarch -L core path"
// invocation; the core and content are taken from it for LOAD_CONTENT.
func (f *frontend) launch(ctx context.Context, argv []string, contentPath string) error {
	corePath, err := corePathFromArgv(argv)
	if err != nil {
		return err
	}
	if strings.ContainsAny(contentPath, "\x00\r\n") {
		return fmt.Errorf("content path contains a control character: %q", contentPath)
	}

	loaded, err := f.loadOverNetwork(ctx, corePath, contentPath)
	if err != nil {
		return err
	}
	if !loaded {
		if err := f.launchTransient(ctx, argv); err != nil {
			return err
		}
	}

	f.mu.Lock()
	f.launchedPath = contentPath
	f.mu.Unlock()
	return nil
}

func corePathFromArgv(argv []string) (string, error) {
	for i := range argv {
		if argv[i] == "-L" && i+2 < len(argv) {
			return argv[i+1], nil
		}
	}
	return "", fmt.Errorf("retroarch command has no core: %q", argv)
}

// loadOverNetwork asks the running RetroArch to load content. It reports false
// when the caller must fall back to a separate RetroArch instance.
func (f *frontend) loadOverNetwork(ctx context.Context, corePath, contentPath string) (bool, error) {
	f.mu.Lock()
	capability := f.capability
	f.mu.Unlock()
	if capability == loadUnsupported {
		return false, nil
	}

	message := commandLoadContent + " " + corePath + "|" + contentPath
	if len(message) > maxCommandLen {
		log.Warn().Int("length", len(message)).Msg("content path too long for a RetroArch network command")
		return false, nil
	}

	before, err := f.status(ctx)
	if err != nil {
		log.Debug().Err(err).Msg("retroarch network commands unreachable, starting a separate instance")
		return false, nil
	}
	if err := f.send(ctx, message); err != nil {
		return false, err
	}
	if capability == loadSupported {
		return true, nil
	}
	return f.probeLoad(ctx, before, contentPath)
}

// probeLoad decides whether RetroArch knew LOAD_CONTENT. A build without it
// logs the line and keeps answering GET_STATUS as before. A build with it
// blocks its main loop while loading, so it stops answering for a while, and
// then either reports the new content or, when loading failed, the old status.
func (f *frontend) probeLoad(ctx context.Context, before, contentPath string) (bool, error) {
	start := f.clock.Now()
	wentSilent := false
	for {
		status, err := f.status(ctx)
		switch {
		case err != nil:
			wentSilent = true
		case isLoaded(status) &&
			(statusNamesContent(status, contentPath) || loadedContent(status) != loadedContent(before)):
			f.setCapability(loadSupported)
			return true, nil
		case wentSilent:
			f.setCapability(loadSupported)
			return false, fmt.Errorf("retroarch could not load %s", contentPath)
		case f.clock.Since(start) >= loadIgnoredWindow:
			log.Info().Msg("retroarch does not support LOAD_CONTENT, launching games in a separate instance")
			f.setCapability(loadUnsupported)
			return false, nil
		}

		if f.clock.Since(start) >= loadProbeTimeout {
			// Still busy: a long load, not a build that ignored the command.
			f.setCapability(loadSupported)
			return true, nil
		}
		select {
		case <-ctx.Done():
			return false, fmt.Errorf("waiting for retroarch to load content: %w", ctx.Err())
		case <-f.clock.After(pollInterval):
		}
	}
}

func (f *frontend) setCapability(c loadCapability) {
	f.mu.Lock()
	f.capability = c
	f.mu.Unlock()
}

// launchTransient runs the game in its own RetroArch, outside Core's process
// tree so it survives a Core restart. retroarch.service is stopped for it and
// started again by the unit's ExecStopPost however the game ends.
func (f *frontend) launchTransient(ctx context.Context, argv []string) error {
	if f.transientActive(ctx) {
		if err := f.systemctl(ctx, "stop", transientUnit); err != nil {
			return fmt.Errorf("stop previous game: %w", err)
		}
	}
	if err := f.systemctl(ctx, "stop", retroArchService); err != nil {
		f.restoreFrontend(ctx)
		return fmt.Errorf("stop retroarch service: %w", err)
	}

	args := make([]string, 0, 16+len(argv))
	args = append(args,
		"--unit="+transientUnit,
		"--collect",
		"--quiet",
		"--expand-environment=no",
		"--property=Conflicts="+retroArchService,
		"--property=After="+retroArchService,
		"--property=TimeoutStopSec=10",
		"--property=ExecStopPost=/usr/bin/systemctl --no-block start "+retroArchService,
	)
	for _, file := range transientEnvironmentFiles {
		args = append(args, "--property=EnvironmentFile="+file)
	}
	args = append(args, "--")
	args = append(args, argv...)

	if err := f.exec.Run(ctx, "systemd-run", args...); err != nil {
		f.restoreFrontend(ctx)
		return fmt.Errorf("start retroarch game unit: %w", err)
	}
	return nil
}

// stop ends running content and leaves the RetroArch menu on screen.
//
// Core keeps ownership of what it launched until the stop is confirmed, so a
// stop that fails leaves the tracker watching the game it could not end.
func (f *frontend) stop(ctx context.Context) error {
	launched := f.currentLaunch()

	if f.transientActive(ctx) {
		if err := f.systemctl(ctx, "stop", transientUnit); err != nil {
			return fmt.Errorf("%w: stop retroarch game unit: %w", platforms.ErrStopFailed, err)
		}
		f.clearLaunch(launched)
		return nil
	}

	start := f.clock.Now()
	status, err := f.status(ctx)
	// RetroArch does not answer while it loads content, so silence after a
	// launch of Core's may be that launch still loading. Wait for it rather
	// than let the next launch start over a game about to appear.
	for err != nil && launched != "" && f.unitActive(ctx, retroArchService) {
		if f.clock.Since(start) >= closeTimeout {
			return fmt.Errorf("%w: retroarch is not answering: %w", platforms.ErrStopFailed, err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: %w", platforms.ErrStopFailed, ctx.Err())
		case <-f.clock.After(pollInterval):
		}
		status, err = f.status(ctx)
	}
	if err != nil || !isLoaded(status) {
		f.clearLaunch(launched)
		return nil //nolint:nilerr // Nothing reachable is running content.
	}

	start = f.clock.Now()
	pressedAgain := false
	if err := f.send(ctx, commandCloseContent); err != nil {
		return fmt.Errorf("%w: %w", platforms.ErrStopFailed, err)
	}
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: %w", platforms.ErrStopFailed, ctx.Err())
		case <-f.clock.After(pollInterval):
		}
		status, err := f.status(ctx)
		if err == nil && !isLoaded(status) {
			f.clearLaunch(launched)
			return nil
		}
		elapsed := f.clock.Since(start)
		if elapsed >= closeTimeout {
			return fmt.Errorf("%w: retroarch still has content loaded", platforms.ErrStopFailed)
		}
		if !pressedAgain && err == nil && elapsed >= closeRetryAfter {
			pressedAgain = true
			if err := f.send(ctx, commandCloseContent); err != nil {
				return fmt.Errorf("%w: %w", platforms.ErrStopFailed, err)
			}
		}
	}
}

// gameRunning reports whether RetroArch has content loaded.
func (f *frontend) gameRunning(ctx context.Context) (bool, error) {
	status, err := f.status(ctx)
	if err != nil {
		if f.transientActive(ctx) {
			return true, nil
		}
		return false, err
	}
	return isLoaded(status), nil
}

// ensureFrontend starts retroarch.service when neither it nor a game unit is
// running, such as after Core was stopped between ending a game and starting
// the menu again.
func (f *frontend) ensureFrontend(ctx context.Context) {
	if f.transientActive(ctx) || f.unitActive(ctx, retroArchService) {
		return
	}
	log.Info().Msg("retroarch is not running, starting it")
	f.restoreFrontend(ctx)
}

func (f *frontend) restoreFrontend(ctx context.Context) {
	if err := f.systemctl(ctx, "--no-block", "start", retroArchService); err != nil {
		log.Error().Err(err).Msg("failed to start retroarch service")
	}
}

func (f *frontend) transientActive(ctx context.Context) bool {
	return f.unitActive(ctx, transientUnit)
}

func (f *frontend) unitActive(ctx context.Context, unit string) bool {
	return f.exec.Run(ctx, "systemctl", "is-active", "--quiet", unit) == nil
}

func (f *frontend) systemctl(ctx context.Context, args ...string) error {
	if err := f.exec.Run(ctx, "systemctl", args...); err != nil {
		return fmt.Errorf("systemctl %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func (f *frontend) currentLaunch() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.launchedPath
}

func (f *frontend) clearLaunch(path string) {
	f.mu.Lock()
	if f.launchedPath == path {
		f.launchedPath = ""
	}
	f.mu.Unlock()
}

// status asks RetroArch for GET_STATUS. RetroArch only answers while its
// network command interface is enabled and its main loop is running.
func (f *frontend) status(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, f.queryTimeout)
	defer cancel()

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "udp", f.addr)
	if err != nil {
		return "", fmt.Errorf("dial retroarch network command interface: %w", err)
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err = conn.SetDeadline(deadline); err != nil {
		return "", fmt.Errorf("set retroarch status deadline: %w", err)
	}
	if _, err = conn.Write([]byte(commandGetStatus)); err != nil {
		return "", fmt.Errorf("query retroarch network command interface: %w", err)
	}
	var buf [4096]byte
	n, err := conn.Read(buf[:])
	if err != nil {
		return "", fmt.Errorf("retroarch network command interface unavailable: %w", err)
	}
	return strings.TrimSpace(string(buf[:n])), nil
}

func (f *frontend) send(ctx context.Context, message string) error {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "udp", f.addr)
	if err != nil {
		return fmt.Errorf("dial retroarch network command interface: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte(message)); err != nil {
		name, _, _ := strings.Cut(message, " ")
		return fmt.Errorf("send retroarch command %s: %w", name, err)
	}
	return nil
}

func isLoaded(status string) bool {
	return strings.HasPrefix(status, statusPlayingPrefix) || strings.HasPrefix(status, statusPausedPrefix)
}

// loadedContent returns what a GET_STATUS reply says is loaded, without the
// playing or paused state, so pausing or resuming does not look like a load.
func loadedContent(status string) string {
	for _, prefix := range []string{statusPlayingPrefix, statusPausedPrefix} {
		if rest, ok := strings.CutPrefix(status, prefix); ok {
			return rest
		}
	}
	return ""
}

// statusNamesContent reports whether a GET_STATUS reply, shaped
// "GET_STATUS PLAYING <system>,<content name>,crc32=<hex>", names the file at
// path. RetroArch reports the file name without its extension.
func statusNamesContent(status, path string) bool {
	if path == "" {
		return false
	}
	// RetroArch does not escape commas in the content name, so the name is
	// everything between the system and the trailing crc32 field.
	_, content, ok := strings.Cut(status, ",")
	if !ok {
		return false
	}
	if i := strings.LastIndex(content, ",crc32="); i >= 0 {
		content = content[:i]
	}
	name := filepath.Base(path)
	name = strings.TrimSuffix(name, filepath.Ext(name))
	return name != "" && strings.EqualFold(strings.TrimSpace(content), name)
}
