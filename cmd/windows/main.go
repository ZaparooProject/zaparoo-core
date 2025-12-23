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
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"syscall"

	"github.com/rs/zerolog/log"
	syswindows "golang.org/x/sys/windows"

	"github.com/ZaparooProject/zaparoo-core/v2/internal/telemetry"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config/migrate"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/windows"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/systray"
)

//go:embed winres/icon.ico
var icon []byte

func isElevated() (bool, error) {
	// https://github.com/golang/go/issues/28804#issuecomment-505326268
	var sid *syswindows.SID

	// Although this looks scary, it is directly copied from the
	// official Windows documentation.
	// The Go API for this is a direct wrap around the official C++ API.
	// See https://docs.microsoft.com/en-us/windows/desktop/api/securitybaseapi/nf-securitybaseapi-checktokenmembership
	err := syswindows.AllocateAndInitializeSid(
		&syswindows.SECURITY_NT_AUTHORITY,
		2,
		syswindows.SECURITY_BUILTIN_DOMAIN_RID,
		syswindows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid)
	if err != nil {
		return false, fmt.Errorf("failed to allocate and initialize SID: %w", err)
	}
	defer func(sid *syswindows.SID) {
		_ = syswindows.FreeSid(sid)
	}(sid)

	// This appears to cast a null pointer, so I'm not sure why this
	// works, but this guy says it does, and it Works for Me™:
	// https://github.com/golang/go/issues/28804#issuecomment-438838144
	token := syswindows.Token(0)

	// Also note that an admin is _not_ necessarily considered
	// elevated.
	// For elevation see https://github.com/mozey/run-as-admin
	return token.IsElevated(), nil
}

func isRunning() bool {
	_, err := syswindows.CreateMutex(
		nil, false,
		syswindows.StringToUTF16Ptr("MUTEX: Zaparoo Core"),
	)
	if err != nil {
		log.Fatal().Err(err).Msg("error creating mutex")
	}
	lastError := syswindows.GetLastError()
	return errors.Is(lastError, syswindows.ERROR_ALREADY_EXISTS)
}

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		os.Exit(1)
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

	pl := &windows.Platform{}
	flags := cli.SetupFlags()

	flags.Pre(pl)

	elevated, elevatedErr := isElevated()
	if elevatedErr != nil {
		return fmt.Errorf("error checking elevated rights: %w", elevatedErr)
	}
	if elevated {
		return errors.New("zaparoo cannot be run with elevated rights")
	}

	logWriters := []io.Writer{os.Stderr}

	defaults := config.BaseDefaults
	iniPath := filepath.Join(helpers.ExeDir(), "tapto.ini")
	if migrate.Required(iniPath, filepath.Join(helpers.ConfigDir(pl), config.CfgFile)) {
		migrated, migrateErr := migrate.IniToToml(iniPath)
		if migrateErr != nil {
			return fmt.Errorf("error migrating config: %w", migrateErr)
		}
		defaults = migrated
	}

	cfg := cli.Setup(
		pl,
		defaults,
		logWriters,
	)

	flags.Post(cfg, pl)

	if isRunning() {
		log.Error().Msg("core is already running")
		return errors.New("zaparoo is already running")
	}

	stopSvc, err := service.Start(pl, cfg)
	if err != nil {
		log.Error().Msgf("error starting service: %s", err)
		return fmt.Errorf("error starting service: %w", err)
	}

	sigs := make(chan os.Signal, 1)
	defer close(sigs)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	exit := make(chan bool, 1)
	defer close(exit)

	systray.Run(cfg, pl, icon,
		func(msg string) {
			log.Debug().Msgf("systray notification: %s", msg)
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
	}

	return nil
}
