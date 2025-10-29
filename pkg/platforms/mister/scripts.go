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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
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
	// Check if console is already active (for video→video transitions)
	if p, ok := pl.(*Platform); ok {
		p.consoleMu.RLock()
		isActive := p.consoleActive
		p.consoleMu.RUnlock()

		if isActive {
			log.Debug().Msg("console already active, skipping F9 sequence")
			return nil
		}
	}

	// We use the F9 key to signal MiSTer_Main to release the framebuffer and
	// allow Linux console access. F9 triggers video_fb_enable() in Main_MiSTer which:
	// 1. Switches VT using VT_ACTIVATE/VT_WAITACTIVE ioctls
	// 2. Sends SPI commands to FPGA to release framebuffer control
	// 3. Stops OSD rendering when video_chvt(0) != 2
	//
	// Problem: When the menu "sleeps", keypresses can be eaten by Main and not
	// trigger the console switch. We use retry logic with exponential
	// backoff and verification to handle this reliably.

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := exec.CommandContext(ctx, "chvt", vt).Run()
	if err != nil {
		log.Debug().Err(err).Msg("open console: error running chvt")
		return fmt.Errorf("failed to run chvt: %w", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	backoff := 50 * time.Millisecond
	maxBackoff := 500 * time.Millisecond

	for time.Now().Before(deadline) {
		// Press F9 to signal MiSTer_Main to release framebuffer
		err := pl.KeyboardPress("{f9}")
		if err != nil {
			return fmt.Errorf("failed to press F9 key: %w", err)
		}

		time.Sleep(backoff)
		backoff = time.Duration(float64(backoff) * 1.5)
		if backoff > maxBackoff {
			backoff = maxBackoff
		}

		// Verify console mode active by checking VT state
		tty, err := getTTY()
		if err != nil {
			return err
		}
		if tty == "tty"+f9ConsoleVT {
			log.Debug().Msg("console mode confirmed")
			// Wait for framebuffer to be ready
			if err := waitForFramebuffer(time.Until(deadline)); err != nil {
				return err
			}

			// Clean tty1 (where F9 takes us)
			if err := cleanConsole(f9ConsoleVT); err != nil {
				log.Debug().Err(err).Msg("failed to clean tty1")
			}

			// Switch to target VT for video playback
			if vt != f9ConsoleVT {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				if err := exec.CommandContext(ctx, "chvt", vt).Run(); err != nil {
					log.Debug().Err(err).Msgf("failed to switch to tty%s", vt)
				}
			}

			// Set console active flag
			if p, ok := pl.(*Platform); ok {
				p.consoleMu.Lock()
				p.consoleActive = true
				p.consoleMu.Unlock()
			}

			return nil
		}
	}

	return errors.New("open console: timeout waiting for console switch after 5s")
}

func closeConsole(pl platforms.Platform) error {
	// Check if console is already inactive (for FPGA/MGL transitions)
	if p, ok := pl.(*Platform); ok {
		p.consoleMu.RLock()
		isActive := p.consoleActive
		p.consoleMu.RUnlock()

		if !isActive {
			log.Debug().Msg("console already inactive, skipping close")
			return nil
		}

		// Restore console cursor state on both TTYs
		if err := restoreConsole(f9ConsoleVT); err != nil {
			log.Debug().Err(err).Msg("failed to restore tty1 state")
		}
		if launcherConsoleVT != f9ConsoleVT {
			if err := restoreConsole(launcherConsoleVT); err != nil {
				log.Debug().Err(err).Msgf("failed to restore tty%s state", launcherConsoleVT)
			}
		}

		// Press F12 to return to FPGA framebuffer
		if err := pl.KeyboardPress("{f12}"); err != nil {
			return fmt.Errorf("failed to press F12 key: %w", err)
		}

		// Clear console active flag
		p.consoleMu.Lock()
		p.consoleActive = false
		p.consoleMu.Unlock()

		log.Debug().Msg("console closed, returned to FPGA mode")
	}

	return nil
}

// waitForFramebuffer waits for the framebuffer device to become accessible
func waitForFramebuffer(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat("/dev/fb0"); err == nil {
			if _, err := os.Stat("/sys/class/graphics/fbcon/cursor_blink"); err == nil {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return errors.New("framebuffer not ready")
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
		err := pl.StopActiveLauncher(platforms.StopForPreemption)
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
		vt = launcherConsoleVT
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
	// Clear screen and reset
	err := writeTty(vt, "\033[2J\033[H")
	if err != nil {
		return err
	}

	// Disable cursor blinking
	err = echoFile("/sys/class/graphics/fbcon/cursor_blink", "0")
	if err != nil {
		return err
	}

	// Hide cursor
	return writeTty(vt, "\033[?25l")
}

func restoreConsole(vt string) error {
	err := writeTty(vt, "\033[?25h")
	if err != nil {
		return err
	}

	return echoFile("/sys/class/graphics/fbcon/cursor_blink", "1")
}
