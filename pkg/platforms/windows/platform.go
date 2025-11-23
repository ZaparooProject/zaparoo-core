//go:build windows

// Zaparoo Core
// Copyright (c) 2025 The Zaparoo Project Contributors.
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
	"sync"
	"syscall"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers/virtualpath"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/kodi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/acr122pcsc"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/externaldrive"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/file"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/mqtt"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/pn532"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/pn532uart"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/rs232barcode"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/simpleserial"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/tty2oled"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	widgetmodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/widgets/models"
	"github.com/adrg/xdg"
	"github.com/rs/zerolog/log"
	"golang.org/x/sys/windows/registry"
)

type Platform struct {
	activeMedia       func() *models.ActiveMedia
	setActiveMedia    func(*models.ActiveMedia)
	trackedProcess    *os.Process
	launchBoxPipe     *LaunchBoxPipeServer
	processMu         sync.RWMutex
	launchBoxPipeLock sync.Mutex
}

func (*Platform) ID() string {
	return platforms.PlatformIDWindows
}

func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	allReaders := []readers.Reader{
		pn532.NewReader(cfg),
		pn532uart.NewReader(cfg),
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
		if cfg.IsDriverEnabled(metadata.ID, metadata.DefaultEnabled) {
			enabled = append(enabled, r)
		}
	}
	return enabled
}

func (*Platform) StartPre(_ *config.Instance) error {
	return nil
}

func (p *Platform) StartPost(
	cfg *config.Instance,
	_ platforms.LauncherContextManager,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
	_ *database.Database,
) error {
	p.activeMedia = activeMedia
	p.setActiveMedia = setActiveMedia

	// Initialize LaunchBox pipe server if LaunchBox is installed
	p.initLaunchBoxPipe(cfg)

	return nil
}

func (p *Platform) Stop() error {
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
	defer p.processMu.Unlock()

	// Kill any existing tracked process before setting new one
	if p.trackedProcess != nil {
		if err := p.trackedProcess.Kill(); err != nil {
			log.Warn().Err(err).Msg("failed to kill previous tracked process")
		}
	}

	p.trackedProcess = proc
	log.Debug().Msgf("set tracked process: %v", proc)
}

func (p *Platform) StopActiveLauncher(_ platforms.StopIntent) error {
	p.processMu.Lock()
	defer p.processMu.Unlock()

	// Kill tracked process if exists
	if p.trackedProcess != nil {
		if err := p.trackedProcess.Kill(); err != nil {
			log.Warn().Err(err).Msg("failed to kill tracked process")
		}
		p.trackedProcess = nil
		log.Debug().Msg("killed tracked process")
	}

	p.setActiveMedia(nil)
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
	cfg *config.Instance, path string, launcher *platforms.Launcher, db *database.Database,
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
	err := helpers.DoLaunch(&helpers.LaunchParams{
		Config:         cfg,
		Platform:       p,
		SetActiveMedia: p.setActiveMedia,
		Launcher:       launcher,
		Path:           path,
		DB:             db,
	})
	if err != nil {
		return fmt.Errorf("launch media: error launching: %w", err)
	}

	return nil
}

func (*Platform) KeyboardPress(_ string) error {
	return nil
}

func (*Platform) GamepadPress(_ string) error {
	return nil
}

func (*Platform) ForwardCmd(_ *platforms.CmdEnv) (platforms.CmdResult, error) {
	return platforms.CmdResult{}, nil
}

func (*Platform) LookupMapping(_ *tokens.Token) (string, bool) {
	return "", false
}

func findSteamDir(cfg *config.Instance) string {
	const fallbackPath = "C:\\Program Files (x86)\\Steam"

	// Check for user-configured Steam install directory first
	if def, ok := cfg.LookupLauncherDefaults("Steam"); ok && def.InstallDir != "" {
		if _, err := os.Stat(def.InstallDir); err == nil {
			log.Debug().Msgf("using user-configured Steam directory: %s", def.InstallDir)
			return def.InstallDir
		}
		log.Warn().Msgf("user-configured Steam directory not found: %s", def.InstallDir)
	}

	// Try 64-bit systems first (most common)
	paths := []string{
		`SOFTWARE\Wow6432Node\Valve\Steam`,
		`SOFTWARE\Valve\Steam`,
	}

	for _, path := range paths {
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}

		installPath, _, err := key.GetStringValue("InstallPath")
		if closeErr := key.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("error closing registry key")
		}
		if err != nil {
			continue
		}

		// Validate the path exists
		if _, statErr := os.Stat(installPath); statErr == nil {
			log.Debug().Msgf("found Steam installation via registry: %s", installPath)
			return installPath
		}
	}

	log.Debug().Msgf("Steam registry detection failed, using fallback: %s", fallbackPath)
	return fallbackPath
}

func (p *Platform) Launchers(cfg *config.Instance) []platforms.Launcher {
	launchers := []platforms.Launcher{
		kodi.NewKodiLocalLauncher(),
		kodi.NewKodiMovieLauncher(),
		kodi.NewKodiTVLauncher(),
		kodi.NewKodiMusicLauncher(),
		kodi.NewKodiSongLauncher(),
		kodi.NewKodiAlbumLauncher(),
		kodi.NewKodiArtistLauncher(),
		kodi.NewKodiTVShowLauncher(),
		{
			ID:       "Steam",
			SystemID: systemdefs.SystemPC,
			Schemes:  []string{shared.SchemeSteam},
			Scanner: func(
				_ context.Context,
				cfg *config.Instance,
				_ string,
				results []platforms.ScanResult,
			) ([]platforms.ScanResult, error) {
				steamRoot := findSteamDir(cfg)
				steamAppsRoot := filepath.Join(steamRoot, "steamapps")

				// Scan official Steam apps
				appResults, err := helpers.ScanSteamApps(steamAppsRoot)
				if err != nil {
					return nil, fmt.Errorf("failed to scan Steam apps: %w", err)
				}
				results = append(results, appResults...)

				// Scan non-Steam games (shortcuts)
				shortcutResults, err := helpers.ScanSteamShortcuts(steamRoot)
				if err != nil {
					log.Warn().Err(err).Msg("failed to scan Steam shortcuts, continuing without them")
				} else {
					results = append(results, shortcutResults...)
				}

				return results, nil
			},
			Launch: func(_ *config.Instance, path string) (*os.Process, error) {
				// Handle native Steam URL format: steam://rungameid/123
				// Normalize to standard virtual path format: steam://123
				if strings.HasPrefix(path, "steam://rungameid/") {
					path = strings.Replace(path, "steam://rungameid/", "steam://", 1)
				}

				id, err := virtualpath.ExtractSchemeID(path, shared.SchemeSteam)
				if err != nil {
					return nil, fmt.Errorf("failed to extract Steam game ID from path: %w", err)
				}

				//nolint:gosec // Safe: launches Steam with game ID from internal database
				cmd := exec.CommandContext(context.Background(),
					"cmd", "/c",
					"start",
					"steam://rungameid/"+id,
				)
				cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
				err = cmd.Start()
				if err != nil {
					return nil, fmt.Errorf("failed to start steam: %w", err)
				}
				return nil, nil //nolint:nilnil // Steam launches don't return a process handle
			},
		},
		{
			ID:       "Flashpoint",
			SystemID: systemdefs.SystemPC,
			Schemes:  []string{shared.SchemeFlashpoint},
			Launch: func(_ *config.Instance, path string) (*os.Process, error) {
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
					"cmd", "/c",
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
		{
			ID:        "WebBrowser",
			Schemes:   []string{"http", "https"},
			Lifecycle: platforms.LifecycleFireAndForget,
			Launch: func(_ *config.Instance, path string) (*os.Process, error) {
				cmd := exec.CommandContext(context.Background(),
					"cmd", "/c",
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
		{
			ID:            "GenericExecutable",
			Extensions:    []string{".exe"},
			AllowListOnly: true,
			Lifecycle:     platforms.LifecycleBlocking, // Block for executables to track completion
			Launch: func(_ *config.Instance, path string) (*os.Process, error) {
				cmd := exec.CommandContext(context.Background(), path)
				cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
				if err := cmd.Start(); err != nil {
					return nil, fmt.Errorf("failed to start executable: %w", err)
				}
				return cmd.Process, nil
			},
		},
		{
			ID:            "GenericScript",
			Extensions:    []string{".bat", ".cmd", ".lnk", ".a3x", ".ahk"},
			AllowListOnly: true,
			Lifecycle:     platforms.LifecycleFireAndForget, // Fire-and-forget for scripts
			Launch: func(_ *config.Instance, path string) (*os.Process, error) {
				ext := strings.ToLower(filepath.Ext(path))
				var cmd *exec.Cmd
				// Extensions not in default PATHEXT need START command for proper execution
				if ext == ".lnk" || ext == ".a3x" || ext == ".ahk" {
					cmd = exec.CommandContext(context.Background(), "cmd", "/c", "start", "", path)
				} else {
					// .bat, .cmd work fine with direct execution
					cmd = exec.CommandContext(context.Background(), "cmd", "/c", path)
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
	}

	// Add RetroBat launchers if available
	retroBatLaunchers := getRetroBatLaunchers(cfg)
	launchers = append(launchers, retroBatLaunchers...)

	return append(helpers.ParseCustomLaunchers(p, cfg.CustomLaunchers()), launchers...)
}

func (*Platform) ShowNotice(
	_ *config.Instance,
	_ widgetmodels.NoticeArgs,
) (func() error, time.Duration, error) {
	return nil, 0, platforms.ErrNotSupported
}

func (*Platform) ShowLoader(
	_ *config.Instance,
	_ widgetmodels.NoticeArgs,
) (func() error, error) {
	return nil, platforms.ErrNotSupported
}

func (*Platform) ShowPicker(
	_ *config.Instance,
	_ widgetmodels.PickerArgs,
) error {
	return platforms.ErrNotSupported
}

func (*Platform) ConsoleManager() platforms.ConsoleManager {
	return platforms.NoOpConsoleManager{}
}
