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

	networkCmdEnableKey = "network_cmd_enable"
	networkCmdPortKey   = "network_cmd_port"
	networkCmdPort      = "55355"
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
func (i *Installer) Install(ctx context.Context) error {
	unit, err := renderUnit(i.Unit, i.Executable)
	if err != nil {
		return err
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

// Uninstall stops and removes the unit. RetroArch's configuration is left as
// it is: the player may rely on network commands for something else.
func (i *Installer) Uninstall(ctx context.Context) error {
	_ = i.systemctl(ctx, "stop", serviceName)
	_ = i.systemctl(ctx, "disable", serviceName)
	if err := i.FS.Remove(UnitPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove service file: %w", err)
	}
	return i.systemctl(ctx, "daemon-reload")
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
func renderUnit(template, executable string) (string, error) {
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
	unit := strings.ReplaceAll(template, unitExecPlaceholder, executable)
	return strings.ReplaceAll(unit, unitDirPlaceholder, filepath.Dir(executable)), nil
}

// EnableNetworkCommands turns on RetroArch's network command interface.
// RetroArch writes its whole configuration back on exit, so the file is only
// edited while retroarch.service is stopped, and the service is started again
// whatever happens.
func EnableNetworkCommands(ctx context.Context, fs afero.Fs, exec command.Executor) (err error) {
	current, err := readRetroArchConfig(fs)
	if err != nil {
		return err
	}
	if NetworkCommandsEnabled(current) {
		return nil
	}

	if stopErr := exec.Run(ctx, "systemctl", "stop", retroArchService); stopErr != nil {
		return fmt.Errorf("stop retroarch to edit its configuration: %w", stopErr)
	}
	defer func() {
		if startErr := exec.Run(ctx, "systemctl", "start", retroArchService); startErr != nil {
			err = errors.Join(err, fmt.Errorf("start retroarch: %w", startErr))
		}
	}()

	// Read again: the stopping RetroArch has just saved its configuration.
	current, err = readRetroArchConfig(fs)
	if err != nil {
		return err
	}
	updated := setConfigValues(current, map[string]string{
		networkCmdEnableKey: "true",
		networkCmdPortKey:   networkCmdPort,
	})
	if err = fs.MkdirAll(filepath.Dir(RetroArchConfigPath), 0o755); err != nil {
		return fmt.Errorf("create retroarch config directory: %w", err)
	}
	if err = pathutil.WriteFileAtomic(fs, RetroArchConfigPath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write retroarch config: %w", err)
	}
	log.Info().Str("path", RetroArchConfigPath).Msg("enabled retroarch network commands")
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
