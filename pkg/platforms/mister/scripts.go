//go:build linux || darwin

package mister

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/wizzomafizzo/mrext/pkg/input"
)

func openConsole(kbd input.Keyboard, vt string) error {
	getTty := func() (string, error) {
		sys := "/sys/devices/virtual/tty/tty0/active"
		if _, err := os.Stat(sys); err != nil {
			return "", err
		}

		tty, err := os.ReadFile(sys)
		if err != nil {
			return "", err
		}

		return strings.TrimSpace(string(tty)), nil
	}

	// we use the F9 key as a means to disable main's usage of the framebuffer and allow scripts to run
	// unfortunately when the menu "sleeps", any key press will be eaten by main and not trigger the console switch
	// there's also no simple way to tell if mister has switched to the console
	// so what we do is switch to tty3, which is unused by mister, then attempt to switch to console,
	// which sets tty to 1 on success, then check in a loop if it actually did change to 1 and keep pressing F9
	// until it's switched

	err := exec.Command("chvt", vt).Run()
	if err != nil {
		return err
	}

	tries := 0
	tty := ""
	for {
		if tries > 10 {
			return fmt.Errorf("could not switch to tty1")
		}
		kbd.Console()
		time.Sleep(50 * time.Millisecond)
		tty, err = getTty()
		if err != nil {
			return err
		}
		if tty == "tty1" {
			break
		}
		tries++
	}

	return nil
}

func runScript(pl Platform, bin string, args string, hidden bool) error {
	if _, err := os.Stat(bin); err != nil {
		return err
	}

	if pl.GetActiveLauncher() != "" {
		// menu must be open to switch tty and launch script
		log.Debug().Msg("killing launcher...")
		err := pl.KillLauncher()
		if err != nil {
			return err
		}
		time.Sleep(2 * time.Second)
	}

	if !hidden {
		err := openConsole(pl.kbd, "3")
		if err != nil {
			hidden = true
			log.Warn().Msg("error opening console, running script headless")
		}
	}

	if !hidden {
		// this is just to follow mister's convention, which reserves
		// tty2 for scripts
		err := exec.Command("chvt", "2").Run()
		if err != nil {
			return err
		}

		// this is how mister launches scripts itself
		launcher := fmt.Sprintf(`#!/bin/bash
export LC_ALL=en_US.UTF-8
export HOME=/root
export LESSKEY=/media/fat/linux/lesskey
export ZAPAROO_RUN_SCRIPT=1
cd $(dirname "%s")
%s
`, bin, bin+" "+args)

		err = os.WriteFile("/tmp/script", []byte(launcher), 0755)
		if err != nil {
			return err
		}

		err = exec.Command(
			"/sbin/agetty",
			"-a",
			"root",
			"-l",
			"/tmp/script",
			"--nohostname",
			"-L",
			"tty2",
			"linux",
		).Run()
		if err != nil {
			return err
		}

		pl.kbd.ExitConsole()

		return nil
	} else {
		cmd := exec.Command(bin, args)
		cmd.Env = os.Environ()
		cmd.Env = append(cmd.Env, "LC_ALL=en_US.UTF-8")
		cmd.Env = append(cmd.Env, "HOME=/root")
		cmd.Env = append(cmd.Env, "LESSKEY=/media/fat/linux/lesskey")
		cmd.Env = append(cmd.Env, "ZAPAROO_RUN_SCRIPT=1")
		cmd.Dir = filepath.Dir(bin)
		return cmd.Run()
	}
}

func echoFile(path string, s string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}

	_, err = f.WriteString(s)
	if err != nil {
		return err
	}

	return f.Close()
}

func writeTty(id string, s string) error {
	tty := "/dev/tty" + id
	return echoFile(tty, s)
}

func cleanConsole(vt string) error {
	err := writeTty(vt, "\033[?25l")
	if err != nil {
		return err
	}

	err = echoFile("/sys/class/graphics/fbcon/cursor_blink", "0")
	if err != nil {
		return err
	}

	return writeTty(vt, "\033[?17;0;0c")
}

func restoreConsole(vt string) error {
	err := writeTty(vt, "\033[?25h")
	if err != nil {
		return err
	}

	return echoFile("/sys/class/graphics/fbcon/cursor_blink", "1")
}
