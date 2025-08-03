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
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
)

//go:embed conf/blacklist-zaparoo.conf
var modprobeFile string

//go:embed conf/60-zaparoo.rules
var udevFile string

const (
	modprobePath = "/etc/modprobe.d/blacklist-zaparoo.conf"
	udevPath     = "/etc/udev/rules.d/60-zaparoo.rules"
	installMsg   = `Zaparoo will perform the following steps if required:
- Add udev rules which allow user access to common NFC reader devices and
  create virtual keyboards/gamepads.
- Block certain NFC kernel modules from loading that prevent access to much
  more common readers.

These steps are safe and can be reverted with the uninstall command.
You may need to reboot for the changes to take effect or unplug and replug any
NFC readers that were already connected.

Continue with install?`
)

func CLIInstall() error {
	if !helpers.YesNoPrompt(installMsg, true) {
		_, _ = fmt.Println("Aborting install.")
		return nil
	}
	err := Install()
	if err != nil {
		_, _ = fmt.Println("Error during install:", err)
		return err
	}
	_, _ = fmt.Println("Install complete. You may need to reboot for changes to take effect.")
	return nil
}

func Install() error {
	if os.Geteuid() != 0 {
		return errors.New("install must be run as root")
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
		_ = exec.CommandContext(ctx, "udevadm", "control", "--reload-rules").Run()
		_ = exec.CommandContext(ctx, "udevadm", "trigger").Run()
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
		_ = exec.CommandContext(ctx, "systemctl", "restart", "systemd-modules-load.service").Run()
		cancel()
	}

	return nil
}

func CLIUninstall() error {
	err := Uninstall()
	if err != nil {
		_, _ = fmt.Println("Error during uninstall:", err)
		return err
	}
	_, _ = fmt.Println("Uninstall complete. You may need to reboot for changes to take effect.")
	return nil
}

func Uninstall() error {
	if os.Geteuid() != 0 {
		return errors.New("uninstall must be run as root")
	}

	// remove modprobe blacklist
	if _, err := os.Stat(modprobePath); !os.IsNotExist(err) {
		err = os.Remove(modprobePath)
		if err != nil {
			return fmt.Errorf("error removing modprobe blacklist: %w", err)
		}
		// this is just for convenience, don't care too much if it fails
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_ = exec.CommandContext(ctx, "systemctl", "restart", "systemd-modules-load.service").Run()
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
		_ = exec.CommandContext(ctx, "udevadm", "control", "--reload-rules").Run()
		_ = exec.CommandContext(ctx, "udevadm", "trigger").Run()
		cancel()
	}

	return nil
}
