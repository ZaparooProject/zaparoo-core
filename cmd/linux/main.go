//go:build linux

/*
Zaparoo Core
Copyright (C) 2023 Gareth Jones
Copyright (C) 2023-2025 Callan Barrett

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

package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/linux"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/linux/installer"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/tui"
	"github.com/rs/zerolog/log"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Parse flags early for install/uninstall operations
	install := flag.String(
		"install",
		"",
		"install component: application, desktop, service, hardware",
	)
	uninstall := flag.String(
		"uninstall",
		"",
		"uninstall component: application, desktop, service, hardware",
	)

	pl := &linux.Platform{}
	flags := cli.SetupFlags()

	daemonMode := flag.Bool(
		"daemon",
		false,
		"run service in foreground with no UI",
	)
	start := flag.Bool(
		"start",
		false,
		"start service and open web UI in browser",
	)

	flags.Pre(pl)

	// Handle install operations
	if *install != "" {
		switch *install {
		case "application":
			if err := installer.InstallApplication(); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
				return fmt.Errorf("application installation failed: %w", err)
			}
			_, _ = fmt.Println("Application installation complete")
		case "desktop":
			if err := installer.InstallDesktop(); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
				return fmt.Errorf("desktop installation failed: %w", err)
			}
			_, _ = fmt.Println("Desktop installation complete")
		case "service":
			if err := installer.InstallService(); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
				return fmt.Errorf("service installation failed: %w", err)
			}
			_, _ = fmt.Println("Service installation complete")
		case "hardware":
			if err := installer.InstallHardware(); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
				return fmt.Errorf("hardware installation failed: %w", err)
			}
			_, _ = fmt.Println("Hardware installation complete")
		default:
			return fmt.Errorf("unknown component: %s (valid: application, desktop, service, hardware)", *install)
		}
		return nil
	}

	// Handle uninstall operations
	if *uninstall != "" {
		switch *uninstall {
		case "application":
			if err := installer.UninstallApplication(); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
				return fmt.Errorf("application uninstallation failed: %w", err)
			}
			_, _ = fmt.Println("Application uninstallation complete")
		case "desktop":
			if err := installer.UninstallDesktop(); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
				return fmt.Errorf("desktop uninstallation failed: %w", err)
			}
			_, _ = fmt.Println("Desktop uninstallation complete")
		case "service":
			if err := installer.UninstallService(); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
				return fmt.Errorf("service uninstallation failed: %w", err)
			}
			_, _ = fmt.Println("Service uninstallation complete")
		case "hardware":
			if err := installer.UninstallHardware(); err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
				return fmt.Errorf("hardware uninstallation failed: %w", err)
			}
			_, _ = fmt.Println("Hardware uninstallation complete")
		default:
			return fmt.Errorf("unknown component: %s (valid: application, desktop, service, hardware)", *uninstall)
		}
		return nil
	}

	// Normal root check for regular operations
	if os.Geteuid() == 0 {
		return errors.New("zaparoo cannot be run as root")
	}

	var logWriters []io.Writer
	if *daemonMode {
		logWriters = []io.Writer{os.Stderr}
	}

	cfg := cli.Setup(
		pl,
		config.BaseDefaults,
		logWriters,
	)

	// Handle start mode (for desktop entry)
	if *start {
		return startAndOpenBrowser(cfg)
	}

	defer func() {
		if err := recover(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Panic: %s\n", err)
			log.Fatal().Msgf("panic: %v", err)
		}
	}()

	flags.Post(cfg, pl)

	var stopSvc func() error
	if !helpers.IsServiceRunning(cfg) {
		log.Info().Msg("starting new service instance")
		var err error
		stopSvc, err = service.Start(pl, cfg)
		if err != nil {
			log.Error().Msgf("error starting service: %s", err)
			return fmt.Errorf("error starting service: %w", err)
		}

		defer func() {
			err := stopSvc()
			if err != nil {
				log.Error().Msgf("error stopping service: %s", err)
			}
		}()
	} else {
		log.Info().
			Int("port", cfg.APIPort()).
			Msg("connecting to existing service instance")
	}

	sigs := make(chan os.Signal, 1)
	defer close(sigs)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	exit := make(chan bool, 1)
	defer close(exit)

	// Handle application modes
	switch {
	case *daemonMode:
		log.Info().Msg("started in daemon mode")
	default:
		// default to showing the TUI
		app, err := tui.BuildMain(
			cfg, pl,
			func() bool { return helpers.IsServiceRunning(cfg) },
			filepath.Join(os.Getenv("HOME"), "Desktop", "core.log"),
			"desktop",
		)
		if err != nil {
			log.Error().Err(err).Msgf("error building UI")
			return fmt.Errorf("error building UI: %w", err)
		}

		err = app.Run()
		if err != nil {
			log.Error().Err(err).Msg("error running UI")
			return fmt.Errorf("error running UI: %w", err)
		}

		exit <- true
	}

	select {
	case <-sigs:
	case <-exit:
	}

	return nil
}

func startAndOpenBrowser(cfg *config.Instance) error {
	// Get actual API port from config
	port := cfg.APIPort()
	webURL := fmt.Sprintf("http://localhost:%d/app/", port)

	// Check if API is already responding
	apiURL := fmt.Sprintf("http://localhost:%d/api/v0.1/ping", port)
	if isAPIRespondingAt(apiURL) {
		// Service already running - just open browser
		_, _ = fmt.Fprintln(os.Stderr, "Service is already running")
		return openBrowser(webURL)
	}

	// All status messages go to stderr
	_, _ = fmt.Fprintln(os.Stderr, "Service not running, attempting to start...")

	// Check if systemd service is installed
	ctx := context.Background()
	checkCmd := exec.CommandContext(ctx, "systemctl", "--user", "status", "zaparoo")
	if err := checkCmd.Run(); err != nil {
		// Service not installed, install it
		_, _ = fmt.Fprintln(os.Stderr, "Service not installed, installing...")
		if err := installer.InstallService(); err != nil {
			return fmt.Errorf("failed to install service: %w", err)
		}
		_, _ = fmt.Fprintln(os.Stderr, "Service installed successfully")
	}

	// Start the service
	_, _ = fmt.Fprintln(os.Stderr, "Starting service...")
	startCmd := exec.CommandContext(ctx, "systemctl", "--user", "start", "zaparoo")
	if err := startCmd.Run(); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}

	// Wait for API to respond (with retries)
	_, _ = fmt.Fprintln(os.Stderr, "Waiting for service to be ready...")
	for range 30 {
		if isAPIRespondingAt(apiURL) {
			_, _ = fmt.Fprintln(os.Stderr, "Service is ready")
			return openBrowser(webURL)
		}
		time.Sleep(time.Second)
	}

	return errors.New("service failed to start within 30 seconds")
}

func openBrowser(url string) error {
	_, _ = fmt.Fprintf(os.Stderr, "Opening %s in browser...\n", url)

	cmd := exec.CommandContext(context.Background(), "xdg-open", url)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to open browser: %w", err)
	}
	return nil
}

func isAPIRespondingAt(url string) bool {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, http.NoBody)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode == http.StatusOK
}
