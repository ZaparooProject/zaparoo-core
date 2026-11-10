//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"syscall"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/telemetry"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/client"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/arkos"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/daemon"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/restart"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/tui"
	"github.com/rivo/tview"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

//go:embed conf/zaparoo.service
var serviceFile string

const (
	serviceFilePath = "/etc/systemd/system/zaparoo.service"
	binaryName      = "zaparoo"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(cli.ExitCodeFor(err))
	}
}

func run() error {
	defer telemetry.Close()
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			_, _ = fmt.Fprintf(os.Stderr, "Panic: %v\n%s\n", r, stack)
			log.Error().
				Interface("panic", r).
				Bytes("stack", stack).
				Msg("recovered from panic")
			telemetry.Flush()
			os.Exit(1)
		}
	}()

	pl := arkos.NewPlatform()
	flags := cli.SetupFlags()

	asDaemon := flag.Bool("daemon", false, "run zaparoo in daemon mode")
	doInstall := flag.Bool("install", false, "install zaparoo systemd service")
	doUninstall := flag.Bool("uninstall", false, "uninstall zaparoo systemd service")

	flags.Pre(pl)

	if *doInstall {
		return installService()
	}
	if *doUninstall {
		return uninstallService()
	}

	logWriters := []io.Writer{zerolog.ConsoleWriter{Out: os.Stderr}}
	if *asDaemon {
		logWriters = []io.Writer{os.Stderr}
	}

	cfg := cli.Setup(
		pl,
		config.BaseDefaults,
		logWriters,
	)

	flags.Post(cfg, pl)

	if *asDaemon {
		return runDaemon(pl, cfg)
	}

	return runTUI(pl, cfg)
}

func runDaemon(pl *arkos.Platform, cfg *config.Instance) error {
	runtime.GOMAXPROCS(2)

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigs)

	svcResult, err := service.Start(pl, cfg)
	if err != nil {
		// The previous version is back on disk, but this process is still the
		// image that failed and nothing here would start the restored one.
		if errors.Is(err, updater.ErrRolledBack) {
			return fmt.Errorf("restarting after update rollback: %w", restart.ExecAfterRollback(err))
		}

		// A taken API port and a database newer than this build were already
		// logged at Warn where they happened.
		if !api.IsAddrInUse(err) && !errors.Is(err, database.ErrSchemaAhead) {
			log.Error().Err(err).Msg("error starting service")
		}
		return fmt.Errorf("error starting service: %w", err)
	}

	select {
	case <-sigs:
		err = svcResult.Stop()
		if err != nil {
			log.Error().Err(err).Msg("error stopping service")
			return fmt.Errorf("error stopping service: %w", err)
		}
	case <-svcResult.Done:
		log.Info().Msg("service shut down internally")
		if err := restart.ExecIfRequested(svcResult.RestartRequested); err != nil {
			return fmt.Errorf("failed to re-exec for restart: %w", err)
		}
	}

	return nil
}

// runTUI connects to an existing daemon or spawns one, then displays the TUI.
func runTUI(pl *arkos.Platform, cfg *config.Instance) error {
	stopDaemon, err := daemon.SpawnDaemon(cfg)
	if err != nil {
		return fmt.Errorf("error spawning daemon: %w", err)
	}
	defer stopDaemon()

	err = tui.BuildAndRetry(cfg, pl, func() (*tview.Application, error) {
		logDestPath := filepath.Join(helpers.DataDir(pl), config.LogFile)
		return tui.BuildMain(
			cfg, pl,
			func() bool { return client.IsServiceRunning(cfg) },
			logDestPath, "storage",
		)
	})
	if err != nil {
		return fmt.Errorf("error displaying TUI: %w", err)
	}

	return nil
}

func systemctl(args ...string) error {
	//nolint:gosec // G204: args from internal install/uninstall logic
	cmd := exec.CommandContext(context.Background(), "systemctl", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("systemctl %s: %w", args[0], err)
	}
	return nil
}

func installService() error {
	if err := os.MkdirAll(arkos.InstallDir, 0o750); err != nil {
		return fmt.Errorf("error creating install directory: %w", err)
	}
	if err := installBinary(); err != nil {
		return err
	}

	//nolint:gosec // System service file needs to be readable by systemd
	err := os.WriteFile(serviceFilePath, []byte(serviceFile), 0o644)
	if err != nil {
		return fmt.Errorf("error writing service file: %w", err)
	}

	if err := systemctl("daemon-reload"); err != nil {
		return fmt.Errorf("error reloading systemd: %w", err)
	}

	if err := systemctl("enable", "zaparoo.service"); err != nil {
		return fmt.Errorf("error enabling service: %w", err)
	}

	// restart, not start: a reinstall over a running service must pick up
	// the new binary.
	if err := systemctl("restart", "zaparoo.service"); err != nil {
		return fmt.Errorf("error starting service: %w", err)
	}

	_, _ = fmt.Println("Zaparoo service installed and started.")
	return nil
}

// installBinary copies the running binary to where the service runs it from,
// unless it is already there.
func installBinary() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("error finding executable: %w", err)
	}
	if resolved, evalErr := filepath.EvalSymlinks(exe); evalErr == nil {
		exe = resolved
	}
	dest := filepath.Join(arkos.InstallDir, binaryName)
	if exe == dest {
		return nil
	}
	data, err := os.ReadFile(exe) //nolint:gosec // G304: path of the running executable
	if err != nil {
		return fmt.Errorf("error reading executable: %w", err)
	}
	tmp := dest + ".new"
	//nolint:gosec // G306: the service runs this binary
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return fmt.Errorf("error writing binary: %w", err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("error installing binary: %w", err)
	}
	return nil
}

func uninstallService() error {
	_ = systemctl("stop", "zaparoo.service")
	_ = systemctl("disable", "zaparoo.service")

	if err := os.Remove(serviceFilePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("error removing service file: %w", err)
	}

	if err := systemctl("daemon-reload"); err != nil {
		return fmt.Errorf("error reloading systemd: %w", err)
	}

	_, _ = fmt.Println("Zaparoo service uninstalled.")
	return nil
}
