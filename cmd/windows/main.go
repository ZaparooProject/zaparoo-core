//go:build windows

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
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/ZaparooProject/zaparoo-core/pkg/ui/systray"
	"github.com/gen2brain/beeep"

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

const notificationTitle = "Zaparoo Core"

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

func isRunning() bool {
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
	pl := &windows.Platform{}
	flags := cli.SetupFlags()

	flags.Pre(pl)

	elevated, err := isElevated()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error checking elevated rights: %s\n", err)
	}
	if elevated {
		_, _ = fmt.Fprintf(os.Stderr, "Zaparoo cannot be run with elevated rights\n")
		os.Exit(1)
	}

	logWriters := []io.Writer{os.Stderr}

	defaults := config.BaseDefaults
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

	if isRunning() {
		log.Error().Msg("core is already running")
		_, _ = fmt.Fprintf(os.Stderr, "Zaparoo Core is already running\n")
		_ = beeep.Notify(notificationTitle, "Zaparoo Core is already running.", "")
		os.Exit(1)
	}

	stopSvc, err := service.Start(pl, cfg)
	if err != nil {
		log.Error().Msgf("error starting service: %s", err)
		_, _ = fmt.Fprintf(os.Stderr, "Error starting service: %s\n", err)
		os.Exit(1)
	}
	err = beeep.Notify(notificationTitle, "Core service started.", "")
	if err != nil {
		log.Error().Msgf("error notifying: %s", err)
	}

	sigs := make(chan os.Signal, 1)
	defer close(sigs)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	exit := make(chan bool, 1)
	defer close(exit)

	systray.Run(cfg, pl, icon,
		func(msg string) {
			err := beeep.Notify(notificationTitle, msg, "")
			if err != nil {
				log.Error().Msgf("error notifying: %s", err)
			}
		},
		func() {
			exit <- true
		},
	)

	select {
	case <-sigs:
	case <-exit:
	}

	err = stopSvc()
	if err != nil {
		log.Error().Msgf("error stopping service: %s", err)
	} else {
		err = beeep.Notify(notificationTitle, "Core service stopped.", "")
		if err != nil {
			log.Error().Msgf("error notifying: %s", err)
		}
	}

	os.Exit(0)
}
