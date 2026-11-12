//go:build linux && !android

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
// SPDX-License-Identifier: GPL-3.0-or-later

package retropie

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/linux/installer"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

//go:embed conf/zaparoo.service
var serviceTemplate string

const (
	ServiceFilePath = "/etc/systemd/system/zaparoo.service"
	serviceName     = "zaparoo.service"

	// InstallSystemFlag and UninstallSystemFlag name the root half of
	// install and uninstall, which the user half runs through sudo.
	InstallSystemFlag   = "install-system"
	UninstallSystemFlag = "uninstall-system"
)

// CommandRunner runs a command with the caller's terminal attached, so sudo
// can ask for a password.
type CommandRunner func(ctx context.Context, name string, args ...string) error

var userNamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// systemSide is the part of the install that needs root.
type systemSide struct {
	fs                afero.Fs
	run               CommandRunner
	lookupUser        func(name string) (*user.User, error)
	installHardware   func() error
	uninstallHardware func() error
	unitPath          string
}

func realSystemSide(run CommandRunner) systemSide {
	return systemSide{
		fs:                afero.NewOsFs(),
		run:               run,
		lookupUser:        user.Lookup,
		installHardware:   installer.InstallHardware,
		uninstallHardware: installer.UninstallHardware,
		unitPath:          ServiceFilePath,
	}
}

// checkExecPath rejects paths a unit file cannot hold unquoted. systemd
// expands "%" specifiers and splits on whitespace.
func checkExecPath(exe string) error {
	if !filepath.IsAbs(exe) || strings.ContainsAny(exe, " \t\r\n\x00\"'\\%$;") {
		return fmt.Errorf("zaparoo must be installed at a path without spaces or special characters: %q", exe)
	}
	return nil
}

func renderUnit(userName, exe string) (string, error) {
	if !userNamePattern.MatchString(userName) {
		return "", fmt.Errorf("invalid user name: %q", userName)
	}
	if err := checkExecPath(exe); err != nil {
		return "", err
	}
	return strings.NewReplacer(
		"@USER@", userName,
		"@WORKDIR@", filepath.Dir(exe),
		"@EXEC@", exe,
	).Replace(serviceTemplate), nil
}

// Install sets up the console side as the RetroPie user, then asks sudo to
// install the service that runs Core as that user.
func (p *Platform) Install(ctx context.Context, exe string, run CommandRunner) error {
	if os.Geteuid() == 0 {
		return errors.New(
			"run \"zaparoo -install\" as the RetroPie user (usually pi), not as root; it asks for sudo when needed",
		)
	}
	current, err := user.Current()
	if err != nil {
		return fmt.Errorf("look up current user: %w", err)
	}
	return p.installUser(ctx, current.Username, exe, run)
}

func (p *Platform) installUser(ctx context.Context, userName, exe string, run CommandRunner) error {
	p.defaults()
	if _, err := renderUnit(userName, exe); err != nil {
		return err
	}
	if err := p.fs.MkdirAll(p.stateDir, 0o750); err != nil {
		return fmt.Errorf("create %s: %w", p.stateDir, err)
	}
	desktopEntry := filepath.Join(p.homeDir, ".config", "autostart", "retropie.desktop")
	if _, err := p.fs.Stat(desktopEntry); err == nil {
		return fmt.Errorf(
			"%s found: this RetroPie install starts EmulationStation from a desktop session, "+
				"which Zaparoo cannot launch games through; only console boot "+
				"(\"Boot to EmulationStation\" without a desktop) is supported", desktopEntry,
		)
	}
	if err := installFrontendHooks(p.fs, p.configsDir, p.stateDir); err != nil {
		return fmt.Errorf("install RetroPie hooks: %w", err)
	}
	if err := run(ctx, "sudo", "--", exe, "-"+InstallSystemFlag+"="+userName); err != nil {
		if rbErr := uninstallFrontendHooks(p.fs, p.configsDir); rbErr != nil {
			log.Warn().Err(rbErr).Msg("retropie: failed to remove hooks after the service install failed")
		}
		return fmt.Errorf("install the Zaparoo service: %w", err)
	}
	return nil
}

// Uninstall removes the service through sudo, then the console side.
func (p *Platform) Uninstall(ctx context.Context, exe string, run CommandRunner) error {
	if os.Geteuid() == 0 {
		return errors.New("run \"zaparoo -uninstall\" as the RetroPie user (usually pi), not as root")
	}
	return p.uninstallUser(ctx, exe, run)
}

func (p *Platform) uninstallUser(ctx context.Context, exe string, run CommandRunner) error {
	p.defaults()
	if err := run(ctx, "sudo", "--", exe, "-"+UninstallSystemFlag); err != nil {
		return fmt.Errorf("uninstall the Zaparoo service: %w", err)
	}
	if err := uninstallFrontendHooks(p.fs, p.configsDir); err != nil {
		return fmt.Errorf("remove RetroPie hooks: %w", err)
	}
	return nil
}

// InstallSystem is the root half of install: the service unit that runs exe
// as userName, and reader hardware support.
func InstallSystem(ctx context.Context, userName, exe string, run CommandRunner) error {
	if os.Geteuid() != 0 {
		return errors.New("the system install must be run as root")
	}
	return realSystemSide(run).install(ctx, userName, exe)
}

func (s systemSide) install(ctx context.Context, userName, exe string) error {
	unit, err := renderUnit(userName, exe)
	if err != nil {
		return err
	}
	u, err := s.lookupUser(userName)
	if err != nil {
		return fmt.Errorf("look up user %q: %w", userName, err)
	}
	if u.Uid == "0" {
		return errors.New("zaparoo must not run as root")
	}
	if err := s.installHardware(); err != nil {
		return fmt.Errorf("install reader support: %w", err)
	}
	if err := writeFileReplacing(s.fs, s.unitPath, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("write service file: %w", err)
	}
	for _, args := range [][]string{
		{"daemon-reload"},
		{"enable", serviceName},
		{"restart", serviceName},
	} {
		if err := s.run(ctx, "systemctl", args...); err != nil {
			return fmt.Errorf("systemctl %s: %w", args[0], err)
		}
	}
	return nil
}

// UninstallSystem is the root half of uninstall.
func UninstallSystem(ctx context.Context, run CommandRunner) error {
	if os.Geteuid() != 0 {
		return errors.New("the system uninstall must be run as root")
	}
	return realSystemSide(run).uninstall(ctx)
}

func (s systemSide) uninstall(ctx context.Context) error {
	// The service may already be stopped or disabled.
	_ = s.run(ctx, "systemctl", "stop", serviceName)
	_ = s.run(ctx, "systemctl", "disable", serviceName)
	if err := removeIfExists(s.fs, s.unitPath); err != nil {
		return err
	}
	if err := s.run(ctx, "systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	if err := s.uninstallHardware(); err != nil {
		return fmt.Errorf("remove reader support: %w", err)
	}
	return nil
}
