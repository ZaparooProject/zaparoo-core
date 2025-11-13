//go:build linux

/*
Zaparoo Core
Copyright (C) 2024, 2025 Callan Barrett

This file is part of Zaparoo Core.

Zaparoo Core is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

Zaparoo Core is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with Zaparoo Core.  If not, see <http://www.gnu.org/licenses/>.
*/

package linux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms/shared/kodi"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/externaldrive"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/file"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/libnfc"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/mqtt"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/opticaldrive"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/pn532"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/simpleserial"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/readers/tty2oled"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/service/tokens"
	widgetmodels "github.com/ZaparooProject/zaparoo-core/v2/pkg/ui/widgets/models"
	"github.com/adrg/xdg"
	"github.com/rs/zerolog/log"
)

type Platform struct {
	activeMedia     func() *models.ActiveMedia
	setActiveMedia  func(*models.ActiveMedia)
	launcherManager platforms.LauncherContextManager
	trackedProcess  *os.Process
	processMu       sync.RWMutex
}

func (*Platform) ID() string {
	return platforms.PlatformIDLinux
}

func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	allReaders := []readers.Reader{
		tty2oled.NewReader(cfg, p),
		file.NewReader(cfg),
		simpleserial.NewReader(cfg),
		pn532.NewReader(cfg),
		libnfc.NewACR122Reader(cfg),
		libnfc.NewLegacyUARTReader(cfg),
		libnfc.NewLegacyI2CReader(cfg),
		opticaldrive.NewReader(cfg),
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
	_ *config.Instance,
	launcherManager platforms.LauncherContextManager,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
	_ *database.Database,
) error {
	p.launcherManager = launcherManager
	p.activeMedia = activeMedia
	p.setActiveMedia = setActiveMedia
	return nil
}

func (*Platform) Stop() error {
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
	// Invalidate old launcher context - signals cleanup goroutines they're stale
	if p.launcherManager != nil {
		p.launcherManager.NewContext()
	}

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

	var err error
	if launcher == nil {
		// Auto-detect launcher as before
		var foundLauncher platforms.Launcher
		foundLauncher, err = helpers.FindLauncher(cfg, p, path)
		if err != nil {
			return fmt.Errorf("launch media: error finding launcher: %w", err)
		}
		launcher = &foundLauncher
	}

	log.Info().Msgf("launch media: using launcher %s for: %s", launcher.ID, path)
	err = helpers.DoLaunch(&helpers.LaunchParams{
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
		NewSteamLauncher(),
		{
			ID:        "WebBrowser",
			Schemes:   []string{"http", "https"},
			Lifecycle: platforms.LifecycleFireAndForget,
			Launch: func(_ *config.Instance, path string) (*os.Process, error) {
				cmd := exec.CommandContext(context.Background(), "xdg-open", path)
				err := cmd.Start()
				if err != nil {
					return nil, fmt.Errorf("failed to open URL in browser: %w", err)
				}
				return nil, nil //nolint:nilnil // Browser launches don't return a process handle
			},
		},
		NewLutrisLauncher(),
		{
			ID:       "Heroic",
			SystemID: systemdefs.SystemPC,
			Schemes:  []string{shared.SchemeHeroic},
			Scanner: func(
				_ context.Context,
				_ *config.Instance,
				_ string,
				results []platforms.ScanResult,
			) ([]platforms.ScanResult, error) {
				// Check if Heroic is installed
				_, err := exec.LookPath("heroic")
				if err != nil {
					log.Debug().Err(err).Msg("Heroic Games Launcher not found in PATH, skipping scanner")
					// Not an error condition - just means Heroic isn't installed
					return results, nil
				}

				// Heroic stores library data in ~/.config/heroic/
				// JSON files: legendary_library.json (Epic), gog_library.json (GOG)
				home, err := os.UserHomeDir()
				if err != nil {
					return results, fmt.Errorf("failed to get user home directory: %w", err)
				}

				heroicConfig := filepath.Join(home, ".config", "heroic")
				if _, err := os.Stat(heroicConfig); os.IsNotExist(err) {
					log.Debug().Msg("Heroic config directory not found")
					return results, nil
				}

				// Note: Full JSON parsing would require library integration
				// For now, we support manual heroic:// URIs through the scheme
				// Future: Implement full library scanning with helpers.ScanHeroicGames()
				log.Debug().Msg("Heroic config found, but scanner not yet implemented")
				return results, nil
			},
			Launch: func(_ *config.Instance, path string) (*os.Process, error) {
				// Extract game app name from heroic://appName format
				appName, err := helpers.ExtractSchemeID(path, shared.SchemeHeroic)
				if err != nil {
					return nil, fmt.Errorf("failed to extract Heroic game name from path: %w", err)
				}

				// Launch via heroic command
				cmd := exec.CommandContext( //nolint:gosec // App name from internal database
					context.Background(),
					"heroic",
					"launch",
					appName,
				)
				err = cmd.Start()
				if err != nil {
					return nil, fmt.Errorf("failed to start Heroic Games Launcher: %w", err)
				}
				return nil, nil //nolint:nilnil // Heroic launches don't return a process handle
			},
		},
		{
			ID:            "Generic",
			Extensions:    []string{".sh"},
			AllowListOnly: true,
			Launch: func(_ *config.Instance, path string) (*os.Process, error) {
				cmd := exec.CommandContext(context.Background(), path)
				if err := cmd.Start(); err != nil {
					return nil, fmt.Errorf("failed to start command: %w", err)
				}
				// Generic launcher can be tracked - return process for lifecycle management
				return cmd.Process, nil
			},
		},
	}

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
