//go:build linux

package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
)

// TODO: allow updating if files have changed

//go:embed conf/zaparoo.service
var serviceFile string

//go:embed conf/blacklist-zaparoo.conf
var modprobeFile string

//go:embed conf/60-zaparoo.rules
var udevFile string

const (
	servicePath  = "/etc/systemd/system/zaparoo.service"
	modprobePath = "/etc/modprobe.d/blacklist-zaparoo.conf"
	udevPath     = "/etc/udev/rules.d/60-zaparoo.rules"
)

func install() error {
	// install and prep systemd service
	if _, err := os.Stat(servicePath); os.IsNotExist(err) {
		exe, err := os.Executable()
		if err != nil {
			exe = "/home/deck/zaparoo/" + config.AppName
		}
		serviceFile = strings.ReplaceAll(serviceFile, "%%EXEC%%", exe)
		serviceFile = strings.ReplaceAll(serviceFile, "%%WORKING%%", filepath.Dir(exe))

		//nolint:gosec // System service file needs to be readable
		err = os.WriteFile(servicePath, []byte(serviceFile), 0o644)
		if err != nil {
			return fmt.Errorf("failed to write service file: %w", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err = exec.CommandContext(ctx, "systemctl", "daemon-reload").Run()
		if err != nil {
			return fmt.Errorf("failed to reload systemd daemon: %w", err)
		}
		err = exec.CommandContext(ctx, "systemctl", "enable", "zaparoo").Run()
		if err != nil {
			return fmt.Errorf("failed to enable zaparoo service: %w", err)
		}
	}

	// install udev rules and refresh
	if _, err := os.Stat(udevPath); os.IsNotExist(err) {
		err = os.WriteFile(udevPath, []byte(udevFile), 0o644) //nolint:gosec // udev rules need to be readable by system
		if err != nil {
			return fmt.Errorf("failed to write udev rules: %w", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err = exec.CommandContext(ctx, "udevadm", "control", "--reload-rules").Run()
		if err != nil {
			return fmt.Errorf("failed to reload udev rules: %w", err)
		}
		err = exec.CommandContext(ctx, "udevadm", "trigger").Run()
		if err != nil {
			return fmt.Errorf("failed to trigger udev: %w", err)
		}
	}

	// install modprobe blacklist
	if _, err := os.Stat(modprobePath); os.IsNotExist(err) {
		//nolint:gosec // modprobe config needs to be readable by system
		err = os.WriteFile(modprobePath, []byte(modprobeFile), 0o644)
		if err != nil {
			return fmt.Errorf("failed to write modprobe config: %w", err)
		}
	}

	return nil
}

func uninstall() error {
	if _, err := os.Stat(servicePath); !os.IsNotExist(err) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err = exec.CommandContext(ctx, "systemctl", "disable", "zaparoo").Run()
		if err != nil {
			return fmt.Errorf("failed to disable zaparoo service: %w", err)
		}
		err = exec.CommandContext(ctx, "systemctl", "stop", "zaparoo").Run()
		if err != nil {
			return fmt.Errorf("failed to stop zaparoo service: %w", err)
		}
		err = exec.CommandContext(ctx, "systemctl", "daemon-reload").Run()
		if err != nil {
			return fmt.Errorf("failed to reload systemd daemon: %w", err)
		}
		err = os.Remove(servicePath)
		if err != nil {
			return fmt.Errorf("failed to remove service file: %w", err)
		}
	}

	if _, err := os.Stat(modprobePath); !os.IsNotExist(err) {
		err = os.Remove(modprobePath)
		if err != nil {
			return fmt.Errorf("failed to remove modprobe config: %w", err)
		}
	}

	if _, err := os.Stat(udevPath); !os.IsNotExist(err) {
		err = os.Remove(udevPath)
		if err != nil {
			return fmt.Errorf("failed to remove udev rules: %w", err)
		}
	}

	return nil
}
