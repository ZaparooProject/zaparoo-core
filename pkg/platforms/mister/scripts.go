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
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mistermain"
	"github.com/rs/zerolog/log"
)

const (
	misterScriptPath        = "/tmp/script"
	misterScriptGrepCommand = "ps ax | grep [/]tmp/script"
	misterWidgetScriptPath  = "/tmp/widget_script"
	misterScriptRunFlag     = "1"
	misterWidgetRunFlag     = "2"
)

var (
	getScriptConsoleManager = func(pl *Platform) platforms.ConsoleManager { return pl.ConsoleManager() }
	runScriptChvt           = func(ctx context.Context, vt string) error {
		return exec.CommandContext(ctx, "chvt", vt).Run() //nolint:gosec // Fixed executable; VT is internal.
	}
	writeScriptLauncher = os.WriteFile
	startScriptCommand  = func(cmd *exec.Cmd) error { return cmd.Start() }
)

func scriptIsActive() bool {
	cmd := exec.CommandContext(context.Background(), "bash", "-c", misterScriptGrepCommand)
	output, err := cmd.Output()
	if err != nil {
		// grep returns an error code if there was no result
		return false
	}
	return strings.TrimSpace(string(output)) != ""
}

func isWidgetScript(bin, args string) bool {
	return strings.HasSuffix(bin, "/zaparoo.sh") && strings.HasPrefix(args, "'-show-")
}

func scriptRunMode(bin, args string) (runScript string, widget bool) {
	if isWidgetScript(bin, args) {
		return misterWidgetRunFlag, true
	}
	return misterScriptRunFlag, false
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
		cmd := exec.CommandContext(context.Background(), bin, args) //nolint:gosec // G204: script runner's purpose
		cmd.Env = os.Environ()
		cmd.Env = append(cmd.Env, "LC_ALL=en_US.UTF-8", "HOME=/root",
			"LESSKEY=/media/fat/linux/lesskey", "ZAPAROO_RUN_SCRIPT="+misterScriptRunFlag)
		cmd.Dir = filepath.Dir(bin)
		err := cmd.Run()
		if err != nil {
			return fmt.Errorf("failed to run script: %w", err)
		}
		return nil
	}

	if pl.activeMedia() != nil {
		// Scripts need clean console state (normal resolution, not scaled video mode)
		// Use StopForConsoleReset to ensure menu is opened before console switch
		log.Debug().Msg("stopping active launcher for script...")
		err := pl.StopActiveLauncher(platforms.StopForConsoleReset)
		if err != nil {
			return err
		}

		// Wait for menu core to become active to ensure console state is reset
		menuWaitCtx, menuWaitCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer menuWaitCancel()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		menuReady := false
		for !menuReady {
			select {
			case <-menuWaitCtx.Done():
				return errors.New("timed out waiting for menu core to load")
			case <-ticker.C:
				if mistermain.GetActiveCoreName() == misterconfig.MenuCore {
					menuReady = true
				}
			}
		}
	}

	scriptPath := misterScriptPath
	runScript, widgetScript := scriptRunMode(bin, args)
	vt := scriptConsoleVT
	log.Debug().Msgf("bin: %s", bin)
	log.Debug().Msgf("args: %s", args)
	if widgetScript {
		// launching widgets, so we'll use a different tty and script name
		// to avoid the active script check (widgets handle this)
		log.Debug().Msg("widget launched, changing params")
		scriptPath = misterWidgetScriptPath
		vt = frontendConsoleVT
	}

	// Run it on-screen like a regular script. Use a background context with
	// timeout since scripts are not launcher operations.
	scriptCtx, scriptCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer scriptCancel()
	cm := getScriptConsoleManager(pl)
	err := cm.Open(scriptCtx, vt)
	if err != nil {
		return fmt.Errorf("failed to open console for script: %w", err)
	}
	consoleOwned := true
	defer func() {
		if consoleOwned {
			if closeErr := cm.Close(); closeErr != nil {
				log.Warn().Err(closeErr).Msg("failed to close script console after setup error")
			}
		}
	}()

	// this is just to follow mister's convention, which reserves
	// tty2 for scripts
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	err = runScriptChvt(ctx, vt)
	cancel()
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

	err = writeScriptLauncher(scriptPath, []byte(launcher), 0o750)
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
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	exit := func() {
		if pl.activeMedia() != nil {
			return
		}
		if closeErr := pl.ConsoleManager().Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close script console")
		}
	}

	// Start script non-blocking
	if err := startScriptCommand(cmd); err != nil {
		return fmt.Errorf("failed to start script: %w", err)
	}

	done := make(chan struct{})
	pl.setTrackedProcessWithCleanup(cmd.Process, done, true)

	// This goroutine exclusively owns cmd.Wait and console cleanup.
	go func() {
		defer close(done)
		waitErr := cmd.Wait()
		killRemainingProcessGroup(cmd.Process, true)
		if waitErr != nil {
			log.Debug().Err(waitErr).Msg("script exited")
		} else {
			log.Debug().Msg("script completed normally")
		}
		exit()
		pl.clearTrackedProcess(cmd.Process)
	}()
	consoleOwned = false

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
