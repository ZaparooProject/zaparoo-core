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

	"github.com/ZaparooProject/zaparoo-core/v2/internal/telemetry"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/cli"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config/migrate"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/windows"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/restart"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/updater"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/systray"
	"github.com/rs/zerolog/log"
	syswindows "golang.org/x/sys/windows"
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

type singleInstance struct {
	closeHandle func(syswindows.Handle) error
	handle      syswindows.Handle
}

func (s *singleInstance) release() error {
	if s == nil || s.handle == 0 {
		return nil
	}
	if err := s.closeHandle(s.handle); err != nil {
		return err
	}
	s.handle = 0
	return nil
}

type singleInstanceOps struct {
	createMutex func(*syswindows.SecurityAttributes, bool, *uint16) (syswindows.Handle, error)
	closeHandle func(syswindows.Handle) error
}

func acquireSingleInstance() (*singleInstance, bool) {
	return acquireSingleInstanceWith(singleInstanceOps{
		createMutex: syswindows.CreateMutex,
		closeHandle: syswindows.CloseHandle,
	})
}

func acquireSingleInstanceWith(ops singleInstanceOps) (*singleInstance, bool) {
	handle, err := ops.createMutex(
		nil, false,
		syswindows.StringToUTF16Ptr("MUTEX: Zaparoo Core"),
	)
	// When another instance already holds the named mutex, CreateMutex reports
	// ERROR_ALREADY_EXISTS. That is the "already running" signal we want — not a
	// failure. Treating it as fatal crashed the second launch instead of exiting
	// cleanly.
	if errors.Is(err, syswindows.ERROR_ALREADY_EXISTS) {
		if closeErr := ops.closeHandle(handle); closeErr != nil {
			log.Debug().Err(closeErr).Msg("could not close duplicate single-instance mutex handle")
		}
		return nil, true
	}
	if err != nil {
		// A genuine mutex-creation failure shouldn't prevent startup; log it and
		// assume no other instance is running.
		log.Error().Err(err).Msg("error creating single-instance mutex")
		return nil, false
	}
	return &singleInstance{handle: handle, closeHandle: ops.closeHandle}, false
}

func restartAfterReleasing(instance *singleInstance, restartFn func() error) error {
	if err := instance.release(); err != nil {
		return fmt.Errorf("releasing single-instance mutex before restart: %w", err)
	}
	return restartFn()
}

// windowsDefaults turns on connection encryption for new installs and, because
// Service.Encryption is a pointer, for existing configs that never wrote the
// key. Remote clients cannot hold a role over a plaintext connection, so this
// is what gates keyboard and gamepad input for anyone off the machine.
func windowsDefaults() config.Values {
	defaults := config.BaseDefaults
	enabled := true
	defaults.Service.Encryption = &enabled
	return defaults
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

	defaults := windowsDefaults()
	iniPath := filepath.Join(helpers.ExeDir(), "tapto.ini")
	if migrate.Required(iniPath, filepath.Join(helpers.ConfigDir(pl), config.CfgFile)) {
		migrated, migrateErr := migrate.IniToToml(iniPath)
		if migrateErr != nil {
			return fmt.Errorf("error migrating config: %w", migrateErr)
		}
		// A tapto.ini predates encryption entirely, so migrating one must not
		// silently hand back the old plaintext default.
		migrated.Service.Encryption = defaults.Service.Encryption
		defaults = migrated
	}

	cfg := cli.Setup(
		pl,
		defaults,
		logWriters,
	)

	flags.Post(cfg, pl)

	instance, running := acquireSingleInstance()
	if running {
		log.Error().Msg("core is already running")
		return errors.New("zaparoo is already running")
	}
	defer func() {
		if releaseErr := instance.release(); releaseErr != nil {
			log.Error().Err(releaseErr).Msg("could not release single-instance mutex")
		}
	}()

	svcResult, err := service.Start(pl, cfg)
	if err != nil {
		// The previous version is back on disk, but this process is still the
		// image that failed and nothing here would start the restored one.
		if errors.Is(err, updater.ErrRolledBack) {
			if releaseErr := instance.release(); releaseErr != nil {
				return fmt.Errorf("releasing single-instance mutex before rollback restart: %w",
					errors.Join(err, releaseErr))
			}
			return fmt.Errorf("restarting after update rollback: %w", restart.ExecAfterRollback(err))
		}

		log.Error().Msgf("error starting service: %s", err)
		return fmt.Errorf("error starting service: %w", err)
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	exit := make(chan bool, 1)

	// The tray owns the main thread until it quits, so the service has to be
	// watched alongside it rather than after it.
	restarting := restart.WaitForShutdown(
		sigs, exit, svcResult.Done, svcResult.RestartRequested, systray.Quit)

	systray.Run(cfg, pl, icon,
		func(msg string) {
			log.Debug().Msgf("systray notification: %s", msg)
		},
		func() {
			exit <- true
		},
	)

	if <-restarting {
		// The service stopped itself on the way to being replaced. Stopping it
		// again would only delay the binary that replaced it.
		if err := restartAfterReleasing(instance, restart.Exec); err != nil {
			return fmt.Errorf("failed to re-exec for restart: %w", err)
		}
		return nil
	}
	if err := svcResult.Stop(); err != nil {
		log.Error().Msgf("error stopping service: %s", err)
	}

	return nil
}
