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
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/pathutil"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	// UnitPath is where LibreELEC keeps user systemd units; /etc/systemd/system
	// is a read-only link to it.
	UnitPath    = "/storage/.config/system.d/zaparoo.service"
	serviceName = "zaparoo.service"

	// RetroArchConfigPath is the user configuration retroarch.service reads.
	RetroArchConfigPath = "/storage/.config/retroarch/retroarch.cfg"
	// retroArchDefaultConfig is what RetroArch copies to the user path on a
	// first start.
	retroArchDefaultConfig = "/etc/retroarch.cfg"

	unitExecPlaceholder = "@ZAPAROO_BIN@"
	unitDirPlaceholder  = "@ZAPAROO_DIR@"
	// unitPortLockPlaceholder is a line of its own in the template, filled
	// with the unit's port lock or dropped.
	unitPortLockPlaceholder = "@ZAPAROO_PORT_LOCK@\n"

	networkCmdEnableKey = "network_cmd_enable"
	networkCmdPortKey   = "network_cmd_port"
	networkCmdPort      = "55355"

	// portLockRule drops RetroArch network commands that do not come from
	// this device. RetroArch listens on every IPv4 interface and has no
	// setting to narrow that.
	portLockRule = "INPUT -p udp --dport " + networkCmdPort + " ! -i lo -j DROP"
	// unitPortLock applies the rule each time Core's service starts, as
	// firewall rules do not outlast a reboot. Its presence in the installed
	// unit is also the record that Core turned network commands on.
	unitPortLock = "# RetroArch network commands were off before Zaparoo was installed, so they\n" +
		"# are kept to this device.\n" +
		"ExecStartPre=-/bin/sh -c 'iptables -w -C " + portLockRule + " 2>/dev/null || iptables -w -I " +
		portLockRule + "'\n"
)

// Installer installs Core as a systemd service on Lakka.
type Installer struct {
	FS   afero.Fs
	Exec command.Executor
	// Unit is the unit file template; its ExecStart names @ZAPAROO_BIN@ and
	// its WorkingDirectory @ZAPAROO_DIR@.
	Unit string
	// Executable is the absolute path of the binary the unit runs.
	Executable string
}

// Install writes and enables the unit, turns on RetroArch's network commands
// and starts Core.
//
// RetroArch takes network commands from the whole network. When they were off,
// Core is the one opening that port, so it is locked to this device first and
// the install fails if it cannot be. When the player already had them on, the
// port is left as they set it up: something else of theirs may be using it.
func (i *Installer) Install(ctx context.Context) error {
	current, err := readRetroArchConfig(i.FS)
	if err != nil {
		return err
	}
	lock := configValues(current)[networkCmdEnableKey] != "true" || i.portLocked()
	unit, err := renderUnit(i.Unit, i.Executable, lock)
	if err != nil {
		return err
	}
	if lock {
		if err := i.lockPort(ctx); err != nil {
			return err
		}
	}
	if err := i.FS.MkdirAll(filepath.Dir(UnitPath), 0o755); err != nil {
		return fmt.Errorf("create systemd unit directory: %w", err)
	}
	if err := pathutil.WriteFileAtomic(i.FS, UnitPath, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("write service file: %w", err)
	}
	if err := i.systemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	if err := i.systemctl(ctx, "enable", serviceName); err != nil {
		return err
	}
	if err := EnableNetworkCommands(ctx, i.FS, i.Exec); err != nil {
		return err
	}
	return i.systemctl(ctx, "restart", serviceName)
}

// Uninstall stops and removes the unit. Network commands Core turned on are
// turned off again before the port lock goes; ones the player had on already
// are left as they are.
func (i *Installer) Uninstall(ctx context.Context) error {
	locked := i.portLocked()
	if locked {
		if err := disableNetworkCommands(ctx, i.FS, i.Exec); err != nil {
			return err
		}
	}
	_ = i.systemctl(ctx, "stop", serviceName)
	_ = i.systemctl(ctx, "disable", serviceName)
	if err := i.FS.Remove(UnitPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove service file: %w", err)
	}
	if locked {
		if err := i.iptables(ctx, "-D"); err != nil {
			log.Debug().Err(err).Msg("retroarch network command port lock was not in place")
		}
	}
	return i.systemctl(ctx, "daemon-reload")
}

// portLocked reports whether the installed unit locks RetroArch's network
// command port, which it does when an install of Core turned the commands on.
func (i *Installer) portLocked() bool {
	unit, err := afero.ReadFile(i.FS, UnitPath)
	return err == nil && strings.Contains(string(unit), portLockRule)
}

// lockPort puts the port lock in place now, rather than at the next start of
// Core's service.
func (i *Installer) lockPort(ctx context.Context) error {
	if i.iptables(ctx, "-C") == nil {
		return nil
	}
	if err := i.iptables(ctx, "-I"); err != nil {
		return fmt.Errorf("keep retroarch network commands to this device: %w", err)
	}
	return nil
}

func (i *Installer) iptables(ctx context.Context, action string) error {
	args := append([]string{"-w", action}, strings.Fields(portLockRule)...)
	if err := i.Exec.Run(ctx, "iptables", args...); err != nil {
		return fmt.Errorf("iptables %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func (i *Installer) systemctl(ctx context.Context, args ...string) error {
	if err := i.Exec.Run(ctx, "systemctl", args...); err != nil {
		return fmt.Errorf("systemctl %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// renderUnit fills the binary location into the unit template. systemd splits
// ExecStart on whitespace and expands % and $, so a path using any of those,
// or quoting, is refused rather than escaped.
func renderUnit(template, executable string, lockPort bool) (string, error) {
	if !filepath.IsAbs(executable) {
		return "", fmt.Errorf("executable path must be absolute: %q", executable)
	}
	if strings.ContainsFunc(executable, func(r rune) bool {
		return r < 0x20 || r == 0x7f || strings.ContainsRune(" \t\"'\\%$;", r)
	}) {
		return "", fmt.Errorf("install Zaparoo to a path without spaces or special characters: %q", executable)
	}
	if !strings.Contains(template, unitExecPlaceholder) {
		return "", errors.New("unit template has no executable placeholder")
	}
	portLock := ""
	if lockPort {
		if !strings.Contains(template, unitPortLockPlaceholder) {
			return "", errors.New("unit template has no port lock placeholder")
		}
		portLock = unitPortLock
	}
	unit := strings.ReplaceAll(template, unitPortLockPlaceholder, portLock)
	unit = strings.ReplaceAll(unit, unitExecPlaceholder, executable)
	return strings.ReplaceAll(unit, unitDirPlaceholder, filepath.Dir(executable)), nil
}

// EnableNetworkCommands turns on RetroArch's network command interface.
func EnableNetworkCommands(ctx context.Context, fs afero.Fs, exec command.Executor) error {
	current, err := readRetroArchConfig(fs)
	if err != nil {
		return err
	}
	if NetworkCommandsEnabled(current) {
		return nil
	}
	if err := editRetroArchConfig(ctx, fs, exec, map[string]string{
		networkCmdEnableKey: "true",
		networkCmdPortKey:   networkCmdPort,
	}); err != nil {
		return err
	}
	log.Info().Str("path", RetroArchConfigPath).Msg("enabled retroarch network commands")
	return nil
}

// disableNetworkCommands turns RetroArch's network command interface off.
func disableNetworkCommands(ctx context.Context, fs afero.Fs, exec command.Executor) error {
	current, err := readRetroArchConfig(fs)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if configValues(current)[networkCmdEnableKey] != "true" {
		return nil
	}
	if err := editRetroArchConfig(ctx, fs, exec, map[string]string{networkCmdEnableKey: "false"}); err != nil {
		return err
	}
	log.Info().Str("path", RetroArchConfigPath).Msg("disabled retroarch network commands")
	return nil
}

// editRetroArchConfig sets values in RetroArch's configuration. RetroArch
// writes its whole configuration back on exit, so the file is only edited
// while retroarch.service is stopped, and the service is started again
// whatever happens.
func editRetroArchConfig(
	ctx context.Context,
	fs afero.Fs,
	exec command.Executor,
	values map[string]string,
) (err error) {
	if stopErr := exec.Run(ctx, "systemctl", "stop", retroArchService); stopErr != nil {
		return fmt.Errorf("stop retroarch to edit its configuration: %w", stopErr)
	}
	defer func() {
		if startErr := exec.Run(ctx, "systemctl", "start", retroArchService); startErr != nil {
			err = errors.Join(err, fmt.Errorf("start retroarch: %w", startErr))
		}
	}()

	// Read now: the stopping RetroArch has just saved its configuration.
	current, err := readRetroArchConfig(fs)
	if err != nil {
		return err
	}
	updated := setConfigValues(current, values)
	if err = fs.MkdirAll(filepath.Dir(RetroArchConfigPath), 0o755); err != nil {
		return fmt.Errorf("create retroarch config directory: %w", err)
	}
	if err = pathutil.WriteFileAtomic(fs, RetroArchConfigPath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write retroarch config: %w", err)
	}
	return nil
}

// readRetroArchConfig returns the user configuration, or the image's default
// when RetroArch has not written one yet. Starting from an empty file instead
// would make RetroArch fall back to its built-in defaults, not Lakka's.
func readRetroArchConfig(fs afero.Fs) (string, error) {
	data, err := afero.ReadFile(fs, RetroArchConfigPath)
	if err == nil {
		return string(data), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read retroarch config: %w", err)
	}
	data, err = afero.ReadFile(fs, retroArchDefaultConfig)
	if err != nil {
		return "", fmt.Errorf("read retroarch default config: %w", err)
	}
	return string(data), nil
}

// NetworkCommandsEnabled reports whether a retroarch.cfg has network commands
// on at the port Core uses.
func NetworkCommandsEnabled(cfg string) bool {
	values := configValues(cfg)
	port, hasPort := values[networkCmdPortKey]
	return values[networkCmdEnableKey] == "true" && (!hasPort || port == networkCmdPort)
}

// configValues parses RetroArch's key = "value" lines. RetroArch keeps the
// first of duplicate keys.
func configValues(cfg string) map[string]string {
	values := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(cfg))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		key, value, ok := parseConfigLine(scanner.Text())
		if _, seen := values[key]; ok && !seen {
			values[key] = value
		}
	}
	return values
}

func parseConfigLine(line string) (key, value string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	key, value, ok = strings.Cut(trimmed, "=")
	if !ok {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(strings.TrimPrefix(value, `"`), `"`)
	return key, value, key != ""
}

// setConfigValues rewrites every line for each key and appends keys the file
// lacks, leaving all other lines byte for byte.
func setConfigValues(cfg string, values map[string]string) string {
	lines := strings.SplitAfter(cfg, "\n")
	seen := make(map[string]bool, len(values))
	var out strings.Builder
	for _, line := range lines {
		if line == "" {
			continue
		}
		key, _, ok := parseConfigLine(line)
		if value, wanted := values[key]; ok && wanted {
			seen[key] = true
			out.WriteString(formatConfigLine(key, value))
			continue
		}
		out.WriteString(line)
	}
	if out.Len() > 0 && !strings.HasSuffix(out.String(), "\n") {
		out.WriteString("\n")
	}
	for _, key := range []string{networkCmdEnableKey, networkCmdPortKey} {
		if value, wanted := values[key]; wanted && !seen[key] {
			out.WriteString(formatConfigLine(key, value))
		}
	}
	return out.String()
}

func formatConfigLine(key, value string) string {
	return key + ` = "` + value + "\"\n"
}
