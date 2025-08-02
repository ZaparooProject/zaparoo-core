package main

import (
	_ "embed"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
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

		err = os.WriteFile(servicePath, []byte(serviceFile), 0644)
		if err != nil {
			return err
		}
		err = exec.Command("systemctl", "daemon-reload").Run()
		if err != nil {
			return err
		}
		err = exec.Command("systemctl", "enable", "zaparoo").Run()
		if err != nil {
			return err
		}
	}

	// install udev rules and refresh
	if _, err := os.Stat(udevPath); os.IsNotExist(err) {
		err = os.WriteFile(udevPath, []byte(udevFile), 0644)
		if err != nil {
			return err
		}
		err = exec.Command("udevadm", "control", "--reload-rules").Run()
		if err != nil {
			return err
		}
		err = exec.Command("udevadm", "trigger").Run()
		if err != nil {
			return err
		}
	}

	// install modprobe blacklist
	if _, err := os.Stat(modprobePath); os.IsNotExist(err) {
		err = os.WriteFile(modprobePath, []byte(modprobeFile), 0644)
		if err != nil {
			return err
		}
	}

	return nil
}

func uninstall() error {
	if _, err := os.Stat(servicePath); !os.IsNotExist(err) {
		err = exec.Command("systemctl", "disable", "zaparoo").Run()
		if err != nil {
			return err
		}
		err = exec.Command("systemctl", "stop", "zaparoo").Run()
		if err != nil {
			return err
		}
		err = exec.Command("systemctl", "daemon-reload").Run()
		if err != nil {
			return err
		}
		err = os.Remove(servicePath)
		if err != nil {
			return err
		}
	}

	if _, err := os.Stat(modprobePath); !os.IsNotExist(err) {
		err = os.Remove(modprobePath)
		if err != nil {
			return err
		}
	}

	if _, err := os.Stat(udevPath); !os.IsNotExist(err) {
		err = os.Remove(udevPath)
		if err != nil {
			return err
		}
	}

	return nil
}
