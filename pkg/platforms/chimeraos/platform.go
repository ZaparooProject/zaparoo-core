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

package chimeraos

import (
	"errors"
	widgetModels "github.com/ZaparooProject/zaparoo-core/pkg/configui/widgets/models"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ZaparooProject/zaparoo-core/pkg/readers/libnfc"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/optical_drive"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/adrg/xdg"
	"github.com/rs/zerolog/log"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/file"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/simple_serial"
)

type Platform struct{}

func (p *Platform) ID() string {
	return platforms.PlatformIDChimeraOS
}

func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	return []readers.Reader{
		file.NewReader(cfg),
		simple_serial.NewReader(cfg),
		libnfc.NewReader(cfg),
		optical_drive.NewReader(cfg),
	}
}

func (p *Platform) StartPre(_ *config.Instance) error {
	return nil
}

func (p *Platform) StartPost(
	_ *config.Instance,
	_ func() *models.ActiveMedia,
	_ func(*models.ActiveMedia),
) error {
	return nil
}

func (p *Platform) Stop() error {
	return nil
}

func (p *Platform) AfterScanHook(_ tokens.Token) error {
	return nil
}

func (p *Platform) ReadersUpdateHook(_ map[string]*readers.Reader) error {
	return nil
}

func (p *Platform) RootDirs(_ *config.Instance) []string {
	return []string{}
}

func (p *Platform) ZipsAsDirs() bool {
	return false
}

func (p *Platform) DataDir() string {
	if v, ok := platforms.HasUserDir(); ok {
		return v
	}
	return filepath.Join(xdg.DataHome, config.AppName)
}

func (p *Platform) LogDir() string {
	if v, ok := platforms.HasUserDir(); ok {
		return v
	}
	return filepath.Join(xdg.DataHome, config.AppName)
}

func (p *Platform) ConfigDir() string {
	if v, ok := platforms.HasUserDir(); ok {
		return v
	}
	return filepath.Join(xdg.ConfigHome, config.AppName)
}

func (p *Platform) TempDir() string {
	return filepath.Join(os.TempDir(), config.AppName)
}

func (p *Platform) NormalizePath(_ *config.Instance, path string) string {
	return path
}

func (p *Platform) KillLauncher() error {
	return nil
}

func (p *Platform) PlayFailSound(_ *config.Instance) {
}

func (p *Platform) PlaySuccessSound(_ *config.Instance) {
}

func (p *Platform) LaunchSystem(_ *config.Instance, _ string) error {
	return nil
}

func (p *Platform) LaunchFile(cfg *config.Instance, path string) error {
	launchers := utils.PathToLaunchers(cfg, p, path)
	if len(launchers) == 0 {
		return errors.New("no launcher found")
	}
	launcher := launchers[0]

	if launcher.AllowListOnly && !cfg.IsLauncherFileAllowed(path) {
		return errors.New("file not allowed: " + path)
	}

	log.Info().Msgf("launching file with %s: %s", launcher.Id, path)
	return launcher.Launch(cfg, path)
}

func (p *Platform) KeyboardInput(_ string) error {
	return nil
}

func (p *Platform) KeyboardPress(name string) error {
	return nil
}

func (p *Platform) GamepadPress(name string) error {
	return nil
}

func (p *Platform) ForwardCmd(_ platforms.CmdEnv) (platforms.CmdResult, error) {
	return platforms.CmdResult{}, nil
}

func (p *Platform) LookupMapping(_ tokens.Token) (string, bool) {
	return "", false
}

func (p *Platform) Launchers() []platforms.Launcher {
	return []platforms.Launcher{
		{
			Id:            "Generic",
			Extensions:    []string{".sh"},
			AllowListOnly: true,
			Launch: func(cfg *config.Instance, path string) error {
				return exec.Command(path).Start()
			},
		},
	}
}

func (p *Platform) ShowNotice(
	_ *config.Instance,
	_ widgetModels.NoticeArgs,
) (func() error, time.Duration, error) {
	return nil, 0, nil
}

func (p *Platform) ShowLoader(
	_ *config.Instance,
	_ widgetModels.NoticeArgs,
) (func() error, error) {
	return nil, nil
}

func (p *Platform) ShowPicker(
	_ *config.Instance,
	_ widgetModels.PickerArgs,
) error {
	return nil
}
