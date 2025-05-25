/*
Zaparoo Core
Copyright (C) 2023 Gareth Jones
Copyright (C) 2023, 2024 Callan Barrett

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
	"errors"
	"flag"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/ui"
	"github.com/ZaparooProject/zaparoo-core/pkg/ui/systray"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ZaparooProject/zaparoo-core/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/pkg/config/migrate"
	"github.com/rs/zerolog/log"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms/windows"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/service"

	syscallWindows "golang.org/x/sys/windows"

	_ "embed"
)

//go:embed winres/icon.ico
var icon []byte

func isElevated() (bool, error) {
	// https://github.com/golang/go/issues/28804#issuecomment-505326268
	var sid *syscallWindows.SID

	// Although this looks scary, it is directly copied from the
	// official Windows documentation.
	// The Go API for this is a direct wrap around the official C++ API.
	// See https://docs.microsoft.com/en-us/windows/desktop/api/securitybaseapi/nf-securitybaseapi-checktokenmembership
	err := syscallWindows.AllocateAndInitializeSid(
		&syscallWindows.SECURITY_NT_AUTHORITY,
		2,
		syscallWindows.SECURITY_BUILTIN_DOMAIN_RID,
		syscallWindows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid)
	if err != nil {
		return false, err
	}
	defer func(sid *syscallWindows.SID) {
		_ = syscallWindows.FreeSid(sid)
	}(sid)

	// This appears to cast a null pointer, so I'm not sure why this
	// works, but this guy says it does, and it Works for Me™:
	// https://github.com/golang/go/issues/28804#issuecomment-438838144
	token := syscallWindows.Token(0)

	// Also note that an admin is _not_ necessarily considered
	// elevated.
	// For elevation see https://github.com/mozey/run-as-admin
	return token.IsElevated(), nil
}

func isGUIRunning() bool {
	_, err := syscallWindows.CreateMutex(
		nil, false,
		syscallWindows.StringToUTF16Ptr("MUTEX: Zaparoo Core"),
	)
	if err != nil {
		log.Fatal().Err(err).Msg("error creating mutex")
	}
	lastError := syscallWindows.GetLastError()
	return errors.Is(lastError, syscallWindows.ERROR_ALREADY_EXISTS)
}

func main() {
	elevated, err := isElevated()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error checking elevated rights: %s\n", err)
	}
	if elevated {
		_, _ = fmt.Fprintf(os.Stderr, "Zaparoo cannot be run with elevated rights\n")
		os.Exit(1)
	}

	pl := &windows.Platform{}
	flags := cli.SetupFlags()

	daemonMode := flag.Bool(
		"daemon",
		false,
		"run service in foreground with no UI",
	)
	guiMode := flag.Bool(
		"gui",
		false,
		"run service as daemon with GUI",
	)

	flags.Pre(pl)

	var logWriters []io.Writer
	if *daemonMode || *guiMode {
		logWriters = []io.Writer{os.Stderr}
	}

	defaults := config.BaseDefaults
	defaults.DebugLogging = true
	iniPath := filepath.Join(utils.ExeDir(), "tapto.ini")
	if migrate.Required(iniPath, filepath.Join(utils.ConfigDir(pl), config.CfgFile)) {
		migrated, err := migrate.IniToToml(iniPath)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Error migrating config: %v\n", err)
			os.Exit(1)
		} else {
			defaults = migrated
		}
	}

	cfg := cli.Setup(
		pl,
		defaults,
		logWriters,
	)

	defer func() {
		if err := recover(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "Panic: %s\n", err)
			log.Fatal().Msgf("panic: %v", err)
		}
	}()

	flags.Post(cfg, pl)

	if !utils.IsServiceRunning(cfg) {
		stopSvc, err := service.Start(pl, cfg)
		if err != nil {
			log.Error().Msgf("error starting service: %s", err)
			_, _ = fmt.Fprintf(os.Stderr, "Error starting service: %s\n", err)
			os.Exit(1)
		}

		defer func() {
			err := stopSvc()
			if err != nil {
				log.Error().Msgf("error stopping service: %s", err)
			}
		}()
	}

	sigs := make(chan os.Signal, 1)
	defer close(sigs)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	exit := make(chan bool, 1)
	defer close(exit)

	if *daemonMode {
		log.Info().Msg("started in daemon mode")
	} else if *guiMode {
		if isGUIRunning() {
			log.Error().Msg("gui is already running")
			fmt.Println("Zaparoo Core GUI is already running")
			os.Exit(1)
		}

		systray.Run(cfg, pl, icon, func() {
			exit <- true
		})
	} else {
		// default to showing the TUI
		app, err := ui.BuildTheUi(
			pl, utils.IsServiceRunning(cfg), cfg,
			filepath.Join(os.Getenv("HOME"), "Desktop", "core.log"),
		)
		if err != nil {
			log.Error().Err(err).Msgf("error building UI")
			_, _ = fmt.Fprintf(os.Stderr, "Error building UI: %s\n", err)
			os.Exit(1)
		}

		err = app.Run()
		if err != nil {
			log.Error().Err(err).Msg("error running UI")
			_, _ = fmt.Fprintf(os.Stderr, "Error running UI: %s\n", err)
			os.Exit(1)
		}

		exit <- true
	}

	select {
	case <-sigs:
	case <-exit:
	}

	os.Exit(0)
}
