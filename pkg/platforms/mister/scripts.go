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
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/rs/zerolog/log"
)

func scriptIsActive() bool {
	cmd := exec.CommandContext(context.Background(), "bash", "-c", "ps ax | grep [/]tmp/script")
	output, err := cmd.Output()
	if err != nil {
		// grep returns an error code if there was no result
		return false
	}
	return strings.TrimSpace(string(output)) != ""
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
	err := pl.ConsoleManager().Open("3")
	if err != nil {
		return fmt.Errorf("failed to open console for script: %w", err)
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

	launcherCtx := pl.launcherManager.GetContext()

	cmd := exec.CommandContext(
		context.Background(),
		"setsid",
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

		// Clear console active flag
		pl.consoleManager.mu.Lock()
		pl.consoleManager.active = false
		pl.consoleManager.mu.Unlock()
	}

	// Start script non-blocking
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start script: %w", err)
	}

	// Track process so it can be killed by StopActiveLauncher
	pl.SetTrackedProcess(cmd.Process)

	// Cleanup in goroutine (non-blocking)
	go func() {
		waitErr := cmd.Wait()

		// Check if script was superseded by new launcher
		if launcherCtx.Err() != nil {
			log.Debug().Msg("script cleanup cancelled - launcher superseded")
			return
		}

		// Handle different exit scenarios
		if waitErr != nil {
			// Check if process was killed by signal
			isKilled := false
			exitErr := &exec.ExitError{}
			if errors.As(waitErr, &exitErr) {
				if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
					sig := status.Signal()
					if status.Signaled() && (sig == syscall.SIGKILL || sig == syscall.SIGTERM) {
						isKilled = true
					}
				}
			}

			if isKilled {
				// Process was killed (likely by StopActiveLauncher for new media)
				log.Debug().Msg("script stopped by new media launch")
				pl.SetTrackedProcess(nil)
				return
			}

			// agetty exits with code 2 when it can't find the specified TTY,
			// which can happen during shutdown or preemption
			var exitError *exec.ExitError
			if !errors.As(waitErr, &exitError) || exitError.ExitCode() != 2 {
				log.Debug().Err(waitErr).Msg("script exited with error")
				pl.SetTrackedProcess(nil)
				exit()
			}
		} else {
			log.Debug().Msg("script completed normally")
			pl.SetTrackedProcess(nil)
			exit()
		}
	}()

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
