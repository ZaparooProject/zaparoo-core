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

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/audio"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/useragent"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/sim"
	simreader "github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/sim"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/testing/fakes/fakeset"
	"github.com/rs/zerolog"
)

// readyTimeout bounds how long a detached serve waits for the instance.
const readyTimeout = 60 * time.Second

type serveOptions struct {
	onlineURL  string
	fakes      []string
	readers    []string
	port       int
	foreground bool
}

func parseServe(args []string) (serveOptions, error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var opts serveOptions
	var fakes, readersFlag string
	fs.IntVar(&opts.port, "port", 0, "API port (default: a free loopback port)")
	fs.StringVar(&opts.onlineURL, "online-url", "", "Zaparoo Online base URL (default: none, or the online fake)")
	fs.StringVar(&fakes, "fakes", "", "comma-separated fakes to start: "+strings.Join(fakeset.Names, ","))
	fs.StringVar(&readersFlag, "readers", "sim0", "comma-separated sim readers to plug in at start")
	fs.BoolVar(&opts.foreground, "foreground", false, "run in this process instead of detaching")
	if err := fs.Parse(args); err != nil {
		return opts, fmt.Errorf("serve: %w", err)
	}
	if fs.NArg() > 0 {
		return opts, fmt.Errorf("serve: unexpected argument %q", fs.Arg(0))
	}
	opts.fakes = splitList(fakes)
	if err := fakeset.Validate(opts.fakes); err != nil {
		return opts, fmt.Errorf("serve: %w", err)
	}
	if contains(opts.fakes, "online") && opts.onlineURL != "" {
		return opts, errors.New("serve: --online-url and --fakes online both set the Online server")
	}
	if opts.port == config.DefaultAPIPort {
		return opts, fmt.Errorf("serve: port %d belongs to a real Core; pick another", config.DefaultAPIPort)
	}
	opts.readers = splitList(readersFlag)
	return opts, nil
}

func cmdServe(ctx context.Context, g globals, args []string, stdout io.Writer) error {
	opts, err := parseServe(args)
	if err != nil {
		return err
	}
	layout, err := sim.NewLayout(g.root)
	if err != nil {
		return fmt.Errorf("sim layout: %w", err)
	}
	if err = layout.CheckRoot(); err != nil {
		return fmt.Errorf("check sim root: %w", err)
	}
	if st, stErr := layout.ReadState(); stErr == nil && processAlive(st.PID) {
		return fmt.Errorf("a sim is already running from %s (pid %d)", layout.Root, st.PID)
	}
	if opts.foreground {
		return serveForeground(ctx, &layout, &opts, stdout)
	}
	return serveDetached(ctx, g, &layout, args, stdout)
}

// serveDetached re-runs serve in the foreground in a new session, logging to
// the root, and returns once the instance reports ready.
func serveDetached(ctx context.Context, g globals, layout *sim.Layout, args []string, stdout io.Writer) error {
	if err := layout.Ensure(); err != nil {
		return fmt.Errorf("create sim root: %w", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find own executable: %w", err)
	}
	logPath := filepath.Join(layout.Logs, "sim-serve.log")
	logFile, err := os.OpenFile(filepath.Clean(logPath), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open serve log: %w", err)
	}
	defer func() { _ = logFile.Close() }()
	_ = os.Remove(layout.State)

	childArgs := append([]string{"--root", layout.Root, "serve", "--foreground"}, args...)
	// Not CommandContext: the detached sim must outlive this process.
	cmd := exec.Command(exe, childArgs...) //nolint:gosec,noctx // Re-runs this binary.
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start sim: %w", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	deadline := time.NewTimer(readyTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err() //nolint:wrapcheck // Caller's own cancellation.
		case err := <-exited:
			if err == nil {
				err = errors.New("exit status 0")
			}
			return fmt.Errorf("sim exited during startup (%w); log: %s\n%s", err, logPath, tail(logPath, 20))
		case <-deadline.C:
			_ = cmd.Process.Kill()
			return fmt.Errorf("sim not ready after %s; log: %s\n%s", readyTimeout, logPath, tail(logPath, 20))
		case <-tick.C:
			st, err := layout.ReadState()
			if err != nil || !st.Ready || st.PID != cmd.Process.Pid {
				continue
			}
			_ = cmd.Process.Release()
			if g.json {
				return writeJSON(stdout, st)
			}
			_, _ = fmt.Fprintf(stdout, "sim running: pid %d, api %s, socket %s\n", st.PID, st.APIURL, layout.Socket)
			for _, name := range sortedKeys(st.Fakes) {
				_, _ = fmt.Fprintf(stdout, "fake %s: %s\n", name, st.Fakes[name])
			}
			return nil
		}
	}
}

func serveForeground(ctx context.Context, layout *sim.Layout, opts *serveOptions, stdout io.Writer) error {
	// Before anything can open an audio device: the sim never makes sound.
	audio.UseSilentOutput()

	fakes, err := fakeset.Start(opts.fakes, fakeset.Options{ROMs: layout.ROMs, StateDir: layout.Root})
	if err != nil {
		return fmt.Errorf("start fakes: %w", err)
	}
	defer func() { _ = fakes.Close() }()

	// Picked after the fakes bind, so the port cannot be one of theirs.
	port := opts.port
	if port == 0 {
		if port, err = freePort(); err != nil {
			return err
		}
	}

	hub := simreader.NewHub()
	for _, name := range opts.readers {
		if _, err = hub.Add(name, simreader.DeviceOptions{}); err != nil {
			return fmt.Errorf("add reader %s: %w", name, err)
		}
	}

	stopRequested := make(chan struct{}, 1)
	pl, err := sim.New(&sim.Options{
		Layout: *layout,
		Hub:    hub,
		Socket: true,
		Mounts: fakes.Mounts,
		OnStop: func() {
			select {
			case stopRequested <- struct{}{}:
			default:
			}
		},
	})
	if err != nil {
		return fmt.Errorf("create sim platform: %w", err)
	}

	if err = helpers.EnsureDirectories(pl); err != nil {
		return fmt.Errorf("create directories: %w", err)
	}
	if err = helpers.InitLogging(pl, []io.Writer{stdout}); err != nil {
		return fmt.Errorf("init logging: %w", err)
	}
	defaults := layout.Defaults(sim.DefaultsOptions{APIPort: port})
	cfg, err := config.NewConfig(helpers.ConfigDir(pl), defaults)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	// Flags win over a config.toml left by an earlier run.
	if err = cfg.SetAPIPort(port); err != nil {
		return fmt.Errorf("set api port: %w", err)
	}
	restoreFakes, err := fakes.Configure(cfg)
	defer restoreFakes()
	if err != nil {
		return fmt.Errorf("configure fakes: %w", err)
	}
	if opts.onlineURL != "" {
		if err = cfg.SetOnlineBaseURL(opts.onlineURL); err != nil {
			return fmt.Errorf("set online url: %w", err)
		}
	}
	// A settings.update or settings.reload reads config.toml back in, so
	// the port and every URL above must be on disk.
	if err = cfg.Save(); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	if os.Getenv("ZAPAROO_TRACE") != "" {
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	}

	svc, err := service.Start(pl, cfg)
	if err != nil {
		return fmt.Errorf("start service: %w", err)
	}

	apiURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	if err := waitHealthy(ctx, apiURL, svc.Done); err != nil {
		_ = svc.Stop()
		return err
	}
	st := &sim.State{
		Layout:  *layout,
		APIPort: port,
		APIURL:  apiURL,
		PID:     os.Getpid(),
		Version: config.AppVersion,
		Fakes:   fakes.URLs,
		Ready:   true,
	}
	if err := layout.WriteState(st); err != nil {
		_ = svc.Stop()
		return fmt.Errorf("publish sim state: %w", err)
	}
	defer func() { _ = os.Remove(layout.State) }()
	_, _ = fmt.Fprintf(stdout, "sim ready: api %s socket %s\n", apiURL, layout.Socket)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigs)
	select {
	case <-sigs:
	case <-stopRequested:
	case <-ctx.Done():
	case <-svc.Done:
		if err := svc.Err(); err != nil {
			return fmt.Errorf("service stopped: %w", err)
		}
		return nil
	}
	if err := svc.Stop(); err != nil {
		return fmt.Errorf("stop service: %w", err)
	}
	return nil
}

// waitHealthy polls the API health endpoint until it answers.
func waitHealthy(ctx context.Context, apiURL string, done <-chan struct{}) error {
	client := &http.Client{Timeout: time.Second, Transport: useragent.Transport(nil)}
	deadline := time.Now().Add(readyTimeout)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL+"/health", http.NoBody)
		if err != nil {
			return fmt.Errorf("health request: %w", err)
		}
		if resp, err := client.Do(req); err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-done:
			return errors.New("service stopped before its API came up")
		case <-ctx.Done():
			return ctx.Err() //nolint:wrapcheck // Caller's own cancellation.
		case <-time.After(100 * time.Millisecond):
		}
	}
	return fmt.Errorf("API at %s not healthy after %s", apiURL, readyTimeout)
}

func freePort() (int, error) {
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("find a free port: %w", err)
	}
	defer func() { _ = l.Close() }()
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return 0, errors.New("find a free port: not a TCP address")
	}
	if addr.Port == config.DefaultAPIPort {
		return freePort()
	}
	return addr.Port, nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return syscall.Kill(pid, 0) == nil
}

func tail(path string, lines int) string {
	data, err := os.ReadFile(path) //nolint:gosec // Our own log file.
	if err != nil {
		return ""
	}
	all := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	return strings.Join(all, "\n")
}
