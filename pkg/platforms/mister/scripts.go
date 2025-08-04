//go:build linux

package mister

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/rs/zerolog/log"
)

func getTTY() (string, error) {
	sys := "/sys/devices/virtual/tty/tty0/active"
	if _, err := os.Stat(sys); err != nil {
		return "", fmt.Errorf("failed to stat tty active file: %w", err)
	}

	tty, err := os.ReadFile(sys)
	if err != nil {
		return "", fmt.Errorf("failed to read tty active file: %w", err)
	}

	return strings.TrimSpace(string(tty)), nil
}

func scriptIsActive() bool {
	cmd := exec.CommandContext(context.Background(), "bash", "-c", "ps ax | grep [/]tmp/script")
	output, err := cmd.Output()
	if err != nil {
		// grep returns an error code if there was no result
		return false
	}
	return strings.TrimSpace(string(output)) != ""
}

func openConsole(pl platforms.Platform, vt string) error {
	// we use the F9 key as a means to disable main's usage of the framebuffer and
	// allow scripts to run unfortunately when the menu "sleeps". any key press will
	// be eaten by main and not trigger the console switch. there's also no simple way
	// to tell if mister has switched to the console. so what we do is switch to tty3,
	// which is unused by mister. then attempt to switch to console, which sets tty
	// to 1 on success. then check in a loop if it actually did change to 1 and keep
	// pressing F9 until it's switched

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := exec.CommandContext(ctx, "chvt", vt).Run()
	if err != nil {
		log.Debug().Err(err).Msg("open console: error running chvt")
		return fmt.Errorf("failed to run chvt: %w", err)
	}

	tries := 0
	tty := ""
	for {
		if tries > 10 {
			return errors.New("open console: could not switch to tty1")
		}
		// switch to console
		err := pl.KeyboardPress("{f9}")
		if err != nil {
			return fmt.Errorf("failed to press F9 key: %w", err)
		}
		time.Sleep(50 * time.Millisecond)
		tty, err = getTTY()
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

func runScript(pl *Platform, bin, args string, hidden bool) error {
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("failed to stat script file: %w", err)
	}

	active := scriptIsActive()
	if active {
		return errors.New("a script is already running")
	}

	if hidden {
		// run the script directly
		cmd := exec.CommandContext(context.Background(), bin, args)
		cmd.Env = os.Environ()
		cmd.Env = append(cmd.Env, "LC_ALL=en_US.UTF-8", "HOME=/root",
			"LESSKEY=/media/fat/linux/lesskey", "ZAPAROO_RUN_SCRIPT=1")
		cmd.Dir = filepath.Dir(bin)
		err := cmd.Run()
		if err != nil {
			return fmt.Errorf("failed to run script: %w", err)
		}
		return nil
	}

	if pl.activeMedia() != nil {
		// menu must be open to switch tty and launch script
		log.Debug().Msg("killing launcher...")
		err := pl.StopActiveLauncher()
		if err != nil {
			return err
		}
		// wait for menu core
		time.Sleep(1 * time.Second)
	}

	// run it on-screen like a regular script
	err := openConsole(pl, "3")
	if err != nil {
		log.Error().Err(err).Msg("error opening console for script")
	}

	scriptPath := "/tmp/script"
	vt := "2"
	runScript := "1"
	// TODO: these shouldn't be hardcoded
	log.Debug().Msgf("bin: %s", bin)
	log.Debug().Msgf("args: %s", args)
	if strings.HasSuffix(bin, "/zaparoo.sh") && strings.HasPrefix(args, "'-show-") {
		// launching widgets, so we'll use a different tty and script name
		// to avoid the active script check (widgets handle this)
		log.Debug().Msg("widget launched, changing params")
		scriptPath = "/tmp/widget_script"
		vt = "4"
		runScript = "2"
	}

	// this is just to follow mister's convention, which reserves
	// tty2 for scripts
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = exec.CommandContext(ctx, "chvt", vt).Run()
	if err != nil {
		return fmt.Errorf("failed to switch to tty %s: %w", vt, err)
	}

	// this is how mister launches scripts itself
	launcher := fmt.Sprintf(`#!/bin/bash
export LC_ALL=en_US.UTF-8
export HOME=/root
export LESSKEY=/media/fat/linux/lesskey
export ZAPAROO_RUN_SCRIPT=%s
cd $(dirname "%s")
%s
`, runScript, bin, bin+" "+args)

	err = os.WriteFile(scriptPath, []byte(launcher), 0o750) //nolint:gosec // Script file needs execute permissions
	if err != nil {
		return fmt.Errorf("failed to write script file: %w", err)
	}

	cmd := exec.CommandContext(
		context.Background(),
		"/sbin/agetty",
		"-a",
		"root",
		"-l",
		scriptPath,
		"--nohostname",
		"-L",
		"tty"+vt,
		"linux",
	)

	exit := func() {
		if pl.activeMedia() != nil {
			return
		}
		// exit console
		err = pl.KeyboardPress("{f12}")
		if err != nil {
			return
		}
	}

	err = cmd.Run()
	if err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) || exitError.ExitCode() != 2 {
			exit()
		}
		return fmt.Errorf("failed to run script command: %w", err)
	}

	exit()
	return nil
}

func echoFile(path, s string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0) //nolint:gosec // Internal path for script output
	if err != nil {
		return fmt.Errorf("failed to open file for echo: %w", err)
	}

	_, err = f.WriteString(s)
	if err != nil {
		return fmt.Errorf("failed to write to file: %w", err)
	}

	err = f.Close()
	if err != nil {
		return fmt.Errorf("failed to close file: %w", err)
	}
	return nil
}

func writeTty(id, s string) error {
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
