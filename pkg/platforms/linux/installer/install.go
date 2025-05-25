package installer

import (
	_ "embed"
	"errors"
	"fmt"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"os"
	"os/exec"
	"path/filepath"
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
	if !utils.YesNoPrompt(installMsg, true) {
		fmt.Println("Aborting install.")
		return nil
	} else {
		err := Install()
		if err != nil {
			fmt.Println("Error during install:", err)
			return err
		}
		fmt.Println("Install complete. You may need to reboot for changes to take effect.")
		return nil
	}
}

func Install() error {
	if os.Geteuid() != 0 {
		return errors.New("install must be run as root")
	}

	// install udev rules
	if _, err := os.Stat(filepath.Dir(udevPath)); os.IsNotExist(err) {
		return errors.New("udev rules directory does not exist")
	} else if _, err := os.Stat(udevPath); os.IsNotExist(err) {
		err = os.WriteFile(udevPath, []byte(udevFile), 0644)
		if err != nil {
			return fmt.Errorf("error creating udev rules: %w", err)
		}
		// these are just for convenience, don't care too much if they fail
		_ = exec.Command("udevadm", "control", "--reload-rules").Run()
		_ = exec.Command("udevadm", "trigger").Run()
	}

	// install modprobe blacklist
	if _, err := os.Stat(filepath.Dir(modprobePath)); os.IsNotExist(err) {
		return errors.New("modprobe directory does not exist")
	} else if _, err := os.Stat(modprobePath); os.IsNotExist(err) {
		err = os.WriteFile(modprobePath, []byte(modprobeFile), 0644)
		if err != nil {
			return fmt.Errorf("error creating modprobe blacklist: %w", err)
		}
		// this is just for convenience, don't care too much if it fails
		_ = exec.Command("systemctl", "restart", "systemd-modules-load.service").Run()
	}

	return nil
}

func CLIUninstall() error {
	err := Uninstall()
	if err != nil {
		fmt.Println("Error during uninstall:", err)
		return err
	}
	fmt.Println("Uninstall complete. You may need to reboot for changes to take effect.")
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
		_ = exec.Command("systemctl", "restart", "systemd-modules-load.service").Run()
	}

	// remove udev rules
	if _, err := os.Stat(udevPath); !os.IsNotExist(err) {
		err = os.Remove(udevPath)
		if err != nil {
			return fmt.Errorf("error removing udev rules: %w", err)
		}
		// these are just for convenience, don't care too much if they fail
		_ = exec.Command("udevadm", "control", "--reload-rules").Run()
		_ = exec.Command("udevadm", "trigger").Run()
	}

	return nil
}
