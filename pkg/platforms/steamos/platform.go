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
	"errors"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/libnfc"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/optical_drive"
	"github.com/ZaparooProject/zaparoo-core/pkg/service/tokens"
	"github.com/ZaparooProject/zaparoo-core/pkg/utils"
	"github.com/adrg/xdg"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ZaparooProject/zaparoo-core/pkg/api/models"

	"github.com/ZaparooProject/zaparoo-core/pkg/config"
	"github.com/ZaparooProject/zaparoo-core/pkg/database/gamesdb"
	"github.com/ZaparooProject/zaparoo-core/pkg/platforms"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/file"
	"github.com/ZaparooProject/zaparoo-core/pkg/readers/simple_serial"
	"github.com/rs/zerolog/log"
)

type Platform struct{}

func (p *Platform) Id() string {
	return "steamos"
}

func (p *Platform) SupportedReaders(cfg *config.Instance) []readers.Reader {
	return []readers.Reader{
		file.NewReader(cfg),
		simple_serial.NewReader(cfg),
		libnfc.NewReader(cfg),
		optical_drive.NewReader(cfg),
	}
}

func (p *Platform) Setup(_ *config.Instance, _ chan<- models.Notification) error {
	return os.MkdirAll(filepath.Join(xdg.DataHome, config.AppName), 0755)
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
	return filepath.Join(xdg.DataHome, config.AppName)
}

func (p *Platform) LogDir() string {
	return filepath.Join(xdg.DataHome, config.AppName)
}

func (p *Platform) ConfigDir() string {
	return filepath.Join(xdg.ConfigHome, config.AppName)
}

func (p *Platform) TempDir() string {
	return filepath.Join(os.TempDir(), config.AppName)
}

func (p *Platform) NormalizePath(_ *config.Instance, path string) string {
	return path
}

func LaunchMenu() error {
	return nil
}

func (p *Platform) KillLauncher() error {
	return nil
}

func (p *Platform) LaunchingEnabled() bool {
	return true
}

func (p *Platform) SetLaunching(_ bool) error {
	return nil
}

func (p *Platform) GetActiveLauncher() string {
	// TODO: is this possible?
	return ""
}

func (p *Platform) PlayFailSound(_ *config.Instance) {
}

func (p *Platform) PlaySuccessSound(_ *config.Instance) {
}

func (p *Platform) ActiveSystem() string {
	return ""
}

func (p *Platform) ActiveGame() string {
	// TODO: can this go?
	return ""
}

func (p *Platform) ActiveGameName() string {
	return ""
}

func (p *Platform) ActiveGamePath() string {
	return ""
}

func (p *Platform) LaunchSystem(_ *config.Instance, _ string) error {
	return nil
}

func (p *Platform) LaunchFile(cfg *config.Instance, path string) error {
	log.Info().Msgf("launching file: %s", path)
	launchers := utils.PathToLaunchers(cfg, p, path)
	if len(launchers) == 0 {
		return errors.New("no launcher found")
	}
	return launchers[0].Launch(cfg, path)
}

func (p *Platform) Shell(cmd string) error {
	return exec.Command("bash", "-c", cmd).Start()
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

func (p *Platform) ForwardCmd(_ platforms.CmdEnv) error {
	return nil
}

func (p *Platform) LookupMapping(_ tokens.Token) (string, bool) {
	return "", false
}

func (p *Platform) Launchers() []platforms.Launcher {
	return []platforms.Launcher{
		{
			Id:       "Steam",
			SystemId: gamesdb.SystemPC,
			Schemes:  []string{"steam"},
			Scanner: func(
				cfg *config.Instance,
				systemId string,
				results []platforms.ScanResult,
			) ([]platforms.ScanResult, error) {
				root := "/home/deck/.steam/steam/steamapps"
				appResults, err := utils.ScanSteamApps(root)
				if err != nil {
					return nil, err
				}
				return append(results, appResults...), nil
			},
			Launch: func(cfg *config.Instance, path string) error {
				id := strings.TrimPrefix(path, "steam://")
				id = strings.TrimPrefix(id, "rungameid/")
				return exec.Command(
					"steam",
					"steam://rungameid/"+id,
				).Start()
			},
		},
		{
			Id:            "Generic",
			Extensions:    []string{".sh"},
			AllowListOnly: true,
			Launch: func(cfg *config.Instance, path string) error {
				return exec.Command("bash", "-c", path).Start()
			},
		},
	}
}
