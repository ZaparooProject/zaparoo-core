//go:build linux

/*
Zaparoo Core
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

package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/linux/installer"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/tui"
	"github.com/rs/zerolog/log"
)

// Installer defines the interface for install/uninstall operations.
// This allows mocking in tests to avoid side effects.
type Installer interface {
	InstallApplication() error
	InstallDesktop() error
	InstallService() error
	InstallHardware() error
	UninstallApplication() error
	UninstallDesktop() error
	UninstallService() error
	UninstallHardware() error
}

// DefaultInstaller implements Installer using the real installer package.
type DefaultInstaller struct{}

func (DefaultInstaller) InstallApplication() error {
	//nolint:wrapcheck // Thin wrapper, error context added by caller
	return installer.InstallApplication()
}

func (DefaultInstaller) InstallDesktop() error {
	//nolint:wrapcheck // Thin wrapper, error context added by caller
	return installer.InstallDesktop()
}

func (DefaultInstaller) InstallService() error {
	//nolint:wrapcheck // Thin wrapper, error context added by caller
	return installer.InstallService()
}

func (DefaultInstaller) InstallHardware() error {
	//nolint:wrapcheck // Thin wrapper, error context added by caller
	return installer.InstallHardware()
}

func (DefaultInstaller) UninstallApplication() error {
	//nolint:wrapcheck // Thin wrapper, error context added by caller
	return installer.UninstallApplication()
}

func (DefaultInstaller) UninstallDesktop() error {
	//nolint:wrapcheck // Thin wrapper, error context added by caller
	return installer.UninstallDesktop()
}

func (DefaultInstaller) UninstallService() error {
	//nolint:wrapcheck // Thin wrapper, error context added by caller
	return installer.UninstallService()
}

func (DefaultInstaller) UninstallHardware() error {
	//nolint:wrapcheck // Thin wrapper, error context added by caller
	return installer.UninstallHardware()
}

// defaultInstaller is the package-level installer used by HandleInstall/HandleUninstall.
// It can be replaced in tests to avoid side effects.
var defaultInstaller Installer = DefaultInstaller{}

// HandleInstall handles the -install flag for all Linux platforms.
func HandleInstall(component string) error {
	switch component {
	case "application":
		if err := defaultInstaller.InstallApplication(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			return fmt.Errorf("application installation failed: %w", err)
		}
		_, _ = fmt.Println("Application installation complete")
	case "desktop":
		if err := defaultInstaller.InstallDesktop(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			return fmt.Errorf("desktop installation failed: %w", err)
		}
		_, _ = fmt.Println("Desktop installation complete")
	case "service":
		if err := defaultInstaller.InstallService(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			return fmt.Errorf("service installation failed: %w", err)
		}
		_, _ = fmt.Println("Service installation complete")
	case "hardware":
		if err := defaultInstaller.InstallHardware(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			return fmt.Errorf("hardware installation failed: %w", err)
		}
		_, _ = fmt.Println("Hardware installation complete")
	default:
		return fmt.Errorf("unknown component: %s (valid: application, desktop, service, hardware)", component)
	}
	return nil
}

// HandleUninstall handles the -uninstall flag for all Linux platforms.
func HandleUninstall(component string) error {
	switch component {
	case "application":
		if err := defaultInstaller.UninstallApplication(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			return fmt.Errorf("application uninstallation failed: %w", err)
		}
		_, _ = fmt.Println("Application uninstallation complete")
	case "desktop":
		if err := defaultInstaller.UninstallDesktop(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			return fmt.Errorf("desktop uninstallation failed: %w", err)
		}
		_, _ = fmt.Println("Desktop uninstallation complete")
	case "service":
		if err := defaultInstaller.UninstallService(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			return fmt.Errorf("service uninstallation failed: %w", err)
		}
		_, _ = fmt.Println("Service uninstallation complete")
	case "hardware":
		if err := defaultInstaller.UninstallHardware(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
			return fmt.Errorf("hardware uninstallation failed: %w", err)
		}
		_, _ = fmt.Println("Hardware uninstallation complete")
	default:
		return fmt.Errorf("unknown component: %s (valid: application, desktop, service, hardware)", component)
	}
	return nil
}

// StartAndOpenBrowser starts the service via systemd and opens the web UI in the browser.
func StartAndOpenBrowser(cfg *config.Instance) error {
	port := cfg.APIPort()
	webURL := fmt.Sprintf("http://localhost:%d/app/", port)

	// Check if API is already responding
	if helpers.IsServiceRunning(cfg) {
		_, _ = fmt.Fprintln(os.Stderr, "Service is already running")
		_, _ = fmt.Fprintf(os.Stderr, "Opening %s in browser...\n", webURL)
		if err := helpers.OpenBrowser(webURL); err != nil {
			return fmt.Errorf("failed to open browser: %w", err)
		}
		return nil
	}

	_, _ = fmt.Fprintln(os.Stderr, "Service not running, attempting to start...")

	// Check if systemd service is installed
	ctx := context.Background()
	checkCmd := exec.CommandContext(ctx, "systemctl", "--user", "status", "zaparoo")
	if err := checkCmd.Run(); err != nil {
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

	// Wait for API to respond (with retries and context cancellation)
	_, _ = fmt.Fprintln(os.Stderr, "Waiting for service to be ready...")
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-waitCtx.Done():
			return errors.New("service failed to start within 30 seconds")
		case <-ticker.C:
			if helpers.IsServiceRunning(cfg) {
				_, _ = fmt.Fprintln(os.Stderr, "Service is ready")
				_, _ = fmt.Fprintf(os.Stderr, "Opening %s in browser...\n", webURL)
				if err := helpers.OpenBrowser(webURL); err != nil {
					return fmt.Errorf("failed to open browser: %w", err)
				}
				return nil
			}
		}
	}
}

// RunApp runs the main application in either daemon or TUI mode.
// It handles signal handling, service lifecycle, and graceful shutdown.
func RunApp(pl platforms.Platform, cfg *config.Instance, daemonMode bool) (returnErr error) {
	defer func() {
		if r := recover(); r != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Panic: %v\n", r)
			log.Error().Msgf("panic recovered: %v", r)
			returnErr = fmt.Errorf("panic: %v", r)
		}
	}()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigs)

	exit := make(chan bool, 1)
	var svcDone <-chan struct{}

	switch {
	case daemonMode:
		if helpers.IsServiceRunning(cfg) {
			log.Info().
				Int("port", cfg.APIPort()).
				Msg("service already running, exiting")
			return nil
		}

		log.Info().Msg("starting service in daemon mode")
		stopSvc, done, err := service.Start(pl, cfg)
		if err != nil {
			log.Error().Msgf("error starting service: %s", err)
			return fmt.Errorf("error starting service: %w", err)
		}
		svcDone = done
		defer func() {
			if err := stopSvc(); err != nil {
				log.Error().Msgf("error stopping service: %s", err)
			}
		}()
		log.Info().Msg("started in daemon mode")

	default:
		stopDaemon, err := helpers.SpawnDaemon(cfg)
		if err != nil {
			return fmt.Errorf("error spawning daemon: %w", err)
		}
		defer stopDaemon()

		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("error getting user home directory: %w", err)
		}

		app, err := tui.BuildMain(
			cfg, pl,
			func() bool { return helpers.IsServiceRunning(cfg) },
			filepath.Join(home, "Desktop", "core.log"),
			"desktop",
		)
		if err != nil {
			log.Error().Err(err).Msgf("error building UI")
			return fmt.Errorf("error building UI: %w", err)
		}

		if err = app.Run(); err != nil {
			log.Error().Err(err).Msg("error running UI")
			return fmt.Errorf("error running UI: %w", err)
		}

		exit <- true
	}

	select {
	case <-sigs:
	case <-exit:
	case <-svcDone:
		log.Info().Msg("service shut down internally")
	}

	return nil
}
