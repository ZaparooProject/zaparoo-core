//go:build linux

/*
Zaparoo Core
Copyright (C) 2024 Callan Barrett

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

package steamos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ZaparooProject/zaparoo-core/v2/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/database/systemdefs"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/v2/pkg/platforms"
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
	activeMedia    func() *models.ActiveMedia
	setActiveMedia func(*models.ActiveMedia)
	trackedProcess *os.Process
	processMu      sync.RWMutex
}

func (*Platform) ID() string {
	return platforms.PlatformIDSteamOS
}

func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	allReaders := []readers.Reader{
		pn532.NewReader(cfg),
		libnfc.NewACR122Reader(cfg),
		file.NewReader(cfg),
		simpleserial.NewReader(cfg),
		opticaldrive.NewReader(cfg),
		mqtt.NewReader(cfg),
		externaldrive.NewReader(cfg),
		tty2oled.NewReader(cfg, p),
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
	_ platforms.LauncherContextManager,
	activeMedia func() *models.ActiveMedia,
	setActiveMedia func(*models.ActiveMedia),
) error {
	p.activeMedia = activeMedia
	p.setActiveMedia = setActiveMedia
	return nil
}

func (*Platform) Stop() error {
	return nil
}

func (p *Platform) SetTrackedProcess(proc *os.Process) {
	p.processMu.Lock()
	defer p.processMu.Unlock()
	p.trackedProcess = proc
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

func (p *Platform) StopActiveLauncher() error {
	// Kill tracked process if it exists
	p.processMu.Lock()
	if p.trackedProcess != nil {
		if err := p.trackedProcess.Kill(); err != nil {
			log.Warn().Err(err).Msg("failed to kill tracked process")
		} else {
			log.Debug().Msg("killed tracked process")
		}
		p.trackedProcess = nil
	}
	p.processMu.Unlock()

	p.setActiveMedia(nil)
	return nil
}

func (*Platform) PlayAudio(_ string) error {
	return nil
}

func (*Platform) LaunchSystem(_ *config.Instance, _ string) error {
	return errors.New("launching systems is not supported")
}

func (p *Platform) LaunchMedia(cfg *config.Instance, path string, launcher *platforms.Launcher) error {
	log.Info().Msgf("launch media: %s", path)

	if launcher == nil {
		foundLauncher, err := helpers.FindLauncher(cfg, p, path)
		if err != nil {
			return fmt.Errorf("launch media: error finding launcher: %w", err)
		}
		launcher = &foundLauncher
	}

	log.Info().Msgf("launch media: using launcher %s for: %s", launcher.ID, path)
	err := helpers.DoLaunch(cfg, p, p.setActiveMedia, launcher, path)
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
	const fallbackPath = "/home/deck/.steam/steam"

	// Check for user-configured Steam install directory first
	if def, ok := cfg.LookupLauncherDefaults("Steam"); ok && def.InstallDir != "" {
		if _, err := os.Stat(def.InstallDir); err == nil {
			log.Debug().Msgf("using user-configured Steam directory: %s", def.InstallDir)
			return def.InstallDir
		}
		log.Warn().Msgf("user-configured Steam directory not found: %s", def.InstallDir)
	}

	// Try common Steam installation paths
	home, err := os.UserHomeDir()
	if err != nil {
		log.Warn().Err(err).Msg("failed to get user home directory")
		return fallbackPath
	}

	paths := []string{
		filepath.Join(home, ".steam", "steam"),
		filepath.Join(home, ".local", "share", "Steam"),
		"/home/deck/.steam/steam", // Steam Deck default
		"/usr/games/steam",
		"/opt/steam",
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			log.Debug().Msgf("found Steam installation: %s", path)
			return path
		}
	}

	log.Debug().Msgf("Steam detection failed, using fallback: %s", fallbackPath)
	return fallbackPath
}

func (p *Platform) Launchers(cfg *config.Instance) []platforms.Launcher {
	launchers := []platforms.Launcher{
		{
			ID:       "Steam",
			SystemID: systemdefs.SystemPC,
			Schemes:  []string{"steam"},
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
				id := strings.TrimPrefix(path, "steam://")
				id = strings.TrimPrefix(id, "rungameid/")
				id = strings.SplitN(id, "/", 2)[0]

				if _, err := strconv.ParseUint(id, 10, 64); err != nil {
					return nil, fmt.Errorf("invalid Steam game ID: %s", id)
				}

				err := exec.CommandContext( //nolint:gosec // Steam ID validated as numeric-only above
					context.Background(),
					"steam",
					"steam://rungameid/"+id,
				).Start()
				if err != nil {
					return nil, fmt.Errorf("failed to start steam: %w", err)
				}
				return nil, nil //nolint:nilnil // Steam launches don't return a process handle
			},
		},
		{
			ID:            "Generic",
			Extensions:    []string{".sh"},
			AllowListOnly: true,
			Launch: func(_ *config.Instance, path string) (*os.Process, error) {
				err := exec.CommandContext(context.Background(), "bash", "-c", path).Start()
				if err != nil {
					return nil, fmt.Errorf("failed to start command: %w", err)
				}
				return nil, nil //nolint:nilnil // Shell script launches don't return a process handle
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
