//go:build windows

// Zaparoo Core
// Copyright (c) 2026 The Zaparoo Project Contributors.
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

package windows

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

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/gamelistxml"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/scraper/localmedia"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/syncutil"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	platformids "github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/ids"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/deverr"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esapi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/esde"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/kodi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/steam/steamtracker"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/windows/windowfocus"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/acr122pcsc"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/externaldrive"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/file"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/mqtt"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/pn532"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/rs232barcode"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/simpleserial"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/tty2oled"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/idle"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	"github.com/adrg/xdg"
	"github.com/rs/zerolog/log"
)

type processWindowFocuser interface {
	Focus(context.Context, uint32) error
}

type Platform struct {
	activeMedia             func() *models.ActiveMedia
	setActiveMedia          func(*models.ActiveMedia)
	customPlatformToSystem  map[string]string
	systemToCustomPlatforms map[string][]string
	trackedProcess          *os.Process
	completedTrackedProcess *os.Process
	launchBoxPipe           *LaunchBoxPipeServer
	steamTracker            *steamtracker.WindowsPlatformIntegration
	launcherManager         platforms.LauncherContextManager
	windowFocuser           processWindowFocuser
	windowFocusCancel       context.CancelFunc
	lastLauncher            platforms.Launcher
	processMu               syncutil.RWMutex
	platformMappingsMu      syncutil.RWMutex
	launchBoxPipeLock       syncutil.Mutex
}

const errWindowsInvalidParameter syscall.Errno = 87

var retroBatLaunchSettleDelay = 2 * time.Second

func (*Platform) ID() string {
	return platformids.Windows
}

func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	allReaders := []readers.Reader{
		pn532.NewReader(cfg),
		file.NewReader(cfg),
		simpleserial.NewReader(cfg),
		rs232barcode.NewReader(cfg),
		acr122pcsc.NewAcr122Pcsc(cfg),
		tty2oled.NewReader(cfg, p),
		mqtt.NewReader(cfg),
		externaldrive.NewReader(cfg),
	}

	var enabled []readers.Reader
	for _, r := range allReaders {
		metadata := r.Metadata()
		driver := config.DriverInfo{
			ID:                metadata.ID,
			DefaultEnabled:    metadata.DefaultEnabled,
			DefaultAutoDetect: metadata.DefaultAutoDetect,
		}
		if cfg.IsReaderEnabled(driver, config.ReaderEnableContextCandidate) {
			enabled = append(enabled, r)
		}
	}
	return enabled
}

func (*Platform) StartPre(_ *config.Instance) error {
	return nil
}

func (p *Platform) StartPost(
	_ context.Context,
	cfg *config.Instance,
	launcherManager platforms.LauncherContextManager,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
	_ *database.Database,
	_ *idle.Scheduler,
) error {
	p.activeMedia = activeMedia
	p.setActiveMedia = setActiveMedia
	p.launcherManager = launcherManager
	p.windowFocuser = windowfocus.New()

	// Initialize LaunchBox pipe server if LaunchBox is installed
	p.initLaunchBoxPipe(cfg)

	// Start Steam tracker for external Steam game detection
	p.steamTracker = steamtracker.NewWindowsPlatformIntegration(
		p.SetTrackedProcess,
		activeMedia,
		setActiveMedia,
	)
	if err := p.steamTracker.Start(); err != nil {
		log.Warn().Err(err).Msg("steam game tracker failed to start")
	}

	return nil
}

func (p *Platform) Stop() error {
	if p.launcherManager != nil {
		p.launcherManager.NewContext()
	}

	// Stop Steam tracker
	if p.steamTracker != nil {
		p.steamTracker.Stop()
	}

	// Stop LaunchBox named pipe server
	p.launchBoxPipeLock.Lock()
	if p.launchBoxPipe != nil {
		p.launchBoxPipe.Stop()
		p.launchBoxPipe = nil
	}
	p.launchBoxPipeLock.Unlock()

	return nil
}

func (*Platform) ScanHook(_ *tokens.Token) error {
	return nil
}

func (*Platform) RootDirs(cfg *config.Instance) []string {
	return cfg.IndexRoots()
}

func (*Platform) Settings() platforms.Settings {
	return platforms.Settings{
		DataDir:    filepath.Join(xdg.DataHome, config.AppName),
		ConfigDir:  filepath.Join(xdg.ConfigHome, config.AppName),
		TempDir:    filepath.Join(os.TempDir(), config.AppName),
		LogDir:     filepath.Join(xdg.DataHome, config.AppName, config.LogsDir),
		ZipsAsDirs: false,
	}
}

func (p *Platform) SetTrackedProcess(proc *os.Process) {
	p.processMu.Lock()
	if p.trackedProcess != nil && proc != nil && p.trackedProcess.Pid == proc.Pid {
		p.processMu.Unlock()
		return
	}

	if p.windowFocusCancel != nil {
		p.windowFocusCancel()
		p.windowFocusCancel = nil
	}
	if p.trackedProcess != nil {
		if err := p.trackedProcess.Kill(); err != nil && !isProcessFinishedError(err) {
			log.Warn().Err(err).Msg("failed to kill previous tracked process")
		}
	}

	p.trackedProcess = proc
	p.completedTrackedProcess = nil
	focuser := p.windowFocuser
	if proc == nil || focuser == nil {
		p.processMu.Unlock()
		log.Debug().Msgf("set tracked process: %v", proc)
		return
	}

	focusCtx := context.Background()
	if p.launcherManager != nil {
		if ctx := p.launcherManager.GetContext(); ctx != nil {
			focusCtx = ctx
		}
	}
	focusCtx, p.windowFocusCancel = context.WithCancel(focusCtx)
	p.processMu.Unlock()

	log.Debug().Msgf("set tracked process: %v", proc)
	pid := uint32(proc.Pid) //nolint:gosec // Windows process IDs are 32-bit values
	go func() {
		if err := focuser.Focus(focusCtx, pid); err != nil && !errors.Is(err, context.Canceled) {
			log.Debug().Err(err).Int("pid", proc.Pid).Msg("launched process window was not focused")
		}
	}()
}

func isProcessFinishedError(err error) bool {
	return errors.Is(err, os.ErrProcessDone) || errors.Is(err, errWindowsInvalidParameter)
}

// WaitTrackedProcess waits for proc and removes its stale process handle when it exits.
func (p *Platform) WaitTrackedProcess(proc *os.Process) error {
	_, err := proc.Wait()

	p.processMu.Lock()
	if p.trackedProcess == proc {
		p.trackedProcess = nil
		p.completedTrackedProcess = proc
	}
	p.processMu.Unlock()

	if err != nil {
		return fmt.Errorf("wait for tracked process: %w", err)
	}
	return nil
}

// ClearTrackedProcessMedia clears active media only when proc is still the latest completed launch.
func (p *Platform) ClearTrackedProcessMedia(proc *os.Process) bool {
	p.processMu.Lock()
	if p.completedTrackedProcess != proc || p.trackedProcess != nil {
		p.processMu.Unlock()
		return false
	}
	p.completedTrackedProcess = nil
	p.processMu.Unlock()

	if p.setActiveMedia != nil {
		p.setActiveMedia(nil)
	}
	return true
}

func (p *Platform) setLastLauncher(l *platforms.Launcher) {
	p.processMu.Lock()
	defer p.processMu.Unlock()
	p.lastLauncher = *l
}

func (p *Platform) StopActiveLauncher(_ platforms.StopIntent) error {
	if p.launcherManager != nil {
		p.launcherManager.NewContext()
	}

	p.processMu.Lock()

	customKill := p.lastLauncher.Kill
	p.lastLauncher = platforms.Launcher{}
	p.completedTrackedProcess = nil

	if customKill != nil {
		p.trackedProcess = nil
		p.processMu.Unlock()
		log.Debug().Msg("using custom Kill function for launcher")
		if err := customKill(&config.Instance{}); err != nil {
			log.Warn().Err(err).Msg("custom Kill function failed")
		}
	} else {
		// Kill tracked process if exists
		if p.trackedProcess != nil {
			if err := p.trackedProcess.Kill(); err != nil && !isProcessFinishedError(err) {
				log.Warn().Err(err).Msg("failed to kill tracked process")
			}
			p.trackedProcess = nil
			log.Debug().Msg("killed tracked process")
		}
		p.processMu.Unlock()
	}

	if p.setActiveMedia != nil {
		p.setActiveMedia(nil)
	}
	return nil
}

func (*Platform) ReturnToMenu() error {
	// No menu concept on this platform
	return nil
}

func (*Platform) LaunchSystem(_ *config.Instance, _ string) error {
	return errors.New("launching systems is not supported")
}

func (p *Platform) LaunchMedia(
	cfg *config.Instance, path string, launcher *platforms.Launcher,
	db *database.Database, opts *platforms.LaunchOptions,
) error {
	log.Info().Msgf("launch media: %s", path)

	if launcher == nil {
		foundLauncher, err := helpers.FindLauncher(cfg, p, path)
		if err != nil {
			return fmt.Errorf("launch media: error finding launcher: %w", err)
		}
		launcher = &foundLauncher
	}

	log.Info().Msgf("launch media: using launcher %s for: %s", launcher.ID, path)

	if isRetroBatLauncher(launcher) {
		_, running, runningErr := esapi.APIRunningGame()
		if runningErr != nil {
			log.Warn().Err(runningErr).Msg("RetroBat ES API unavailable, assuming no game is running")
		} else if running {
			log.Info().Msg("exiting current RetroBat media")
			if stopErr := p.StopActiveLauncher(platforms.StopForPreemption); stopErr != nil {
				return fmt.Errorf("failed to stop active RetroBat launcher: %w", stopErr)
			}
			time.Sleep(retroBatLaunchSettleDelay)
		}
	}

	err := platforms.DoLaunch(&platforms.LaunchParams{
		Config:         cfg,
		Platform:       p,
		SetActiveMedia: p.setActiveMedia,
		Launcher:       launcher,
		Path:           path,
		DB:             db,
		Options:        opts,
	}, helpers.GetPathName)
	if err != nil {
		return fmt.Errorf("launch media: error launching: %w", err)
	}

	p.setLastLauncher(launcher)

	return nil
}

func (*Platform) KeyboardPress(_ string) error {
	return nil
}

func (*Platform) GamepadPress(_ string) error {
	return nil
}

func (*Platform) Screenshot() (*platforms.ScreenshotResult, error) {
	return nil, platforms.ErrNotSupported
}

func (*Platform) ForwardCmd(_ *platforms.CmdEnv) (platforms.CmdResult, error) {
	return platforms.CmdResult{}, nil
}

func (*Platform) LookupMapping(_ *tokens.Token) (string, bool) {
	return "", false
}

func (p *Platform) Launchers(cfg *config.Instance) []platforms.Launcher {
	const staticLauncherCount = 14
	launchers := make([]platforms.Launcher, 0, staticLauncherCount+len(esde.SystemMap))

	launchers = append(launchers,
		kodi.NewKodiLocalLauncher(),
		kodi.NewKodiMovieLauncher(),
		kodi.NewKodiTVLauncher(),
		kodi.NewKodiMusicLauncher(),
		kodi.NewKodiSongLauncher(),
		kodi.NewKodiAlbumLauncher(),
		kodi.NewKodiArtistLauncher(),
		kodi.NewKodiTVShowLauncher(),
		steam.NewSteamLauncher(steam.DefaultWindowsOptions()),
		platforms.Launcher{
			ID:       "Flashpoint",
			SystemID: systemdefs.SystemPC,
			Schemes:  []string{shared.SchemeFlashpoint},
			Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
				// Handle native Flashpoint URL format: flashpoint://run/123
				// Normalize to standard virtual path format: flashpoint://123
				if strings.HasPrefix(path, "flashpoint://run/") {
					path = strings.Replace(path, "flashpoint://run/", "flashpoint://", 1)
				}

				id, err := virtualpath.ExtractSchemeID(path, shared.SchemeFlashpoint)
				if err != nil {
					return nil, fmt.Errorf("failed to extract Flashpoint game ID from path: %w", err)
				}

				//nolint:gosec // Safe: launches Flashpoint with game ID from internal database
				cmd := exec.CommandContext(context.Background(),
					helpers.ComSpec(), "/c",
					"start",
					"flashpoint://run/"+id,
				)
				cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
				err = cmd.Start()
				if err != nil {
					return nil, fmt.Errorf("failed to start flashpoint: %w", err)
				}
				return nil, nil //nolint:nilnil // Flashpoint launches don't return a process handle
			},
		},
		platforms.Launcher{
			ID:        "WebBrowser",
			Schemes:   []string{"http", "https"},
			Lifecycle: platforms.LifecycleFireAndForget,
			Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
				//nolint:gosec // Safe: opens URL in default browser via cmd start
				cmd := exec.CommandContext(context.Background(),
					helpers.ComSpec(), "/c",
					"start",
					path,
				)
				cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
				err := cmd.Start()
				if err != nil {
					return nil, fmt.Errorf("failed to open URL in browser: %w", err)
				}
				return nil, nil //nolint:nilnil // Browser launches don't return a process handle
			},
		},
		platforms.Launcher{
			ID:            "GenericExecutable",
			Extensions:    []string{".exe"},
			AllowListOnly: true,
			Lifecycle:     platforms.LifecycleBlocking,
			Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
				//nolint:gosec // Safe: executes user-configured allow-listed executable
				cmd := exec.CommandContext(context.Background(), path)
				cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
				if err := cmd.Start(); err != nil {
					return nil, fmt.Errorf("failed to start executable: %w", err)
				}
				return cmd.Process, nil
			},
		},
		platforms.Launcher{
			ID:            "GenericScript",
			Extensions:    []string{".bat", ".cmd", ".lnk", ".a3x", ".ahk"},
			AllowListOnly: true,
			Lifecycle:     platforms.LifecycleFireAndForget,
			Launch: func(_ *config.Instance, path string, _ *platforms.LaunchOptions) (*os.Process, error) {
				ext := strings.ToLower(filepath.Ext(path))
				var cmd *exec.Cmd
				// Extensions not in default PATHEXT need START command for proper execution
				if ext == ".lnk" || ext == ".a3x" || ext == ".ahk" {
					//nolint:gosec // Safe: executes user-configured allow-listed script
					cmd = exec.CommandContext(context.Background(), helpers.ComSpec(), "/c", "start", "", path)
				} else {
					// .bat, .cmd work fine with direct execution
					//nolint:gosec // Safe: executes user-configured allow-listed script
					cmd = exec.CommandContext(context.Background(), helpers.ComSpec(), "/c", path)
				}
				cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
				err := cmd.Start()
				if err != nil {
					return nil, fmt.Errorf("failed to start script: %w", err)
				}
				return nil, nil //nolint:nilnil // Script launches don't return a process handle
			},
		},
		p.NewLaunchBoxLauncher(),
	)

	launchers = append(launchers, getRetroBatLaunchers()...)

	launchers = append(launchers, deverr.GetDevErrLaunchers()...)

	return append(helpers.ParseCustomLaunchers(p, cfg.CustomLaunchers()), launchers...)
}

func (*Platform) ConsoleManager() platforms.ConsoleManager {
	return platforms.NoOpConsoleManager{}
}

func (*Platform) ManagedByPackageManager() bool {
	return false
}

func (*Platform) Scrapers(_ *config.Instance) map[string]platforms.Scraper {
	gamelist := gamelistxml.NewPlatformScraper()
	media := localmedia.NewPlatformScraper()
	return map[string]platforms.Scraper{gamelist.ID: gamelist, media.ID: media}
}
