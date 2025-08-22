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

package bazzite

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"
	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/helpers"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/file"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/libnfc"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/opticaldrive"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/simpleserial"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	widgetmodels "github.com/ZaparooProject/zaparoo-core/pkg/ui/widgets/models"
	"github.com/adrg/xdg"
	"github.com/rs/zerolog/log"
)

type Platform struct {
	activeMedia    func() *models.ActiveMedia
	setActiveMedia func(*models.ActiveMedia)
}

func (*Platform) ID() string {
	return platforms.PlatformIDBazzite
}

func (*Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	allReaders := []readers.Reader{
		file.NewReader(cfg),
		simpleserial.NewReader(cfg),
		libnfc.NewReader(cfg),
		opticaldrive.NewReader(cfg),
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

func (*Platform) NormalizePath(_ *config.Instance, path string) string {
	return path
}

func (p *Platform) StopActiveLauncher() error {
	p.setActiveMedia(nil)
	return nil
}

func (*Platform) PlayAudio(_ string) error {
	return nil
}

func (*Platform) LaunchSystem(_ *config.Instance, _ string) error {
	return errors.New("launching systems is not supported")
}

func (p *Platform) LaunchMedia(cfg *config.Instance, path string) error {
	log.Info().Msgf("launch media: %s", path)
	launcher, err := helpers.FindLauncher(cfg, p, path)
	if err != nil {
		return fmt.Errorf("launch media: error finding launcher: %w", err)
	}

	log.Info().Msgf("launch media: using launcher %s for: %s", launcher.ID, path)
	err = helpers.DoLaunch(cfg, p, p.setActiveMedia, &launcher, path)
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
		{
			ID:            "Generic",
			Extensions:    []string{".sh"},
			AllowListOnly: true,
			Launch: func(_ *config.Instance, path string) error {
				return exec.CommandContext(context.Background(), path).Start()
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
