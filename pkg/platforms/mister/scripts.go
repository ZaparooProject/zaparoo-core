//go:build linux && !android

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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/command"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	misterconfig "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/mister/mistermain"
	"github.com/rs/zerolog/log"
	"github.com/spf13/afero"
)

const (
	misterScriptPath        = "/tmp/script"
	misterScriptGrepCommand = "ps ax | grep [/]tmp/script"
	misterWidgetScriptPath  = "/tmp/widget_script"
	misterScriptRunFlag     = "1"
	misterWidgetRunFlag     = "2"
	scriptsLauncherID       = "Scripts"
	scriptExt               = ".sh"

	// launchOriginEnv tells scripts which frontend launched them. Update All
	// reads it to decide how to hand the console back after a core load. The
	// value comes from the caller's launch_origin_id advarg.
	launchOriginEnv = "LAUNCH_ORIGIN_ID"
)

var (
	checkScriptActive       = scriptIsActive
	getScriptConsoleManager = func(pl *Platform) platforms.ConsoleManager { return pl.ConsoleManager() }
	runScriptChvt           = func(ctx context.Context, vt string) error {
		return exec.CommandContext(ctx, "chvt", vt).Run() //nolint:gosec // Fixed executable; VT is internal.
	}
	writeScriptLauncher          = os.WriteFile
	startScriptCommand           = func(cmd *exec.Cmd) error { return cmd.Start() }
	runHiddenScriptCommand       = func(cmd *exec.Cmd) error { return cmd.Run() }
	killHiddenScriptProcessGroup = func(pid int) error { return syscall.Kill(-pid, syscall.SIGKILL) }
	scriptActiveCoreName         = mistermain.GetActiveCoreName
	scriptFPGAActive             = func(pl *Platform) bool { return pl.isFPGAActive() }
	scriptReturnToMenu           = func(pl *Platform) error { return pl.ReturnToMenu() }
)

func scriptIsActive(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "bash", "-c", misterScriptGrepCommand)
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
	return runScriptContext(context.Background(), pl, bin, args, hidden, "")
}

// runScriptContext exports a non-empty launchOrigin to the script as
// LAUNCH_ORIGIN_ID.
func runScriptContext(
	ctx context.Context,
	pl *Platform,
	bin, args string,
	hidden bool,
	launchOrigin string,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("failed to stat script file: %w", err)
	}

	active := checkScriptActive(ctx)
	if active {
		return platforms.ErrScriptAlreadyRunning
	}

	if hidden {
		// Hidden scripts run synchronously, so the caller's execution lease
		// bounds both process lifetime and any side effects after expiry.
		// args is already shell-quoted, so bash splits it into words exactly
		// as the visible launcher's command line does.
		//nolint:gosec // G204: script runner's purpose
		cmd := exec.CommandContext(ctx, "bash", "-c", `exec "$0" `+args, bin)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Cancel = func() error {
			if cmd.Process == nil {
				return os.ErrProcessDone
			}
			if err := killHiddenScriptProcessGroup(cmd.Process.Pid); err != nil {
				if errors.Is(err, syscall.ESRCH) {
					return os.ErrProcessDone
				}
				return fmt.Errorf("kill hidden script process group: %w", err)
			}
			return nil
		}
		cmd.Env = os.Environ()
		cmd.Env = append(cmd.Env, "LC_ALL=en_US.UTF-8", "HOME=/root",
			"LESSKEY=/media/fat/linux/lesskey", "ZAPAROO_RUN_SCRIPT="+misterScriptRunFlag)
		if launchOrigin != "" {
			// Appended last so it overrides an inherited value.
			cmd.Env = append(cmd.Env, launchOriginEnv+"="+launchOrigin)
		}
		cmd.Dir = filepath.Dir(bin)
		err := runHiddenScriptCommand(cmd)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return fmt.Errorf("failed to run script: %w", ctxErr)
			}
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
		if err := waitForMenuCore(ctx); err != nil {
			return err
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

	// Run it on-screen like a regular script. Bound console setup by both
	// its normal timeout and the caller's execution lease.
	scriptCtx, scriptCancel := context.WithTimeout(ctx, 30*time.Second)
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
	chvtCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	err = runScriptChvt(chvtCtx, vt)
	cancel()
	if err != nil {
		return fmt.Errorf("failed to switch to tty %s: %w", vt, err)
	}

	launchOriginExport := ""
	if launchOrigin != "" {
		launchOriginExport = "export " + launchOriginEnv + "=" + command.ShellQuote(launchOrigin) + "\n"
	}

	// this is how mister launches scripts itself
	launcher := fmt.Sprintf(`#!/bin/bash
export LC_ALL=en_US.UTF-8
export HOME=/root
export LESSKEY=/media/fat/linux/lesskey
export ZAPAROO_RUN_SCRIPT=%s
%scd "$(dirname %s)"
%s
`, runScript, launchOriginExport, command.ShellQuote(bin), command.ShellQuote(bin)+" "+args)

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

	// Start script non-blocking, but never start after the caller's lease has
	// expired. Once started, MiSTer's script process owns its normal lifecycle.
	if err := ctx.Err(); err != nil {
		return err
	}
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

// waitForMenuCore blocks until MiSTer reports the menu core, which is when the
// console is back at its normal resolution.
func waitForMenuCore(ctx context.Context) error {
	menuWaitCtx, menuWaitCancel := context.WithTimeout(ctx, 5*time.Second)
	defer menuWaitCancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-menuWaitCtx.Done():
			return errors.New("timed out waiting for menu core to load")
		case <-ticker.C:
			if scriptActiveCoreName() == misterconfig.MenuCore {
				return nil
			}
		}
	}
}

// createScriptsLauncher exposes the MiSTer Scripts folder as the Scripts
// system. A script launched this way runs on screen with no arguments;
// mister.script is the way to pass arguments or run one hidden.
func createScriptsLauncher(pl *Platform) platforms.Launcher {
	return platforms.Launcher{
		ID:            scriptsLauncherID,
		SystemID:      systemdefs.SystemScript,
		Folders:       []string{misterconfig.ScriptsDir},
		Extensions:    []string{scriptExt},
		NoActiveMedia: true,
		Scanner:       scanScripts(misterconfig.ScriptsDir),
		Launch:        launchScript(pl, misterconfig.ScriptsDir),
	}
}

// scriptRelPath returns path relative to scriptsDir when it names a script
// the Scripts system lists: a .sh file inside the folder with no hidden
// segment, which keeps helper scripts under folders like .config out, and
// not inside an archive, where it could not be run.
func scriptRelPath(scriptsDir, path string) (string, bool) {
	if !filepath.IsAbs(path) || !strings.EqualFold(filepath.Ext(path), scriptExt) {
		return "", false
	}
	rel, err := filepath.Rel(scriptsDir, filepath.Clean(path))
	if err != nil || rel == "." {
		return "", false
	}
	for _, segment := range strings.Split(rel, string(filepath.Separator)) {
		if strings.HasPrefix(segment, ".") || helpers.IsZip(segment) {
			return "", false
		}
	}
	return rel, true
}

func scanScripts(
	scriptsDir string,
) func(context.Context, *config.Instance, string, []platforms.ScanResult) ([]platforms.ScanResult, error) {
	return func(
		_ context.Context, _ *config.Instance, _ string, files []platforms.ScanResult,
	) ([]platforms.ScanResult, error) {
		scripts := make([]platforms.ScanResult, 0, len(files))
		for i := range files {
			if _, ok := scriptRelPath(scriptsDir, files[i].Path); !ok {
				continue
			}
			// A script is known by its file name, so it is listed under
			// exactly that instead of a title parsed out of it.
			script := files[i]
			base := filepath.Base(script.Path)
			script.Name = strings.TrimSuffix(base, filepath.Ext(base))
			scripts = append(scripts, script)
		}
		return scripts, nil
	}
}

func launchScript(
	pl *Platform,
	scriptsDir string,
) func(*config.Instance, string, *platforms.LaunchOptions) (*os.Process, error) {
	return func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
		if _, ok := scriptRelPath(scriptsDir, path); !ok {
			return nil, fmt.Errorf("invalid script: %s", path)
		}
		scriptPath := filepath.Clean(path)
		if err := checkScriptFile(afero.NewOsFs(), scriptPath); err != nil {
			return nil, err
		}

		ctx := context.Background()
		if pl.launcherManager != nil {
			ctx = pl.launcherManager.GetContext()
		}

		// The launch has already stopped tracked media, but a core Main is
		// still running has to go too: a script needs the menu's console.
		if scriptFPGAActive(pl) {
			log.Debug().Msg("FPGA core active, returning to menu before script")
			if err := scriptReturnToMenu(pl); err != nil {
				return nil, fmt.Errorf("failed to return to menu: %w", err)
			}
			if err := waitForMenuCore(ctx); err != nil {
				return nil, err
			}
		}

		// The runner tracks the process and restores the console itself.
		if err := runScriptContext(ctx, pl, scriptPath, "", false, ""); err != nil {
			return nil, err
		}
		return nil, nil //nolint:nilnil // No process handle; the runner owns it.
	}
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
