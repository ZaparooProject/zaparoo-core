// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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

package installer

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/adrg/xdg"
)

//go:embed conf/blacklist-zaparoo.conf
var modprobeFile string

//go:embed conf/60-zaparoo.rules
var udevFile string

//go:embed conf/zaparoo.service
var systemdServiceFile string

//go:embed zaparoo.desktop
var desktopFile string

//go:embed icons/hicolor/16x16/apps/zaparoo.png
var icon16 []byte

//go:embed icons/hicolor/32x32/apps/zaparoo.png
var icon32 []byte

//go:embed icons/hicolor/48x48/apps/zaparoo.png
var icon48 []byte

//go:embed icons/hicolor/128x128/apps/zaparoo.png
var icon128 []byte

//go:embed icons/hicolor/256x256/apps/zaparoo.png
var icon256 []byte

const (
	modprobePath = "/etc/modprobe.d/blacklist-zaparoo.conf"
	udevPath     = "/etc/udev/rules.d/60-zaparoo.rules"
)

// InstallApplication installs application files (binary, application launcher entry, icon).
// Does not install systemd service or desktop shortcut. Must NOT be run as root.
func InstallApplication() error {
	return doInstallApplication(&helpers.RealCommandExecutor{})
}

// doInstallApplication is the internal testable implementation.
func doInstallApplication(cmd helpers.CommandExecutor) error {
	if os.Geteuid() == 0 {
		return errors.New("application install must not be run as root")
	}

	// Get the current binary path
	binaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("error getting executable path: %w", err)
	}

	// Install binary to ~/.local/bin
	binDir := filepath.Join(xdg.Home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil { //nolint:gosec // XDG directory needs to be accessible
		return fmt.Errorf("error creating bin directory: %w", err)
	}

	destBinary := filepath.Join(binDir, "zaparoo")
	if err := helpers.CopyFile(binaryPath, destBinary, 0o755); err != nil {
		return fmt.Errorf("error installing binary: %w", err)
	}

	// Install desktop file to applications
	desktopDir := filepath.Join(xdg.DataHome, "applications")
	if err := os.MkdirAll(desktopDir, 0o755); err != nil { //nolint:gosec // XDG directory needs to be accessible
		return fmt.Errorf("error creating applications directory: %w", err)
	}

	desktopPath := filepath.Join(desktopDir, "zaparoo.desktop")
	//nolint:gosec // Desktop file needs to be readable by desktop environment
	if err := os.WriteFile(desktopPath, []byte(desktopFile), 0o644); err != nil {
		return fmt.Errorf("error writing desktop file: %w", err)
	}

	// Update desktop database if available
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = cmd.Run(ctx, "update-desktop-database", desktopDir)
	cancel()

	// Install icons at multiple sizes
	iconSizes := map[string][]byte{
		"16x16":   icon16,
		"32x32":   icon32,
		"48x48":   icon48,
		"128x128": icon128,
		"256x256": icon256,
	}

	for size, iconData := range iconSizes {
		iconDir := filepath.Join(xdg.DataHome, "icons", "hicolor", size, "apps")
		if err := os.MkdirAll(iconDir, 0o755); err != nil { //nolint:gosec // XDG directory needs to be accessible
			return fmt.Errorf("error creating icon directory %s: %w", size, err)
		}

		iconPath := filepath.Join(iconDir, "zaparoo.png")
		//nolint:gosec // Icon file needs to be readable by desktop environment
		if err := os.WriteFile(iconPath, iconData, 0o644); err != nil {
			return fmt.Errorf("error writing icon file %s: %w", size, err)
		}
	}

	// Update icon cache if available
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	iconBaseDir := filepath.Join(xdg.DataHome, "icons", "hicolor")
	_ = cmd.Run(ctx, "gtk-update-icon-cache", "-f", "-t", iconBaseDir)
	cancel()

	return nil
}

// InstallService installs systemd user service.
// Does not install application files. Must NOT be run as root.
func InstallService() error {
	return doInstallService(&helpers.RealCommandExecutor{})
}

// doInstallService is the internal testable implementation.
func doInstallService(cmd helpers.CommandExecutor) error {
	if os.Geteuid() == 0 {
		return errors.New("service install must not be run as root")
	}

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Resolve symlinks to get actual binary path
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	// Create template data
	type ServiceData struct {
		ExecPath string
	}
	data := ServiceData{ExecPath: execPath}

	// Parse service file as template
	tmpl, err := template.New("service").Parse(systemdServiceFile)
	if err != nil {
		return fmt.Errorf("failed to parse service template: %w", err)
	}

	// Execute template
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to execute service template: %w", err)
	}

	// Install systemd user service
	systemdDir := filepath.Join(xdg.ConfigHome, "systemd", "user")
	if err := os.MkdirAll(systemdDir, 0o755); err != nil { //nolint:gosec // XDG directory needs to be accessible
		return fmt.Errorf("error creating systemd directory: %w", err)
	}

	servicePath := filepath.Join(systemdDir, "zaparoo.service")
	//nolint:gosec // Service file needs to be readable by systemd
	if err := os.WriteFile(servicePath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("error writing systemd service file: %w", err)
	}

	// Reload systemd user daemon
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	_ = cmd.Run(ctx, "systemctl", "--user", "daemon-reload")
	cancel()

	return nil
}

// InstallDesktop installs desktop shortcut.
// Does not install application files. Must NOT be run as root.
func InstallDesktop() error {
	if os.Geteuid() == 0 {
		return errors.New("desktop install must not be run as root")
	}

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Resolve symlinks to get actual binary path
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	// Create template data
	type DesktopData struct {
		ExecPath string
	}
	data := DesktopData{ExecPath: execPath}

	// Parse desktop file as template
	tmpl, err := template.New("desktop").Parse(desktopFile)
	if err != nil {
		return fmt.Errorf("failed to parse desktop template: %w", err)
	}

	// Execute template
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to execute desktop template: %w", err)
	}

	// Install desktop shortcut to ~/Desktop
	desktopPath := filepath.Join(xdg.Home, "Desktop", "zaparoo.desktop")
	desktopDir := filepath.Dir(desktopPath)

	// Create Desktop directory if it doesn't exist
	if err := os.MkdirAll(desktopDir, 0o755); err != nil { //nolint:gosec // Desktop directory needs to be accessible
		return fmt.Errorf("error creating Desktop directory: %w", err)
	}

	//nolint:gosec // Desktop file needs to be readable by desktop environment
	if err := os.WriteFile(desktopPath, buf.Bytes(), 0o755); err != nil {
		return fmt.Errorf("error writing desktop shortcut: %w", err)
	}

	return nil
}

// InstallHardware installs hardware support files (udev rules, modprobe blacklist).
// Must be run as root.
func InstallHardware() error {
	return doInstallHardware(&helpers.RealCommandExecutor{})
}

// doInstallHardware is the internal testable implementation.
func doInstallHardware(cmd helpers.CommandExecutor) error {
	if os.Geteuid() != 0 {
		return errors.New("hardware install must be run as root")
	}

	// install udev rules
	if _, err := os.Stat(filepath.Dir(udevPath)); os.IsNotExist(err) {
		return errors.New("udev rules directory does not exist")
	} else if _, err := os.Stat(udevPath); os.IsNotExist(err) {
		err = os.WriteFile(udevPath, []byte(udevFile), 0o644) //nolint:gosec // udev rules need to be readable by system
		if err != nil {
			return fmt.Errorf("error creating udev rules: %w", err)
		}
		// these are just for convenience, don't care too much if they fail
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = cmd.Run(ctx, "udevadm", "control", "--reload-rules")
		_ = cmd.Run(ctx, "udevadm", "trigger")
		cancel()
	}

	// install modprobe blacklist
	if _, err := os.Stat(filepath.Dir(modprobePath)); os.IsNotExist(err) {
		return errors.New("modprobe directory does not exist")
	} else if _, err := os.Stat(modprobePath); os.IsNotExist(err) {
		//nolint:gosec // modprobe config needs to be readable by system
		err = os.WriteFile(modprobePath, []byte(modprobeFile), 0o644)
		if err != nil {
			return fmt.Errorf("error creating modprobe blacklist: %w", err)
		}
		// this is just for convenience, don't care too much if it fails
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = cmd.Run(ctx, "systemctl", "restart", "systemd-modules-load.service")
		cancel()
	}

	return nil
}

// UninstallApplication removes application files (binary, application launcher entry, icon).
// Does not remove systemd service or desktop shortcut. Must NOT be run as root.
func UninstallApplication() error {
	return doUninstallApplication(&helpers.RealCommandExecutor{})
}

// doUninstallApplication is the internal testable implementation.
func doUninstallApplication(cmd helpers.CommandExecutor) error {
	if os.Geteuid() == 0 {
		return errors.New("application uninstall must not be run as root")
	}

	// Remove icons at all sizes
	iconSizes := []string{"16x16", "32x32", "48x48", "128x128", "256x256"}
	for _, size := range iconSizes {
		iconPath := filepath.Join(xdg.DataHome, "icons", "hicolor", size, "apps", "zaparoo.png")
		if _, err := os.Stat(iconPath); !os.IsNotExist(err) {
			if err := os.Remove(iconPath); err != nil {
				return fmt.Errorf("error removing icon %s: %w", size, err)
			}
		}
	}

	// Update icon cache if available
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	iconBaseDir := filepath.Join(xdg.DataHome, "icons", "hicolor")
	_ = cmd.Run(ctx, "gtk-update-icon-cache", "-f", "-t", iconBaseDir)
	cancel()

	// Remove desktop file from applications
	desktopPath := filepath.Join(xdg.DataHome, "applications", "zaparoo.desktop")
	if _, err := os.Stat(desktopPath); !os.IsNotExist(err) {
		if err := os.Remove(desktopPath); err != nil {
			return fmt.Errorf("error removing desktop file: %w", err)
		}
	}

	// Update desktop database if available
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	desktopDir := filepath.Join(xdg.DataHome, "applications")
	_ = cmd.Run(ctx, "update-desktop-database", desktopDir)
	cancel()

	// Remove binary
	binaryPath := filepath.Join(xdg.Home, ".local", "bin", "zaparoo")
	if _, err := os.Stat(binaryPath); !os.IsNotExist(err) {
		if err := os.Remove(binaryPath); err != nil {
			return fmt.Errorf("error removing binary: %w", err)
		}
	}

	// Remove autostart file if it exists
	autostartPath := filepath.Join(xdg.ConfigHome, "autostart", "zaparoo.desktop")
	if _, err := os.Stat(autostartPath); !os.IsNotExist(err) {
		_ = os.Remove(autostartPath) // Don't fail if this doesn't work
	}

	return nil
}

// UninstallService removes systemd user service.
// Does not remove application files. Must NOT be run as root.
func UninstallService() error {
	return doUninstallService(&helpers.RealCommandExecutor{})
}

// doUninstallService is the internal testable implementation.
func doUninstallService(cmd helpers.CommandExecutor) error {
	if os.Geteuid() == 0 {
		return errors.New("service uninstall must not be run as root")
	}

	// Stop and disable systemd service if running
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	_ = cmd.Run(ctx, "systemctl", "--user", "stop", "zaparoo")
	_ = cmd.Run(ctx, "systemctl", "--user", "disable", "zaparoo")
	cancel()

	// Remove systemd service file
	servicePath := filepath.Join(xdg.ConfigHome, "systemd", "user", "zaparoo.service")
	if _, err := os.Stat(servicePath); !os.IsNotExist(err) {
		if err := os.Remove(servicePath); err != nil {
			return fmt.Errorf("error removing systemd service: %w", err)
		}
	}

	// Reload systemd user daemon
	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	_ = cmd.Run(ctx, "systemctl", "--user", "daemon-reload")
	cancel()

	return nil
}

// UninstallDesktop removes desktop shortcut.
// Does not remove application files. Must NOT be run as root.
func UninstallDesktop() error {
	if os.Geteuid() == 0 {
		return errors.New("desktop uninstall must not be run as root")
	}

	// Remove desktop shortcut
	desktopPath := filepath.Join(xdg.Home, "Desktop", "zaparoo.desktop")
	if _, err := os.Stat(desktopPath); !os.IsNotExist(err) {
		if err := os.Remove(desktopPath); err != nil {
			return fmt.Errorf("error removing desktop shortcut: %w", err)
		}
	}

	return nil
}

// UninstallHardware removes hardware support files.
// Must be run as root.
func UninstallHardware() error {
	return doUninstallHardware(&helpers.RealCommandExecutor{})
}

// doUninstallHardware is the internal testable implementation.
func doUninstallHardware(cmd helpers.CommandExecutor) error {
	if os.Geteuid() != 0 {
		return errors.New("hardware uninstall must be run as root")
	}

	// remove modprobe blacklist
	if _, err := os.Stat(modprobePath); !os.IsNotExist(err) {
		err = os.Remove(modprobePath)
		if err != nil {
			return fmt.Errorf("error removing modprobe blacklist: %w", err)
		}
		// this is just for convenience, don't care too much if it fails
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = cmd.Run(ctx, "systemctl", "restart", "systemd-modules-load.service")
		cancel()
	}

	// remove udev rules
	if _, err := os.Stat(udevPath); !os.IsNotExist(err) {
		err = os.Remove(udevPath)
		if err != nil {
			return fmt.Errorf("error removing udev rules: %w", err)
		}
		// these are just for convenience, don't care too much if they fail
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = cmd.Run(ctx, "udevadm", "control", "--reload-rules")
		_ = cmd.Run(ctx, "udevadm", "trigger")
		cancel()
	}

	return nil
}
